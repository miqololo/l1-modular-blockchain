# ADR-0002 — Verification approach: optimistic + fraud proofs

**Status**: Accepted (in principle)
**Date**: 2026-05-20
**Decision drivers**: Must support open-weight LLMs (8B+), must be cheap enough for sub-dollar inference, must not require specialized hardware on every validator.

## Decision

The verification approach for AI inference results is **optimistic with interactive fraud proofs**. Submitted inferences are accepted by default and finalize after a challenge window unless disputed. A successful dispute resolves via bisection or direct re-execution within a registered deterministic verification domain.

Specifically rejected (for the MVP):
- **TEE-based attestation** (Intel SGX / AWS Nitro): single-vendor trust, hardware availability constraints, weakens decentralization story.
- **zkML**: prover overhead is 1000x–10000x naive inference for current schemes; not viable for sub-second latency or sub-dollar pricing on 7B+ models.
- **Reputation only**: no cryptographic guarantee, only economic. Acceptable as a fallback but not as the primary verification.

## Considered alternatives

| Approach | Decentralization | Verification cost | Latency to finality | Status |
|---|---|---|---|---|
| Optimistic + fraud proofs | High | Low (avg) / high (dispute) | Challenge window | **Chosen** |
| TEE attestation | Medium | Low | Sub-second | Future |
| zkML | High | Very high | Minutes | Future |
| Reputation | Low | Zero | Sub-second | Rejected |
| Hybrid (TEE + optimistic) | Medium-High | Low | Sub-second | Future research |

## Open problems

Acknowledged unresolved sub-problems that must be solved before phase 3:

1. **Bisection of transformer inference** — no published canonical bisection game for transformer forward passes. ADR-0004 will commit to a specific approach (likely per-layer re-execution with DA-backed intermediate state).
2. **Challenger economics** — equilibrium analysis pending. ADR-0005.
3. **Verification domain registry** — how domains are added/removed, who validates them, what determinism evidence is required. ADR-0006.

## Consequences

- The project's load-bearing innovation is the dispute game design. Most engineering effort in phases 3–4 goes here.
- We depend on determinism. If the determinism harness shows that no current `(model, runtime, hw)` tuple is bit-exact reproducible, this ADR must be reopened.
- Public testnet demonstrating a successful challenge is the credibility marker for the project.

## Prior art consulted

- Ora — opML paper. _link to be added when cited in protocol spec_
- Gensyn — Verde paper. _link to be added_
- Inference Labs — Sertn protocol. _link to be added_
- Arbitrum Nitro fraud proofs (general bisection model).
- Optimism Cannon (MIPS-based single-step proofs).

Citations will be filled in by the protocol-architect agent during spec drafting.
