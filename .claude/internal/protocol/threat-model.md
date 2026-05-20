# Threat Model — v1

**Status:** v1 — worked attacks against the shipped Phase 3.y protocol.
**Last updated:** 2026-05-20.

This document enumerates how adversaries might attack the verification protocol and what defends against each one. The protocol is in [verification-protocol.md](verification-protocol.md). Read it first.

Format for each attack:

> **Precondition** — what must be true for the attack to be possible.
> **Damage** — what harm results if it succeeds.
> **Cost to attacker** — bonds, gas, opportunity cost.
> **Profit if successful** — what the attacker walks away with.
> **Mitigation** — what protocol mechanism makes it unprofitable.
> **Residual risk** — what's left after mitigation.

---

## Adversaries (overview)

- **A1** — Malicious provider: wants paid for work not done.
- **A2** — Malicious challenger: wants bonds from honest providers.
- **A3** — Colluding provider + challenger or provider + voucher.
- **A4** — Network adversary: can delay or censor a single tx.
- **A5** — Compromised authority key.
- **A6** — Determinism break (structural, not adversarial).

---

## A1 — Malicious provider

### A1.1 — Submit wrong output and hope no challenger watches

**Precondition.** Service is active. No watcher subscribed during this submission's challenge window.

**Damage.** Requester pays for a result that is not the deterministic function of the input under the domain.

**Cost to attacker.** None if uncaught (provider bond returned on finalize). On catch: `ProviderBondAmount = 50` plus all future earnings (key reputation, but providers are pseudonymous so this is weak).

**Profit if successful.** `r.Escrow.Amount` (the escrow payout).

**Mitigation.**
- `determinism-harness` is always-on by default in the demo stack and watches every `ResultSubmitted` event; in production at least one third-party challenger is assumed per active service.
- `ChallengeWindowBlocks = 45` (~45 s) gives an asynchronous watcher time to re-run and post a challenge.
- Bond ratio `ProviderBond / MinEscrow = 50` means catching just 2% of cheats turns the provider EV-negative.

**Residual risk.** If *no* honest watcher exists for a service for hours (low-volume marketplace, niche domain), the provider can quietly cheat. Phase 4 work: a minimum-stake-weighted incentive for at least one resident challenger per domain. Mitigation today: the indexer surfaces "services with no recent challenger activity" as a watchlist.

### A1.2 — Submit at deadline-edge to escape challenge window

**Precondition.** Network delay between provider's submission and challenger's view of it.

**Damage.** Challenger receives the `ResultSubmitted` event with `< K` blocks of challenge window remaining; cannot re-run inference + sign + broadcast in time.

**Cost to attacker.** None (legal action).

**Profit if successful.** Escrow without challenge risk.

**Mitigation.**
- `ChallengeWindowBlocks` is measured from `SubmittedAtHeight`, which is the height at which the submission's *inclusion* committed, not the wall clock the provider sent. Late submissions don't shorten the window.
- The window is independent of the request deadline, so a deadline-edge submission still gets a full window.

**Residual risk.** If a challenger is on a slow connection and the chain produces blocks fast (1s in demo), even a full 45-block window may be tight. Sized for the demo, not production. Phase 2/3 work: per-domain windows scaling with model size (re-execution time).

### A1.3 — Provide a result that's "close enough" but not bit-exact

**Precondition.** Domain is registered but somehow tolerates near-matches (it does not — output hash equality is the only comparison).

**Damage.** None. The chain compares `output_hash` byte-for-byte; the challenger's "near match" is treated as a different output and so is the provider's.

**Cost / profit / mitigation.** N/A — the attack is precluded by definition. Recorded here because reviewers ask it.

### A1.4 — Pre-compute and reuse outputs for repeated inputs

**Precondition.** Same `input_hash` arrives twice.

**Damage.** None — this is *legal*. The output *is* a deterministic function of the input under the domain; reusing a cached output is correct.

**Mitigation.** N/A. (If a service wants to charge for compute and not just for outputs, that's a service-level policy decision out of protocol scope.)

---

## A2 — Malicious challenger

### A2.1 — File a spurious challenge to grief an honest provider

**Precondition.** Active `SUBMITTED` request. Attacker is willing to post `ChallengerBondAmount = 50`.

**Damage.** Provider can no longer immediately finalize. Their bond stays locked through the resolution window.

**Cost to attacker.**
- Without Phase 3.y: in the Phase 3.x cut, the authority resolves; a wise authority dismisses; challenger loses `ChallengerBond`.
- With Phase 3.y: if no provider-side voucher arrives, default classification slashes the provider — the spurious challenge **wins**. This is why 3.y exists: vouchers transform the default from "slash if challenged" to "neutral if challenged."

**Profit if successful.** Provider's `ProviderBond = 50`.

**Mitigation (Phase 3.y).**
- `determinism-harness` watches and vouchers for honest providers (`MsgVouch`, `supports_provider=true`).
- One honest voucher → `providerVouchers >= challengerVouchers (= 0)` and provider wins; challenger forfeits 50, voucher gets a 50-share reward.
- Demonstrated end-to-end via `make demo-spurious` (2026-05-20).

**Residual risk.** If no voucher arrives (low-activity service, harness offline), default tally is `providerVouchers = 0`, which loses by the rule `providerVouchers > 0 && providerVouchers >= challengerVouchers`. Net: an isolated provider on a quiet service can be griefed by anyone with 50 aios. Open problem (§7.1 of protocol spec): require challengers to *prove* their counter-output before slashing follows.

### A2.2 — Bond-grief by chained spurious challenges across many services

**Precondition.** Many active services with no voucher coverage.

**Damage.** Provider economy degrades — operational cost rises, providers may exit the marketplace.

**Cost to attacker.** `ChallengerBondAmount × N` (50 × N). If all are dismissed (Phase 3.y with vouchers), attacker loses 50N.

**Profit.** Zero if dismissed. Equal to `ProviderBondAmount × N` (50N) if slashed for lack of vouchers.

**Mitigation.** Same as A2.1. Also: the `determinism-harness` is per-domain; one harness covers all services in its domain, so a chained spurious attack is bounded by harness up-time, not provider count.

**Residual risk.** Targeting domains with no harness coverage. Phase 4 work: register the harness as a chain participant with up-time guarantees; or require a non-trivial registration bond for challengers.

---

## A3 — Collusion

### A3.1 — Provider + challenger collude to extract bonds and re-grief

**Precondition.** Two identities both controlled by the attacker. Attacker treats both bonds as part of the same wallet.

**Damage.** Bonds shuffle internally; nominal "slash" results in attacker paying attacker. No protocol value moves, but the request's escrow goes to "challenger" who is also "provider" — i.e. the attacker recovers escrow on a wrong submission *plus* keeps both bonds.

**Cost.** `ProviderBond + ChallengerBond = 100`, paid by attacker to themselves. Net cost: 0 (minus tx fees).

**Profit.** `r.Escrow.Amount` (the requester's payment, refunded "to the requester" — but wait, the requester is the actual user, not the attacker. Reread the slash flow.)

**Reanalysis.** On `SLASHED`:
- `r.Escrow → r.Requester` (the actual user, not the attacker)
- `r.ProviderBond → challenger` (attacker)
- `challengerBond → challenger` (attacker)

Net to attacker: gets back own `ChallengerBond` + own `ProviderBond` (paid to "challenger"). Net cost: 0. Net to user: escrow refunded. **The user has been griefed (no service performed) but not financially harmed.**

**Mitigation.**
- The collusion produces no direct financial profit for the attacker but *denies service* — the requester gets a refund instead of a result. Phase 3.z work item: charge an explicit `disputeFee` (paid by provider on slash) that goes to the protocol treasury or burned, breaking the 0-net-cost equilibrium.
- The provider's reputation/identity is burned (their key is publicly slashed); pseudonymity-busting via off-chain signals (IP, payment rails) is the practical defense.

**Residual risk.** Sustained denial-of-service on a target service by burning identities. Cheap. Real. Open problem.

### A3.2 — Provider + voucher collude (sybil voucher)

**Precondition.** Attacker controls the provider and at least one voucher identity in the same domain. No second voucher contesting them.

**Damage.** Any honest challenge can be dismissed: `providerVouchers >= challengerVouchers (= 0)` is trivially satisfied with one sybil voucher.

**Cost.** `VoucherBondAmount = 25` (held; refunded on dismiss).

**Profit.** Avoid `ProviderBondAmount = 50` slash on a wrong submission. Net: gain 50 - 0 (voucher bond returned) = 50.

**Mitigation (today, weak).**
- Honest challengers can recruit honest counter-vouchers; in a balanced marketplace `providerVouchers >= challengerVouchers` becomes contested.
- The `determinism-harness` vouchers for the *correct* side; if it disagrees with the provider, it vouches for the challenger.

**Mitigation (Phase 3.z steps 1 + 2 — landed 2026-05-20).**
- **Voucher eligibility (shipped, tightened in step 2):** `MsgVouch` is rejected unless the voucher operates an **active** service in the disputed request's verification domain. Deactivated services no longer count. See `applyVouch` in [chain/internal/state/tx.go](../../../chain/internal/state/tx.go) and the helper `voucherEligible` in [chain/internal/state/state.go](../../../chain/internal/state/state.go).
- **Service-registration bond (shipped, step 2):** `Params.ServiceRegistrationBond` (default 100 aios) is locked at `applyRegisterService`. `Params.MinServiceLifetimeBlocks` (default 1000 ≈ 17 min) is the minimum age below which deactivation forfeits the bond instead of refunding it. A sybil voucher must therefore either (a) commit 100 aios for ≥ 17 min OR (b) forfeit the bond. Combined with the active-eligibility requirement, sybil-vouching has a hard cost floor.
- **VoucherMargin param (shipped step 1, default 0):** `Params.VoucherMargin` lets the chain require a net excess of provider-side over challenger-side vouchers (`providerVouchers - challengerVouchers ≥ VoucherMargin`). Default 0 keeps the 3.y demo working (single harness voucher tips the vote against a spurious challenge). Bumping to 1+ once a 2-watcher market exists adds depth.

**Mitigation (still planned).**
- **VoucherMargin > 0 in production** (Phase 3.z step 3). Gated on at least two independent honest watchers per domain.
- **Voucher bond proportional to provider bond** (Phase 3.z step 4).
- **Forfeit-bond treasury sweep / burn** (future ADR). Today forfeit bonds accumulate in `moduleEscrowAddr` — not security-relevant short-term but should be claimable by a treasury or burned for clean accounting.

**Residual risk.** As of Phase 3.z step 2, a 10-sybil-voucher campaign now costs 1,000 aios up-front (or full forfeit on early exit). That's a material economic floor and a publicly visible footprint. With `VoucherMargin = 0` (current default), a single eligible sybil voucher can still tip; the substantive *additional* defense lands when `VoucherMargin > 0` is the rule. ADR-0007 ([../adr/0007-voucher-sybil-resistance.md](../adr/0007-voucher-sybil-resistance.md)) tracks the open items.

---

## A4 — Network adversary

### A4.1 — Censor an honest challenger's `MsgChallenge` from inclusion

**Precondition.** Validator(s) producing blocks during the challenge window are colluding with the provider (or just adversarial to challengers).

**Damage.** Challenger's tx never enters a block before `ChallengeWindowBlocks` expires → request finalizes for the malicious provider.

**Cost.** Censoring validator may face standard consensus slashing if detected (Phase 0.5 chain has no slashing yet; CometBFT does).

**Profit.** Escrow + provider bond returned to malicious provider.

**Mitigation (today, partial).**
- The chain is a single producer in Phase 0.5 (the `RunBlockProducer` goroutine); censorship is "the operator drops your tx" — fully trusted. **Pre-CometBFT, this attack is undefended.**
- In Phase 1+ (CometBFT), the standard validator-honest-majority assumption kicks in.

**Mitigation (planned).**
- DA-layer-anchored challenges: a `MsgChallenge` is *commitable* on a separate DA layer (Celestia) and bind retroactively; the chain accepts late challenges if backed by an earlier DA commit.
- `ChallengeWindowBlocks` longer than the maximum validator rotation period.

**Residual risk.** Phase 0.5: total censorship vulnerability. Acceptable for demo; flagged in PHASE.md.

### A4.2 — Drop `ResultSubmitted` SSE so harness/challenger never see it

**Precondition.** Attacker controls the network path between chain and harness.

**Damage.** Harness/challenger never knows there's a submission to re-run.

**Cost.** None (passive).

**Profit.** Same as A1.1 (silent cheat).

**Mitigation.**
- SSE consumers reconcile state on reconnect by polling `/requests?status=SUBMITTED&since=<height>`; missed events are surfaced.
- Multiple independent challengers reduce the chance all of them are network-isolated.

**Residual risk.** A challenger relying on a single SSE feed is vulnerable to single-network-path failures. Documented in `docs/integrate-an-inference-node.md` as a deployment requirement (run reconciler).

---

## A5 — Compromised authority

### A5.1 — Authority key stolen, attacker registers a non-deterministic domain

**Precondition.** Authority private key compromised.

**Damage.** Attacker can register a domain like `(<any model>, hosted-api-X, ?, ?)`. Honest providers on this domain submit; honest challengers cannot reproduce (non-determinism); challenges succeed; honest providers lose their bonds.

**Cost.** None (attacker is using stolen authority).

**Profit.** Drain provider bonds by colluding with own "challenger" identity.

**Mitigation (today).**
- Single key. **Total trust in authority.** This is a known centralization for Phase 0.5–3.y.
- `MsgDeactivateDomain` exists so the authority (or a recovered authority key) can retire a bad domain.

**Mitigation (planned).**
- Phase 4: replace authority with a 2-of-3 multisig; then a governance module.
- Domain registrations include the determinism-evidence transcript; on-chain reverifier can refuse a domain that doesn't show two-host evidence.

**Residual risk.** Phase 0.5 through 3.y: authority is a single point of failure. Documented in PHASE.md and ADR-0003.

---

## A6 — Determinism break (structural)

### A6.1 — A registered domain turns out to be non-deterministic

**Precondition.** Domain passes the determinism gate (which today is "same-machine 2 processes 6 hashes," see `make determinism-check`) but is non-deterministic across other axes (different CPU microcode, different libc, different thread count).

**Damage.** Honest providers and honest challengers produce different output hashes on the same input. Challenges succeed against honest providers. Bonds drain.

**Cost.** None — the protocol is doing what it was told to do.

**Profit.** Indirect — adversarial harness operator can profit from spurious-but-formally-correct challenges. Or it just kills the marketplace.

**Mitigation (today).**
- `MsgDeactivateDomain` lets the authority retire the domain when divergence is detected.
- The harness can detect divergence on *itself* (different runs producing different hashes within the harness's own setup) and refuse to operate.

**Mitigation (planned).**
- Cross-host determinism gate before domain registration (Phase 1).
- Domain "audit period" — first N submissions on a new domain are advisory-only (no slash) while the harness builds confidence.
- Refund-on-deactivation: when a domain is retired, open requests bound to its services are refunded (currently a one-line change, blocked on policy choice — see verification-protocol.md §7.5).

**Residual risk.** Until Phase 1's cross-host gate is in place, a newly-registered domain that happens to fail on some hardware that no one tested can cost honest providers their bonds before being caught. The economic damage is bounded by the number of submissions before deactivation × `ProviderBondAmount`. In a small marketplace this is tolerable; at scale it requires the cross-host gate.

---

## Economic invariants the protocol must preserve

1. **Honest provider EV > 0.** Expected revenue per honest submission exceeds expected slashing (which approaches 0 for honest behavior except via A6.1).
2. **Honest challenger EV > 0.** Reward on correctly identifying a wrong submission exceeds bond loss on a spurious one × spurious rate.
3. **Malicious provider EV < 0.** A1.x analyses must show negative EV for cheating in steady state.
4. **Voucher EV > 0 for honest vouchers.** Reward share must exceed bond × loss probability for vouching on the correct side.
5. **Sybil voucher EV ≤ 0.** Open problem (A3.2). Today, sybil-vouching is profitable — this is the dominant unresolved issue.

If any of these are violated, the protocol is broken, even if no concrete attack is identified.

---

## What an adversary cannot do (within trust assumptions)

- **Forge a submission as a different provider.** Signatures over canonical attestation bytes prevent it. (Compromised private keys are out of scope per A5.)
- **Replay a previous submission for a new request.** `request_id` is included in the signed canonical bytes.
- **Submit on a deactivated service.** `applyRequestInference` checks service active flag; `applySubmitResult` re-checks at submit time.
- **Submit on an inactive domain.** Same — domain inactive → submission rejected.
- **Change a finalized request's outcome.** Terminal statuses are immutable; subsequent txs against finalized requests error out.

These are the protocol's hard guarantees. Everything else is a probabilistic argument over bonded behavior.
