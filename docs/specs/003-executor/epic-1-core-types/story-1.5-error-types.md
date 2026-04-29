# Story 1.5: Error Types

**Epic:** [Epic 1 — Core Types & Command Model](EPIC.md)  
**Spec ref:** SPEC-003 §11  
**Depends on:** Nothing  
**Blocks:** Epics 3, 4, 5

---

## Summary

Sentinel error values for the executor error catalog. Defined centrally, returned by later epics.

## Deliverables

```go
var (
    ErrReducerNotFound  = errors.New("reducer not found")
    ErrLifecycleReducer = errors.New("cannot invoke lifecycle reducer externally")
    ErrExecutorBusy     = errors.New("executor inbox full")
    ErrExecutorShutdown = errors.New("executor is shut down")
    ErrReducerPanic     = errors.New("reducer panicked")
    ErrCommitFailed     = errors.New("commit failed")
    ErrExecutorFatal    = errors.New("executor in fatal state")
)
```

## Acceptance Criteria

- [ ] All 7 error sentinels defined
- [ ] Each distinguishable via `errors.Is`
- [ ] Error messages are concise and distinct
- [ ] Errors are `var` sentinels (not typed errors) — wrapping adds context at call sites

## Design Notes

- Sentinel errors are simple for v1. If structured error info is needed later (e.g., which reducer panicked, what constraint failed), wrap with `fmt.Errorf("...: %w", ErrReducerPanic)` at call sites rather than creating typed error structs now.
- `ErrCommitFailed` wraps the underlying store error at the call site. The sentinel identifies the category; the wrapped error provides detail.
