# Story 6.3: Timer Goroutine & Wakeup

**Epic:** [Epic 6 — Scheduled Reducers](EPIC.md)  
**Spec ref:** SPEC-003 §9.4  
**Depends on:** Story 6.2  
**Blocks:** Story 6.4

---

## Summary

Background goroutine that scans for due schedules and enqueues internal reducer calls into the executor inbox.

## Deliverables

- ```go
  type Scheduler struct {
      executor    *Executor
      tableID     TableID
      wakeup      chan struct{}   // signal to rescan
      ctx         context.Context
      cancel      context.CancelFunc
  }
  ```

- ```go
  func (s *Scheduler) Run(ctx context.Context)
  ```
  - Loop:
    1. Scan `sys_scheduled` for rows where `next_run_at_ns <= now`
    2. For each due row, enqueue `CallReducerCmd` with `CallSourceScheduled`
    3. Compute next wakeup time from earliest future `next_run_at_ns`
    4. Sleep until min(next wakeup, rescan signal)
  - `select` on: timer expiry, `s.wakeup` channel, `ctx.Done()`

- ```go
  func (s *Scheduler) Notify()
  ```
  - Non-blocking send to `s.wakeup` channel
  - Called after any commit that might have changed sys_scheduled

- Internal command construction:
  ```go
  CallReducerCmd{
      Request: ReducerRequest{
          ReducerName: row.reducer_name,
          Args:        row.args,
          Source:      CallSourceScheduled,
          Caller: CallerContext{
              // internal caller: zero ConnectionID, system Identity
          },
      },
      ResponseCh: internalResponseCh,
  }
  ```

## Acceptance Criteria

- [ ] Due schedule enqueued as CallReducerCmd with CallSourceScheduled
- [ ] Timer wakes up at next due time (not polling)
- [ ] Notify() triggers immediate rescan
- [ ] Context cancellation stops timer goroutine
- [ ] Multiple due schedules all enqueued in one scan
- [ ] Schedule in the future not enqueued until due
- [ ] **Benchmark:** schedule wakeup to executor enqueue < 10 ms (§12)

## Design Notes

- The timer reads `sys_scheduled` via a committed state snapshot (read-only, no transaction needed). This is safe because the timer only needs to see committed schedules.
- The timer does not delete or modify `sys_scheduled` rows. Firing semantics (delete/advance) happen inside the scheduled reducer's transaction (Story 6.4).
- Notify is called after each commit by the post-commit pipeline. This ensures newly created schedules are picked up promptly.
- v1 simplification: the timer can do a full table scan of `sys_scheduled`. For large schedule tables, an index on `next_run_at_ns` would help — deferred to v2.
