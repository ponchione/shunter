# Story 3.2: Run Loop & Panic Envelope

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.2, §4.1  
**Depends on:** Story 3.1  
**Blocks:** Stories 3.3, 3.4

---

## Summary

The executor goroutine: receive commands from inbox, process one at a time with panic protection.

## Deliverables

- ```go
  func (e *Executor) Run(ctx context.Context)
  ```
  - Select on `ctx.Done()` and `e.inbox`
  - On context cancellation: return
  - On channel close (`!ok`): return
  - On command: call `e.dispatchSafely(cmd)`
  - Single goroutine — no concurrency within the loop

- ```go
  func (e *Executor) dispatchSafely(cmd ExecutorCommand)
  ```
  - `defer func() { if r := recover(); r != nil { e.handleDispatchPanic(cmd, r) } }()`
  - Calls `e.dispatch(cmd)`

- ```go
  func (e *Executor) handleDispatchPanic(cmd ExecutorCommand, r any)
  ```
  - Log the panic with stack trace
  - If the command has a ResponseCh, send an error response
  - Does NOT set `fatal` — that's only for post-commit panics (Epic 5)

- ```go
  func (e *Executor) dispatch(cmd ExecutorCommand)
  ```
  - Type switch on cmd
  - Stub implementations for now; Epic 4 fills in reducer calls, Epic 3.4 fills in subscription commands

## Acceptance Criteria

- [ ] Run processes commands sequentially (one at a time)
- [ ] Context cancellation terminates Run
- [ ] Closed inbox terminates Run without spin
- [ ] Panic in dispatch → executor survives, processes next command
- [ ] Panic recovery sends error response to command's ResponseCh if present
- [ ] No goroutine leak after Run returns
- [ ] **Benchmark:** dequeue + begin empty transaction < 5 µs (§12)

## Design Notes

- `dispatchSafely` is the outer envelope protecting the executor goroutine. Individual reducer panic recovery (Epic 4, Story 4.2) is a separate inner layer.
- The two-layer panic recovery is intentional: inner layer handles expected reducer panics gracefully; outer layer catches unexpected bugs in executor infrastructure itself.
