# Integrate an Inference Node

How to run a worker that earns fees by serving inference requests for services you own.

← back to [docs/README.md](README.md)

---

## What you'll have at the end

A daemon that:
1. Subscribes to the chain's SSE event stream.
2. Filters `InferenceRequested` events for services owned by your provider account.
3. Runs the underlying model.
4. Signs an attestation and submits `MsgSubmitResult`.
5. Receives the escrow as payment.

The Phase 0.5 stack ships with a reference implementation in Go (`inference-node/`). You can run it as-is for your services, or port the same logic to any language.

## Prerequisites

1. The aios stack is running (`make demo`).
2. You have a registered service (see [Integrate a Service](integrate-a-service.md)).
3. You have the provider account's private key.
4. You have access to some inference runtime (llama.cpp, vLLM, OpenAI-compatible API, your own server — whatever produces text from a prompt).

## Three integration options

### Option 1 — Run the bundled reference

Easiest. The `inference-node` container in the default `docker-compose.yml` already does this for the `bob`-owned default service.

To add a second service under a second account, you'd add another `inference-node` container with different `PROVIDER_KEY` and `LLAMA_SERVER_URL` env vars:

```yaml
inference-node-alice:
  build: { context: ./inference-node }
  environment:
    CHAIN_URL: http://chain:26657
    LLAMA_SERVER_URL: http://llama-server-alice:8080
    PROVIDER_KEY: alice
    KEYRING_PATH: /keys/keys.json
    HARDWARE_TAG: cpu-x86_64-alice-runtime
  volumes:
    - aios-chain:/keys:ro
  depends_on:
    chain: { condition: service_healthy }
    llama-server-alice: { condition: service_healthy }
```

This pattern scales: one inference-node container per provider, each pointing at its own runtime sidecar.

### Option 2 — Fork and modify the reference

Clone `inference-node/` and adapt:

- `internal/executor/llama_http/executor.go` — swap llama-server for your runtime. Implement `Execute(ctx, Request) (Result, error)` returning your model's output.
- `cmd/inferenced/main.go` — wires watcher + executor + submitter. Adjust the event filter if you want to serve multiple services.

The dependency graph is thin: watcher (SSE consumer), executor (calls model), submitter (signs + POSTs). Each is in its own package with no cross-coupling.

### Option 3 — Write your own from scratch

The protocol is open and documented. The full integration is ~200 lines of code in any language. Here's the algorithm:

```
1. Open GET /events?types=InferenceRequested as an SSE stream.
2. For each event:
   a. Parse payload.service_id, payload.request_id, payload.input_text, payload.input_hash.
   b. GET /services/{service_id}. If owner != your address, skip.
   c. GET /requests/{request_id}. If status != "PENDING", skip (might be a re-delivery).
   d. Run your inference: output = model(payload.input_text).
   e. Compute output_hash = sha256_hex(output).
   f. Build the attestation (see Signing).
   g. Sign the attestation with your provider key.
   h. Get your nonce: GET /accounts/{your_address}.nonce.
   i. Build, sign, and POST a submit_result transaction.
3. On stream drop, reconnect with exponential backoff.
```

That's it. The watcher, executor, and submitter are three loose components; nothing forces you to use the reference structure.

## Step-by-step using the reference (Option 1 detail)

### Configure

The reference reads these env vars (defaults in parentheses):

| Env | Default | Purpose |
|---|---|---|
| `CHAIN_URL` | `http://chain:26657` | Chain HTTP base |
| `LLAMA_SERVER_URL` | `http://llama-server:8080` | Inference runtime |
| `KEYRING_PATH` | `/keys/keys.json` | Provider keys file (created by chain on first boot) |
| `PROVIDER_KEY` | `bob` | Which dev key to load |
| `HARDWARE_TAG` | `cpu-x86_64-tinyllama-q4` | Recorded in attestations |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

For a fresh provider account:
1. Generate a keypair off the chain volume (e.g. in your own keystore).
2. Add it to `keys.json` (or mount your own JSON with the same shape — see below).
3. Set `PROVIDER_KEY` to its name.

The `keys.json` schema:

```json
{
  "chain_id": "aios-devnet-1",
  "keys": [
    {
      "name": "alice",
      "address": "aios1...",
      "pub_key_hex": "...",
      "priv_key_hex": "..."
    }
  ]
}
```

`priv_key_hex` is the **64-byte Ed25519 private key** (seed || pubkey, the form `ed25519.GenerateKey` returns in Go) as hex. The reference's `LoadSigner` accepts both 32-byte and 64-byte forms.

### Run

```bash
docker compose up -d inference-node
```

Or directly via Go:

```bash
cd inference-node
go run ./cmd/inferenced \
  --chain http://localhost:26657 \
  --llama http://localhost:8080 \
  --keyring /path/to/keys.json \
  --key-name myprovider \
  --hw-tag cpu-x86_64-myruntime
```

The daemon logs each step:

```
inferenced starting    chain_url=http://chain:26657 provider_key=bob
PHASE 0.5: real inference, UNVERIFIED ...
loaded provider key    name=bob address=aios1e00de12b...
event stream connected url=http://chain:26657/events?types=InferenceRequested
handling inference request   request_id=1 service_id=1 input_hash=79b15fff...
inference produced     request_id=1 output_hash=ab47d68b... output_len=1024
result finalized       request_id=1 finalized=true
```

## Writing your own (Option 3 worked example)

A minimal Node.js inference node, ~100 lines:

```js
// my-inference-node.js
import { request } from "node:http";
import { signAsync, getPublicKey } from "@noble/ed25519";
import { sha256 } from "@noble/hashes/sha256";
import { readFileSync } from "node:fs";

const CHAIN = process.env.CHAIN_URL || "http://localhost:26657";
const KEYS = JSON.parse(readFileSync(process.env.KEYRING_PATH, "utf-8"));
const me = KEYS.keys.find(k => k.name === process.env.PROVIDER_KEY);
const privKey = hexToBytes(me.priv_key_hex.slice(0, 64));  // 32-byte seed

async function runInference(prompt) {
  // Call your own model server — whatever you like.
  const res = await fetch(`${process.env.LLAMA_URL}/completion`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ prompt, n_predict: 256, temperature: 0 }),
  });
  return (await res.json()).content;
}

function bytesToHex(b) { return Array.from(b).map(x => x.toString(16).padStart(2,"0")).join(""); }
function hexToBytes(h) {
  const a = new Uint8Array(h.length / 2);
  for (let i = 0; i < a.length; i++) a[i] = parseInt(h.substr(i*2, 2), 16);
  return a;
}

async function getAccount() {
  const r = await fetch(`${CHAIN}/accounts/${me.address}`);
  return r.json();
}

async function submitResult(reqId, inputHash, output, outputHash) {
  const attestation = {
    provider: me.address,
    model_sha256: "my-model-v1",
    runtime_id: "my-runtime",
    hardware_tag: process.env.HW_TAG || "cpu",
    input_hash: inputHash,
    output_hash: outputHash,
    produced_at_height: 0,
  };
  // Phase 0.5 attestation signature (over a string canonical form).
  const canon = `provider=${attestation.provider}|model=${attestation.model_sha256}|runtime=${attestation.runtime_id}|hw=${attestation.hardware_tag}|in=${inputHash}|out=${outputHash}|h=0`;
  const sigBytes = await signAsync(sha256(new TextEncoder().encode(canon)), privKey);
  attestation.signature_hex = bytesToHex(sigBytes);

  const payload = {
    provider: me.address,
    request_id: reqId,
    result: {
      output_hash: outputHash,
      output_uri: "inline:" + outputHash,
      output_text: output,
      attestation,
    },
  };
  const { nonce } = await getAccount();
  const envelope = {
    type: "submit_result",
    nonce,
    pub_key_hex: me.pub_key_hex,
    payload,
  };
  const txSig = await signAsync(new TextEncoder().encode(JSON.stringify(envelope)), privKey);
  const signedTx = { ...envelope, signature_hex: bytesToHex(txSig) };
  const r = await fetch(`${CHAIN}/tx`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(signedTx),
  });
  console.log("submit:", r.status, await r.text());
}

async function main() {
  const res = await fetch(`${CHAIN}/events?types=InferenceRequested`);
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const lines = buf.split("\n");
    buf = lines.pop();
    for (const line of lines) {
      if (!line.startsWith("data:")) continue;
      const ev = JSON.parse(line.slice(5).trim());
      if (ev.type !== "InferenceRequested") continue;
      const p = ev.payload;

      // Filter to our services
      const svc = await (await fetch(`${CHAIN}/services/${p.service_id}`)).json();
      if (svc.owner !== me.address) continue;

      const output = await runInference(p.input_text);
      const outputHash = bytesToHex(sha256(new TextEncoder().encode(output)));
      await submitResult(p.request_id, p.input_hash, output, outputHash);
    }
  }
}

main().catch(e => { console.error(e); process.exit(1); });
```

That's a complete inference node. Run it against any model you like.

## What the chain validates

When you submit `MsgSubmitResult`, the chain checks:

1. **Signature** verifies against the embedded `pub_key_hex`. Derived address = signer.
2. **Nonce** equals the account's current nonce. (Replay protection.)
3. **`provider` in payload** equals the signer address.
4. **Request exists** and is in `PENDING` status.
5. **Service exists** and the signer is its `owner`.
6. **`attestation.input_hash`** equals the request's `input_hash`. (Anti-substitution.)
7. **Current height ≤ `deadline_height`.** (Late submissions rejected.)
8. **(Phase 1+)** If the service has `verification_domain_id != 0`, the attestation's `(verification_domain_id, model_sha256, runtime_id, hardware_tag, precision)` must exactly match the registered domain.

Pass all checks → escrow released, status → `FINALIZED`.

If the service references a verified domain, you cannot submit unless your runtime configuration matches it. The chain rejects with `attestation does not match service's verification domain` and prints what was expected vs. what you sent.

## What the chain does NOT validate (yet)

Phase 3 (initial): a divergent attestation gets caught and slashed. But the resolution mechanism in this phase is "first-challenger-wins" — if the challenger files within the window, the provider loses. There's no protection yet against a malicious challenger filing a spurious challenge. Phase 3.x adds:
- **Provider bonds**: providers stake on every submission; bond goes to challenger on successful challenge.
- **Challenger bonds**: spurious challenges cost the challenger.
- **Dispute game**: rather than first-mover wins, the chain runs a bisection or interactive re-execution game to determine truth.

Until Phase 3.x: the bundled `determinism-harness` is implicitly trusted (it re-runs faithfully). Don't permit untrusted challengers on a real testnet without bond economics in place.

## Phase 3.x: bond economics

You stake on every submission. Get it right → bond returned. Get it wrong → bond goes to the challenger.

| Param | Default | Purpose |
|---|---|---|
| `Params.ProviderBondAmount` | 50 aios | Locked at `MsgSubmitResult` |
| `Params.ChallengerBondAmount` | 50 aios | Locked at `MsgChallenge` (charged to the challenger, not you) |

Your account needs at least `service_price + provider_bond_amount` to submit. The chain checks this at submit time and rejects with `insufficient funds` if you're short.

## What happens if your output is wrong

Phase 3.x:

1. You submit `submit_result` with an attestation. Chain locks your **provider bond** (50 aios). Status → `SUBMITTED`.
2. The challenge window (`Params.ChallengeWindowBlocks`) starts.
3. The bundled harness (or any other honest verifier) re-runs the prompt independently.
4. If the harness's `output_hash` differs from yours, it files `MsgChallenge`, locking its own **challenger bond**. Status → `CHALLENGED`.
5. After a resolution window with no further action, the chain transitions to `SLASHED`:
   - **Requester** gets the escrow refunded.
   - **You** lose your provider bond — it goes to the challenger.
   - **Challenger** keeps their own bond and gets yours.
6. You walk away with nothing AND a 50-aios loss.

If your output is **correct**: no challenge fires; after the challenge window expires you receive `escrow + provider_bond`. The honest path is unchanged from Phase 1 except for the ~45-block delay.

### Phase 3.y: voucher protection

Phase 3.y closes the spurious-challenger vulnerability. If a malicious account files `MsgChallenge` against your honest output, a 3rd party (the bundled `determinism-harness` by default) can file `MsgVouch` with their own attestation supporting your output. When the chain processes the dispute:

- Provider-side vouchers ≥ 1 → **DISMISSED**: you keep your escrow and bond, plus you get a share of the spurious challenger's forfeit bond as compensation.
- No provider-side vouchers (or more challenger-side) → **SLASHED**: you lose your bond as in Phase 3.x.

For honest providers: as long as at least one voucher with the same verification domain is online and runs honestly, you're protected. Run multiple vouchers (just spin up extra harness instances) for redundancy.

For malicious providers: the slash path is unchanged.

The remaining Phase 3.z work: multi-voucher quorum, voucher staking / sybil resistance. Until then, the system assumes ≥ 1 honest voucher is observing.

## Reliability

The reference implementation handles:

- **SSE reconnect with exponential backoff** (500 ms → 1 s → 2 s → ... → 30 s cap)
- **Nonce-mismatch recovery** — re-fetches the nonce on each tx
- **Idempotency** — if it re-receives an event after a restart, it re-checks the request's status before processing
- **Per-request timeout** — 2 minutes for the inference call

What it does **not** handle in Phase 0.5:

- Concurrent multi-request batching — requests are processed sequentially
- Cancellation / pre-emption if a higher-priority request arrives
- Memory backpressure if the SSE stream outpaces the executor

These are reasonable Phase 0.5 limitations. Adjust if your workload needs them.

## Performance expectations

Reference setup (CPU, TinyLlama 1.1B Q4_K_M, 4 threads):

- Request → result on the wire: 5–15 seconds per request
- Most of the time is in llama-server's `/completion`; chain overhead is ~50 ms
- Throughput: 1 request at a time (no batching in Phase 0.5)

GPU + larger models: substantially faster per-token, but you'll want to add request batching to maximize utilization. That's Phase 4 work.

## Next

- [Signing](signing.md) — provider-side signature details (attestation + envelope)
- [Chain API](chain-api.md) — full SSE + REST reference
- [Data Model](data-model.md) — Attestation, Result, Event payload shapes
- [Tutorial: End-to-end](tutorial-end-to-end.md) — see a full request lifecycle
