# SPEC-004: Subscription Evaluator

**Status:** Draft
**Depends on:** SPEC-001 (`CommittedReadView`, `Changeset`, `ProductValue`, `Bound`), SPEC-003 (`TxID`, `ConnectionID`, `Identity`, `ReducerCallResult`), SPEC-005 (`ClientSender` / `FanOutSender` delivery surface, backpressure contract), SPEC-006 (`SchemaLookup`, `IndexResolver`)
**Depended on by:** SPEC-003 (executor hands changesets to the evaluator post-commit), SPEC-005 (protocol layer consumes `FanOutMessage` / `SubscriptionUpdate` / `SubscriptionError` and registers subscriptions via the manager)

---

## 1. Purpose

The subscription evaluator answers one question after every committed transaction: **which clients care about this change, and what exactly changed in their view of the data?**

A subscription is a standing query. The client declares a predicate over a table (or a join of two tables). The evaluator maintains the invariant that each subscriber's view is a live mirror of its query result set, updated incrementally after every commit via deltas (rows added, rows removed).

This is the most performance-critical subsystem in Shunter. Every committed transaction must flow through subscription evaluation before clients see updates. The design must handle thousands of concurrent subscriptions without making evaluation time proportional to the total subscription count.

---

## 2. Concepts and Terminology

| Term | Definition |
|------|-----------|
| **Subscription** | A registered standing query associated with a client connection. Defined by a predicate over one or two tables. |
| **Predicate** | A structured filter expression that defines which rows a subscription matches. Not an opaque function — must be inspectable at registration time. |
| **Delta** | The difference between a subscription's result set before and after a transaction. Expressed as `(inserts []ProductValue, deletes []ProductValue)`. |
| **Changeset** | The mutation journal from a committed transaction: all inserts and deletes per table. Produced by the transaction executor (SPEC-003). |
| **Pruning** | Skipping evaluation of subscriptions that cannot possibly be affected by a given changeset. |
| **Fan-out** | Delivering computed deltas to the correct client connections. |
| **Query hash** | A deterministic identifier for a subscription's predicate structure and parameters, used for deduplication. |

---

## 3. Subscription Predicate Model

### 3.1 Why Not Opaque Go Functions

The evaluator's pruning optimization depends on inspecting the predicate at registration time to extract `(table, column, value)` tuples. An opaque `func(row ProductValue) bool` cannot be inspected, so every subscription would be evaluated for every change. This is O(subscriptions) per transaction — unacceptable at scale.

### 3.2 Predicate Representation

A subscription predicate is a structured expression tree:

```go
// Predicate is a filter expression over rows from one or two tables.
type Predicate interface {
    // Tables returns the table IDs this predicate reads from.
    Tables() []TableID
    // sealed prevents external implementations.
    sealed()
}

// ColEq matches rows where a column equals a literal value.
//   Example: messages.channel_id = 42
type ColEq struct {
    Table  TableID
    Column ColID
    Value  Value
}

// ColRange matches rows where a column falls within a range.
//   Example: events.timestamp >= 1000 AND events.timestamp < 2000
type ColRange struct {
    Table  TableID
    Column ColID
    Lower  Bound // inclusive/exclusive + value, or unbounded
    Upper  Bound
}

// And combines two predicates. Both must match.
type And struct {
    Left  Predicate
    Right Predicate
}

// AllRows matches every row in a table (no filter).
type AllRows struct {
    Table TableID
}

// Join matches rows from two tables joined on a column pair,
// with an optional filter on either side.
type Join struct {
    Left      TableID
    Right     TableID
    LeftCol   ColID
    RightCol  ColID
    Filter    Predicate // optional additional filter (may be nil)
}
```

### 3.3 Predicate Constraints

1. A subscription reads from **at most two tables**. Three-way joins and above are rejected at registration.
2. Join subscriptions require an **index on the join column** of at least one side. Unindexed joins are rejected — they would require full table scans on every delta.
3. Predicates must use **literal values**, not expressions referencing other columns (except in join conditions). This enables value-based pruning.

### 3.4 Query Hash

Each predicate is identified by a deterministic hash (blake3, 32 bytes). The hash is computed from the predicate's canonical serialized form:

- **Non-parameterized predicates** (same for all clients): hash of the predicate structure.
- **Parameterized predicates** (per-client values, e.g., "my messages"): hash of the predicate structure + client identity.

Two clients with structurally identical predicates and identical parameter values share the same query hash. This enables deduplication (Section 7).

---

## 4. Subscription Lifecycle

### 4.1 Registration

```go
// SubscriptionRegisterRequest carries the validated subscription parameters from the
// protocol layer to the executor and then to the subscription manager.
type SubscriptionRegisterRequest struct {
    ConnID         ConnectionID
    SubscriptionID SubscriptionID
    Predicate      Predicate      // validated and compiled by the protocol layer
    RequestID      uint32         // echoed in SubscribeApplied
}

// SubscriptionRegisterResult is returned by Register after the initial query executes
// and the subscription is fully registered.
type SubscriptionRegisterResult struct {
    SubscriptionID SubscriptionID
    InitialRows    []ProductValue // all rows matching the predicate at registration time
}
```

```
Subscribe(connID ConnectionID, predicate) → (initialRows []ProductValue, subscriptionID SubscriptionID, error)
```

1. **Validate** the predicate: at most 2 tables, required indexes exist, column types match.
2. **Compile** the predicate into execution plans (Section 6).
3. **Compute query hash** from the predicate's canonical form.
4. **Check deduplication**: if another subscription with the same hash exists, reuse its compiled plans.
5. **Execute the initial query** against current committed state.
6. **Register** the subscription in the manager's pruning indexes (Section 5).
7. **Return** the initial rows to the client (delivered as "inserts" in the subscription's first delta).

The entire registration flow runs inside one executor command, not as an out-of-band snapshot read. Therefore no committed transaction may slip between “execute initial query” and “start receiving deltas.”

For v1, “compile” may be a no-op that records the validated predicate itself as the executable plan. The important contract is ownership and ordering: compilation (even if trivial), initial query materialization, and activation all happen inside the same executor command.

### 4.2 Deregistration

```
Unsubscribe(connID ConnectionID, subscriptionID) → error
```

1. **Remove** the client from the subscription's subscriber set.
2. **Decrement** ref counts on pruning indexes.
3. If the subscription has **no remaining subscribers**, remove it from all indexes and free compiled plans.
4. Optionally **send final delta** showing removed rows (configurable per client).

### 4.3 Client Disconnect

When a client connection drops, all its subscriptions are removed. This is equivalent to calling Unsubscribe for each active subscription, but batched for efficiency.

---

## 5. Pruning Indexes

The subscription manager maintains three parallel indexes. Each `(query, table)` pair appears in **exactly one** index. On a table change, all three are consulted and results unioned.

### 5.1 Tier 1: Value Index (SearchArguments)

For subscriptions with a `ColEq` predicate on a table.

**Structure:**

```go
type ValueIndex struct {
    // cols[table][col] = refcount of active entries for that column.
    // Used by TrackedColumns during candidate collection.
    cols map[TableID]map[ColID]int

    // args[table][col][encodedValue] = set of query hashes.
    // encodedValue is the canonical byte form of a Value used as a map key.
    args map[TableID]map[ColID]map[string]map[QueryHash]struct{}
}
```

**Lookup on row change:**

```
For each (tableID, colID) tracked in cols for this table:
    Extract the column value from the changed row.
    Look up (tableID, colID, encode(value)) in args.
    Add all matching query hashes to the candidate set.
```

**Cost**: O(indexed_columns) per changed row for the map lookup (amortized O(1) per level). For the common case (one equality predicate), this is constant-time per changed row — vastly better than scanning all subscriptions.

**Data-structure note**: tier-1 is pure equality lookup. There is no predicate pattern today that requires ordered iteration over value keys, so a nested map is the simplest correct structure. Empty-map cleanup on Remove gives the same "entry disappears when no hashes remain" effect a B-tree range-delete would provide. SpacetimeDB's reference implementation uses a BTreeMap here but still accesses it equality-only.

**Example**: 10,000 clients subscribe to `messages WHERE channel_id = ?` with different channel IDs. Inserting a message in channel 42 looks up `(messages, channel_id, 42)` and finds only the subscriptions for that channel.

### 5.2 Tier 2: Join Edge Index

For join subscriptions with a filter on the joined table.

**Structure:**

```go
type JoinEdge struct {
    LHSTable   TableID
    RHSTable   TableID
    LHSJoinCol ColID
    RHSJoinCol ColID
    RHSFilterCol ColID  // the filtered column on the RHS
}

type JoinEdgeIndex struct {
    // edges[edge][encodedFilterValue] = set of query hashes.
    edges map[JoinEdge]map[string]map[QueryHash]struct{}
    // byTable[LHSTable][edge] = refcount, so EdgesForTable returns the
    // LHSTable-rooted edges without scanning the full edges map.
    byTable map[TableID]map[JoinEdge]int
}
```

**Data-structure note**: candidate collection iterates the edges whose LHSTable matches the changed table. SpacetimeDB's reference implementation serves this via a BTreeMap's prefix scan; Shunter serves it via the `byTable` denormalization. Both approaches preserve the same external contract — the ordered key is an implementation detail, not a requirement of the tier-2 semantics.

**Lookup on row change in LHS table:**

```
For each JoinEdge where LHSTable matches:
    Extract the join column value from the changed row.
    Look up the corresponding RHS row via index on RHSJoinCol.
    Extract the RHS filter column value.
    Look up (edge, rhs_value) → query hashes.
```

This prunes join subscriptions by checking whether the changed row could satisfy the join + filter condition.

### 5.3 Tier 3: Table Fallback

For subscriptions with no extractable equality filter — complex predicates, range-only predicates, or `AllRows`.

**Structure:**

```go
type TableIndex struct {
    // tables maps TableID → set of query hashes.
    tables map[TableID]map[QueryHash]struct{}
}
```

**Lookup**: Any change to a table triggers evaluation of all queries in `tables[tableID]`. This is the pessimistic fallback.

**Goal**: Minimize the number of subscriptions that fall into Tier 3. The predicate model (Section 3) is designed so that the most common subscription patterns (equality filters) land in Tier 1.

### 5.4 Index Placement Invariant

When a subscription is registered, for each table it reads from:

1. If the predicate has a `ColEq` on this table → place in **Value Index**.
2. Else if the predicate is a join with a filterable edge involving this table → place in **Join Edge Index**.
3. Else → place in **Table Fallback**.

A subscription touching two tables may appear in different tiers for each table.

---

## 6. Delta Computation — Incremental View Maintenance

> **Row-payload encoding.** `ProductValue` rows computed here are serialized to wire bytes by the protocol layer (SPEC-005) using BSATN as defined in SPEC-002 §3.3. The name "BSATN" is borrowed from SpacetimeDB and is non-standard; see the canonical disclaimer in **SPEC-002 §3.1**. SPEC-004 never touches BSATN bytes directly — it operates on decoded `ProductValue`.

### 6.1 Single-Table Subscriptions

For a subscription `V = filter(T)` on one table:

After a transaction inserts rows `dT(+)` and deletes rows `dT(-)` from table T:

```
delta_inserts = [row for row in dT(+) if filter(row)]
delta_deletes = [row for row in dT(-)  if filter(row)]
```

Apply the subscription's filter to the inserted rows → new rows entering the result set.
Apply the subscription's filter to the deleted rows → rows leaving the result set.

**No deduplication needed** — a single table cannot produce duplicate rows in a filtered scan.

### 6.2 Two-Table Join Subscriptions

For a subscription `V = T1 join T2` (with optional filter):

This is mathematically derived from incremental view maintenance. The key insight: the new view state `V'` can be expressed as `V + dV` where `dV` depends only on the changes `dT1` and `dT2`, not the entire tables.

Given:
- `dT1(+)` = rows inserted into T1
- `dT1(-)` = rows deleted from T1
- `dT2(+)` = rows inserted into T2
- `dT2(-)` = rows deleted from T2
- `T1'` = T1 after transaction (T1 + dT1(+) - dT1(-))
- `T2'` = T2 after transaction

The delta decomposes into **4 insert fragments** and **4 delete fragments**:

```
Insert fragments:
  I1: dT1(+) join T2'       — new T1 rows joining with current T2
  I2: T1'    join dT2(+)    — current T1 rows joining with new T2
  I3: dT1(+) join dT2(-)   — new T1 rows joining with deleted T2
  I4: dT1(-) join dT2(+)   — deleted T1 rows joining with new T2

Delete fragments:
  D1: dT1(-) join T2'       — deleted T1 rows that were in the join
  D2: T1'    join dT2(-)   — current T1 rows joining with deleted T2
  D3: dT1(+) join dT2(+)   — cancellation: both sides inserted
  D4: dT1(-) join dT2(-)   — cancellation: both sides deleted
```

**Why 4+4?** This comes from expanding `(T1 + dT1) join (T2 + dT2)` and collecting terms. Some fragments (I3, I4, D3, D4) appear in both the positive and negative expansion — they cancel against each other in the final result. The evaluation still produces them, and the deduplication step (Section 6.3) resolves the cancellations.

### 6.3 Bag-Semantic Deduplication for Joins

The 8 fragments for a join subscription may produce duplicate or contradictory rows (a row appearing in both insert and delete fragments). These must be reconciled using **bag semantics** — preserving multiplicity.

**Algorithm:**

```
insertCounts := map[ProductValue]int{}
deleteCounts := map[ProductValue]int{}

// Phase 1: Count inserts
for each row produced by insert fragments I1..I4:
    insertCounts[row]++

// Phase 2: Count deletes, canceling against inserts
for each row produced by delete fragments D1..D4:
    if insertCounts[row] > 0:
        insertCounts[row]--   // cancel: row in both insert and delete
    else:
        deleteCounts[row]++

// Phase 3: Materialize
inserts = []ProductValue{}
for row, n := range insertCounts where n > 0:
    append row n times to inserts

deletes = []ProductValue{}
for row, n := range deleteCounts where n > 0:
    append row n times to deletes
```

**Why bag semantics?** For a semijoin `T1 semijoin T2`, a client needs to know the multiplicity — how many T2 rows each T1 row joins with. Deduplicating to set semantics would lose this information and cause the client's view to diverge from the true query result.

### 6.4 Execution Against Delta State

Each fragment references either the **base table** (committed state after the transaction) or the **delta** (just the inserted or deleted rows from this transaction).

The evaluator needs a data source that can serve both:
- Full table scans and index scans on committed state
- Scans over just the delta rows (inserts or deletes), also supporting index lookups

**Delta indexes**: When a transaction commits, the evaluator builds temporary indexes over the delta rows for each indexed column. This allows fragments like `dT1(+) join T2'` to use index lookups on the delta side, not just full scans.

```go
// DeltaView wraps committed state + transaction deltas.
type DeltaView struct {
    // Committed state (read-only snapshot after this transaction).
    committed CommittedReadView

    // Delta rows from this transaction, per table.
    inserts map[TableID][]ProductValue
    deletes map[TableID][]ProductValue

    // Scratch indexes built over delta rows for efficient equality lookup.
    deltaIdx DeltaIndexes
}

// DeltaIndexes provides per-transaction scratch index scans over delta rows.
// These are not real store indexes — they are ephemeral equality maps built
// just for the columns referenced by the current set of active subscriptions.
type DeltaIndexes struct {
    // insertIdx[tableID][colID][encodedValue] = positions into inserts[tableID].
    insertIdx map[TableID]map[ColID]map[string][]int
    // deleteIdx mirrors the same shape for deletes.
    deleteIdx map[TableID]map[ColID]map[string][]int
}
```

Delta indexes are built eagerly when the DeltaView is constructed (once per transaction evaluation, not per subscription). Only columns referenced by at least one active subscription are indexed.

**Keying rationale**: `ColID` is the natural coordinate for delta-side scratch indexes because they have no identity separate from the transaction — there is no persistent `IndexID` to name them. Committed-side access on the other hand still uses the real store `IndexID` (see §10.3 `CommittedReadView.IndexSeek`). This is a deliberate divergence from SpacetimeDB's `DeltaStore` trait, which unifies delta and committed lookups under a single `IndexId`; Shunter trades that symmetry for a simpler delta view that does not depend on `SchemaRegistry` / `IndexResolver` at construction time.

---

## 7. Evaluation Loop

This is the main algorithm that runs after every committed transaction.

```go
// CommitFanout is the complete per-connection delta output from one EvalAndBroadcast call.
// Keyed by ConnectionID; contains all affected subscriptions per connection.
type CommitFanout map[ConnectionID][]SubscriptionUpdate
```

### 7.1 Trigger

The transaction executor (SPEC-003) calls the evaluator synchronously after committing a transaction. The evaluator runs on the **same goroutine** as the executor — no lock contention, no race conditions. The evaluator receives `*Changeset` (pointer). It must not mutate the changeset.

### 7.2 Algorithm

```
EvalTransaction(changeset *Changeset) → CommitFanout:

  1. If no active subscriptions: return immediately.

  2. Build DeltaView from changeset + committed state.
     Build delta indexes for columns referenced by active subscriptions.

  3. Collect candidate queries:
     candidates := HashSet<QueryHash>{}

     For each table T modified in changeset:
       For each changed row R in T (inserts and deletes):
         // Tier 1: Value Index
         For each (colID) tracked for T in ValueIndex:
           value := R.Column(colID)
           candidates.AddAll(ValueIndex.Lookup(T, colID, value))

         // Tier 2: Join Edge Index
         For each JoinEdge involving T:
           joinValue := R.Column(edge.LHSJoinCol)
           rhsRow := committed.IndexLookup(edge.RHSTable, edge.RHSJoinCol, joinValue)
           if rhsRow != nil:
             filterValue := rhsRow.Column(edge.RHSFilterCol)
             candidates.AddAll(JoinEdgeIndex.Lookup(edge, filterValue))

       // Tier 3: Table Fallback
       candidates.AddAll(TableFallback.Lookup(T))

  4. Evaluate each candidate query:
     fanout := CommitFanout{}

     For each queryHash in candidates:
       query := subscriptionManager.GetQuery(queryHash)

       // Compute delta per table in query
       For each tc in changeset.Tables matching query:
         deltaInserts, deltaDeletes := evalDelta(deltaView, query, tc)
         if len(deltaInserts) == 0 && len(deltaDeletes) == 0: continue

         // Fan out to all subscribers
         For each client subscribed to queryHash:
           fanout[client.ConnID] = append(fanout[client.ConnID], SubscriptionUpdate{
             SubscriptionID: client.SubID,
             TableID:        tc.TableID,
             TableName:      tc.TableName,
             Inserts:        deltaInserts,
             Deletes:        deltaDeletes,
           })

  5. Send FanOutMessage{TxDurable: durableNotify, Fanout: fanout} to FanOutWorker.inbox.
```

### 7.3 Row-Level vs Table-Level Candidate Collection

**Step 3 above checks every changed row** against Tier 1 and Tier 2 indexes. For transactions that modify many rows in the same table, this could be expensive.

**Optimization**: For Tier 1, batch the lookup. Instead of checking each row individually, collect all distinct values for each tracked column from the changeset, then look up each distinct value once.

```
For each (colID) tracked for T in ValueIndex:
    distinctValues := set of unique values for colID across all changed rows
    for value in distinctValues:
        candidates.AddAll(ValueIndex.Lookup(T, colID, value))
```

This reduces lookups from O(changed_rows) to O(distinct_values_per_column), which matters for bulk inserts with repeated values.

### 7.4 Memoized Encoding

When multiple clients subscribe to the same query (same query hash), the delta is computed once and encoded once per wire format. The encoded bytes are shared across all recipients:

```go
type memoizedResult struct {
    binary []byte  // nil until first binary client needs it
    json   []byte  // nil until first JSON client needs it
}
```

This avoids redundant work proportional to subscriber count.

---

## 8. Fan-Out and Delivery

### 8.1 Decoupled Delivery

The evaluation loop (Section 7) produces a `CommitFanout` synchronously on the executor goroutine. The executor waits only until ownership of those deltas has been handed to the fan-out subsystem via `FanOutMessage`. Actual websocket sends happen on a separate fan-out goroutine so slow clients do not block executor ordering.

```go
type FanOutWorker struct {
    // Receives computed deltas from the executor.
    inbox chan FanOutMessage

    // Narrow fan-out delivery seam backed by the protocol layer.
    // A protocol adapter may wrap SPEC-005's ClientSender to satisfy this.
    sender FanOutSender

    // Per-connection delivery policy needed by the fan-out worker.
    confirmedReads map[ConnectionID]bool
}

// FanOutSender is the subscription-side delivery contract.
// SPEC-005 owns concrete websocket/outbound-buffer behavior; SPEC-004
// consumes that behavior through this narrow interface.
type FanOutSender interface {
    SendTransactionUpdate(connID ConnectionID, txID TxID, updates []SubscriptionUpdate) error
    SendReducerResult(connID ConnectionID, result *ReducerCallResult) error
    SendSubscriptionError(connID ConnectionID, subID SubscriptionID, message string) error
}

type FanOutMessage struct {
    // TxDurable becomes ready when the transaction is durable.
    // Fan-out waits for readiness before sending if the client requires confirmed reads.
    // This readiness channel is supplied by the executor's post-commit pipeline and is
    // backed by the durability subsystem; it is not itself the exported SPEC-002 handle.
    TxDurable <-chan TxID

    // Fanout contains per-connection subscription updates for this commit.
    Fanout CommitFanout

    // Errors contains per-connection subscription-evaluation failures that
    // must be delivered before normal updates for the same batch.
    Errors map[ConnectionID][]SubscriptionError

    // Optional caller metadata for reducer-originated commits. When present, the
    // caller's per-connection update is routed into ReducerCallResult instead of
    // being emitted as a standalone TransactionUpdate.
    CallerConnID *ConnectionID
    CallerResult *ReducerCallResult
}
```

### 8.2 Fan-Out Algorithm

```
For each FanOutMessage received:
  0. Deliver any queued SubscriptionError entries in msg.Errors.
  1. Wait for TxDurable (if confirmed reads required by any client).
  2. Read the pre-grouped CommitFanout entries keyed by ConnectionID.
     A connection may have multiple subscriptions affected by one transaction.
  3. For each connection:
     Build a TransactionUpdate message containing `Updates []SubscriptionUpdate`
     for that connection only. Preserve one update entry per affected subscription;
     do not merge entries across distinct SubscriptionIDs.
     Send via the protocol layer.
  4. Special case: if this commit came from `CallReducer`, the caller connection's
     update slice is routed into `ReducerCallResult.transaction_update` instead of
     also receiving a standalone `TransactionUpdate` for the same `tx_id`.
```

### 8.3 Aggregation

Multiple subscriptions for the same connection may produce deltas for the same table in one transaction. These are packaged together in one `TransactionUpdate`, while preserving one `SubscriptionUpdate` entry per subscription:

```
Per-connection packaging:
  Start from CommitFanout[connID] = []SubscriptionUpdate.
  Preserve SubscriptionUpdate boundaries so each entry retains its SubscriptionID.
  A single TransactionUpdate may therefore contain multiple entries for the same table
  when they belong to different subscriptions.
```

### 8.4 Client Backpressure

**Problem**: If a client's network connection is slow, the fan-out goroutine must not block the executor.

**Design**: Each client connection has a bounded outbound buffer (SPEC-005). The fan-out goroutine attempts a non-blocking send:
- **Success**: Message queued for the client's websocket writer.
- **Buffer full**: Client is marked as **lagging**. Options (configurable per deployment):
  - **Drop updates**: Skip this delta. Client's view becomes stale. On next successful send, include a "resync required" flag.
  - **Disconnect**: Close the client connection. Client must reconnect and re-subscribe (receiving fresh initial state).

**Recommendation**: Default to disconnect-on-lag for v1. It is simpler to implement correctly and forces clients to handle reconnection, which they must handle anyway (network failures). Drop-with-resync is a v2 optimization.

### 8.5 Dropped Client Cleanup

When a client is disconnected (network failure, lag disconnect, or explicit close):
1. The fan-out worker marks the client as dropped and signals the `ConnectionID` on the shared channel returned by `SubscriptionManager.DroppedClients()`.
2. The manager's evaluation-error path may write to the same channel; the executor still drains only one channel after each post-commit pipeline step.
3. This two-phase approach avoids the fan-out goroutine needing write access to the subscription manager.

---

## 9. Performance Constraints

### 9.1 Targets

| Metric | Target | Rationale |
|--------|--------|-----------|
| Evaluation latency (single-table, 1K subs) | < 1 ms | Pruning reduces to ~O(10) evals; each is a filter over a small changeset |
| Evaluation latency (single-table, 10K subs) | < 5 ms | Same pruning benefit; linear in affected subs, not total |
| Join fragment evaluation | < 10 ms per subscription | 8 fragments, each involving index lookups on delta + committed state |
| Fan-out channel depth | Bounded, configurable | Default: 64 messages. Prevents unbounded memory growth |
| Delta index construction | < 1 ms for typical transactions | Proportional to (changed_rows * indexed_columns) |

### 9.2 Allocation Discipline

The evaluation loop is the hot path. Minimize allocations:

1. **Buffer pooling**: Reuse `[]byte` buffers for delta encoding via `sync.Pool`. Default buffer size: 4 KiB. Pool returns oversized buffers to the runtime.
2. **Slice reuse**: The `DeltaView.inserts` and `DeltaView.deletes` slices should be pooled and reused across transactions.
3. **Map reuse**: The candidate `HashSet` and the dedup maps in bag-semantic dedup should be allocated once and cleared (not reallocated) per transaction.
4. **Avoid `interface{}` on hot path**: Row comparisons in bag-semantic dedup should use direct byte comparison of encoded rows, not Go equality on interface values.

### 9.3 Scaling Characteristics

| Dimension | Scaling behavior |
|-----------|-----------------|
| Total subscriptions | O(1) per eval via pruning (for equality predicates). O(n) for Tier 3 fallback subscriptions. |
| Affected subscriptions | O(k) where k = subscriptions whose predicates match changed values. Linear and unavoidable. |
| Changed rows per tx | O(r * c) for delta index construction (r = rows, c = indexed columns). O(r) for candidate collection with batching optimization. |
| Join fragments | O(8) per join subscription (fixed). Each fragment cost depends on index availability. |
| Clients per query | O(1) for computation. O(n) for delivery (fan-out). Encoding is O(1) via memoization. |

---

## 10. Interfaces to Other Subsystems

### 10.1 From Transaction Executor (SPEC-003)

The subscription subsystem is called from executor commands. It must support both registration-time operations and post-commit evaluation:

```go
type SubscriptionManager interface {
    Register(req SubscriptionRegisterRequest, view CommittedReadView) (SubscriptionRegisterResult, error)
    Unregister(connID ConnectionID, subscriptionID SubscriptionID) error
    DisconnectClient(connID ConnectionID) error
    EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView, meta PostCommitMeta)
    DroppedClients() <-chan ConnectionID   // non-blocking; executor drains after each commit
}

// PostCommitMeta carries executor-owned delivery metadata into the
// subscription fan-out seam. Zero value means ordinary non-caller,
// fast-read delivery.
type PostCommitMeta struct {
    TxDurable    <-chan TxID
    CallerConnID *ConnectionID
    CallerResult *ReducerCallResult
}

// TxDurable contract:
// - Non-nil for every post-commit invocation the executor makes, regardless of
//   whether Fanout is empty. The executor allocates the channel from
//   DurabilityHandle.WaitUntilDurable(txID) (SPEC-002 §4.2 / SPEC-003 §7)
//   before calling EvalAndBroadcast. An empty-fanout transaction may still
//   need durability gating for a caller-reducer's ReducerCallResult.
// - TxDurable == nil is reserved for the zero-value PostCommitMeta used by
//   tests that bypass the executor; production code paths never observe it.

// Changeset and TableChangeset are defined in SPEC-001 §6.1.
// The evaluator receives *Changeset from the executor after each commit.
// Relevant fields:
//   cs.TxID          — transaction identifier
//   cs.Tables        — map[TableID]*TableChangeset; only tables with changes are present
//   tc.TableID       — which table changed
//   tc.TableName     — table name for wire encoding
//   tc.Inserts       — []ProductValue; rows whose net effect is "now present"
//   tc.Deletes       — []ProductValue; rows whose net effect is "now absent"
```

The committed read view is a stable read-only handle supplied by SPEC-001 for the duration of registration or evaluation work. Registration-sensitive reads are performed inside the executor command that also mutates subscription indexes. Callers must honor the `CommittedReadView` lifetime contract from SPEC-001: materialize the needed rows promptly, close the view before blocking work, and never hold it across network I/O or channel waits.

### 10.2 To Client Protocol (SPEC-005)

The evaluator produces per-client deltas:

```go
// SubscriptionUpdate is the per-subscription component of a transaction delta.
// One per subscription affected by a commit.
type SubscriptionUpdate struct {
    SubscriptionID SubscriptionID  // which subscription this update is for
    TableID        TableID
    TableName      string
    Inserts        []ProductValue
    Deletes        []ProductValue
}

// TransactionUpdate is sent to a client after evaluation.
type TransactionUpdate struct {
    TxID    TxID
    Updates []SubscriptionUpdate  // one entry per affected subscription
}

// SubscriptionError is the client-facing evaluation-failure payload.
// The wire projection (SPEC-005 §8.4) carries only `SubscriptionID`
// and `Message`; `QueryHash` and `Predicate` are retained in the Go
// value for server-side logging and are not sent to clients.
type SubscriptionError struct {
    SubscriptionID SubscriptionID
    QueryHash      QueryHash
    Predicate      string
    Message        string
}

// ReducerCallResult is forward-declared here to document the caller-diversion
// seam. SPEC-005 §8.7 owns the concrete wire shape.
type ReducerCallResult struct {
    RequestID         uint32
    Status            uint8
    TxID              TxID
    Error             string
    Energy            uint64
    TransactionUpdate []SubscriptionUpdate
}
```

Encoding of `[]ProductValue` into the wire `RowList` format and actual enqueueing to per-client outbound buffers happen in the protocol layer (SPEC-005 §3.4 / delivery contract), not in the evaluator.
The fan-out worker talks to the protocol layer through the narrow `FanOutSender` seam described in §8.1; protocol satisfies that seam via a `FanOutSenderAdapter` over its broader `ClientSender` surface (SPEC-005 §13). The adapter converts subscription-domain values (`[]SubscriptionUpdate`, `*ReducerCallResult`, raw message strings) into protocol-wire structs before calling `ClientSender`; `SendSubscriptionError` is routed through `ClientSender.Send(connID, msg)` with a wire `SubscriptionError`. Delivery errors are mapped back to `ErrSendBufferFull` / `ErrSendConnGone` subscription-layer sentinels so the fan-out worker can react without importing protocol types.

### 10.3 From In-Memory Store (SPEC-001)

The evaluator needs read-only access to committed state. These are provided by `CommittedReadView` (SPEC-001 §7.2):
- `TableScan(tableID TableID) RowIterator`
- `IndexScan(tableID TableID, indexID IndexID, value Value) RowIterator`
- `IndexRange(tableID TableID, indexID IndexID, lower, upper Bound) RowIterator`
- `RowCount(tableID TableID) uint64`

`RowIterator` is `iter.Seq2[RowID, ProductValue]` (SPEC-001 §5.4). `Row` is replaced by `ProductValue` throughout.

### 10.4 From Schema (SPEC-006)

Predicate validation (`ValidatePredicate`, §3.3 / Story 1.2) and Tier-2 candidate collection (`PruningIndexes.CollectCandidatesForTable`, §5 / Story 2.4) consume schema-side surfaces declared in SPEC-006 §7:

- `SchemaLookup` — narrow read-only methods (`Table`, `TableByName`, `TableExists`, `TableName`, `ColumnExists`, `ColumnType`, `HasIndex`). Used by `ValidatePredicate`. The `subscription` package may declare its own narrower local interface for testing, but the canonical type is the SPEC-006 declaration; `*SchemaRegistry` satisfies it directly.
- `IndexResolver` — single method `IndexIDForColumn(table TableID, col ColID) (IndexID, bool)`. Supplied to `Manager` at construction (`NewManager(schema SchemaLookup, resolver IndexResolver, ...)`); `*SchemaRegistry` satisfies it. When the resolver returns `false` for a column that validation confirmed has an index, `Register()` returns `ErrJoinIndexUnresolved`.

Both interfaces are produced by SPEC-006 `Build()` and are immutable for the engine's lifetime (SPEC-006 §5.1 freeze).

---

## 11. Error Handling

### 11.1 Evaluation Errors

If delta computation fails for a subscription (e.g., corrupted index, type mismatch):
1. Log the error with the subscription's query hash and SQL/predicate representation.
2. Emit the error through the `FanOutSender.SendSubscriptionError` seam (§8.1) for each affected client. The protocol adapter (SPEC-005 §13) translates this into a wire-format `SubscriptionError` delivered via `ClientSender.Send`; the wire `request_id` is `0` for these spontaneous failures (see SPEC-005 §8.4 `request_id` semantics).
3. Unregister the affected subscription(s) / query state without disconnecting unrelated subscriptions on the same connection.
4. Do **not** abort the evaluation loop — other subscriptions are unaffected.

### 11.2 Registration Errors

Returned to the caller synchronously:
- Predicate references nonexistent table or column.
- Join predicate references column without an index.
- Predicate involves >2 tables.
- Row limit exceeded on initial snapshot (configurable).

### 11.3 Invariant Violations

The following are bugs, not recoverable errors:
- Delta dedup produces negative counts (impossible if IVM algebra is correct).
- A query hash maps to a nonexistent query state.
- A client appears in the subscriber list of a query but not in the client map.

These should panic with a diagnostic message.

---

## 12. Open Design Decisions

### 12.1 Subscription Query Language

**Options:**

| Approach | Pruning support | Expressiveness | Complexity |
|----------|----------------|---------------|------------|
| Go predicate builder (recommended) | Full — predicates are inspectable structs | Medium — covers equality, range, joins | Low |
| SQL subset parser | Full — parse tree is inspectable | High — familiar syntax | Medium — requires parser |
| Raw Go functions | None — opaque | Unlimited | Lowest code, worst performance |

**Recommendation**: Go predicate builder for v1. A SQL subset can be layered on top later as syntactic sugar that compiles to the same predicate structs. Raw Go functions should never be the primary path.

Example API:

```go
// Subscribe to messages in a specific channel.
sub := shunter.Where(schema.Messages, shunter.Eq("channel_id", channelID))

// Subscribe to a join: players with their guild info.
sub := shunter.Join(schema.Players, schema.Guilds, "guild_id", "id",
    shunter.Eq("guild_id", myGuildID))
```

### 12.2 Update Granularity

**Row-level** (recommended for v1): Deltas contain full rows for inserts and deletes. An update is represented as a delete of the old row + insert of the new row.

**Column-level** (future): Deltas contain only changed columns. Reduces bandwidth for wide tables with frequent partial updates.

Row-level is simpler, correct, and sufficient for v1. Column-level can be added as a wire-format optimization without changing the evaluation algorithm.

### 12.3 Confirmed Reads

Should delta delivery wait for the transaction to be durable (fsync'd to commit log)?

- **Yes (confirmed reads)**: Client only sees data that will survive a crash. Higher latency.
- **No (fast reads)**: Client sees data immediately after in-memory commit. Lower latency, but client could see data that is lost on crash.

**Recommendation**: Make this configurable per client. Default to fast reads. Clients that need strong consistency opt into confirmed reads. The fan-out worker supports this via executor-supplied durability-ready metadata — it either waits or skips.

---

## 13. Verification

### 13.1 Unit Tests

1. **Single-table delta**: Insert/delete rows, verify delta matches filter application.
2. **Join delta correctness**: For known T1, T2, dT1, dT2 — verify the 4+4 fragment evaluation produces the mathematically correct delta (compare against full re-evaluation of the join).
3. **Bag semantics**: Verify that join deltas preserve multiplicity. Construct a case where a row joins with 3 RHS rows, delete 1 RHS row, verify delta shows 1 delete (not 0 or 3).
4. **Pruning correctness**: Register subscriptions with known predicates. Apply changesets. Verify that pruned subscriptions produce the same results as full evaluation.
5. **Deduplication**: Register the same predicate from multiple clients. Verify evaluation runs once, all clients receive the same delta.

### 13.2 Property Tests

1. **IVM invariant**: For any sequence of transactions, the accumulated deltas applied to the initial snapshot must equal the result of re-evaluating the full query from scratch.
2. **Pruning safety**: Evaluation with pruning must produce identical results to evaluation without pruning (all subscriptions evaluated).
3. **Registration/deregistration symmetry**: After registering and then deregistering a subscription, all indexes must return to their prior state.

### 13.3 Benchmark Targets

1. **10,000 equality subscriptions, 1 table change**: Evaluate in < 2 ms.
2. **100 join subscriptions, 10 changed rows**: Evaluate in < 20 ms.
3. **Subscription registration/deregistration**: < 100 us per operation.
4. **Fan-out to 1,000 clients (same query)**: < 1 ms (encode once, replicate pointers).
