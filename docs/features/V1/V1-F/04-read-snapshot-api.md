# V1-F Task 04: Implement the minimal local read API

Parent plan: `docs/features/V1/V1-F/2026-04-23_212927-hosted-runtime-v1f-local-runtime-calls-implplan.md`

Objective: provide safe local reads without leaking mutable state or long-lived snapshots.

Files:
- Modify or create `runtime_local.go`
- Optionally create `local_read.go`
- Update `runtime_local_test.go`

Implementation requirements:
- add `Read(ctx, fn func(LocalReadView) error) error` or the equivalent preferred minimal API
- reject nil callback before acquiring a snapshot
- acquire `r.state.Snapshot()`, invoke the callback, and always close the snapshot
- expose a narrow `LocalReadView` surface for table scan/get/count behavior
- only add SQL convenience if it reuses a shared one-off query evaluator; otherwise defer SQL and ship the read callback surface only
