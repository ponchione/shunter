# Story 6.5: Startup Replay

**Epic:** [Epic 6 — Scheduled Reducers](EPIC.md)  
**Spec ref:** SPEC-003 §9.2, §9.4  
**Depends on:** Story 6.4  
**Blocks:** Nothing

---

## Summary

On executor startup, scan sys_scheduled to populate the timer with all pending wakeups. Ensures schedules survive process restart.

## Deliverables

- ```go
  func (s *Scheduler) ReplayFromCommitted(store *CommittedState, tableID TableID)
  ```
  - Acquire snapshot of committed state
  - Scan all rows in sys_scheduled
  - For each row:
    - If `next_run_at_ns <= now`: mark as immediately due
    - If `next_run_at_ns > now`: register wakeup at that time
  - Close snapshot
  - Trigger initial timer scan

- Called during executor startup, after recovery but before accepting external commands.
- Story 3.6 owns the larger startup sequence; this story owns only the scheduler-replay step within that sequence.

## Acceptance Criteria

- [ ] All sys_scheduled rows scanned at startup
- [ ] Past-due schedules fire promptly after startup
- [ ] Future schedules wake timer at correct time
- [ ] Empty sys_scheduled → no timer activity
- [ ] Schedules from prior process session are picked up correctly
- [ ] Startup replay completes before executor accepts commands

## Design Notes

- This is the mechanism that makes schedules survive restart. The in-memory timer is ephemeral — `sys_scheduled` is the durable source of truth. On startup, the timer is rebuilt from the table.
- Past-due schedules (where `next_run_at_ns < now`) will fire as soon as the executor starts processing commands. Multiple past-due schedules will queue up and execute sequentially.
- If recovery truncated some committed transactions (crash before durable), some schedules that were created in those transactions won't exist in `sys_scheduled`. This is correct — those transactions are considered uncommitted after recovery.
