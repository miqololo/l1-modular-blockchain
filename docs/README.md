# aios — Integration Documentation

Documentation for developers integrating with the **Decentralized AI Operating System**: a modular L1 marketplace where anyone can register AI services, anyone can request inference, and inference workers earn fees by serving real work.

This folder is the **integration manual**. It tells you how to use the system from the outside — how to register a service, run an inference node, build a client, sign transactions, and read the chain. Internal design docs (ADRs, phase tracker, protocol research) live in `.claude/internal/` and are not required reading to integrate.

---

## Where to start

| Your role | Start here |
|---|---|
| **First-time user** — I just want to see it work | [Getting Started](getting-started.md) → run `make demo`, hit the UI |
| **Skeptical reviewer** — I want to verify the security claim | [Tutorial for Reviewers (EN)](TUTORIAL.md) — runs 7 demos including 2 falsifiability ones, validates every sub-claim |
| **Auditing for reproducibility** | [Reproducibility Statement](REPRODUCIBILITY.md) — pinned configuration, step-by-step certification checklist with sign-off |
| **Russian-language reader** | [Полное руководство (RU)](TUTORIAL.ru.md) — comprehensive Russian-language project overview |
| **Curious about the architecture** | [Architecture](architecture.md) — system overview, data flow |
| **Want to walk the whole flow** | [End-to-end Tutorial](tutorial-end-to-end.md) — sign a tx, follow it through every component |
| **Registering a new AI service** | [Integrate a Service](integrate-a-service.md) |
| **Running an inference node** | [Integrate an Inference Node](integrate-an-inference-node.md) |
| **Building a client / SDK / dApp** | [Integrate a Client](integrate-a-client.md) |
| **Looking up an API call** | [Chain API](chain-api.md) · [Indexer API](indexer-api.md) |
| **Signing transactions** | [Signing](signing.md) — Ed25519 details, canonical encoding |
| **Understanding objects** | [Data Model](data-model.md) — Service, Request, Result, Attestation, Event |
| **What's next?** | [Phases & Roadmap](phases-and-roadmap.md) |
| **Wait, what does "X" mean?** | [Glossary](glossary.md) |

---

## Reading order (recommended path)

If you're integrating for the first time, read in this order — each doc assumes you've seen the previous one:

1. [Getting Started](getting-started.md) — run the demo locally, see all components alive
2. [Architecture](architecture.md) — what each container does and how they talk
3. [Data Model](data-model.md) — vocabulary: services, requests, events, attestations
4. [Signing](signing.md) — Ed25519 transaction signing details
5. [Chain API](chain-api.md) — HTTP + SSE reference
6. [Indexer API](indexer-api.md) — REST reference
7. [End-to-end Tutorial](tutorial-end-to-end.md) — sign and submit your own transaction
8. One of the integration guides below for your role

---

## Integration paths

The system has three integration surfaces. Each guide is standalone.

### Path A — Register an AI Service ([guide](integrate-a-service.md))

You have an AI model (LLM, image generator, embedding model, custom logic) and you want providers to be able to earn fees serving it.

You'll:
1. Run an inference node (Path B) that knows how to handle your model.
2. Register a `Service` on the chain that maps to your inference logic.
3. Set the price; the chain handles escrow + payment automatically.

### Path B — Run an Inference Node ([guide](integrate-an-inference-node.md))

You have compute (CPU, GPU) and you want to earn fees by serving inference requests.

You'll:
1. Subscribe to the chain's SSE event stream.
2. Filter for `InferenceRequested` events for services you own.
3. Run your inference (using whatever model/runtime).
4. Sign and submit a `MsgSubmitResult` transaction.
5. Chain releases escrow to your provider account.

### Path C — Build a Client / dApp ([guide](integrate-a-client.md))

You want users to be able to send inference requests and see results.

You'll:
1. Generate or import an Ed25519 keypair (or integrate a wallet — Phase 1+).
2. Read service listings from the indexer's REST API.
3. Sign and POST a `MsgRequestInference` transaction.
4. Poll the indexer (or subscribe to the chain's SSE) for finalization.

---

## File map

```
docs/
├── README.md                          ← you are here
├── getting-started.md                 quickstart: clone → make demo → browser
├── architecture.md                    system view, container map, data flow
├── tutorial-end-to-end.md             walk one request through every component
├── integrate-a-service.md             Path A — register a marketplace service
├── integrate-an-inference-node.md     Path B — run your own inference worker
├── integrate-a-client.md              Path C — build a frontend / SDK
├── chain-api.md                       reference: chain HTTP + SSE endpoints
├── indexer-api.md                     reference: indexer REST endpoints
├── signing.md                         reference: Ed25519 + canonical encoding
├── data-model.md                      reference: domain types
├── phases-and-roadmap.md              public roadmap (light version)
└── glossary.md                        terminology
```

---

## Status: Phase 3.x (bonds)

Phase 3.x adds **bond economics** on top of Phase 3's challenge mechanism. Providers stake on every submission, challengers stake on every challenge. Lying providers lose their bond to the challenger; the malicious-provider demo shows a 50-aios transfer from cheater to detector in under 30 seconds.

**What works today**: provider bonds locked at submit and released on `FINALIZED`. Challenger bonds locked at `MsgChallenge` and returned on `SLASHED`. A successful slash transfers the provider's bond to the challenger. End-to-end balance accounting verified across all four actors (requester, provider, challenger, module escrow).

**What's still in Phase 3.y** (open): a real dispute resolution mechanism. Today's chain still auto-slashes after the resolution window because it has no way to **dismiss** a challenge — meaning a malicious *challenger* could grief honest providers. The voucher mechanism in [ADR-0004](.claude/internal/adr/0004-dispute-game-shape.md) closes this. Until then, only trust the bundled harness as challenger.

If you build a service today on a verified domain: cheating costs you your bond. Honest providers wait ~45 s for finalization and walk away with escrow + bond returned.

See [Phases & Roadmap](phases-and-roadmap.md) for what changes when.

---

## Questions or issues?

Read [Glossary](glossary.md) for terminology. For internal design rationale (the deeper "why" behind decisions like "optimistic vs zk verification"), see `.claude/internal/` — but you don't need it to integrate.
