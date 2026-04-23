# Protocol ↔ executor seam: `ExecutorInbox` interface

**Date:** 2026-04-14
**Phase:** 7 (SPEC-005 Epic 3 — WebSocket transport & connection lifecycle, Story 3.4 onwards)

## Decision

The `protocol/` package defines a narrow `ExecutorInbox` interface that captures only the lifecycle events the transport layer needs to hand off. Story 3.4 ships a single method:

```go
type ExecutorInbox interface {
    OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
}
```

Story 3.6 extends the interface with OnDisconnect; later epics add submit-and-return subscription / call-reducer methods. Those later methods use protocol-owned request structs so wire metadata (`RequestID`, `QueryID`) and response channels can cross the seam without importing `executor` internals.

The **adapter** that converts these calls into `executor.OnConnectCmd` + `executor.OnDisconnectCmd` + `ReducerResponse` is owned by the current runtime/bootstrap layer for now. Tests use fakes. If a second consumer emerges, the adapter can be promoted into a `protocol/executorbridge` subpackage without breaking the interface.

## Why

- **Clean-room boundary respected.** Spec §13 explicitly says the protocol layer sends commands via the executor's inbox; it does not import executor internals. A narrow interface prevents vocabulary sprawl and keeps the two packages independently testable.
- **Per-story granularity.** The interface grows one method at a time as stories land. This avoids defining methods for unimplemented flows (OnDisconnect, subscription commands) and keeps the RED/GREEN cycle small.
- **Adapter in runtime bootstrap code is the path of least resistance.** Shunter's current hosted bring-up still wires the executor graph explicitly. Bridging `OnConnect` to `executor.OnConnectCmd` is ~15 lines and belongs next to the runtime/server bring-up code. No `executoradapter` subpackage needed until there's a second caller.
- **Test fakes are trivial.** A struct with a few fields and an error-returning method covers every protocol-layer assertion without spinning up a real executor.

## Admission ordering (Story 3.4)

`Conn.RunLifecycle` enforces the following order after a successful upgrade:

1. `ExecutorInbox.OnConnect(ctx, connID, identity)` — blocks for the admit / reject decision.
2. On reject (non-nil error): close WebSocket with `StatusPolicyViolation` (1008, SPEC-005 §11.1). Do NOT register in ConnManager. Do NOT send InitialConnection.
3. On admit: `ConnManager.Add(conn)` BEFORE the first write, so concurrent fan-out delivery can resolve the `ConnectionID`.
4. Encode + write `InitialConnection` (server message tag 1) as the first binary frame. On write failure: de-register + close with `StatusInternalError` (1011).

`InitialConnection` is never gzipped even when compression is negotiated at upgrade — the body is sent with an explicit `CompressionNone` envelope byte so client decoders branch consistently per SPEC-005 §3.3.

## Server wiring (Story 3.4)

`protocol.Server` gained two fields:

```go
type Server struct {
    // ... existing auth + options ...
    Executor ExecutorInbox
    Conns    *ConnManager
    Upgraded func(ctx context.Context, uc *UpgradeContext) // still optional
}
```

`HandleSubscribe` resolution order after a successful upgrade:

1. `Upgraded != nil` — call it (test + advanced-host escape hatch).
2. `Executor != nil && Conns != nil` — build a `Conn` and drive `RunLifecycle`.
3. Otherwise — close with `StatusNormalClosure`, preserving pre-3.4 bring-up behavior.

A `NewServer(...)` constructor was considered and rejected: struct-literal init is already the pattern across the codebase, and adding a constructor introduces a migration cliff for existing Server consumers (tests + `cmd/` samples that arrive later).

## Tradeoff accepted

The runtime/bootstrap layer owns the adapter. A misbehaving adapter (never responds, panics, etc.) becomes a protocol-layer hang visible only at runtime. Tests must exercise the adapter separately from the protocol layer. The alternative — pulling the full `executor.OnConnectCmd` shape into `protocol/` — was rejected because it forces `protocol/` to import `executor` (spec-forbidden) or duplicate the command types (guaranteed to drift).
