---
name: scaffold-cosmos-module
description: TDD-disciplined sequence for adding a new message to the Cosmos SDK `x/aiservice` module. Walks proto → keeper test → keeper → msg_server test → msg_server → CLI → integration test. Use whenever you're adding a new on-chain action (RegisterService, RequestInference, SubmitResult, etc.). Enforces test-first and the "no business logic in handlers" rule.
---

# Scaffold a Cosmos SDK Message

A disciplined sequence for adding a new message to `x/aiservice`. Follow every step in order. Skipping a step is the most common source of consensus bugs.

## Inputs needed

Before scaffolding, gather:
1. **Message name** (e.g. `MsgRegisterService`)
2. **Fields** of the message and its response
3. **State changes** — what does this message create / update / delete?
4. **Events** — what events does it emit?
5. **Rejection cases** — what should cause `ValidateBasic` to fail vs the keeper to fail?
6. **Phase compliance check**: per `.claude/internal/PHASE.md`, is this message in scope?

If any of these are unknown, **stop and ask**.

## The sequence

### Step 1 — Proto: types

Add the message type to `proto/aiservice/v1/tx.proto`:

```proto
message MsgRegisterService {
  option (cosmos.msg.v1.signer) = "owner";
  string owner = 1;
  string name = 2;
  cosmos.base.v1beta1.Coin price = 3 [(gogoproto.nullable) = false];
}

message MsgRegisterServiceResponse {
  uint64 service_id = 1;
}
```

If you introduce new shared types (e.g. `Service`, `Attestation`), put them in `proto/aiservice/v1/types.proto`.

### Step 2 — Proto: events

Add events to `proto/aiservice/v1/events.proto`:

```proto
message EventServiceRegistered {
  uint64 service_id = 1;
  string owner = 2;
  string name = 3;
  cosmos.base.v1beta1.Coin price = 4 [(gogoproto.nullable) = false];
}
```

### Step 3 — Generate code

```bash
make proto-gen
```

If this fails, fix the proto syntax. Don't move on until generation is clean.

### Step 4 — Keeper unit tests (failing)

Create `chain/x/aiservice/keeper/service_test.go` (or extend an existing file). Write these tests BEFORE writing any keeper code:

```go
func TestKeeper_RegisterService_HappyPath(t *testing.T) {
  k, ctx := setupKeeper(t)

  id, err := k.RegisterService(ctx, "alice", "translate-en-fr", sdk.NewInt64Coin("aios", 100))
  require.NoError(t, err)
  require.Equal(t, uint64(1), id)

  svc, err := k.GetService(ctx, id)
  require.NoError(t, err)
  require.Equal(t, "alice", svc.Owner)
  require.Equal(t, "translate-en-fr", svc.Name)
}

func TestKeeper_RegisterService_RejectsZeroPrice(t *testing.T) {
  k, ctx := setupKeeper(t)
  _, err := k.RegisterService(ctx, "alice", "translate-en-fr", sdk.NewInt64Coin("aios", 0))
  require.ErrorIs(t, err, types.ErrZeroPrice)
}

func TestKeeper_RegisterService_RejectsDuplicateName(t *testing.T) {
  k, ctx := setupKeeper(t)
  _, err := k.RegisterService(ctx, "alice", "translate-en-fr", sdk.NewInt64Coin("aios", 100))
  require.NoError(t, err)
  _, err = k.RegisterService(ctx, "bob", "translate-en-fr", sdk.NewInt64Coin("aios", 50))
  require.ErrorIs(t, err, types.ErrDuplicateName)
}
```

Run: `go test ./x/aiservice/keeper/...` — **expect failures**. Confirm the failure is "function does not exist" (good) or "assertion failed" (good) and not a compile error in the test file itself.

### Step 5 — Errors

Add error declarations to `chain/x/aiservice/types/errors.go`:

```go
var (
  ErrZeroPrice     = sdkerrors.Register(ModuleName, 2, "price must be positive")
  ErrDuplicateName = sdkerrors.Register(ModuleName, 3, "service name already exists")
)
```

Never use `errors.New` or `fmt.Errorf` for business errors — they don't survive gRPC marshaling cleanly. Always register.

### Step 6 — Keeper implementation (minimum to pass)

Create `chain/x/aiservice/keeper/service.go`:

```go
func (k Keeper) RegisterService(ctx context.Context, owner string, name string, price sdk.Coin) (uint64, error) {
  if !price.IsPositive() {
    return 0, types.ErrZeroPrice
  }
  // Check duplicate name via secondary index
  if existing, err := k.ServiceByName.Get(ctx, name); err == nil {
    return 0, fmt.Errorf("%w: service %d already uses name %q", types.ErrDuplicateName, existing, name)
  }

  id, err := k.NextServiceID.Next(ctx)
  if err != nil {
    return 0, fmt.Errorf("allocating service id: %w", err)
  }

  svc := types.Service{Id: id, Owner: owner, Name: name, Price: price}
  if err := k.Services.Set(ctx, id, svc); err != nil {
    return 0, fmt.Errorf("storing service: %w", err)
  }
  if err := k.ServiceByName.Set(ctx, name, id); err != nil {
    return 0, fmt.Errorf("indexing by name: %w", err)
  }

  return id, nil
}
```

Run tests again. They should pass. If not, the keeper is doing something the tests didn't expect — usually that means the test is right and the keeper is wrong, but think about which is true.

### Step 7 — Msg server test (failing)

Create `chain/x/aiservice/keeper/msg_server_test.go`:

```go
func TestMsgServer_RegisterService_EmitsEvent(t *testing.T) {
  k, ctx := setupKeeper(t)
  ms := keeper.NewMsgServerImpl(k)

  res, err := ms.RegisterService(ctx, &types.MsgRegisterService{
    Owner: "alice", Name: "translate-en-fr", Price: sdk.NewInt64Coin("aios", 100),
  })
  require.NoError(t, err)
  require.Equal(t, uint64(1), res.ServiceId)

  events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
  require.Len(t, events.ToABCIEvents(), 1)
  require.Equal(t, "aios.aiservice.v1.EventServiceRegistered", events[0].Type)
}
```

### Step 8 — Msg server (thin handler)

Create `chain/x/aiservice/keeper/msg_server.go`:

```go
func (m msgServer) RegisterService(ctx context.Context, msg *types.MsgRegisterService) (*types.MsgRegisterServiceResponse, error) {
  id, err := m.k.RegisterService(ctx, msg.Owner, msg.Name, msg.Price)
  if err != nil {
    return nil, err
  }

  sdkCtx := sdk.UnwrapSDKContext(ctx)
  if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventServiceRegistered{
    ServiceId: id, Owner: msg.Owner, Name: msg.Name, Price: msg.Price,
  }); err != nil {
    return nil, fmt.Errorf("emitting event: %w", err)
  }

  return &types.MsgRegisterServiceResponse{ServiceId: id}, nil
}
```

**No business logic in the handler.** It parses, delegates, emits, returns. If you find yourself writing validation here, move it to `ValidateBasic` or the keeper.

### Step 9 — Msg validation

Add `ValidateBasic` and `GetSigners` to `chain/x/aiservice/types/msgs.go`:

```go
func (m MsgRegisterService) ValidateBasic() error {
  if _, err := sdk.AccAddressFromBech32(m.Owner); err != nil {
    return fmt.Errorf("invalid owner address: %w", err)
  }
  if len(m.Name) == 0 || len(m.Name) > 64 {
    return fmt.Errorf("name must be 1..64 chars")
  }
  if !m.Price.IsPositive() {
    return ErrZeroPrice
  }
  return nil
}
```

`ValidateBasic` catches stateless errors (malformed inputs). The keeper catches stateful errors (duplicate, not found).

### Step 10 — Integration test (failing first)

Create `chain/testutil/integration/register_service_test.go`. Spin up the full app, post the tx, assert state.

```go
func TestIntegration_RegisterService(t *testing.T) {
  app := setupAppWithDevKeys(t)
  alice := app.DevKeys["alice"]

  msg := &types.MsgRegisterService{
    Owner: alice.Address.String(),
    Name: "translate-en-fr",
    Price: sdk.NewInt64Coin("aios", 100),
  }

  res, err := app.PostMsg(alice, msg)
  require.NoError(t, err)
  require.Equal(t, uint64(0), res.Code)

  // Query the service back
  svc, err := app.AIService.GetService(app.NewContext(), 1)
  require.NoError(t, err)
  require.Equal(t, "translate-en-fr", svc.Name)
}
```

### Step 11 — CLI

Add to `chain/x/aiservice/client/cli/tx.go`:

```go
func CmdRegisterService() *cobra.Command {
  cmd := &cobra.Command{
    Use:   "register-service [name] [price]",
    Short: "Register a new AI service",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
      clientCtx, err := client.GetClientTxContext(cmd)
      if err != nil { return err }

      price, err := sdk.ParseCoinNormalized(args[1])
      if err != nil { return err }

      msg := &types.MsgRegisterService{
        Owner: clientCtx.GetFromAddress().String(),
        Name:  args[0],
        Price: price,
      }
      return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
    },
  }
  flags.AddTxFlagsToCmd(cmd)
  return cmd
}
```

### Step 12 — Lint + commit

```bash
cd chain
make lint
make test
```

Both must pass. Then commit with a message stating the behavior delta:

```
feat(aiservice): MsgRegisterService with TDD

- Add MsgRegisterService + response in proto
- Add EventServiceRegistered
- Keeper: RegisterService with ZeroPrice / DuplicateName rejections
- MsgServer: thin handler emitting typed event
- CLI: `aid tx aiservice register-service <name> <price>`
- Tests: 3 keeper unit, 1 msg_server, 1 integration
```

## Anti-patterns this skill exists to prevent

- "Implement first, write tests after" — passes locally but doesn't catch the rejection cases you didn't think of
- "Business logic in the msg server handler" — couples validation to gRPC; can't reuse from genesis or migrations
- "Use `errors.New` instead of registered errors" — breaks gRPC error codes for clients
- "Validate addresses in the keeper" — `ValidateBasic` is the right home, lighter on chain
- "Skip the integration test — the unit tests already pass" — they don't catch wiring bugs in the app config or codec
