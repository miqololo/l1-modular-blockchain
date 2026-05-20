# Decentralized AI Operating System — Project Rules

> First decentralized AI OS: modular L1 blockchain + verifiable AI inference + generative agents + unified service marketplace.

This file is auto-loaded into every Claude Code conversation in this repo. **Read it fully before any non-trivial change.**

---

## 1. Mission & non-negotiables

- **MVP goal**: prove **technical feasibility** of optimistic fraud proofs for AI inference on a modular L1.
- **Verification approach**: optimistic + fraud proofs. Not TEE. Not zkML. Not reputation. The verification protocol is the load-bearing innovation.
- **Determinism is the foundation**: if inference cannot be reproduced bit-exact, the fraud proof model collapses. Every inference-touching change must preserve determinism guarantees defined in [.claude/internal/protocol/verification-protocol.md](.claude/internal/protocol/verification-protocol.md).
- **No hosted-API inference in the verification path**. Groq / Together / HuggingFace Inference API are non-deterministic and forbidden as substrate for fraud-provable inference. They may be used for non-verified demo UX only, clearly labeled.

## 2. Repo layout

```
chain/                  Cosmos SDK L1 (Go). Custom aiservice module.
inference-node/         Off-chain inference worker (Go). Produces signed (input, output, attestation).
contracts/              CosmWasm contracts (Rust). Deferred until phase 5.
frontend/               Next.js + Keplr/Leap. Marketplace UI.
indexer/                Postgres indexer (Go). Reads chain events, exposes REST/GraphQL.
determinism-harness/    Phase 0 experiment. Validates bit-exact reproducibility before chain work.
proto/                  Shared protobuf schemas. Single source of truth for chain ↔ off-chain types.
docs/                   Public integration docs (getting-started, API refs, integration guides).
scripts/                Dev/CI scripts.
.claude/                Claude config + internal dev docs (this file's home).
.claude/internal/       Internal-only docs: PHASE.md, ADRs, prior-art, protocol drafts.
```

Each package has its own `CLAUDE.md` with package-specific rules. **Read the package `CLAUDE.md` before editing files in that package.**

Public-facing integration docs live in `docs/`. Internal dev docs (phase tracker, ADRs, protocol design drafts, prior-art research) live in `.claude/internal/` — they describe how the work is organized, not how external developers integrate. Keep them split: integrators don't need to read ADRs; engineers don't need to wade through getting-started guides.

## 3. Phase gate — what is allowed to be built right now

The project is divided into phases. **Work outside the current phase is rejected.** Current phase is recorded in [.claude/internal/PHASE.md](.claude/internal/PHASE.md).

| Phase | Allowed work | Gate to next |
|---|---|---|
| 0 — Protocol & determinism (**parallel track**) | Spec writing, determinism harness, prior-art notes. **No longer blocks chain code.** | Bit-exact reproducibility demonstrated on 2+ machines (continues during 0.5+) |
| **0.5 — Demo slice** | Chain (`x/aiservice` with Register/Request/Submit + immediate finalize) + inference-node (real LLM via llama-server sidecar, clearly labeled *unverified*) + indexer (Postgres + REST) + frontend (Next.js + Keplr) + docker-compose. TDD enforced. | `docker compose up` boots the full stack; demo runs end-to-end; tutorial written |
| 1 — Reference inference node (verified path) | Replace `llama_http` executor with a deterministic-runtime executor; first verification domain qualified; attestation payload schema | Two independent nodes produce identical signatures on identical inputs |
| 2 — Marketplace hardening | Service registry hardening, payment flows, 100-request soak test | Single-node devnet completes 100 requests without state corruption |
| 3 — Challenge & fraud proofs | Dispute object, bisection/re-execution game, slashing | Successful challenge of a malicious node on devnet |
| 4 — Public testnet + agents-engineer scope opens | Generative agents, expanded analytics dashboards | — |
| 5 — Celestia DA + CosmWasm extensions | DA migration, third-party extension contracts | — |

If a task spans phases, stop and surface the conflict. Do not pre-build.

## 4. TDD is mandatory

**Every behavioral change is test-first.** No exceptions for "small" changes — small changes are the most common source of regressions.

### The cycle (red → green → refactor)

1. **Red**: write the smallest failing test that captures the new behavior. Run it. Confirm it fails for the *expected reason* (not a syntax error or missing import).
2. **Green**: write the minimum production code to make it pass. No extra features, no speculative abstractions.
3. **Refactor**: improve names, remove duplication, tighten types. Tests must still pass.
4. **Commit**: one logical change per commit. Commit message states behavior delta.

Use the `/tdd-cycle` skill to walk through this with discipline on a non-trivial change.

### What requires a test

| Change type | Test required? |
|---|---|
| New chain message handler | **Yes** — unit + integration |
| New keeper method touching state | **Yes** — unit on keeper |
| Bug fix | **Yes** — regression test that fails before the fix |
| Inference pipeline change | **Yes** — determinism test on golden inputs |
| Protobuf schema change | **Yes** — round-trip test |
| Refactor with no behavior change | No new test, but existing suite must pass unchanged |
| Doc/spec change | No |
| Dependency bump | Existing suite must pass |

### What "real test" means

- **Real database, real chain, real model weights.** No mocks for the system under test's own dependencies. Mocks are allowed only at the *outer* system boundary (e.g. stubbing a hosted API in a unit test of a caller).
- Integration tests run against a fresh devnet spun up by the test, not a long-lived shared one.
- Determinism tests pin model weights by hash, seed by value, precision by enum, hardware by tag. A determinism test that doesn't pin all four is invalid.

### Coverage expectations

We do **not** chase coverage percentage. We require:
- 100% of message handlers have integration tests
- 100% of consensus-affecting code paths have unit tests
- 100% of fraud-proof dispute paths have end-to-end tests once phase 3 starts
- Every bug fix adds a regression test

## 5. Coding rules

### General
- **No premature abstraction.** Three repetitions before you extract. Two similar functions are fine.
- **No backwards-compat shims** until we have external users (i.e. post-mainnet). Just change the code.
- **No feature flags** for incomplete features. Use a branch.
- **No dead code, no commented-out code, no `// TODO: removed for X`.** Delete it.
- **Comments are rare and explain WHY**, not WHAT. If you can name the function/variable better instead, do that.
- **Errors propagate explicitly.** No silent catches, no fallback values that mask failures, no "if it errors, just return empty". If a caller wants a fallback, the *caller* implements it.
- **Validate at boundaries only.** User input, external APIs, untrusted chain messages = validate. Internal function calls between our own code = trust the types.

### Go (chain, inference-node, indexer)
- `gofmt` + `goimports` + `golangci-lint run` must pass.
- Errors wrapped with `fmt.Errorf("doing X: %w", err)`. No bare returns of foreign errors.
- No `panic` outside of `init()` and genuine invariant violations. Especially never panic in consensus paths.
- Table-driven tests. One `t.Run(name, ...)` per case.
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal. Prefer `require` in setup, `assert` in checks.
- Cosmos SDK conventions: messages in `types/msgs.go`, keeper methods in `keeper/`, msg handlers thin, business logic in keeper.

### Rust (contracts, possibly inference-node)
- `cargo fmt` + `cargo clippy -- -D warnings` must pass.
- No `unwrap()` or `expect()` in production code. Tests may use `unwrap` for brevity.
- Errors via `thiserror`. `anyhow` only at binary boundaries.
- `#[cfg(test)]` mod tests at bottom of file for unit tests.

### TypeScript (frontend)
- `tsc --noEmit` + `eslint` + `prettier` must pass.
- `strict: true` in tsconfig. No `any`. No `@ts-ignore` without a comment explaining the exact reason.
- Functional React components, hooks. No class components.
- Tests with Vitest + React Testing Library. Test behavior, not implementation.

### Python (determinism-harness only)
- `ruff` + `black` + `mypy --strict` must pass.
- Type hints on all signatures.
- `pytest` for tests. Determinism tests run twice in CI to catch flakes.

## 6. Determinism rules (special)

Anything touching model inference must:
1. **Pin the model** by SHA-256 of weights file. Loading by HF model ID alone is forbidden in the verification path.
2. **Pin the runtime** by exact version (vLLM, llama.cpp, candle — choose one per ADR-0003).
3. **Pin sampling** to greedy (temperature=0, top_k=1) for verifiable inference. Sampling modes are a future research item.
4. **Pin numerics**: precision enum (FP32 / BF16 / FP16 / INT8), no mixed-precision.
5. **Pin hardware class** by tag (e.g. `nvidia-a100-80gb`, `nvidia-h100-80gb`). Different hardware classes are *different verification domains*.
6. **Record everything** in the attestation: model hash, runtime version, precision, hardware tag, seed, input hash, output hash, timestamp.

If you cannot satisfy 1–6, the code is not allowed in the verification path. It can live in a clearly-labeled `unverified/` subfolder.

## 7. Security rules

- No `unsafe` Rust without an ADR.
- No `cgo` in chain code without an ADR. Determinism risk.
- No `rand` in consensus paths. Use the chain's seed.
- No file I/O in keeper methods. State only.
- No external network calls in keeper methods.
- Protobuf changes require backwards-compatibility analysis once we hit phase 4 (testnet).
- All new dependencies require a one-line justification in the PR.

## 8. Working with Claude in this repo

### Subagents
Specialized agents live in `.claude/agents/`. Use them when the task matches:
- **cosmos-engineer** — Cosmos SDK / CometBFT / Go module work
- **cosmwasm-engineer** — CosmWasm / Rust contract work
- **ml-determinism-engineer** — Inference reproducibility, model pinning, attestations
- **protocol-architect** — Verification protocol design, fraud proof games, economic security
- **tdd-enforcer** — Reviews changes for test-first discipline before they're committed

### Skills
Reusable workflows live in `.claude/skills/`:
- **tdd-cycle** — Walk red→green→refactor for a specific change
- **new-package** — Scaffold a new package with conventions
- **verify-determinism** — Run the determinism harness against a model+runtime combo

### Slash commands
- `/spec <topic>` — Draft or update a protocol spec section
- `/test-all` — Run the full cross-language test suite

### Memory
Per-conversation memory is in `~/.claude/projects/<this-dir-encoded>/memory/`. Project-wide facts go here (.claude/CLAUDE.md), conversation-specific things stay in memory.

## 9. Definition of done

A change is **not done** until:
- [ ] Tests written first, all pass locally
- [ ] Linters pass for the languages touched
- [ ] No new dead code, no commented-out code
- [ ] Determinism guarantees preserved (if inference-touching)
- [ ] Phase-gate check: the change belongs in the current phase
- [ ] .claude/CLAUDE.md / package .claude/CLAUDE.md updated if conventions changed
- [ ] One logical commit with a message stating the behavior delta

If you're unsure whether something is "done," it isn't.

## 10. Anti-patterns — instant red flags

- "I'll add a TODO and come back to it" → no. Either do it now or open an issue.
- "Let me mock the database for this test" → no. Use a real one.
- "This should never happen, but just in case…" → delete the code.
- "I'll add a feature flag so we can ship it half-done" → no.
- "Let me also clean up these unrelated files while I'm here" → no. Separate PR.
- "The test is flaky, let me add a retry" → no. Find the race.
- "I'll skip the test for now because the harness is slow" → no. Speed up the harness.
- "Let me add a fallback in case the model API is down" → in verification path: no. Fail loud.
