package state_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

// Phase 3.z: MsgDeactivateDomain cascades. When the authority retires a
// verification domain, every dependent piece of state unwinds:
//   - Services bound to the domain → auto-deactivated, registration bonds
//     refunded in full (regardless of MinServiceLifetimeBlocks).
//   - Open requests on those services → voided. Every locked party is made
//     whole; nobody wins, nobody loses.

func TestDomainVoid_PendingRequest_RefundsRequester(t *testing.T) {
	s, domain, providerSvc, r := setupDispute(t)
	const inputHash = "input-hash-pending"

	prePrice := uint64(100)
	preRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)

	// Request: alice escrows 100 to the chain.
	reqTx := signTx(t, r.requesterPriv, r.requesterPub, 0, types.TxRequestInference, types.MsgRequestInference{
		Requester: r.requesterAddr, ServiceID: providerSvc,
		InputHash: inputHash, InputURI: "inline:test", InputText: "test",
		MaxPrice: types.Coin{Denom: "aios", Amount: prePrice},
	})
	_, err = s.SubmitTx(reqTx)
	require.NoError(t, err)

	postReq, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance-prePrice, postReq.Balance, "escrow debited")

	// Authority deactivates the domain. PENDING request should be voided.
	deactivateDomain(t, s, r, domain, 1)

	postVoid, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance, postVoid.Balance, "escrow returned to requester on void")

	req, err := s.GetRequest(1)
	require.NoError(t, err)
	require.Equal(t, types.StatusRefunded, req.Status, "PENDING request voided → REFUNDED")
}

func TestDomainVoid_SubmittedRequest_RefundsBothPartiesAndKeepsProviderBond(t *testing.T) {
	s, domain, providerSvc, r := setupDispute(t)

	preProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	preRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)

	requestID := submitInferenceAndResult(t, s, r, domain, providerSvc, 0, 1, "result-hash")

	// Both parties have locked stakes: requester escrow + provider bond.
	postSubmit, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance-50, postSubmit.Balance)
	postReq, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance-100, postReq.Balance)

	// Authority voids the domain. Two flows fire for the provider:
	//   (a) Request-level: provider bond (50) returned via executeVoidDueToDomain
	//   (b) Service-level: registration bond (100) refunded by the cascade,
	//       overriding MinServiceLifetimeBlocks because the chain is killing
	//       the service, not the operator.
	deactivateDomain(t, s, r, domain, 1)

	postVoidProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance+100, postVoidProvider.Balance,
		"provider made whole + service registration bond refunded by cascade")

	postVoidRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance, postVoidRequester.Balance,
		"requester escrow returned in full on domain void")

	req, err := s.GetRequest(requestID)
	require.NoError(t, err)
	require.Equal(t, types.StatusRefunded, req.Status, "SUBMITTED request voided → REFUNDED")
}

func TestDomainVoid_ChallengedRequest_RefundsEveryone(t *testing.T) {
	s, domain, providerSvc, r := setupDispute(t)

	preProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	preChallenger, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	preVoucher, err := s.Account(r.voucherAddr)
	require.NoError(t, err)
	preRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)

	requestID := submitInferenceAndResult(t, s, r, domain, providerSvc, 0, 1, "honest-hash")
	tickN(t, s, 1)
	fileChallenge(t, s, r, domain, requestID, 0, "alternate-hash")
	tickN(t, s, 1)
	fileVouch(t, s, r, domain, requestID, 1, "honest-hash") // back provider

	// Authority deactivates the domain BEFORE resolution window expires.
	// Every locked party made whole. The provider and voucher each ALSO get
	// their service-registration bond (+100) refunded by the cascade — they
	// each had a service in the dying domain.
	deactivateDomain(t, s, r, domain, 1)

	postProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance+100, postProvider.Balance,
		"provider request bond returned + service registration bond refunded")

	postChallenger, err := s.Account(r.challengerAddr)
	require.NoError(t, err)
	require.Equal(t, preChallenger.Balance, postChallenger.Balance,
		"challenger bond returned on void (no service to refund)")

	postVoucher, err := s.Account(r.voucherAddr)
	require.NoError(t, err)
	require.Equal(t, preVoucher.Balance+100, postVoucher.Balance,
		"voucher vouch bond returned + witness service registration bond refunded")

	postRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, preRequester.Balance, postRequester.Balance,
		"requester escrow returned on void")

	req, err := s.GetRequest(requestID)
	require.NoError(t, err)
	require.Equal(t, types.StatusRefunded, req.Status, "CHALLENGED request voided → REFUNDED")
}

func TestDomainVoid_ServiceAutoDeactivatedAndBondRefunded(t *testing.T) {
	// Verify the service-level cascade independently of any open request.
	// Even if the service was just registered (way under MinServiceLifetimeBlocks),
	// domain void should refund the full registration bond.
	s, domain, providerSvc, r := setupDispute(t)

	preProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	preVoucher, err := s.Account(r.voucherAddr)
	require.NoError(t, err)

	// No requests pending. Just deactivate the domain (auth nonce 1 because
	// nonce 0 went to register-domain in setupDispute).
	deactivateDomain(t, s, r, domain, 1)

	// Both the provider's service AND the voucher's witness service should be
	// deactivated with full bond refund.
	postProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance+100, postProvider.Balance,
		"provider's registration bond refunded on domain void (lifetime bypassed)")

	postVoucher, err := s.Account(r.voucherAddr)
	require.NoError(t, err)
	require.Equal(t, preVoucher.Balance+100, postVoucher.Balance,
		"voucher's witness service bond refunded too")

	svc, err := s.GetService(providerSvc)
	require.NoError(t, err)
	require.False(t, svc.Active, "service marked inactive")
}

func TestDomainVoid_FinalizedRequest_Untouched(t *testing.T) {
	// Terminal-state requests (FINALIZED, SLASHED, REFUNDED) should not be
	// touched by domain void — they're already done. Drive a request through
	// the normal Phase 3 path: submit → wait challenge window → block producer
	// auto-finalizes. Then deactivate the domain.
	s, domain, providerSvc, r := setupDispute(t)
	p, err := s.Params()
	require.NoError(t, err)
	p.ChallengeWindowBlocks = 2 // tiny window so we don't tick 45 times
	require.NoError(t, s.SetParams(p))

	requestID := submitInferenceAndResult(t, s, r, domain, providerSvc, 0, 1, "result-hash")
	tickN(t, s, 4) // 4 > SubmittedAtHeight(0) + ChallengeWindowBlocks(2)+1; auto-finalize fires

	req, err := s.GetRequest(requestID)
	require.NoError(t, err)
	require.Equal(t, types.StatusFinalized, req.Status, "precondition: auto-finalized")

	// Capture balances after the finalize. From here forward, deactivating the
	// domain should only refund the service registration bond (+100); the
	// finalized request must not be re-touched.
	midProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	midRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)

	deactivateDomain(t, s, r, domain, 1)

	postProvider, err := s.Account(r.providerAddr)
	require.NoError(t, err)
	require.Equal(t, midProvider.Balance+100, postProvider.Balance,
		"only the service bond refund hits; finalized escrow is not double-refunded")

	postRequester, err := s.Account(r.requesterAddr)
	require.NoError(t, err)
	require.Equal(t, midRequester.Balance, postRequester.Balance,
		"requester balance unchanged: they paid for service that was delivered before void")
}

// deactivateDomain signs MsgDeactivateDomain with the authority key. Uses
// `authNonce` because the auth key has already used nonce 0 to register the
// domain during setupDispute.
func deactivateDomain(t *testing.T, s *state.State, r disputeRoles, domain uint64, authNonce uint64) {
	t.Helper()
	tx := signTx(t, r.authPriv, r.authPub, authNonce, types.TxDeactivateDomain, types.MsgDeactivateDomain{
		Authority: r.authAddr, DomainID: domain,
	})
	_, err := s.SubmitTx(tx)
	require.NoError(t, err)
}
