# Story 3.4: Command Dispatch

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.4, §2.5, §8, §13.4  
**Depends on:** Story 3.2  
**Blocks:** Story 4.1, Story 3.5

---

## Summary

The `dispatch()` type switch routing commands to handlers. Includes subscription command handlers that delegate to SubscriptionManager.

## Deliverables

> **Updated 2026-04-19 (Phase 2 Slice 2).** Dispatch routes the set-based
> subscription commands instead of the former single-subscription
> commands.

- ```go
  func (e *Executor) dispatch(cmd ExecutorCommand)
  ```
  Complete type switch:
  - `CallReducerCmd` → `e.handleCallReducer(cmd)` (stub here, implemented in Epic 4)
  - `RegisterSubscriptionSetCmd` → `e.handleRegisterSubscriptionSet(cmd)`
  - `UnregisterSubscriptionSetCmd` → `e.handleUnregisterSubscriptionSet(cmd)`
  - `DisconnectClientSubscriptionsCmd` → `e.handleDisconnectClientSubscriptions(cmd)`
  - Unknown command type → log error, no-op

- ```go
  func (e *Executor) handleRegisterSubscriptionSet(cmd RegisterSubscriptionSetCmd)
  ```
  - Acquire `CommittedReadView` via `e.committed.Snapshot()`
  - Call `e.subs.RegisterSet(cmd.Request, view)`
  - `SubscriptionManager` may use `view` only for the duration of the call; any retained state must be copied before return
  - Close snapshot
  - Send result on `cmd.ResponseCh`

- ```go
  func (e *Executor) handleUnregisterSubscriptionSet(cmd UnregisterSubscriptionSetCmd)
  ```
  - Call `e.subs.UnregisterSet(cmd.ConnID, cmd.QueryID, view)`
  - Send result on `cmd.ResponseCh`

- ```go
  func (e *Executor) handleDisconnectClientSubscriptions(cmd DisconnectClientSubscriptionsCmd)
  ```
  - Call `e.subs.DisconnectClient(cmd.ConnID)`
  - Send error on `cmd.ResponseCh`

- Executor/package docs on read routing (this story is the authoritative owner for that documentation boundary):
  - reads that must be atomic with subscription registration or commit ordering go through `RegisterSubscriptionSetCmd`
  - purely observational reads that do not need atomic registration semantics stay outside the executor queue and use direct `CommittedState.Snapshot()` by design

## Acceptance Criteria

- [ ] CallReducerCmd routed to handleCallReducer
- [ ] RegisterSubscriptionSetCmd acquires snapshot, calls RegisterSet, closes snapshot, sends result
- [ ] UnregisterSubscriptionSetCmd calls UnregisterSet, sends result
- [ ] DisconnectClientSubscriptionsCmd calls DisconnectClient, sends error
- [ ] RegisterSet uses committed read view (not tx-local state)
- [ ] Snapshot closed even if RegisterSet returns error
- [ ] Unknown command type logged, not panicked
- [ ] Executor read-routing docs distinguish atomic registration reads from allowed direct-snapshot observational reads

## Design Notes

- RegisterSubscriptionSet acquires a snapshot inside the executor goroutine, guaranteeing atomicity with commit ordering. Between dequeue and snapshot acquisition, no other command runs — this is the §2.5 atomicity guarantee.
- Subscription commands do not create transactions. They are read-only or metadata operations delegated to SubscriptionManager.
- This story is the canonical home for SPEC-003's read-routing rule. Other stories may cross-reference it, but they should not restate a competing policy.
- SPEC-003 explicitly allows direct snapshots for purely observational reads. This story should document that boundary so implementers do not accidentally funnel all reads through the executor.
- `handleCallReducer` is defined as a stub here (logs "not implemented") and replaced by Epic 4.
