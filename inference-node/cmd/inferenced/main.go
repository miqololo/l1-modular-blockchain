// inferenced — Phase 0.5 inference worker.
//
// Subscribes to the chain's /events SSE stream, processes InferenceRequested
// events for services owned by THIS node's provider account, calls llama-server,
// signs an attestation, and broadcasts MsgSubmitResult to /tx.
//
// PHASE 0.5: real inference, NOT verifiable. See inference-node/CLAUDE.md.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/aios/inference-node/internal/chain"
	"github.com/aios/inference-node/internal/executor/llama_http"
)

func main() {
	chainURL := flag.String("chain", envOr("CHAIN_URL", "http://chain:26657"), "Chain HTTP URL")
	keyringPath := flag.String("keyring", envOr("KEYRING_PATH", "/keys/keys.json"), "Path to keys.json")
	keyName := flag.String("key-name", envOr("PROVIDER_KEY", "bob"), "Dev key name to load")
	llamaURL := flag.String("llama", envOr("LLAMA_SERVER_URL", "http://llama-server:8080"), "llama-server URL")
	hardwareTag := flag.String("hw-tag", envOr("HARDWARE_TAG", "cpu-x86_64-tinyllama-q4"), "Hardware tag")
	tokenizerID := flag.String("tokenizer-id", envOr("TOKENIZER_ID", ""), "Tokenizer identifier for attestations (Phase 1; empty for legacy domains)")
	// Phase 1 additions:
	modelPath := flag.String("model-path", envOr("MODEL_PATH", "/models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"), "Path to the model file (for SHA-256 fingerprinting)")
	modelSHAOverride := flag.String("model-sha", envOr("MODEL_SHA256", ""), "Override computed model SHA-256 (use for verifying against a remote pin)")
	precision := flag.String("precision", envOr("PRECISION", "q4_k_m"), "Numerical precision tag for attestations")
	runtimeID := flag.String("runtime-id", envOr("RUNTIME_ID", "llama.cpp-server"), "Runtime identifier for attestations")
	logLevel := flag.String("log-level", envOr("LOG_LEVEL", "info"), "Log level")
	flag.Parse()

	logger, err := newLogger(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Warn("PHASE 0.5: real inference, UNVERIFIED — see inference-node/CLAUDE.md")

	// Wait for the keyring to exist (chain may still be initializing).
	if err := waitFor(*keyringPath, 60*time.Second, logger); err != nil {
		logger.Fatal("keyring not available", zap.Error(err))
	}

	signer, err := chain.LoadSigner(*keyringPath, *keyName)
	if err != nil {
		logger.Fatal("load signer", zap.Error(err))
	}
	logger.Info("loaded provider key",
		zap.String("name", *keyName),
		zap.String("address", signer.Address()))

	cli := chain.NewClient(*chainURL, logger)

	// Wait for the chain to respond at /status.
	if err := waitChainReady(cli, logger); err != nil {
		logger.Fatal("chain not ready", zap.Error(err))
	}

	exec, err := llama_http.NewExecutor(*llamaURL, *hardwareTag, logger)
	if err != nil {
		logger.Fatal("executor init", zap.Error(err))
	}

	// Wait for llama-server to respond.
	if err := exec.WaitReady(60 * time.Second); err != nil {
		logger.Fatal("llama-server not ready", zap.Error(err))
	}

	// Phase 1: compute / load the model fingerprint at startup. This is what
	// gets embedded in every attestation. If the file is unavailable we still
	// run (with a placeholder string), but the chain's domain check will reject
	// our submissions if the service references a non-zero domain.
	modelSHA := *modelSHAOverride
	if modelSHA == "" {
		s, err := sha256File(*modelPath)
		if err != nil {
			logger.Warn("could not compute model SHA-256; submissions to verified domains will fail",
				zap.String("path", *modelPath), zap.Error(err))
			modelSHA = "unverified-tinyllama-q4"
		} else {
			modelSHA = s
		}
	}
	logger.Info("model fingerprint",
		zap.String("model_path", *modelPath),
		zap.String("model_sha256", modelSHA),
		zap.String("runtime_id", *runtimeID),
		zap.String("precision", *precision),
		zap.String("hardware_tag", *hardwareTag),
		zap.String("tokenizer_id", *tokenizerID))

	cfg := nodeConfig{
		modelSHA256: modelSHA,
		runtimeID:   *runtimeID,
		precision:   *precision,
		hardwareTag: *hardwareTag,
		tokenizerID: *tokenizerID,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handleSignals(cancel, logger)

	runLoop(ctx, cli, signer, exec, cfg, logger)
}

// nodeConfig bundles attestation pinning fields.
type nodeConfig struct {
	modelSHA256 string
	runtimeID   string
	precision   string
	hardwareTag string
	tokenizerID string
}

// sha256File computes the SHA-256 of a file in lowercase hex.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func runLoop(ctx context.Context, cli *chain.Client, signer *chain.Signer, exec *llama_http.Executor, cfg nodeConfig, logger *zap.Logger) {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return
		}
		err := cli.SubscribeEvents(ctx, []string{"InferenceRequested"}, func(ev chain.Event) {
			handleEvent(ctx, cli, signer, exec, cfg, ev, logger)
		})
		if err == nil || ctx.Err() != nil {
			return
		}
		logger.Warn("event stream ended; reconnecting", zap.Error(err), zap.Duration("backoff", backoff))
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

func handleEvent(ctx context.Context, cli *chain.Client, signer *chain.Signer, exec *llama_http.Executor, cfg nodeConfig, ev chain.Event, logger *zap.Logger) {
	var payload chain.InferenceRequestedPayload
	if err := ev.Decode(&payload); err != nil {
		logger.Warn("decode event", zap.Error(err))
		return
	}

	// Only handle requests for services owned by us.
	svc, err := cli.GetService(payload.ServiceID)
	if err != nil {
		logger.Warn("get service", zap.Uint64("service_id", payload.ServiceID), zap.Error(err))
		return
	}
	if svc.Owner != signer.Address() {
		logger.Debug("not our service; skipping",
			zap.Uint64("service_id", payload.ServiceID),
			zap.String("owner", svc.Owner),
			zap.String("ours", signer.Address()))
		return
	}

	// Check the request is still PENDING (could have been finalized already).
	req, err := cli.GetRequest(payload.RequestID)
	if err != nil {
		logger.Warn("get request", zap.Uint64("request_id", payload.RequestID), zap.Error(err))
		return
	}
	if req.Status != "PENDING" {
		logger.Debug("request not pending; skipping",
			zap.Uint64("request_id", payload.RequestID),
			zap.String("status", req.Status))
		return
	}

	l := logger.With(
		zap.Uint64("request_id", payload.RequestID),
		zap.Uint64("service_id", payload.ServiceID),
		zap.String("input_hash", payload.InputHash))
	l.Info("handling inference request")

	// Resolve the prompt. PHASE 0.5: input_text is inlined on the event for the demo.
	prompt := payload.InputText
	if prompt == "" {
		prompt = fmt.Sprintf("Request %d. Respond briefly.", payload.RequestID)
	}

	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := exec.Execute(execCtx, llama_http.Request{Prompt: prompt})
	if err != nil {
		l.Error("execute failed", zap.Error(err))
		return
	}

	// PHASE 3 DEMO ONLY. If MALICIOUS_PROVIDER=1, swap the real LLM output
	// for fabricated bytes. output_hash is recomputed so the chain accepts it
	// (input_hash still matches); the bundled determinism-harness will then
	// re-run honestly, detect divergence, and file MsgChallenge during the
	// challenge window.
	if os.Getenv("MALICIOUS_PROVIDER") == "1" {
		fake := fmt.Sprintf("MALICIOUS_PROVIDER=1 output for request %d. Honest output discarded.", payload.RequestID)
		fh := sha256.Sum256([]byte(fake))
		result = llama_http.Result{
			Output:     fake,
			OutputHash: hex.EncodeToString(fh[:]),
			OutputURI:  "inline:" + hex.EncodeToString(fh[:]),
		}
		l.Warn("MALICIOUS_PROVIDER mode: submitting fabricated output",
			zap.String("fake_output_hash", result.OutputHash))
	}

	l.Info("inference produced",
		zap.String("output_hash", result.OutputHash),
		zap.Int("output_len", len(result.Output)))

	// Phase 1 attestation: real model SHA, domain ID, precision, tokenizer.
	attestation := chain.Attestation{
		Provider:             signer.Address(),
		VerificationDomainID: svc.VerificationDomainID,
		ModelSHA256:          cfg.modelSHA256,
		RuntimeID:            cfg.runtimeID,
		HardwareTag:          cfg.hardwareTag,
		Precision:            cfg.precision,
		TokenizerID:          cfg.tokenizerID,
		InputHash:            payload.InputHash,
		OutputHash:           result.OutputHash,
		ProducedAt:           cli.Height(),
	}
	// Phase 0.5 signature: SHA-256(canonical(attestation)) signed with the
	// provider's chain key. Phase 1 swaps in a typed bytes encoding.
	sig := signer.SignAttestation(attestation)
	attestation.SignatureHex = sig

	msg := chain.MsgSubmitResult{
		Provider:  signer.Address(),
		RequestID: payload.RequestID,
		Result: chain.Result{
			OutputHash:  result.OutputHash,
			OutputURI:   result.OutputURI,
			OutputText:  result.Output,
			Attestation: attestation,
		},
	}
	resp, err := cli.SubmitTxAs(signer, chain.TxSubmitResult, msg)
	if err != nil {
		l.Error("submit-result tx failed", zap.Error(err))
		return
	}
	l.Info("result finalized", zap.Bool("finalized", resp.Finalized))
}

func waitFor(path string, timeout time.Duration, logger *zap.Logger) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		logger.Info("waiting for file", zap.String("path", path))
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s", path)
}

func waitChainReady(cli *chain.Client, logger *zap.Logger) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := cli.Status(); err == nil {
			logger.Info("chain ready", zap.String("chain_id", status.ChainID), zap.Int64("height", status.Height))
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("chain not reachable")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if err := cfg.Level.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
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
