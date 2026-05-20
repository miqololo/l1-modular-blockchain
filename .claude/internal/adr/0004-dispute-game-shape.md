# ADR-0004 — Dispute game shape

**Status**: Phase 3.x accepted + shipped. Phase 3.y voucher mechanism accepted + shipped. Phase 3.z+ escalation layer: **federated re-execution committee accepted; Nitro-style bisection explicitly rejected**.
**Date**: 2026-05-20 (multi-phase revision)
**Decision drivers**: Need a credible challenge mechanism that (a) demonstrably works end-to-end, (b) has correct economic incentives, (c) doesn't require multi-month engineering for the dispute resolution itself.

## Context

The project's load-bearing innovation is optimistic verification of AI inference. By Phase 3 (initial) the chain had:
- Verification domains pinning `(model_sha256, runtime, hardware, precision)`
- Attestations carrying that tuple
- `MsgChallenge` accepting an alternate attestation with a different `output_hash`
- A block-producer auto-transition `CHALLENGED → SLASHED` after a fixed window

The Phase 3 (initial) mechanism was "first challenger wins after timeout." That's enough to demonstrate the *mechanism*, but it's unsound economically — a malicious challenger could grief honest providers for free.

This ADR records the staged design:
1. **Phase 3 (initial, shipped)** — auto-slash after timeout. No bonds.
2. **Phase 3.x (this ADR's primary scope)** — provider + challenger bonds.
3. **Phase 3.y (proposed)** — real dispute resolution: bisection or interactive re-execution.

## Decision (Phase 3.x — accepted)

Both parties stake. Slashing redistributes value.

**Provider bond**: `Params.ProviderBondAmount` (default 50 aios) is locked at `MsgSubmitResult`. On `FINALIZED` it's returned to the provider. On `SLASHED` it's transferred to the challenger.

**Challenger bond**: `Params.ChallengerBondAmount` (default 50 aios) is locked at `MsgChallenge`. On `SLASHED` (challenger wins) it's returned to the challenger. There is no path to forfeit in Phase 3.x because there is no dismissal mechanism yet — see Phase 3.y below.

Both bonds live in the module escrow account during their lifecycle. The chain enforces minimum balances at lock time.

### Economic outcomes

| Scenario | Provider | Challenger | Requester |
|---|---|---|---|
| Honest, no challenge | +escrow, +bond returned | n/a | -escrow |
| Honest, spurious challenge (Phase 3.y will block this) | -bond | +bond+stolen bond | refund |
| Malicious, caught | -bond | +bond returned, +provider bond | refund |
| Malicious, not caught (within window) | +escrow, +bond | n/a | -escrow |

The "honest provider hit by spurious challenge" row is the open vulnerability. It's why Phase 3.y is necessary.

## Decision (Phase 3.y — proposed)

Replace the auto-slash with a real dispute resolution. Three options:

### Option A: Stake-weighted vote

Anyone with a stake on-chain can vote `CHALLENGER_WINS` or `PROVIDER_WINS` during the resolution window. Whichever side accumulates ≥ N tokens wins. Tokens voting for the losing side are forfeit (proportionally).

Pros: simple to implement; aligns with "skin in the game" incentives.
Cons: vote brigading; requires meaningful stake distribution; sybil-resistant only with sufficient token decentralization.

### Option B: Interactive bisection

Provider and challenger play a back-and-forth game over the inference trace. Each round halves the disputed range until a single token (or single op) is identified, which the chain re-executes deterministically.

Pros: cryptoeconomic security only requires *one honest party*. Same model as Arbitrum/Optimism for EVM.
Cons: requires interactive UX from both sides; protocol design effort is substantial; AI inference traces are complex to bisect.

### Option C: Per-block re-execution voucher

A 3rd-party voucher with stake re-runs the inference and signs an attestation. The chain treats two-of-three agreement (provider, challenger, voucher) as decisive. Vouchers earn fees on each request.

Pros: leverages the verification domain — anyone with the runtime can be a voucher. Cheap, parallelizable.
Cons: vouchers can collude; "two-of-three" is weaker than bisection's "1-of-N honest" assumption.

**Recommendation (subject to further analysis): Option C for Phase 3.y, with the door open to Option B for high-value services in Phase 4+.** The reasoning: Option C is the closest in spirit to what the determinism-harness already does — it makes a voucher role official, with explicit reward + slashing rules. Option B's interactive game is a larger engineering project that can land later as a separate codebase layer.

## Open problems (Phase 3.y)

1. **Voucher selection** — who is allowed to vote? Anyone? Stake-weighted lottery? Pre-registered set?
2. **Voucher rewards** — how much do correct vouchers earn? Where does the budget come from (per-request fee, inflation, slashed bonds)?
3. **Quorum** — does the chain wait for N vouchers or any majority within the resolution window?
4. **Tie-breaking** — if vouchers disagree with both provider and challenger, what happens?
5. **Domain compliance** — must vouchers run in the same verification domain? (Yes, almost certainly.)

## Consequences

**Of Phase 3.x (accepted and shipped 2026-05-20)**:
- Economic security against malicious providers: ✓
- Economic security against malicious challengers: ✗ (Phase 3.y)
- Demoable as a complete flow: ✓ (`make demo-malicious`)
- Production-ready for a permissionless testnet: ✗ — needs Phase 3.y first

**Of Phase 3.y (proposed)**:
- Requires Param additions: `voucher_reward_amount`, `voucher_quorum`, `dispute_resolution_window_blocks`.
- New tx types: `MsgVoucherAttestation` (or similar).
- New events: `VoucherSubmitted`, `DisputeResolved`.
- Frontend changes: vouchers need a UX to opt in.
- Inference-node changes: optional voucher mode that listens for challenges and submits if it has the same model.

## Why not bisection in Phase 3.y

Bisection for transformer inference is an open research problem. The interactive game design (which subtree of the computation to bisect into, how to encode model state at intermediate points, how to handle attention caches) is significantly harder than for EVM/MIPS. A Phase 4+ effort with proper protocol research is the right home for it. Phase 3.y unblocks the marketplace with Option C in the meantime.

## Decision (Phase 3.z+ escalation — accepted 2026-05-20)

When the voucher mechanism is insufficient (single-voucher tie, no vouchers arrived, voucher-set integrity in question), escalation is **federated re-execution committee**, NOT Nitro-style interactive bisection.

### Why Nitro-style bisection is rejected for AI inference

Arbitrum / Optimism Cannon work because their terminal "single step" can be re-executed *on-chain*: it's one WASM or MIPS instruction with a known cost. The chain's referee can settle the dispute by literally running the step itself.

That does not work for AI inference:

- The "single step" of a transformer forward pass is a tensor op over weights that the chain does not have (multi-GB).
- Even with weights, the chain runs no inference runtime (no CGO; no GPUs; gas would be impossible to bound).
- Reducing to a single op via bisection requires committing to intermediate hidden states at every checkpoint, which is megabytes per dispute round.

Bisection terminates in a step that *cannot be settled on-chain*, so the entire game structure provides no benefit. opML works around this by committing to a canonical MIPS-VM emulator for the runtime, which adds a layer of complexity we don't want.

### Federated re-execution committee — the chosen design

When `MsgEscalateDispute` is filed (Phase 3.z step 5), the request transitions to `IN_REVIEW`:

1. **Committee selection.** A pre-registered set of reviewers (Phase 4 starts as authority-curated; Phase 5 moves to stake-weighted election) is notified via a new `EventReviewCommitteeAssigned` event. Each reviewer must operate an active service in the disputed request's domain (same eligibility rule as vouchers).
2. **Review window.** Reviewers have N blocks to re-run the inference and post `MsgReview` with attestation + bond. Bond size is `Params.ReviewerBondAmount` (target: ≥ provider bond).
3. **Resolution.** At window close, the chain counts: if ≥ 2/3 of reviewers agree with provider, DISMISS; if ≥ 2/3 agree with challenger, SLASH; else escalate further (manual authority decision in Phase 3.z; permanent stalemate slashes both sides + escrow refunded in Phase 5).
4. **Rewards.** Winning reviewers split a reward pool sourced from losing reviewers' forfeit bonds + losing main-dispute bonds. Reviewer rewards are strictly larger than voucher rewards to compensate for the higher engagement cost.

### Why this is "still optimistic, not committee-only"

The committee is **only invoked on escalation** — the happy path remains:
- 99% of requests: no challenge → finalize via timer.
- ~0.9% of requests: challenge filed → voucher resolves cheaply.
- ~0.1% of requests: voucher inconclusive → escalate to committee.

The committee is the protocol's "deep escalation" tier, not the default. This keeps gas/latency low for the common case while providing a credible escape valve for hard disputes.

### What ships when

- **Phase 3.z step 5a (this ADR's commitment)** — Spec and types only. `MsgEscalateDispute`, `StatusInReview`, `MsgReview`, `EventReviewCommitteeAssigned`, `EventReviewSubmitted`, `EventReviewResolved`. No runtime logic; trying to escalate today returns `ErrEscalationNotImplemented`.
- **Phase 3.z step 5b** — Committee selection (authority-curated). 2-of-3 resolution. Bonds + rewards.
- **Phase 4** — Stake-weighted committee election. Multi-round review. Rate limiting.
- **Phase 5+** — Cross-domain committee delegation; reviewer-of-reviewers (recursive disputes).

### Open problems (deferred to step 5b)

- **Committee size.** Fixed N? Adaptive to dispute value? Default 5; tunable per domain.
- **Bond size for reviewers.** Likely ≥ provider bond × some multiplier (>1) to make collusion expensive.
- **What if no reviewers respond?** Escalate to authority (Phase 3.z); fall back to forced-refund-everyone (Phase 5).
- **Liveness vs safety tradeoff.** Long review window favors safety (more time to recruit honest reviewers) at the cost of liveness (requester waits longer for resolution).

## References (added in this revision)

- See `.claude/internal/prior-art/ora-opml.md` for the MIPS-VM emulator approach we rejected.
- See `.claude/internal/prior-art/gensyn-verde.md` for the verifier-set-with-random-spot-check approach — closer to our committee design but with statistical (vs majority-vote) resolution.

## References

- Arbitrum Nitro fraud proofs (bisection over WASM)
- Optimism Cannon (bisection to single MIPS step)
- Ora opML (optimistic ML verification — closest prior art to this project)
- Gensyn Verde (distributed ML training with fraud proofs)
- Inference Labs Sertn (inference attestation protocol)

Specific implementation references will be added to `.claude/internal/prior-art/` when Phase 3.y scoping begins.
