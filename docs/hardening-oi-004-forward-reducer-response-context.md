# OI-004 — `forwardReducerResponse` ctx / Done lifecycle (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-21.

Direct analog to the already-closed `watchReducerResponse` slice on the
protocol-side watcher (`docs/hardening-oi-004-watch-reducer-response-lifecycle.md`,
2026-04-20). That slice tied the watcher goroutine to `conn.closed`; this
slice closes the symmetric leak on the executor-adapter side of the same
CallReducer round-trip.

Follows the shape of the four prior OI-004 sub-slices landed the same
calendar week:
- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- `docs/hardening-oi-004-sender-disconnect-context.md`
- `docs/hardening-oi-004-supervise-disconnect-context.md`
- `docs/hardening-oi-004-closeall-disconnect-context.md`

## Sharp edge

`executor/protocol_inbox_adapter.go::ProtocolInboxAdapter.CallReducer`
submits a `CallReducerCmd` into the executor, then spawns
`go a.forwardReducerResponse(ctx, req, respCh)` at
`protocol_inbox_adapter.go:128`. The spawned goroutine reads from the
executor-internal `chan ProtocolCallReducerResponse` and forwards the
heavy `protocol.TransactionUpdate` onto the caller's
`req.ResponseCh` (buffer 1 at the `handle_callreducer.go:30`
allocation), which is then picked up by
`runReducerResponseWatcher` and delivered to the client.

Before this slice, `forwardReducerResponse` selected on two arms:
- `case resp := <-respCh:` — executor finished and fed the internal
  response channel
- `case <-ctx.Done():` — dispatch ctx cancelled

The production dispatch ctx is rooted at `context.Background()`
(`protocol/upgrade.go:201`) and threaded through
`Conn.runDispatchLoop` into every handler, then into
`executor.CallReducer` → `forwardReducerResponse`. With a Background
root, the ctx.Done arm never fires, so if the executor accepted the
CallReducer but then never fed the internal respCh the goroutine
leaked forever — executor crash mid-commit, hung reducer on a
shutting-down engine, scheduler-held lock against the commit path,
any code path that returns from the executor worker without driving
the request's reply seam.

Exact same hazard class as the 2026-04-20
`watchReducerResponse` leak on the protocol-side watcher. That slice
fixed its own respCh-never-fires case by tying the watcher to
`conn.closed`; the executor-adapter forwarder had an identical shape
with the same Background-rooted ctx, still open.

The `conn.closed` channel is closed by `Conn.Disconnect` as step 4 of
the SPEC-005 §5.3 teardown, so wiring it into the forwarder's select
gives a promptly-firing lifecycle signal even when the dispatch ctx
never cancels.

## Fix

Narrow and pin. Propagate `conn.closed` through the existing
`protocol.CallReducerRequest` struct so the executor adapter can
observe connection teardown without a protocol → executor dependency
inversion:

1. `protocol/lifecycle.go::CallReducerRequest` gains a
   `Done <-chan struct{}` field, contract-documented to carry the
   owning connection's lifecycle signal. A nil Done blocks forever on
   its select arm — matches pre-wire behavior for callers that do not
   attach a lifecycle signal (keeps the existing
   `executor/protocol_inbox_adapter_test.go` callers working unchanged).
2. `protocol/handle_callreducer.go::handleCallReducer` sets
   `Done: conn.closed` when building the request.
3. `executor/protocol_inbox_adapter.go::forwardReducerResponse` adds a
   third select arm: `case <-req.Done:`. The goroutine exits on the
   first of `respCh` / `ctx.Done()` / `req.Done`, whichever fires
   first.

Nothing else changes. Happy path unchanged: when `respCh` fires, the
select completes on that arm and the existing forwarding logic
(committed / NoSuccessNotify / failed / encode-error sub-cases) runs
exactly as before. Leak path: after `conn.closed` fires the select
returns, the goroutine exits, the `*Conn` and its transitive state
become collectible.

## Scope / limits

- Closes the narrow sub-hazard at the `forwardReducerResponse`
  boundary only.
- Does not change the `ExecutorInbox` contract. Only
  `CallReducerRequest` grows a new optional field; callers that don't
  set it keep the current behavior (select arm on `<-nil` blocks
  forever).
- Does not change the ctx threading. Dispatch still hands
  `context.Background()` through; fixing the broader "dispatch ctx is
  Background" observation is a separate audit (the dispatch-handler
  ctx audit still open under OI-004). This slice fixes the one
  forwarder-leak site using a lifecycle signal rather than widening
  scope.
- Does not change `sendTransactionUpdateWithContext`. The protocol-
  side `req.ResponseCh` is buffer 1 in production, so the post-
  respCh send never blocks under the single-sender contract.
- Does not eliminate the possibility that the outer `watchReducerResponse`
  watcher exits via `conn.closed` and a late write succeeds into its
  buffer — that write is benign (buffered 1, garbage-collected with
  the channel). The fix here only closes the *upstream* forwarder
  leak.

Diff surface:
- `protocol/lifecycle.go::CallReducerRequest` — new `Done <-chan struct{}`
  field + contract comment.
- `protocol/handle_callreducer.go::handleCallReducer` — wires
  `Done: conn.closed`.
- `executor/protocol_inbox_adapter.go::forwardReducerResponse` —
  third select arm + contract comment naming the pin test.

## Pinned by

Two focused tests in `executor/forward_reducer_response_done_test.go`:

- `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs`
  — primary leak-fix pin. `respCh` is unbuffered and never fed;
  `req.Done` is a live channel that's closed partway through. The
  test first asserts the forwarder does *not* return during a short
  window where both arms are open (proves the goroutine is actually
  parked on the select), then closes `req.Done` and asserts the
  goroutine returns within 1 s. Also asserts nothing was written to
  `req.ResponseCh` on the Done-triggered exit. Fails if a future
  refactor drops the `req.Done` select arm, reverts the request
  struct field, or short-circuits the forwarder on any arm other
  than its three defined ones.
- `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneAlreadyClosed`
  — pins the pre-closed Done case. If `req.Done` is already closed
  at entry, the forwarder must not wedge on `<-respCh`; it must
  return promptly. Fails if a future refactor re-orders the select
  to probe respCh unconditionally before entering the select block.

Both pass with current code. Both run green under `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing in the post-change full-suite run:
- `TestCloseAllBoundsDisconnectOnInboxHang`
- `TestCloseAllDeliversOnInboxOK`
- `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`
- `TestSuperviseLifecycleDeliversOnInboxOK`
- `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`
- `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`
- `TestWatchReducerResponseExitsOnConnClose`
- `TestWatchReducerResponseDeliversOnRespCh`
- `TestWatchReducerResponseExitsOnRespChClose`
- `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnContextCancelWhenOutboundBlocked`
  (pre-existing ctx-cancel-on-outbound-block pin, still passing)

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
  Different hang class from the forwarder leak — request-side block
  rather than response-side leak — but the same "Background-rooted
  caller" shape. A narrow sub-slice would audit whether any handler
  call forwards ctx into an executor seam that can block unboundedly
  and, if so, derive a per-request timeout.

## Authoritative artifacts

- This document.
- `protocol/lifecycle.go::CallReducerRequest` — new `Done` field.
- `protocol/handle_callreducer.go::handleCallReducer` — wires
  `conn.closed` into `Done`.
- `executor/protocol_inbox_adapter.go::forwardReducerResponse` —
  third select arm + contract comment.
- `executor/forward_reducer_response_done_test.go` — new focused pin
  tests.
- `TECH-DEBT.md` — OI-004 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
