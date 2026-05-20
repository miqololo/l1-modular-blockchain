package state

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"go.etcd.io/bbolt"

	"github.com/aios/aios/internal/types"
)

// SubmitTx validates a SignedTx, applies it inside a single bbolt transaction,
// emits the corresponding event, and returns the resulting object id (service
// id, request id) where applicable.
//
// The validation order matches what a real chain does: signature, nonce,
// payload validation, then state mutation. Any failure rolls back the whole tx.
func (s *State) SubmitTx(tx types.SignedTx) (TxReceipt, error) {
	if err := tx.ValidateBasic(); err != nil {
		return TxReceipt{}, fmt.Errorf("validate: %w", err)
	}

	pub, err := hex.DecodeString(tx.PubKeyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return TxReceipt{}, fmt.Errorf("%w: pubkey", types.ErrInvalidSignature)
	}
	sig, err := hex.DecodeString(tx.SignatureHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return TxReceipt{}, fmt.Errorf("%w: signature", types.ErrInvalidSignature)
	}
	canonical, err := tx.CanonicalBytes()
	if err != nil {
		return TxReceipt{}, fmt.Errorf("canonical: %w", err)
	}
	if !ed25519.Verify(pub, canonical, sig) {
		return TxReceipt{}, types.ErrInvalidSignature
	}

	signerAddr := types.AddressFromPubKey(pub)

	var receipt TxReceipt
	err = s.db.Update(func(btx *bbolt.Tx) error {
		// Nonce check: must equal current nonce for this account.
		expectedNonce := nextNonce(btx, signerAddr)
		if tx.Nonce != expectedNonce {
			return fmt.Errorf("%w: expected %d got %d", types.ErrInvalidNonce, expectedNonce, tx.Nonce)
		}

		h := s.heightLocked(btx)

		switch tx.Type {
		case types.TxRegisterService:
			id, ev, err := s.applyRegisterService(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type, NewID: id}
			receipt.pendingEvent = ev
		case types.TxRequestInference:
			id, ev, err := s.applyRequestInference(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type, NewID: id}
			receipt.pendingEvent = ev
		case types.TxSubmitResult:
			finalized, events, err := s.applySubmitResult(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type, Finalized: finalized}
			receipt.pendingEvents = events
		case types.TxTransfer:
			ev, err := s.applyTransfer(btx, signerAddr, tx.Payload)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvent = ev
		case types.TxRegisterDomain:
			id, ev, err := s.applyRegisterDomain(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type, NewID: id}
			receipt.pendingEvent = ev
		case types.TxChallenge:
			ev, err := s.applyChallenge(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvent = ev
		case types.TxVouch:
			ev, err := s.applyVouch(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvent = ev
		case types.TxUpdateService:
			ev, err := s.applyUpdateService(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvent = ev
		case types.TxDeactivateService:
			ev, err := s.applyDeactivateService(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvent = ev
		case types.TxDeactivateDomain:
			events, err := s.applyDeactivateDomain(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvents = events
		case types.TxResolveChallenge:
			events, err := s.applyResolveChallenge(btx, signerAddr, tx.Payload, h)
			if err != nil {
				return err
			}
			receipt = TxReceipt{Type: tx.Type}
			receipt.pendingEvents = events
		default:
			return fmt.Errorf("unknown tx type %q", tx.Type)
		}

		if err := incNonce(btx, signerAddr); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return TxReceipt{}, err
	}

	// Emit events outside the db tx so subscribers can't deadlock the writer.
	if receipt.pendingEvent.Type != "" {
		s.emit(receipt.pendingEvent)
	}
	for _, ev := range receipt.pendingEvents {
		s.emit(ev)
	}
	return receipt, nil
}

// TxReceipt is returned to the caller of SubmitTx.
type TxReceipt struct {
	Type      types.TxType
	NewID     uint64
	Finalized bool

	pendingEvent  types.Event
	pendingEvents []types.Event
}

func (s *State) heightLocked(btx *bbolt.Tx) int64 {
	return int64(getUint64(btx.Bucket(bktMeta), keyHeight))
}

// ─── per-type appliers ──────────────────────────────────────────────────────

func (s *State) applyRegisterService(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (uint64, types.Event, error) {
	var msg types.MsgRegisterService
	if err := json.Unmarshal(payload, &msg); err != nil {
		return 0, types.Event{}, fmt.Errorf("payload: %w", err)
	}
	if msg.Owner != signer {
		return 0, types.Event{}, fmt.Errorf("owner != signer (%s != %s)", msg.Owner, signer)
	}
	if msg.Name == "" {
		return 0, types.Event{}, types.ErrEmptyName
	}
	if !msg.Price.IsPositive() {
		return 0, types.Event{}, types.ErrZeroPrice
	}
	if existing := btx.Bucket(bktSvcByName).Get([]byte(msg.Name)); existing != nil {
		return 0, types.Event{}, types.ErrDuplicateName
	}

	// Phase 1: verify the referenced domain exists and is active.
	if msg.VerificationDomainID != 0 {
		d, err := getDomain(btx, msg.VerificationDomainID)
		if err != nil {
			return 0, types.Event{}, err
		}
		if !d.Active {
			return 0, types.Event{}, types.ErrDomainInactive
		}
	}

	// Phase 3.z step 2: lock the service-registration bond. This is the
	// sybil-cost gate: a would-be sybil voucher must commit ServiceRegistrationBond
	// per identity and either wait MinServiceLifetimeBlocks for a refund or
	// forfeit on early deactivation.
	params, err := s.paramsLocked(btx)
	if err != nil {
		return 0, types.Event{}, err
	}
	bond := params.ServiceRegistrationBond
	if bond.IsPositive() {
		if err := debit(btx, signer, bond.Amount); err != nil {
			return 0, types.Event{}, fmt.Errorf("locking service registration bond: %w", err)
		}
		if err := credit(btx, moduleEscrowAddr, bond.Amount); err != nil {
			return 0, types.Event{}, err
		}
	}

	id := getUint64(btx.Bucket(bktMeta), keyNextSvcID)
	if err := putUint64(btx.Bucket(bktMeta), keyNextSvcID, id+1); err != nil {
		return 0, types.Event{}, err
	}

	svc := types.Service{
		ID: id, Owner: msg.Owner, Name: msg.Name, Description: msg.Description,
		Price: msg.Price, Active: true, CreatedAtHeight: height,
		VerificationDomainID: msg.VerificationDomainID,
		RegistrationBond:     bond,
	}
	if err := putService(btx, svc); err != nil {
		return 0, types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.ServiceRegisteredPayload{
		ServiceID: id, Owner: msg.Owner, Name: msg.Name, Description: msg.Description, Price: msg.Price,
	})
	ev := types.Event{
		Type:        types.EventServiceRegistered,
		BlockHeight: height,
		Payload:     payloadBz,
	}
	return id, ev, nil
}

func (s *State) applyRequestInference(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (uint64, types.Event, error) {
	var msg types.MsgRequestInference
	if err := json.Unmarshal(payload, &msg); err != nil {
		return 0, types.Event{}, fmt.Errorf("payload: %w", err)
	}
	if msg.Requester != signer {
		return 0, types.Event{}, fmt.Errorf("requester != signer")
	}
	svc, err := getService(btx, msg.ServiceID)
	if err != nil {
		return 0, types.Event{}, err
	}
	if !svc.Active {
		return 0, types.Event{}, types.ErrServiceInactive
	}
	if msg.MaxPrice.Denom != svc.Price.Denom || msg.MaxPrice.Amount < svc.Price.Amount {
		return 0, types.Event{}, types.ErrInsufficientPrice
	}

	params, err := s.paramsLocked(btx)
	if err != nil {
		return 0, types.Event{}, err
	}
	deadline := msg.DeadlineHeight
	if deadline == 0 {
		deadline = height + params.MaxRequestDeadlineBlocks
	}
	if deadline <= height {
		return 0, types.Event{}, types.ErrInvalidDeadline
	}

	// Escrow the *service price* (not max_price) from the requester.
	if err := debit(btx, signer, svc.Price.Amount); err != nil {
		return 0, types.Event{}, err
	}
	if err := credit(btx, moduleEscrowAddr, svc.Price.Amount); err != nil {
		return 0, types.Event{}, err
	}

	id := getUint64(btx.Bucket(bktMeta), keyNextReqID)
	if err := putUint64(btx.Bucket(bktMeta), keyNextReqID, id+1); err != nil {
		return 0, types.Event{}, err
	}

	req := types.InferenceRequest{
		ID: id, ServiceID: msg.ServiceID, Requester: signer,
		InputHash: msg.InputHash, InputURI: msg.InputURI, InputText: msg.InputText,
		Escrow: svc.Price, DeadlineHeight: deadline,
		Status: types.StatusPending, CreatedAtHeight: height,
	}
	if err := putRequest(btx, req); err != nil {
		return 0, types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.InferenceRequestedPayload{
		RequestID: id, ServiceID: req.ServiceID, Requester: req.Requester,
		InputHash: req.InputHash, InputURI: req.InputURI, InputText: req.InputText,
		Escrow: req.Escrow, DeadlineHeight: req.DeadlineHeight,
	})
	ev := types.Event{
		Type:        types.EventInferenceRequested,
		BlockHeight: height,
		Payload:     payloadBz,
	}
	return id, ev, nil
}

func (s *State) applySubmitResult(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (bool, []types.Event, error) {
	var msg types.MsgSubmitResult
	if err := json.Unmarshal(payload, &msg); err != nil {
		return false, nil, fmt.Errorf("payload: %w", err)
	}
	if msg.Provider != signer {
		return false, nil, fmt.Errorf("provider != signer")
	}
	req, err := getRequest(btx, msg.RequestID)
	if err != nil {
		return false, nil, err
	}
	if req.Status != types.StatusPending {
		return false, nil, types.ErrRequestNotPending
	}
	svc, err := getService(btx, req.ServiceID)
	if err != nil {
		return false, nil, err
	}
	if signer != svc.Owner {
		return false, nil, types.ErrWrongProvider
	}
	if msg.Result.Attestation.InputHash != req.InputHash {
		return false, nil, types.ErrInputHashMismatch
	}
	if height > req.DeadlineHeight {
		return false, nil, fmt.Errorf("%w: height %d > deadline %d", types.ErrInvalidDeadline, height, req.DeadlineHeight)
	}

	// Phase 1: enforce attestation matches the service's verification domain.
	att := msg.Result.Attestation
	if svc.VerificationDomainID != 0 {
		dom, err := getDomain(btx, svc.VerificationDomainID)
		if err != nil {
			return false, nil, fmt.Errorf("loading svc domain %d: %w", svc.VerificationDomainID, err)
		}
		if att.VerificationDomainID != dom.ID ||
			att.ModelSHA256 != dom.ModelSHA256 ||
			att.RuntimeID != dom.RuntimeID ||
			att.HardwareTag != dom.HardwareTag ||
			att.Precision != dom.Precision {
			return false, nil, fmt.Errorf("%w: domain %d expects (%s, %s, %s, %s), got (%s, %s, %s, %s)",
				types.ErrAttestationDomainMismatch, dom.ID,
				dom.ModelSHA256, dom.RuntimeID, dom.HardwareTag, dom.Precision,
				att.ModelSHA256, att.RuntimeID, att.HardwareTag, att.Precision)
		}
		// Phase 1: tokenizer pinning. Domains registered before Phase 1 have
		// no TokenizerID (empty); they don't enforce the check (back-compat).
		// Domains registered with a non-empty TokenizerID require an exact
		// match in the attestation.
		if dom.TokenizerID != "" && att.TokenizerID != dom.TokenizerID {
			return false, nil, fmt.Errorf("%w: domain %d tokenizer %q != attestation tokenizer %q",
				types.ErrAttestationDomainMismatch, dom.ID, dom.TokenizerID, att.TokenizerID)
		}
	}

	params, err := s.paramsLocked(btx)
	if err != nil {
		return false, nil, err
	}

	// Phase 3.x: lock the provider's bond. Released back on FINALIZED;
	// transferred to challenger on SLASHED.
	if params.ProviderBondAmount.IsPositive() {
		if err := debit(btx, signer, params.ProviderBondAmount.Amount); err != nil {
			return false, nil, fmt.Errorf("locking provider bond %s: %w", params.ProviderBondAmount, err)
		}
		if err := credit(btx, moduleEscrowAddr, params.ProviderBondAmount.Amount); err != nil {
			return false, nil, err
		}
		bond := params.ProviderBondAmount
		req.ProviderBond = &bond
	}

	req.Result = &msg.Result
	req.Status = types.StatusSubmitted
	req.SubmittedAtHeight = height

	var events []types.Event
	submittedPayload, _ := json.Marshal(types.ResultSubmittedPayload{
		RequestID: req.ID, Provider: signer, OutputHash: msg.Result.OutputHash, OutputURI: msg.Result.OutputURI,
	})
	events = append(events, types.Event{
		Type: types.EventResultSubmitted, BlockHeight: height, Payload: submittedPayload,
	})

	finalized := false
	if params.ChallengeWindowBlocks == 0 {
		// Backwards-compatible immediate finalize (Phase 0.5 / Phase 1 if
		// challenge_window_blocks=0). Phase 3 default sets it >0, so this
		// branch is only used when the operator explicitly disables challenges.
		if err := debit(btx, moduleEscrowAddr, req.Escrow.Amount); err != nil {
			return false, nil, err
		}
		if err := credit(btx, signer, req.Escrow.Amount); err != nil {
			return false, nil, err
		}
		// Phase 3.z: also return the provider bond on immediate finalize. The
		// previous implementation locked the bond just above but only released
		// it on the deferred-finalize path in commitBlock, leaving the bond
		// stuck in escrow when ChallengeWindowBlocks=0.
		var bondReturned types.Coin
		if req.ProviderBond != nil && req.ProviderBond.IsPositive() {
			if err := debit(btx, moduleEscrowAddr, req.ProviderBond.Amount); err != nil {
				return false, nil, err
			}
			if err := credit(btx, signer, req.ProviderBond.Amount); err != nil {
				return false, nil, err
			}
			bondReturned = *req.ProviderBond
		}
		req.Status = types.StatusFinalized
		req.FinalizedAtHeight = height
		paid := req.Escrow
		req.Paid = &paid
		finalized = true

		finalizedPayload, _ := json.Marshal(types.RequestFinalizedPayload{
			RequestID: req.ID, Provider: signer, Paid: paid, ProviderBondReturned: bondReturned,
		})
		events = append(events, types.Event{
			Type: types.EventRequestFinalized, BlockHeight: height, Payload: finalizedPayload,
		})
	}
	// Phase 3: when ChallengeWindowBlocks > 0, finalization is deferred. The
	// block producer (commitBlock) sweeps SUBMITTED requests after the window
	// expires and either finalizes (no challenge) or leaves CHALLENGED ones
	// for resolution.

	if err := putRequest(btx, req); err != nil {
		return false, nil, err
	}
	return finalized, events, nil
}

// applyChallenge — Phase 3.
//
// A challenger asserts the provider's submitted output is wrong by attaching
// their own attestation with a different output_hash. The chain validates:
//   1. The request exists and is in SUBMITTED status (challenge window open).
//   2. The challenger's attestation references the same input_hash.
//   3. The challenger's output_hash differs from the provider's (otherwise no
//      real dispute).
//   4. The challenger's attestation tuple matches the service's verification
//      domain (when set).
//
// Phase 3 simple: a valid challenge transitions the request to CHALLENGED. The
// block producer then auto-resolves in the challenger's favor after a short
// window (in this iteration the chain trusts the challenger because the only
// expected challenger is the bundled determinism-harness, which re-runs
// honestly). Phase 3.x replaces auto-resolution with a real dispute game.
func (s *State) applyChallenge(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (types.Event, error) {
	var msg types.MsgChallenge
	if err := json.Unmarshal(payload, &msg); err != nil {
		return types.Event{}, fmt.Errorf("payload: %w", err)
	}
	if msg.Challenger != signer {
		return types.Event{}, fmt.Errorf("challenger != signer")
	}

	req, err := getRequest(btx, msg.RequestID)
	if err != nil {
		return types.Event{}, err
	}
	if req.Status != types.StatusSubmitted {
		return types.Event{}, fmt.Errorf("%w: status=%s", types.ErrChallengeNotApplicable, req.Status)
	}

	params, err := s.paramsLocked(btx)
	if err != nil {
		return types.Event{}, err
	}
	if height > req.SubmittedAtHeight+params.ChallengeWindowBlocks {
		return types.Event{}, fmt.Errorf("%w: now %d > submitted %d + %d",
			types.ErrChallengeWindowClosed, height, req.SubmittedAtHeight, params.ChallengeWindowBlocks)
	}

	if msg.Attestation.InputHash != req.InputHash {
		return types.Event{}, types.ErrInputHashMismatch
	}
	if req.Result == nil {
		// Should be impossible if status is SUBMITTED.
		return types.Event{}, fmt.Errorf("invariant: SUBMITTED request without a result")
	}
	if msg.Attestation.OutputHash == req.Result.OutputHash {
		return types.Event{}, types.ErrChallengeOutputMatches
	}

	// Domain match: if the service has a domain, the challenger's tuple must
	// match it. Otherwise the challenge is comparing apples to oranges.
	svc, err := getService(btx, req.ServiceID)
	if err != nil {
		return types.Event{}, err
	}
	if svc.VerificationDomainID != 0 {
		dom, err := getDomain(btx, svc.VerificationDomainID)
		if err != nil {
			return types.Event{}, fmt.Errorf("load domain: %w", err)
		}
		if msg.Attestation.VerificationDomainID != dom.ID ||
			msg.Attestation.ModelSHA256 != dom.ModelSHA256 ||
			msg.Attestation.RuntimeID != dom.RuntimeID ||
			msg.Attestation.HardwareTag != dom.HardwareTag ||
			msg.Attestation.Precision != dom.Precision {
			return types.Event{}, fmt.Errorf("%w: challenger tuple != service domain",
				types.ErrAttestationDomainMismatch)
		}
		// Phase 1: tokenizer pinning (same back-compat rule as submit).
		if dom.TokenizerID != "" && msg.Attestation.TokenizerID != dom.TokenizerID {
			return types.Event{}, fmt.Errorf("%w: challenger tokenizer %q != domain tokenizer %q",
				types.ErrAttestationDomainMismatch, msg.Attestation.TokenizerID, dom.TokenizerID)
		}
	}

	// Phase 3.x: lock challenger's bond. Returned on successful SLASH.
	// Phase 3.y will introduce forfeit on dismissed challenges.
	if params.ChallengerBondAmount.IsPositive() {
		if err := debit(btx, signer, params.ChallengerBondAmount.Amount); err != nil {
			return types.Event{}, fmt.Errorf("locking challenger bond %s: %w", params.ChallengerBondAmount, err)
		}
		if err := credit(btx, moduleEscrowAddr, params.ChallengerBondAmount.Amount); err != nil {
			return types.Event{}, err
		}
	}

	req.Challenges = append(req.Challenges, types.Challenge{
		Challenger:  signer,
		PostedAt:    height,
		Attestation: msg.Attestation,
		Bond:        params.ChallengerBondAmount,
	})
	req.Status = types.StatusChallenged
	if err := putRequest(btx, req); err != nil {
		return types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.ChallengedPayload{
		RequestID:            req.ID,
		Challenger:           signer,
		ChallengerOutputHash: msg.Attestation.OutputHash,
		ProviderOutputHash:   req.Result.OutputHash,
		ChallengerBond:       params.ChallengerBondAmount,
	})
	return types.Event{
		Type:        types.EventChallenged,
		BlockHeight: height,
		Payload:     payloadBz,
	}, nil
}

// applyUpdateService — Phase 2 catch-up.
//
// Service owner updates `description` and `price`. Cannot change:
//   - name (unique index; renaming would conflict)
//   - owner (would defeat the marketplace's identity binding)
//   - verification_domain_id (would let bad providers re-domain after
//     accumulating reputation; immutable for security)
//
// Service must be active; updating a deactivated service is rejected.
func (s *State) applyUpdateService(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (types.Event, error) {
	var msg types.MsgUpdateService
	if err := json.Unmarshal(payload, &msg); err != nil {
		return types.Event{}, fmt.Errorf("payload: %w", err)
	}
	if msg.Owner != signer {
		return types.Event{}, fmt.Errorf("owner field != signer")
	}
	svc, err := getService(btx, msg.ServiceID)
	if err != nil {
		return types.Event{}, err
	}
	if svc.Owner != signer {
		return types.Event{}, types.ErrNotOwner
	}
	if !svc.Active {
		return types.Event{}, types.ErrServiceInactive
	}
	if !msg.Price.IsValid() || !msg.Price.IsPositive() {
		return types.Event{}, types.ErrZeroPrice
	}
	params, err := s.paramsLocked(btx)
	if err != nil {
		return types.Event{}, err
	}
	if msg.Price.IsLT(params.MinServicePrice) {
		return types.Event{}, fmt.Errorf("%w: %s below min %s", types.ErrZeroPrice, msg.Price, params.MinServicePrice)
	}

	svc.Description = msg.Description
	svc.Price = msg.Price
	if err := putService(btx, svc); err != nil {
		return types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.ServiceUpdatedPayload{
		ServiceID:   svc.ID,
		Owner:       svc.Owner,
		Description: svc.Description,
		Price:       svc.Price,
	})
	return types.Event{
		Type: types.EventServiceUpdated, BlockHeight: height, Payload: payloadBz,
	}, nil
}

// applyDeactivateService — Phase 2 catch-up.
//
// Service owner takes the service offline. New requests against it are
// rejected (applyRequestInference checks svc.Active). Pending requests still
// finalize normally — they were created when the service was active.
//
// Phase 4+ may add MsgReactivateService.
func (s *State) applyDeactivateService(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (types.Event, error) {
	var msg types.MsgDeactivateService
	if err := json.Unmarshal(payload, &msg); err != nil {
		return types.Event{}, fmt.Errorf("payload: %w", err)
	}
	if msg.Owner != signer {
		return types.Event{}, fmt.Errorf("owner field != signer")
	}
	svc, err := getService(btx, msg.ServiceID)
	if err != nil {
		return types.Event{}, err
	}
	if svc.Owner != signer {
		return types.Event{}, types.ErrNotOwner
	}
	if !svc.Active {
		return types.Event{}, types.ErrServiceAlreadyInactive
	}

	// Phase 3.z step 2: refund the registration bond only if the service has
	// lived past MinServiceLifetimeBlocks. Drive-by sybil registrations
	// (register → vouch → deactivate) hit this branch and forfeit the bond.
	params, err := s.paramsLocked(btx)
	if err != nil {
		return types.Event{}, err
	}
	var bondRefunded types.Coin
	if svc.RegistrationBond.IsPositive() {
		eligibleForRefund := height-svc.CreatedAtHeight >= params.MinServiceLifetimeBlocks
		if eligibleForRefund {
			if err := debit(btx, moduleEscrowAddr, svc.RegistrationBond.Amount); err != nil {
				return types.Event{}, fmt.Errorf("releasing registration bond: %w", err)
			}
			if err := credit(btx, svc.Owner, svc.RegistrationBond.Amount); err != nil {
				return types.Event{}, err
			}
			bondRefunded = svc.RegistrationBond
		} else {
			// Phase 3.z step 3: forfeit-bond treasury sweep. Move the
			// forfeited bond from module escrow to the treasury so module
			// escrow only ever holds bonds in active disputes.
			if err := debit(btx, moduleEscrowAddr, svc.RegistrationBond.Amount); err != nil {
				return types.Event{}, fmt.Errorf("sweeping forfeit bond to treasury: %w", err)
			}
			if err := credit(btx, ModuleTreasuryAddr, svc.RegistrationBond.Amount); err != nil {
				return types.Event{}, err
			}
		}
	}

	svc.Active = false
	svc.RegistrationBond = types.Coin{} // zero out so it can't be refunded twice
	if err := putService(btx, svc); err != nil {
		return types.Event{}, err
	}
	payloadBz, _ := json.Marshal(types.ServiceDeactivatedPayload{
		ServiceID: svc.ID, Owner: svc.Owner, BondRefunded: bondRefunded,
	})
	return types.Event{
		Type: types.EventServiceDeactivated, BlockHeight: height, Payload: payloadBz,
	}, nil
}

// applyDeactivateDomain — Phase 2 catch-up.
//
// Authority retires a verification domain. Used when a runtime/model/hw tuple
// is found to be non-deterministic in practice (the harness disqualifies it).
// Services using the domain can no longer accept new requests; pending
// requests still resolve. Phase 4+ adds governance over this.
func (s *State) applyDeactivateDomain(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) ([]types.Event, error) {
	var msg types.MsgDeactivateDomain
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}
	auth := string(btx.Bucket(bktMeta).Get(keyAuthority))
	if auth == "" || signer != auth {
		return nil, fmt.Errorf("%w: signer %s != authority %s", types.ErrUnauthorized, signer, auth)
	}
	if msg.Authority != signer {
		return nil, fmt.Errorf("authority field != signer")
	}
	d, err := getDomain(btx, msg.DomainID)
	if err != nil {
		return nil, err
	}
	if !d.Active {
		return nil, types.ErrDomainInactive
	}
	d.Active = false
	if err := putDomain(btx, d); err != nil {
		return nil, err
	}

	// Phase 3.z: cascade. Find every service bound to this domain; for each
	// still-active one, auto-deactivate AND refund its registration bond
	// (regardless of MinServiceLifetimeBlocks — the chain is killing the
	// service, not the operator). Then for every open request on those
	// services, void via executeVoidDueToDomain.
	var (
		affectedServiceIDs []uint64
		serviceEvents      []types.Event
		voidEvents         []types.Event
	)

	// Pass 1: collect affected service ids (collect first, mutate after, to
	// avoid invalidating the cursor).
	svcCur := btx.Bucket(bktServices).Cursor()
	for k, v := svcCur.First(); k != nil; k, v = svcCur.Next() {
		var svc types.Service
		if err := json.Unmarshal(v, &svc); err != nil {
			return nil, err
		}
		if svc.VerificationDomainID == d.ID {
			affectedServiceIDs = append(affectedServiceIDs, svc.ID)
		}
	}

	// Pass 2: deactivate active services + refund their bonds in full.
	for _, sid := range affectedServiceIDs {
		svc, err := getService(btx, sid)
		if err != nil {
			return nil, err
		}
		if !svc.Active {
			continue
		}
		var bondRefunded types.Coin
		if svc.RegistrationBond.IsPositive() {
			if err := debit(btx, moduleEscrowAddr, svc.RegistrationBond.Amount); err != nil {
				return nil, fmt.Errorf("refunding service bond on domain void: %w", err)
			}
			if err := credit(btx, svc.Owner, svc.RegistrationBond.Amount); err != nil {
				return nil, err
			}
			bondRefunded = svc.RegistrationBond
		}
		svc.Active = false
		svc.RegistrationBond = types.Coin{}
		if err := putService(btx, svc); err != nil {
			return nil, err
		}
		svcPayloadBz, _ := json.Marshal(types.ServiceDeactivatedPayload{
			ServiceID:    svc.ID,
			Owner:        svc.Owner,
			BondRefunded: bondRefunded,
		})
		serviceEvents = append(serviceEvents, types.Event{
			Type: types.EventServiceDeactivated, BlockHeight: height, Payload: svcPayloadBz,
		})
	}

	// Pass 3: void every open request bound to one of the affected services.
	affectedSet := make(map[uint64]bool, len(affectedServiceIDs))
	for _, sid := range affectedServiceIDs {
		affectedSet[sid] = true
	}
	var requestsToVoid []types.InferenceRequest
	reqCur := btx.Bucket(bktRequests).Cursor()
	for k, v := reqCur.First(); k != nil; k, v = reqCur.Next() {
		var r types.InferenceRequest
		if err := json.Unmarshal(v, &r); err != nil {
			return nil, err
		}
		if !affectedSet[r.ServiceID] {
			continue
		}
		switch r.Status {
		case types.StatusPending, types.StatusSubmitted, types.StatusChallenged:
			requestsToVoid = append(requestsToVoid, r)
		}
	}
	for _, r := range requestsToVoid {
		ev, err := s.executeVoidDueToDomain(btx, r, height)
		if err != nil {
			return nil, err
		}
		voidEvents = append(voidEvents, ev)
	}

	domainPayloadBz, _ := json.Marshal(types.DomainDeactivatedPayload{
		DomainID:            d.ID,
		Authority:           signer,
		ServicesDeactivated: len(serviceEvents),
		RequestsVoided:      len(voidEvents),
	})
	out := []types.Event{{
		Type: types.EventDomainDeactivated, BlockHeight: height, Payload: domainPayloadBz,
	}}
	out = append(out, serviceEvents...)
	out = append(out, voidEvents...)
	return out, nil
}

// applyResolveChallenge — Phase 2 catch-up.
//
// Authority explicitly resolves an open CHALLENGED request, overriding the
// time-based auto-resolution. Used when the voucher mechanism can't reach a
// decision (no vouchers, hardware fault, contested edge case).
//
// Returns the events for the chosen resolution path (RequestDismissed or
// RequestSlashed). The bond/escrow distribution is the same as the auto-paths
// in commitBlock — we share the logic by delegating to the helpers below.
func (s *State) applyResolveChallenge(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) ([]types.Event, error) {
	var msg types.MsgResolveChallenge
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}
	auth := string(btx.Bucket(bktMeta).Get(keyAuthority))
	if auth == "" || signer != auth {
		return nil, fmt.Errorf("%w: signer %s != authority %s", types.ErrUnauthorized, signer, auth)
	}
	if msg.Authority != signer {
		return nil, fmt.Errorf("authority field != signer")
	}
	r, err := getRequest(btx, msg.RequestID)
	if err != nil {
		return nil, err
	}
	if r.Status != types.StatusChallenged {
		return nil, fmt.Errorf("%w: status=%s", types.ErrChallengeNotApplicable, r.Status)
	}
	switch msg.Decision {
	case "dismiss":
		ev, err := s.executeDismiss(btx, r, height)
		if err != nil {
			return nil, err
		}
		return []types.Event{ev}, nil
	case "slash":
		ev, err := s.executeSlash(btx, r, height)
		if err != nil {
			return nil, err
		}
		return []types.Event{ev}, nil
	default:
		return nil, fmt.Errorf("%w: got %q", types.ErrInvalidDecision, msg.Decision)
	}
}

// applyVouch — Phase 3.y.
//
// A voucher independently runs the inference and signs an attestation. If the
// voucher's output_hash matches the provider's, they're defending the provider
// (the challenge will be dismissed at resolution if enough vouchers agree).
// If it matches the challenger's, they're reinforcing the slash.
//
// Rules:
//   - request must be CHALLENGED
//   - attestation must reference the same input_hash
//   - attestation tuple must match the service's verification domain
//   - output_hash must equal either the provider's or the (first) challenger's
//     — a voucher with a third output is rejected (their domain is broken or
//     they're trying to game the vote)
//   - one vouch per account per request (no ballot stuffing)
//   - voucher must arrive before resolution timeout
//
// Locks the voucher bond from the voucher's balance into module escrow.
func (s *State) applyVouch(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (types.Event, error) {
	var msg types.MsgVouch
	if err := json.Unmarshal(payload, &msg); err != nil {
		return types.Event{}, fmt.Errorf("payload: %w", err)
	}
	if msg.Voucher != signer {
		return types.Event{}, fmt.Errorf("voucher != signer")
	}

	req, err := getRequest(btx, msg.RequestID)
	if err != nil {
		return types.Event{}, err
	}
	if req.Status != types.StatusChallenged {
		return types.Event{}, fmt.Errorf("%w: status=%s", types.ErrVouchNotApplicable, req.Status)
	}
	if len(req.Challenges) == 0 || req.Result == nil {
		return types.Event{}, fmt.Errorf("invariant: CHALLENGED without challenges or result")
	}

	if msg.Attestation.InputHash != req.InputHash {
		return types.Event{}, types.ErrInputHashMismatch
	}

	// Tuple must match the service's domain (same check as for MsgChallenge).
	svc, err := getService(btx, req.ServiceID)
	if err != nil {
		return types.Event{}, err
	}
	if svc.VerificationDomainID != 0 {
		dom, err := getDomain(btx, svc.VerificationDomainID)
		if err != nil {
			return types.Event{}, fmt.Errorf("load domain: %w", err)
		}
		if msg.Attestation.VerificationDomainID != dom.ID ||
			msg.Attestation.ModelSHA256 != dom.ModelSHA256 ||
			msg.Attestation.RuntimeID != dom.RuntimeID ||
			msg.Attestation.HardwareTag != dom.HardwareTag ||
			msg.Attestation.Precision != dom.Precision {
			return types.Event{}, fmt.Errorf("%w: voucher tuple != service domain",
				types.ErrAttestationDomainMismatch)
		}
		// Phase 1: tokenizer pinning (same back-compat rule as submit + challenge).
		if dom.TokenizerID != "" && msg.Attestation.TokenizerID != dom.TokenizerID {
			return types.Event{}, fmt.Errorf("%w: voucher tokenizer %q != domain tokenizer %q",
				types.ErrAttestationDomainMismatch, msg.Attestation.TokenizerID, dom.TokenizerID)
		}

		// Phase 3.z: voucher must own a service in this domain (skin-in-the-game).
		// Without this, anyone with 25 aios can vouch and tip a single-margin
		// vote. Requiring domain residency means a sybil voucher must first
		// register a service in the domain — a public, traceable footprint.
		eligible, err := voucherEligible(btx, signer, svc.VerificationDomainID)
		if err != nil {
			return types.Event{}, fmt.Errorf("eligibility check: %w", err)
		}
		if !eligible {
			return types.Event{}, types.ErrVouchNotEligible
		}
	}

	// Voucher's hash must match one of the disputed outputs.
	providerHash := req.Result.OutputHash
	challengerHash := req.Challenges[0].Attestation.OutputHash
	var supportsProvider bool
	switch msg.Attestation.OutputHash {
	case providerHash:
		supportsProvider = true
	case challengerHash:
		supportsProvider = false
	default:
		return types.Event{}, fmt.Errorf("%w: voucher hash %s != provider %s and != challenger %s",
			types.ErrVouchHashMismatch, msg.Attestation.OutputHash, providerHash, challengerHash)
	}

	// One vouch per account per request.
	for _, v := range req.Vouchers {
		if v.Voucher == signer {
			return types.Event{}, types.ErrVouchDuplicate
		}
	}

	// Resolution window check — the block producer also enforces this from the
	// other direction (it doesn't resolve until the window expires), but we
	// reject late vouches to keep the math clean.
	const challengeResolutionWindowBlocks = 20
	if height > req.Challenges[0].PostedAt+challengeResolutionWindowBlocks {
		return types.Event{}, fmt.Errorf("%w: resolution window closed", types.ErrVouchNotApplicable)
	}

	params, err := s.paramsLocked(btx)
	if err != nil {
		return types.Event{}, err
	}

	// Phase 3.z step 4: voucher bond scales with provider bond. If the request
	// has no provider bond (e.g. immediate-finalize legacy path), or scaling is
	// disabled (BP=0), fall back to the fixed VoucherBondAmount.
	voucherBond := params.VoucherBondAmount
	if params.VoucherBondScaleBP > 0 && req.ProviderBond != nil && req.ProviderBond.IsPositive() {
		scaled := req.ProviderBond.Amount * uint64(params.VoucherBondScaleBP) / 10000
		voucherBond = types.Coin{Denom: req.ProviderBond.Denom, Amount: scaled}
	}

	// Lock the voucher bond.
	if voucherBond.IsPositive() {
		if err := debit(btx, signer, voucherBond.Amount); err != nil {
			return types.Event{}, fmt.Errorf("locking voucher bond: %w", err)
		}
		if err := credit(btx, moduleEscrowAddr, voucherBond.Amount); err != nil {
			return types.Event{}, err
		}
	}

	v := types.Voucher{
		Voucher:          signer,
		PostedAt:         height,
		Attestation:      msg.Attestation,
		Bond:             voucherBond,
		SupportsProvider: supportsProvider,
	}
	req.Vouchers = append(req.Vouchers, v)
	if err := putRequest(btx, req); err != nil {
		return types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.VouchedPayload{
		RequestID:         req.ID,
		Voucher:           signer,
		VoucherOutputHash: msg.Attestation.OutputHash,
		SupportsProvider:  supportsProvider,
		Bond:              voucherBond,
	})
	return types.Event{
		Type:        types.EventVouched,
		BlockHeight: height,
		Payload:     payloadBz,
	}, nil
}

// applyRegisterDomain registers a new VerificationDomain. Authority-only.
//
// In Phase 1 the chain has a single Authority address (set at bootstrap). Phase
// 2+ moves this to governance.
func (s *State) applyRegisterDomain(btx *bbolt.Tx, signer string, payload json.RawMessage, height int64) (uint64, types.Event, error) {
	var msg types.MsgRegisterDomain
	if err := json.Unmarshal(payload, &msg); err != nil {
		return 0, types.Event{}, fmt.Errorf("payload: %w", err)
	}

	// Authority check. If no authority has been set yet, the *first* signer
	// becomes the authority (boot-strap convenience). Phase 2 removes this.
	auth := string(btx.Bucket(bktMeta).Get(keyAuthority))
	if auth == "" {
		if err := btx.Bucket(bktMeta).Put(keyAuthority, []byte(signer)); err != nil {
			return 0, types.Event{}, err
		}
		auth = signer
	}
	if signer != auth {
		return 0, types.Event{}, fmt.Errorf("%w: signer %s != authority %s", types.ErrUnauthorized, signer, auth)
	}
	if msg.Authority != signer {
		return 0, types.Event{}, fmt.Errorf("authority field %s != signer %s", msg.Authority, signer)
	}

	if msg.ModelSHA256 == "" || msg.RuntimeID == "" || msg.HardwareTag == "" || msg.Precision == "" {
		return 0, types.Event{}, fmt.Errorf("model_sha256, runtime_id, hardware_tag, precision all required")
	}
	if msg.VoucherMargin < 0 {
		return 0, types.Event{}, fmt.Errorf("voucher_margin must be non-negative, got %d", msg.VoucherMargin)
	}

	id := getUint64(btx.Bucket(bktMeta), keyNextDomainID)
	if err := putUint64(btx.Bucket(bktMeta), keyNextDomainID, id+1); err != nil {
		return 0, types.Event{}, err
	}
	d := types.VerificationDomain{
		ID:            id,
		ModelSHA256:   msg.ModelSHA256,
		RuntimeID:     msg.RuntimeID,
		HardwareTag:   msg.HardwareTag,
		Precision:     msg.Precision,
		TokenizerID:   msg.TokenizerID,
		Description:   msg.Description,
		RegisteredAt:  height,
		Active:        true,
		VoucherMargin: msg.VoucherMargin,
	}
	if err := putDomain(btx, d); err != nil {
		return 0, types.Event{}, err
	}

	payloadBz, _ := json.Marshal(types.DomainRegisteredPayload{
		DomainID:    id,
		ModelSHA256: d.ModelSHA256,
		RuntimeID:   d.RuntimeID,
		HardwareTag: d.HardwareTag,
		Precision:   d.Precision,
		TokenizerID: d.TokenizerID,
		Description: d.Description,
	})
	ev := types.Event{
		Type:        types.EventDomainRegistered,
		BlockHeight: height,
		Payload:     payloadBz,
	}
	return id, ev, nil
}

func (s *State) applyTransfer(btx *bbolt.Tx, signer string, payload json.RawMessage) (types.Event, error) {
	var msg types.MsgTransfer
	if err := json.Unmarshal(payload, &msg); err != nil {
		return types.Event{}, err
	}
	if msg.From != signer {
		return types.Event{}, errors.New("from != signer")
	}
	if msg.Amount.Amount == 0 {
		return types.Event{}, types.ErrZeroPrice
	}
	if err := debit(btx, msg.From, msg.Amount.Amount); err != nil {
		return types.Event{}, err
	}
	if err := credit(btx, msg.To, msg.Amount.Amount); err != nil {
		return types.Event{}, err
	}
	// No public event for transfers in Phase 0.5.
	return types.Event{}, nil
}

func (s *State) paramsLocked(btx *bbolt.Tx) (types.Params, error) {
	bz := btx.Bucket(bktMeta).Get(keyParams)
	var p types.Params
	if err := json.Unmarshal(bz, &p); err != nil {
		return types.Params{}, err
	}
	return p, nil
}

// AccountNonce returns the expected next nonce for an address.
func (s *State) AccountNonce(addr string) uint64 {
	var n uint64
	_ = s.db.View(func(btx *bbolt.Tx) error {
		n = nextNonce(btx, addr)
		return nil
	})
	return n
}
