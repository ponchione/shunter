# SPEC-003 — Epic Decomposition

Source: [SPEC-003-executor.md](./SPEC-003-executor.md)

---

## Epic 1: Core Types & Command Model

**Spec sections:** §2.2–§2.4, §3.1–§3.3, §6, §7, §8, §9.3, §11

All foundational types, interfaces, and error sentinels the executor is built on. No behavior — pure definitions.

**Scope:**
- `TxID`, `ScheduleID`, `SubscriptionID` named types
- `CallSource`, `ReducerStatus`, `LifecycleKind` enums
- `ReducerHandler` function type (raw byte-oriented runtime signature)
- `RegisteredReducer`, `CallerContext`, `ReducerRequest`, `ReducerResponse`, `ReducerContext` structs
- `ExecutorCommand` interface + `CallReducerCmd`, `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, `DisconnectClientSubscriptionsCmd` command types
- `DurabilityHandle`, `SubscriptionManager`, `SchedulerHandle` interfaces
- Error sentinels: `ErrReducerNotFound`, `ErrLifecycleReducer`, `ErrExecutorBusy`, `ErrExecutorShutdown`, `ErrReducerPanic`, `ErrCommitFailed`, `ErrExecutorFatal`

**Testable outcomes:**
- All types compile and construct correctly
- Enum values are distinct
- Command types satisfy `ExecutorCommand` interface
- Error sentinels are distinguishable via `errors.Is`

**Dependencies:** None. This is the leaf.

---

## Epic 2: Reducer Registry

**Spec sections:** §3.2, §10.1

Registration system for reducers and lifecycle hooks. Immutable after engine start.

**Scope:**
- `ReducerRegistry` struct: map of name → `RegisteredReducer`
- `Register()` with name uniqueness enforcement
- `Lookup()` by name
- Lifecycle name reservation: `OnConnect` and `OnDisconnect` cannot be registered as normal reducers
- `Freeze()` to make registry immutable after startup

**Testable outcomes:**
- Register reducer, lookup by name — found
- Register duplicate name → error
- Register normal reducer with lifecycle name → error
- Register lifecycle reducer with `LifecycleOnConnect` — accepted
- Lookup non-existent name → not found
- Register after Freeze → error

**Dependencies:** Epic 1 (RegisteredReducer, LifecycleKind)

---

## Epic 3: Executor Core

**Spec sections:** §2.1–§2.5, §4.1

The executor goroutine, bounded inbox, command dispatch routing, and shutdown.

**Scope:**
- `Executor` struct: inbox channel, registry, committed-state handle, durability handle, subscription manager, TxID counter, fatal state flag
- `NewExecutor()` constructor with configurable inbox capacity and TxID initialization from recovery
- `run(ctx)` goroutine: receive from inbox with context cancellation, process one command at a time
- `dispatchSafely()` top-level panic recovery envelope
- `dispatch()` command type switch: route CallReducerCmd, subscription commands, lifecycle commands
- Subscription command handlers: `Register`, `Unregister`, `DisconnectClient` delegated to SubscriptionManager
- Explicit read-routing boundary: registration-sensitive reads stay executor-serialized; purely observational reads may use direct snapshots out-of-band
- Submit methods: blocking send, optional `ErrExecutorBusy` on full inbox
- Shutdown: close inbox, drain remaining, reject after close with `ErrExecutorShutdown`, then tear down durability only after admissions stop and drain completes
- Startup orchestration: recovery TxID hand-off, scheduler replay, dangling-client sweep, and first-accept gating

**Testable outcomes:**
- Submit command, executor processes it
- Submit to full inbox with block policy — blocks until space
- Submit to full inbox with reject policy — `ErrExecutorBusy`
- Submit after shutdown — `ErrExecutorShutdown`
- Close inbox — run loop terminates cleanly without spin
- Panic in dispatchSafely — executor goroutine survives, continues processing
- RegisterSubscriptionCmd acquires snapshot and calls SubscriptionManager.Register atomically
- Purely observational reads are documented to use direct snapshots rather than executor commands
- Durability shutdown happens only after the executor stops admitting new write commands and drains queued work
- Recovery hand-off and startup sequencing are documented so no external command can interleave ahead of scheduler replay or dangling-client cleanup

**Dependencies:** Epic 1 (types, commands, interfaces), Epic 2 (registry)

---

## Epic 4: Reducer Transaction Lifecycle

**Spec sections:** §3.4, §3.5, §4.2–§4.6

Begin, execute, commit, and rollback for a single reducer call. ReducerContext construction and invariants.

**Scope:**
- Begin: look up reducer in registry, construct `CallerContext` with dequeue-time timestamp, create `Transaction` from committed state, build `ReducerContext`
- Execute: reducer-local panic recovery block, call `reducer.Handler(ctx, args)`
- Reducer execution invariants: no synchronous nested reducer calls, no retain-after-return, no cross-goroutine use of transaction-owned objects, no blocking I/O on executor goroutine
- Commit path: call `store.Commit()`, assign next TxID, build `ReducerResponse` with `StatusCommitted`
- Rollback on user error: discard transaction, respond `StatusFailedUser`
- Rollback on panic: discard transaction, respond `StatusFailedPanic`
- Commit failure: respond `StatusFailedUser` (constraint) or `StatusFailedInternal` (engine error)
- Lifecycle reducer rejection: `ErrLifecycleReducer` before begin if external caller names a lifecycle reducer
- ReducerContext invariants: valid only during synchronous invocation, no goroutine escape, no retain

**Testable outcomes:**
- Call reducer with valid args → `StatusCommitted`, state mutated
- Call reducer with malformed args via typed adapter → `StatusFailedUser`, state unchanged
- Reducer returns error → `StatusFailedUser`, state unchanged
- Reducer panics → `StatusFailedPanic`, state unchanged, executor continues
- Commit fails (uniqueness) → `StatusFailedUser`, state unchanged
- Commit fails (internal) → `StatusFailedInternal`, state unchanged
- CallerContext.Timestamp set at dequeue, not caller-provided
- Two sequential reducer calls commit in FIFO order
- External call to lifecycle reducer → `ErrLifecycleReducer`, no transaction
- Reducer execution docs/API explicitly forbid retain-after-return, cross-goroutine use, and blocking I/O on the executor goroutine

**Dependencies:** Epic 3 (executor loop, dispatch), SPEC-001 (NewTransaction, Commit, CommittedState)

---

## Epic 5: Post-Commit Pipeline

**Spec sections:** §5.1–§5.4

Ordered post-commit steps: durability handoff, snapshot, subscription evaluation, delta delivery, dropped client drain, fatal state on post-commit failure.

**Scope:**
- Post-commit step ordering (must be exact):
  1. `DurabilityHandle.EnqueueCommitted(txID, changeset)`
  2. Acquire `CommittedReadView` via `CommittedState.Snapshot()`
  3. `SubscriptionManager.EvalAndBroadcast(txID, changeset, view, meta)`
  4. Close snapshot
  5. Send `ReducerResponse` to caller
  6. Non-blocking drain of `SubscriptionManager.DroppedClients()` → call `DisconnectClient` for each
  7. Return to dequeue next command
- Durability handoff is queue admission, not fsync wait
- Acknowledged success and delta handoff may occur before durability; crash recovery may lose recently acknowledged but not-yet-durable txs
- Subscription evaluation is synchronous — executor blocked until all subscriptions evaluated and deltas handed off
- Fatal state transition: if any post-commit step panics, executor enters failed state, rejects all future write commands with `ErrExecutorFatal`
- No rollback possible after commit — post-commit failures are fatal, not recoverable

**Testable outcomes:**
- Committed changeset handed to durability before subscription evaluation
- Subscription evaluation sees committed state (not tx-local)
- Reducer response sent after subscription evaluation completes
- Dropped clients drained after response delivery
- Post-commit panic → executor enters fatal state
- After fatal state, future write commands rejected with `ErrExecutorFatal`
- Durability backpressure stalls executor but does not drop commits
- Commit acknowledged before durability, crash before fsync, restart loses tx

**Dependencies:** Epic 4 (commit path), SPEC-001 (Snapshot/ReadView), SPEC-002 (DurabilityHandle), SPEC-004 (SubscriptionManager)

---

## Epic 6: Scheduled Reducers

**Spec sections:** §9

Durable scheduled reducers: `sys_scheduled` system table, transactional schedule/cancel, timer wakeups, firing semantics, and startup replay.

**Scope:**
- `sys_scheduled` built-in table: `schedule_id` (autoincrement PK), `reducer_name`, `args`, `next_run_at_ns`, `repeat_ns`
- `SchedulerHandle` implementation: `Schedule()`, `ScheduleRepeat()`, `Cancel()`
- Schedule/cancel are transactional — mutations to `sys_scheduled` roll back if surrounding reducer rolls back
- Timer goroutine: scan `sys_scheduled` for due rows, enqueue internal `CallReducerCmd` with `CallSourceScheduled` plus `ScheduleID` / `IntendedFireAt`
- One-shot success: execute reducer + delete `sys_scheduled` row in same transaction, single commit
- Repeating success: execute reducer + advance `next_run_at_ns` by interval (fixed-rate from intended fire time, not completion time)
- Failure: transaction rolls back, `sys_scheduled` row unchanged, retry after restart or rescan
- Broken schedule rows (missing reducer / future typed-adapter decode failure) are retried until operator intervention; v1 does not quarantine them automatically
- Startup replay: on executor start, scan `sys_scheduled` to populate timer with pending wakeups
- Crash semantics: at-least-once relative to durable state (not exactly-once)

**Testable outcomes:**
- Schedule one-shot, fire, sys_scheduled row deleted on commit
- Schedule one-shot, reducer fails, sys_scheduled row still present
- Schedule repeating, fire, next_run_at_ns advanced by interval from intended time (not wall clock)
- Cancel schedule in same reducer, sys_scheduled row deleted on commit
- Schedule inside reducer that rolls back — schedule not persisted
- Restart with pending schedules — all rescanned and enqueued
- At-least-once: schedule survives crash between fire and durable commit

**Dependencies:** Epic 4 (transaction lifecycle), Epic 5 (post-commit pipeline for scheduled reducer commits)

---

## Epic 7: Lifecycle Reducers & Client Management

**Spec sections:** §10

OnConnect/OnDisconnect lifecycle hooks, `sys_clients` system table, and direct invocation protection.

**Scope:**
- `sys_clients` built-in table: `connection_id` (bytes(16) PK), `identity` (bytes(32)), `connected_at` (int64 unix nanos)
- OnConnect flow (internal command from protocol layer):
  1. Insert `sys_clients` row
  2. Run OnConnect reducer if registered
  3. Commit → keep row, allow connection
  4. Error/panic → rollback both reducer writes and row insertion, reject connection
- OnDisconnect flow (internal command from protocol layer):
  1. Run OnDisconnect reducer if registered
  2. Delete `sys_clients` row in same transaction
  3. Commit once
  4. On failure: roll back reducer writes, then run separate cleanup transaction that deletes `sys_clients` row regardless
- Direct invocation protection: external call to lifecycle reducer name → `ErrLifecycleReducer` before transaction begin; implementation lives in Epic 4 and Epic 7 adds lifecycle-context integration verification
- `sys_clients` changes appear in normal changesets and trigger subscription deltas
- Disconnect cannot be vetoed by the reducer
- Startup dangling-client sweep removes crash-leftover `sys_clients` rows before first accept

**Testable outcomes:**
- OnConnect commits → `sys_clients` row present, connection accepted
- OnConnect reducer fails → `sys_clients` row absent, connection rejected
- OnDisconnect commits → `sys_clients` row deleted
- OnDisconnect reducer fails → `sys_clients` row still deleted via cleanup transaction
- External `CallReducerCmd` naming "OnConnect" → `ErrLifecycleReducer`
- `sys_clients` insert/delete appears in changeset, triggers subscription eval
- No registered OnConnect reducer → still inserts `sys_clients` row and commits
- Restart with stale `sys_clients` rows → startup sweep cleans them before first accept

**Dependencies:** Epic 4 (transaction lifecycle), Epic 5 (post-commit pipeline for lifecycle commits)

---

## Dependency Graph

```
Epic 1: Core Types & Command Model
  └── Epic 2: Reducer Registry
        └── Epic 3: Executor Core
              └── Epic 4: Reducer Transaction Lifecycle  ← SPEC-001 (Store)
                    └── Epic 5: Post-Commit Pipeline  ← SPEC-002 (Durability), SPEC-004 (Subscriptions)
                          ├── Epic 6: Scheduled Reducers
                          └── Epic 7: Lifecycle Reducers
```

## Error Types

Errors introduced where first needed:

| Error | Introduced in |
|---|---|
| `ErrReducerNotFound` | Epic 1 (defined), Epic 4 (returned) |
| `ErrLifecycleReducer` | Epic 1 (defined), Epic 4 (returned) |
| `ErrExecutorBusy` | Epic 1 (defined), Epic 3 (returned) |
| `ErrExecutorShutdown` | Epic 1 (defined), Epic 3 (returned) |
| `ErrReducerPanic` | Epic 1 (defined), Epic 4 (returned) |
| `ErrCommitFailed` | Epic 1 (defined), Epic 4 (returned) |
| `ErrExecutorFatal` | Epic 1 (defined), Epic 5 (returned) |
