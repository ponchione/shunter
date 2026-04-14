# Story 4.1: Field Discovery & Type Mapping via Reflect

**Epic:** [Epic 4 — Reflection-Path Registration](EPIC.md)
**Spec ref:** SPEC-006 §11, §11.1, §11.3, §2
**Depends on:** Epic 1 (GoTypeToValueKind, snake_case), Epic 2 (ParseTag)
**Blocks:** Story 4.2

---

## Summary

Walk a Go struct's exported fields via `reflect`, resolve types, parse tags, handle embedding, and produce ordered per-field metadata for later table-definition assembly.

## Deliverables

- `type fieldInfo struct` — intermediate per-field result:
  ```go
  type fieldInfo struct {
      FieldName  string
      ColumnName string
      Type       ValueKind
      Tags       *TagDirectives
  }
  ```

- `func discoverFields(t reflect.Type) ([]fieldInfo, error)` — given a struct type:
  1. Iterate exported fields in declaration order
  2. For each field:
     - Unexported field → skip silently
     - `shunter:"-"` → skip entirely
     - Embedded non-pointer struct → recurse and flatten in declaration order
     - Embedded pointer-to-struct → error
     - Unsupported type → error via `GoTypeToValueKind`
     - Supported field → parse tag, derive column name, emit `fieldInfo`
  3. Return ordered `[]fieldInfo`

- Error formatting convention:
  ```
  schema error: Player.CachedAt: field type *time.Time is not supported; use int64 for Unix nanoseconds
  schema error: Player.ID: autoincrement requires primarykey or unique
  schema error: Player: duplicate primarykey on fields ID and UID
  ```

## Acceptance Criteria

- [ ] Struct with all 13 supported scalar types → 13 `fieldInfo` entries with correct `ValueKind`
- [ ] `*time.Time` field → error mentioning struct and field name
- [ ] `int` field → error suggesting explicit width
- [ ] Unexported field → silently skipped
- [ ] `shunter:"-"` field → silently skipped
- [ ] Embedded non-pointer struct and deeply nested embedding → flattened in declaration order
- [ ] Embedded pointer-to-struct → error
- [ ] Error messages include both struct type name and field name

## Design Notes

- `discoverFields` stops at ordered field metadata. It does not assemble indexes or call the builder.
- The recursive embedding walk should retain enough path context for useful error messages (`Player.BaseEntity.ID`).
- Field ordering follows `reflect.Type.Field(i)` order, which matches declaration order.
