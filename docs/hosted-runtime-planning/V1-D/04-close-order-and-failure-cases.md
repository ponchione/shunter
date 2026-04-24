# V1-D Task 04: Implement `Close` ordering and lifecycle failure handling

Parent plan: `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Objective: make shutdown safe, idempotent, and aligned with kernel contracts.

Files:
- Modify `runtime_lifecycle.go`
- Update `runtime_lifecycle_test.go`

Implementation requirements:
- add `Runtime.Close() error`
- stop scheduler before `Executor.Shutdown()` closes the inbox
- shut down fan-out worker and other owned goroutines in reverse-safe order
- close durability on failed startup and normal shutdown
- surface fatal subsystem state through narrow readiness/health inspection
- do not add `ListenAndServe`, `HTTPHandler`, sockets, or protocol-backed fan-out in V1-D

Run:
- `rtk go test .`
