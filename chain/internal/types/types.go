// Package types defines the on-chain data model: services, requests, results,
// transactions, and events. Maps 1:1 onto the Cosmos SDK proto definitions in
// /proto/aiservice/v1/. The Go shapes here are what Phase 0.5 actually uses;
// Phase 1 replaces them with generated proto types but the field names and
// semantics are preserved.
package types

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ─── Domain types ────────────────────────────────────────────────────────────

type Coin struct {
	Denom  string `json:"denom"`
	Amount uint64 `json:"amount"`
}

func (c Coin) String() string { return fmt.Sprintf("%d%s", c.Amount, c.Denom) }
func (c Coin) IsPositive() bool { return c.Amount > 0 }
func (c Coin) IsValid() bool    { return c.Denom != "" }

// IsLT reports c < other (same denom required; differing denom returns false).
func (c Coin) IsLT(other Coin) bool {
	if c.Denom != other.Denom {
		return false
	}
	return c.Amount < other.Amount
}

type Service struct {
	ID                   uint64 `json:"id"`
	Owner                string `json:"owner"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Price                Coin   `json:"price"`
	// VerificationDomainID 0 means "unverified" (Phase 0.5 default). Non-zero
	// references a domain registered via MsgRegisterDomain; the chain rejects
	// MsgSubmitResult whose attestation tuple doesn't match the domain.
	VerificationDomainID uint64 `json:"verification_domain_id"`
	Active               bool   `json:"active"`
	CreatedAtHeight      int64  `json:"created_at_height"`
	// Phase 3.z step 2: bond locked at registration. Refunded on
	// MsgDeactivateService only if the service has lived past
	// Params.MinServiceLifetimeBlocks. Forfeit otherwise. Stored per-service
	// (not just read from current params) so post-registration param changes
	// don't retroactively widen refunds.
	RegistrationBond Coin `json:"registration_bond,omitempty"`
}

type RequestStatus string

const (
	StatusPending    RequestStatus = "PENDING"
	StatusSubmitted  RequestStatus = "SUBMITTED"  // result submitted, in challenge window
	StatusChallenged RequestStatus = "CHALLENGED" // challenge filed, awaiting resolution
	StatusFinalized  RequestStatus = "FINALIZED"  // honest path: provider got escrow
	StatusSlashed    RequestStatus = "SLASHED"    // challenge resolved against provider; requester refunded
	StatusRefunded   RequestStatus = "REFUNDED"   // deadline elapsed without submission
)

// Attestation v1 — Phase 1+.
//
// Fields are ordered for stable canonical encoding. The signature is over the
// sorted-keys JSON marshalling of the *unsigned* attestation (everything except
// SignatureHex). See CanonicalAttestation below.
//
// VerificationDomainID is set when the service references a registered
// domain; the chain validates that the (model_sha256, runtime_id, hardware_tag,
// precision) tuple in this attestation matches the registered domain. In Phase
// 0.5 this was always 0 (unverified); Phase 1 services may opt in.
type Attestation struct {
	Provider             string `json:"provider"`
	VerificationDomainID uint64 `json:"verification_domain_id"`
	ModelSHA256          string `json:"model_sha256"`
	RuntimeID            string `json:"runtime_id"`
	HardwareTag          string `json:"hardware_tag"`
	Precision            string `json:"precision"`
	// Phase 1: tokenizer pinning. Phase 0.5–3.z implicitly assumed the
	// tokenizer was bundled inside the model file (GGUF); empty TokenizerID
	// preserves that assumption (legacy attestations + legacy domains keep
	// working). When the registered domain pins a TokenizerID, the chain
	// requires the attestation's TokenizerID to match exactly — this catches
	// the case where two runtimes load the same model but apply different
	// tokenization (BPE edge cases, byte fallback, special-token handling).
	// `omitempty` ensures legacy attestations produce identical canonical
	// bytes pre- and post-Phase-1.
	TokenizerID  string `json:"tokenizer_id,omitempty"`
	InputHash    string `json:"input_hash"`
	OutputHash   string `json:"output_hash"`
	ProducedAt   int64  `json:"produced_at_height"`
	SignatureHex string `json:"signature_hex"`
}

// CanonicalAttestation returns the bytes that get signed: the attestation with
// SignatureHex zeroed, JSON-marshalled with Go's default field order (i.e. the
// struct declaration order above). Anything signing or verifying Phase 1
// attestations MUST produce identical bytes — see docs/signing.md.
func (a Attestation) CanonicalBytes() ([]byte, error) {
	c := a
	c.SignatureHex = ""
	return json.Marshal(c)
}

// VerificationDomain — a registered (model, runtime, precision, hardware)
// tuple. Services opt in to a domain at registration; attestations are valid
// only if their tuple matches the domain's.
type VerificationDomain struct {
	ID                uint64 `json:"id"`
	ModelSHA256       string `json:"model_sha256"`
	RuntimeID         string `json:"runtime_id"`
	HardwareTag       string `json:"hardware_tag"`
	Precision         string `json:"precision"`
	// Phase 1: tokenizer pinning. Empty = "don't check" (backwards compat
	// with Phase 0.5–3.z domains). Non-empty = attestations on this domain
	// must declare a matching TokenizerID.
	TokenizerID       string `json:"tokenizer_id,omitempty"`
	Description       string `json:"description"`
	RegisteredAt      int64  `json:"registered_at_height"`
	Active            bool   `json:"active"`
	// Phase 3.z step 3: per-domain override of Params.VoucherMargin. When > 0
	// this margin is used at resolution time for any request bound to this
	// domain, taking precedence over the global default. 0 (the zero value)
	// means "inherit from Params.VoucherMargin" — preserves backwards
	// compatibility for domains registered before step 3.
	//
	// Recommended production values:
	//   - Low-stakes / single-watcher domains: 0 (inherit global, typically 0)
	//   - High-stakes / multi-watcher domains: 1+ (require net excess)
	VoucherMargin int64 `json:"voucher_margin,omitempty"`
}

// MsgRegisterDomain — admin-only in Phase 1 (only the chain authority key can
// register domains). Phase 2+ adds governance-driven registration.
type MsgRegisterDomain struct {
	Authority   string `json:"authority"`
	ModelSHA256 string `json:"model_sha256"`
	RuntimeID   string `json:"runtime_id"`
	HardwareTag string `json:"hardware_tag"`
	Precision   string `json:"precision"`
	// Phase 1: optional tokenizer pinning. Empty = don't check (back-compat).
	TokenizerID string `json:"tokenizer_id,omitempty"`
	Description string `json:"description"`
	// Phase 3.z step 3: optional per-domain margin override. 0 = inherit
	// from Params.VoucherMargin at resolution time.
	VoucherMargin int64 `json:"voucher_margin,omitempty"`
}

// Precision values that are recognized. The chain accepts any string but only
// canonical values match across-machine.
const (
	PrecisionFP32   = "fp32"
	PrecisionBF16   = "bf16"
	PrecisionFP16   = "fp16"
	PrecisionINT8   = "int8"
	PrecisionQ4_K_M = "q4_k_m"
	PrecisionQ5_K_M = "q5_k_m"
)

type Result struct {
	OutputHash  string      `json:"output_hash"`
	OutputURI   string      `json:"output_uri"`
	OutputText  string      `json:"output_text"` // PHASE 0.5: inlined for demo display
	Attestation Attestation `json:"attestation"`
}

type InferenceRequest struct {
	ID                uint64        `json:"id"`
	ServiceID         uint64        `json:"service_id"`
	Requester         string        `json:"requester"`
	InputHash         string        `json:"input_hash"`
	InputURI          string        `json:"input_uri"`
	InputText         string        `json:"input_text"` // PHASE 0.5: inlined for demo display
	Escrow            Coin          `json:"escrow"`
	DeadlineHeight    int64         `json:"deadline_height"`
	Status            RequestStatus `json:"status"`
	CreatedAtHeight   int64         `json:"created_at_height"`
	FinalizedAtHeight int64         `json:"finalized_at_height,omitempty"`
	Result            *Result       `json:"result,omitempty"`
	Paid              *Coin         `json:"paid,omitempty"`

	// Phase 3+: challenge tracking. SubmittedAtHeight marks when the
	// challenge window starts. Challenges is the list of challenges filed
	// against this request (Phase 3 simple: first challenge wins).
	SubmittedAtHeight int64       `json:"submitted_at_height,omitempty"`
	Challenges        []Challenge `json:"challenges,omitempty"`

	// Phase 3.x: ProviderBond is the amount the provider posted at submit.
	// It's locked in the module escrow account and resolves on terminal status.
	ProviderBond *Coin `json:"provider_bond,omitempty"`

	// Phase 3.y: vouchers backing either provider or challenger after a
	// challenge is filed.
	Vouchers []Voucher `json:"vouchers,omitempty"`
}

// Challenge — Phase 3. A challenger asserts that the provider's submitted
// output is wrong; they sign an attestation with what they believe the correct
// output is. Phase 3 simple: if a valid challenge is filed during the window,
// the chain trusts the challenger (the bundled determinism-harness is the only
// challenger in this iteration and it re-runs honestly). Phase 3.x replaces
// this with a real dispute game where both attestations are adjudicated.
type Challenge struct {
	Challenger  string      `json:"challenger"`
	PostedAt    int64       `json:"posted_at_height"`
	Attestation Attestation `json:"attestation"`
	// Phase 3.x: Bond is the challenger's stake. Returned on successful slash;
	// forfeit if the challenge is dismissed (Phase 3.y mechanism).
	Bond Coin `json:"bond"`
}

// Voucher — Phase 3.y. A third party who independently runs the inference in
// the same verification domain and signs an attestation matching one of the
// disputed outputs. If a voucher's attestation matches the provider's
// output_hash, it backs the provider (challenge is dismissed). If it matches
// the challenger's output_hash, it backs the challenger (provider is slashed).
// A voucher whose output_hash matches neither is rejected.
type Voucher struct {
	Voucher     string      `json:"voucher"`
	PostedAt    int64       `json:"posted_at_height"`
	Attestation Attestation `json:"attestation"`
	Bond        Coin        `json:"bond"`
	// SupportsProvider is true iff Attestation.OutputHash equals the provider's
	// submitted output_hash. Set at apply time; clients can derive it from the
	// hash but we surface it for convenience.
	SupportsProvider bool `json:"supports_provider"`
}

type Params struct {
	ChallengeWindowBlocks    int64 `json:"challenge_window_blocks"`
	MinServicePrice          Coin  `json:"min_service_price"`
	MaxRequestDeadlineBlocks int64 `json:"max_request_deadline_blocks"`
	// Phase 3.x: economic security via bonds.
	ProviderBondAmount       Coin  `json:"provider_bond_amount"`
	ChallengerBondAmount     Coin  `json:"challenger_bond_amount"`
	// Phase 3.y: voucher mechanism.
	// VoucherBondAmount is staked by each voucher on MsgVouch.
	// VoucherRewardAmount is paid to each correct voucher from the
	// losing side's forfeit bond when a challenge is dismissed.
	VoucherBondAmount     Coin `json:"voucher_bond_amount"`
	VoucherRewardAmount   Coin `json:"voucher_reward_amount"`
	// Phase 3.z: sybil resistance. VoucherMargin is the net excess of provider-
	// side vouchers over challenger-side vouchers required to dismiss a
	// challenge. Default 0 keeps the Phase 3.y behavior (provider wins ties).
	// Raise to 1+ once a redundant honest watcher market exists; doing so
	// strengthens sybil resistance at the cost of demanding multiple honest
	// vouchers in the worst case.
	VoucherMargin         int64 `json:"voucher_margin"`
	// Phase 3.z step 2: service-registration bond. Locked at registration;
	// refunded on deactivation only if the service has lived past
	// MinServiceLifetimeBlocks. Sybil voucher setup now requires the attacker
	// to commit ServiceRegistrationBond per identity and either wait the
	// lifetime out (locking capital for hours) or forfeit the bond.
	ServiceRegistrationBond  Coin  `json:"service_registration_bond"`
	MinServiceLifetimeBlocks int64 `json:"min_service_lifetime_blocks"`
	// Phase 3.z step 4: voucher bond scales with the request's provider bond.
	// Voucher bond at MsgVouch time = r.ProviderBond.Amount * BP / 10000.
	// 5000 = 50% (matches the legacy 25-aios voucher / 50-aios provider ratio).
	// 10000 = 100% (voucher fully matches the provider's risk).
	// 0 disables scaling and falls back to VoucherBondAmount.
	VoucherBondScaleBP int64 `json:"voucher_bond_scale_bp"`
}

func DefaultParams() Params {
	return Params{
		// Phase 3: challenge window — needs to comfortably exceed a single
		// re-run on the slowest expected hardware (CPU TinyLlama is ~5-15s).
		ChallengeWindowBlocks:    45,
		MinServicePrice:          Coin{Denom: "aios", Amount: 1},
		MaxRequestDeadlineBlocks: 10_000,
		// Phase 3.x: bonds. Provider posts on submit; rewarded back on
		// FINALIZED, slashed to challenger on SLASHED. Challenger posts on
		// MsgChallenge; refunded on successful slash (no penalty in Phase
		// 3.x simple — Phase 3.y adds counter-challenge mechanism).
		ProviderBondAmount:       Coin{Denom: "aios", Amount: 50},
		ChallengerBondAmount:     Coin{Denom: "aios", Amount: 50},
		// Phase 3.y: vouchers post a smaller stake and earn a reward when
		// they back the winning side. Reward comes from the losing side's
		// forfeit bond; with 1 voucher this fully consumes the 50-aios
		// challenger bond as 25 to provider + 25 to voucher.
		VoucherBondAmount:        Coin{Denom: "aios", Amount: 25},
		VoucherRewardAmount:      Coin{Denom: "aios", Amount: 25},
		// Phase 3.z default keeps 3.y semantics until a 2-watcher market exists.
		VoucherMargin:            0,
		// Phase 3.z step 2: bond commits the registrant. 100 aios is roughly
		// 2× the voucher bond, making sybil-eligibility ~5× costlier than the
		// vouch itself. Tunable. Lifetime 1000 blocks ≈ 17 min at 1s blocks —
		// enough that drive-by sybil registration can't pump-and-dump the
		// bond, short enough that honest test/demo flows don't stall.
		ServiceRegistrationBond:  Coin{Denom: "aios", Amount: 100},
		MinServiceLifetimeBlocks: 1000,
		// Phase 3.z step 4: voucher bond = 50% of provider bond (matches the
		// legacy 25/50 ratio at the default ProviderBondAmount).
		VoucherBondScaleBP: 5000,
	}
}

// ─── Transactions ────────────────────────────────────────────────────────────

// TxType is the discriminator on the signed envelope.
type TxType string

const (
	TxRegisterService  TxType = "register_service"
	TxRequestInference TxType = "request_inference"
	TxSubmitResult     TxType = "submit_result"
	TxTransfer         TxType = "transfer" // used to fund accounts in dev
	TxRegisterDomain   TxType = "register_domain"
	TxChallenge        TxType = "challenge" // Phase 3: dispute a submitted result
	TxVouch            TxType = "vouch"     // Phase 3.y: take a side in a dispute
	// Phase 2 catch-up — lifecycle msgs
	TxUpdateService     TxType = "update_service"
	TxDeactivateService TxType = "deactivate_service"
	TxDeactivateDomain  TxType = "deactivate_domain"
	TxResolveChallenge  TxType = "resolve_challenge"
)

// MsgRegisterService maps to proto MsgRegisterService.
type MsgRegisterService struct {
	Owner                string `json:"owner"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Price                Coin   `json:"price"`
	// VerificationDomainID is optional. 0 = unverified service. Non-zero must
	// reference an existing active domain.
	VerificationDomainID uint64 `json:"verification_domain_id"`
}

type MsgRequestInference struct {
	Requester      string `json:"requester"`
	ServiceID      uint64 `json:"service_id"`
	InputHash      string `json:"input_hash"`
	InputURI       string `json:"input_uri"`
	InputText      string `json:"input_text"` // PHASE 0.5 inlines the prompt for the demo
	MaxPrice       Coin   `json:"max_price"`
	DeadlineHeight int64  `json:"deadline_height"`
}

type MsgSubmitResult struct {
	Provider  string `json:"provider"`
	RequestID uint64 `json:"request_id"`
	Result    Result `json:"result"`
}

// MsgTransfer is dev-only — used by the chain itself to fund alice/bob from
// the faucet account on init. Not exposed via REST as a user-facing endpoint.
type MsgTransfer struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount Coin   `json:"amount"`
}

// MsgChallenge — Phase 3. A challenger asserts the provider's submitted output
// is wrong. The challenger's attestation should reference the same input_hash
// but a different output_hash. Phase 3 simple: any well-formed challenge
// transitions the request to CHALLENGED and (after a short resolution window)
// the chain trusts the challenger and slashes the provider.
type MsgChallenge struct {
	Challenger  string      `json:"challenger"`
	RequestID   uint64      `json:"request_id"`
	Attestation Attestation `json:"attestation"`
}

// MsgVouch — Phase 3.y. A voucher independently ran the inference in the same
// verification domain and signs an attestation. If their output_hash matches
// the provider's, they're backing the provider (defending against a spurious
// challenge). If it matches the challenger's, they're backing the challenger
// (reinforcing the slash). If it matches neither, the chain rejects.
type MsgVouch struct {
	Voucher     string      `json:"voucher"`
	RequestID   uint64      `json:"request_id"`
	Attestation Attestation `json:"attestation"`
}

// ─── Phase 2 catch-up: lifecycle messages ─────────────────────────────────

// MsgUpdateService — the service owner updates mutable fields (description and
// price). The name, owner, and verification_domain_id cannot be changed —
// rewriting them would defeat the marketplace integrity guarantees.
type MsgUpdateService struct {
	Owner       string `json:"owner"`
	ServiceID   uint64 `json:"service_id"`
	Description string `json:"description"`
	Price       Coin   `json:"price"`
}

// MsgDeactivateService — the service owner takes their service offline. New
// requests are rejected after this; pending requests still resolve normally.
type MsgDeactivateService struct {
	Owner     string `json:"owner"`
	ServiceID uint64 `json:"service_id"`
}

// MsgDeactivateDomain — chain authority retires a verification domain. Services
// referencing it can no longer accept new requests; pending requests still
// resolve. Used when a runtime+model tuple is found to be non-deterministic.
type MsgDeactivateDomain struct {
	Authority string `json:"authority"`
	DomainID  uint64 `json:"domain_id"`
}

// MsgResolveChallenge — chain authority explicitly resolves an open challenge,
// overriding the auto-resolution timer. Decision must be "dismiss" or "slash".
// Used for edge cases the voucher mechanism can't handle (e.g. all vouchers
// abstain or hardware fault renders the domain temporarily unverifiable).
type MsgResolveChallenge struct {
	Authority string `json:"authority"`
	RequestID uint64 `json:"request_id"`
	Decision  string `json:"decision"` // "dismiss" | "slash"
}

// SignedTx is the wire format submitted to /tx.
// signature is over canonical(envelope-without-signature).
type SignedTx struct {
	Type        TxType          `json:"type"`
	Nonce       uint64          `json:"nonce"`
	PubKeyHex   string          `json:"pub_key_hex"`
	SignatureHex string         `json:"signature_hex"`
	Payload     json.RawMessage `json:"payload"`
}

// CanonicalBytes returns the deterministic byte sequence signed by the sender.
// The signature does NOT cover itself; we serialize a copy with the signature
// field zeroed.
func (t SignedTx) CanonicalBytes() ([]byte, error) {
	copy := SignedTx{
		Type:      t.Type,
		Nonce:     t.Nonce,
		PubKeyHex: t.PubKeyHex,
		Payload:   t.Payload,
	}
	return json.Marshal(copy)
}

func (t SignedTx) ValidateBasic() error {
	switch t.Type {
	case TxRegisterService, TxRequestInference, TxSubmitResult, TxTransfer,
		TxRegisterDomain, TxChallenge, TxVouch,
		TxUpdateService, TxDeactivateService, TxDeactivateDomain, TxResolveChallenge:
	default:
		return fmt.Errorf("unknown tx type %q", t.Type)
	}
	if len(t.PubKeyHex) == 0 {
		return errors.New("pub_key_hex required")
	}
	if _, err := hex.DecodeString(t.PubKeyHex); err != nil {
		return fmt.Errorf("pub_key_hex: %w", err)
	}
	if len(t.SignatureHex) == 0 {
		return errors.New("signature_hex required")
	}
	if _, err := hex.DecodeString(t.SignatureHex); err != nil {
		return fmt.Errorf("signature_hex: %w", err)
	}
	if len(t.Payload) == 0 {
		return errors.New("payload required")
	}
	return nil
}

// ─── Events ──────────────────────────────────────────────────────────────────

type EventType string

const (
	EventServiceRegistered    EventType = "ServiceRegistered"
	EventInferenceRequested   EventType = "InferenceRequested"
	EventResultSubmitted      EventType = "ResultSubmitted"
	EventRequestFinalized     EventType = "RequestFinalized"
	EventRequestRefunded      EventType = "RequestRefunded"
	EventBlockCommitted       EventType = "BlockCommitted"
	EventDomainRegistered     EventType = "DomainRegistered"
	EventChallenged           EventType = "Challenged"        // Phase 3
	EventRequestSlashed       EventType = "RequestSlashed"    // Phase 3
	EventVouched              EventType = "Vouched"           // Phase 3.y
	EventRequestDismissed     EventType = "RequestDismissed"  // Phase 3.y — challenge dismissed in provider's favor
	// Phase 2 catch-up: lifecycle
	EventServiceUpdated     EventType = "ServiceUpdated"
	EventServiceDeactivated EventType = "ServiceDeactivated"
	EventDomainDeactivated  EventType = "DomainDeactivated"
	// Phase 3.z: emitted when MsgDeactivateDomain voids an open request bound
	// to the now-dead domain. Distinct from RequestRefunded (deadline expiry,
	// requester-only payout) because every locked party gets their value back.
	EventRequestVoided EventType = "RequestVoided"
)

// Event is the SSE wire shape consumed by inference-node and indexer.
type Event struct {
	Type        EventType       `json:"type"`
	BlockHeight int64           `json:"block_height"`
	TxHash      string          `json:"tx_hash,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

// Typed event payloads — these are what `Payload` decodes to per Type.

type ServiceRegisteredPayload struct {
	ServiceID   uint64 `json:"service_id"`
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       Coin   `json:"price"`
}

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

type ResultSubmittedPayload struct {
	RequestID  uint64 `json:"request_id"`
	Provider   string `json:"provider"`
	OutputHash string `json:"output_hash"`
	OutputURI  string `json:"output_uri"`
}

type RequestFinalizedPayload struct {
	RequestID uint64 `json:"request_id"`
	Provider  string `json:"provider"`
	Paid      Coin   `json:"paid"`
	// Phase 3.x: provider bond returned alongside the escrow payout.
	ProviderBondReturned Coin `json:"provider_bond_returned,omitempty"`
}

type RequestRefundedPayload struct {
	RequestID uint64 `json:"request_id"`
	Requester string `json:"requester"`
	Refunded  Coin   `json:"refunded"`
}

type BlockCommittedPayload struct {
	Height int64  `json:"height"`
	Time   string `json:"time"`
}

type DomainRegisteredPayload struct {
	DomainID    uint64 `json:"domain_id"`
	ModelSHA256 string `json:"model_sha256"`
	RuntimeID   string `json:"runtime_id"`
	HardwareTag string `json:"hardware_tag"`
	Precision   string `json:"precision"`
	TokenizerID string `json:"tokenizer_id,omitempty"`
	Description string `json:"description"`
}

// ChallengedPayload — Phase 3.
type ChallengedPayload struct {
	RequestID            uint64 `json:"request_id"`
	Challenger           string `json:"challenger"`
	ChallengerOutputHash string `json:"challenger_output_hash"`
	ProviderOutputHash   string `json:"provider_output_hash"`
	// Phase 3.x: challenger's locked bond, in case observers care.
	ChallengerBond Coin `json:"challenger_bond,omitempty"`
}

// VouchedPayload — Phase 3.y.
type VouchedPayload struct {
	RequestID        uint64 `json:"request_id"`
	Voucher          string `json:"voucher"`
	VoucherOutputHash string `json:"voucher_output_hash"`
	SupportsProvider bool   `json:"supports_provider"`
	Bond             Coin   `json:"bond"`
}

// RequestDismissedPayload — Phase 3.y. The challenge was dismissed; provider's
// output prevails. Provider gets escrow + bond returned; vouchers backing the
// provider get bond + reward; challenger forfeits bond.
type RequestDismissedPayload struct {
	RequestID              uint64 `json:"request_id"`
	Provider               string `json:"provider"`
	Challenger             string `json:"challenger"`
	Paid                   Coin   `json:"paid"`
	ProviderBondReturned   Coin   `json:"provider_bond_returned,omitempty"`
	ChallengerBondForfeit  Coin   `json:"challenger_bond_forfeit,omitempty"`
	VoucherCount           int    `json:"voucher_count"`
}

// Phase 2 catch-up payloads

type ServiceUpdatedPayload struct {
	ServiceID   uint64 `json:"service_id"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
	Price       Coin   `json:"price"`
}

type ServiceDeactivatedPayload struct {
	ServiceID uint64 `json:"service_id"`
	Owner     string `json:"owner"`
	// BondRefunded — empty when the service was deactivated before
	// Params.MinServiceLifetimeBlocks (forfeit); equals svc.RegistrationBond
	// when refunded. Phase 3.z step 2.
	BondRefunded Coin `json:"bond_refunded,omitempty"`
}

type DomainDeactivatedPayload struct {
	DomainID  uint64 `json:"domain_id"`
	Authority string `json:"authority"`
	// Phase 3.z: counts of dependent state unwound by this deactivation.
	// Indexers use these to surface the blast radius in a single number.
	ServicesDeactivated int `json:"services_deactivated,omitempty"`
	RequestsVoided      int `json:"requests_voided,omitempty"`
}

// RequestVoidedPayload — emitted once per open request voided by a
// MsgDeactivateDomain that hit the request's verification domain. Every
// locked party gets their stake back; nobody wins.
type RequestVoidedPayload struct {
	RequestID              uint64 `json:"request_id"`
	ServiceID              uint64 `json:"service_id"`
	DomainID               uint64 `json:"domain_id"`
	PriorStatus            RequestStatus `json:"prior_status"`
	Requester              string `json:"requester"`
	EscrowRefunded         Coin   `json:"escrow_refunded"`
	Provider               string `json:"provider,omitempty"`               // empty if request never reached SUBMITTED
	ProviderBondReturned   Coin   `json:"provider_bond_returned,omitempty"` // present iff status >= SUBMITTED
	Challenger             string `json:"challenger,omitempty"`             // present iff status == CHALLENGED
	ChallengerBondReturned Coin   `json:"challenger_bond_returned,omitempty"`
	VoucherBondsReturned   int    `json:"voucher_bonds_returned,omitempty"` // count, not value (multiple vouchers, possibly mixed)
}

// RequestFinalizedPayload — augmented in Phase 3.x to also surface the
// provider bond's release. Defined earlier in the file already; we extend
// it here. (Done as a second struct so existing JSON consumers don't break;
// added optional fields.)

// RequestSlashedPayload — Phase 3. Provider is judged wrong; requester refunded.
type RequestSlashedPayload struct {
	RequestID    uint64 `json:"request_id"`
	Provider     string `json:"provider"`
	Challenger   string `json:"challenger"`
	Refunded     Coin   `json:"refunded"`
	// Phase 3.x: bonds resolved. ProviderBondSlashed → challenger; ChallengerBondReturned → challenger.
	ProviderBondSlashed   Coin `json:"provider_bond_slashed,omitempty"`
	ChallengerBondReturned Coin `json:"challenger_bond_returned,omitempty"`
	// Phase 3.z step 3+4: voucher-bond resolution counts. Provider-side
	// vouchers lost → bonds swept to treasury. Challenger-side vouchers won
	// → bonds returned. Counts only (not values) because a request can have
	// multiple vouchers at different scaled amounts.
	VoucherBondsForfeitedToTreasury int `json:"voucher_bonds_forfeited_to_treasury,omitempty"`
	ChallengerVoucherBondsReturned  int `json:"challenger_voucher_bonds_returned,omitempty"`
}

// ─── Address derivation ──────────────────────────────────────────────────────

// AddressFromPubKey derives a bech32-like prefixed hex address from an Ed25519
// public key. Phase 0.5 uses `aios1<hex(sha256(pubkey)[:20])>` for readability;
// Phase 1 swaps in bech32 with the cosmos prefix.
func AddressFromPubKey(pub []byte) string {
	h := Sha256(pub)
	return "aios1" + hex.EncodeToString(h[:20])
}

func IsValidAddress(s string) bool {
	if !strings.HasPrefix(s, "aios1") {
		return false
	}
	body := strings.TrimPrefix(s, "aios1")
	if len(body) != 40 {
		return false
	}
	_, err := hex.DecodeString(body)
	return err == nil
}

// ─── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrInvalidSignature   = errors.New("invalid signature")
	ErrInvalidNonce       = errors.New("invalid nonce")
	ErrInvalidAddress     = errors.New("invalid address")
	ErrServiceNotFound    = errors.New("service not found")
	ErrServiceInactive    = errors.New("service inactive")
	ErrDuplicateName      = errors.New("service name already registered")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInsufficientPrice  = errors.New("max_price below service price")
	ErrInvalidDeadline    = errors.New("invalid deadline")
	ErrRequestNotFound    = errors.New("request not found")
	ErrRequestNotPending  = errors.New("request not pending")
	ErrWrongProvider      = errors.New("wrong provider for service")
	ErrInputHashMismatch  = errors.New("attestation input_hash != request input_hash")
	ErrZeroPrice          = errors.New("price must be positive")
	ErrEmptyName          = errors.New("name must not be empty")

	// Phase 1
	ErrDomainNotFound       = errors.New("verification domain not found")
	ErrDomainInactive       = errors.New("verification domain inactive")
	ErrAttestationDomainMismatch = errors.New("attestation does not match service's verification domain")
	ErrUnauthorized         = errors.New("unauthorized: requires chain authority")

	// Phase 3
	ErrChallengeWindowClosed = errors.New("challenge window has closed")
	ErrChallengeNotApplicable = errors.New("request not in a state that accepts challenges")
	ErrChallengeOutputMatches = errors.New("challenger's output_hash matches provider's; no dispute")

	// Phase 3.y
	ErrVouchNotApplicable = errors.New("request not in a state that accepts vouches")
	ErrVouchHashMismatch  = errors.New("voucher's output_hash matches neither provider nor challenger")
	ErrVouchDuplicate     = errors.New("this account has already vouched on this request")

	// Phase 3.z — voucher sybil resistance
	ErrVouchNotEligible = errors.New("voucher does not own a service in the disputed request's verification domain")

	// Phase 2 catch-up
	ErrNotOwner            = errors.New("signer is not the service owner")
	ErrServiceAlreadyInactive = errors.New("service is already inactive")
	ErrInvalidDecision     = errors.New("decision must be 'dismiss' or 'slash'")
)
