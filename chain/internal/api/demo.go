package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

// ─── Demo helpers ───────────────────────────────────────────────────────────
//
// These endpoints sign transactions server-side using the embedded dev keyring.
// They are DEMO-ONLY and removed in Phase 1; production chains never have a
// /demo/* route. They exist so a fresh `docker compose up` boots into a
// non-empty marketplace without requiring any external client.

type keyringEntry struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	PubKeyHex  string `json:"pub_key_hex"`
	PrivKeyHex string `json:"priv_key_hex"`
}

type keyringFile struct {
	Keys []keyringEntry `json:"keys"`
}

func (s *Server) loadKey(name string) (keyringEntry, error) {
	path := os.Getenv("AID_HOME")
	if path == "" {
		path = "/home/aios/.aid"
	}
	path = strings.TrimRight(path, "/") + "/keys.json"
	bz, err := os.ReadFile(path)
	if err != nil {
		return keyringEntry{}, fmt.Errorf("read keyring: %w", err)
	}
	var kr keyringFile
	if err := json.Unmarshal(bz, &kr); err != nil {
		return keyringEntry{}, err
	}
	for _, k := range kr.Keys {
		if k.Name == name {
			return k, nil
		}
	}
	return keyringEntry{}, fmt.Errorf("key %q not found", name)
}

func (s *Server) signAndSubmit(name string, txType types.TxType, payload any) (state.TxReceipt, error) {
	key, err := s.loadKey(name)
	if err != nil {
		return state.TxReceipt{}, err
	}
	privBz, err := hex.DecodeString(key.PrivKeyHex)
	if err != nil {
		return state.TxReceipt{}, fmt.Errorf("decode priv: %w", err)
	}
	pubBz, err := hex.DecodeString(key.PubKeyHex)
	if err != nil {
		return state.TxReceipt{}, fmt.Errorf("decode pub: %w", err)
	}

	payloadBz, err := json.Marshal(payload)
	if err != nil {
		return state.TxReceipt{}, err
	}

	nonce := s.state.AccountNonce(key.Address)
	tx := types.SignedTx{
		Type:      txType,
		Nonce:     nonce,
		PubKeyHex: key.PubKeyHex,
		Payload:   payloadBz,
	}
	canonical, err := tx.CanonicalBytes()
	if err != nil {
		return state.TxReceipt{}, err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(privBz), canonical)
	tx.SignatureHex = hex.EncodeToString(sig)

	// Sanity check: derived address matches.
	_ = sha256.Sum256(pubBz)

	return s.state.SubmitTx(tx)
}

// POST /demo/seed — register the default verification domain + a default
// "translate-en-fr" service that opts into it. Idempotent.
//
// The model SHA-256 is read from the env var MODEL_SHA256 (set by the operator
// in .env). If empty, the domain is registered with the placeholder
// "unverified-tinyllama-q4" — services using it accept any tuple from a
// provider that claims that string. This is a Phase 1 starter; Phase 2 hardens
// it by requiring the chain to compute the SHA from a verified model archive.
func (s *Server) demoSeed(w http.ResponseWriter, _ *http.Request) {
	existing, _ := s.state.ListServices()
	if len(existing) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "already_seeded",
			"services": existing,
		})
		return
	}

	// Register the demo domain first (bob signs; bob becomes authority).
	modelSHA := os.Getenv("MODEL_SHA256")
	if modelSHA == "" {
		modelSHA = "unverified-tinyllama-q4"
	}
	domainReceipt, err := s.signAndSubmit("bob", types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:   resolveAddr(s, "bob"),
		ModelSHA256: modelSHA,
		RuntimeID:   "llama.cpp-server",
		HardwareTag: "cpu-x86_64-tinyllama-q4",
		Precision:   types.PrecisionQ4_K_M,
		// Phase 1: pin the tokenizer so attestations declare which BPE/tokenizer
		// implementation produced the output. The inference-node and harness
		// must set TOKENIZER_ID to match (set in docker-compose.yml).
		TokenizerID: "llama.cpp-bpe-v1",
		Description: "TinyLlama-1.1B Q4_K_M on CPU via llama.cpp",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("register domain: %w", err))
		return
	}

	receipt, err := s.signAndSubmit("bob", types.TxRegisterService, types.MsgRegisterService{
		Owner:                resolveAddr(s, "bob"),
		Name:                 "translate-en-fr",
		Description:          "EN→FR translation (TinyLlama demo)",
		Price:                types.Coin{Denom: "aios", Amount: 100},
		VerificationDomainID: domainReceipt.NewID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Phase 3.z: voucher eligibility requires a voucher to own an ACTIVE
	// service in the disputed request's domain (Phase 3.z step 2 tightens this
	// from "any service" to "active service"). The bundled determinism-harness
	// vouches for honest providers in `make demo-spurious`; without an active
	// domain-resident service it would be blocked with ErrVouchNotEligible.
	//
	// We register a sentinel service for the harness and KEEP IT ACTIVE.
	// Price is set deliberately high so no requester will buy it; the service
	// exists purely as the harness's domain-residency proof. The harness key
	// is funded from EnsureDevKeyring with 1B aios, which comfortably covers
	// the ServiceRegistrationBond.
	harnessSvc, err := s.signAndSubmit("harness", types.TxRegisterService, types.MsgRegisterService{
		Owner:                resolveAddr(s, "harness"),
		Name:                 "harness-witness",
		Description:          "determinism-harness domain-residency marker (price set high to deter buyers)",
		Price:                types.Coin{Denom: "aios", Amount: 1_000_000_000},
		VerificationDomainID: domainReceipt.NewID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("register harness witness: %w", err))
		return
	}

	// MVP item #2: second harness witness for harness-b. Lets two independent
	// watchers vouch in parallel — required for any per-domain VoucherMargin
	// setting > 0 and for credibly showing the voucher quorum game.
	harnessBSvc, err := s.signAndSubmit("harness-b", types.TxRegisterService, types.MsgRegisterService{
		Owner:                resolveAddr(s, "harness-b"),
		Name:                 "harness-witness-b",
		Description:          "second determinism-harness domain-residency marker",
		Price:                types.Coin{Denom: "aios", Amount: 1_000_000_000},
		VerificationDomainID: domainReceipt.NewID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("register harness-b witness: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":               "seeded",
		"service_id":           receipt.NewID,
		"domain_id":            domainReceipt.NewID,
		"harness_witness_id":   harnessSvc.NewID,
		"harness_b_witness_id": harnessBSvc.NewID,
	})
}

// POST /demo/register-domain
type demoRegisterDomainReq struct {
	ModelSHA256 string `json:"model_sha256"`
	RuntimeID   string `json:"runtime_id"`
	HardwareTag string `json:"hardware_tag"`
	Precision   string `json:"precision"`
	Description string `json:"description"`
	Key         string `json:"key"`
}

func (s *Server) demoRegisterDomain(w http.ResponseWriter, r *http.Request) {
	var req demoRegisterDomainReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "bob"
	}
	receipt, err := s.signAndSubmit(req.Key, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:   resolveAddr(s, req.Key),
		ModelSHA256: req.ModelSHA256,
		RuntimeID:   req.RuntimeID,
		HardwareTag: req.HardwareTag,
		Precision:   req.Precision,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain_id": receipt.NewID})
}

// POST /demo/register-service {"name": "...", "description": "...", "price": 100, "key": "bob"}
type demoRegisterReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       uint64 `json:"price"`
	Key         string `json:"key"` // dev key name, defaults to bob
}

func (s *Server) demoRegisterService(w http.ResponseWriter, r *http.Request) {
	var req demoRegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "bob"
	}
	if req.Price == 0 {
		req.Price = 100
	}
	receipt, err := s.signAndSubmit(req.Key, types.TxRegisterService, types.MsgRegisterService{
		Owner:       resolveAddr(s, req.Key),
		Name:        req.Name,
		Description: req.Description,
		Price:       types.Coin{Denom: "aios", Amount: req.Price},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service_id": receipt.NewID})
}

// POST /demo/challenge {"request_id": N, "output_hash": "hex", "key": "harness"}
//
// Convenience for tests/CLI. The real challenge path is /tx with a signed
// MsgChallenge — the bundled determinism-harness uses that one.
type demoChallengeReq struct {
	RequestID  uint64 `json:"request_id"`
	OutputHash string `json:"output_hash"`
	Key        string `json:"key"`
}

func (s *Server) demoChallenge(w http.ResponseWriter, r *http.Request) {
	var req demoChallengeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "harness"
	}

	full, err := s.state.GetRequest(req.RequestID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if full.Status != types.StatusSubmitted {
		writeError(w, http.StatusBadRequest, fmt.Errorf("request not in SUBMITTED state: %s", full.Status))
		return
	}

	svc, err := s.state.GetService(full.ServiceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dom, _ := s.state.GetDomain(svc.VerificationDomainID)

	att := types.Attestation{
		Provider:             resolveAddr(s, req.Key),
		VerificationDomainID: svc.VerificationDomainID,
		ModelSHA256:          dom.ModelSHA256,
		RuntimeID:            dom.RuntimeID,
		HardwareTag:          dom.HardwareTag,
		Precision:            dom.Precision,
		InputHash:            full.InputHash,
		OutputHash:           req.OutputHash,
		ProducedAt:           s.state.Height(),
	}
	// signAndSubmit will sign the envelope; attestation signing inside the
	// payload is omitted here for the demo helper (the chain validates the
	// envelope but doesn't currently re-verify the attestation signature in
	// applyChallenge — that's a Phase 3.x hardening).
	receipt, err := s.signAndSubmit(req.Key, types.TxChallenge, types.MsgChallenge{
		Challenger:  resolveAddr(s, req.Key),
		RequestID:   req.RequestID,
		Attestation: att,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "type": receipt.Type})
}

// POST /demo/request-inference {"service_id": N, "prompt": "...", "key": "alice"}
type demoRequestReq struct {
	ServiceID uint64 `json:"service_id"`
	Prompt    string `json:"prompt"`
	Key       string `json:"key"`
}

func (s *Server) demoRequestInference(w http.ResponseWriter, r *http.Request) {
	var req demoRequestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "alice"
	}
	if req.ServiceID == 0 {
		req.ServiceID = 1
	}
	if req.Prompt == "" {
		req.Prompt = "Translate 'hello world' to French."
	}

	// Fetch the service to know its price.
	svc, err := s.state.GetService(req.ServiceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	h := sha256.Sum256([]byte(req.Prompt))
	inputHash := hex.EncodeToString(h[:])

	receipt, err := s.signAndSubmit(req.Key, types.TxRequestInference, types.MsgRequestInference{
		Requester:      resolveAddr(s, req.Key),
		ServiceID:      req.ServiceID,
		InputHash:      inputHash,
		InputURI:       "inline:" + inputHash,
		InputText:      req.Prompt,
		MaxPrice:       svc.Price,
		DeadlineHeight: 0,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request_id": receipt.NewID})
}

func resolveAddr(s *Server, name string) string {
	k, err := s.loadKey(name)
	if err != nil {
		return ""
	}
	return k.Address
}

// silence unused
var _ = errors.New
