# Reads, Queries, And Views

Status: current v1 app-author guidance
Scope: local committed reads, declared queries, and declared live views.

Shunter has three app-facing read paths:

- `Runtime.Read` for in-process callback-scoped committed-state reads.
- `Runtime.CallQuery` for named request/response declared reads.
- `Runtime.SubscribeView` for named live view admission and initial rows.

The protocol path exposes corresponding raw and declared read behavior to
external clients when protocol serving is enabled.

Use local reads for app-internal code. Use declared queries and views when the
read surface is part of the app contract, should be permissioned by name, or
should appear in generated clients.

## Local Committed Reads

Use `Runtime.Read` when Go code in the same process needs direct committed
state access.

```go
err := rt.Read(ctx, func(view shunter.LocalReadView) error {
	count := view.RowCount(messagesTableID)
	_ = count

	for rowID, row := range view.TableScan(messagesTableID) {
		_ = rowID
		_ = row
	}
	return nil
})
```

The `LocalReadView` is valid only during the callback. Do not retain it or row
views beyond the callback.

Use index APIs for known access paths:

```go
err := rt.Read(ctx, func(view shunter.LocalReadView) error {
	for rowID, row := range view.SeekIndex(
		messagesTableID,
		messagesByChannelIndexID,
		types.NewString("general"),
	) {
		_ = rowID
		_ = row
	}
	return nil
})
```

## Declared Queries

Declare request/response read surfaces on the module.

```go
mod.Query(shunter.QueryDeclaration{
	Name: "recent_messages",
	SQL:  "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{
		Required: []string{"messages:read"},
	},
})
```

Call them locally with `Runtime.CallQuery`:

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

Use declared queries when the read is part of the app contract, should appear
in generated clients, or needs declaration-level permissions.

Declared queries can carry typed app parameters. Use SQL placeholders named
after product-schema columns, then pass ordered runtime values with
`WithDeclaredReadParameters`.

```go
mod.Query(shunter.QueryDeclaration{
	Name: "messages_by_topic",
	SQL:  "SELECT * FROM messages WHERE topic = :topic AND id > :after_id",
	Permissions: shunter.PermissionMetadata{
		Required: []string{"messages:read"},
	},
}, shunter.WithQueryParameters(shunter.ProductSchema{
	Columns: []shunter.ProductColumn{
		{Name: "topic", Type: "string"},
		{Name: "after_id", Type: "uint64"},
	},
}))
```

```go
result, err := rt.CallQuery(
	ctx,
	"messages_by_topic",
	shunter.WithDeclaredReadPermissions("messages:read"),
	shunter.WithDeclaredReadParameters(types.ProductValue{
		types.NewString("general"),
		types.NewUint64(100),
	}),
)
```

Parameter values are bound as typed values, not interpolated into SQL text.
The product-value order must match the `Parameters.Columns` order. `:sender`
remains caller identity and `sender` is reserved as a parameter name.

## Declared Views

Declare live read surfaces on the module.

```go
mod.View(shunter.ViewDeclaration{
	Name: "live_messages",
	SQL:  "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{
		Required: []string{"messages:subscribe"},
	},
})
```

Subscribe locally:

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
_ = sub.InitialRows
```

`SubscribeView` admits the subscription and returns initial rows. Protocol
clients receive ongoing transaction updates through the protocol path.

Declared views use the same parameter model as declared queries:

```go
mod.View(shunter.ViewDeclaration{
	Name: "live_messages_by_topic",
	SQL:  "SELECT * FROM messages WHERE topic = :topic",
	Permissions: shunter.PermissionMetadata{
		Required: []string{"messages:subscribe"},
	},
}, shunter.WithViewParameters(shunter.ProductSchema{
	Columns: []shunter.ProductColumn{
		{Name: "topic", Type: "string"},
	},
}))
```

```go
sub, err := rt.SubscribeView(
	ctx,
	"live_messages_by_topic",
	8,
	shunter.WithDeclaredReadPermissions("messages:subscribe"),
	shunter.WithDeclaredReadParameters(types.ProductValue{
		types.NewString("general"),
	}),
)
```

## SQL Surface

Shunter's SQL surface is intentionally narrow. Do not treat it as broad SQL
database compatibility.

Current read compatibility by surface:

- Protocol `OneOffQuery` executes Shunter's read-only SQL subset against a
  committed snapshot. Supported shapes include single-table reads, bounded
  joins and multi-way joins, column projections and aliases, `COUNT`/`SUM`
  aggregates including `COUNT(DISTINCT column)`, `ORDER BY`, `LIMIT`, and
  `OFFSET`. Table read policies and visibility filters apply. There is no
  root-level local raw SQL API in v1.
- Declared queries use `QueryDeclaration.SQL`, `Runtime.CallQuery`, and the
  protocol declared-query path. They use the one-off read executor with
  declaration-level permission metadata and may expose private tables when the
  declaration permission allows the caller. Empty SQL is metadata-only and
  returns `ErrDeclaredReadNotExecutable` when executed. Executable declared
  queries may use declared app placeholders such as `:topic`; callers must
  supply matching typed declared-read parameters.
- Raw subscriptions use protocol `SubscribeSingle` and `SubscribeMulti` to
  register table-shaped live reads. They support single-table and join
  predicates, including `SELECT *` for single tables and `SELECT table.*` or
  alias-shaped emitted relations for joins. Table read policies and visibility
  filters apply. Raw subscriptions reject column projections, aggregates,
  `ORDER BY`, `LIMIT`, and `OFFSET`.
- Declared live views use `ViewDeclaration.SQL`, `Runtime.SubscribeView`, and
  the protocol declared-view subscription path. Supported shapes include
  table-shaped reads, joins, self joins, cross joins, multi-way joins, column
  projections over the emitted relation, and `COUNT`/`SUM` aggregates including
  join, cross-join, and multi-way aggregate views. `COUNT(DISTINCT column)` is
  supported. Aggregate aliases must use `AS`. `ORDER BY`, `LIMIT`, and
  `OFFSET` are supported only for single-table, non-aggregate live views and
  shape the initial snapshot only. Declared live views may use the same
  declared app placeholders and parameter binding as declared queries.
- Local runtime reads use `Runtime.Read` for callback-scoped committed-state
  access. `Runtime.CallQuery` and `Runtime.SubscribeView` are the local
  declared-read APIs.

Declared-read parameters are only for declared queries and declared views. Raw
protocol `OneOffQuery`, `SubscribeSingle`, and `SubscribeMulti` do not accept
client-side parameter payloads.

If a read is important to the app contract, prefer a declared query or declared
view over ad hoc raw SQL.

For declared live views with `ORDER BY`, `LIMIT`, or `OFFSET`, only the
initial snapshot follows those clauses. These clauses are currently limited to
single-table, non-aggregate live views. Post-commit delivery is not maintained
as a top-N/windowed view: non-aggregate views emit ordinary row deltas over
matching rows, and aggregate views emit replacement aggregate rows when the
aggregate changes.

## Permissions

Declared queries and views can carry `PermissionMetadata`. Local callers pass
declared-read permissions with `WithDeclaredReadPermissions`, or use
`WithDeclaredReadAllowAllPermissions` in trusted tests and tooling.

In dev auth mode, local declared-read calls allow all permissions unless the
caller explicitly supplies permissions. Strict mode removes that default
allow-all behavior.

Use `WithDeclaredReadAuthPrincipal`, `WithDeclaredReadIdentity`, and
`WithDeclaredReadConnectionID` when a trusted in-process adapter has already
authenticated the caller outside the protocol path.

## Visibility

Visibility filters narrow rows before read evaluation or live delivery.

```go
mod.VisibilityFilter(shunter.VisibilityFilterDeclaration{
	Name: "own_messages",
	SQL:  "SELECT * FROM messages WHERE owner = :sender",
})
```

Use visibility filters for row-level caller isolation. Use permissions for
surface-level admission.

## Indexing

Indexes matter for reads that must stay fast:

- reducer lookups
- declared query predicates
- declared view predicates
- subscription predicates
- joins
- visibility filters
- local `SeekIndex` and `SeekIndexRange` reads

Large scans, unindexed live joins, and high-fanout subscriptions should be
treated as app design risks until the app has measured its workload.
