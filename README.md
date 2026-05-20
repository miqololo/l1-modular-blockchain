# Decentralized AI Operating System

[![lint](https://github.com/OWNER/REPO/actions/workflows/lint.yml/badge.svg)](https://github.com/OWNER/REPO/actions/workflows/lint.yml)
[![test](https://github.com/OWNER/REPO/actions/workflows/test.yml/badge.svg)](https://github.com/OWNER/REPO/actions/workflows/test.yml)
[![demos](https://github.com/OWNER/REPO/actions/workflows/demos.yml/badge.svg)](https://github.com/OWNER/REPO/actions/workflows/demos.yml)

A modular L1 marketplace where AI inference is a first-class on-chain service.
Real signatures, real state machine, real off-chain LLM execution, all wired
together as a marketplace.

> First decentralized AI OS: L1 + verifiable AI + generative agents + unified service marketplace.

> **CI badges**: replace `OWNER/REPO` above with the GitHub org/repo path after the first push to GitHub. The three workflows (`lint`, `test`, `demos`) exercise the full Phase 3.z protocol on every commit — see [Continuous integration](#continuous-integration) below for what each one proves.

---

## Run it

```bash
git clone <this-repo> aios
cd aios
make demo
```

First boot downloads TinyLlama-1.1B-Chat (~700 MB) and brings up seven containers. After ~3–6 minutes:

- **Marketplace UI**:      <http://localhost:3030>
- **Chain HTTP**:          <http://localhost:26657>
- **Indexer REST**:        <http://localhost:8081>
- **Harness verdicts**:    <http://localhost:8090/report>

`make demo` seeds a verification domain + service and submits a demo inference request. The bundled `determinism-harness` watches the request finalize, re-runs it independently, and reports whether the provider's `output_hash` matches its own re-run.

```bash
make poll              # poll until request 1 finalizes
make harness-report    # see the harness verdict
```

See [docs/getting-started.md](docs/getting-started.md) for the full quickstart.

## Test it

The fastest test is the end-to-end demo itself:

```bash
make demo            # bring up + seed + request
make poll            # wait until the inference finalizes
```

Output ends with `[attempt N] status=FINALIZED` when the LLM has produced a result and the chain has paid the provider.

Per-component tests live with their source. Each runs inside Docker (no host Go/Node required):

```bash
# Chain state machine tests
docker run --rm -v "$PWD/chain:/src" -w /src golang:1.22.5-alpine \
  sh -c "go mod tidy && go test ./..."

# Indexer store tests
docker run --rm -v "$PWD/indexer:/src" -w /src golang:1.22.5-alpine \
  sh -c "go mod tidy && go test ./..."

# Inference-node executor tests
docker run --rm -v "$PWD/inference-node:/src" -w /src golang:1.22.5-alpine \
  sh -c "go mod tidy && go test ./..."
```

## Continuous integration

Three GitHub Actions workflows run on every push and pull request:

| Workflow | What it proves | Time |
|---|---|---|
| [`lint`](.github/workflows/lint.yml) | `gofmt`, `go vet`, `golangci-lint` clean across `chain`, `inference-node`, `indexer`, `determinism-harness` | ~1 min |
| [`test`](.github/workflows/test.yml) | `go test -race ./...` passes in every package, including the integration tests for the dispute game (`dispute_integration_test.go`, `domain_voiding_test.go`, `phase_3z_345_test.go`, `tokenizer_pinning_test.go`) | ~2 min |
| [`demos`](.github/workflows/demos.yml) | Full docker-compose stack exercises each documented outcome of the protocol — the same demos a reviewer runs locally, asserted programmatically | ~15 min |

The `demos` workflow has seven matrix jobs:

1. **honest** — `make demo` → `make poll` ends in `FINALIZED`
2. **malicious** — `make demo-malicious` → `make poll` ends in `SLASHED`
3. **spurious** — `make demo-spurious` → request ends in `FINALIZED` (challenge dismissed)
4. **no-watcher** (falsifiability) — both harnesses stopped, malicious provider's wrong-hash output finalizes — the threat model's documented failure mode
5. **tokenizer-mismatch** (falsifiability) — provider with wrong `TOKENIZER_ID` is rejected at `MsgSubmitResult`, request times out into `REFUNDED`
6. **determinism-check** — same prompt × 2 llama-server processes × 3 runs = 6 identical SHA-16 hashes (asserts cross-process determinism on the demo tuple)
7. **multi-watcher** — both `determinism-harness` and `determinism-harness-b` independently verify the same request and agree

A green badge means **all seven of these outcomes hold on stock Linux runners with the pinned model + runtime**. A red badge points at the specific demo that regressed, with container logs and the chain's request payload dumped into the run output.

The CI helper [scripts/ci-assert-status.sh](scripts/ci-assert-status.sh) is the same polling primitive each job uses; it's safe to invoke from any local shell too.

## Integrate

The integration manual lives in **[docs/](docs/README.md)**. Start there if you want to:

- Register a new AI service → [docs/integrate-a-service.md](docs/integrate-a-service.md)
- Run your own inference worker → [docs/integrate-an-inference-node.md](docs/integrate-an-inference-node.md)
- Build a client / SDK / frontend → [docs/integrate-a-client.md](docs/integrate-a-client.md)

API references:
- [Chain API (HTTP + SSE)](docs/chain-api.md)
- [Indexer API (REST)](docs/indexer-api.md)
- [Signing (Ed25519 + canonical encoding)](docs/signing.md)
- [Data Model](docs/data-model.md)

Walk-throughs:
- [End-to-end Tutorial](docs/tutorial-end-to-end.md) — fund an account, register a service, submit a request, watch it finalize
- [Architecture](docs/architecture.md) — what each container does and how they talk

## Repo layout

```
chain/                 Custom Go L1 service (HTTP + SSE, bbolt persistence)
inference-node/        Off-chain inference worker (Go). Calls llama-server.
indexer/               Chain event indexer (Go + SQLite). REST API.
frontend/              Next.js + in-browser Ed25519 wallet.
proto/                 Proto definitions (target shape for Phase 1).
contracts/             CosmWasm extensions — Phase 5 (placeholder).
determinism-harness/   Bit-exact reproducibility tests — parallel track (placeholder).
docker/                One-shot init scripts (model download).
scripts/               Cross-package helpers (wait-healthy).
docs/                  Public integration documentation.

.claude/               Claude Code configuration + internal dev docs.
                       Agents, skills, slash commands, project rules, ADRs,
                       phase tracker, protocol research, prior-art notes.
```

The `.claude/` folder is for Claude Code (the CLI/IDE assistant) and internal
development tracking. Integrators don't need to read anything in it.

## Common commands

```bash
make help               # show all targets
make demo               # one-command demo (recommended)
make poll               # poll request 1 until finalized
make up / down / reset  # docker compose lifecycle (reset wipes volumes)
make logs               # tail all service logs
make seed               # register the default demo service (idempotent)
make ps                 # show service status
```

## What this is and isn't yet

**Phase 3 (initial) — real today**: chain state machine, Ed25519 signing, escrow transfers, typed events, off-chain inference via TinyLlama, indexer ingest, deadline-triggered refunds, **verification domain registry**, **attestation v1** with full `(model_sha256, runtime, hardware, precision)` pinning, **determinism-harness** that independently re-runs every submitted inference, and **`MsgChallenge` + on-chain auto-slash** when divergence is detected.

```bash
make demo                     # honest path: SUBMITTED → (45-block window) → FINALIZED
make demo-malicious           # malicious provider → harness challenges → SLASHED + refund
make demo-spurious            # honest provider + spurious challenger → voucher dismisses → FINALIZED
make demo-multi-watcher       # show TWO independent harnesses watching one request
make demo-no-watcher          # falsifiability: no honest watcher → wrong hash finalizes
make demo-tokenizer-mismatch  # falsifiability: wrong TOKENIZER_ID → chain rejects MsgSubmitResult
make determinism-check        # same prompt × 2 llama-server processes × 3 runs = 6 hashes
REMOTE_HOST=root@<ip> make cross-host-determinism-check  # MVP item #1: cross-host determinism (needs ssh access to a 2nd host)
make harness-report           # see verdicts
```

**Now shipped through Phase 3.z + first part of Phase 1:** full dispute game (3.x bonds + 3.y vouchers + 3.z sybil resistance steps 1–4), service-registration bond with treasury sweep, per-domain `VoucherMargin`, voucher bond scales with provider bond, `MsgDeactivateDomain` cascade, tokenizer pinning, **two independent watchers** (`determinism-harness` + `determinism-harness-b`), falsifiability demos.

**Not yet** (with the phase that adds each):
- ~~Cross-host determinism on two physical hosts~~ — **shipped 2026-05-20**; `make cross-host-determinism-check`; validated Apple M3 Pro (Rosetta) ↔ AMD EPYC-Rome
- Verified-runtime executor (replace `llama_http`) — Phase 1
- BFT consensus / Cosmos SDK swap — Phase 1
- Federated re-execution committee (escalation beyond vouchers) — ADR-0004 design accepted; implementation Phase 4
- Authority → multisig → governance — Phase 4
- Generative agents — Phase 4
- CosmWasm extensions, Celestia DA — Phase 5

See [docs/phases-and-roadmap.md](docs/phases-and-roadmap.md) for what changes when.

## Working with Claude in this repo

`.claude/` is set up for [Claude Code](https://claude.com/claude-code):

- 11 specialized subagents (cosmos, frontend, indexer, devops, ml-determinism,
  protocol-architect, tdd-enforcer, analytics, agents, tutorial-writer,
  cosmwasm) — each self-gates by phase.
- 6 skills (TDD cycle, dockerize-service, scaffold-cosmos-module, etc.).
- Slash commands: `/demo`, `/devnet`, `/spec`, `/test-all`, `/phase`, `/review-tdd`.

If you're not using Claude Code, ignore `.claude/` — it doesn't affect the project at all.

## License

TBD.
