# Protocol specifications

The verification protocol is the project's load-bearing innovation. These documents are the source of truth.

## Files

- [verification-protocol.md](verification-protocol.md) — main spec
- [threat-model.md](threat-model.md) — adversaries and attacks

## Updating

Protocol changes go through the protocol-architect agent (`/spec <topic>`). Every change includes:
- The diff to the spec
- Updated ADRs if the change resolves an open problem
- Updated threat model if attack surface changes

**The spec is versioned**. Once we hit phase 2, breaking changes to the spec require a major version bump and an upgrade plan.
