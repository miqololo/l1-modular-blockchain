# Architecture

System-level view of the Decentralized AI Operating System.

## One-line pitch

A modular L1 blockchain where AI inference is a first-class on-chain service, made trustworthy via optimistic fraud proofs that re-execute disputed inferences inside a deterministic verification domain.

## Component map

```
┌────────────────────────────────────────────────────────────────────────┐
│                          User / dApp / Agent                           │
└────────────────────────────────┬───────────────────────────────────────┘
                                 │
                  ┌──────────────▼───────────────┐
                  │  Frontend (Next.js + Keplr)  │ ───┐
                  └──────────────┬───────────────┘    │ REST/GraphQL reads
                                 │                    │
                       Tx signing │                    │
                                 │           ┌────────▼──────────┐
                  ┌──────────────▼───┐       │   Indexer (Go +   │
                  │   CometBFT RPC   │       │   Postgres)        │
                  └──────────────┬───┘       └────────▲──────────┘
                                 │                    │
                                 │       WS events    │
              ┌──────────────────▼────────────────────┴───┐
              │       L1: Cosmos SDK + CometBFT           │
              │                                            │
              │  x/aiservice (Go module)                   │
              │    ├─ Register / Request / SubmitResult    │
              │    ├─ Escrow & payment                     │
              │    ├─ Dispute object & challenge logic     │
              │    └─ Slashing                             │
              │                                            │
              │  CosmWasm (phase 5+) — extensions          │
              └─────┬───────────────────────────┬─────────┘
                    │                           │
        InferenceRequested events       MsgSubmitResult / MsgChallenge
                    │                           │
        ┌───────────▼──────────┐    ┌──────────▼───────────┐
        │   Inference Node     │    │   Challenger Node    │
        │   (Go + runtime)     │    │   (Go + runtime)     │
        │                      │    │                      │
        │  - Pinned model     │    │  Re-executes disputed │
        │  - Greedy sampling  │    │  inference inside     │
        │  - Signed attestation│    │  same domain         │
        └──────────────────────┘    └──────────────────────┘

                  ┌─────────────────────────────┐
                  │ Celestia DA (phase 5+)      │
                  │ Stores large blobs:         │
                  │  - Model commitments        │
                  │  - Full attestation payloads│
                  └─────────────────────────────┘
```

## Data flow — happy path inference

1. User signs `MsgRequestInference(service_id, input_hash, max_price)` and broadcasts.
2. Chain validates, escrows funds, emits `InferenceRequested(request_id, service_id, input_hash)`.
3. Provider's inference node sees the event, loads input by hash (off-chain content-addressed store), runs model deterministically.
4. Provider signs attestation: `(request_id, output_hash, model_hash, runtime_version, precision, hw_tag, signature)`.
5. Provider broadcasts `MsgSubmitResult(request_id, attestation)`.
6. Chain validates signature, enters challenge window (N blocks).
7. If no challenge by end of window: escrow → provider, request finalized.

## Data flow — disputed inference (phase 3+)

1. Within challenge window, a challenger sees a submitted result they believe is wrong.
2. Challenger posts `MsgChallenge(request_id, bond)`.
3. Chain emits `Disputed(request_id, challenger)`.
4. Challenger re-executes inference in the same verification domain off-chain.
5. Interactive bisection game OR direct re-execution proof (decision in ADR-0004) determines who is correct.
6. Winner takes both bonds; loser is slashed. Escrow goes to the correct party.

## Trust assumptions

- **At least one honest challenger** is watching disputed-window submissions for each service. Without this, optimistic security collapses.
- **Bounded async** within the challenge window. Heavy network delay can let a malicious prover escape a challenge.
- **Deterministic verification domain**. The protocol is sound only within `(model_hash, runtime_version, precision, hw_class)` tuples that have been validated by the determinism harness.

## Non-goals (explicit)

- **Privacy of inputs/outputs**. The MVP is fully transparent. Encrypted inference is a future research direction.
- **Sub-second latency**. Optimistic challenge windows are seconds-to-minutes. Real-time use cases are out of scope.
- **Closed-source models**. Verification requires public weight files. Closed-weight models can be exposed via a wrapper service but without the verification guarantee.
- **Non-deterministic sampling modes**. Greedy only for the MVP.

## Open architectural questions

These are research / decision items, captured here so we don't pretend they're solved:

1. **Bisection vs re-execution.** For 8B models, re-execution may be cheap enough on-chain (via DA blobs + verifier nodes). For 70B+, bisection is required but the bisection game shape for transformer inference is unsolved. (ADR-0004 will pick.)
2. **Challenger funding.** Who runs challenger nodes and what's their reward? If only the slashed bond pays, challenger ROI may be negative in equilibrium. (ADR-0005.)
3. **Tokenizer determinism.** Different tokenizer implementations (HF transformers, sentencepiece, tiktoken) can disagree on edge cases. We may need to canonicalize input at the chain level. (ADR-0006.)
4. **Quantized model fingerprinting.** GGUF files of "the same" model can differ across quantizer versions. Pin by file hash, not model name. (Documented in `CLAUDE.md` §6.)
