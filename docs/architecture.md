# Architecture

What's actually running when you do `make demo`, and how the pieces talk to each other.

← back to [docs/README.md](README.md)

---

## At a glance

```
┌────────────┐     POST /tx        ┌────────────────┐    SSE       ┌────────────────────┐
│  Frontend  │ ──────────────────▶│    Chain (L1)  │ ────────────▶│  inference-node    │
│  (Next.js) │                    │   HTTP + SSE   │              │  (Go daemon)       │
│  Ed25519   │ ◀─── REST ────     │   bbolt state  │ ◀── POST /tx│                    │
│  in-browser│                    └────────┬───────┘              └──────────┬─────────┘
└─────┬──────┘                             │ SSE                            │ POST /completion
      │ REST                                ▼                                 ▼
      ▼                              ┌────────────┐                  ┌──────────────┐
   ┌──────────────┐                  │  Indexer   │                  │ llama-server │
   │ End user     │                  │ (Go +      │                  │ (llama.cpp + │
   └──────────────┘                  │  SQLite)   │                  │  TinyLlama)  │
                                     └────────────┘                  └──────────────┘
```

The system is six containers. Five do work; one is a one-shot model fetcher.

## Components

### `chain` — the L1

**What it is**: a custom Go HTTP service implementing a deterministic state machine. Real Ed25519 signatures, bbolt persistence, block production every 1 second, typed event stream over Server-Sent Events.

**Why custom (vs Cosmos SDK)**: Phase 0.5 uses a minimal Go chain (~800 lines) instead of full Cosmos SDK for boot reliability. Phase 1 replaces it with Cosmos SDK + CometBFT; the message types and event shapes carry over. See [Phases & Roadmap](phases-and-roadmap.md).

**What it owns**:
- Accounts (Ed25519 → bech32-style address)
- Services (the marketplace listings)
- Inference requests (lifecycle: PENDING → SUBMITTED → FINALIZED / REFUNDED)
- Escrow balances
- Nonces (replay protection)
- The event stream

**What it does NOT do**: actual inference. The chain only ever sees hashes — it doesn't see your prompts or outputs in plaintext (Phase 0.5 inlines them as a demo convenience; Phase 1 moves to content-addressed off-chain storage).

**Endpoints**: see [Chain API](chain-api.md).

### `inference-node` — the off-chain worker

**What it is**: a Go daemon that subscribes to the chain's SSE stream, filters for `InferenceRequested` events on services it owns, runs the underlying model, and broadcasts `MsgSubmitResult`.

**Why off-chain**: AI inference is too expensive to run inside consensus. The chain coordinates and settles; the model runs off-chain. Verification of the result happens via the fraud-proof game (Phase 3) — until then, the inference is "real but unverified."

**What it owns**:
- The provider account's private key (mounted from the chain's keyring volume)
- The connection to its inference runtime (llama-server in Phase 0.5; pluggable in Phase 1)
- Backoff and reconnect logic for the SSE stream
- The attestation signing logic

### `llama-server` — the inference runtime

**What it is**: `llama.cpp`'s HTTP server, run as a sidecar. Loads a pinned model (TinyLlama-1.1B-Chat-Q4_K_M in Phase 0.5) and serves a `/completion` API.

**Why a sidecar (vs embedded in inference-node)**: keeps the Go binary pure-Go, makes the runtime swappable by changing one container, and gives us a stable image SHA for verification domains.

**Pluggable**: Phase 1+ will support vLLM and candle as alternative runtimes. The HTTP API is what couples them — same `/completion` shape works across runtimes.

### `indexer` — the read-side service

**What it is**: a Go daemon that subscribes to the chain's SSE stream, mirrors the state into SQLite, and exposes a REST API for clients.

**Why a separate service**: the chain is optimized for sequential consistency, not query patterns. The indexer denormalizes for the queries clients actually run (list services, filter requests by status, compute stats).

**What it owns**:
- A SQLite database with `services`, `requests`, derived stats views
- The mapping from chain events to SQL upserts
- The REST API for the frontend
- Idempotent ingest (re-processing events is safe)

**SQLite (vs Postgres)**: Phase 0.5 uses SQLite (via `modernc.org/sqlite`, pure Go, no CGO) for one-container simplicity. Phase 4 hardening can swap to Postgres when query volume warrants it. The store layer is abstracted to make the swap mechanical.

### `frontend` — the UI

**What it is**: Next.js 14 (App Router) + an in-browser Ed25519 demo wallet.

**Wallet flow** (Phase 0.5):
1. On first interaction the frontend generates an Ed25519 keypair and stores it in `localStorage`.
2. Alternatively, "Use dev: alice" or "Use dev: bob" imports the chain's devnet keyring — useful because those accounts are funded at genesis.
3. Transactions are signed in the browser; only the signed envelope is sent to the chain.

**Why no Keplr in 0.5**: Keplr expects Cosmos SDK chains with full BFT consensus. Phase 1 (when the chain becomes Cosmos SDK + CometBFT) reintroduces Keplr / Leap.

**What it reads from where**:
- Service list, request status, finalized outputs → from the **indexer** (`http://localhost:8081`)
- Account balance, current nonce → from the **chain** directly (`http://localhost:26657`)
- Transaction broadcasts → POST to the chain's `/tx`

### `model-init` — one-shot

Downloads the model into the shared volume on first compose-up. Verifies SHA-256 if `MODEL_SHA256` is set in `.env`. Exits 0. Future restarts skip the download.

### `determinism-harness` — Phase 1+ verifier

**What it is**: a Go daemon that watches `RequestFinalized` events. For each one, it fetches the full request from the chain, independently calls llama-server with the same prompt, and compares its own SHA-256 of the output to the provider's submitted `output_hash`.

**Why**: this is the Phase 3 challenger role rehearsed without slashing. Phase 1 introduces the *observation* (chain accepts the attestation; harness independently verifies it matches a reproduction). Phase 3 adds the dispute machinery — at that point the harness becomes a real challenger that can lock provider bonds when divergence is detected.

**Outputs**: a small REST API at `:8090` with `/report` showing per-request verdicts (`OK | DIVERGENT | SKIPPED | ERROR`) and roll-up counts. Run `make harness-report` to see them.

**Limitations** (Phase 1):
- Only re-runs requests with `input_uri = inline:...` (where the prompt is on-chain). Phase 4 adds content-addressed fetch.
- Calls the same llama-server the provider used. A real challenger in Phase 3 will run its own runtime instance.
- No on-chain effect. Divergence is logged; nothing happens to the lying provider until Phase 3.

## Data flow — happy-path inference

Step-by-step, what happens when a user clicks "Sign and submit" in the UI:

1. **Browser**: SHA-256 the prompt → `input_hash`. Build a `request_inference` payload. Sign with Ed25519. POST to `http://localhost:26657/tx`.
2. **Chain**: verify signature, verify nonce, validate payload (service exists, requester has funds, deadline future), debit escrow from requester, credit module escrow account, allocate new `request_id`, write `InferenceRequest` row in bbolt, emit `InferenceRequested` event on SSE.
3. **Inference-node**: receives `InferenceRequested` over SSE. Filters: is the service ID owned by *this* node's provider account? If yes, proceed.
4. **Inference-node → llama-server**: POST `/completion` with the prompt. Wait for `content`. SHA-256 the output → `output_hash`. Build `Attestation` struct.
5. **Inference-node**: sign the attestation (Ed25519 over a canonical encoding). Build `submit_result` payload. Fetch nonce from chain. Sign envelope. POST to chain's `/tx`.
6. **Chain**: verify signature, verify the attestation's `input_hash` matches the request, verify the signer is the service owner, verify the deadline hasn't elapsed. Mark request `SUBMITTED`. Because `ChallengeWindowBlocks=0` (Phase 0.5), immediately finalize: debit module escrow, credit provider, mark `FINALIZED`. Emit `ResultSubmitted` then `RequestFinalized` events.
7. **Indexer**: receives both events. Marks the SQLite row as `FINALIZED`. Fetches the full request from chain to capture `output_text`.
8. **Frontend**: polls `http://localhost:8081/requests/N` every 2 s. Sees `status=FINALIZED`. Renders the output.

Total wall time on a warm cache: 1–3 seconds for chain steps, 5–15 seconds for inference on CPU.

## Data flow — refund

If no provider submits a result by the request's `deadline_height`:

1. Chain's block producer goroutine ticks at height N.
2. It scans pending requests; for each whose deadline < N, it refunds: debit module escrow, credit requester, mark `REFUNDED`, emit `RequestRefunded`.
3. Indexer updates the SQLite row.

This is the only auto-triggered chain action — every other state transition requires a signed transaction.

## Trust model (Phase 0.5)

- The chain is **single-node** in Phase 0.5. There's no BFT consensus yet. Anyone running the chain binary controls all state. This is acceptable for the demo; Phase 1 swaps in CometBFT for real consensus.
- The inference-node is **trusted to do honest inference**. If it submits garbage, the chain has no way to tell — the fraud-proof challenge mechanism arrives in Phase 3.
- The provider's **private key lives in the chain volume** (`/keys/keys.json`). The inference-node mounts it read-only. In production each provider runs their own infrastructure with their own keys.

These are clearly-bounded Phase 0.5 simplifications. See [Phases & Roadmap](phases-and-roadmap.md) for what Phase 1+ change.

## Why this architecture

The decomposition is intentional, not incidental:

- **Chain ↔ inference-node ↔ frontend are loosely coupled via HTTP+SSE**. Any of the three can be rewritten without touching the others. The chain doesn't know what language the inference-node is written in; the inference-node doesn't know the frontend exists.
- **The indexer is optional**. The frontend could read directly from the chain — but most chain implementations don't index efficiently for query patterns the UI needs. The indexer absorbs that complexity.
- **The inference runtime is a sidecar**. Same reason: pluggable, identifiable by image SHA, doesn't pollute the inference-node's Go binary.

The result: integrating with any one layer is a single problem, not a tangle.

## Next

- [Data Model](data-model.md) — the actual fields on Service, Request, Result, Attestation, Event
- [End-to-end Tutorial](tutorial-end-to-end.md) — walk one request through every component yourself
- [Integrate a Service](integrate-a-service.md) / [Integrate an Inference Node](integrate-an-inference-node.md) / [Integrate a Client](integrate-a-client.md) — depending on what you want to build
