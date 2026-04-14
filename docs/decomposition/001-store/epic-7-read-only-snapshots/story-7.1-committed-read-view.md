# Story 7.1: CommittedReadView Interface + CommittedSnapshot

**Epic:** [Epic 7 — Read-Only Snapshots](EPIC.md)  
**Spec ref:** SPEC-001 §7.2  
**Depends on:** Epic 3 (Bound and index scan primitives), Epic 4 (Indexes), Story 5.1 (CommittedState), Story 5.3 (RowIterator)  
**Blocks:** Story 7.2

---

## Summary

Read-only point-in-time view of committed state. Used by subscription evaluator for initial state delivery.

## Deliverables

- `CommittedReadView` interface:
  ```go
  type CommittedReadView interface {
      Close()
      TableScan(tableID TableID) RowIterator
      IndexScan(tableID TableID, indexID IndexID, value Value) RowIterator
      IndexRange(tableID TableID, indexID IndexID, lower, upper Bound) RowIterator
      RowCount(tableID TableID) uint64
  }
  ```

- `CommittedSnapshot` struct (implements CommittedReadView):
  ```go
  type CommittedSnapshot struct {
      tables map[TableID]*Table   // shallow copy of table map at snapshot time
      mu     *sync.RWMutex        // held as read lock until Close()
  }
  ```

- `func (cs *CommittedState) Snapshot() CommittedReadView`
  - Acquires RLock on `cs.mu`
  - Shallow-copies table map
  - Returns CommittedSnapshot holding the lock

- `Close()` — releases RLock. Must be called exactly once.

- Snapshot usage contract owned by this story:
  - Callers must materialize any needed rows while the snapshot is open, then call `Close()` before network I/O, client encoding, waiting on channels, subscription-registration bookkeeping, or any other blocking work
  - The API docs for `CommittedReadView` and `CommittedState.Snapshot()` must state this explicitly because commit starvation prevention depends on caller behavior, not just lock mechanics

- `TableScan` — iterates all rows in committed table, undefined order

- `IndexScan` — point lookup on index, returns matching rows as (RowID, ProductValue) pairs
  - Lookup index by IndexID from table schema
  - Call Index.Seek, resolve RowIDs to rows

- `IndexRange` — range scan using Bound semantics
  - Call Index.SeekRange with Bound-derived low/high keys
  - Resolve RowIDs to rows

- `RowCount` — returns `len(table.rows)`

## Acceptance Criteria

- [ ] Snapshot TableScan returns all committed rows
- [ ] Snapshot IndexScan by PK returns correct row
- [ ] Snapshot IndexScan by non-existent value returns empty iterator
- [ ] Snapshot IndexRange returns rows in key order
- [ ] Snapshot IndexRange with unbounded lower returns everything up to upper
- [ ] Snapshot RowCount matches actual committed row count
- [ ] After Close(), snapshot methods panic or are otherwise not usable
- [ ] Snapshot sees state at time of Snapshot() call, not later mutations
- [ ] Public API docs for Snapshot/CommittedReadView explicitly forbid holding a snapshot across blocking or network work

## Design Notes

- Shallow copy of table map is sufficient because Table contents are only mutated under write lock (Commit). While a snapshot holds RLock, no Commit can proceed, so the Table pointers are stable.
- `IndexScan` takes a single `Value` (not IndexKey) because it's a convenience for single-column equality lookups. Internally wraps in IndexKey.
- `IndexRange` takes `Bound` (not `*IndexKey`) because callers need inclusive/exclusive control for subscription range predicates.
