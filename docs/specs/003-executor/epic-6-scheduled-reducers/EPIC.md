# Epic 6: Scheduled Reducers

**Parent:** [SPEC-003-executor.md](../SPEC-003-executor.md) §9  
**Blocked by:** Epic 4 (Reducer Execution), Epic 5 (Post-Commit Pipeline)  
**Blocks:** Nothing

---

## Stories

| Story | File | Summary |
|---|---|---|
| 6.1 | [story-6.1-sys-scheduled-table.md](story-6.1-sys-scheduled-table.md) | sys_scheduled schema, ScheduleID type, system table registration |
| 6.2 | [story-6.2-transactional-schedule.md](story-6.2-transactional-schedule.md) | SchedulerHandle implementation: Schedule, ScheduleRepeat, Cancel as tx mutations |
| 6.3 | [story-6.3-timer-wakeup.md](story-6.3-timer-wakeup.md) | Timer goroutine, scan for due schedules, enqueue internal CallReducerCmd |
| 6.4 | [story-6.4-firing-semantics.md](story-6.4-firing-semantics.md) | One-shot delete, repeating advance (fixed-rate), failure rollback |
| 6.5 | [story-6.5-startup-replay.md](story-6.5-startup-replay.md) | On startup, scan sys_scheduled and populate timer with pending wakeups |

## Implementation Order

```
Story 6.1 (Table schema)
  └── Story 6.2 (Transactional schedule/cancel)
        └── Story 6.3 (Timer goroutine)
              └── Story 6.4 (Firing semantics)
                    └── Story 6.5 (Startup replay)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 6.1 | `executor/sys_scheduled.go` |
| 6.2 | `executor/scheduler.go` |
| 6.3–6.5 | `executor/scheduler_worker.go`, `executor/scheduler_worker_test.go` |
