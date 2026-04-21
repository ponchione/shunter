# OI-005 — subscription-seam read-view lifetime (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-20.

Follows the same shape as the prior OI-005 / OI-006 sub-slices
(`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-005-snapshot-iter-useafterclose.md`,
`docs/hardening-oi-005-snapshot-iter-retention.md`,
`docs/hardening-oi-006-fanout-aliasing.md`).

## Sharp edge

`subscription/eval.go::EvalAndBroadcast` receives a borrowed
`store.CommittedReadView` from the executor and runs the post-commit
evaluation loop synchronously on the executor goroutine. The calling
seam in `executor/executor.go:540-541` is:

```go
e.subs.EvalAndBroadcast(txID, changeset, view, meta)
view.Close()
```

Contract: `view.Close()` releases the `CommittedState` RLock. Any
reference to the view that escapes past the `EvalAndBroadcast`
synchronous return and fires a method call afterwards reads against a
released lock — the same class of hazard the three iter-surface OI-005
sub-slices pinned inside `store/snapshot.go`, viewed one layer up at
the subscription seam.

Today's code keeps the contract:

- `evaluate` stashes the view into a `DeltaView` which is `Release()`d
  in `defer`, before `evaluate` returns.
- `collectCandidatesInto` and the per-query evaluators invoke view
  methods synchronously during the call.
- The `FanOutMessage` published on `m.inbox` carries only materialized
  row slices, per-connection fan-out maps, errors, and caller metadata —
  no view reference. The fan-out worker therefore never calls back into
  the view.

But the contract was load-bearing and unpinned: nothing in code or
tests asserted it. A future change that plumbed the view into
`FanOutMessage`, spawned a goroutine from inside `evaluate`, or stashed
the view into per-subscriber state would silently race the executor's
post-return `view.Close()` — a released-lock read or data race on
`CommittedState.rows`, hard to catch at review time.

## Fix

Narrow and pin. Two-part change, no behavior change:

1. Comment pin at `subscription/eval.go::EvalAndBroadcast` documenting
   the view-lifetime contract, the executor seam that enforces
   `view.Close()` immediately after return, and the pin test.
2. Instrument test: `trackingView` in
   `subscription/eval_view_lifetime_test.go` wraps a real
   `CommittedReadView`, counts every method invocation, and records any
   call that arrives after `Close()`. The test invokes
   `EvalAndBroadcast`, closes the tracker on the executor goroutine the
   instant the call returns (matching the real executor sequencing),
   drains the fan-out inbox, and asserts zero post-close method
   invocations.

No production-code change to the eval path. The contract held before
this slice; the slice makes the contract asserted and visible to
future changes.

## Scope / limits

This is a contract pin, not a lifetime enforcement mechanism:

- The tracker observes whether the view is called after a post-return
  `Close`. It does not prevent a future change from stashing the view
  into per-subscriber state or a spawned goroutine.
- It does not cover the broader `store/state_view.go` /
  `store/committed_state.go` shared-state escape routes, which stay
  open as an OI-005 sub-hazard.
- It does not change the underlying snapshot lifetime model — that
  still depends on the three already-closed iter-surface OI-005
  sub-slices for correctness against mid-iter / use-after-Close / GC
  retention.
- The single-goroutine-ownership contract between executor and
  subscription seam remains the fundamental invariant.

Deepening into an owned-context model where `EvalAndBroadcast`
acquires its own view from a `snapshotFn` and closes it internally
would change the executor–subscription seam and the fan-out
`PostCommitMeta` shape. That is not the shape of this sub-slice and
would be its own decision doc.

Diff surface:
- `subscription/eval.go::EvalAndBroadcast` — new contract comment
  naming the executor seam and the pin tests.

## Pinned by

Two focused tests in `subscription/eval_view_lifetime_test.go`:

- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join` — Join predicate
  exercises Tier-2 join-edge candidate probing (view.IndexSeek +
  view.GetRow in `collectCandidatesInto`) and join delta evaluation
  (view.IndexSeek via `delta_join.go`). After `EvalAndBroadcast`
  returns, `view.Close()` is called, the fan-out inbox is drained, and
  the tracker is asserted to have recorded zero post-close calls. The
  test also asserts the tracker observed at least one call during
  evaluation so the instrument is not vacuously passing.
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable` — single-
  table `ColEq` predicate exercises the non-join eval path
  (`EvalSingleTableDelta` via `evalQuery`). Same post-close assertion.

Both pass with current code. Either fails immediately if a future
change lets any reference to the view escape past `EvalAndBroadcast`
return: a goroutine spawned from inside `evaluate` that re-reads the
view, a view pointer stashed into `FanOutMessage`, a per-query cache
that holds the view past the synchronous call — any of those produce a
non-zero `callsAfter` counter and the test fails.

Already-landed OI-005 / OI-006 pins unchanged and still passing:
- `TestCommittedSnapshotTableScanPanicsOnMidIterClose`
- `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`
- `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose`
- `TestCommittedSnapshotTableScanPanicsAfterClose`
- `TestCommittedSnapshotIndexScanPanicsAfterClose`
- `TestCommittedSnapshotIndexRangePanicsAfterClose`
- `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`
- `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`
- `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`

## Remaining OI-005 sub-hazards

Still open:
- `store/state_view.go` / `store/committed_state.go` shared-state
  escape routes — direct-method read paths bypass the snapshot
  construct / Close envelope and may leak references into callers that
  outlive the short synchronous seam. Each exported method is its own
  potential narrow sub-slice.

OI-005 stays open for those. The narrow sub-hazard closed here is the
subscription-seam read-view-lifetime contract between
`executor/executor.go:540-541` and
`subscription/eval.go::EvalAndBroadcast`.

## Authoritative artifacts

- This document.
- `subscription/eval.go::EvalAndBroadcast` — contract comment.
- `subscription/eval_view_lifetime_test.go` — new focused tests.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
