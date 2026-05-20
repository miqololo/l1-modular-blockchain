# Disqualified verification domains

Tuples of `(model, runtime, precision, hardware)` that have proven **non-deterministic** in our testing and are therefore disqualified as verification domains.

This is a living document. Negative results are valuable — they prevent re-testing the same failed combinations.

## Format

Each entry:

```
### <date> — <model> + <runtime>@<version> + <precision> + <hw_tag>

**Status**: DISQUALIFIED

**Evidence**:
- Test run: `determinism-harness/report/<filename>.json`
- Failure mode: <one sentence — what diverged>

**Root cause** (if known): <one paragraph>

**Reopen if**: <conditions under which we'd retest — e.g. runtime version bump, flag becomes available>
```

## Entries

(none yet — entries added as the determinism harness runs)
