# V1-E Task 04: Implement `ListenAndServe` and connection-aware close ordering

Parent plan: `docs/features/V1/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md`

Objective: provide the easy serving path and preserve disconnect cleanup ordering.

Files:
- Modify `runtime_network.go`
- Modify `runtime_lifecycle.go`
- Update `runtime_network_test.go`

Implementation requirements:
- add `Runtime.ListenAndServe(ctx context.Context) error`
- auto-call `Start(ctx)` when appropriate
- gracefully shut down HTTP serving on context cancellation
- prepend protocol connection shutdown before executor shutdown in `Close()`
- call `ConnManager.CloseAll(ctx, protocolInbox)` before executor shutdown so disconnect cleanup can route through the inbox
- do not add REST, MCP, local reducer/query, export, or example-replacement work here
