# inference-node/ — Off-chain inference worker

Daemon that:
1. Watches the chain for `InferenceRequested` events
2. Runs a pinned model deterministically
3. Submits a signed `(input_hash, output, attestation)` tuple back to the chain

## Critical constraints

- **Determinism is the product.** Every line of code in the inference path must preserve bit-exact reproducibility. See [docs/protocol/verification-protocol.md](../docs/protocol/verification-protocol.md) §3.
- **No hosted APIs.** The node loads weights from disk and runs them in-process via the runtime selected in ADR-0003. No HTTP calls to model providers.
- **Runtime is the inference engine** (e.g. llama.cpp, vLLM, candle). One runtime per node. Multi-runtime is a separate package.
- **Hardware tag is mandatory at startup.** Node refuses to start without a `--hardware-tag` flag matching a known class.

## Layout (target)

```
cmd/inferenced/        Binary entrypoint
internal/
  watcher/             Chain event subscriber
  executor/            Runtime adapter (one impl per engine)
  attestor/            Signs results, builds attestation payload
  submitter/           Posts MsgSubmitResult
testdata/
  golden/              Input → expected output pairs for determinism tests
```

## TDD checklist

- Every executor implementation has a **determinism test**: run the same input twice, assert bit-exact equal outputs.
- Every executor has a **cross-machine test** in CI: run on two hardware classes, assert correct divergence (different domains) or correct convergence (same domain).
- Watcher → executor → submitter wiring tested end-to-end against a local devnet.

## Forbidden

- `math/rand` without an explicit seed sourced from the request
- `time.Now()` anywhere on the inference path (timestamps come from the chain context)
- Goroutine non-determinism: no `select` on multiple channels in the inference path without deterministic ordering
- Any logging that includes wall-clock time inside the attestation payload
