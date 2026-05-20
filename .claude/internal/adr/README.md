# Architecture Decision Records

ADRs capture decisions that shape the system and the reasoning behind them.

## Format

Each ADR is a markdown file named `NNNN-<slug>.md`. NNNN is a 4-digit sequence number. Once accepted, ADRs are **immutable** — supersede with a new ADR rather than editing in place.

Required sections:
- **Status**: Draft / Accepted / Superseded by ADR-XXXX
- **Date**
- **Decision drivers**: 1–3 bullets on what forces the decision
- **Decision**: the choice, in 1–3 sentences
- **Considered alternatives**: what else we evaluated and why it lost
- **Consequences**: what becomes easier, what becomes harder
- **Open problems** (optional): sub-problems left unresolved by this ADR

## Index

| # | Title | Status |
|---|---|---|
| 0001 | [Monorepo layout](0001-monorepo-layout.md) | Accepted |
| 0002 | [Optimistic verification](0002-optimistic-verification.md) | Accepted (in principle) |
| 0003 | [Runtime + initial verification domains](0003-runtime-and-verification-domains.md) | Accepted |
| 0004 | [Dispute game shape (3.x bonds + 3.y voucher + 3.z committee)](0004-dispute-game-shape.md) | 3.x/3.y shipped, 3.z+ committee design Accepted (impl deferred) |
| 0005 | Challenger economics | Pending |
| 0006 | Verification domain registry | Pending |
| 0007 | [Voucher sybil resistance](0007-voucher-sybil-resistance.md) | Step 1 Accepted (shipped); steps 2–4 Proposed |

## When to write an ADR

- Any design choice with multi-month consequences
- Any choice between two technologies (DB, runtime, library)
- Any choice that constrains future development
- Any choice that the team would re-litigate without a written record

When in doubt, write one. A two-paragraph ADR is better than a missing one.
