# V1-G Task 03: Implement narrow description/export APIs

Parent plan: `docs/features/V1/V1-G/2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`

Objective: expose detached V1-G introspection without v1.5 contract/codegen expansion.

Files:
- Create `runtime_describe.go`
- Modify `runtime.go` if runtime must preserve module version/metadata from build inputs

Implementation requirements:
- Add `ModuleDescription` with name, version, and metadata only.
- Add `RuntimeDescription` with module description and existing runtime health/readiness diagnostics only.
- Add `Module.Describe()` with defensive metadata copy.
- Add `Runtime.ExportSchema()` delegating to `schema.Engine.ExportSchema()`.
- Add `Runtime.Describe()` without lifecycle side effects.
- Do not add canonical JSON, codegen, query/view declarations, permissions, migration metadata, network/local behavior changes, or subsystem handles.
