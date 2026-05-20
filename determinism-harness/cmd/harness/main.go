// harness — Phase 1 determinism validator for the aios marketplace.
//
// Subscribes to the chain's ResultSubmitted SSE events. For each finalized
// request, the harness independently replays the inference (calling the same
// llama-server the inference-node used) and compares the output hash against
// the attestation submitted on-chain.
//
// Pass = both nodes (the actual provider + the harness re-runner) produce the
// same output hash, proving the (model, runtime, hardware, precision) tuple is
// reproducible. This is exactly the property Phase 3's fraud-proof challenge
// will rely on; the harness is the challenger role rehearsed without slashing.
//
// Failure modes (each logged):
//   - DIVERGENT — provider's output_hash differs from re-run.
//   - UNREACHABLE — couldn't contact llama-server or chain.
//   - MISSING_INPUT — request used input_uri other than inline:<hash> and we
//     can't fetch the bytes. Phase 0.5 only generates inline URIs, so this
//     should not happen in the demo.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type Result struct {
	RequestID            uint64 `json:"request_id"`
	ServiceID            uint64 `json:"service_id"`
	VerificationDomainID uint64 `json:"verification_domain_id"`
	ChainOutputHash      string `json:"chain_output_hash"`
	ReplayOutputHash     string `json:"replay_output_hash"`
	Verdict              string `json:"verdict"` // OK, DIVERGENT, ERROR, SKIPPED
	Note                 string `json:"note,omitempty"`
	ChallengeFiled       bool   `json:"challenge_filed,omitempty"`
	VouchFiled           bool   `json:"vouch_filed,omitempty"` // Phase 3.y
	CheckedAt            string `json:"checked_at"`
}

// signer holds the harness's chain key (loaded from the keyring at startup).
type signer struct {
	name    string
	addr    string
	pubHex  string
	privKey ed25519.PrivateKey
}

type Harness struct {
	chainURL string
	llamaURL string
	logger   *zap.Logger
	client   *http.Client

	signer *signer // nil if not loaded; harness still observes but cannot challenge

	mu      sync.Mutex
	results []Result // bounded — last 100 for /report
}

const maxStoredResults = 100

func main() {
	chainURL := flag.String("chain", envOr("CHAIN_URL", "http://chain:26657"), "Chain HTTP URL")
	llamaURL := flag.String("llama", envOr("LLAMA_SERVER_URL", "http://llama-server:8080"), "Independent llama-server endpoint to replay against")
	listenAddr := flag.String("listen", envOr("LISTEN", ":8090"), "HTTP listen address for /report and /health")
	logLevel := flag.String("log-level", envOr("LOG_LEVEL", "info"), "Log level")
	keyringPath := flag.String("keyring", envOr("KEYRING_PATH", "/keys/keys.json"), "Path to keys.json (for filing challenges)")
	keyName := flag.String("key-name", envOr("HARNESS_KEY", "harness"), "Dev key name to load for filing challenges")
	flag.Parse()

	logger, err := newLogger(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	h := &Harness{
		chainURL: strings.TrimRight(*chainURL, "/"),
		llamaURL: strings.TrimRight(*llamaURL, "/"),
		logger:   logger,
		client:   &http.Client{Timeout: 90 * time.Second},
	}

	// Try to load the harness key. Optional — without it the harness still
	// observes and reports verdicts, but cannot file MsgChallenge on DIVERGENT.
	if s, err := loadSigner(*keyringPath, *keyName); err != nil {
		logger.Warn("could not load harness key; observation-only mode",
			zap.String("keyring", *keyringPath), zap.String("name", *keyName), zap.Error(err))
	} else {
		h.signer = s
		logger.Info("loaded harness key for filing challenges",
			zap.String("name", s.name), zap.String("address", s.addr))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handleSignals(cancel, logger)

	// Wait for chain to be reachable.
	if err := waitChainReady(ctx, *chainURL, logger); err != nil {
		logger.Fatal("chain not reachable", zap.Error(err))
	}

	// HTTP server for /health and /report
	srv := h.httpServer(*listenAddr)
	go func() {
		<-ctx.Done()
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		logger.Info("harness listening", zap.String("addr", *listenAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http", zap.Error(err))
		}
	}()

	// Main loop: subscribe to ResultSubmitted.
	h.runWatchLoop(ctx)
}

func (h *Harness) runWatchLoop(ctx context.Context) {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		err := h.subscribe(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		h.logger.Warn("event stream ended; reconnecting", zap.Error(err), zap.Duration("backoff", backoff))
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

func (h *Harness) subscribe(ctx context.Context) error {
	// Phase 3.y: subscribe to ResultSubmitted (so we can challenge if needed)
	// AND Challenged (so we can defend honest providers by vouching when
	// someone else has already filed a challenge).
	url := h.chainURL + "/events?types=ResultSubmitted,Challenged"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	// no timeout for SSE
	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe status %d", resp.StatusCode)
	}
	h.logger.Info("subscribed to chain events",
		zap.String("chain", h.chainURL),
		zap.String("types", "ResultSubmitted,Challenged"))

	reader := bufio.NewReader(resp.Body)
	for {
		if ctx.Err() != nil {
			return nil
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read stream: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Type != "ResultSubmitted" && ev.Type != "Challenged" {
			continue
		}
		var p struct {
			RequestID uint64 `json:"request_id"`
		}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			continue
		}
		go h.checkRequest(ctx, p.RequestID)
	}
}

func (h *Harness) checkRequest(ctx context.Context, requestID uint64) {
	r := Result{RequestID: requestID, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	defer func() { h.store(r) }()

	// Fetch the request from the chain. In Phase 3 we're called on
	// ResultSubmitted, so the request is in SUBMITTED state (the challenge
	// window is still open). We re-run and challenge before it auto-finalizes.
	full, err := h.fetchRequest(ctx, requestID)
	if err != nil {
		r.Verdict = "ERROR"
		r.Note = "fetch request: " + err.Error()
		h.logger.Error("fetch request", zap.Uint64("request_id", requestID), zap.Error(err))
		return
	}
	r.ServiceID = full.ServiceID
	if full.Result == nil {
		r.Verdict = "ERROR"
		r.Note = "no result on submitted request"
		return
	}
	r.VerificationDomainID = full.Result.Attestation.VerificationDomainID
	r.ChainOutputHash = full.Result.OutputHash

	// Phase 1: harness only re-runs requests whose input_uri is inline.
	if !strings.HasPrefix(full.InputURI, "inline:") {
		r.Verdict = "SKIPPED"
		r.Note = "input_uri not inline; phase 2+ adds content-addressed fetch"
		return
	}

	prompt := full.InputText
	if prompt == "" {
		r.Verdict = "ERROR"
		r.Note = "input_text empty"
		return
	}

	// Replay via llama-server. We re-derive output and compute the same hash
	// the inference-node would have computed.
	output, err := h.replay(ctx, prompt)
	if err != nil {
		r.Verdict = "ERROR"
		r.Note = "replay: " + err.Error()
		return
	}
	hash := sha256.Sum256([]byte(output))
	r.ReplayOutputHash = hex.EncodeToString(hash[:])

	if r.ReplayOutputHash == r.ChainOutputHash {
		r.Verdict = "OK"
		h.logger.Info("determinism verified",
			zap.Uint64("request_id", requestID),
			zap.String("output_hash", r.ChainOutputHash))

		// Phase 3.y: if the request is already CHALLENGED, file a voucher
		// supporting the provider. This defends honest providers against
		// spurious challenges. If status is SUBMITTED, there's no challenge
		// to vouch on — the harness's "OK" verdict is just an observation.
		if full.Status == "CHALLENGED" && h.signer != nil {
			if err := h.fileVouch(ctx, requestID, r.ReplayOutputHash, full); err != nil {
				r.Note += "; vouch failed: " + err.Error()
				h.logger.Error("file vouch", zap.Uint64("request_id", requestID), zap.Error(err))
				return
			}
			r.VouchFiled = true
			r.Note += "vouch filed supporting provider"
			h.logger.Info("vouch filed supporting provider",
				zap.Uint64("request_id", requestID),
				zap.String("voucher", h.signer.addr))
		}
		return
	}

	r.Verdict = "DIVERGENT"
	r.Note = "chain output_hash != harness re-run output_hash"
	h.logger.Warn("DIVERGENT OUTPUTS — provider lied or runtime is non-deterministic",
		zap.Uint64("request_id", requestID),
		zap.String("chain_hash", r.ChainOutputHash),
		zap.String("replay_hash", r.ReplayOutputHash))

	if h.signer == nil {
		r.Note += " (observation-only mode; no challenge filed)"
		return
	}

	// Phase 3.y: if a challenge is already filed (status CHALLENGED) AND
	// our replay matches the existing challenger's hash, vouch for the
	// challenger. If our replay matches neither, we silently abstain.
	if full.Status == "CHALLENGED" {
		// Check if a challenge already matches our replay.
		for _, c := range full.Challenges {
			if c.Attestation.OutputHash == r.ReplayOutputHash {
				if err := h.fileVouch(ctx, requestID, r.ReplayOutputHash, full); err != nil {
					r.Note += "; vouch (challenger-side) failed: " + err.Error()
					return
				}
				r.VouchFiled = true
				r.Note += "; vouch filed supporting challenger"
				h.logger.Info("vouch filed supporting challenger",
					zap.Uint64("request_id", requestID))
				return
			}
		}
		r.Note += "; CHALLENGED but our replay matches neither side"
		return
	}

	// Status is SUBMITTED with divergence — file the challenge ourselves.
	if err := h.fileChallenge(ctx, requestID, r.ReplayOutputHash, full); err != nil {
		r.Note += "; challenge failed: " + err.Error()
		h.logger.Error("file challenge", zap.Uint64("request_id", requestID), zap.Error(err))
		return
	}
	r.ChallengeFiled = true
	r.Note += "; challenge filed"
	h.logger.Info("challenge filed",
		zap.Uint64("request_id", requestID),
		zap.String("challenger", h.signer.addr))
}

// fileVouch posts MsgVouch with our attestation. Used in two cases:
//   (1) ResultSubmitted seen but the request was already CHALLENGED by someone
//       else before we re-ran. We vouch for whichever side our hash matches.
//   (2) Challenged event for a request whose ResultSubmitted we already saw
//       and verified. (Same code path — re-running again is cheap and safe.)
func (h *Harness) fileVouch(ctx context.Context, requestID uint64, ourHash string, full fullRequest) error {
	dom, err := h.fetchDomain(ctx, full.Result.Attestation.VerificationDomainID)
	if err != nil {
		return fmt.Errorf("fetch domain: %w", err)
	}
	att := map[string]any{
		"provider":               h.signer.addr,
		"verification_domain_id": dom.ID,
		"model_sha256":           dom.ModelSHA256,
		"runtime_id":             dom.RuntimeID,
		"hardware_tag":           dom.HardwareTag,
		"precision":              dom.Precision,
		"tokenizer_id":           dom.TokenizerID,
		"input_hash":             full.InputHash,
		"output_hash":            ourHash,
		"produced_at_height":     0,
		"signature_hex":          "",
	}
	payload := map[string]any{
		"voucher":     h.signer.addr,
		"request_id":  requestID,
		"attestation": att,
	}
	return h.signAndSubmit(ctx, "vouch", payload)
}

func (h *Harness) fileChallenge(ctx context.Context, requestID uint64, replayHash string, full fullRequest) error {
	// Fetch the service's verification domain so the attestation tuple matches.
	dom, err := h.fetchDomain(ctx, full.Result.Attestation.VerificationDomainID)
	if err != nil {
		return fmt.Errorf("fetch domain: %w", err)
	}

	att := map[string]any{
		"provider":               h.signer.addr,
		"verification_domain_id": dom.ID,
		"model_sha256":           dom.ModelSHA256,
		"runtime_id":             dom.RuntimeID,
		"hardware_tag":           dom.HardwareTag,
		"precision":              dom.Precision,
		"tokenizer_id":           dom.TokenizerID,
		"input_hash":             full.InputHash,
		"output_hash":            replayHash,
		"produced_at_height":     0,
		"signature_hex":          "", // Phase 3 simple: chain doesn't verify the inner attestation sig
	}
	payload := map[string]any{
		"challenger":  h.signer.addr,
		"request_id":  requestID,
		"attestation": att,
	}
	return h.signAndSubmit(ctx, "challenge", payload)
}

type domainView struct {
	ID          uint64 `json:"id"`
	ModelSHA256 string `json:"model_sha256"`
	RuntimeID   string `json:"runtime_id"`
	HardwareTag string `json:"hardware_tag"`
	Precision   string `json:"precision"`
	// Phase 1: optional. Empty when the domain doesn't pin a tokenizer
	// (legacy domains keep working).
	TokenizerID string `json:"tokenizer_id,omitempty"`
}

func (h *Harness) fetchDomain(ctx context.Context, id uint64) (domainView, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/domains/%d", h.chainURL, id), nil)
	if err != nil {
		return domainView{}, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return domainView{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domainView{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var d domainView
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return domainView{}, err
	}
	return d, nil
}

// signedTx mirrors chain/internal/types.SignedTx exactly. Go's encoding/json
// marshals struct fields in DECLARATION ORDER, so this struct gives us the
// stable canonical encoding the chain expects. Maps would marshal
// alphabetically and break signature verification.
type signedTx struct {
	Type         string          `json:"type"`
	Nonce        uint64          `json:"nonce"`
	PubKeyHex    string          `json:"pub_key_hex"`
	SignatureHex string          `json:"signature_hex"`
	Payload      json.RawMessage `json:"payload"`
}

func (h *Harness) signAndSubmit(ctx context.Context, txType string, payload any) error {
	payloadBz, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Fetch our nonce.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/accounts/%s", h.chainURL, h.signer.addr), nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get nonce status %d", resp.StatusCode)
	}
	var acc struct {
		Nonce uint64 `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&acc); err != nil {
		return err
	}

	tx := signedTx{
		Type:      txType,
		Nonce:     acc.Nonce,
		PubKeyHex: h.signer.pubHex,
		Payload:   payloadBz,
		// SignatureHex deliberately empty for canonicalization.
	}
	canonical, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(h.signer.privKey, canonical)
	tx.SignatureHex = hex.EncodeToString(sig)

	body, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.chainURL+"/tx", bytes.NewReader(body))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := h.client.Do(postReq)
	if err != nil {
		return err
	}
	defer postResp.Body.Close()
	respBody, _ := io.ReadAll(postResp.Body)
	if postResp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /tx %d: %s", postResp.StatusCode, string(respBody))
	}
	return nil
}

func loadSigner(path, name string) (*signer, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r struct {
		Keys []struct {
			Name       string `json:"name"`
			Address    string `json:"address"`
			PubKeyHex  string `json:"pub_key_hex"`
			PrivKeyHex string `json:"priv_key_hex"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(bz, &r); err != nil {
		return nil, err
	}
	for _, k := range r.Keys {
		if k.Name != name {
			continue
		}
		priv, err := hex.DecodeString(k.PrivKeyHex)
		if err != nil {
			return nil, err
		}
		return &signer{
			name:    name,
			addr:    k.Address,
			pubHex:  k.PubKeyHex,
			privKey: ed25519.PrivateKey(priv),
		}, nil
	}
	return nil, fmt.Errorf("key %q not found in %s", name, path)
}

type fullRequest struct {
	ID              uint64  `json:"id"`
	ServiceID       uint64  `json:"service_id"`
	InputHash       string  `json:"input_hash"`
	InputURI        string  `json:"input_uri"`
	InputText       string  `json:"input_text"`
	Status          string  `json:"status"`
	Result          *struct {
		OutputHash  string `json:"output_hash"`
		OutputURI   string `json:"output_uri"`
		Attestation struct {
			Provider             string `json:"provider"`
			VerificationDomainID uint64 `json:"verification_domain_id"`
			ModelSHA256          string `json:"model_sha256"`
			RuntimeID            string `json:"runtime_id"`
			HardwareTag          string `json:"hardware_tag"`
			Precision            string `json:"precision"`
		} `json:"attestation"`
	} `json:"result,omitempty"`
	// Phase 3.y: see existing challenges so the harness can decide whether
	// its OK verdict translates to a provider-side vouch.
	Challenges []struct {
		Challenger  string `json:"challenger"`
		Attestation struct {
			OutputHash string `json:"output_hash"`
		} `json:"attestation"`
	} `json:"challenges,omitempty"`
}

func (h *Harness) fetchRequest(ctx context.Context, id uint64) (fullRequest, error) {
	url := fmt.Sprintf("%s/requests/%d", h.chainURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fullRequest{}, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fullRequest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fullRequest{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var full fullRequest
	if err := json.NewDecoder(resp.Body).Decode(&full); err != nil {
		return fullRequest{}, err
	}
	return full, nil
}

type completionRequest struct {
	Prompt      string  `json:"prompt"`
	NPredict    int     `json:"n_predict"`
	Temperature float64 `json:"temperature"`
	TopK        int     `json:"top_k"`
	TopP        float64 `json:"top_p"`
	Stream      bool    `json:"stream"`
}

type completionResponse struct {
	Content string `json:"content"`
}

func (h *Harness) replay(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(completionRequest{
		Prompt: prompt, NPredict: 256, Temperature: 0, TopK: 1, TopP: 1, Stream: false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.llamaURL+"/completion", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llama status %d: %s", resp.StatusCode, string(rb))
	}
	var out completionResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return "", err
	}
	return out.Content, nil
}

func (h *Harness) store(r Result) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = append(h.results, r)
	if len(h.results) > maxStoredResults {
		h.results = h.results[len(h.results)-maxStoredResults:]
	}
}

func (h *Harness) httpServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /report", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		out := struct {
			Items []Result `json:"items"`
			Counts struct {
				OK        int `json:"ok"`
				Divergent int `json:"divergent"`
				Skipped   int `json:"skipped"`
				Errors    int `json:"errors"`
			} `json:"counts"`
		}{Items: append([]Result(nil), h.results...)}
		for _, r := range h.results {
			switch r.Verdict {
			case "OK":
				out.Counts.OK++
			case "DIVERGENT":
				out.Counts.Divergent++
			case "SKIPPED":
				out.Counts.Skipped++
			default:
				out.Counts.Errors++
			}
		}
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	return &http.Server{
		Addr:         addr,
		Handler:      withCORS(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}

func waitChainReady(ctx context.Context, chainURL string, logger *zap.Logger) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r, err := (&http.Client{Timeout: 3 * time.Second}).Get(chainURL + "/status")
		if err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				logger.Info("chain ready", zap.String("url", chainURL))
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("chain not reachable at %s", chainURL)
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		h.ServeHTTP(w, r)
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if err := cfg.Level.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	return cfg.Build()
}

func handleSignals(cancel context.CancelFunc, logger *zap.Logger) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	logger.Info("signal received", zap.String("signal", sig.String()))
	cancel()
}
