# Ora — opML

**Citation.** Ora Protocol team. *opML: optimistic Machine Learning on blockchain.* 2023–2024. Whitepaper and reference implementation on GitHub (`ora-io/opml`).

## One-paragraph summary

opML is an optimistic verification scheme for off-chain ML inference, deliberately structured as the AI-inference analogue of optimistic rollups: a result is published on-chain with a bond; a challenge window opens; anyone with the same model + runtime can re-execute, detect divergence, and post a fraud proof. The dispute is resolved by an interactive bisection game (similar to Arbitrum's) that narrows the disagreement to a single instruction trace step, which is then re-executed by the chain. opML targets larger models than zkML by paying linear (not exponential) overhead for verification at the cost of a synchronous challenge window.

## What we can borrow

- **The optimistic + interactive-bisection shape itself.** Our Phase 0.5 / 3 chooses the same trust model: latency-tolerant, large-model-friendly, ML-prover-light. Our open ADR-0004 ("dispute game shape") explicitly considers a bisection variant.
- **Domain pinning vocabulary.** opML's pinning of model weights + runtime + precision before any dispute makes sense matches our `verification domain` tuple. We use slightly different field names (`hardware_tag` vs opML's processor architecture string) but the idea is the same.
- **Single-step on-chain re-execution as the terminal step.** opML reduces the dispute to a small VM step (Fraud Proof VM, mips-style). Our planned bisection (ADR-0004) does not yet specify the terminal step — opML's choice to commit to a deterministic MIPS-style VM rather than the model's native runtime is something we should evaluate before specifying ours.
- **Engineering: persistent challenger services.** opML's deployment lessons emphasize that an *always-on* challenger is the load-bearing assumption, not a nice-to-have. We mirror this with the `determinism-harness` running by default in the demo stack.

## What does not transfer

- **Substrate.** opML targets EVM L2s (Optimism, Base) using Solidity dispute contracts. We are a Cosmos-adjacent custom Go chain with bbolt persistence (Phase 0.5) → CometBFT (Phase 1+). The dispute mechanism is the same in spirit but a direct port is not possible.
- **Single-runtime-per-domain.** opML in practice runs one canonical runtime per domain. We allow any runtime as long as it produces bit-exact output within the registered domain. This is a strictly looser constraint but introduces a per-runtime determinism-gate burden we own. opML's experience suggests we should expect to register very few runtimes initially.
- **No vouchers / sided supporters.** opML's dispute is two-party (submitter vs challenger) plus an on-chain referee. Our Phase 3.y voucher mechanism is a different design choice that comes with the sybil-voucher problem (A3.2 in our threat model) opML avoids.

## Open questions for the opML team / community

1. **Cross-runtime divergence in practice.** Have you seen real-world divergence between two builds of the *same* MIPS-VM-emulating runtime on different hosts? If yes, how did you remediate? This bears on our cross-host determinism gate (verification-protocol.md §3.2).
2. **Bisection-step gas costs at scale.** What is the on-chain cost of a single MIPS step in the worst case (large model layer)? Our Cosmos chain has similar gas-cost dynamics; a number from production opML would inform whether the bisection is economically viable for >7B models.
3. **Submission rate.** What is the steady-state submission rate per challenger? This sets a watcher's per-second compute requirement and is directly relevant to our Phase 4 challenger-incentive design.

## Pointers for our work

- Treat opML's bisection contract as a structural reference for our ADR-0004.
- The opML *Fraud Proof VM* approach (committing to a fixed MIPS-style VM that simulates the runtime) is an alternative to our domain-registration-of-real-runtimes approach. Both have advantages; the trade is "few canonical VMs + always-on emulation overhead" (opML) vs "many real runtimes + per-runtime determinism gates" (ours).

## Required to be cited

In `verification-protocol.md` §9 and in ADR-0004 once it lands.
