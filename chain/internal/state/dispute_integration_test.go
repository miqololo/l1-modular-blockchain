package state_test

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

// Full dispute-game integration: SUBMITTED → CHALLENGED → VOUCH → resolution.
// Exercises every Phase 3.x / 3.y / 3.z mechanism in one flow, including the
// new service-registration bonds. This is the test that was blocked on funding
// helpers before the Phase 3.z step 2 `Mint`/`SetParams` shipped.

const (
	demoDomainModel    = "0000000000000000000000000000000000000000000000000000000000000001"
	demoDomainRuntime  = "test-runtime"
	demoDomainHardware = "test-hardware"
	demoDomainPrec     = "fp32"

	// Resolution window is hardcoded in block.go / tx.go as 20 blocks. Ticking
	// 25 puts us safely past it.
	ticksToResolve = 25
)

// disputeRoles bundles the four keypairs used across the full flow.
type disputeRoles struct {
	authPub  ed25519.PublicKey
	authPriv ed25519.PrivateKey
	authAddr string

	providerPub  ed25519.PublicKey
	providerPriv ed25519.PrivateKey
	providerAddr string

	challengerPub  ed25519.PublicKey
	challengerPriv ed25519.PrivateKey
	challengerAddr string

	voucherPub  ed25519.PublicKey
	voucherPriv ed25519.PrivateKey
	voucherAddr string

	requesterPub  ed25519.PublicKey
	requesterPriv ed25519.PrivateKey
	requesterAddr string
}

// setupDispute boots a state with full Phase 3.z bonds enabled, funds five
// roles, registers the demo domain, the provider's service, and the voucher's
// witness service. Returns the domain id, provider service id, and the roles.
func setupDispute(t *testing.T) (*state.State, uint64, uint64, disputeRoles) {
	t.Helper()
	// Bonds: provider 50, challenger 50, voucher 25, service registration 100.
	// Lifetime 0 so we can verify happy-path refunds; eligibility-on-active is
	// independent of lifetime.
	s := newStateWithBond(t, 100, 0)
	p, err := s.Params()
	require.NoError(t, err)
	// Ensure dispute-game params are at defaults (the test-default newState
	// path zeros some of them; newStateWithBond uses DefaultParams + overrides).
	require.Equal(t, uint64(50), p.ProviderBondAmount.Amount)
	require.Equal(t, uint64(50), p.ChallengerBondAmount.Amount)
	require.Equal(t, uint64(25), p.VoucherBondAmount.Amount)

	var r disputeRoles
	r.authPub, r.authPriv, r.authAddr = makeKey(t)
	r.providerPub, r.providerPriv, r.providerAddr = makeKey(t)
	r.challengerPub, r.challengerPriv, r.challengerAddr = makeKey(t)
	r.voucherPub, r.voucherPriv, r.voucherAddr = makeKey(t)
	r.requesterPub, r.requesterPriv, r.requesterAddr = makeKey(t)

	// Fund all five accounts comfortably for any combination of bonds.
	for _, addr := range []string{r.authAddr, r.providerAddr, r.challengerAddr, r.voucherAddr, r.requesterAddr} {
		require.NoError(t, s.Mint(addr, 10_000))
	}

	// Authority registers the demo domain (first signer becomes authority).
	domTx := signTx(t, r.authPriv, r.authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: r.authAddr, ModelSHA256: demoDomainModel, RuntimeID: demoDomainRuntime,
		HardwareTag: demoDomainHardware, Precision: demoDomainPrec, Description: "dispute test domain",
	})
	domReceipt, err := s.SubmitTx(domTx)
	require.NoError(t, err)
	domain := domReceipt.NewID

	// Provider registers their service in the domain.
	provSvcTx := signTx(t, r.providerPriv, r.providerPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: r.providerAddr, Name: "provider-svc", Description: "provider's service",
		Price: types.Coin{Denom: "aios", Amount: 100}, VerificationDomainID: domain,
	})
	provSvcReceipt, err := s.SubmitTx(provSvcTx)
	require.NoError(t, err)
	providerSvc := provSvcReceipt.NewID

	// Voucher registers their witness service — domain-residency proof so that
	// MsgVouch passes the Phase 3.z step 2 eligibility check.
	voucherSvcTx := signTx(t, r.voucherPriv, r.voucherPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: r.voucherAddr, Name: "voucher-witness", Description: "voucher's witness",
		Price: types.Coin{Denom: "aios", Amount: 1_000_000_000}, VerificationDomainID: domain,
	})
	_, err = s.SubmitTx(voucherSvcTx)
	require.NoError(t, err)

	return s, domain, providerSvc, r
}

// submitInferenceAndResult drives PENDING → SUBMITTED. Returns the request id
// and the provider's output hash (which the test will then dispute).
func submitInferenceAndResult(
	t *testing.T,
	s *state.State,
	r disputeRoles,
	domain uint64,
	serviceID uint64,
	requesterNonce uint64,
	providerNonce uint64,
	providerOutputHash string,
) uint64 {
	t.Helper()
	const inputHash = "input-hash-AAAA"
	reqTx := signTx(t, r.requesterPriv, r.requesterPub, requesterNonce, types.TxRequestInference, types.MsgRequestInference{
		Requester: r.requesterAddr, ServiceID: serviceID, InputHash: inputHash,
		InputURI: "inline:test", InputText: "test prompt",
		MaxPrice: types.Coin{Denom: "aios", Amount: 100},
	})
	reqReceipt, err := s.SubmitTx(reqTx)
	require.NoError(t, err)
	requestID := reqReceipt.NewID

	resTx := signTx(t, r.providerPriv, r.providerPub, providerNonce, types.TxSubmitResult, types.MsgSubmitResult{
		Provider: r.providerAddr, RequestID: requestID,
		Result: types.Result{
			OutputHash: providerOutputHash,
			OutputURI:  "inline:result",
			OutputText: "result",
			Attestation: types.Attestation{
				Provider: r.providerAddr, VerificationDomainID: domain,
				ModelSHA256: demoDomainModel, RuntimeID: demoDomainRuntime,
				HardwareTag: demoDomainHardware, Precision: demoDomainPrec,
				InputHash: inputHash, OutputHash: providerOutputHash,
			},
		},
	})
	_, err = s.SubmitTx(resTx)
	require.NoError(t, err)

	return requestID
}

// fileChallenge drives SUBMITTED → CHALLENGED. The challenger commits to a
// different output hash than the provider.
func fileChallenge(
	t *testing.T,
	s *state.State,
	r disputeRoles,
	domain uint64,
	requestID uint64,
	nonce uint64,
	challengerOutputHash string,
) {
	t.Helper()
	tx := signTx(t, r.challengerPriv, r.challengerPub, nonce, types.TxChallenge, types.MsgChallenge{
		Challenger: r.challengerAddr, RequestID: requestID,
		Attestation: types.Attestation{
			Provider: r.challengerAddr, VerificationDomainID: domain,
			ModelSHA256: demoDomainModel, RuntimeID: demoDomainRuntime,
			HardwareTag: demoDomainHardware, Precision: demoDomainPrec,
			InputHash: "input-hash-AAAA", OutputHash: challengerOutputHash,
		},
	})
	_, err := s.SubmitTx(tx)
	require.NoError(t, err)
}

// fileVouch drives a CHALLENGED request's voucher tally.
func fileVouch(
	t *testing.T,
	s *state.State,
	r disputeRoles,
	domain uint64,
	requestID uint64,
	nonce uint64,
	supportedOutputHash string,
) {
	t.Helper()
	tx := signTx(t, r.voucherPriv, r.voucherPub, nonce, types.TxVouch, types.MsgVouch{
		Voucher: r.voucherAddr, RequestID: requestID,
		Attestation: types.Attestation{
			Provider: r.voucherAddr, VerificationDomainID: domain,
			ModelSHA256: demoDomainModel, RuntimeID: demoDomainRuntime,
			HardwareTag: demoDomainHardware, Precision: demoDomainPrec,
			InputHash: "input-hash-AAAA", OutputHash: supportedOutputHash,
		},
	})
	_, err := s.SubmitTx(tx)
	require.NoError(t, err)
}

// tickN advances the block height by n ticks.
func tickN(t *testing.T, s *state.State, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		require.NoError(t, s.Tick())
	}
}

// Dismissal path: provider submits honest output, challenger files spurious
// challenge, voucher backs provider. Resolution should DISMISS the challenge.
func TestDisputeFlow_DismissedByVoucher(t *testing.T) {
	s, domain, providerSvc, r := setupDispute(t)

	const honestHash = "honest-output-hash"
	const spuriousHash = "spurious-counter-hash"

	// Capture balances after setup so we can isolate dispute-game flows.
	preProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	preChallenger, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	preVoucher, err := s.Account(r.voucherAddr)
	require.NoError(t, err)
	preRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)

	requestID := submitInferenceAndResult(t, s, r, domain, providerSvc, 0, 1, honestHash)

	// After submit, escrow (100) is debited from requester and provider bond
	// (50) is debited from provider.
	postSubmit, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance-100, postSubmit.Balance, "requester paid escrow")
	postProvider1, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance-50, postProvider1.Balance, "provider posted bond")

	tickN(t, s, 1)
	fileChallenge(t, s, r, domain, requestID, 0, spuriousHash)
	postChallenger1, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	require.Equal(t, preChallenger.Balance-50, postChallenger1.Balance, "challenger posted bond")

	tickN(t, s, 1)
	fileVouch(t, s, r, domain, requestID, 1, honestHash) // back provider
	postVoucher1, err := s.Account(r.voucherAddr)
	require.NoError(t, err)
	require.Equal(t, preVoucher.Balance-25, postVoucher1.Balance, "voucher posted bond")

	// Drive resolution by ticking past the resolution window.
	tickN(t, s, ticksToResolve)

	req, err := s.GetRequest(requestID)
	require.NoError(t, err)
	require.Equal(t, types.StatusFinalized, req.Status,
		"voucher backed provider; provider wins → DISMISSED → FINALIZED")

	// Dismissal flow money map:
	//   escrow (100) → provider
	//   provider bond (50) → provider returned
	//   challenger bond (50) → reward pool
	//   voucher bond (25) → voucher returned
	//   reward pool (50) split among recipients (provider + 1 voucher = 2) = 25 each
	// Final provider delta: +100 (escrow) +0 (bond returned) +25 (reward share) = +125
	// Final voucher delta:  +0 (bond returned) +25 (reward share) = +25
	// Final challenger delta: -50 (lost bond)
	// Final requester delta: -100 (paid for service)
	postProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance+125, postProvider.Balance,
		"provider receives escrow + bond + reward share")

	postChallenger, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	require.Equal(t, preChallenger.Balance-50, postChallenger.Balance,
		"challenger forfeits bond on dismissal")

	postVoucher, err := s.Account(r.voucherAddr)
	require.NoError(t, err)
	require.Equal(t, preVoucher.Balance+25, postVoucher.Balance,
		"voucher receives bond back + reward share equal to bond")

	postRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance-100, postRequester.Balance,
		"requester pays for service; not refunded on dismissal")
}

// Slash path: provider submits dishonest output, challenger files correct
// challenge, no voucher arrives (or voucher backs challenger). Resolution
// should SLASH the provider.
func TestDisputeFlow_SlashedWithoutVoucher(t *testing.T) {
	s, domain, providerSvc, r := setupDispute(t)

	const wrongHash = "wrong-provider-output"
	const correctHash = "correct-challenger-output"

	preProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	preChallenger, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	preRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)

	requestID := submitInferenceAndResult(t, s, r, domain, providerSvc, 0, 1, wrongHash)

	tickN(t, s, 1)
	fileChallenge(t, s, r, domain, requestID, 0, correctHash)

	// No voucher this time. Drive resolution.
	tickN(t, s, ticksToResolve)

	req, err := s.GetRequest(requestID)
	require.NoError(t, err)
	require.Equal(t, types.StatusSlashed, req.Status,
		"no provider voucher arrived; default branch is SLASH")

	// Slash flow money map:
	//   escrow (100) → requester (refunded)
	//   provider bond (50) → challenger (slashing reward)
	//   challenger bond (50) → challenger (returned)
	// Final provider delta:   -50 (bond forfeit)
	// Final challenger delta: +50 (provider's bond)
	// Final requester delta:  0 (escrow paid then refunded)
	postProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance-50, postProvider.Balance,
		"provider forfeits bond on slash")

	postChallenger, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	require.Equal(t, preChallenger.Balance+50, postChallenger.Balance,
		"challenger receives provider bond on slash + own bond returned")

	postRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance, postRequester.Balance,
		"requester is made whole by escrow refund")
}

// Eligibility enforcement at the integration level: a voucher with no service
// in the disputed request's domain is rejected. (Unit-tested in
// voucher_eligibility_test.go; this confirms applyVouch routes the error
// through the full SubmitTx pipeline.)
func TestDisputeFlow_VouchRejectedWithoutEligibility(t *testing.T) {
	s, domain, providerSvc, r := setupDispute(t)
	const honestHash = "honest-output"
	const spuriousHash = "spurious-counter"

	requestID := submitInferenceAndResult(t, s, r, domain, providerSvc, 0, 1, honestHash)
	tickN(t, s, 1)
	fileChallenge(t, s, r, domain, requestID, 0, spuriousHash)
	tickN(t, s, 1)

	// A wholly unrelated key has no service in the domain → ineligible.
	outsiderPub, outsiderPriv, outsiderAddr := makeKey(t)
	require.NoError(t, s.Mint(outsiderAddr, 10_000))

	tx := signTx(t, outsiderPriv, outsiderPub, 0, types.TxVouch, types.MsgVouch{
		Voucher: outsiderAddr, RequestID: requestID,
		Attestation: types.Attestation{
			Provider: outsiderAddr, VerificationDomainID: domain,
			ModelSHA256: demoDomainModel, RuntimeID: demoDomainRuntime,
			HardwareTag: demoDomainHardware, Precision: demoDomainPrec,
			InputHash: "input-hash-AAAA", OutputHash: honestHash,
		},
	})
	_, err := s.SubmitTx(tx)
	require.ErrorIs(t, err, types.ErrVouchNotEligible,
		"outsider with no service in domain must be rejected at the boundary")
}
