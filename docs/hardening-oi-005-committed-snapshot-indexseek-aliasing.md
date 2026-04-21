# OI-005 — CommittedSnapshot.IndexSeek BTree-alias escape (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-20.

Follows the same shape as the prior OI-005 / OI-006 sub-slices
(`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`,
`docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`,
`docs/hardening-oi-005-snapshot-iter-useafterclose.md`,
`docs/hardening-oi-005-snapshot-iter-retention.md`,
`docs/hardening-oi-006-fanout-aliasing.md`).

## Sharp edge

`store/snapshot.go::CommittedSnapshot.IndexSeek` is the public
`CommittedReadView` entry point subscribers use to resolve join-edge
and register-set candidate RowIDs. Its body read:

```go
func (s *CommittedSnapshot) IndexSeek(...) []types.RowID {
    s.ensureOpen()
    _, idx, ok := s.lookupIndex(tableID, indexID)
    if !ok {
        return nil
    }
    return idx.Seek(key)
}
```

`idx.Seek(key)` forwards to `BTreeIndex.Seek`, which returns
`b.entries[idx].rowIDs` — a **live alias** of the index entry's
internal `[]types.RowID` backing array.

Contract envelope: the snapshot holds an RLock on `CommittedState`.
While the snapshot is open, no writer can mutate the index. After
`Close()`, the RLock is released; a subsequent writer on the same key
calls `slices.Insert` / `slices.Delete` on `e.rowIDs`, which either:

- mutates the backing array in place (capacity headroom case for
  `slices.Insert`; always the case for `slices.Delete`'s shift), or
- reallocates to a new backing array (capacity-overflow `slices.Insert`),
  leaving any aliased header stale.

A caller that retained the returned slice past `Close()` therefore
either observes writer mutations (in-place case) or reads a
silently-stale view (reallocate case). Under concurrent access this is
an unsynchronized read racing a writer's in-place mutation — the same
class of `CommittedState`-internal-storage escape the iter-surface
OI-005 sub-slices pinned inside `store/snapshot.go`, viewed one layer
over at the index-seek surface.

Current callers do not retain the slice past the iter loop:
- `subscription/delta_view.go:165` → `delta_join.go:85`, `:122` (join
  delta evaluation)
- `subscription/eval.go:286` (Tier-2 join-edge candidate probing)
- `subscription/register_set.go:92`, `:117` (initial-query join
  evaluation)
- `subscription/placement.go:162` (Tier-2 placement indexing)

All use the pattern `for _, rid := range view.IndexSeek(...)` and
finish synchronously within the borrowed view's lifetime. The contract
holds today, but it was load-bearing and unpinned: nothing in code
prevented a future caller from stashing the slice into a per-subscriber
struct, handing it to a spawned goroutine, or returning it through the
fan-out seam.

## Fix

Narrow and pin. Two-part change, zero behavior change for current
callers (the cloned slice has the same length, contents, and ordering
the alias did for the synchronous-use pattern):

1. `store/snapshot.go::CommittedSnapshot.IndexSeek` now `slices.Clone`s
   the `BTreeIndex`-internal slice before returning, with a contract
   comment naming the OI-005 escape-route rationale and the pin tests.
   An early `len == 0` return avoids allocating a zero-length clone.
2. Instrument tests: `store/snapshot_indexseek_aliasing_test.go`
   exercises both writer-mutation shapes that would otherwise reach the
   aliased backing — `slices.Insert` (append at same key) and
   `slices.Delete` (remove at same key). Each test seeds a non-unique
   secondary index with multiple RowIDs per key, snapshots, calls
   `IndexSeek`, `Close()`s, mutates the table via the writer, and
   asserts the previously returned slice's length and contents did not
   drift. Both tests also sanity-check that the writer's mutation did
   in fact land by taking a fresh snapshot.

No change to any caller. No change to `CommittedReadView` interface
shape. No change to `BTreeIndex.Seek` or `Index.Seek` — those stay as
the internal fast-path; the copy lives at the public read-view
boundary where the escape risk crosses out of `store/`.

Copy cost: O(n) where n = distinct RowIDs at the matched key. For the
unique-index cases (primary key, unique secondary), n ≤ 1. For
non-unique indexes, n is bounded by the cardinality of that key; in
the reference workload shape it is typically small.

## Scope / limits

- Closes the narrow sub-hazard at the `CommittedSnapshot.IndexSeek`
  boundary only. It does not touch `BTreeIndex.Seek` or `Index.Seek`,
  which remain internal alias-returning fast paths. If a future caller
  outside `store/` reaches them, that caller must either hold the
  RLock for the full use window or copy itself.
- Does not close the `CommittedState.Table(id) *Table` escape route —
  that returns a pointer to a committed `*Table` whose maps/indexes are
  mutated only under the `CommittedState` write lock; the snapshot
  envelope enforces the lock discipline, but the raw pointer itself is
  a separate surface that will need its own narrow slice if widened
  beyond current internal callers.
- Does not touch `TxState.Inserts` / `Deletes` / `AllInserts` /
  `AllDeletes`, which return internal maps uncopied but are
  transaction-local (single-writer discipline, not a
  shared-state-across-threads hazard).
- Does not change the iter-surface OI-005 pins already closed
  (`TableScan` / `IndexScan` / `IndexRange` mid-iter-close,
  use-after-Close, GC retention).

## Pinned by

Two focused tests in `store/snapshot_indexseek_aliasing_test.go`:

- `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert`
  — seeds two rows under key `red` on a non-unique secondary index,
  snapshots, `IndexSeek`s, `Close()`s, writer appends a third row at
  `red`, asserts the previously returned slice length and elements
  are unchanged.
- `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove`
  — seeds three rows under key `blue`, snapshots, `IndexSeek`s,
  `Close()`s, writer removes the middle row (forcing
  `slices.Delete`'s in-place shift), asserts the returned slice is
  unchanged. This case is the strongest: `slices.Delete` always
  mutates the backing in place, so an aliased header would always
  observe the shift.

Both run green under `-race -count=3`. Either fails immediately if a
future change reverts the `slices.Clone` at the public boundary or
otherwise re-introduces an alias path.

Already-landed OI-005 / OI-006 pins unchanged and still passing:
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
- `store/state_view.go` / `store/committed_state.go` shared-state
  escape routes beyond `IndexSeek`:
  - `CommittedState.Table(id) *Table` returns a raw table pointer.
    Safe today because the snapshot envelope holds the RLock for the
    use window, but the pointer itself outlives the lock envelope for
    any caller that keeps it.
  - `StateView.ScanTable` / `SeekIndex` / `SeekIndexRange` yield rows
    and RowIDs via `iter.Seq2`/`iter.Seq`. `StateView` is used inside
    reducer execution under the executor's single-writer discipline,
    but the shared-state sub-layer (committed tables through
    `sv.committed.Table`) is reached without an RLock.

Each remaining escape is its own potential narrow sub-slice.

## Authoritative artifacts

- This document.
- `store/snapshot.go::CommittedSnapshot.IndexSeek` — slice clone +
  contract comment.
- `store/snapshot_indexseek_aliasing_test.go` — new focused tests.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
