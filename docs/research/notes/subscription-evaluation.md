# Subscription Evaluation — Deep Research Note

Research into SpacetimeDB's subscription evaluation subsystem (`crates/subscription/`, `crates/core/src/subscription/`, supporting crates). This note extracts algorithms and architectural patterns for spec derivation.

---

## 1. Conceptual Model

A **subscription** is a standing SQL query. When a transaction commits changes that affect the query's result set, the subscriber receives a **delta**: rows added to the result set (inserts) and rows removed from it (deletes).

The system must answer one question efficiently after every commit: **which subscriptions are affected by this transaction's changes, and what are the deltas?**

---

## 2. Plan Compilation Pipeline

### 2.1 Entry Point

```
SQL text
  → parse + type-check
  → resolve Row-Level Security filters (may produce multiple plan variants)
  → compile to physical plan (ProjectPlan)
  → optimize (15-pass rewrite pipeline)
  → compile delta fragments (SubscriptionPlan)
```

### 2.2 Physical Plan Operators

The physical plan is a tree of:
- **TableScan** — full scan of a table, with optional `delta: Option<Delta>` flag
- **IxScan** — index-based scan (equality or range), also has `delta` flag
- **IxJoin** — index join (left-deep), `rhs_delta` flag on right side
- **HashJoin** — hash join
- **NLJoin** — nested loop join
- **Filter** — tuple-at-a-time predicate

The `Delta` enum is `{Inserts, Deletes}` — when set on a scan node, the executor reads from the transaction's delta rows instead of the base table.

### 2.3 Optimization Pipeline (Strict Order)

1. View expansion (add implicit sender filters)
2. Canonicalization (literals to RHS, flatten AND/OR)
3. PushConstAnd — push down constant AND predicates
4. PushConstEq — push down constant equality predicates
5. ReorderDeltaJoinRhs — reorder delta joins
6. PullFilterAboveHashJoin — extract filters from hash joins with deltas
7. IxScanEq3Col — 3-column index scan matching
8. IxScanEq2Col — 2-column index scan matching
9. IxScanEq — 1-column equality index scan
10. IxScanAnd — multi-column AND index scan
11. ReorderHashJoin — reorder hash join operands
12. HashToIxJoin — convert hash join to index join where possible
13. UniqueIxJoinRule — mark unique index joins
14. UniqueHashJoinRule — mark unique hash joins
15. Introduce semijoins
16. ComputePositions (must be last)

### 2.4 Delta Fragment Generation — The Core Algebra

This is the heart of subscription evaluation. It implements **incremental view maintenance (IVM)**.

#### Single-Table Subscriptions

For `V = sigma(R)` (select with filter on one table):
- 1 insert fragment: scan `Delta::Inserts` of R through filter
- 1 delete fragment: scan `Delta::Deletes` of R through filter

Simple — apply the filter to the changed rows.

#### Two-Table Join Subscriptions

For `V = R join S`, the IVM derivation:

```
V  = R x S                           (current state)
V' = R' x S'                         (next state)
   = (R + dR) x (S + dS)             (expand with deltas)
   = RS + R(dS) + (dR)S + (dR)(dS)

delta_V = R'(dS) + (dR)S' + (dR)(dS) - (self-eliminating terms)

Splitting into positive and negative:
  dR = dR(+) - dR(-)
  dS = dS(+) - dS(-)

Insert fragments (4):
  1. dR(+) join S'     — new R rows joining with current+new S
  2. R' join dS(+)     — current+new R rows joining with new S
  3. dR(+) join dS(-)  — new R rows joining with deleted S
  4. dR(-) join dS(+)  — deleted R rows joining with new S

Delete fragments (4):
  1. dR(-) join S'     — deleted R rows that were in join
  2. R' join dS(-)     — current R rows joining with deleted S
  3. dR(+) join dS(+)  — cancel: both sides inserted (appears in both + and -)
  4. dR(-) join dS(-)  — cancel: both sides deleted
```

**Implementation**: The `new_plan()` helper clones the original physical plan, walks all `TableScan` nodes, sets `delta` flags on matching tables, re-optimizes, and converts to a pipelined executor.

**Constraint**: Only 1-table and 2-table subscriptions supported. >2 tables rejected at compilation.

### 2.5 SubscriptionPlan Structure

```
SubscriptionPlan {
    return_id: TableId,          // table the subscription returns
    return_name: TableName,
    table_ids: Vec<TableId>,     // all tables read by this subscription
    fragments: Fragments {
        insert_plans: Vec<PipelinedProject>,  // 1 or 4
        delete_plans: Vec<PipelinedProject>,  // 1 or 4
    },
    plan_opt: ProjectPlan,       // original optimized plan (no delta flags)
}
```

### 2.6 JoinEdge Structure

For certain join patterns, a `JoinEdge` is extracted for runtime pruning:

```
JoinEdge {
    lhs_table: TableId,
    rhs_table: TableId,
    lhs_join_col: ColId,
    rhs_join_col: ColId,
    rhs_col: ColId,          // column filtered on RHS
}
```

Requirements: two-table join, unique index on RHS join column, no self-joins, single-column index lookup.

---

## 3. Runtime Subscription Management

### 3.1 SubscriptionManager — Central Coordinator

```
SubscriptionManager {
    clients: HashMap<ClientId, ClientInfo>,
    queries: HashMap<QueryHash, QueryState>,

    // Three pruning indexes (a query appears in exactly one):
    tables:      IntMap<TableId, HashSet<QueryHash>>,        // no filter → must eval for any change
    search_args: SearchArguments,                             // equality filter → eval only for matching values
    join_edges:  JoinEdges,                                   // join with filter → eval only for matching join keys

    indexes: QueriedTableIndexIds,    // ref-counted index tracking
    send_worker_queue: BroadcastQueue,
}
```

### 3.2 Query Identification

`QueryHash` = blake3 hash of SQL text. For parameterized queries (with RLS/views), identity is mixed in:

```
Simple:       blake3(sql_bytes)
Parameterized: blake3(sql_bytes || identity_bytes)
```

32-byte hash, used as key everywhere.

### 3.3 Three-Tier Pruning

This is the key optimization that makes fan-out tractable with many subscriptions.

**Tier 1 — Table Index** (`tables`): Maps `TableId → Set<QueryHash>`. If a query has no equality filter on a table, any change to that table requires evaluating the query. This is the fallback for complex predicates.

**Tier 2 — SearchArguments** (`search_args`): For `WHERE col = <literal>` subscriptions. Structure:

```
cols: HashMap<TableId, HashSet<ColId>>
args: BTreeMap<(TableId, ColId, AlgebraicValue), HashSet<QueryHash>>
```

On row change: extract field values from the changed row, look up `(table_id, col_id, value)` in the BTreeMap. Only matching queries are evaluated. This turns O(subscriptions) into O(log(distinct_values)).

Example: 10,000 clients each subscribe to `SELECT * FROM messages WHERE channel_id = ?` with different channel IDs. A message insert in channel 42 only evaluates subscriptions for channel 42.

**Tier 3 — JoinEdges** (`join_edges`): For join subscriptions with a filter on the joined table.

```
edges: BTreeMap<JoinEdge, HashMap<AlgebraicValue, HashSet<QueryHash>>>
```

When a row changes in the LHS table, look up the join column value in the RHS table, then look up queries matching that RHS value. Pruning skips join subscriptions where the join condition can't possibly be satisfied by the changed row.

**Invariant**: Each (query, table) pair appears in exactly one of the three tiers.

### 3.4 Query Deduplication (Three Levels)

1. **Within a subscription call**: HashSet deduplicates queries in the same `subscribe(queries)` request.
2. **Within a client**: `subscription_ref_count` per (client, query_hash). Same query referenced by multiple subscription sets increments ref count without re-registering.
3. **Across all clients**: `queries: HashMap<QueryHash, QueryState>` is global. `eval_delta()` runs once per unique query, result replicated to all subscribers.

### 3.5 Registration Flow

```
add_subscription(client, queries):
  for each query:
    hash = query.hash()
    query_state = queries.entry(hash).or_insert(new QueryState)
    if query_state.has_no_subscribers():
      insert_query():  // update pruning indexes
        - extract table_ids, search_args, join_edges from query
        - update indexes ref counts
        - for each table: insert into search_args OR join_edges OR tables
    increment client ref count
    add client to query_state.subscriptions
```

### 3.6 Deregistration Flow

```
remove_subscription(client, query_id):
  for each query_hash in subscription:
    decrement client ref count
    if ref count == 0:
      remove client from query_state.subscriptions
      if query_state has no subscribers:
        remove_query_from_tables():  // reverse of insert_query
          - remove from join_edges, search_args, tables
          - decrement index ref counts
        delete from queries map
```

---

## 4. Delta Computation Algorithm

### 4.1 eval_delta()

Called once per affected query per transaction.

```
eval_delta(tx: DeltaTx, plan: SubscriptionPlan) -> Option<(inserts, deletes)>:

  if NOT join:
    // Simple path — no dedup needed, single-table can't produce duplicates
    for_each_insert(tx, plan.insert_plans) → push to inserts
    for_each_delete(tx, plan.delete_plans) → push to deletes

  if join:
    // Bag-semantic dedup via ref counting
    insert_counts: HashMap<Row, count>
    delete_counts: HashMap<Row, count>

    for_each_insert(tx, plan.insert_plans):
      insert_counts[row] += 1

    for_each_delete(tx, plan.delete_plans):
      if row in insert_counts AND count > 0:
        insert_counts[row] -= 1    // cancel out: row appears in both insert and delete fragments
      else:
        delete_counts[row] += 1

    // Materialize with multiplicity
    inserts = [row repeated n times for (row, n) in insert_counts if n > 0]
    deletes = [row repeated n times for (row, n) in delete_counts if n > 0]

  if both empty: return None
  return Some(inserts, deletes)
```

**Why bag semantics matter**: For semijoin `R semijoin S`, a client needs to know for each row in R how many rows it joins with in S. Removing duplicates would lose this information.

### 4.2 DeltaTx — Transaction Delta Wrapper

Wraps a read-only transaction reference + the transaction's mutation data (`TxData`). Implements two traits:

- **Datastore**: Delegates to base transaction for table scans and index scans on committed state.
- **DeltaStore**: Exposes transaction's inserts/deletes as scannable sources with built indexes.

Delta indexes are built eagerly at construction:

```
DeltaTableIndexes {
    inserts: HashMap<(TableId, IndexId), BTreeMap<AlgebraicValue, SmallVec<[usize; 1]>>>
    deletes: HashMap<(TableId, IndexId), BTreeMap<AlgebraicValue, SmallVec<[usize; 1]>>>
}
```

For each indexed column, a BTreeMap maps value → positions in the delta array. This allows index scans on delta rows (not just full scans).

---

## 5. Evaluation Loop (Post-Commit)

### 5.1 Main Flow

```
commit_and_broadcast_event(caller, event, tx):
  1. Commit tx, downgrade to read-only
  2. Acquire subscriptions READ lock
  3. Build DeltaTx from committed write data
  4. Call eval_updates_sequential():
     a. For each table with changes in this tx:
        - queries_for_table_update(table_update):
            Look up in search_args (by row values)
            Look up in join_edges (by join key values)
            Look up in tables (fallback)
            → union of candidate QueryHashes
        - Deduplicate across tables (HashSet)
     b. For each unique query:
        - For each plan fragment:
            eval_delta(tx, fragment) → Option<(inserts, deletes)>
        - Encode result once (memoized per format: BSATN or JSON)
        - Create ClientUpdate for each subscriber
     c. Package as ComputedQueries
     d. Send to SendWorker via BroadcastQueue (non-blocking)
  5. Release subscriptions lock
  6. Return to unblock main transaction thread
```

### 5.2 Memoized Encoding

When N clients subscribe to the same query, the delta is computed once and encoded once per wire format:

```
ops_bsatn: Option<(encoded_result, ...)> = None
ops_json:  Option<(encoded_result, ...)> = None

for each client subscribing to this query:
  match client.protocol:
    Binary → use or compute ops_bsatn (encode once, reuse)
    Text   → use or compute ops_json  (encode once, reuse)
```

---

## 6. Fan-Out Architecture

### 6.1 SendWorker

Runs in a separate async task. Decouples message aggregation and sending from the transaction commit path.

```
SendWorker {
    rx: mpsc::UnboundedReceiver<SendWorkerMessage>,
    clients: HashMap<ClientId, SendWorkerClient>,
    // Reusable aggregation maps (cleared after each broadcast):
    table_updates_client_id_table_id: HashMap<(ClientId, TableId), TableUpdate>,
    table_updates_client_id: HashMap<ClientId, DatabaseUpdate>,
}
```

### 6.2 Message Types

```
SendWorkerMessage:
  | Broadcast { tx_offset, queries: ComputedQueries }   // delta fan-out
  | SendMessage { recipient, tx_offset?, message }       // direct message (subscribe/unsubscribe response)
  | AddClient { client_id, dropped_flag, outbound_ref }
  | RemoveClient(client_id)
```

### 6.3 Broadcast Flow

```
receive Broadcast { tx_offset, queries }:
  1. Await tx_offset (blocks until tx is durable — only if confirmed_reads enabled)
  2. Aggregate updates by (client_id, table_id) → merge same-table updates
  3. Aggregate by client_id → build per-client DatabaseUpdate
  4. Special handling: reducer caller gets TransactionUpdateMessage with event metadata
  5. Non-callers get plain update (no event metadata in V10+)
  6. Send each client's message to their websocket worker
```

### 6.4 Client Lifecycle in SendWorker

- **AddClient**: Registers client with dropped flag and outbound reference
- **RemoveClient**: Removes from tracking map
- **Dropped detection**: `dropped: Arc<AtomicBool>` set by SendWorker on error; checked before sending
- **Cancelled detection**: Checks websocket connection status before sending
- **Cleanup**: `remove_dropped_clients()` called periodically on SubscriptionManager to clean up pruning indexes

---

## 7. Wire Format

### 7.1 BSATN (Binary)

Rows serialized in BSATN format into a `BytesMut` buffer. The builder tracks a **size hint**:

- **FixedSize(n)**: All rows are exactly n bytes. Client decodes by dividing total bytes by row size. Minimal overhead.
- **RowOffsets(vec)**: Variable-size rows. Stores byte offset of each row boundary for random access / parallel decode.

The builder auto-detects: starts optimistic (FixedSize), downgrades to RowOffsets if any row differs in size.

**Compression**: Applied when uncompressed size > 1 KiB. Options: Brotli (level 1, fastest) or gzip (fast). Decision per-message.

### 7.2 JSON (Text)

Each row serialized individually to a JSON string, stored in `Vec<ByteString>`. No compression. No pooling.

### 7.3 Message Structure

```
TransactionUpdate {
    tables: [
        TableUpdate {
            table_id, table_name,
            update: QueryUpdate {
                inserts: BsatnRowList | JsonRows,
                deletes: BsatnRowList | JsonRows,
            }
        }, ...
    ],
    transaction_update_id,
}
```

### 7.4 Buffer Pooling

`BsatnRowListBuilderPool`: Object pool for `BytesMut` buffers.

- Default buffer capacity: 4 KiB
- Max pool size: 1024 buffers (4 MiB total)
- Take: acquire from pool (clear if reused) or allocate new
- Put: return to pool only when ref count = 1 (last client releases shared buffer)
- JSON has no pooling (fresh Vec per use)

---

## 8. Protocol Versioning (V1 vs V2)

| Aspect | V1 | V2 |
|--------|----|----|
| Subscribe | SubscribeSingle or SubscribeMulti | SubscribeV2 with query_set_id |
| Event tables | Not supported | Supported |
| Unsubscribe | Always sends dropped rows | Configurable via UnsubscribeFlags |
| Message format | SerializableMessage (SubscriptionMessage, TransactionUpdateMessage) | ws_v2::ServerMessage (SubscribeApplied, TransactionUpdate, ReducerResult) |
| Query grouping | Flat list of queries | Grouped by query_set_id |
| Reducer result | Event always included (pre-V10) | Event only sent to caller (V10+) |

---

## 9. Initial State Delivery

When a client first subscribes:

```
add_subscription(client, queries):
  1. Compile/cache query plans
  2. Begin transaction (Workload::Subscribe)
  3. Materialize views if needed
  4. Downgrade to read-only tx
  5. Execute query plan against full table (not delta) → snapshot of matching rows
  6. Enforce row limit (reject if too many rows)
  7. Add to SubscriptionManager (write lock, brief)
  8. Send initial snapshot as "inserts" via BroadcastQueue
```

The initial snapshot is sent through the same SendWorker queue as deltas, with a tx_offset, ensuring correct ordering with concurrent commits.

---

## 10. Error Handling

- **Evaluation errors**: Non-fatal. Error logged, client receives SubscriptionError message, client marked as `dropped`. Subscription remains registered but client will be cleaned up.
- **Compilation errors**: Fatal for that subscription. Returned to caller, subscription not registered.
- **Dropped clients**: `dropped: Arc<AtomicBool>` flag checked before sending. Periodic cleanup removes subscriptions for dropped clients.
- **V2 failures**: Failed subscriptions collected during eval, removed from SubscriptionManager after eval completes (separate write lock acquisition).

---

## 11. Confirmed Reads

Optional per-client feature. When enabled:

- `TransactionOffset = oneshot::Receiver<TxOffset>` attached to each broadcast
- SendWorker awaits the offset before sending (blocks until tx is durable)
- Guarantees client only sees committed+durable transactions

When disabled: SendWorker sends immediately after eval, without waiting for durability.

---

## 12. Key Design Decisions and Tradeoffs

### What SpacetimeDB Does Well

1. **Three-tier pruning** (search_args, join_edges, tables) turns subscription eval from O(all_subscriptions) to O(affected_subscriptions) for the common case of equality-predicate subscriptions.

2. **Query deduplication** across clients means 10,000 clients with the same query = 1 eval + 10,000 sends (not 10,000 evals).

3. **Decoupled send worker** keeps the transaction commit path fast. Eval happens synchronously (under read lock), but aggregation and sending happen async.

4. **Bag-semantic delta computation** is correct for joins — preserving multiplicity is mathematically required for IVM.

5. **Memoized encoding** avoids re-serializing the same delta for each client.

### Complexity Budget Observations

1. The 4+4 join fragment approach is correct but expensive. Each join subscription generates 8 plan fragments that must all be evaluated. This is inherent to IVM for two-table joins.

2. The BTreeMap in SearchArguments uses composite keys `(TableId, ColId, AlgebraicValue)`. Range queries on this are efficient for single-table lookups but the key comparison includes the AlgebraicValue, which may be expensive for large values.

3. Delta indexes (BTreeMap per indexed column on delta rows) are built eagerly at DeltaTx construction. For transactions with many changed rows across many indexed columns, this construction cost could be significant.

4. The unbounded MPSC channel to SendWorker has no backpressure. If clients are slow, memory grows without bound. The only mitigation is the `dropped` flag detection.

### Differences for Shunter

1. **No WASM** — reducers are Go functions, so the host/guest ABI layer disappears entirely.
2. **No RLS/views** — simplifies plan compilation significantly (no multi-variant plans per identity).
3. **No SQL parser needed for v1** — subscriptions could be Go predicates or a simpler DSL. But the pruning optimization (SearchArguments) depends on being able to extract `(table, column, value)` from the predicate at compile time.
4. **Single goroutine model** — no need for read/write lock separation on SubscriptionManager. The executor goroutine can own it directly.
5. **Protocol Buffers or custom binary** — BSATN is SpacetimeDB-specific. Shunter needs its own encoding but the size-hint pattern (fixed vs variable row sizes) is worth adopting.
6. **Go's GC** — buffer pooling is still valuable but the pressure is different. `sync.Pool` is the natural analog.
