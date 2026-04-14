# Epic 4: Reducer Transaction Lifecycle

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §3.4, §3.5, §4.2–§4.6  
**Blocked by:** Epic 3 (Executor Core), SPEC-001 (Store: NewTransaction, Commit)  
**Blocks:** Epic 5 (Post-Commit Pipeline), Epic 6 (Scheduled Reducers), Epic 7 (Lifecycle Reducers)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 4.1 | [story-4.1-begin-phase.md](story-4.1-begin-phase.md) | Reducer lookup, CallerContext with dequeue timestamp, Transaction + ReducerContext construction |
| 4.2 | [story-4.2-execute-phase.md](story-4.2-execute-phase.md) | Reducer-local panic recovery, handler invocation |
| 4.3 | [story-4.3-commit-path.md](story-4.3-commit-path.md) | store.Commit, TxID assignment, ReducerResponse for success |
| 4.4 | [story-4.4-rollback-and-failure.md](story-4.4-rollback-and-failure.md) | Rollback on error/panic, commit failure status mapping |

## Implementation Order

```
Story 4.1 (Begin)
  └── Story 4.2 (Execute)
        ├── Story 4.3 (Commit)
        └── Story 4.4 (Rollback + failure)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 4.1–4.4 | `executor/dispatch_reducer.go`, `executor/dispatch_reducer_test.go` |
