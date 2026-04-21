# OI-005 — `StateView.SeekIndexRange` BTree-alias escape route (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-20.

Follows the shape of the prior OI-005 sub-slices
(`docs/hardening-oi-005-state-view-seekindex-aliasing.md`,
`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`,
`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`,
`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-005-snapshot-iter-useafterclose.md`,
`docs/hardening-oi-005-snapshot-iter-retention.md`) and the protocol
OI-004 sub-slice
(`docs/hardening-oi-004-watch-reducer-response-lifecycle.md`).

## Sharp edge

`store/state_view.go::StateView.SeekIndexRange` drives its yield loop
from `BTreeIndex.SeekRange(low, high)`:

```go
return func(yield func(types.RowID) bool) {
    if sv.committed != nil {
        if table, idx, ok := sv.lookupIndex(tableID, indexID); ok {
            for rid := range idx.BTree().SeekRange(low, high) {
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

`BTreeIndex.SeekRange` is an `iter.Seq[types.RowID]` that walks
`b.entries` live:

```go
for i := startIdx; i < len(b.entries); i++ {
    e := b.entries[i]
    if high != nil && e.key.Compare(*high) >= 0 {
        return
    }
    for _, rid := range e.rowIDs {
        if !yield(rid) { return }
    }
}
```

`len(b.entries)` and `b.entries[i]` are read every step from the backing
array, and each entry's `rowIDs` slice is read live too. If a yield
callback reaches into the BTree and drops the last RowID of an entry
that sits behind the current cursor, `BTreeIndex.Remove` fires
`slices.Delete(b.entries, idx, idx+1)` which shifts the tail of
`b.entries` down in place inside the same backing array. The outer
loop's `i++` then skips over an entry that would otherwise have been
yielded next. The observable drift is the same shape as the
`StateView.SeekIndex` hazard closed earlier in the same day
(`docs/hardening-oi-005-state-view-seekindex-aliasing.md`), only at the
range boundary instead of the exact-key seek boundary.

Under executor single-writer discipline no concurrent writer runs
during a reducer's synchronous iteration, so today's pattern is safe.
But the yield callback itself could reach a path that mutates the BTree
(future refactor, direct `CommittedState` access from a reducer, a new
narrow API that borrows the view for a follow-on mutation). The contract
— "no mid-iter mutation of the underlying index" — was load-bearing but
unpinned.

This is the range-query analog of the `StateView.SeekIndex` fix. The
snapshot-layer `CommittedSnapshot.IndexSeek` fix closed the same class
at a different boundary
(`docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`) by
cloning the returned slice; the state-view `SeekIndex` fix did the same
for exact-key lookups; this slice closes the `iter.Seq` analog by
materializing the range up front.

## Fix

Narrow and pin. Two-part change:

1. `store/state_view.go::StateView.SeekIndexRange` — range over
   `slices.Collect(idx.BTree().SeekRange(low, high))` instead of the
   raw `iter.Seq`. `slices.Collect` exhausts the iterator once into a
   fresh `[]types.RowID`; iteration walks the independent copy, so a
   mid-iter BTree mutation cannot drift the outer cursor or the inner
   rowIDs slice. Contract comment on `StateView.SeekIndexRange` names
   the pin test.
2. `store/state_view_seekindexrange_aliasing_test.go` — new file, one
   focused test that drives a contract-violating scenario directly:
   register a primary-key index on a single `uint64` column, insert
   five rows with distinct IDs (one `b.entries` entry per row),
   iterate via `sv.SeekIndexRange(nil, nil)`, and at the first yield
   body reach into `idx.BTree().Remove(key_1, rid_1)`. Before this fix
   the iteration observed the `slices.Delete(b.entries, 0, 1)` shift
   and yielded rowIDs for keys `[1, 3, 4, 5]` (skipping key 2 — the
   entry that shifted into the cursor's old position). With the
   `slices.Collect` materialization iteration yields all five pre-iter
   rowIDs. The test asserts the full set regardless of the in-flight
   BTree mutation.

The `GetRow`/`IsDeleted` filters in `StateView.SeekIndexRange` are
deliberately bypassed: the test calls `idx.BTree().Remove` directly
(not `tbl.DeleteRow`), so the row stays in `Table.rows` and the
visibility filters emit every RowID the materialized range hands them.
This is what makes the drift observable — a test that also deleted the
row would have the `GetRow` filter mask the outer-loop skip under the
aliased path.

## Scope / limits

- Closes the narrow sub-hazard at the `StateView.SeekIndexRange`
  boundary only. `StateView.ScanTable` stays as it is; its iterator
  ranges over `Table.Scan()` which yields from `t.rows` (a map) and
  copies each row before yielding — no materialized `[]RowID` alias,
  but it still has the broader `sv.committed` / `sv.tx` lifetime
  concern enumerated under OI-005. A separate narrow sub-slice would
  pin the single-writer contract for `ScanTable` in the shape of
  `hardening-oi-005-subscription-seam-read-view-lifetime.md`.
- Does not change the `BTreeIndex` API. `BTreeIndex.SeekRange` still
  walks `b.entries` live — the contract there is unchanged, and
  callers are responsible for their own cloning / materialization (as
  `CommittedSnapshot.IndexSeek`, `StateView.SeekIndex`, and now
  `StateView.SeekIndexRange` all do).
- Does not change `CommittedState.Table(id) *Table` raw-pointer
  exposure. That remains an enumerated sub-hazard under OI-005.
- Performance: one `slices.Collect` allocation per `SeekIndexRange`
  call, sized to the matching RowID count. `SeekIndexRange` is not a
  hot path in current code (scheduler/scans use `ScanTable`;
  subscriptions evaluate via `DeltaView`). If profiling later surfaces
  a hot caller, a per-caller override (e.g. a
  borrow-the-iterator variant guarded by a single-writer contract
  comment) can be added without disturbing the current pin. Note that
  `slices.Collect` also forces early-exit (`yield → false`) to walk the
  full range up front; for current callers this is a no-op since they
  all drain.

Diff surface:
- `store/state_view.go` — `slices.Collect(idx.BTree().SeekRange(low, high))`,
  contract comment on `SeekIndexRange`.
- `store/state_view_seekindexrange_aliasing_test.go` — new focused pin
  test.

## Pinned by

One focused test in `store/state_view_seekindexrange_aliasing_test.go`:

- `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`
  — fails if `slices.Collect` is removed or the range is switched back
  to the aliased `iter.Seq`. Pre-fix observation: yielded rowIDs for
  keys `[1, 3, 4, 5]` (key 2 skipped by the shifted outer index);
  post-fix observation: all five pre-iter rowIDs. Passes under
  `-race -count=3`.

Already-landed OI-004 / OI-005 / OI-006 pins unchanged and still
passing:
- `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`
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
- `StateView.ScanTable` iterator surface — no materialized slice
  alias, but the closure captures `sv.committed` / `sv.tx` and yields
  rows without a `KeepAlive` / lock check. Could take a single-writer
  contract pin in the shape of
  `hardening-oi-005-subscription-seam-read-view-lifetime.md`.

OI-005 stays open for those. The narrow sub-hazard closed here is the
`StateView.SeekIndexRange` BTree-alias escape route only.

## Authoritative artifacts

- This document.
- `store/state_view.go::StateView.SeekIndexRange` — `slices.Collect`
  fix and contract comment.
- `store/state_view_seekindexrange_aliasing_test.go` — new focused pin
  test.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin anchor.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
