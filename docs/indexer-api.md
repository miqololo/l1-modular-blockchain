# Indexer API Reference

The `indexer` service mirrors chain state into SQLite and exposes a small read-only REST API for clients that don't want to talk to the chain directly.

Base URL (default): `http://localhost:8081`

← back to [docs/README.md](README.md)

---

## When to use the indexer vs the chain directly

| Read | Recommended source |
|---|---|
| One service by id | Either |
| All services | Indexer (paginated, denormalized for UI) |
| One request by id | Either |
| Filter requests by status/service | Indexer |
| Aggregate stats | Indexer only (`/stats/summary`) |
| Live updates | Chain SSE (`/events`) — indexer doesn't expose SSE |
| Account balance | Chain only (`/accounts/{addr}`) |
| Submit tx | Chain only (`POST /tx`) |

The indexer is *eventually consistent*. After a tx finalizes on the chain, expect ~1–2 seconds before the indexer reflects it.

## Endpoints

### `GET /health`

```json
{ "status": "ok" }
```

### `GET /services`

```json
{
  "items": [
    {
      "id": 1,
      "owner": "aios1...",
      "name": "translate-en-fr",
      "description": "EN→FR translation",
      "price_denom": "aios",
      "price_amount": 100,
      "verification_domain_id": 0,
      "active": true,
      "created_at_height": 848
    }
  ]
}
```

Ordered by `id` ascending. Returns up to 200 items per response (Phase 0.5 default; Phase 1 adds cursor pagination).

**Note the field shapes**: indexer flattens `price` into `price_denom` + `price_amount` (uint64). The chain returns it as a nested `Coin` object.

### `GET /services/{id}`

```json
{
  "id": 1,
  "owner": "aios1...",
  ...
}
```

404 if not found.

### `GET /requests`

```json
{
  "items": [ { Request }, ... ]
}
```

Filters:

- `?status=PENDING` — `PENDING`, `SUBMITTED`, `FINALIZED`, `REFUNDED`
- `?service_id=N` — only requests for a specific service

Ordered by `id` descending (most recent first). Up to 200 items.

### `GET /requests/{id}`

```json
{
  "id": 1,
  "service_id": 1,
  "requester": "aios1...",
  "input_hash": "hex",
  "input_uri": "inline:hex",
  "input_text": "the prompt",
  "escrow_denom": "aios",
  "escrow_amount": 100,
  "deadline_height": 10848,
  "status": "FINALIZED",
  "created_at_height": 848,
  "finalized_at_height": 850,
  "output_hash": "hex",
  "output_uri": "inline:hex",
  "output_text": "...",
  "provider": "aios1...",
  "paid_denom": "aios",
  "paid_amount": 100
}
```

Once `status` is `FINALIZED`, the output fields are populated. For `REFUNDED`, only `finalized_at_height` is set (no output).

Polling pattern for clients:

```js
async function pollRequest(id, ms = 2000, maxTries = 30) {
  for (let i = 0; i < maxTries; i++) {
    const r = await fetch(`http://localhost:8081/requests/${id}`);
    if (r.ok) {
      const data = await r.json();
      if (data.status === "FINALIZED" || data.status === "REFUNDED") return data;
    }
    await new Promise(r => setTimeout(r, ms));
  }
  throw new Error("timed out");
}
```

For live updates, prefer the chain's `/events` SSE over polling — but the indexer's polling endpoint is simpler if you're already on the indexer.

### `GET /stats/summary`

```json
{
  "services_total": 1,
  "requests_total": 1,
  "requests_finalized": 1,
  "requests_pending": 0,
  "requests_refunded": 0
}
```

Aggregate counters, recomputed on each request from the SQLite tables. Cheap to call. Phase 4+ adds time-bucketed metrics.

## CORS

`Access-Control-Allow-Origin: *` is set. The indexer is intended to be reachable directly from browsers.

## Schema and idempotency

The indexer guarantees idempotent ingest: re-processing the same event twice produces the same final state. The natural key is `(block_height, tx_index, event_index)`. In Phase 0.5 these are derived from the chain's SSE event metadata.

The SQLite schema is in `indexer/internal/store/store.go` if you want to peek. The store is read-only from your perspective — you can't write to it via the API.

## Versioning

Like the chain API, the indexer's response shapes will likely change between Phase 0.5 and Phase 1 (when the chain becomes Cosmos SDK + CometBFT). The semantic content (services, requests, stats) stays; the JSON keys may shift to match Cosmos SDK conventions.

For long-lived integrations: depend on the **chain event payloads** (which are designed to carry across phases) and re-derive your own indexer, or use ours and accept some churn.

## Performance

Phase 0.5 expectations:

- `/services`: < 5 ms with up to ~10k services
- `/requests` (filtered): < 50 ms with up to ~100k requests
- `/stats/summary`: < 10 ms

SQLite WAL mode is enabled; concurrent reads don't block. Writes are serialized through the single subscriber goroutine — no contention with reads.

## Next

- [Chain API](chain-api.md) — write side
- [Integrate a Client](integrate-a-client.md) — full client-side reading + writing flow
