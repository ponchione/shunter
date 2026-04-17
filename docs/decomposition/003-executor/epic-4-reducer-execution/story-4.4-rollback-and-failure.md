# Story 4.4: Rollback & Failure Paths

**Epic:** [Epic 4 — Reducer Transaction Lifecycle](EPIC.md)  
**Spec ref:** SPEC-003 §4.5, §4.6  
**Depends on:** Story 4.2  
**Blocks:** Nothing

---

## Summary

Handle all non-commit outcomes: reducer error, reducer panic, and commit failure. Discard transaction, send appropriate response.

## Deliverables

- **Reducer error path** (`err != nil` from handler):
  - `store.Rollback(tx)`
  - Discard transaction (no committed state mutation)
  - Send response:
    ```go
    ReducerResponse{
        Status: StatusFailedUser,
        Error:  err,
    }
    ```

- **Reducer panic path** (`panicked != nil`):
  - `store.Rollback(tx)`
  - Discard transaction
  - Send response:
    ```go
    ReducerResponse{
        Status: StatusFailedPanic,
        Error:  fmt.Errorf("%v: %w", panicked, ErrReducerPanic),
    }
    ```

- **Commit failure path** (`commitErr != nil` from store.Commit):
  - `store.Rollback(tx)` is still called to release transaction-local state even though committed state is already guaranteed unchanged
  - Committed state unchanged (store guarantee)
  - Classify error:
    - User-visible constraint violation → `StatusFailedUser`
    - Internal/engine error → `StatusFailedInternal`
  - Send response:
    ```go
    ReducerResponse{
        Status: status,
        Error:  fmt.Errorf("commit: %w", commitErr),
    }
    ```

- Typed-adapter argument decode failures from SPEC-006 are treated as ordinary reducer errors in this story's status mapping: user-visible failure, rollback, no committed mutation.

- **Rollback cost:** O(1) disposal of transaction-local state. No undo operations on committed state.

## Acceptance Criteria

- [ ] Reducer returns error → `StatusFailedUser`, committed state unchanged
- [ ] Reducer panics → `StatusFailedPanic`, committed state unchanged, executor continues
- [ ] Commit fails with uniqueness error → `StatusFailedUser`
- [ ] Commit fails with internal error → `StatusFailedInternal`
- [ ] Failed commit leaves committed state exactly as before
- [ ] Error in response wraps original error for caller diagnosis
- [ ] No durability handoff on any failure path
- [ ] No subscription evaluation on any failure path
- [ ] Transaction is not retained after rollback (no memory leak)
- [ ] Malformed reducer args surfaced by a typed adapter produce `StatusFailedUser` and leave state unchanged
- [ ] **Benchmark:** rollback of failed reducer < 20 µs (§17)

## Design Notes

- Rollback is explicit at the store boundary: call `store.Rollback(tx)` on every pre-commit failure path so any transaction-local allocations, sequence reservations, and iterator state are released promptly. There is still no undo log against committed state.
- Distinguishing user-visible vs internal commit failures: SPEC-001 errors like `ErrPrimaryKeyViolation` and `ErrUniqueConstraintViolation` are user-visible (StatusFailedUser). Everything else is internal (StatusFailedInternal). The executor can use `errors.Is` checks against known SPEC-001 user-visible errors.
- Panic value is formatted with `%v` and wrapped with `ErrReducerPanic` sentinel for `errors.Is` matching.
