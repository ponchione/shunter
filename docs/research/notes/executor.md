# Transaction Executor — Deep Research Note

Research into SpacetimeDB's transaction execution subsystem (`crates/core/src/db/relational_db.rs`, `crates/core/src/host/`, `crates/execution/`, `crates/core/src/util/jobs.rs`). Extracts concurrency model, transaction lifecycle, commit pipeline, and scheduler design.

---

## 1. Concurrency Model

### 1.1 Single-Writer Enforcement

SpacetimeDB enforces single-writer semantics through two mechanisms:

**Write lock on CommittedState.** `begin_mut_tx()` acquires a write-arc on an `RwLock<CommittedState>`. Only one `MutTxId` can exist at a time — the next caller blocks until the current transaction commits or rolls back.

**`!Send` marker on `MutTxId`.** A `PhantomData<Rc<()>>` field on `MutTxId` makes it non-`Send`, so the compiler prevents it from crossing thread or `await`-point boundaries. This enforces that the mutable transaction stays on the per-module executor thread.

### 1.2 Per-Module Single-Core Executor

Each module (database instance) has a **`SingleCoreExecutor`** (`crates/core/src/util/jobs.rs`):
- Implemented as a spawned thread that hosts a `tokio::task::LocalSet`
- Receives work via an `mpsc::UnboundedSender<Box<dyn FnOnce() -> LocalBoxFuture>>`
- All reducer calls for a module serialize through this queue
- The runtime may migrate executors across pinned cores for load balancing, but each executor still processes one job stream at a time
- Provides execution isolation without explicit locks inside reducer code

### 1.3 Read-Only Concurrency

Read-only transactions (`Tx`) acquire only a read lock. Multiple `Tx` can coexist with each other and with the async durability writer. They cannot coexist with a mutable transaction committing (write lock excludes them momentarily at commit).

---

## 2. Full Transaction Lifecycle

```
ModuleHost::call_reducer(params)
  → dispatch to SingleCoreExecutor (channel send, non-blocking)
    → begin_mut_tx()  — acquire write lock on CommittedState
    → call reducer function (WASM/JS invocation)
       — operations: insert/delete/scan on TxState via MutTxId
       — error handling: user errors caught; traps kill instance
       — panic: scopeguard fires on_panic(), propagates, no commit
    → evaluate views (same MutTxId, pre-commit)
    → commit_tx_downgrade()
       — flush TxState into CommittedState
       — assign TxOffset (monotonic)
       — release write lock
       — return (TxData, TxMetrics, Tx)  [Tx = downgraded read-only view]
    → request_durability(tx_data)  — NON-BLOCKING send to durability worker
    → evaluate subscriptions (with read lock on subscriptions, using Tx)
    → broadcast deltas to clients (synchronous)
    → return EventStatus to caller
```

Steps 1-4 (begin through release write lock) are serialized. Steps 5-7 run with a read-only view and can be interleaved with the next reducer beginning.

---

## 3. Commit Pipeline Ordering Guarantees

The critical ordering constraint is:

1. **Acquire subscriptions read lock** BEFORE committing. Prevents clients from receiving subscription N's updates before commit N is complete (race where broadcast happens before new subscriber joins).
2. **Commit in-memory state** (write lock released). Changes become visible to read transactions.
3. **Send durability request** (async). Does not block the commit pipeline.
4. **Evaluate subscriptions** (synchronous). Uses the downgraded read-only Tx.
5. **Broadcast to clients** (synchronous). Returns before caller receives response.
6. **Release subscriptions read lock**.

Durability is **deliberately async**. The rationale: waiting for fsync would serialize all commits across all modules. By decoupling durability from commit visibility, the in-memory state advances at memory speed while the commit log catches up asynchronously.

**What this means for crash recovery:** At crash time, some committed transactions may not be on disk. The commit log writer tracks the highest durable TxOffset. On restart, the in-memory state is rebuilt from snapshots + all log entries up to that offset. Transactions after the last durable TxOffset are lost. This is a deliberate durability tradeoff, not a bug.

---

## 4. What Commit Returns (TxData)

Commit returns `TxData`, the net-effect changeset (same structure documented in store research note):

```
TxData {
    entries: map[TableId → TxDataTableEntry]
    tx_offset: Option<u64>
}

TxDataTableEntry {
    table_name: string
    inserts: [ProductValue]     — net new rows
    deletes: [ProductValue]     — net removed rows
    truncated: bool
    ephemeral: bool
}
```

This is the canonical input to the subscription evaluator and commit log writer. It is wrapped in `Arc<TxData>` and shared with both without copying.

---

## 5. Reducer Call Context

A reducer receives an `ExecutionContext` containing a `ReducerContext`:

```
ReducerContext {
    name:               ReducerName       — reducer name (for logging, metrics)
    caller_identity:    Identity          — who invoked the reducer (public key hash)
    caller_connection_id: ConnectionId   — WebSocket connection ID (0 for internal calls)
    timestamp:          Timestamp         — invocation time (assigned at call time, monotonic)
    arg_bsatn:          Bytes             — BSATN-encoded arguments
}
```

The context is stored in the `ExecutionContext` and accessible throughout the transaction via the `MutTxId`. Reducer code accesses it through a thread-local or via the WASM/JS host bindings.

---

## 6. Error and Panic Handling

Three classes of failure:

**User error** — reducer returns an explicit error (non-panic). Transaction is rolled back. Status = `FailedUser`. Module instance is reused.

**Trap / execution error** — WASM trap or unrecoverable execution error. Transaction is rolled back. Module instance is **discarded and recreated**. Status = `FailedInternal`.

**Panic** — Rust panic in module host code (not reducer code). `scopeguard::defer_on_unwind!` fires `on_panic()` which marks the module as unusable. The entire module is shut down.

In all failure cases: the write lock is released (MutTxId dropped), no TxData is produced, no durability write is issued, no subscription evaluation occurs.

---

## 7. Scheduled Reducers

Scheduled reducers are managed by a `SchedulerActor` per database:

- Uses `tokio::time::DelayQueue<QueueItem>` for timer management
- Entries stored in a system table (`st_scheduled_*`); scheduler scans on startup
- When a reducer schedules a future call, it sends a message to the scheduler via `mpsc::UnboundedSender` (non-blocking)
- At the scheduled time, the actor calls the reducer via the same `ModuleHost::call_reducer()` path — identical to an external call, but with no caller connection ID

**Scheduling constraints:**
- Maximum delay: ~2 years (tokio DelayQueue limitation)
- If a scheduled reducer fails, the schedule entry is removed; it does not retry automatically
- Scheduled calls are executed on the same `SingleCoreExecutor` as all other calls — they serialize with external reducer calls

---

## 8. Built-In Lifecycle Reducers

Two lifecycle hooks are automatically invoked by the runtime:

**OnConnect** — fired when a client establishes a WebSocket connection. A row is inserted into `st_clients` first, then this reducer is called inside the same transaction path. User code can set up per-client state. Cannot be called directly by clients.

**OnDisconnect** — fired when a client disconnects (clean close or network failure). The runtime removes the subscriber first, then invokes the lifecycle reducer if present. On the successful path, `st_clients` cleanup happens in the same transaction before commit. If the reducer fails, the runtime still performs cleanup in a fallback transaction so the disconnect cannot be vetoed. Cannot be called directly by clients.

Both go through the same reducer-execution machinery but are gated: clients that attempt to invoke them directly receive `ReducerCallError::LifecycleReducer`.

---

## 9. Key Insights for Shunter's Go Design

### 9.1 The channel replaces the write lock

SpacetimeDB uses an `RwLock` because it must coordinate multiple OS threads (Tokio async runtime). Shunter's single executor goroutine reads from a channel — by definition only one transaction runs at a time. No locks needed for write serialization.

### 9.2 Durability async decoupling is the right call

The async durability pattern is not a simplification — it's a deliberate throughput tradeoff. Shunter should adopt it: commit in-memory synchronously, write log asynchronously, track highest durable offset for crash recovery.

### 9.3 Subscription evaluation must happen synchronously post-commit

The subscription evaluator must see the committed state (not TxState) and must run before returning to the caller. Clients should receive deltas in the same "round" as the commit, not on a subsequent tick. This is a correctness requirement, not a performance preference.

### 9.4 The downgrade pattern is a Rust artifact

In SpacetimeDB, `commit_tx_downgrade()` converts `MutTxId` to `Tx` so the subscription evaluator can read the committed state using the read-side API. In Go, because CommittedState is accessed directly and there's no `Rc` ownership model, this is just "the executor passes CommittedState (read-only) to the subscription evaluator after commit". No downgrade needed.

### 9.5 WASM lifecycle is eliminated

SpacetimeDB's "discard module instance on trap" logic exists because WASM instances have corrupted linear memory after a trap. Go functions don't have this property — a recovered panic leaves no corrupted state. The Go equivalent is just `recover()` in a deferred function, roll back the transaction, return an error.

### 9.6 No energy budget

SpacetimeDB has an energy/compute budget per reducer call (spam prevention for the hosted service). Shunter doesn't need this — it's an embedded library with a trusted module.
