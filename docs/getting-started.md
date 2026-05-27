# Getting Started With Shunter

Shunter is a Go library for hosting stateful realtime application modules.
An application defines a module, then either runs Shunter as the app's backend
server with `shunter.Run` or builds a lower-level runtime and owns more of the
HTTP/lifecycle wiring itself.

This page is intentionally short. Use it to learn the order of operations, then
move to the task-specific guides under [docs/how-to](how-to/README.md).

## The Short Version

The normal hosted-backend flow is:

1. Add Shunter as a Go dependency and import the root package.
2. Define a module with tables, reducers, reads, permissions, and metadata;
   add procedures when client-callable workflows need external I/O.
3. Read config with `shunter.ConfigFromEnv`.
4. Run the backend with `shunter.Run`.
5. Export a contract when app-facing schema or read surfaces change.
6. Generate TypeScript bindings for frontend clients.

The embedded/runtime-library path still exists for applications that need
custom HTTP routing, in-process workers, or lower-level lifecycle control:
build with `shunter.Build`, start with `Runtime.Start` or
`Runtime.ListenAndServe`, call reducers and reads locally, and close the
runtime during shutdown.

In an application module, add the dependency in the usual Go way:

```bash
go get github.com/ponchione/shunter
```

The root package is the main app-facing API:

```go
import "github.com/ponchione/shunter"
```

Lower-level packages such as `schema`, `types`, `bsatn`, `contractworkflow`,
and `codegen` are used when declaring schemas, constructing values, encoding
payloads, exporting contracts, or generating clients. Runtime implementation
packages such as `store`, `executor`, `commitlog`, and `subscription` are not
the normal app integration surface.

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

Table and index IDs are assigned by the built schema from declaration order.
Until generated app helpers cover this path, keep handwritten ID constants next
to the corresponding table declarations and update them deliberately when table
or index order changes.

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

## Run As A Backend

For a standard hosted app server, keep the application shell thin:

```go
package main

import (
	"context"
	"log"

	"github.com/ponchione/shunter"
	"example.com/myapp/internal/app"
)

func main() {
	cfg := shunter.ConfigFromEnv()
	cfg.EnableProtocol = true
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/myapp"
	}

	if err := shunter.Run(context.Background(), app.Module(), cfg); err != nil {
		log.Fatal(err)
	}
}
```

`ConfigFromEnv` reads `SHUNTER_DATA_DIR`, `SHUNTER_LISTEN_ADDR`,
`SHUNTER_ENABLE_PROTOCOL`, `SHUNTER_AUTH_MODE`, `SHUNTER_AUTH_SIGNING_KEY`,
`SHUNTER_AUTH_ISSUERS`, `SHUNTER_AUTH_AUDIENCES`, and
`SHUNTER_AUTH_OIDC_ISSUERS`. Use dev auth for local work and strict auth with
explicit key material or JWKS verification for public protocol serving.

## Build And Start Manually

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
runtime default `./shunter-data`, which is convenient for local development but
too implicit for production services.

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

The canonical hosted example is
[`examples/hosted-chat`](../examples/hosted-chat/README.md). It shows the
server entrypoint, contract export binary, generated TypeScript path, and a
frontend-shaped client that calls a reducer and subscribes to a live view.

## Export A Contract

Export a contract from an app-owned binary that links your module. Contract
JSON is the review and codegen artifact for app-facing tables, reducers,
procedures, queries, views, permissions, read models, and migration metadata.
Starting the runtime is not required for contract export.

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir: "./data/chat",
})
if err != nil {
	return err
}
defer rt.Close()

if err := contractworkflow.ExportRuntimeFile(rt, "shunter.contract.json"); err != nil {
	return err
}
```

The generic `cmd/shunter` CLI can diff contracts, run policy checks, plan
migrations, and generate clients from existing contract JSON files. It does not
dynamically load your module code.

## Continue

- [Concepts](concepts.md) explains the terms used across the docs.
- [Module anatomy](how-to/module-anatomy.md) covers module declarations.
- [Reducer patterns](how-to/reducer-patterns.md) covers reducer design.
- [Reads, queries, and views](how-to/reads-queries-views.md) covers reads and
  live views.
- [Serve protocol traffic](how-to/serve-protocol-traffic.md) covers HTTP and
  WebSocket serving.
- [Host Shunter as a backend](how-to/host-shunter-backend.md) covers the
  standard static Go app server path.
- [Persistence and shutdown](how-to/persistence-and-shutdown.md) covers
  `DataDir`, snapshots, backup, restore, and shutdown.
- [Contract export and codegen](how-to/contract-export-and-codegen.md) covers
  contracts and generated clients.
- [Testing Shunter modules](how-to/testing-shunter-modules.md) covers test
  patterns.
