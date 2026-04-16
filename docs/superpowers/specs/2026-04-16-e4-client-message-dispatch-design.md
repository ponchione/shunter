# E4 Client Message Dispatch — Design Spec

**Date:** 2026-04-16
**Phase:** 7d
**Parent:** SPEC-005 Epic 4
**Depends on:** E1 (wire codecs), E3 (connection state, subscription tracker)
**Blocks:** E5 (server message delivery)

---

## Overview

Replace the stub `runReadPump` with a full dispatch loop that decodes incoming WebSocket frames and routes them to typed handler functions. Four handlers cover the complete client message vocabulary: Subscribe, Unsubscribe, CallReducer, OneOffQuery.

## Design Decisions

### Direct dispatch (no intermediate channel)

The read loop calls handlers directly on the read goroutine. Handlers that need async processing (Subscribe, CallReducer) route through the executor's existing channel inbox. Epic 6 adds backpressure by wrapping the dispatch call site later — handler signatures stay unchanged.

**Rationale:** E4 scope excludes backpressure enforcement. Direct dispatch is simpler, testable, and the async boundary already exists at the executor inbox.

### Protocol-owned name resolution

A `protocol.SchemaLookup` interface maps wire-format string names (table, column) to internal IDs (schema.TableID, types.ColID). The protocol layer compiles predicates into `subscription.Predicate` before handing them to the executor/subscription layer.

**Rationale:** Keeps subscription package decoupled from wire-format strings. Protocol owns wire concerns. Matches SOLID — subscription validates structural correctness of compiled predicates, protocol validates wire-format correctness.

---

## Story 4.1: Frame Reader & Tag Dispatch

**File:** `protocol/dispatch.go`, `protocol/dispatch_test.go`

### Components

**`MessageHandlers`** struct:
```go
type MessageHandlers struct {
    OnSubscribe   func(ctx context.Context, conn *Conn, msg *SubscribeMsg)
    OnUnsubscribe func(ctx context.Context, conn *Conn, msg *UnsubscribeMsg)
    OnCallReducer func(ctx context.Context, conn *Conn, msg *CallReducerMsg)
    OnOneOffQuery func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg)
}
```

**`runDispatchLoop(ctx context.Context, handlers *MessageHandlers)`** — replaces `runReadPump`:

1. `ws.Read(ctx)` — get next frame
2. Reject text frames: close 1002 (protocol error)
3. If `conn.Compression`: `UnwrapCompressed(frame)` then `DecodeClientMessage(tagBody)`
4. If no compression: `DecodeClientMessage(frame)` directly
5. `MarkActivity()` on every successful read (preserves keepalive contract from Story 3.5)
6. Switch on tag: call corresponding handler from `MessageHandlers`
7. Nil handler for a tag: close 1002 (unconfigured message type)
8. `ErrUnknownMessageTag` / `ErrMalformedMessage`: close 1002, log details
9. WebSocket read error: return (triggers disconnect via supervisor)

**Supervisor update:** `superviseLifecycle` accepts `dispatchDone <-chan struct{}` replacing `readPumpDone`.

### Error Close Codes

| Condition | Close code | Reason |
|---|---|---|
| Text frame | 1002 | "text frames not supported" |
| Unknown tag | 1002 | "unknown message tag: {tag}" |
| Malformed body | 1002 | "malformed message" |
| Nil handler | 1002 | "unsupported message type" |
| Read error | — | disconnect pipeline (Story 3.6) |

---

## Story 4.2: Subscribe Handler

**Files:** `protocol/handle_subscribe.go`, `protocol/handle_subscribe_test.go`

### Interfaces

**`protocol.SchemaLookup`:**
```go
type SchemaLookup interface {
    TableByName(name string) (schema.TableID, *schema.TableSchema, bool)
}
```

Resolves wire-format table name to internal ID and full schema. Column lookup goes through `*schema.TableSchema` (which already has column-by-name methods).

### Handler: `handleSubscribe`

```
func handleSubscribe(
    ctx context.Context,
    conn *Conn,
    msg *SubscribeMsg,
    executor ExecutorInbox,
    schema SchemaLookup,
)
```

Steps:
1. `conn.Subscriptions.Reserve(msg.SubscriptionID)` — reject duplicate (pending or active)
2. Resolve `msg.Query.TableName` via `SchemaLookup.TableByName`
3. For each predicate: resolve column name via table schema, reject unknown columns
4. Validate v1 subset: equality predicates only (reject range predicates)
5. `NormalizePredicates(tableID, tableSchema, msg.Query.Predicates)` → `subscription.Predicate`
6. `executor.RegisterSubscription(ctx, req)` — submit to executor inbox (channel send)
7. On submission failure (inbox full/shutdown): release subscription_id from tracker, send `SubscriptionError`
8. On submission success: subscription stays pending. E5 handles SubscribeApplied delivery and tracker activation.

On any validation failure (steps 1-4): send `SubscriptionError` to `conn.OutboundCh`, release subscription_id if reserved.

### Predicate Normalization

`NormalizePredicates(tableID, tableSchema, preds []Predicate) (subscription.Predicate, error)`:

- `[]` (empty) → `subscription.AllRows{Table: tableID}`
- `[P1]` → `subscription.ColEq{Table: tableID, Column: colID, Value: value}`
- `[P1, P2, P3]` → left-associative And: `And{And{ColEq(P1), ColEq(P2)}, ColEq(P3)}`

---

## Story 4.3: Unsubscribe & CallReducer Handlers

**Files:** `protocol/handle_unsubscribe.go`, `protocol/handle_callreducer.go`, `protocol/handle_callreducer_test.go`

### Unsubscribe: `handleUnsubscribe`

```
func handleUnsubscribe(
    ctx context.Context,
    conn *Conn,
    msg *UnsubscribeMsg,
    executor ExecutorInbox,
)
```

1. Check `subscription_id` state in tracker:
   - Active → proceed
   - Pending → `ErrSubscriptionNotFound` (cannot unsubscribe before SubscribeApplied)
   - Missing → `ErrSubscriptionNotFound`
2. `executor.UnregisterSubscription(ctx, conn.ID, msg.SubscriptionID)` — submit to inbox
3. On submission failure: send error to client, tracker unchanged
4. On submission success: `conn.Subscriptions.Remove(msg.SubscriptionID)` immediately (read loop is single-goroutine, no race)
5. `UnsubscribeApplied` client delivery is E5's responsibility

### CallReducer: `handleCallReducer`

```
func handleCallReducer(
    ctx context.Context,
    conn *Conn,
    msg *CallReducerMsg,
    executor ExecutorInbox,
)
```

1. Reject lifecycle names `"OnConnect"`, `"OnDisconnect"` at protocol layer → send `ReducerCallResult` with `status=3` (not_found) and `ErrLifecycleReducer` message directly to `conn.OutboundCh`
2. `executor.CallReducer(ctx, req)` — submit to inbox (channel send)
3. On submission failure: send `ReducerCallResult` with error to `conn.OutboundCh`
4. On submission success: return. E5 handles `ReducerCallResult` delivery from executor response.

Lifecycle filtering happens here so the executor never sees these names from a client call path.

---

## Story 4.4: OneOffQuery Handler

**Files:** `protocol/handle_oneoff.go`, `protocol/handle_oneoff_test.go`

### Interface

**`CommittedStateAccess`:**
```go
type CommittedStateAccess interface {
    Snapshot() store.CommittedReadView
}
```

### Handler: `handleOneOffQuery`

```
func handleOneOffQuery(
    ctx context.Context,
    conn *Conn,
    msg *OneOffQueryMsg,
    stateAccess CommittedStateAccess,
    schema SchemaLookup,
)
```

1. Resolve table name via `SchemaLookup.TableByName`
2. Validate predicate columns exist
3. `stateAccess.Snapshot()` → `view`
4. `view.TableScan(tableID)` + filter by predicates → collect matching rows
5. Encode rows as RowList into buffer
6. `view.Close()` — release snapshot **before** network write
7. Send `OneOffQueryResult{Status: 0, Rows: encoded}` to `conn.OutboundCh`
8. On any error: send `OneOffQueryResult{Status: 1, Error: msg}` (snapshot closed in defer)

Does NOT go through executor. Read-only snapshot is safe for concurrent access.

---

## ExecutorInbox Extension

Current (E3):
```go
type ExecutorInbox interface {
    OnConnect(ctx, connID, identity) error
    OnDisconnect(ctx, connID, identity) error
    DisconnectClientSubscriptions(ctx, connID) error
}
```

E4 adds:
```go
    RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error
    UnregisterSubscription(ctx context.Context, connID types.ConnectionID, subID types.SubscriptionID) error
    CallReducer(ctx context.Context, req CallReducerRequest) error
```

Submit-and-return. Error means inbox full or executor shutdown — the command was NOT accepted. Success means the executor WILL process it. Response delivery (SubscribeApplied, UnsubscribeApplied, ReducerCallResult) is E5's job, wired through response channels embedded in the request types.

`RegisterSubscriptionRequest` and `CallReducerRequest` are protocol-side types wrapping wire fields + a response channel for E5 to watch.

---

## Response Delivery Boundary

E4 handlers send these responses directly:
- `SubscriptionError` (validation failures in Subscribe)
- `OneOffQueryResult` (self-contained query result)
- `ReducerCallResult` with `status=3` (lifecycle reducer rejection)

E5 handles:
- `SubscribeApplied` (requires initial rows from executor)
- `UnsubscribeApplied` (requires optional dropped rows)
- `TransactionUpdate` (fan-out from committed transactions)
- `ReducerCallResult` for actual reducer executions (routed through post-commit pipeline)

---

## Testing Strategy

Each story's `_test.go` uses mock dependencies:
- **Mock ExecutorInbox** — records calls, returns canned responses
- **Mock SchemaLookup** — configurable table/column mappings
- **Mock CommittedStateAccess** — returns a mock snapshot with canned rows
- **Dispatch loop tests** — use `websocket` in-memory pipe (httptest server + client) to feed raw frames

No real executor, store, or subscription manager in E4 tests. Handler logic tested in isolation.

---

## File Summary

| Story | New files |
|---|---|
| 4.1 | `protocol/dispatch.go`, `protocol/dispatch_test.go` |
| 4.2 | `protocol/handle_subscribe.go`, `protocol/handle_subscribe_test.go` |
| 4.3 | `protocol/handle_unsubscribe.go`, `protocol/handle_callreducer.go`, `protocol/handle_callreducer_test.go` |
| 4.4 | `protocol/handle_oneoff.go`, `protocol/handle_oneoff_test.go` |

Modified: `protocol/lifecycle.go` (ExecutorInbox extension), `protocol/disconnect.go` (supervisor update).
