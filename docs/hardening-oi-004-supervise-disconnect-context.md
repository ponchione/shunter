# OI-004 — `superviseLifecycle` disconnect context (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-21.

Follows the shape of the same-day
`docs/hardening-oi-004-sender-disconnect-context.md` precedent and the
earlier `docs/hardening-oi-004-watch-reducer-response-lifecycle.md` sub-slice.

## Sharp edge

`protocol/disconnect.go::Conn.superviseLifecycle` is the supervisor that
converts the first exit of `runDispatchLoop` or `runKeepalive` into the
full SPEC-005 §5.3 teardown. It is spawned exactly once per admitted
connection by the default Upgraded handler at
`protocol/upgrade.go:211`:

```go
go c.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", s.Executor, s.Conns, dispatchDone, keepaliveDone)
```

The caller hardcodes `context.Background()`. The supervisor previously
forwarded that ctx directly into `c.Disconnect(ctx, ...)`:

```go
c.Disconnect(ctx, code, reason, inbox, mgr)
```

`Conn.Disconnect` threads the ctx into
`inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` at
steps 1 and 2 of the SPEC-005 §5.3 teardown. Both honor ctx
cancellation via the adapter's select arm
(`executor/protocol_inbox_adapter.go:58-63`) and `awaitReducerStatus`
(`executor/protocol_inbox_adapter.go:133-145`). With a
`context.Background()` root the two calls were effectively
non-cancellable — the identical hazard that the overflow-driven
`connManagerSender.enqueueOnConn` path hit before
`docs/hardening-oi-004-sender-disconnect-context.md` landed. Concrete
hang paths:

- executor dispatch deadlock (queue full with a held lock, wedged
  single-writer main loop)
- executor crashes mid-transaction and the `respCh` the inbox adapter
  is waiting on is never fed
- `DisconnectClientSubscriptions` seam inside the executor holds an
  internal lock against a scheduler or fan-out goroutine that is
  itself stuck

In each case the supervisor goroutine blocked inside step 1 or step 2.
`closeOnce.Do` had latched but the body never reached
`close(c.closed)`, so `runDispatchLoop` / `runKeepalive` /
`runOutboundWriter` could not observe the close signal and the `*Conn`
(and transitively its outbound channel, decoded frames, keep-alive
state, websocket handle) was held alive for the process lifetime.

The other two `Conn.Disconnect` call sites already fit the bounded-ctx
contract:
- `ConnManager.CloseAll` (`protocol/conn.go:150`) forwards the engine
  shutdown ctx supplied by the caller; callers are expected to bound
  the shutdown window.
- `connManagerSender.enqueueOnConn` (`protocol/sender.go:119-123`)
  derives `context.WithTimeout(context.Background(), DisconnectTimeout)`
  after the 2026-04-21 sender-disconnect-context slice.

The supervisor was the one remaining site holding a Background-rooted
ctx against the inbox calls.

## Fix

Narrow and pin. One-line change, zero behavior change for the
non-hang path:

1. `protocol/disconnect.go::superviseLifecycle` — after selecting on
   `dispatchDone` / `keepaliveDone`, derive a bounded ctx from
   `context.WithTimeout(ctx, c.opts.DisconnectTimeout)` with
   `defer cancel()`, then forward that ctx into `Conn.Disconnect`.
   Reuses the existing `ProtocolOptions.DisconnectTimeout` field
   (default `5 * time.Second`) introduced by the sender-disconnect
   slice — no new option, no new default.

Happy path unchanged: the inbox calls return promptly, the supervisor
exits, the deferred `cancel` fires, the ctx is collected with the
goroutine. Hang path: after `DisconnectTimeout` the inbox calls return
`ctx.Err()`; `Conn.Disconnect` logs the error at info level
(`log.Printf` in disconnect.go lines 37–42) and proceeds to steps 3–5
of the teardown unconditionally — `mgr.Remove` + `cancelRead` +
`close(c.closed)` + optional `closeWithHandshake`. Disconnect cannot
be vetoed (SPEC-003 §10.4), so the `*Conn` becomes collectible.

## Scope / limits

- Closes the narrow sub-hazard at the `superviseLifecycle` boundary
  only. Other detached-goroutine sites in
  `protocol/conn.go` / `protocol/lifecycle.go` /
  `protocol/outbound.go` / `protocol/keepalive.go` are not touched;
  OI-004 stays open for those surfaces if workload or audit evidence
  surfaces a specific leak site.
- Does not change the `ExecutorInbox` contract. The inbox adapters
  already honor ctx cancellation; this slice just stops handing them
  a non-cancellable ctx from the supervisor site.
- Does not change the `Conn.Disconnect` ordering. Steps 1–2 still
  run synchronously before steps 3–5; the only change is that a
  hung step 1 or 2 now times out instead of blocking forever.
- Does not change `ConnManager.CloseAll` (`protocol/conn.go:150`) —
  that site forwards a caller-owned shutdown ctx and is the caller's
  responsibility to bound.
- Reuses the existing `DisconnectTimeout` default (5 s). The rationale
  recorded in `docs/hardening-oi-004-sender-disconnect-context.md`
  still applies: long enough for a normal executor dispatch + on-
  disconnect reducer; short enough that a process-wide stall doesn't
  accrete unbounded conn state across repeated teardowns.

Diff surface:
- `protocol/disconnect.go::superviseLifecycle` — bounded-ctx derive +
  contract comment naming the pin test.

## Pinned by

Two focused tests in `protocol/supervise_disconnect_timeout_test.go`:

- `TestSuperviseLifecycleBoundsDisconnectOnInboxHang` — primary
  leak-fix pin. Reuses the `blockingInbox` helper from
  `protocol/sender_disconnect_timeout_test.go` to block
  `DisconnectClientSubscriptions` on `<-ctx.Done()`. Sets
  `DisconnectTimeout = 150ms`, closes `dispatchDone` to simulate a
  ws-read exit, waits for the supervisor to reach the inbox, and
  asserts:
  - the blocking inbox call was invoked
  - `conn.closed` fired (step 4 of the teardown ran)
  - supervisor returned (both done channels drained)
  - elapsed time ≥ `DisconnectTimeout` (ctx bounded, didn't trip
    early on an unrelated signal)
  - elapsed time ≤ `DisconnectTimeout + 1 s` slack (ctx actually
    bounded, didn't leak past the timeout)
  - `mgr.Get(conn.ID) == nil` (step 3 ran after step 1 returned)
  - `OnDisconnect` count == 1 (teardown proceeded through step 2
    after step 1's ctx-bounded return)
  Fails if a future refactor restores the direct `ctx` forward, drops
  the `defer cancel()`, or short-circuits steps 3–5 on ctx
  cancellation.
- `TestSuperviseLifecycleDeliversOnInboxOK` — happy-path pin. A
  normal (non-blocking) `fakeInbox` drives the supervisor and the
  test asserts the supervisor completes well under
  `DisconnectTimeout` (so the bounded ctx is the ceiling, not the
  floor). Fails if a future change serialises on
  `<-time.After(DisconnectTimeout)` instead of returning on inbox
  completion.

Both pass with current code. Both run green under `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing in the post-change full-suite run:
- `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`
- `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`
- `TestWatchReducerResponseExitsOnConnClose`
- `TestWatchReducerResponseDeliversOnRespCh`
- `TestWatchReducerResponseExitsOnRespChClose`
- `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`
- `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`
- `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert`
- `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove`
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join`
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable`
- `TestCommittedSnapshotTableScanPanicsOnMidIterClose`
- `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`
- `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose`
- `TestCommittedSnapshotTableScanPanicsAfterClose`
- `TestCommittedSnapshotIndexScanPanicsAfterClose`
- `TestCommittedSnapshotIndexRangePanicsAfterClose`
- `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`
- `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`
- `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`
- `TestSuperviseLifecycleInvokesDisconnectOnReadPumpExit`

## Remaining OI-004 sub-hazards

Still open:
- other detached goroutines in the protocol lifecycle surface
  (`protocol/conn.go`, `protocol/lifecycle.go`, `protocol/outbound.go`,
  `protocol/keepalive.go`) — each is its own potential narrow
  sub-slice if a specific leak site surfaces
- `ClientSender.Send` remains synchronous without its own ctx; a
  Send-ctx parameter would let callers propagate a shorter
  cancellation scope than `DisconnectTimeout` into the overflow
  path, but no concrete consumer needs that today
- `ConnManager.CloseAll` forwards whatever ctx the caller supplies;
  the contract assumes the caller bounds shutdown but no pin
  enforces it

## Authoritative artifacts

- This document.
- `protocol/disconnect.go::superviseLifecycle` — bounded-ctx derive +
  contract comment.
- `protocol/supervise_disconnect_timeout_test.go` — new focused pin
  tests.
- `TECH-DEBT.md` — OI-004 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
