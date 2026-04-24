# V1-G Task 03: Implement module/runtime description and schema export

Parent plan: `docs/hosted-runtime-planning/V1-G/2026-04-23_213651-hosted-runtime-v1g-export-introspection-foundation-implplan.md`

Objective: expose the minimum detached snapshot APIs that V1 needs.

Files:
- Modify `module.go`
- Modify `runtime.go`
- Create or modify `runtime_export.go`

Implementation requirements:
- add `ModuleDescription`, `ReducerDescription`, and `RuntimeStatus`
- add `Module.Describe()`
- add `Runtime.Describe()`
- add `Runtime.ExportSchema()` delegating to `schema.Engine.ExportSchema()` when available
- keep reducer metadata intentionally narrow: name, lifecycle, optional kind
- do not expose lower-level handles such as `CommittedState`, executor, conn manager, or protocol server
