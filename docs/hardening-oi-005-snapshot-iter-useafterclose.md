# OI-005 — snapshot iterator use-after-Close (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / read-view lifetime rules) landed 2026-04-20.

Follows the same shape as the prior OI-005 iter-retention slice
(`docs/hardening-oi-005-snapshot-iter-retention.md`).

## Sharp edge

`store/snapshot.go` exposes three iterator entry points on
`*CommittedSnapshot`: `TableScan`, `IndexScan`, and `IndexRange`. Each
calls `s.ensureOpen()` at iter construction and then returns an
`iter.Seq2` closure. Before this slice, the returned closure did **not**
re-check the closed state at iter-body entry.

Consequence: a caller that calls `Close()` between iter construction and
iterator consumption would silently race the already-freed RLock. The
sequence is:

```go
snap := cs.Snapshot()       // RLock held
it := snap.TableScan(0)     // ensureOpen() ok; closure captures t.Scan()
snap.Close()                // closed = true; RUnlock fires
for rid, row := range it {  // iterates t.rows map with NO lock held
    // concurrent writer can acquire Lock() and mutate t.rows here
}
```

The `CommittedSnapshot.close` path sets `closed = true` under
`closeOnce.Do` and releases the RLock. A concurrent writer can then
acquire the write lock and mutate `Table.rows` (a Go `map`) while the
range body is still dereferencing it — a `concurrent map read / write`
race, not just a soft correctness concern.

The prior iter-retention slice fixed the GC-finalizer variant of this
hazard via `defer runtime.KeepAlive(s)`. That fix prevents the
finalizer from releasing the RLock under an in-flight iter, but it does
not protect against an **explicit** `Close()` call by a misbehaving
caller.

## Fix

Each of the three iterator bodies now calls `s.ensureOpen()` at iter
body entry, immediately after `defer runtime.KeepAlive(s)`. The
construction-time `ensureOpen()` check is retained. The body-entry
check converts the sequential mis-use pattern (construct → Close →
iterate) from a silent race on freed state into the same deterministic
`"store: CommittedSnapshot used after Close"` panic that the
construction-time check already produces.

Diff surface:
- `store/snapshot.go::TableScan` — adds `s.ensureOpen()` inside the
  returned iter body.
- `store/snapshot.go::IndexRange` — same.
- `store/snapshot.go::rowsFromRowIDs` — same (used by `IndexScan`).

Ordering note: the body check runs after `defer runtime.KeepAlive(s)`
so that if the check panics, the deferred `KeepAlive` still runs during
panic unwinding, keeping the snapshot reachable while the panic
propagates.

## Pinned by

Three focused tests in `store/snapshot_iter_useafterclose_test.go`,
one per iterator entry point. Each constructs the iter, calls
`snap.Close()`, then attempts to range over the iter and asserts the
exact `"store: CommittedSnapshot used after Close"` panic:

- `TestCommittedSnapshotTableScanPanicsAfterClose`
- `TestCommittedSnapshotIndexScanPanicsAfterClose`
- `TestCommittedSnapshotIndexRangePanicsAfterClose`

The prior pin
`store/snapshot_iter_retention_test.go::TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`
continues to pass: the body-entry check is additive and does not
remove the KeepAlive retention fix.

## Scope

This slice closes one specific sub-hazard of `OI-005`
(sequential construct → explicit Close → iterate, which previously
produced a silent race). It does **not** close the broader cross-goroutine
snapshot sharing concern: a concurrent `Close()` called from a
different goroutine after the iter body has already entered can still
race internal data between the body-entry check and each yield.
Addressing that requires either per-iteration checks plus refcount
discipline or a broader redesign of the read-view lifetime model, and
belongs to a separate narrow slice.

Remaining `OI-005` sub-hazards (still open):
- cross-goroutine snapshot sharing / ownership rules
- long-held read-view lifetime hazards at the subscription/evaluator seam
- `state_view.go` / `committed_state.go` shared-state escape routes

## Authoritative artifacts

- This document.
- `store/snapshot.go` — fix surface.
- `store/snapshot_iter_useafterclose_test.go` — new focused tests.
- `TECH-DEBT.md` — OI-005 updated with sub-hazard closed + pin anchors.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
