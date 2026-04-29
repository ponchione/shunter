# Epic 1: Schema Types & Type Mapping

**Parent:** [SPEC-006-schema.md](../SPEC-006-schema.md) §2, §8, §13 (error ownership for type-mapping contracts)
**Blocked by:** Nothing — leaf epic
**Blocks:** Epic 3 (Builder), Epic 4 (Reflection), Epic 6 (schema export)

**Cross-spec:** Consumes `ValueKind` from SPEC-001 §2.1. Produces `TableSchema` consumed by SPEC-001 §3.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 1.1 | [story-1.1-schema-structs.md](story-1.1-schema-structs.md) | TableSchema, ColumnSchema, IndexSchema, TableID, IndexID |
| 1.2 | [story-1.2-type-mapping.md](story-1.2-type-mapping.md) | Go type → ValueKind mapping, named type resolution, excluded types |
| 1.3 | [story-1.3-snake-case.md](story-1.3-snake-case.md) | CamelCase → snake_case conversion for table and column names |
| 1.4 | [story-1.4-valuekind-export-bounds.md](story-1.4-valuekind-export-bounds.md) | ValueKind export strings and integer auto-increment bounds contract |

## Implementation Order

```
Story 1.1 (Schema structs)
  ├── Story 1.2 (Type mapping) — needs ValueKind from 1.1 re-export
  └── Story 1.4 (Export strings + bounds contract) — needs ValueKind from 1.1 re-export
Story 1.3 (Snake_case) — independent, parallel with any
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 1.1 | `schema/types.go` |
| 1.2 | `schema/typemap.go`, `schema/typemap_test.go` |
| 1.3 | `schema/naming.go`, `schema/naming_test.go` |
| 1.4 | `schema/valuekind_export.go`, `schema/valuekind_export_test.go` |
