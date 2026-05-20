# Phases & Roadmap

What's in Phase 0.5 (now), and what each later phase adds. Public version of the internal roadmap.

← back to [docs/README.md](README.md)

---

## Where we are

**Phase 3.y — Fraud-proof challenges (dispute game with vouchers).** On top of Phase 3.x's bonds, a third party (a "voucher") can independently re-run the inference and break ties between provider and challenger. Spurious challengers now lose their bond when a voucher backs the provider; honest vouchers are rewarded from the forfeit pool.

`make demo` boots all seven services and finalizes after the 45-block window — provider gets escrow + bond. `make demo-malicious`: provider lies → harness challenges → no defending voucher → `SLASHED` + refund. `make demo-spurious`: alice files a fake challenge against bob's honest output → harness vouches for bob → challenge `DISMISSED` → spurious challenger loses 50 aios, bob keeps everything + reward. See [Getting Started](getting-started.md).

## The whole arc

| Phase | Focus | What's new for integrators |
|---|---|---|
| 0.5 | Demo slice | Marketplace lifecycle, real inference, in-browser demo wallet |
| 1 | Verifiable inference | Domain registry, attestation v1, determinism-harness — *verification observability* |
| 3 | Fraud-proof challenges (initial) | `CHALLENGED`/`SLASHED` statuses, `MsgChallenge`, on-chain auto-slash — *verification enforced* |
| 3.x | Fraud-proof challenges (bonds) | Provider + challenger bonds, slashing economics — *cheating costs money* |
| **3.y (now)** | Fraud-proof challenges (dispute game) | Voucher mechanism per ADR-0004 Option C — *spurious challengers caught too* |
| 3.z | Hardening | Multi-voucher quorum, sybil resistance, bisection fallback |
| 4 | Public testnet + agents | Real testnet endpoints, generative-agent SDK, expanded analytics |
| 5 | Modular DA + CosmWasm | Celestia DA, third-party extension contracts |

> Phase 2 was "marketplace hardening" — service updates, payment flows, governance over authority. Most of that is now folded into the Phase 3 / 3.x work since the challenge mechanism touches all of those concerns. The roadmap stays numbered against the original plan.

> **About "Cosmos SDK swap"**: earlier docs framed Phase 1 as a Cosmos SDK swap. That's now deferred indefinitely — the custom Go chain has demonstrated the full Phase 1 + Phase 3 (initial) pipeline. A future SDK swap is an engineering decision, not a protocol milestone.

## What changes for integrators in each phase

### Phase 1 — Cosmos SDK + verifiable inference

**Breaking for clients:**
- Chain becomes Cosmos SDK + CometBFT. Endpoints change shape: `POST /tx` → `POST /broadcast_tx_sync`, etc. Standard Cosmos RPC.
- Address format upgrades to proper bech32 with checksumming. Phase 0.5 `aios1<40-hex>` becomes `aios1<bech32+checksum>`.
- Transaction signing switches from raw JSON canonical to Cosmos SDK's SignDoc / Amino. Wallets handle this transparently.

**Additive:**
- Keplr / Leap browser wallet integration. The bundled frontend updates to use these instead of the in-browser demo wallet.
- Deterministic-runtime inference: pinned model hash, pinned runtime version, pinned hardware tag. Verification domain registry on-chain.
- Attestation v1: full typed payload (CBOR-encoded), proper Ed25519 signing.

**Migration path:** the message shapes (`MsgRegisterService`, `MsgRequestInference`, `MsgSubmitResult`) and event names carry over. The transport layer changes. Plan to update your client's HTTP calls; the application logic stays.

### Phase 2 — Marketplace hardening

**Additive:**
- `MsgUpdateService` — change description, price, or `active` flag.
- `MsgDeactivateService` — soft-delete a service (existing pending requests still complete).
- Multi-provider support per service (load balancing, failover).
- 100-request soak test passes — production-ready for early users.

**Breaking:** none expected; this is mostly extensions.

### Phase 3 (initial) — Fraud-proof challenges (current)

**Additive:**
- `MsgChallenge` — any account can dispute a submitted result by attaching their own attestation with a different `output_hash`.
- `Params.ChallengeWindowBlocks = 45`. `submit_result` no longer finalizes immediately — the request stays in `SUBMITTED` until the window expires (no challenge → `FINALIZED`) or a challenge arrives (`SUBMITTED → CHALLENGED → SLASHED`).
- New statuses: `CHALLENGED` (challenge pending resolution), `SLASHED` (provider lost; requester refunded).
- New events: `Challenged`, `RequestSlashed`.
- `harness` keypair created at genesis and funded — the bundled determinism-harness is now an active participant, not just an observer.
- Demo: `make demo-malicious` enables `MALICIOUS_PROVIDER=1`, watches it get caught.

**Breaking for clients:**
- `RequestFinalized` arrives `ChallengeWindowBlocks` after `ResultSubmitted` (~45 seconds at 1 s blocks). Polling logic should handle the new wait.
- `CHALLENGED` and `SLASHED` are new terminal-ish statuses.

**Breaking for providers:**
- Lying now has consequences. `MALICIOUS_PROVIDER=1` is what bad code looks like; the chain catches it.

### Phase 3.x — Fraud-proof challenges (hardened with bonds) — current

**Additive (shipped):**
- **Provider bond** (`Params.ProviderBondAmount`, default 50 aios). Locked at `MsgSubmitResult`; released on `FINALIZED`; transferred to challenger on `SLASHED`.
- **Challenger bond** (`Params.ChallengerBondAmount`, default 50 aios). Locked at `MsgChallenge`; returned to challenger on `SLASHED`.
- **`RequestFinalized.ProviderBondReturned`** field — observers can see the bond reflow.
- **`RequestSlashed.ProviderBondSlashed`** + **`ChallengerBondReturned`** fields.
- **`InferenceRequest.ProviderBond`** + **`Challenge.Bond`** fields exposed via REST.

**Breaking for providers:**
- You now need `service_price + provider_bond_amount` in your account to submit. Underfunded providers get `insufficient funds` errors.
- Bad output is now economically costly, not just zero-EV.

### Phase 3.y — Fraud-proof challenges (dispute game) — current

**Additive (shipped):**
- `MsgVouch` tx — anyone with the verification domain can submit a tiebreaking attestation.
- `Params.VoucherBondAmount` (25 aios) — voucher's stake.
- `Params.VoucherRewardAmount` (25 aios) — paid from the forfeit pool.
- New event `Vouched` — emitted when a vouch lands.
- New event `RequestDismissed` — emitted when the chain decides in the provider's favor.
- New terminal outcome: a challenge can now end in `DISMISSED` (request becomes `FINALIZED`), not just `SLASHED`.
- Harness updated: subscribes to `Challenged` events and defends honest providers by vouching.
- Make target `make demo-spurious` — full end-to-end demonstration.

**Breaking for challengers:** spurious challenges now lose their bond. Run a voucher if you're a serious participant and want defensive coverage of your own services.

### Phase 3.z — Hardening — proposed

- Multi-voucher quorum: today one voucher decides — production needs N-of-M.
- Stake-weighted vouching: prevent sybil attacks where the same operator runs many vouchers.
- Bisection fallback (ADR-0004 Option B) for tied or high-value disputes.
- `MsgDeactivateDomain`, `MsgUpdateService`.

### Phase 4 — Public testnet + agents

**Additive:**
- Public testnet endpoints (no localhost required).
- Generative-agent SDK: account abstraction, on-chain spending caps, programmatic request submission.
- Expanded analytics: provider performance metrics, request latency distributions, time-bucketed volume.
- Rate limiting on public endpoints.

**Breaking:** CORS narrows; clients may need to use authenticated endpoints for some operations.

### Phase 5 — Modular DA + CosmWasm

**Additive:**
- Celestia / Avail integration for data availability. Large attestation payloads move off the main chain.
- CosmWasm extension contracts: third parties can deploy auction logic, custom listing types, reputation pools, etc. on top of the core marketplace.

**Breaking:** none expected.

## Stability guarantees

We don't have formal stability guarantees yet — Phase 0.5 through Phase 3 will change shape as we learn. After Phase 3 (when verification lands), expect:

- **Event payload schemas** become stable. Once `InferenceRequested` v1 is shipped, fields are only added (never removed or renamed).
- **Message types** become stable. `MsgRequestInference` v1 doesn't change shape.
- **Endpoint paths** become stable with version prefixes (`/v1/services`, `/v1/tx`).

Until then: code defensively, use the indexer's denormalized view where possible, and watch the changelogs for each phase release.

## Migration help

Each phase release will include:
- A `MIGRATION.md` listing breaking changes and the path from the previous phase
- A migration script (where applicable) for indexer / database schemas
- A bridge period where both old and new endpoints work simultaneously (where feasible)

## Next

- [Architecture](architecture.md) — the current Phase 0.5 system
- [Getting Started](getting-started.md) — try the demo
- One of the integration guides for your role
