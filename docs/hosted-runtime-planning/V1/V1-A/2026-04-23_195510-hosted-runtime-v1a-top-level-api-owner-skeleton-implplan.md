# Implplan: Hosted Runtime V1-A Top-Level API Owner Skeleton

Status: ready for implementation after review/acceptance
Scope: V1-A only; planning artifact, not implementation

## Goal

Introduce the first root package surface for `github.com/ponchione/shunter` without starting runtime services or changing lower-level schema semantics.

The V1-A public names are:

- `shunter.Module`
- `shunter.Config`
- `shunter.Runtime`
- `shunter.Build(module, config)`

This slice creates the owner vocabulary and public validation boundary. It deliberately does not make an empty module build successfully. A successful real `Build` path is deferred until V1-B module registration wrappers expose schema/version registration through `Module`.

## Decisions locked for this slice

1. Empty module behavior
   - `Build(shunter.NewModule("name"), shunter.Config{})` must fail in V1-A because no user schema/version has been registered.
   - Do not add `AllowEmptySchema`, fake tables, hidden default schema versions, or a smoke-only empty-engine mode.
   - The expected error should wrap or preserve the existing schema-layer validation error, likely `schema.ErrSchemaVersionNotSet` first. If the implementation order later reaches table validation, `schema.ErrNoTables` remains the correct schema-layer failure.

2. Schema version behavior
   - No implicit schema-version default in `NewModule`.
   - `Module.SchemaVersion(...)` belongs to V1-B.
   - Until V1-B lands, V1-A `Build` should clearly fail rather than secretly defaulting to version 1.

3. Lower-level schema semantics
   - Do not edit `schema.EngineOptions`, `schema.Builder.Build`, schema validation, or schema error behavior for V1-A.
   - Use the existing `schema.NewBuilder()` and `(*schema.Builder).Build(schema.EngineOptions)` seam as-is.

4. Package shape
   - The root package `github.com/ponchione/shunter` is the normal app-facing v1 package.
   - Do not add an `engine/` package in this slice.

## Grounded repo facts

Verified while writing this plan:

- `rtk go list .` currently fails with `no Go files in /home/gernsback/source/shunter`; V1-A creates the root package.
- `schema.NewBuilder() *schema.Builder` exists.
- `(*schema.Builder).Build(schema.EngineOptions) (*schema.Engine, error)` exists.
- `schema.EngineOptions` currently has `DataDir`, `ExecutorQueueCapacity`, `DurabilityQueueCapacity`, `EnableProtocol`, and `StartupSnapshotSchema`.
- `schema.Engine` exposes `Registry()`, `ExportSchema()`, and `Start(context.Context)`; V1-A must not call `Start`.
- Existing schema validation already has `ErrSchemaVersionNotSet`, `ErrNoTables`, and `ErrAlreadyBuilt`.

## Files to create

- `module.go`
- `config.go`
- `runtime.go`
- `module_test.go`
- `runtime_test.go` if separating tests improves readability

No existing production Go files should need edits for V1-A.

## Public API target

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

Implementation notes:

- Store `name string`, `version string`, `metadata map[string]string`, and private `builder *schema.Builder`.
- `NewModule(name)` initializes `builder` with `schema.NewBuilder()` but does not set schema version or register tables.
- Blank names may be constructed, but `Build` rejects blank/whitespace-only names.
- `Version` is chainable and stores the exact supplied string.
- `Metadata(values)` defensively copies the input map.
- `Metadata(nil)` should clear metadata to an empty state; pin this behavior.
- `MetadataMap()` returns a defensive copy.

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

Implementation notes:

- Keep `Config` scalar and runtime-focused.
- Do not embed lower-level protocol/auth/subsystem structs.
- `ListenAddr` and `AuthMode` are retained for future V1-D/V1-E behavior but do not start serving in V1-A.
- `DataDir == ""` remains valid.
- Negative queue capacities are invalid.
- Unknown `AuthMode` values are invalid.

### Runtime and Build

```go
type Runtime struct { /* private fields */ }

func Build(mod *Module, cfg Config) (*Runtime, error)
func (r *Runtime) ModuleName() string
func (r *Runtime) Config() Config
```

Implementation notes:

- `Runtime` stores `moduleName string`, `config Config`, and private `engine *schema.Engine` for later V1 phases.
- `Build` validation order:
  1. reject nil module
  2. reject blank module name after `strings.TrimSpace`
  3. reject negative executor queue capacity
  4. reject negative durability queue capacity
  5. reject unknown auth mode
  6. call `mod.builder.Build(schema.EngineOptions{...})`
  7. if schema build fails, return nil runtime and wrap the error with hosted-runtime context
  8. if schema build succeeds in a later V1-B-capable module, return `Runtime{moduleName, config, engine}`
- Map config to schema options exactly:

```go
schema.EngineOptions{
    DataDir:                 cfg.DataDir,
    ExecutorQueueCapacity:   cfg.ExecutorQueueCapacity,
    DurabilityQueueCapacity: cfg.DurabilityQueueCapacity,
    EnableProtocol:          cfg.EnableProtocol,
}
```

- Do not set `StartupSnapshotSchema`.
- Do not call `engine.Start(...)`.
- Do not start goroutines, open sockets, create HTTP handlers, or initialize protocol serving.
- `Runtime.Config()` returns a value copy.

## TDD steps

### 1. Add failing root tests first

Create root package tests before production code.

Module tests:

- `NewModule("chat")` returns a non-nil module.
- `Name()` returns `chat` exactly.
- `Version("v0.1.0")` is chainable and visible through `VersionString()`.
- `Metadata(input)` copies input; mutating `input` afterward does not affect `MetadataMap()`.
- `MetadataMap()` copies output; mutating the returned map does not affect later reads.
- `Metadata(nil)` clears metadata.
- Blank module name construction is allowed, but `Build` rejects it.

Build/config tests:

- nil module is rejected before schema build.
- blank/whitespace-only module name is rejected before schema build.
- negative executor queue capacity is rejected before schema build.
- negative durability queue capacity is rejected before schema build.
- invalid `AuthMode` is rejected before schema build.
- blank `DataDir` is accepted by public validation, so `Build(NewModule("chat"), Config{})` reaches schema build and fails with `schema.ErrSchemaVersionNotSet` or other schema-layer validation rather than a config error.
- `EnableProtocol` and `ListenAddr` are accepted/retained as config fields, but no serving method exists yet.
- If tests need to distinguish validation order, assert with `errors.Is` for schema errors and substring/field-specific checks for root validation errors; do not overfit full error strings.

Expected first run:

```bash
rtk go test .
```

It should fail with missing-symbol/root-package errors before production files exist.

### 2. Implement `Module`

Create `module.go` with the module shell and defensive metadata behavior.

Run:

```bash
rtk go test .
```

Expected: module symbols compile, but config/runtime/build tests still fail until those symbols exist.

### 3. Implement `Config`

Create `config.go` with `AuthMode` and `Config`.

Run:

```bash
rtk go test .
```

Expected: config symbols compile, but runtime/build tests still fail until `Build`/`Runtime` exist.

### 4. Implement `Runtime` and `Build`

Create `runtime.go` with validation, schema option mapping, and private schema engine ownership.

Run:

```bash
rtk go test .
```

Expected: root package tests pass. In V1-A, the normal empty-module build path should fail at schema validation rather than return a runtime.

### 5. Format and validate

Run:

```bash
rtk go fmt .
rtk go test . -count=1
rtk go test ./schema -count=1
rtk go test ./... -count=1
rtk go vet . ./schema
```

If broad tests fail because of unrelated dirty OI-002/query/protocol state, report the exact unrelated failures and preserve the narrower passing root/schema gates. Do not fix unrelated parity code inside V1-A.

## Files not to touch

Do not edit these for V1-A unless a compile error proves a direct need:

- `schema/build.go`
- `schema/builder.go`
- `schema/validate_schema.go`
- `protocol/*`
- `query/sql/*`
- v1.5/v2 docs

## Docs/handoff follow-through after implementation

After V1-A implementation lands, update only:

- `NEXT_SESSION_HANDOFF.md`
  - mark V1-A implemented or report blocker
  - point the next slice at V1-B module registration wrappers
- `TECH-DEBT.md` / hosted-runtime roadmap only if they still describe V1-A as not planned or not started

Do not churn v1.5/v2 docs during the implementation patch.

## Completion criteria

V1-A is complete when:

- root package `github.com/ponchione/shunter` exists and `rtk go list .` succeeds
- public `Module`, `Config`, `Runtime`, and `Build` symbols exist
- module metadata/version behavior is defensively pinned
- public `Build` validation is pinned for nil module, blank name, negative queues, and invalid auth mode
- `Build` maps accepted config into existing `schema.EngineOptions`
- empty-module build fails clearly through existing schema validation; no empty-schema escape hatch exists
- no runtime services, goroutines, sockets, lifecycle methods, local calls, registration wrappers, codegen, permissions, or migration metadata have been introduced
- targeted RTK gates pass, with any unrelated dirty-state broad failures explicitly isolated

## Immediate next slice after V1-A

V1-B module registration wrappers:

- `Module.SchemaVersion(...)`
- `Module.TableDef(...)`
- `Module.Reducer(...)`
- `Module.OnConnect(...)`
- `Module.OnDisconnect(...)`

V1-B should be the first slice where a non-empty, explicitly versioned module can build successfully through the top-level API.
