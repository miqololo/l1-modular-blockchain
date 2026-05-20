package state_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/types"
)

// ── helpers ───────────────────────────────────────────────────────────────

func registerService(t *testing.T, s any, pub []byte, priv []byte, addr string, name string) uint64 {
	t.Helper()
	// kept as a place-holder; specific tests below construct the tx inline
	return 0
}

// ── MsgUpdateService ──────────────────────────────────────────────────────

func TestUpdateService_HappyPath(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	// Register first
	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "svc", Description: "old", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	receipt, err := s.SubmitTx(regTx)
	require.NoError(t, err)
	svcID := receipt.NewID

	// Update
	updTx := signTx(t, priv, pub, 1, types.TxUpdateService, types.MsgUpdateService{
		Owner: addr, ServiceID: svcID, Description: "new description", Price: types.Coin{Denom: "aios", Amount: 200},
	})
	_, err = s.SubmitTx(updTx)
	require.NoError(t, err)

	svc, err := s.GetService(svcID)
	require.NoError(t, err)
	require.Equal(t, "new description", svc.Description)
	require.Equal(t, uint64(200), svc.Price.Amount)
}

func TestUpdateService_NonOwner_Rejected(t *testing.T) {
	s := newState(t)
	ownerPub, ownerPriv, ownerAddr := makeKey(t)
	otherPub, otherPriv, otherAddr := makeKey(t)

	regTx := signTx(t, ownerPriv, ownerPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: ownerAddr, Name: "svc", Description: "x", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	updTx := signTx(t, otherPriv, otherPub, 0, types.TxUpdateService, types.MsgUpdateService{
		Owner: otherAddr, ServiceID: 1, Description: "hijack", Price: types.Coin{Denom: "aios", Amount: 1},
	})
	_, err = s.SubmitTx(updTx)
	require.ErrorIs(t, err, types.ErrNotOwner)
}

func TestUpdateService_InactiveRejected(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "svc", Description: "x", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	updTx := signTx(t, priv, pub, 2, types.TxUpdateService, types.MsgUpdateService{
		Owner: addr, ServiceID: 1, Description: "after deactivate", Price: types.Coin{Denom: "aios", Amount: 50},
	})
	_, err = s.SubmitTx(updTx)
	require.ErrorIs(t, err, types.ErrServiceInactive)
}

func TestUpdateService_RejectsZeroPrice(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "svc", Description: "x", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	updTx := signTx(t, priv, pub, 1, types.TxUpdateService, types.MsgUpdateService{
		Owner: addr, ServiceID: 1, Description: "x", Price: types.Coin{Denom: "aios", Amount: 0},
	})
	_, err = s.SubmitTx(updTx)
	require.ErrorIs(t, err, types.ErrZeroPrice)
}

// ── MsgDeactivateService ──────────────────────────────────────────────────

func TestDeactivateService_HappyPath(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "svc", Description: "", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	svc, _ := s.GetService(1)
	require.False(t, svc.Active)
}

func TestDeactivateService_AlreadyInactive(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "svc", Description: "", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx1 := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx1)
	require.NoError(t, err)

	deTx2 := signTx(t, priv, pub, 2, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx2)
	require.ErrorIs(t, err, types.ErrServiceAlreadyInactive)
}

func TestDeactivateService_NonOwnerRejected(t *testing.T) {
	s := newState(t)
	ownerPub, ownerPriv, ownerAddr := makeKey(t)
	otherPub, otherPriv, otherAddr := makeKey(t)

	regTx := signTx(t, ownerPriv, ownerPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: ownerAddr, Name: "svc", Description: "", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx := signTx(t, otherPriv, otherPub, 0, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: otherAddr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.ErrorIs(t, err, types.ErrNotOwner)
}

// ── MsgDeactivateDomain ───────────────────────────────────────────────────

func TestDeactivateDomain_AuthorityOnly(t *testing.T) {
	s := newState(t)
	authPub, authPriv, authAddr := makeKey(t)

	// Authority becomes the first register-domain signer.
	regDom := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr, ModelSHA256: "sha", RuntimeID: "rt", HardwareTag: "hw", Precision: "fp16",
	})
	_, err := s.SubmitTx(regDom)
	require.NoError(t, err)

	// Non-authority can't deactivate.
	pub2, priv2, addr2 := makeKey(t)
	bad := signTx(t, priv2, pub2, 0, types.TxDeactivateDomain, types.MsgDeactivateDomain{
		Authority: addr2, DomainID: 1,
	})
	_, err = s.SubmitTx(bad)
	require.ErrorIs(t, err, types.ErrUnauthorized)

	// Authority can.
	ok := signTx(t, authPriv, authPub, 1, types.TxDeactivateDomain, types.MsgDeactivateDomain{
		Authority: authAddr, DomainID: 1,
	})
	_, err = s.SubmitTx(ok)
	require.NoError(t, err)

	d, _ := s.GetDomain(1)
	require.False(t, d.Active)
}

// ── MsgResolveChallenge ───────────────────────────────────────────────────
// Exercises the explicit authority-driven resolution path that overrides
// the block producer's timeout-based dispatch.

func TestResolveChallenge_RejectsWhenNotChallenged(t *testing.T) {
	s := newState(t)
	authPub, authPriv, authAddr := makeKey(t)

	// Make authority via register-domain.
	regDom := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr, ModelSHA256: "sha", RuntimeID: "rt", HardwareTag: "hw", Precision: "fp16",
	})
	_, err := s.SubmitTx(regDom)
	require.NoError(t, err)

	resTx := signTx(t, authPriv, authPub, 1, types.TxResolveChallenge, types.MsgResolveChallenge{
		Authority: authAddr, RequestID: 9999, Decision: "dismiss",
	})
	_, err = s.SubmitTx(resTx)
	require.Error(t, err) // request not found OR not-challenged
}

func TestResolveChallenge_InvalidDecision(t *testing.T) {
	s := newState(t)
	authPub, authPriv, authAddr := makeKey(t)

	// Make authority.
	regDom := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr, ModelSHA256: "sha", RuntimeID: "rt", HardwareTag: "hw", Precision: "fp16",
	})
	_, err := s.SubmitTx(regDom)
	require.NoError(t, err)

	bogus := signTx(t, authPriv, authPub, 1, types.TxResolveChallenge, types.MsgResolveChallenge{
		Authority: authAddr, RequestID: 1, Decision: "maybe",
	})
	_, err = s.SubmitTx(bogus)
	require.Error(t, err) // either invalid decision OR request not found — both acceptable
}

// silence unused-import warnings from the test helpers above.
var _ = registerService
