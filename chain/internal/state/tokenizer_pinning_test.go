package state_test

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

// Phase 1: tokenizer pinning. The domain tuple now includes an optional
// TokenizerID. Empty TokenizerID on a domain = legacy "don't check" behavior
// (backwards compatible with Phase 0.5–3.z domains). Non-empty = attestations
// must match exactly at submit / challenge / vouch time.

func TestTokenizerPin_PersistedOnDomain(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)
	tx := signTx(t, priv, pub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:   addr,
		ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000001",
		RuntimeID:   "test-runtime",
		HardwareTag: "test-hardware",
		Precision:   "fp32",
		TokenizerID: "test-tokenizer-v1",
	})
	receipt, err := s.SubmitTx(tx)
	require.NoError(t, err)

	dom, err := s.GetDomain(receipt.NewID)
	require.NoError(t, err)
	require.Equal(t, "test-tokenizer-v1", dom.TokenizerID, "tokenizer ID persisted on domain")
}

func TestTokenizerPin_OmittedRemainsEmpty(t *testing.T) {
	// Backwards compatibility: domains registered without TokenizerID continue
	// to have empty string, signalling "don't check" at attestation time.
	s := newState(t)
	pub, priv, addr := makeKey(t)
	tx := signTx(t, priv, pub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:   addr,
		ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000002",
		RuntimeID:   "test-runtime",
		HardwareTag: "test-hardware",
		Precision:   "fp32",
		// TokenizerID intentionally omitted
	})
	receipt, err := s.SubmitTx(tx)
	require.NoError(t, err)

	dom, err := s.GetDomain(receipt.NewID)
	require.NoError(t, err)
	require.Equal(t, "", dom.TokenizerID, "omitted TokenizerID → empty string")
}

// tokenizerScenario bundles the keys + IDs needed for the submit-result tests
// below. Reduces signature noise in the per-case tests.
type tokenizerScenario struct {
	state       *state.State
	domain      uint64
	svcID       uint64
	pPub        ed25519.PublicKey
	pPriv       ed25519.PrivateKey
	pAddr       string
	rPub        ed25519.PublicKey
	rPriv       ed25519.PrivateKey
	rAddr       string
}

// setupTokenizerScenario registers a domain with the given TokenizerID and a
// service in that domain. Funds the provider + requester.
func setupTokenizerScenario(t *testing.T, tokenizerID string) tokenizerScenario {
	t.Helper()
	st := newStateWithBond(t, 100, 0)
	authPub, authPriv, authAddr := makeKey(t)
	pPub, pPriv, pAddr := makeKey(t)
	rPub, rPriv, rAddr := makeKey(t)
	require.NoError(t, st.Mint(pAddr, 10_000))
	require.NoError(t, st.Mint(rAddr, 10_000))
	require.NoError(t, st.Mint(authAddr, 1_000))

	domTx := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr,
		ModelSHA256: "00000000000000000000000000000000000000000000000000000000000000aa",
		RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
		TokenizerID: tokenizerID,
	})
	domReceipt, err := st.SubmitTx(domTx)
	require.NoError(t, err)

	svcTx := signTx(t, pPriv, pPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: pAddr, Name: "tokenizer-test-svc",
		Price: types.Coin{Denom: "aios", Amount: 100}, VerificationDomainID: domReceipt.NewID,
	})
	svcReceipt, err := st.SubmitTx(svcTx)
	require.NoError(t, err)

	return tokenizerScenario{
		state: st, domain: domReceipt.NewID, svcID: svcReceipt.NewID,
		pPub: pPub, pPriv: pPriv, pAddr: pAddr,
		rPub: rPub, rPriv: rPriv, rAddr: rAddr,
	}
}

// requestAndSubmit drives the inference lifecycle to SUBMITTED (or fails on
// the submit step if the attestation tokenizer is wrong). Returns the submit
// error so tests can assert on it.
func (sc tokenizerScenario) requestAndSubmit(t *testing.T, attestationTokenizer string) error {
	t.Helper()
	reqTx := signTx(t, sc.rPriv, sc.rPub, 0, types.TxRequestInference, types.MsgRequestInference{
		Requester: sc.rAddr, ServiceID: sc.svcID, InputHash: "input-hash",
		InputURI: "inline:test", InputText: "test",
		MaxPrice: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := sc.state.SubmitTx(reqTx)
	require.NoError(t, err)

	resTx := signTx(t, sc.pPriv, sc.pPub, 1, types.TxSubmitResult, types.MsgSubmitResult{
		Provider: sc.pAddr, RequestID: 1,
		Result: types.Result{
			OutputHash: "ok", OutputURI: "inline:out", OutputText: "ok",
			Attestation: types.Attestation{
				Provider: sc.pAddr, VerificationDomainID: sc.domain,
				ModelSHA256: "00000000000000000000000000000000000000000000000000000000000000aa",
				RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
				TokenizerID: attestationTokenizer,
				InputHash:   "input-hash", OutputHash: "ok",
			},
		},
	})
	_, err = sc.state.SubmitTx(resTx)
	return err
}

func TestTokenizerPin_SubmitWithMatchingTokenizerAccepted(t *testing.T) {
	sc := setupTokenizerScenario(t, "test-tokenizer-v1")
	err := sc.requestAndSubmit(t, "test-tokenizer-v1")
	require.NoError(t, err, "matching tokenizer → accepted")
}

func TestTokenizerPin_SubmitWithMismatchedTokenizerRejected(t *testing.T) {
	sc := setupTokenizerScenario(t, "test-tokenizer-v1")
	err := sc.requestAndSubmit(t, "different-tokenizer")
	require.ErrorIs(t, err, types.ErrAttestationDomainMismatch,
		"different tokenizer → submission rejected")
}

func TestTokenizerPin_SubmitWithEmptyAttestationTokenizerRejectedWhenDomainPins(t *testing.T) {
	sc := setupTokenizerScenario(t, "test-tokenizer-v1")
	err := sc.requestAndSubmit(t, "") // legacy provider on a strict domain
	require.ErrorIs(t, err, types.ErrAttestationDomainMismatch,
		"legacy attestation (empty TokenizerID) on a strict domain → rejected")
}

func TestTokenizerPin_SubmitWithEmptyDomainTokenizerSkipsCheck(t *testing.T) {
	// Domain with empty TokenizerID = legacy. Attestation can have ANY tokenizer
	// value (including empty) and the chain accepts it. This preserves
	// backwards compatibility with Phase 0.5–3.z domains.
	sc := setupTokenizerScenario(t, "")

	err := sc.requestAndSubmit(t, "any-string-here-doesnt-matter")
	require.NoError(t, err, "legacy domain (empty TokenizerID) accepts any attestation tokenizer")
}

func TestTokenizerPin_SubmitWithEmptyOnBothSidesAccepted(t *testing.T) {
	// Pure backwards-compat path: legacy domain + legacy attestation.
	sc := setupTokenizerScenario(t, "")
	err := sc.requestAndSubmit(t, "")
	require.NoError(t, err, "fully-legacy path remains compatible")
}
