---
description: Run the tdd-enforcer agent against the current diff. Usage — /review-tdd
---

Delegate to the `tdd-enforcer` agent to review the current diff for TDD discipline.

The agent will:
1. Read the uncommitted diff
2. Verify tests exist for every behavioral change
3. Verify tests would have failed before the production code
4. Flag any mocks of project-internal components (forbidden by `.claude/CLAUDE.md` §4)
5. Confirm phase compliance
6. Run linters

Report its verdict verbatim. Do not soften findings.
