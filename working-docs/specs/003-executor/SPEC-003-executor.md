# SPEC-003 — Transaction Executor

**Status:** Baseline implementation contract; verify against live code  
**Depends on:** SPEC-001 (In-Memory Store), SPEC-002 (Commit Log — `DurabilityHandle` + `max_applied_tx_id` at recovery), SPEC-004 (Subscription Evaluator), SPEC-006 (Schema Definition)  
**Depended on by:** SPEC-002 (Commit Log — consumes `TxID` contract declared here; bidirectional interface dep resolved via SPEC-002 declaring `DurabilityHandle` and SPEC-003 declaring `TxID`), SPEC-005 (Client Protocol)

---

## 1. Purpose and Scope

The transaction executor is the single goroutine that owns all mutable database state in Shunter. It serializes reducer calls, system lifecycle hooks, scheduled reducers, and subscription registration work that must be atomic with commits.

This spec defines:
- The executor ownership and queueing model
- Reducer registration and invocation shape
- `ReducerContext` and its invariants
- Transaction begin/execute/commit/rollback flow
- Post-commit ordering rules
- Error handling and panic containment
- Scheduled reducer persistence and replay behavior
- Built-in lifecycle reducers (`OnConnect`, `OnDisconnect`)
- Interfaces the executor exposes to SPEC-002, SPEC-004, and SPEC-005

This spec does not define:
- Store internals (SPEC-001)
- Commit-log encoding or recovery format (SPEC-002)
- Subscription delta algorithms (SPEC-004)
- Wire protocol framing (SPEC-005)
- Schema reflection and typed reducer adapters (SPEC-006)

---

## 2. Executor Model

### 2.1 Ownership Rule

All mutable access to the database passes through one goroutine: the executor goroutine.

The executor exclusively owns:
- the mutable path into `CommittedState`
- creation and disposal of `Transaction` values
- mutation of built-in system tables (`sys_clients`, `sys_scheduled`)
- ordering between commit, durability handoff, and subscription evaluation

No other goroutine may mutate committed state directly.

### 2.2 Single Input Queue

Shunter uses one logical executor inbox, not independent call channels.

```go
type ExecutorCommand interface {
    isExecutorCommand()
}
```

All ordering-sensitive work enters the same queue:
- external reducer calls
- scheduled reducer firings
- OnConnect / OnDisconnect internal lifecycle calls
- subscription register / unregister commands that must be atomic with commit ordering

A single queue avoids unspecified fairness between multiple channels and gives one total order for all write-affecting operations.

```go
type Executor struct {
    inbox chan ExecutorCommand
}

func (e *Executor) run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case cmd, ok := <-e.inbox:
            if !ok {
                return
            }
            e.dispatchSafely(cmd)
        }
    }
}
```

Normative requirements:
- `inbox` MUST be bounded.
- Sending to a full inbox MAY block or return `ErrExecutorBusy`; engine configuration chooses policy.
- Closing `inbox` MUST terminate the loop cleanly; implementations MUST use the `ok` result from channel receive.

### 2.3 Queue Ordering

The executor guarantees FIFO processing in receive order from `inbox`.

This total order defines:
- reducer serialization
- relative ordering of scheduled reducers vs external reducers
- atomic subscription registration vs commit visibility
- lifecycle hook ordering relative to normal reducer calls

If two commands are accepted into `inbox`, the one received first is fully processed first.

### 2.4 Commands

At minimum the executor must support these command types:

```go
// SubscriptionID is an executor/subscription-internal uint32 allocation used
// for manager bookkeeping and deterministic fanout ordering. Client-visible
// wire correlation uses QueryID (SPEC-005).
type SubscriptionID uint32

type CallReducerCmd struct {
    Request    ReducerRequest
    ResponseCh chan<- ReducerResponse
}

// Updated 2026-04-19: set-based commands replace the
// former single-subscription RegisterSubscriptionCmd / UnregisterSubscriptionCmd.
type RegisterSubscriptionSetCmd struct {
    Request    SubscriptionSetRegisterRequest
    ResponseCh chan<- SubscriptionSetRegisterResult
}

type UnregisterSubscriptionSetCmd struct {
    ConnID     ConnectionID
    QueryID    uint32
    ResponseCh chan<- SubscriptionSetUnregisterResult
}

type DisconnectClientSubscriptionsCmd struct {
    ConnID     ConnectionID
    ResponseCh chan<- error
}

// OnConnectCmd and OnDisconnectCmd are bespoke lifecycle commands dispatched by
// the protocol layer; see §10.3 / §10.4. They are NOT encoded as CallReducerCmd
// because the sys_clients row insert (§10.3) and the guaranteed cleanup tx
// (§10.4) are not expressible through the plain reducer-call path.
type OnConnectCmd struct {
    ConnID     ConnectionID
    Identity   Identity
    Principal  AuthPrincipal
    ResponseCh chan<- ReducerResponse
}

type OnDisconnectCmd struct {
    ConnID     ConnectionID
    Identity   Identity
    Principal  AuthPrincipal
    ResponseCh chan<- ReducerResponse
}
```

`SubscriptionSetRegisterRequest`, `SubscriptionSetRegisterResult`, and `SubscriptionSetUnregisterResult` are defined in SPEC-004 §4.1.

Scheduled reducers use `CallReducerCmd` with `Source = CallSourceScheduled`. Lifecycle reducers (`OnConnect` / `OnDisconnect`) do not fit the `CallReducerCmd` shape and use their own command types above; the executor treats their post-commit work as `CallSourceLifecycle` internally (§10.3, §10.4). v1 has no `init`/`update` lifecycle command (see SPEC-006 §9).

### 2.5 Read-Only Access

There are two classes of reads:

1. Pure read-only queries that do not need atomic registration semantics may use `CommittedState.Snapshot()` directly.
2. Reads that must be atomic with subscription registration or commit ordering MUST execute through the executor queue.

Specifically:
- initial subscription query execution and subscription registration MUST occur inside one executor command
- ad-hoc read-only inspection used only for debugging or non-atomic API endpoints MAY use direct snapshots

This resolves the atomicity requirement from SPEC-004: registration-sensitive reads are executor-serialized, not out-of-band.

---

## 3. Reducers

### 3.1 Runtime Reducer Signature

**Go-package home.** `ReducerHandler`, `ReducerContext`, `CallerContext`, and the reducer-facing interfaces (`ReducerDB`, `ReducerScheduler`) are declared in `types/reducer.go`. SPEC-003 owns the contract; `types/` is the canonical symbol home. SPEC-006 re-exports these for ergonomic builder/registration call sites (see SPEC-006 §1 footnote); the canonical declaration is not in `schema/`. Identifier types shared with SPEC-001 (`Identity`, `ConnectionID`, `TxID`, `ScheduleID`, `SubscriptionID`) live in `types/types.go` — see SPEC-001 §1.1.

The executor invokes reducers through a raw runtime signature:

```go
type ReducerHandler func(ctx *ReducerContext, argBSATN []byte) ([]byte, error)
```

Parameters:
- `ctx`: transaction-scoped database and call metadata
- `argBSATN`: BSATN-encoded reducer arguments exactly as supplied by the caller

Returns:
- `[]byte`: optional BSATN-encoded reducer return value; nil means no return payload
- `error`: aborts the transaction if non-nil

"BSATN" is the binary encoding defined in SPEC-002 §3.3; the name is non-standard and not byte-compatible with SpacetimeDB's `bsatn` crate — see the canonical disclaimer in **SPEC-002 §3.1**.

SPEC-006 may provide typed registration helpers that decode arguments into Go structs and re-encode return values, but the executor runtime contract is byte-oriented and fully specified here. Typed adapters are out of scope for v1 (SPEC-006 §4.3). The sentinel for typed-adapter argument-decode failures, reserved as `ErrReducerArgsDecode`, is owned by SPEC-006 rather than SPEC-003. SPEC-003 classifies any non-nil error returned by a `ReducerHandler` as `StatusFailedUser` (§11) regardless of sentinel identity; a future typed adapter does not require a dedicated executor-level catalog entry.

### 3.2 Reducer Registration

Reducers are registered before engine start.

```go
type RegisteredReducer struct {
    Name       string
    Handler    ReducerHandler
    Lifecycle  LifecycleKind // None, OnConnect, OnDisconnect
}
```

Rules:
- Reducer names MUST be unique.
- Lifecycle reducer names are reserved and may not be registered as normal reducers.
- Registration is immutable after executor start.

### 3.3 ReducerRequest

```go
type ReducerRequest struct {
    ReducerName string
    Args        []byte
    Caller      CallerContext
    Source      CallSource
    ScheduleID  ScheduleID // populated iff Source == CallSourceScheduled
    // IntendedFireAt is the scheduler's target fire time in Unix nanoseconds.
    // Populated iff Source == CallSourceScheduled.
    IntendedFireAt int64
}

type AuthPrincipal struct {
    Issuer      string
    Subject     string
    Audience    []string
    Permissions []string
}

type CallerContext struct {
    Identity            Identity
    ConnectionID        ConnectionID // zero for internal callers
    Principal           AuthPrincipal
    Timestamp           time.Time
    Permissions         []string
    AllowAllPermissions bool
}

type CallSource int
const (
    CallSourceExternal CallSource = iota
    CallSourceScheduled
    CallSourceLifecycle
)

type ReducerResponse struct {
    Status      ReducerStatus
    Error       error
    ReturnBSATN []byte
    TxID        TxID
}

type ReducerStatus int
const (
    StatusCommitted ReducerStatus = iota
    StatusFailedUser
    StatusFailedPanic
    StatusFailedInternal
    StatusFailedPermission
)
```

The executor, not the caller, sets `Caller.Timestamp` when the command is dequeued. Caller-provided timestamps must be ignored. The executor copies caller slices before invoking reducer code. `Caller.Principal` is external-auth context only; reducer permission admission uses `Caller.Permissions` / `AllowAllPermissions`. For serialization, logging, and protocol/wire surfaces, only the UTC wall-clock portion of `time.Time` is meaningful; Go's monotonic component is process-local and must be discarded outside the executor process.

### 3.4 ReducerContext

```go
type ReducerContext struct {
    ReducerName string
    Caller      CallerContext
    DB          *Transaction
    Scheduler   SchedulerHandle
}
```

`ReducerContext` is valid only during the synchronous reducer invocation.

Reducers may:
- read and write tables through `DB`
- inspect built-in tables such as `sys_clients`
- create or cancel schedules through `SchedulerHandle`
- return an error to abort the transaction

Reducers may not:
- call other reducers synchronously
- mutate committed state except via `DB`
- retain `ReducerContext`, `Transaction`, iterators, snapshots, row references, or scheduler internals after returning
- use `DB` or any transaction-owned object from another goroutine

### 3.5 No-I/O Constraint

The executor does not sandbox reducer code. Therefore “no I/O” is a programming rule, not a runtime-enforced guarantee.

Normative rule:
- Reducers MUST NOT perform blocking network, disk, or RPC I/O on the executor goroutine.

Reason:
- blocking reducer code stalls all queued work
- external side effects cannot be rolled back atomically with the transaction

If applications need background work, they must spawn it outside the reducer and later submit a new reducer call with the result. They MUST NOT capture `ReducerContext` or `Transaction` in that background goroutine.

---

## 4. Transaction Lifecycle

### 4.1 Dispatch Envelope

Each command is processed inside a top-level recovery envelope:

```go
func (e *Executor) dispatchSafely(cmd ExecutorCommand) {
    defer func() {
        if r := recover(); r != nil {
            e.handleDispatchPanic(r)
        }
    }()
    e.dispatch(cmd)
}
```

This protects the executor goroutine itself from unexpected panics.

### 4.2 Begin

For a reducer call:
1. Look up the registered reducer.
2. Construct `CallerContext` with dequeue-time timestamp.
3. Create a fresh `Transaction` from current committed state.
4. Construct `ReducerContext`.

```go
tx := store.NewTransaction(committed, schema)
ctx := &ReducerContext{
    ReducerName: req.ReducerName,
    Caller: CallerContext{
        Identity:            req.Caller.Identity,
        ConnectionID:        req.Caller.ConnectionID,
        Principal:           req.Caller.Principal.Copy(),
        Timestamp:           time.Now().UTC(),
        Permissions:         append([]string(nil), req.Caller.Permissions...),
        AllowAllPermissions: req.Caller.AllowAllPermissions,
    },
    DB:        tx,
    Scheduler: e.scheduler.Handle(),
}
```

### 4.3 Execute

Reducer execution occurs inside a reducer-local panic recovery block:

```go
var (
    ret []byte
    err error
    panicked any
)

func() {
    defer func() {
        if r := recover(); r != nil {
            panicked = r
        }
    }()
    ret, err = reducer.Handler(ctx, req.Args)
}()
```

Outcomes:
- `err != nil` → user failure, rollback
- `panicked != nil` → reducer panic, rollback, return `StatusFailedPanic`
- neither → attempt commit

### 4.4 Commit

On successful reducer return, the executor calls store commit.

```go
changeset, commitErr := store.Commit(committed, tx)
if commitErr != nil {
    // classify and respond per §4.6
}
txID := e.allocateTxID() // atomic increment of executor-owned counter (§13.2)
changeset.TxID = txID
```

Required invariant from SPEC-001:
- `Commit` is atomic from the executor’s perspective: if it returns an error, committed state MUST remain unchanged.
- `store.Commit` does NOT allocate or return `TxID` (Model A). The executor owns the monotonic counter and stamps `changeset.TxID` after commit returns, before handing the changeset to durability and subscription evaluation.

Commit sequence:
1. validate and finalize tx-local state
2. apply committed-state mutations atomically
3. store returns `(*Changeset, error)` with `changeset.TxID` zero-valued
4. executor allocates `TxID` from its monotonic counter and stamps it on the returned changeset
5. `Changeset` is then read-only for durability and subscription consumers

### 4.5 Rollback

If reducer execution failed or panicked:
- discard `Transaction`
- do not mutate committed state
- do not enqueue durability
- do not evaluate subscriptions

Rollback is O(1) disposal of transaction-local state.

### 4.6 Commit Failure

If `store.Commit` returns an error:
- respond with `StatusFailedInternal` if the error is engine/internal
- respond with `StatusFailedUser` if the error is a user-visible invariant violation such as uniqueness failure
- do not run post-commit steps

The store MUST ensure failed commit leaves no partial committed mutations.

---

## 5. Post-Commit Pipeline

After commit succeeds, the executor MUST perform these steps in this exact order:

1. Hand the committed `Changeset` to durability
2. Evaluate subscriptions synchronously against a stable committed read view
3. Hand all resulting deltas to the client-protocol layer for delivery
4. Send reducer response to the caller
5. If the fan-out worker has signaled dropped clients (via a non-blocking check of `SubscriptionManager.DroppedClients()`), call `SubscriptionManager.DisconnectClient(connID)` for each dropped connection before dequeueing the next command.
6. Dequeue the next executor command

### 5.1 Durability Before Subscription Evaluation

The executor hands the committed changeset to durability before subscription evaluation.

This step does not wait for fsync. It only waits, if necessary, for the durability subsystem to accept the work item into its bounded queue.

Therefore:
- commit visibility is ahead of durable persistence
- client-visible success and delta delivery do not imply disk durability
- crash recovery may lose recently committed transactions that were accepted by the executor but not yet persisted

### 5.2 Stable Read View for Subscription Evaluation

The executor MUST pass a stable committed read view to the subscription subsystem.

`CommittedReadView` is defined in SPEC-001 §7.2. The executor acquires a snapshot after `EnqueueCommitted` returns (queue admission only) and before `EvalAndBroadcast`, then closes it before the reducer response is sent.

The view must remain valid for the full duration of subscription evaluation.

### 5.3 Subscription Evaluation is Synchronous

Subscription evaluation and delta handoff are synchronous with respect to executor ordering.

This means the executor does not begin the next command until:
- all affected subscriptions have been evaluated
- the resulting deltas have been handed to the protocol layer

The protocol layer may still send bytes asynchronously after handoff. The executor only waits for successful ownership transfer to protocol buffers/queues, not for network flush.

**Design tradeoff — correctness over throughput:** Synchronous evaluation guarantees strict ordering: a client cannot observe commit N+1's delta before N's. The cost is that the executor is occupied for the full duration of evaluation before it dequeues the next command. For v1's target workload (SodorYard: fewer than 10 subscriptions, each evaluating a small number of predicates) evaluation is well under the <20 ms target and has negligible impact on reducer throughput. This tradeoff would not hold for high-subscription workloads such as a production multiplayer game with thousands of active subscriptions, where async delta pipelines become necessary. That case is out of scope for v1.

### 5.4 Failure in Post-Commit Steps

Once commit succeeds, the transaction is already visible in memory and cannot be rolled back.

Therefore:
- reducer panics before commit are recoverable per-request failures
- panics or unrecoverable internal errors after commit are executor-fatal errors

Normative rule:
- If the durability subsystem, the subscription manager, or the delta-handoff path **panics** or signals an invariant violation after commit, the executor MUST transition the engine into a failed state and reject future write commands until restart.
- **Per-query evaluation errors are NOT fatal.** SPEC-004 §11.1 scopes these: a single subscription's delta computation failing (corrupted index, type mismatch, join-index resolution) is logged, surfaced to affected clients via `SubscriptionError`, and the offending query is unregistered — the executor continues. The post-commit pipeline as a whole does not fail; only that query's subscribers see failure.
- The dividing line: if `SubscriptionManager.EvalAndBroadcast` returns normally, the executor continues. If it panics, the executor is fatal. Internal per-query failures that the manager catches and converts to `SubscriptionError` deliveries are normal returns.

Reason:
- post-commit subsystem failure leaves uncertain client-observable side effects; continuing as if nothing happened risks reordering or silent loss
- per-query errors are localized and communicated to exactly the affected clients; they do not compromise the executor's ordering invariants

---

## 6. TxID

Each committed transaction receives a monotonically increasing `TxID`.

```go
type TxID uint64 // declared in the `types/` Go package; SPEC-003 owns the contract
```

Rules:
- `TxID` starts at 1
- `TxID(0)` means “no committed transaction” / bootstrap / initial state
- `TxID` order is commit order as observed by the executor
- The executor owns allocation (Model A). `store.Commit` returns `(*Changeset, error)`; the executor stamps `changeset.TxID` before the post-commit pipeline. SPEC-001 §5.6 and §6.1 describe the stamping contract.

Uses:
- commit log sequencing (SPEC-002)
- delta messages and client gap detection (SPEC-005)
- durability-progress reporting

**Recovery handoff:** `OpenAndRecover` (SPEC-002 §6.1) returns `(committed, maxAppliedTxID, nil)`. The executor receives `maxAppliedTxID` at startup and initializes its internal counter to `maxAppliedTxID + 1`. The next committed transaction receives `maxAppliedTxID + 1`. If `maxAppliedTxID` is 0 (empty store), the first transaction receives TxID 1.

---

## 7. Durability Interface

The executor interacts with the durability subsystem through this interface:

```go
type DurabilityHandle interface {
    // EnqueueCommitted blocks only for bounded-queue backpressure.
    // It MUST NOT drop accepted commits silently.
    EnqueueCommitted(txID TxID, changeset *Changeset)

    // DurableTxID returns the highest tx known durable on disk.
    DurableTxID() TxID

    // WaitUntilDurable returns a channel that receives txID when
    // the transaction is durably persisted. Used by the subscription
    // fan-out worker (SPEC-004 §8) to gate confirmed-read delivery.
    // If txID == 0, returns nil. If txID is already durable, the
    // returned channel is pre-filled and closed (non-blocking).
    WaitUntilDurable(txID TxID) <-chan TxID

    // Close stops new admissions, drains queued work, performs a final fsync,
    // and shuts down the durability worker.
    // Returns the final durable TxID and any latched fatal error.
    Close() (TxID, error)
}
```

Contract:
- `EnqueueCommitted` is synchronous queue admission, not fsync completion
- queue admission MAY block when the durability worker is backpressured
- once `EnqueueCommitted` returns, durability owns the changeset
- if durability has already latched a fatal error, `EnqueueCommitted` MUST panic immediately
- `WaitUntilDurable(0)` returns nil; `WaitUntilDurable(txID>0)` never blocks and always returns a channel (ready or pending); the channel receives exactly one value and is closed
- executor shutdown must stop accepting new write commands before the durability subsystem is torn down
- `Close` is for shutdown/lifecycle management, not for the post-commit hot path

This intentionally avoids a post-commit recoverable error path. A live executor must be paired with a live durability handle.

---

## 8. Subscription Interface

The executor depends on a subscription manager, not just a post-commit callback.

```go
// Updated 2026-04-19: set-based RegisterSet / UnregisterSet
// replace the former single-subscription Register / Unregister.
type SubscriptionManager interface {
    RegisterSet(req SubscriptionSetRegisterRequest, view CommittedReadView) (SubscriptionSetRegisterResult, error)
    UnregisterSet(connID ConnectionID, queryID uint32, view CommittedReadView) (SubscriptionSetUnregisterResult, error)
    DisconnectClient(connID ConnectionID) error
    EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView, meta PostCommitMeta)
    DroppedClients() <-chan ConnectionID   // non-blocking; executor drains after each commit
}
```

`SubscriptionSetRegisterRequest`, `SubscriptionSetRegisterResult`, and `SubscriptionSetUnregisterResult` are defined in SPEC-004 §4.1. `PostCommitMeta` is declared in SPEC-004 §10.1 and carries executor-owned delivery metadata (`TxDurable`, `CallerConnID`, `CallerOutcome`) into the evaluator.

Rules:
- `RegisterSet` MUST be called from an executor command so initial query execution and registration are atomic with commit ordering
- the `CommittedReadView` passed to `RegisterSet` is owned by the caller for the duration of the call only; `SubscriptionManager` MUST NOT retain it past return and MUST copy any snapshot-derived state it wants to keep
- `EvalAndBroadcast` runs synchronously inside the post-commit pipeline
- The executor MUST populate `meta.TxDurable` with a non-nil channel obtained from `DurabilityHandle.WaitUntilDurable(txID)` for every post-commit invocation (see SPEC-004 §10.1 for the TxDurable-on-empty-fanout rule)
- `DisconnectClient` removes all subscriptions for a client when the protocol layer reports disconnect

---

## 9. Scheduled Reducers

### 9.1 Design Goal

Scheduled reducers must survive process restart and must not be lost merely because the process crashes between timer fire and reducer completion.

### 9.2 Durable Representation

Pending schedules live in a built-in table:

```go
sys_scheduled {
    schedule_id:     uint64  primarykey autoincrement
    reducer_name:    string
    args:            bytes
    next_run_at_ns:  int64
    repeat_ns:       int64   // 0 = one-shot
}
```

The in-memory scheduler is only a cache of future wakeups. `sys_scheduled` is the source of truth and is replayed on startup.

### 9.3 Scheduling API

```go
type SchedulerHandle interface {
    Schedule(reducerName string, args []byte, at time.Time) (ScheduleID, error)
    ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error)
    Cancel(id ScheduleID) (bool, error)
}
```

Scheduling and cancellation are transactional DB mutations. If the surrounding reducer rolls back, schedule changes roll back too.

`ScheduleRepeat` has one first-fire policy in v1: the first firing occurs at `now + interval`. There is no separate first-fire timestamp parameter in v1; a future `ScheduleRepeatAt(...)`-style variant is deferred.

### 9.4 Firing Semantics

When a schedule becomes due, the scheduler enqueues an internal reducer call into the executor inbox.

The schedule row MUST remain present until the scheduled reducer transaction commits successfully.

One-shot schedule success path:
- execute reducer
- in the same transaction, delete the corresponding `sys_scheduled` row
- commit once

Repeating schedule success path:
- execute reducer
- in the same transaction, advance `next_run_at_ns` to the next intended fire time
- commit once

Failure path:
- if reducer returns error or panics, the transaction rolls back
- `sys_scheduled` remains unchanged
- the schedule is retried after restart or explicit rescan

Scheduler pickup latency:
- v1 correctness does not depend on an explicit post-commit scheduler wakeup hook
- newly inserted `sys_scheduled` rows MUST be observed on the scheduler's next committed-state rescan even if no `Notify()` path is wired
- an implementation MAY add a non-blocking wakeup/notify optimization to reduce latency, but that optimization is not part of the minimum v1 correctness contract

Crash semantics:
- exactly-once execution is not guaranteed across crash because commits may be visible in memory before durable persistence
- relative to durable state, scheduled reducers are at-least-once

### 9.5 Repeat Drift

For repeating schedules, v1 uses fixed-rate semantics based on the prior scheduled fire time, not “current completion time plus interval.”

If an invocation intended for `T` has interval `I`, the next row value becomes `T + I`, even if execution started or finished later than `T`.

This avoids unbounded drift under load.

---

## 10. Built-In Lifecycle Reducers

The v1 lifecycle reducer set is exactly `OnConnect` and `OnDisconnect`. There is **no `init` or `update`** lifecycle hook; SPEC-006 §9 owns that deferral. Lifecycle reducers are dispatched via the bespoke `OnConnectCmd` / `OnDisconnectCmd` executor commands declared in §2.4 — not via `CallReducerCmd` — because the transaction shapes in §10.3 and §10.4 include synthetic `sys_clients` row writes that `CallReducerCmd` does not express.

### 10.1 Registration

Applications may optionally register:

```go
type LifecycleKind int
const (
    LifecycleNone LifecycleKind = iota
    LifecycleOnConnect
    LifecycleOnDisconnect
)
```

The names `OnConnect` and `OnDisconnect` are reserved and may not be registered as normal public reducers. Schema-level enforcement of this rule lives in SPEC-006 §9.

### 10.2 sys_clients Table

```go
sys_clients {
    connection_id: bytes(16) primarykey
    identity:      bytes      // 32 bytes; see SPEC-001 §2.4
    connected_at:  int64      // Unix nanoseconds
}
```

Rules:
- rows are inserted on accepted connect
- rows are deleted on disconnect cleanup
- reducer code may read this table
- `sys_clients` changes appear in normal changesets and can trigger subscription deltas

### 10.3 OnConnect

OnConnect is dispatched by the protocol layer via `OnConnectCmd` (§2.4) before the client may issue normal reducer calls.

Transaction semantics:
- open a new transaction
- insert `sys_clients` row
- run `OnConnect` reducer if registered, with `CallerContext.Source = CallSourceLifecycle`
- if reducer commits, keep row and allow connection
- if reducer returns error or panics, roll back both reducer changes and row insertion, then reject the connection
- the commit allocates one `TxID` (no TxID is consumed on rollback; see §6)
- on commit success, run the §5 post-commit pipeline with `source = CallSourceLifecycle` so subscribers see the `sys_clients` insert

### 10.4 OnDisconnect

OnDisconnect is dispatched by the protocol layer via `OnDisconnectCmd` (§2.4) after the client is considered gone.

Disconnect cannot be vetoed.

Success path:
- open a new transaction
- run `OnDisconnect` reducer with `CallerContext.Source = CallSourceLifecycle`
- delete `sys_clients` row in the same transaction
- commit once; the commit allocates one `TxID` (§6)
- run the §5 post-commit pipeline with `source = CallSourceLifecycle`

Failure path (reducer returns error or panics):
- roll back the reducer transaction (no `TxID` is allocated; in-progress sequence IDs from that tx are discarded per SPEC-001 normal rollback semantics)
- log the reducer failure
- open a **fresh cleanup transaction** that only deletes the `sys_clients` row
- commit the cleanup transaction; the commit allocates one `TxID` — this is the sole `TxID` consumed by a failed OnDisconnect
- run the §5 post-commit pipeline for the cleanup commit with `source = CallSourceLifecycle` so subscribers still see the `sys_clients` delete

No reducer runs inside the cleanup transaction; `CallerContext.Source = CallSourceLifecycle` is stamped on the post-commit pipeline only.

**Pinned contracts (resolving SPEC-AUDIT SPEC-003 §1.5):**
1. **CallSource.** `CallSourceLifecycle` is reused for the cleanup post-commit pipeline. The enum describes how a surrounding reducer call was framed; the cleanup commit is framed as the tail of the same OnDisconnect operation, so reusing `CallSourceLifecycle` matches that framing rather than inventing a separate `CallSourceSystem` value.
2. **TxID allocation.** A rolled-back reducer transaction allocates no `TxID` (stamping is tied to commit, §6). The cleanup commit allocates exactly one. Sequence-ID / ScheduleID gaps that the rolled-back reducer may have produced follow SPEC-001 normal rollback behavior — no compensating mechanism.
3. **Cleanup panics.** A panic during the cleanup-commit's post-commit pipeline follows §5.4 and is executor-fatal — identical treatment to any other post-commit panic. A panic inside the cleanup transaction itself (pre-commit) is logged; the `sys_clients` row may leak until the next startup dangling-client sweep (SPEC-AUDIT SPEC-003 §2.2 tracks the sweep owner) but the executor continues.
4. **Fatal-state interaction.** `OnDisconnectCmd` is **not** short-circuited when the executor has latched `fatal = true`. The cleanup commit MUST still attempt, because leaking live `sys_clients` rows is a worse outcome than rejecting new writes — subscribers to `sys_clients` would otherwise observe phantom connected clients indefinitely. `CallReducerCmd` remains short-circuited in fatal state (existing behavior, §5.4); only the lifecycle cleanup path opts in to this exception.

This guarantees eventual client-row cleanup even when the reducer fails halfway through.

### 10.6 Startup Dangling-Client Sweep

After recovery reconstructs committed state and after the scheduler replays `sys_scheduled`, the engine MUST sweep any surviving `sys_clients` rows before it accepts external commands.

Sweep contract:
- read committed `sys_clients` rows from recovered state
- for each surviving row, enqueue or invoke the OnDisconnect cleanup path so the row is deleted through the normal lifecycle semantics
- complete the sweep before the first external reducer or subscription-registration command is admitted

This is the recovery-side complement to §10.4's guaranteed cleanup rule. If the process crashes while clients are connected, the next startup MUST not leave phantom connected clients in `sys_clients` indefinitely.

### 10.7 Direct Invocation Protection

External callers may not invoke lifecycle reducers by name.

If a request names a lifecycle reducer and `Source != CallSourceLifecycle`, the executor rejects it with `ErrLifecycleReducer` before transaction begin.

---

## 11. Error Catalog

| Error | Meaning |
|---|---|
| `ErrReducerNotFound` | No reducer registered with the given name |
| `ErrLifecycleReducer` | External caller attempted to invoke a lifecycle reducer |
| `ErrExecutorBusy` | Bounded inbox is full and engine is configured to reject rather than block |
| `ErrExecutorShutdown` | Executor is stopping or has stopped |
| `ErrReducerPanic` | Reducer panicked before commit |
| `ErrCommitFailed` | Store commit rejected the transaction |
| `ErrExecutorFatal` | Executor entered failed state after a post-commit fatal error |

Any non-nil error returned by a reducer handler (including future typed-adapter decode failures wrapping SPEC-006's reserved `ErrReducerArgsDecode`) is classified as `StatusFailedUser` via the generic handler-error path. SPEC-003 does not declare a dedicated decode sentinel; see SPEC-006 §4.3 and §3.1 above.

Additional catalog rules:
- `ErrReducerNotFound` is returned before transaction begin and is surfaced with `StatusFailedInternal` in the reducer-response path. v1 treats an unknown reducer name as a deployed-schema / caller-contract mismatch rather than a store invariant violation.
- No dedicated sentinel is provided for misuse of a per-call `SchedulerHandle` after the reducer returns, or for attempted schema mutation under a running executor. Those are programming/engine-contract violations handled by freeze-time rules and general implementation logging rather than executor-level runtime classification.

---

## 12. Reference-Informed Shunter Decisions

### 12.1 Fixed-rate repeat semantics vs explicit reducer-driven reschedule

Unlike SpacetimeDB's explicit-reschedule model, Shunter's `ScheduleRepeat` is system-managed: repeating schedules advance automatically from `intended_fire_time + repeat_ns`. Reducers do not need to re-register themselves after each firing; they stop the repeat by calling `Cancel(scheduleID)` or by removing the row through normal transactional logic.

### 12.2 Bounded executor inbox vs unbounded dispatch queue

SpacetimeDB uses an effectively unbounded reducer-dispatch queue. Shunter bounds the executor inbox and exposes backpressure / reject-on-full policy explicitly. This prevents OOM-under-flood at the cost of caller-visible blocking or `ErrExecutorBusy` responses.

### 12.3 Dequeue-time timestamp stamping vs enqueue-time caller stamping

Shunter stamps `CallerContext.Timestamp` when the command is dequeued, not when it is submitted. This keeps timestamps aligned with executor ordering rather than caller clock or queue residence time.

### 12.4 Post-commit panic fatality with localized per-query recovery

v1 treats any post-commit panic or invariant-violation signal from durability, snapshot acquisition, or the subscription manager as executor-fatal. The one deliberate exception is SPEC-004's localized per-query evaluation errors: if the manager catches an individual query failure, converts it into `SubscriptionError`, and returns normally, the executor continues. This is stricter than SpacetimeDB's more selective recovery behavior.

### 12.5 Scheduled-row mutation is atomic with reducer writes

Shunter deletes or advances `sys_scheduled` in the same transaction as the scheduled reducer's writes. A failed reducer therefore leaves the row pending for retry instead of losing the scheduled invocation. The tradeoff is that a persistently failing scheduled reducer can consume executor time on every rescan until an operator fixes the reducer or cancels the schedule.

---

## 13. Interfaces to Other Subsystems

### 13.1 SPEC-001 (Store)

The executor requires:

```go
func NewTransaction(committed *CommittedState, schema SchemaRegistry) *Transaction
func Commit(committed *CommittedState, tx *Transaction) (changeset *Changeset, err error)
func Rollback(tx *Transaction)
func (cs *CommittedState) Snapshot() CommittedReadView
```

`Commit` returns `(*Changeset, error)` only. The executor allocates `TxID` (Model A; §4.4, §6, §13.2) and stamps `changeset.TxID` before the post-commit pipeline.

Required store guarantees:
- failed commit leaves committed state unchanged
- `Rollback(tx)` discards transaction-local state immediately; the executor MUST call it on every pre-commit failure path rather than relying on GC alone
- snapshot/read-view lifetime is explicit and releasable
- snapshot operations are read-only and safe for subscription evaluation

### 13.2 SPEC-002 (Commit Log)

The executor depends on `DurabilityHandle` from §7.

SPEC-002 must implement:
- bounded queue admission with backpressure
- durable tx tracking
- crash recovery from last durable tx, not last in-memory committed tx

SPEC-002 provides `max_applied_tx_id` from `OpenAndRecover`. The engine's startup path MUST pass that recovered value into executor construction/initialization before first accept. Scheduler replay and the §10.6 dangling-client sweep both happen after recovery and before the executor begins admitting external commands.

### 13.3 SPEC-004 (Subscriptions)

The executor depends on `SubscriptionManager` from §8.

SPEC-004 must assume:
- initial subscription registration is executed on the executor, not concurrently outside it
- post-commit evaluation receives a stable committed read view and a committed changeset
- delta handoff is synchronous with respect to executor ordering

### 13.4 SPEC-005 (Protocol)

SPEC-005 must route through the executor for:
- reducer calls
- OnConnect / OnDisconnect internal commands
- subscription registration and unregistration commands

Purely observational reads that do not require atomic registration semantics may use direct snapshots.

### 13.5 SPEC-006 (Schema)

SPEC-006 must provide:
- immutable reducer registry at startup
- optional typed adapters around the raw `ReducerHandler` byte-oriented runtime signature
- lifecycle reducer declaration

The `*SchemaRegistry` value passed to `NewExecutor` is the canonical interface declared in SPEC-006 §7. It satisfies the narrower `SchemaLookup` and `IndexResolver` sub-interfaces that SPEC-004 and SPEC-005 consume, so the executor can pass the same value to subsystem constructors without adapters. Registry freeze and the full engine boot ordering are owned by SPEC-006 §5.1 / §5.2; the executor begins admitting external work only after recovery, scheduler replay, and the §10.6 dangling-client sweep complete.

---

## 14. Open Questions

Only unresolved questions that remain after this spec:

1. Should the executor reject full inbox sends (`ErrExecutorBusy`) or block by default?
   Recommendation: block by default for embedded correctness; expose reject mode as advanced config.

2. Should v1 include optional reducer execution timeouts?
   Recommendation: no timeout by default; if added, timeout applies only before commit and is implemented as reducer cancellation policy, not post-commit interruption.

3. Should ad-hoc read-only API calls share the executor queue for strict linearizability, or use direct snapshots for lower latency?
   Recommendation: direct snapshots for v1 except for registration-sensitive operations already called out above.

---

## 15. Verification

Minimum verification matrix:

| Test | Verifies |
|---|---|
| Call reducer with valid args, get committed response | Basic dispatch |
| Call reducer with malformed args, receive user-visible failure | Argument decoding path |
| Reducer inserts rows, state visible after commit | Commit success |
| Reducer returns error, state unchanged | Rollback on user error |
| Reducer panics, state unchanged, executor continues | Recoverable reducer panic |
| Commit fails uniqueness check, state unchanged | Atomic failed commit |
| Two reducer calls enqueue in order and commit in order | FIFO executor ordering |
| Subscription registration command cannot interleave with intervening commit | Atomic registration semantics |
| Post-commit evaluation sees committed rows, not tx-local rows | Read-view correctness |
| Durability queue backpressure stalls commit admission but not fsync wait | Queue-admission contract |
| Commit acknowledged, crash before durability, restart loses tx | Best-effort durability semantics |
| Panic in subscription evaluation moves executor to fatal state | Post-commit panic containment |
| One-shot scheduled reducer survives restart before firing | Schedule persistence |
| Scheduled reducer error leaves `sys_scheduled` row present | No lost schedule on failure |
| Successful one-shot scheduled reducer deletes row in same transaction | Atomic fire-and-delete |
| Repeating scheduled reducer advances `next_run_at_ns` from intended fire time | Fixed-rate repeat semantics |
| OnConnect rejection leaves no `sys_clients` row | Connect rollback |
| OnDisconnect failure still removes `sys_clients` row via cleanup tx | Disconnect cleanup guarantee |
| External call to lifecycle reducer name is rejected | Direct invocation protection |
| Closing executor inbox terminates run loop without spin | Clean shutdown |
| Request submitted after shutdown gets `ErrExecutorShutdown` | Shutdown behavior |

---

## 16. Summary of v1 Decisions

- One bounded executor inbox for all ordering-sensitive work
- Raw runtime reducer signature is `func(ctx, argBSATN) (returnBSATN, error)`
- Initial subscription registration is executor-serialized
- Durability handoff happens before subscription evaluation, but durability remains async to disk
- Scheduled reducers are durable and not deleted until successful transaction commit
- OnDisconnect cannot veto disconnect; cleanup is guaranteed even after reducer failure
- Reducer panics before commit are recoverable; post-commit panics are executor-fatal

---

## 17. Performance Targets

These are engineering targets, not correctness requirements.

| Operation | Target |
|---|---|
| Dequeue + begin empty transaction | < 5 µs |
| Empty reducer dispatch | < 10 µs |
| Commit of 100 inserts, excluding subscription eval | < 500 µs |
| Rollback of failed reducer | < 20 µs |
| Schedule wakeup to executor enqueue | < 10 ms |
