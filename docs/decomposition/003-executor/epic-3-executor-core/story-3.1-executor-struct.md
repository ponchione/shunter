# Story 3.1: Executor Struct & Constructor

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.1–§2.2, §6  
**Depends on:** Epic 1 (types, interfaces), Epic 2 (registry)  
**Blocks:** Story 3.2

---

## Summary

Executor struct holding all owned state. Constructor with configurable inbox capacity and TxID initialization from recovery.

## Deliverables

- ```go
  type Executor struct {
      inbox       chan ExecutorCommand
      registry    *ReducerRegistry
      store       *CommittedState       // from SPEC-001
      durability  DurabilityHandle
      subs        SubscriptionManager
      scheduler   *Scheduler            // from Epic 6; nil until wired
      nextTxID    TxID
      fatal       bool                  // true after post-commit panic
      rejectMode  bool                  // true = ErrExecutorBusy; false = block
  }
  ```

- ```go
  type ExecutorConfig struct {
      InboxCapacity int
      RejectOnFull  bool
  }
  ```

- ```go
  func NewExecutor(
      cfg ExecutorConfig,
      registry *ReducerRegistry,
      store *CommittedState,
      durability DurabilityHandle,
      subs SubscriptionManager,
      recoveredTxID TxID,
  ) *Executor
  ```
  - `inbox` = `make(chan ExecutorCommand, cfg.InboxCapacity)`
  - `nextTxID` = `recoveredTxID + 1` (if recoveredTxID is 0, first commit gets TxID 1)
  - Registry must be frozen before construction

## Acceptance Criteria

- [ ] NewExecutor creates bounded channel with given capacity
- [ ] nextTxID = recoveredTxID + 1
- [ ] recoveredTxID=0 → first commit assigns TxID 1
- [ ] recoveredTxID=500 → first commit assigns TxID 501
- [ ] Panics if registry is not frozen
- [ ] InboxCapacity must be > 0

## Design Notes

- `fatal` flag is checked at the top of every dispatch. Set by Epic 5 (post-commit pipeline) on unrecoverable error.
- `scheduler` is nil initially; wired in Epic 6. The executor doesn't depend on it for basic operation.
- SPEC-002 `OpenAndRecover` returns `maxAppliedTxID`. That value is passed as `recoveredTxID` here.
