---
name: verify-determinism
description: Run the determinism harness against a (model, runtime, precision, hardware) tuple to validate bit-exact reproducibility. Use when adding a new model/runtime to the verification path, debugging non-determinism, or before promoting a tuple to production verification domain status.
---

# Verify Determinism

Run the determinism harness for a specific verification-domain tuple and produce a JSON report.

## Inputs

1. **Model**: SHA-256 of the weight file (or path to weight file, from which we'll compute hash)
2. **Runtime**: name + version (e.g. `llama.cpp@b3825`, `vllm@0.6.3`)
3. **Precision**: `fp32` / `bf16` / `fp16` / `int8` / `q4_k_m` / `q5_k_m`
4. **Hardware tag**: one of the registered classes (e.g. `nvidia-a100-80gb`, `cpu-x86_64-avx512`)
5. **Input set**: which input file(s) under `determinism-harness/inputs/`

## Process

### Step 1: Pre-flight checks
- Confirm the weight file hash matches what's claimed (compute and compare)
- Confirm runtime version matches by querying the binary
- Confirm hardware class matches expected (read `/proc/cpuinfo`, `nvidia-smi --query-gpu=name`)
- Confirm sampling config is greedy (temperature=0, top_k=1)

If any pre-flight fails, **stop** and report. Do not proceed with bad inputs.

### Step 2: Same-machine determinism
- Run the full input set twice with cold caches
- Diff outputs byte-for-byte
- Record: pass/fail, divergence locations if any

### Step 3: Cross-machine determinism (if a second machine of the same class is available)
- Run on machine A and machine B
- Diff outputs
- Record: pass/fail

If only one machine of the class is available, mark cross-machine as `unverified`, not `passed`.

### Step 4: Expected-divergence sanity check
- Run on a *different* hardware class
- Confirm outputs DO diverge (if they don't, your "different class" tag is wrong)

### Step 5: Emit JSON report
Write to `determinism-harness/report/<timestamp>-<model_hash_short>-<runtime>-<precision>-<hw_tag>.json`:

```json
{
  "tuple": {
    "model_sha256": "...",
    "runtime": "llama.cpp@b3825",
    "precision": "fp16",
    "hardware_tag": "nvidia-a100-80gb"
  },
  "results": {
    "same_machine": { "status": "pass", "runs": 2, "divergences": [] },
    "cross_machine": { "status": "pass" | "unverified", "machines": [...], "divergences": [] },
    "expected_divergence_check": { "status": "pass", "compared_against": "cpu-x86_64-avx512" }
  },
  "env": {
    "kernel": "...",
    "driver": "...",
    "cuda": "..."
  },
  "verdict": "QUALIFIED" | "DISQUALIFIED" | "INCONCLUSIVE"
}
```

### Step 6: Promote or disqualify
- `QUALIFIED` → tuple is added to the registry of valid verification domains
- `DISQUALIFIED` → tuple is added to `.claude/internal/prior-art/runtime-disqualifications.md` with the divergence evidence
- `INCONCLUSIVE` → re-run with more inputs or escalate to ml-determinism-engineer

## Forbidden during this skill

- Retrying a failed determinism run "to see if it passes this time". A flake **is** a fail. Diagnose, don't retry.
- Skipping the expected-divergence check. If you don't confirm that different hw classes actually produce different outputs, your tagging is unverified.
- Running with non-greedy sampling. Different topic entirely.

## Output

```
## Determinism verdict: QUALIFIED / DISQUALIFIED / INCONCLUSIVE

Tuple: (model=<hash[:8]>, runtime=<v>, precision=<p>, hw=<tag>)

Evidence:
- Same-machine: <result>
- Cross-machine: <result>
- Divergence sanity: <result>

Report: <path to JSON>

Next action:
- (e.g. Add to verification domain registry)
- (or: file in disqualifications doc)
```
