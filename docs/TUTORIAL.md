# Tutorial — For a Skeptical Reviewer

This document is written for someone who reads the repo and wants to **verify the claim**, not just understand the codebase. It is the English counterpart of [`TUTORIAL.ru.md`](TUTORIAL.ru.md), restructured around falsifiability rather than narrative.

Read it in order. By the end you will have run six demos that, together, exercise every load-bearing claim of the protocol — including the failure modes.

> **Companion document:** [`REPRODUCIBILITY.md`](REPRODUCIBILITY.md) is the one-page checklist with pinned commit, model hashes, expected outputs. Use this tutorial to understand *why* the protocol works the way it does; use `REPRODUCIBILITY.md` to certify a given checkout.

---

## 1. The claim

> **There exists a deployable protocol in which an off-chain AI inference provider cannot profit from returning a wrong result, without on-chain re-execution of the inference itself, given a single honest watcher per service.**

Three sub-claims sit underneath:

1. **Determinism holds.** The same `(model, runtime, hardware, precision, tokenizer)` tuple produces bit-identical output hashes across independent runs. (Currently verified cross-process; cross-host is the Phase 1 gate.)
2. **The dispute game is economically sound.** A malicious provider's expected loss exceeds their expected gain at the protocol's default bond ratios.
3. **The defense is sybil-resistant.** A bonded sybil voucher cannot cheaply tip a dispute — they must commit real capital that is either time-locked or forfeit.

A reviewer's job is to test whether each sub-claim holds under both the success path and the documented failure path.

---

## 2. Verifying the claim in 15 minutes

```bash
git clone <repo-url> aios && cd aios
cp .env.example .env
docker compose up -d   # first time: ~700MB model download, ~5 min wait
```

Wait until `make ps` shows every service as `healthy`. Then run, in order:

```bash
make demo                     # ~90s — sub-claim 2 (honest path)
make demo-malicious           # ~90s — sub-claim 2 (caught cheat)
make demo-spurious            # ~90s — sub-claim 3 (sybil-resistant voucher)
make demo-multi-watcher       # ~30s — confirms two independent watchers agree
make demo-no-watcher          # ~90s — falsifiability: documented failure mode
make demo-tokenizer-mismatch  # ~180s — falsifiability: pinning is enforced
make determinism-check        # ~30s — sub-claim 1 (cross-process)
```

Section §3 below explains what each one should produce. If any of them deviates from the documented outcome, the corresponding sub-claim is broken, and the project owner wants to know about it.

---

## 3. What you should see and why

### 3.1 `make demo` — honest path

**Setup.** Alice posts a request paying 100 aios. Bob (honest provider) runs TinyLlama, produces an output, posts `MsgSubmitResult` with a signed attestation, locking 50 aios as his provider bond.

**Expected end state**: `status=FINALIZED`. The on-chain bookkeeping should show:
- Escrow (100 aios) credited to Bob.
- Provider bond (50 aios) returned to Bob.
- No challenge filed.

**What this tests.** The "no one cheated, no one challenged" base case. If this fails, nothing else can be trusted.

### 3.2 `make demo-malicious` — caught cheat

**Setup.** The bundled `determinism-harness` is running and watching. The inference-node is restarted with `MALICIOUS_PROVIDER=1`, which makes it return a fabricated output instead of the real one. Bob still signs the attestation, putting his name on the lie.

**Expected end state**: `status=SLASHED`. Specifically:
- The harness independently ran the inference, got a different output hash, and filed `MsgChallenge` with its own attestation.
- After the resolution window expired with no successful voucher defense for Bob, the chain transitioned `CHALLENGED → SLASHED`.
- Bob's bond (50) was transferred to the harness.
- Alice's escrow (100) was returned to her.

**What this tests.** The protocol can catch a wrong answer when there's at least one honest watcher. The `harness-report` endpoint should show `verdict=DIVERGENT` for this request. If you see `FINALIZED` instead, the threat model's main attack (A1.1) is undefended in this deployment.

### 3.3 `make demo-spurious` — sybil-resistant voucher

**Setup.** Bob is honest again. Alice files a **spurious challenge** — she ran no inference, she just posts a random `output_hash` claiming it differs from Bob's. (In a normal Phase 3.x deployment, this would slash Bob unfairly.)

**Expected end state**: `status=FINALIZED`. Specifically:
- The harness independently verified Bob's result, sees no divergence, files `MsgVouch` taking Bob's side.
- After the resolution window, vouchers tally: 1 provider-side (harness), 0 challenger-side.
- With default `VoucherMargin=0`, that's `1 ≥ 0 → DISMISS`.
- Alice's challenger bond (50) is forfeit (split between Bob and the harness as reward).
- Bob keeps his escrow (100) + bond (50 returned).
- Harness gets its voucher bond (25) back + reward share (25).

**What this tests.** A grief attack against an honest provider fails when an honest watcher exists. This is the Phase 3.y deliverable that closed attack A2.1.

### 3.4 `make demo-multi-watcher` — two independent watchers agree

**Setup.** Both `determinism-harness` (port 8090) and `determinism-harness-b` (port 8091) are running. They have separate chain keys, separate llama-server backends (`llama-server` and `llama-server-b`), and separately registered witness services in the domain.

**Expected output.** Both harness `/report` endpoints show `verdict=OK` for the same `request_id`. The shell assertion in the Makefile target enforces this.

**What this tests.** The protocol's "honest majority of watchers" assumption is no longer hypothetical — two independent processes with disjoint state both verified the same inference and agreed. This is what unlocks per-domain `VoucherMargin > 0` in production: when two watchers exist, the dismiss rule can require a 1-vote margin without breaking the demo.

### 3.5 `make demo-no-watcher` — falsifiability: documented failure mode

**Setup.** Both harnesses are **stopped** (`docker compose stop determinism-harness determinism-harness-b`). The inference-node is restarted with `MALICIOUS_PROVIDER=1`. Bob fabricates an output.

**Expected end state**: `status=FINALIZED` with the **fabricated** `output_hash`. The protocol degrades exactly as the threat model (§A1.1) predicts: with no honest watcher, a malicious provider gets paid for a wrong result.

**What this tests.** The protocol does **not** make magical claims. Its security is contingent on the watcher assumption being satisfied operationally. This demo is here to make that contingency *visible* — a reviewer who runs it sees what the failure looks like and can audit operational guidance accordingly.

When you're done, restart the harnesses:
```bash
docker compose up -d determinism-harness determinism-harness-b
```

### 3.6 `make demo-tokenizer-mismatch` — falsifiability: pinning is enforced

**Setup.** The chain's registered domain pins `tokenizer_id = "llama.cpp-bpe-v1"`. The inference-node is restarted with `TOKENIZER_ID=wrong-tokenizer`. Now the provider's attestation declares a tokenizer that doesn't match the domain.

**Expected end state**: `status=REFUNDED`. The chain rejected `MsgSubmitResult` with `ErrAttestationDomainMismatch`. Bob could never submit a result; Alice's request hit the deadline and her escrow was refunded.

**What this tests.** Tokenizer pinning is real, not advisory. The Phase 1 fix for the "different BPE implementations diverging" problem (verification-protocol.md §7.7) holds. If this demo produced `FINALIZED`, tokenizer pinning would be a documentation artifact and the determinism claim would be unsafe.

When you're done, restore:
```bash
TOKENIZER_ID=llama.cpp-bpe-v1 docker compose up -d --force-recreate inference-node
```

### 3.7 `make determinism-check` — cross-process determinism

**Setup.** Two independent `llama-server` containers, both running TinyLlama 1.1B Q4_K_M with `--threads 4 --temp 0`. The same prompt is sent to each three times, and SHA-256 of the output is computed.

**Expected output.** Six identical 16-character hashes printed at the end, followed by `All six hashes identical → cross-process determinism holds for this tuple.`

**What this tests.** Sub-claim 1 (determinism) holds at the cross-process level on this hardware. Note: this is **necessary but not sufficient** — true cross-host determinism requires running on two physical machines (see §6 below).

---

## 4. What is *not* being claimed

Scope discipline matters for a reviewer. None of the following are claimed by this checkout:

| Not claimed | Why not | When |
|---|---|---|
| Production-ready security | No external audit. Single dev key as authority. Single-node consensus. | Phase 4 |
| Cross-host determinism | The harness only verifies cross-process on one machine. Two physical hosts has not been done in this repo. | Phase 1 hard requirement |
| Multi-validator consensus | The chain is a single Go goroutine producing blocks. Censorship attacks (A4.1) are undefended. | Phase 1, via CometBFT migration |
| Verified-runtime executor | The inference path uses `llama_http` (`llama-server` over HTTP). The code carries a banner labelling this as "real inference, unverified" — a deliberate Phase 0.5 honesty marker. | Phase 1, via in-process runtime |
| Large-model support | TinyLlama 1.1B is the only qualified model. Determinism for 7B+ models has not been demonstrated on any hardware class. | Phase 4 |
| Generative agents | The `agents/` directory exists but is empty. | Phase 4 |
| CosmWasm extensions | The `contracts/` directory exists but is empty. | Phase 5 |
| Token economics with monetary value | `aios` is a test denom. No emissions, no fees, no validator-set economics. | Phase 4 |

If a reviewer sees marketing copy elsewhere claiming any of the above, treat it as forward-looking commentary, not as the technical-feasibility MVP this checkout demonstrates.

---

## 5. Reading the code

After running the demos, the natural next step is to verify that the on-chain logic actually does what §3 describes. The shortest reading order:

1. **State machine — `chain/internal/state/tx.go`.**
   - `applyRegisterDomain` — domain registration, including tokenizer-pin field.
   - `applyRegisterService` — service registration with `Params.ServiceRegistrationBond` (100 aios) locked.
   - `applyRequestInference` — escrow lock, `PENDING` status.
   - `applySubmitResult` — provider bond lock + tuple match (model/runtime/hardware/precision/**tokenizer**).
   - `applyChallenge` — challenger bond lock + tuple match against the same domain.
   - `applyVouch` — voucher bond (scaled by `VoucherBondScaleBP`), eligibility check (`voucherEligible` requires active service in domain).
   - `applyDeactivateService` — bond refund only past `MinServiceLifetimeBlocks`; otherwise routes to treasury.
   - `applyDeactivateDomain` — cascade: deactivate services + refund their bonds in full + void all open requests.
   - `applyResolveChallenge` — authority override path.

2. **Resolution helpers — `chain/internal/state/resolution.go`.**
   - `executeSlash` — slash path; provider bond → challenger; losing voucher bonds → treasury.
   - `executeDismiss` — dismiss path; reward pool composed of forfeit bonds; distributed to provider + winning vouchers.
   - `executeVoidDueToDomain` — domain-death cascade; everyone gets their stake back.

3. **Block producer — `chain/internal/state/block.go`.**
   - `commitBlock` — time-driven transitions: `PENDING` past deadline → `REFUNDED`; `SUBMITTED` past challenge window → `FINALIZED`; `CHALLENGED` past resolution window → `executeSlash` or `executeDismiss` based on per-domain `VoucherMargin`.

4. **Off-chain components.**
   - `inference-node/cmd/inferenced/main.go` — watcher → executor → submitter; constructs the `Attestation` struct including `TokenizerID` from env var.
   - `determinism-harness/cmd/harness/main.go` — independent re-runner; files `MsgChallenge` on divergence or `MsgVouch` defending honest providers.

5. **Tests as executable specifications.**
   - `chain/internal/state/dispute_integration_test.go` — full SUBMITTED→CHALLENGED→VOUCH→{DISMISSED, SLASHED, REFUNDED} flows with exact balance assertions.
   - `chain/internal/state/registration_bond_test.go` — bond lock/refund/forfeit math.
   - `chain/internal/state/tokenizer_pinning_test.go` — 7 cases covering match/mismatch/legacy.
   - `chain/internal/state/domain_voiding_test.go` — cascade refunds across all open request statuses.
   - `chain/internal/state/phase_3z_345_test.go` — per-domain margin + scaling + treasury sweep + immediate-finalize bug fix.

If a demo behaves unexpectedly, these tests are the second source of truth.

---

## 6. The pinned configuration

The protocol's claim is only meaningful relative to a specific configuration. The version of the project at this checkout pins:

| Pinning | Value |
|---|---|
| Model | `TinyLlama-1.1B-Chat-v1.0-GGUF` Q4_K_M, SHA-256 `9fecc3b3cd76bba89d504f29b616eedf7da85b96540e490ca5824d3f7d2776a0` |
| Runtime | `ghcr.io/ggml-org/llama.cpp:server` (latest at build time; pin to a specific tag for production) |
| Hardware tag | `cpu-x86_64-tinyllama-q4` |
| Precision | `q4_k_m` |
| Tokenizer ID | `llama.cpp-bpe-v1` |
| Sampling | greedy: `temperature=0, top_k=1, top_p=1, seed=0` |
| Chain consensus | single-producer Go goroutine, 1s block time (Phase 0.5; CometBFT in Phase 1) |
| Provider bond | 50 aios |
| Challenger bond | 50 aios |
| Voucher bond | 50% of provider bond (scaled via `VoucherBondScaleBP=5000`) |
| Service registration bond | 100 aios |
| Minimum service lifetime | 1000 blocks (~17 min) |
| Challenge window | 45 blocks (~45s) |
| Voucher resolution window | 20 blocks (~20s) |
| Per-domain voucher margin | 0 (the demo domain inherits the global default) |

If you want to verify the claim under different parameters (e.g. `VoucherMargin=2` for stricter sybil resistance with multi-watcher), re-register the domain with the new value — see `MsgRegisterDomain.VoucherMargin` documented in [`docs/data-model.md`](data-model.md).

---

## 7. Threat coverage

The threat model lives in `.claude/internal/protocol/threat-model.md`. Each enumerated attack has either a shipped mitigation or an explicit "this is the residual risk" disclosure. As of this checkout:

| Attack ID | Description | Status |
|---|---|---|
| A1.1 | Submit wrong output, no challenger watches | Mitigated by honest watcher assumption + bond economics; falsifiable via `make demo-no-watcher` |
| A1.2 | Submit at deadline-edge to escape challenge window | Mitigated: window measured from inclusion height, not wall clock |
| A1.3 | Provide "close-enough" output | Precluded: chain compares hash bytes, not semantic similarity |
| A2.1 | Spurious challenge griefing | Mitigated: voucher mechanism + falsifiable via `make demo-spurious` |
| A2.2 | Chained spurious challenges across services | Mitigated by voucher mechanism + economic loss on dismissal |
| A3.1 | Provider+challenger collusion | Partial: zero net cost to attacker but no profit; documented residual risk; full mitigation requires explicit `disputeFee` |
| A3.2 | Sybil voucher | Mitigated through 4 layers: eligibility (step 1), registration bond (step 2), per-domain margin (step 3), scaled voucher bond (step 4) |
| A4.1 | Validator-level censorship of `MsgChallenge` | **Undefended** in Phase 0.5 (single producer); Phase 1 CometBFT mitigates via standard consensus |
| A4.2 | SSE drop → harness never sees event | Mitigated by reconciler in harness; documented operational requirement |
| A5.1 | Authority key compromise | **Documented critical centralization**; mitigation is Phase 4 multisig→governance |
| A6.1 | Determinism break post-domain-registration | Mitigated via `MsgDeactivateDomain` cascade — all open requests refunded, all locked bonds returned |

Three attacks remain only partially mitigated: A3.1 (collusion), A4.1 (single-producer censorship), A5.1 (authority centralization). Phase 4 closes A4.1 (CometBFT) and A5.1 (governance). A3.1 is fundamentally hard and tracked as an open problem.

---

## 8. Open problems

The complete list lives in `.claude/internal/protocol/verification-protocol.md` §7. The most consequential remaining items, in order of severity:

1. **Cross-host determinism not yet validated.** The current proof is cross-process on one machine. CPU microcode, libc, kernel scheduler may all differ between hosts. The Phase 1 gate explicitly requires two-host validation before any domain is treated as production-grade.
2. **Federated re-execution committee not implemented.** ADR-0004 commits to the design (chosen over Nitro-style bisection, which doesn't fit AI inference). The runtime is Phase 4 work. Until then, all disputes resolve via vouchers; if vouchers tie or are missing, the authority can resolve manually.
3. **Authority is a single dev key.** Phase 4 replaces with multisig → governance. Current operational guidance is to treat authority compromise as catastrophic.
4. **Forfeit bond treasury has no withdrawal mechanism yet.** Bonds accumulate in `ModuleTreasuryAddr` indefinitely. A future ADR routes withdrawals to governance / public-goods / burn.
5. **Tokenizer fingerprinting beyond a string identifier.** Currently `tokenizer_id` is a free-form string the operator picks. A stronger version would be a hash of the actual tokenizer vocab + merges + special-token rules. Phase 4 work.

If any of these prevents a reviewer from believing sub-claim 1, 2, or 3, that's an honest debate to have with the project owner.

---

## 9. What to do if a demo fails

The order of investigation:

1. **Check `make ps`.** All services should be `(healthy)`. If a service is `(unhealthy)`, read its logs with `docker compose logs <service>` and check whether the host has the resources.
2. **Check `make logs`** for any panics or errors. The chain should not panic in normal operation; if it does, capture the panic + a reproduction command and open an issue.
3. **Check the request payload.** `curl -s http://localhost:26657/requests/1 | jq` shows the full request including challenges, vouchers, and result. The `events` SSE stream at `/events` shows every state transition.
4. **Check the harness reports.**
   - `curl -s http://localhost:8090/report | jq` — harness A
   - `curl -s http://localhost:8091/report | jq` — harness B
   Each `item` shows `verdict` (`OK` / `DIVERGENT` / `ERROR`), the input hash, both output hashes, and whether a challenge or vouch was filed.
5. **Reset and retry.** `make reset` removes all volumes (including the model cache; first re-run re-downloads). `make demo` rebuilds from scratch.

If a demo's *expected outcome* differs from the documented one, that's a finding worth surfacing to the project owner — it means either the documentation is stale or the implementation regressed. The CI workflow `.github/workflows/demos.yml` is supposed to catch this; if your demos diverge from CI, identify the host-specific factor (CPU model, kernel, libc) and report it.

---

## 10. Bottom line

The Phase 3.z + Phase 1 (partial) checkout demonstrates:

- ✅ A working dispute game with provider, challenger, and voucher economics.
- ✅ Sybil resistance through four layered mechanisms with measurable costs.
- ✅ Falsifiable failure modes that match the threat model.
- ✅ Cross-process determinism on the demo tuple.
- ✅ Tokenizer pinning enforced at the protocol level.
- ✅ Two independent watchers running in default config (not theatre).
- ✅ End-to-end CI that exercises every documented outcome.

It does **not** yet demonstrate:

- ❌ Cross-host determinism (Phase 1 gate; requires a second physical host).
- ❌ Multi-validator consensus / censorship resistance (Phase 1, CometBFT).
- ❌ Production-grade authority (Phase 4 governance).

If a reviewer can run §2's seven demos and observe the seven outcomes documented in §3, the **technical feasibility** of the protocol is established for the small-model case. Production readiness is a different question — see §4 and §8 for what stands between this checkout and a value-bearing testnet.
