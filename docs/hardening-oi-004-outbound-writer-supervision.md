# OI-004 — outbound-writer supervision (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-21.

Follows the same narrow-and-pin shape as the earlier OI-004 slices:
- `docs/hardening-oi-004-supervise-disconnect-context.md`
- `docs/hardening-oi-004-dispatch-handler-context.md`
- `docs/hardening-oi-004-closeall-disconnect-context.md`

## Sharp edge

`protocol/upgrade.go` admitted a connection, then spawned four detached
per-connection goroutines:
- `runDispatchLoop`
- `runKeepalive`
- `runOutboundWriter`
- `superviseLifecycle`

But `superviseLifecycle` only watched `dispatchDone` and
`keepaliveDone`:

```go
go c.superviseLifecycle(..., dispatchDone, keepaliveDone)
```

That meant an outbound-only failure path was invisible to the
supervisor. If `runOutboundWriter` exited first — most concretely on a
write-side WebSocket failure from `protocol/outbound.go:29` or `:37` —
no disconnect was triggered until some other goroutine happened to exit.
During that gap:
- the `ConnManager` still held the `*Conn`
- subscriptions were not reaped
- `c.closed` stayed open
- `runKeepalive` / `runDispatchLoop` could remain alive even though the
  delivery path was already dead

This was a real detached-goroutine ownership hole in the remaining
OI-004 surface listed in `TECH-DEBT.md` under `protocol/outbound.go`.

## Fix

Narrow and pin:

1. `protocol/upgrade.go`
   - wrap `runOutboundWriter` with an `outboundDone` channel, mirroring
     the existing dispatch / keepalive supervision wiring
2. `protocol/disconnect.go::superviseLifecycle`
   - extend the first-exit select to watch `outboundDone` too
   - wait for `outboundDone` during the post-disconnect drain alongside
     `dispatchDone` and `keepaliveDone`

Net effect: any of the three owned goroutines exiting first now drives
one bounded `Disconnect`, and the supervisor does not return until all
three goroutines have observed teardown and unwound.

## Scope / limits

- Closes only the outbound-writer supervision gap.
- Does not change `runOutboundWriter` write semantics or add a new
  write-side timeout.
- Does not change `ClientSender.Send`; the no-ctx follow-on remains
  open separately.
- Does not change the bounded-disconnect-context behavior from the
  earlier supervisor slice; it reuses that existing contract.
- Other detached-goroutine audits in `protocol/conn.go`,
  `protocol/lifecycle.go`, and `protocol/keepalive.go` remain open if a
  specific leak site surfaces.

Diff surface:
- `protocol/upgrade.go`
- `protocol/disconnect.go`
- `protocol/disconnect_test.go`
- `protocol/close_test.go`
- `protocol/supervise_disconnect_timeout_test.go`

## Pinned by

Focused tests:
- `protocol/disconnect_test.go::TestSuperviseLifecycleInvokesDisconnectOnOutboundWriterExit`
  - primary pin for the new seam: closes a synthetic `outboundDone`
    while `dispatchDone` / `keepaliveDone` remain open, asserts the
    supervisor drives `Disconnect`, then closes the remaining done
    channels and asserts the supervisor returns
- `protocol/disconnect_test.go::TestSuperviseLifecycleInvokesDisconnectOnReadPumpExit`
  - expanded happy-path coverage so the default supervised goroutine set
    now includes the outbound writer too
- `protocol/supervise_disconnect_timeout_test.go::{TestSuperviseLifecycleBoundsDisconnectOnInboxHang,TestSuperviseLifecycleDeliversOnInboxOK}`
  - updated to include `outboundDone` in the supervisor drain contract
- `protocol/close_test.go::TestClientInitiatedClose_DisconnectSequenceRuns`
  - updated to reflect the full supervised goroutine set

## Remaining OI-004 sub-hazards

Still open:
- other detached goroutines in the protocol lifecycle surface
  (`protocol/conn.go`, `protocol/lifecycle.go`, `protocol/keepalive.go`)
  if a specific leak site surfaces
- `ClientSender.Send` remains synchronous without its own ctx

## Authoritative artifacts

- This document.
- `protocol/upgrade.go` — outbound writer now reports `outboundDone`.
- `protocol/disconnect.go::superviseLifecycle` — now supervises all
  three owned goroutines.
- `TECH-DEBT.md` — OI-004 updated with the closed outbound-writer
  sub-hazard.
- `docs/current-status.md` — hardening bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect the new baseline.
