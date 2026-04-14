# SPEC-004 — Epic Decomposition

Source: [SPEC-004-subscriptions.md](./SPEC-004-subscriptions.md)

---

## Epic 1: Predicate Types & Query Hash

**Spec sections:** §3.1–§3.4

Structured predicate expression tree that the entire pruning and evaluation system inspects.

**Scope:**
- `Predicate` sealed interface with `Tables() []TableID`
- Concrete types: `ColEq`, `ColRange`, `And`, `AllRows`, `Join`
- `Bound` struct (value, inclusive/exclusive, unbounded)
- Predicate validation: at most 2 tables, join requires index, literal values only
- `QueryHash` type (blake3, 32 bytes)
- Canonical serialization of predicate structure
- Hash computation: non-parameterized (structure only) vs parameterized (structure + client identity)
- Two clients with identical predicate + params → same hash

**Testable outcomes:**
- Construct each predicate type, `Tables()` returns correct table IDs
- `And` of two single-table predicates → 1 table
- `Join` → 2 tables
- Predicate referencing 3 tables → validation error
- Join without index on join column → validation error
- Identical predicates → identical query hash
- Same structure, different parameter values → different hash
- Same structure, same params, different client → same hash (non-parameterized)
- Predicate remains inspectable/structured; opaque Go callback predicates are not a supported registration contract (§3.1)
- v1 query-language contract is the Go predicate builder described in §12.1

**Dependencies:** None. This is the leaf.

---

## Epic 2: Pruning Indexes

**Spec sections:** §5.1–§5.4

Three-tier index structure that maps changesets to candidate subscriptions in sub-linear time.

**Scope:**
- `ValueIndex` (Tier 1): nested map `table → column → encoded(value) → set of query hashes` (equality-only lookup)
- `JoinEdgeIndex` (Tier 2): `JoinEdge → encoded(rhs_filter_value) → set of query hashes`, plus a `byTable` denormalization so `EdgesForTable` runs without iterating the full edge map
- `TableIndex` (Tier 3 fallback): `table → set of query hashes`
- Index placement logic per §5.4 invariant: ColEq → Tier 1, join with filterable edge → Tier 2, else → Tier 3
- Insert/remove operations on all three indexes (used by registration/deregistration)
- Candidate collection: given a set of changed rows, union results from all three tiers

**Testable outcomes:**
- Register ColEq subscription → appears in ValueIndex, not others
- Register AllRows subscription → appears in TableIndex fallback
- Register Join with filter → appears in JoinEdgeIndex
- Remove subscription → no longer appears in any index
- Candidate lookup on ValueIndex: change to tracked column+value returns correct hashes
- Candidate lookup on ValueIndex: change to untracked value returns empty
- Candidate lookup on TableIndex: any change to table returns all fallback hashes
- Two-table subscription may appear in different tiers for each table

**Dependencies:** Epic 1 (Predicate types, QueryHash)

---

## Epic 3: DeltaView & Delta Computation

**Spec sections:** §6.1–§6.4

Incremental view maintenance engine. Computes deltas for single-table and join subscriptions.

**Scope:**
- `DeltaView` struct: committed read view + per-table insert/delete slices + delta indexes
- `DeltaIndexes`: per-transaction scratch indexes over delta rows, keyed by `(TableID, ColID)` via nested maps with canonical encoded-value keys. Stores positions (int indices) into the insert/delete slice rather than copying rows
- Eager delta index construction (once per transaction, not per subscription)
- Single-table delta: filter inserts → delta inserts, filter deletes → delta deletes
- Join delta: 4 insert fragments (I1–I4) + 4 delete fragments (D1–D4) per §6.2
- Bag-semantic deduplication: insert/delete count maps with cancellation per §6.3
- Only build delta indexes for columns referenced by active subscriptions
- Buffer pooling for `[]byte` via `sync.Pool` (§9.2)
- Slice and map reuse across transactions (§9.2)
- Direct byte comparison for row dedup, not `interface{}` equality (§9.2)

**Testable outcomes:**
- Single-table: insert rows matching filter → delta inserts only
- Single-table: delete rows matching filter → delta deletes only
- Single-table: insert row not matching filter → empty delta
- Join delta: dT1(+) join T2' produces correct insert fragment
- Join delta: all 8 fragments computed, bag dedup resolves cancellations
- Bag semantics: row in both insert and delete fragments → cancels
- Bag semantics: row joining 3 RHS rows, delete 1 → delta shows 1 delete
- Delta indexes support index lookup on delta rows
- DeltaView constructed once, serves multiple subscriptions
- Benchmark: delta index construction < 1 ms for typical transactions (§9.1)

**Dependencies:** Epic 1 (Predicate types for filter application), SPEC-001 (CommittedReadView for base table access)

---

## Epic 4: Subscription Manager

**Spec sections:** §4.1–§4.3, §10.1–§10.3

Central registry that tracks active subscriptions, manages deduplication, and implements the `SubscriptionManager` interface.

**Scope:**
- `SubscriptionManager` interface: `Register`, `Unregister`, `DisconnectClient`, `EvalAndBroadcast`, `DroppedClients`
- Internal query state: `queryHash → {compiledPlans, subscriberSet, refCount}`
- Subscriber set: `queryHash → map[ConnectionID]SubscriptionID`
- `Register` flow: validate → compile → hash → dedup check → initial query → insert into pruning indexes → return initial rows
- `Unregister`: remove client from subscriber set, decrement ref count, if zero → remove from all indexes and free plans
- `DisconnectClient`: batch unsubscribe for all subscriptions of a connection
- Registration runs inside executor command (no gap between initial query and delta start)
- `DroppedClients() <-chan ConnectionID` for executor drain loop

**Testable outcomes:**
- Register subscription → initial rows returned, subscription queryable
- Register same predicate from two clients → shared query state (single compiled plan)
- Unregister one of two clients → query state still alive
- Unregister last client → query state and index entries removed
- DisconnectClient removes all subscriptions for that connection
- Register with invalid predicate (3 tables) → error
- Register with unindexed join column → error
- DroppedClients channel receives disconnected ConnectionIDs
- Benchmark: registration/deregistration < 100 µs per operation (§9.1)
- v1 update granularity is row-level full-row inserts/deletes; updates are represented as delete+insert (§12.2)

**Dependencies:** Epic 1 (Predicate, QueryHash), Epic 2 (pruning indexes for placement), SPEC-001 (CommittedReadView for initial query)

---

## Epic 5: Evaluation Loop

**Spec sections:** §7.1–§7.4, §9.1–§9.3

The hot-path algorithm that runs after every committed transaction.

**Scope:**
- `EvalTransaction(changeset *Changeset) → CommitFanout`
- Early exit: no active subscriptions → return immediately
- Build DeltaView from changeset + committed state
- Candidate collection: iterate changed rows, consult all 3 pruning tiers, union results
- Batched Tier 1 optimization: collect distinct values per column, one lookup per distinct value (§7.3)
- Per-candidate evaluation: call delta computation (Epic 3) for each candidate query
- Fan-out assembly: group `SubscriptionUpdate` entries by `ConnectionID`
- `CommitFanout` type: `map[ConnectionID][]SubscriptionUpdate`
- Memoized encoding: `memoizedResult{binary, json}` — compute once per query hash, share across clients (§7.4)
- Runs synchronously on executor goroutine — no locks needed
- Changeset is read-only (must not mutate)

**Testable outcomes:**
- No active subscriptions → empty fanout, fast return
- Single-table subscription: changeset with matching rows → correct delta in fanout
- Join subscription: changeset touching one side → correct 8-fragment delta
- Pruning: subscription on table A not triggered by changeset on table B
- Pruning: equality subscription only triggered by matching value
- Batched Tier 1: bulk insert with repeated values → one lookup per distinct value
- Memoized encoding: two clients same query → encoded once
- Multiple subscriptions per connection → grouped in one TransactionUpdate
- Benchmark: 10K equality subs, 1 table change → < 2 ms (§9.1)
- Benchmark: 100 join subs, 10 changed rows → < 20 ms (§9.1)
- Benchmark: fan-out 1K clients same query → < 1 ms (§9.1)
- Scaling claims in §9.3 are validated by the benchmark/property suite and by candidate-collection / memoization behavior

**Dependencies:** Epic 2 (pruning indexes for candidate collection), Epic 3 (delta computation), Epic 4 (subscription manager for subscriber lookup), SPEC-001 (CommittedReadView), SPEC-003 (Changeset / executor trigger)

---

## Epic 6: Fan-Out & Delivery

**Spec sections:** §8.1–§8.5

Decoupled delivery goroutine that sends computed deltas to client connections without blocking the executor.

**Scope:**
- `FanOutWorker` struct: inbox channel, client connection map
- `FanOutMessage`: `TxDurable <-chan TxID` + `CommitFanout`
- Fan-out algorithm: wait for durability (if confirmed reads), iterate per-connection entries, build `TransactionUpdate`, send to client outbound channel
- Per-connection aggregation: multiple `SubscriptionUpdate` entries preserved per subscription, not merged
- Caller client special case: reducer result metadata alongside delta
- Backpressure: non-blocking send to bounded client buffer; buffer full → disconnect client (v1)
- `DroppedClients()` channel: fan-out signals dropped ConnectionIDs, executor drains after each commit
- Fan-out channel depth: bounded, configurable (default 64)
- Confirmed reads vs fast reads: configurable per client via TxDurable wait

**Testable outcomes:**
- FanOutMessage sent → each connection receives its TransactionUpdate
- Multiple subscriptions per connection → single TransactionUpdate with multiple entries
- Slow client (full buffer) → disconnected
- Disconnected client → ConnectionID appears on DroppedClients channel
- Confirmed reads: delivery waits for TxDurable signal
- Fast reads: delivery proceeds without waiting
- Fan-out does not block executor goroutine
- Channel depth bounds memory growth

**Dependencies:** Epic 5 (produces CommitFanout), SPEC-005 (protocol sender / outbound buffering)

---

## Dependency Graph

```
Epic 1: Predicate Types & Query Hash
  ├── Epic 2: Pruning Indexes
  │     └── Epic 5: Evaluation Loop ← Epic 3, Epic 4
  ├── Epic 3: DeltaView & Delta Computation  ← SPEC-001 (Store)
  └── Epic 4: Subscription Manager  ← Epic 2, SPEC-001 (Store)
                                          │
                                    Epic 5: Evaluation Loop
                                          └── Epic 6: Fan-Out & Delivery  ← SPEC-005 (Protocol)
```

## Error Types

Errors introduced where first needed:

| Error | Introduced in |
|---|---|
| `ErrTooManyTables` | Epic 1 (validation) |
| `ErrUnindexedJoin` | Epic 1 (validation) |
| `ErrInvalidPredicate` | Epic 1 (validation) |
| `ErrTableNotFound` | Epic 1 (validation / registration path — predicate references nonexistent table) |
| `ErrColumnNotFound` | Epic 1 (validation / registration path — predicate references nonexistent column) |
| `ErrInitialRowLimit` | Epic 4 (registration — initial snapshot too large) |
| `ErrSubscriptionNotFound` | Epic 4 (unregister — unknown subscription ID) |
| `ErrSubscriptionEval` | Epic 5 (evaluation — corrupted index or type mismatch) |
