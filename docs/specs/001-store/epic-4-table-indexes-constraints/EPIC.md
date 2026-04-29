# Epic 4: Table Indexes & Constraints

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §3.1 (PK rules), §3.3, §4.5  
**Blocked by:** Epic 2 (Table), Epic 3 (BTreeIndex)  
**Blocks:** Epic 5 (Transaction Layer)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 4.1 | [story-4.1-index-wrapper.md](story-4.1-index-wrapper.md) | Index struct wrapping BTreeIndex + IndexSchema, table index initialization |
| 4.2 | [story-4.2-index-maintenance.md](story-4.2-index-maintenance.md) | Synchronous index update on insert/delete |
| 4.3 | [story-4.3-unique-constraints.md](story-4.3-unique-constraints.md) | PK and unique index enforcement |
| 4.4 | [story-4.4-set-semantics.md](story-4.4-set-semantics.md) | rowHashIndex for tables without PK, duplicate row rejection |
| 4.5 | [story-4.5-constraint-error-types.md](story-4.5-constraint-error-types.md) | Error types for constraint violations |

## Implementation Order

```
Story 4.1 (Index wrapper + table init)
  └── Story 4.2 (Index maintenance)
        ├── Story 4.3 (Unique constraints)
        └── Story 4.4 (Set semantics)
Story 4.5 (Error types) — parallel, but consumed by 4.3 and 4.4
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 4.1 | `index.go` |
| 4.2 | `index.go` (extend), `index_test.go` |
| 4.3 | `constraints.go`, `constraints_test.go` |
| 4.4 | `set_semantics.go`, `set_semantics_test.go` |
| 4.5 | `errors.go` (extend) |
