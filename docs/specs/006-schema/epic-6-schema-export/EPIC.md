# Epic 6: Schema Export & Codegen Interface

**Parent:** [SPEC-006-schema.md](../SPEC-006-schema.md) §12
**Blocked by:** Epic 5 (SchemaRegistry to export from), Epic 1 Story 1.4 (ValueKind export strings)
**Blocks:** Nothing (terminal epic)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 6.1 | [story-6.1-export-types.md](story-6.1-export-types.md) | SchemaExport, TableExport, ColumnExport, IndexExport, ReducerExport types |
| 6.2 | [story-6.2-export-schema.md](story-6.2-export-schema.md) | Engine.ExportSchema() traversal over the immutable SchemaRegistry |
| 6.3 | [story-6.3-codegen-tool-contract.md](story-6.3-codegen-tool-contract.md) | `shunter-codegen` CLI contract for consuming exported schema JSON |
| 6.4 | [story-6.4-export-json-contract.md](story-6.4-export-json-contract.md) | JSON serialization contract and export snapshot/value semantics |

## Implementation Order

```
Story 6.1 (Export types)
  └── Story 6.2 (ExportSchema traversal)
        ├── Story 6.4 (JSON contract + snapshot semantics)
        └── Story 6.3 (Codegen tool contract) — consumes the JSON output surface
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 6.1 | `schema/export.go` |
| 6.2 | `schema/export.go` (extend), `schema/export_test.go` |
| 6.3 | Future target: `cmd/shunter-codegen/main.go`, `cmd/shunter-codegen/main_test.go` (not present in the current docs-first repo) |
| 6.4 | `schema/export_json_test.go`, `docs/codegen/schema-export-contract.md` |
