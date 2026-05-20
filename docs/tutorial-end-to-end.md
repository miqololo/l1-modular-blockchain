# End-to-End Tutorial

Walk one inference request through every component, hand-signing transactions and watching the chain react. ~15 minutes.

← back to [docs/README.md](README.md)

---

By the end of this tutorial you'll have:
- Generated an Ed25519 keypair and computed its `aios1...` address
- Funded it via a `transfer` from one of the dev accounts
- Registered a service you own
- Submitted an inference request against your service
- Watched the inference-node pick it up, run the model, sign the attestation, and finalize the request
- Seen the result both on-chain and in the indexer

We'll use `bash + jq + node` since Node 20 has Ed25519 in its standard library. The same flow works in any language that can sign Ed25519 and POST JSON.

## Setup

```bash
git clone <this-repo> aios
cd aios
make demo
```

After `make demo` completes, you have:
- `bob` registered as the owner of service id 1 (`translate-en-fr`)
- One finalized request from `alice`

Verify:
```bash
curl -s http://localhost:26657/services/1 | jq
curl -s http://localhost:26657/requests/1 | jq .status   # "FINALIZED"
```

We're going to do the same flow, but with our own freshly-generated account.

## Step 1 — Generate a new keypair

```bash
node <<'EOF'
const ed = require('@noble/ed25519');
const { sha256 } = require('@noble/hashes/sha256');

(async () => {
  const priv = ed.utils.randomPrivateKey();
  const pub = await ed.getPublicKey(priv);
  const toHex = b => Array.from(b).map(x => x.toString(16).padStart(2,'0')).join('');
  const addr = 'aios1' + toHex(sha256(pub)).slice(0, 40);
  console.log(JSON.stringify({
    address: addr,
    pub_key_hex: toHex(pub),
    priv_key_hex: toHex(priv),
  }, null, 2));
})();
EOF
```

This won't work directly because `@noble/ed25519` isn't installed in your shell's Node — but the inference-node container has it. Easier: use the frontend container which has the same library:

```bash
docker compose exec frontend node -e '
const ed = require("@noble/ed25519");
const { sha256 } = require("@noble/hashes/sha256");
(async () => {
  const priv = ed.utils.randomPrivateKey();
  const pub = await ed.getPublicKey(priv);
  const toHex = b => Array.from(b).map(x => x.toString(16).padStart(2,"0")).join("");
  const addr = "aios1" + toHex(sha256(pub)).slice(0, 40);
  console.log(JSON.stringify({ address: addr, pub_key_hex: toHex(pub), priv_key_hex: toHex(priv) }));
})();
'
```

Output (yours will differ):

```json
{"address":"aios1abc123...","pub_key_hex":"...","priv_key_hex":"..."}
```

Save these three values — we'll need them.

```bash
export MY_ADDR="aios1abc123..."
export MY_PUB="..."
export MY_PRIV="..."
```

## Step 2 — Fund your account

The chain doesn't have a faucet endpoint in Phase 0.5. Use the bundled `alice` account (1B aios at genesis) to send you some.

Read `alice`'s key from the chain's keyring:

```bash
docker compose exec chain cat /home/aios/.aid/keys.json | jq '.keys[] | select(.name=="alice")'
```

Output:

```json
{
  "name": "alice",
  "address": "aios1e6151...",
  "pub_key_hex": "...",
  "priv_key_hex": "..."
}
```

Use those values to sign a `transfer` of 10,000 aios to your new address. We'll do this inside the frontend container (which has Node + @noble/ed25519):

```bash
docker compose exec -T frontend node <<EOF
const ed = require('@noble/ed25519');

async function main() {
  // Read alice's key from the chain volume (the frontend has /keys mounted).
  const fs = require('fs');
  const ring = JSON.parse(fs.readFileSync('/keys/keys.json', 'utf-8'));
  const alice = ring.keys.find(k => k.name === 'alice');

  const aliceSeed = Buffer.from(alice.priv_key_hex.slice(0, 64), 'hex');

  const myAddr = "${MY_ADDR}";

  // Get alice's current nonce
  const accRes = await fetch('http://chain:26657/accounts/' + alice.address);
  const acc = await accRes.json();

  const payload = {
    from: alice.address,
    to: myAddr,
    amount: { denom: "aios", amount: 10000 },
  };
  const envelope = { type: "transfer", nonce: acc.nonce, pub_key_hex: alice.pub_key_hex, payload };
  const canonical = JSON.stringify(envelope);
  const sig = await ed.signAsync(new TextEncoder().encode(canonical), aliceSeed);
  const signedTx = { ...envelope, signature_hex: Buffer.from(sig).toString('hex') };

  const res = await fetch('http://chain:26657/tx', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(signedTx),
  });
  console.log('transfer status', res.status, await res.text());
}
main().catch(e => { console.error(e); process.exit(1); });
EOF
```

Verify your new account has 10,000:

```bash
curl -s "http://localhost:26657/accounts/${MY_ADDR}" | jq
```

```json
{
  "address": "aios1abc...",
  "balance": 10000,
  "nonce": 0
}
```

## Step 3 — Register a service

Now sign and submit `register_service`. The chain will reject this because Phase 0.5's bundled `inference-node` only serves services owned by `bob` — but that's fine for now; we just want to see the registration flow.

```bash
docker compose exec -T frontend node <<EOF
const ed = require('@noble/ed25519');

async function main() {
  const priv = Buffer.from("${MY_PRIV}".slice(0, 64), 'hex');
  const myAddr = "${MY_ADDR}";

  const accRes = await fetch('http://chain:26657/accounts/' + myAddr);
  const acc = await accRes.json();

  const payload = {
    owner: myAddr,
    name: "tutorial-svc-" + Math.floor(Math.random()*9999),
    description: "Tutorial service",
    price: { denom: "aios", amount: 50 },
  };
  const envelope = { type: "register_service", nonce: acc.nonce, pub_key_hex: "${MY_PUB}", payload };
  const sig = await ed.signAsync(new TextEncoder().encode(JSON.stringify(envelope)), priv);
  const signedTx = { ...envelope, signature_hex: Buffer.from(sig).toString('hex') };

  const res = await fetch('http://chain:26657/tx', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(signedTx),
  });
  console.log('register status', res.status, await res.text());
}
main().catch(e => { console.error(e); process.exit(1); });
EOF
```

Output:

```
register status 200 {"type":"register_service","height":NNN,"service_id":2}
```

Note the `service_id` — yours is service number 2 (service 1 is the demo `translate-en-fr`).

```bash
curl -s http://localhost:26657/services/2 | jq
```

Your service is on the chain. Now anyone could request inference against it… *if* an inference node serves it.

## Step 4 — Request inference against the existing service

Since we don't have an inference node for our own service, let's request against service 1 (which `bob`'s node serves) so we can see the full lifecycle.

Use the demo endpoint with `key: "alice"` to keep it simple, OR sign manually with our key:

```bash
docker compose exec -T frontend node <<EOF
const ed = require('@noble/ed25519');
const { sha256 } = require('@noble/hashes/sha256');

async function main() {
  const priv = Buffer.from("${MY_PRIV}".slice(0, 64), 'hex');
  const myAddr = "${MY_ADDR}";

  const prompt = "Tutorial: please respond.";
  const inputHash = Buffer.from(sha256(new TextEncoder().encode(prompt))).toString('hex');

  const accRes = await fetch('http://chain:26657/accounts/' + myAddr);
  const acc = await accRes.json();

  const payload = {
    requester: myAddr,
    service_id: 1,
    input_hash: inputHash,
    input_uri: "inline:" + inputHash,
    input_text: prompt,
    max_price: { denom: "aios", amount: 100 },
    deadline_height: 0,
  };
  const envelope = { type: "request_inference", nonce: acc.nonce, pub_key_hex: "${MY_PUB}", payload };
  const sig = await ed.signAsync(new TextEncoder().encode(JSON.stringify(envelope)), priv);
  const signedTx = { ...envelope, signature_hex: Buffer.from(sig).toString('hex') };

  const res = await fetch('http://chain:26657/tx', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(signedTx),
  });
  console.log('request status', res.status, await res.text());
}
main().catch(e => { console.error(e); process.exit(1); });
EOF
```

Output:

```
request status 200 {"type":"request_inference","height":NNN,"request_id":N}
```

Capture the `request_id` — say it's `3`.

## Step 5 — Watch the lifecycle

Three things happen now, in order, in different containers:

### 5a. Chain emits `InferenceRequested`

```bash
# Tail the chain's event stream — you'll see your event flow by.
curl -N "http://localhost:26657/events?types=InferenceRequested,ResultSubmitted,RequestFinalized" &
EVT_PID=$!
sleep 20
kill $EVT_PID 2>/dev/null || true
```

You'll see something like:

```
event: InferenceRequested
data: {"type":"InferenceRequested","block_height":...,"payload":{"request_id":3,"service_id":1,"requester":"aios1abc..."...}}

event: ResultSubmitted
data: {"type":"ResultSubmitted","block_height":...,"payload":{"request_id":3,"provider":"aios1e00de...","output_hash":"..."}}

event: RequestFinalized
data: {"type":"RequestFinalized","block_height":...,"payload":{"request_id":3,"provider":"aios1e00de...","paid":{"denom":"aios","amount":100}}}
```

Three events for one request: the chain emits `InferenceRequested` immediately, the inference-node submits a result (`ResultSubmitted`), and the chain immediately finalizes (`RequestFinalized`). Phase 3+ introduces a challenge window where `RequestFinalized` is delayed.

### 5b. Inference-node logs

```bash
docker compose logs inference-node | grep -A1 "request_id=3"
```

Output:

```
... handling inference request   request_id=3 service_id=1 input_hash=...
... inference produced     request_id=3 output_hash=... output_len=...
... result finalized       request_id=3 finalized=true
```

The inference-node picked up the event, called llama-server, signed the attestation, broadcast `submit_result`, and got an immediate-finalize response.

### 5c. Chain final state

```bash
curl -s "http://localhost:26657/requests/3" | jq
```

```json
{
  "id": 3,
  "service_id": 1,
  "requester": "aios1abc...",
  "input_hash": "...",
  "input_text": "Tutorial: please respond.",
  "escrow": { "denom": "aios", "amount": 100 },
  "status": "FINALIZED",
  "result": {
    "output_text": "...the model's reply...",
    "attestation": {
      "provider": "aios1e00de...",
      "input_hash": "...",
      "output_hash": "...",
      "signature_hex": "..."
    }
  },
  "paid": { "denom": "aios", "amount": 100 }
}
```

And your account balance dropped by 100 (the service price):

```bash
curl -s "http://localhost:26657/accounts/${MY_ADDR}" | jq '.balance, .nonce'
# 9900
# 2  ← nonce incremented twice (register_service + request_inference)
```

### 5d. Indexer reflects it

```bash
curl -s "http://localhost:8081/requests/3" | jq
```

Same data, denormalized for query patterns. Browser-facing apps usually read here.

## Step 6 — Check via the UI

Open <http://localhost:3030>. Your request appears in the Requests list; click it to see the full result. The service registry shows both `translate-en-fr` (bob's) and `tutorial-svc-N` (yours).

## What you learned

- An account is just an Ed25519 keypair. The address is derived from the pubkey.
- A service is a row on the chain that someone owns. Registering one is one signed transaction.
- A request is also a row. Submitting one escrows funds from the requester to a module account.
- An inference-node anywhere in the world can subscribe to the chain's SSE stream and serve requests.
- The chain only validates signatures, nonces, balances, and protocol invariants. It does not (yet) validate the *correctness* of the LLM output — that's the Phase 3 fraud-proof game.
- The indexer is an eventually-consistent denormalized read view; the chain is the source of truth.

## Common questions

**Q: Could I run my own inference-node for the service I registered?**

Yes — that's [Integrate an Inference Node](integrate-an-inference-node.md). Spin up an inference-node container with `PROVIDER_KEY` pointing at your keypair and serving your service id.

**Q: What if my request never finalizes?**

Wait up to `Params.max_request_deadline_blocks` blocks (~10000 in Phase 0.5; 1 block per second). After the deadline, the block producer emits `RequestRefunded` and your escrow is returned automatically.

**Q: Can I sign multiple transactions in parallel?**

In Phase 0.5: not without coordination. The chain rejects nonce mismatches strictly. Build a queue locally and increment nonces in order.

**Q: How do I integrate this from JavaScript without containers?**

The chain and indexer are plain HTTP — any browser or Node process can call them. Add `@noble/ed25519` to your project and follow [Integrate a Client](integrate-a-client.md).

## Next

- [Integrate a Service](integrate-a-service.md) — register more services
- [Integrate an Inference Node](integrate-an-inference-node.md) — serve requests for your own services
- [Integrate a Client](integrate-a-client.md) — build a richer client UI
- [Signing](signing.md) — deeper dive into Ed25519 + canonical encoding
