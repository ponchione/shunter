# Story 2.4: Error Types

**Epic:** [Epic 2 — Schema & Table Storage](EPIC.md)  
**Spec ref:** SPEC-001 §9  
**Depends on:** Nothing  
**Blocks:** Nothing (consumed by other stories as needed)

---

## Summary

Collect all SPEC-001 error types introduced through Epic 2.

## Deliverables

Sentinel errors and structured error types:

| Error | Type | Introduced |
|---|---|---|
| `ErrTableNotFound` | sentinel | table lookup miss |
| `ErrColumnNotFound` | sentinel | column name lookup miss |
| `ErrTypeMismatch` | struct | wrong ValueKind for column |
| `ErrRowShapeMismatch` | struct | row width != schema width |
| `ErrNullNotAllowed` | sentinel | reserved, v1 no-op |
| `ErrInvalidFloat` | sentinel | from Epic 1 (NaN), listed here for catalog completeness |

All implement `error`. Structured errors have `Error() string` with useful detail.

## Acceptance Criteria

- [ ] Each error type satisfies `error` interface
- [ ] `errors.Is` works for sentinels
- [ ] Struct errors include meaningful message with field names/values
- [ ] Catalog matches SPEC-001 §9 for errors introduced so far

## Design Notes

- Remaining errors (`ErrPrimaryKeyViolation`, `ErrUniqueConstraintViolation`, `ErrDuplicateRow`, `ErrRowNotFound`) arrive in Epic 4 and Epic 5.
- All errors in one file (`errors.go`) so the catalog is greppable.
