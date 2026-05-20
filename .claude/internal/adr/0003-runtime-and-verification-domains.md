# ADR-0003 — Runtime selection and initial verification domain

**Status**: Accepted
**Date**: 2026-05-20
**Decision drivers**: Need a real LLM in the Phase 0.5 demo without coupling Go binaries to CUDA/CGO toolchains; need a runtime story that survives into Phase 1 verification.

## Decision

For Phase 0.5 the inference runtime is **`llama-server` (llama.cpp HTTP server) run as a Docker sidecar**, called from the `inference-node` Go binary via HTTP. The first model is **TinyLlama 1.1B Chat Q4_K_M (GGUF)** running on CPU.

Phase 0.5 declares **zero verification domains**. The active `(model, runtime, precision, hardware)` tuple is recorded as `(tinyllama-1.1b-chat-q4_k_m, llama.cpp-server, Q4_K_M, cpu-x86_64-generic)` but explicitly **disqualified for verification** — single-machine, threaded execution, no SHA pinning, no deterministic-flag enforcement. Phase 1 builds the verification domain registry and qualifies this (or a successor) tuple.

## Considered alternatives

| Option | Pro | Con | Status |
|---|---|---|---|
| **llama.cpp via CGO embedded** | One binary, no IPC | Heavy build toolchain in Go CI; pinning the C++ build is brittle | Rejected |
| **vLLM (Python)** in sidecar | Production-quality for larger models, batching | Heavier image (~3GB), GPU-only for performance | Deferred (Phase 1 candidate) |
| **candle (Rust)** embedded | Pure Rust → could be CGO-free if used from Go via FFI | Less mature; fewer supported model formats | Deferred |
| **Hosted API (Groq / Together / HF)** | Zero ops | Forbidden by CLAUDE.md §6 — non-deterministic, in the verification path | Rejected |
| **`llama-server` sidecar (chosen)** | Pure Go inference-node; swappable; image-SHA identifiable | One extra container; HTTP overhead per call (~ms) | **Chosen** |

## Why a Q4_K_M quantization

A 4-bit K-means GGUF quantization keeps the model under 1 GB, runs at acceptable latency on CPU (no GPU required for the demo), and is the smallest unit of "real LLM" we can demo. Larger / less-quantized models are Phase 1 candidates once GPU support lands.

A documented downside: GGUF quants depend on the quantizer version. Two GGUFs of "the same" TinyLlama produced by different quantizer revisions can differ in numerics. Phase 1 mitigates by pinning by SHA-256 of the file. Phase 0.5 just uses one pinned URL.

## Consequences

- **Inference-node stays pure Go.** No CGO, no C++ toolchain in CI.
- **Runtime is identifiable by container image SHA.** Phase 1's verification domain registry stores `(model_sha256, runtime_image_sha256, precision, hw_tag)` and accepts attestations only for registered tuples.
- **Swapping runtimes in Phase 1 means changing one container.** vLLM, candle, or a custom runtime can replace llama-server without touching the Go code, as long as it speaks the same `/completion` API or we adapt the executor.
- **Phase 0.5 cannot offer a verification guarantee.** This is intentional and documented loudly (banner comments in code, `docs/TUTORIAL.md` §6, README phase callout).

## Open problems

1. **Multi-threading in llama-server**: at `--threads 4` and Q4_K_M, llama.cpp is not bit-exact deterministic across CPU types. Phase 1 must either (a) force `--threads 1` for verification-domain tuples, accepting throughput cost, or (b) prove that K-quantized dequantization plus single-precision accumulation is reproducible across hardware classes. Open research item.
2. **GGUF quantizer version drift**: each new llama.cpp release may produce slightly different Q4_K_M outputs from the same source weights. Phase 1's domain registry pins the file by SHA, but the human story of "I want to add a new model" needs documenting.
3. **Tokenizer canonicalization**: Phase 4 may need a chain-level canonical tokenizer to avoid cross-implementation divergence on edge-case inputs (zero-width joiners, unusual whitespace). Phase 0.5 trusts each runtime's tokenizer; Phase 1 records the tokenizer version in the attestation; Phase 3+ may need stricter canonicalization.

## Operational notes

- Default mirror: `https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF`
- Pinned SHA-256: `c1d18a2caf3046769f2cea7716afa2b664bf1ef6c5dafbf8731ce4abee4a5f7d` (verify against `MODEL_SHA256` env var)
- llama-server image: `ghcr.io/ggerganov/llama.cpp:server`
- Sampling: greedy (`temperature=0`, `top_k=1`, `top_p=1`) — both for Phase 0.5 demo predictability and to match Phase 1+ verification requirements
