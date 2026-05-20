# Arbitrum Nitro — fraud proofs

**Citation.** Offchain Labs. *Arbitrum Nitro: a second-generation optimistic rollup.* Whitepaper (2022) and ongoing protocol documentation at `docs.arbitrum.io/why-nitro`.

## One-paragraph summary

Arbitrum Nitro is an optimistic Ethereum L2 with a battle-tested interactive fraud proof game. State transitions are advanced off-chain; a validator posts a claimed state root on L1; any other validator can challenge it within a window; the dispute is resolved by an interactive bisection over the WASM execution trace (`arbitrator` is the WASM-on-EVM emulator). The bisection narrows the disagreement to a single WASM instruction, which Ethereum then executes on-chain to determine which side was lying. This is the canonical reference for "how to make an interactive fraud proof actually work in production."

## What we can borrow

- **The interactive bisection structure.** Two parties exchange ranges → midpoint commits → eventually a single-step disagreement → on-chain referee. This is the playbook for our ADR-0004 if we choose the bisection branch. Nitro's specific protocol (one-step proof contract + assertion timing) is reusable as a structural template.
- **Why Nitro switched from MIPS to WASM.** The original Arbitrum used MIPS (like Cannon does); Nitro switched to WASM for tooling and language flexibility. The lessons learned about the trade — instruction-set granularity, gas-cost-per-step, debugger ergonomics — are directly relevant for whether our terminal step should be "one WASM instruction" or "one llama.cpp tensor op."
- **Challenge-period economics.** Nitro's 6.4-day challenge window and its rationale (give honest validators time across major outages) inform our `ChallengeWindowBlocks` discussion. Our 45-block window is sized for the demo, not production; Nitro's reasoning ("the worst-case honest-watcher offline window") is the right lens.
- **Validator-set sizing and incentives.** Nitro has formal "active validator" requirements with stake. Our `determinism-harness` is the analogue but unbonded; a hardening path is to require challengers to register and bond.
- **Two-party dispute as primary game; multi-party only as escalation.** Nitro's main game is two-party (submitter + challenger). Multi-validator agreement is a secondary mechanism. Our Phase 3.y voucher mechanism is structurally different (multi-party from the start), and Nitro's experience suggests that may add complexity without proportional benefit — worth weighing against opML's two-party model.

## What does not transfer

- **EVM execution semantics.** Nitro's terminal step is a WASM instruction within an EVM-state-aware emulator. Our terminal step would be either a tensor op inside an LLM forward pass or a runtime VM step — fundamentally different semantics. The bisection structure is reusable; the single-step proof contract is not.
- **L2 economics.** Nitro's bonds, gas pricing, and validator rewards are tuned for "the L1 settles fraud proofs, the L2 carries the workload." We are not an L2 in this sense (Phase 5 considers Celestia DA, but the chain is its own L1). The economic comparison points don't translate directly; the *shape* of the analysis does.
- **State-machine vs neural-network verification.** Nitro verifies a deterministic state machine over a fixed instruction set. We verify a deterministic forward pass over a fixed model + runtime. The state-machine model is well-defined down to the instruction; the LLM forward pass relies on the runtime to be the state machine — adding a layer (runtime determinism) that Nitro doesn't face.

## Open questions for the Arbitrum / Offchain Labs team / community

1. **Real-world challenge frequency.** What's the actual rate of fraud-proof challenges in Nitro's production? Approximately zero, but characterizing that precisely sets expectations for our challenger market.
2. **Cost of the on-chain one-step proof execution.** What is the gas cost of a single WASM step on Ethereum in the worst case? Our analog on Cosmos has different but related cost dynamics.
3. **Validator coordination failures.** Has Nitro experienced any case where multiple honest validators were offline simultaneously and a wrong assertion progressed further than expected? Their incident reports inform our challenge-window sizing.

## Pointers for our work

- ADR-0004 ("dispute game shape") should explicitly compare opML (rollup-style for ML), Verde (random-step), and Nitro (interactive WASM bisection). Each is a node on the design space.
- The Nitro `arbitrator` codebase is the highest-quality reference implementation of an interactive bisection that exists in production. Worth a code-reading session before we write our own bisection.

## Required to be cited

In `verification-protocol.md` §9 and in ADR-0004.
