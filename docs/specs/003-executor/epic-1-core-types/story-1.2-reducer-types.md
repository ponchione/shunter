# Story 1.2: Reducer Types

**Epic:** [Epic 1 — Core Types & Command Model](EPIC.md)  
**Spec ref:** SPEC-003 §3.1–§3.4  
**Depends on:** Story 1.1  
**Blocks:** Stories 1.3, Epic 2

---

## Summary

Type definitions for the reducer runtime: handler signature, registration struct, call request/response, and execution context.

**Go-package home.** All types in this story live in the `types/` package (`types/reducer.go`) — the canonical symbol home (SPEC-003 §3.1). SPEC-006 re-exports `ReducerHandler` / `ReducerContext` for ergonomic builder call sites; it does not redeclare them. `Identity`, `ConnectionID`, and `TxID` are imported from `types/types.go` (SPEC-001 §1.1, §2.4), never re-declared here.

## Deliverables

- ```go
  type ReducerHandler func(ctx *ReducerContext, argBSATN []byte) ([]byte, error)
  ```
  Byte-oriented runtime signature. SPEC-006 may wrap with typed adapters.

- ```go
  type RegisteredReducer struct {
      Name      string
      Handler   ReducerHandler
      Lifecycle LifecycleKind
  }
  ```

- ```go
  type CallerContext struct {
      Identity     Identity       // from SPEC-001
      ConnectionID ConnectionID   // zero for internal callers
      Timestamp    time.Time      // set at dequeue, not caller-provided
  }
  ```

- ```go
  type ReducerRequest struct {
      ReducerName    string
      Args           []byte
      Caller         CallerContext
      Source         CallSource
      ScheduleID     ScheduleID // populated iff Source == CallSourceScheduled
      IntendedFireAt int64      // unix nanos; populated iff Source == CallSourceScheduled
  }
  ```

- ```go
  type ReducerResponse struct {
      Status      ReducerStatus
      Error       error
      ReturnBSATN []byte
      TxID        TxID
  }
  ```

- ```go
  type ReducerContext struct {
      ReducerName string
      Caller      CallerContext
      DB          *Transaction   // from SPEC-001
      Scheduler   SchedulerHandle
  }
  ```

## Acceptance Criteria

- [ ] ReducerHandler signature accepts `*ReducerContext` and `[]byte`, returns `([]byte, error)`
- [ ] RegisteredReducer has Name, Handler, and Lifecycle fields
- [ ] CallerContext.ConnectionID is zero-value for internal callers
- [ ] ReducerResponse carries Status, Error, optional ReturnBSATN, and TxID
- [ ] ReducerRequest carries `ScheduleID` and `IntendedFireAt` for scheduled calls; both stay zero-valued for non-scheduled sources
- [ ] ReducerContext references Transaction and SchedulerHandle (interfaces from SPEC-001 and Story 1.4)

## Design Notes

- ReducerContext is valid only during synchronous reducer invocation. Enforcement is by contract, not runtime check.
- CallerContext.Timestamp is set by the executor at dequeue time (Epic 4), not by the caller. Caller-supplied timestamps are ignored. Only the UTC wall-clock portion is meaningful on the wire or in logs; Go's monotonic component is process-local and must be stripped outside the executor process.
- For `CallSourceScheduled`, the scheduler populates both `ScheduleID` and `IntendedFireAt` so the executor can mutate the correct `sys_scheduled` row with fixed-rate semantics. Other call sources leave those fields zero-valued.
- `Identity` and `ConnectionID` are declared in `types/types.go` (SPEC-001 §2.4, §1.1). This story imports them; no placeholder aliases are needed once Story 1.6 of SPEC-001 has landed.
