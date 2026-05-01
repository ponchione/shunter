# Hosted Runtime V1-G Export and Introspection Implementation Plan

Status: validated against live repo on 2026-04-24.
Scope: V1-G only; implementation-facing plan.

Goal: expose narrow module/runtime/schema description APIs in the root package so diagnostics and later v1.5 contract export have a stable foundation, without adding canonical contract files, codegen, query/view declarations, permissions, migration metadata, admin/control-plane APIs, or example replacement.

Grounded repo facts:
- V1-F is complete in the current working tree: local reducer calls and callback-owned local reads exist in `runtime_local.go`.
- `shunter.Module` already stores name, version string, and defensively copied metadata.
- `shunter.Runtime` stores module name, config, schema engine, schema registry, committed state, lifecycle health, and V1-F local APIs.
- `schema.Engine.ExportSchema() *schema.SchemaExport` already returns a detached JSON-friendly schema snapshot.
- `schema.SchemaExport` includes schema version, table exports, and reducer exports.
- `schema.ReducerExport` currently exposes only reducer name plus lifecycle flag; V1-G must stay honest and not invent argument schema, return schema, permissions, query/view, migration, or codegen metadata.
- `Runtime.Health()` already returns a detached lifecycle/readiness snapshot.

Scope:
- Add `Module.Describe()` returning detached module identity/version/metadata.
- Add `Runtime.ExportSchema()` reusing `schema.Engine.ExportSchema()` and valid after successful `Build`, without requiring `Start`.
- Add `Runtime.Describe()` returning detached module and runtime lifecycle/readiness diagnostics.
- Preserve defensive copies for maps and detached schema exports.

Non-goals:
- No `shunter.contract.json` or canonical JSON writing.
- No codegen.
- No query/view declarations.
- No permission/read-model metadata.
- No migration metadata or contract diff tooling.
- No network/local-call behavior changes.
- No hello-world/example replacement.
- No lower-level subsystem handle exposure.

Locked decisions:
1. `Module.Describe()` is valid before `Build` and reports only authored module identity/version/metadata.
2. `Runtime.ExportSchema()` delegates to `schema.Engine.ExportSchema()` instead of duplicating registry traversal.
3. `Runtime.ExportSchema()` is build-time introspection and does not require `Start(ctx)`.
4. `Runtime.Describe()` reports existing runtime diagnostics; it must not start/stop resources or change lifecycle state.
5. Reducer metadata remains narrow: name and lifecycle flag from the existing schema export.

Files likely to change:
- Create `runtime_describe.go`.
- Create `runtime_describe_test.go`.
- Modify `runtime.go` to preserve module version and metadata on built runtimes.
- Update the execution-plan status only if it prevents stale handoff guidance.

Validation commands:
- `rtk go test . -run 'Test(ModuleDescribe|RuntimeExportSchema|RuntimeDescribe)' -count=1`
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc . Runtime.Describe`

Historical sequencing note: later hosted-runtime slices have landed. Do not use
this completed V1-G implementation plan as a live handoff; use
the relevant feature plan for current hosted-runtime status.
