# Epic 1: Core Types & Command Model

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §2.2–§2.4, §3.1–§3.3, §6, §7, §8, §9.3, §11  
**Blocked by:** Nothing  
**Blocks:** Epic 2 (Reducer Registry), Epic 3 (Executor Core)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 1.1 | [story-1.1-foundation-types.md](story-1.1-foundation-types.md) | TxID, ScheduleID, SubscriptionID, CallSource, ReducerStatus, LifecycleKind |
| 1.2 | [story-1.2-reducer-types.md](story-1.2-reducer-types.md) | ReducerHandler, RegisteredReducer, CallerContext, ReducerRequest, ReducerResponse, ReducerContext |
| 1.3 | [story-1.3-command-types.md](story-1.3-command-types.md) | ExecutorCommand interface, CallReducerCmd, subscription commands |
| 1.4 | [story-1.4-subsystem-interfaces.md](story-1.4-subsystem-interfaces.md) | DurabilityHandle, SubscriptionManager, SchedulerHandle |
| 1.5 | [story-1.5-error-types.md](story-1.5-error-types.md) | Error sentinel values for executor error catalog |

## Implementation Order

```
Story 1.1 (Foundation types)
  ├── Story 1.2 (Reducer types) ← uses TxID, ReducerStatus, LifecycleKind
  ├── Story 1.3 (Command types) ← uses ReducerRequest, ReducerResponse, SubscriptionID
  ├── Story 1.4 (Subsystem interfaces) ← uses TxID, Changeset, ScheduleID
  └── Story 1.5 (Error types)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 1.1 | `executor/types.go` |
| 1.2 | `executor/reducer.go` |
| 1.3 | `executor/command.go` |
| 1.4 | `executor/interfaces.go` |
| 1.5 | `executor/errors.go` |
