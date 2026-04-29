# Story 5.3: StateView — Unified Read Interface

**Epic:** [Epic 5 — Transaction Layer](EPIC.md)  
**Spec ref:** SPEC-001 §5.4  
**Depends on:** Stories 5.1, 5.2, Epic 4 (Index seeks)  
**Blocks:** Stories 5.4, 5.5, 5.6

---

## Summary

Merges committed state and tx-local state into a single read path. The "what does this transaction see?" layer.

## Deliverables

- `RowIterator` type alias:
  ```go
  type RowIterator = iter.Seq2[RowID, ProductValue]
  ```

- `StateView` struct:
  ```go
  type StateView struct {
      committed *CommittedState
      tx        *TxState
  }
  ```

- `func NewStateView(committed *CommittedState, tx *TxState) *StateView`

- `func (sv *StateView) GetRow(tableID TableID, rowID RowID) (ProductValue, bool)`
  1. If rowID in `tx.inserts[tableID]` → return that row
  2. If rowID in `tx.deletes[tableID]` → return not found
  3. Else look up in committed table rows

- `func (sv *StateView) ScanTable(tableID TableID) iter.Seq2[RowID, ProductValue]`
  1. Yield committed rows NOT in tx.deletes
  2. Yield tx-local inserts
  3. Order undefined

- `func (sv *StateView) SeekIndex(tableID TableID, indexID IndexID, key IndexKey) iter.Seq[RowID]`
  1. Query committed index via Seek
  2. Filter out RowIDs in tx.deletes
  3. Linear-scan tx.inserts, yield rows whose extracted key equals `key`

- `func (sv *StateView) SeekIndexRange(tableID TableID, indexID IndexID, low, high *IndexKey) iter.Seq[RowID]`
  1. Query committed B-tree range, filter deletes
  2. Linear-scan tx.inserts, include rows whose extracted key falls in [low, high)

- `func (sv *StateView) SeekIndexBounds(tableID TableID, indexID IndexID, low, high Bound) iter.Seq[RowID]`
  1. Query committed index via `BTreeIndex.SeekBounds(low, high)` (Story 3.3), filter deletes
  2. Linear-scan tx.inserts, include rows whose extracted key satisfies both `Bound` endpoints per SPEC-001 §4.4 (inclusive/exclusive/unbounded)
  3. Required by SPEC-004 predicate scans that need exclusive endpoints on non-integer keys; `SeekIndexRange` remains the half-open convenience wrapper (SPEC-AUDIT SPEC-001 §1.2/§2.6)

## Acceptance Criteria

- [ ] GetRow: committed row visible when not deleted
- [ ] GetRow: committed row invisible after tx delete
- [ ] GetRow: tx-local inserted row visible
- [ ] GetRow: non-existent RowID → not found
- [ ] ScanTable: committed rows minus deletes, plus tx inserts
- [ ] ScanTable: no duplicates (committed and tx-local RowIDs are disjoint)
- [ ] SeekIndex: committed index result filtered by deletes
- [ ] SeekIndex: tx-local rows matching key included
- [ ] SeekIndex: tx-local rows NOT matching key excluded
- [ ] SeekIndexRange: committed range filtered, tx-local range matched
- [ ] SeekIndexBounds with exclusive low / inclusive high: boundary rows handled per Bound semantics (committed + tx-local)
- [ ] SeekIndexBounds with both Unbounded: same result as ScanTable filtered by index
- [ ] Empty tx (no inserts/deletes): StateView behaves same as committed state
- [ ] Nil tx.inserts/tx.deletes for a table: handled gracefully (no panic)
- [ ] Unknown TableID: ScanTable returns empty iterator; GetRow returns (nil, false); SeekIndex/SeekIndexRange/SeekIndexBounds return empty iterators

## Design Notes

- Linear scan of tx.inserts for SeekIndex/SeekIndexRange is O(n) in tx-local row count. Acceptable in v1 — most reducers insert small numbers of rows. Profiling may justify tx-local indexes in v2.
- No duplicates possible between committed and tx-local results because RowID spaces are disjoint (monotonic counter, no reuse).
- Unknown `TableID` passed to `GetRow`, `ScanTable`, `SeekIndex`, `SeekIndexRange`, or `SeekIndexBounds` yields an empty result (no error, no panic). Error-returning shape is reserved for the mutation-side `Transaction.Insert/Delete/Update` boundary (SPEC-001 §9 `ErrTableNotFound`).
