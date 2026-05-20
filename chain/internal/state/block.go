package state

import (
	"context"
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"

	"github.com/aios/aios/internal/types"
)

// RunBlockProducer is the chain's "consensus loop" for the custom Go chain:
// once per interval it advances the height, sweeps expired requests, processes
// challenge-window expiry, and emits BlockCommitted.
//
// Phase 1+ replacement (Cosmos SDK + CometBFT) would move this into
// BeginBlock / EndBlock hooks.
func RunBlockProducer(ctx context.Context, s *State, interval time.Duration, logger *zap.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("block producer stopping")
			return
		case <-t.C:
			if err := s.commitBlock(); err != nil {
				logger.Error("commit block", zap.Error(err))
			}
		}
	}
}

// Tick advances the chain by one block. It is the same operation
// `RunBlockProducer` performs on its ticker, exposed for tests and for any
// future driver (e.g. a CometBFT BeginBlock/EndBlock adapter) that wants
// fine-grained control over block timing.
func (s *State) Tick() error {
	return s.commitBlock()
}

// commitBlock advances height by 1, processes time-driven transitions, emits events.
//
// Transitions handled here:
//   PENDING   + deadline elapsed                 → REFUNDED (escrow to requester)
//   SUBMITTED + challenge window expired         → FINALIZED (escrow to provider)
//   CHALLENGED + resolution window expired       → SLASHED (Phase 3 simple: trust challenger; refund requester)
func (s *State) commitBlock() error {
	var height int64
	var emitted []types.Event

	const challengeResolutionWindowBlocks = 20 // ~20s at 1s block time; placeholder for Phase 3.x dispute game

	err := s.db.Update(func(btx *bbolt.Tx) error {
		meta := btx.Bucket(bktMeta)
		height = int64(getUint64(meta, keyHeight)) + 1
		if err := putUint64(meta, keyHeight, uint64(height)); err != nil {
			return err
		}

		params, err := s.paramsLocked(btx)
		if err != nil {
			return err
		}

		var toRefund, toFinalize, toSlash, toDismiss []types.InferenceRequest
		c := btx.Bucket(bktRequests).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var r types.InferenceRequest
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			switch r.Status {
			case types.StatusPending:
				if height > r.DeadlineHeight {
					toRefund = append(toRefund, r)
				}
			case types.StatusSubmitted:
				// Honest finalization: challenge window expired without dispute.
				if height > r.SubmittedAtHeight+params.ChallengeWindowBlocks {
					toFinalize = append(toFinalize, r)
				}
			case types.StatusChallenged:
				// Phase 3.y resolution, hardened in 3.z. After the resolution
				// window expires:
				//   - count vouchers backing provider vs challenger
				//   - provider needs at least one supporter AND a net excess
				//     of the effective margin over the challenger side
				//   - effective margin is the per-domain override (3.z step 3)
				//     if the request's service is bound to a domain that sets
				//     one, else the global Params.VoucherMargin.
				if len(r.Challenges) == 0 ||
					height <= r.Challenges[0].PostedAt+challengeResolutionWindowBlocks {
					continue
				}
				var providerVouchers, challengerVouchers int
				for _, v := range r.Vouchers {
					if v.SupportsProvider {
						providerVouchers++
					} else {
						challengerVouchers++
					}
				}
				margin := params.VoucherMargin
				svc, err := getService(btx, r.ServiceID)
				if err == nil && svc.VerificationDomainID != 0 {
					if dom, derr := getDomain(btx, svc.VerificationDomainID); derr == nil && dom.VoucherMargin > 0 {
						margin = dom.VoucherMargin
					}
				}
				net := int64(providerVouchers - challengerVouchers)
				if providerVouchers > 0 && net >= margin {
					toDismiss = append(toDismiss, r)
				} else {
					toSlash = append(toSlash, r)
				}
			}
		}

		// REFUNDED — deadline expired, no submission
		for _, r := range toRefund {
			if err := debit(btx, moduleEscrowAddr, r.Escrow.Amount); err != nil {
				return err
			}
			if err := credit(btx, r.Requester, r.Escrow.Amount); err != nil {
				return err
			}
			r.Status = types.StatusRefunded
			r.FinalizedAtHeight = height
			if err := putRequest(btx, r); err != nil {
				return err
			}
			payloadBz, _ := json.Marshal(types.RequestRefundedPayload{
				RequestID: r.ID, Requester: r.Requester, Refunded: r.Escrow,
			})
			emitted = append(emitted, types.Event{
				Type: types.EventRequestRefunded, BlockHeight: height, Payload: payloadBz,
			})
		}

		// FINALIZED — challenge window expired without dispute. Phase 3.x
		// releases provider bond alongside the escrow payout.
		for _, r := range toFinalize {
			svc, err := getService(btx, r.ServiceID)
			if err != nil {
				return err
			}
			// Escrow → provider
			if err := debit(btx, moduleEscrowAddr, r.Escrow.Amount); err != nil {
				return err
			}
			if err := credit(btx, svc.Owner, r.Escrow.Amount); err != nil {
				return err
			}
			// Provider bond → provider (Phase 3.x)
			var bondReturned types.Coin
			if r.ProviderBond != nil && r.ProviderBond.IsPositive() {
				if err := debit(btx, moduleEscrowAddr, r.ProviderBond.Amount); err != nil {
					return err
				}
				if err := credit(btx, svc.Owner, r.ProviderBond.Amount); err != nil {
					return err
				}
				bondReturned = *r.ProviderBond
			}
			r.Status = types.StatusFinalized
			r.FinalizedAtHeight = height
			paid := r.Escrow
			r.Paid = &paid
			if err := putRequest(btx, r); err != nil {
				return err
			}
			payloadBz, _ := json.Marshal(types.RequestFinalizedPayload{
				RequestID:            r.ID,
				Provider:             svc.Owner,
				Paid:                 paid,
				ProviderBondReturned: bondReturned,
			})
			emitted = append(emitted, types.Event{
				Type: types.EventRequestFinalized, BlockHeight: height, Payload: payloadBz,
			})
		}

		// SLASHED — challenge resolution timed out (no provider-side voucher).
		// Shared with MsgResolveChallenge's "slash" decision.
		for _, r := range toSlash {
			ev, err := s.executeSlash(btx, r, height)
			if err != nil {
				return err
			}
			emitted = append(emitted, ev)
		}

		// DISMISSED — Phase 3.y. Provider-side voucher won. Shared with
		// MsgResolveChallenge's "dismiss" decision.
		for _, r := range toDismiss {
			ev, err := s.executeDismiss(btx, r, height)
			if err != nil {
				return err
			}
			emitted = append(emitted, ev)
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, ev := range emitted {
		s.emit(ev)
	}

	bcPayload, _ := json.Marshal(types.BlockCommittedPayload{
		Height: height,
		Time:   time.Now().UTC().Format(time.RFC3339Nano),
	})
	s.emit(types.Event{
		Type: types.EventBlockCommitted, BlockHeight: height, Payload: bcPayload,
	})
	return nil
}
