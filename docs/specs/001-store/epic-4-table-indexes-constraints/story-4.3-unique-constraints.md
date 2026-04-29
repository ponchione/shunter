# Story 4.3: Unique & Primary Key Constraints

**Epic:** [Epic 4 — Table Indexes & Constraints](EPIC.md)  
**Spec ref:** SPEC-001 §3.1 (PK rules), §4.5  
**Depends on:** Story 4.2  
**Blocks:** Epic 5 (Transaction.Insert constraint checking)

---

## Summary

Enforce uniqueness on primary key and unique indexes at insert time.

## Deliverables

- Unique check in `insertIntoIndexes`:
  - Before inserting key→rowID, check if key already exists in BTreeIndex via `Seek`
  - If `idx.unique` and key exists → return error
  - If `idx.schema.Primary` → `ErrPrimaryKeyViolation`
  - If `idx.unique && !idx.schema.Primary` → `ErrUniqueConstraintViolation`

- Primary key rules:
  - At most one per table (enforced at schema validation time, Story 2.1)
  - Primary implies unique
  - When PK exists, rowHashIndex is NOT created (Story 4.4)

- Non-unique indexes: no check, just append RowID to existing key entry

## Acceptance Criteria

- [ ] Insert row with PK=1, insert another with PK=1 → `ErrPrimaryKeyViolation`
- [ ] Insert row with unique idx key=A, insert another with key=A → `ErrUniqueConstraintViolation`
- [ ] Non-unique index: two rows with same key both accepted
- [ ] Delete row with PK=1, then insert new row with PK=1 → succeeds
- [ ] Multi-column unique index: `(A,1)` and `(A,2)` both accepted, second `(A,1)` rejected
- [ ] Partial index rollback: insert fails on unique index 2, indexes 0–1 cleaned up

## Design Notes

- Error distinction between PK and unique-secondary matters for caller diagnostics. Same mechanism, different error type.
- Constraint check is `Seek` then conditional insert — not atomic at the BTree level, but safe because Table operations are single-threaded within a transaction. Concurrency safety comes from the executor's single-writer model (Epic 5/6).
