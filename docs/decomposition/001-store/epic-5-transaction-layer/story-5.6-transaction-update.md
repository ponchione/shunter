# Story 5.6: Transaction.Update

**Epic:** [Epic 5 — Transaction Layer](EPIC.md)  
**Spec ref:** SPEC-001 §5.5  
**Depends on:** Stories 5.4, 5.5  
**Blocks:** Epic 6

---

## Summary

Update = Delete(old) + Insert(new), with undelete optimization when new row is identical to a previously deleted committed row.

## Deliverables

- `func (t *Transaction) Update(tableID TableID, rowID RowID, newRow ProductValue) (RowID, error)`

  **Algorithm:**
  0. Look up table by `TableID` via `committed.Table(tableID)`. If not found, return `(0, ErrTableNotFound)`.
  1. Look up current row via StateView.GetRow — if not found, return `ErrRowNotFound`
  2. Delete(tableID, rowID) — remove old row
  3. Insert(tableID, newRow) — insert new row
  4. If Insert fails (constraint violation), must undo the Delete:
     - If old row was tx-local: re-add to tx.inserts
     - If old row was committed: remove from tx.deletes
  5. Return new RowID from Insert

  **Undelete case:** If newRow is identical to a committed row that was deleted earlier in the TX (including the one just deleted in step 2), Insert's undelete logic cancels that delete and returns the committed RowID.

## Acceptance Criteria

- [ ] Update committed row with new value → old in deletes, new in inserts, new RowID returned
- [ ] Update tx-local row with new value → old removed from inserts, new added, new RowID
- [ ] Update to identical value (committed row) → collapses to no-op (undelete cancels the delete)
- [ ] Update non-existent row → `ErrRowNotFound`, no state change
- [ ] Update with unknown TableID → `ErrTableNotFound`, no state change
- [ ] Update that would violate unique constraint → error, old row still visible (rollback)
- [ ] Update that changes PK value → old key freed, new key checked for uniqueness
- [ ] Update that changes non-indexed column → indexes unchanged for that column

## Design Notes

- Rollback on failed Insert is critical. If Delete succeeds but Insert fails, the row must be restored to its pre-Update state. Without rollback, a failed update silently deletes the row.
- The undelete optimization means "update row X to same value" is a no-op from the changeset perspective. This is correct behavior per spec §6.2.
- Update returns a NEW RowID (from Insert) unless undelete fires, in which case it returns the original committed RowID.
