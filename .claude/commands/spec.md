---
description: Draft or update a protocol specification section. Delegates to the protocol-architect subagent. Usage — /spec <topic-or-question>
argument-hint: <topic to spec>
---

You are about to work on the project's verification protocol spec. **This is the load-bearing innovation of the project — be rigorous.**

**Task**: $ARGUMENTS

Steps:
1. Read `.claude/internal/protocol/verification-protocol.md` and any related ADRs to ground yourself in current decisions.
2. Delegate to the `protocol-architect` agent with the task above. Give it the necessary file paths and the specific question to answer.
3. When the agent returns, review the spec delta. Confirm:
   - Every claim about prior art has a citation
   - Every economic claim has worked numbers
   - Open problems are explicitly flagged
4. Save the result. Decisions become ADRs under `.claude/internal/adr/`. Open problems go into the relevant section of the spec.
5. Summarize back: what was decided, what's still open, what hand-offs to engineering this creates.
