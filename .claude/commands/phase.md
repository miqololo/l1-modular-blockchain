---
description: Print or advance the current phase. Usage — /phase (read) or /phase advance <N> (gate check + bump)
argument-hint: [advance <N>]
---

Phase management for the project.

**Task**: $ARGUMENTS

If no arguments → read and print `.claude/internal/PHASE.md` summarizing:
- Current phase
- Allowed work
- Gate to next phase
- Status of gate conditions (passed/blocked/unverified)

If `advance <N>` → run the gate check:
1. Read the gate conditions for phase N-1 → N from `.claude/CLAUDE.md` §3.
2. For each condition, verify objectively (test results, file presence, ADR status).
3. If any condition fails, **refuse to advance** and list the failing conditions.
4. If all pass, update `.claude/internal/PHASE.md` and commit with message `chore: advance to phase <N>`.

Never advance a phase without all gate conditions met. This is the project's most important discipline.
