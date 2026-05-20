package state_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/types"
)

// Phase 3.z step 2: service-registration bond economics. Together with the
// eligibility check shipped in step 1, this raises the sybil-voucher cost from
// "free" to "bond × lifetime in capital lockup, or full bond forfeit on early
// deactivation".

const testBond uint64 = 100

func TestRegistrationBond_LocksAtRegister(t *testing.T) {
	s := newStateWithBond(t, testBond, 1000)
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 500))

	pre, err := s.Account(addr)
	require.NoError(t, err)
	require.Equal(t, uint64(500), pre.Balance)

	tx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "bonded-svc", Description: "x",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err = s.SubmitTx(tx)
	require.NoError(t, err)

	post, err := s.Account(addr)
	require.NoError(t, err)
	require.Equal(t, uint64(400), post.Balance, "bond should be debited from owner")

	svc, err := s.GetService(1)
	require.NoError(t, err)
	require.Equal(t, testBond, svc.RegistrationBond.Amount,
		"service should record the bond actually paid")
}

func TestRegistrationBond_InsufficientFundsRejects(t *testing.T) {
	s := newStateWithBond(t, testBond, 1000)
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 50)) // less than the bond

	tx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "broke-svc", Description: "x",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(tx)
	require.Error(t, err, "should refuse registration when owner can't cover the bond")

	// State should be untouched: no service, balance unchanged.
	_, err = s.GetService(1)
	require.Error(t, err)
	acct, err := s.Account(addr)
	require.NoError(t, err)
	require.Equal(t, uint64(50), acct.Balance, "balance must not be debited on failed registration")
}

func TestRegistrationBond_ForfeitedOnEarlyDeactivate(t *testing.T) {
	const lifetime = 1000
	s := newStateWithBond(t, testBond, lifetime)
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 500))

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "early-quit", Description: "x",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	// Deactivate immediately — height has barely advanced.
	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	acct, err := s.Account(addr)
	require.NoError(t, err)
	require.Equal(t, uint64(400), acct.Balance,
		"early deactivation should NOT refund the bond (it stays in escrow / forfeit)")
}

func TestRegistrationBond_RefundedOnLatedeactivate(t *testing.T) {
	// With MinServiceLifetimeBlocks = 0, deactivation always passes the lifetime
	// check immediately. This is the "honest operator who has run the service
	// long enough" path.
	s := newStateWithBond(t, testBond, 0)
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 500))

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "long-lived", Description: "x",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	acct, err := s.Account(addr)
	require.NoError(t, err)
	require.Equal(t, uint64(500), acct.Balance,
		"deactivation past lifetime should refund the bond")
}

// Eligibility tightening: a voucher whose only service in the domain has been
// deactivated should no longer be eligible.
func TestVoucherEligibility_DeactivatedServiceNotEligible(t *testing.T) {
	s := newStateWithBond(t, testBond, 0) // lifetime 0 so we can deactivate easily
	authPub, authPriv, authAddr := makeKey(t)
	require.NoError(t, s.Mint(authAddr, 500))
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 500))

	// Authority registers a domain.
	authTx := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr, ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000001",
		RuntimeID: "test-runtime", HardwareTag: "test-hardware", Precision: "fp32",
	})
	domReceipt, err := s.SubmitTx(authTx)
	require.NoError(t, err)
	domain := domReceipt.NewID

	// Subject registers a service in the domain, then deactivates it.
	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "transient", Description: "x",
		Price: types.Coin{Denom: "aios", Amount: 100}, VerificationDomainID: domain,
	})
	regReceipt, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	// While active: eligible.
	ok, err := s.IsVoucherEligible(addr, domain)
	require.NoError(t, err)
	require.True(t, ok, "active service in domain → eligible")

	// Deactivate.
	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: regReceipt.NewID,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	// Now: NOT eligible.
	ok, err = s.IsVoucherEligible(addr, domain)
	require.NoError(t, err)
	require.False(t, ok,
		"after deactivation, eligibility (which requires Active=true) must fail")
}

func TestRegistrationBond_DefaultParamsValues(t *testing.T) {
	// Phase 3.z step 2 ships these defaults. This test guards against
	// accidental drift in the default-params block.
	p := types.DefaultParams()
	require.Equal(t, uint64(100), p.ServiceRegistrationBond.Amount,
		"default service-registration bond is 100 aios")
	require.Equal(t, "aios", p.ServiceRegistrationBond.Denom)
	require.Equal(t, int64(1000), p.MinServiceLifetimeBlocks,
		"default minimum service lifetime is 1000 blocks (~17 min at 1s)")
}
