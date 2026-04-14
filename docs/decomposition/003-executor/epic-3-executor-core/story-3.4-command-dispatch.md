# Story 3.4: Command Dispatch

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.4, §2.5, §8, §13.4  
**Depends on:** Story 3.2  
**Blocks:** Story 4.1, Story 3.5

---

## Summary

The `dispatch()` type switch routing commands to handlers. Includes subscription command handlers that delegate to SubscriptionManager.

## Deliverables

- ```go
  func (e *Executor) dispatch(cmd ExecutorCommand)
  ```
  Complete type switch:
  - `CallReducerCmd` → `e.handleCallReducer(cmd)` (stub here, implemented in Epic 4)
  - `RegisterSubscriptionCmd` → `e.handleRegisterSubscription(cmd)`
  - `UnregisterSubscriptionCmd` → `e.handleUnregisterSubscription(cmd)`
  - `DisconnectClientSubscriptionsCmd` → `e.handleDisconnectClientSubscriptions(cmd)`
  - Unknown command type → log error, no-op

- ```go
  func (e *Executor) handleRegisterSubscription(cmd RegisterSubscriptionCmd)
  ```
  - Acquire `CommittedReadView` via `e.store.Snapshot()`
  - Call `e.subs.Register(cmd.Request, view)`
  - Close snapshot
  - Send result on `cmd.ResponseCh`

- ```go
  func (e *Executor) handleUnregisterSubscription(cmd UnregisterSubscriptionCmd)
  ```
  - Call `e.subs.Unregister(cmd.ConnID, cmd.SubscriptionID)`
  - Send error on `cmd.ResponseCh`

- ```go
  func (e *Executor) handleDisconnectClientSubscriptions(cmd DisconnectClientSubscriptionsCmd)
  ```
  - Call `e.subs.DisconnectClient(cmd.ConnID)`
  - Send error on `cmd.ResponseCh`

- Executor/package docs on read routing:
  - reads that must be atomic with subscription registration or commit ordering go through `RegisterSubscriptionCmd`
  - purely observational reads that do not need atomic registration semantics stay outside the executor queue and use direct `CommittedState.Snapshot()` by design

## Acceptance Criteria

- [ ] CallReducerCmd routed to handleCallReducer
- [ ] RegisterSubscriptionCmd acquires snapshot, calls Register, closes snapshot, sends result
- [ ] UnregisterSubscriptionCmd calls Unregister, sends error
- [ ] DisconnectClientSubscriptionsCmd calls DisconnectClient, sends error
- [ ] Register uses committed read view (not tx-local state)
- [ ] Snapshot closed even if Register returns error
- [ ] Unknown command type logged, not panicked
- [ ] Executor read-routing docs distinguish atomic registration reads from allowed direct-snapshot observational reads

## Design Notes

- RegisterSubscription acquires a snapshot inside the executor goroutine, guaranteeing atomicity with commit ordering. Between dequeue and snapshot acquisition, no other command runs — this is the §2.5 atomicity guarantee.
- Subscription commands do not create transactions. They are read-only or metadata operations delegated to SubscriptionManager.
- SPEC-003 explicitly allows direct snapshots for purely observational reads. This story should document that boundary so implementers do not accidentally funnel all reads through the executor.
- `handleCallReducer` is defined as a stub here (logs "not implemented") and replaced by Epic 4.
