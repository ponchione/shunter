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
      timerNotify func()  // signal timer to rescan after commit
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
    - `next_run_at_ns` = now + interval (or first fire time)
    - `repeat_ns` = interval.Nanoseconds()

- ```go
  func (h *schedulerHandle) Cancel(id ScheduleID) bool
  ```
  - Delete row from `sys_scheduled` where `schedule_id = id`
  - Return true if row existed and was deleted

- Transactional guarantee: since all operations go through `h.tx`, if the surrounding reducer rolls back, these mutations are discarded.

## Acceptance Criteria

- [ ] Schedule inserts row with correct fields, returns ScheduleID
- [ ] ScheduleRepeat inserts row with repeat_ns > 0
- [ ] Cancel deletes row, returns true
- [ ] Cancel non-existent ID returns false
- [ ] Reducer rollback → schedule insert discarded
- [ ] Reducer rollback → cancel reverted (row still present)
- [ ] ScheduleID is autoincremented by sys_scheduled's sequence

## Design Notes

- `timerNotify` is called after the surrounding transaction commits (not during Schedule/Cancel). The post-commit pipeline or the executor itself calls it. This is wired in Story 6.3.
- The SchedulerHandle is constructed per-reducer-invocation as part of ReducerContext (Story 4.1). It holds a reference to the active transaction.
- Validation (e.g., reducer name exists) could be done here or deferred to firing time. v1 recommendation: validate at schedule time for better DX.
