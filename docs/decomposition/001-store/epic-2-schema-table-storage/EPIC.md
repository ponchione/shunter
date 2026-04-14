# Epic 2: Schema & Table Storage

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §3  
**Blocked by:** Epic 1 (Core Value Types)  
**Blocks:** Epic 4 (Table Indexes & Constraints), Epic 5 (Transaction Layer)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 2.1 | [story-2.1-schema-structs.md](story-2.1-schema-structs.md) | TableSchema, ColumnSchema, IndexSchema, TableID, IndexID |
| 2.2 | [story-2.2-table-row-storage.md](story-2.2-table-row-storage.md) | Table struct, row CRUD, RowID allocation, scan |
| 2.3 | [story-2.3-row-validation.md](story-2.3-row-validation.md) | Type/column-count validation against schema |
| 2.4 | [story-2.4-error-types.md](story-2.4-error-types.md) | Error catalog through Epic 2 |

## Implementation Order

```
Story 2.1 (Schema structs)
  └── Story 2.2 (Table + row storage)
        └── Story 2.3 (Row validation)
Story 2.4 (Error types) — parallel with any, best alongside 2.3
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 2.1 | `schema.go` |
| 2.2 | `table.go`, `table_test.go` |
| 2.3 | `validate.go`, `validate_test.go` |
| 2.4 | `errors.go` |
