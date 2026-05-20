# determinism-harness/ — Phase 0 experiment

**Highest-leverage code in the repo right now.** If this experiment fails, the entire fraud-proof architecture must change.

## Goal

Empirically validate that a chosen open model can produce **bit-exact identical outputs** across:
1. Two runs on the same machine
2. Two different machines of the same hardware class
3. Cold start vs warm start

…under tightly pinned conditions (model hash, runtime version, precision, sampling mode).

## Layout (target)

```
configs/               YAML configs: model + runtime + precision + hw class
inputs/                Curated test prompts (varied lengths, languages, edge cases)
runners/
  llamacpp/            llama.cpp adapter
  vllm/                vLLM adapter
  candle/              candle (Rust) adapter
report/                Output diff reports
pyproject.toml         If Python; or Cargo.toml if Rust
```

## Methodology

1. For each `(model, runtime, precision, hw_class)` tuple:
   - Run all inputs twice locally → assert bit-exact
   - Run on a second machine of same class → assert bit-exact
   - Run on a different class → record divergence (expected)
2. Emit a JSON report per tuple with: hashes, timing, divergence locations (if any), runtime version, env capture.
3. If divergence is found in a "should converge" pair, the tuple is **disqualified** as a verification domain.

## Rules

- **Every run pins**: weight file SHA-256, runtime semver, precision enum, hardware tag, sampling = greedy.
- **Every divergence is investigated, not retried.** A flaky determinism test means the model+runtime is non-deterministic, not that the test is wrong.
- **No hidden state.** Cold start every test. No KV-cache reuse across cases unless explicitly testing cache-affecting code.
- **Reports are committed to the repo** under `report/` as historical evidence.

## Initial candidates to test (decide in ADR-0003)

- Llama-3.1-8B-Instruct, FP16, llama.cpp, single-thread CPU
- Llama-3.1-8B-Instruct, FP16, llama.cpp, CUDA A100
- Mistral-7B-Instruct, FP16, vLLM, A100
- Qwen2.5-7B, INT8, llama.cpp, CPU

The point is not to pick the "best" — it's to find the smallest set of `(model, runtime, hw)` tuples that reproduce reliably.
