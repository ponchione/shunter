# SpacetimeDB Codebase Map

Research note mapping where the four core subsystems live in the SpacetimeDB codebase (`reference/SpacetimeDB/`). This is a navigation aid for spec derivation — not a spec itself.

---

## 1. In-Memory Table/Row Storage and Indexing

### Crates

| Crate | Role |
|-------|------|
| **`crates/table/`** | Core table implementation: row storage in pages, index structures, pointer maps, BFLATN encoding |
| **`crates/datastore/`** | Transaction-aware state management: committed state vs. transactional state, mutation tracking |
| **`crates/sats/`** | Algebraic type system and BSATN serialization — defines `AlgebraicType`, `AlgebraicValue`, `ProductValue` |
| **`crates/data-structures/`** | Generic utilities: hash maps, object pools |

### Key Files — `crates/table/`

| File | Summary |
|------|---------|
| `src/table.rs` (~2874 lines) | `Table` struct and `RowRef`: page management, index maintenance, schema enforcement |
| `src/page.rs` (~2571 lines) | 64 KiB page abstraction: fixed-len rows top-down, var-len granules bottom-up, freelist |
| `src/pages.rs` | Page collection management across a table |
| `src/page_pool.rs` | Shared page pool across all tables for memory reuse |
| `src/pointer_map.rs` | `RowHash → RowPointer` hash index for deduplication (used when no unique constraint exists) |
| `src/var_len.rs` | 64-byte granule chain management for variable-length data |
| `src/blob_store.rs` | Content-addressed blob storage (BLAKE3 hash) for large objects (>65 KB) |
| `src/static_layout.rs` | Fast BFLATN↔BSATN conversion for fixed-length types |
| `src/bflatn_to.rs` | Row writing: serialize `AlgebraicValue` into page layout |
| `src/bflatn_from.rs` | Row reading: deserialize page layout back to values |
| `src/table_index/mod.rs` | Index abstraction with type-specialized keys (U8–U64, I8–I64, F32, F64, String, Product) |
| `src/table_index/btree_index.rs` | Non-unique BTree index: `BTreeMap<K, SmallVec<[RowPointer; 1]>>` |
| `src/table_index/hash_index.rs` | Non-unique hash index: `HashMap<K, SmallVec<[RowPointer; 1]>>` |
| `src/table_index/unique_btree_index.rs` | Unique BTree: `BTreeMap<K, RowPointer>` |
| `src/table_index/unique_hash_index.rs` | Unique hash: `HashMap<K, RowPointer>` |
| `src/table_index/unique_direct_index.rs` | Direct array indexing for small key spaces (e.g., bool, small integers) |

### Key Files — `crates/datastore/`

| File | Summary |
|------|---------|
| `src/locking_tx_datastore/committed_state.rs` | `CommittedState`: immutable snapshot of all committed tables + blob store + page pool |
| `src/locking_tx_datastore/tx_state.rs` | `TxState`: per-transaction insert/delete tables, separate blob store |
| `src/locking_tx_datastore/datastore.rs` | `Locking`: top-level datastore managing lock acquisition order |
| `src/locking_tx_datastore/mut_tx.rs` | `MutTxId`: mutable transaction handle with read-write access |
| `src/traits.rs` | `TxData` struct (mutation journal), transaction traits, isolation levels |

### Architecture Notes

- **Row-store** with typed columns. Rows encoded in BFLATN format within 64 KiB pages.
- Fixed-length fields stored inline in pages. Variable-length fields (strings, arrays) stored as 64-byte granule chains; objects >65 KB go to content-addressed blob store.
- **Transaction isolation**: inserts go to `TxState.insert_tables` (separate Table instances), deletes are tracked in `TxState.delete_tables` referencing committed rows. Committed state never modified during a transaction.
- Six index types with type-specialized keys to avoid boxing overhead.

---

## 2. Commit Log / WAL

### Crates

| Crate | Role |
|-------|------|
| **`crates/commitlog/`** | Core WAL: segment management, commit encoding, CRC32C checksums, offset indexing |
| **`crates/durability/`** | Async durability interface: batching, background flush actor, `DurableOffset` watch |
| **`crates/snapshot/`** | Point-in-time database snapshots for fast recovery |

### Key Files — `crates/commitlog/`

| File | Summary |
|------|---------|
| `src/lib.rs` (~646 lines) | `Commitlog<T>` public API: flush, sync, reset |
| `src/commitlog.rs` (~1440 lines) | `Generic<R: Repo, T>`: segment lifecycle, traversal, append |
| `src/commit.rs` (~485 lines) | `Commit` / `StoredCommit`: per-batch record with min_tx_offset, epoch, n_txs, checksum |
| `src/segment.rs` (~986 lines) | Segment header (`(ds)^2` magic), writer, reader, metadata; 1 GiB max per segment |
| `src/payload.rs` | `Encode` / `Decoder` traits for pluggable serialization |
| `src/payload/txdata.rs` (~726 lines) | Canonical entry format: `Txdata<T>` with flags for inputs/outputs/mutations |
| `src/repo/mod.rs` | Repository trait: abstract segment storage backend |
| `src/repo/fs.rs` | Filesystem repository implementation |
| `src/index/indexfile.rs` | Offset index at 4 KiB intervals for O(log n) seeking within segments |

### Key Files — `crates/durability/`

| File | Summary |
|------|---------|
| `src/lib.rs` (~266 lines) | `Durability` trait (async append), `History` trait (fold/iterate), `DurableOffset` watch handle |
| `src/imp/local.rs` (~418 lines) | `Local<T>`: background actor batching transactions, writing to commitlog, flushing+syncing |

### Key Files — `crates/snapshot/`

| File | Summary |
|------|---------|
| `src/lib.rs` (~1299 lines) | Snapshot creation/restoration, zstd compression, blake3 verification, segment compressor |
| `src/remote.rs` (~993 lines) | Remote snapshot transfer: `BlobProvider` trait, async streaming upload/download, content-addressed object storage |

### Core Integration — `crates/core/src/db/`

| File | Summary |
|------|---------|
| `durability.rs` | `DurabilityWorker`: reorder buffer, gap detection, submits to durability layer |
| `persistence.rs` | `Persistence`: coordinates durability + snapshot worker + disk size monitoring |
| `snapshot.rs` | `SnapshotWorker`: triggers snapshots, recovery orchestration |

### Architecture Notes

- **Commit** bundles 1+ transactions with CRC32C checksum. Segments hold up to 1 GiB, compressed (zstd) when full.
- **Entry format**: `Txdata` with bitflags for optional inputs/outputs/mutations. Mutations are insert/delete/truncate per table.
- **Durability flow**: Transaction → bounded MPSC channel → background actor batches → commitlog.flush_and_sync() → DurableOffset advances via watch channel.
- **Recovery**: Load latest snapshot → replay commitlog from snapshot's tx_offset → reconstruct in-memory state. Corrupt commits at segment tail are skipped.

---

## 3. Transaction Execution and Reducer Dispatch

### Crates

| Crate | Role |
|-------|------|
| **`crates/core/`** | Transaction lifecycle, reducer dispatch, single-threaded executor model |
| **`crates/datastore/`** | Low-level transactional datastore (begin/commit/rollback) |
| **`crates/execution/`** | DML executors (insert, delete, update) |
| **`crates/bindings/`** | Public API for WASM modules to call host functions |
| **`crates/bindings-sys/`** | Low-level syscall ABI between WASM guest and host |

### Key Files — Transaction Lifecycle

| File | Summary |
|------|---------|
| `crates/core/src/db/relational_db.rs` | `RelationalDB`: `begin_mut_tx()`, `commit_tx()`, `rollback_mut_tx()`, snapshot management |
| `crates/datastore/src/locking_tx_datastore/datastore.rs` | `Locking`: serializable isolation via global mutex |
| `crates/datastore/src/locking_tx_datastore/mut_tx.rs` | `MutTxId`: mutable transaction handle |
| `crates/datastore/src/traits.rs` | `TxData`: immutable record of all inserts/deletes per table after commit |

### Key Files — Reducer Dispatch

| File | Summary |
|------|---------|
| `crates/core/src/host/wasm_common/module_host_actor.rs` | `ModuleHostCommon::call_reducer_with_tx()`: main reducer dispatcher, WASM execution, commit orchestration |
| `crates/core/src/client/client_connection.rs` | `call_reducer()` / `call_reducer_v2()`: client-facing entry points |
| `crates/core/src/util/jobs.rs` | `SingleCoreExecutor`: one Tokio runtime per database, pinned to CPU core |
| `crates/core/src/host/scheduler.rs` | `Scheduler` trait: delayed/scheduled reducer execution |
| `crates/execution/src/dml.rs` | DML executors that mutate datastore during reducer execution |

### Key Files — WASM Host Interface

| File | Summary |
|------|---------|
| `crates/bindings/src/lib.rs` | WASM module API: table operations, reducer registration |
| `crates/bindings-sys/src/lib.rs` | Syscall interface: `insert_row()`, `delete_by_pk()`, `iter_all()`, etc. |
| `crates/core/src/host/wasm_common/abi.rs` | ABI version detection and compatibility |

### Transaction Lifecycle

```
1. BEGIN    → RelationalDB::begin_mut_tx(Serializable)
             → Locking acquires global mutex
             → Returns MutTxId

2. EXECUTE  → call_reducer_with_tx() sets tx in WASM instance
             → WASM calls host ABI (insert_row, delete_by_pk, etc.)
             → Mutations recorded in TxState (insert_tables / delete_tables)

3. COMMIT   → commit_and_broadcast_event()
             → Locking::commit_mut_tx() → creates TxData from TxState
             → Vec<ProductValue> → Arc<[ProductValue]> for cheap sharing
             → Releases mutex
             → DurabilityWorker::request_durability() queues async WAL write
             → eval_updates_sequential() evaluates subscriptions against delta
             → broadcast_queue.send() pushes updates to clients

4. ABORT    → rollback_mut_tx() discards TxState
             → No durability record, no subscription broadcast
```

### Architecture Notes

- **Single-threaded per database**: `SingleCoreExecutor` pins one Tokio runtime to a CPU core. All reducers for a given database execute sequentially.
- **Serializable isolation**: Global mutex during transaction. No concurrent mutations on same database.
- **Mutation journal**: `TxData` captures `Arc<[ProductValue]>` arrays of inserts/deletes per table. Created at commit time from `TxState`. Shared between durability layer and subscription evaluator.
- **Commit-then-broadcast is atomic**: Subscriptions evaluated under read lock before write lock fully released, preventing duplicate updates.

---

## 4. Subscription Evaluation and Client Delta Fan-Out

### Crates

| Crate | Role |
|-------|------|
| **`crates/subscription/`** | Subscription plan compilation: SQL → physical plans with insert/delete delta fragments |
| **`crates/core/src/subscription/`** (module, not crate) | Runtime subscription management: registration, delta eval, index pruning, fan-out orchestration |
| **`crates/client-api/`** | WebSocket HTTP endpoint: connection lifecycle, protocol negotiation (v1/v2, BSATN/JSON) |
| **`crates/client-api-messages/`** | Wire protocol types: `ClientMessage`, `ServerMessage`, `TableUpdate`, `DatabaseUpdate` |
| **`crates/sql-parser/`** | SQL AST definitions and parser: converts SQL text to typed AST nodes; dependency of `expr`, `physical-plan`, `execution` |
| **`crates/query/`** | SQL parsing and type checking; entry point for compiling subscriptions |
| **`crates/physical-plan/`** | Query optimization and physical plan compilation; supports delta scans and index joins |
| **`crates/expr/`** | Expression type checking, Row-Level Security filter resolution, view handling |

### Key Files — Plan Compilation (`crates/subscription/`)

| File | Summary |
|------|---------|
| `src/lib.rs` (~542 lines) | `SubscriptionPlan`: insert_plans + delete_plans fragments; `JoinEdge` for pruning; incremental view maintenance algebra |

### Key Files — Runtime Management (`crates/core/src/subscription/`)

| File | Summary |
|------|---------|
| `module_subscription_manager.rs` (~3216 lines) | **Orchestration center**: `SubscriptionManager` with query registry, `SearchArguments` (parameterized pruning), `JoinEdges` (join pruning), `eval_updates_sequential()` main loop |
| `module_subscription_actor.rs` (~4359 lines) | Async wrapper: `ModuleSubscriptions`, metrics, broadcast queue |
| `delta.rs` (~126 lines) | `eval_delta()`: compute (inserts, deletes) for one subscription against one transaction's changes; bag-semantic dedup for joins |
| `tx.rs` (~100+ lines) | `DeltaTx`: read-only transaction wrapper exposing delta rows with built indexes |
| `mod.rs` (~345 lines) | `execute_plan()`, `collect_table_update()`, `execute_plans()` — run plans and build results |
| `execution_unit.rs` (~50 lines) | `QueryHash`: blake3-based subscription identifier |
| `websocket_building.rs` (~150+ lines) | BSATN/JSON encoding for wire format; `RowListBuilder` trait |
| `row_list_builder_pool.rs` (~91 lines) | Object pool for `BytesMut` buffers to reduce allocation pressure |

### Key Files — Client Protocol

| File | Summary |
|------|---------|
| `crates/client-api/src/routes/subscribe.rs` (~2322 lines) | WebSocket handler: HTTP upgrade, protocol negotiation, message routing |
| `crates/client-api-messages/src/websocket/v1.rs` | V1 protocol: `Subscribe`, `SubscribeSingle`, `TransactionUpdate` messages |
| `crates/client-api-messages/src/websocket/v2.rs` | V2 protocol: `SubscribeMulti`, `QuerySetId`, bandwidth optimizations |

### Key Files — Query Compilation

| File | Summary |
|------|---------|
| `crates/query/src/lib.rs` (~101 lines) | `compile_subscription()`: SQL → `ProjectPlan` with RLS resolution |
| `crates/physical-plan/src/compile.rs` | Physical plan compilation with delta scan support |

### Delta Computation Algorithm

For single-table subscription `V = σ(R)`:
- 1 insert plan (scan delta inserts through filter)
- 1 delete plan (scan delta deletes through filter)

For join subscription `V = R ⋈ S`, incremental view maintenance produces:
- 4 insert plan fragments: `dR(+) ⋈ S`, `R ⋈ dS(+)`, `dR(+) ⋈ dS(-)`, `dR(-) ⋈ dS(+)`
- 4 delete plan fragments: `dR(-) ⋈ S`, `R ⋈ dS(-)`, `dR(+) ⋈ dS(+)`, `dR(-) ⋈ dS(-)`

Bag-semantic dedup via ref-counting removes spurious duplicates from union of fragments.

### Subscription Optimization

1. **SearchArguments**: For `WHERE col = <literal>` subscriptions, index by `(TableId, ColId, Value) → Set<QueryHash>`. On row change, look up only matching subscriptions instead of scanning all.
2. **JoinEdges**: For join subscriptions, track `(lhs_table, rhs_table, join_cols, filter_col) → Value → Set<QueryHash>`. Skip re-evaluation when join condition can't be satisfied by the changed rows.
3. **Memoized encoding**: Multiple clients subscribing to same query share one encoded result.
4. **Buffer pooling**: `BytesMut` buffers reused across updates (default 4 KiB per buffer, 4 MiB pool).
5. **Async fan-out**: `SendWorker` actor decouples encoding/routing from transaction commit path.

### Wire Format

```
ServerMessage::TransactionUpdate {
    tables: [
        TableUpdate {
            table_id, table_name,
            update: QueryUpdate {
                inserts: BsatnRowList | JsonRows,
                deletes: BsatnRowList | JsonRows,
            },
        }, ...
    ],
    transaction_update_id,
}
```

BSATN rows include size hints (fixed or variable with offsets) for parallel client-side decode.

---

## Cross-Cutting: Crate Dependency Flow

```
sats (type system)
  ↓
table (row storage, pages, indexes)
  ↓
datastore (tx state: committed vs. transactional)
  ↓
core/db (RelationalDB: tx lifecycle, durability integration)
  ↓
core/host (reducer dispatch, WASM execution)
  ↓
core/subscription (delta eval, fan-out)
  ↓
client-api (WebSocket, protocol)

commitlog ← durability ← core/db/persistence
snapshot ← core/db/snapshot
sql-parser ← expr ← physical-plan ← subscription
```

## Crate Index (All Relevant)

| Crate | One-Line Summary |
|-------|-----------------|
| `sats` | Algebraic type system: types, values, BSATN binary serialization |
| `table` | In-memory row storage: pages, indexes (btree/hash/direct), pointer maps |
| `datastore` | Transactional datastore: committed state, tx state, isolation, mutation journal |
| `commitlog` | Write-ahead log: segments, commits, CRC32C, offset indexes |
| `durability` | Async durability interface: background flush actor, DurableOffset watch |
| `snapshot` | Point-in-time snapshots: creation, zstd compression, blake3 verification, recovery |
| `core` | Central orchestrator: RelationalDB, reducer dispatch, subscription management, persistence |
| `execution` | DML executors: insert, delete, update operations on datastore |
| `subscription` | Subscription plan compilation: SQL → delta fragment plans |
| `sql-parser` | SQL AST and parser: SQL text → typed AST nodes (wraps `sqlparser` crate with SpacetimeDB-specific AST) |
| `query` | SQL parsing, type checking, subscription compilation entry point |
| `physical-plan` | Query optimizer: physical plans with delta scan support |
| `expr` | Expression type checking, RLS filter resolution |
| `client-api` | WebSocket server: HTTP upgrade, protocol negotiation, message routing |
| `client-api-messages` | Wire protocol types: v1/v2 message definitions, BSATN/JSON formats |
| `bindings` | WASM module public API: table ops, reducer registration |
| `bindings-sys` | WASM↔host syscall ABI: insert_row, delete_by_pk, iterators |
| `schema` | Table/index/constraint schema definitions |
| `primitives` | Shared primitive types: TableId, IndexId, ColId, etc. |
