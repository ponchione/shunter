# OI-004 — `watchReducerResponse` goroutine lifecycle (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-004
(protocol lifecycle / goroutine ownership) landed 2026-04-20.

Follows the same shape as the OI-005 / OI-006 sub-slices
(`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`,
`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`,
`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-006-fanout-aliasing.md`).

## Sharp edge

`protocol/async_responses.go::watchReducerResponse` is the watcher the
CallReducer dispatch path spawns after the executor has accepted a
reducer request (`protocol/handle_callreducer.go:43`). Its body read:

```go
func watchReducerResponse(conn *Conn, respCh <-chan TransactionUpdate) {
    go func() {
        resp, ok := <-respCh
        if !ok {
            return
        }
        sender := connOnlySender{conn: conn}
        if err := sender.SendTransactionUpdate(conn.ID, &resp); err != nil {
            logReducerDeliveryError(conn, resp.ReducerCall.RequestID, err)
        }
    }()
}
```

The goroutine blocks unconditionally on `<-respCh`. If the executor
accepts the CallReducer request (`CallReducer` returns nil so the
dispatch path reaches line 43) but never sends on or closes `respCh`,
the goroutine never wakes up. Concrete ways this reaches production:

- executor crashes or panics after accepting the request but before
  emitting the envelope; the response channel is dropped on the floor
- engine shutdown path tears down the executor with a reducer still
  in-flight and does not drain per-request `ResponseCh`s
- a future reducer path returns early without closing `respCh` (today's
  committed and synthetic-failure paths both emit, so current code does
  not hit this, but the contract was unpinned)

In each case the `*Conn` owned by the closure stays reachable, which
prevents GC of the connection state, its outbound buffer, and every
transitive reference it holds (decoded frames in the inbox, pending
pong timestamps, websocket handles). Over the process lifetime these
accrete silently — a classic owned-lifecycle escape that the existing
`connOnlySender.Send` close-awareness (`<-s.conn.closed`) does not
compensate for, because the select never reaches `Send` when the outer
`<-respCh` read is still blocked.

Existing teardown surface already satisfies the owned-lifecycle
contract for every other protocol-layer goroutine: `Conn.Disconnect`
(`protocol/disconnect.go:47`) closes `c.closed` as step 4 of the
SPEC-005 §5.3 teardown, and that closed channel is what
`runDispatchLoop`, `runKeepalive`, and the write loop use to exit. The
watcher was the one hold-out that did not observe it.

## Fix

Narrow and pin. Two-part change, zero behavior change for current
callers on the happy path (envelope still goes out on `respCh`):

1. `protocol/async_responses.go` — the goroutine body is moved into a
   package-level helper `runReducerResponseWatcher` and the body's
   blocking read becomes a two-case `select` on `respCh` and
   `conn.closed`. If `conn.closed` wins, the watcher returns
   immediately. The happy path (`respCh` fires first) is unchanged
   apart from living inside the select. A contract comment on
   `watchReducerResponse` records the rationale and the pin tests.
2. `protocol/async_responses_test.go` — new file, three focused tests
   that exercise the watcher body directly via
   `runReducerResponseWatcher`:
   - `TestWatchReducerResponseExitsOnConnClose` — never-firing respCh
     plus `close(conn.closed)` exits the watcher within 2 s.
   - `TestWatchReducerResponseDeliversOnRespCh` — a single send on
     respCh produces a decoded `TagTransactionUpdate` on `OutboundCh`;
     guards the happy path against a future refactor that inverts the
     select arms.
   - `TestWatchReducerResponseExitsOnRespChClose` — a closed (not
     sent-on) respCh exits the watcher cleanly and does not deliver a
     zero-value TransactionUpdate.

Helper extraction is purely for testability — the test owns the
goroutine itself and waits on a `done` channel it closes after the
body returns, giving a deterministic bounded-wait pattern that would
otherwise require `runtime.NumGoroutine` sampling.

Post-close behavior: `respCh` is allocated with buffer 1 at
`protocol/handle_callreducer.go:30`, so a single post-close send from
a still-running executor completes without blocking and the message
is garbage-collected with the channel. The fan-out seam is unaffected:
fan-out delivery uses `ConnManager.Get`, which returns nil for
disconnected IDs, so no second delivery attempt races the watcher
exit.

## Scope / limits

- Closes the narrow sub-hazard at the `watchReducerResponse` boundary
  only. It does not touch any other goroutine in
  `protocol/conn.go` / `protocol/lifecycle.go` / `protocol/outbound.go`
  / `protocol/sender.go` / `protocol/keepalive.go`. OI-004 stays open
  for those surfaces if workload or audit evidence surfaces a specific
  leak site.
- Does not change the `CallReducerRequest.ResponseCh` contract at the
  executor seam. The executor is still expected to send or close the
  channel on normal completion paths; the fix only protects against
  the failure modes where that contract is not upheld.
- Does not add a `context.Context` parameter. The `Conn`'s closed
  channel already represents the owned lifecycle; adding a parallel
  ctx would duplicate the cancellation signal. If a future caller
  needs a shorter cancellation scope than the conn's lifetime, threading
  ctx is a small follow-on.
- Does not harden the synthetic-failure paths in
  `sendSyntheticFailure` or the non-async delivery paths; those run
  synchronously on the dispatch goroutine and are not subject to the
  same leak class.

## Pinned by

Three focused tests in `protocol/async_responses_test.go`:

- `TestWatchReducerResponseExitsOnConnClose` — the primary leak-fix
  pin. Fails if the `<-conn.closed` arm is removed or the select
  collapses back to an unconditional blocking read.
- `TestWatchReducerResponseDeliversOnRespCh` — the happy-path pin.
  Fails if the select arms are inverted in a way that always races to
  `conn.closed` or drops the outbound send.
- `TestWatchReducerResponseExitsOnRespChClose` — the channel-close
  pin. Fails if a future refactor drops the `ok` check and delivers a
  zero-value `TransactionUpdate`.

All three run green under `-race -count=3`.

Already-landed OI-005 / OI-006 pins unchanged and still passing in the
post-change full-suite run (`Go test: 1127 passed in 10 packages`):
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
  `protocol/sender.go`, `protocol/keepalive.go`) — each is its own
  potential narrow sub-slice if a specific leak site surfaces
- `CallReducerRequest.ResponseCh` is still a side-channel the executor
  must upkeep; a stronger contract would own the response delivery
  seam inside the executor, but that is broader scope than OI-004

## Authoritative artifacts

- This document.
- `protocol/async_responses.go::watchReducerResponse` /
  `runReducerResponseWatcher` — helper split + `<-conn.closed` arm +
  contract comment.
- `protocol/async_responses_test.go` — new focused pin tests.
- `TECH-DEBT.md` — OI-004 updated with sub-hazard closed + pin anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
