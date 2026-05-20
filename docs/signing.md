# Signing Transactions

Everything you need to construct a signed transaction the chain will accept.

← back to [docs/README.md](README.md)

---

## TL;DR

1. Build the payload (type-specific JSON object — see [Data Model](data-model.md)).
2. Fetch your account's current nonce: `GET /accounts/{your_address}`.
3. Build the unsigned envelope:
   ```json
   {
     "type": "<tx-type>",
     "nonce": <current_nonce>,
     "pub_key_hex": "<your_pubkey_hex>",
     "payload": { ... }
   }
   ```
4. Compute `canonical = json.Marshal(envelope_above)` — this is what gets signed. **The `signature_hex` field is NOT in the canonical bytes.**
5. Sign with Ed25519: `signature = ed25519.Sign(privkey, canonical)`.
6. Build the final `SignedTx`:
   ```json
   {
     "type": "...",
     "nonce": ...,
     "pub_key_hex": "...",
     "signature_hex": "<hex(signature)>",
     "payload": { ... }
   }
   ```
7. `POST /tx` to the chain. Response is the tx receipt.

## Canonical encoding

The chain re-serializes your envelope using Go's `encoding/json` and verifies the signature against that. To produce bit-identical canonical bytes from any language, follow Go's `encoding/json` conventions:

- Struct field order in the canonical struct (NOT alphabetical):
  ```
  type SignedTx struct {
      Type         TxType          `json:"type"`
      Nonce        uint64          `json:"nonce"`
      PubKeyHex    string          `json:"pub_key_hex"`
      SignatureHex string          `json:"signature_hex"`  // EXCLUDED from canonical
      Payload      json.RawMessage `json:"payload"`
  }
  ```
  Canonical bytes use the same order **minus `signature_hex`**.
- HTML-escape special characters (`<`, `>`, `&`) — Go does this by default.
- No trailing newline.
- Whitespace: none. Go's `json.Marshal` produces compact output.

### Reference: Go

```go
canonical := SignedTx{
    Type:      "request_inference",
    Nonce:     0,
    PubKeyHex: "0a1b...",
    Payload:   payloadBytes,
    // SignatureHex deliberately empty
}
bz, _ := json.Marshal(canonical)
sig := ed25519.Sign(priv, bz)
canonical.SignatureHex = hex.EncodeToString(sig)
```

### Reference: TypeScript (in-browser)

```ts
import * as ed from "@noble/ed25519";

const envelope = {
  type: "request_inference",
  nonce: 0,
  pub_key_hex: walletPubHex,
  payload: { ... },
};
const canonical = JSON.stringify(envelope);
const sig = await ed.signAsync(new TextEncoder().encode(canonical), privKeyBytes);

const signedTx = {
  ...envelope,
  signature_hex: bytesToHex(sig),
};
```

**Critical**: the TypeScript object's property order must match Go's struct order: `type`, `nonce`, `pub_key_hex`, `payload`. ES2015+ preserves insertion order for string keys, so this works as long as you build the object in the right order. Don't spread anything in between or you'll change the order.

### Reference: Python

```python
import json, nacl.signing

envelope = {
    "type": "request_inference",
    "nonce": 0,
    "pub_key_hex": pub_hex,
    "payload": payload,
}
# Python's json.dumps preserves insertion order in 3.7+.
canonical = json.dumps(envelope, separators=(",", ":"))
signing_key = nacl.signing.SigningKey(priv_bytes)
sig = signing_key.sign(canonical.encode()).signature

signed_tx = {**envelope, "signature_hex": sig.hex()}
```

Note `separators=(",", ":")` — without it, `json.dumps` adds spaces and the signature won't verify.

## Key generation

The chain accepts any standard Ed25519 keypair. Generate one however you like:

### Browser

```ts
import * as ed from "@noble/ed25519";
const priv = ed.utils.randomPrivateKey();      // 32 bytes
const pub  = await ed.getPublicKey(priv);      // 32 bytes
const address = "aios1" + sha256(pub).slice(0, 20).hex();
```

### Go

```go
pub, priv, _ := ed25519.GenerateKey(rand.Reader)
// priv is 64 bytes (seed||pub), pub is 32 bytes
addressHash := sha256.Sum256(pub)
address := "aios1" + hex.EncodeToString(addressHash[:20])
```

### Python

```python
import nacl.signing
sk = nacl.signing.SigningKey.generate()
pub = bytes(sk.verify_key)              # 32 bytes
priv = bytes(sk)                         # 32 bytes (seed only)
import hashlib
address = "aios1" + hashlib.sha256(pub).hexdigest()[:40]
```

## Address derivation

```
address = "aios1" + lowercase_hex(sha256(pubkey_bytes)[:20])
```

Where `pubkey_bytes` is the **32-byte Ed25519 public key**, not the 64-byte private key form.

The chain validates that the signer's derived address matches the addresses embedded in payloads (e.g. `payload.owner` for `register_service`, `payload.requester` for `request_inference`, `payload.provider` for `submit_result`).

## Nonce management

Every account has a nonce that starts at 0 and increments on every accepted transaction. Replays of the same nonce are rejected.

To get your current nonce:

```bash
curl http://localhost:26657/accounts/aios1abc...
# { "address": "...", "balance": 1000000000, "nonce": 0 }
```

If you submit transactions concurrently from the same account, you must allocate nonces in order locally. There's no nonce-gap support in Phase 0.5 — a tx with nonce=5 will be rejected if the account's current nonce is 3.

## Attestation signing (provider-side) — Phase 1+

Providers sign attestations with the same encoding rule as tx envelopes: JSON-marshal the struct with `signature_hex` zeroed, sign the bytes directly with Ed25519, hex-encode the signature.

```go
// chain/internal/types/types.go — the canonical reference
clone := attestation
clone.SignatureHex = ""
canonical, _ := json.Marshal(clone)
sig := ed25519.Sign(priv, canonical)
attestation.SignatureHex = hex.EncodeToString(sig)
```

The Go struct field order is the canonical order (Go's `encoding/json` marshals struct fields in declaration order):

```
provider, verification_domain_id, model_sha256, runtime_id, hardware_tag,
precision, input_hash, output_hash, produced_at_height, signature_hex
```

Implementations in other languages must build the JSON object in this exact order. TypeScript example:

```ts
const att = {
  provider: providerAddr,
  verification_domain_id: domainId,
  model_sha256: modelSha,
  runtime_id: runtimeId,
  hardware_tag: hwTag,
  precision: precision,
  input_hash: inputHash,
  output_hash: outputHash,
  produced_at_height: height,
};
const canonical = JSON.stringify(att);  // signature_hex absent → omitted
const sig = await ed.signAsync(new TextEncoder().encode(canonical), privKey);
const signed = { ...att, signature_hex: bytesToHex(sig) };
```

Domain pinning: the chain re-verifies `(model_sha256, runtime_id, hardware_tag, precision)` against the registered domain on every submission. A mismatch returns `attestation does not match service's verification domain`.

> Phase 1 uses ordered-JSON canonicalization. Phase 2+ may switch to typed CBOR; the schema is forward-compatible.

## Common pitfalls

1. **Including `signature_hex` in the canonical bytes**. The signature signs the envelope *without* its own signature field. Build a copy without `signature_hex`, marshal, then sign.
2. **Wrong field order in TypeScript/JSON**. Always: `type`, `nonce`, `pub_key_hex`, `payload`.
3. **Pretty-printing the JSON**. Compact JSON (no spaces, no newlines) is required.
4. **Mismatched address**. Your envelope's `pub_key_hex` derives an address; that address must match the address fields in your payload.
5. **Stale nonce**. If you have multiple concurrent senders for one account, you'll get nonce-mismatch errors. Fetch fresh between sends, or maintain a local counter.
6. **Wrong Ed25519 form**. Some libraries return 64-byte private keys (seed + pubkey concatenated); others return 32-byte seeds. The `@noble/ed25519` and Python `nacl` libraries expect 32-byte seeds; Go's `ed25519.Sign` accepts the 64-byte form.

## Verifying a signature (server-side)

This is what the chain does, for completeness:

```go
pub, _ := hex.DecodeString(tx.PubKeyHex)
sig, _ := hex.DecodeString(tx.SignatureHex)

clone := tx
clone.SignatureHex = ""
canonical, _ := json.Marshal(clone)

if !ed25519.Verify(pub, canonical, sig) {
    return ErrInvalidSignature
}

signerAddr := "aios1" + hex.EncodeToString(sha256.Sum256(pub)[:20])
// signerAddr must match payload.owner / payload.requester / payload.provider
```

## Next

- [Chain API](chain-api.md) — `POST /tx` reference
- [Integrate a Client](integrate-a-client.md) — complete signing flow for browser dApps
- [Integrate an Inference Node](integrate-an-inference-node.md) — provider-side signing
