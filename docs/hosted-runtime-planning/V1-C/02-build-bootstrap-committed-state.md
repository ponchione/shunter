# V1-C Task 02: Add failing tests for committed-state bootstrap and reopen

Parent plan: `docs/hosted-runtime-planning/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Objective: prove `Build` now owns durable-state bootstrap/recovery instead of returning only a schema shell.

Files:
- Create or modify `runtime_build_test.go`

Tests to add:
- `Build` with a temp data dir bootstraps committed state for declared tables
- first build writes enough durable state that a later build against the same dir recovers cleanly
- blank `Config.DataDir` remains publicly valid and normalizes to a runtime-owned default path
- `Build` does not start lifecycle-owned goroutines or network serving

Assertions guidance:
- verify table presence through runtime-owned built state/registry
- avoid asserting internal example wiring or protocol setup

Run:
- `rtk go test .`
