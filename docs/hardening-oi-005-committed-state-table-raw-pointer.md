# OI-005 — CommittedState.Table(id) raw-pointer contract pin (Tier-B hardening)

Records the narrow Tier-B hardening sub-slice of `TECH-DEBT.md` OI-005
(snapshot / committed-read-view lifetime rules) landed 2026-04-21.

Closes the last enumerated OI-005 sub-hazard: `CommittedState.Table(id)
*Table` raw-pointer exposure. All eight previously enumerated OI-005
sub-hazards (iter-retention, iter use-after-Close, iter mid-iter-close,
subscription-seam read-view lifetime, `CommittedSnapshot.IndexSeek`
BTree-alias, `StateView.SeekIndex` BTree-alias, `StateView.SeekIndexRange`
BTree-alias, `StateView.ScanTable` iter surface) plus this one are now
pinned.

Follows the contract-pin shape of the earlier OI-005 slice
`docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`, which
landed the subscription-seam read-view lifetime pin without a production
code change. No production-code semantic change in this slice either.

## Sharp edge

`store/committed_state.go::CommittedState.Table(id) (*Table, bool)`
acquires `cs.mu.RLock()`, reads the `cs.tables` map, releases the RLock,
and returns a raw `*Table` pointer. The RLock bounds only the map
lookup, not the lifetime of the returned pointer. Callers use the
pointer — including mutating calls such as `AllocRowID`,
`InsertRow`, `DeleteRow`, and sequence `Next` via `applyAutoIncrement`
— after the RLock is released.

Under the existing executor single-writer discipline and the
`CommittedSnapshot` open→Close RLock-lifetime model, every current
caller stays inside a safe envelope. But the envelope rule was
unwritten. Concretely, three legal envelopes existed without being
documented:

1. `CommittedSnapshot` — `store/snapshot.go:69,169,200,209`. The
   snapshot acquires `cs.RLock()` in `Snapshot()` and releases it in
   `close()`; the snapshot's open→Close lifetime bounds every method
   call on the returned `*Table`. The three iterator surfaces
   additionally `runtime.KeepAlive(s)` the snapshot so the RLock is
   not released mid-iteration (the OI-005 iter-retention /
   use-after-Close / mid-iter-close pins already enforce this).
2. `Transaction` / `StateView` — `store/transaction.go:103,218,229,256`
   and `store/state_view.go:40,69,185`. The reducer runs on the single
   executor goroutine under single-writer discipline
   (`executor/executor.go`). No concurrent writer runs during a
   reducer's synchronous window.
3. Commitlog recovery bootstrap — `commitlog/recovery.go:83,95,102,113`.
   Runs on a single goroutine before any reader attaches.

Three hazards the envelope rule prevents but that were never asserted:

- **Escape past envelope**: a caller that stashed `*Table` into a
  goroutine running after snapshot `Close()` or past reducer return
  would race future writers on `t.rows` / `t.indexes` / `t.sequence`.
- **Stale-after-re-register**: a caller that retained `*Table` across a
  subsequent `RegisterTable(id, replacement)` would hold a pointer
  that no longer tracks the committed table-of-record. Future callers
  who believed retention was safe would silently observe a divergent
  pointer.
- **Non-executor-goroutine read without RLock**: a caller on a goroutine
  other than the executor's single-writer goroutine, reading via the
  pointer without holding `cs.RLock()`, would race any in-progress
  reducer write.

Today's callers (`store/snapshot.go`, `store/transaction.go`,
`store/state_view.go`, `commitlog/recovery.go`) stay inside one of the
three envelopes. But the contract was load-bearing and unpinned: a
future change that plumbed the pointer into a goroutine-owned scope,
stored it in per-client state, or read it from a non-executor goroutine
without RLock would silently violate the safety model — hard to catch
at review time.

## Fix

Narrow contract pin, no production-code semantic change:

1. Contract comment on `CommittedState.Table(id)` enumerating the three
   legal envelopes, the three hazards the envelope rule prevents, and a
   pointer to the current in-tree callers that stay inside the envelope.
2. Contract comment on `CommittedState.TableIDs()` naming the same
   envelope rule (TableIDs returns a bare slice; subsequent per-id
   `Table()` lookups must also be inside a legal envelope).
3. Pin tests in `store/committed_state_table_contract_test.go` that
   assert the two observable invariants making the contract auditable:
   - `TestCommittedStateTableSameEnvelopeReturnsSamePointer` —
     repeated `cs.Table(id)` calls in a single envelope return the
     same `*Table` identity. Pins pointer-identity stability so a
     future change that introduced copying or wrapping at the lookup
     boundary is visible.
   - `TestCommittedStateTableRetainedPointerIsStaleAfterReRegister` —
     a pointer retained across `RegisterTable(id, replacement)` does
     not track writes committed via the replacement. Pins the
     stale-after-re-register hazard shape: an
     `InsertRow(AllocRowID(), {…})` on the replacement leaves
     `retained.RowCount() == 0`, demonstrating that retention across
     re-register is unsafe without an envelope-level guard.
   - `TestCommittedStateTableSnapshotEnvelopeHoldsRLockUntilClose` —
     while a `CommittedSnapshot` is open, a writer attempting
     `cs.Lock()` blocks; after `snap.Close()` the writer proceeds.
     Pins the first of the three envelopes at the lock level so a
     future change that broke the snapshot RLock lifetime (e.g.,
     `Close()` stopped releasing the RLock, or `Snapshot()` stopped
     acquiring it) would fail this test deterministically.

No production code path changes. The contract held before this slice;
the slice makes the envelope rule asserted and visible to future
changes.

## Scope / limits

This is a contract pin, not a lifetime enforcement mechanism:

- The pins document the envelope-rule invariants observationally. They
  do not prevent a future change from stashing `*Table` into a
  goroutine that outlives the envelope or into per-subscriber state.
- Enforcement would require either a narrower interface wrapper that
  re-checks snapshot openness on every access (option (a) in the
  `NEXT_SESSION_HANDOFF.md` analysis) or a generation-counter
  invalidation model on `*Table` itself. Both are broader than a
  narrow sub-slice and would be separate decision docs.
- The single-writer-goroutine contract between executor and store
  remains the fundamental invariant under which the envelope rule is
  sound. Breaking single-writer discipline would re-open every raw
  `*Table` access site even with this pin.

Diff surface:
- `store/committed_state.go::CommittedState.Table` — new contract
  comment.
- `store/committed_state.go::CommittedState.TableIDs` — new contract
  comment.
- `store/committed_state_table_contract_test.go` — three new focused
  tests.

## Pinned by

Three focused tests in `store/committed_state_table_contract_test.go`:

- `TestCommittedStateTableSameEnvelopeReturnsSamePointer` — repeated
  `Table(id)` returns identical `*Table`; fails if a future refactor
  wrapped / copied at the lookup boundary and broke identity within a
  single envelope.
- `TestCommittedStateTableRetainedPointerIsStaleAfterReRegister` —
  retained pointer does not track writes on the replacement after
  `RegisterTable` swap; fails if a future change added a forwarding
  layer that made retained pointers auto-follow the re-register.
- `TestCommittedStateTableSnapshotEnvelopeHoldsRLockUntilClose` — a
  writer's `cs.Lock()` blocks for the `CommittedSnapshot`'s open
  lifetime and proceeds after `Close()`; fails if the snapshot
  envelope stopped holding the RLock at the expected boundaries.

All three pass under `-race -count=3`.

Already-landed OI-005 / OI-006 pins unchanged and still passing:
- `TestCommittedSnapshotTableScanPanicsOnMidIterClose`
- `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`
- `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose`
- `TestCommittedSnapshotTableScanPanicsAfterClose`
- `TestCommittedSnapshotIndexScanPanicsAfterClose`
- `TestCommittedSnapshotIndexRangePanicsAfterClose`
- `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`
- `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert`
- `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove`
- `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`
- `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`
- `TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete`
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join`
- `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable`
- `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`
- `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`

## Remaining OI-005 sub-hazards

None enumerated. This slice closes the last entry on the OI-005
remaining sub-hazards list. OI-005 remains open as a theme because the
envelope rule is enforced only by discipline and observational pins;
promoting to machine-enforced lifetime would be its own decision doc
(see Scope / limits).

## Authoritative artifacts

- This document.
- `store/committed_state.go::CommittedState.Table` — contract comment.
- `store/committed_state.go::CommittedState.TableIDs` — contract
  comment.
- `store/committed_state_table_contract_test.go` — three new focused
  tests.
- `TECH-DEBT.md` — OI-005 updated with this sub-hazard closed; the
  remaining-sub-hazards list is now empty for OI-005.
- `docs/current-status.md` — hardening / correctness bullet refreshed.
- `NEXT_SESSION_HANDOFF.md` — updated to reflect new baseline.
