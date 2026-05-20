# aios Tutorial — Phase 0.5 Demo Slice

A walkthrough of the **Decentralized AI Operating System**'s first vertical slice: a custom Go L1 hosting an AI service marketplace, with a real off-chain LLM inference worker, a chain event indexer, and a wallet-connected web UI — all running via `make demo`.

> **Verified end-to-end.** `make demo` brings up six containers and finalizes an inference request from a real TinyLlama-1.1B-Chat model. Tested 2026-05-20.

---

## Table of Contents

1. [What we built](#1-what-we-built)
2. [Architecture at a glance](#2-architecture-at-a-glance)
3. [Run it](#3-run-it)
4. [Code walkthrough — the request lifecycle](#4-code-walkthrough--the-request-lifecycle)
5. [Why a custom Go chain in Phase 0.5 (and what Phase 1 changes)](#5-why-a-custom-go-chain-in-phase-05-and-what-phase-1-changes)
6. [What's real, what's not yet](#6-whats-real-whats-not-yet)
7. [Q&A](#7-qa)
8. [Where to go next](#8-where-to-go-next)

---

## 1. What we built

A modular L1 architecture where AI inference is a first-class on-chain service. Phase 0.5 ships:

- A **custom Go L1** (`chain/`) implementing the marketplace state machine: `MsgRegisterService`, `MsgRequestInference`, `MsgSubmitResult` (with immediate finalization in 0.5; challenge windows arrive in Phase 3). Real Ed25519 signatures. Persistence in bbolt. Block production every 1s. Typed events streamed over SSE.
- An **off-chain inference worker** (`inference-node/`) that subscribes to the chain's SSE event stream, runs a real LLM via the `llama-server` sidecar (TinyLlama 1.1B Q4_K_M), and submits results back as signed transactions.
- A **chain event indexer** (`indexer/`) writing to SQLite with idempotent ingest, exposing a REST API.
- A **Next.js + in-browser Ed25519 wallet** (`frontend/`) where users browse services, sign `request_inference` transactions, and watch results finalize.

```
┌──────────────┐  HTTP   ┌──────────────┐  SSE   ┌──────────────────┐
│  Frontend    │ ─────▶ │     Chain    │ ─────▶ │ inference-node   │
│ (Next.js)    │        │  (Go HTTP    │        │  (Go daemon)     │
│  Ed25519     │ ◀───── │   + SSE)     │ ◀── tx │                  │
└──────┬───────┘        └──────┬───────┘        └────────┬─────────┘
       │                       │ SSE                     │ /completion
       ▼                       ▼                         ▼
   ┌─────────┐           ┌──────────┐           ┌──────────────┐
   │ Indexer │           │ subscriber           │ llama-server │
   │ (Go +   │ ─────────▶│ + SQLite │           │ (llama.cpp + │
   │ SQLite) │           └──────────┘           │  TinyLlama)  │
   └─────────┘                                  └──────────────┘
```

**Everything in this slice is real except the verification guarantee.** The chain settles, signatures are checked, escrow moves, and the LLM produces output — but the result is not yet *cryptographically verifiable*. That arrives in Phases 1 and 3. See §6 for an explicit table.

---

## 2. Architecture at a glance

`make demo` (which wraps `docker compose up -d` + seed + first request) boots six containers:

| Container       | Image / Build                          | Purpose                                                         | Ports         |
|---              |---                                     |---                                                              |---            |
| `model-init`    | `alpine` + curl                        | One-shot: downloads + SHA-checks TinyLlama Q4_K_M into a volume  | —             |
| `llama-server`  | `ghcr.io/ggml-org/llama.cpp:server`    | Runs the pinned model, HTTP `/completion` API                    | 8080          |
| `chain`         | `aios/chain:dev`                       | Custom Go L1 — POST `/tx`, GET `/events` (SSE), state in bbolt   | 26657         |
| `inference-node`| `aios/inference-node:dev`              | Subscribes SSE → calls llama-server → broadcasts `submit_result` | —             |
| `indexer`       | `aios/indexer:dev`                     | Subscribes SSE → SQLite → REST API                               | 8081          |
| `frontend`      | `aios/frontend:dev`                    | Next.js + in-browser Ed25519 wallet                              | 3030          |

`depends_on` with `condition: service_healthy` enforces startup order. The chain is the root dependency — everything else waits for it.

---

## 3. Run it

### Prerequisites

- Docker Desktop (or Linux Docker with the `compose` plugin v2.x).
- ~2 GB free disk for the model + images.

### One command

```bash
git clone <this-repo>
cd l1-blockchain-with-ai-agent
make demo
```

The `make demo` target:
1. Copies `.env.example` → `.env` if missing.
2. Runs `docker compose up -d`.
3. Waits for healthchecks (`scripts/wait-healthy.sh`).
4. Seeds a default service (`translate-en-fr`, provider `bob`) via `POST /demo/seed`.
5. Submits a demo inference request via `POST /demo/request-inference`.
6. Prints the URL and tells you how to poll.

Then:

```bash
open http://localhost:3030     # the marketplace UI
# or
make poll                       # wait for the request to finalize on the CLI
```

### Expected output

```
$ make demo
→ created .env from .env.example
→ docker compose up -d (first boot downloads ~700MB model, can take 5+ min)
 Container aios-model-init    Started
 Container aios-chain         Healthy
 Container aios-llama-server  Healthy
 Container aios-indexer       Healthy
 Container aios-inference-node Started
 Container aios-frontend      Healthy
→ waiting for healthy services
all services healthy after 7s
→ seeding default service (translate-en-fr, provider=bob)
{"service_id":1,"status":"seeded"}
→ submitting demo inference request
{"request_id":1}
→ open http://localhost:3030
→ poll /requests/1 with: make poll

$ make poll
[attempt 1] status=FINALIZED
```

### Browser flow

1. Open <http://localhost:3030>
2. Stats: 1 service, 1 request, 1 finalized
3. Click the `translate-en-fr` service
4. Click "Use dev: alice" to use the funded alice key
5. Type a new prompt
6. Click "Sign and submit"
7. Browser computes SHA-256, signs the tx with Ed25519, POSTs to chain
8. Page navigates to `/requests/N`, polls indexer every 2s until `FINALIZED`
9. Result text appears

### Troubleshooting

| Symptom | Fix |
|---|---|
| Port 3030 in use | Set `FRONTEND_PORT=3031` in `.env`. |
| `model-init` taking forever | First download is ~700 MB. Check `docker compose logs model-init` for progress. Override `MODEL_MIRROR_URL` if HuggingFace is slow. |
| `llama-server` health timeout | First model load is ~30 s on slow CPUs. Tune `start_period` in `docker-compose.yml`. |
| Frontend shows "indexer unreachable" | `docker compose logs indexer`. Usually a transient startup-order issue; retry. |
| Output text is gibberish or off-topic | TinyLlama-1.1B is a 1.1B-param model — its output quality is intentionally lo-fi. The point of Phase 0.5 is the pipeline, not the model. Phase 1+ swaps in larger models. |
| `make reset` then re-`make demo` | Wipes model cache + chain state; first boot again. |

---

## 4. Code walkthrough — the request lifecycle

Here's an inference request's full journey, with quoted code from each layer.

### 4.1 User signs a `request_inference` tx in the browser

```ts
// frontend/src/components/RequestInferenceForm.tsx
async function onSubmit(e: React.FormEvent) {
  const w = await ensureWallet();
  const inputHashBytes = sha256(new TextEncoder().encode(prompt));
  const inputHash = toHex(inputHashBytes);
  const account = await getAccount(w.address);
  const payload = {
    requester: w.address,
    service_id: service.id,
    input_hash: inputHash,
    input_uri: "inline:" + inputHash,
    input_text: prompt,
    max_price: { denom: service.price_denom, amount: service.price_amount },
    deadline_height: 0,
  };
  const tx = await signTx(w, "request_inference", account.nonce, payload);
  const res = await submitTx(tx);
  router.push(`/requests/${res.request_id}`);
}
```

The wallet is generated in-browser:

```ts
// frontend/src/lib/wallet.ts
const priv = ed.utils.randomPrivateKey();
const pub = await ed.getPublicKey(priv);
const w: Wallet = {
  address: addressFromPubKey(pub),  // "aios1" + hex(sha256(pub)[:20])
  pubKeyHex: toHex(pub),
  privKeyHex: toHex(priv),
};
```

For the demo, alice/bob private keys are exposed via `/api/dev-keyring` (gated by `EXPOSE_DEV_KEYRING=1`) so users can sign as a funded account without seeding their own.

### 4.2 Chain validates the tx

```go
// chain/internal/state/tx.go
canonical, err := tx.CanonicalBytes()
if !ed25519.Verify(pub, canonical, sig) {
    return TxReceipt{}, types.ErrInvalidSignature
}
signerAddr := types.AddressFromPubKey(pub)
expectedNonce := nextNonce(btx, signerAddr)
if tx.Nonce != expectedNonce {
    return TxReceipt{}, fmt.Errorf("%w: expected %d got %d", types.ErrInvalidNonce, expectedNonce, tx.Nonce)
}
```

Then `applyRequestInference` runs inside the same bbolt transaction:

```go
// chain/internal/state/tx.go
// Escrow the *service price* (not max_price) from the requester.
if err := debit(btx, signer, svc.Price.Amount); err != nil { return ... }
if err := credit(btx, moduleEscrowAddr, svc.Price.Amount); err != nil { return ... }

id := getUint64(btx.Bucket(bktMeta), keyNextReqID)
req := types.InferenceRequest{
    ID: id, ServiceID: msg.ServiceID, Requester: signer,
    InputHash: msg.InputHash, InputURI: msg.InputURI, InputText: msg.InputText,
    Escrow: svc.Price, DeadlineHeight: deadline,
    Status: types.StatusPending, CreatedAtHeight: height,
}
```

Two design choices worth calling out:

- **Escrow the *service price*, not the requester's max_price.** Otherwise a provider could pocket the entire bid for any submitted result (fee griefing).
- **Validation, then state mutation, all in one atomic bbolt tx.** Either everything happens or nothing does — including the event emission, which is deferred until after the tx commits.

### 4.3 Chain emits a typed event over SSE

```go
// chain/internal/api/api.go (events handler)
w.Header().Set("Content-Type", "text/event-stream")
for {
    select {
    case ev := <-ch:
        fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(bz))
        flusher.Flush()
    case <-heartbeat.C:
        _, _ = w.Write([]byte(": ping\n\n"))
    }
}
```

Any subscriber (inference-node, indexer, a future analytics tool) gets the same stream. Heartbeats every 15s keep proxies from timing out.

### 4.4 inference-node observes the event

```go
// inference-node/cmd/inferenced/main.go
err := cli.SubscribeEvents(ctx, []string{"InferenceRequested"}, func(ev chain.Event) {
    handleEvent(ctx, cli, signer, exec, ev, logger)
})
```

The handler filters: only requests for services owned by **this** node's provider account get processed.

```go
svc, err := cli.GetService(payload.ServiceID)
if svc.Owner != signer.Address() {
    return // not our service
}
```

### 4.5 Real inference via llama-server

```go
// inference-node/internal/executor/llama_http/executor.go
// REAL INFERENCE, UNVERIFIED.
func (e *Executor) callLlamaServer(ctx context.Context, prompt string) (string, error) {
    body, _ := json.Marshal(completionRequest{
        Prompt: prompt, NPredict: 256,
        Temperature: 0, TopK: 1, TopP: 1, Stream: false,
    })
    // POST {serverURL}/completion, decode response.Content
}
```

Greedy sampling (`temperature=0, top_k=1`) — same parameters Phase 1's deterministic-runtime executor will use. The shape of this code doesn't change between phases; only the runtime pinning does.

### 4.6 Provider submits the result

```go
// inference-node/cmd/inferenced/main.go
attestation := chain.Attestation{
    Provider: signer.Address(),
    InputHash: payload.InputHash,
    OutputHash: result.OutputHash,
    HardwareTag: exec.HardwareTag(),
    // ...
}
attestation.SignatureHex = signer.SignAttestation(attestation)

msg := chain.MsgSubmitResult{
    Provider: signer.Address(),
    RequestID: payload.RequestID,
    Result: chain.Result{
        OutputHash: result.OutputHash,
        OutputURI: result.OutputURI,
        OutputText: result.Output,
        Attestation: attestation,
    },
}
resp, err := cli.SubmitTxAs(signer, chain.TxSubmitResult, msg)
```

The chain validates the attestation's `input_hash` matches the request, releases escrow to the provider, and emits `RequestFinalized`.

```go
// chain/internal/state/tx.go (applySubmitResult)
if params.ChallengeWindowBlocks == 0 {
    if err := debit(btx, moduleEscrowAddr, req.Escrow.Amount); err != nil { return ... }
    if err := credit(btx, signer, req.Escrow.Amount); err != nil { return ... }
    req.Status = types.StatusFinalized
    req.FinalizedAtHeight = height
    paid := req.Escrow; req.Paid = &paid
    finalized = true
}
```

Phase 0.5 finalizes immediately (`ChallengeWindowBlocks=0`). Phase 3 sets this to ~100 blocks and adds the dispute machinery.

### 4.7 Indexer ingests, UI sees it

```go
// indexer/internal/sub/sub.go
case "RequestFinalized":
    handleRequestFinalized(ev.Payload, ev.BlockHeight, st, chainURL, logger)
```

The indexer fetches the full request (with the output_text) from the chain and writes it to SQLite. The frontend's `/requests/{id}` page polls `/requests/{id}` every 2 s until `status === "FINALIZED"`, then displays the output.

---

## 5. Why a custom Go chain in Phase 0.5 (and what Phase 1 changes)

The original Phase 0.5 design called for **Cosmos SDK + CometBFT** as the L1 (see `docs/adr/0002-optimistic-verification.md`). That's still the Phase 1 target. Phase 0.5 ships a custom Go chain instead, for these reasons:

| Concern | Cosmos SDK | Custom Go |
|---|---|---|
| Lines of code to wire | ~3000+ (depinject, autocli, multi-module) | ~800 |
| Build dependencies (CGO, protoc, buf) | Heavy | None |
| Time to first `docker compose up` working | Days of debugging | Hours |
| What the demo actually proves | Marketplace mechanics | Marketplace mechanics |
| Production-readiness | Yes | No (single-node) |

The Phase 0.5 chain is a **structural ancestor** of the Phase 1 chain. The message types (`MsgRegisterService`, `MsgRequestInference`, `MsgSubmitResult`), the event shapes (`ServiceRegistered`, `InferenceRequested`, `ResultSubmitted`, `RequestFinalized`), the escrow lifecycle, the attestation payload — all of these survive into Phase 1 unchanged.

What Phase 1 swaps:

- bbolt → IAVL store (via Cosmos SDK)
- Custom block producer → CometBFT (BFT consensus, real validators)
- HTTP `/tx` endpoint → ABCI++ CheckTx/DeliverTx
- SSE event stream → CometBFT WebSocket subscription
- In-browser Ed25519 demo wallet → Keplr / Leap (real wallet UX)
- `/demo/*` admin endpoints → removed
- Single-node devnet → multi-validator testnet (Phase 2)

The proto definitions in `/proto/aiservice/v1/` remain the source of truth — they describe what Phase 1 produces, and the Phase 0.5 Go types mirror them.

---

## 6. What's real, what's not yet

| Component / Property                | Status in Phase 0.5                                | Lands in       |
|---                                  |---                                                 |---             |
| L1 state machine                    | **REAL** — custom Go chain, bbolt, blocks every 1s | already        |
| `register_service` + escrow         | **REAL** — keeper-tested, end-to-end                | already        |
| `request_inference` + escrow lock   | **REAL** — funds locked from requester              | already        |
| `submit_result` + finalize          | **REAL** — escrow released to provider              | already        |
| Typed events over SSE               | **REAL** — emitted, indexed                         | already        |
| Real Ed25519 signatures             | **REAL** — chain verifies on every tx               | already        |
| Off-chain inference worker          | **REAL** — Go daemon, real SSE subscription         | already        |
| LLM output                          | **REAL** — TinyLlama 1.1B via llama.cpp             | already        |
| Indexer                             | **REAL** — SQLite, idempotent ingest, REST API      | already        |
| Wallet flow                         | **REAL** — in-browser Ed25519 demo wallet           | already        |
| **BFT consensus**                   | NOT YET — single-node Go chain                      | **Phase 1**    |
| **Cosmos SDK + CometBFT**           | NOT YET — custom Go chain in 0.5                    | **Phase 1**    |
| **Real wallet (Keplr/Leap)**        | NOT YET — in-browser demo wallet                    | **Phase 1**    |
| **Determinism guarantee**           | NOT YET — single machine, no domain pinning         | **Phase 1**    |
| **Cryptographic attestation v1**    | NOT YET — Phase 0.5 uses simplified scheme          | **Phase 1**    |
| **Fraud-proof challenge**           | NOT YET — `ChallengeWindowBlocks=0`                 | **Phase 3**    |
| **Slashing**                        | NOT YET                                             | **Phase 3**    |
| Multi-machine verification          | NOT YET                                             | **Phase 1**    |
| Generative agents                   | NOT YET                                             | **Phase 4**    |
| CosmWasm extensions                 | NOT YET                                             | **Phase 5**    |

> **Reader trap warning.** This demo runs a real LLM. It is tempting to call that "verifiable AI." It is not. Verifiability requires (a) determinism so an honest challenger can reproduce the output, and (b) a chain-level dispute game that converts honest reproduction into a slashing of dishonest providers. Both arrive in later phases. Phase 0.5 demonstrates the *marketplace mechanics*, not the verification property.

---

## 7. Q&A

**Q: Why is the Phase 0.5 chain not Cosmos SDK?**

Time and reliability. Cosmos SDK v0.50 is ~3000+ lines of correct wiring for a fresh module — depinject, autocli, the module manager, codec registration, multi-store mounts, gov-authority plumbing. Getting that right in one shot without iterating Go builds is brittle. Phase 0.5 uses a custom Go chain that implements the same marketplace mechanics in ~800 lines, ships in hours, and provides the same lifecycle (`MsgRegisterService` → `MsgRequestInference` → `MsgSubmitResult` → escrow release) so Phase 1's Cosmos SDK swap is mechanical, not architectural.

**Q: Is the inference here verifiable?**

No — see §6. The LLM output is real, but the chain has no way to detect a wrong output today. A malicious provider in Phase 0.5 could submit garbage and still receive the escrow. Phase 1 introduces determinism pinning so an honest challenger can reproduce; Phase 3 introduces the dispute game so cheating becomes economically unprofitable.

**Q: How does the project make money on a wrong inference today?**

Phase 0.5 has no slashing — a malicious provider can scam the requester and walk away with the bid. This is an acknowledged failure mode of the *demo*, not the *protocol*. We chose to ship the marketplace mechanics first so the Phase 3 dispute game has something to hang off. Production-style economics arrive in Phases 2 (payment hardening) and 3 (slashing).

**Q: Why optimistic verification and not zkML?**

Today's zkML provers run roughly 1000–10000× slower than the underlying inference. For a 7B-parameter model, that puts a single proof in the hours-and-tens-of-dollars range. Optimistic verification gets to "good enough most of the time" cheaply, with a worst-case re-execution cost that only honest-vs-honest disagreement triggers. We retain the option to add zkML later for specific high-value services; we don't bet the substrate on it.

**Q: Where do generative agents fit?**

Agents are clients of the marketplace, not a separate substrate. A "generative agent" in this project is a piece of software with an on-chain account, a chain-enforced spending budget, and the ability to call `request_inference` autonomously against services it chooses. Phase 4 opens the `agents/` package and the `agents-engineer` subagent.

**Q: Why TinyLlama and not GPT-4?**

Two reasons. (1) Closed-weight models can't be put in the verification path because the chain has no way to assert which model produced an output. Verification requires a fingerprint (SHA-256 of weights) that anyone can independently reproduce. (2) The Phase 0.5 demo prioritizes a small download, fast cold start, and CPU-only operation. Phase 1+ adds larger open-weight models (Llama-3.1-8B, Mistral-7B, Qwen2.5-7B) under the determinism harness.

**Q: What does Phase 1 add?**

Three things, in order: (1) replace the custom Go chain with Cosmos SDK + CometBFT — same message lifecycle, real BFT consensus; (2) replace `llama_http` with a deterministic-runtime executor — pinned model weights by SHA, pinned runtime binary, pinned precision, pinned hardware tag; (3) qualify the first verification domain via the `determinism-harness`. Two independent inference-nodes producing identical attestations on identical inputs is the gate to Phase 2.

**Q: Why a sidecar instead of embedded llama.cpp?**

CGO would couple the inference-node binary to llama.cpp's build toolchain (cmake + a C++17 compiler + occasionally CUDA). That makes CI slow, makes cross-compilation hard, and turns every llama.cpp upgrade into a Go module crisis. Sidecar via HTTP keeps the Go binary pure-Go, and `llama-server` is identifiable by container image SHA — which is exactly what verification domains need.

**Q: Why is the output so off-topic? The prompt asks for a French translation and TinyLlama writes about a job application.**

TinyLlama-1.1B is a small model that drifts off-topic on most prompts. The point of Phase 0.5 is the *pipeline* — chain → SSE → worker → LLM → attestation → finalization — not the LLM's quality. Phase 1 swaps in larger open-weight models that follow instructions better.

**Q: Can I add my own AI service?**

Yes. Two ways:

- **Via the UI**: open <http://localhost:3030>, click "Use dev: bob", then visit the chain's `/demo/register-service` endpoint to register a service under a different name. (Phase 0.5: there's only one inference-node serving the marketplace, so additional services need their own worker to be useful.)
- **Via the HTTP API**: POST a signed `register_service` tx to `http://localhost:26657/tx`. The chain's REST and event API is fully open — any client that can do Ed25519 and JSON can participate.

**Q: How is determinism tested?**

The `determinism-harness/` package runs a chosen `(model, runtime, precision, hw)` tuple twice on the same machine, then on two machines of the same hardware class, then on a different hardware class (to confirm expected divergence). It diffs outputs byte-for-byte. A flaky test means the tuple is non-deterministic, not that the test is wrong. Flaky tuples are recorded in `docs/prior-art/runtime-disqualifications.md`. The harness runs in parallel with all subsequent phases.

**Q: What happens if I lose my browser-generated key?**

In Phase 0.5: the funds are unrecoverable, but the funds are fake (devnet aios tokens). The "Use dev: alice" / "Use dev: bob" buttons let you switch to known funded accounts. Phase 1 introduces real wallet integration (Keplr/Leap) with seed-phrase backup.

---

## 8. Where to go next

- **Roadmap** — [docs/PHASE.md](PHASE.md) lists the gates to Phase 1 and beyond.
- **Protocol spec** — [docs/protocol/verification-protocol.md](protocol/verification-protocol.md) is the load-bearing design document for the verification model.
- **Architecture** — [docs/architecture.md](architecture.md) has the system-level view.
- **ADRs** — [docs/adr/](adr/) records every accepted design decision with its tradeoffs.
- **Project rules** — [CLAUDE.md](../CLAUDE.md) enumerates TDD discipline, determinism rules, anti-patterns. Read before any non-trivial change.

---

*Phase 0.5 — first vertical slice of the decentralized AI operating system. Verified `make demo` end-to-end on 2026-05-20.*
