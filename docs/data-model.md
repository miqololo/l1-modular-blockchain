# Data Model

The on-chain and event types that integrators interact with. Everything in this document is JSON over HTTP — there's no protobuf or special encoding to learn (yet — Phase 1 adds proto).

← back to [docs/README.md](README.md)

---

## Coin

A token amount.

```json
{
  "denom": "aios",
  "amount": 100
}
```

| Field | Type | Notes |
|---|---|---|
| `denom` | string | Currently `"aios"` is the only denom. |
| `amount` | uint64 | Base unit, no decimals. |

## Service

A registered marketplace listing. Owned by one provider account.

```json
{
  "id": 1,
  "owner": "aios1e00de12badd182673668e150dbd50d4bcd0a908f",
  "name": "translate-en-fr",
  "description": "EN→FR translation (TinyLlama demo)",
  "price": { "denom": "aios", "amount": 100 },
  "verification_domain_id": 0,
  "active": true,
  "created_at_height": 848,
  "registration_bond": { "denom": "aios", "amount": 100 }
}
```

| Field | Type | Notes |
|---|---|---|
| `id` | uint64 | Auto-assigned, monotonic from 1. |
| `owner` | string | Bech32-style: `aios1` + 40 hex chars derived from the owner's Ed25519 public key. |
| `name` | string | Unique across the marketplace. Max 256 chars. |
| `description` | string | Human-readable. Optional. |
| `price` | Coin | Per-inference price. Escrowed from the requester on each request. |
| `verification_domain_id` | uint64 | 0 = unverified. Phase 1+: non-zero references a `VerificationDomain` row; the chain enforces tuple match on `submit_result`. |
| `active` | bool | If false, new requests are rejected. |
| `created_at_height` | int64 | Block at which the service was registered. |
| `registration_bond` | Coin | Phase 3.z step 2. The bond actually paid at registration (snapshot of `Params.ServiceRegistrationBond` at the time). Refunded on `deactivate_service` only if `current_height − created_at_height ≥ Params.MinServiceLifetimeBlocks`; otherwise forfeit into the escrow account. Empty/zero on legacy services registered before Phase 3.z step 2. |

## InferenceRequest

A user's request for inference. State transitions through `PENDING → SUBMITTED → FINALIZED` (or `PENDING → REFUNDED` on deadline elapse).

```json
{
  "id": 1,
  "service_id": 1,
  "requester": "aios1e6151fef1bd12f3d4ba162953b1c24d7f7af5bfe",
  "input_hash": "79b15fffaa553f34d514335b5303459e2691222bf741aa1115ef5299ffb00b14",
  "input_uri": "inline:79b15fffaa553f34d514335b5303459e2691222bf741aa1115ef5299ffb00b14",
  "input_text": "Translate hello to French",
  "escrow": { "denom": "aios", "amount": 100 },
  "deadline_height": 10848,
  "status": "FINALIZED",
  "created_at_height": 848,
  "finalized_at_height": 850,
  "result": { ... },
  "paid": { "denom": "aios", "amount": 100 }
}
```

| Field | Type | Notes |
|---|---|---|
| `id` | uint64 | Auto-assigned. |
| `service_id` | uint64 | The service this request targets. |
| `requester` | string | Address of the account that paid escrow. |
| `input_hash` | string | Lowercase hex SHA-256 of the input prompt bytes. |
| `input_uri` | string | Where the prompt lives. Phase 0.5 uses `inline:<hash>` and inlines `input_text` for demo convenience. |
| `input_text` | string | The prompt (Phase 0.5 demo). Phase 1+: fetched from `input_uri` (content-addressed off-chain storage). |
| `escrow` | Coin | Funds locked from the requester. Released to provider on `FINALIZED`, returned to requester on `REFUNDED`. |
| `deadline_height` | int64 | Block by which a result must be submitted; otherwise refund. |
| `status` | enum | `PENDING` \| `SUBMITTED` \| `CHALLENGED` \| `FINALIZED` \| `SLASHED` \| `REFUNDED`. (Phase 3+: `CHALLENGED`, `SLASHED`.) |
| `created_at_height` | int64 | |
| `finalized_at_height` | int64 | Only set on `FINALIZED` or `REFUNDED`. |
| `result` | Result \| null | Populated when status ≥ `SUBMITTED`. |
| `paid` | Coin \| null | Amount paid to provider on `FINALIZED`. |
| `submitted_at_height` | int64 | Phase 3+: when the challenge window opened. |
| `challenges` | Challenge[] | Phase 3+: filed against this request. |
| `provider_bond` | Coin \| null | Phase 3.x+: locked at submit; resolves on terminal status. |
| `vouchers` | Voucher[] | Phase 3.y+: 3rd-party attestations tied-breaking the dispute. |

## Result

The provider's submitted answer.

```json
{
  "output_hash": "ab47d68b...",
  "output_uri": "inline:ab47d68b...",
  "output_text": "Bonjour le monde",
  "attestation": { ... }
}
```

| Field | Type | Notes |
|---|---|---|
| `output_hash` | string | Lowercase hex SHA-256 of the output bytes. |
| `output_uri` | string | Phase 0.5: `inline:<hash>`. Phase 1+: content-addressed pointer. |
| `output_text` | string | The output text (Phase 0.5 demo). |
| `attestation` | Attestation | Signed claim about how the inference was produced. |

## VerificationDomain (Phase 1+)

A registered `(model, runtime, hardware, precision)` tuple. Services opt in to a domain at registration; the chain rejects `submit_result` whose attestation tuple doesn't match the domain's.

```json
{
  "id": 1,
  "model_sha256": "9fecc3b3cd76bba89d504f29b616eedf7da85b96540e490ca5824d3f7d2776a0",
  "runtime_id": "llama.cpp-server",
  "hardware_tag": "cpu-x86_64-tinyllama-q4",
  "precision": "q4_k_m",
  "tokenizer_id": "llama.cpp-bpe-v1",
  "description": "TinyLlama-1.1B Q4_K_M on CPU via llama.cpp",
  "registered_at_height": 19,
  "active": true,
  "voucher_margin": 0
}
```

`tokenizer_id` (Phase 1, optional) — identifier for the tokenizer implementation. Empty (the JSON field is omitted) = legacy "don't check" behavior (preserves backwards compatibility with Phase 0.5–3.z domains). Non-empty = attestations on this domain must declare a matching `tokenizer_id` at `submit_result` / `challenge` / `vouch` time. Pinning catches the case where two runtimes load the same model file but apply different tokenization (BPE merge order, byte fallback rules, special-token handling) — silent determinism breakage that would otherwise look like an honest dispute.

`voucher_margin` (Phase 3.z step 3, optional) — overrides `Params.VoucherMargin` at dispute-resolution time for any request bound to this domain. 0 = inherit the global value (the default; preserves Phase 3.y demo behavior). > 0 = require a net excess of provider-side vouchers. The omitempty JSON tag means this field is absent on the wire for domains registered before Phase 3.z step 3.

| Field | Notes |
|---|---|
| `id` | Auto-assigned, monotonic from 1. |
| `model_sha256` | Lowercase hex SHA-256 of the actual weight file. Phase 1: the chain's demo seed uses `MODEL_SHA256` env to register; production uses governance + reproducible model archives. |
| `runtime_id` | Free-form string identifying the inference runtime (e.g. `llama.cpp-server`, `vllm@0.6.3`). Pinned for the tuple. |
| `hardware_tag` | Free-form string for the hardware class. Different tags = different domains. |
| `precision` | One of `fp32`, `bf16`, `fp16`, `int8`, `q4_k_m`, `q5_k_m`. |
| `description` | Human-readable. |
| `registered_at_height` | When the domain was added. |
| `active` | If false, services using this domain reject new requests (Phase 2 adds `MsgDeactivateDomain`). |

Two attestations are **comparable** only if they share the same domain — that is, identical `(model_sha256, runtime_id, hardware_tag, precision)`. The harness uses this property to validate provider submissions: it re-runs the inference under the same tuple and confirms the output hashes match.

## Attestation (v1, Phase 1+)

Provider's signed claim about the inference run. The signature commits the provider to a specific `(input_hash, output_hash)` pair under a specific verification domain. Phase 3's challenge mechanism uses these commitments — a challenger who can produce a different `output_hash` under the same domain wins the dispute.

```json
{
  "provider": "aios1e00de12badd182673668e150dbd50d4bcd0a908f",
  "verification_domain_id": 1,
  "model_sha256": "9fecc3b3cd76bba89d504f29b616eedf7da85b96540e490ca5824d3f7d2776a0",
  "runtime_id": "llama.cpp-server",
  "hardware_tag": "cpu-x86_64-tinyllama-q4",
  "precision": "q4_k_m",
  "tokenizer_id": "llama.cpp-bpe-v1",
  "input_hash": "79b15fff...",
  "output_hash": "ab47d68b...",
  "produced_at_height": 848,
  "signature_hex": "7d56c8c0..."
}
```

| Field | Notes |
|---|---|
| `provider` | Address that signed. Must equal the service's owner. |
| `verification_domain_id` | Must equal the service's `verification_domain_id` (or both 0). |
| `model_sha256` | Lowercase hex SHA-256 of the weight file. Must match the domain's `model_sha256`. |
| `runtime_id` | Must match the domain's `runtime_id`. |
| `hardware_tag` | Must match the domain's `hardware_tag`. |
| `precision` | Must match the domain's `precision`. |
| `input_hash` | Must match `InferenceRequest.input_hash`. |
| `output_hash` | Must match `Result.output_hash`. |
| `produced_at_height` | Block height at which the provider produced the result. |
| `signature_hex` | Ed25519 signature over the attestation marshalled to JSON with `signature_hex` zeroed. See [Signing](signing.md#attestation-signing). |

## Voucher (Phase 3.y+)

A 3rd party who independently runs the inference and signs an attestation matching one of the disputed outputs.

```json
{
  "voucher": "aios1...",
  "posted_at_height": 110,
  "attestation": { ... },
  "bond": { "denom": "aios", "amount": 25 },
  "supports_provider": true
}
```

| Field | Notes |
|---|---|
| `voucher` | Address of the voucher. |
| `posted_at_height` | Block at which the vouch landed. |
| `attestation` | Voucher's signed attestation. Tuple must match service's verification domain. |
| `bond` | Stake locked at `MsgVouch`. Returned (with reward) if voucher backed the winning side; forfeit otherwise. |
| `supports_provider` | `true` iff `attestation.output_hash == provider's output_hash`. `false` iff it matches the challenger's. A hash matching neither is rejected. |

`InferenceRequest.vouchers` is an array of these. After the resolution window the block producer counts vouchers and resolves the dispute:
- Provider-side vouchers ≥ 1 AND ≥ challenger-side vouchers → `DISMISSED` (request finalizes; challenger forfeits bond)
- Otherwise → `SLASHED` (provider loses bond)

## Challenge (Phase 3+)

A challenger's signed claim that the provider's `output_hash` for a given request is wrong.

```json
{
  "challenger": "aios1...",
  "posted_at_height": 105,
  "attestation": { ... },
  "bond": { "denom": "aios", "amount": 50 }
}
```

| Field | Notes |
|---|---|
| `challenger` | Address that filed the challenge. |
| `posted_at_height` | Block at which the chain accepted the challenge. |
| `attestation` | The challenger's own attestation — same tuple as the service's verification domain, same `input_hash` as the request, but a different `output_hash`. |
| `bond` | Phase 3.x+: challenger's locked stake. Returned to the challenger on `SLASHED`. |

The chain validates the challenger's attestation matches the service's verification domain. Phase 3 simple: any well-formed challenge transitions the request to `CHALLENGED`; after a resolution window the chain trusts the challenger and the provider is slashed. Phase 3.x adds real adjudication (bisection / interactive re-execution) and provider/challenger bonds.

`InferenceRequest.challenges` is an array of these. Phase 3 simple uses only the first.

## Request status state machine (Phase 3.y+)

```
                                       ┌────────────┐
                                       │  PENDING   │
                                       └─────┬──────┘
                                             │ MsgSubmitResult
                                             ▼
                                       ┌────────────┐
                                       │ SUBMITTED  │
                                       └──┬─────┬───┘
        no challenge during window        │     │  MsgChallenge with valid alt
        ┌─────────────────────────────────┘     └─────────────────┐
        ▼                                                          ▼
  ┌───────────┐                                              ┌────────────┐
  │ FINALIZED │                                              │ CHALLENGED │
  │ → provider│                                              └──────┬─────┘
  └───────────┘                                                     │ (Phase 3.y)
                                                                    │  MsgVouch
                                                            ┌───────┴────────┐
                                                            │                │
                                       resolution window    │                │
                                       expires with         ▼                ▼
                                       ≥1 provider-side  ┌──────────┐  ┌────────┐
                                       voucher           │FINALIZED │  │SLASHED │
                                                         │(dismissed│  │→ refund│
                                                         │ provider │  └────────┘
                                                         │ rewarded)│
                                                         └──────────┘
```

## SignedTx

The wire format every chain-mutating call uses.

```json
{
  "type": "request_inference",
  "nonce": 0,
  "pub_key_hex": "0a1b...",
  "signature_hex": "deadbeef...",
  "payload": { ... }
}
```

| Field | Notes |
|---|---|
| `type` | `register_service`, `request_inference`, `submit_result`, `transfer`, `register_domain` (Phase 1+), `challenge` (Phase 3+), `vouch` (Phase 3.y+). |
| `nonce` | Account's current nonce (anti-replay). Must equal `GET /accounts/{addr}.nonce`. |
| `pub_key_hex` | The signer's Ed25519 public key (hex). Address is derived from it. |
| `signature_hex` | Ed25519 signature over `json.Marshal({type, nonce, pub_key_hex, payload})` — see [Signing](signing.md) for canonical-encoding rules. |
| `payload` | Type-specific payload (see below). |

### Payload shapes

#### `register_service`

```json
{
  "owner": "aios1...",
  "name": "string",
  "description": "string",
  "price": { "denom": "aios", "amount": 100 },
  "verification_domain_id": 1
}
```

`owner` must equal the signer's derived address. `verification_domain_id` is optional (defaults 0 = unverified); if non-zero it must reference an existing active domain.

#### `vouch` (Phase 3.y+)

```json
{
  "voucher": "aios1...",
  "request_id": 1,
  "attestation": {
    "provider": "aios1...",
    "verification_domain_id": 1,
    "model_sha256": "...",
    "runtime_id": "...",
    "hardware_tag": "...",
    "precision": "...",
    "input_hash": "...",
    "output_hash": "<hash matching either the provider's or the (first) challenger's>",
    "produced_at_height": 0,
    "signature_hex": ""
  }
}
```

`voucher` must equal the signer. The request must be in `CHALLENGED` status. The voucher's `output_hash` must match either the provider's or the challenger's — a third hash is rejected. One vouch per account per request.

#### `challenge` (Phase 3+)

```json
{
  "challenger": "aios1...",
  "request_id": 1,
  "attestation": {
    "provider": "aios1...",
    "verification_domain_id": 1,
    "model_sha256": "...",
    "runtime_id": "llama.cpp-server",
    "hardware_tag": "cpu-x86_64-tinyllama-q4",
    "precision": "q4_k_m",
    "input_hash": "...",
    "output_hash": "<challenger's hash, different from provider's>",
    "produced_at_height": 0,
    "signature_hex": ""
  }
}
```

`challenger` must equal the signer. The challenger's `attestation` tuple must match the service's verification domain. `attestation.output_hash` must differ from the provider's submitted `output_hash` (otherwise no real dispute). The challenge must arrive within `Params.ChallengeWindowBlocks` of `submitted_at_height`.

#### `register_domain` (Phase 1+, authority-only)

```json
{
  "authority": "aios1...",
  "model_sha256": "9fecc3b3...",
  "runtime_id": "llama.cpp-server",
  "hardware_tag": "cpu-x86_64-tinyllama-q4",
  "precision": "q4_k_m",
  "tokenizer_id": "llama.cpp-bpe-v1",
  "description": "TinyLlama-1.1B Q4_K_M on CPU",
  "voucher_margin": 0
}
```

Only the chain's authority account can call this. Bootstrap: the first signer of `register_domain` becomes the authority (Phase 2 moves this to governance).

`tokenizer_id` (Phase 1, optional) — pins the tokenizer implementation. Empty (omitted) = no check; non-empty = attestations submitted on this domain must declare a matching `TokenizerID`. Choose a stable identifier per tokenizer build (e.g. `llama.cpp-bpe-v1`, `huggingface-tokenizers-fast-0.15`). Once set, providers, challengers, and vouchers on this domain must include the same string in their attestations.

`voucher_margin` (Phase 3.z step 3, optional) — per-domain override of `Params.VoucherMargin`. Set to 0 (or omit) to inherit the global value at resolution time. Set to ≥ 1 to require a net excess of provider-side vouchers over challenger-side vouchers; useful for high-stakes domains that have at least two independent watchers. Cannot be negative.

#### `request_inference`

```json
{
  "requester": "aios1...",
  "service_id": 1,
  "input_hash": "hex",
  "input_uri": "inline:hex",
  "input_text": "the prompt",
  "max_price": { "denom": "aios", "amount": 100 },
  "deadline_height": 0
}
```

`requester` must equal the signer. `max_price` must ≥ service price (the chain escrows only the service price). `deadline_height = 0` means "use the chain's default".

#### `submit_result`

```json
{
  "provider": "aios1...",
  "request_id": 1,
  "result": {
    "output_hash": "hex",
    "output_uri": "inline:hex",
    "output_text": "...",
    "attestation": { ... }
  }
}
```

`provider` must equal the signer and the service's owner. `attestation.input_hash` must equal the request's `input_hash`.

#### `update_service` (Phase 2)

```json
{
  "owner": "aios1...",
  "service_id": 1,
  "description": "new description",
  "price": { "denom": "aios", "amount": 150 }
}
```

`owner` must equal the signer and the service's current owner. The service must be active. `price` must be ≥ `Params.MinServicePrice`. Emits `ServiceUpdated`.

#### `deactivate_service` (Phase 2)

```json
{
  "owner": "aios1...",
  "service_id": 1
}
```

`owner` must equal the signer and the service's current owner. The service must currently be active (else `ErrServiceAlreadyInactive`). Emits `ServiceDeactivated`. In-flight requests on this service continue to finalize; only new `request_inference` is blocked.

#### `deactivate_domain` (Phase 2, authority-only)

```json
{
  "authority": "aios1...",
  "domain_id": 1
}
```

`authority` must equal the chain authority. The domain must exist. Emits `DomainDeactivated`. After this, `submit_result` attestations matching the deactivated domain are rejected with `ErrDomainInactive`. Refunding open requests on services bound to the domain is open work (`verification-protocol.md` §7.5).

#### `resolve_challenge` (Phase 3.x+, authority-only)

```json
{
  "authority": "aios1...",
  "request_id": 7,
  "decision": "dismiss"
}
```

`decision` ∈ `{"dismiss", "slash"}`. `authority` must equal the chain authority. The request must be in `CHALLENGED` status. Dispatches to the same `executeDismiss` / `executeSlash` helpers as the block-producer timeout path, so resulting balance / event flow is identical. Emits `RequestDismissed` or `RequestSlashed`.

## Events

The chain emits typed events on its SSE stream. Each carries a `type`, the `block_height` at which it was emitted, and a `payload` whose shape depends on `type`.

### `ServiceRegistered`

```json
{
  "service_id": 1,
  "owner": "aios1...",
  "name": "translate-en-fr",
  "description": "...",
  "price": { "denom": "aios", "amount": 100 }
}
```

### `Vouched` (Phase 3.y+)

```json
{
  "request_id": 1,
  "voucher": "aios1...",
  "voucher_output_hash": "...",
  "supports_provider": true,
  "bond": { "denom": "aios", "amount": 25 }
}
```

### `RequestDismissed` (Phase 3.y+)

```json
{
  "request_id": 1,
  "provider": "aios1...",
  "challenger": "aios1...",
  "paid": { "denom": "aios", "amount": 100 },
  "provider_bond_returned": { "denom": "aios", "amount": 50 },
  "challenger_bond_forfeit": { "denom": "aios", "amount": 50 },
  "voucher_count": 1
}
```

The challenge was dismissed: provider gets paid normally, challenger's bond is forfeit and distributed as rewards to provider + supporting vouchers. Final `status` is `FINALIZED` (the dismissal restores the honest finalization path).

### `Challenged` (Phase 3+)

```json
{
  "request_id": 1,
  "challenger": "aios1...",
  "challenger_output_hash": "cfa5983b...",
  "provider_output_hash": "b3709aa1..."
}
```

### `RequestSlashed` (Phase 3+)

```json
{
  "request_id": 1,
  "provider": "aios1...",
  "challenger": "aios1...",
  "refunded": { "denom": "aios", "amount": 100 },
  "provider_bond_slashed": { "denom": "aios", "amount": 50 },
  "challenger_bond_returned": { "denom": "aios", "amount": 50 }
}
```

The request transitioned to `SLASHED`. The provider received nothing and lost their `provider_bond_slashed` (Phase 3.x+) to the challenger. The challenger got their `challenger_bond_returned`. The requester got their escrow back.

### `DomainRegistered` (Phase 1+)

```json
{
  "domain_id": 1,
  "model_sha256": "9fecc3b3...",
  "runtime_id": "llama.cpp-server",
  "hardware_tag": "cpu-x86_64-tinyllama-q4",
  "precision": "q4_k_m",
  "description": "..."
}
```

### `InferenceRequested`

```json
{
  "request_id": 1,
  "service_id": 1,
  "requester": "aios1...",
  "input_hash": "hex",
  "input_uri": "inline:hex",
  "input_text": "the prompt",
  "escrow": { "denom": "aios", "amount": 100 },
  "deadline_height": 10848
}
```

This is the event inference-nodes subscribe to.

### `ResultSubmitted`

```json
{
  "request_id": 1,
  "provider": "aios1...",
  "output_hash": "hex",
  "output_uri": "inline:hex"
}
```

### `RequestFinalized`

```json
{
  "request_id": 1,
  "provider": "aios1...",
  "paid": { "denom": "aios", "amount": 100 }
}
```

Phase 0.5: emitted immediately after `ResultSubmitted`. Phase 3+: emitted only after the challenge window closes without a successful challenge.

### `RequestRefunded`

```json
{
  "request_id": 1,
  "requester": "aios1...",
  "refunded": { "denom": "aios", "amount": 100 }
}
```

Emitted by the block producer when a request's deadline elapses without a result.

### `BlockCommitted`

```json
{
  "height": 848,
  "time": "2026-05-20T08:49:09.659212635Z"
}
```

A keepalive emitted every block (~1 s in Phase 0.5).

### `ServiceUpdated` (Phase 2)

```json
{
  "service_id": 1,
  "owner": "aios1...",
  "description": "new description",
  "price": { "denom": "aios", "amount": 150 }
}
```

Indexers should overwrite the cached service row in place. Field-level diffs are not provided; carry the whole post-update state.

### `ServiceDeactivated` (Phase 2; bond accounting added in Phase 3.z step 2)

```json
{
  "service_id": 1,
  "owner": "aios1...",
  "bond_refunded": { "denom": "aios", "amount": 100 }
}
```

Indexers should mark the service `active=false`. In-flight requests remain visible but new `request_inference` is rejected by the chain. `bond_refunded` is empty/zero when the service was deactivated before `Params.MinServiceLifetimeBlocks` (the bond is forfeit, retained in the module escrow account). When the service has lived past the lifetime, `bond_refunded` equals the service's `registration_bond` and was credited back to the owner.

### `DomainDeactivated` (Phase 2; cascade added in Phase 3.z)

```json
{
  "domain_id": 1,
  "authority": "aios1...",
  "services_deactivated": 2,
  "requests_voided": 5
}
```

Indexers should mark the domain `active=false`. The `services_deactivated` and `requests_voided` counts surface the blast radius of the deactivation in a single number — each one corresponds to a `ServiceDeactivated` and `RequestVoided` event that follows in the same block. The `authority` field records which authority key called the deactivation (useful once authority is a multisig in Phase 4).

### `RequestVoided` (Phase 3.z)

```json
{
  "request_id": 42,
  "service_id": 1,
  "domain_id": 1,
  "prior_status": "CHALLENGED",
  "requester": "aios1...",
  "escrow_refunded": { "denom": "aios", "amount": 100 },
  "provider": "aios1...",
  "provider_bond_returned": { "denom": "aios", "amount": 50 },
  "challenger": "aios1...",
  "challenger_bond_returned": { "denom": "aios", "amount": 50 },
  "voucher_bonds_returned": 1
}
```

Emitted when `MsgDeactivateDomain` voids an open request bound to the dying domain. The request transitions to `REFUNDED` regardless of its prior status, and every locked party — requester, provider, challenger, vouchers — receives their stake back. Distinct from `RequestRefunded` (deadline expiry, requester-only payout) because every party is made whole.

- `prior_status` is `PENDING`, `SUBMITTED`, or `CHALLENGED` (terminal statuses are not voided).
- `provider` and `provider_bond_returned` are present iff `prior_status >= SUBMITTED`.
- `challenger` and `challenger_bond_returned` are present iff `prior_status == CHALLENGED`.
- `voucher_bonds_returned` is a count, not a value (multiple vouchers possible, mixed sides; the chain refunds them all).

## Address format

```
aios1 + lowercase-hex(sha256(pubkey_bytes)[:20])
```

40 hex chars after the `aios1` prefix. Example: `aios1e00de12badd182673668e150dbd50d4bcd0a908f`.

Phase 1+: switches to bech32 with proper checksumming. The Phase 0.5 format is intentionally a simple hex tag for demo readability.

## Next

- [Signing](signing.md) — how to construct a signed transaction
- [Chain API](chain-api.md) — endpoints that consume/produce these types
- [Indexer API](indexer-api.md) — denormalized read views of these types
