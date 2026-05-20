---
name: tutorial-section
description: Add or update a section in docs/TUTORIAL.md for a freshly-shipped feature. Use after merging a chunk that introduces a new user-visible behavior or architectural component. Cross-checks code references against the current repo state so the tutorial doesn't rot.
---

# Tutorial Section

Add a section to `docs/TUTORIAL.md` describing a freshly-shipped feature. Cross-check every code reference against the current repo.

## Inputs needed

1. **What was shipped** — feature name, brief description
2. **Files changed** (from `git diff` of the chunk)
3. **User-visible behavior delta** — what can the user do now that they couldn't before
4. **Section position** in TUTORIAL.md — usually appended; sometimes inserted

## Steps

### Step 1 — Read the source

For every file mentioned in the section, read it. Do not paraphrase from memory. Note line numbers for citations.

### Step 2 — Draft using the section template

```markdown
## <N>. <Section title>

**What this section covers**: <1–2 sentence preview>

### What we built

<2–4 paragraphs, plain English. State the feature, the user-visible behavior, and where it fits in the architecture.>

### How it works

<Annotated walkthrough. Quote real code from real files. Every snippet has a path comment.>

```go
// chain/x/aiservice/keeper/service.go
func (k Keeper) RegisterService(ctx context.Context, ...) (uint64, error) {
  // (verbatim from file)
}
```

<Explain the WHY of non-obvious decisions. Skip the obvious — well-named code doesn't need narration.>

### Try it

<Concrete commands the reader runs. Include expected output.>

```bash
aid tx aiservice register-service my-llm 100aios --from alice --keyring-backend test
```

Expected output:
```
{"service_id":"1"}
```

### Troubleshooting

<Common failure modes and their fixes. Only include actual issues that have come up.>

### Q&A

**Q: <Specific question a reader is likely to ask>**

A: <3–6 sentences. Concrete answer. File/line refs if relevant.>

**Q: <Another specific question>**

A: ...
```

### Step 3 — Verify references

Walk every file:line reference in your draft and confirm it points to what you claim. If the file has moved or the line has changed, fix.

For every command you wrote, run it (or note "needs verification" if you can't run it in this environment) and confirm the output matches what's documented.

### Step 4 — Q&A quality check

Each Q is:
- Specific (not "How does X work?" but "Why does X use Y instead of Z?")
- Likely to be asked (real curiosity, not strawman)
- Answerable in 3–6 sentences
- Concrete in its answer (references code, not vague principles)

If a Q has more than 6 sentences of answer, split it.

### Step 5 — Integrate

Append (or insert) into `docs/TUTORIAL.md`. Update the table of contents at the top of the file.

### Step 6 — Footer

Add a footer to the section:

```markdown
---
*Verified against commit `<short SHA>` on <YYYY-MM-DD>.*
```

This makes drift visible. If the SHA is stale, the section needs re-verification.

## Forbidden

- Paraphrasing code instead of quoting it
- Inventing API methods, fields, or flags
- "Configure X as usual" — describe the configuration explicitly
- Q&A items written without a real question motivating them
- Marketing language ("seamless," "powerful," "next-gen") — describe what it does

## Output format

```
## What changed in TUTORIAL.md
- Section added: <N. Title>
- Position: appended / inserted at <position>
- Word count: <approx>

## Code references verified
- file:line — (what it shows)
- file:line — (what it shows)

## Q&A entries added
- <count>, topics: <topic1>, <topic2>, ...

## Verification
- Demo command run locally: <yes/no, output matches>
- Commit SHA stamped: <sha>
```
