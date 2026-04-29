# Story 5.3: Fatal State Transition

**Epic:** [Epic 5 — Post-Commit Pipeline](EPIC.md)  
**Spec ref:** SPEC-003 §5.4  
**Depends on:** Story 5.1  
**Blocks:** Nothing

---

## Summary

If any post-commit step panics, the executor transitions to a fatal state and rejects future write-affecting commands until restart.

## Deliverables

- Modify `postCommit` to wrap all steps in a panic recovery:
  ```go
  func (e *Executor) postCommit(...) {
      defer func() {
          if r := recover(); r != nil {
              e.fatal = true
              // log fatal error with stack trace
              // attempt to send error response if not already sent
          }
      }()
      // ... pipeline steps ...
  }
  ```

- Fatal state check at top of `dispatch`:
  ```go
  func (e *Executor) dispatch(cmd ExecutorCommand) {
      if e.fatal && isWriteAffecting(cmd) {
          // send ErrExecutorFatal on cmd's ResponseCh
          return
      }
      // ... normal dispatch ...
  }
  ```

- After `fatal = true`:
  - future reducer / lifecycle / scheduled write-affecting commands get `ErrExecutorFatal`
  - No recovery possible without engine restart
  - Submit also checks `e.fatal` for write-affecting commands (Story 3.3)

## Acceptance Criteria

- [ ] Panic in EnqueueCommitted after commit → fatal state
- [ ] Panic in EvalAndBroadcast after commit → fatal state
- [ ] Panic in snapshot acquisition after commit → fatal state
- [ ] After fatal, future write-affecting commands receive `ErrExecutorFatal`
- [ ] After fatal, Submit of a write-affecting command returns `ErrExecutorFatal`
- [ ] Fatal flag is not set for pre-commit panics (those are handled by Story 4.4)
- [ ] Fatal state is permanent — no auto-recovery

## Design Notes

- The key invariant: once commit succeeds, the transaction is visible in memory and cannot be rolled back. If post-commit steps fail, we're in an inconsistent state (committed but not durably queued, or committed but subscriptions not evaluated). Continuing would risk silent data loss or reordering.
- This is deliberately harsh for v1. A production system might attempt partial recovery (e.g., if only subscription eval failed, retry it). v1 treats all post-commit failures as fatal because the recovery logic is complex and the failure modes are rare.
- Pre-commit panics (reducer panics) are NOT fatal — they're per-request failures that leave committed state unchanged.
- SPEC-003's contract is specifically about future write commands. Implementations may choose to reject a broader set of commands for simplicity, but the required minimum behavior is to reject write-affecting work.
