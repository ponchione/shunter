# Hosted runtime version phases

Status: planning baseline
Scope: version-phase plan for hosted-runtime work. This is not an implementation plan and should not be used as permission to write code directly.

This document sits between the architecture contracts and detailed implementation plans:
- `docs/decomposition/hosted-runtime-v1-contract.md` defines the target v1 contract.
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md` defines near-follow-on platform usability work.
- `docs/decomposition/hosted-runtime-v2-directions.md` parks larger structural/runtime evolution.
- `docs/hosted-runtime-implementation-roadmap.md` remains the epic/order tracker.

Decision baseline:
- Hosted-first remains locked.
- v1 is the coherent hosted runtime/server shape.
- v1.5 is developer/platform usability on top of v1.
- v2+ is structural/runtime evolution after real v1/v1.5 usage creates pressure.
- No v1.5 or v2 implementation should start until the v1 module/runtime owner is real.

---

## Phase 0: Planning baseline reset

Goal: keep planning separate from accidental implementation and make the repo state explicit before more implementation planning.

Decisions:
- Treat the accidental root-package code as a reverted spike, not accepted implementation.
- Treat the deleted skeleton implplan as a superseded prior planning artifact, not the active version-phase plan.
- Do not carry forward `schema.EngineOptions.AllowEmptySchema` as an accepted design decision.
- Revisit empty-module behavior deliberately during V1-A/V1-B planning.

Decisions for the next V1-A implementation plan:
- `Build(NewModule("name"), Config{})` should not succeed with no user schema in V1-A.
- Empty modules are rejected for v1 app modules; do not add smoke/dev/bootstrap empty-schema behavior unless a later slice deliberately designs it.
- Schema/contract version should not default implicitly in `NewModule`; explicit `Module.SchemaVersion(...)` belongs to V1-B.
- The root package is the normal app-facing v1 surface; do not add an `engine` package for V1-A.

Completion criteria:
- There is a clear phase plan before any next code slice.
- Any implementation plan names exactly which phase/slice it belongs to.
- Temporary spike decisions are not silently promoted into v1 contract decisions.

---

## V1-A: Top-level API owner skeleton

Goal: create the public app-facing owner surface without moving the full subsystem graph in one patch.

Target surface:
- `shunter.Module`
- `shunter.Config`
- `shunter.Runtime`
- `shunter.Build(module, config)`

Dependencies:
- Existing `schema.Builder` and `schema.Engine` build seam.
- Current module path `github.com/ponchione/shunter`.

Acceptance criteria:
- A consumer can import `github.com/ponchione/shunter`.
- App code can construct a module, set version/metadata, create config, and call `Build`.
- `Build` validates nil module, blank module name, negative queue capacities, and invalid auth mode.
- `Runtime` stores module identity, config, and the private engine/build result needed by later phases.
- No runtime services start.
- No sockets open.
- No goroutines are started.
- No v1.5/v2 surfaces are introduced.

Explicit non-goals:
- Network serving.
- `Start`, `Close`, `ListenAndServe`, `HTTPHandler`.
- Local reducer/query APIs.
- Contract snapshots/codegen.
- Permissions or migration metadata.
- Multi-module hosting or control-plane work.

Decisions for V1-A:
- V1-A should not build empty modules successfully; it may expose `Build`, but accepted public inputs should still fail clearly at the existing schema validation seam until V1-B registration wrappers exist.
- `NewModule` must not default schema version to 1; explicit `Module.SchemaVersion(...)` belongs to V1-B.

Recommended implementation posture:
- Keep V1-A validation-only and avoid changing lower-level schema semantics.
- Prefer returning the existing schema validation error over adding fake tables or broad empty-schema escape hatches.

---

## V1-B: Module definition surface

Goal: make module authoring explicit, imperative, and module-first.

Target surface:
- `Module.SchemaVersion(...)`
- `Module.TableDef(...)`
- `Module.Reducer(...)`
- `Module.OnConnect(...)`
- `Module.OnDisconnect(...)`
- optional reflection helper wrappers after the explicit path is stable

Dependencies:
- V1-A root module type exists.
- Existing `schema.Builder` table/reducer/lifecycle registration methods.

Acceptance criteria:
- Module packages can expose explicit `Register(mod *shunter.Module)` hooks.
- Domain packages can contribute schema/reducers through those hooks.
- Build-time errors are clear for malformed table/reducer/lifecycle definitions.
- Reflection/tag helpers remain convenience layers, not the core identity.
- Empty-module behavior is resolved here if not already resolved in V1-A.

Explicit non-goals:
- Handler-object reducer style.
- Dynamic plugins.
- Cross-language modules.
- Query/view declarations; those are v1.5.

Planning decisions still needed:
- How close the root `Module` wrapper should stay to `schema.Builder` names and types.
- Whether `SchemaVersion` is required before build or can default.
- Whether `Build` freezes the module permanently after successful build.

---

## V1-C: Runtime build pipeline

Goal: make `Build(module, config)` own subsystem assembly instead of leaving app code to wire the kernel graph manually.

Target behavior:
- Normalize runtime config.
- Build/freeze module schema.
- Initialize persistence/recovery.
- Wire store, commit log, executor, subscriptions, protocol, lifecycle hooks, and scheduler as needed.
- Return one `Runtime` owner object.

Dependencies:
- V1-A root owner types.
- V1-B module registration wrappers.
- Working manual assembly pattern in `cmd/shunter-example/main.go`.

Acceptance criteria:
- App code calls `shunter.Build(...)` instead of constructing kernel subsystems directly.
- Startup/build failures are reported before a partial runtime is exposed.
- Internal subsystem handles do not leak into normal app code.
- Lower-level packages remain available as advanced/internal surfaces.

Explicit non-goals:
- Starting network listeners directly in `Build`.
- Sidecar/control-plane process.
- Cloud deployment model.
- v1.5 contract export/codegen.

Planning decisions still needed:
- Whether `Build` should perform persistence/recovery immediately or defer some recovery work to `Start(ctx)`.
- How to represent build-time resources that must later be closed if `Start` is never called.
- Exact config defaults for data dir, queues, protocol enablement, auth, and listen address.

---

## V1-D: Runtime lifecycle ownership

Goal: make runtime start/stop behavior explicit and owned by `shunter.Runtime`.

Target surface:
- `Start(ctx)`
- `Close()`
- readiness/health inspection

Dependencies:
- V1-C runtime build pipeline.

Acceptance criteria:
- Start is tested for success, failure, context cancellation, and repeated calls.
- Close is tested for clean shutdown, repeated calls, and partial-start cleanup.
- Goroutine ownership and shutdown order are internal to `Runtime`.
- Fatal subsystem states are observable enough for diagnostics.

Explicit non-goals:
- Network convenience methods; those are V1-E.
- Broad admin API.
- Runtime process manager/control plane.

Planning decisions still needed:
- Idempotency rules for double start/close.
- Whether `Start(ctx)` blocks or returns after background workers are ready.
- Readiness semantics: what must be initialized before the runtime is considered ready?

---

## V1-E: Runtime network surface

Goal: expose the WebSocket-first runtime through easy direct serving and composable handler access.

Target surface:
- `ListenAndServe(...)` or equivalent clean default.
- `HTTPHandler()` for host-app composition.
- Protocol/network options through top-level config.

Dependencies:
- V1-D lifecycle ownership.
- Existing `protocol` server/handler behavior.

Acceptance criteria:
- Simple apps can run the runtime with one serving call.
- Larger host apps can mount the runtime handler.
- External client model remains WebSocket-first.
- REST/MCP are not introduced as core runtime identity.

Explicit non-goals:
- REST-first API.
- MCP-first API.
- Broad admin/control surface.

Planning decisions still needed:
- Exact method names and signatures.
- Whether `ListenAndServe` starts lifecycle automatically or requires prior `Start(ctx)`.
- How auth mode and listen address map into protocol/server options.

---

## V1-F: Local runtime calls

Goal: expose local reducer/query calls as legitimate secondary APIs for tests, tools, admin flows, and in-process integrations.

Target capabilities:
- Local reducer invocation.
- Local read/query helpers.
- Clear local identity/auth context.

Dependencies:
- V1-C/V1-D runtime ownership.
- Existing executor/protocol call paths.

Acceptance criteria:
- Tests can invoke reducers without opening a WebSocket client.
- Tools can perform simple reads without knowing store internals.
- Local call behavior aligns with external call semantics where practical.
- Docs label local calls as secondary APIs, not replacement external client model.

Explicit non-goals:
- Replacing WebSocket as the external client contract.
- Broad SQL/view system.
- Admin/control-plane expansion.

Planning decisions still needed:
- Exact identity/caller model for local calls.
- Whether local query helpers expose SQL-like strings, typed handles, or minimal read views in v1.

---

## V1-G: Export and introspection foundation

Goal: provide enough runtime/module introspection for diagnostics and for v1.5 contract export to start cleanly later.

Target capabilities:
- Export module identity/version/metadata.
- Export schema information.
- Export reducer metadata.
- Describe enough runtime/module state for diagnostics.

Dependencies:
- V1-B module declarations.
- Existing schema export support.

Acceptance criteria:
- Runtime/module can describe schema and reducer metadata through a stable API.
- v1.5 canonical contract export has a clear foundation.
- The export is not yet the full v1.5 `shunter.contract.json` artifact.

Explicit non-goals:
- Canonical contract snapshots.
- Codegen.
- Permissions/read-model metadata.
- Migration metadata.

Planning decisions still needed:
- Whether `Runtime.ExportSchema()` is enough for v1 or whether a separate `ModuleDescription` is needed.
- Which reducer metadata belongs in v1 vs v1.5.

---

## V1-H: Hello-world replacement and v1 proof

Goal: replace the manual subsystem bootstrap story with a true hosted-runtime example.

Target example shape:
- Define a table.
- Define a reducer.
- Build/start runtime.
- Connect a client.
- Observe live state.
- Shut down cleanly.

Dependencies:
- V1-A through V1-G enough to avoid manual subsystem assembly in app code.

Acceptance criteria:
- The normal example no longer reads like subsystem assembly.
- A new app author can understand Shunter without learning every kernel package first.
- The example exercises module definition, runtime build, serving path, reducer call, subscription/live update, and shutdown.
- Existing low-level manual example is either removed from the normal path or clearly retained as internal/reference material.

Explicit non-goals:
- Full tutorial site.
- Generated frontend app.
- v1.5 codegen/contract workflow.

---

## V1 complete means

V1 is complete only when:
- App authors can define a module through the top-level API.
- `Build` owns runtime assembly.
- `Runtime` owns lifecycle and serving.
- WebSocket remains the primary external client model.
- Local calls exist as secondary test/tool/admin APIs.
- Basic export/introspection exists for schema/reducer/module metadata.
- The hello-world path no longer requires manual kernel graph wiring.

V1 is not complete merely because `Module`, `Config`, `Runtime`, and `Build` names exist.

---

## V1.5-A: Query/view declarations

Goal: add code-first declared read surfaces to the module model.

Target capabilities:
- Named read queries.
- Named live views/subscriptions.
- Declarations registered on the module.
- Exportable metadata for contracts/codegen.

Dependencies:
- V1 module definition surface and export/introspection foundation.

Acceptance criteria:
- Query/view declarations attach to the module alongside schema and reducers.
- They are inspectable/exportable.
- They do not become a full SQL/view system.

Non-goals:
- Full SQL/view model.
- String/DSL-first query authoring.
- Runtime-shape changes.

---

## V1.5-B: Canonical contract export

Goal: export a full module contract as deterministic canonical JSON.

Target artifact:
- module identity and module version
- schema/contract version
- tables
- reducers
- queries
- views
- reserved or populated fields for permission/read-model declarations
- reserved or populated fields for migration metadata
- codegen/export metadata

Dependencies:
- V1.5-A declared read surface.
- V1-G export/introspection foundation.

Acceptance criteria:
- Output is deterministic enough for review diffs.
- Default repo snapshot path is `shunter.contract.json`.
- Output path is configurable.
- Canonical JSON is source of truth; human docs are generated/secondary.

Non-goals:
- Executable migrations.
- Generated server/module implementation.

---

## V1.5-C: Client bindings and codegen

Goal: generate useful client bindings from the canonical contract.

Primary target:
- Frontend/client bindings.

Secondary targets:
- typed internal clients for tests/tools/admin scripts
- downstream generator artifacts

Dependencies:
- V1.5-B canonical contract export.

Acceptance criteria:
- Client bindings are generated from exported contract data, not hidden runtime state.
- Generated bindings cover schema, reducers, declared queries, and declared views.

Non-goals:
- Every language target.
- Full framework scaffolding.
- Server/module implementation generation.

---

## V1.5-D: Permissions and read-model metadata

Goal: attach narrow product-facing policy metadata to declared read/write surfaces.

Attach to:
- reducers
- named queries
- named views/subscriptions

Dependencies:
- V1.5-A declared surfaces.
- V1.5-B export artifact.

Acceptance criteria:
- Metadata appears in contract export.
- Generated clients/docs can inspect the metadata.
- Runtime does not grow a broad standalone policy framework in v1.5.

Non-goals:
- Broad multi-tenant auth product.
- Full policy language.

---

## V1.5-E: Migration metadata and contract diff tooling

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

Dependencies:
- V1.5-B canonical contract export.
- V1.5-D metadata attachment patterns if policy/read-model metadata is already present.

Acceptance criteria:
- Metadata is exported through the canonical contract.
- Contract diff tooling can compare current export against a previous `shunter.contract.json`.
- Tooling can warn when author-declared and inferred changes disagree.
- Runtime startup remains non-blocking for migration metadata.

Non-goals:
- Runtime-blocking migration enforcement.
- Executable migration runners.
- Implicit migrations during startup.

---

## V1.5 complete means

V1.5 is complete only when:
- Query/view declarations exist and are exportable.
- Canonical contract JSON is deterministic and useful in review/CI.
- Client bindings are generated from the contract.
- Narrow permission/read-model metadata appears on reducers/queries/views.
- Descriptive migration metadata and diff tooling exist.
- Runtime shape from v1 remains intact.

---

## V2+ parking lot

Do not implement these as part of v1/v1.5 unless a later audit explicitly moves them earlier:
- multi-module hosting
- stronger runtime↔module boundary
- out-of-process module execution
- dynamic plugin loading
- broad admin/control plane
- cloud/multi-tenant runtime management
- executable migration systems
- full SQL/view system
- broad standalone policy framework
- cross-language module authoring
- split contract artifacts beyond the v1.5 full module contract

V2 planning posture:
- Keep one-module hosting as the valid simple mode even if multi-module support later appears.
- Move toward a stronger host/module seam only where real v1/v1.5 usage proves it valuable.
- Treat out-of-process execution as optional and later than the boundary design.
- Build admin/CLI around real runtime/export APIs, not a separate imagined control plane.
- Build executable migrations on top of v1.5 contract snapshots/diffs, not as implicit startup behavior.

---

## Next planning step

The next planning artifact should be a revised first implementation plan for V1-A, not code.

That plan must decide:
- empty-module behavior
- schema-version defaulting vs explicit version registration
- whether lower-level schema semantics may be touched
- exact tests proving the root package surface without drifting into V1-B

After V1-A is accepted, the next implementation plan should be V1-B module registration wrappers.
