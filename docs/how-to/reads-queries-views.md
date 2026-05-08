# Reads, Queries, And Views

Status: rough draft
Scope: local committed reads, declared queries, and declared live views.

Shunter has three app-facing read paths:

- `Runtime.Read` for in-process callback-scoped committed-state reads.
- `Runtime.CallQuery` for named request/response declared reads.
- `Runtime.SubscribeView` for named live view admission and initial rows.

The protocol path exposes corresponding raw and declared read behavior to
external clients when protocol serving is enabled.

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

## SQL Surface

Shunter's SQL surface is intentionally narrow. Do not treat it as broad SQL
database compatibility.

Use `docs/v1-compatibility.md` as the current support matrix for:

- one-off raw SQL
- declared queries
- raw subscriptions
- declared live views
- local runtime reads

If a read is important to the app contract, prefer a declared query or declared
view over ad hoc raw SQL.

## Permissions

Declared queries and views can carry `PermissionMetadata`. Local callers pass
declared-read permissions with `WithDeclaredReadPermissions`, or use
`WithDeclaredReadAllowAllPermissions` in trusted tests and tooling.

In dev auth mode, local declared-read calls allow all permissions unless the
caller explicitly supplies permissions. Strict mode removes that default
allow-all behavior.

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
