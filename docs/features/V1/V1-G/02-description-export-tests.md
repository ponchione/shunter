# V1-G Task 02: Add failing tests for description/export APIs

Parent plan: `docs/features/V1/V1-G/2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`

Objective: pin the V1-G root API before implementation.

Files:
- Create `runtime_describe_test.go`

Tests to add:
- `Module.Describe()` is valid before build and returns detached name/version/metadata.
- `Runtime.ExportSchema()` is valid after `Build` and before `Start`, returns schema version/tables/reducers/lifecycle reducer metadata, and returns detached snapshots.
- `Runtime.Describe()` reports module identity and current `Runtime.Health()` state without requiring `Start`.
- `Runtime.Describe()` reflects ready/closed state after lifecycle changes without exposing lower-level handles.
