# Superseded V1-G Task 03

This older task draft was superseded by `03-description-export-implementation.md`
and `2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`.

The landed V1-G implementation lives in `runtime_describe.go` and keeps the
surface narrow:
- `ModuleDescription`
- `RuntimeDescription`
- `Module.Describe()`
- `Runtime.ExportSchema()`
- `Runtime.Describe()`

Do not add the older draft's separate `ReducerDescription` or `RuntimeStatus`
types for V1-G.
