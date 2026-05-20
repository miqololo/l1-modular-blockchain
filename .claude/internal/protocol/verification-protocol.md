# Verification Protocol — v1

**Status:** v1 — describes what is shipped through Phase 3.y. Open problems are tracked at the end.
**Last updated:** 2026-05-20.

This document specifies how the chain verifies that an off-chain AI inference was performed correctly. It is the **load-bearing innovation** of the project.

---

## 1. Threat model and goals

### 1.1 Goal

A requester pays a provider for an off-chain inference and gets a result. The chain must make it **economically irrational** for a provider to submit a wrong result, without re-executing the inference on every validator.

"Wrong" is defined precisely: a result is wrong if, within the request's **verification domain** (§3), an honest re-runner of the same inputs gets a different output hash.

### 1.2 What we are NOT trying to do

- **Hide the input** from the provider. The provider must read the input to run the model. Zero-knowledge inference is out of scope.
- **Verify subjective quality**. "Was this translation good?" is not a protocol question. The protocol only verifies that the output is the deterministic function of the input under the registered domain.
- **Force determinism on the model**. Models can be non-deterministic (sampling, MoE routing); they just cannot then be registered under a verification domain that requires bit-exact reproducibility.

### 1.3 Trust assumptions

| Assumption | Where it lives | What breaks if violated |
|---|---|---|
| At least one honest watcher per active service exists during the challenge window | Off-chain (challenger market) | Wrong results finalize undisputed (A1.1, A4.1) |
| The chain orders txs honestly (one honest-majority validator set) | Standard consensus | Censorship of `MsgChallenge` (A4.1); double-spend; revert |
| The registered domain is genuinely deterministic | Domain registry + harness | Honest provider gets slashed (A6.1) |
| Provider's chain key is not stolen | Off-chain key management | Attacker submits with provider's identity (out of scope) |
| `MsgRegisterDomain` is gated by an authority that vets domains | `applyRegisterDomain` requires authority sig | Adversary registers a non-deterministic tuple (A6.1) |

The protocol degrades **detectably** when (1) is violated (challenges never come) — wrong results accumulate, but the *next* honest watcher can challenge with retroactive evidence as long as they post the original output (see §4.6). It degrades **catastrophically** when (3) is violated — honest providers lose their bonds even when they did the right thing.

---

## 2. Actors

| Actor | Role | Bonded? | Reward | Slashing condition |
|---|---|---|---|---|
| **Requester** | Posts `MsgRequestInference`, escrows `max_price` | Escrow only | Refund on dispute lost by provider | Never (purely a buyer) |
| **Provider** | Owns one or more services. Runs inference, posts `MsgSubmitResult` with attestation. | `ProviderBondAmount` per submission (default 50) | Receives escrow on finalize; bond returned | Bond → challenger if `SLASHED` |
| **Challenger** | Watches `ResultSubmitted` events. If their re-run within the same domain produces a different output hash, posts `MsgChallenge`. | `ChallengerBondAmount` per challenge (default 50) | Provider's bond on `SLASHED` + own bond returned | Bond → reward pool if `DISMISSED` |
| **Voucher** | Third party with the same verification domain. Posts `MsgVouch` taking a side in an open dispute. | `VoucherBondAmount` (default 25) | `VoucherRewardAmount` (25) + own bond returned if their side wins | Bond → reward pool if their side loses |
| **Authority** | Initial-bootstrap dev key. Registers domains. Can post `MsgResolveChallenge` for explicit resolution. | Not bonded (trusted) | None | Manual revocation (Phase 4 work) |

The authority is a known centralization point and is documented as such in `.claude/internal/adr/0003-runtime-and-verification-domains.md`. Phase 4 replaces it with a governance module.

---

## 3. Verification domain

A **verification domain** is the tuple under which two outputs are comparable:

```
(model_sha256, runtime_id, hardware_tag, precision, tokenizer_id)
```

- `model_sha256` — SHA-256 of the GGUF/safetensors file. Different quantizations are different domains.
- `runtime_id` — e.g. `llama.cpp-server-b3447`, `vllm-0.5.4`. Stable string committed by the authority.
- `hardware_tag` — coarse class (`cpu-x86_64-avx2`, `nvidia-a100-80gb`). Phase 1+: needs finer-grained empirical validation.
- `precision` — quantization or floating point class (`q4_k_m`, `bf16`, `fp32`). Implied by the model SHA in most cases but kept explicit so wrong precision is rejectable at submit time.
- `tokenizer_id` (Phase 1) — identifier for the tokenizer implementation. Empty = legacy "don't check" (used by Phase 0.5–3.z domains and preserved for backwards compatibility); non-empty = attestations must declare a matching `TokenizerID` at submit / challenge / vouch time. Different tokenizer implementations disagree on edge cases (BPE merges, byte fallback, special-token handling) even when fed the same model file; pinning the tokenizer prevents these silent divergence sources from being classified as honest disputes.

A service is registered against **one** verification domain. The provider's attestation must match it exactly or the chain rejects the submission with `ErrAttestationDomainMismatch`.

### 3.1 Determinism requirement

Before a domain may be registered (`MsgRegisterDomain`), the authority must hold evidence that the tuple produces bit-exact outputs across at least two independent runs on hardware matching `hardware_tag`. In practice this evidence comes from the `determinism-harness` (`make determinism-check` exercises the same-machine case across two processes; cross-machine validation is Phase 1 work).

If the determinism assumption fails after a domain is registered, the authority calls `MsgDeactivateDomain`. As of Phase 3.z this transaction cascades: services bound to the dying domain auto-deactivate (registration bonds refunded in full, overriding the lifetime check), and every open request on those services is voided (escrow + provider bond + challenger bond + voucher bonds all returned). See §7.5 for the design rationale and `applyDeactivateDomain` + `executeVoidDueToDomain` for the implementation.

### 3.2 Empirical validation today (Phase 0.5 demo)

- `determinism-harness/` independently re-runs every finalized inference within its own domain. On divergence, it currently logs a verdict (`DIVERGENT`) and files `MsgChallenge`. On agreement, it logs `OK`; if a separate party then files a spurious challenge, the harness can voucher for the provider (`MsgVouch`, `supports_provider=true`).
- `make determinism-check` runs the same prompt against two independent `llama-server` processes and compares output SHAs. As of 2026-05-20 the demo tuple `(tinyllama-1.1b-Q4_K_M, llama.cpp-server, cpu-x86_64, q4_k_m)` produces 6 identical hashes across 2 processes × 3 runs each.

**Important caveat:** "two processes on the same machine" is *not* the cross-machine validation that Phase 1's gate demands. It rules out per-process state leakage; it does not rule out CPU-microarchitecture-dependent reductions in the runtime. Phase 1's harness must add a remote runner.

---

## 4. Inference lifecycle

The status machine is implemented in `chain/internal/types/types.go` (status enum) and driven by `chain/internal/state/tx.go` + `chain/internal/state/block.go` + `chain/internal/state/resolution.go`.

### 4.1 State diagram

```
              MsgRequestInference
                     │
                     ▼
                 PENDING ─────deadline elapsed───▶ REFUNDED  (escrow → requester)
                     │
            MsgSubmitResult
                     │
                     ▼
                SUBMITTED ────challenge window expires────▶ FINALIZED  (escrow → provider, bond returned)
                     │
              MsgChallenge
                     │
                     ▼
               CHALLENGED  ─── resolution window expires, vouchers count ─┐
                     │                                                    │
                     ├─ MsgResolveChallenge dismiss / vouchers favor   ──▶ FINALIZED (dismissed)
                     │     ↳ escrow → provider, provider bond returned, challenger bond → reward pool,
                     │       provider-side voucher bonds returned + reward share
                     │
                     └─ MsgResolveChallenge slash / no provider voucher─▶ SLASHED
                           ↳ escrow → requester, provider bond → challenger,
                             challenger bond returned
```

`StatusRefunded` and `StatusSlashed` and `StatusFinalized` are terminal. `StatusFinalized` covers both the "no challenge" and "challenge dismissed" outcomes — the dismissal flag lives in the emitted event payload.

### 4.2 MsgRequestInference

Implemented in `applyRequestInference` (`chain/internal/state/tx.go`).

- Validates: requester signature, service exists and is **active** (Phase 2 addition; previously any service was OK), `max_price ≥ service.price ≥ MinServicePrice`, requester balance covers `max_price`.
- Effects: escrow `max_price` from requester to `moduleEscrowAddr`. Allocate a `request_id`. Set `DeadlineHeight = currentHeight + RequestDeadlineBlocks`.
- Event: `InferenceRequested { request_id, service_id, requester, input_hash, deadline_height, max_price, domain_id }`.

### 4.3 MsgSubmitResult

Implemented in `applySubmitResult`.

- Validates: provider signature, request exists and is `PENDING`, current height ≤ `DeadlineHeight`, provider owns the service, attestation domain matches the service's domain (model_sha256, runtime_id, hardware_tag, precision all equal). Provider balance covers `ProviderBondAmount`.
- Effects: lock `ProviderBondAmount` from provider into escrow. Record `r.ProviderBond`, `r.OutputHash`, `r.Attestation`, `r.SubmittedAtHeight = currentHeight`. Status → `SUBMITTED`.
- Event: `ResultSubmitted { request_id, provider, output_hash, attestation, submitted_at_height }`.

### 4.4 MsgChallenge

Implemented in `applyChallenge`.

- Validates: challenger signature, request is `SUBMITTED`, `currentHeight ≤ r.SubmittedAtHeight + params.ChallengeWindowBlocks`, challenger has balance for `ChallengerBondAmount`, challenger's claimed `output_hash` differs from `r.OutputHash`. (The chain does not — and cannot — verify which output is correct; that is the dispute's job. It only checks the challenger is *contesting* a different output.)
- Effects: lock `ChallengerBondAmount` into escrow. Append `Challenge{Challenger, Bond, PostedAt: currentHeight, OutputHash: claimed}` to `r.Challenges`. Status → `CHALLENGED`.
- Event: `Challenged { request_id, challenger, claimed_output_hash, posted_at_height }`.

### 4.5 MsgVouch (Phase 3.y)

Implemented in `applyVouch`.

- Validates: voucher signature, request is `CHALLENGED`, current height ≤ `r.Challenges[0].PostedAt + ResolutionWindowBlocks`, voucher has balance for `VoucherBondAmount`, voucher's attestation matches the request's domain.
- Effects: lock `VoucherBondAmount` into escrow. Append `Voucher{Voucher, Bond, SupportsProvider, OutputHash, PostedAt}` to `r.Vouchers`.
- Event: `Vouched { request_id, voucher, supports_provider, output_hash }`.

### 4.6 Resolution

Two paths converge on the same outcome functions, both in `chain/internal/state/resolution.go`:

- **Timeout path** (`commitBlock`): after `r.Challenges[0].PostedAt + challengeResolutionWindowBlocks` blocks, the block producer counts vouchers. Provider wins iff `providerVouchers > 0 && providerVouchers ≥ challengerVouchers`. Wins → `executeDismiss`. Loses → `executeSlash`.
- **Explicit path** (`MsgResolveChallenge`): the authority calls `applyResolveChallenge` with `decision ∈ {"slash", "dismiss"}`. Dispatches to the same `executeSlash` / `executeDismiss`. Used for the Phase 3.x cut where the authority is the trusted resolver and for emergency override even in 3.y.

#### 4.6.1 executeSlash — provider loses

| Account flow | Amount |
|---|---|
| `moduleEscrowAddr` → `r.Requester` | `r.Escrow.Amount` (refund) |
| `moduleEscrowAddr` → `challenger` | `r.ProviderBond.Amount` (slashing reward) |
| `moduleEscrowAddr` → `challenger` | `challengerBond.Amount` (bond returned) |

Voucher bonds:
- Provider-side vouchers (none, since this branch means they lost or none existed): **forfeit** (stay in escrow — Phase 3.z work item: should be paid to challenger / reward pool explicitly).
- Challenger-side voucher bonds: **returned** in current code (Phase 3.z: reward them with a share).

Status → `SLASHED`. Event: `RequestSlashed { request_id, provider, challenger, refunded, provider_bond_slashed, challenger_bond_returned }`.

#### 4.6.2 executeDismiss — provider wins

| Account flow | Amount |
|---|---|
| `moduleEscrowAddr` → `service.Owner` | `r.Escrow.Amount` (normal payout) |
| `moduleEscrowAddr` → `service.Owner` | `r.ProviderBond.Amount` (bond returned) |
| `moduleEscrowAddr` → provider-side voucher | `v.Bond.Amount` per voucher (bond returned) |

Reward pool composition:
- + `challengerBond.Amount` (challenger forfeited)
- + sum of challenger-side voucher bonds (they forfeited)

Reward pool distribution:
- `recipients = 1 (provider) + len(providerVouchers)`
- `share = pool / recipients`; remainder → provider
- Each provider-side voucher gets `share`

Status → `FINALIZED`. Event: `RequestDismissed { request_id, provider, challenger, paid, provider_bond_returned, challenger_bond_forfeit, voucher_count }`.

### 4.7 Block producer (time-driven transitions)

`RunBlockProducer` ticks once per `interval` (1s in dev). Each tick:

1. Increment height.
2. Scan all requests:
   - `PENDING` past `DeadlineHeight` → refund.
   - `SUBMITTED` past challenge window → finalize.
   - `CHALLENGED` past resolution window → classify by voucher tally → dismiss or slash.
3. Emit `BlockCommitted { height, time }`.

The chain is intentionally Tendermint-shaped but does *not* use CometBFT in Phase 0.5. The Cosmos SDK port is on the Phase 1 roadmap (see ADR-0001 and PHASE.md).

---

## 5. Attestation v1

Defined in `chain/internal/types/types.go` (`Attestation` struct) and validated in `applySubmitResult`.

### 5.1 Fields

```jsonc
{
  "provider_pubkey": "<hex ed25519>",
  "model_sha256":    "<hex>",
  "runtime_id":      "llama.cpp-server",
  "hardware_tag":    "cpu-x86_64-tinyllama-q4",
  "precision":       "q4_k_m",
  "input_hash":      "<sha256 hex>",
  "output_hash":     "<sha256 hex>",
  "request_id":      42,
  "submitted_at":    "2026-05-20T12:34:56Z",
  "signature_hex":   "<hex ed25519 over canonical encoding with signature_hex=\"\">"
}
```

### 5.2 Canonicalization rule

`signature_hex` is computed over a canonical JSON encoding of the attestation **with `signature_hex` set to the empty string** and field order matching the struct definition. Order matters because Go's `encoding/json` emits fields in struct order, not lexicographic order; downstream verifiers (harness, voucher, future chain reverifier) must match this byte-for-byte.

This is a known foot-gun: the canonical encoder is a single function in `chain/internal/state/tx.go` and any reimplementation must produce identical bytes. Phase 1 work: replace JSON with a properly canonical format (proto with `deterministic = true` is the leading candidate; CBOR canonical form is the backup).

### 5.3 Verification at submit time

The chain itself does not re-execute inference. It verifies:

1. `signature_hex` validates against `provider_pubkey` over the canonical bytes.
2. `provider_pubkey` matches the service's registered owner key.
3. `input_hash` matches the request's input hash.
4. `(model_sha256, runtime_id, hardware_tag, precision)` matches the service's domain exactly.
5. `request_id` matches.
6. `submitted_at` parses; not currently used in adversarial checks but recorded.

Off-chain consumers (challenger, voucher, harness) additionally re-run the input within the same domain and compare their output hash to `output_hash`. That is the *substance* of verification; everything on-chain is bookkeeping.

---

## 6. Economic security (Phase 3.y defaults)

```
ProviderBondAmount       = 50 aios       (locked at submit, returned on finalize, forfeit on slash)
ChallengerBondAmount     = 50 aios       (locked at challenge, returned on slash, forfeit on dismiss)
VoucherBondAmount        = 25 aios       (legacy fixed amount; used only when ProviderBond is nil)
VoucherBondScaleBP       = 5000 (50%)    (Phase 3.z step 4; voucher bond = providerBond × BP / 10000)
VoucherRewardAmount      = 25 aios       (paid from reward pool to each winning voucher)
ServiceRegistrationBond  = 100 aios      (Phase 3.z step 2; locked at register, refundable only past lifetime)
MinServiceLifetimeBlocks = 1000          (~17 min at 1 s blocks; early deactivation forfeits to treasury)
VoucherMargin            = 0             (Phase 3.z step 1; per-domain override available, step 3)
MinServicePrice          = 1 aios
ChallengeWindowBlocks    = 45            (~45 s at 1 s block time)
```

**Treasury (Phase 3.z step 3).** Forfeit bonds — early-deactivation service bonds and losing provider-side voucher bonds on the slash path — are routed to `moduleTreasuryAddr` rather than left to accumulate in module escrow. This keeps module escrow holding only bonds in active disputes. A future ADR routes treasury withdrawals to governance / public-goods funding / burn.

These are placeholders sized for the demo, not derived from an empirical safety margin. ADR-0005 (open) is the home for the real economic analysis.

### 6.1 Honesty equilibrium (informal)

For a provider tempted to cheat on a single inference of escrow value `E`:

- Expected gain from cheating: `E × P(no challenger watches)`
- Expected loss from cheating: `ProviderBond × P(challenged ∧ resolved against provider)`

Honest is favored when `ProviderBond × P(caught) > E × P(uncaught)`. In the demo with `E = 1, ProviderBond = 50`, the provider can sustain catching probability as low as `2%` and remain better off honest. This argument breaks if (a) many small wrong submissions can be aggregated by an attacker faster than challengers can keep up, or (b) the challenge probability is correlated with what the provider can observe (e.g. the provider can detect a challenger's IP and only cheat when absent) — both A3.x patterns in the threat model.

### 6.2 Challenger participation

A challenger spends `monitoring_cost + ChallengerBond` per challenge filed. Expected reward is `ProviderBond × P(provider actually wrong | I am challenging)`. The challenger should only file when they have evidence (their own re-run) that the provider is wrong — which is exactly what an honest challenger does. The bond mechanism prevents fishing.

A subtler problem: with `ChallengerBond == ProviderBond`, the challenger's downside on a spurious challenge equals the provider's upside on a successful slash. Asymmetric bonds (challenger pays more) discourage spurious challenges but raise the bar for honest ones; asymmetric the other way (challenger pays less) invites griefing. ADR-0005 work item.

### 6.3 Voucher game (Phase 3.y)

A voucher pays `VoucherBond = 25` to take a side. Winning vouchers get `VoucherReward = 25` (effectively their stake back as reward, plus original bond). The reward pool is fed by the loser's bonds (challenger's 50 + losing-voucher 25-each). With 1 winning voucher and a 50-aios pool, the share is 50 — well above the 25 reward target. As multiple vouchers join, share per voucher drops; in the implementation we cap reward at `share = pool / recipients` (no fixed `VoucherRewardAmount` payment — the constant exists in params but is *not* the rule the code uses).

This is a gap between params and code: the doc and `params.VoucherRewardAmount` field suggest a fixed reward, but `executeDismiss` actually splits the pool. Fix in either direction (drop the constant, or split pool but top up from treasury to hit the target) is Phase 3.z work.

---

## 7. Open problems (Phase 3.z / 4 work)

Tracked here until they get ADRs:

1. **Multi-voucher quorum.** ~~Currently 1 voucher tips the tally. Sybil attack: provider creates 10 voucher identities and wins every dispute.~~ **Phase 3.z steps 1–4 (shipped 2026-05-20):** `MsgVouch` requires the voucher to operate an **active** service in the disputed request's verification domain (`voucherEligible` helper). Service registration locks `Params.ServiceRegistrationBond` (default 100 aios), refundable only after `Params.MinServiceLifetimeBlocks` (default 1000 ≈ 17 min) — early deactivation forfeits the bond to the treasury. Voucher bonds scale with the disputed request's provider bond (`Params.VoucherBondScaleBP`, default 5000 = 50% → numerically identical to the legacy 25/50 ratio at default settings, but proportional in any future high-stake config). The dismiss rule is `providerVouchers > 0 && (providerVouchers - challengerVouchers) ≥ margin` where margin is the per-domain override (`VerificationDomain.VoucherMargin`) if set, else global `Params.VoucherMargin` (default 0). Remaining gap: production deployments should set margin ≥ 1 once a 2-watcher market exists per domain. Bisection-style further escalation (Phase 3.z step 5) is committed to "federated re-execution committee" in ADR-0004; implementation deferred. ADR-0007 tracks the staged plan.
2. **Bisection fallback.** When vouchers cannot resolve (split tally, no vouchers, domain in dispute), the chain should fall back to an interactive bisection game over the inference trace. Sketched in ADR-0004 but unimplemented. Required to credibly extend to >7B parameter models.
3. **Slashing voucher bonds.** Today losing voucher bonds stay in escrow on slash. Should they go to challenger? To treasury? To the next dispute's reward pool?
4. **Challenger-side voucher reward on slash.** Currently the challenger gets the provider's bond. Challenger-side vouchers get their bond back but no reward share. Symmetry with dismiss path is missing.
5. ~~**Domain deactivation refunds.**~~ **Shipped 2026-05-20.** `MsgDeactivateDomain` now cascades: every service bound to the dying domain is auto-deactivated and its registration bond refunded in full (overriding `MinServiceLifetimeBlocks` — the chain is killing the service, not the operator), and every open request (PENDING / SUBMITTED / CHALLENGED) on those services is voided via `executeVoidDueToDomain` ([chain/internal/state/resolution.go](../../../chain/internal/state/resolution.go)). Every locked party — requester, provider, challenger, vouchers — gets their stake back. New event `RequestVoided` is emitted per voided request; `DomainDeactivatedPayload` carries the blast-radius counts (`services_deactivated`, `requests_voided`). Terminal-status requests (FINALIZED, SLASHED, REFUNDED) are untouched.
6. **Authority replacement.** The `authority` key is currently a hardcoded dev key. Phase 4 replaces with a 2-of-3 multisig and then a governance module.
7. ~~**Tokenizer canonicalization.**~~ **Shipped 2026-05-20.** The verification domain tuple now includes an optional `tokenizer_id` field; attestations on a domain with a non-empty `TokenizerID` must match it exactly. Legacy domains (empty `TokenizerID`) continue to skip the check for backwards compatibility. See [chain/internal/types/types.go](../../../chain/internal/types/types.go) for the struct and [chain/internal/state/tx.go](../../../chain/internal/state/tx.go) for the check sites (applySubmitResult, applyChallenge, applyVouch).
8. **Cross-machine determinism.** `make determinism-check` is same-machine only. Phase 1 gate requires two-host validation.
9. **Censorship resistance for `MsgChallenge`.** A colluding validator-provider pair can refuse to include a challenger's tx. Mitigation: extend the challenge window past the validator rotation period; or require challenges to be commitable to a separate DA layer.

---

## 8. Mapping spec → code

| Spec section | File(s) |
|---|---|
| §2 actors / bond params | `chain/internal/types/types.go` (`Params`) |
| §3 verification domain | `chain/internal/types/types.go` (`VerificationDomain`); `applyRegisterDomain` in `chain/internal/state/tx.go` |
| §4.2 PENDING → SUBMITTED | `applyRequestInference`, `applySubmitResult` in `chain/internal/state/tx.go` |
| §4.4 CHALLENGED | `applyChallenge` in `chain/internal/state/tx.go` |
| §4.5 vouchers | `applyVouch` in `chain/internal/state/tx.go` |
| §4.6 resolution | `executeSlash`, `executeDismiss` in `chain/internal/state/resolution.go`; `applyResolveChallenge` in `chain/internal/state/tx.go`; `commitBlock` in `chain/internal/state/block.go` |
| §4.7 block producer | `RunBlockProducer` in `chain/internal/state/block.go` |
| §5 attestation | `Attestation`, `CanonicalAttestationBytes` in `chain/internal/types/types.go` and `chain/internal/state/tx.go` |
| §6 bond defaults | `DefaultParams` in `chain/internal/types/types.go` |

---

## 9. References

See `.claude/internal/prior-art/` for individual notes.

- Ora — opML ([ora-opml.md](../prior-art/ora-opml.md))
- Gensyn — Verde ([gensyn-verde.md](../prior-art/gensyn-verde.md))
- Arbitrum Nitro fraud proofs ([arbitrum-nitro.md](../prior-art/arbitrum-nitro.md))
- Optimism Cannon (planned — MIPS-level single-step proofs)
- Inference Labs Sertn (planned — attestation payload comparison)
- Modulus Labs / EZKL (planned — zkML contrast for trust-curve framing)
