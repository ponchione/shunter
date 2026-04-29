# Epic 3: Executor Core

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §2.1–§2.5, §4.1  
**Blocked by:** Epic 1 (Core Types), Epic 2 (Reducer Registry)  
**Blocks:** Epic 4 (Reducer Transaction Lifecycle)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 3.1 | [story-3.1-executor-struct.md](story-3.1-executor-struct.md) | Executor struct, NewExecutor constructor, TxID counter initialization |
| 3.2 | [story-3.2-run-loop.md](story-3.2-run-loop.md) | run(ctx) goroutine, inbox receive, dispatchSafely panic envelope |
| 3.3 | [story-3.3-submit-methods.md](story-3.3-submit-methods.md) | Blocking submit, ErrExecutorBusy reject policy, backpressure |
| 3.4 | [story-3.4-command-dispatch.md](story-3.4-command-dispatch.md) | dispatch() type switch, subscription command handlers, direct-snapshot read boundary |
| 3.5 | [story-3.5-shutdown.md](story-3.5-shutdown.md) | Close inbox, drain, ErrExecutorShutdown, durability teardown ordering |
| 3.6 | [story-3.6-startup-orchestration.md](story-3.6-startup-orchestration.md) | Recovery hand-off, scheduler replay, dangling-client sweep, and first-accept gating |

## Implementation Order

```
Story 3.1 (Struct + constructor)
  └── Story 3.2 (Run loop)
        ├── Story 3.3 (Submit methods)
        └── Story 3.4 (Dispatch routing)
              \ /
               X
              / \
             Story 3.5 (Shutdown)
               \
                └── Story 3.6 (Startup orchestration)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 3.1–3.3, 3.5 | `executor/executor.go`, `executor/executor_test.go` |
| 3.4 | `executor/dispatch.go` |
| 3.6 | `engine/startup.go`, `executor/executor.go`, integration startup tests |
