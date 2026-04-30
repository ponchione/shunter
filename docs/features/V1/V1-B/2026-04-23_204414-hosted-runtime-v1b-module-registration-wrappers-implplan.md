# Hosted Runtime V1-B Module Registration Wrappers Implementation Plan

> For Hermes: use the subagent-driven-development skill to execute this plan task-by-task if the user asks for implementation.

Historical status: landed; retained as planning context only.

Goal: add the V1-B top-level module authoring wrappers so an explicitly versioned, non-empty module can build successfully through the root `shunter` API.

Architecture: keep `shunter.Module` as a thin owner over the existing `schema.Builder` seam. Do not redesign schema semantics and do not start runtime lifecycle/network work. V1-B should only expose root-package registration methods that delegate to the already-implemented schema builder and preserve existing schema-layer validation/errors.

Tech stack: Go, root package `github.com/ponchione/shunter`, existing `schema` and `types` packages, RTK-wrapped Go toolchain commands.

---

## Current grounded context

Read and verified while writing this plan:
- `docs/specs/hosted-runtime-version-phases.md` defines the V1-B target surface as:
  - `Module.SchemaVersion(...)`
  - `Module.TableDef(...)`
  - `Module.Reducer(...)`
  - `Module.OnConnect(...)`
  - `Module.OnDisconnect(...)`
- `docs/specs/hosted-runtime-v1-contract.md` keeps v1 module authoring explicit and imperative first.
- `schema.Builder` already exposes the exact lower-level primitives V1-B needs:
  - `SchemaVersion(v uint32) *Builder`
  - `TableDef(def TableDefinition, opts ...TableOption) *Builder`
  - `Reducer(name string, h ReducerHandler) *Builder`
  - `OnConnect(h func(*ReducerContext) error) *Builder`
  - `OnDisconnect(h func(*ReducerContext) error) *Builder`
- `schema.Builder.Build(...)` already enforces the key validation behavior V1-B should preserve, including:
  - `schema.ErrSchemaVersionNotSet`
  - `schema.ErrNoTables`
  - `schema.ErrDuplicateTableName`
  - `schema.ErrDuplicateReducerName`
  - `schema.ErrReservedReducerName`
  - `schema.ErrNilReducerHandler`
  - `schema.ErrDuplicateLifecycleReducer`
  - `schema.ErrAlreadyBuilt`
- The minimal successful lower-level shape is established by the `schema.Builder` API itself: `SchemaVersion(1)` + `TableDef(...)` + `Build(...)`.

Original live-repo reality while this plan was written:
- `rtk go list .` still failed with `no Go files in /home/ponchione/source/shunter`.
- This historical plan assumed V1-A would land first or that V1-B would be implemented stacked directly on top of the V1-A patch.

## Scope

In scope:
- add V1-B wrapper methods on `shunter.Module`
- make a non-empty, explicitly versioned module build successfully through `shunter.Build(...)`
- pin delegation/error-preservation behavior with root-package tests

Out of scope:
- `Runtime.Start`
- `Runtime.Close`
- `ListenAndServe`
- `HTTPHandler`
- network serving
- goroutine ownership
- local reducer/query APIs
- reflection helper wrappers like `RegisterTable[T]`
- export/introspection APIs beyond what internal tests need
- v1.5 query/view/codegen/permissions/migration work
- lower-level schema behavior changes unless a compile error forces a tiny compatibility edit

## Decisions to lock for V1-B

1. Explicit schema version remains required.
   - `Module.SchemaVersion(v)` is the root-package way to satisfy the existing schema requirement.
   - Do not default schema version inside `NewModule` or `Build`.
   - Root-package success in V1-B requires the caller to set a non-zero schema version.

2. `Module` remains a thin wrapper, not a parallel schema DSL.
   - Use the existing schema types directly in the root wrapper signatures:
     - `schema.TableDefinition`
     - `schema.TableOption`
     - `schema.ReducerHandler`
     - `schema.ReducerContext`
   - Do not invent root-level duplicate definition structs in V1-B.

3. Existing schema validation remains source of truth.
   - Root wrappers should delegate registration and let `Build` surface schema-layer validation.
   - Preserve `errors.Is(..., schema.ErrX)` behavior by wrapping rather than replacing schema errors.

4. Module build/freeze behavior follows the underlying builder.
   - A successful first `Build` should freeze the module through the builder’s existing `ErrAlreadyBuilt` behavior.
   - Do not add a separate root-level mutable/frozen state machine in this slice.

5. Reflection helpers remain convenience work for later.
   - V1-B should establish the explicit-first path only.

## Files to modify

Assuming V1-A files exist after stacking/landing:
- Modify: `module.go`
- Modify: `module_test.go`
- Modify: `runtime_test.go` or keep all root tests in `module_test.go`

Possible additional test-only helper file if needed:
- Create: `test_helpers_test.go`

Do not plan edits to these unless implementation proves a compile necessity:
- `schema/build.go`
- `schema/builder.go`
- `schema/validate_schema.go`


## Public API target for V1-B

Add these methods to `shunter.Module`:

```go
func (m *Module) SchemaVersion(v uint32) *Module
func (m *Module) TableDef(def schema.TableDefinition, opts ...schema.TableOption) *Module
func (m *Module) Reducer(name string, h schema.ReducerHandler) *Module
func (m *Module) OnConnect(h func(*schema.ReducerContext) error) *Module
func (m *Module) OnDisconnect(h func(*schema.ReducerContext) error) *Module
```

Implementation shape:
- each method is chainable and returns `m`
- each method delegates directly to `m.builder`
- no new validation is added in the wrapper layer beyond nil-module handling already owned by `Build`
- wrapper methods should not eagerly panic on nil handlers; preserve schema-layer validation timing and sentinels

Expected V1-B usage shape:

```go
mod := shunter.NewModule("chat").
    SchemaVersion(1).
    TableDef(schema.TableDefinition{
        Name: "messages",
        Columns: []schema.ColumnDefinition{
            {Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
            {Name: "body", Type: types.KindString},
        },
    }).
    Reducer("send_message", func(ctx *schema.ReducerContext, argBSATN []byte) ([]byte, error) {
        return nil, nil
    })
```

This slice does not need new `Runtime` methods. The already-planned V1-A `Build` path should now succeed for explicitly versioned non-empty modules.

---

## Task 1: Reconfirm stack prerequisites before coding

Objective: make sure V1-B is implemented on top of the V1-A root package instead of accidentally broadening scope.

Files:
- Read: `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`
- Read/inspect: `module.go`, `config.go`, `runtime.go`, `module_test.go` once V1-A exists

Step 1: Verify the root package exists.
Run:
```bash
rtk go list .
```
Expected:
- success after V1-A is present
- if it still says `no Go files`, stop and land/apply V1-A first

Step 2: Verify the lower-level builder methods still match this plan.
Run:
```bash
rtk go doc ./schema.Builder
rtk go doc ./schema.TableDefinition
rtk go doc ./schema.ReducerHandler
rtk go doc ./schema.ReducerContext
```
Expected:
- the delegation seam is unchanged

Step 3: Do not start implementation if V1-A is missing.
If the root package still does not exist, treat that as a hard prerequisite blocker, not a prompt to mix V1-A and unrelated V1-B/V1-C work.

---

## Task 2: Add the first failing success-path test for explicit module build

Objective: prove the first successful root-package build path that V1-B is supposed to unlock.

Files:
- Modify: `module_test.go`

Step 1: Add a same-package test (package `shunter`, not `shunter_test`) so the test can inspect private runtime state if needed.

Add a test shaped like:

```go
func TestBuildExplicitVersionedModuleSucceeds(t *testing.T) {
    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{
                {Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
                {Name: "body", Type: types.KindString},
            },
        })

    rt, err := Build(mod, Config{})
    if err != nil {
        t.Fatalf("Build returned error: %v", err)
    }
    if rt == nil {
        t.Fatal("Build returned nil runtime")
    }
    if got := rt.ModuleName(); got != "chat" {
        t.Fatalf("ModuleName = %q, want chat", got)
    }
    if got := rt.engine.Registry().Version(); got != 1 {
        t.Fatalf("registry version = %d, want 1", got)
    }
    if _, ts, ok := rt.engine.Registry().TableByName("messages"); !ok || ts == nil {
        t.Fatal("messages table missing from built registry")
    }
}
```

Step 2: Run the single test.
Run:
```bash
rtk go test . -run TestBuildExplicitVersionedModuleSucceeds -count=1
```
Expected:
- fail with missing `SchemaVersion` / `TableDef` methods until implementation lands

---

## Task 3: Add failing tests for wrapper delegation preserving schema validation

Objective: pin that V1-B does not replace lower-level validation behavior.

Files:
- Modify: `module_test.go`

Step 1: Add a no-table test.

```go
func TestBuildSchemaVersionWithoutTablesStillFailsAtSchemaLayer(t *testing.T) {
    mod := NewModule("chat").SchemaVersion(1)

    _, err := Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrNoTables) {
        t.Fatalf("expected ErrNoTables, got %v", err)
    }
}
```

Step 2: Add a duplicate-table test.

```go
func TestBuildDuplicateTableDefPreservesSchemaError(t *testing.T) {
    mod := NewModule("chat").SchemaVersion(1)
    def := schema.TableDefinition{
        Name: "messages",
        Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
    }
    mod.TableDef(def)
    mod.TableDef(def)

    _, err := Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrDuplicateTableName) {
        t.Fatalf("expected ErrDuplicateTableName, got %v", err)
    }
}
```

Step 3: Run just these tests.
Run:
```bash
rtk go test . -run 'TestBuildSchemaVersionWithoutTablesStillFailsAtSchemaLayer|TestBuildDuplicateTableDefPreservesSchemaError' -count=1
```
Expected:
- fail until the wrapper methods exist

---

## Task 4: Implement `Module.SchemaVersion` and `Module.TableDef`

Objective: unlock explicit schema versioning and table registration through the root package.

Files:
- Modify: `module.go`

Step 1: Add imports needed by the wrapper signatures.
Likely:
```go
import "github.com/ponchione/shunter/schema"
```

Step 2: Add chainable delegation methods.

```go
func (m *Module) SchemaVersion(v uint32) *Module {
    m.builder.SchemaVersion(v)
    return m
}

func (m *Module) TableDef(def schema.TableDefinition, opts ...schema.TableOption) *Module {
    m.builder.TableDef(def, opts...)
    return m
}
```

Step 3: Run the three tests from Tasks 2-3.
Run:
```bash
rtk go test . -run 'TestBuildExplicitVersionedModuleSucceeds|TestBuildSchemaVersionWithoutTablesStillFailsAtSchemaLayer|TestBuildDuplicateTableDefPreservesSchemaError' -count=1
```
Expected:
- pass if V1-A `Build` is already in place and uses `m.builder.Build(...)`
- otherwise fail only on wrapper tests scheduled later in this plan

---

## Task 5: Add failing reducer-wrapper tests

Objective: pin named reducer registration behavior through the root package.

Files:
- Modify: `module_test.go`

Step 1: Add a reducer registration success-path test.

```go
func TestBuildReducerWrapperRegistersReducer(t *testing.T) {
    handler := func(ctx *schema.ReducerContext, argBSATN []byte) ([]byte, error) {
        return nil, nil
    }

    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        }).
        Reducer("send_message", handler)

    rt, err := Build(mod, Config{})
    if err != nil {
        t.Fatalf("Build returned error: %v", err)
    }
    if _, ok := rt.engine.Registry().Reducer("send_message"); !ok {
        t.Fatal("send_message reducer missing from registry")
    }
}
```

Step 2: Add duplicate and nil-handler tests.

```go
func TestBuildDuplicateReducerWrapperPreservesSchemaError(t *testing.T) {
    handler := func(ctx *schema.ReducerContext, argBSATN []byte) ([]byte, error) { return nil, nil }

    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        }).
        Reducer("send_message", handler).
        Reducer("send_message", handler)

    _, err := Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrDuplicateReducerName) {
        t.Fatalf("expected ErrDuplicateReducerName, got %v", err)
    }
}

func TestBuildNilReducerWrapperPreservesSchemaError(t *testing.T) {
    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        }).
        Reducer("send_message", nil)

    _, err := Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrNilReducerHandler) {
        t.Fatalf("expected ErrNilReducerHandler, got %v", err)
    }
}
```

Step 3: Run these tests.
Run:
```bash
rtk go test . -run 'TestBuildReducerWrapperRegistersReducer|TestBuildDuplicateReducerWrapperPreservesSchemaError|TestBuildNilReducerWrapperPreservesSchemaError' -count=1
```
Expected:
- fail with missing `Reducer` method until implemented

---

## Task 6: Implement `Module.Reducer`

Objective: expose plain-function reducer registration through the root package.

Files:
- Modify: `module.go`

Step 1: Add the wrapper method.

```go
func (m *Module) Reducer(name string, h schema.ReducerHandler) *Module {
    m.builder.Reducer(name, h)
    return m
}
```

Step 2: Run the reducer tests.
Run:
```bash
rtk go test . -run 'TestBuildReducerWrapperRegistersReducer|TestBuildDuplicateReducerWrapperPreservesSchemaError|TestBuildNilReducerWrapperPreservesSchemaError' -count=1
```
Expected:
- all three pass

---

## Task 7: Add failing lifecycle-wrapper tests

Objective: pin `OnConnect` / `OnDisconnect` wrapper behavior and preserved schema sentinels.

Files:
- Modify: `module_test.go`

Step 1: Add lifecycle registration success test.

```go
func TestBuildLifecycleWrappersRegisterHandlers(t *testing.T) {
    onConnect := func(ctx *schema.ReducerContext) error { return nil }
    onDisconnect := func(ctx *schema.ReducerContext) error { return nil }

    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        }).
        OnConnect(onConnect).
        OnDisconnect(onDisconnect)

    rt, err := Build(mod, Config{})
    if err != nil {
        t.Fatalf("Build returned error: %v", err)
    }
    if rt.engine.Registry().OnConnect() == nil {
        t.Fatal("OnConnect handler missing")
    }
    if rt.engine.Registry().OnDisconnect() == nil {
        t.Fatal("OnDisconnect handler missing")
    }
}
```

Step 2: Add duplicate/nil lifecycle tests.

```go
func TestBuildDuplicateOnConnectWrapperPreservesSchemaError(t *testing.T) {
    handler := func(ctx *schema.ReducerContext) error { return nil }

    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        }).
        OnConnect(handler).
        OnConnect(handler)

    _, err := Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrDuplicateLifecycleReducer) {
        t.Fatalf("expected ErrDuplicateLifecycleReducer, got %v", err)
    }
}

func TestBuildNilOnDisconnectWrapperPreservesSchemaError(t *testing.T) {
    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        }).
        OnDisconnect(nil)

    _, err := Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrNilReducerHandler) {
        t.Fatalf("expected ErrNilReducerHandler, got %v", err)
    }
}
```

Step 3: Run the lifecycle tests.
Run:
```bash
rtk go test . -run 'TestBuildLifecycleWrappersRegisterHandlers|TestBuildDuplicateOnConnectWrapperPreservesSchemaError|TestBuildNilOnDisconnectWrapperPreservesSchemaError' -count=1
```
Expected:
- fail with missing lifecycle wrapper methods until implemented

---

## Task 8: Implement `Module.OnConnect` and `Module.OnDisconnect`

Objective: expose lifecycle handler registration through the root package without adding new semantics.

Files:
- Modify: `module.go`

Step 1: Add both wrapper methods.

```go
func (m *Module) OnConnect(h func(*schema.ReducerContext) error) *Module {
    m.builder.OnConnect(h)
    return m
}

func (m *Module) OnDisconnect(h func(*schema.ReducerContext) error) *Module {
    m.builder.OnDisconnect(h)
    return m
}
```

Step 2: Run the lifecycle tests.
Run:
```bash
rtk go test . -run 'TestBuildLifecycleWrappersRegisterHandlers|TestBuildDuplicateOnConnectWrapperPreservesSchemaError|TestBuildNilOnDisconnectWrapperPreservesSchemaError' -count=1
```
Expected:
- all three pass

---

## Task 9: Add a build-freeze regression test

Objective: pin that V1-B follows underlying builder freeze semantics instead of introducing silent rebuild behavior.

Files:
- Modify: `module_test.go`

Step 1: Add the test.

```go
func TestBuildSecondCallPreservesAlreadyBuiltError(t *testing.T) {
    mod := NewModule("chat").
        SchemaVersion(1).
        TableDef(schema.TableDefinition{
            Name: "messages",
            Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
        })

    rt, err := Build(mod, Config{})
    if err != nil || rt == nil {
        t.Fatalf("first Build failed: rt=%v err=%v", rt, err)
    }

    _, err = Build(mod, Config{})
    if err == nil || !errors.Is(err, schema.ErrAlreadyBuilt) {
        t.Fatalf("expected ErrAlreadyBuilt on second Build, got %v", err)
    }
}
```

Step 2: Run the test.
Run:
```bash
rtk go test . -run TestBuildSecondCallPreservesAlreadyBuiltError -count=1
```
Expected:
- pass once V1-B is fully implemented on top of V1-A build behavior

---

## Task 10: Run focused validation, then broader validation if the tree allows

Objective: verify the root package slice is complete without drifting into unrelated correctness cleanup.

Files:
- Modify only files from prior tasks

Step 1: Format root files.
Run:
```bash
rtk go fmt .
```
Expected:
- success

Step 2: Run focused root-package tests.
Run:
```bash
rtk go test . -count=1
```
Expected:
- pass

Step 3: Reconfirm schema package remains green.
Run:
```bash
rtk go test ./schema -count=1
rtk go vet . ./schema
```
Expected:
- pass

Step 4: Run broad tests if no unrelated dirty-state blocker appears.
Run:
```bash
rtk go test ./... -count=1
```
Expected:
- pass, or
- fail only for unrelated pre-existing OI-002/query/protocol issues that should be reported but not fixed inside V1-B

---

## Verification checklist

V1-B is complete when all of the following are true:
- root `shunter.Module` exposes `SchemaVersion`, `TableDef`, `Reducer`, `OnConnect`, and `OnDisconnect`
- all wrapper methods are chainable
- `Build(NewModule("chat").SchemaVersion(1).TableDef(...), Config{})` succeeds
- built runtime registry shows the expected schema version and table presence
- reducer registration works through the root wrapper
- lifecycle registration works through the root wrapper
- duplicate/nil/reserved validation still surfaces the existing schema sentinels via `errors.Is`
- second successful build on the same module preserves `schema.ErrAlreadyBuilt`
- no runtime lifecycle/network/local-call surfaces were added
- focused RTK validation passes, with any broader unrelated failures explicitly isolated

## Risks and guardrails

1. The original tree still lacked V1-A files.
   - Historical guardrail: do not blur this into an implementation request for both V1-A and V1-B unless the user explicitly wants stacked implementation.

2. Testability of success-path internals.
   - Guardrail: keep root tests in package `shunter` so they can inspect `rt.engine` until public export/introspection APIs arrive in later slices.

3. Temptation to introduce root-level duplicate schema types.
   - Guardrail: reuse `schema` package types directly in V1-B; wider root DSL design belongs later if ever needed.

4. Scope creep into reflection helpers or runtime lifecycle.
   - Guardrail: V1-B stops at explicit registration wrappers and build success through existing `Build`.

## Historical sequencing note

The later hosted-runtime slices have since landed. Do not treat this completed
V1-B plan as a live handoff; use `docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` for
current hosted-runtime status.
