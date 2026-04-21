# OI-004 — `connManagerSender.enqueueOnConn` disconnect context (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-21.

Follows the same shape as the prior OI-004 sub-slice
(`docs/hardening-oi-004-watch-reducer-response-lifecycle.md`) and the
OI-005 / OI-006 sub-slice precedents
(`docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`,
`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`,
`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`,
`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-006-fanout-aliasing.md`).

## Sharp edge

`protocol/sender.go::connManagerSender.enqueueOnConn` is the common
encode-and-enqueue path for `Send` / `SendTransactionUpdate` /
`SendTransactionUpdateLight`. When the per-connection `OutboundCh` is
full the non-blocking `default` arm of the select runs the SPEC-005
§10.1 overflow teardown:

```go
default:
    go conn.Disconnect(context.Background(), websocket.StatusPolicyViolation, "send buffer full", s.inbox, s.mgr)
    return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
```

The goroutine is detached — the caller of `Send` returns immediately
with `ErrClientBufferFull`, and the teardown runs in the background.
`Conn.Disconnect` (`protocol/disconnect.go:35`) threads the ctx into
`inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` at
steps 1 and 2 of the SPEC-005 §5.3 teardown, both of which honor
`<-ctx.Done()` in `executor/protocol_inbox_adapter.go` (`awaitReducerStatus`
at lines 133–145, the direct select at lines 58–63).

With `context.Background()` those two calls are effectively
non-cancellable. Concrete ways the goroutine reaches production
unbounded:

- executor dispatch deadlock (queue full with a held lock, wedged
  single-writer main loop)
- executor crashes mid-transaction and the `respCh` the inbox adapter
  is waiting on is never fed
- `DisconnectClientSubscriptions` seam inside the executor holds an
  internal lock against a scheduler or fan-out goroutine that is
  itself stuck

In each case the detached goroutine holds a live reference to the
`*Conn` (and transitively its outbound channel, decoded frames,
keep-alive state, and websocket handle) forever. `closeOnce.Do` has
latched but the body has not run past the blocked inbox call, so
`c.closed` is also never closed — `runDispatchLoop`, `runKeepalive`,
and the write loop for that conn also cannot exit. This is the same
goroutine-ownership leak class as `watchReducerResponse` pre-fix
(`docs/hardening-oi-004-watch-reducer-response-lifecycle.md`), just at
the overflow-driven teardown boundary instead of the caller-bound
reducer response boundary.

The other `Conn.Disconnect` call sites already pass a cancellable
ctx: `ConnManager.CloseAll` (`protocol/conn.go:150`) forwards the
engine-shutdown ctx, and `Conn.superviseLifecycle` (`protocol/disconnect.go:77`)
forwards the HTTP handler ctx. The overflow path was the one
hold-out.

## Fix

Narrow and pin. Three-part change, zero behavior change for the
non-hang overflow path:

1. `protocol/options.go` — new `ProtocolOptions.DisconnectTimeout`
   field (default `5 * time.Second`). 5 s is the ceiling the detached
   goroutine will spend inside the inbox calls before the teardown
   proceeds to `mgr.Remove` / `close(c.closed)` anyway. Long enough
   for a normal executor dispatch + on-disconnect reducer; short
   enough that a process-wide stall doesn't accrete unbounded conn
   state across repeated overflows.
2. `protocol/sender.go::connManagerSender.enqueueOnConn` — the
   detached goroutine now creates a bounded ctx from
   `context.WithTimeout(context.Background(), conn.opts.DisconnectTimeout)`
   and defers its `cancel()`, then forwards that ctx into
   `Conn.Disconnect`. Happy path unchanged: the inbox calls return
   promptly, the goroutine exits, the deferred `cancel` fires, and
   the ctx is collected with the goroutine.
3. `protocol/options_test.go` — extended to assert the
   `DisconnectTimeout = 5 * time.Second` default, matching the
   pattern already used for `PingInterval`, `IdleTimeout`, and
   `CloseHandshakeTimeout`.

The `context.Background()` root is preserved because the overflow
path has no caller ctx to inherit — `Send` / `SendTransactionUpdate` /
`SendTransactionUpdateLight` are synchronous calls that the fan-out
worker and executor response seams make without a cancellation scope.
Threading a server-lifetime ctx through `NewClientSender` would be
broader than this narrow slice and is a follow-on if engine-shutdown
cancellation ever needs to reach into overflow-driven teardown faster
than `DisconnectTimeout`.

Post-timeout behavior: `inbox.DisconnectClientSubscriptions` and
`inbox.OnDisconnect` return `ctx.Err()` (the adapter's select arm
maps `<-ctx.Done()` to `ctx.Err()`). `Conn.Disconnect` logs the error
at info level (`log.Printf` in disconnect.go lines 37–42) and
continues to steps 3–5 unconditionally — disconnect cannot be vetoed
(SPEC-003 §10.4). The `*Conn` becomes collectible once all goroutines
observing `c.closed` have exited.

## Scope / limits

- Closes the narrow sub-hazard at the
  `connManagerSender.enqueueOnConn` overflow boundary only. It does
  not touch other detached-goroutine sites in
  `protocol/conn.go` / `protocol/lifecycle.go` /
  `protocol/outbound.go` / `protocol/keepalive.go`. OI-004 stays
  open for those surfaces if workload or audit evidence surfaces a
  specific leak site.
- Does not change the `ExecutorInbox` contract. The inbox adapters
  already honor ctx cancellation; this slice just stops handing them
  a non-cancellable ctx.
- Does not change the `Conn.Disconnect` ordering. Steps 1–2 still
  run synchronously before steps 3–5; the only change is that a
  hung step 1 or 2 now times out instead of blocking forever.
- Does not introduce a per-operation ctx plumbed through
  `ClientSender.Send`. The overflow path is the only `Send` seam
  that spawns a goroutine; adding a Send-ctx parameter would be a
  broader API change without a concrete consumer today.
- 5 s is a heuristic, not a reference-sourced value. Reference
  SpacetimeDB uses a tokio `abort_handle.abort()` rather than a
  graceful teardown, so there is no direct parity anchor to mirror;
  5 s matches the shape the existing `CloseHandshakeTimeout` (250 ms)
  sets for the ws Close handshake, scaled up by an order of
  magnitude for the heavier executor dispatch the inbox calls
  incur.

Diff surface:
- `protocol/options.go` — `DisconnectTimeout` field + default.
- `protocol/sender.go::connManagerSender.enqueueOnConn` — bounded-ctx
  spawn + contract comment naming the pin test.
- `protocol/options_test.go` — default-value assertion extended.

## Pinned by

Two focused tests in `protocol/sender_disconnect_timeout_test.go`:

- `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang` — primary
  leak-fix pin. A `blockingInbox` blocks
  `DisconnectClientSubscriptions` on `<-ctx.Done()` to simulate an
  executor-dispatch stall. The test sets `DisconnectTimeout = 150ms`,
  overflows the outbound channel, waits for the detached goroutine
  to complete the teardown, and asserts:
  - `ErrClientBufferFull` returned from the overflow `Send`
  - the blocking inbox call was invoked
  - `conn.closed` fired (step 4 of the teardown ran)
  - elapsed time ≥ `DisconnectTimeout` (ctx bounded, didn't trip
    early on an unrelated signal)
  - elapsed time ≤ `DisconnectTimeout + 1 s` slack (ctx actually
    bounded, didn't leak past the timeout)
  - `mgr.Get(conn.ID) == nil` (step 3 ran after step 1 returned)
  - `OnDisconnect` count == 1 (teardown proceeded through step 2
    after step 1's ctx-bounded return)
  Fails if a future refactor restores `context.Background()`, drops
  the `defer cancel()`, or short-circuits steps 3–5 on ctx
  cancellation.
- `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK` — happy-path
  pin. A normal (non-blocking) `fakeInbox` drives the overflow path
  and the test asserts the detached goroutine completes well under
  `DisconnectTimeout` (so the bounded ctx is the ceiling, not the
  floor). Fails if a future change serialises on
  `<-time.After(DisconnectTimeout)` instead of returning on inbox
  completion.

Both pass with current code. Both run green under `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing in the post-change full-suite run:
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

## Authoritative artifacts

- This document.
- `protocol/options.go` — `DisconnectTimeout` field + 5 s default.
- `protocol/sender.go::connManagerSender.enqueueOnConn` — bounded-ctx
  spawn + contract comment.
- `protocol/sender_disconnect_timeout_test.go` — new focused pin tests.
- `protocol/options_test.go` — default-value assertion extended.
- `TECH-DEBT.md` — OI-004 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
