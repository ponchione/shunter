# Story 4.2: TableDefinition Assembly from Reflected Fields

**Epic:** [Epic 4 — Reflection-Path Registration](EPIC.md)
**Spec ref:** SPEC-006 §4, §4.1, §3.3, §11
**Depends on:** Story 4.1
**Blocks:** Story 4.3

---

## Summary

Turn ordered reflected field metadata into a `TableDefinition`: derive the default table/column names, translate tag directives into `ColumnDefinition`s, and assemble explicit secondary index definitions including named composite indexes.

## Deliverables

- `func buildTableDefinition(typeName string, fields []fieldInfo, opts ...TableOption) (TableDefinition, error)`:
  1. Derive table name from snake_case of the Go type name, overridden by `WithTableName` if present
  2. Build `[]ColumnDefinition` from `[]fieldInfo`
  3. Build `[]IndexDefinition` from field tags:
     - `primarykey` column contributes no explicit `IndexDefinition`
     - plain `unique` contributes `<col>_uniq`
     - plain `index` contributes `<col>_idx`
     - `index:<name>` groups fields into one composite index in field order
  4. Reject mixed unique/non-unique participation on the same named composite index
  5. Return assembled `TableDefinition`

## Acceptance Criteria

- [ ] Default table name uses snake_case of the Go type name, and `WithTableName("sessions")` overrides it
- [ ] PK and autoincrement directives set the corresponding `ColumnDefinition` flags
- [ ] Plain `index` and plain `unique` each produce the correct single-column `IndexDefinition`
- [ ] Two fields with `index:guild_score` produce one composite `IndexDefinition` with columns in field order
- [ ] Mixed unique flags on the same named composite index → error
- [ ] No explicit `IndexDefinition` is emitted for the PK column

## Design Notes

- Primary index synthesis stays in `Build()`. Reflection-path assembly only creates the declarative `TableDefinition` consumed by the builder.
- This split keeps cross-field index assembly separate from the public generic API in Story 4.3.
