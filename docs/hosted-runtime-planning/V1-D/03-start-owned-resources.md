# V1-D Task 03: Implement `Runtime.Start` and owned runtime resources

Parent plan: `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Objective: wire the runtime-owned startup path without adding networking.

Files:
- Modify `runtime.go`
- Create or modify `runtime_lifecycle.go`

Implementation requirements:
- add `Runtime.Start(ctx context.Context) error`
- call `schema.Engine.Start(ctx)` before protocol admission
- create/start the durability worker only inside `Start`
- create/start the executor, scheduler, subscription manager, and internal fan-out worker
- use a private no-op fan-out sender so V1-D stays network-free
- store lifecycle state needed by V1-E
- keep `Start` context as startup/cancellation context, not lifetime context
