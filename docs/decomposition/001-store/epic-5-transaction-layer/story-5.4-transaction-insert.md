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
  1. Validate row against table schema (Story 2.3 `ValidateRow`)
  2. Check NaN in float columns (already enforced by Value construction, but belt-and-suspenders)
  3. **Undelete check** (set-semantics and PK tables):
     - If an identical committed row exists in `tx.deletes`, cancel the delete and return the committed RowID
     - For PK tables: match by PK value
     - For no-PK tables: match by full row equality
  4. Check uniqueness against committed indexes (filtered by tx.deletes) + tx-local inserts
     - For each unique/PK index: extract key, seek committed index, check tx inserts
  5. Check set-semantics duplicate (no-PK tables) against committed rowHashIndex (filtered by tx.deletes) + tx-local inserts
  6. Allocate provisional RowID from table's counter
  7. Store in tx.inserts

- `func (t *Transaction) View() *StateView`

## Acceptance Criteria

- [ ] Insert valid row → returns RowID, row visible via StateView
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

## Design Notes

- Undelete is the trickiest part. When a committed row is deleted then re-inserted with identical value, the delete is canceled rather than creating a new tx-local row. This ensures the changeset collapses to no-op at commit time.
- Constraint checking must consider both layers: committed (minus deletes) and tx-local inserts. A unique key that exists in committed state but is deleted in this tx should NOT block a new insert with that key.
- `ErrRowNotFound` is NOT returned by Insert — that's a Delete/Update concern (Story 5.5).
