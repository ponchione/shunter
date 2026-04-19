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

> **Updated 2026-04-19 (Phase 2 Slice 2).** The single-subscription
> commands `RegisterSubscriptionCmd` / `UnregisterSubscriptionCmd`
> were replaced by the set-based commands below. One `QueryID`
> identifies a subscription set (`Predicates` length >= 1; length 1
> is the Single path).

- ```go
  type RegisterSubscriptionSetCmd struct {
      Request    SubscriptionSetRegisterRequest  // defined in SPEC-004 §4.1
      ResponseCh chan<- SubscriptionSetRegisterResult
  }
  func (RegisterSubscriptionSetCmd) isExecutorCommand() {}
  ```

- ```go
  type UnregisterSubscriptionSetCmd struct {
      ConnID     ConnectionID
      QueryID    uint32
      ResponseCh chan<- SubscriptionSetUnregisterResult
  }
  func (UnregisterSubscriptionSetCmd) isExecutorCommand() {}
  ```

- ```go
  type DisconnectClientSubscriptionsCmd struct {
      ConnID     ConnectionID
      ResponseCh chan<- error
  }
  func (DisconnectClientSubscriptionsCmd) isExecutorCommand() {}
  ```

- ```go
  // Lifecycle dispatch commands (SPEC-003 §2.4 / §10.3 / §10.4).
  // Bespoke because the sys_clients insert (OnConnect) and the
  // guaranteed cleanup tx (OnDisconnect) are not expressible through
  // CallReducerCmd.
  type OnConnectCmd struct {
      ConnID     ConnectionID
      Identity   Identity
      ResponseCh chan<- ReducerResponse
  }
  func (OnConnectCmd) isExecutorCommand() {}

  type OnDisconnectCmd struct {
      ConnID     ConnectionID
      Identity   Identity
      ResponseCh chan<- ReducerResponse
  }
  func (OnDisconnectCmd) isExecutorCommand() {}
  ```

## Acceptance Criteria

- [ ] All command types satisfy `ExecutorCommand` interface
- [ ] Each command type has an optional `ResponseCh` for async result delivery; nil is permitted when the caller intentionally discards the response
- [ ] CallReducerCmd carries full ReducerRequest
- [ ] RegisterSubscriptionSetCmd references SubscriptionSetRegisterRequest (SPEC-004 type)
- [ ] UnregisterSubscriptionSetCmd carries ConnectionID + QueryID; DisconnectClientSubscriptionsCmd carries ConnectionID
- [ ] OnConnectCmd and OnDisconnectCmd carry ConnectionID + Identity and are distinct from CallReducerCmd

## Design Notes

- `isExecutorCommand()` is unexported — prevents external packages from creating new command types.
- Scheduled reducers use `CallReducerCmd` with `CallSourceScheduled` as the Source. Lifecycle reducers (`OnConnect` / `OnDisconnect`) do NOT fit the `CallReducerCmd` shape — the `sys_clients` row insert (§10.3) and the guaranteed cleanup tx (§10.4) are not expressible through the plain reducer-call path — so they use `OnConnectCmd` / `OnDisconnectCmd` and `CallerContext.Source = CallSourceLifecycle` is stamped inside the executor.
- `SubscriptionSetRegisterRequest`, `SubscriptionSetRegisterResult`, and `SubscriptionSetUnregisterResult` types come from SPEC-004. Use placeholder types or imports until SPEC-004 is implemented.
- `ResponseCh` may be nil. In that case the executor silently drops the response after completing the command. Callers that require result delivery or visibility into failures MUST supply a channel and drain it.
