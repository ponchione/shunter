# Story 4.5: Constraint Error Types

**Epic:** [Epic 4 — Table Indexes & Constraints](EPIC.md)  
**Spec ref:** SPEC-001 §9  
**Depends on:** Nothing  
**Blocks:** Nothing (consumed by Stories 4.3, 4.4)

---

## Summary

Error types for constraint violations introduced in Epic 4.

## Deliverables

Extend `errors.go` with:

| Error | Type | Trigger |
|---|---|---|
| `ErrPrimaryKeyViolation` | struct | duplicate PK on insert |
| `ErrUniqueConstraintViolation` | struct | duplicate unique index key on insert |
| `ErrDuplicateRow` | sentinel | exact duplicate row in set-semantics table |

- `ErrPrimaryKeyViolation` fields:
  - `TableName string`
  - `IndexName string`
  - `Key IndexKey` (the conflicting key)

- `ErrUniqueConstraintViolation` fields:
  - `TableName string`
  - `IndexName string`
  - `Key IndexKey`

- `ErrDuplicateRow` — sentinel, no fields needed (the row itself is known to caller)

All implement `error` with descriptive messages.

## Acceptance Criteria

- [ ] `ErrPrimaryKeyViolation` message includes table name, index name, and key
- [ ] `ErrUniqueConstraintViolation` message includes table name, index name, and key
- [ ] `errors.Is(err, ErrDuplicateRow)` works
- [ ] `errors.As(err, &ErrPrimaryKeyViolation{})` works for PK errors
- [ ] `errors.As(err, &ErrUniqueConstraintViolation{})` works for unique errors
- [ ] Combined with Epic 2 errors, catalog now covers 8 of 9 SPEC-001 §9 errors (missing: `ErrRowNotFound`, added in Epic 5)
