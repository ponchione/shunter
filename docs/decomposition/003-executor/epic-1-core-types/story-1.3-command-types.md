# Story 1.3: Command Types

**Epic:** [Epic 1 — Core Types & Command Model](EPIC.md)  
**Spec ref:** SPEC-003 §2.2–§2.4  
**Depends on:** Stories 1.1, 1.2  
**Blocks:** Epic 3

---

## Summary

The `ExecutorCommand` interface and all concrete command types that flow through the executor inbox.

## Deliverables

- ```go
  type ExecutorCommand interface {
      isExecutorCommand()
  }
  ```

- ```go
  type CallReducerCmd struct {
      Request    ReducerRequest
      ResponseCh chan<- ReducerResponse
  }
  func (CallReducerCmd) isExecutorCommand() {}
  ```

- ```go
  type RegisterSubscriptionCmd struct {
      Request    SubscriptionRegisterRequest  // defined in SPEC-004 §4.1
      ResponseCh chan<- SubscriptionRegisterResult
  }
  func (RegisterSubscriptionCmd) isExecutorCommand() {}
  ```

- ```go
  type UnregisterSubscriptionCmd struct {
      ConnID         ConnectionID
      SubscriptionID SubscriptionID
      ResponseCh     chan<- error
  }
  func (UnregisterSubscriptionCmd) isExecutorCommand() {}
  ```

- ```go
  type DisconnectClientSubscriptionsCmd struct {
      ConnID     ConnectionID
      ResponseCh chan<- error
  }
  func (DisconnectClientSubscriptionsCmd) isExecutorCommand() {}
  ```

## Acceptance Criteria

- [ ] All command types satisfy `ExecutorCommand` interface
- [ ] Each command type has a `ResponseCh` for async result delivery
- [ ] CallReducerCmd carries full ReducerRequest
- [ ] RegisterSubscriptionCmd references SubscriptionRegisterRequest (SPEC-004 type)
- [ ] UnregisterSubscriptionCmd and DisconnectClientSubscriptionsCmd carry ConnectionID

## Design Notes

- `isExecutorCommand()` is unexported — prevents external packages from creating new command types.
- Scheduled reducers and lifecycle reducers use `CallReducerCmd` with `CallSourceScheduled` or `CallSourceLifecycle` as the Source. No special command types needed.
- `SubscriptionRegisterRequest` and `SubscriptionRegisterResult` types come from SPEC-004. Use placeholder types or imports until SPEC-004 is implemented.
