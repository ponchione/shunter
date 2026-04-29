# Epic 3: Builder & Builder-Path Registration

**Parent:** [SPEC-006-schema.md](../SPEC-006-schema.md) §4.2, §4.3, §5 (Builder struct, registration mutators, EngineOptions)
**Blocked by:** Epic 1 (schema types used in TableDefinition)
**Blocks:** Epic 4 (RegisterTable calls Builder.TableDef), Epic 5 (Build validates Builder state)

**Cross-spec:** `ReducerHandler` type from SPEC-003 §10.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 3.1 | [story-3.1-builder-core.md](story-3.1-builder-core.md) | Builder struct, NewBuilder, TableDef, SchemaVersion, EngineOptions |
| 3.2 | [story-3.2-reducer-registration.md](story-3.2-reducer-registration.md) | Reducer, OnConnect, OnDisconnect registration |

## Implementation Order

```
Story 3.1 (Builder core + TableDef)
  └── Story 3.2 (Reducer registration)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 3.1 | `schema/builder.go`, `schema/builder_test.go` |
| 3.2 | `schema/builder.go` (extend), `schema/builder_test.go` |
