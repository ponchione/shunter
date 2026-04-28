# Hosted Runtime V1-C Runtime Build Pipeline Implementation Plan

> For Hermes: use the subagent-driven-development skill to execute this plan task-by-task if the user asks for implementation.

Status: concretely validated against the live repo on 2026-04-23.
Scope: V1-C only; planning artifact, not implementation.

Goal: make `shunter.Build(module, config)` own the hosted-runtime build/recovery foundation instead of returning only a schema-engine-backed shell, while still deferring started goroutines, lifecycle, network serving, local calls, and public shutdown APIs to later V1 slices.

Architecture: keep the root `shunter.Runtime` as the owner object. In V1-C, `Build` should freeze/build the module schema, normalize runtime config, open or bootstrap durable state, create the reducer registry from module registrations, and store a private runtime build plan that V1-D can start safely. Derive this from package contracts and targeted tests, not from example code.

Tech stack: Go, root package `github.com/ponchione/shunter`, existing `schema`, `store`, `commitlog`, `executor`, `subscription`, `protocol`, `auth`, and `types` packages, RTK-wrapped Go toolchain commands.

---

## Current grounded context

Read and verified while writing this plan:

- V1-A and V1-B implplans now live under `docs/hosted-runtime-planning/`.
- `docs/decomposition/hosted-runtime-version-phases.md` defines V1-C as the runtime build pipeline after V1-A/V1-B.
- `docs/hosted-runtime-implementation-roadmap.md` V1-3 says `Build(module, config)` should own subsystem assembly.
- `docs/decomposition/hosted-runtime-v1-contract.md` says the normal app path should not manually assemble kernel internals.
- Live repo reality at validation time: root-package V1-A/V1-B code is not present in this checkout yet (`module.go`, `config.go`, `runtime.go`, and `module_test.go` are absent). This plan is therefore stacked after the V1-A and V1-B implplans.
- Example binaries are not implementation sources of truth for this slice. Do not inspect or copy example code to design the runtime build pipeline.

Go API facts verified with `rtk go doc` / file inspection:

- `schema.Builder.Build(schema.EngineOptions) (*schema.Engine, error)` validates/freeze-builds schema and sets `b.built = true` on success.
- `schema.Engine` exposes `Registry()`, `ExportSchema()`, and `Start(context.Context) error`; current `Start` only checks schema compatibility against `StartupSnapshotSchema`.
- `schema.SchemaRegistry` exposes `Tables()`, `Reducer(name)`, `Reducers()`, `OnConnect()`, `OnDisconnect()`, and `Version()`.
- `commitlog.OpenAndRecoverDetailed(dir, reg)` returns `(*store.CommittedState, types.TxID, commitlog.RecoveryResumePlan, error)`.
- `commitlog.NewDurabilityWorkerWithResumePlan(...)` creates and starts a worker goroutine immediately.
- `executor.NewExecutor(...)` constructs an executor but does not start its `Run` goroutine.
- `executor.Executor.Startup(ctx, scheduler)` performs startup work; `Executor.Run(ctx)` and `Scheduler.Run(ctx)` are goroutine/lifecycle work.
- `subscription.NewManager(...)` constructs a manager without starting a goroutine.
- `subscription.NewFanOutWorker(...).Run(ctx)` is goroutine/lifecycle work.
- `protocol.Server` construction requires auth/protocol/executor/state wiring; actually serving remains `HandleSubscribe` behind an HTTP server.

## Validation conclusion for V1-C

V1-C must not extract implementation from example binaries into `Build`.

The reason is concrete: examples are consumer/demo code, not runtime architecture. The runtime pipeline should be specified from the kernel package contracts and hosted-runtime docs. Starting resources that require shutdown coordination also remains out of scope because public `Runtime.Close()` is explicitly V1-D and network serving is V1-E.

Therefore, V1-C should land the build/recovery foundation and private runtime-owned plan only:

- schema/module build
- config normalization
- data directory creation/defaulting
- recovery/open-or-bootstrap committed state
- reducer registry construction from module/schema registrations
- private fields needed to assemble executor/durability/subscription/protocol in V1-D/V1-E

V1-C should not start durability workers, executor loops, scheduler loops, fan-out workers, HTTP servers, or network handlers.

## Scope

In scope:

- normalize `Config` into private runtime options
- make `Build` open or bootstrap persistent committed state from the module registry
- add root-package private helpers based on kernel package contracts and tests
- create a private reducer registry from the built module registry
- store enough private runtime-owned state for V1-D to start/close the graph later
- pin this behavior with root-package tests using temp dirs

Out of scope:

- public `Runtime.Start`
- public `Runtime.Close`
- public `ListenAndServe`
- public `HTTPHandler`
- starting `commitlog.DurabilityWorker`
- starting executor/scheduler/fan-out goroutines
- opening sockets or serving HTTP
- local reducer/query APIs
- auth/signing-key configuration surface changes beyond keeping V1-A `AuthMode`
- v1.5/v2 export/codegen/permissions/migration work
- lower-level schema/store/commitlog/executor/protocol redesign

## Decisions to lock for V1-C

1. V1-C is a build/recovery foundation, not lifecycle start.
   - `Build` may create directories and initial snapshots.
   - `Build` must not start long-lived goroutines.
   - `Build` must not create a `DurabilityWorker`, because its constructor starts a goroutine and V1-C has no public `Close`.

2. Blank `Config.DataDir` remains valid.
   - Preserve V1-A's public-validation decision that blank data dir is accepted.
   - Normalize blank data dir to a runtime default private path.
   - Use `./shunter-data` as the V1-C default unless implementation review chooses a better documented runtime-owned default.

3. V1-C should use the module registry as source of truth.
   - Reducers and lifecycle hooks already registered through V1-B module wrappers should be read from `schema.SchemaRegistry`.
   - Build a private `executor.ReducerRegistry` from registry reducers/lifecycle handlers.
   - Do not maintain a second root-level reducer list in V1-C.

4. Protocol server construction remains deferred until V1-E unless V1-D proves it needs a dormant server handle.
   - Current `protocol.Server` construction depends on auth/signing-key and executor inbox state that are not fully expressed by V1-C config.
   - Do not invent auth/signing-key config in V1-C just to instantiate a server early.

5. Recovery/bootstrap belongs in V1-C.
   - Use the commitlog/store package contracts directly: attempt `commitlog.OpenAndRecoverDetailed`, handle `commitlog.ErrNoData` by registering tables in a fresh `store.CommittedState`, write an initial snapshot with `commitlog.NewSnapshotWriter`, and reopen.
   - This can be done during `Build` without starting goroutines.

## Files likely to modify

Assuming V1-A and V1-B files exist after stacking/landing:

- Modify: `config.go`
- Modify: `runtime.go`
- Modify: `module_test.go` or `runtime_test.go`
- Possible create: `runtime_build.go`
- Possible create: `runtime_build_test.go`

Do not edit unless implementation proves a direct compile necessity:

- `schema/build.go`
- `schema/builder.go`
- `schema/validate_schema.go`
- `commitlog/durability.go`
- `executor/executor.go`
- `protocol/server.go`


## Private implementation target

Expected private `Runtime` shape after V1-C, exact names flexible:

```go
type Runtime struct {
    moduleName string
    config Config

    engine *schema.Engine
    registry schema.SchemaRegistry

    dataDir string
    state *store.CommittedState
    recoveredTxID types.TxID
    resumePlan commitlog.RecoveryResumePlan

    reducers *executor.ReducerRegistry
}
```

Do not include started resources in V1-C:

```go
// Defer these to V1-D/V1-E:
// durability *commitlog.DurabilityWorker
// executor *executor.Executor
// scheduler *executor.Scheduler
// subscriptions *subscription.Manager
// fanOutInbox chan subscription.FanOutMessage
// fanOutWorker *subscription.FanOutWorker
// conns *protocol.ConnManager
// server *protocol.Server
```

If implementation wants to store a private `runtimePlan` struct instead of putting every field directly on `Runtime`, that is acceptable as long as tests can verify the V1-C behavior without public API expansion.

## Task 1: Reconfirm stack prerequisites before coding

Objective: ensure V1-C is implemented only after V1-A/V1-B root APIs exist.

Files:
- Read: `docs/hosted-runtime-planning/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`
- Read: `docs/hosted-runtime-planning/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`
- Inspect: `module.go`, `config.go`, `runtime.go`, `module_test.go`

Run:

```bash
rtk go list .
rtk go doc ./schema.SchemaRegistry
rtk go doc ./commitlog.OpenAndRecoverDetailed
rtk go doc ./commitlog.NewSnapshotWriter
rtk go doc ./executor.ReducerRegistry
```

Expected:
- `rtk go list .` succeeds after V1-A exists.
- `Module.SchemaVersion` and `Module.TableDef` tests from V1-B pass before V1-C starts.

Stop condition:
- If root package still does not exist, stop and apply V1-A/V1-B first. Do not mix V1-C with creating the root package from scratch.

## Task 2: Add a failing test proving Build bootstraps durable state

Objective: prove `Build` now does more than schema-shell construction by creating/opening committed state for the module's tables.

Files:
- Modify: `runtime_build_test.go` or `module_test.go`

Test shape:

```go
func TestBuildBootstrapsCommittedStateForModuleTables(t *testing.T) {
    dir := t.TempDir()
    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{
                {Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
                {Name: "body", Type: types.KindString},
            },
        })

    rt, err := Build(mod, Config{DataDir: dir})
    if err != nil {
        t.Fatalf("Build returned error: %v", err)
    }
    if rt.state == nil {
        t.Fatal("runtime state is nil")
    }
    if _, ok := rt.state.Table(0); !ok {
        t.Fatal("messages table was not registered in committed state")
    }
    if rt.resumePlan.NextTxID != 0 {
        t.Fatalf("resume next tx = %d, want 0 on first boot", rt.resumePlan.NextTxID)
    }
}
```

Run:

```bash
rtk go test . -run TestBuildBootstrapsCommittedStateForModuleTables -count=1
```

Expected:
- fails until V1-C runtime state/bootstrap fields and helper exist.

## Task 3: Add a failing test proving recovery reopens existing state

Objective: pin that `Build` uses `commitlog.OpenAndRecoverDetailed` on existing data rather than always overwriting state.

Files:
- Modify: `runtime_build_test.go` or `module_test.go`

Test shape:

```go
func TestBuildReopensExistingBootstrappedState(t *testing.T) {
    dir := t.TempDir()

    firstMod := validChatModule()
    first, err := Build(firstMod, Config{DataDir: dir})
    if err != nil {
        t.Fatalf("first Build returned error: %v", err)
    }
    if first.state == nil {
        t.Fatal("first runtime state is nil")
    }

    secondMod := validChatModule()
    second, err := Build(secondMod, Config{DataDir: dir})
    if err != nil {
        t.Fatalf("second Build returned error: %v", err)
    }
    if second.state == nil {
        t.Fatal("second runtime state is nil")
    }
    if second.registry.Version() != 1 {
        t.Fatalf("registry version = %d, want 1", second.registry.Version())
    }
}
```

Important: use a fresh module for the second build. V1-B preserves underlying `schema.ErrAlreadyBuilt` if the same module is built twice.

Run:

```bash
rtk go test . -run TestBuildReopensExistingBootstrappedState -count=1
```

Expected:
- fails until recovery/bootstrap helper exists.

## Task 4: Add a failing test proving reducer registry is derived from the module

Objective: prove V1-C captures module reducers/lifecycle hooks for later executor startup.

Files:
- Modify: `runtime_build_test.go` or `module_test.go`

Test shape:

```go
func TestBuildCreatesReducerRegistryFromModule(t *testing.T) {
    dir := t.TempDir()
    reduce := func(ctx *schema.ReducerContext, args []byte) ([]byte, error) { return nil, nil }
    onConnect := func(ctx *schema.ReducerContext) error { return nil }
    onDisconnect := func(ctx *schema.ReducerContext) error { return nil }

    mod := validChatModule().
        Reducer("send_message", reduce).
        OnConnect(onConnect).
        OnDisconnect(onDisconnect)

    rt, err := Build(mod, Config{DataDir: dir})
    if err != nil {
        t.Fatalf("Build returned error: %v", err)
    }
    if rt.reducers == nil || !rt.reducers.IsFrozen() {
        t.Fatal("runtime reducer registry is nil or not frozen")
    }
    if _, ok := rt.reducers.Lookup("send_message"); !ok {
        t.Fatal("send_message reducer missing")
    }
    if _, ok := rt.reducers.LookupLifecycle(executor.LifecycleOnConnect); !ok {
        t.Fatal("on-connect lifecycle reducer missing")
    }
    if _, ok := rt.reducers.LookupLifecycle(executor.LifecycleOnDisconnect); !ok {
        t.Fatal("on-disconnect lifecycle reducer missing")
    }
}
```

Run:

```bash
rtk go test . -run TestBuildCreatesReducerRegistryFromModule -count=1
```

Expected:
- fails until V1-C builds/stores the private executor reducer registry.

## Task 5: Implement config normalization

Objective: keep public config narrow while giving V1-C a concrete data dir and queue defaults.

Files:
- Modify: `config.go`
- Modify or create: `runtime_build.go`

Implementation shape:

```go
const defaultDataDir = "./shunter-data"
const defaultExecutorQueueCapacity = 256
const defaultDurabilityQueueCapacity = 256

func normalizeConfig(cfg Config) (Config, string, error) {
    if cfg.ExecutorQueueCapacity < 0 {
        return Config{}, "", errInvalidExecutorQueueCapacity
    }
    if cfg.DurabilityQueueCapacity < 0 {
        return Config{}, "", errInvalidDurabilityQueueCapacity
    }
    if cfg.AuthMode != AuthModeDev && cfg.AuthMode != AuthModeStrict {
        return Config{}, "", errInvalidAuthMode
    }

    normalized := cfg
    dataDir := strings.TrimSpace(cfg.DataDir)
    if dataDir == "" {
        dataDir = defaultDataDir
    }
    if normalized.ExecutorQueueCapacity == 0 {
        normalized.ExecutorQueueCapacity = defaultExecutorQueueCapacity
    }
    if normalized.DurabilityQueueCapacity == 0 {
        normalized.DurabilityQueueCapacity = defaultDurabilityQueueCapacity
    }
    return normalized, dataDir, nil
}
```

Guardrail:
- Preserve any V1-A tests that expect invalid config to fail before schema build.
- If V1-A intentionally preserves zero capacities as zero in `Runtime.Config()`, either update the test expectation deliberately for V1-C or store raw and normalized configs separately. Prefer storing raw public `Config()` and private normalized options if compatibility matters.

Run:

```bash
rtk go test . -run 'TestBuildRejects|TestRuntimeConfig|TestBuildBootstraps' -count=1
```

Expected:
- public validation tests still pass or are updated only for intentionally locked V1-C normalization behavior.

## Task 6: Implement state open/bootstrap helper

Objective: implement durable-state bootstrap from the store/commitlog package contracts under root runtime ownership.

Files:
- Modify or create: `runtime_build.go`

Implementation shape:

```go
func openOrBootstrapState(dataDir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, commitlog.RecoveryResumePlan, error) {
    if err := os.MkdirAll(dataDir, 0o755); err != nil {
        return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("mkdir data dir: %w", err)
    }

    committed, maxTxID, plan, err := commitlog.OpenAndRecoverDetailed(dataDir, reg)
    if err == nil {
        return committed, maxTxID, plan, nil
    }
    if !errors.Is(err, commitlog.ErrNoData) {
        return nil, 0, commitlog.RecoveryResumePlan{}, err
    }

    fresh := store.NewCommittedState()
    for _, tid := range reg.Tables() {
        ts, ok := reg.Table(tid)
        if !ok {
            return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("registry missing table %d", tid)
        }
        fresh.RegisterTable(tid, store.NewTable(ts))
    }
    if err := commitlog.NewSnapshotWriter(dataDir, reg).CreateSnapshot(fresh, 0); err != nil {
        return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("initial snapshot: %w", err)
    }
    return commitlog.OpenAndRecoverDetailed(dataDir, reg)
}
```

Run:

```bash
rtk go test . -run 'TestBuildBootstrapsCommittedStateForModuleTables|TestBuildReopensExistingBootstrappedState' -count=1
```

Expected:
- bootstrap/recovery tests pass.

## Task 7: Implement reducer-registry construction from schema registry

Objective: prepare the later executor startup graph without duplicating module registration state.

Files:
- Modify or create: `runtime_build.go`

Implementation shape:

```go
func buildExecutorReducerRegistry(reg schema.SchemaRegistry) (*executor.ReducerRegistry, error) {
    rr := executor.NewReducerRegistry()
    for _, name := range reg.Reducers() {
        h, ok := reg.Reducer(name)
        if !ok {
            return nil, fmt.Errorf("schema registry missing reducer %q", name)
        }
        if err := rr.Register(executor.RegisteredReducer{Name: name, Handler: h}); err != nil {
            return nil, err
        }
    }
    if h := reg.OnConnect(); h != nil {
        if err := rr.Register(executor.RegisteredReducer{Name: "on_connect", Handler: lifecycleReducerHandler(h), Lifecycle: executor.LifecycleOnConnect}); err != nil {
            return nil, err
        }
    }
    if h := reg.OnDisconnect(); h != nil {
        if err := rr.Register(executor.RegisteredReducer{Name: "on_disconnect", Handler: lifecycleReducerHandler(h), Lifecycle: executor.LifecycleOnDisconnect}); err != nil {
            return nil, err
        }
    }
    rr.Freeze()
    return rr, nil
}

func lifecycleReducerHandler(h func(*schema.ReducerContext) error) schema.ReducerHandler {
    return func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
        return nil, h(ctx)
    }
}
```

Guardrail:
- Confirm actual lifecycle reducer names do not conflict with schema reserved reducer-name rules. If `executor.Register` rejects these placeholder names, use names that match existing executor lifecycle conventions or add private constants in root package only.
- Do not add public lifecycle naming behavior in V1-C.

Run:

```bash
rtk go test . -run TestBuildCreatesReducerRegistryFromModule -count=1
```

Expected:
- reducer-registry test passes.

## Task 8: Wire V1-C helpers into Build

Objective: have `Build` return a runtime that owns schema, normalized config, recovered state, resume plan, and reducer registry.

Files:
- Modify: `runtime.go`
- Modify or create: `runtime_build.go`

Implementation order inside `Build`:

1. root validation from V1-A: nil module, blank name, negative queues, invalid auth mode
2. normalize config/data dir
3. call `mod.builder.Build(schema.EngineOptions{...})`
4. read `registry := engine.Registry()`
5. call `openOrBootstrapState(dataDir, registry)`
6. call `buildExecutorReducerRegistry(registry)`
7. return `Runtime` with private owner fields populated

Important:
- Do not call `engine.Start(ctx)` in V1-C unless the compatibility-check context is deliberately moved here and a test pins it.
- Do not call `commitlog.NewDurabilityWorkerWithResumePlan` in V1-C.
- Do not call `executor.NewExecutor` yet if it would require a real durability handle that only V1-D can own/close.
- Do not create `protocol.Server` yet unless V1-D/V1-E plan is revised to account for auth config and dormant server construction.

Run:

```bash
rtk go test . -run 'TestBuildBootstrapsCommittedStateForModuleTables|TestBuildReopensExistingBootstrappedState|TestBuildCreatesReducerRegistryFromModule' -count=1
```

Expected:
- all V1-C tests pass.

## Task 9: Focused validation

Objective: prove V1-C did not regress V1-A/V1-B or lower-level schema behavior.

Run:

```bash
rtk go fmt .
rtk go test . -count=1
rtk go test ./schema -count=1
rtk go vet . ./schema
```

Then, if the working tree allows it:

```bash
rtk go test ./... -count=1
```

Expected:
- root and schema gates pass
- broad tests pass, or unrelated dirty-state failures are reported without fixing OI-002/query/protocol code inside V1-C

---

## Verification checklist

V1-C is complete when all of the following are true:

- `Build` still preserves all V1-A validation behavior and V1-B successful explicit module builds.
- `Build` normalizes runtime config privately without widening the public config surface.
- `Build` creates or opens the data directory for a valid module.
- First boot writes an initial snapshot and reopens committed state successfully.
- Existing bootstrapped state can be reopened by a fresh equivalent module.
- `Runtime` privately owns committed state, recovered tx id, recovery resume plan, registry, and reducer registry.
- Reducers and lifecycle hooks registered through V1-B wrappers appear in the private executor reducer registry.
- No durability worker, executor loop, scheduler loop, fan-out worker, HTTP server, socket, `Start`, `Close`, `ListenAndServe`, `HTTPHandler`, local call API, v1.5, or v2 surface is added.
- Focused RTK validation passes.

## Risks and guardrails

1. Example-code extraction can accidentally import demo assumptions into runtime architecture.
   - Guardrail: do not copy or depend on example code. Use kernel package contracts, docs, and targeted tests as the source of truth.

2. Durability worker constructor starts a goroutine.
   - Guardrail: store `RecoveryResumePlan` in V1-C and create the worker only in V1-D when `Close` ownership exists.

3. Protocol server construction currently needs auth/executor details not represented in V1-C config.
   - Guardrail: defer protocol server construction to V1-E unless a later V1-D plan deliberately introduces a dormant server config seam.

4. Blank `DataDir` defaulting can surprise tests.
   - Guardrail: keep blank public config valid, but document and test the private normalized default.

5. Same module cannot be built twice after schema builder success.
   - Guardrail: recovery/reopen tests must use a fresh equivalent module, preserving V1-B `schema.ErrAlreadyBuilt` behavior.

## Historical sequencing note

The later hosted-runtime slices have since landed. Do not treat this completed
V1-C plan as a live handoff; use `HOSTED_RUNTIME_PLANNING_HANDOFF.md` for
current hosted-runtime status.
