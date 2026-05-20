# Prior art notes

Curated notes on related work. The protocol-architect agent uses these for citations; the rest of the team uses them to avoid reinventing.

## Required reading before phase 1

- **Ora — opML**: optimistic ML verification on Ethereum. Read for the core dispute pattern.
- **Gensyn — Verde**: distributed ML training with fraud proofs. Read for the proof aggregation ideas.
- **Inference Labs — Sertn**: inference attestation protocol. Read for the attestation payload shape.
- **Arbitrum Nitro**: general optimistic rollup fraud proofs. Read for bisection game design.
- **Optimism Cannon**: MIPS single-step proofs. Read for the "one instruction" granularity argument.

## How to add a note

One file per project / paper. Filename: `<lowercase-slug>.md`. Sections:
- Citation (URL + paper title + authors + year)
- One-paragraph summary
- What we can borrow
- What doesn't apply to us
- Open questions for the author / community

## Files

- [ora-opml.md](ora-opml.md) — Ora's optimistic ML verification, the closest published cousin to our scheme
- [gensyn-verde.md](gensyn-verde.md) — Gensyn's distributed ML verification, source for our voucher-pool intuition and sybil-bounds questions
- [arbitrum-nitro.md](arbitrum-nitro.md) — Arbitrum's interactive WASM bisection, structural reference for ADR-0004
- [runtime-disqualifications.md](runtime-disqualifications.md) — runtime/precision tuples that have proven non-deterministic under our test conditions

## Outstanding (to be added)

- Optimism Cannon (MIPS single-step proofs) — granularity argument
- Inference Labs Sertn — attestation payload shape comparison
- Modulus Labs / EZKL — zkML contrast for the trust-curve framing
