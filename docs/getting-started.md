# Getting Started With Shunter

Status: rough draft
Scope: first orientation path for application authors embedding Shunter in Go.

Shunter is a Go library for hosting stateful realtime application modules.
An application defines a module, builds a runtime from that module, starts the
runtime, then writes through reducers and reads through committed read APIs,
declared queries, declared views, or protocol clients.

This page is intentionally short. Use it to learn the order of operations, then
move to the task-specific guides under `docs/how-to/`.

## The Short Version

The normal application flow is:

1. Define a module with tables, reducers, reads, permissions, and metadata.
2. Build a runtime with `shunter.Build`.
3. Start the runtime with `Runtime.Start` or `Runtime.ListenAndServe`.
4. Call reducers for writes.
5. Read committed state with `Runtime.Read`, `Runtime.CallQuery`, or
   `Runtime.SubscribeView`.
6. Close the runtime during shutdown.
7. Export a contract when app-facing schema or read surfaces change.

The root package is the main app-facing API:

```go
import "github.com/ponchione/shunter"
```

Lower-level packages such as `schema`, `types`, `bsatn`, and `codegen` are used
when declaring schemas, constructing values, encoding payloads, or generating
clients. Runtime implementation packages such as `store`, `executor`,
`commitlog`, and `subscription` are not the normal app integration surface.

## Define A Module

Create one module declaration per hosted application module.

```go
package app

import (
	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const messagesTableID schema.TableID = 0

func Module() *shunter.Module {
	return shunter.NewModule("chat").
		Version("v0.1.0").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "messages",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
				{Name: "body", Type: types.KindString},
			},
		}).
		Reducer("send_message", sendMessage).
		Query(shunter.QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT * FROM messages",
		}).
		View(shunter.ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages",
		})
}
```

Table IDs are assigned by the built schema. Until generated app helpers cover
this path, keep table ID constants close to the corresponding table
declarations and update them deliberately when table order changes.

## Write Through Reducers

Reducers are the write boundary. A reducer receives a transaction-scoped
`*schema.ReducerContext` and raw byte arguments.

```go
func sendMessage(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	body := string(args)

	_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{
		types.NewUint64(0),
		types.NewString(body),
	})
	return nil, err
}
```

Reducer arguments and results are byte slices at the runtime boundary. Your app
can use `bsatn`, JSON, protobuf, or a narrow app-specific encoding. Keep the
choice consistent across local calls, protocol clients, and tests.

## Build And Start

`Build` validates the module and opens or initializes runtime state. `Start`
starts runtime-owned lifecycle, durability, executor, subscription, and
protocol resources.

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir: "./data/chat",
})
if err != nil {
	return err
}
defer rt.Close()

if err := rt.Start(ctx); err != nil {
	return err
}
```

Use a durable `DataDir` for real applications. A blank `DataDir` uses the
runtime default and is convenient for development, but production services
should choose an explicit location owned by the app deployment.

## Call A Reducer Locally

Local calls are useful for tests, CLIs, background workers, and app-owned HTTP
handlers that do not need to speak the WebSocket protocol.

```go
res, err := rt.CallReducer(ctx, "send_message", []byte("hello"))
if err != nil {
	return err
}
if res.Status != shunter.StatusCommitted {
	return fmt.Errorf("send_message failed: %v", res.Error)
}
```

Strict permission checks use caller options:

```go
res, err := rt.CallReducer(
	ctx,
	"send_message",
	payload,
	shunter.WithPermissions("messages:write"),
)
```

## Read State

Use `Runtime.Read` for direct committed-state reads inside the process. The
read view is valid only during the callback.

```go
err := rt.Read(ctx, func(view shunter.LocalReadView) error {
	for _, row := range view.TableScan(messagesTableID) {
		fmt.Println(row[1].AsString())
	}
	return nil
})
```

Use declared queries and views when you want named app-facing read surfaces
with permissions, visibility filters, contract export, and client generation.

```go
result, err := rt.CallQuery(ctx, "recent_messages")
if err != nil {
	return err
}
_ = result.Rows
```

```go
sub, err := rt.SubscribeView(ctx, "live_messages", 1)
if err != nil {
	return err
}
_ = sub.InitialRows
```

## Serve Protocol Traffic

Enable protocol serving when external clients should connect over Shunter's
WebSocket protocol.

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
})
if err != nil {
	return err
}

if err := rt.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
	return err
}
```

If your application already owns the HTTP server, call `Runtime.Start` and mount
`Runtime.HTTPHandler()` instead.

## Export A Contract

Export a contract from an app-owned binary that links your module. Contract
JSON is the review and codegen artifact for app-facing tables, reducers,
queries, views, permissions, read models, and migration metadata.

```go
if err := contractworkflow.ExportRuntimeFile(rt, "shunter.contract.json"); err != nil {
	return err
}
```

The generic `cmd/shunter` CLI can diff, validate, plan, and generate from
existing contract JSON files. It does not dynamically load your module code.

## Next Pages

- `docs/concepts.md` explains the terms used across the docs.
- `docs/how-to/module-anatomy.md` covers module declarations.
- `docs/how-to/reducer-patterns.md` covers reducer design.
- `docs/how-to/reads-queries-views.md` covers reads and live views.
- `docs/how-to/serve-protocol-traffic.md` covers HTTP and WebSocket serving.
- `docs/how-to/persistence-and-shutdown.md` covers `DataDir`, snapshots, and
  shutdown.
- `docs/how-to/contract-export-and-codegen.md` covers contracts and generated
  clients.
- `docs/how-to/testing-shunter-modules.md` covers test patterns.
