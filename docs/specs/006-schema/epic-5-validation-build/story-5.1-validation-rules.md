# Story 5.1: Structural Validation Rules

**Epic:** [Epic 5 — Validation, Build & SchemaRegistry](EPIC.md)
**Spec ref:** SPEC-006 §9, §13
**Depends on:** Epic 3 (Builder state to validate)
**Blocks:** Story 5.3 (Build calls validation)

**Cross-spec:** Validates the table/column/index structures later frozen into the registry consumed by SPEC-001/002/003.

---

## Summary

Validate the structural parts of registered tables before `Build()` proceeds: table names, column definitions, index definitions, and aggregate multi-error reporting.

## Deliverables

- `func validateStructure(b *Builder) []error` — returns all structural validation errors found.

  Table-level checks:
  - Table name must be non-empty → `ErrEmptyTableName`
  - Table name must match `[A-Za-z][A-Za-z0-9_]*` → `ErrInvalidTableName`
  - Table name must be unique across all registered tables → `ErrDuplicateTableName`
  - Table must have at least one column
  - At most one `PrimaryKey` column per table → `ErrDuplicatePrimaryKey`
  - `AutoIncrement` only on integer-typed columns → `ErrAutoIncrementType`
  - `AutoIncrement` requires `PrimaryKey` or `Unique` on the same column → `ErrAutoIncrementRequiresKey`

  Column-level checks:
  - Column name must be non-empty → `ErrEmptyColumnName`
  - Column name must match `[a-z][a-z0-9_]*` → `ErrInvalidColumnName`
  - Column name must be unique within the table
  - Column type must be a valid `ValueKind`
  - `Nullable` is reserved and MUST be `false` in v1. The v1 builder API exposes no `Nullable` input, so `Build()` produces nullable-free columns by construction; explicit `ErrNullableColumn` rejection activates once the builder surface is extended (Session 12+ drift — see SPEC-006 §13 and TECH-DEBT). No silent coercion in either case.

  Index-level checks:
  - Index name must be non-empty
  - Index name must be unique within the table
  - Index must reference at least one column
  - Every index column must reference an existing column name
  - Composite index columns use the order provided by the registration path
  - Primary index must reference exactly one column in v1
  - Mixed `unique` vs non-`unique` on the same named composite index within one table → error
  - A PK column must not also appear in an explicit `IndexDefinition`

## Acceptance Criteria

- [ ] Empty table name → `ErrEmptyTableName`
- [ ] Table name `"123bad"` → `ErrInvalidTableName`
- [ ] Duplicate table name → `ErrDuplicateTableName`
- [ ] Table with zero columns or duplicate column names → structural validation error
- [ ] Empty column name → `ErrEmptyColumnName`
- [ ] Column name `"123"` → `ErrInvalidColumnName`
- [ ] Two PK columns on one table → `ErrDuplicatePrimaryKey`
- [ ] `AutoIncrement` on `String` column or without PK/unique → validation error
- [ ] Target state: column declared with `Nullable = true` → `ErrNullableColumn` (pending builder-surface extension; see Session 12+ drift note in SPEC-006 §13 / TECH-DEBT)
- [ ] Index with zero columns or nonexistent column reference → validation error
- [ ] Explicit index containing the PK column or mixed unique flags on a composite index within one table → validation error

## Design Notes

- Structural validation returns all discovered errors in one pass for better developer experience.
- Reflection-path composite indexes are already in declaration order when they arrive here; builder-path registrations supply explicit column order and that order is accepted as authoritative.
- Mixed-unique validation is scoped to one table. Reflection-path registration catches the common case earlier in Story 4.2 while builder-path registration ultimately relies on the per-table duplicate-index-name / consistency checks here.
- Reflection-path users hit the tag-parser prohibition on `primarykey` + `index` / `index:<name>` before this story. The explicit "PK column must not appear in an `IndexDefinition`" rule here exists to keep the builder path aligned with that contract.
- Reducer and top-level schema checks are intentionally split into Story 5.5 so this story stays focused on table structure.
