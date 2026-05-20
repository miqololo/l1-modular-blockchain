package state_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aios/aios/internal/types"
)

// Phase 3.z steps 3, 4, plus treasury sweep + the immediate-finalize bond
// bug fix. Each test isolates the new behavior with a focused setup.

// ── Step 3: per-domain VoucherMargin ────────────────────────────────────────

func TestPerDomainVoucherMargin_AcceptedAtRegister(t *testing.T) {
	s := newState(t) // zero-bond default
	pub, priv, addr := makeKey(t)
	tx := signTx(t, priv, pub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:     addr,
		ModelSHA256:   "0000000000000000000000000000000000000000000000000000000000000001",
		RuntimeID:     "test-runtime",
		HardwareTag:   "test-hardware",
		Precision:     "fp32",
		VoucherMargin: 2,
	})
	receipt, err := s.SubmitTx(tx)
	require.NoError(t, err)

	dom, err := s.GetDomain(receipt.NewID)
	require.NoError(t, err)
	require.Equal(t, int64(2), dom.VoucherMargin, "per-domain margin persisted")
}

func TestPerDomainVoucherMargin_RejectsNegative(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)
	tx := signTx(t, priv, pub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:     addr,
		ModelSHA256:   "0000000000000000000000000000000000000000000000000000000000000002",
		RuntimeID:     "test-runtime",
		HardwareTag:   "test-hardware",
		Precision:     "fp32",
		VoucherMargin: -1,
	})
	_, err := s.SubmitTx(tx)
	require.Error(t, err, "negative VoucherMargin must be rejected")
}

func TestPerDomainVoucherMargin_DefaultZeroInheritsGlobal(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)
	tx := signTx(t, priv, pub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority:   addr,
		ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000003",
		RuntimeID:   "test-runtime",
		HardwareTag: "test-hardware",
		Precision:   "fp32",
		// VoucherMargin omitted → 0 → inherit from Params at resolution time
	})
	receipt, err := s.SubmitTx(tx)
	require.NoError(t, err)

	dom, err := s.GetDomain(receipt.NewID)
	require.NoError(t, err)
	require.Equal(t, int64(0), dom.VoucherMargin, "default 0 = inherit")
}

// ── Step 4: VoucherBondScaleBP ──────────────────────────────────────────────

func TestVoucherBondScale_DefaultIs5000BP(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, int64(5000), p.VoucherBondScaleBP,
		"default scale must be 5000 (50%) to preserve legacy 25-of-50 ratio")
}

func TestVoucherBondScale_NumericallyMatchesLegacy(t *testing.T) {
	// 50 aios provider bond × 5000 BP / 10000 = 25 aios → same as the legacy
	// fixed VoucherBondAmount = 25. Default behavior is unchanged.
	p := types.DefaultParams()
	providerBond := p.ProviderBondAmount.Amount
	scaled := providerBond * uint64(p.VoucherBondScaleBP) / 10000
	require.Equal(t, p.VoucherBondAmount.Amount, scaled,
		"default scale must reproduce the legacy voucher bond amount")
}

func TestVoucherBondScale_HigherStakesDoubleVoucherBond(t *testing.T) {
	// If a deployment runs ProviderBondAmount = 200 (4× default), the voucher
	// bond should scale to 100 (4× default). Sanity arithmetic; no state.
	highProviderBond := uint64(200)
	scaleBP := int64(5000)
	scaled := highProviderBond * uint64(scaleBP) / 10000
	require.Equal(t, uint64(100), scaled)
}

// ── Treasury sweep ──────────────────────────────────────────────────────────

func TestTreasury_ForfeitFromEarlyDeactivate(t *testing.T) {
	s := newStateWithBond(t, 100, 1000) // bond 100, lifetime 1000 blocks
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 500))

	preTreasury, err := s.TreasuryBalance()
	require.NoError(t, err)
	require.Equal(t, uint64(0), preTreasury, "treasury starts empty")

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "early-quit",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err = s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	postTreasury, err := s.TreasuryBalance()
	require.NoError(t, err)
	require.Equal(t, uint64(100), postTreasury,
		"early-deactivation bond should land in treasury, not stay in escrow")

	// The owner's balance reflects only the registration debit, no refund.
	post, err := s.Account(addr)
	require.NoError(t, err)
	require.Equal(t, uint64(400), post.Balance, "no bond refund on early deactivation")
}

func TestTreasury_NoForfeitOnLateDeactivate(t *testing.T) {
	s := newStateWithBond(t, 100, 0) // lifetime 0 → any deactivation is "late"
	pub, priv, addr := makeKey(t)
	require.NoError(t, s.Mint(addr, 500))

	regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "long-lived",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(regTx)
	require.NoError(t, err)

	deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
		Owner: addr, ServiceID: 1,
	})
	_, err = s.SubmitTx(deTx)
	require.NoError(t, err)

	postTreasury, err := s.TreasuryBalance()
	require.NoError(t, err)
	require.Equal(t, uint64(0), postTreasury,
		"late deactivation refunds owner; treasury should not gain")
}

func TestTreasury_AccumulatesAcrossForfeits(t *testing.T) {
	s := newStateWithBond(t, 100, 1000)
	for i := 0; i < 3; i++ {
		pub, priv, addr := makeKey(t)
		require.NoError(t, s.Mint(addr, 500))
		regTx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
			Owner: addr, Name: "svc-" + string(rune('a'+i)),
			Price: types.Coin{Denom: "aios", Amount: 100},
		})
		_, err := s.SubmitTx(regTx)
		require.NoError(t, err)
		deTx := signTx(t, priv, pub, 1, types.TxDeactivateService, types.MsgDeactivateService{
			Owner: addr, ServiceID: uint64(i + 1),
		})
		_, err = s.SubmitTx(deTx)
		require.NoError(t, err)
	}
	postTreasury, err := s.TreasuryBalance()
	require.NoError(t, err)
	require.Equal(t, uint64(300), postTreasury, "3 forfeits × 100 each")
}

// ── Immediate-finalize bond fix ─────────────────────────────────────────────

// ── Treasury sweep on slash path (integration) ─────────────────────────────

func TestTreasury_SlashForfeitsProviderSideVoucherBondToTreasury(t *testing.T) {
	// Full SUBMITTED → CHALLENGED → VOUCH(provider) → SLASH flow. Uses a
	// per-domain VoucherMargin = 2 to force SLASH even though a provider-side
	// voucher exists (1 provider voucher - 0 challenger vouchers = 1 < 2).
	// The voucher's bond should land in the treasury, not stay in escrow.
	s := newStateWithBond(t, 100, 0)
	authPub, authPriv, authAddr := makeKey(t)
	pPub, pPriv, pAddr := makeKey(t)
	cPub, cPriv, cAddr := makeKey(t)
	vPub, vPriv, vAddr := makeKey(t)
	rPub, rPriv, rAddr := makeKey(t)
	for _, addr := range []string{pAddr, cAddr, vAddr, rAddr} {
		require.NoError(t, s.Mint(addr, 10_000))
	}

	// Domain with VoucherMargin = 2 — even one provider-side voucher won't
	// reach the dismiss threshold.
	domTx := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr,
		ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000088",
		RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
		VoucherMargin: 2,
	})
	domReceipt, err := s.SubmitTx(domTx)
	require.NoError(t, err)
	domain := domReceipt.NewID

	// Provider registers service in this strict-margin domain.
	psvcTx := signTx(t, pPriv, pPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: pAddr, Name: "strict-svc",
		Price: types.Coin{Denom: "aios", Amount: 100}, VerificationDomainID: domain,
	})
	_, err = s.SubmitTx(psvcTx)
	require.NoError(t, err)

	// Voucher registers their witness service.
	vsvcTx := signTx(t, vPriv, vPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: vAddr, Name: "witness", Price: types.Coin{Denom: "aios", Amount: 1_000_000_000},
		VerificationDomainID: domain,
	})
	_, err = s.SubmitTx(vsvcTx)
	require.NoError(t, err)

	preTreasury, err := s.TreasuryBalance()
	require.NoError(t, err)

	// Requester requests inference.
	reqTx := signTx(t, rPriv, rPub, 0, types.TxRequestInference, types.MsgRequestInference{
		Requester: rAddr, ServiceID: 1, InputHash: "input-hash",
		InputURI: "inline:test", InputText: "test",
		MaxPrice: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err = s.SubmitTx(reqTx)
	require.NoError(t, err)

	// Provider submits. Result hash is the disputed one.
	resTx := signTx(t, pPriv, pPub, 1, types.TxSubmitResult, types.MsgSubmitResult{
		Provider: pAddr, RequestID: 1,
		Result: types.Result{
			OutputHash: "providers-hash", OutputURI: "inline", OutputText: "ok",
			Attestation: types.Attestation{
				Provider: pAddr, VerificationDomainID: domain,
				ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000088",
				RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
				InputHash: "input-hash", OutputHash: "providers-hash",
			},
		},
	})
	_, err = s.SubmitTx(resTx)
	require.NoError(t, err)

	tickN(t, s, 1)
	// Challenger files with a different hash.
	chalTx := signTx(t, cPriv, cPub, 0, types.TxChallenge, types.MsgChallenge{
		Challenger: cAddr, RequestID: 1,
		Attestation: types.Attestation{
			Provider: cAddr, VerificationDomainID: domain,
			ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000088",
			RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
			InputHash: "input-hash", OutputHash: "challengers-hash",
		},
	})
	_, err = s.SubmitTx(chalTx)
	require.NoError(t, err)

	tickN(t, s, 1)
	// Voucher backs the provider's (about-to-lose) side.
	vouchTx := signTx(t, vPriv, vPub, 1, types.TxVouch, types.MsgVouch{
		Voucher: vAddr, RequestID: 1,
		Attestation: types.Attestation{
			Provider: vAddr, VerificationDomainID: domain,
			ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000088",
			RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
			InputHash: "input-hash", OutputHash: "providers-hash",
		},
	})
	_, err = s.SubmitTx(vouchTx)
	require.NoError(t, err)

	tickN(t, s, ticksToResolve)

	req, err := s.GetRequest(1)
	require.NoError(t, err)
	require.Equal(t, types.StatusSlashed, req.Status,
		"margin=2 + 1 provider voucher + 0 challenger voucher → 1 < 2 → SLASH")

	postTreasury, err := s.TreasuryBalance()
	require.NoError(t, err)
	// Voucher bond = providerBond(50) × scale(5000) / 10000 = 25.
	require.Equal(t, preTreasury+25, postTreasury,
		"provider-side voucher's bond (25) should be swept to treasury on slash")
}

// ── Immediate-finalize bond fix (ChallengeWindowBlocks=0 path) ──────────────

func TestImmediateFinalize_ReturnsProviderBond(t *testing.T) {
	// Set ChallengeWindowBlocks=0 to exercise the immediate-finalize path
	// where the pre-existing bug would leak the provider bond into escrow.
	s := newStateWithBond(t, 0, 0) // no service bond, to isolate provider bond accounting
	p, err := s.Params()
	require.NoError(t, err)
	p.ChallengeWindowBlocks = 0
	require.NoError(t, s.SetParams(p))

	authPub, authPriv, authAddr := makeKey(t)
	pPub, pPriv, pAddr := makeKey(t)
	rPub, rPriv, rAddr := makeKey(t)
	require.NoError(t, s.Mint(pAddr, 10_000))
	require.NoError(t, s.Mint(rAddr, 10_000))

	// Authority registers a domain.
	domTx := signTx(t, authPriv, authPub, 0, types.TxRegisterDomain, types.MsgRegisterDomain{
		Authority: authAddr,
		ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000099",
		RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
	})
	domReceipt, err := s.SubmitTx(domTx)
	require.NoError(t, err)
	domain := domReceipt.NewID

	// Provider registers their service.
	svcTx := signTx(t, pPriv, pPub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: pAddr, Name: "imm-svc", Price: types.Coin{Denom: "aios", Amount: 100},
		VerificationDomainID: domain,
	})
	_, err = s.SubmitTx(svcTx)
	require.NoError(t, err)

	preProvider, err := s.Account(pAddr)
	require.NoError(t, err)

	// Request.
	reqTx := signTx(t, rPriv, rPub, 0, types.TxRequestInference, types.MsgRequestInference{
		Requester: rAddr, ServiceID: 1, InputHash: "input-hash",
		InputURI: "inline:test", InputText: "test",
		MaxPrice: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err = s.SubmitTx(reqTx)
	require.NoError(t, err)

	// Submit result → immediate finalize.
	resTx := signTx(t, pPriv, pPub, 1, types.TxSubmitResult, types.MsgSubmitResult{
		Provider: pAddr, RequestID: 1,
		Result: types.Result{
			OutputHash: "out", OutputURI: "inline:out", OutputText: "ok",
			Attestation: types.Attestation{
				Provider: pAddr, VerificationDomainID: domain,
				ModelSHA256: "0000000000000000000000000000000000000000000000000000000000000099",
				RuntimeID: "test-runtime", HardwareTag: "test-hw", Precision: "fp32",
				InputHash: "input-hash", OutputHash: "out",
			},
		},
	})
	receipt, err := s.SubmitTx(resTx)
	require.NoError(t, err)
	require.True(t, receipt.Finalized, "ChallengeWindowBlocks=0 should immediately finalize")

	// Provider should have: pre - 50 (bond) + 100 (escrow) + 50 (bond returned) = pre + 100
	post, err := s.Account(pAddr)
	require.NoError(t, err)
	require.Equal(t, preProvider.Balance+100, post.Balance,
		"immediate finalize must return the provider bond (was a bug pre-Phase 3.z)")
}
