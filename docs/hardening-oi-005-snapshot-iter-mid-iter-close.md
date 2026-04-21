# OI-005 — snapshot iter mid-iter Close (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-20.

Follows the same shape as the prior OI-005 / OI-006 sub-slices
(`docs/hardening-oi-005-snapshot-iter-useafterclose.md`,
`docs/hardening-oi-005-snapshot-iter-retention.md`,
`docs/hardening-oi-006-fanout-aliasing.md`).

## Sharp edge

`*CommittedSnapshot.TableScan` / `IndexScan` / `IndexRange` each check
`s.ensureOpen()` exactly once at iter-body entry. Shape:

```go
return func(yield func(types.RowID, types.ProductValue) bool) {
    defer runtime.KeepAlive(s)
    s.ensureOpen()                  // body-entry check only
    for rid, row := range inner {
        if !yield(rid, row) {
            return
        }
    }
}
```

The previously landed use-after-Close sub-slice added that body-entry
check (see `docs/hardening-oi-005-snapshot-iter-useafterclose.md`) and
pinned the sequential pattern `construct → Close → iterate`. But a
partially consumed iterator, whose owner yields once and then
experiences a `Close()` call (from the same goroutine in a caller
body, or from another goroutine holding a reference to the snapshot),
continues yielding subsequent rows with the RLock already released.
Any concurrent writer that acquires the write lock between `Close()`
and the next yield can mutate `t.rows` under the in-flight iter — a
map-concurrent-read-and-write data race under the race detector, and
silent inconsistent reads without it.

The "single-goroutine-ownership for the full iter lifetime" contract
was load-bearing but unpinned; nothing in code or tests asserted it,
and the body-entry check gave a false sense of safety for the
partial-iter-then-Close pattern.

## Fix

Each iter-body for-loop now re-calls `s.ensureOpen()` per-iteration,
before the row is yielded:

```go
return func(yield func(types.RowID, types.ProductValue) bool) {
    defer runtime.KeepAlive(s)
    s.ensureOpen()
    for rid, row := range inner {
        // OI-005 mid-iter-close defense-in-depth.
        s.ensureOpen()
        if !yield(rid, row) {
            return
        }
    }
}
```

Applied to the three iter surfaces on `*CommittedSnapshot`:
- `TableScan` (`store/snapshot.go::TableScan`)
- `IndexRange` (`store/snapshot.go::IndexRange`)
- `rowsFromRowIDs` (`store/snapshot.go::rowsFromRowIDs`, the shared
  body underneath `IndexScan`)

`IndexSeek`, `GetRow`, and `RowCount` return eagerly (not iters); their
existing single-entry `ensureOpen()` already covers them.

## Scope / limits

This is defense-in-depth, not a complete race fix:

- The per-iteration check narrows the race window but cannot close
  it. A `Close()` that races between the `ensureOpen()` check and an
  in-flight read of `t.rows` (during the `range inner` advance, or
  during `t.GetRow` inside the loop body) is still a data race at the
  machine level. The race detector will still flag the misuse.
- The contract pinned here is: if `Close()` has already returned
  before a given iteration step begins, the iter halts with the
  deterministic panic `"store: CommittedSnapshot used after Close"`
  rather than silently yielding one or more additional rows against
  a released lock.
- Callers are still expected to own the iter for its entire lifetime
  from a single goroutine, the same contract the prior OI-005
  sub-slices implicitly relied on.

Deepening into a mutex-guarded read + iter advance would change the
snapshot semantics and cost per-yield lock overhead for every
well-behaved caller. That is not the shape of this sub-slice.

Diff surface:
- `store/snapshot.go::TableScan` — per-iter `ensureOpen()` inside the
  `for rid, row := range inner` body.
- `store/snapshot.go::IndexRange` — per-iter `ensureOpen()` inside the
  `for rid := range idx.BTree().Scan()` body.
- `store/snapshot.go::rowsFromRowIDs` — per-iter `ensureOpen()` inside
  the `for _, rid := range rowIDs` body.

## Pinned by

Three focused tests in
`store/snapshot_iter_mid_iter_close_test.go`, one per iter surface.
Each seeds the stock players table with 3 rows, constructs an iter,
then on the first loop-body yield calls `snap.Close()` and
`continue`s. The following iteration must panic with the construction-time
contract message.

Tests:
- `TestCommittedSnapshotTableScanPanicsOnMidIterClose`
- `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`
- `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose` (covers
  the `IndexScan → rowsFromRowIDs` path by driving a collected
  multi-RowID slice directly through `rowsFromRowIDs`)

All three fail without the fix (the loop continues past `Close()`
yielding further rows and the `t.Fatal("iter continued yielding after
mid-iter Close")` fires), pass with it.

Already-landed OI-005 pins unchanged and still passing:
- `TestCommittedSnapshotTableScanPanicsAfterClose`
- `TestCommittedSnapshotIndexScanPanicsAfterClose`
- `TestCommittedSnapshotIndexRangePanicsAfterClose`
- `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`

## Remaining OI-005 sub-hazards

Still open:
- Long-held read-view lifetime hazards at the subscription/evaluator
  seam (`subscription/eval.go` retains a `CommittedReadView` for the
  duration of `evaluate`; contract is already held by current code
  — no goroutine outlives the synchronous call — but remains
  unpinned).
- `state_view.go` / `committed_state.go` shared-state escape routes.

OI-005 stays open for those. The narrow sub-hazard closed here is
the mid-iter-close behavior on the three iter entry points.

## Authoritative artifacts

- This document.
- `store/snapshot.go` — fix surface.
- `store/snapshot_iter_mid_iter_close_test.go` — new focused tests.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin
  anchors.
- `docs/current-status.md` — hardening / correctness bullet
  refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
