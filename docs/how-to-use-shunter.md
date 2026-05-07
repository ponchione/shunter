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

## Schema And Indexing

Primary-key columns synthesize a unique `pk` index. Secondary indexes are
declared on `schema.TableDefinition.Indexes` with column names in key order.

```go
schema.TableDefinition{
	Name: "messages",
	Columns: []schema.ColumnDefinition{
		{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
		{Name: "channel", Type: types.KindString},
		{Name: "owner", Type: types.KindBytes},
		{Name: "created_at", Type: types.KindInt64},
	},
	Indexes: []schema.IndexDefinition{
		{Name: "by_channel_created", Columns: []string{"channel", "created_at"}},
		{Name: "by_owner", Columns: []string{"owner"}},
	},
}
```

Index columns should match the access paths your app depends on:

- Add indexes for hot reducer lookups, local `SeekIndex` or `SeekIndexRange`
  reads, raw or declared read predicates, subscription predicates, join keys,
  and visibility-filter columns.
- Put the most selective equality or join column first in composite indexes.
  Key order is part of the schema contract.
- Do not repeat a primary-key column in an explicit secondary index; the schema
  builder already creates the primary-key index.
- One-off raw SQL can fall back to scans for supported shapes, but large scans,
  live joins, and high-fanout subscriptions should be treated as app design
  issues until the published performance envelope says otherwise.
- Indexes increase write cost and memory use, so index deliberate access paths
  rather than every column.

## In-Process Trust Model

Shunter v1 is an in-process Go runtime. Your reducers, lifecycle hooks,
scheduled reducer calls, and migration hooks run inside the application process,
not inside a sandbox. Shunter does not provide WASM isolation, dynamic module
upload, process-level memory limits, or automatic protection from goroutines
started by app code.

Reducer rules for v1 app code:

- Mutate Shunter state only through the reducer `DB` APIs.
- Keep reducer work deterministic where replay, scheduling, or recovery depends
  on the result.
- Avoid external side effects inside reducers. If a side effect is required,
  make the app-level idempotency and failure behavior explicit.
- Do not retain reducer contexts, read views, scheduler handles, or row values
  beyond their documented callback or reducer scope.
- Do not perform long-running network, disk, RPC, or sleep work on the
  serialized executor path.

Reducer user errors and reducer panics roll back the reducer transaction and
are reported as failed reducer results. The executor recovers reducer panics,
records them as failed calls, and continues serving later work. That recovery
does not protect the process from app-started goroutines, process-wide panics,
deadlocks, memory exhaustion, or blocking calls that never return.
Local reducer calls expose typed statuses for user errors, app panics,
permission failures, and Shunter runtime failures. Protocol `TransactionUpdate`
failure strings include the same source distinction with `app reducer error:`,
`app reducer panic:`, `permission denied:`, or `shunter runtime error:`
prefixes.

Lifecycle hooks and scheduled reducer calls use the same in-process trust
boundary. Migration hooks may run during startup or through an offline
maintenance binary; write them as app-owned data migrations that can fail
clearly and be retried safely from a known backup.

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
- See `docs/authentication.md` for the current dev/strict auth contract.
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

## Snapshot And Compact

Use `CreateSnapshot` when your process wants to write a full durable snapshot
at the runtime's current committed transaction horizon. The call is
synchronous and can block commits while state is serialized, so service
processes should stop admitting writes first.

```go
snapshotTxID, err := rt.CreateSnapshot()
if err != nil {
	return err
}
if err := rt.CompactCommitLog(snapshotTxID); err != nil {
	return err
}
```

Use this flow for app-owned maintenance jobs or graceful shutdown hooks after
traffic has been quiesced. `CompactCommitLog` only deletes sealed commit log
segments that are fully covered by the completed snapshot TX ID you pass.

## Backup And Restore

Shunter does not currently expose an online backup API. Treat backups as an
app-owned offline operation over the complete `DataDir`.

For a backup:

1. Stop accepting reducer calls and protocol traffic.
2. Optionally call `CreateSnapshot` and `CompactCommitLog` to shorten the
   replay suffix that will be copied.
3. Call `Close` and wait for it to return without error.
4. Copy the entire `DataDir` as one unit, preserving file contents and
   directory structure.

For a restore:

1. Stop the runtime process that owns the destination `DataDir`.
2. Restore the complete backup into an empty data directory; do not merge a
   backup over an existing Shunter data directory.
3. Start the same application module schema line against that directory.

Recovery validates the registered schema version and embedded snapshot schema
against the restored data. If the module schema is incompatible, `Build`
fails instead of rewriting durable state.

App-owned migration or maintenance binaries can run the same offline schema
preflight before starting the runtime. Missing or empty data directories are
treated as compatible fresh starts, and existing incompatible directories return
the same schema mismatch category as startup recovery:

```go
if err := shunter.CheckDataDirCompatibility(mod, shunter.Config{DataDir: "./data/chat"}); err != nil {
	return err
}
```

If the module declares startup migration hooks with `Module.MigrationHook`, an
offline binary can run those same registered hooks after stopping the runtime:

```go
result, err := shunter.RunModuleDataDirMigrations(ctx, mod, shunter.Config{DataDir: "./data/chat"})
if err != nil {
	return err
}
_ = result.DurableTxID
```

App-owned binaries can perform the offline directory copy after runtime
ownership has stopped:

```go
if err := shunter.BackupDataDir("./data/chat", "./backups/chat-2026-05-04"); err != nil {
	return err
}
if err := shunter.RestoreDataDir("./backups/chat-2026-05-04", "./data/chat"); err != nil {
	return err
}
```

The generic CLI uses the same helpers:

```bash
rtk go run ./cmd/shunter backup --data-dir ./data/chat --out ./backups/chat-2026-05-04
rtk go run ./cmd/shunter restore --backup ./backups/chat-2026-05-04 --data-dir ./data/chat
```

Restore refuses to merge into a non-empty destination.

For the operator-facing startup, shutdown, backup, restore, migration, upgrade,
and release checklist, see `docs/operations.md`.

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
	shunter.WithAuthPrincipal(shunter.AuthPrincipal{
		Issuer:  "https://issuer.example",
		Subject: "user-123",
	}),
	shunter.WithPermissions("messages:write"),
)
```

Use `WithAuthPrincipal` when an app-owned local adapter has already validated a
caller outside the WebSocket protocol. Permission checks still use
`WithPermissions`; principal permissions are context for reducer code, not an
admission bypass. In dev auth mode, omitting permissions allows all local
reducer permissions by default.

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
export. The exact SQL accepted by one-off raw SQL, raw subscriptions, declared
queries, and declared live views is the read-surface matrix in
`docs/v1-compatibility.md`.

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
Executable views may use table-shaped multi-way joins such as `SELECT a.*`,
column projections over the emitted relation, single-table `ORDER BY`, `LIMIT`,
and `OFFSET` initial snapshots, single-table `COUNT`/`SUM` aggregates, and
join and cross-join `COUNT`/`SUM` aggregates, including multi-way joins. Live
views still reject aggregate aliases without `AS`.

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

For in-process app-owned health checks, use `InspectRuntimeHealth(rt)` or
`InspectHostHealth(host)` and map their `Status` through `HealthzStatusCode`
or `ReadyzStatusCode` when an HTTP-style status is useful.

For multi-module hosts, `Host.ListenAndServe(ctx, addr)` starts every hosted
runtime, serves `Host.HTTPHandler()`, and closes the hosted runtimes when the
context is canceled.

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

For auth mode behavior, token claims, permissions, visibility, and production
strict-mode setup, see `docs/authentication.md`.

## Export Contracts

Use contract export when another project, generated client, or review workflow
needs the runtime's app-facing shape.

```go
if err := contractworkflow.ExportRuntimeFile(rt, shunter.DefaultContractSnapshotFilename); err != nil {
	return err
}
if err := contractworkflow.GenerateRuntimeFile(rt, "client.ts", codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
	return err
}
```

The exported `ModuleContract` includes module identity, schema, queries, views,
visibility filters, permissions, read-model metadata, migration metadata, and
codegen metadata.

Generated TypeScript includes row interfaces, a `TableRows` table-name-to-row
map, table subscription helper functions, executable declared-read name unions,
SQL maps, byte-level declared-read helper functions, and protocol metadata.
Typed decoding for declared query/view result rows belongs to the TypeScript
client runtime track.

The generic CLI operates on existing contract JSON files:

```bash
rtk go run ./cmd/shunter contract diff --previous old.json --current shunter.contract.json
rtk go run ./cmd/shunter contract policy --previous old.json --current shunter.contract.json --strict
rtk go run ./cmd/shunter contract plan --previous old.json --current shunter.contract.json --validate
rtk go run ./cmd/shunter contract codegen --contract shunter.contract.json --language typescript --out client.ts
rtk go run ./cmd/shunter backup --data-dir ./data/chat --out ./backups/chat-2026-05-04
```

`contract plan` is a dry-run review artifact. When the plan includes blocking
or data-rewrite changes, text output includes `guidance backup-restore`, and
JSON output sets `summary.backup_recommended` with a `backup-restore` guidance
entry.

The CLI does not dynamically load app modules. Export contracts from an
app-owned binary that links the module. Backup and restore operate on offline
`DataDir` directories through `shunter.BackupDataDir` and
`shunter.RestoreDataDir`.

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
- Follow `docs/operations.md` for offline backup/restore, migration, upgrade,
  and release workflows.
- Run targeted package tests for changed app code and Shunter integration
  tests that cover reducer, read, protocol, and recovery paths.
- Check `InspectRuntimeHealth(rt)`, `InspectHostHealth(host)`, or diagnostics
  endpoints for readiness and degraded recovery information in service
  environments.

## Useful Go Doc Entrypoints

```bash
rtk go doc github.com/ponchione/shunter
rtk go doc github.com/ponchione/shunter.Module
rtk go doc github.com/ponchione/shunter.Config
rtk go doc github.com/ponchione/shunter.Runtime
rtk go doc github.com/ponchione/shunter/schema.TableDefinition
rtk go doc github.com/ponchione/shunter/types.ReducerContext
```
