# Story 5.2: TxState

**Epic:** [Epic 5 — Transaction Layer](EPIC.md)  
**Spec ref:** SPEC-001 §5.3  
**Depends on:** Story 5.1, Epic 1 (RowID, ProductValue)  
**Blocks:** Story 5.3

---

## Summary

The local mutation buffer for an in-progress transaction. Tracks inserts and deletes without duplicating committed index structures.

## Deliverables

- `TxState` struct:
  ```go
  type TxState struct {
      inserts map[TableID]map[RowID]ProductValue
      deletes map[TableID]map[RowID]struct{}
  }
  ```

- `func NewTxState() *TxState`

- `func (tx *TxState) AddInsert(tableID TableID, rowID RowID, row ProductValue)`
  - Store tx-local row keyed by provisional RowID

- `func (tx *TxState) RemoveInsert(tableID TableID, rowID RowID)`
  - Remove tx-local row (insert-then-delete collapse)

- `func (tx *TxState) AddDelete(tableID TableID, rowID RowID)`
  - Mark committed RowID as hidden

- `func (tx *TxState) CancelDelete(tableID TableID, rowID RowID)`
  - Undelete: remove from deletes set (reinsert-identical optimization)

- `func (tx *TxState) IsInserted(tableID TableID, rowID RowID) bool`

- `func (tx *TxState) IsDeleted(tableID TableID, rowID RowID) bool`

- `func (tx *TxState) Inserts(tableID TableID) map[RowID]ProductValue`
  - Returns tx-local inserts for table (may be nil)

- `func (tx *TxState) Deletes(tableID TableID) map[RowID]struct{}`
  - Returns delete set for table (may be nil)

- RowID-class invariant owned by this story:
  - All tx-local provisional `RowID` values allocated during a transaction are strictly greater than any committed `RowID` that existed when the transaction began
  - This invariant must be documented and covered by tests because `StateView` correctness depends on it; check order alone is not sufficient

## Acceptance Criteria

- [ ] AddInsert then IsInserted → true
- [ ] RemoveInsert then IsInserted → false
- [ ] AddDelete then IsDeleted → true
- [ ] CancelDelete then IsDeleted → false
- [ ] Inserts returns all tx-local rows for table
- [ ] Deletes returns all hidden RowIDs for table
- [ ] Operations on table A don't affect table B
- [ ] Fresh TxState: all lookups return false/nil
- [ ] Given a committed table state with existing RowIDs, a new tx-local provisional RowID is always greater than the committed maximum visible at transaction start
- [ ] StateView-style lookup ordering is safe because committed-row IDs and tx-local provisional IDs cannot overlap the wrong way around

## Design Notes

- TxState is a plain buffer. No indexes, no constraint checks. Those happen in Transaction (Stories 5.4–5.6) using TxState + CommittedState together.
