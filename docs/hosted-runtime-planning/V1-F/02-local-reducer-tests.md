# V1-F Task 02: Add failing tests for local reducer calls

Parent plan: `docs/hosted-runtime-planning/V1-F/2026-04-23_212927-hosted-runtime-v1f-local-runtime-calls-implplan.md`

Objective: pin the intended local reducer-call API and runtime-state behavior.

Files:
- Create `runtime_local_test.go`

Tests to add:
- local reducer calls require an already-started, ready runtime
- built-but-not-started returns the not-ready sentinel
- startup in progress preserves `ErrRuntimeStarting`
- closing/closed preserves `ErrRuntimeClosed`
- successful local reducer calls return executor-aligned status, error, return bytes, and tx id
- reducer user errors stay in the result shape, not as admission failures
- context cancellation while waiting is handled predictably
