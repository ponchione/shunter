# V1-G Task 02: Add failing tests for module and runtime descriptions

Parent plan: `docs/hosted-runtime-planning/V1-G/2026-04-23_213651-hosted-runtime-v1g-export-introspection-foundation-implplan.md`

Objective: pin the public description/export behavior before implementation.

Files:
- Create `runtime_export_test.go`

Tests to add:
- `Module.Describe()` returns name, version, and detached metadata
- `Runtime.ExportSchema()` returns a fresh detached schema export snapshot
- `Runtime.Describe()` includes module description, schema summary, reducer summary, and narrow runtime status
- description/export work before `Start(ctx)` and after `Close()` as planned
- mutating returned maps/slices does not affect later calls
