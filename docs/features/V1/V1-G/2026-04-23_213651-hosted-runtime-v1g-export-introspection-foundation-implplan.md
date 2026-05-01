# Hosted Runtime V1-G Export and Introspection Foundation Implementation Plan

Superseded note: this 2026-04-23 draft is retained as historical context only.
Use `2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`
and `00-current-execution-plan.md` for the live V1-G contract. The landed
surface is `ModuleDescription`, `RuntimeDescription`, `Module.Describe`,
`Runtime.ExportSchema`, and `Runtime.Describe`; do not revive the older
`ReducerDescription` or `RuntimeStatus` draft shape from this file.

> For Hermes: use the subagent-driven-development skill to execute this plan task-by-task if the user asks for implementation.

Status: concretely validated against the live repo on 2026-04-23.
Scope: V1-G only; planning artifact, not implementation.

Goal: expose enough module/runtime/schema/reducer introspection through the root hosted runtime API to support diagnostics now and to give V1.5 canonical contract export a clean foundation later.

Architecture: V1-G should layer a small root-package description/export surface over the module metadata captured by V1-A/V1-B and the schema export support already implemented in `schema.Engine.ExportSchema()`. Runtime descriptions should be detached snapshots suitable for diagnostics and later JSON contract assembly, but they must not become the full V1.5 `shunter.contract.json` artifact, client codegen, query/view declarations, permissions metadata, migration metadata, or control-plane API.

Tech stack: Go, root package `github.com/ponchione/shunter`, existing `schema` package export/registry APIs, V1-A through V1-F root runtime files, RTK-wrapped Go toolchain commands.

---

## Current grounded context

Read and verified while writing this plan:

- `docs/specs/hosted-runtime-version-phases.md` defines V1-G as the export and introspection foundation in the hosted-runtime sequence.
- `docs/specs/hosted-runtime-v1-contract.md` says `Runtime` should expose schema/module export and introspection as part of the minimum hosted runtime owner surface.
- `docs/hosted-runtime-implementation-roadmap.md` V1-6 says the export/introspection hook should reuse existing schema export work, define a small module description structure, and avoid the full v1.5 canonical contract.
- V1-A through V1-F plans under `docs/features/V1/` define the root `Module`, `Config`, `Runtime`, `Build`, module registration wrappers, private runtime build/lifecycle/network/local-call state, and explicitly defer export/introspection to V1-G.
- Live repo reality at validation time: the root `shunter` package is still absent in this checkout (`rtk go list .` reports `no Go files in /home/ponchione/source/shunter`). This plan is therefore stacked after the V1-A through V1-F implementation plans.
- The former bundled demo command is a demo consumer, not an implementation source of truth for V1-G runtime architecture.

Go API facts verified with `rtk go doc` and file inspection:

- `schema.Engine.ExportSchema() *schema.SchemaExport` already returns a detached JSON-friendly schema snapshot.
- `schema.SchemaExport` currently contains `Version uint32`, `Tables []schema.TableExport`, and `Reducers []schema.ReducerExport`.
- `schema.TableExport` contains `Name`, `Columns`, and `Indexes`.
- `schema.ColumnExport` contains `Name` and string `Type`.
- `schema.IndexExport` contains `Name`, column names, `Unique`, and `Primary`.
- `schema.ReducerExport` contains `Name` and `Lifecycle`.
- `schema.Engine.Registry() schema.SchemaRegistry` exposes the immutable registry.
- `schema.SchemaRegistry` exposes `Tables()`, `Table(...)`, `TableByName(...)`, `Reducers()`, `Reducer(name)`, `OnConnect()`, `OnDisconnect()`, and `Version()`.
- `schema.Engine.ExportSchema()` includes normal reducers and lifecycle reducers (`OnConnect`, `OnDisconnect`) in the exported reducer list, but reducer metadata is currently only name + lifecycle flag. There is no reducer argument/return signature metadata in the current schema builder or registry contracts.
- `schema.SchemaRegistry` returns defensive copies of table schemas and reducer name slices, so V1-G root descriptions can build detached snapshots without reaching into unexported registry fields.
- V1-A planned `Module` stores `name`, `version`, metadata map, and private `*schema.Builder`; V1-A planned `Runtime` stores module identity/config and private schema engine.
- V1-C planned `Runtime` stores `engine`, `registry`, `dataDir`, `state`, recovered tx, resume plan, and reducer registry; V1-D/V1-E/V1-F add lifecycle/network/local surfaces. V1-G should read private runtime state for diagnostics but should not expose lower-level handles.

## Validation conclusion for V1-G

V1-G should be the first hosted-runtime slice that exposes runtime/module/schema/reducer descriptions publicly through the root package.

The safest V1-G API is a small set of detached snapshot methods:

```go
func (m *Module) Describe() ModuleDescription
func (r *Runtime) Describe() RuntimeDescription
func (r *Runtime) ExportSchema() *schema.SchemaExport
```

`ModuleDescription` is about authored module identity and declared metadata that is available before build. `RuntimeDescription` is about the built runtime owner and should include module identity, normalized/public config summary, lifecycle/health summary if V1-D exists, and schema/reducer summaries. `ExportSchema()` should delegate to the built `schema.Engine.ExportSchema()` or registry-backed equivalent and should return a fresh detached copy every call.

Reducer metadata in V1-G should intentionally stay narrow because the current schema registry does not carry reducer argument schemas, return schemas, permissions, query/view attachments, or migration metadata. V1-G should expose reducer names, lifecycle classification, and source category only. It should not fake richer metadata that would later be mistaken for canonical contract support.

V1-G must not add canonical JSON contract export, codegen, query/view declarations, permission/read-model metadata, migration metadata, admin/control-plane APIs, new network APIs, local call behavior, or example replacement.

## Scope

In scope:

- expose module identity/version/metadata snapshots from `Module`
- expose built runtime module identity and narrow runtime diagnostic state from `Runtime`
- expose schema export by reusing `schema.Engine.ExportSchema()` / `schema.SchemaExport`
- expose reducer metadata already known to the schema registry/export path: reducer name and lifecycle classification
- add detached-copy tests for metadata maps and exported schema/description slices
- document that this is a V1 foundation for later V1.5 canonical contract export, not the canonical contract itself
- keep descriptions safe to call without starting network serving or local calls

Out of scope:

- canonical `shunter.contract.json`
- deterministic canonical JSON writer or output-path config
- client codegen/bindings
- query/view declarations
- permissions/read-model metadata
- migration metadata or contract diff tooling
- REST/MCP/admin/control-plane APIs
- multi-module hosting
- new network serving APIs; V1-E owns network surface
- local reducer/query calls; V1-F owns local calls
- hello-world replacement
- broad SQL/view system
- lower-level schema redesign solely to make export richer

## Decisions to lock for V1-G

1. `ExportSchema()` is schema-only and reuses existing schema export support.
   - Public target:
     ```go
     func (r *Runtime) ExportSchema() *schema.SchemaExport
     ```
   - It should call `r.engine.ExportSchema()` if `r.engine` exists.
   - If V1-C/V1-D stores `r.registry` but `r.engine` can be nil only in invalid partial states, prefer returning an empty detached export or a clear error-bearing description from `Describe`; do not expose a raw registry handle.
   - Because `schema.Engine.ExportSchema()` returns a detached snapshot, V1-G should not duplicate table/index traversal in root code unless prior slices remove the engine field.

2. `Module.Describe()` describes authored module identity, not runtime state.
   - Public target:
     ```go
     type ModuleDescription struct {
         Name     string            `json:"name"`
         Version  string            `json:"version,omitempty"`
         Metadata map[string]string `json:"metadata,omitempty"`
     }

     func (m *Module) Describe() ModuleDescription
     ```
   - It should be valid before build.
   - It should return detached metadata maps.
   - Nil module behavior should be safe if the method can be called on nil: return zero description rather than panic, unless existing root package style consistently avoids nil receiver support. Tests should pin whichever behavior is chosen.
   - Do not include schema/reducer data on `ModuleDescription` unless V1-B implementation already keeps safe public declaration snapshots. The root `Module` wraps `schema.Builder`, whose public inspection surface is intentionally absent before build.

3. `Runtime.Describe()` is narrow diagnostics, not a control-plane API.
   - Public target:
     ```go
     type RuntimeDescription struct {
         Module   ModuleDescription    `json:"module"`
         Schema   *schema.SchemaExport `json:"schema,omitempty"`
         Runtime  RuntimeStatus        `json:"runtime"`
         Reducers []ReducerDescription `json:"reducers,omitempty"`
     }
     ```
   - Exact field names can move during implementation, but the description should include module identity, schema version/export, reducer summary, and lifecycle/health summary if V1-D has introduced those concepts.
   - It should not expose `*store.CommittedState`, `schema.SchemaRegistry`, executor, protocol server, connection manager, durability worker, goroutine channels, or private data-dir internals beyond narrow diagnostics.

4. Runtime status should preserve V1-D health semantics without redesigning lifecycle.
   - If V1-D exposes public `Ready()` / `Health()` / state access, `Describe()` should use those public or private shared helpers.
   - If V1-D only has private lifecycle fields, add a small internal snapshot helper rather than duplicating state transition logic.
   - Public status shape may be:
     ```go
     type RuntimeStatus struct {
         State   string `json:"state"`
         Ready   bool   `json:"ready"`
         Serving bool   `json:"serving,omitempty"`
         LastError string `json:"last_error,omitempty"`
     }
     ```
   - Do not add new lifecycle states or behavior in V1-G. Describe existing state only.

5. Reducer metadata stays honest and narrow.
   - Public target:
     ```go
     type ReducerDescription struct {
         Name      string `json:"name"`
         Lifecycle bool   `json:"lifecycle"`
         Kind      string `json:"kind,omitempty"` // optional: "reducer", "on_connect", "on_disconnect"
     }
     ```
   - Reducer names and lifecycle flags can come from `schema.SchemaExport.Reducers`.
   - Do not invent argument schemas, return schemas, auth/permission metadata, read-model metadata, query/view links, or codegen names in V1-G.
   - If lifecycle names are currently exported as `OnConnect` / `OnDisconnect`, preserve those names in V1-G rather than renaming silently.

6. Descriptions must be detached snapshots.
   - Mutating a returned metadata map must not mutate the module/runtime.
   - Mutating returned schema export slices/fields must not mutate later `ExportSchema()` / `Describe()` results.
   - Mutating returned reducer description slices must not mutate later descriptions.

7. `Describe()` and `ExportSchema()` do not require a started runtime.
   - A successfully built runtime should be introspectable before `Start(ctx)`.
   - Describing during starting/ready/closing/closed should be safe and should report current status rather than returning local-call readiness errors.
   - `ExportSchema()` should remain valid after `Close()` as long as the runtime object still owns the built schema engine. If implementation decides closed runtime introspection should be restricted, it must preserve a clear sentinel and tests; the preferred V1-G behavior is that static module/schema descriptions remain available after close.

8. JSON tags are allowed, but canonical JSON is not V1-G.
   - Add JSON tags to description structs because diagnostics/tools may marshal them.
   - Do not add deterministic canonical marshaling, file output, CLI commands, or `shunter.contract.json` defaults in this slice.

## Files likely to modify

Assuming V1-A through V1-F files exist after stacking/landing:

- Modify: `module.go`
- Modify: `runtime.go`
- Create or modify: `runtime_export.go`
- Create: `runtime_export_test.go`
- Possibly modify: `runtime_lifecycle.go` if a private state snapshot helper is needed for `RuntimeDescription.Runtime`
- Possibly modify: `config.go` only if a narrow public config summary type is already planned by prior slices and belongs in `RuntimeDescription`

Avoid editing unless implementation proves a direct need:

- `schema/export.go`
- `schema/registry.go`
- `schema/builder.go`
- `executor/*`
- `store/*`
- `protocol/*`
- `subscription/*`

Permitted lower-level schema edit only if implementation chooses to improve existing export detachment/tests without changing semantics:

- Add tests in `schema/export_test.go` for any already-existing export behavior that V1-G depends on and is not sufficiently pinned. Do not expand `schema.SchemaExport` with v1.5 metadata in V1-G.

## Public API target for V1-G

Preferred minimum API:

```go
type ModuleDescription struct {
    Name     string            `json:"name"`
    Version  string            `json:"version,omitempty"`
    Metadata map[string]string `json:"metadata,omitempty"`
}

func (m *Module) Describe() ModuleDescription

type ReducerDescription struct {
    Name      string `json:"name"`
    Lifecycle bool   `json:"lifecycle"`
    Kind      string `json:"kind,omitempty"`
}

type RuntimeStatus struct {
    State     string `json:"state"`
    Ready     bool   `json:"ready"`
    Serving   bool   `json:"serving,omitempty"`
    LastError string `json:"last_error,omitempty"`
}

type RuntimeDescription struct {
    Module   ModuleDescription    `json:"module"`
    Runtime  RuntimeStatus        `json:"runtime"`
    Schema   *schema.SchemaExport `json:"schema,omitempty"`
    Reducers []ReducerDescription `json:"reducers,omitempty"`
}

func (r *Runtime) Describe() RuntimeDescription
func (r *Runtime) ExportSchema() *schema.SchemaExport
```

Acceptable simplification if V1-D lifecycle state names differ:

```go
type RuntimeStatus struct {
    Ready bool `json:"ready"`
}
```

But prefer carrying the existing V1-D health/readiness state if it exists, because diagnostics are explicitly in V1-G scope.

Do not add in V1-G:

```go
// V1.5:
// func (r *Runtime) ExportContract(...)
// func (m *Module) Query(...)
// func (m *Module) View(...)
// type Contract struct { ... permissions/migrations/codegen ... }

// Examples:
// rewrite or hello-world changes

// Later adapters/control plane:
// CLI/admin/REST/MCP description endpoints
```

## Private implementation target

Expected private helpers, exact names flexible:

```go
func describeModuleFields(name, version string, metadata map[string]string) ModuleDescription
func copyStringMap(in map[string]string) map[string]string
func reducerDescriptionsFromSchemaExport(exp *schema.SchemaExport) []ReducerDescription
func (r *Runtime) runtimeStatusSnapshot() RuntimeStatus
```

`runtimeStatusSnapshot` should reuse V1-D/V1-E lifecycle/serving state under the existing mutex/atomic discipline. It should not introduce a parallel lifecycle state machine just for export.

## Task 1: Reconfirm stack prerequisites before coding

Objective: ensure V1-G is implemented after the root runtime/module APIs and prior V1 slices, without smuggling earlier-slice work into this slice.

Files:
- Read: this plan
- Read: V1-A through V1-F plans under `docs/features/V1/`
- Inspect once prior slices exist: `module.go`, `config.go`, `runtime.go`, `runtime_build.go`, `runtime_lifecycle.go`, `runtime_network.go`, `runtime_local.go`

Run:

```bash
rtk go list .
rtk go doc ./schema.Engine.ExportSchema
rtk go doc ./schema.SchemaExport
rtk go doc ./schema.SchemaRegistry
rtk go doc ./schema.Engine.Registry
```

Expected:
- `rtk go list .` succeeds after V1-A exists.
- `Runtime` owns a built `*schema.Engine` or `schema.SchemaRegistry` after V1-C.
- V1-D lifecycle tests, V1-E network tests, and V1-F local-call tests pass before V1-G implementation starts.

Stop condition:
- If the root package or V1-C built schema/runtime fields do not exist, stop and apply/land earlier slices first. Do not implement V1-G by creating the root package, build pipeline, lifecycle, network, or local-call APIs from scratch.

## Task 2: Add failing tests for `Module.Describe`

Objective: pin module identity/version/metadata export and detachment before implementation.

Files:
- Create or modify: `runtime_export_test.go` or `module_test.go`

Test shape:

```go
func TestModuleDescribeReturnsIdentityVersionAndMetadataSnapshot(t *testing.T) {
    metadata := map[string]string{"owner": "tests", "tier": "dev"}
    mod := NewModule("chat").Version("v0.1.0").Metadata(metadata)

    desc := mod.Describe()
    if desc.Name != "chat" {
        t.Fatalf("Name = %q, want chat", desc.Name)
    }
    if desc.Version != "v0.1.0" {
        t.Fatalf("Version = %q, want v0.1.0", desc.Version)
    }
    if desc.Metadata["owner"] != "tests" || desc.Metadata["tier"] != "dev" {
        t.Fatalf("Metadata = %#v", desc.Metadata)
    }

    metadata["owner"] = "mutated-input"
    desc.Metadata["tier"] = "mutated-output"

    again := mod.Describe()
    if again.Metadata["owner"] != "tests" || again.Metadata["tier"] != "dev" {
        t.Fatalf("metadata was not detached: %#v", again.Metadata)
    }
}
```

Optional nil receiver test if matching root style:

```go
func TestNilModuleDescribeReturnsZeroDescription(t *testing.T) {
    var mod *Module
    if desc := mod.Describe(); desc.Name != "" || desc.Metadata != nil {
        t.Fatalf("nil module description = %#v", desc)
    }
}
```

Run:

```bash
rtk go test . -run 'Test(ModuleDescribe|NilModuleDescribe)' -count=1
```

Expected:
- fail with missing `Module.Describe` / `ModuleDescription` symbols.

## Task 3: Implement `ModuleDescription` and `Module.Describe`

Objective: expose authored module identity without touching runtime or schema export yet.

Files:
- Modify: `module.go`
- Possibly create: `runtime_export.go` if keeping description structs together

Implementation outline:

```go
type ModuleDescription struct {
    Name     string            `json:"name"`
    Version  string            `json:"version,omitempty"`
    Metadata map[string]string `json:"metadata,omitempty"`
}

func (m *Module) Describe() ModuleDescription {
    if m == nil {
        return ModuleDescription{}
    }
    return ModuleDescription{
        Name:     m.name,
        Version:  m.version,
        Metadata: copyStringMap(m.metadata),
    }
}
```

Run:

```bash
rtk go test . -run 'Test(ModuleDescribe|NilModuleDescribe)' -count=1
```

Expected:
- module description tests pass.

## Task 4: Add failing tests for `Runtime.ExportSchema`

Objective: prove the root runtime exposes existing schema export support as a detached snapshot.

Files:
- Modify/create: `runtime_export_test.go`

Test shape:

```go
func TestRuntimeExportSchemaDelegatesToBuiltSchemaExport(t *testing.T) {
    rt := buildTestRuntime(t) // helper from prior slices: versioned module with messages table and reducer

    exp := rt.ExportSchema()
    if exp == nil {
        t.Fatal("ExportSchema returned nil")
    }
    if exp.Version != 1 {
        t.Fatalf("schema version = %d, want 1", exp.Version)
    }
    if len(exp.Tables) == 0 || exp.Tables[0].Name != "messages" {
        t.Fatalf("tables = %#v, want first user table messages", exp.Tables)
    }
    if len(exp.Reducers) == 0 || exp.Reducers[0].Name == "" {
        t.Fatalf("reducers missing: %#v", exp.Reducers)
    }
}

func TestRuntimeExportSchemaReturnsDetachedSnapshot(t *testing.T) {
    rt := buildTestRuntime(t)

    exp := rt.ExportSchema()
    exp.Version = 99
    exp.Tables[0].Name = "mutated"
    exp.Reducers = append(exp.Reducers, schema.ReducerExport{Name: "mutated"})

    again := rt.ExportSchema()
    if again.Version == 99 || again.Tables[0].Name == "mutated" {
        t.Fatalf("ExportSchema did not return detached snapshot: %#v", again)
    }
    for _, reducer := range again.Reducers {
        if reducer.Name == "mutated" {
            t.Fatalf("mutated reducer leaked into later export: %#v", again.Reducers)
        }
    }
}
```

Notes:
- Use whatever test helper V1-B/V1-C created for a valid built runtime.
- If system tables appear after user tables, assert user table presence by scanning names rather than assuming exact list length.
- Do not require runtime `Start(ctx)`; schema export should work after successful `Build`.

Run:

```bash
rtk go test . -run 'TestRuntimeExportSchema' -count=1
```

Expected:
- fail until `Runtime.ExportSchema` exists.

## Task 5: Implement `Runtime.ExportSchema`

Objective: expose schema export through root runtime without duplicating lower-level schema traversal.

Files:
- Create/modify: `runtime_export.go`
- Possibly modify: `runtime.go` only if prior slices did not retain `engine *schema.Engine`

Implementation outline:

```go
func (r *Runtime) ExportSchema() *schema.SchemaExport {
    if r == nil || r.engine == nil {
        return &schema.SchemaExport{}
    }
    return r.engine.ExportSchema()
}
```

If V1-C ended with `r.registry` but no `r.engine`, prefer adding a small private helper that uses the already-built engine if available. Only duplicate export traversal from registry if the engine field truly does not exist, and keep behavior aligned with `schema.Engine.ExportSchema()`.

Run:

```bash
rtk go test . -run 'TestRuntimeExportSchema' -count=1
```

Expected:
- runtime schema export tests pass.

## Task 6: Add failing tests for reducer descriptions

Objective: pin reducer metadata as name + lifecycle only, and preserve lifecycle reducer classification.

Files:
- Modify: `runtime_export_test.go`

Test shape:

```go
func TestRuntimeDescribeIncludesReducerDescriptions(t *testing.T) {
    rt := buildRuntimeWithReducersAndLifecycle(t)

    desc := rt.Describe()
    reducers := map[string]ReducerDescription{}
    for _, reducer := range desc.Reducers {
        reducers[reducer.Name] = reducer
    }

    send, ok := reducers["send_message"]
    if !ok {
        t.Fatalf("send_message missing from reducer descriptions: %#v", desc.Reducers)
    }
    if send.Lifecycle {
        t.Fatalf("send_message lifecycle = true, want false")
    }

    onConnect, ok := reducers["OnConnect"]
    if !ok {
        t.Fatalf("OnConnect missing from reducer descriptions: %#v", desc.Reducers)
    }
    if !onConnect.Lifecycle {
        t.Fatalf("OnConnect lifecycle = false, want true")
    }
}

func TestRuntimeDescribeReducerDescriptionsAreDetached(t *testing.T) {
    rt := buildRuntimeWithReducersAndLifecycle(t)

    desc := rt.Describe()
    desc.Reducers[0].Name = "mutated"

    again := rt.Describe()
    if len(again.Reducers) == 0 || again.Reducers[0].Name == "mutated" {
        t.Fatalf("reducer descriptions were not detached: %#v", again.Reducers)
    }
}
```

Run:

```bash
rtk go test . -run 'TestRuntimeDescribe.*Reducer' -count=1
```

Expected:
- fail until `Runtime.Describe`, `RuntimeDescription`, and `ReducerDescription` exist.

## Task 7: Implement reducer description mapping

Objective: derive root reducer descriptions from schema export without inventing richer metadata.

Files:
- Modify: `runtime_export.go`

Implementation outline:

```go
type ReducerDescription struct {
    Name      string `json:"name"`
    Lifecycle bool   `json:"lifecycle"`
    Kind      string `json:"kind,omitempty"`
}

func reducerDescriptionsFromSchemaExport(exp *schema.SchemaExport) []ReducerDescription {
    if exp == nil || len(exp.Reducers) == 0 {
        return nil
    }
    out := make([]ReducerDescription, len(exp.Reducers))
    for i, reducer := range exp.Reducers {
        kind := "reducer"
        if reducer.Lifecycle {
            switch reducer.Name {
            case "OnConnect":
                kind = "on_connect"
            case "OnDisconnect":
                kind = "on_disconnect"
            default:
                kind = "lifecycle"
            }
        }
        out[i] = ReducerDescription{Name: reducer.Name, Lifecycle: reducer.Lifecycle, Kind: kind}
    }
    return out
}
```

Run:

```bash
rtk go test . -run 'TestRuntimeDescribe.*Reducer' -count=1
```

Expected:
- compile may still fail until `Runtime.Describe` exists; helper behavior should be ready.

## Task 8: Add failing tests for `Runtime.Describe` module/schema/runtime status

Objective: prove the runtime description includes module identity, schema export, and lifecycle diagnostics without requiring started runtime.

Files:
- Modify: `runtime_export_test.go`

Test shape:

```go
func TestRuntimeDescribeIncludesModuleSchemaAndStatusBeforeStart(t *testing.T) {
    rt := buildTestRuntimeWithModuleMetadata(t, map[string]string{"owner": "tests"})

    desc := rt.Describe()
    if desc.Module.Name != "chat" {
        t.Fatalf("module name = %q, want chat", desc.Module.Name)
    }
    if desc.Module.Metadata["owner"] != "tests" {
        t.Fatalf("module metadata = %#v", desc.Module.Metadata)
    }
    if desc.Schema == nil || desc.Schema.Version != 1 {
        t.Fatalf("schema = %#v, want version 1", desc.Schema)
    }
    if desc.Runtime.Ready {
        t.Fatalf("runtime Ready before Start = true, want false")
    }
}

func TestRuntimeDescribeStaticDataAvailableAfterClose(t *testing.T) {
    rt := buildStartedTestRuntime(t)
    if err := rt.Close(); err != nil {
        t.Fatal(err)
    }

    desc := rt.Describe()
    if desc.Module.Name == "" || desc.Schema == nil || len(desc.Schema.Tables) == 0 {
        t.Fatalf("static description missing after Close: %#v", desc)
    }
}
```

Adjust status assertions to match the actual V1-D state names/helpers. The important behavior is:
- `Describe` is safe before start.
- `Describe` reports not-ready before start.
- static module/schema data remains available after close.

Run:

```bash
rtk go test . -run 'TestRuntimeDescribe(IncludesModuleSchemaAndStatusBeforeStart|StaticDataAvailableAfterClose)' -count=1
```

Expected:
- fail until runtime description/status exists.

## Task 9: Implement `RuntimeDescription`, `RuntimeStatus`, and `Runtime.Describe`

Objective: expose the narrow diagnostic snapshot while reusing module/schema/reducer helpers.

Files:
- Modify: `runtime_export.go`
- Possibly modify: `runtime_lifecycle.go` for a private status snapshot helper
- Possibly modify: `runtime.go` if V1-C did not retain enough module identity fields

Implementation outline:

```go
type RuntimeStatus struct {
    State     string `json:"state"`
    Ready     bool   `json:"ready"`
    Serving   bool   `json:"serving,omitempty"`
    LastError string `json:"last_error,omitempty"`
}

type RuntimeDescription struct {
    Module   ModuleDescription    `json:"module"`
    Runtime  RuntimeStatus        `json:"runtime"`
    Schema   *schema.SchemaExport `json:"schema,omitempty"`
    Reducers []ReducerDescription `json:"reducers,omitempty"`
}

func (r *Runtime) Describe() RuntimeDescription {
    if r == nil {
        return RuntimeDescription{}
    }
    exp := r.ExportSchema()
    return RuntimeDescription{
        Module: ModuleDescription{
            Name:     r.moduleName,
            Version:  r.moduleVersion,
            Metadata: copyStringMap(r.moduleMetadata),
        },
        Runtime:  r.runtimeStatusSnapshot(),
        Schema:   exp,
        Reducers: reducerDescriptionsFromSchemaExport(exp),
    }
}
```

Important implementation notes:
- V1-A originally planned only `moduleName`; V1-G likely needs V1-A/V1-B/V1-C implementation to retain `moduleVersion` and `moduleMetadata` on `Runtime` during `Build` as private fields. If those fields are absent, add them in V1-G because module identity export is in scope.
- Reuse existing `Module.Describe()` or a shared helper during `Build` if the built runtime stores a module description snapshot.
- If V1-D lifecycle has mutex-protected state, acquire the same mutex or use its public snapshot helper. Do not read racing fields unsafely.
- If status `LastError` stores an error, export only `err.Error()` for diagnostics; do not expose private error wrappers as structured control-plane state.

Run:

```bash
rtk go test . -run 'TestRuntimeDescribe' -count=1
```

Expected:
- runtime description tests pass after status helper details are aligned with prior slices.

## Task 10: Add JSON marshal smoke tests without canonical-contract promises

Objective: prove description structs are JSON-friendly while explicitly avoiding canonical contract output semantics.

Files:
- Modify: `runtime_export_test.go`

Test shape:

```go
func TestRuntimeDescriptionIsJSONMarshalable(t *testing.T) {
    rt := buildTestRuntime(t)

    data, err := json.Marshal(rt.Describe())
    if err != nil {
        t.Fatalf("Marshal RuntimeDescription: %v", err)
    }
    if !bytes.Contains(data, []byte(`"module"`)) || !bytes.Contains(data, []byte(`"schema"`)) {
        t.Fatalf("description JSON missing expected sections: %s", data)
    }
}
```

Do not add tests for deterministic key order, exact canonical bytes, default output path, or `shunter.contract.json`; those belong to V1.5-B.

Run:

```bash
rtk go test . -run TestRuntimeDescriptionIsJSONMarshalable -count=1
```

Expected:
- pass.

## Task 11: Add API comments documenting V1/V1.5 boundary

Objective: make export/introspection intent clear to callers and future implementers.

Files:
- Modify: `runtime_export.go`
- Possibly modify: `module.go` comments if `ModuleDescription` lives there

Add comments explaining:
- `ModuleDescription` is the authored module identity snapshot.
- `RuntimeDescription` is diagnostic/runtime description, not a canonical contract artifact.
- `ExportSchema` returns schema-only export using the schema package format.
- Reducer descriptions only include metadata available in V1: name and lifecycle classification.
- V1.5 will own canonical contract JSON, query/view declarations, codegen, permissions metadata, and migration metadata.

Run:

```bash
rtk go test . -count=1
```

Expected:
- root package tests pass.

## Task 12: Format and validate

Objective: finish the implementation slice with focused validation and relevant broader checks.

Run:

```bash
rtk go fmt .
rtk go test . -count=1
rtk go test ./schema -count=1
rtk go vet . ./schema
rtk go test ./... -count=1
```

Expected:
- root package tests pass
- schema package tests pass
- vet passes for touched packages
- broad tests pass unless an unrelated dirty working-tree failure exists; if so, report exact unrelated failures and preserve the narrower passing gates

## Risks and guardrails

- Do not turn V1-G into V1.5 canonical contract export. No `shunter.contract.json`, canonical JSON writer, codegen, query/view declaration export, permissions metadata, or migration metadata.
- Do not fake reducer signatures. The current schema registry only knows reducer names and lifecycle hooks. Expose that honestly.
- Do not expose lower-level runtime handles such as `schema.SchemaRegistry`, committed state, executor, protocol server, connection manager, or worker channels as public fields.
- Do not require `Start(ctx)` for static export/introspection; build-time module/schema data should be inspectable before serving.
- Do not add new lifecycle or network behavior in this slice. Describe existing state only.
- Keep all returned maps/slices/export structs detached so diagnostics/tools cannot mutate runtime state.
- Do not use the former bundled demo command as an implementation source of truth.
- If `schema.Engine.ExportSchema()` lacks a V1-G-needed schema detail, prefer documenting the limitation over widening lower-level schema export into v1.5 territory.

## Historical Sequencing Note

Later hosted-runtime slices have landed. Do not use this completed V1-G
implementation plan as a live handoff; use
the relevant feature plan for current hosted-runtime status.
