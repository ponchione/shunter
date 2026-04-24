# V1-G Task 04: Pin detachment and runtime-status snapshot behavior

Parent plan: `docs/hosted-runtime-planning/V1-G/2026-04-23_213651-hosted-runtime-v1g-export-introspection-foundation-implplan.md`

Objective: make sure descriptions stay honest, detached, and free of new lifecycle behavior.

Files:
- Update `runtime_export_test.go`
- Modify `runtime_export.go` or `runtime_lifecycle.go` only as needed for a shared private status snapshot helper

Implementation requirements:
- return detached metadata maps and reducer slices
- describe current lifecycle state rather than introducing new behavior
- allow built runtimes to be introspectable without requiring `Start(ctx)`
- keep JSON tags if useful, but do not add canonical JSON export or `shunter.contract.json` behavior
