# Gensyn — Verde

**Citation.** Gensyn protocol team. *Verde: a verifiable distributed ML training and inference protocol.* Multi-year research program; whitepapers at `gensyn.ai/papers`; reference implementation in Rust.

## One-paragraph summary

Gensyn's Verde is a verification protocol for distributed ML — both training and inference — built around an interactive proof-of-learning-style game. Submitters publish work claims (typically training-step gradients or inference outputs) backed by a bond. Verifiers re-execute selected steps and dispute on divergence. The protocol uses a referee-graph approach where multiple verifiers can independently flag disagreement, and disputes are resolved by re-execution of a randomly-selected fraction of the trajectory rather than full bisection. Verde leans heavily on probabilistic guarantees ("with high probability, you cannot have cheated more than ε fraction of steps undetected") and on deliberate redundancy in the verifier set.

## What we can borrow

- **Probabilistic-spot-check resolution model.** For very long inference traces (large models, long prompts), full bisection may be impractical even after the bisection. Verde's random-step re-execution is a cheaper, statistically-bounded alternative. Worth comparing to opML's bisection in ADR-0004.
- **Verifier-set redundancy as primary defense.** Verde does not assume "one honest verifier is enough"; it assumes a set, and reasons about k-of-n agreement. This is closer to our Phase 3.y voucher mechanism than opML's two-party model. Our sybil-voucher problem (A3.2) is exactly Verde's k-of-n sybil problem; how they bound n with sybil resistance is directly relevant.
- **Reward-pool aggregation.** Verde's reward-pool-to-verifier-cluster distribution matches our Phase 3.y `executeDismiss` reward pool (split among recipients = provider + provider-side vouchers). Their analysis of when this incentivizes honest re-execution vs free-riding is something we should mirror.
- **Pin-everything attestation.** Verde's attestation lists weight hash, runtime version, hardware fingerprint, and the random seed for sampling (where applicable). Our Attestation v1 (verification-protocol.md §5) matches structurally; the random-seed handling is something we should adopt verbatim once we move beyond greedy decoding.

## What does not transfer

- **Training in scope.** Verde verifies training as well as inference. We deliberately scope ourselves to *inference only* (verification-protocol.md §1.2). Verifying training is a fundamentally different problem (longer traces, more nondeterminism sources, gradient-noise tolerance) and explicitly out of scope until at least Phase 6.
- **Substrate-independent / hardware-flexible.** Verde tries to remain hardware-agnostic by accepting wider determinism tolerances and using statistical agreement. We commit to bit-exact determinism within a registered domain (`hardware_tag` is part of the domain tuple). Strictly stronger constraint; less flexibility, but a cleaner game.
- **Their dispute resolution is not interactive.** Verde uses random-step re-execution as a one-shot test rather than an interactive bisection game. We have not yet committed to one or the other (ADR-0004 open).

## Open questions for the Gensyn team / community

1. **How do you bound the verifier-set sybil problem?** This is our A3.2 (sybil voucher). Verde appears to use stake-weighted reputation; have you found that sufficient at small marketplace sizes, or did it require subsidy?
2. **What hardware-tolerance windows have you found empirically?** Verde's wider determinism tolerance is the alternative path to bit-exact-with-hardware-tags. We chose hard determinism; understanding when tolerance-windows work better would refine our position.
3. **Random-step re-execution: how do you generate the seed?** A predictable seed lets a malicious submitter pre-test which steps will be checked. Verde's seeding mechanism is the operational detail that decides whether the scheme is sound in adversarial conditions.

## Pointers for our work

- Compare Verde's random-step re-execution to opML's interactive bisection in ADR-0004. The choice is real and not yet made.
- Verde's verifier-set economic analysis is the relevant prior art for our Phase 4 challenger-incentive design, since our Phase 3.y voucher mechanism is conceptually a verifier set.

## Required to be cited

In `verification-protocol.md` §9 and in the eventual ADR-0007 ("Verifier-set sybil resistance") if/when that lands.
