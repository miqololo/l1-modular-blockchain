---
name: tdd-cycle
description: Walk through the red→green→refactor TDD cycle for a specific behavioral change. Use when starting a non-trivial change (new message, new endpoint, new behavior, bug fix). Enforces test-first discipline explicitly rather than implicitly.
---

# TDD Cycle

A disciplined walk through red → green → refactor. The user invokes this skill at the start of a behavioral change. You guide them (or yourself, if working autonomously) through five gates.

## Gate 1: Define the behavior delta

State, in one sentence, the *behavioral* change. Not the implementation, the behavior.

- ✅ "Reject `MsgRegisterService` when price is zero"
- ✅ "Indexer reprocesses a block without creating duplicate rows"
- ❌ "Refactor the keeper" (no behavior change → TDD doesn't apply; just run the existing suite)
- ❌ "Add a struct for service config" (no behavior; come back when there is one)

If the change has no observable behavioral delta, **stop**. This is not TDD work, it's refactoring. Run the existing suite to confirm nothing breaks, then proceed without writing new tests.

## Gate 2: Red — write a failing test

Write the smallest test that captures the new behavior. Run it. Confirm:
1. It fails.
2. It fails for the **expected reason** (the assertion, not a compile error or missing import).

If the test compiles and runs but passes, the behavior already exists — either you're testing the wrong thing, or there's nothing to do.

Required outputs at this gate:
- The test file path and test name
- The exact failure message from the test runner
- A one-line confirmation that the failure reason matches the intended behavior

## Gate 3: Green — minimum production code

Write the **minimum** production code needed to pass the test. Resist:
- Adding extra fields "while you're here"
- Generalizing the implementation beyond the test
- Adding helpers that aren't used yet
- Refactoring existing code in this commit

Run the test. Confirm it passes. Run the full package suite. Confirm nothing else broke.

If something else broke, you discovered a hidden coupling. Either:
- Add a test that captures the coupling (then fix it in a separate red→green), or
- Revert and rethink

## Gate 4: Refactor

Now improve names, remove duplication, tighten types. Tests must still pass after every refactor step.

Refactor checklist:
- Any duplication introduced? Extract.
- Any name that doesn't say what the thing does? Rename.
- Any comments explaining WHAT the code does? Replace with better names.
- Any types that are stringly-typed? Strengthen.
- Any dead code from "version 1" of the implementation? Delete.

Run the full suite again at the end.

## Gate 5: Coverage of rejection paths

The happy path is necessary but not sufficient. For each new behavior, ask:
- What if the input is invalid?
- What if the caller is unauthorized?
- What if a precondition is missing?
- What if a conflicting state exists?
- What if the underlying call fails?

For each YES, write a test (back to Gate 2 for each).

If you skip rejection-path tests because "they're obvious", you will regret it. Add them.

## Output at the end

```
## TDD cycle complete

Behavior delta: <one sentence>

Tests added:
- file:test_name — happy path
- file:test_name — rejection: <case>
- file:test_name — rejection: <case>

Production code:
- file (summary of change)

Suite status:
- Affected package: PASS (<N> tests)
- Full repo: PASS / NOT_RUN
```

## Anti-patterns this skill exists to prevent

- "Tests after" — writing tests post-hoc to "verify" code that already exists. Often the test ends up shaped to fit the code instead of the behavior.
- "Mega-test" — one test that exercises everything. Hard to interpret failures. Split it.
- "Happy-path only" — passes today, breaks tomorrow on edge cases. Always add rejection tests.
- "Mock the world" — see `.claude/CLAUDE.md` §4: no mocks of project's own components.
- "Refactor + behavior change in one commit" — impossible to bisect later. Always separate.
