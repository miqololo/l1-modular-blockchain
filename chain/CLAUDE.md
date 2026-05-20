# chain/ — Cosmos SDK L1

Custom Cosmos SDK app with the `x/aiservice` module. CometBFT consensus.

## Layout (target — not yet scaffolded)

```
app/                  App wiring, module manifest
cmd/aid/              Binary entrypoint (`aid` = AI daemon)
x/aiservice/
  keeper/             State machine logic
  types/              Messages, events, errors, codec
  client/cli/         CLI commands
  module.go           Module manifest
proto/aiservice/v1/   Protobuf — copied from /proto at build
testutil/             Test helpers (devnet bootstrap, fixtures)
```

## Rules specific to this package

- **No business logic in msg handlers.** Handlers parse → validate → delegate to keeper. Keepers own the state machine.
- **Keepers are pure** w.r.t. their inputs. No file I/O, no network, no time.Now() — use `ctx.BlockTime()`.
- **Every msg has**: a unit test on `MsgServer.Handle*`, an integration test through the full app, and a CLI test.
- **State keys**: define in `types/keys.go`. Never inline a key prefix.
- **Events**: emit on every state mutation. Indexer depends on them.
- **Errors**: declared in `types/errors.go` with `sdkerrors.Register`. Never return raw `errors.New`.

## TDD checklist for a new message

1. Define the proto in `/proto/aiservice/v1/tx.proto`
2. Write a **failing** keeper test for the happy path
3. Write a **failing** keeper test for each rejection case (auth, invariant, missing state)
4. Implement the minimum keeper code to pass them
5. Write a **failing** integration test that posts the msg through the app
6. Wire msg handler, codec, cli
7. Refactor

## Tools

- `make test` — unit + integration
- `make lint` — golangci-lint
- `make proto-gen` — regenerate protos
- `make localnet` — spin up a single-validator devnet
