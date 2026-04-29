# Story 4.2: Synchronous Index Maintenance

**Epic:** [Epic 4 — Table Indexes & Constraints](EPIC.md)  
**Spec ref:** SPEC-001 §4.5  
**Depends on:** Story 4.1  
**Blocks:** Stories 4.3, 4.4

---

## Summary

Every insert/delete on a table updates all indexes synchronously.

## Deliverables

- `func (t *Table) insertIntoIndexes(rowID RowID, row ProductValue) error`
  - For each index: extract key, insert key→rowID into BTreeIndex
  - If any index rejects (unique violation), roll back previously inserted index entries for this row and return error
  - Rollback: on failure at index N, remove entries from indexes 0..N-1

- `func (t *Table) removeFromIndexes(rowID RowID, row ProductValue)`
  - For each index: extract key, remove key→rowID from BTreeIndex
  - No failure path — row is known to exist

- Wire into Table's insert/delete paths:
  - `InsertRow` calls `insertIntoIndexes` after storing row (or before, with rollback)
  - `DeleteRow` calls `removeFromIndexes` before removing row from map (need the row to extract keys)

## Acceptance Criteria

- [ ] Insert row into table with 3 indexes: all 3 indexes contain the key
- [ ] Delete row: all 3 indexes no longer contain the key
- [ ] Index Seek after insert finds the row
- [ ] Index Seek after delete does not find the row
- [ ] Insert 1000 rows, delete 500: index state consistent with remaining 500
- [ ] Failure rollback: if index 2 rejects, indexes 0 and 1 are also cleaned up, row not in table

## Design Notes

- Index maintenance is synchronous and in-process. No deferred or background rebuilds in v1.
- Rollback on partial index insertion is critical for correctness. If insert succeeds in indexes 0–1 but fails on index 2 (unique violation), entries in 0–1 must be removed.
- Delete order matters: extract keys from row BEFORE removing row from `rows` map.
