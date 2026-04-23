# Implplan: Hosted Runtime Top-Level API Skeleton

Status: ready for implementation. This is planning-only; no production code has been changed by this plan.

Supersedes stale plan files in `.hermes/plans/`, including the earlier hosted-runtime skeleton draft `2026-04-23_184706-hosted-runtime-top-level-api-skeleton-plan.md` and prior OI-002 parity plans.

## Goal

Create the first hosted-runtime v1 public surface in the root package `github.com/ponchione/shunter`:

- `shunter.Module`
- `shunter.Config`
- `shunter.Runtime`
- `shunter.Build(module, config)`

This first slice is intentionally validation-only. It creates the public owner objects and calls the existing private `schema.Builder.Build(...)`, but it must not start runtime services, move subsystem wiring out of `cmd/shunter-example/main.go`, or introduce v1.5/v2 surfaces.

## Grounded repo facts

Checked before writing this plan:

- `go.mod` module path is `github.com/ponchione/shunter`.
- `rtk go list .` currently fails with `no Go files in /home/gernsback/source/shunter`; creating root `.go` files creates the top-level import package.
- `schema.NewBuilder() *schema.Builder` exists.
- `(*schema.Builder).Build(schema.EngineOptions) (*schema.Engine, error)` exists.
- `schema.EngineOptions` currently has:
  - `DataDir string`
  - `ExecutorQueueCapacity int`
  - `DurabilityQueueCapacity int`
  - `EnableProtocol bool`
  - `StartupSnapshotSchema *SnapshotSchema`
- `schema.Engine` exposes `Registry()`, `ExportSchema()`, and `Start(context.Context)`, but this slice should keep the built engine private and must not call `Start`.

## Scope

Implement only:

- root package `package shunter`
- module identity shell with defensive metadata behavior
- scalar runtime config shell
- runtime owner shell with private schema engine
- `Build` validation and private schema-engine build
- tests for the above

Do not implement in this slice:

- `Runtime.Start`, `Close`, `ListenAndServe`, or `HTTPHandler`
- network serving or protocol handler ownership
- real subsystem graph assembly beyond the existing `schema.Engine` validation/build seam
- moving/replacing `cmd/shunter-example/main.go`
- local reducer/query APIs
- schema/reducer/lifecycle registration wrappers on `Module`
- `Runtime.ExportSchema()` or registry accessors unless a compile-time need appears
- codegen, contract snapshots, query/view declarations, permissions, migration metadata
- v1.5/v2 docs or behavior
- any OI-002/parity slice cleanup unless a direct compile failure requires compatibility work

## Files to create

- `module.go`
- `config.go`
- `runtime.go`
- `module_test.go`
- optionally `runtime_test.go` if test organization is clearer split

No existing production package should need edits for this skeleton.

## Target API

### Module

```go
type Module struct { /* private fields */ }

func NewModule(name string) *Module
func (m *Module) Name() string
func (m *Module) Version(v string) *Module
func (m *Module) VersionString() string
func (m *Module) Metadata(values map[string]string) *Module
func (m *Module) MetadataMap() map[string]string
```

Implementation details:

- Store `name string`, `version string`, `metadata map[string]string`, and private `builder *schema.Builder`.
- `NewModule(name)` initializes `builder` with `schema.NewBuilder()`.
- Blank names may be constructed, but `Build` rejects them after `strings.TrimSpace`.
- `Version` is chainable and stores the exact string passed.
- `Metadata` must defensively copy the input map.
- `Metadata(nil)` should clear or set empty metadata; choose one simple behavior and pin it.
- `MetadataMap()` must return a defensive copy so callers cannot mutate internal state.

### Config

```go
type AuthMode int

const (
    AuthModeDev AuthMode = iota
    AuthModeStrict
)

type Config struct {
    DataDir                 string
    ExecutorQueueCapacity   int
    DurabilityQueueCapacity int
    EnableProtocol          bool
    ListenAddr              string
    AuthMode                AuthMode
}
```

Implementation details:

- Keep `Config` scalar and runtime-focused.
- Do not embed lower-level `protocol.Options`, auth validators, or subsystem handles.
- `ListenAddr` and `AuthMode` are stored for future hosted-runtime slices but do not drive networking/auth behavior yet.
- `DataDir == ""` remains valid and maps through to schema engine defaults.
- Negative queue capacities are invalid.
- For `AuthMode`, prefer rejecting unknown values now because it is cheap and public API behavior should be pinned early:
  - valid: `AuthModeDev`, `AuthModeStrict`
  - invalid: any other value

### Runtime / Build

```go
type Runtime struct { /* private fields */ }

func Build(mod *Module, cfg Config) (*Runtime, error)
func (r *Runtime) ModuleName() string
func (r *Runtime) Config() Config
```

Implementation details:

- `Runtime` stores:
  - `moduleName string`
  - `config Config`
  - private `engine *schema.Engine`
- `Build` validation order:
  1. reject nil module
  2. reject blank module name after `strings.TrimSpace`
  3. reject negative executor queue capacity
  4. reject negative durability queue capacity
  5. reject unknown auth mode
  6. call `mod.builder.Build(schema.EngineOptions{...})`
- Map config to schema options exactly:

```go
schema.EngineOptions{
    DataDir:                 cfg.DataDir,
    ExecutorQueueCapacity:   cfg.ExecutorQueueCapacity,
    DurabilityQueueCapacity: cfg.DurabilityQueueCapacity,
    EnableProtocol:          cfg.EnableProtocol,
}
```

- Do not set `StartupSnapshotSchema` in this slice.
- Do not call `engine.Start(...)`.
- Do not start goroutines, open sockets, or initialize protocol serving.
- `Runtime.Config()` returns a value copy.

## TDD steps

### 1. Add failing root tests first

Create `module_test.go` / `runtime_test.go` before production code.

Module tests:

- `NewModule("chat")` returns a non-nil module.
- `Name()` returns `chat`.
- `Version("v0.1.0")` is chainable and visible through `VersionString()`.
- `Metadata(input)` copies input: mutating `input` afterward does not affect `MetadataMap()`.
- `MetadataMap()` copies output: mutating returned map does not affect subsequent `MetadataMap()`.
- blank name is allowed at construction but rejected by `Build`.

Runtime/build tests:

- `Build(NewModule("chat"), Config{})` returns non-nil runtime.
- `Runtime.ModuleName()` returns `chat`.
- `Runtime.Config()` returns all scalar config values.
- nil module is rejected.
- blank/whitespace-only module name is rejected.
- negative executor queue capacity is rejected.
- negative durability queue capacity is rejected.
- blank `DataDir` is allowed.
- invalid `AuthMode` is rejected.
- `EnableProtocol` and `ListenAddr` are retained in `Runtime.Config()` but no serving method exists yet.

Run and expect missing-symbol failures:

```bash
rtk go test .
```

### 2. Implement `Module`

Create `module.go` with the module shell and defensive metadata behavior.

Run:

```bash
rtk go test .
```

Expected: tests still fail until config/runtime/build symbols exist, but module symbols compile.

### 3. Implement `Config`

Create `config.go` with `AuthMode` and `Config`.

Run:

```bash
rtk go test .
```

Expected: tests still fail until runtime/build symbols exist.

### 4. Implement `Runtime` and `Build`

Create `runtime.go` with validation and private schema engine build.

Minimum error behavior:

- return non-nil `error` with human-readable messages
- messages should identify the invalid field, but tests should avoid overfitting to exact full strings unless useful

Run:

```bash
rtk go test .
```

Expected: root package tests pass.

### 5. Format and validate touched packages

Run:

```bash
rtk go fmt ./...
rtk go test .
rtk go test ./schema ./executor ./protocol ./subscription ./store ./commitlog
rtk go test ./...
```

If broad tests fail because of pre-existing dirty OI-002/parity changes, capture exact failures and rerun the root/schema-focused checks to prove this skeleton independently. Do not fix unrelated parity code inside this hosted-runtime skeleton slice unless it blocks compilation of the root package.

## Docs/handoff follow-through after implementation

After implementation lands, update only the docs needed to keep the next session current:

- `NEXT_SESSION_HANDOFF.md`
  - mark this skeleton implemented
  - point the next slice at module schema/reducer registration wrappers
- `TECH-DEBT.md` and/or hosted-runtime roadmap only if they still describe this skeleton as unplanned/not started

Do not churn v1.5/v2 docs during the skeleton patch.

## Completion criteria

Implementation is complete when:

- root import package `github.com/ponchione/shunter` exists
- app code can create a module, set version/metadata, create a config, and call `Build`
- `Build` validates nil/blank/negative/invalid-auth inputs
- `Runtime` stores module name/config/private `*schema.Engine`
- no networking, lifecycle methods, local call APIs, registration wrappers, codegen, permissions, or migration metadata have been introduced
- root and relevant package tests pass through the RTK gates above, or unrelated dirty-state failures are clearly isolated

## Immediate next slice after this one

Module schema/reducer registration wrappers:

- `Module.TableDef(...)`
- `Module.SchemaVersion(...)`
- `Module.Reducer(...)`
- `Module.OnConnect(...)`
- `Module.OnDisconnect(...)`

That follow-up should be its own TDD slice and should wrap/adapt the existing `schema.Builder` methods without starting the runtime graph.
