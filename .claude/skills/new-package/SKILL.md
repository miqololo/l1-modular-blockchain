---
name: new-package
description: Scaffold a new package in the monorepo with conventions (.claude/CLAUDE.md, lint config, test config, Makefile target). Use when adding a new top-level directory or sub-package that needs its own build/test surface.
---

# New Package

Scaffold a new package consistently. Avoids ad-hoc structure that drifts from the rest of the repo.

## Inputs needed

Before scaffolding, gather:
1. **Package path** (e.g. `tools/keyman`, `chain/x/somemodule`)
2. **Language** (go / rust / typescript / python)
3. **Phase** (must be ≤ current phase from `.claude/internal/PHASE.md`)
4. **Purpose** (one sentence — what does this package do that no other does?)
5. **Owner agent** (which subagent will primarily work on this — cosmos-engineer / ml-determinism-engineer / etc.)

If any of these are unknown, **stop and ask**.

## Phase gate

Read `.claude/internal/PHASE.md`. If the package's phase > current phase, refuse to scaffold and explain. Do not create empty placeholder packages for future phases.

## Steps

### 1. Create directory
`mkdir -p <path>`

### 2. Write `<path>/CLAUDE.md`

Required sections:
- One-line purpose
- Layout (target structure, may be aspirational)
- Rules specific to this package (TDD checklist, language conventions, forbidden patterns)
- Tools (lint, test, build commands)

Copy the shape from an existing package .claude/CLAUDE.md (e.g. `chain/CLAUDE.md` for Go, `contracts/CLAUDE.md` for Rust).

### 3. Language-specific bootstrap

| Language | Files |
|---|---|
| Go | `go.mod`, `Makefile`, `.golangci.yml` (or inherit from root), `<name>_test.go` skeleton |
| Rust | `Cargo.toml`, `src/lib.rs` or `src/main.rs`, `tests/` |
| TypeScript | `package.json`, `tsconfig.json`, `vitest.config.ts` |
| Python | `pyproject.toml`, `tests/test_<name>.py`, `ruff.toml` |

### 4. Failing first test

The package starts with **one failing test** that asserts the package's smallest meaningful invariant. This forces TDD from day one and proves the build works.

Example:
- Go: `TestPackageBootstrap` that asserts the package's main constructor returns non-nil
- Rust: `#[test] fn it_builds()` that calls the public API
- Python: `test_module_imports()`

### 5. Wire into root Makefile

Add the package to the root `Makefile`'s `test`, `lint`, and `build` targets so it's part of `make test-all`.

### 6. Update root `.claude/CLAUDE.md` §2 (Repo layout)

Add a one-line entry for the new package.

### 7. Commit

Single commit: `chore: scaffold <name> package`. No code beyond the scaffolding.

## Output

```
## New package: <path>
- Language: <lang>
- Phase: <N>
- Owner agent: <agent-name>

Files created:
- <path>/CLAUDE.md
- <path>/<bootstrap files>

Wired into:
- Root Makefile targets: test, lint, build
- Root .claude/CLAUDE.md §2

Initial test status:
- <one failing test as expected>: <name>
```
