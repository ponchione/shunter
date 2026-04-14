# Epic 2: Reducer Registry

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §3.2, §10.1  
**Blocked by:** Epic 1 (Core Types)  
**Blocks:** Epic 3 (Executor Core)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 2.1 | [story-2.1-registry.md](story-2.1-registry.md) | ReducerRegistry struct, Register, Lookup, name uniqueness |
| 2.2 | [story-2.2-lifecycle-validation.md](story-2.2-lifecycle-validation.md) | Lifecycle name reservation, Freeze immutability |

## Implementation Order

```
Story 2.1 (Registry)
  └── Story 2.2 (Lifecycle validation + freeze)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 2.1–2.2 | `executor/registry.go`, `executor/registry_test.go` |
