# ADR-0007 — Voucher sybil resistance

**Status**: Steps 1–4 accepted and shipped. Treasury sweep also shipped. Step 5 (escalation beyond vouchers) committed to "federated re-execution committee" design in ADR-0004; implementation deferred.
**Date**: 2026-05-20 (steps 1–4 shipped same day; revisions are immutable thereafter — supersede via a new ADR)
**Decision drivers**: Phase 3.y's voucher mechanism closed the spurious-challenger attack (A2.1) but opened A3.2 (sybil-voucher collusion). A single sybil voucher with 25 aios could tip any dispute toward the provider's side, including disputes where the provider is provably wrong. This ADR records the staged fix.

## Context

After Phase 3.y, the dispute resolution rule was:

```
providerVouchers > 0 && providerVouchers >= challengerVouchers  →  DISMISSED
otherwise                                                       →  SLASHED
```

This treats every voucher equally and accepts the first arrival as decisive on a tie. Combined with no eligibility check, the attack is:

1. Malicious provider submits a wrong output.
2. Honest challenger files `MsgChallenge`.
3. Attacker creates a fresh keypair with 25 aios of bond, files `MsgVouch` supporting provider's hash. (The keypair could not have produced that hash deterministically, but the chain only checks that the voucher's hash *matches* either the provider's or the challenger's — not that the voucher actually ran the inference.)
4. Tally: `providerVouchers=1`, `challengerVouchers=0` → `DISMISSED`.
5. Provider keeps the escrow + bond; challenger loses bond; the sybil voucher gets bond back + reward share.

Net cost to attacker: 0 (the "sybil voucher" is just another wallet the attacker funds). Provider got paid for a wrong inference.

[`threat-model.md`](../protocol/threat-model.md) A3.2 calls this the dominant unresolved attack on Phase 3.y.

## Decision (step 1 — accepted, shipped 2026-05-20)

Two changes:

### 1. Voucher eligibility

`MsgVouch` is rejected unless the voucher operates at least one service in the disputed request's verification domain. Code:

- New error: `types.ErrVouchNotEligible`
- New helper: `voucherEligible(btx, owner, domainID) (bool, error)` in `chain/internal/state/state.go`
- Public reader: `(*State).IsVoucherEligible(owner, domainID)` for tests / future queries
- Check inserted in `applyVouch` after the tuple match and before the bond lock

The check is bypassed for `domainID == 0` (unverified services) so the Phase 0.5 demo path is preserved.

### 2. `Params.VoucherMargin`

New `int64` field on `Params`. The dismiss rule becomes:

```
providerVouchers > 0 && (providerVouchers - challengerVouchers) >= VoucherMargin  →  DISMISSED
```

Default 0 reproduces Phase 3.y's "provider wins ties" semantics — so the demo (where the bundled determinism-harness is the only voucher) keeps working without modification. Operators tighten it to 1+ when a multi-watcher market exists.

## Why not just tighten the rule today?

Setting `VoucherMargin > 0` in step 1 would break `make demo-spurious` (the harness is the only honest watcher; with margin=1 a spurious challenge would slash the honest provider because 1−0=1 is not > 1). The demo is load-bearing for explaining the protocol; the margin bump waits until at least two independent honest watchers per domain are realistic.

## Why eligibility alone isn't sufficient

~~Service registration is currently free.~~ Step 2 closed that gap. The remaining gaps:

- **~~Step 2 (proposed):~~ Step 2 (shipped 2026-05-20):** a service-registration bond (`Params.ServiceRegistrationBond`, default 100 aios). Locked at `applyRegisterService`. Refunded at `applyDeactivateService` only if `currentHeight - service.CreatedAtHeight >= Params.MinServiceLifetimeBlocks` (default 1000, ~17 min at 1 s blocks). Otherwise the bond stays in module escrow (forfeit). Eligibility check is also tightened to require an **active** service in the domain — combined, a sybil voucher must either (a) keep their sentinel service active and lock 100 aios for at least 17 min, or (b) forfeit the bond on early deactivation. Code: `applyRegisterService` and `applyDeactivateService` in [chain/internal/state/tx.go](../../../chain/internal/state/tx.go); `voucherEligible` in [chain/internal/state/state.go](../../../chain/internal/state/state.go); new `Service.RegistrationBond` field tracked per-service in case params change later.
- **~~Step 3 (proposed):~~ Step 3 (shipped 2026-05-20):** `VerificationDomain.VoucherMargin` field — per-domain override of the global `Params.VoucherMargin`. High-stakes domains can opt into stricter margins (≥ 1) without forcing it on demo/low-stakes domains. `MsgRegisterDomain` accepts an optional `voucher_margin`; 0 = inherit global; > 0 = use override. Production operational guidance: set ≥ 1 once at least two independent honest watchers exist per domain (the demo runs with one harness and so uses 0). Resolution rule in `commitBlock`: `providerVouchers > 0 && (providerVouchers - challengerVouchers) ≥ effectiveMargin`.
- **~~Step 4 (proposed, longer-term):~~ Step 4 (shipped 2026-05-20):** `Params.VoucherBondScaleBP` — voucher bond at `MsgVouch` time scales with the disputed request's `r.ProviderBond.Amount` via `(providerBond × scaleBP) / 10000`. Default 5000 (50%) numerically reproduces the legacy 25-aios voucher bond at the default 50-aios provider bond. When operators raise the provider bond for high-value services, the voucher bond scales proportionally — preserving the "voucher has skin proportional to the risk being adjudicated" invariant. Falls back to fixed `VoucherBondAmount` when scaling is disabled (BP = 0) or the request has no provider bond.
- **Treasury sweep (Phase 3.z, shipped 2026-05-20):** Forfeit bonds — early-deactivation registration bonds and losing provider-side voucher bonds on the slash path — are routed to a new `moduleTreasuryAddr` rather than accumulating in module escrow. `(*State).TreasuryBalance()` exposes the running total. Future ADR routes withdrawals to governance / public-goods / burn.

## Considered alternatives

### Alternative A: Require the voucher to produce a fresh attestation signed within the dispute window

Force the voucher to re-run the inference themselves between the challenge and the vouch. Pros: strong skin-in-the-game proof. Cons: introduces a new "inference timestamp" field, requires the chain to enforce a clock semantically tied to inference duration (hard), and effectively requires every voucher to have hot hardware. Rejected for step 1; revisit in step 4 if cheaper alternatives prove insufficient.

### Alternative B: Voucher reputation system

Track historical voucher accuracy and weight votes by reputation. Pros: long-term-honest watchers accrue power. Cons: cold-start problem; concentrates power in early movers; reputation gaming. Rejected as overreach for step 1.

### Alternative C: Random-step re-execution of provider's output by the chain

Gensyn-Verde-style. Spot-check the disputed inference at a random step. Pros: doesn't depend on voucher set quality. Cons: requires the chain to run inference, which contradicts our optimistic premise. Rejected.

## Consequences

**Positive.**
- A3.2 sybil-voucher attack now requires the attacker to (a) commit `ServiceRegistrationBond = 100` per sybil identity, (b) keep the sentinel service active across any disputes they vote on, and (c) either wait `MinServiceLifetimeBlocks` for a refund or forfeit the bond. A 10-sybil-voucher campaign now costs 1,000 aios up-front, recoverable only by tying up capital for ~17 min × identities. The "free" sybil-vouch is closed.
- Default behavior preserved for the demo: `make demo-spurious` still passes — the demo seed now also registers an active `harness-witness` service owned by the harness key, costing the harness one bond instance (from its 1 B aios genesis allocation).
- Eligibility is now real skin-in-the-game: the voucher must operate an *active* service, not just have registered one. Deactivated services no longer satisfy the gate.
- `Params.VoucherMargin` (from step 1) and `Params.ServiceRegistrationBond` + `Params.MinServiceLifetimeBlocks` provide tuning knobs for operators.

**Negative / implications.**
- **Demo seed cost.** `make demo` (and `/demo/seed`) now consumes 200 aios from dev wallets (bob + harness × 100 each). Both have 1 B aios from `EnsureDevKeyring`, so demo flows are unaffected — but explicit in the docs.
- **Test infrastructure required funding helpers.** New `(*State).SetParams` and `(*State).Mint` are public on `State` for genesis use; the test helper `newState` zeroes the bond by default so existing tests don't need to be rewritten. Bond-specific tests use `newStateWithBond` from `state_test.go`.
- **Forfeit bonds accumulate in `moduleEscrowAddr`.** No treasury sweep yet — they're just locked. A future ADR routes them to a treasury / burn account. Not security-relevant in the short term.
- The `voucherEligible` cursor scan is still O(n) over services (Phase 2/4 hardening item: add a `services_by_owner_domain` index).

## References

- [verification-protocol.md §7.1](../protocol/verification-protocol.md) — open-problem entry, now marked as "Phase 3.z step 1 shipped"
- [threat-model.md A3.2](../protocol/threat-model.md) — sybil-voucher attack analysis
- [ADR-0004](0004-dispute-game-shape.md) — staged dispute-game design that includes the voucher mechanism
- [prior-art/gensyn-verde.md](../prior-art/gensyn-verde.md) — verifier-set sybil bounds in the closest published cousin protocol
