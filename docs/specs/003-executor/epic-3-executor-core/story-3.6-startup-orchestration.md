# Story 3.6: Startup Orchestration & Recovery Handoff

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.5, §6, §10.6, §13.2, §13.5  
**Depends on:** Stories 3.1, 3.4, 3.5, Epic 6 Story 6.5, Epic 7 Story 7.5, SPEC-002 recovery, SPEC-006 §5.2  
**Blocks:** first external command acceptance

---

## Summary

Owner story for the engine-side startup sequence that hands recovered state into the executor, replays scheduler state, sweeps dangling clients, and only then begins accepting external commands.

## Deliverables

- Document the executor-adjacent startup order after SPEC-006's freeze/registry boot steps:
  1. recovery completes and returns recovered committed state plus `max_applied_tx_id`
  2. construct / initialize executor with the recovered committed state and recovered TxID hand-off
  3. `Scheduler.ReplayFromCommitted(...)`
  4. run the dangling-client sweep from Story 7.5
  5. start scheduler run loop
  6. start executor run loop
  7. only now may protocol / external callers submit commands

- `max_applied_tx_id` hand-off contract:
  - the recovered value from SPEC-002 is the authoritative input to executor TxID initialization
  - first external acceptance is gated on that value being installed
  - no external command may race ahead of recovery or scheduler replay

- Shutdown cross-link:
  - this story owns the startup half of the lifecycle contract; Story 3.5 owns the shutdown half
  - both together define the engine-level ordering around durability and command admission

## Acceptance Criteria

- [ ] Startup sequence names recovery, executor initialization, scheduler replay, dangling-client sweep, scheduler run, executor run, first accept in that order
- [ ] `max_applied_tx_id` hand-off from SPEC-002 is owned here rather than implied indirectly by constructor prose alone
- [ ] No external reducer or subscription-registration command may be admitted before scheduler replay and dangling-client sweep finish
- [ ] Story cross-links SPEC-006 §5.2 as the higher-level boot owner rather than redefining freeze/order policy independently
- [ ] Story cross-links Story 3.5 for shutdown ordering so startup/shutdown ownership is explicit

## Design Notes

- SPEC-006 §5.2 remains the top-level engine boot authority. This story owns only the executor-side sub-sequence after recovery has produced committed state.
- The scheduler replay and dangling-client sweep are both correctness steps, not optional warmup. Running them after first accept would let external work interleave ahead of past-due schedules or stale `sys_clients` cleanup.
- This story intentionally centralizes the `max_applied_tx_id` hand-off so Story 3.1 does not have to carry full engine-startup orchestration by itself.
