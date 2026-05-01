# Hosted runtime implementation roadmap

Status: historical implementation tracker through completed V2.5; no active
hosted-runtime slice queued
Scope: implementation-facing roadmap for turning the hosted-runtime
architecture docs into ordered epics. This is not a detailed first-patch
implementation plan.

This roadmap follows:
- `docs/specs/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md`
- `docs/specs/hosted-runtime-version-phases.md`
- `docs/specs/hosted-runtime-v1-contract.md`
- `docs/specs/hosted-runtime-v1.5-follow-ons.md`
- `docs/specs/hosted-runtime-v2-directions.md`
- `docs/specs/EXECUTION-ORDER.md`

Audit result:
- the hosted-runtime doc set has been reconciled for v1/v1.5/v2 scope boundaries
- this roadmap is ready to drive the next post-audit implementation-planning step
- this roadmap should still be treated as an epic/order document, not as the detailed first-patch implementation plan

Current status note:
- V1, V1.5, V2, and the V2.5 read-authorization follow-on have landed in
  live code.
- This file is no longer the active next-slice tracker. Open a new
  feature-specific plan only when a new target is explicitly queued.

Current repo reality:
- Shunter already has substantial kernel packages: `schema`, `store`, `commitlog`, `executor`, `subscription`, `protocol`, and `query`.
- The root `shunter` package uses the top-level API instead of exposing manual subsystem assembly as the normal app path.
- V1-H proved the app-author/operator path, but the temporary bundled example command has since been removed because it did not serve a maintained product or integration purpose.
- This roadmap starts after the relevant kernel pieces are ready enough to wire through a top-level runtime.

---

## 1. Roadmap thesis

The next implementation track should be hosted-runtime-first.

That means the next major product of the repo should not be another manually wired example.
It should be a top-level Shunter API that lets an app define a module, build a runtime, and serve clients without directly assembling the kernel graph.

The completed progression is:
1. v1: top-level hosted runtime API
2. v1 hardening: app-author path replaces manual bootstrap
3. v1.5: query/view declarations, export, codegen, permissions metadata, and
   migration metadata
4. v2: hosted-runtime hardening through completed V2-G
5. v2.5: read-authorization completion for table policy, declared reads, and
   row-level visibility

---

## 2. Dependency boundary

Do not start by implementing v1.5 or v2 surfaces.

The first implementation target is v1 because all later surfaces need a stable module/runtime owner:
- query/view declarations need a module definition to attach to
- codegen needs a module export contract
- permissions metadata needs reducers/queries/views to attach to
- migration metadata needs schema/contract snapshots
- v2 runtime/module boundary decisions need v1 usage feedback

The v1 runtime should be built as a thin but real owner over the existing kernel packages.
It should not duplicate kernel logic.

---

## 3. v1 epics

### Epic V1-1: Top-level package/API surface

Goal: create the public app-facing surface that future app authors import.

Target concepts:
- `shunter.Module`
- `shunter.Config`
- `shunter.Runtime`
- `shunter.Build(module, config)`

Likely work:
- decide the actual package location for the top-level API
- add minimal exported types
- keep config narrow and runtime-focused
- make lower-level packages remain usable but no longer be the normal app path

Acceptance criteria:
- a consumer can import the top-level package and construct a module/config/runtime without touching subsystem constructors directly
- the API shape matches `docs/specs/hosted-runtime-v1-contract.md`
- no v1.5 declarations are required yet

Non-goals:
- codegen
- contract snapshots
- multi-module hosting
- executable migrations

### Epic V1-2: Module definition surface

Goal: make module authoring explicit and module-first.

Target capabilities:
- create a named module
- attach module version and metadata
- register schema/tables explicitly
- register plain-function reducers
- register lifecycle hooks: connect/disconnect

Likely work:
- wrap/adapt existing schema builder behavior behind module methods
- wrap/adapt reducer registration behind module methods
- keep reflection/tag helpers optional, not the core identity
- define module validation/freeze behavior before runtime build

Acceptance criteria:
- module packages can expose explicit `Register(mod)` hooks
- domain packages can contribute schema/reducers through those hooks
- malformed module definitions fail at build time with clear errors

Non-goals:
- handler-object reducer style
- dynamic plugins
- cross-language modules

### Epic V1-3: Runtime config and build pipeline

Goal: make `Build(module, config)` own subsystem assembly.

Target capabilities:
- normalize runtime config
- freeze/build module schema
- initialize persistence/recovery
- wire store, commit log, executor, subscriptions, protocol, and lifecycle hooks
- return one `Runtime` owner object

Likely work:
- move the working assembly pattern into root runtime ownership
- move lifecycle/shutdown ordering into runtime ownership
- avoid leaking internal worker/channel wiring into app code
- keep config limited to runtime concerns: data dir, queues, protocol/listen, auth mode, logging/metrics hooks

Acceptance criteria:
- app code calls `Build(...)` instead of manually constructing kernel subsystems
- startup failures are reported before partial runtime exposure
- shutdown can cleanly stop the owned subsystem graph

Non-goals:
- secondary runtime process
- sidecar/control plane
- cloud deployment model

### Epic V1-4: Runtime lifecycle and network surface

Goal: make the runtime easy to start directly and easy to compose.

Target methods:
- `Start(ctx)`
- `Close()`
- `ListenAndServe(...)`
- `HTTPHandler()`
- readiness/health inspection

Likely work:
- define idempotency rules for start/close
- define behavior for context cancellation
- expose WebSocket protocol through the handler surface
- keep `ListenAndServe(...)` as the easy default path
- keep `HTTPHandler()` as the composition escape hatch

Acceptance criteria:
- simple apps can call one serving method
- larger host apps can mount the runtime handler
- lifecycle behavior is tested for start, close, and double-close/double-start cases

Non-goals:
- REST-first API
- MCP-first API
- broad admin API

### Epic V1-5: Local runtime calls

Goal: expose local reducer/query calls as legitimate secondary APIs.

Target capabilities:
- local reducer invocation for tests/tools/admin flows
- local query/read helper access
- clear identity/auth context for local calls

Likely work:
- adapt existing executor/protocol call paths rather than inventing separate semantics
- define local call error behavior consistently with external calls where practical
- expose only enough read/query surface for v1

Acceptance criteria:
- tests can invoke reducers without opening a WebSocket client
- tools can perform simple reads without knowing store internals
- local calls are documented as secondary APIs, not the primary external client model

Non-goals:
- replacing WebSocket as the external client contract
- broad SQL/view system

### Epic V1-6: Export/introspection hooks

Goal: give the runtime enough introspection to support v1.5 exports later.

Target capabilities:
- export module identity/version/metadata
- export schema information
- export reducer metadata
- expose enough runtime/module description for diagnostics

Likely work:
- reuse existing schema export work where possible
- define a small module description structure
- do not implement the full v1.5 canonical contract yet

Acceptance criteria:
- runtime/module can describe its schema and reducers through a stable API
- v1.5 contract export has a clear place to start

Non-goals:
- canonical `shunter.contract.json`
- codegen
- permissions/migration metadata

### Epic V1-7: Hello-world replacement

Status: V1-H landed; bundled demo command later removed.

Goal: replace the manual bootstrap story with a true hosted-runtime example.

Target example shape:
- define a table
- define a reducer
- build/start runtime
- connect client
- observe live state

Likely work:
- rewrite or add an example that uses the top-level API
- keep any low-level manual bootstrap example only as an internal/reference example if still useful
- keep docs from pointing at throwaway demo commands as the normal path

Acceptance criteria:
- the main example no longer reads like subsystem assembly
- a new app author can understand Shunter from the example without learning every kernel package first

Non-goals:
- full tutorial site
- generated frontend app

---

## 4. v1 verification gates

Before calling v1 hosted runtime complete:

1. Build/test the new top-level package.
2. Run focused tests for touched packages.
3. Run a maintained hosted-runtime proof end to end when one exists.
4. Confirm that proof exercises:
   - module definition
   - runtime build
   - serving path
   - reducer call
   - subscription or live update path
   - clean shutdown
5. Confirm app code does not manually assemble kernel internals.
6. Confirm lower-level packages remain available for advanced/internal usage.

Relevant command pattern:
- use `rtk go test` for touched packages first
- expand to broader `rtk go test ./...` when the hosted-runtime surface is integrated
- use `rtk go fmt` on touched Go files before finishing implementation work

---

## 5. v1.5 epics

Start v1.5 only after the v1 runtime owner and module model are real.

### Epic V1.5-1: Query/view declarations

Goal: add code-first named reads and live views/subscriptions to the module model.

Target capabilities:
- named read queries
- named declarative live views/subscriptions
- declarations registered with the module
- exportable metadata for contracts/codegen

Non-goals:
- full SQL/view system
- string/DSL-first query authoring

### Epic V1.5-2: Canonical contract export and binding-export foundation

Goal: export a full module contract artifact as canonical JSON.

Target artifact:
- module identity and module version
- schema/contract version
- tables
- reducers
- queries
- views
- reserved fields for permissions/read-model declarations, populated by Epic V1.5-4
- reserved fields for migration metadata, populated by Epic V1.5-5
- codegen/export metadata

Default repo snapshot name:
- `shunter.contract.json`

Rules:
- canonical JSON is the source of truth
- generated human-readable docs are secondary
- output path may be configured

### Epic V1.5-3: Client bindings/codegen

Goal: generate useful client bindings from the canonical contract.

Primary target:
- frontend/client bindings

Secondary targets:
- typed internal clients for tests/tools/admin scripts
- downstream generator artifacts

Non-goals:
- generating server/module implementation
- every possible language target
- broad framework scaffolding

### Epic V1.5-4: Permissions/read-model metadata

Goal: attach narrow policy metadata to declared read/write surfaces.

Attach to:
- reducers
- named queries
- named views/subscriptions

Non-goals:
- broad standalone policy framework
- complex multi-tenant auth product

### Epic V1.5-5: Migration metadata and diff tooling

Goal: make schema/module evolution visible and reviewable without executing migrations.

Target capabilities:
- module-level version/compatibility summary
- optional declaration-level change metadata
- author-declared intent
- tool-inferred contract diffs
- warning/CI-oriented mismatch checks

Compatibility levels:
- compatible
- breaking
- unknown

Optional classifications:
- additive
- deprecated
- data-rewrite-needed
- manual-review-needed

Non-goals:
- runtime-blocking enforcement
- executable migration runners
- implicit migrations during startup

---

## 6. v1.5 verification gates

Before calling v1.5 complete:

1. Query/view declarations are exported in the canonical contract.
2. Contract JSON output is deterministic enough for review diffs.
3. `shunter.contract.json` can be committed and compared in CI-style workflows.
4. Client bindings are generated from the contract, not from hidden runtime state.
5. Permissions metadata appears on reducers/queries/views in the export.
6. Migration metadata supports both module-level and declaration-level use.
7. Contract diff tooling can warn on declared-vs-inferred mismatch.
8. Runtime startup remains non-blocking for migration metadata.

---

## 7. v2 parking lot

Do not implement these as part of v1/v1.5 unless a later audit explicitly moves them earlier:

- multi-module hosting
- out-of-process module execution
- dynamic plugin loading
- broad admin/control plane
- cloud/multi-tenant runtime management
- executable migration runners
- full SQL/view system
- broad standalone policy framework
- cross-language module authoring

These belong in `docs/specs/hosted-runtime-v2-directions.md` until real app usage proves the need.

---

## 8. Documentation updates during implementation

As implementation lands, keep these docs aligned:

- `docs/specs/hosted-runtime-v1-contract.md`
  - update when public v1 API names settle
  - update when lifecycle/network/local-call behavior changes

- `docs/specs/hosted-runtime-v1.5-follow-ons.md`
  - update when export/codegen/query/migration details become concrete
  - keep transitional "both" surfaces documented

- `docs/specs/hosted-runtime-v2-directions.md`
  - move items out only when they are intentionally pulled earlier
  - add cleanup notes when v1.5 overlaps become obsolete

- `docs/specs/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md`
  - keep as the high-level model, not the detailed task tracker

- `docs/specs/EXECUTION-ORDER.md`
  - add hosted-runtime phases after the core kernel execution order when implementation work begins

---

## 9. Current hosted-runtime slice marker

Hosted-runtime work through V2.5 has landed in live code. No next hosted-runtime
slice is queued in this roadmap.

Landed proof points:
- root package imports as `github.com/ponchione/shunter`
- `Module`, `Config`, `Runtime`, and `Build(...)` exist
- `Runtime.Start`, `Close`, `HTTPHandler`, `ListenAndServe`, local calls, describe, and schema export exist
- query/view declarations, contract export, codegen, permission metadata, and
  migration metadata exist
- V2.5 read authorization enforces table policy, declared reads, and
  row-level visibility
- root/runtime tests now carry the maintained hosted-runtime and
  read-authorization proofs
- the prior bundled demo command was removed because it was throwaway code, not a durable product surface

If hosted-runtime work continues, start from an explicit user target and add a
fresh feature-specific plan under `docs/features/`.

---

## 10. Practical bottom line

The architecture docs are now clear enough to stop asking broad design questions.

The hosted-runtime implementation path is now:
- keep the current hosted-runtime and read-authorization gauntlets green
- start new hosted-runtime work only from an explicit target and a
  feature-specific plan
- leave remaining structural ambitions parked until real hosted apps create
  pressure
