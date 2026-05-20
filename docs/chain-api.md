# Chain API Reference

The `chain` service exposes everything you need to write transactions, read state, and follow events. All endpoints are HTTP/JSON; the event stream is Server-Sent Events.

Base URL (default): `http://localhost:26657`

← back to [docs/README.md](README.md)

---

## Read endpoints

### `GET /health`

```json
{ "status": "ok" }
```

Used by Docker healthchecks. Always 200 when the process is up.

### `GET /status`

```json
{
  "chain_id": "aios-devnet-1",
  "height": 848,
  "time": "2026-05-20T08:49:09.659212635Z"
}
```

Use to verify the chain is reachable and to learn the current block height.

### `GET /params`

```json
{
  "challenge_window_blocks": 45,
  "min_service_price": { "denom": "aios", "amount": 1 },
  "max_request_deadline_blocks": 10000,
  "provider_bond_amount": { "denom": "aios", "amount": 50 },
  "challenger_bond_amount": { "denom": "aios", "amount": 50 },
  "voucher_bond_amount": { "denom": "aios", "amount": 25 },
  "voucher_reward_amount": { "denom": "aios", "amount": 25 },
  "voucher_margin": 0,
  "service_registration_bond": { "denom": "aios", "amount": 100 },
  "min_service_lifetime_blocks": 1000,
  "voucher_bond_scale_bp": 5000
}
```

Read-only in Phase 3.z. Phase 4 adds `MsgUpdateParams` (governance-gated). New fields by phase:

- `provider_bond_amount`, `challenger_bond_amount` — Phase 3.x bond-economics dispute game.
- `voucher_bond_amount`, `voucher_reward_amount` — Phase 3.y voucher mechanism. `voucher_bond_amount` is now a fallback only — see `voucher_bond_scale_bp` below.
- `voucher_margin` — Phase 3.z step 1. Net excess of provider-side over challenger-side vouchers required to dismiss; default 0 keeps 3.y semantics. **A per-domain override is also available** (Phase 3.z step 3) — see `VerificationDomain.voucher_margin` in the data model.
- `service_registration_bond`, `min_service_lifetime_blocks` — Phase 3.z step 2. Bond locked at `register_service`; refunded on `deactivate_service` only if the service has lived past the lifetime; otherwise forfeit (routed to the treasury account).
- `voucher_bond_scale_bp` — Phase 3.z step 4. Voucher bond at `vouch` time = `r.provider_bond.amount × voucher_bond_scale_bp / 10000`. Default 5000 BP (50%) numerically reproduces the legacy 25-aios voucher / 50-aios provider ratio. Set to 0 to disable scaling and fall back to `voucher_bond_amount`.

### `GET /services`

```json
{
  "items": [
    {
      "id": 1,
      "owner": "aios1...",
      "name": "translate-en-fr",
      "description": "...",
      "price": { "denom": "aios", "amount": 100 },
      "verification_domain_id": 0,
      "active": true,
      "created_at_height": 848
    }
  ]
}
```

All services. For paginated reads, use the indexer (`http://localhost:8081/services`).

### `GET /services/{id}`

One service. Returns 404 if not found.

```json
{
  "id": 1,
  "owner": "aios1...",
  ...
}
```

### `GET /requests`

```json
{
  "items": [ { Request }, ... ]
}
```

Filters:
- `?status=PENDING` — `PENDING`, `SUBMITTED`, `FINALIZED`, `REFUNDED`
- `?service_id=N` — only requests for a specific service

Combine: `GET /requests?status=PENDING&service_id=1`.

### `GET /requests/{id}`

One request, full detail including `result` if status ≥ `SUBMITTED`. See [Data Model](data-model.md).

### `GET /accounts/{addr}`

```json
{
  "address": "aios1...",
  "balance": 1000000000,
  "nonce": 5
}
```

`nonce` is the **next** nonce to use for a transaction from this account. Required for [signing](signing.md).

### `GET /domains` (Phase 1+)

```json
{
  "items": [
    {
      "id": 1,
      "model_sha256": "9fecc3b3...",
      "runtime_id": "llama.cpp-server",
      "hardware_tag": "cpu-x86_64-tinyllama-q4",
      "precision": "q4_k_m",
      "tokenizer_id": "llama.cpp-bpe-v1",
      "description": "...",
      "registered_at_height": 19,
      "active": true
    }
  ]
}
```

### `GET /domains/{id}`

One domain. 404 if not found.

### `GET /authority` (Phase 1+)

```json
{ "authority": "aios1..." }
```

The address of the account authorised to call `MsgRegisterDomain`. Bootstrapped by first-signer-becomes-authority on the first `register_domain` tx; Phase 2 moves this to on-chain governance.

### `GET /events`

Server-Sent Events stream of all typed events.

```
$ curl -N http://localhost:26657/events
event: BlockCommitted
data: {"type":"BlockCommitted","block_height":1,"payload":{"height":1,"time":"..."}}

event: ServiceRegistered
data: {"type":"ServiceRegistered","block_height":2,"payload":{"service_id":1,...}}

: ping
```

Lines starting with `: ` are SSE keepalives (every 15 s).

Filter by type:

```bash
curl -N "http://localhost:26657/events?types=InferenceRequested,RequestFinalized"
```

Comma-separated, no spaces. Unknown types are silently dropped from the stream.

The stream replays a small buffer of recent events (up to ~1024) on connect, then streams live. Newly-connecting consumers see recent history; long-running consumers don't get duplicates.

See [Data Model](data-model.md) for each event's payload shape.

## Write endpoints

### `POST /tx`

Submit a signed transaction.

**Request body**: a `SignedTx` (see [Data Model](data-model.md) and [Signing](signing.md)):

```json
{
  "type": "register_service",
  "nonce": 0,
  "pub_key_hex": "0a1b...",
  "signature_hex": "deadbeef...",
  "payload": { ... }
}
```

**Response (200)**: type-dependent receipt.

For `register_service`:
```json
{ "type": "register_service", "height": 848, "service_id": 1 }
```

For `request_inference`:
```json
{ "type": "request_inference", "height": 849, "request_id": 1 }
```

For `submit_result`:
```json
{ "type": "submit_result", "height": 850, "finalized": true }
```

For `transfer`:
```json
{ "type": "transfer", "height": 851 }
```

For `register_domain` (Phase 1+, authority-only):
```json
{ "type": "register_domain", "height": 852, "service_id": 0 }
```

The `service_id` field is reused as the new domain id (this is a quirk of Phase 0.5/1's shared TxReceipt shape; Phase 2 splits them).

For `challenge` (Phase 3+):
```json
{ "type": "challenge", "height": 105 }
```

Emits a `Challenged` event. The request transitions to `CHALLENGED`. The resolution window then opens for `MsgVouch` transactions; after the window expires the block producer decides:
- ≥ 1 provider-side voucher → `RequestDismissed` event, status returns to `FINALIZED`
- otherwise → `RequestSlashed` event, status `SLASHED`

For `vouch` (Phase 3.y+):
```json
{ "type": "vouch", "height": 108 }
```

Emits a `Vouched` event. The vouch is recorded under `InferenceRequest.vouchers` and decides the resolution at window expiry.

For `update_service` (Phase 2 lifecycle, owner-only):
```json
{ "type": "update_service", "height": 901 }
```

Emits a `ServiceUpdated` event. The signer must own the service and the service must be active. Validates the new price against `MinServicePrice`.

For `deactivate_service` (Phase 2 lifecycle, owner-only):
```json
{ "type": "deactivate_service", "height": 902 }
```

Emits a `ServiceDeactivated` event. The service becomes ineligible for new `request_inference` txs; in-flight requests under it continue to finalize. Idempotent: deactivating an already-inactive service returns `ErrServiceAlreadyInactive`.

For `deactivate_domain` (Phase 2 lifecycle, authority-only; cascade added in Phase 3.z):
```json
{ "type": "deactivate_domain", "height": 903 }
```

Cascades through dependent state. In a single transaction the chain emits:
1. One `DomainDeactivated` event (carries `services_deactivated` + `requests_voided` counts).
2. One `ServiceDeactivated` event per service bound to the domain — registration bonds are refunded in full, **overriding `MinServiceLifetimeBlocks`** because the chain is killing the service, not the operator.
3. One `RequestVoided` event per open request (PENDING / SUBMITTED / CHALLENGED) on those services — every locked party gets their stake back (escrow + provider bond + challenger bond + voucher bonds).

Terminal-status requests (FINALIZED, SLASHED, REFUNDED) are untouched.

For `resolve_challenge` (Phase 3.x+, authority-only):
```json
{ "type": "resolve_challenge", "height": 110 }
```

Resolves an open challenge explicitly with `decision = "dismiss"` or `"slash"`, taking precedence over the timeout-driven resolution in the block producer. Dispatches to the same `executeSlash` / `executeDismiss` helpers used by the block producer, so the resulting bond / escrow flows are identical regardless of which path resolved the dispute.

**Errors (400)**: validation, signature, nonce, or business-rule failures. Body:

```json
{ "error": "invalid nonce: expected 5 got 4" }
```

Common errors:
- `invalid signature` — pubkey doesn't match the sig, or canonical bytes differ
- `invalid nonce: expected N got M` — stale or skipped nonce
- `service not found` — referenced `service_id` doesn't exist
- `insufficient funds: have X need Y` — requester or signer underfunded
- `max_price below service price` — submitted `max_price` < service's price
- `wrong provider for service` — `submit_result` signer isn't the service owner
- `attestation input_hash != request input_hash` — provider submitted for a different request's input

## Demo-only endpoints

These exist for the `make demo` flow and are removed in Phase 1. They sign transactions server-side using the dev keyring.

### `POST /demo/seed`

Idempotent. Registers a default `translate-en-fr` service owned by `bob`.

```json
{ "service_id": 1, "status": "seeded" }
```

Or, if already seeded:

```json
{ "status": "already_seeded", "services": [...] }
```

### `POST /demo/register-service`

```json
{
  "name": "my-service",
  "description": "...",
  "price": 100,
  "key": "bob"
}
```

`key` is the dev keyring name (`alice` or `bob`). Returns `{ "service_id": N }`.

### `POST /demo/request-inference`

```json
{
  "service_id": 1,
  "prompt": "...",
  "key": "alice"
}
```

Returns `{ "request_id": N }`.

### `POST /demo/challenge` (Phase 3+)

```json
{
  "request_id": 1,
  "output_hash": "<challenger's hash>",
  "key": "harness"
}
```

Convenience for manual testing. Returns `{ "ok": true }`. The real challenge path is `POST /tx` with a signed `MsgChallenge`.

### `POST /demo/register-domain` (Phase 1+)

```json
{
  "model_sha256": "9fecc3b3...",
  "runtime_id": "llama.cpp-server",
  "hardware_tag": "cpu-x86_64-tinyllama-q4",
  "precision": "q4_k_m",
  "description": "...",
  "key": "bob"
}
```

Returns `{ "domain_id": N }`. Authority-only — see `/authority`.

### `POST /demo/update-service` (Phase 2)

```json
{
  "service_id": 1,
  "description": "new copy",
  "price_amount": 150,
  "key": "bob"
}
```

Returns `{ "ok": true }`. Signer must own the service. Useful for the demo flow `seed → update → request`.

### `POST /demo/deactivate-service` (Phase 2)

```json
{ "service_id": 1, "key": "bob" }
```

Returns `{ "ok": true }`. After this call, new `request_inference` against the service is rejected with `ErrServiceInactive`.

### `POST /demo/deactivate-domain` (Phase 2)

```json
{ "domain_id": 1, "key": "authority" }
```

Returns `{ "ok": true }`. Authority-only.

### `POST /demo/resolve-challenge` (Phase 3.x+)

```json
{ "request_id": 7, "decision": "dismiss", "key": "authority" }
```

`decision` must be `"dismiss"` or `"slash"`. Authority-only. Returns `{ "ok": true, "decision": "dismiss" }`.

## Internal-only routes (not stable)

These work today but aren't guaranteed across phases:

- The HTTP server uses Go's `net/http` mux with path parameters. There's no versioning prefix yet (no `/v1/...`); Phase 1 will introduce versioning.
- Response shapes may change between Phase 0.5 and Phase 1 — the chain itself is being replaced. The event payloads (`InferenceRequested`, etc.) are designed to carry over; the HTTP wrapper around them may not.

For production integration: depend on the **event shapes**, not the HTTP route layout.

## Rate limits / quotas

None in Phase 0.5. Phase 4+ adds rate limiting for public testnet stability.

## CORS

`Access-Control-Allow-Origin: *` is set globally for browser access. Phase 4+ tightens this for the public testnet.

## Next

- [Indexer API](indexer-api.md) — denormalized read API for clients
- [Signing](signing.md) — build a `SignedTx`
- [End-to-end Tutorial](tutorial-end-to-end.md) — hit these endpoints in sequence
