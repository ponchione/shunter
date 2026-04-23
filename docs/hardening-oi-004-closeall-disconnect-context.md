# OI-004 — `ConnManager.CloseAll` disconnect context (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-21.

Follows the shape of the three prior OI-004 sub-slices landed the same
calendar week:
- `docs/hardening-oi-004-supervise-disconnect-context.md`
- `docs/hardening-oi-004-sender-disconnect-context.md`
- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`

## Sharp edge

`protocol/conn.go::ConnManager.CloseAll` is the graceful-shutdown entry
point (SPEC-005 §11.1, close code 1000). For each registered `*Conn`
it spawns a goroutine that invokes
`c.Disconnect(ctx, CloseNormal, "server shutdown", inbox, m)` and the
parent call blocks on a `sync.WaitGroup` until every per-conn
teardown returns.

Before this slice, the caller-supplied `ctx` was threaded straight
through to each `Conn.Disconnect`, which then forwarded it into
`inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` — steps
1 and 2 of the SPEC-005 §5.3 teardown. Both honor ctx cancellation via
the adapter's select arm (`executor/protocol_inbox_adapter.go:58-63`)
and `awaitReducerStatus` (`executor/protocol_inbox_adapter.go:133-145`).

The hazard: the caller contract was unpinned. A caller that handed
`CloseAll` a `context.Background()` (or any ctx that never cancels)
made the inbox calls effectively non-cancellable for every connection
in the manager. A single hung inbox seam — executor dispatch deadlock,
inbox-drain stall, scheduler-held lock against fan-out, executor crash
waiting on a never-fed `respCh` — pinned its `*Conn`'s
supervisor-spawned goroutine and transitively the `WaitGroup` in
`CloseAll`, which blocks process shutdown.

Same hazard class as the two other Background-rooted
`Conn.Disconnect` call sites already closed this week:
- `Conn.superviseLifecycle` previously forwarded
  `context.Background()` received from `protocol/upgrade.go:211` into
  `Conn.Disconnect` (closed 2026-04-21, see
  `docs/hardening-oi-004-supervise-disconnect-context.md`).
- `connManagerSender.enqueueOnConn` previously spawned
  `go conn.Disconnect(context.Background(), ...)` for the SPEC-005
  §10.1 overflow teardown (closed 2026-04-21, see
  `docs/hardening-oi-004-sender-disconnect-context.md`).

`CloseAll` is the third and final site in that family. Today the only
callers are tests (`protocol/close_test.go`) that pass
`context.Background()` and the OI-008 server lifecycle that does not
yet exist. Pinning the contract now prevents a future runtime/bootstrap layer from
re-introducing the same latent leak.

## Fix

Narrow and pin. Per-conn bounded-ctx derive inside the `CloseAll`
goroutine, zero behavior change for the happy path:

1. `protocol/conn.go::ConnManager.CloseAll` — each per-conn goroutine
   now derives `context.WithTimeout(ctx, c.opts.DisconnectTimeout)`
   with `defer cancel()` before calling `Disconnect`. Reuses the
   existing `ProtocolOptions.DisconnectTimeout` field (default `5 s`,
   introduced by the sender-disconnect slice) — no new option, no new
   default.

The outer ctx is still honored: a cancellation on the caller's ctx
propagates into every per-conn derived ctx immediately (Go's
`context.WithTimeout` on a cancelled parent cancels synchronously).
But a Background-rooted caller no longer stalls shutdown indefinitely.

Happy path unchanged: inbox calls return promptly, per-conn
Disconnect completes, deferred cancel fires, WaitGroup drains. Hang
path: after `DisconnectTimeout` the inbox returns `ctx.Err()`;
`Conn.Disconnect` logs the error at info level and proceeds to steps
3-5 of the teardown unconditionally — `mgr.Remove` + `cancelRead` +
`close(c.closed)` + optional `closeWithHandshake` — so the `*Conn`
becomes collectible and the WaitGroup unblocks.

## Scope / limits

- Closes the narrow sub-hazard at the `ConnManager.CloseAll` boundary
  only. Closes the `Background`-rooted `Conn.Disconnect` call-site
  family started by the two prior sub-slices: supervisor, sender
  overflow, CloseAll all now derive a bounded ctx at the spawn point.
- Does not change the `ExecutorInbox` contract. The adapters already
  honor ctx cancellation; this slice just stops handing them a
  non-cancellable ctx from the CloseAll site.
- Does not change the `Conn.Disconnect` ordering. Steps 1-2 still
  run synchronously before steps 3-5; the only change is that a
  hung step 1 or 2 now times out per-conn instead of blocking
  forever.
- Does not change the outer `CloseAll` ctx semantics. A caller that
  passes a short ctx still cuts the teardown short; a caller that
  passes Background now has its per-conn window bounded by
  `DisconnectTimeout` rather than unbounded.
- Reuses the existing `DisconnectTimeout` default (5 s). The
  rationale from
  `docs/hardening-oi-004-sender-disconnect-context.md` still applies:
  long enough for a normal executor dispatch + on-disconnect
  reducer; short enough that a process-wide stall doesn't accrete
  unbounded conn state across repeated teardowns.

Diff surface:
- `protocol/conn.go::ConnManager.CloseAll` — per-conn bounded-ctx
  derive + contract comment naming the pin tests.

## Pinned by

Two focused tests in `protocol/closeall_disconnect_timeout_test.go`:

- `TestCloseAllBoundsDisconnectOnInboxHang` — primary leak-fix pin.
  Reuses the `blockingInbox` helper from
  `protocol/sender_disconnect_timeout_test.go` to block
  `DisconnectClientSubscriptions` on `<-ctx.Done()`. Sets
  `DisconnectTimeout = 150ms`, calls `CloseAll` with
  `context.Background()`, waits for the goroutine to hit the inbox,
  and asserts:
  - `CloseAll` returned (WaitGroup drained)
  - `conn.closed` fired (step 4 of the teardown ran)
  - elapsed time ≥ `DisconnectTimeout` (ctx bounded, didn't trip
    early on an unrelated signal)
  - elapsed time ≤ `DisconnectTimeout + 1 s` slack (ctx actually
    bounded, didn't leak past the timeout)
  - `mgr.Get(conn.ID) == nil` (step 3 ran after step 1 returned)
  - `DisconnectClientSubscriptions` + `OnDisconnect` counts == 1
    (teardown proceeded through step 2 after step 1's ctx-bounded
    return)
  Fails if a future refactor restores a direct `ctx` forward into
  `Conn.Disconnect`, drops the `defer cancel()`, or short-circuits
  steps 3-5 on ctx cancellation.
- `TestCloseAllDeliversOnInboxOK` — happy-path pin. A normal
  (non-blocking) `fakeInbox` drives `CloseAll` and the test asserts
  completion well under `DisconnectTimeout` (so the bounded ctx is
  the ceiling, not the floor). Fails if a future change serialises
  on `<-time.After(DisconnectTimeout)` instead of returning on inbox
  completion.

Both pass with current code. Both run green under `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing in the post-change full-suite run:
- `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`
- `TestSuperviseLifecycleDeliversOnInboxOK`
- `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`
- `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`
- `TestWatchReducerResponseExitsOnConnClose`
- `TestWatchReducerResponseDeliversOnRespCh`
- `TestWatchReducerResponseExitsOnRespChClose`
- `TestCloseAll_DisconnectsEveryConnection` (pre-existing happy-path
  coverage)
- `TestCloseAll_EmptyManagerNoOp` (pre-existing no-op coverage)
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
- dispatch-handler ctx audit: `protocol/dispatch.go:168` spawns
  handler goroutines with the `ctx` received by `runDispatchLoop`,
  hardcoded to `context.Background()` at `protocol/upgrade.go:201`.
  Handlers forward that ctx into `inbox.CallReducer` /
  `inbox.RegisterSubscriptionSet` / `inbox.UnregisterSubscriptionSet`.
  Different hang class from the disconnect path — request/response
  rather than teardown — but the same "Background-rooted caller"
  shape. A narrow sub-slice would audit whether any handler call
  forwards ctx into an executor seam that can block unboundedly and,
  if so, derive a per-request timeout.
- `forwardReducerResponse` ctx audit:
  `executor/protocol_inbox_adapter.go:128` spawns
  `go a.forwardReducerResponse(ctx, req, respCh)` with the
  Background ctx from the dispatch loop. If the executor never sends
  on its internal `respCh` (crash mid-commit), the goroutine leaks.
  Analog to the 2026-04-20 `watchReducerResponse` fix on the other
  seam — narrow sub-slice if workload evidence surfaces.

## Authoritative artifacts

- This document.
- `protocol/conn.go::ConnManager.CloseAll` — per-conn bounded-ctx
  derive + contract comment.
- `protocol/closeall_disconnect_timeout_test.go` — new focused pin
  tests.
- `TECH-DEBT.md` — OI-004 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
