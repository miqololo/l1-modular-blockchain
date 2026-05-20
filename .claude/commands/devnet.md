---
description: Manage the local devnet via docker-compose. Usage — /devnet up | down | reset | status
argument-hint: up | down | reset | status
---

Manage the local single-validator devnet.

**Task**: $ARGUMENTS

Map the argument to a concrete action:

- `up` — `docker compose up -d`, then wait for all healthchecks to be green (use `scripts/wait-healthy.sh`). Print a summary: chain RPC at `:26657`, REST at `:1317`, indexer API at `:8081`, frontend at `:3000`.
- `down` — `docker compose down`. Preserves volumes so the next `up` is fast.
- `reset` — `docker compose down -v` (removes volumes, including the model cache and devnet state). Confirm with the user before running because the model re-download takes time.
- `status` — `docker compose ps` plus a curl to each health endpoint, print a one-line status per service.

If any healthcheck fails on `up`, **do not retry silently**. Print the failing service's logs (`docker compose logs <service> | tail -50`) and stop.

If `reset` is requested without explicit confirmation in this turn, ask the user to confirm before running — model re-download is several minutes.

When complete, print:

```
## Devnet status
- chain:           UP / DOWN / DEGRADED
- inference-node:  UP / DOWN / DEGRADED
- indexer:         UP / DOWN / DEGRADED
- postgres:        UP / DOWN / DEGRADED
- llama-server:    UP / DOWN / DEGRADED
- frontend:        UP / DOWN / DEGRADED

## Endpoints
- Chain RPC:       http://localhost:26657
- Chain REST:      http://localhost:1317
- Indexer API:     http://localhost:8081
- Frontend:        http://localhost:3000
```
