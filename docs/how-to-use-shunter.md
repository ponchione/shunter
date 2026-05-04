# How To Use Shunter

This guide is for application code that embeds Shunter through the root
`github.com/ponchione/shunter` package. Use Go doc for API detail and this
guide for the order of operations and the main decisions an app author makes.

Shunter is a hosted runtime library. Your application defines a module, builds
a runtime from it, starts that runtime, and then calls reducers, reads state, or
serves protocol traffic through runtime-owned APIs.

## Mental Model

A Shunter application has three layers:

- Module declaration: tables, reducers, lifecycle hooks, declared reads,
  visibility filters, permission metadata, migration metadata, and app module
  identity.
- Runtime ownership: committed state, durable logging, recovery, serialized
  reducer execution, subscriptions, local reads, protocol serving, health, and
  contract/schema export.
- Application shell: your process, HTTP server, CLI, worker, tests, or service
  supervisor that calls `Build`, `Start`, `Close`, `HTTPHandler`, or
  `ListenAndServe`.

The root `shunter` package is the app-facing surface. Lower-level packages such
as `schema`, `types`, `protocol`, `store`, and `commitlog` exist when you need
specific control, but a normal app should start at the root package.

## Define A Module

Create one module declaration per hosted application module. Table IDs are
assigned by the builder from the module schema; keep table ID constants near
the table declarations until typed helpers or generated app code cover this
path.

```go
package chat

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
			ReadModel: shunter.ReadModelMetadata{
				Tables: []string{"messages"},
				Tags:   []string{"history"},
			},
		}).
		View(shunter.ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages",
			ReadModel: shunter.ReadModelMetadata{
				Tables: []string{"messages"},
				Tags:   []string{"realtime"},
			},
		})
}

func sendMessage(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	body := string(args)
	_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{
		types.NewUint64(0),
		types.NewString(body),
	})
	return nil, err
}
```

Important module rules:

- `NewModule(name)` creates the declaration shell. Blank names are rejected by
  `Build`.
- `SchemaVersion` is the application schema version used by schema and
  recovery contracts.
- `Module.Version(...)` is application module metadata exported into
  `ModuleContract` artifacts. It is not the Shunter runtime/tool version.
- `TableDef` registers schema through the schema builder.
- `Reducer` registers a synchronous reducer handler and optional passive
  permission metadata.
- `Query` and `View` declare named read surfaces. If `SQL` is empty, the
  declaration is metadata-only and cannot be executed with `CallQuery` or
  `SubscribeView`.
- `Build` snapshots the module. Mutating the `Module` value after `Build` does
  not change the built runtime.

Reducers run on the serialized executor path. Do not retain
`*schema.ReducerContext`, use it from another goroutine, or do blocking
network/disk/RPC work while holding the executor.

## Build The Runtime

`Build` validates the module, builds the schema registry, opens or bootstraps
durable state, constructs reducer and declared-read catalogs, and returns a
runtime in the built state. It does not start workers or protocol services.

```go
rt, err := shunter.Build(chat.Module(), shunter.Config{
	DataDir: "./data/chat",
})
if err != nil {
	return err
}
defer rt.Close()
```

Configuration basics:

- `DataDir` stores snapshots, commit log segments, and recovery metadata. Blank
  uses the runtime default `./shunter-data`.
- `ExecutorQueueCapacity` and `DurabilityQueueCapacity` default to conservative
  non-zero capacities.
- `AuthModeDev` is the zero-value auth mode. Local calls in dev mode allow all
  permissions unless the caller explicitly sets permissions.
- `AuthModeStrict` requires explicit auth configuration when protocol serving
  is enabled.
- `Observability` configures runtime-scoped logs, metrics, diagnostics, and
  tracing. The zero value is a no-op.

Use separate data directories for separate applications or incompatible schema
lines. Treat `DataDir` as runtime-owned state.

## Start And Stop

Call `Start` before local reducer calls, local reads, declared reads, or
directly served HTTP traffic.

```go
ctx := context.Background()

if err := rt.Start(ctx); err != nil {
	return err
}
defer rt.Close()
```

`Close` shuts down runtime-owned lifecycle, durability, executor,
subscription, and protocol resources. It is safe to defer after a successful
`Build`, but most applications should treat a `Start` failure as startup
failure and exit.

For long-running service processes that want Shunter to own the HTTP server
lifecycle, use `ListenAndServe` instead of calling `Start` yourself.

```go
rt, err := shunter.Build(chat.Module(), shunter.Config{
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

`ListenAndServe` starts the runtime if needed, serves `Runtime.HTTPHandler()`,
and closes runtime ownership when the context is canceled.

## Call Reducers Locally

Use `CallReducer` when your process wants to invoke a reducer without going
through the WebSocket protocol.

```go
res, err := rt.CallReducer(ctx, "send_message", []byte("hello"))
if err != nil {
	return err
}
if res.Status != shunter.StatusCommitted {
	return fmt.Errorf("send_message failed: %v", res.Error)
}
```

Reducer arguments and results are raw byte slices at the runtime boundary. Your
application can choose its encoding. Use `bsatn` when you want Shunter's binary
value encoding, or keep a narrower app-specific encoding when that is enough.

Local calls accept caller options:

```go
res, err := rt.CallReducer(
	ctx,
	"send_message",
	payload,
	shunter.WithRequestID(42),
	shunter.WithPermissions("messages:write"),
)
```

Use permission options when testing strict permission behavior. In dev auth
mode, omitting permissions allows all local reducer permissions by default.

## Read State Locally

Use `Runtime.Read` for callback-scoped committed-state reads. The read view is
valid only during the callback.

```go
err := rt.Read(ctx, func(view shunter.LocalReadView) error {
	count := view.RowCount(messagesTableID)
	fmt.Printf("messages: %d\n", count)
	return nil
})
```

Use declared reads when you want named app-owned read surfaces with SQL,
permission metadata, read-model metadata, visibility filtering, and contract
export.

```go
result, err := rt.CallQuery(
	ctx,
	"recent_messages",
	shunter.WithDeclaredReadPermissions("messages:read"),
)
if err != nil {
	return err
}
for _, row := range result.Rows {
	fmt.Println(row[1].AsString())
}
```

Subscribe to a declared view locally with `SubscribeView`:

```go
sub, err := rt.SubscribeView(
	ctx,
	"live_messages",
	7,
	shunter.WithDeclaredReadPermissions("messages:subscribe"),
)
if err != nil {
	return err
}
fmt.Println(sub.TableName, len(sub.InitialRows))
```

`SubscribeView` admits the subscription and returns the initial rows. Protocol
clients receive ongoing transaction updates through the protocol path.

## Serve Protocol Traffic

Set `Config.EnableProtocol` to mount the WebSocket protocol endpoint at
`/subscribe`.

Use `HTTPHandler` when your application owns the HTTP server:

```go
rt, err := shunter.Build(chat.Module(), shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
})
if err != nil {
	return err
}
if err := rt.Start(ctx); err != nil {
	return err
}
defer rt.Close()

server := &http.Server{
	Addr:    "127.0.0.1:3000",
	Handler: rt.HTTPHandler(),
}
return server.ListenAndServe()
```

`HTTPHandler` does not start lifecycle. If you use it directly, call `Start`
before serving traffic.

If `Observability.Diagnostics.MountHTTP` is enabled, `HTTPHandler` also mounts
runtime diagnostics endpoints such as health, readiness, and optional metrics
handlers.

## Permissions And Visibility

Permission metadata is passive until a runtime path checks it. The main places
to attach it are:

- reducer declarations with `WithReducerPermissions`
- query and view declarations with `PermissionMetadata`
- table read policies through schema table options such as
  `schema.WithReadPermissions`

Example:

```go
mod.Reducer("send_message", sendMessage, shunter.WithReducerPermissions(
	shunter.PermissionMetadata{Required: []string{"messages:write"}},
))

mod.Query(shunter.QueryDeclaration{
	Name:        "recent_messages",
	SQL:         "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{Required: []string{"messages:read"}},
})
```

Visibility filters are row-level SQL filters attached to a module:

```go
mod.VisibilityFilter(shunter.VisibilityFilterDeclaration{
	Name: "own_messages",
	SQL:  "SELECT * FROM messages WHERE body = :sender",
})
```

Use declared read options such as `WithDeclaredReadIdentity`,
`WithDeclaredReadPermissions`, and `WithDeclaredReadAllowAllPermissions` to test
caller-specific behavior locally.

## Export Contracts

Use contract export when another project, generated client, or review workflow
needs the runtime's app-facing shape.

```go
contractJSON, err := rt.ExportContractJSON()
if err != nil {
	return err
}
if err := os.WriteFile(shunter.DefaultContractSnapshotFilename, contractJSON, 0o666); err != nil {
	return err
}
```

The exported `ModuleContract` includes module identity, schema, queries, views,
visibility filters, permissions, read-model metadata, migration metadata, and
codegen metadata.

The generic CLI operates on existing contract JSON files:

```bash
rtk go run ./cmd/shunter contract diff --previous old.json --current shunter.contract.json
rtk go run ./cmd/shunter contract policy --previous old.json --current shunter.contract.json --strict
rtk go run ./cmd/shunter contract codegen --contract shunter.contract.json --language typescript --out client.ts
```

The CLI does not dynamically load app modules. Export contracts from an
app-owned binary that links the module.

## Versioning

There are two separate version concepts:

- Shunter repo/tool/runtime version: stored in `VERSION`, exposed through
  `shunter.CurrentBuildInfo()`, and printed by `shunter version`.
- App module version: set with `Module.Version(...)` and exported into
  `ModuleContract` artifacts.

Do not use `Module.Version(...)` to report the Shunter runtime version.

For Shunter release binaries, stamp exact metadata with linker variables:

```bash
rtk go build -ldflags "-X github.com/ponchione/shunter.Version=v0.1.0 -X github.com/ponchione/shunter.Commit=<git-sha> -X github.com/ponchione/shunter.Date=<utc-rfc3339>" ./cmd/shunter
```

## Operational Checklist

Before relying on a Shunter-backed app:

- Keep module declarations deterministic. Table and reducer order affects the
  built schema contract.
- Set a durable `DataDir` outside temporary directories.
- Start the runtime before local calls or direct HTTP serving.
- Close the runtime during shutdown.
- Export and review `shunter.contract.json` when changing app-facing tables,
  reducers, queries, views, permissions, or visibility filters.
- Run targeted package tests for changed app code and Shunter integration
  tests that cover reducer, read, protocol, and recovery paths.
- Check `rt.Health()` and diagnostics endpoints for readiness and degraded
  recovery information in service environments.

## Useful Go Doc Entrypoints

```bash
rtk go doc github.com/ponchione/shunter
rtk go doc github.com/ponchione/shunter.Module
rtk go doc github.com/ponchione/shunter.Config
rtk go doc github.com/ponchione/shunter.Runtime
rtk go doc github.com/ponchione/shunter/schema.TableDefinition
rtk go doc github.com/ponchione/shunter/types.ReducerContext
```
