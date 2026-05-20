# Integrate a Client

How to build a frontend, SDK, or CLI that lets users browse services and pay for inference.

← back to [docs/README.md](README.md)

---

## What you'll build

A client (browser dApp, mobile app, CLI tool, automation bot — anything) that:
1. Manages an Ed25519 keypair for the user.
2. Reads service listings from the indexer.
3. Builds, signs, and submits `request_inference` transactions.
4. Watches request status until finalized or refunded.
5. Displays the result.

## Two integration approaches

### A — Direct HTTP (any language)

Talk to the chain's REST/SSE endpoints and the indexer's REST endpoints directly. No SDK required. ~200 lines for a complete client.

### B — Reuse the reference frontend

Clone `frontend/` (Next.js + `@noble/ed25519` + zod). Adapt the UI; the core logic is in `src/lib/wallet.ts` and `src/lib/chain.ts`.

Both approaches use the same APIs. Pick the one that matches your stack.

## Approach A — direct HTTP

### Step 1 — Manage a keypair

Phase 0.5 has no wallet integration; you generate and store the key yourself. See [Signing — key generation](signing.md#key-generation).

Browser:
```ts
import * as ed from "@noble/ed25519";

// Generate
const priv = ed.utils.randomPrivateKey();
const pub  = await ed.getPublicKey(priv);
const addr = "aios1" + (await sha256Hex(pub)).slice(0, 40);

// Persist (Phase 0.5 demo: localStorage. Production: a real wallet.)
localStorage.setItem("priv", bytesToHex(priv));
localStorage.setItem("pub", bytesToHex(pub));
localStorage.setItem("addr", addr);
```

For a CLI: write the keypair to a file in `~/.aios/`.

### Step 2 — Fund the account

Phase 0.5 doesn't have a faucet endpoint. For testing: use one of the bundled dev accounts (`alice`, `bob`) via the chain's `/keys/keys.json` (the chain volume mounts at `/keys` in the bundled stack, and the frontend exposes a `/api/dev-keyring` endpoint when `EXPOSE_DEV_KEYRING=1` is set).

For production (Phase 1+): real wallets connect via Keplr/Leap and users top up via standard Cosmos faucets.

### Step 3 — Read service listings

```ts
const res = await fetch("http://localhost:8081/services");
const { items } = await res.json();
// items = [{ id, owner, name, description, price_denom, price_amount, ... }]
```

Note the indexer's flat shape (`price_denom`, `price_amount`) vs. the chain's nested `price: { denom, amount }`. Match whichever you call.

### Step 4 — Build the request_inference payload

```ts
// SHA-256 the prompt
const promptBytes = new TextEncoder().encode(prompt);
const inputHash = await sha256Hex(promptBytes);

const payload = {
  requester: myAddress,
  service_id: selectedService.id,
  input_hash: inputHash,
  input_uri: "inline:" + inputHash,
  input_text: prompt,
  max_price: { denom: selectedService.price_denom, amount: selectedService.price_amount },
  deadline_height: 0,
};
```

`max_price` should be ≥ the service's price. Setting it equal is fine (and what the reference frontend does).

### Step 5 — Sign and submit

Full Web Crypto-flavored example:

```ts
import * as ed from "@noble/ed25519";

async function signAndSubmit(payload: any) {
  // Get current nonce
  const acc = await (await fetch(`http://localhost:26657/accounts/${myAddress}`)).json();

  // Build envelope (order matters — see signing.md)
  const envelope = {
    type: "request_inference",
    nonce: acc.nonce,
    pub_key_hex: myPubHex,
    payload,
  };
  const canonical = JSON.stringify(envelope);
  const sig = await ed.signAsync(
    new TextEncoder().encode(canonical),
    hexToBytes(myPrivHex),
  );

  const signedTx = { ...envelope, signature_hex: bytesToHex(sig) };
  const res = await fetch("http://localhost:26657/tx", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(signedTx),
  });
  if (!res.ok) throw new Error(`tx failed: ${res.status} ${await res.text()}`);
  return res.json();
  // { type: "request_inference", height: 100, request_id: 1 }
}
```

### Step 6 — Watch for completion

Two patterns. Pick what suits your client.

#### Status state machine (Phase 3+)

A request transitions through these states:

| Status | Meaning | Terminal? |
|---|---|---|
| `PENDING` | Escrow locked, awaiting provider | no |
| `SUBMITTED` | Provider has submitted; challenge window open | no |
| `CHALLENGED` | A challenger filed `MsgChallenge`; resolution pending | no |
| `FINALIZED` | Challenge window expired without dispute; provider got escrow | yes |
| `SLASHED` | Challenge resolved against provider; requester refunded | yes |
| `REFUNDED` | Deadline elapsed before any submission; requester refunded | yes |

Your client should treat `FINALIZED`, `SLASHED`, and `REFUNDED` all as "terminal." On `SLASHED` and `REFUNDED` the requester's escrow returns; on `FINALIZED` the provider keeps it.

### Pattern 1: poll the indexer

```ts
async function waitForResult(requestId: number) {
  for (let i = 0; i < 60; i++) {
    const r = await fetch(`http://localhost:8081/requests/${requestId}`);
    if (r.ok) {
      const data = await r.json();
      switch (data.status) {
        case "FINALIZED": return data;                             // provider paid, you got your inference
        case "SLASHED":  throw new Error("provider was slashed; refunded");  // bad output caught
        case "REFUNDED": throw new Error("request refunded (deadline elapsed)");
      }
      // PENDING, SUBMITTED, CHALLENGED → keep polling
    }
    await new Promise(r => setTimeout(r, 2000));
  }
  throw new Error("timed out");
}

const result = await waitForResult(txResult.request_id);
console.log(result.output_text);
```

Simple, works for any client. Polling interval of 1–3 s is comfortable.

#### Pattern 2: SSE from the chain

For real-time updates, especially if you're showing live status to a user, subscribe to the chain's event stream:

```ts
const url = `http://localhost:26657/events?types=RequestFinalized,RequestRefunded`;
const evtSrc = new EventSource(url);

evtSrc.addEventListener("RequestFinalized", (ev) => {
  const data = JSON.parse(ev.data);
  if (data.payload.request_id === myRequestId) {
    // It's our request! Fetch full details.
    fetch(`http://localhost:8081/requests/${myRequestId}`)
      .then(r => r.json())
      .then(req => showResult(req));
    evtSrc.close();
  }
});
```

`EventSource` is built into browsers. For Node, use `eventsource` from npm.

## Approach B — reuse `frontend/`

The bundled Next.js frontend is ~15 files. Key paths:

| File | Purpose |
|---|---|
| `src/lib/wallet.ts` | Ed25519 keypair generation, persistence, signing |
| `src/lib/chain.ts` | HTTP client for the chain (`getAccount`, `submitTx`, `getStatus`) |
| `src/lib/indexer.ts` | Zod-validated fetch wrappers for the indexer |
| `src/components/RequestInferenceForm.tsx` | The signing UI |
| `src/app/services/[id]/page.tsx` | Service detail + form |
| `src/app/requests/[id]/page.tsx` | Request status + result display (polls indexer) |

Reusable building blocks:

```ts
// Use these as a starting point in your own Next.js / React app.
import { getOrCreateWallet, signTx } from "./wallet";
import { submitTx, getAccount } from "./chain";

const wallet = await getOrCreateWallet();
const account = await getAccount(wallet.address);
const payload = { /* request_inference payload */ };
const signedTx = await signTx(wallet, "request_inference", account.nonce, payload);
const res = await submitTx(signedTx);
```

The wallet stores `{ address, pubKeyHex, privKeyHex }` in `localStorage`. For a production wallet integration (Keplr / Leap), swap the `wallet.ts` impl and keep the call sites unchanged.

## A complete CLI client (Bash + jq + Node)

If you want to integrate from a script, here's a one-shot:

```bash
#!/usr/bin/env bash
# request.sh — submit one inference request from the CLI.
set -euo pipefail

CHAIN=${CHAIN:-http://localhost:26657}
PROMPT="$1"
SERVICE_ID=${SERVICE_ID:-1}

# Sign + submit via Node (because we need Ed25519)
node <<'EOF'
const ed = await import("@noble/ed25519");
const { sha256 } = await import("@noble/hashes/sha256");
const fs = await import("node:fs");

const priv = ed.utils.randomPrivateKey();
const pub = await ed.getPublicKey(priv);
const toHex = b => Array.from(b).map(x => x.toString(16).padStart(2,"0")).join("");
const addr = "aios1" + toHex(sha256(pub)).slice(0, 40);

console.log("Generated wallet:", addr);
// In a real script: fund this address first (transfer from alice/bob).
EOF
```

Most real CLI clients (especially in CI / automation flows) just call the same HTTP endpoints — there's nothing special about Ed25519 that requires a particular language.

## Testing your client

A standalone test:

```ts
import { describe, it, expect } from "vitest";

describe("aios client", () => {
  it("registers, requests, and observes finalization", async () => {
    const wallet = await getOrCreateWallet();
    // (Pre-fund via dev keyring in test setup.)

    const tx = await submitTx(await signTx(wallet, "request_inference", 0, {
      requester: wallet.address,
      service_id: 1,
      input_hash: "abc",
      input_uri: "inline:abc",
      input_text: "hi",
      max_price: { denom: "aios", amount: 100 },
      deadline_height: 0,
    }));

    expect(tx.request_id).toBeDefined();

    // Poll
    let req;
    for (let i = 0; i < 30; i++) {
      req = await fetch(`http://localhost:8081/requests/${tx.request_id}`).then(r => r.json());
      if (req.status === "FINALIZED") break;
      await new Promise(r => setTimeout(r, 2000));
    }
    expect(req.status).toBe("FINALIZED");
    expect(req.output_text).toBeTruthy();
  });
});
```

Run against a live `make demo` stack.

## What changes in Phase 1

When the chain becomes Cosmos SDK + CometBFT (Phase 1):

- **Wallet**: Keplr / Leap support; users won't manage raw keys.
- **Endpoints**: same path layout as today, with the addition of CometBFT-style RPC endpoints (`/abci_query`, `/broadcast_tx_commit`). The Phase 0.5 `POST /tx` route maps to `broadcast_tx_sync`.
- **Address format**: real bech32 with checksumming. Phase 0.5's `aios1 + hex` becomes `aios1...` with a 6-char checksum suffix.
- **Signing**: Cosmos SDK uses Amino / SignDoc canonicalization, not raw JSON. Wallets handle this; your code calls `client.signAndBroadcast(addr, [msg], fee)` instead of building the envelope yourself.

For long-lived integrations: use the reference frontend as a template — it'll be updated to Phase 1 conventions and the deltas will be visible in the diff.

## Common issues

| Symptom | Cause |
|---|---|
| `invalid signature` | Canonical encoding mismatch. JSON key order, spaces, or `signature_hex` accidentally in canonical bytes. |
| `invalid nonce` | Stale nonce. Re-fetch `/accounts/{addr}` before each tx. |
| Request stays `PENDING` forever | No inference node is serving the service. Check that an inference-node container exists for the service's owner. |
| `insufficient funds` | Account needs funding. Use the dev keyring or transfer from alice/bob. |
| CORS errors in browser | Should not happen — chain and indexer set `Access-Control-Allow-Origin: *`. If you see this, you're hitting a service that isn't actually those two. |

## Next

- [Signing](signing.md) — canonical encoding rules
- [Chain API](chain-api.md) — `POST /tx`, `GET /events`, `GET /accounts/{addr}`
- [Indexer API](indexer-api.md) — `GET /services`, `GET /requests/{id}`
- [Tutorial: End-to-end](tutorial-end-to-end.md) — full walkthrough including a client
