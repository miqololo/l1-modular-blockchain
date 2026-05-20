// Package chain is the HTTP+SSE client for the aios chain.
//
// PHASE 0.5: talks to a custom Go HTTP service (see chain/internal/api).
// PHASE 1: this package gets replaced with a Cosmos SDK / CometBFT client.
package chain

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ─── Shared types (mirror chain/internal/types) ─────────────────────────────

type Coin struct {
	Denom  string `json:"denom"`
	Amount uint64 `json:"amount"`
}

type Service struct {
	ID                   uint64 `json:"id"`
	Owner                string `json:"owner"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Price                Coin   `json:"price"`
	VerificationDomainID uint64 `json:"verification_domain_id"`
	Active               bool   `json:"active"`
	CreatedAtHeight      int64  `json:"created_at_height"`
}

type Request struct {
	ID              uint64  `json:"id"`
	ServiceID       uint64  `json:"service_id"`
	Requester       string  `json:"requester"`
	InputHash       string  `json:"input_hash"`
	InputURI        string  `json:"input_uri"`
	InputText       string  `json:"input_text"`
	Escrow          Coin    `json:"escrow"`
	DeadlineHeight  int64   `json:"deadline_height"`
	Status          string  `json:"status"`
	CreatedAtHeight int64   `json:"created_at_height"`
	Result          *Result `json:"result,omitempty"`
}

type Result struct {
	OutputHash  string      `json:"output_hash"`
	OutputURI   string      `json:"output_uri"`
	OutputText  string      `json:"output_text"`
	Attestation Attestation `json:"attestation"`
}

// Attestation v1 — field order matches chain/internal/types.Attestation.
//
// The signature is over the JSON marshalling of this struct with SignatureHex
// zeroed. Field order MUST match the chain's struct, because Go's encoding/json
// uses declaration order. Any drift breaks verification.
type Attestation struct {
	Provider             string `json:"provider"`
	VerificationDomainID uint64 `json:"verification_domain_id"`
	ModelSHA256          string `json:"model_sha256"`
	RuntimeID            string `json:"runtime_id"`
	HardwareTag          string `json:"hardware_tag"`
	Precision            string `json:"precision"`
	// Phase 1: tokenizer pinning. MUST stay in this exact position to keep
	// canonical JSON bytes aligned with chain/internal/types/types.go. Empty
	// = "domain doesn't pin a tokenizer" (legacy). omitempty makes legacy
	// attestations byte-identical pre- and post-Phase-1.
	TokenizerID  string `json:"tokenizer_id,omitempty"`
	InputHash    string `json:"input_hash"`
	OutputHash   string `json:"output_hash"`
	ProducedAt   int64  `json:"produced_at_height"`
	SignatureHex string `json:"signature_hex"`
}

type Status struct {
	ChainID string `json:"chain_id"`
	Height  int64  `json:"height"`
	Time    string `json:"time"`
}

type TxType string

const (
	TxRegisterService  TxType = "register_service"
	TxRequestInference TxType = "request_inference"
	TxSubmitResult     TxType = "submit_result"
)

type SignedTx struct {
	Type         TxType          `json:"type"`
	Nonce        uint64          `json:"nonce"`
	PubKeyHex    string          `json:"pub_key_hex"`
	SignatureHex string          `json:"signature_hex"`
	Payload      json.RawMessage `json:"payload"`
}

type MsgSubmitResult struct {
	Provider  string `json:"provider"`
	RequestID uint64 `json:"request_id"`
	Result    Result `json:"result"`
}

type TxResponse struct {
	Type      string `json:"type"`
	Height    int64  `json:"height"`
	ServiceID uint64 `json:"service_id,omitempty"`
	RequestID uint64 `json:"request_id,omitempty"`
	Finalized bool   `json:"finalized,omitempty"`
}

// ─── Event types ────────────────────────────────────────────────────────────

type Event struct {
	Type        string          `json:"type"`
	BlockHeight int64           `json:"block_height"`
	Payload     json.RawMessage `json:"payload"`
}

func (e Event) Decode(into any) error { return json.Unmarshal(e.Payload, into) }

type InferenceRequestedPayload struct {
	RequestID      uint64 `json:"request_id"`
	ServiceID      uint64 `json:"service_id"`
	Requester      string `json:"requester"`
	InputHash      string `json:"input_hash"`
	InputURI       string `json:"input_uri"`
	InputText      string `json:"input_text"`
	Escrow         Coin   `json:"escrow"`
	DeadlineHeight int64  `json:"deadline_height"`
}

// ─── Signer ────────────────────────────────────────────────────────────────

type Signer struct {
	name    string
	addr    string
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
}

// LoadSigner reads keys.json (the chain's dev keyring) and returns the named key.
func LoadSigner(path, name string) (*Signer, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read keyring: %w", err)
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
		return nil, fmt.Errorf("decode keyring: %w", err)
	}
	for _, k := range r.Keys {
		if k.Name == name {
			pub, err := hex.DecodeString(k.PubKeyHex)
			if err != nil {
				return nil, fmt.Errorf("decode pubkey: %w", err)
			}
			priv, err := hex.DecodeString(k.PrivKeyHex)
			if err != nil {
				return nil, fmt.Errorf("decode privkey: %w", err)
			}
			return &Signer{
				name: name,
				addr: k.Address,
				pub:  ed25519.PublicKey(pub),
				priv: ed25519.PrivateKey(priv),
			}, nil
		}
	}
	return nil, fmt.Errorf("key %q not found in %s", name, path)
}

func (s *Signer) Address() string { return s.addr }
func (s *Signer) Name() string    { return s.name }

// SignAttestation produces a deterministic signature over the canonical
// JSON encoding of an attestation (SignatureHex zeroed). Phase 1+ format.
func (s *Signer) SignAttestation(a Attestation) string {
	clone := a
	clone.SignatureHex = ""
	canonical, _ := json.Marshal(clone)
	sig := ed25519.Sign(s.priv, canonical)
	return hex.EncodeToString(sig)
}

// sha256 import alias removed (no longer used directly here).
var _ = sha256.Sum256

// signTx signs a SignedTx envelope (signature is over canonical bytes minus signature).
func (s *Signer) signTx(tx *SignedTx) {
	tx.PubKeyHex = hex.EncodeToString(s.pub)
	clone := SignedTx{
		Type:      tx.Type,
		Nonce:     tx.Nonce,
		PubKeyHex: tx.PubKeyHex,
		Payload:   tx.Payload,
	}
	canonical, _ := json.Marshal(clone)
	tx.SignatureHex = hex.EncodeToString(ed25519.Sign(s.priv, canonical))
}

// ─── Client ────────────────────────────────────────────────────────────────

type Client struct {
	baseURL string
	http    *http.Client
	logger  *zap.Logger
	height  atomic.Int64
}

func NewClient(baseURL string, logger *zap.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
		logger:  logger,
	}
}

func (c *Client) Height() int64 { return c.height.Load() }

func (c *Client) Status() (Status, error) {
	var st Status
	err := c.getJSON("/status", &st)
	if err == nil {
		c.height.Store(st.Height)
	}
	return st, err
}

func (c *Client) GetService(id uint64) (Service, error) {
	var s Service
	return s, c.getJSON(fmt.Sprintf("/services/%d", id), &s)
}

func (c *Client) GetRequest(id uint64) (Request, error) {
	var r Request
	return r, c.getJSON(fmt.Sprintf("/requests/%d", id), &r)
}

func (c *Client) AccountNonce(addr string) (uint64, error) {
	var resp struct {
		Address string `json:"address"`
		Balance uint64 `json:"balance"`
		Nonce   uint64 `json:"nonce"`
	}
	if err := c.getJSON("/accounts/"+addr, &resp); err != nil {
		return 0, err
	}
	return resp.Nonce, nil
}

func (c *Client) getJSON(path string, into any) error {
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// SubmitTxAs signs and submits a transaction with the given signer.
func (c *Client) SubmitTxAs(signer *Signer, txType TxType, payload any) (TxResponse, error) {
	bz, err := json.Marshal(payload)
	if err != nil {
		return TxResponse{}, err
	}
	nonce, err := c.AccountNonce(signer.Address())
	if err != nil {
		return TxResponse{}, fmt.Errorf("get nonce: %w", err)
	}
	tx := SignedTx{
		Type:    txType,
		Nonce:   nonce,
		Payload: bz,
	}
	signer.signTx(&tx)

	body, err := json.Marshal(tx)
	if err != nil {
		return TxResponse{}, err
	}
	resp, err := c.http.Post(c.baseURL+"/tx", "application/json", bytes.NewReader(body))
	if err != nil {
		return TxResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return TxResponse{}, fmt.Errorf("POST /tx: %d %s", resp.StatusCode, string(errBody))
	}
	var out TxResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return TxResponse{}, err
	}
	c.height.Store(out.Height)
	return out, nil
}

// SubscribeEvents reads the SSE stream from /events and calls handler for each
// event whose type matches `types` (or all if empty). Returns on context cancel
// or stream error.
func (c *Client) SubscribeEvents(ctx context.Context, types []string, handler func(Event)) error {
	url := c.baseURL + "/events"
	if len(types) > 0 {
		url += "?types=" + strings.Join(types, ",")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a no-timeout client for SSE.
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe status %d", resp.StatusCode)
	}

	c.logger.Info("event stream connected", zap.String("url", url))

	reader := bufio.NewReader(resp.Body)
	for {
		if err := ctx.Err(); err != nil {
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
		var ev Event
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			c.logger.Warn("decode event", zap.Error(err), zap.String("data", data))
			continue
		}
		c.height.Store(ev.BlockHeight)
		handler(ev)
	}
}
