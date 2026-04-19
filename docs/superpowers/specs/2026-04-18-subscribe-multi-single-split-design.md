# SubscribeMulti / SubscribeSingle split + query-set grouping

Phase 2 Slice 2 — remaining narrow parity work after the `QueryID`
request+response rename closed.

Pin to flip: `protocol/parity_message_family_test.go::TestPhase2DeferralSubscribeNoMultiOrSingleVariants`.

Reference:
- `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
  (`SubscribeSingle`, `SubscribeMulti`, `Unsubscribe`, `UnsubscribeMulti`,
  `SubscribeApplied`, `SubscribeMultiApplied`, `UnsubscribeApplied`,
  `UnsubscribeMultiApplied`, `SubscriptionError`).
- `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs`
  (`add_subscription`, `add_subscription_multi`, `remove_subscription`).

---

## 1. Goal

Close the `SubscribeMulti` / `SubscribeSingle` variant split and the
one-QueryID-per-query-set grouping semantics that reference exposes on
its v1 wire. Collapse Shunter's single `Subscribe` / `Unsubscribe`
envelopes into the reference-aligned Single vs Multi pair plus
matching response envelopes. No legacy-name shims retained.

## 2. Non-goals

- SQL-string form for `SubscribeMulti.Queries`. Structured predicates
  stay; SQL belongs to the separate `OneOffQuery` SQL slice
  (Phase 2 Slice 1).
- `TotalHostExecutionDurationMicros` on applied envelopes. Tracked as
  a named deferral pin; not closed here.
- `SubscriptionError.TableID` optional field.
- Optional-`RequestID` / optional-`QueryID` on `SubscriptionError`
  for non-request-originated errors.
- Any change to the `SubscriptionUpdate` shape carried by
  `TransactionUpdate` / `TransactionUpdateLight`.
- `OneOffQuery` SQL front door and `P0-RECOVERY-002` — explicitly
  deferred by audit direction.

## 3. Wire surface

### 3.1 Client → server envelopes (`protocol/client_messages.go`)

```go
type SubscribeSingleMsg struct {
    RequestID uint32
    QueryID   uint32
    Query     Query
}

type SubscribeMultiMsg struct {
    RequestID uint32
    QueryID   uint32
    Queries   []Query
}

type UnsubscribeSingleMsg struct {
    RequestID   uint32
    QueryID     uint32
    SendDropped bool
}

type UnsubscribeMultiMsg struct {
    RequestID uint32
    QueryID   uint32
}
```

No legacy `SubscribeMsg` / `UnsubscribeMsg` names retained. Callers
migrate to the Single/Multi pair.

### 3.2 Server → client envelopes (`protocol/server_messages.go`)

```go
type SubscribeSingleApplied struct {
    RequestID uint32
    QueryID   uint32
    TableName string
    Rows      []byte // encoded RowList
}

type SubscribeMultiApplied struct {
    RequestID uint32
    QueryID   uint32
    Update    []SubscriptionUpdate // merged initial snapshot across all queries in set
}

type UnsubscribeSingleApplied struct {
    RequestID uint32
    QueryID   uint32
    HasRows   bool
    Rows      []byte
}

type UnsubscribeMultiApplied struct {
    RequestID uint32
    QueryID   uint32
    Update    []SubscriptionUpdate // delete-only delta of rows still live at unsubscribe
}

type SubscriptionError struct { // unchanged
    RequestID uint32
    QueryID   uint32
    Error     string
}
```

`SubscribeMultiApplied.Update` / `UnsubscribeMultiApplied.Update` reuse
the existing `[]SubscriptionUpdate` type already used by
`TransactionUpdate` / `TransactionUpdateLight`. No new row-batch shape.

`SubscribeMultiApplied.Update` entries carry `Inserts` populated and
`Deletes` nil. `UnsubscribeMultiApplied.Update` is the inverse.

### 3.3 Tag bytes (`protocol/tags.go`)

Wire byte values 1–4 (client) and 1–8 (server) stay fixed; only Go
symbol names change where renamed.

```go
// Client → server
TagSubscribeSingle   uint8 = 1  // renamed from TagSubscribe
TagUnsubscribeSingle uint8 = 2  // renamed from TagUnsubscribe
TagCallReducer       uint8 = 3
TagOneOffQuery       uint8 = 4
TagSubscribeMulti    uint8 = 5  // new
TagUnsubscribeMulti  uint8 = 6  // new

// Server → client
TagInitialConnection        uint8 = 1
TagSubscribeSingleApplied   uint8 = 2  // renamed from TagSubscribeApplied
TagUnsubscribeSingleApplied uint8 = 3  // renamed from TagUnsubscribeApplied
TagSubscriptionError        uint8 = 4
TagTransactionUpdate        uint8 = 5
TagOneOffQueryResult        uint8 = 6
TagReducerCallResult        uint8 = 7  // reserved
TagTransactionUpdateLight   uint8 = 8
TagSubscribeMultiApplied    uint8 = 9  // new
TagUnsubscribeMultiApplied  uint8 = 10 // new
```

## 4. Subscription-manager grouping

### 4.1 External key

Wire identity is `(ConnID, QueryID)`. Internal per-predicate key stays
`types.SubscriptionID` (used by fan-out, pruning indexes, and
`SubscriptionUpdate.SubscriptionID`). One `(ConnID, QueryID)` maps to
1..N internal `SubscriptionID` values.

### 4.2 API (`subscription/manager.go`)

Replaces existing `Register` / `Unregister`. No legacy wrappers.

```go
type SubscriptionSetRegisterRequest struct {
    ConnID         types.ConnectionID
    QueryID        uint32
    Predicates     []Predicate      // len 1 for Single; len >= 1 for Multi
    ClientIdentity *types.Identity
    RequestID      uint32
}

type SubscriptionSetRegisterResult struct {
    QueryID uint32
    Update  []SubscriptionUpdate // Inserts populated; one entry per (allocated SubscriptionID, table) pair
}

type SubscriptionSetUnregisterResult struct {
    QueryID uint32
    Update  []SubscriptionUpdate // Deletes populated; rows still live at unsubscribe time
}

type SubscriptionManager interface {
    RegisterSet(req SubscriptionSetRegisterRequest, view store.CommittedReadView) (SubscriptionSetRegisterResult, error)
    UnregisterSet(connID types.ConnectionID, queryID uint32, view store.CommittedReadView) (SubscriptionSetUnregisterResult, error)
    DisconnectClient(connID types.ConnectionID) error
    EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView, meta PostCommitMeta)
    DroppedClients() <-chan types.ConnectionID
}
```

`UnregisterSet` returns `Deletes`-populated `SubscriptionUpdate`
entries for rows still live at unsubscribe time. The handler uses
that to populate `UnsubscribeMultiApplied.Update`. For
`UnsubscribeSingleApplied` the handler collapses the len-1 result
back into the single-table `{HasRows, Rows}` shape. A
`CommittedReadView` is threaded through `UnregisterSet` so that the
still-live row set is observed under the same read-view contract as
registration.

### 4.3 Atomic registration (pre-validation, not rollback)

Matches reference's actual pattern:

1. Compile/validate all predicates in `Predicates` before touching
   registry state. Any compile or schema error → return error; no
   state changes.
2. Reject if `(ConnID, QueryID)` already live on this connection
   (reference `try_insert`). Return `ErrQueryIDAlreadyLive`.
3. Dedup identical predicates within the same call (reference
   `hash_set.insert(hash)` dedup).
4. Allocate N internal `SubscriptionID`s and register under the
   shared QueryID.
5. Run initial snapshot for each predicate against the same
   `CommittedReadView`. Merge rows into one `[]SubscriptionUpdate`,
   one entry per (SubscriptionID, table) pair.

Handler translates any returned error into one `SubscriptionError{
QueryID, Error}`.

### 4.4 Bookkeeping

Add to the subscription registry:

```go
querySets map[types.ConnectionID]map[uint32][]types.SubscriptionID
```

`DisconnectClient` drops the entire `querySets[conn]` bucket as well
as the existing per-connection state.

### 4.5 Single path collapses into set path

`SubscribeSingle` handler calls `RegisterSet` with `len(Predicates) == 1`.
The response adapter extracts the single-table `{TableName, Rows}`
shape for `SubscribeSingleApplied`. `SubscribeMulti` handler calls the
same API with the full list and emits `SubscribeMultiApplied{Update}`.
Reference does this literally — `add_subscription` is a one-line
wrapper around `add_subscription_multi`
(`reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:955-957`).

## 5. Dispatch + handlers

### 5.1 Dispatch (`protocol/dispatch.go`)

Add arms for `TagSubscribeMulti` and `TagUnsubscribeMulti`. Rename the
two existing arms to `TagSubscribeSingle` / `TagUnsubscribeSingle`.
Malformed bodies route through the existing `closeProtocolError`
helper.

### 5.2 Handlers

Split into four files:

- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_unsubscribe_single.go`
- `protocol/handle_unsubscribe_multi.go`

All four call into `RegisterSet` / `UnregisterSet`. Only the response
envelope shape differs per handler.

## 6. Fan-out worker

Unchanged. `SubscriptionUpdate.SubscriptionID` remains the internal
per-predicate key used by fan-out delta assembly. Grouping is a
register/unregister-only concern. `TransactionUpdate` heavy / light
delivery semantics are untouched.

## 7. Parity pins

### 7.1 Flip the deferral pin

Replace `TestPhase2DeferralSubscribeNoMultiOrSingleVariants` with
positive-shape pins on the new envelopes:

- `TestPhase2SubscribeSingleShape` → `{RequestID, QueryID, Query}`
- `TestPhase2SubscribeMultiShape` → `{RequestID, QueryID, Queries}`
- `TestPhase2UnsubscribeSingleShape` → `{RequestID, QueryID, SendDropped}`
- `TestPhase2UnsubscribeMultiShape` → `{RequestID, QueryID}`
- `TestPhase2SubscribeSingleAppliedCarriesRows` (rename of existing
  `TestPhase2SubscribeAppliedCarriesQueryID`)
- `TestPhase2UnsubscribeSingleAppliedCarriesRows` (rename of existing
  `TestPhase2UnsubscribeAppliedCarriesQueryID`)
- `TestPhase2SubscribeMultiAppliedCarriesUpdate` → `{RequestID, QueryID, Update}`
- `TestPhase2UnsubscribeMultiAppliedCarriesUpdate` → same shape

### 7.2 Add deferral pins for known out-of-scope divergences

- `TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration` — pins
  the absence of `TotalHostExecutionDurationMicros`.
- `TestPhase2DeferralSubscriptionErrorNoTableID` — pins the absence
  of `TableID` / optional-ness of `RequestID` / `QueryID`.
- `TestPhase2DeferralSubscribeMultiQueriesStructured` — pins
  `Queries []Query` structured, not SQL string.

### 7.3 Tag-byte stability pin

Extend existing pin to cover `TagSubscribeMulti = 5`,
`TagUnsubscribeMulti = 6`, `TagSubscribeMultiApplied = 9`,
`TagUnsubscribeMultiApplied = 10`.

## 8. Grouping-semantics tests (`subscription/manager_test.go`)

- `TestRegisterSetMultiAtomicOnInvalidPredicate` — SubscribeMulti with
  one invalid predicate → `SubscriptionError`, no subs registered,
  QueryID free for reuse.
- `TestRegisterSetMultiMergesInitialSnapshot` — N predicates across M
  tables → one merged `[]SubscriptionUpdate` with per-(sub,table)
  entries.
- `TestUnregisterSetDropsAllInSet` — UnsubscribeMulti drops all
  internal subs mapped under the QueryID.
- `TestRegisterSetRejectsDuplicateQueryID` — second RegisterSet with
  live QueryID on same conn → error.
- `TestRegisterSetDedupsIdenticalPredicates` — two identical
  predicates in one set → registered once.
- `TestDisconnectClientClearsQuerySets` — DisconnectClient drops the
  entire `querySets[conn]` bucket.

## 9. Codec tests

Extend `protocol/server_messages_test.go` and
`protocol/client_messages_test.go` with BSATN round-trip for the four
new envelopes (`SubscribeMultiMsg`, `UnsubscribeMultiMsg`,
`SubscribeMultiApplied`, `UnsubscribeMultiApplied`) and the two new
tag bytes.

## 10. Docs to update

- `docs/parity-phase0-ledger.md` §`P0-PROTOCOL-004` — flip the
  `SubscribeMulti` / `SubscribeSingle` deferral note to closed; add
  new positive pins by name; record §7.2 divergences as named
  deferrals.
- `docs/current-status.md:112` — flip "No `SubscribeMulti` /
  `SubscribeSingle` variant split yet" to closed; note remaining
  divergences.
- `NEXT-SESSION-PROMPT.md` — update "Suggested next slice" to the
  next remaining parity anchor (`OneOffQuery` SQL or
  `P0-RECOVERY-002`).
- `docs/spacetimedb-parity-roadmap.md` — mark Phase 2 Slice 2
  variant split closed.

## 11. Out-of-scope known divergences (deferrals, not closed)

Pinned in §7.2 so the next parity pass has explicit handles:

| # | Divergence | Reference | Pin |
|---|---|---|---|
| 1 | `TotalHostExecutionDurationMicros` on applied envelopes | `v1.rs:321, 335, 384, 399` | `TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration` |
| 2 | `SubscriptionError.TableID: Option<TableId>` | `v1.rs:365` | `TestPhase2DeferralSubscriptionErrorNoTableID` |
| 3 | `SubscriptionError.RequestID` / `.QueryID` optional | `v1.rs:355, 358` | same pin as #2 |
| 4 | `SubscribeMulti.Queries` is structured, reference uses `Box<[Box<str>]>` | `v1.rs:205` | `TestPhase2DeferralSubscribeMultiQueriesStructured` |
| 5 | `DatabaseUpdate` shape (ref: grouped by `TableId`; Shunter: per-`SubscriptionID`) | `v1.rs:541, 570` | existing `SubscriptionUpdate` shape — not pinned here |

## 12. Sequencing

Execution order left to the writing-plans skill output. One narrow
constraint the plan must respect: audit direction requires strict TDD
with a failing test first, minimal fix, no sweeping formatting or
unrelated cleanup.
