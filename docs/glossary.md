# Glossary

Quick reference for terminology used across the docs.

← back to [docs/README.md](README.md)

---

**Account** — an Ed25519 keypair plus a derived address (`aios1...`). State: balance + nonce. See [Data Model](data-model.md#address-format).

**aios** — both the project name and the base token denomination. Phase 0.5 has 1 billion `aios` minted at genesis, split between `alice` and `bob` in the dev keyring.

**Attestation** — a signed claim from a provider asserting that a given input produced a given output under a specific runtime configuration. The signature commits the provider; it does not (in Phase 0.5) prove correctness. See [Data Model](data-model.md#attestation).

**Block** — a unit of chain progress. Phase 0.5: one block per second. Phase 1+: CometBFT consensus, typically ~6 s.

**Block producer** — Phase 0.5's goroutine that ticks every second, increments height, sweeps expired requests for refunds, emits `BlockCommitted`. Phase 1+: CometBFT validators.

**Challenge** — Phase 3+: a dispute against a submitted inference result. The challenger signs their own attestation with a different `output_hash` and submits `MsgChallenge` during the challenge window. The chain enters `CHALLENGED` status; Phase 3 simple auto-resolves in the challenger's favor after a resolution window (`SUBMITTED → CHALLENGED → SLASHED`). Phase 3.x replaces auto-resolution with a real dispute game.

**Challenge window** — the number of blocks between `ResultSubmitted` and the chain's auto-finalize. `Params.ChallengeWindowBlocks` controls it. Phase 0.5/1: 0 (immediate finalize). Phase 3+: 45 by default (45 s) — long enough for a CPU-based harness to re-run TinyLlama and file `MsgChallenge` if needed.

**Chain** — the aios L1. Phase 0.5: a custom Go HTTP service with bbolt persistence. Phase 1+: Cosmos SDK + CometBFT.

**CometBFT** — the BFT consensus engine used by Cosmos SDK chains. Replaces Phase 0.5's single-node block producer in Phase 1+.

**Deadline** — the block height by which a request must receive a submitted result. If the deadline elapses, the chain auto-refunds.

**Demo wallet** — Phase 0.5's in-browser Ed25519 keypair stored in `localStorage`. Replaced by Keplr/Leap in Phase 1+.

**Dev keyring** — the chain's bundled `alice` and `bob` accounts, funded at genesis. Lives at `/home/aios/.aid/keys.json` in the chain container.

**Ed25519** — the signature scheme used for both transactions and attestations. RFC 8032. Fast, small keys, standardized.

**Escrow** — funds locked from the requester when an `InferenceRequested` tx is accepted. Released to the provider on finalization, or returned to the requester on refund.

**Event** — a typed message emitted by the chain on its SSE stream. See [Data Model](data-model.md#events) for the full list.

**Finalization** — the request's terminal good state. Status `FINALIZED`. Escrow has been paid to the provider, output is committed.

**Hardware tag** — an identifier (e.g. `cpu-x86_64-tinyllama-q4`, `nvidia-a100-fp16`) recorded in attestations. Different tags are different verification domains in Phase 1+.

**Indexer** — the read-side service mirroring chain state into SQLite. Eventually consistent, ~1–2 s lag. See [Indexer API](indexer-api.md).

**Inference node** — an off-chain worker that subscribes to chain events and submits results. Owned by a provider. See [Integrate an Inference Node](integrate-an-inference-node.md).

**Input hash** — SHA-256 hex of the prompt bytes. Committed in the request; the provider's attestation must reference the same hash.

**llama-server** — the HTTP server bundled with llama.cpp. Phase 0.5's inference runtime. Pluggable: any HTTP completion API works.

**Module account** — an account owned by the chain itself (not a user). Phase 0.5 has one: the escrow account that holds funds during a request's lifecycle.

**Nonce** — a per-account counter incremented on each accepted transaction. Replay protection. Clients must use the current nonce; gaps are rejected.

**Optimistic verification** — the approach where results are accepted by default and disputed via fraud proofs. The alternative to zkML (cryptographic proof of every inference) and TEE attestation (hardware vendor trust). Phase 3 implements the basic dispute step (challenge → auto-slash); Phase 3.x adds bonds and a real adjudication game.

**SLASHED** — terminal status for a request whose provider was successfully challenged. Escrow returns to requester; provider gets nothing. Phase 3.x adds explicit bond slashing on top.

**CHALLENGED** — intermediate status. A challenger has filed `MsgChallenge` against a `SUBMITTED` request; the chain is awaiting resolution. Phase 3 simple auto-resolves to `SLASHED` after a resolution window; Phase 3.x runs a dispute game first.

**Output hash** — SHA-256 hex of the inference output bytes. Committed in the attestation; future challenge mechanisms compare against it.

**Phase** — a discrete chunk of the roadmap. See [Phases & Roadmap](phases-and-roadmap.md).

**Price** — what a service charges per inference, in `aios`. Set at registration; not yet updatable in Phase 0.5 (Phase 2 adds `MsgUpdateService`).

**Provider** — the owner of a service. Earns the escrow when their inference node submits a result.

**Requester** — the user paying for an inference.

**Request** — an `InferenceRequest` — the on-chain record of a single inference job from requester to provider.

**Result** — the provider's submitted answer to a request, including the attestation. Committed once per request.

**Service** — a marketplace listing. Has an owner, name, description, price, and (later) a verification domain. See [Integrate a Service](integrate-a-service.md).

**SSE** — Server-Sent Events. The chain streams typed events as `text/event-stream` over HTTP. See [Chain API](chain-api.md#get-events).

**Verification domain** — Phase 1+: a registered `(model_hash, runtime_version, precision, hardware_tag)` tuple. Two attestations are comparable only within the same domain. The basis of fraud-proof challenges. Phase 0.5 services use `verification_domain_id=0` (unverified).

**Verifiable** — in this project, means: outputs can be cryptographically disputed, and the chain enforces slashing on whoever computed incorrectly. Phase 0.5's inference is *real* but not *verifiable*. Phase 3 makes it verifiable.

**Worker** — synonym for inference node.

**zkML** — zero-knowledge proofs of machine learning inference. Heavyweight alternative to optimistic verification. The aios protocol can support zkML for high-value services but isn't built on it.

## Next

- [Architecture](architecture.md)
- [Data Model](data-model.md)
- [docs/README.md](README.md)
