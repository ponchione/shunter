# Hosted Runtime V1-D Runtime Lifecycle Ownership Implementation Plan

> For Hermes: use the subagent-driven-development skill to execute this plan task-by-task if the user asks for implementation.

Historical status: landed; retained as planning context only.
Scope: V1-D only; planning artifact, not current implementation target.

Goal: add `shunter.Runtime` lifecycle ownership so a V1-C-built runtime can start and stop its long-lived kernel workers safely, while keeping network serving, `ListenAndServe`, `HTTPHandler`, and local reducer/query APIs deferred to later V1 slices.

Architecture: `Build` remains the construction/recovery phase from V1-C. `Runtime.Start(ctx)` performs startup checks, creates and wires lifecycle-owned subsystem handles, runs executor startup sequencing, launches owned background goroutines, and marks the runtime ready. `Runtime.Close()` performs idempotent reverse-order shutdown. V1-D derives behavior from kernel package contracts and tests, not from example/demo code.

Tech stack: Go, root package `github.com/ponchione/shunter`, existing `schema`, `store`, `commitlog`, `executor`, `subscription`, and `types` packages, RTK-wrapped Go toolchain commands.

---

## Current grounded context

Read and verified while writing this plan:

- `docs/specs/hosted-runtime-version-phases.md` defines V1-D as:
  - `Runtime.Start(ctx)`
  - `Runtime.Close()`
  - readiness/health inspection
  - success/failure/cancellation/repeated-call tests
  - internal goroutine ownership and shutdown order
  - fatal subsystem state observability
- `docs/specs/hosted-runtime-v1-contract.md` says `Runtime` is the stable owner object for lifecycle and later network/local/export surfaces.
- `docs/features/V1/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md` deliberately defers started goroutines, public lifecycle APIs, network serving, and public shutdown APIs to V1-D/V1-E.
- Original live-repo reality at planning time: the root `shunter` package was not present in that checkout (`rtk go list .` reported `no Go files`). This plan was therefore stacked after the V1-A, V1-B, and V1-C implementation plans.
- The former bundled demo command remains a demo consumer, not an implementation source of truth for this slice.

Go API facts verified with `rtk go doc` and file inspection:

- `schema.Engine.Start(context.Context) error` performs startup schema compatibility checks against recovered snapshot metadata.
- `commitlog.NewDurabilityWorkerWithResumePlan(dir, plan, opts)` creates and starts the durability worker goroutine immediately.
- `commitlog.DurabilityWorker` implements the executor `DurabilityHandle` contract through `EnqueueCommitted` and `WaitUntilDurable`, and exposes `Close() (uint64, error)`.
- `executor.NewExecutor(cfg, reducers, state, registry, recoveredTxID)` constructs an executor but does not start its `Run` loop.
- `executor.Executor.Startup(ctx, scheduler)` must run before `Scheduler.Run`, `Executor.Run`, and protocol admission; it replays schedules, sweeps dangling clients, and flips external readiness.
- `executor.Executor.SchedulerFor()` returns a scheduler wired to the executor inbox and explicitly documents that callers must stop `Scheduler.Run` before `Executor.Shutdown` closes the inbox.
- `executor.Executor.Run(ctx)` runs until its context is canceled or the inbox is closed.
- `executor.Executor.Shutdown()` closes the executor inbox and waits for `Run` to finish.
- `subscription.NewManager(schema, indexResolver, opts...)` constructs the subscription manager without starting a goroutine.
- `subscription.WithFanOutInbox(inbox)` wires manager evaluation to a fan-out inbox; nil inbox means evaluation runs without dispatch.
- `subscription.NewFanOutWorker(inbox, sender, dropped)` constructs a fan-out worker; `FanOutWorker.Run(ctx)` is the background goroutine.
- `subscription.FanOutSender` is normally protocol-backed, but V1-E owns protocol/network serving. V1-D should use a private no-op sender only to prove runtime ownership of the worker without opening sockets or accepting clients.

## Validation conclusion for V1-D

V1-D should be the first hosted-runtime slice that starts long-lived runtime goroutines and owns their shutdown order.

V1-D must not add network serving. It may create an internal fan-out worker with a private no-op sender because no external clients can connect until V1-E. That keeps the runtime ownership graph complete without smuggling protocol/server construction into this slice.

V1-D should not instantiate `protocol.Server`, open HTTP listeners, expose `ListenAndServe`, expose `HTTPHandler`, or add local reducer/query APIs.

## Scope

In scope:

- add `Runtime.Start(ctx context.Context) error`
- add `Runtime.Close() error`
- add narrow readiness/health inspection methods
- create/start/own the durability worker
- create/start/own the subscription manager and internal fan-out worker
- create/start/own the executor and scheduler
- call `schema.Engine.Start(ctx)` and `executor.Executor.Startup(ctx, scheduler)` in the correct startup order
- test start success, start failure, context cancellation, repeated calls, close idempotency, close-before-start, and partial-start cleanup
- store private runtime lifecycle state needed by V1-E

Out of scope:

- `ListenAndServe`
- `HTTPHandler`
- HTTP server construction
- WebSocket listener/socket opening
- protocol-backed fan-out sender construction
- client connection acceptance
- local reducer/query public APIs
- public runtime admin/control-plane APIs
- v1.5 query/view/export/codegen/permissions/migration surfaces
- lower-level kernel redesign

## Decisions to lock for V1-D

1. `Start(ctx)` is non-blocking after readiness.
   - It performs startup sequencing and returns once owned background workers are ready.
   - It does not block for the lifetime of the runtime.
   - Long-lived shutdown is owned by `Close()`.

2. The `ctx` passed to `Start(ctx)` is a startup/cancellation context, not the runtime lifetime context.
   - If `ctx` is canceled before readiness, `Start` returns the context error and cleans up any partially started resources.
   - Canceling `ctx` after `Start` returns does not automatically stop the runtime.
   - The runtime creates its own internal lifecycle context and cancels it in `Close()`.

3. Start idempotency is intentionally simple.
   - Calling `Start(ctx)` on an already ready runtime returns nil.
   - Calling `Start(ctx)` while another goroutine is in the middle of starting returns `ErrRuntimeStarting`.
   - Calling `Start(ctx)` after `Close()` returns `ErrRuntimeClosed`.
   - If startup fails and partial resources are cleaned up, the runtime returns to the built/not-started state unless the caller explicitly closed it.

4. Close idempotency is strict.
   - `Close()` before `Start()` is valid and marks the runtime closed.
   - Repeated `Close()` calls return nil after the first close completes.
   - `Close()` on a partially started runtime must clean up whatever was created.
   - `Close()` returns a joined/aggregated error if owned subsystem close operations fail.

5. Readiness means background runtime ownership is established.
   - Ready is false before `Start` and during `Close`.
   - Ready becomes true only after:
     1. `schema.Engine.Start(ctx)` succeeds.
     2. durability worker construction succeeds.
     3. subscription manager and fan-out worker are constructed.
     4. executor and scheduler are constructed.
     5. `executor.Startup(ctx, scheduler)` succeeds.
     6. executor, scheduler, and fan-out goroutines have been launched under runtime ownership.
   - Ready becomes false before shutdown starts.

6. V1-D fan-out is runtime-owned but protocol-neutral.
   - Create the fan-out inbox and worker in V1-D.
   - Use a private no-op `subscription.FanOutSender` in V1-D because V1-E owns protocol/server construction.
   - Keep the no-op sender unexported and documented as temporary V1-D internal wiring.
   - V1-E replaces or wraps this with a protocol-backed sender.

7. Shutdown order is based on kernel package contracts.
   - Stop scheduler/fan-out goroutines before closing the executor inbox, because `Executor.SchedulerFor()` documents that scheduler must stop before `Executor.Shutdown()` closes the inbox.
   - Shut down executor before closing durability, because executor post-commit code can enqueue durability work.
   - Close durability last so accepted commits can flush.

## Files likely to modify

Assuming V1-A/V1-B/V1-C files exist after stacking/landing:

- Modify: `runtime.go`
- Modify or create: `runtime_lifecycle.go`
- Modify or create: `runtime_lifecycle_test.go`
- Possible modify: `config.go` if V1-D needs private lifecycle queue defaults for fan-out capacity
- Possible modify: `runtime_build.go` if V1-C stores private build-plan fields that need to be promoted into lifecycle-owned fields

Do not edit unless implementation proves a direct compile necessity:

- `schema/build.go`
- `schema/engine.go`
- `commitlog/durability.go`
- `executor/executor.go`
- `executor/scheduler_worker.go`
- `subscription/manager.go`
- `subscription/fanout_worker.go`
- `protocol/*`

## Public API target for V1-D

Add these root-package methods/types:

```go
func (r *Runtime) Start(ctx context.Context) error
func (r *Runtime) Close() error
func (r *Runtime) Ready() bool
func (r *Runtime) Health() RuntimeHealth
```

Expected health shape, exact names flexible:

```go
type RuntimeState string

const (
    RuntimeStateBuilt    RuntimeState = "built"
    RuntimeStateStarting RuntimeState = "starting"
    RuntimeStateReady    RuntimeState = "ready"
    RuntimeStateClosing  RuntimeState = "closing"
    RuntimeStateClosed   RuntimeState = "closed"
    RuntimeStateFailed   RuntimeState = "failed"
)

type RuntimeHealth struct {
    State     RuntimeState
    Ready     bool
    LastError error
}
```

Expected sentinel errors, exact names flexible:

```go
var ErrRuntimeStarting = errors.New("shunter: runtime is starting")
var ErrRuntimeClosed = errors.New("shunter: runtime is closed")
```

Do not add:

```go
// V1-E:
// func (r *Runtime) ListenAndServe(...) error
// func (r *Runtime) HTTPHandler() http.Handler

// V1-F:
// func (r *Runtime) CallReducer(...)
// func (r *Runtime) Query(...)
// func (r *Runtime) ReadView(...)
```

## Private implementation target

Expected private `Runtime` shape after V1-D, exact names flexible:

```go
type Runtime struct {
    // V1-A/V1-C fields.
    moduleName string
    config Config
    engine *schema.Engine
    registry schema.SchemaRegistry
    dataDir string
    state *store.CommittedState
    recoveredTxID types.TxID
    resumePlan commitlog.RecoveryResumePlan
    reducers *executor.ReducerRegistry

    // V1-D lifecycle state.
    mu sync.Mutex
    stateName RuntimeState
    ready atomic.Bool
    lastErr error

    lifecycleCtx context.Context
    lifecycleCancel context.CancelFunc
    wg sync.WaitGroup

    durability *commitlog.DurabilityWorker
    subscriptions *subscription.Manager
    fanOutInbox chan subscription.FanOutMessage
    fanOutWorker *subscription.FanOutWorker
    executor *executor.Executor
    scheduler *executor.Scheduler
}
```

Use an internal helper struct if preferred, but keep lifecycle ownership private to `Runtime`.

## Startup order target

`Runtime.Start(ctx)` should do this, in order:

1. Reject nil receiver defensively if implementation convention allows it, or let nil panic consistently with normal Go method behavior. Tests do not need to depend on nil receiver behavior.
2. Under lock:
   - return nil if already ready
   - return `ErrRuntimeStarting` if state is starting
   - return `ErrRuntimeClosed` if state is closing/closed
   - set state to starting and clear old startup error
3. Check `ctx.Err()` before creating resources.
4. Call `r.engine.Start(ctx)`.
5. Create the durability worker:
   - options from `commitlog.DefaultCommitLogOptions()`
   - set `ChannelCapacity` from normalized/private durability queue capacity if V1-C stored one
   - use `commitlog.NewDurabilityWorkerWithResumePlan(r.dataDir, r.resumePlan, opts)`
6. Create `fanOutInbox := make(chan subscription.FanOutMessage, fanOutCapacity)`.
7. Create `subscriptions := subscription.NewManager(r.registry, r.registry, subscription.WithFanOutInbox(fanOutInbox))`.
8. Create `exec := executor.NewExecutor(executor.ExecutorConfig{InboxCapacity: normalized executor queue capacity, Durability: durability, Subscriptions: subscriptions}, r.reducers, r.state, r.registry, r.recoveredTxID)`.
9. Create `scheduler := exec.SchedulerFor()`.
10. Call `exec.Startup(ctx, scheduler)` before any executor/scheduler/protocol admission goroutines start.
11. Create internal lifecycle context for runtime-owned goroutines.
12. Create `fanOutWorker := subscription.NewFanOutWorker(fanOutInbox, noopFanOutSender{}, subscriptions.DroppedChanSend())`.
13. Store all created handles on `Runtime` while still holding the lifecycle lock or before publishing ready.
14. Launch goroutines under a runtime wait group:
    - `go exec.Run(executorRunCtx)`
    - `go scheduler.Run(lifecycleCtx)`
    - `go fanOutWorker.Run(lifecycleCtx)`
15. Set state ready and `ready=true`.
16. Return nil.

If any step fails before readiness:

- call the same private partial cleanup helper used by `Close()`
- leave `ready=false`
- record `LastError`
- return the wrapped failure
- transition back to built/not-started if cleanup succeeds and the runtime was not explicitly closed

## Shutdown order target

`Runtime.Close()` should do this, in order:

1. Under lock:
   - return nil if already closed
   - if built but never started, set state closed and return nil
   - set state closing and `ready=false`
   - snapshot owned handles into locals or keep the lock through state mutation only; avoid holding the lock while waiting on goroutines if helper design can avoid deadlocks
2. Cancel the runtime lifecycle context to stop scheduler and fan-out worker.
3. Wait for scheduler/fan-out worker goroutines to exit.
4. Call `executor.Shutdown()` after scheduler is stopped, so no late scheduled send races the closed executor inbox.
5. Close the durability worker after executor shutdown finishes.
6. Clear private started-resource fields or leave them only for diagnostics; either way, prevent reuse after close.
7. Set state closed.
8. Return nil or an aggregated close error.

Important guardrail:

- Do not cancel the executor run context before `executor.Shutdown()` drains the inbox unless implementation chooses a separate forced-shutdown path and tests pin dropped-work behavior. The safer V1-D default is to stop producers first, then let `Executor.Shutdown()` close/drain the inbox and wait for `Run` to finish.

## Task 1: Reconfirm stack prerequisites before coding

Objective: ensure V1-D is implemented only after V1-A/V1-B/V1-C root APIs and private V1-C build fields exist.

Files:
- Read: `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`
- Read: `docs/features/V1/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`
- Read: `docs/features/V1/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`
- Inspect: `module.go`, `config.go`, `runtime.go`, `runtime_build.go`, and root tests once prior slices exist

Run:

```bash
rtk go list .
rtk go doc ./commitlog.NewDurabilityWorkerWithResumePlan
rtk go doc ./commitlog.DurabilityWorker
rtk go doc ./executor.Executor.Startup
rtk go doc ./executor.Executor.SchedulerFor
rtk go doc ./executor.Executor.Shutdown
rtk go doc ./subscription.NewManager
rtk go doc ./subscription.NewFanOutWorker
rtk go doc ./subscription.FanOutSender
```

Expected:

- `rtk go list .` succeeds after V1-A exists.
- V1-B explicit module build tests pass before V1-D starts.
- V1-C build/recovery tests pass before V1-D starts.

Stop condition:

- If the root package or V1-C private build fields are missing, stop and apply/land prior slices first. Do not mix V1-D with creating the root package from scratch.

## Task 2: Add lifecycle state and health tests first

Objective: pin readiness/health behavior before starting subsystem goroutines.

Files:
- Modify or create: `runtime_lifecycle_test.go`

Test cases:

```go
func TestRuntimeInitialHealthIsBuiltAndNotReady(t *testing.T) {
    rt := buildValidTestRuntime(t)

    if rt.Ready() {
        t.Fatal("new runtime is ready before Start")
    }
    health := rt.Health()
    if health.State != RuntimeStateBuilt {
        t.Fatalf("state = %q, want %q", health.State, RuntimeStateBuilt)
    }
    if health.Ready {
        t.Fatal("health reports ready before Start")
    }
    if health.LastError != nil {
        t.Fatalf("unexpected last error: %v", health.LastError)
    }
}
```

Run:

```bash
rtk go test . -run TestRuntimeInitialHealthIsBuiltAndNotReady -count=1
```

Expected:
- fail until `Ready`, `Health`, `RuntimeState`, and initial state storage exist.

## Task 3: Implement lifecycle state/health shell

Objective: add public readiness/health inspection without starting workers yet.

Files:
- Modify: `runtime.go`
- Modify or create: `runtime_lifecycle.go`

Implementation notes:

- Initialize `Runtime` state to `RuntimeStateBuilt` when V1-C `Build` returns a runtime.
- `Ready()` should be lock-free via `atomic.Bool` or lock-protected; either is acceptable if race-free.
- `Health()` should return a value copy of state and last error.
- Do not expose internal subsystem handles through health.

Run:

```bash
rtk go test . -run TestRuntimeInitialHealthIsBuiltAndNotReady -count=1
```

Expected:
- pass.

## Task 4: Add a failing test for successful Start and Close

Objective: prove V1-D starts owned background workers and can shut them down cleanly.

Files:
- Modify: `runtime_lifecycle_test.go`

Test shape:

```go
func TestRuntimeStartAndCloseOwnLifecycle(t *testing.T) {
    rt := buildValidTestRuntime(t)

    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("Start returned error: %v", err)
    }
    if !rt.Ready() {
        t.Fatal("runtime not ready after Start")
    }
    health := rt.Health()
    if health.State != RuntimeStateReady || !health.Ready {
        t.Fatalf("health after Start = %+v", health)
    }
    if rt.durability == nil {
        t.Fatal("durability worker not created")
    }
    if rt.executor == nil {
        t.Fatal("executor not created")
    }
    if rt.scheduler == nil {
        t.Fatal("scheduler not created")
    }
    if rt.subscriptions == nil {
        t.Fatal("subscription manager not created")
    }
    if rt.fanOutWorker == nil {
        t.Fatal("fan-out worker not created")
    }

    if err := rt.Close(); err != nil {
        t.Fatalf("Close returned error: %v", err)
    }
    if rt.Ready() {
        t.Fatal("runtime ready after Close")
    }
    if got := rt.Health().State; got != RuntimeStateClosed {
        t.Fatalf("state after Close = %q, want closed", got)
    }
}
```

Run:

```bash
rtk go test . -run TestRuntimeStartAndCloseOwnLifecycle -count=1
```

Expected:
- fail until `Start`, `Close`, and private lifecycle wiring exist.

## Task 5: Implement private no-op fan-out sender

Objective: let V1-D own and start a fan-out worker without importing V1-E protocol/server concerns.

Files:
- Modify or create: `runtime_lifecycle.go`

Implementation shape:

```go
type noopFanOutSender struct{}

func (noopFanOutSender) SendTransactionUpdateHeavy(types.ConnectionID, subscription.CallerOutcome, []subscription.SubscriptionUpdate, *subscription.EncodingMemo) error {
    return nil
}
func (noopFanOutSender) SendTransactionUpdateLight(types.ConnectionID, uint32, []subscription.SubscriptionUpdate, *subscription.EncodingMemo) error {
    return nil
}
func (noopFanOutSender) SendSubscriptionError(types.ConnectionID, subscription.SubscriptionError) error {
    return nil
}
```

Guardrails:

- Keep this type unexported.
- Do not make it a public runtime option.
- Add a comment that V1-E replaces/wraps it with protocol-backed delivery.

Run:

```bash
rtk go test . -run TestRuntimeStartAndCloseOwnLifecycle -count=1
```

Expected:
- still fail until full lifecycle wiring exists, but no missing sender implementation remains.

## Task 6: Implement Start resource creation and startup sequencing

Objective: make `Start` create and publish all V1-D-owned runtime resources in the correct order.

Files:
- Modify: `runtime_lifecycle.go`
- Possible modify: `runtime.go` for private fields

Implementation notes:

- Add `ErrRuntimeStarting` and `ErrRuntimeClosed` sentinels.
- Use a mutex to protect state transitions and resource publication.
- Use `commitlog.DefaultCommitLogOptions()` and override queue/channel capacity from private normalized config if V1-C stored it.
- Use `r.registry` for both `subscription.SchemaLookup` and `subscription.IndexResolver`; `schema.SchemaRegistry` embeds both interfaces.
- Use `subscription.WithFanOutInbox(r.fanOutInbox)` so post-commit evaluation hands messages to the fan-out worker.
- Use `exec.SchedulerFor()` instead of constructing the scheduler manually; its docs encode the shutdown ordering requirement.
- Call `exec.Startup(ctx, scheduler)` before launching `exec.Run` / `scheduler.Run`.
- Launch executor, scheduler, and fan-out worker under a `sync.WaitGroup` or small private goroutine helper.

Run:

```bash
rtk go test . -run TestRuntimeStartAndCloseOwnLifecycle -count=1
```

Expected:
- may hang/fail until `Close` shutdown order is implemented; do not leave a hanging test in the final implementation.

## Task 7: Implement Close and partial cleanup helper

Objective: make lifecycle shutdown deterministic, idempotent, and safe for started or partially started resources.

Files:
- Modify: `runtime_lifecycle.go`

Implementation notes:

- Factor cleanup into a private helper used by both `Close()` and failed `Start()` paths.
- Shutdown order:
  1. mark not ready
  2. cancel scheduler/fan-out lifecycle context
  3. wait for scheduler/fan-out goroutines
  4. call `executor.Shutdown()` if executor was started
  5. close durability worker if present
  6. update health state and last error
- Do not call `executor.Shutdown()` before scheduler has observed cancellation and exited.
- Use `errors.Join` for multiple close errors if the Go version allows it.
- After successful `Close`, leave state `RuntimeStateClosed` and make later `Close` calls return nil.

Run:

```bash
rtk go test . -run TestRuntimeStartAndCloseOwnLifecycle -count=1
```

Expected:
- pass.

## Task 8: Add repeated Start and Close tests

Objective: pin V1-D idempotency semantics.

Files:
- Modify: `runtime_lifecycle_test.go`

Test cases:

```go
func TestRuntimeStartIsIdempotentAfterReady(t *testing.T) {
    rt := buildValidTestRuntime(t)
    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("first Start: %v", err)
    }
    t.Cleanup(func() { _ = rt.Close() })

    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("second Start on ready runtime: %v", err)
    }
}

func TestRuntimeCloseIsIdempotent(t *testing.T) {
    rt := buildValidTestRuntime(t)
    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("Start: %v", err)
    }
    if err := rt.Close(); err != nil {
        t.Fatalf("first Close: %v", err)
    }
    if err := rt.Close(); err != nil {
        t.Fatalf("second Close: %v", err)
    }
}

func TestRuntimeCloseBeforeStartClosesRuntime(t *testing.T) {
    rt := buildValidTestRuntime(t)
    if err := rt.Close(); err != nil {
        t.Fatalf("Close before Start: %v", err)
    }
    if err := rt.Start(context.Background()); !errors.Is(err, ErrRuntimeClosed) {
        t.Fatalf("Start after Close error = %v, want ErrRuntimeClosed", err)
    }
}
```

Run:

```bash
rtk go test . -run 'TestRuntimeStartIsIdempotentAfterReady|TestRuntimeCloseIsIdempotent|TestRuntimeCloseBeforeStartClosesRuntime' -count=1
```

Expected:
- pass after lifecycle state machine is implemented.

## Task 9: Add Start cancellation test

Objective: pin that `Start(ctx)` respects startup cancellation and leaves no ready runtime behind.

Files:
- Modify: `runtime_lifecycle_test.go`

Test shape:

```go
func TestRuntimeStartWithCanceledContextFailsWithoutReadiness(t *testing.T) {
    rt := buildValidTestRuntime(t)
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    err := rt.Start(ctx)
    if err == nil || !errors.Is(err, context.Canceled) {
        t.Fatalf("Start error = %v, want context.Canceled", err)
    }
    if rt.Ready() {
        t.Fatal("runtime ready after canceled Start")
    }
    health := rt.Health()
    if health.State == RuntimeStateReady {
        t.Fatalf("state after canceled Start = ready")
    }
    if health.LastError == nil {
        t.Fatal("LastError not recorded after canceled Start")
    }

    // Runtime may be retried after a cleaned-up startup cancellation.
    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("retry Start after canceled startup: %v", err)
    }
    if err := rt.Close(); err != nil {
        t.Fatalf("Close after retry: %v", err)
    }
}
```

Run:

```bash
rtk go test . -run TestRuntimeStartWithCanceledContextFailsWithoutReadiness -count=1
```

Expected:
- pass.

## Task 10: Add startup failure and partial cleanup test seam

Objective: prove partial-start cleanup without forcing hard-to-trigger kernel failures or widening public API.

Files:
- Modify: `runtime_lifecycle.go`
- Modify: `runtime_lifecycle_test.go`

Preferred implementation approach:

- Add narrow private function fields or package-level variables only if needed for tests, for example:
  - `newDurabilityWorkerFn`
  - `newExecutorFn`
  - `newFanOutWorkerFn`
- Keep seams unexported and reset them with `t.Cleanup`.
- Use them only to force an error after one earlier resource has been created, so the test can assert cleanup.

Test target:

```go
func TestRuntimeStartFailureCleansPartialResources(t *testing.T) {
    rt := buildValidTestRuntime(t)

    // Arrange a private test seam so startup fails after durability creation
    // but before readiness is published.

    err := rt.Start(context.Background())
    if err == nil {
        t.Fatal("Start returned nil error for injected failure")
    }
    if rt.Ready() {
        t.Fatal("runtime ready after failed Start")
    }
    if rt.durability != nil || rt.executor != nil || rt.scheduler != nil || rt.fanOutWorker != nil {
        t.Fatalf("partial resources not cleaned up: health=%+v", rt.Health())
    }

    // Retry should be allowed after cleanup.
    restoreStartupSeam(t)
    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("retry Start after injected failure: %v", err)
    }
    if err := rt.Close(); err != nil {
        t.Fatalf("Close after retry: %v", err)
    }
}
```

Run:

```bash
rtk go test . -run TestRuntimeStartFailureCleansPartialResources -count=1
```

Expected:
- pass.

Guardrail:

- Do not add exported testing hooks or public dependency-injection surfaces for this. If private seams feel too invasive, make this a same-package test using a tiny unexported lifecycle builder interface.

## Task 11: Add concurrent Start/Close smoke tests if state locking is non-trivial

Objective: catch obvious lifecycle races before broader validation.

Files:
- Modify: `runtime_lifecycle_test.go`

Suggested tests:

- concurrent duplicate `Start` calls should result in one success and the rest either nil after ready or `ErrRuntimeStarting`, but no panic/data race.
- concurrent duplicate `Close` calls should all return nil or one aggregated close error; no panic/data race.

Run with race detector only if repo norms and runtime budget allow:

```bash
rtk go test . -run 'TestRuntimeConcurrentStart|TestRuntimeConcurrentClose' -count=1
```

Optional broader race check:

```bash
rtk go test . -race -run 'TestRuntimeConcurrentStart|TestRuntimeConcurrentClose' -count=1
```

Expected:
- pass, or document why race detector is skipped if too slow/unavailable.

## Task 12: Focused validation

Objective: prove V1-D did not regress V1-A/V1-B/V1-C or lower-level kernel packages.

Run:

```bash
rtk go fmt .
rtk go test . -count=1
rtk go test ./schema ./commitlog ./executor ./subscription -count=1
rtk go vet . ./schema ./commitlog ./executor ./subscription
```

Then, if the working tree allows it:

```bash
rtk go test ./... -count=1
```

Expected:

- root and touched-package gates pass
- broad tests pass, or unrelated dirty-state failures are reported without fixing OI-002/query/protocol code inside V1-D

---

## Verification checklist

V1-D is complete when all of the following are true:

- `Runtime.Start(ctx)` exists and returns after startup readiness, not after runtime shutdown.
- `Runtime.Close()` exists and is idempotent.
- `Runtime.Ready()` and `Runtime.Health()` expose narrow readiness/diagnostic state.
- `Start` calls `schema.Engine.Start(ctx)`.
- `Start` constructs and owns the durability worker using the V1-C `RecoveryResumePlan`.
- `Start` constructs and owns the subscription manager and fan-out worker.
- `Start` constructs the executor and scheduler from V1-C private state.
- `Start` calls `executor.Startup(ctx, scheduler)` before launching executor/scheduler goroutines.
- `Start` launches executor, scheduler, and fan-out goroutines under runtime ownership.
- `Close` stops scheduler/fan-out before executor shutdown, then closes durability after executor shutdown.
- Start success, canceled start, injected start failure, repeated start, close before start, repeated close, and partial cleanup are tested.
- No HTTP listener, socket, `ListenAndServe`, `HTTPHandler`, protocol server, local reducer/query API, v1.5, or v2 surface is added.
- Focused RTK validation passes.

## Risks and guardrails

1. Scheduler shutdown can race executor inbox closure.
   - Guardrail: follow `Executor.SchedulerFor()` docs: stop scheduler before `Executor.Shutdown()` closes the inbox.

2. Executor context cancellation can drop buffered work.
   - Guardrail: prefer `Executor.Shutdown()` for graceful executor stop; do not cancel executor run context before shutdown drains the inbox unless a later plan deliberately defines forced shutdown.

3. Durability worker starts immediately on construction.
   - Guardrail: create it only inside `Start`, and always close it on failed startup or `Close`.

4. Fan-out normally needs a protocol sender, but protocol is V1-E.
   - Guardrail: use a private no-op sender in V1-D and keep protocol-backed sender wiring out of this slice.

5. Startup failure after partial resource creation can leak goroutines.
   - Guardrail: implement one private cleanup helper and test an injected failure after at least one resource has been created.

6. Scope creep into network serving is tempting once lifecycle exists.
   - Guardrail: V1-D stops at lifecycle ownership. V1-E owns `ListenAndServe`, `HTTPHandler`, protocol server construction, and auth/listen mapping.

## Historical sequencing note

The later hosted-runtime slices have since landed. Do not treat this completed
V1-D plan as a live handoff; use `HOSTED_RUNTIME_PLANNING_HANDOFF.md` for
current hosted-runtime status.
