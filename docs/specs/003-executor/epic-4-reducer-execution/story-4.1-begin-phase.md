# Story 4.1: Begin Phase

**Epic:** [Epic 4 — Reducer Transaction Lifecycle](EPIC.md)  
**Spec ref:** SPEC-003 §4.2, §3.4  
**Depends on:** Story 3.4, SPEC-001 (NewTransaction)  
**Blocks:** Story 4.2

---

## Summary

First phase of reducer dispatch: look up reducer, construct CallerContext with dequeue timestamp, create Transaction and ReducerContext.

## Deliverables

- ```go
  func (e *Executor) handleCallReducer(cmd CallReducerCmd)
  ```
  Top-level handler for CallReducerCmd. Orchestrates begin → execute → commit/rollback.

- Begin phase steps:
  1. Check lifecycle guard: if reducer name is a lifecycle name and `cmd.Request.Source != CallSourceLifecycle` → respond `ErrLifecycleReducer`, return
  2. Look up reducer in registry → if not found, respond `ErrReducerNotFound` with `StatusFailedInternal`, return
  3. Construct `CallerContext`:
     - `Identity` and `ConnectionID` from `cmd.Request.Caller`
     - `Timestamp` = `time.Now().UTC()` (dequeue time, ignoring any caller-provided timestamp)
  4. Create `Transaction` from committed state: `store.NewTransaction(e.committed, schema)`
  5. Build `ReducerContext`:
     ```go
     ctx := &ReducerContext{
         ReducerName: req.ReducerName,
         Caller:      callerCtx,
         DB:          tx,
         Scheduler:   e.scheduler.Handle(),
     }
     ```
  6. Proceed to execute phase (Story 4.2)

## Acceptance Criteria

- [ ] External call to lifecycle reducer name → `ErrLifecycleReducer` response, no Transaction created
- [ ] Unknown reducer name → `ErrReducerNotFound` + `StatusFailedInternal` response, no Transaction created
- [ ] CallerContext.Timestamp is `time.Now().UTC()`, not caller-supplied
- [ ] Transaction created from current committed state
- [ ] ReducerContext has correct ReducerName, Caller, DB, Scheduler
- [ ] **Benchmark:** empty reducer dispatch (begin + execute no-op + commit empty) < 10 µs (§17)

## Design Notes

- Lifecycle guard check happens before Transaction creation — fail fast, no wasted work.
- Timestamp is set at dequeue time, not at submit time. This means queued commands may have timestamps slightly later than when the caller submitted them. This is correct per spec: the executor, not the caller, owns the timestamp.
- If scheduler is nil (not yet wired), SchedulerHandle can be a no-op stub that returns errors. Epic 6 replaces it.
