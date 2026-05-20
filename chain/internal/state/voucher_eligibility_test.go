package state_test

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

// Phase 3.z: voucher sybil resistance. The eligibility helper requires a
// would-be voucher to operate at least one service in the disputed request's
// verification domain. Without this gate, anyone with 25 aios can vouch and
// tip a single-margin vote (threat-model.md A3.2).

// seedDomain1 registers a verification domain using `priv/pub` as the
// bootstrap authority. Returns the domain id (= 1 on a fresh state).
func seedDomain1(t *testing.T, s *state.State, pub ed25519.PublicKey, priv ed25519.PrivateKey, addr string, nonce uint64) uint64 {
	t.Helper()
	tx := signTx(t, priv, pub, nonce, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:   addr,
		ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000001",
		RuntimeID:   "test-runtime",
		HardwareTag: "test-hardware",
		Precision:   "fp32",
		Description: "test domain",
	})
	receipt, err := s.SubmitTx(tx)
	require.NoError(t, err)
	return receipt.NewID
}

func TestVoucherEligibility_UnverifiedDomainAlwaysEligible(t *testing.T) {
	s := newState(t)
	_, _, addr := makeKey(t)

	ok, err := s.IsVoucherEligible(addr, 0)
	require.NoError(t, err)
	require.True(t, ok, "domain_id=0 (unverified) should bypass eligibility")
}

func TestVoucherEligibility_NoServiceRejected(t *testing.T) {
	s := newState(t)
	_, _, addr := makeKey(t)

	ok, err := s.IsVoucherEligible(addr, 1)
	require.NoError(t, err)
	require.False(t, ok, "no service registered → not eligible")
}

func TestVoucherEligibility_WrongDomainRejected(t *testing.T) {
	s := newState(t)
	authPub, authPriv, authAddr := makeKey(t)
	pub, priv, addr := makeKey(t)

	// Authority bootstraps two domains.
	domain1 := seedDomain1(t, s, authPub, authPriv, authAddr, 0)
	domain2Tx := signTx(t, authPriv, authPub, 1, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr, ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000002",
		RuntimeID: "test-runtime", HardwareTag: "test-hardware", Precision: "fp32",
		Description: "second domain",
	})
	receipt, err := s.SubmitTx(domain2Tx)
	require.NoError(t, err)
	domain2 := receipt.NewID
	require.NotEqual(t, domain1, domain2)

	// Subject registers a service in domain 1.
	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner:                addr,
		Name:                 "service-in-domain-1",
		Description:          "test",
		Price:                types.Coin{Denom: "aios", Amount: 100},
		VerificationDomainID: domain1,
	})
	_, err = s.SubmitTx(regTx)
	require.NoError(t, err)

	// Asked about domain 2 → not eligible (their service is in 1, not 2).
	ok, err := s.IsVoucherEligible(addr, domain2)
	require.NoError(t, err)
	require.False(t, ok, "service in wrong domain → not eligible")
}

func TestVoucherEligibility_OwnsServiceInDomain(t *testing.T) {
	s := newState(t)
	authPub, authPriv, authAddr := makeKey(t)
	pub, priv, addr := makeKey(t)

	domain := seedDomain1(t, s, authPub, authPriv, authAddr, 0)

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner:                addr,
		Name:                 "domain-resident",
		Description:          "test",
		Price:                types.Coin{Denom: "aios", Amount: 100},
		VerificationDomainID: domain,
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	ok, err := s.IsVoucherEligible(addr, domain)
	require.NoError(t, err)
	require.True(t, ok, "owner of a service in the domain should be eligible")
}

func TestVoucherEligibility_DistinctAddressesIsolated(t *testing.T) {
	s := newState(t)
	authPub, authPriv, authAddr := makeKey(t)
	pubA, privA, addrA := makeKey(t)
	_, _, addrB := makeKey(t)

	domain := seedDomain1(t, s, authPub, authPriv, authAddr, 0)

	// A registers; B does not.
	regTx := signTx(t, privA, pubA, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner:                addrA,
		Name:                 "A's service",
		Description:          "test",
		Price:                types.Coin{Denom: "aios", Amount: 100},
		VerificationDomainID: domain,
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	okA, err := s.IsVoucherEligible(addrA, domain)
	require.NoError(t, err)
	require.True(t, okA)

	okB, err := s.IsVoucherEligible(addrB, domain)
	require.NoError(t, err)
	require.False(t, okB, "B did not register in domain; should not be eligible")
}

func TestVoucherMargin_DefaultIsZero(t *testing.T) {
	// Phase 3.z ships VoucherMargin = 0 by default to keep 3.y semantics
	// (provider wins ties). This test guards against accidental bumps that
	// would break the demo (where a single harness voucher must be able to
	// dismiss a spurious challenge).
	params := types.DefaultParams()
	require.Equal(t, int64(0), params.VoucherMargin,
		"default VoucherMargin must be 0 to preserve 3.y demo semantics")
}
