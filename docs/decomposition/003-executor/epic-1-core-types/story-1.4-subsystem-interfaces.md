# Story 1.4: Subsystem Interfaces

**Epic:** [Epic 1 — Core Types & Command Model](EPIC.md)  
**Spec ref:** SPEC-003 §7, §8, §9.3  
**Depends on:** Story 1.1  
**Blocks:** Epic 3, Epic 5, Epic 6

---

## Summary

Interfaces the executor consumes from other subsystems: durability, subscriptions, and scheduler. Defined here, implemented elsewhere.

## Deliverables

- ```go
  type DurabilityHandle interface {
      EnqueueCommitted(txID TxID, changeset *Changeset)
      DurableTxID() TxID
      Close() (TxID, error)
  }
  ```
  - `EnqueueCommitted` blocks only for bounded-queue backpressure, not fsync
  - Must not drop accepted commits silently
  - If durability has latched a fatal error, `EnqueueCommitted` must panic immediately
  - Implemented by SPEC-002

- ```go
  type SubscriptionManager interface {
      Register(req SubscriptionRegisterRequest, view CommittedReadView) (SubscriptionRegisterResult, error)
      Unregister(connID ConnectionID, subscriptionID SubscriptionID) error
      DisconnectClient(connID ConnectionID) error
      EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView)
      DroppedClients() <-chan ConnectionID
  }
  ```
  - `Register` must be called from executor command (atomic with commit ordering)
  - `EvalAndBroadcast` runs synchronously in post-commit pipeline
  - `DroppedClients` returns a channel for non-blocking drain
  - Implemented by SPEC-004

- ```go
  type SchedulerHandle interface {
      Schedule(reducerName string, args []byte, at time.Time) (ScheduleID, error)
      ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error)
      Cancel(id ScheduleID) bool
  }
  ```
  - Operations are transactional — roll back with surrounding reducer
  - Implemented in Epic 6

## Acceptance Criteria

- [ ] DurabilityHandle has EnqueueCommitted, DurableTxID, Close methods
- [ ] SubscriptionManager has Register, Unregister, DisconnectClient, EvalAndBroadcast, DroppedClients methods
- [ ] SchedulerHandle has Schedule, ScheduleRepeat, Cancel methods
- [ ] All interfaces compile against their method signatures
- [ ] Changeset and CommittedReadView types referenced from SPEC-001

## Design Notes

- DurabilityHandle is also defined in SPEC-002 §4. Both specs describe the same contract from opposite sides. Implementation defines it once; whichever package is implemented first creates the interface.
- SubscriptionManager's `DroppedClients()` returns `<-chan ConnectionID` — a receive-only channel. Executor drains it non-blocking after each commit (Epic 5).
- SchedulerHandle mutations are logically part of the current transaction's writes. If the reducer rolls back, schedule mutations are discarded. This transactional coupling is implemented in Epic 6, not enforced by the interface itself.
