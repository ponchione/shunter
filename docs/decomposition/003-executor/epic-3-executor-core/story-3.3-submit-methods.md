# Story 3.3: Submit Methods & Backpressure

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.2  
**Depends on:** Story 3.2  
**Blocks:** Story 3.5

---

## Summary

Methods to submit commands to the executor inbox. Support both blocking and reject-on-full policies.

## Deliverables

- ```go
  func (e *Executor) Submit(cmd ExecutorCommand) error
  ```
  - Acquire `e.submitMu.RLock()` before checking shutdown / fatal state and before attempting the channel send; release after the send decision is complete
  - If `e.fatal` and `cmd` is write-affecting → return `ErrExecutorFatal`
  - If inbox is closed or shutdown flag is set → return `ErrExecutorShutdown`
  - If `e.rejectMode`:
    - Non-blocking send; if full → return `ErrExecutorBusy`
  - Else:
    - Blocking send (blocks caller until space available)
  - Return nil on success

- ```go
  func (e *Executor) SubmitWithContext(ctx context.Context, cmd ExecutorCommand) error
  ```
  - Same as Submit but also respects caller's context for cancellation while waiting
  - Hold `e.submitMu.RLock()` across the shutdown/fatal checks and the `select` that races `ctx.Done()` with the inbox send

## Acceptance Criteria

- [ ] Submit on non-full inbox → command enqueued, nil returned
- [ ] Submit on full inbox, rejectMode=true → `ErrExecutorBusy`
- [ ] Submit on full inbox, rejectMode=false → blocks until space
- [ ] Submit of a write-affecting command after fatal → `ErrExecutorFatal`
- [ ] Submit after shutdown → `ErrExecutorShutdown`
- [ ] SubmitWithContext cancelled while blocking → returns context error
- [ ] Concurrent Submit vs Shutdown does not panic or send on a closed channel
- [ ] Submitted command is received by Run loop

## Design Notes

- Shutdown detection must be synchronized with the eventual channel close. The minimum v1 contract is a lock or equivalent mechanism that makes "check shutdown/fatal → attempt send" atomic with respect to Shutdown's close path; `submitMu sync.RWMutex` is one acceptable shape.
- Fatal-state rejection must match SPEC-003's minimum contract: future write-affecting work is rejected. Implementations may reject more broadly, but the decomposition should not require that stronger behavior by default.
- `SubmitWithContext` is the preferred API for external callers (protocol layer). Internal callers (scheduler, lifecycle) may use `Submit` directly since they run in controlled contexts.
- Spec recommends "block by default" — `rejectMode` is an advanced configuration knob.
