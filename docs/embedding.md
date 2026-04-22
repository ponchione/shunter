# Embedding Shunter

This doc walks through the minimal wiring surface for embedding Shunter into
a Go host program. The companion binary at `cmd/shunter-example/main.go`
implements everything below; read it alongside this doc.

## What gets wired

```
schema.Builder → schema.SchemaRegistry
                      │
                      ▼
commitlog.OpenAndRecoverDetailed ──► store.CommittedState
                      │                      │
                      ▼                      │
commitlog.DurabilityWorker ──► durabilityAdapter
                      │                      │
                      ▼                      ▼
                executor.NewExecutor(cfg, reducerRegistry, committed, schemaReg, maxTxID)
                      │
                      ▼
                executor.Startup(ctx, nil) ─── flips external-admission gate
                      │
                      ▼
                protocol.Server { Executor: NewProtocolInboxAdapter(exec), ... }
                      │
                      ▼
                http.Server mux.Handle("/subscribe", server.HandleSubscribe)
```

## Step by step

### 1. Declare the schema

```go
b := schema.NewBuilder()
b.SchemaVersion(1)
b.TableDef(schema.TableDefinition{
    Name: "greetings",
    Columns: []schema.ColumnDefinition{
        {Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
        {Name: "message", Type: types.KindString},
    },
})
eng, err := b.Build(schema.EngineOptions{})
reg := eng.Registry()
```

`schema.SchemaRegistry` is the hub consumed by every downstream subsystem.

### 2. Open the data directory

```go
committed, maxTxID, plan, err := commitlog.OpenAndRecoverDetailed(dataDir, reg)
```

- Returns `ErrNoData` on first boot. Bootstrap by creating an empty
  `store.CommittedState`, registering every table from the registry, writing
  an initial snapshot at TxID 0 via `commitlog.NewSnapshotWriter`, then
  re-running `OpenAndRecoverDetailed`.
- On subsequent boots the call replays snapshot + segments up to the durable
  horizon, returns the recovered `*store.CommittedState`, the highest
  applied `TxID`, and the resume plan used to reopen the commit-log tail.

### 3. Start the durability worker

```go
dw, err := commitlog.NewDurabilityWorkerWithResumePlan(dataDir, plan, commitlog.DefaultCommitLogOptions())
```

The executor expects a `DurabilityHandle` taking `types.TxID`. The commit-log
worker uses `uint64` — a four-line adapter bridges them:

```go
type durabilityAdapter struct{ dw *commitlog.DurabilityWorker }
func (a durabilityAdapter) EnqueueCommitted(txID types.TxID, cs *store.Changeset) {
    a.dw.EnqueueCommitted(uint64(txID), cs)
}
func (a durabilityAdapter) WaitUntilDurable(txID types.TxID) <-chan types.TxID {
    return a.dw.WaitUntilDurable(txID)
}
```

### 4. Register reducers

```go
rr := executor.NewReducerRegistry()
rr.Register(executor.RegisteredReducer{Name: "say_hello", Handler: sayHello})
rr.Freeze()
```

`ReducerRegistry` is separate from the schema builder's reducer list — the
schema builder records reducers for declarative purposes (export, validation),
while the executor's registry owns runtime dispatch. Freeze before constructing
the executor.

### 5. Construct and start the executor

```go
exec := executor.NewExecutor(executor.ExecutorConfig{
    Durability: durabilityAdapter{dw},
}, rr, committed, reg, uint64(maxTxID))

if err := exec.Startup(ctx, nil); err != nil { return err }
go exec.Run(ctx)
```

`Startup` runs the scheduler-replay + dangling-client sweep (SPEC-003 §10.6,
§13.5) then flips the external-admission gate. External protocol traffic is
rejected with `ErrExecutorNotStarted` until Startup finishes.

The `nil` scheduler is valid when sys_scheduled replay is not needed. Embedders
that rely on scheduled reducers wire a `Scheduler` here — at the time of
writing the scheduler constructor reaches the executor's unexported inbox, so
scheduler wiring is still an internal / test-only path.

### 6. Stand up the protocol server

```go
server := &protocol.Server{
    JWT:      &auth.JWTConfig{SigningKey: key, AuthMode: auth.AuthModeAnonymous},
    Mint:     &auth.MintConfig{Issuer: "...", Audience: "...", SigningKey: key, Expiry: 24 * time.Hour},
    Options:  protocol.DefaultProtocolOptions(),
    Executor: executor.NewProtocolInboxAdapter(exec),
    Conns:    protocol.NewConnManager(),
    Schema:   reg,
    State:    stateAdapter{committed},
}

mux := http.NewServeMux()
mux.HandleFunc("/subscribe", server.HandleSubscribe)
http.ListenAndServe(addr, mux)
```

`*store.CommittedState` returns a concrete `*CommittedSnapshot` from its
`Snapshot()` method; the protocol layer's `CommittedStateAccess` interface
expects the `CommittedReadView` interface. A two-line adapter bridges the
shape:

```go
type stateAdapter struct{ cs *store.CommittedState }
func (a stateAdapter) Snapshot() store.CommittedReadView { return a.cs.Snapshot() }
```

### 7. Graceful shutdown

On SIGINT/SIGTERM, cancel the root context, shut the HTTP server down with a
bounded timeout, call `exec.Shutdown()` (waits for Run to drain), then
`dw.Close()` to flush the commit log. See `cmd/shunter-example/main.go` for
the ordering.

## What is deliberately out of scope

- **Subscriptions** — the example runs with the noop `SubscriptionManager`
  default, so reducer writes do not fan out to subscribers. Wiring
  `subscription.Manager` + `FanOutWorker` requires an adapter that widens
  `schema.SchemaRegistry` with `ColumnCount` (the subscription-layer
  `SchemaLookup` interface demands it).
- **Scheduled reducers** — `executor.Scheduler` reads an unexported executor
  channel; production wiring for that path is still pending.
- **Authentication in strict mode** — the example uses anonymous auth so it
  can be dialed without an external IdP. Production embedders wire
  `AuthModeStrict` with their own JWT issuer.

## Running the example

```sh
go build ./cmd/shunter-example
./shunter-example -addr :8080 -data ./shunter-data
```

Dial `/subscribe` with a WebSocket client using one of the accepted
subprotocols (`v1.bsatn.spacetimedb` or `v1.bsatn.shunter`) to verify the
server admits an anonymous connection. Ctrl-C exits cleanly.
