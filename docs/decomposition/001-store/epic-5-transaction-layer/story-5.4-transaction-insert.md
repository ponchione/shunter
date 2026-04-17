# Story 5.4: Transaction.Insert

**Epic:** [Epic 5 — Transaction Layer](EPIC.md)  
**Spec ref:** SPEC-001 §5.5  
**Depends on:** Story 5.3, Epic 4 (constraints)  
**Blocks:** Story 5.6

---

## Summary

Insert a row within a transaction. Allocates provisional RowID, validates schema, checks constraints against both committed and tx-local state, handles undelete optimization.

## Deliverables

- `Transaction` struct:
  ```go
  type Transaction struct {
      state   *StateView
      tx      *TxState
      schema  SchemaRegistry
  }
  ```

- `func NewTransaction(committed *CommittedState, schema SchemaRegistry) *Transaction`
  - Creates TxState, StateView, wraps them

- `func (t *Transaction) Insert(tableID TableID, row ProductValue) (RowID, error)`

  **Algorithm:**
  0. Look up table by `TableID` via `committed.Table(tableID)`. If not found, return `(0, ErrTableNotFound)`.
  1. Validate row against table schema (Story 2.3 `ValidateRow`)
  2. Check NaN in float columns (already enforced by Value construction, but belt-and-suspenders)
  3. **Undelete check** (set-semantics and PK tables):
     - For PK tables: locate the candidate committed row via PK value, then require **full-row equality** (`ProductValue.Equal`) to trigger undelete. PK-match-without-row-equality is NOT an undelete — the delete stays in `tx.deletes` and the insert proceeds as a new tx-local row (old row lands in changeset Deletes, new row in Inserts). Without this, a reducer that deletes `(pk=5, name="a")` then inserts `(pk=5, name="b")` would silently collapse both into a no-op and subscribers never see the name change.
     - For no-PK tables: match by full row equality directly against candidates in `tx.deletes`.
     - On full-row-equal match: cancel that delete (remove the committed RowID from `tx.deletes[tableID]`) and return the committed RowID. No new tx-local row created.
  4. Check uniqueness against committed indexes (filtered by tx.deletes) + tx-local inserts
     - For each unique/PK index: extract key, seek committed index, check tx inserts
  5. Check set-semantics duplicate (no-PK tables) against committed rowHashIndex (filtered by tx.deletes) + tx-local inserts
  6. Allocate provisional RowID from table's counter
  7. Store in tx.inserts

- `func (t *Transaction) View() *StateView`

## Acceptance Criteria

- [ ] Insert valid row → returns RowID, row visible via StateView
- [ ] Insert with unknown TableID → `ErrTableNotFound`; no RowID allocated
- [ ] Insert with schema mismatch → error, no RowID allocated
- [ ] Insert duplicate PK (committed, not deleted) → `ErrPrimaryKeyViolation`
- [ ] Insert duplicate PK (tx-local) → `ErrPrimaryKeyViolation`
- [ ] Insert duplicate PK (committed but deleted in tx) → undelete, returns committed RowID
- [ ] Insert duplicate unique key → `ErrUniqueConstraintViolation`
- [ ] Insert exact duplicate row (no-PK, committed, not deleted) → `ErrDuplicateRow`
- [ ] Insert exact duplicate row (no-PK, committed, deleted in tx) → undelete
- [ ] Insert exact duplicate of tx-local row (no-PK) → `ErrDuplicateRow`
- [ ] After undelete: RowID returned is the original committed RowID
- [ ] After undelete: row no longer in tx.deletes
- [ ] Provisional RowID > all committed RowIDs
- [ ] PK table: delete committed `(pk=5, name="a")`, insert `(pk=5, name="b")` → no undelete; tx.deletes retains the committed RowID, tx.inserts gains the new row; commit emits both delete and insert

## Design Notes

- Undelete is the trickiest part. When a committed row is deleted then re-inserted with identical value, the delete is canceled rather than creating a new tx-local row. This ensures the changeset collapses to no-op at commit time.
- Constraint checking must consider both layers: committed (minus deletes) and tx-local inserts. A unique key that exists in committed state but is deleted in this tx should NOT block a new insert with that key.
- `ErrRowNotFound` is NOT returned by Insert — that's a Delete/Update concern (Story 5.5).
