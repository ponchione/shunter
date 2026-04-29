# Story 4.4: Set Semantics (Duplicate Row Prevention)

**Epic:** [Epic 4 — Table Indexes & Constraints](EPIC.md)  
**Spec ref:** SPEC-001 §3.3  
**Depends on:** Story 4.2, Epic 1 (ProductValue hashing + equality)  
**Blocks:** Epic 5, Epic 8 (ApplyChangeset delete-by-hash)

---

## Summary

Tables without a primary key use row-level hashing to prevent exact duplicate rows.

## Deliverables

- Extend `Table` struct:
  ```go
  type Table struct {
      // ... existing fields ...
      rowHashIndex map[uint64][]RowID   // only for tables with no PK
  }
  ```

- `NewTable` creates `rowHashIndex` only when `PrimaryIndex() == nil`

- On insert (set-semantics table):
  1. Hash row using `ProductValue.Hash64()`
  2. Lookup bucket `rowHashIndex[hash]`
  3. Compare each candidate row for exact equality (`ProductValue.Equal`)
  4. If exact duplicate exists → `ErrDuplicateRow`
  5. Otherwise append new RowID to bucket

- On delete (set-semantics table):
  1. Hash the deleted row
  2. Remove specific RowID from bucket
  3. If bucket becomes empty, delete map entry

- Tables WITH a PK: `rowHashIndex` is nil, no hash checks. PK unique index handles uniqueness.

## Acceptance Criteria

- [ ] No-PK table: insert row A, insert identical row A → `ErrDuplicateRow`
- [ ] No-PK table: insert row A, insert row B (different) → both accepted
- [ ] Hash collision: two non-equal rows with same hash → both accepted (bucket has 2 entries)
- [ ] Delete row A from bucket, insert row A again → succeeds
- [ ] Delete last row in bucket → bucket removed from map
- [ ] PK table: `rowHashIndex` is nil, no hash operations occur
- [ ] No-PK table with 1000 rows: all unique, all findable via hash index

## Design Notes

- Hash collisions are expected. Bucket is `[]RowID`, full equality check on candidates. MUST NOT assume hash is unique.
- `ProductValue.Hash64()` from Epic 1 Story 1.5 is the hash function. Same hasher seed as the rest of the store.
- This is the ONLY duplicate-prevention mechanism for no-PK tables. Without it, exact duplicate rows could accumulate silently.
