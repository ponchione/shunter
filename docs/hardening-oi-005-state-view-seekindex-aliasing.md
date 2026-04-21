# OI-005 — `StateView.SeekIndex` BTree-alias escape route (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-20.

Follows the same shape as the prior OI-005 sub-slices
(`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`,
`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`,
`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-005-snapshot-iter-useafterclose.md`,
`docs/hardening-oi-005-snapshot-iter-retention.md`) and the protocol
OI-004 sub-slice
(`docs/hardening-oi-004-watch-reducer-response-lifecycle.md`).

## Sharp edge

`store/state_view.go::StateView.SeekIndex` drives its yield loop from
the raw slice returned by `BTreeIndex.Seek(key)`:

```go
return func(yield func(types.RowID) bool) {
    if sv.committed != nil {
        if table, idx, ok := sv.lookupIndex(tableID, indexID); ok {
            for _, rid := range idx.Seek(key) {
                ...
                if !yield(rid) {
                    return
                }
            }
            ...
        }
    }
}
```

`BTreeIndex.Seek(key)` returns `b.entries[idx].rowIDs` — a live alias
of the entry's internal `[]RowID`. Go's `for _, v := range slc` captures
the slice header (ptr, len, cap) at loop init and indexes into the
backing array each iteration. If the backing array is mutated in place
during iteration — `slices.Delete` on a middle element shifts the tail
down inside the same backing and zeros the trailing slot — the
iteration reads the shifted values instead of the elements that were
present at seek time.

Under executor single-writer discipline no concurrent writer runs
during a reducer's synchronous iteration, so today's pattern is safe.
But the yield callback itself could reach a path that mutates the BTree
entry (future refactor, direct `CommittedState` access from a reducer,
a new narrow API that borrows the view for a follow-on mutation). The
contract — "no mid-iter mutation of the underlying index entry" — was
load-bearing but unpinned.

This is the `StateView`-layer analog of the `CommittedSnapshot.IndexSeek`
sub-hazard closed earlier in the same day
(`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`). The
snapshot boundary was hardened by cloning the result at the public
read-view exit; the state-view boundary had the same alias path at its
exit to the yield loop.

## Fix

Narrow and pin. Two-part change:

1. `store/state_view.go::StateView.SeekIndex` — range over
   `slices.Clone(idx.Seek(key))` instead of `idx.Seek(key)`. Clone is
   taken once at iterator-body entry; iteration walks the independent
   copy so any mid-iter BTree mutation on the source entry cannot drift
   the yielded RowIDs. `slices` is added to the imports. Contract
   comment on `StateView.SeekIndex` names the pin test.
2. `store/state_view_seekindex_aliasing_test.go` — new file, one
   focused test that drives a contract-violating scenario directly:
   insert five rows under a shared secondary-index key, iterate via
   `sv.SeekIndex`, and at the first yield body reach into
   `idx.BTree().Remove(key, middleRowID)`. Before this fix, the
   iteration observed the `slices.Delete` shift and yielded four
   drifted RowIDs with the middle one missing. With the clone,
   iteration yields all five pre-iter RowIDs. The test asserts the
   full set is yielded regardless of the in-flight BTree mutation.

The `GetRow`/`IsDeleted` filters in `StateView.SeekIndex` are deliberately
bypassed: the test calls `idx.BTree().Remove` directly (not
`tbl.DeleteRow`), so the row stays in `Table.rows` and the visibility
filters emit every RowID the clone hands them. This is what makes the
drift observable — a test that also deleted the row would have the
`GetRow` filter mask the shift under the aliased path.

`StateView.SeekIndexRange` is deliberately out of scope for this slice:
it does not touch a `Seek`-returned slice. It ranges over
`idx.BTree().SeekRange(low, high)`, which is already an `iter.Seq`
yielding RowIDs one at a time from `b.entries` — no materialized
`[]RowID` alias escapes into the iter body. A separate narrow sub-
slice would be required if a reference-backed hazard around the
`SeekRange` iter surfaces.

## Scope / limits

- Closes the narrow sub-hazard at the `StateView.SeekIndex` boundary
  only. `StateView.ScanTable` and `StateView.SeekIndexRange` stay as
  they are; each is its own potential sub-slice if workload or review
  evidence surfaces a specific alias-observation site.
- Does not change the BTreeIndex API. `BTreeIndex.Seek` still returns
  an aliased slice — the contract there is unchanged, and callers
  responsible for their own cloning (as `CommittedSnapshot.IndexSeek`
  and now `StateView.SeekIndex` both do).
- Does not change `CommittedState.Table(id) *Table` raw-pointer
  exposure or the `StateView` single-writer contract more broadly.
  Those remain enumerated sub-hazards under OI-005.
- Performance: one `slices.Clone` allocation per `SeekIndex` call.
  Cost is O(|matches|) for the secondary index key. `SeekIndex` is not
  a hot path in current code (scheduler/scans use `ScanTable`;
  subscriptions evaluate via `DeltaView`). If profiling later surfaces
  a hot SeekIndex caller, a per-caller override (e.g. a
  borrow-the-slice variant guarded by a single-writer contract
  comment) can be added without disturbing the current pin.

Diff surface:
- `store/state_view.go` — import `"slices"`, `slices.Clone(idx.Seek(key))`,
  contract comment on `SeekIndex`.
- `store/state_view_seekindex_aliasing_test.go` — new focused pin
  test.

## Pinned by

One focused test in `store/state_view_seekindex_aliasing_test.go`:

- `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation` —
  fails if `slices.Clone` is removed or the range is switched back to
  the aliased `idx.Seek(key)`. Observed vs. want would diverge by the
  middle RowID. Passes under `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing in the post-change full-suite run (`Go test: 1132 passed in 10
packages`):
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
- `StateView.ScanTable` / `StateView.SeekIndexRange` iterator surfaces
  — neither aliases a materialized slice, but both close over
  `sv.committed` / `sv.tx` and yield rows without a `KeepAlive` / lock
  check. Each could take a single-writer contract pin in the shape of
  `hardening-oi-005-subscription-seam-read-view-lifetime.md`.

OI-005 stays open for those. The narrow sub-hazard closed here is the
`StateView.SeekIndex` BTree-alias escape route only.

## Authoritative artifacts

- This document.
- `store/state_view.go::StateView.SeekIndex` — `slices.Clone` fix and
  contract comment.
- `store/state_view_seekindex_aliasing_test.go` — new focused pin
  test.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin anchor.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
