# Story 1.1: Foundation Types

**Epic:** [Epic 1 — Core Types & Command Model](EPIC.md)  
**Spec ref:** SPEC-003 §6, §3.3, §10.1  
**Depends on:** Nothing  
**Blocks:** Stories 1.2, 1.3, 1.4

---

## Summary

Named types, numeric IDs, and enums that everything else builds on.

## Deliverables

- `type TxID uint64`
  - TxID(0) means "no committed transaction" / bootstrap
  - Starts at 1 for first committed transaction

- `type ScheduleID uint64`

- `type SubscriptionID uint32`
  - Client-chosen, unique within a connection (matches SPEC-005 wire format)

- ```go
  type CallSource int
  const (
      CallSourceExternal  CallSource = iota
      CallSourceScheduled
      CallSourceLifecycle
  )
  ```

- ```go
  type ReducerStatus int
  const (
      StatusCommitted      ReducerStatus = iota
      StatusFailedUser
      StatusFailedPanic
      StatusFailedInternal
  )
  ```

- ```go
  type LifecycleKind int
  const (
      LifecycleNone        LifecycleKind = iota
      LifecycleOnConnect
      LifecycleOnDisconnect
  )
  ```

## Acceptance Criteria

- [ ] TxID(0) is distinct zero value
- [ ] All CallSource values are distinct
- [ ] All ReducerStatus values are distinct
- [ ] All LifecycleKind values are distinct
- [ ] SubscriptionID is uint32 (not uint64)

## Design Notes

- TxID counter initialization from recovery is handled in Epic 3 (executor constructor). This story only defines the type.
- SubscriptionID is uint32 for executor/subscription-internal bookkeeping and deterministic fanout ordering. Client-visible subscription correlation on SPEC-005 wire messages uses QueryID.
