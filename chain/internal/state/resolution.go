package state

import (
	"encoding/json"
	"fmt"

	"go.etcd.io/bbolt"

	"github.com/aios/aios/internal/types"
)

// executeSlash transitions a CHALLENGED request to SLASHED. Used by both the
// block producer (timeout auto-resolution) and MsgResolveChallenge (authority
// override).
//
// Effects:
//   - Escrow → requester (refund)
//   - Provider bond → challenger (slashing reward, Phase 3.x)
//   - Challenger bond → challenger (refund)
//   - Voucher bonds:
//       - provider-side (lost): bond → moduleTreasuryAddr (Phase 3.z step 3)
//       - challenger-side (won): bond → voucher (returned)
//   - Status SLASHED, emit RequestSlashed event.
func (s *State) executeSlash(btx *bbolt.Tx, r types.InferenceRequest, height int64) (types.Event, error) {
	svc, err := getService(btx, r.ServiceID)
	if err != nil {
		return types.Event{}, err
	}
	challenger := r.Challenges[0].Challenger
	challengerBond := r.Challenges[0].Bond

	if err := debit(btx, moduleEscrowAddr, r.Escrow.Amount); err != nil {
		return types.Event{}, err
	}
	if err := credit(btx, r.Requester, r.Escrow.Amount); err != nil {
		return types.Event{}, err
	}
	var providerBondSlashed types.Coin
	if r.ProviderBond != nil && r.ProviderBond.IsPositive() {
		if err := debit(btx, moduleEscrowAddr, r.ProviderBond.Amount); err != nil {
			return types.Event{}, err
		}
		if err := credit(btx, challenger, r.ProviderBond.Amount); err != nil {
			return types.Event{}, err
		}
		providerBondSlashed = *r.ProviderBond
	}
	var challengerBondReturned types.Coin
	if challengerBond.IsPositive() {
		if err := debit(btx, moduleEscrowAddr, challengerBond.Amount); err != nil {
			return types.Event{}, err
		}
		if err := credit(btx, challenger, challengerBond.Amount); err != nil {
			return types.Event{}, err
		}
		challengerBondReturned = challengerBond
	}

	// Phase 3.z step 3 (treasury sweep) + step 4 (correct challenger-voucher
	// resolution):
	//   - Provider-side vouchers lost. Forfeit their bonds to the treasury.
	//   - Challenger-side vouchers won. Return their bonds.
	var voucherBondsForfeitedToTreasury, challengerVoucherBondsReturned int
	for _, v := range r.Vouchers {
		if !v.Bond.IsPositive() {
			continue
		}
		if v.SupportsProvider {
			if err := debit(btx, moduleEscrowAddr, v.Bond.Amount); err != nil {
				return types.Event{}, fmt.Errorf("forfeit provider-side voucher bond to treasury: %w", err)
			}
			if err := credit(btx, ModuleTreasuryAddr, v.Bond.Amount); err != nil {
				return types.Event{}, err
			}
			voucherBondsForfeitedToTreasury++
		} else {
			if err := debit(btx, moduleEscrowAddr, v.Bond.Amount); err != nil {
				return types.Event{}, fmt.Errorf("returning challenger-side voucher bond: %w", err)
			}
			if err := credit(btx, v.Voucher, v.Bond.Amount); err != nil {
				return types.Event{}, err
			}
			challengerVoucherBondsReturned++
		}
	}

	r.Status = types.StatusSlashed
	r.FinalizedAtHeight = height
	if err := putRequest(btx, r); err != nil {
		return types.Event{}, err
	}
	payloadBz, _ := json.Marshal(types.RequestSlashedPayload{
		RequestID:                       r.ID,
		Provider:                        svc.Owner,
		Challenger:                      challenger,
		Refunded:                        r.Escrow,
		ProviderBondSlashed:             providerBondSlashed,
		ChallengerBondReturned:          challengerBondReturned,
		VoucherBondsForfeitedToTreasury: voucherBondsForfeitedToTreasury,
		ChallengerVoucherBondsReturned:  challengerVoucherBondsReturned,
	})
	return types.Event{
		Type: types.EventRequestSlashed, BlockHeight: height, Payload: payloadBz,
	}, nil
}

// executeDismiss transitions a CHALLENGED request to FINALIZED (with the
// dismissal flag). Used by both the block producer and MsgResolveChallenge.
//
// Effects:
//   - Escrow → provider (paid normally)
//   - Provider bond → provider (returned)
//   - Challenger bond → reward pool (forfeit)
//   - Provider-side vouchers: bond returned + reward share
//   - Challenger-side vouchers: bond forfeit into reward pool
//   - Status FINALIZED, emit RequestDismissed event.
func (s *State) executeDismiss(btx *bbolt.Tx, r types.InferenceRequest, height int64) (types.Event, error) {
	svc, err := getService(btx, r.ServiceID)
	if err != nil {
		return types.Event{}, err
	}
	challenger := r.Challenges[0].Challenger
	challengerBond := r.Challenges[0].Bond

	if err := debit(btx, moduleEscrowAddr, r.Escrow.Amount); err != nil {
		return types.Event{}, err
	}
	if err := credit(btx, svc.Owner, r.Escrow.Amount); err != nil {
		return types.Event{}, err
	}
	var providerBondReturned types.Coin
	if r.ProviderBond != nil && r.ProviderBond.IsPositive() {
		if err := debit(btx, moduleEscrowAddr, r.ProviderBond.Amount); err != nil {
			return types.Event{}, err
		}
		if err := credit(btx, svc.Owner, r.ProviderBond.Amount); err != nil {
			return types.Event{}, err
		}
		providerBondReturned = *r.ProviderBond
	}

	var providerVouchers []types.Voucher
	var lostVoucherBondPool uint64
	for _, v := range r.Vouchers {
		if v.SupportsProvider {
			providerVouchers = append(providerVouchers, v)
		} else if v.Bond.IsPositive() {
			lostVoucherBondPool += v.Bond.Amount
		}
	}
	rewardPool := uint64(0)
	if challengerBond.IsPositive() {
		rewardPool += challengerBond.Amount
	}
	rewardPool += lostVoucherBondPool

	for _, v := range providerVouchers {
		if v.Bond.IsPositive() {
			if err := debit(btx, moduleEscrowAddr, v.Bond.Amount); err != nil {
				return types.Event{}, err
			}
			if err := credit(btx, v.Voucher, v.Bond.Amount); err != nil {
				return types.Event{}, err
			}
		}
	}

	recipients := uint64(1 + len(providerVouchers))
	if rewardPool > 0 {
		share := rewardPool / recipients
		remainder := rewardPool - share*recipients
		if err := debit(btx, moduleEscrowAddr, rewardPool); err != nil {
			return types.Event{}, err
		}
		if err := credit(btx, svc.Owner, share+remainder); err != nil {
			return types.Event{}, err
		}
		for _, v := range providerVouchers {
			if err := credit(btx, v.Voucher, share); err != nil {
				return types.Event{}, err
			}
		}
	}

	r.Status = types.StatusFinalized
	r.FinalizedAtHeight = height
	paid := r.Escrow
	r.Paid = &paid
	if err := putRequest(btx, r); err != nil {
		return types.Event{}, err
	}
	payloadBz, _ := json.Marshal(types.RequestDismissedPayload{
		RequestID:             r.ID,
		Provider:              svc.Owner,
		Challenger:            challenger,
		Paid:                  paid,
		ProviderBondReturned:  providerBondReturned,
		ChallengerBondForfeit: challengerBond,
		VoucherCount:          len(providerVouchers),
	})
	return types.Event{
		Type: types.EventRequestDismissed, BlockHeight: height, Payload: payloadBz,
	}, nil
}

// executeVoidDueToDomain unwinds every bond + escrow on an open request whose
// service's verification domain has been deactivated. Phase 3.z.
//
// Unlike executeSlash / executeDismiss, this is not a dispute outcome — it's a
// "domain failure" event triggered by the authority. Nobody wins, nobody loses:
// every locked party gets their stake back and the request terminates in
// REFUNDED status. Emits RequestVoided.
//
// Caller is responsible for ensuring `r.Status` ∈ {PENDING, SUBMITTED, CHALLENGED}.
// Terminal statuses (FINALIZED, SLASHED, REFUNDED) are not the caller's
// business — they're already done.
func (s *State) executeVoidDueToDomain(btx *bbolt.Tx, r types.InferenceRequest, height int64) (types.Event, error) {
	svc, err := getService(btx, r.ServiceID)
	if err != nil {
		return types.Event{}, err
	}

	// Always refund the requester's escrow.
	if err := debit(btx, moduleEscrowAddr, r.Escrow.Amount); err != nil {
		return types.Event{}, fmt.Errorf("refunding escrow on void: %w", err)
	}
	if err := credit(btx, r.Requester, r.Escrow.Amount); err != nil {
		return types.Event{}, err
	}

	// Provider bond (locked at MsgSubmitResult).
	var providerBondReturned types.Coin
	var provider string
	if r.Status == types.StatusSubmitted || r.Status == types.StatusChallenged {
		provider = svc.Owner
		if r.ProviderBond != nil && r.ProviderBond.IsPositive() {
			if err := debit(btx, moduleEscrowAddr, r.ProviderBond.Amount); err != nil {
				return types.Event{}, fmt.Errorf("returning provider bond on void: %w", err)
			}
			if err := credit(btx, svc.Owner, r.ProviderBond.Amount); err != nil {
				return types.Event{}, err
			}
			providerBondReturned = *r.ProviderBond
		}
	}

	// Challenger bond (locked at MsgChallenge).
	var challenger string
	var challengerBondReturned types.Coin
	if r.Status == types.StatusChallenged && len(r.Challenges) > 0 {
		challenger = r.Challenges[0].Challenger
		cBond := r.Challenges[0].Bond
		if cBond.IsPositive() {
			if err := debit(btx, moduleEscrowAddr, cBond.Amount); err != nil {
				return types.Event{}, fmt.Errorf("returning challenger bond on void: %w", err)
			}
			if err := credit(btx, challenger, cBond.Amount); err != nil {
				return types.Event{}, err
			}
			challengerBondReturned = cBond
		}
	}

	// Voucher bonds (one per voucher; refund regardless of side since the
	// dispute itself is void).
	voucherBondsReturned := 0
	for _, v := range r.Vouchers {
		if !v.Bond.IsPositive() {
			continue
		}
		if err := debit(btx, moduleEscrowAddr, v.Bond.Amount); err != nil {
			return types.Event{}, fmt.Errorf("returning voucher bond on void: %w", err)
		}
		if err := credit(btx, v.Voucher, v.Bond.Amount); err != nil {
			return types.Event{}, err
		}
		voucherBondsReturned++
	}

	priorStatus := r.Status
	r.Status = types.StatusRefunded
	r.FinalizedAtHeight = height
	if err := putRequest(btx, r); err != nil {
		return types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.RequestVoidedPayload{
		RequestID:              r.ID,
		ServiceID:              r.ServiceID,
		DomainID:               svc.VerificationDomainID,
		PriorStatus:            priorStatus,
		Requester:              r.Requester,
		EscrowRefunded:         r.Escrow,
		Provider:               provider,
		ProviderBondReturned:   providerBondReturned,
		Challenger:             challenger,
		ChallengerBondReturned: challengerBondReturned,
		VoucherBondsReturned:   voucherBondsReturned,
	})
	return types.Event{
		Type: types.EventRequestVoided, BlockHeight: height, Payload: payloadBz,
	}, nil
}
