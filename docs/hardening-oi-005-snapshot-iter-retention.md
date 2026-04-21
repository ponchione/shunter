# OI-005 — snapshot iterator GC retention (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / read-view lifetime rules) landed 2026-04-20.

## Sharp edge

`store/snapshot.go` exposes three iterator entry points on
`*CommittedSnapshot`: `TableScan`, `IndexScan`, and `IndexRange`.
Before this slice, each returned an `iter.Seq2` closure that captured
`*Table` but **not** `*CommittedSnapshot`. That meant a caller holding
only the iter (not the snapshot) could let the snapshot become
unreachable between iter construction and iter consumption.

`CommittedSnapshot` has a best-effort finalizer
(`finalizeCommittedSnapshot`, `snapshot.go:51-54`) that calls
`s.cs.RUnlock()`. If that fired during an in-flight `range` over the
iter, the RLock would be released mid-iteration and a concurrent writer
could mutate `Table.rows` (a Go `map`) while the range body was still
dereferencing it. That is a `concurrent map read / write` race, not
just a soft correctness concern.

Reproduction in test:

```go
snap := cs.Snapshot()
it := snap.TableScan(0)
snap = nil
for range 5 { runtime.GC() }
// concurrent writer attempts cs.Lock()
// without the fix, the write lock acquires immediately: finalizer
// fired, RLock is gone, iter body is about to race the writer
```

## Fix

Each iterator returned by `*CommittedSnapshot` now captures `s`
explicitly via `defer runtime.KeepAlive(s)` at the top of the iterator
body. The closure value therefore retains a reference to `s` for the
lifetime of the range body, so the finalizer cannot run until both the
iter variable and the range body are done.

Diff surface:
- `store/snapshot.go::TableScan` — wraps `t.Scan()` so the returned
  closure references `s`.
- `store/snapshot.go::IndexRange` — adds `defer runtime.KeepAlive(s)`
  to the existing inline closure.
- `store/snapshot.go::rowsFromRowIDs` — adds
  `defer runtime.KeepAlive(s)` to the inline closure (used by
  `IndexScan`).

## Pinned by

- `store/snapshot_iter_retention_test.go::TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`
  — constructs an iter, drops the snapshot reference, runs
  `runtime.GC()` repeatedly, asserts a concurrent `cs.Lock()` does not
  succeed while the iter is still held, then releases the iter and
  asserts the write lock is eventually acquired. Without the fix this
  test races the writer through immediately; with the fix the write
  lock blocks until the iter is released.

## Scope

This slice closes one specific sub-hazard of `OI-005` (premature
snapshot GC mid-iteration). `OI-005` stays open for the remaining
read-view lifetime concerns (use-after-Close, cross-goroutine snapshot
sharing, long-held read-view hazards at the subscription/evaluator
seam) which are broader than this fix and will need their own slices.

## Authoritative artifacts

- This document.
- `store/snapshot.go` — fix surface.
- `store/snapshot_iter_retention_test.go` — new focused test.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin anchor.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
