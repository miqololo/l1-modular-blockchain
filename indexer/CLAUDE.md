# indexer/ — Chain event indexer

Subscribes to CometBFT WebSocket, parses chain events, writes to Postgres, exposes REST API for the frontend.

**Status: phase 4.** Stub-only until phase 2 emits events worth indexing.

## Layout (target)

```
cmd/indexerd/          Binary entrypoint
internal/
  subscriber/          WS event subscription, reconnection
  parser/              Event → typed struct
  store/               Postgres repo (pgx)
  api/                 REST handlers (chi router)
migrations/            sql-migrate or goose
```

## Rules

- **Idempotent ingest.** Reprocessing a height must not produce duplicate rows. Use `(block_height, tx_index, event_index)` as the natural key.
- **Schema migrations are forward-only.** No `down` migrations. If you need to undo, write a new forward migration.
- **No business logic in the API layer.** Handlers query the store and serialize. Business logic belongs in the chain.
- **Use a real Postgres in tests** (testcontainers or a dev container). No SQL mocks.

## TDD checklist

- Every event parser has a golden-file test (event JSON → expected struct)
- Every store method tested against a real Postgres
- API handlers tested with `httptest` + real store
- Reconnection / catch-up logic tested by killing the WS mid-stream
