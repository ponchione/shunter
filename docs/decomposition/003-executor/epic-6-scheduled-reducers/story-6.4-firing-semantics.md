# Story 6.4: Firing Semantics

**Epic:** [Epic 6 — Scheduled Reducers](EPIC.md)  
**Spec ref:** SPEC-003 §9.4, §9.5  
**Depends on:** Story 6.3  
**Blocks:** Story 6.5

---

## Summary

When a scheduled reducer fires: execute within a transaction that also mutates sys_scheduled. One-shot deletes the row; repeating advances next_run_at_ns using fixed-rate semantics.

## Deliverables

- **One-shot success path:**
  1. Begin transaction
  2. Execute reducer handler
  3. In same transaction, delete `sys_scheduled` row for this schedule_id
  4. Commit once (reducer writes + row deletion are atomic)

- **Repeating success path:**
  1. Begin transaction
  2. Execute reducer handler
  3. In same transaction, update `sys_scheduled` row: `next_run_at_ns = intended_fire_time + repeat_ns`
  4. Commit once

- **Failure path** (reducer error or panic):
  - Transaction rolls back
  - `sys_scheduled` row unchanged
  - Schedule retried after restart or next timer rescan

- **Fixed-rate semantics:**
  - Next fire time = `intended_fire_time + repeat_ns` (not `completion_time + repeat_ns`)
  - If execution ran late (started at T+5 instead of T), next fire is still `T + interval`
  - Prevents unbounded drift under load

- Implementation: the executor's `handleCallReducer` detects `CallSourceScheduled` and adds the sys_scheduled mutation to the transaction before commit.

## Acceptance Criteria

- [ ] One-shot: reducer commits → sys_scheduled row deleted in same commit
- [ ] One-shot: reducer fails → sys_scheduled row unchanged
- [ ] Repeating: reducer commits → next_run_at_ns = old_next_run_at_ns + repeat_ns
- [ ] Repeating: reducer fails → next_run_at_ns unchanged
- [ ] Fixed-rate: intended fire T, interval I, late execution → next = T + I (not now + I)
- [ ] All sys_scheduled mutations are in same transaction as reducer writes
- [ ] Changeset includes sys_scheduled changes (triggers subscriptions if any)

## Design Notes

- The sys_scheduled mutation is added to the transaction after the reducer handler returns but before commit. This ensures atomicity: if commit fails, both reducer writes and schedule mutation roll back together.
- Fixed-rate vs fixed-delay: fixed-rate is better for periodic tasks that should maintain a steady cadence. If a fire is delayed by 50ms, the next fire catches up. If it's delayed by more than one interval, multiple fires may become due simultaneously — the timer will enqueue them all.
- Crash semantics: at-least-once. If the executor commits (memory) but crashes before durable persistence, recovery replays from the last durable state. The schedule row still exists in durable state, so the reducer fires again.
