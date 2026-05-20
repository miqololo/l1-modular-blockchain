# ADR-0001 — Monorepo layout

**Status**: Accepted
**Date**: 2026-05-20
**Decision drivers**: Multi-language stack (Go + Rust + TS + Python), single team, shared protobuf schemas, need for cross-cutting refactors.

## Decision

Single monorepo at the root of this directory. Per-language sub-packages with their own build tooling (`go.mod`, `Cargo.toml`, `package.json`, `pyproject.toml`). Shared protobuf schemas in `/proto`. Root `Makefile` orchestrates cross-language commands.

## Considered alternatives

1. **Polyrepo (one repo per language)**. Rejected: protobuf schema sync becomes a release dance; cross-cutting refactors require coordinated PRs across repos.
2. **Single Go module at root**. Rejected: ties the frontend and contracts to Go tooling, which they don't need.
3. **Nx / Turborepo / Bazel**. Deferred: extra learning + maintenance overhead before we have any code. Revisit at phase 4 if build orchestration becomes painful.

## Consequences

- Pros: atomic cross-language PRs, single CI pipeline, one CLAUDE.md hierarchy
- Cons: per-language tooling drift if not disciplined; the root Makefile must be kept in sync

## Follow-ups

- ADR-0007 will revisit build orchestration once we have 3+ active packages
