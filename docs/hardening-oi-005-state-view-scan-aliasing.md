# OI-005 — `StateView.ScanTable` iterator surface (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-21.

Follows the shape of the prior OI-005 sub-slices
(`docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`,
`docs/hardening-oi-005-state-view-seekindex-aliasing.md`,
`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`,
`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`,
`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-005-snapshot-iter-useafterclose.md`,
`docs/hardening-oi-005-snapshot-iter-retention.md`).

## Sharp edge

`store/state_view.go::StateView.ScanTable` drives its yield loop
from `Table.Scan()`:

```go
return func(yield func(types.RowID, types.ProductValue) bool) {
    if sv.committed != nil {
        if table, ok := sv.committed.Table(tableID); ok {
            for id, row := range table.Scan() {
                if sv.tx.IsDeleted(tableID, id) {
                    continue
                }
                if !yield(id, row) {
                    return
                }
            }
        }
    }
    for id, row := range sv.tx.Inserts(tableID) {
        ...
    }
}
```

`Table.Scan()` is an `iter.Seq2` that ranges `t.rows` live:

```go
func (t *Table) Scan() iter.Seq2[types.RowID, types.ProductValue] {
    return func(yield func(types.RowID, types.ProductValue) bool) {
        for id, row := range t.rows {
            if !yield(id, row.Copy()) {
                return
            }
        }
    }
}
```

`t.rows` is the concrete `map[types.RowID]types.ProductValue`. The outer
`range t.rows` loop reads map state every step — its window therefore
spans the full `StateView.ScanTable` yield loop. Row payloads are
already `Copy()`d at yield time, so payload isolation exists, but the
outer map iteration does not.

Under executor single-writer discipline no concurrent writer runs
during a reducer's synchronous iteration, so today's pattern is safe.
But the contract — "no mid-iter mutation of the underlying `t.rows`
map" — was load-bearing and unpinned at the `StateView` boundary. A
yield callback that reaches a path mutating `t.rows` (future refactor,
direct `CommittedState` access from a reducer, a new narrow API that
borrows the view for a follow-on mutation), or a caller that retained
the iterator past the executor single-writer window, would race the
live map iteration.

Go spec §6.3 pins the observable failure mode: *"If a map entry that
has not yet been reached is removed during iteration, the corresponding
iteration value will not be produced."* The drift is the iteration
silently skipping rows that were present at iter-construction time —
hard to catch at review time, deterministic per the Go spec, and at
odds with the "stable view within the single-writer window" mental
model of every caller.

This is the `Table.Scan` analog of the `StateView.SeekIndex` /
`StateView.SeekIndexRange` fixes. Those closed the same class at the
BTree boundary by cloning / collecting the slice / `iter.Seq`; this
slice closes the `Table.Scan` analog by collecting the (RowID,
ProductValue) pairs up front.

## Fix

Narrow and pin. Two-part change:

1. `store/state_view.go::StateView.ScanTable` — collect the committed
   scan into an `[]entry{id, row}` slice pre-sized via
   `table.RowCount()` before entering the yield loop. The yield loop
   then iterates the materialized slice, so a mid-iter mutation of
   `t.rows` (direct delete, insert, replace) cannot drift the outer
   iteration. Contract comment on `StateView.ScanTable` names the pin
   test.
2. `store/state_view_scan_aliasing_test.go` — new file, one focused
   test that drives a contract-violating scenario directly: register a
   primary-key index on a single `uint64` column, insert five rows
   with distinct IDs, iterate via `sv.ScanTable(0)`, and at the first
   yield body reach into `tbl.DeleteRow(notYieldedID)`. Before this fix
   the live map iteration observed the Go runtime's deletion of the
   unreached entry and yielded only four RowIDs. With the pre-collect
   materialization iteration yields all five pre-iter RowIDs. The test
   asserts the full set regardless of the in-flight committed
   mutation.

The `IsDeleted` filter in `StateView.ScanTable` is deliberately left
on the tx-side: the test calls `Table.DeleteRow` directly on the
committed table (not `sv.tx.Delete`), so the tx-state has no delete
marker for the removed RowID and the materialized entry is still
yielded. This is what makes the drift observable — a test that deleted
via `sv.tx.Delete` would have the `IsDeleted` filter mask the outer-
loop skip under the aliased path.

## Scope / limits

- Closes the narrow sub-hazard at the `StateView.ScanTable` boundary
  only. `Table.Scan()` stays as it is; its `iter.Seq2` still ranges
  `t.rows` live — the contract there is unchanged, and callers are
  responsible for their own materialization. `CommittedSnapshot.TableScan`
  already holds the `CommittedState` RLock for the iter's lifetime
  (with `runtime.KeepAlive` + `ensureOpen` per OI-005 iter-retention /
  use-after-Close / mid-iter-close sub-slices) and so does not need
  separate materialization.
- Does not change the `Table` API. Callers that hold a raw `*Table`
  (e.g. through `CommittedState.Table(id) *Table`) still see the live
  `Table.Scan()` semantics; the narrow sub-hazard for that raw-pointer
  escape remains open under OI-005.
- Performance: one `[]entry` allocation per `ScanTable` call, pre-sized
  to `table.RowCount()`. Peak memory is O(n) rows — equivalent to
  `slices.Collect(Table.Scan())`, since `Table.Scan` already `Copy()`s
  each row payload before yielding. Current callers (`Transaction.Scan`
  at `store/transaction.go:299`, tests in `store/state_view_test.go`)
  already walk the iterator to completion inside a bounded reducer
  window, so the transient peak does not outlive the synchronous call.
  If profiling later surfaces a hot caller that wants streaming
  semantics inside the single-writer window, a per-caller override
  (e.g. a borrow-the-iterator variant guarded by a contract comment)
  can be added without disturbing the current pin.

Diff surface:
- `store/state_view.go` — materialization + contract comment on
  `ScanTable`.
- `store/state_view_scan_aliasing_test.go` — new focused pin test.

## Pinned by

One focused test in `store/state_view_scan_aliasing_test.go`:

- `TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete`
  — fails if the materialization is removed and the yield loop is
  switched back to the aliased live `Table.Scan()` range. Pre-fix
  observation: four RowIDs yielded (Go spec: unreached-entry deletion
  during map iteration not produced); post-fix observation: all five
  pre-iter RowIDs yielded, exact IDs compared against the insertion set
  to guard against any re-ordering artifact. Passes under
  `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing:
- `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`
- `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`
- `TestDispatchLoop_HandlerCtxCancelsOnConnClose`
- `TestDispatchLoop_HandlerCtxCancelsOnOuterCtx`
- `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs`
- `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneAlreadyClosed`
- `TestCloseAllBoundsDisconnectOnInboxHang`
- `TestCloseAllDeliversOnInboxOK`
- `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`
- `TestSuperviseLifecycleDeliversOnInboxOK`
- `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`
- `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`
- `TestWatchReducerResponseExitsOnConnClose`
- `TestWatchReducerResponseDeliversOnRespCh`
- `TestWatchReducerResponseExitsOnRespChClose`
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

## Remaining OI-005 sub-hazards

Still open:
- `CommittedState.Table(id) *Table` raw-pointer escape — the returned
  `*Table` is accessed outside the `CommittedState` RLock envelope by
  every caller. Under single-writer discipline this is safe, but the
  contract is unpinned. A narrow sub-slice: either wrap the return in
  a re-checking interface or add a contract-pin test following the
  subscription-seam precedent.

OI-005 stays open for that. The narrow sub-hazard closed here is the
`StateView.ScanTable` iterator surface only — the last remaining
`StateView` iter-surface escape route is now pinned alongside
`SeekIndex` and `SeekIndexRange`.

## Authoritative artifacts

- This document.
- `store/state_view.go::StateView.ScanTable` — materialization fix and
  contract comment.
- `store/state_view_scan_aliasing_test.go` — new focused pin test.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin anchor.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
