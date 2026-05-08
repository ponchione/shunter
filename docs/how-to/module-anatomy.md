# Module Anatomy

Status: current v1 app-author guidance
Scope: declaring app modules through the root `shunter` package.

A Shunter module is the app-owned declaration that becomes a runtime. Keep the
module declaration deterministic and close to the reducer code it registers.

## Basic Shape

```go
func Module() *shunter.Module {
	return shunter.NewModule("chat").
		Version("v0.1.0").
		SchemaVersion(1).
		Metadata(map[string]string{
			"owner": "chat-service",
		}).
		TableDef(messagesTable()).
		Reducer("send_message", sendMessage).
		Query(recentMessagesQuery()).
		View(liveMessagesView())
}
```

The most common module methods are:

- `NewModule(name)` creates the declaration shell.
- `Version(v)` sets app-owned module version metadata.
- `SchemaVersion(v)` sets the application schema version.
- `Metadata(values)` stores string metadata for contract export.
- `TableDef(def, opts...)` registers a table.
- `Reducer(name, handler, opts...)` registers a reducer.
- `Query(decl)` registers a named request/response read.
- `View(decl)` registers a named live read.
- `VisibilityFilter(decl)` registers row-level read filtering.
- `Migration(...)` and `TableMigration(...)` attach descriptive migration
  metadata.
- `MigrationHook(...)` registers an app-owned startup migration hook.

Blank module names are allowed at construction time and rejected by `Build`.

## Declaration Order And IDs

The schema builder assigns table and index IDs from the validated module
declaration. Handwritten code that calls reducer DB or local read APIs still
needs those IDs.

Keep table and index ID constants near their declarations until generated app
helpers cover this path:

```go
const (
	messagesTableID          schema.TableID = 0
	messagesByChannelIndexID schema.IndexID = 1
)
```

Update these constants deliberately when table or index order changes, and
review the exported contract as part of the change.

## Tables

Declare tables with `schema.TableDefinition`.

```go
func messagesTable() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "channel", Type: types.KindString},
			{Name: "owner", Type: types.KindBytes},
			{Name: "body", Type: types.KindString},
			{Name: "created_at", Type: types.KindInt64},
		},
		Indexes: []schema.IndexDefinition{
			{Name: "by_channel_created", Columns: []string{"channel", "created_at"}},
			{Name: "by_owner", Columns: []string{"owner"}},
		},
	}
}
```

Primary-key columns synthesize a unique primary-key index before declared
secondary indexes. Do not repeat the same primary-key access path as a
secondary index unless a future schema need explicitly requires it.

Add secondary indexes for access paths that matter:

- reducer lookups
- `Runtime.Read` index seeks
- declared query and view predicates
- raw protocol read predicates
- subscription predicates
- join keys
- visibility-filter columns

Composite index order matters. Put the equality or join column that best
narrows the search first.

## Reducers

Reducer registration binds a public reducer name to a Go handler.

```go
mod.Reducer("send_message", sendMessage, shunter.WithReducerPermissions(
	shunter.PermissionMetadata{Required: []string{"messages:write"}},
))
```

Permission metadata is passive until a runtime path checks it. Attach it during
module declaration so contracts and generated clients see the intended access
surface.

## Lifecycle Hooks

`OnConnect` and `OnDisconnect` register app callbacks for protocol connection
lifecycle events. They receive a reducer context, so the same reducer rules
apply: do not retain the context, do not use it from another goroutine, and do
not run long blocking work on the executor path.

`MigrationHook` registers startup migration code for the module. Hooks run as
app-owned migrations during startup or through offline maintenance tooling; keep
them idempotent and take backups before data-rewrite migrations. See
[Persistence and shutdown](persistence-and-shutdown.md) for the operational
flow.

## Declared Queries

Use declared queries for named request/response reads.

```go
mod.Query(shunter.QueryDeclaration{
	Name: "recent_messages",
	SQL:  "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{
		Required: []string{"messages:read"},
	},
	ReadModel: shunter.ReadModelMetadata{
		Tables: []string{"messages"},
		Tags:   []string{"history"},
	},
})
```

If `SQL` is empty, the declaration is metadata-only and cannot be executed with
`Runtime.CallQuery` or the protocol declared-query path.

## Declared Views

Use declared views for named live reads.

```go
mod.View(shunter.ViewDeclaration{
	Name: "live_messages",
	SQL:  "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{
		Required: []string{"messages:subscribe"},
	},
	ReadModel: shunter.ReadModelMetadata{
		Tables: []string{"messages"},
		Tags:   []string{"realtime"},
	},
})
```

Declared views are exported in contracts and are the preferred stable surface
for client subscriptions.

## Visibility Filters

Visibility filters narrow rows for caller-specific reads.

```go
mod.VisibilityFilter(shunter.VisibilityFilterDeclaration{
	Name: "own_messages",
	SQL:  "SELECT * FROM messages WHERE owner = :sender",
})
```

Use visibility filters for data that should always be narrowed by caller
identity. Use permission metadata for admission decisions such as "can this
caller use this read surface at all?"

## Metadata And Migrations

`Metadata` stores app-owned string metadata in the exported contract.
`Migration` and `TableMigration` attach descriptive migration metadata to the
contract; they do not rewrite data by themselves. Use `MigrationHook` for
app-owned migration code and the offline helpers in the persistence guide for
preflight and migration runs.

## Build Snapshot

`Build` snapshots the module definition. Mutating the `Module` value after
`Build` does not change the built runtime.

That rule is useful for tests and startup safety, but app code should still
prefer immutable module construction: declare the module once, build it once,
then pass the runtime around.

## Versioning

Keep these version concepts separate:

- `Module.Version(...)` is app module metadata exported into contracts.
- `Module.SchemaVersion(...)` is the app schema/recovery version.
- `VERSION`, `shunter.CurrentBuildInfo()`, and `shunter version` describe the
  Shunter runtime/tool version.
