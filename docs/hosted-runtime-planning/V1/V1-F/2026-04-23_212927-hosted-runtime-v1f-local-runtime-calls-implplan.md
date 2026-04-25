# Hosted Runtime V1-F Local Runtime Calls Implementation Plan

> For Hermes: use the subagent-driven-development skill to execute this plan task-by-task if the user asks for implementation.

Status: concretely validated against the live repo on 2026-04-23.
Scope: V1-F only; planning artifact, not implementation.

Goal: expose local reducer calls and minimal local read/query helpers on `shunter.Runtime` as legitimate secondary APIs for tests, tools, admin flows, and in-process integrations, without replacing the WebSocket-first external client model.

Architecture: V1-F should adapt the same runtime-owned executor, committed state, schema registry, and narrow SQL/query behavior that V1-D/V1-E already own. Local reducer calls should go through the executor command path so transaction, durability, subscription, and reducer outcome semantics stay aligned with protocol reducer calls where practical. Local reads should use short-lived committed read views or a narrow SQL helper over committed snapshots, not direct mutable store access. The slice must not add new network APIs, admin/control-plane APIs, v1.5 query/view declarations, or broad SQL/view systems.

Tech stack: Go, root package `github.com/ponchione/shunter`, existing `executor`, `store`, `schema`, `protocol`, `query/sql`, `types`, and `bsatn` packages, RTK-wrapped Go toolchain commands.

---

## Current grounded context

Read and verified while writing this plan:

- `docs/decomposition/hosted-runtime-version-phases.md` defines V1-F as local runtime calls after V1-C/V1-D runtime ownership and before V1-G export/introspection.
- `docs/decomposition/hosted-runtime-v1-contract.md` says the primary external client surface remains WebSocket, while local reducer/query calls are legitimate secondary APIs for tests, tooling, admin/maintenance flows, and in-process integrations.
- `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md` defines `Start(ctx)`, `Close()`, `Ready()`, lifecycle state, and lifecycle-owned executor/scheduler/fan-out resources.
- `docs/hosted-runtime-planning/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md` defines the protocol-backed network surface and keeps local reducer/query APIs out of V1-E.
- Live repo reality at validation time: the root `shunter` package is still absent in this checkout (`rtk go list .` reports `no Go files in /home/ponchione/source/shunter`). This plan is therefore stacked after V1-A, V1-B, V1-C, V1-D, and V1-E implementation plans.
- The former bundled demo command is a demo consumer, not an implementation source of truth for V1-F runtime architecture.

Go API facts verified with `rtk go doc` and file inspection:

- `executor.Executor.Submit(cmd)` sends an in-process/test command to the executor inbox and is deliberately not gated by `externalReady`; it validates buffered response channels and rejects fatal/shutdown executors.
- `executor.Executor.SubmitWithContext(ctx, cmd)` is the external admission entrypoint used by protocol paths and is rejected with `ErrExecutorNotStarted` until `Startup` completes.
- `executor.CallReducerCmd` carries an `executor.ReducerRequest` plus a buffered `chan<- executor.ReducerResponse` and optional protocol response channel.
- `executor.ReducerRequest` contains `ReducerName`, `Args`, `Caller types.CallerContext`, `RequestID`, `Source`, and flags/schedule metadata.
- `executor.ReducerResponse` contains `Status`, `Error`, `ReturnBSATN`, and `TxID`.
- `executor.ReducerStatus` currently has `StatusCommitted`, `StatusFailedUser`, `StatusFailedPanic`, and `StatusFailedInternal`.
- `executor.CallSourceExternal` is the normal client RPC source. Local runtime calls should use this source unless implementation deliberately introduces a narrower source in the executor package; do not invent a separate transaction path in root package code.
- `executor.ProtocolInboxAdapter.CallReducer(ctx, protocol.CallReducerRequest)` exists for protocol calls, but it returns only an admission error and drives protocol-heavy response envelopes. It is not the right primary local reducer API because V1-F needs a synchronous local result shape without constructing fake protocol connections.
- `protocol.CallReducerRequest` includes connection/identity/request fields and protocol response delivery details. It is useful for semantic alignment, but local calls should not depend on WebSocket/protocol connection state.
- `store.CommittedState.Snapshot()` returns a `store.CommittedReadView`.
- `store.CommittedReadView` is a read-only point-in-time snapshot. `Close()` must be called promptly because an open snapshot holds the read lock and can block commits.
- `protocol.handleOneOffQuery` is unexported and implements current one-off SQL behavior by parsing `query/sql`, validating predicates through `subscription`, scanning a committed read view, encoding rows, and sending protocol response envelopes. V1-F cannot call it directly from the root package without exporting or duplicating behavior.
- `query/sql` is a narrow parser/coercion package, not a full query engine. It accepts a constrained SQL subset used by Subscribe/OneOffQuery paths.
- `schema.SchemaRegistry` exposes `Tables()`, table lookup through `SchemaLookup`, reducer lookup through `Reducer(name)`, `Reducers()`, lifecycle hooks, and `Version()`.
- `types.CallerContext` carries `Identity`, `ConnectionID`, and `Timestamp`; `types.Identity` is a 32-byte canonical identity with `IsZero()`.

## Validation conclusion for V1-F

V1-F should be the first hosted-runtime slice that exposes local, in-process runtime calls through the root API.

The reducer path should submit `executor.CallReducerCmd` to the runtime-owned executor with a buffered response channel and wait for `executor.ReducerResponse`. This preserves serialized transaction execution, reducer status, durable post-commit behavior, and subscription fan-out behavior. It also avoids creating fake protocol connections or bypassing executor transaction semantics.

The read path should expose a minimal helper that is safe and hard to misuse. The lowest-risk V1-F read surface is a callback-based read view:

```go
func (r *Runtime) Read(ctx context.Context, fn func(LocalReadView) error) error
```

This lets the runtime acquire a committed snapshot, pass a narrow local view, and always close the snapshot when the callback returns. It avoids leaking `store.CommittedReadView` as a long-lived public handle and avoids forcing v1 to design a full SQL/query API.

A convenience SQL helper may be included only if it is implemented as a thin root-owned wrapper around an exported/internalized shared one-off query evaluation helper, not by duplicating `protocol.handleOneOffQuery` logic into the root package. If implementation cannot cleanly share that evaluator without touching protocol internals too much, keep V1-F to `Read(...)` plus table scan/get helpers and defer SQL convenience to a later narrow follow-up.

V1-F must not add REST/MCP/admin APIs, new serving APIs, v1.5 query/view declarations, contract export/codegen, permissions/migration metadata, broad SQL, or local APIs that become the primary external client contract.

## Scope

In scope:

- add local reducer invocation on `Runtime`
- add local caller/identity options and sane dev/test defaults
- add a minimal local read helper over committed snapshots
- align reducer call status/error behavior with executor/protocol semantics where practical
- require runtime readiness for calls that depend on executor lifecycle
- define clear errors for not-started, starting, closing, closed, and nil/invalid inputs
- pin snapshot closure behavior in tests
- add docs/comments that local calls are secondary APIs, not a WebSocket replacement

Out of scope:

- new network serving APIs; V1-E owns network surface
- replacing WebSocket as the external client model
- broad SQL/view system
- v1.5 query/view declarations
- export/introspection; V1-G owns that
- hello-world replacement; V1-H owns that
- REST/MCP-first surfaces
- broad admin/control-plane APIs
- contract snapshots/codegen
- permissions/read-model metadata
- migration metadata
- multi-module hosting
- lower-level executor/store/protocol redesign unless a tiny shared-query seam is required for a convenience helper

## Decisions to lock for V1-F

1. Local calls require an already-started, ready runtime.
   - `CallReducer` and `Read` should return a not-ready sentinel when the runtime is built but not started.
   - `CallReducer` and `Read` should return `ErrRuntimeStarting` while startup is in progress.
   - Calls during closing/after closed should preserve `ErrRuntimeClosed` or a similarly named closed sentinel via `errors.Is`.
   - V1-F should not auto-start the runtime. `ListenAndServe(ctx)` may auto-start because it is an easy serving path; local APIs should be explicit and predictable for tests/tools.

2. Local reducer calls use the executor command path.
   - Do not call reducer handlers directly from the root package.
   - Do not mutate `store.CommittedState` directly.
   - Do not construct fake WebSocket/protocol connections just to invoke a reducer.
   - Submit `executor.CallReducerCmd` with `ResponseCh: make(chan executor.ReducerResponse, 1)`.
   - Use `executor.CallSourceExternal` for local calls unless implementation intentionally adds an executor-level `CallSourceLocal` with tests across executor/subscription semantics. Adding a new source is not required for V1-F.

3. Local reducer response shape should be small and executor-aligned.
   - Add a root result type similar to:
     ```go
     type ReducerResult struct {
         Status      ReducerStatus
         Error       error
         ReturnBSATN []byte
         TxID        types.TxID
     }
     ```
   - Either alias/wrap `executor.ReducerStatus` as the root public status type or define a root enum that maps one-to-one. Prefer a type alias if it does not overexpose executor internals.
   - Preserve reducer user errors as `StatusFailedUser` + error, not as admission failures.
   - Return admission/runtime errors from `CallReducer` itself.

4. Local caller identity is explicit but has a dev/test default.
   - Add an options struct rather than expanding positional parameters:
     ```go
     type CallReducerOptions struct {
         Identity     types.Identity
         ConnectionID types.ConnectionID
         RequestID    uint32
         Flags        byte
         Timestamp    time.Time
     }
     ```
   - Add `CallReducer(ctx, reducerName string, args []byte, opts ...CallReducerOption) (ReducerResult, error)` if the project prefers functional options, or `CallReducer(ctx, req LocalReducerRequest) (ReducerResult, error)` if implementation wants request structs. Pick one and keep it consistent with existing root API style.
   - Blank/zero identity should map to a documented local dev/test identity, not to a protocol-authenticated user. Use a deterministic private local identity helper so tests are stable.
   - Do not add broad auth policy or permission metadata in V1-F.

5. Local reads use callback-owned snapshots.
   - Preferred public API:
     ```go
     func (r *Runtime) Read(ctx context.Context, fn func(LocalReadView) error) error
     ```
   - `Read` checks runtime readiness, acquires `r.state.Snapshot()`, invokes `fn`, and closes the snapshot in a defer.
   - `fn == nil` is invalid and should fail before acquiring a snapshot.
   - The callback receives a narrow `LocalReadView`, not the mutable committed state.
   - The implementation must not leak snapshots on callback errors or panics; if preserving panic behavior, defer `Close()` before re-panicking.

6. Minimal local read view surface.
   - Add a narrow view wrapper instead of exporting raw `*store.CommittedState`:
     ```go
     type LocalReadView interface {
         TableScan(table string) iter.Seq[types.ProductValue]
         GetRow(table string, rowID types.RowID) (types.ProductValue, bool)
         RowCount(table string) (int, bool)
     }
     ```
   - If Go iterator ergonomics or imports make `iter.Seq` awkward for root users, an alternate simple method is acceptable:
     ```go
     ScanTable(table string) ([]types.ProductValue, error)
     ```
   - The V1-F plan preference is safety and simplicity over perfect zero-allocation reads. Avoid exposing a long-lived raw `store.CommittedReadView` unless tests prove the callback wrapper is too restrictive.
   - Unknown table names should return a clear error or `ok=false` consistently; do not panic.

7. SQL convenience is optional in this slice.
   - If included, use a small method such as:
     ```go
     func (r *Runtime) Query(ctx context.Context, query string, opts ...QueryOption) (QueryResult, error)
     ```
   - It must reuse or extract shared one-off query evaluation from `protocol` rather than duplicating the protocol handler's parser/validator/scanner logic.
   - If that extraction is more than a small shared helper, defer `Query` and ship only `Read` in V1-F. The V1-F acceptance criterion is local read/query helpers, not a full SQL public API.

8. Context behavior.
   - `CallReducer` must respect caller context while waiting to submit and while waiting for the executor response.
   - If `ctx` is canceled after submit but before response, return `ctx.Err()` and do not block indefinitely. The executor may still complete the already-accepted transaction; document this as normal async cancellation semantics.
   - `Read` should check `ctx.Err()` before acquiring a snapshot and before invoking the callback. It cannot preempt arbitrary callback code.

## Files likely to modify

Assuming V1-A through V1-E files exist after stacking/landing:

- Modify: `runtime.go`
- Modify or create: `runtime_local.go`
- Create: `runtime_local_test.go`
- Possibly modify: `runtime_lifecycle.go` to expose private state checks/helpers used by local APIs
- Possibly modify: `runtime_network.go` only if local closed/closing state sentinels are centralized there by V1-E
- Possibly create: `local_read.go` if keeping read wrapper separate improves clarity

Avoid editing unless implementation proves a direct need:

- `executor/executor.go`
- `executor/command.go`
- `executor/protocol_inbox_adapter.go`
- `store/committed.go`
- `protocol/handle_oneoff.go`
- `query/sql/*`
- `subscription/*`

Permitted tiny lower-level seam if SQL convenience is included:

- Extract a shared, non-network one-off query evaluator from `protocol/handle_oneoff.go` into a package that both protocol and root runtime can use. This should be a small refactor with parity tests preserved, not a behavior rewrite.

## Public API target for V1-F

Preferred minimum API:

```go
type ReducerStatus = executor.ReducerStatus

const (
    StatusCommitted      = executor.StatusCommitted
    StatusFailedUser     = executor.StatusFailedUser
    StatusFailedPanic    = executor.StatusFailedPanic
    StatusFailedInternal = executor.StatusFailedInternal
)

type ReducerResult struct {
    Status      ReducerStatus
    Error       error
    ReturnBSATN []byte
    TxID        types.TxID
}

type CallReducerOptions struct {
    Identity     types.Identity
    ConnectionID types.ConnectionID
    RequestID    uint32
    Flags        byte
    Timestamp    time.Time
}

type CallReducerOption func(*CallReducerOptions)

func WithCallerIdentity(id types.Identity) CallReducerOption
func WithCallerConnection(id types.ConnectionID) CallReducerOption
func WithRequestID(id uint32) CallReducerOption
func WithCallFlags(flags byte) CallReducerOption
func WithCallTimestamp(t time.Time) CallReducerOption

func (r *Runtime) CallReducer(ctx context.Context, reducerName string, args []byte, opts ...CallReducerOption) (ReducerResult, error)

type LocalReadView interface {
    ScanTable(table string) ([]types.ProductValue, error)
    GetRow(table string, rowID types.RowID) (types.ProductValue, bool, error)
    RowCount(table string) (int, bool, error)
}

func (r *Runtime) Read(ctx context.Context, fn func(LocalReadView) error) error
```

Acceptable alternate reducer API if implementation prefers request structs:

```go
type LocalReducerRequest struct {
    ReducerName string
    Args        []byte
    Caller      CallReducerOptions
}

func (r *Runtime) CallReducer(ctx context.Context, req LocalReducerRequest) (ReducerResult, error)
```

Do not add in V1-F:

```go
// V1-G:
// func (r *Runtime) ExportSchema(...)
// func (r *Runtime) Describe(...)

// V1.5:
// func (m *Module) Query(...)
// func (m *Module) View(...)
// func (r *Runtime) ExportContract(...)

// Later adapters:
// REST/MCP/admin surfaces
```

## Private implementation target

Expected private helpers, exact names flexible:

```go
var ErrRuntimeNotReady = errors.New("shunter: runtime is not ready")
var ErrReducerNameRequired = errors.New("shunter: reducer name is required")
var ErrReadCallbackRequired = errors.New("shunter: read callback is required")

func (r *Runtime) checkLocalCallReady() error
func defaultLocalIdentity() types.Identity
func buildCallerContext(opts CallReducerOptions) types.CallerContext
```

`checkLocalCallReady` should use V1-D/V1-E lifecycle state rather than duplicating state transitions. It should preserve existing sentinels with `errors.Is` where possible:

- built/not started -> `ErrRuntimeNotReady`
- starting -> `ErrRuntimeStarting`
- ready -> nil
- closing/closed -> `ErrRuntimeClosed`
- failed -> return the health `LastError` wrapped with local-call context, or a clear `ErrRuntimeNotReady` if the runtime is no longer safe to use

## Task 1: Reconfirm stack prerequisites before coding

Objective: ensure V1-F is implemented on top of the prior hosted-runtime slices rather than accidentally mixing root/lifecycle/network work into this slice.

Files:
- Read: this plan
- Read: V1-D and V1-E plans under `docs/hosted-runtime-planning/`
- Inspect: `runtime.go`, `runtime_lifecycle.go`, `runtime_network.go`, and root tests once prior slices exist

Run:

```bash
rtk go list .
rtk go doc ./executor.Executor.Submit
rtk go doc ./executor.CallReducerCmd
rtk go doc ./executor.ReducerRequest
rtk go doc ./executor.ReducerResponse
rtk go doc ./store.CommittedReadView
```

Expected:
- `rtk go list .` succeeds after V1-A exists.
- `Runtime.Start`, `Runtime.Close`, and `Runtime.Ready` tests from V1-D pass before V1-F starts.
- `Runtime.HTTPHandler` / `ListenAndServe` tests from V1-E pass if V1-E has landed.

Stop condition:
- If root package or V1-D lifecycle does not exist, stop and implement earlier slices first. Do not smuggle V1-D lifecycle or V1-E network work into V1-F.

## Task 2: Add failing tests for local-call readiness gates

Objective: pin lifecycle error behavior before adding local call implementation.

Files:
- Create/modify: `runtime_local_test.go`

Test cases:

```go
func TestCallReducerRequiresReadyRuntime(t *testing.T) {
    rt := buildTestRuntime(t)

    _, err := rt.CallReducer(context.Background(), "send_message", nil)
    if !errors.Is(err, ErrRuntimeNotReady) {
        t.Fatalf("CallReducer before Start error = %v, want ErrRuntimeNotReady", err)
    }
}

func TestReadRequiresReadyRuntime(t *testing.T) {
    rt := buildTestRuntime(t)

    err := rt.Read(context.Background(), func(LocalReadView) error { return nil })
    if !errors.Is(err, ErrRuntimeNotReady) {
        t.Fatalf("Read before Start error = %v, want ErrRuntimeNotReady", err)
    }
}

func TestLocalCallsAfterCloseReturnRuntimeClosed(t *testing.T) {
    rt := buildStartedTestRuntime(t)
    if err := rt.Close(); err != nil {
        t.Fatal(err)
    }

    _, callErr := rt.CallReducer(context.Background(), "send_message", nil)
    if !errors.Is(callErr, ErrRuntimeClosed) {
        t.Fatalf("CallReducer after Close error = %v, want ErrRuntimeClosed", callErr)
    }

    readErr := rt.Read(context.Background(), func(LocalReadView) error { return nil })
    if !errors.Is(readErr, ErrRuntimeClosed) {
        t.Fatalf("Read after Close error = %v, want ErrRuntimeClosed", readErr)
    }
}
```

Run:

```bash
rtk go test . -run 'Test(CallReducerRequiresReadyRuntime|ReadRequiresReadyRuntime|LocalCallsAfterCloseReturnRuntimeClosed)' -count=1
```

Expected:
- fail with missing `CallReducer`, `Read`, `LocalReadView`, and/or `ErrRuntimeNotReady` symbols.

## Task 3: Add failing tests for local reducer success and reducer failure

Objective: prove local reducer calls go through the executor and expose executor-aligned outcomes.

Files:
- Modify: `runtime_local_test.go`

Test shape:

```go
func TestCallReducerInvokesReducerThroughExecutor(t *testing.T) {
    rt := buildStartedRuntimeWithReducer(t, "send_message", func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
        if len(args) == 0 || string(args) != "hello" {
            return nil, fmt.Errorf("bad args: %q", args)
        }
        return []byte("ok"), nil
    })
    defer rt.Close()

    res, err := rt.CallReducer(context.Background(), "send_message", []byte("hello"), WithRequestID(7))
    if err != nil {
        t.Fatalf("CallReducer returned admission error: %v", err)
    }
    if res.Status != StatusCommitted {
        t.Fatalf("status = %v, want committed; reducer err = %v", res.Status, res.Error)
    }
    if string(res.ReturnBSATN) != "ok" {
        t.Fatalf("return = %q, want ok", res.ReturnBSATN)
    }
    if res.TxID == 0 {
        t.Fatal("expected non-zero committed tx id")
    }
}

func TestCallReducerUserErrorIsResultNotAdmissionError(t *testing.T) {
    rt := buildStartedRuntimeWithReducer(t, "fail", func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
        return nil, errors.New("user failed")
    })
    defer rt.Close()

    res, err := rt.CallReducer(context.Background(), "fail", nil)
    if err != nil {
        t.Fatalf("CallReducer admission error = %v, want nil", err)
    }
    if res.Status != StatusFailedUser {
        t.Fatalf("status = %v, want user failure", res.Status)
    }
    if res.Error == nil || !strings.Contains(res.Error.Error(), "user failed") {
        t.Fatalf("result error = %v, want user failed", res.Error)
    }
}
```

Notes:
- Use the existing reducer signature from V1-B (`schema.ReducerHandler`) and V1-D started runtime helpers.
- If existing reducer handlers return BSATN-encoded bytes in tests, adjust expected return bytes to match actual reducer behavior. Do not add a new encoding layer in `CallReducer`.

Run:

```bash
rtk go test . -run 'TestCallReducer(InvokesReducerThroughExecutor|UserErrorIsResultNotAdmissionError)' -count=1
```

Expected:
- fail until local reducer API is implemented.

## Task 4: Implement local reducer options and result types

Objective: add the public local-call types without wiring the executor yet.

Files:
- Create/modify: `runtime_local.go`

Implementation steps:
1. Add `ReducerStatus` alias and status const aliases if choosing the alias approach.
2. Add `ReducerResult`.
3. Add `CallReducerOptions` and option helpers.
4. Add local-call sentinel errors.
5. Add a private `applyCallReducerOptions(opts []CallReducerOption) CallReducerOptions` helper.
6. Add a private `defaultLocalIdentity()` helper with deterministic non-zero bytes.

Run:

```bash
rtk go test . -run TestCallReducerRequiresReadyRuntime -count=1
```

Expected:
- compile gets past type symbols, still fails until method behavior exists.

## Task 5: Implement local reducer readiness checks and executor submission

Objective: make `Runtime.CallReducer` work through `executor.CallReducerCmd`.

Files:
- Modify: `runtime_local.go`
- Possibly modify: `runtime_lifecycle.go` for shared state-check helper

Implementation outline:

```go
func (r *Runtime) CallReducer(ctx context.Context, name string, args []byte, opts ...CallReducerOption) (ReducerResult, error) {
    if ctx == nil {
        ctx = context.Background()
    }
    if strings.TrimSpace(name) == "" {
        return ReducerResult{}, ErrReducerNameRequired
    }
    if err := ctx.Err(); err != nil {
        return ReducerResult{}, err
    }
    if err := r.checkLocalCallReady(); err != nil {
        return ReducerResult{}, err
    }

    callOpts := applyCallReducerOptions(opts)
    caller := buildCallerContext(callOpts)

    responseCh := make(chan executor.ReducerResponse, 1)
    cmd := executor.CallReducerCmd{
        Request: executor.ReducerRequest{
            ReducerName: name,
            Args:        append([]byte(nil), args...),
            Caller:      caller,
            RequestID:   callOpts.RequestID,
            Source:      executor.CallSourceExternal,
            Flags:       callOpts.Flags,
        },
        ResponseCh: responseCh,
    }

    if err := r.executor.Submit(cmd); err != nil {
        return ReducerResult{}, err
    }

    select {
    case res := <-responseCh:
        return ReducerResult{
            Status:      res.Status,
            Error:       res.Error,
            ReturnBSATN: append([]byte(nil), res.ReturnBSATN...),
            TxID:        res.TxID,
        }, nil
    case <-ctx.Done():
        return ReducerResult{}, ctx.Err()
    }
}
```

Important details:
- Use the actual runtime-owned executor field name from V1-D.
- Check that executor is non-nil as part of readiness; if nil while health says ready, return a clear internal/not-ready error.
- Copy `args` and `ReturnBSATN` defensively.
- Do not use `ProtocolResponseCh`; local calls should not create protocol-heavy envelopes.

Run:

```bash
rtk go test . -run 'TestCallReducer(RequiresReadyRuntime|InvokesReducerThroughExecutor|UserErrorIsResultNotAdmissionError)' -count=1
```

Expected:
- local reducer tests pass or reveal lifecycle-helper adjustments needed.

## Task 6: Add failing tests for local read callback behavior

Objective: prove the read helper acquires, exposes, and closes snapshots safely.

Files:
- Modify: `runtime_local_test.go`

Test cases:

```go
func TestReadExposesCommittedSnapshotAndClosesIt(t *testing.T) {
    rt := buildStartedRuntimeWithSeedRows(t)
    defer rt.Close()

    called := false
    err := rt.Read(context.Background(), func(view LocalReadView) error {
        called = true
        rows, err := view.ScanTable("messages")
        if err != nil {
            return err
        }
        if len(rows) != 1 {
            t.Fatalf("rows len = %d, want 1", len(rows))
        }
        count, ok, err := view.RowCount("messages")
        if err != nil || !ok || count != 1 {
            t.Fatalf("RowCount = (%d,%v,%v), want (1,true,nil)", count, ok, err)
        }
        return nil
    })
    if err != nil {
        t.Fatalf("Read returned error: %v", err)
    }
    if !called {
        t.Fatal("callback was not called")
    }

    // Immediately call a reducer after Read returns. If the snapshot leaked,
    // the commit may block. Use a short timeout to catch leaks.
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    if _, err := rt.CallReducer(ctx, "send_message", []byte("after-read")); err != nil {
        t.Fatalf("CallReducer after Read returned error, possible leaked snapshot: %v", err)
    }
}

func TestReadClosesSnapshotWhenCallbackReturnsError(t *testing.T) {
    rt := buildStartedRuntimeWithSeedRows(t)
    defer rt.Close()

    want := errors.New("stop")
    err := rt.Read(context.Background(), func(LocalReadView) error { return want })
    if !errors.Is(err, want) {
        t.Fatalf("Read error = %v, want callback error", err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    if _, err := rt.CallReducer(ctx, "send_message", []byte("after-error")); err != nil {
        t.Fatalf("CallReducer after errored Read returned error, possible leaked snapshot: %v", err)
    }
}

func TestReadRejectsNilCallback(t *testing.T) {
    rt := buildStartedTestRuntime(t)
    defer rt.Close()

    if err := rt.Read(context.Background(), nil); !errors.Is(err, ErrReadCallbackRequired) {
        t.Fatalf("Read nil callback error = %v, want ErrReadCallbackRequired", err)
    }
}
```

Run:

```bash
rtk go test . -run 'TestRead(ExposesCommittedSnapshotAndClosesIt|ClosesSnapshotWhenCallbackReturnsError|RejectsNilCallback)' -count=1
```

Expected:
- fail until `Read` and `LocalReadView` are implemented.

## Task 7: Implement `Read` and `LocalReadView`

Objective: add safe callback-owned local reads.

Files:
- Modify: `runtime_local.go`
- Possibly create: `local_read.go`

Implementation outline:

```go
type localReadView struct {
    registry schema.SchemaRegistry
    view     store.CommittedReadView
}

func (r *Runtime) Read(ctx context.Context, fn func(LocalReadView) error) error {
    if ctx == nil {
        ctx = context.Background()
    }
    if fn == nil {
        return ErrReadCallbackRequired
    }
    if err := ctx.Err(); err != nil {
        return err
    }
    if err := r.checkLocalCallReady(); err != nil {
        return err
    }

    snapshot := r.state.Snapshot()
    defer snapshot.Close()

    if err := ctx.Err(); err != nil {
        return err
    }
    return fn(localReadView{registry: r.registry, view: snapshot})
}
```

Local view methods:
- Resolve table name through the schema registry.
- `ScanTable` copies rows into a new slice so callers cannot depend on snapshot lifetime after callback returns.
- `GetRow` returns `(row, true, nil)` when found, `(nil, false, nil)` when absent, and an error for unknown tables if using error-return style.
- `RowCount` returns `(0, false, nil)` or an unknown-table error consistently. Prefer error for unknown table so tooling gets clear feedback.

Run:

```bash
rtk go test . -run 'TestRead|TestCallReducer' -count=1
```

Expected:
- local reducer/read tests pass.

## Task 8: Optional shared SQL query helper only if low-risk

Objective: decide whether a V1-F `Query` convenience helper can be included without broadening the slice.

Files if implemented:
- Modify/create a small shared helper near current one-off query code, exact package to be decided during implementation.
- Modify: `runtime_local.go`
- Add tests: `runtime_local_test.go`

Decision gate:
- If sharing current one-off query evaluation requires major protocol refactors, skip this task and document that V1-F ships `Read` as the minimal local read/query helper.
- If sharing is small, extract a non-network helper that takes query string, identity/caller, `schema.SchemaLookup`, and `store.CommittedReadView`, and returns rows/errors without protocol envelopes.

Required tests if included:
- valid `SELECT * FROM messages` returns rows
- unknown table returns clear query error
- unsupported SQL preserves `query/sql.ErrUnsupportedSQL` via `errors.Is` where applicable
- callback/snapshot closure still happens on query errors

Run:

```bash
rtk go test . ./protocol ./query/sql -run 'Test(Query|OneOff|Parse)' -count=1
```

Expected:
- existing protocol one-off query tests still pass.

## Task 9: Add comments/docs that local calls are secondary APIs

Objective: make the public API intent explicit without expanding documentation beyond this slice.

Files:
- Modify: `runtime_local.go`
- Possibly modify: `docs/decomposition/hosted-runtime-v1-contract.md` only if implementation settles names that differ from the contract

Add package/API comments explaining:
- local calls are for tests, tooling, admin/maintenance flows, and in-process integrations
- WebSocket remains the primary external client model
- local reads use point-in-time snapshots and must not expose mutable store state
- context cancellation after reducer acceptance may return to the caller before the accepted transaction completes

Run:

```bash
rtk go test . -count=1
```

Expected:
- root package tests pass.

## Task 10: Format and validate

Objective: finish the implementation slice with focused and then broader validation.

Run:

```bash
rtk go fmt .
rtk go test . -count=1
rtk go test ./executor ./store ./protocol ./query/sql -count=1
rtk go vet . ./executor ./store ./protocol ./query/sql
rtk go test ./... -count=1
```

Expected:
- all touched-package tests pass
- broad tests pass unless an unrelated dirty working-tree failure exists; if so, report exact unrelated failures and preserve narrower passing gates

## Risks and guardrails

- Snapshot leaks can block commits. Use callback-owned snapshots and `defer Close()` before invoking caller code.
- Direct reducer invocation would bypass transaction/durability/subscription semantics. Always submit `executor.CallReducerCmd`.
- Protocol adapter reuse is tempting but wrong for the primary local reducer API because it is designed around WebSocket response envelopes and connection lifecycle.
- Local APIs must not become a REST/MCP/admin/control-plane surface. Keep them in-process methods on `Runtime` only.
- Do not invent v1.5 declared queries/views in this slice. A local `Read` helper is runtime access, not module query declaration.
- Do not use the former bundled demo command as an implementation source of truth.
- Keep unknown table/reducer behavior clear and testable; preserve executor/schema sentinel errors where existing contracts already provide them.

## Immediate next slice

After V1-F lands, the next planning/implementation slice is V1-G export and introspection foundation:

- module identity/version/metadata export
- schema information export
- reducer metadata export
- narrow runtime/module description for diagnostics
- no full v1.5 canonical contract, codegen, permissions, or migration metadata yet
