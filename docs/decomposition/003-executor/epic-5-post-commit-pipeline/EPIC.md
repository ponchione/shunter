# Epic 5: Post-Commit Pipeline

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §5.1–§5.4  
**Blocked by:** Epic 4 (Reducer Execution), SPEC-001 (Snapshot/ReadView), SPEC-002 (DurabilityHandle), SPEC-004 (SubscriptionManager)  
**Blocks:** Epic 6 (Scheduled Reducers), Epic 7 (Lifecycle Reducers)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 5.1 | [story-5.1-ordered-pipeline.md](story-5.1-ordered-pipeline.md) | Durability handoff → snapshot → subscription eval → delta → response, including acknowledged-before-durable semantics |
| 5.2 | [story-5.2-dropped-client-drain.md](story-5.2-dropped-client-drain.md) | Non-blocking drain of DroppedClients channel after each commit |
| 5.3 | [story-5.3-fatal-state.md](story-5.3-fatal-state.md) | Post-commit panic → fatal state, ErrExecutorFatal rejection |

## Implementation Order

```
Story 5.1 (Ordered pipeline)
  ├── Story 5.2 (Dropped client drain)
  └── Story 5.3 (Fatal state)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 5.1–5.3 | `executor/pipeline.go`, `executor/pipeline_test.go` |
