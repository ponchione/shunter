# V1-D Task 02: Add failing tests for `Start` readiness and partial-start cleanup

Parent plan: `docs/features/V1/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Objective: pin the V1-D lifecycle contract before implementation.

Files:
- Create or modify `runtime_lifecycle_test.go`

Tests to add:
- `Start(ctx)` returns after readiness and does not block for runtime lifetime
- repeated `Start` calls behave predictably
- canceling startup before readiness cleans up partial resources and leaves runtime retryable
- `Close()` before `Start()` is valid and idempotent
- `Start` after `Close` preserves the closed-runtime sentinel
- readiness/health inspection reflects built, starting, ready, and closed states
