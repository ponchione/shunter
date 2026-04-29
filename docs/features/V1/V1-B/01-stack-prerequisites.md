# V1-B Task 01: Reconfirm stack prerequisites

Parent plan: `docs/features/V1/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`

Objective: verify V1-B is being implemented on top of the V1-A root package instead of mixing scopes.

Prerequisites:
- V1-A root package exists and `rtk go list .` succeeds
- Read `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`
- Inspect `module.go`, `config.go`, `runtime.go`, and root-package tests

Checks:
- `rtk go list .`
- `rtk go doc ./schema.Builder`
- `rtk go doc ./schema.TableDefinition`
- `rtk go doc ./schema.ReducerHandler`
- `rtk go doc ./schema.ReducerContext`

Stop if:
- the root package still does not exist
- the `schema.Builder` delegation seam has materially changed

Done when:
- V1-A is confirmed as landed or stacked
- the wrapper signatures and dependencies are re-grounded against live code
