# Epic 5: Validation, Build & SchemaRegistry

**Parent:** [SPEC-006-schema.md](../SPEC-006-schema.md) §5 (Build / Start boundary), §6, §7, §9, §10, §13 (validation/version errors)
**Blocked by:** Epic 3 (Builder state)
**Blocks:** Epic 6 (Schema Export needs SchemaRegistry)

**Cross-spec:** SchemaRegistry consumed by SPEC-001 (store), SPEC-002 (commit log snapshots), SPEC-003 (executor). Schema version stored in snapshots per SPEC-002 §5.3. Reflection-path registrations from Epic 4 flow through this same validation/build pipeline once available.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 5.1 | [story-5.1-validation-rules.md](story-5.1-validation-rules.md) | Table / column / index validation rules and structural multi-error reporting |
| 5.2 | [story-5.2-system-tables.md](story-5.2-system-tables.md) | Auto-register sys_clients and sys_scheduled |
| 5.3 | [story-5.3-build-orchestration.md](story-5.3-build-orchestration.md) | Build() method, TableID/IndexID assignment, Engine construction |
| 5.4 | [story-5.4-schema-registry.md](story-5.4-schema-registry.md) | SchemaRegistry interface implementation |
| 5.5 | [story-5.5-reducer-schema-validation.md](story-5.5-reducer-schema-validation.md) | Reducer/lifecycle and schema-level validation rules |
| 5.6 | [story-5.6-schema-version-check.md](story-5.6-schema-version-check.md) | Startup schema version + structure comparison |

## Implementation Order

```
Story 5.1 (Structural validation)
  ├── Story 5.2 (System tables) — parallel with 5.1
  └── Story 5.5 (Reducer + schema-level validation) — parallel with 5.1
        └── Story 5.3 (Build orchestration) — needs 5.1 + 5.2 + 5.5; consumes builder-path inputs first and reflection-path inputs once Epic 4 lands
              └── Story 5.4 (SchemaRegistry)
                    └── Story 5.6 (Schema version check)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 5.1 | `schema/validate_structure.go`, `schema/validate_structure_test.go` |
| 5.2 | `schema/system_tables.go`, `schema/system_tables_test.go` |
| 5.3 | `schema/build.go`, `schema/build_test.go` |
| 5.4 | `schema/registry.go`, `schema/registry_test.go` |
| 5.5 | `schema/validate_schema.go`, `schema/validate_schema_test.go` |
| 5.6 | `schema/version.go`, `schema/version_test.go` |
