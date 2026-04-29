# Story 2.3: Row Type Validation

**Epic:** [Epic 2 — Schema & Table Storage](EPIC.md)  
**Spec ref:** SPEC-001 §3.1, §9  
**Depends on:** Story 2.1, Story 2.2  
**Blocks:** Epic 5 (Transaction.Insert calls this)

---

## Summary

Validate that a ProductValue matches the table's schema before storage.

## Deliverables

- `func ValidateRow(schema *TableSchema, row ProductValue) error`
  - Column count must match `len(schema.Columns)`
  - Each `row[i].Kind()` must match `schema.Columns[i].Type`
  - Nullable check: v1 all columns non-nullable, but zero value of a type IS valid — "nullable" means SQL NULL, not Go zero. v1 has no NULL concept, so this check is a no-op. Field retained for forward compat.

## Acceptance Criteria

- [ ] Valid row passes
- [ ] Wrong column count → `ErrRowShapeMismatch`
- [ ] Wrong type in column 0 → `ErrTypeMismatch` naming column 0
- [ ] Wrong type in column N-1 → `ErrTypeMismatch` naming last column
- [ ] All-zero values for matching types → valid (zero != null in v1)
- [ ] Error messages include column name and expected/got types

## Design Notes

- Validation is a pure function over schema + row. Keeps Table struct focused on storage.
- Use the spec-published row-shape vocabulary: width mismatches are `ErrRowShapeMismatch`, aligning store validation with SPEC-002 decode errors for schema-width failures.
- Called by Transaction.Insert (Epic 5), not by raw Table.InsertRow. Epic 2 tests call it directly.
