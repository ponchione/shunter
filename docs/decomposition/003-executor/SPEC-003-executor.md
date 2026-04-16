# SPEC-003 — Transaction Executor

**Status:** Draft  
**Depends on:** SPEC-001 (In-Memory Store), SPEC-004 (Subscription Evaluator), SPEC-006 (Schema Definition)  
**Depended on by:** SPEC-002 (Commit Log), SPEC-005 (Client Protocol)

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
// SubscriptionID is a client-chosen uint32 that uniquely identifies one subscription
// within an active connection. Matches the subscription_id field on the wire (SPEC-005).
type SubscriptionID uint32

type CallReducerCmd struct {
    Request    ReducerRequest
    ResponseCh chan<- ReducerResponse
}

type RegisterSubscriptionCmd struct {
    Request    SubscriptionRegisterRequest
    ResponseCh chan<- SubscriptionRegisterResult
}

type UnregisterSubscriptionCmd struct {
    ConnID         ConnectionID
    SubscriptionID SubscriptionID
    ResponseCh     chan<- error
}

type DisconnectClientSubscriptionsCmd struct {
    ConnID     ConnectionID
    ResponseCh chan<- error
}
```

`SubscriptionRegisterRequest` is defined in SPEC-004 §4.1.

Scheduled reducers and lifecycle reducers use `CallReducerCmd` with an internal call source.

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

SPEC-006 may provide typed registration helpers that decode arguments into Go structs and re-encode return values, but the executor runtime contract is byte-oriented and fully specified here.

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
}

type CallerContext struct {
    Identity     Identity
    ConnectionID ConnectionID // zero for internal callers
    Timestamp    time.Time
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
)
```

The executor, not the caller, sets `Caller.Timestamp` when the command is dequeued. Caller-provided timestamps must be ignored.

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
        Identity:     req.Caller.Identity,
        ConnectionID: req.Caller.ConnectionID,
        Timestamp:    time.Now().UTC(),
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
changeset, txID, commitErr := store.Commit(committed, tx, schema)
```

Required invariant from SPEC-001:
- `Commit` is atomic from the executor’s perspective: if it returns an error, committed state MUST remain unchanged.

Commit sequence:
1. validate and finalize tx-local state
2. apply committed-state mutations atomically
3. assign `TxID`
4. build read-only `Changeset`
5. return

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

`CommittedReadView` is defined in SPEC-001 §7.2. The executor acquires a snapshot immediately after commit and holds it for the duration of subscription evaluation, then closes it before dequeueing the next command.

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
- if durability handoff, subscription evaluation, or delta handoff panics after commit, the executor MUST transition the engine into a failed state and reject future write commands until restart

Reason:
- post-commit failure leaves uncertain client-observable side effects; continuing as if nothing happened risks reordering or silent loss

---

## 6. TxID

Each committed transaction receives a monotonically increasing `TxID`.

```go
type TxID uint64
```

Rules:
- `TxID` starts at 1
- `TxID(0)` means “no committed transaction” / bootstrap / initial state
- `TxID` order is commit order as observed by the executor

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
- executor shutdown must stop accepting new write commands before the durability subsystem is torn down
- `Close` is for shutdown/lifecycle management, not for the post-commit hot path

This intentionally avoids a post-commit recoverable error path. A live executor must be paired with a live durability handle.

---

## 8. Subscription Interface

The executor depends on a subscription manager, not just a post-commit callback.

```go
type SubscriptionManager interface {
    Register(req SubscriptionRegisterRequest, view CommittedReadView) (SubscriptionRegisterResult, error)
    Unregister(connID ConnectionID, subscriptionID SubscriptionID) error
    DisconnectClient(connID ConnectionID) error
    EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView)
    DroppedClients() <-chan ConnectionID   // non-blocking; executor drains after each commit
}
```

`SubscriptionRegisterRequest` and `SubscriptionRegisterResult` are defined in SPEC-004 §4.1.

Rules:
- `Register` MUST be called from an executor command so initial query execution and registration are atomic with commit ordering
- `EvalAndBroadcast` runs synchronously inside the post-commit pipeline
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

Crash semantics:
- exactly-once execution is not guaranteed across crash because commits may be visible in memory before durable persistence
- relative to durable state, scheduled reducers are at-least-once

### 9.5 Repeat Drift

For repeating schedules, v1 uses fixed-rate semantics based on the prior scheduled fire time, not “current completion time plus interval.”

If an invocation intended for `T` has interval `I`, the next row value becomes `T + I`, even if execution started or finished later than `T`.

This avoids unbounded drift under load.

---

## 10. Built-In Lifecycle Reducers

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

The names `OnConnect` and `OnDisconnect` are reserved and may not be registered as normal public reducers.

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

OnConnect is invoked by the protocol layer through an internal executor command before the client may issue normal reducer calls.

Transaction semantics:
- insert `sys_clients` row
- run OnConnect reducer if registered
- if reducer commits, keep row and allow connection
- if reducer returns error or panics, roll back both reducer changes and row insertion, then reject the connection

### 10.4 OnDisconnect

OnDisconnect is invoked by the protocol layer through an internal executor command after the client is considered gone.

Disconnect cannot be vetoed.

Success path:
- run OnDisconnect reducer
- delete `sys_clients` row in the same transaction
- commit once

Failure path:
- if OnDisconnect returns error or panics, roll back reducer writes
- then run a separate internal cleanup transaction that deletes the `sys_clients` row anyway
- log the reducer failure

This guarantees eventual client-row cleanup even when the reducer fails halfway through.

### 10.5 Direct Invocation Protection

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

---

## 12. Performance Constraints

These are engineering targets, not correctness requirements.

| Operation | Target |
|---|---|
| Dequeue + begin empty transaction | < 5 µs |
| Empty reducer dispatch | < 10 µs |
| Commit of 100 inserts, excluding subscription eval | < 500 µs |
| Rollback of failed reducer | < 20 µs |
| Schedule wakeup to executor enqueue | < 10 ms |

---

## 13. Interfaces to Other Subsystems

### 13.1 SPEC-001 (Store)

The executor requires:

```go
func NewTransaction(committed *CommittedState, schema SchemaRegistry) *Transaction
func Commit(committed *CommittedState, tx *Transaction, schema SchemaRegistry) (changeset *Changeset, txID TxID, err error)
func (cs *CommittedState) Snapshot() CommittedReadView
```

Required store guarantees:
- failed commit leaves committed state unchanged
- snapshot/read-view lifetime is explicit and releasable
- snapshot operations are read-only and safe for subscription evaluation

### 13.2 SPEC-002 (Commit Log)

The executor depends on `DurabilityHandle` from §7.

SPEC-002 must implement:
- bounded queue admission with backpressure
- durable tx tracking
- crash recovery from last durable tx, not last in-memory committed tx

SPEC-002 provides `max_applied_tx_id` from `OpenAndRecover`. The executor stores this value and increments it atomically on each successful commit to assign the next TxID.

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

The `*SchemaRegistry` value passed to `NewExecutor` is the canonical interface declared in SPEC-006 §7. It satisfies the narrower `SchemaLookup` and `IndexResolver` sub-interfaces that SPEC-004 and SPEC-005 consume, so the executor can pass the same value to subsystem constructors without adapters. Registry freeze and the full engine boot ordering are owned by SPEC-006 §5.1 / §5.2; the executor is constructed in step 4 of that sequence and may treat the registry as fully populated and immutable for its lifetime.

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
