# Hosted runtime quickstart

This is the current start-here path for running Shunter as a hosted runtime/server.

The normal runnable example is `cmd/shunter-example`. It defines a tiny module through the top-level `github.com/ponchione/shunter` API, builds a `shunter.Runtime`, serves the WebSocket protocol at `/subscribe`, and shuts down through runtime ownership.

This doc is no longer a manual subsystem-wiring guide. Low-level packages such as `schema`, `commitlog`, `executor`, `subscription`, and `protocol` remain available for internal/advanced work, but normal app code should not assemble that graph directly.

## Run the example

```sh
rtk go run ./cmd/shunter-example -addr :8080 -data ./shunter-data
```

The server listens on the configured address and exposes the subscription/reducer WebSocket endpoint at:

```text
ws://localhost:8080/subscribe
```

Use one of the accepted subprotocols:

- `v1.bsatn.spacetimedb`
- `v1.bsatn.shunter`

The example uses `shunter.AuthModeDev`, so it is dialable locally without an external identity provider. Strict auth remains a runtime mode for non-demo configurations.

## What the example proves

`cmd/shunter-example` is intentionally small:

- module: `hello`
- table: `greetings`
- reducer: `say_hello`
- runtime: built with `shunter.Build(...)`
- serving: `Runtime.ListenAndServe(ctx)` / `Runtime.HTTPHandler()`
- external proof path: WebSocket protocol, not local-only calls

The test file `cmd/shunter-example/main_test.go` verifies:

- cold boot and recovery against the same data directory
- development WebSocket admission and identity-token handshake
- subscription to `SELECT * FROM greetings`
- reducer call to `say_hello` over protocol messages
- non-caller subscriber receives a `TransactionUpdateLight` insert
- context cancellation shuts serving/runtime ownership down cleanly
- the example source does not manually assemble the kernel graph

Run the proof with:

```sh
rtk go test ./cmd/shunter-example -count=1
```

## App code shape

The example should read like app code:

```go
mod := shunter.NewModule("hello").
    SchemaVersion(1).
    TableDef(schema.TableDefinition{
        Name: "greetings",
        Columns: []schema.ColumnDefinition{
            {Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
            {Name: "message", Type: types.KindString},
        },
    }).
    Reducer("say_hello", sayHello)

rt, err := shunter.Build(mod, shunter.Config{
    DataDir:        dataDir,
    ListenAddr:     addr,
    AuthMode:       shunter.AuthModeDev,
    EnableProtocol: true,
})
if err != nil {
    return err
}
return rt.ListenAndServe(ctx)
```

Normal example code may import the top-level `shunter` package plus small schema/types helpers needed to declare a module. It should not instantiate the commit-log worker, executor, subscription manager, protocol server, fan-out worker, or connection manager directly.

## Where the subsystem wiring lives now

Runtime assembly moved behind the top-level runtime owner:

- `runtime_build.go` owns config normalization, schema build, recovery/bootstrap, and reducer-registry construction.
- `runtime_lifecycle.go` owns start/close ordering for durability, executor, scheduler, subscription fan-out, and lifecycle state.
- `runtime_network.go` owns protocol server construction, `HTTPHandler()`, and `ListenAndServe(ctx)`.
- `runtime_local.go` owns secondary local reducer/read helpers.
- `runtime_describe.go` owns v1 describe/export foundations.

That internal wiring is still real, but it is no longer the normal app-author bootstrap story.

## Deliberately out of scope

- full tutorial site
- generated frontend/client app
- canonical contract JSON or client codegen
- v1.5 query/view declarations
- production auth walkthrough
- multi-module hosting or admin/control-plane work
