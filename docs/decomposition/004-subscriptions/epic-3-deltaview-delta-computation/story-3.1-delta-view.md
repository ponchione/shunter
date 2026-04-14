# Story 3.1: DeltaView & Delta Indexes

**Epic:** [Epic 3 — DeltaView & Delta Computation](EPIC.md)
**Spec ref:** SPEC-004 §6.4, §10.3
**Depends on:** Epic 1 (Predicate types), SPEC-001 (CommittedReadView), SPEC-003/SPEC-001 (`*Changeset` commit output)
**Blocks:** Stories 3.2, 3.3, 3.5

---

## Summary

Wraps committed state + transaction deltas into a unified data source. Builds temporary indexes over delta rows so fragments like `dT1(+) join T2'` can use index lookups on the delta side.

## Deliverables

- `DeltaView` struct:
  ```go
  type DeltaView struct {
      committed  CommittedReadView
      inserts    map[TableID][]ProductValue
      deletes    map[TableID][]ProductValue
      deltaIdx   DeltaIndexes
  }
  ```

- `DeltaIndexes` struct:
  ```go
  type DeltaIndexes struct {
      insertIdx map[TableID]map[IndexID]*btree.Map[Value, []int]
      deleteIdx map[TableID]map[IndexID]*btree.Map[Value, []int]
  }
  ```
  Maps value → positions in the corresponding `inserts`/`deletes` slice.

- `NewDeltaView(committed CommittedReadView, changeset *Changeset, activeIndexes map[TableID][]IndexID) *DeltaView`
  - Copies insert/delete slices from changeset
  - Builds delta indexes only for columns in `activeIndexes` (columns referenced by at least one active subscription)
  - Eager construction: once per transaction, not per subscription

- Access methods:
  - `InsertedRows(table TableID) []ProductValue`
  - `DeletedRows(table TableID) []ProductValue`
  - `DeltaIndexScan(table TableID, indexID IndexID, value Value, inserted bool) []ProductValue` — lookup delta rows by indexed value
  - `CommittedScan(table TableID) RowIterator` — delegate to committed view
  - `CommittedIndexScan(table TableID, indexID IndexID, value Value) RowIterator`

## Acceptance Criteria

- [ ] Construct DeltaView from changeset → inserts/deletes accessible per table
- [ ] Delta index built for specified columns only
- [ ] Delta index not built for unspecified columns
- [ ] `DeltaIndexScan` returns correct rows matching value
- [ ] `DeltaIndexScan` on non-indexed column → panic (caller bug)
- [ ] Empty changeset for a table → empty slices, no delta indexes
- [ ] Committed access methods delegate correctly
- [ ] Benchmark: delta index construction < 1 ms for 100 rows × 3 indexed columns

## Design Notes

- `activeIndexes` is computed once per evaluation cycle from the set of active subscriptions. This avoids building indexes for columns no subscription cares about.
- Delta index values map to positions (int slices) rather than copying rows. This avoids double-storing ProductValue data.
- DeltaView does not own the committed view — caller (evaluation loop) manages its lifecycle.
- SPEC-001's `CommittedReadView` must be closed by the caller before any blocking work. DeltaView may borrow it for in-process evaluation only; it must not extend the snapshot lifetime into fan-out or channel waits.
