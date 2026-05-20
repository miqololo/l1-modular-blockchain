# Getting Started

The fastest path from clone to "I have an AI marketplace running on localhost".

← back to [docs/README.md](README.md)

---

## Prerequisites

- **Docker Desktop** (macOS / Windows) or Linux Docker with the `compose` plugin v2+
- **~2 GB free disk** for the model and images
- A terminal

That's it. You don't need Go, Node, pnpm, or any other toolchain on your host — everything builds inside Docker.

## Run the stack

```bash
git clone <this-repo> aios
cd aios
make demo
```

On the first run this downloads TinyLlama-1.1B-Chat-Q4_K_M (~700 MB, one time) and brings up six containers. Expect:

- **Cold start**: 3–6 minutes (model download dominates)
- **Warm restart** (`make down` → `make up`): ~30 seconds

When it's ready you'll see:

```
→ seeding default service (translate-en-fr, provider=bob)
{"service_id":1,"status":"seeded"}
→ submitting demo inference request
{"request_id":1}
→ open http://localhost:3030
→ poll /requests/1 with: make poll
```

## Verify it works

### From the CLI

```bash
make poll
```

Output:

```
[attempt 1] status=PENDING
[attempt 2] status=FINALIZED
```

Within ~10 seconds the request finalizes — meaning the chain accepted a `MsgRequestInference`, the inference-node observed it via SSE, called llama-server, signed an attestation, submitted `MsgSubmitResult`, and the chain released escrow to the provider.

### From the browser

Open <http://localhost:3030>. You'll see:

- **Stats**: 1 service, 1 request, 1 finalized
- **Services**: `translate-en-fr` with the demo price
- Click the service → see the request form
- Click "Use dev: alice" → import the funded test account
- Type any prompt → "Sign and submit"
- The page navigates to `/requests/N` and polls until you see the LLM output

### Hit the APIs directly

The chain and indexer both expose simple HTTP APIs:

```bash
# Chain status
curl -s http://localhost:26657/status

# All registered services
curl -s http://localhost:26657/services

# Request 1 with full attestation
curl -s http://localhost:26657/requests/1

# Indexer summary stats
curl -s http://localhost:8081/stats/summary

# Tail chain events as they happen (SSE)
curl -N http://localhost:26657/events
```

See [Chain API](chain-api.md) and [Indexer API](indexer-api.md) for full reference.

## URLs

| Service                | URL                          | What it is |
|---                     |---                           |---         |
| Frontend               | <http://localhost:3030>      | Marketplace UI |
| Chain                  | <http://localhost:26657>     | L1 HTTP + SSE |
| Indexer                | <http://localhost:8081>      | Read-side REST |
| llama-server           | <http://localhost:8080>      | LLM completion API |
| Determinism harness    | <http://localhost:8090>      | `/report` — verdicts per finalized request (Phase 1+) |

## Lifecycle commands

```bash
make demo        # bring up + seed a default service + submit a demo request
make up          # bring up, no seed
make down        # stop (keeps data + model cache)
make reset       # stop + wipe ALL volumes (re-downloads model on next up)
make ps          # show service status
make logs        # tail all logs
make seed        # register the default demo service (idempotent)
make poll        # poll request 1 until finalized/refunded
```

## Test the components

Right now there's no host-side `go test` because the project assumes Docker-based builds. To run the chain's tests:

```bash
docker run --rm -v "$PWD/chain:/src" -w /src golang:1.22.5-alpine \
  sh -c "go mod tidy && go test ./..."
```

Similar pattern for `indexer/` and `inference-node/`. Frontend tests:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:20-alpine \
  sh -c "corepack enable && pnpm install --no-frozen-lockfile && pnpm test"
```

(Tests exist for the chain state machine, executor, indexer store, and frontend wallet logic. See each package's `*_test.go` / `*.test.tsx`.)

## Configure

`make demo` auto-creates `.env` from `.env.example`. Override defaults there:

```bash
# .env
FRONTEND_PORT=3030
CHAIN_PORT=26657
INDEXER_PORT=8081
LLAMA_PORT=8080

MODEL_MIRROR_URL=https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf
MODEL_SHA256=                 # blank = no SHA verification (Phase 0.5 default)
```

Use a different port if 3030/26657/8081/8080 are taken:

```bash
echo "FRONTEND_PORT=3131" >> .env
make down && make up
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `port already in use` | Set the port in `.env` (`FRONTEND_PORT=3131`), `make down`, `make up`. |
| `model-init` taking forever | First download is ~700 MB. `make logs` and look at the `model-init` container's progress. |
| `llama-server unhealthy` after long wait | Model load on slow CPU can take >30 s. `docker compose logs llama-server` — if you see "model loaded", it's actually fine, the healthcheck just hasn't reconverged. |
| Frontend says "indexer unreachable" | Transient startup-order issue. Refresh in a few seconds. |
| Output text is gibberish | TinyLlama-1.1B is small; it ignores most prompts. The pipeline works; Phase 1 swaps in larger models. |
| Stack is wedged | `make reset && make demo` — clean slate. Re-downloads the model. |

## Next

- [Architecture](architecture.md) — what's actually running and why
- [End-to-end Tutorial](tutorial-end-to-end.md) — sign and submit a transaction yourself, follow it through every container
