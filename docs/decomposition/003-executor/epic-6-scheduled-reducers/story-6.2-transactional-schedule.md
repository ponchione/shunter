# Story 6.2: Transactional Schedule & Cancel

**Epic:** [Epic 6 — Scheduled Reducers](EPIC.md)  
**Spec ref:** SPEC-003 §9.3  
**Depends on:** Story 6.1  
**Blocks:** Story 6.3

---

## Summary

SchedulerHandle implementation: Schedule, ScheduleRepeat, and Cancel as transactional operations on sys_scheduled. Roll back with the surrounding transaction.

## Deliverables

- ```go
  type schedulerHandle struct {
      tx          *Transaction
      tableID     TableID
      timerNotify func()  // optional signal to reduce rescan latency after commit
  }
  ```

- ```go
  func (h *schedulerHandle) Schedule(reducerName string, args []byte, at time.Time) (ScheduleID, error)
  ```
  - Insert row into `sys_scheduled` via `h.tx`:
    - `reducer_name` = reducerName
    - `args` = args
    - `next_run_at_ns` = at.UnixNano()
    - `repeat_ns` = 0
  - Return allocated schedule_id (autoincrement)

- ```go
  func (h *schedulerHandle) ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error)
  ```
  - Insert row with:
    - `next_run_at_ns` = now + interval
    - `repeat_ns` = interval.Nanoseconds()

- ```go
  func (h *schedulerHandle) Cancel(id ScheduleID) (bool, error)
  ```
  - Delete row from `sys_scheduled` where `schedule_id = id`
  - Return `(true, nil)` if the row existed and was deleted
  - Return `(false, nil)` if the schedule was not found
  - Return `(false, err)` if a matching row was found but delete failed

- Transactional guarantee: since all operations go through `h.tx`, if the surrounding reducer rolls back, these mutations are discarded.

## Acceptance Criteria

- [ ] Schedule inserts row with correct fields, returns ScheduleID
- [ ] ScheduleRepeat inserts row with repeat_ns > 0
- [ ] Cancel deletes row, returns `(true, nil)`
- [ ] Cancel non-existent ID returns `(false, nil)`
- [ ] Cancel delete failure returns non-nil error without collapsing into “not found”
- [ ] Reducer rollback → schedule insert discarded
- [ ] Reducer rollback → cancel reverted (row still present)
- [ ] ScheduleID is autoincremented by sys_scheduled's sequence

## Design Notes

- A post-commit wakeup/notify hook is an optional latency optimization, not a correctness requirement. The minimum v1 contract is that committed-state rescans eventually observe newly inserted or cancelled schedules.
- The SchedulerHandle is constructed per-reducer-invocation as part of ReducerContext (Story 4.1). It holds a reference to the active transaction.
- Validation (e.g., reducer name exists) could be done here or deferred to firing time. v1 recommendation: validate at schedule time for better DX.
- `ScheduleRepeat` has one first-fire policy in v1: `now + interval`. A separate "first fire at X, then repeat every interval" API is deferred.
