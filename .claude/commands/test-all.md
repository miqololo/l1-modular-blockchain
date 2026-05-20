---
description: Run the full cross-language test suite for the monorepo. Usage — /test-all
---

Run the full test suite across all active packages.

Steps:
1. Read `.claude/internal/PHASE.md` to know which packages are active.
2. For each active package, run its test target via `make -C <package> test`.
3. If the root `Makefile` has a `test-all` target, prefer that.
4. Aggregate failures. For each failure, report:
   - Package
   - Test name
   - First 20 lines of the failure output
5. Run linters via `make lint-all` (or per-package equivalents).
6. Produce a final verdict:

```
## Test-all verdict

Packages: <N> active, <M> tested
Tests: <X> pass, <Y> fail
Lint: <pass/fail per package>

Failures:
- <package>::<test name> — <first line of error>
- ...
```

If anything fails, **do not** suggest skipping or `--no-verify`. Report the failure faithfully.
