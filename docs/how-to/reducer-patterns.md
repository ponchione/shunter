# Reducer Patterns

Status: rough draft
Scope: writing reducers for Shunter modules.

Reducers are the only supported write boundary for normal Shunter apps. They
run synchronously on the runtime executor and receive a transaction-scoped
database through `*schema.ReducerContext`.

## Signature

```go
func sendMessage(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	// Decode args, read/write ctx.DB, return encoded result.
	return nil, nil
}
```

Reducer arguments and results are byte slices at the runtime boundary. Choose
one encoding for each reducer and keep it documented near the reducer or in
generated client bindings.

## Insert Rows

```go
_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{
	types.NewUint64(0),
	types.NewString(channel),
	types.NewString(body),
})
if err != nil {
	return nil, err
}
```

For auto-increment primary keys, pass the zero value in the primary-key column
and let the store assign the committed key.

## Read Before Write

Reducers can read through the same transaction-scoped database.

```go
for rowID, row := range ctx.DB.SeekIndex(
	uint32(messagesTableID),
	uint32(messagesByChannelIndexID),
	types.NewString(channel),
) {
	_ = rowID
	_ = row
}
```

Use indexes for reducer lookups that are expected to stay hot as data grows.

## Update And Delete

```go
row, ok := ctx.DB.GetRow(uint32(messagesTableID), rowID)
if !ok {
	return nil, fmt.Errorf("message not found")
}

row[2] = types.NewString(newBody)
if _, err := ctx.DB.Update(uint32(messagesTableID), rowID, row); err != nil {
	return nil, err
}
```

```go
if err := ctx.DB.Delete(uint32(messagesTableID), rowID); err != nil {
	return nil, err
}
```

Keep row construction positional and close to table declarations until generated
typed helpers exist for the app.

## Caller Metadata

Reducers can inspect caller metadata through the reducer context. Local callers
provide equivalent metadata with options such as `WithIdentity`,
`WithAuthPrincipal`, `WithConnectionID`, and `WithPermissions`.

Admission checks use the caller permission set propagated through the runtime
path. Principal metadata is context for app code; it is not an admission bypass.

## Error Behavior

Returning an error rolls back the reducer transaction and reports a failed
reducer result to the caller.

Reducer panics are recovered by the executor, reported as app reducer panics,
and the runtime continues serving later work. This recovery does not protect
the process from app-started goroutines, process-wide panics, deadlocks, memory
exhaustion, or blocking calls that never return.

## Do

- Keep reducers deterministic when replay, recovery, or scheduling depends on
  the result.
- Mutate Shunter state only through `ctx.DB`.
- Keep index use aligned with table declarations.
- Return app validation failures as normal errors.
- Keep reducer logic small enough to test directly through `Runtime.CallReducer`.

## Avoid

- Retaining `*schema.ReducerContext` after the reducer returns.
- Using the reducer context from another goroutine.
- Performing long-running network, disk, RPC, or sleep work on the executor
  path.
- Depending on broad SQL mutation. Writes belong in reducers.
- Hiding table ID or column order assumptions far from table declarations.

## Side Effects

Prefer doing external side effects outside reducers. If a reducer must trigger
an external effect, make app-level idempotency explicit so retry, failure, or
recovery behavior is understandable.

Common patterns:

- write an outbox row in the reducer, then process it outside the executor
- make the reducer idempotent with an app request ID
- perform external calls before the reducer only when the reducer can safely
  reject or reconcile the result
