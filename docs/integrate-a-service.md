# Integrate a Service

How to register a new AI service on the aios marketplace so users can pay you to run inference.

← back to [docs/README.md](README.md)

---

## What you'll have at the end

A `Service` row on the chain, owned by an account you control, with a name, description, and price. Users can submit `request_inference` transactions naming your service. Your inference node (Path B) will see those requests, run them, and earn the escrow.

## Prerequisites

1. The aios stack is running (`make demo`).
2. You have (or will create) an Ed25519 keypair to own the service.
3. You have an inference node ready (or plan to set one up — see [Integrate an Inference Node](integrate-an-inference-node.md)).

You don't need to do anything fancy. A service is just a row on the chain.

## Step 1 — Choose your provider key

Two options:

### Option A: use the devnet keyring

Easiest for testing. The chain ships with `alice` and `bob` keys, both funded at genesis. You can register a service as `bob` via the demo endpoint without any client-side signing:

```bash
curl -X POST http://localhost:26657/demo/register-service \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-translate-service",
    "description": "EN-FR translation (demo)",
    "price": 100,
    "key": "bob"
  }'
```

Response:

```json
{ "service_id": 2 }
```

This is exactly how `make demo` seeds its initial service. The dev keyring lives in the `aios-chain` Docker volume at `/keys/keys.json`.

### Option B: bring your own key

Better for anything beyond local testing. You generate the keypair, fund it (Phase 0.5: by transferring from `alice`/`bob`), and sign the `register_service` transaction yourself.

Generate a keypair — see [Signing](signing.md#key-generation) for code samples in TS, Go, and Python.

Derive your address:

```
aios1 + lowercase_hex(sha256(pubkey)[:20])
```

Fund it via a `transfer` from `alice` or `bob`:

```bash
# Sign and submit a transfer tx from alice's account. See `signing.md` for the
# canonical format. The example here uses the demo flow for simplicity.
```

(A polished CLI helper is on the Phase 1 roadmap. For Phase 0.5, you can also pre-fund by editing the chain's keys.json before first boot.)

## Step 2 — Build the `register_service` transaction

Payload shape ([Data Model](data-model.md#register_service)):

```json
{
  "owner": "aios1<your-derived-address>",
  "name": "my-service",
  "description": "What your service does",
  "price": { "denom": "aios", "amount": 100 }
}
```

Constraints:
- `owner` must equal the signer's derived address.
- `name` must be unique across the chain. The chain rejects duplicates.
- `price.amount` must be ≥ `Params.min_service_price.amount` (1 in Phase 0.5).

## Step 3 — Sign and submit

1. Get your account's current nonce:

   ```bash
   curl http://localhost:26657/accounts/aios1abc...
   # { "address": "...", "balance": 100000, "nonce": 0 }
   ```

2. Build the envelope and sign per [Signing](signing.md):

   ```ts
   const envelope = {
     type: "register_service",
     nonce: 0,
     pub_key_hex: walletPubHex,
     payload: {
       owner: walletAddress,
       name: "my-service",
       description: "What your service does",
       price: { denom: "aios", amount: 100 },
     },
   };
   const canonical = JSON.stringify(envelope);
   const sig = await ed.signAsync(new TextEncoder().encode(canonical), privKey);
   const signedTx = { ...envelope, signature_hex: bytesToHex(sig) };
   ```

3. POST to the chain:

   ```bash
   curl -X POST http://localhost:26657/tx \
     -H "Content-Type: application/json" \
     -d "$(cat signedTx.json)"
   ```

   Response:

   ```json
   { "type": "register_service", "height": 100, "service_id": 2 }
   ```

## Step 4 — Verify

```bash
curl http://localhost:26657/services/2
```

Or check the indexer:

```bash
curl http://localhost:8081/services
```

Or open <http://localhost:3030> — your service appears in the list.

## Step 5 — Make sure an inference node will serve it

A registered service is just a listing — it can only fulfil requests if an inference node owned by the same provider is running. See [Integrate an Inference Node](integrate-an-inference-node.md) for the worker side.

The current Phase 0.5 stack has **one** inference-node container, configured to serve requests for whatever service `bob` owns. If you register a service under a different account, requests against it will sit in `PENDING` forever (and eventually `REFUNDED` after the deadline).

For testing: register your service as `bob` (Option A above) and the bundled inference-node will pick it up automatically. For production: run your own inference node — Path B has the full setup.

## Opting into a verification domain (Phase 1+)

Phase 1 introduces the **verification domain registry**: a curated set of `(model, runtime, hardware, precision)` tuples that have proven bit-exact reproducible.

Services can opt in by setting `verification_domain_id` at registration. When set:

- The chain rejects any `submit_result` whose attestation doesn't match the domain's tuple.
- Independent observers (the bundled `determinism-harness`, anyone else) can re-run the inference and verify the provider's `output_hash`.
- Phase 3+: a challenger who finds divergence can dispute the result and slash the provider.

To register a service into the demo domain:

```bash
curl -X POST http://localhost:26657/demo/register-service \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-verified-service",
    "description": "Verified TinyLlama service",
    "price": 100,
    "key": "bob",
    "verification_domain_id": 1
  }'
```

(The Phase 1 demo registers `verification_domain_id=1` automatically when you run `make demo`. Browse domains with `curl http://localhost:26657/domains`.)

Services with `verification_domain_id=0` ("unverified") still work — they just don't have the chain's cross-check protection. Useful for closed-weight models that can't be fingerprinted, or for development.

To register a *new* domain (authority-only):

```bash
curl -X POST http://localhost:26657/demo/register-domain \
  -H "Content-Type: application/json" \
  -d '{
    "model_sha256": "<sha-256 of your model file>",
    "runtime_id": "vllm@0.6.3",
    "hardware_tag": "nvidia-a100-80gb",
    "precision": "fp16",
    "description": "Mistral-7B fp16 on A100",
    "key": "bob"
  }'
```

The chain's first `register_domain` signer becomes the authority. Subsequent registrations must use the same key (Phase 2 moves this to governance).

## Choosing a price

In Phase 0.5 there's no slashing — a malicious provider can pocket the escrow without delivering. Users are aware of this; demo prices are uniformly low (10–100 base units).

Pricing considerations for later phases:
- Larger models cost more compute → higher price
- Longer outputs (more tokens) → higher price
- Verified domains (Phase 1+) command a premium over unverified
- Reputation (Phase 4+) lets established providers charge more

The chain doesn't constrain your price beyond `Params.min_service_price`. Market dynamics handle the rest.

## Naming conventions

Suggested format: `<task>-<src>-<dst>-<variant>`. Examples:
- `translate-en-fr` — translation, English to French
- `summarize-en` — English summarization
- `embed-text-mini` — embeddings, small variant
- `image-gen-sd-15` — image generation, Stable Diffusion 1.5

The chain doesn't enforce a format. Pick something descriptive; the description field is for free-form explanation.

## Updating a service

Phase 0.5 doesn't have an `update_service` message. To change the price or description, you'd register a new service. Phase 2 adds `MsgUpdateService` and `MsgDeactivateService`.

## Removing a service

Phase 0.5 doesn't have a "delete" — services persist forever. Phase 2 adds `MsgDeactivateService` (sets `active=false`; new requests are rejected, existing pending requests still finalize).

## Common issues

| Symptom | Likely cause |
|---|---|
| `service name already registered` | Pick a different name. |
| `invalid signature` | Likely a canonical-encoding mismatch. See [Signing](signing.md#common-pitfalls). |
| `invalid nonce` | Your account's nonce moved (someone else used the same key). Re-fetch `/accounts/{addr}`. |
| Service registered but no inference node serves it | Either register under `bob` (the default provider key), or run your own inference node — see Path B. |

## Next

- [Integrate an Inference Node](integrate-an-inference-node.md) — actually serve requests for your service
- [Chain API](chain-api.md) — full `POST /tx` reference
- [Tutorial: End-to-end](tutorial-end-to-end.md) — register, request, finalize all in one walkthrough
