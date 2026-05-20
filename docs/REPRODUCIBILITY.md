# Reproducibility Statement

This is the one-page checklist for **independent verification** of the protocol's technical-feasibility claim. Run every step. Observe every output. Disagreement with any expected outcome is a finding — please open an issue with your output captured verbatim.

> If you want to understand *why* each step is what it is, read [`TUTORIAL.md`](TUTORIAL.md) first. This document is the certification checklist; the tutorial is the explanation.

---

## The claim being certified

> A deployable protocol exists in which an off-chain AI inference provider cannot profit from returning a wrong result, without on-chain re-execution of the inference itself, given the presence of at least one honest watcher per service.

Three sub-claims:

1. **Determinism** holds at the cross-process level for the pinned `(model, runtime, hardware, precision, tokenizer)` tuple.
2. **The dispute game** correctly resolves: honest paths finalize; wrong submissions are slashed when challenged; spurious challenges against honest providers are dismissed.
3. **Sybil resistance** is real, not theatre: bonded watchers, registration bonds, and per-domain margins all hold under the documented failure modes.

This document certifies sub-claims 1, 2, 3 with TinyLlama-1.1B-Q4_K_M as the pinned model. Step 1 covers cross-process determinism (same host); **Step 1b covers cross-host determinism across two physically distinct linux/amd64 hosts** — the Phase 1 hard requirement, validated 2026-05-20.

---

## Requirements

| What | Minimum |
|---|---|
| Docker (any) | 20.10+ |
| docker-compose v2 | bundled with modern Docker |
| RAM | 2 GB free |
| Disk | 1 GB free (700 MB for the model + ~300 MB for images) |
| Network | one-time 700 MB download for the model |
| Time | 15 minutes |

No Go, Node, or Python on the host — everything runs in containers.

---

## Pinned configuration

The protocol's claim is only meaningful relative to these pins. Verify they match what you observe.

| Pin | Value |
|---|---|
| Repository commit | `<RECORD GIT SHA AT TIME OF VERIFICATION>` |
| Model file | `tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf` |
| Model SHA-256 | `9fecc3b3cd76bba89d504f29b616eedf7da85b96540e490ca5824d3f7d2776a0` |
| Runtime container | `ghcr.io/ggml-org/llama.cpp:server` |
| Hardware tag | `cpu-x86_64-tinyllama-q4` |
| Precision | `q4_k_m` |
| Tokenizer ID | `llama.cpp-bpe-v1` |
| Sampling | greedy: `temperature=0, top_k=1, top_p=1, seed=0` |
| Threads | 4 |
| Context size | 2048 |
| Chain consensus | Phase 0.5 single-producer goroutine, 1 s block time |
| Block time | 1 second |
| Challenge window | 45 blocks |
| Voucher resolution window | 20 blocks |
| Provider bond | 50 aios |
| Challenger bond | 50 aios |
| Voucher bond | 50% of provider bond (scale BP 5000) |
| Service registration bond | 100 aios |
| Minimum service lifetime | 1000 blocks |
| Voucher margin (global default) | 0 |

Record the commit SHA you verified before running:
```bash
git rev-parse HEAD > /tmp/aios-verified-commit.sha
cat /tmp/aios-verified-commit.sha
```

---

## Step 0 — Bring up the stack

```bash
git clone <repo-url> aios && cd aios
cp .env.example .env
docker compose up -d
```

Wait for healthy state. On the first run, this includes a ~700 MB model download (~30 s on broadband).

```bash
docker compose ps
```

**Expected:** all of `aios-chain`, `aios-llama-server`, `aios-llama-server-b`, `aios-inference-node`, `aios-determinism-harness`, `aios-determinism-harness-b`, `aios-indexer`, `aios-frontend` show `(healthy)`. `aios-model-init` shows `Exited (0)` — it's a one-shot.

---

## Step 1 — Verify cross-process determinism (sub-claim 1, single host)

```bash
make determinism-check
```

**Expected:** six 16-character hashes, all identical. The script prints:
```
All six hashes identical → cross-process determinism holds for this tuple.
```

**What it proves:** the same prompt sent to two independent llama-server processes produces bit-identical output three times each. Cross-process determinism is necessary for the protocol's safety claim.

## Step 1b — Verify cross-host determinism (sub-claim 1, two physical hosts)

```bash
REMOTE_HOST=root@<your-remote-ip> make cross-host-determinism-check
```

Requires SSH key-based access to a second host running Linux. The script:

1. Boots a local `linux/amd64` llama-server container (Rosetta JIT on Apple Silicon; native on x86 Linux/Windows hosts).
2. SCPs `scripts/remote-llama-setup.sh` to the remote, which installs Docker (apt) if missing, downloads + SHA-256-verifies the model, and starts llama-server on `127.0.0.1:8080`.
3. Sends the pinned prompt three times to each side, computes SHA-16 of the output.
4. Asserts all 6 hashes are byte-identical.

**Expected output ends with:**
```
✓ CROSS-HOST DETERMINISM HOLDS
  Hash: 000b7524ab56747f
  Pinned tuple: tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf @ llama.cpp:server @ linux/amd64 @ q4_k_m @ greedy(seed=0)
```

The hash value will be `000b7524ab56747f` for the canonical tuple above. If your run produces a different hash that is still consistent across both hosts, your runtime version or libc may differ — record the discrepancy in the sign-off notes; the determinism claim still holds for *your* configuration.

**Reference run (2026-05-20):**

| Host | CPU | OS | Hash (3 runs each) |
|---|---|---|---|
| Local (laptop) | Apple M3 Pro under linux/amd64 Rosetta JIT | Darwin 25.3.0 (Docker) | `000b7524ab56747f` |
| Remote (cloud VM) | AMD EPYC-Rome native | Ubuntu 26.04 LTS, Linux 7.0.0-15-generic | `000b7524ab56747f` |

All 6 hashes byte-identical. Cross-host determinism holds for the pinned tuple.

**Cross-architecture note (informational):** the same prompt against the local `linux/arm64` llama-server (`docker compose up -d llama-server` on Apple Silicon, port 8080 native) produces a different hash (`a80f29da55264c0a`). This is **expected** and **correct**: arm64 and amd64 are different `hardware_tag` values and therefore different verification domains by protocol design. The cross-architecture divergence empirically validates that `hardware_tag` is a load-bearing field, not decorative.

**What this proves:**

- Sub-claim 1 (determinism) holds across two physically distinct hosts running the same `(model, runtime, hardware_tag, precision, tokenizer)` tuple. This is the Phase 1 hard requirement of the protocol.
- The protocol's `hardware_tag` field correctly discriminates between non-equivalent execution environments (arm64 vs amd64).
- A reviewer with one second host can independently re-run Step 1b and observe the same six matching hashes.

---

## Step 2 — Verify the honest path (sub-claim 2.a)

```bash
make demo
```

**Expected:** the script returns a `request_id` (typically `1`), then `make poll` shows the request transitioning `PENDING → SUBMITTED → FINALIZED` within ~90 seconds.

```bash
curl -s http://localhost:26657/requests/1 | python3 -m json.tool
```

**Expected fields:**
- `status: "FINALIZED"`
- `provider_bond` (50 aios) was locked and released — see emitted `RequestFinalized` event for `provider_bond_returned`
- `paid` field set to the escrow amount (100 aios)

**What it proves:** the protocol's base case works. If this fails, nothing else can be trusted.

---

## Step 3 — Verify a malicious provider is caught (sub-claim 2.b)

```bash
make demo-malicious
```

**Expected:** request transitions `PENDING → SUBMITTED → CHALLENGED → SLASHED`. The harness's `/report` should show `verdict=DIVERGENT` and `challenge_filed=true` for this request.

```bash
curl -s http://localhost:8090/report | python3 -m json.tool
```

**Expected fields:** at least one `item` with `verdict: "DIVERGENT"`, distinct `provider_hash` and `harness_hash`, `challenge_filed: true`.

**Balance check:**
```bash
ALICE=$(curl -s http://localhost:26657/accounts/aios1<alice's-address> | python3 -c "import json,sys; print(json.load(sys.stdin)['balance'])")
```
Alice should have received her escrow refund. The challenger (harness) account should have gained the provider's bond.

**What it proves:** the protocol slashes a wrong submission when an honest watcher catches it. The economic deterrent works.

---

## Step 4 — Verify the voucher defense (sub-claim 3.a)

```bash
make demo-spurious
```

**Expected:** request transitions `PENDING → SUBMITTED → CHALLENGED → FINALIZED` (dismissed). Alice loses 50 aios (her spurious challenger bond); the harness gains 25 (reward) after returning its 25 (voucher bond).

**What it proves:** an honest provider is **not** at the mercy of a malicious challenger. The voucher mechanism protects honest work. Combined with Step 3, this closes both A1 and A2 attack classes.

---

## Step 5 — Verify two independent watchers agree (sub-claim 3.b)

```bash
make demo-multi-watcher
```

**Expected output ends with:**
```
✓ both independent watchers verified the same request
```

The script asserts that both `determinism-harness` (port 8090) and `determinism-harness-b` (port 8091) reported `verdict=OK` for the new request.

**What it proves:** the "two independent honest watchers" precondition for raising per-domain `VoucherMargin` above 0 is operationally achievable in this configuration. The voucher quorum game is not single-actor theatre.

---

## Step 6 — Verify the documented failure mode (falsifiability A)

```bash
make demo-no-watcher
```

**Expected:** request transitions to `FINALIZED` with the **fabricated** `output_hash`. Compare:

```bash
curl -s http://localhost:26657/requests/1 | python3 -c "
import json, sys
d = json.load(sys.stdin)
print('status:', d['status'])
print('output_hash:', d.get('result', {}).get('output_hash', 'n/a'))
"
```

**What it proves:** the protocol does *not* make magical claims. Without an honest watcher in the network, the malicious provider wins. This is the documented failure mode (threat model §A1.1) — observing it is part of certifying that the security claim is *contingent*, not absolute.

After this step, restore the watchers:
```bash
docker compose up -d determinism-harness determinism-harness-b
```

---

## Step 7 — Verify tokenizer pinning is enforced (falsifiability B)

```bash
make demo-tokenizer-mismatch
```

**Expected:** request transitions to `REFUNDED` because the inference-node — restarted with `TOKENIZER_ID=wrong-tokenizer` — couldn't get its `MsgSubmitResult` accepted by the chain. The deadline expires; Alice is refunded.

**What it proves:** tokenizer pinning is a protocol-level enforcement, not a documentation suggestion. The Phase 1 fix for "BPE divergence between runtimes" holds.

Restore:
```bash
TOKENIZER_ID=llama.cpp-bpe-v1 docker compose up -d --force-recreate inference-node
```

---

## Step 8 — Verify CI confirms steps 1–7

Browse to `.github/workflows/demos.yml` in this repo and observe the seven matrix jobs covering steps 1, 2, 3, 4, 5, 6, 7. The CI runs the same demos you just ran on stock Linux runners.

After pushing this checkout to GitHub:
```bash
gh run list --workflow=demos.yml --limit=5
```

**Expected:** the most recent run on the main branch is `completed success`.

**What it proves:** these certifications hold not only on your machine but on independent stock Linux hardware. The protocol's behavior is environment-independent at this level (modulo the cross-host determinism gate, which is the Phase 1 work item).

---

## Final certification

After successful completion of Steps 0–7 (and ideally Step 8), the technical-feasibility MVP is **certified** for the pinned configuration:

- ☐ Step 0 — stack came up healthy
- ☐ Step 1 — six identical hashes (cross-process determinism)
- ☐ Step 1b — cross-host determinism holds (6 identical hashes across two physical amd64 hosts)
- ☐ Step 2 — `FINALIZED` (honest path)
- ☐ Step 3 — `SLASHED` (malicious provider caught)
- ☐ Step 4 — `FINALIZED` via voucher (spurious challenge dismissed)
- ☐ Step 5 — two independent watchers agreed
- ☐ Step 6 — `FINALIZED` with fabricated hash (documented failure mode)
- ☐ Step 7 — `REFUNDED` (tokenizer pinning enforced)
- ☐ Step 8 — CI passes in the project's GitHub Actions

Filling all eight boxes constitutes independent reproduction of the technical-feasibility claim under the pinned configuration. **This does not certify**: cross-host determinism, multi-validator consensus, production-grade authority, large-model support, generative-agent composition, or anything that requires the Phase 4 / Phase 5 work yet to be done. Those are explicitly out of scope per [`TUTORIAL.md`](TUTORIAL.md) §4.

---

## Sign-off

| Field | Value |
|---|---|
| Verifier name | |
| Date (UTC) | |
| Commit SHA verified | |
| Host CPU model | (`uname -m && grep "model name" /proc/cpuinfo \| head -1`) |
| Host OS / kernel | (`uname -srm`) |
| Docker version | (`docker --version`) |
| All eight boxes ticked? | yes / no |
| Notes / deviations | |
| Signature | |

Save the completed sign-off alongside a transcript of the commands and outputs you ran. That artifact is what makes the certification independently auditable.

---

## What to do if a step fails

A failure is **informative**. It either points at a real protocol regression (in which case the project owner needs to know), or at an environmental factor that the protocol depends on (in which case the operational guidance needs updating).

Investigation order:
1. `docker compose ps` — is any service unhealthy?
2. `docker compose logs <service>` — what does the failing service say?
3. `curl -s http://localhost:26657/requests/<id>` — full request state, including challenges and vouchers.
4. `curl -s http://localhost:8090/report` and `curl -s http://localhost:8091/report` — what verdict did each harness reach?
5. Compare against `.github/workflows/demos.yml` outcome for the same step — if CI passes but local fails, the divergence points at a host-specific factor.
6. Open an issue with the transcript and the sign-off fields above.

---

## Open audit items

The following are known limitations of this checkout that an auditor should *not* attempt to certify:

1. **Cross-host determinism.** Out of scope of this document. To certify it, run Steps 1, 2, 3 on a second physical host or cloud VM with a different CPU model, compare `output_hash` values across hosts, and add the comparison to your sign-off notes.
2. **Censorship resistance.** With a single-producer chain, the protocol assumes the producer doesn't censor `MsgChallenge`. A multi-host adversarial test is out of scope of this document.
3. **Authority key compromise.** The dev key in `EnsureDevKeyring` is exposed in `.aid/keys.json` inside the container. Treat the local devnet as compromised by definition; production guidance requires multisig (Phase 4).
4. **Long-running soak.** Steps 1–7 are single-request validations. A long-running soak (100+ requests, mixed honest/malicious/spurious) is a Phase 2 follow-up.

If any of (1)–(4) matter for the use case you're evaluating, that's a Phase-level question for the project owner, not a defect in this checkout.
