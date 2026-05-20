# contracts/ — CosmWasm contracts

**Status: deferred until phase 5.** Do not add code here until [docs/PHASE.md](../docs/PHASE.md) reaches phase 5.

When activated, contracts will host:
- Third-party marketplace extensions (custom listing types, auctions)
- Reputation / staking pools
- Anything not consensus-critical

Consensus-critical logic stays in the Go module, not here. CosmWasm code is for *extension*, not core.

## Future rules

- `cargo clippy -- -D warnings` clean
- No `panic!`, `unwrap()`, or `expect()` in contract entrypoints
- `cw-multi-test` for integration tests
- One contract per crate
