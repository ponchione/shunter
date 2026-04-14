# Epic 4: Reflection-Path Registration

**Parent:** [SPEC-006-schema.md](../SPEC-006-schema.md) §4.1, §11
**Blocked by:** Epic 1 (type mapping / naming contracts), Epic 2 (tag parser), Epic 3 (Builder.TableDef)
**Blocks:** Epic 5 (reflection-produced schemas need validation)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 4.1 | [story-4.1-field-discovery.md](story-4.1-field-discovery.md) | Reflect on struct fields, embed flattening, skip/reject rules, field metadata |
| 4.2 | [story-4.2-table-definition-assembly.md](story-4.2-table-definition-assembly.md) | Convert discovered fields into `TableDefinition`, naming overrides, and composite index assembly |
| 4.3 | [story-4.3-register-table-integration.md](story-4.3-register-table-integration.md) | `RegisterTable[T]` generic API, type checks, builder integration, path equivalence |

## Implementation Order

```
Story 4.1 (Field discovery)
  └── Story 4.2 (TableDefinition assembly)
        └── Story 4.3 (RegisterTable integration)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 4.1 | `schema/reflect.go`, `schema/reflect_test.go` |
| 4.2 | `schema/reflect_build.go`, `schema/reflect_build_test.go` |
| 4.3 | `schema/register_table.go`, `schema/register_table_test.go` |
