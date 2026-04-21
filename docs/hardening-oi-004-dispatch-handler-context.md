# OI-004 — dispatch-handler ctx (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-21.

Sixth OI-004 sub-slice. Follows the shape of the five prior sub-slices
landed the same calendar week:

- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- `docs/hardening-oi-004-sender-disconnect-context.md`
- `docs/hardening-oi-004-supervise-disconnect-context.md`
- `docs/hardening-oi-004-closeall-disconnect-context.md`
- `docs/hardening-oi-004-forward-reducer-response-context.md`

Direct analog to the `forwardReducerResponse` slice landed earlier the
same day: that slice closed a response-side goroutine leak by wiring
`conn.closed` as an additional select arm; this slice closes the
request-side analog by wiring `conn.closed` as an additional ctx-cancel
source on the per-message handler goroutine.

## Sharp edge

`protocol/dispatch.go:192` spawns one goroutine per inbound message:

```
go func(run func()) {
    defer func() { <-c.inflightSem }()
    run()
}(run)
```

`run` is a closure over one of:

- `handlers.OnSubscribeSingle(ctx, c, &m)`
- `handlers.OnSubscribeMulti(ctx, c, &m)`
- `handlers.OnUnsubscribeSingle(ctx, c, &m)`
- `handlers.OnUnsubscribeMulti(ctx, c, &m)`
- `handlers.OnCallReducer(ctx, c, &m)`
- `handlers.OnOneOffQuery(ctx, c, &m)`

The `ctx` captured in each closure is the one received by
`runDispatchLoop`, hardcoded to `context.Background()` at
`protocol/upgrade.go:201`. Every handler except `handleOneOffQuery`
forwards that ctx into `ExecutorInbox.CallReducer` /
`ExecutorInbox.RegisterSubscriptionSet` /
`ExecutorInbox.UnregisterSubscriptionSet`, which the
`ProtocolInboxAdapter` translates to
`executor.SubmitWithContext(ctx, cmd)`. In the default (non-reject)
inbox mode, that seam is:

```
select {
case e.inbox <- cmd:
    return nil
case <-ctx.Done():
    return ctx.Err()
}
```

With a Background-rooted `ctx`, the `ctx.Done()` arm never fires. If
the executor wedges (hung reducer on the main loop, scheduler pump
blocked on a long commit, engine stall), the command inbox fills to
`InboxCapacity` and every subsequent handler goroutine parks on
`e.inbox <- cmd` indefinitely. Each hung goroutine holds:

- an `inflightSem` slot (`dispatch.go:186-190`), capacity
  `IncomingQueueMessages`
- a closure capture of the `*Conn` and the decoded message
- a chain back to the executor through the submitted command

After `IncomingQueueMessages` concurrent hangs the connection's read
loop closes with `1008 "too many requests"` (`dispatch.go:188`) and
the supervisor drives Disconnect. Disconnect step 4 fires
`close(c.closed)` unconditionally — but the hung goroutines do not
observe that channel, so they stay parked forever, pinning the `*Conn`
and the `inflightSem` past teardown. Disconnect's bounded-ctx
sub-slices (supervisor, CloseAll, sender overflow) protect the
*teardown* path itself but do not unblock the request-side goroutines.

Same hazard class as the earlier `forwardReducerResponse` leak on the
executor-adapter side of the CallReducer round-trip: Background-rooted
ctx, unbounded wait, goroutine pinned past `conn.closed`. Different
select shape (this one blocks inside `SubmitWithContext` before the
command is even accepted), so the fix lives at a different seam, but
the closure pattern is the same.

The `OneOffQuery` handler does not route through the executor inbox —
it reads directly from the committed state via
`CommittedStateAccess.Snapshot()` and returns synchronously — so the
Background-ctx observation does not create a leak there. No code
change for that handler; handlerCtx still flows through for
consistency and future-proofing.

## Fix

Narrow and pin. Derive a dispatch-scoped `handlerCtx` inside
`runDispatchLoop` that cancels on either the outer ctx OR `c.closed`,
and pass it into every handler closure instead of the outer `ctx`:

```go
handlerCtx, handlerCancel := context.WithCancel(ctx)
defer handlerCancel()
go func() {
    select {
    case <-c.closed:
        handlerCancel()
    case <-handlerCtx.Done():
    }
}()
```

After the switch, every `run = func() { handlers.On*(handlerCtx, c, &m) }`
closure captures `handlerCtx` in place of `ctx`.

`c.closed` is closed at step 4 of the SPEC-005 §5.3 teardown (see
`protocol/disconnect.go:47`) regardless of the outer ctx plumbing, so
handler goroutines observe teardown through `SubmitWithContext`'s
`ctx.Done()` arm and exit promptly. The inflightSem slot releases
via the existing `defer func() { <-c.inflightSem }()`, and the `*Conn`
capture in the closure becomes collectible.

Read ctx (`readCtx`) is untouched. It continues to derive from the
outer ctx + `c.readCtx` and feed `c.ws.Read` only. Keeping read and
handler ctxs separate preserves the existing test-harness contract
around `c.readCtx` (used to cancel Read without cancelling the outer
ctx).

## Scope / limits

- Fixes the dispatch-handler goroutine leak only. The fix does not
  change the default `InboxCapacity`, the Background-rooted production
  ctx at `protocol/upgrade.go:201`, or the `ExecutorInbox` contract.
- Request-side handlers still block inside the inbox-submit arm for
  the time between conn teardown signal and `close(c.closed)` firing
  on the Disconnect goroutine. Disconnect's bounded-ctx slices
  (supervisor, CloseAll, sender overflow) already cap that window at
  `DisconnectTimeout` (default 5 s).
- Does not change handler bodies. Handlers return when
  `SubmitWithContext` returns `ctx.Err()`; existing error paths
  synthesize failure envelopes where applicable (e.g.
  `handle_callreducer.go::sendSyntheticFailure`), which route through
  `conn.OutboundCh` via non-blocking sends in `sender.go` and
  `dispatch.go::sendError` — these late sends are benign after
  teardown because `runOutboundWriter` has exited and subsequent
  sends simply sit in the buffered channel or drop via the `default:`
  arm.
- Does not close the open-ended `ClientSender.Send` no-ctx follow-on
  or the other detached-goroutine audits in `conn.go` / `lifecycle.go`
  / `outbound.go` / `keepalive.go`. Those remain on the OI-004 open
  list.
- Does not change the `OneOffQuery` handler behavior. `handleOneOffQuery`
  does not route through the executor inbox, so the Background-ctx
  observation does not create a leak there. handlerCtx still flows
  through for consistency and future-proofing.

Diff surface:

- `protocol/dispatch.go::runDispatchLoop` — new `handlerCtx` / watcher
  goroutine and every handler closure switched from `ctx` to
  `handlerCtx`.

## Pinned by

Two focused tests in `protocol/dispatch_handler_ctx_test.go`:

- `TestDispatchLoop_HandlerCtxCancelsOnConnClose` — primary leak-fix
  pin. Installs a handler that blocks on `ctx.Done()`; drives a
  subscribe frame to the conn, captures the handler ctx, asserts the
  handler does NOT return during a 25 ms window while both arms are
  open (proves it's actually parked), closes `conn.closed` directly,
  and asserts the handler returns within 1 s and `ctx.Err() != nil`.
  Fails if a future refactor reverts handler closures to the outer
  `ctx`, drops the `c.closed` watcher, or collapses `handlerCtx` onto
  the outer `ctx` without a teardown wire.
- `TestDispatchLoop_HandlerCtxCancelsOnOuterCtx` — pins the second
  leg of the contract: outer ctx cancellation still propagates into
  handler ctx. Prevents a future refactor from accidentally severing
  `handlerCtx` from the outer ctx while wiring `c.closed`.

Both pass with current code. Both run green under `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing in the post-change full-suite run.

## Remaining OI-004 sub-hazards

Still open:

- other detached goroutines in the protocol lifecycle surface
  (`protocol/conn.go`, `protocol/lifecycle.go`,
  `protocol/outbound.go`, `protocol/keepalive.go`) — each is its own
  potential narrow sub-slice if a specific leak site surfaces
- `ClientSender.Send` remains synchronous without its own ctx; a
  Send-ctx parameter would let callers propagate a shorter
  cancellation scope than `DisconnectTimeout` into the overflow path,
  but no concrete consumer needs that today

## Authoritative artifacts

- This document.
- `protocol/dispatch.go::runDispatchLoop` — `handlerCtx` derivation
  and closure updates.
- `protocol/dispatch_handler_ctx_test.go` — new focused pin tests.
- `TECH-DEBT.md` — OI-004 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
