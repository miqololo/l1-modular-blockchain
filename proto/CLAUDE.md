# proto/ — Shared protobuf schemas

Single source of truth for types crossing the chain ↔ off-chain boundary.

## Layout (target)

```
aiservice/v1/
  tx.proto           Messages (RegisterService, RequestInference, SubmitResult, ...)
  query.proto        Queries
  types.proto        Shared types (Service, InferenceRequest, Attestation)
  events.proto       Typed event definitions
buf.yaml             buf config
buf.gen.yaml         codegen targets (Go for chain/indexer, TS for frontend)
```

## Rules

- **Versioning by package**: `v1`, `v2`. Never rename a field. Never reuse a field number. New fields get new numbers.
- **`buf lint`** must pass.
- **`buf breaking`** runs in CI against `main`. Breaking changes require an ADR.
- **Generated code lives in each consumer**, not here. This package only owns `.proto` files and the buf config.
- **Events are typed**. No `map<string, string>` blobs. The indexer relies on schema.
