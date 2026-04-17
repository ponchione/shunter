# Story 5.5: Transaction.Delete

**Epic:** [Epic 5 — Transaction Layer](EPIC.md)  
**Spec ref:** SPEC-001 §5.5  
**Depends on:** Story 5.3  
**Blocks:** Story 5.6

---

## Summary

Delete a row visible in the transaction view. Branches on whether the row is tx-local or committed.

## Deliverables

- `func (t *Transaction) Delete(tableID TableID, rowID RowID) error`

  **Algorithm:**
  0. Look up table by `TableID` via `committed.Table(tableID)`. If not found, return `ErrTableNotFound`.
  1. If rowID is in `tx.inserts[tableID]`:
     - Remove from tx.inserts (insert-then-delete collapses to no-op)
     - Return nil
  2. If rowID is in `tx.deletes[tableID]`:
     - Already deleted → return `ErrRowNotFound`
  3. If rowID exists in committed table:
     - Add to tx.deletes
     - Return nil
  4. Otherwise → return `ErrRowNotFound`

- New error type: `ErrRowNotFound` — sentinel error

## Acceptance Criteria

- [ ] Delete committed row → added to tx.deletes, no longer visible via StateView
- [ ] Delete tx-local row → removed from tx.inserts, no trace remains
- [ ] Delete already-deleted committed row → `ErrRowNotFound`
- [ ] Delete non-existent RowID → `ErrRowNotFound`
- [ ] Delete with unknown TableID → `ErrTableNotFound`
- [ ] After deleting tx-local row: RowID not in inserts or deletes
- [ ] After deleting committed row: row still in committed state (unchanged), just hidden via tx.deletes
- [ ] Insert then delete same row in TX → ScanTable shows neither

## Design Notes

- Delete of tx-local row is a true removal (from inserts map). Delete of committed row is a hide (added to deletes set). This distinction matters for changeset production in Epic 6.
- `ErrRowNotFound` completes the error catalog from SPEC-001 §9 (all 9 errors now have a home).
