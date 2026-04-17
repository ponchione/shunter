# Story 5.1: Ordered Pipeline

**Epic:** [Epic 5 — Post-Commit Pipeline](EPIC.md)  
**Spec ref:** SPEC-003 §5.1–§5.3  
**Depends on:** Story 4.3  
**Blocks:** Stories 5.2, 5.3

---

## Summary

The strict post-commit step ordering: durability handoff, snapshot acquisition, subscription evaluation, snapshot release, response delivery. Success is acknowledged before fsync, so crash-loss semantics are part of this story's contract.

## Deliverables

- ```go
  func (e *Executor) postCommit(txID TxID, changeset *Changeset, ret []byte, responseCh chan<- ReducerResponse)
  ```
  Steps in exact order:
  1. `e.durability.EnqueueCommitted(txID, changeset)` — queue admission, not fsync
  2. `view := e.committed.Snapshot()` — acquire stable committed read view
  3. `e.subs.EvalAndBroadcast(txID, changeset, view, meta)` — synchronous subscription evaluation; `meta` is a `subscription.PostCommitMeta` built from `DurabilityHandle.WaitUntilDurable(txID)`, caller conn (if any), and `CallerResult` placeholder (§5, SPEC-004 §10.1)
  4. `view.Close()` — release read view
  5. Send `ReducerResponse{Status: StatusCommitted, TxID: txID, ReturnBSATN: ret}` on `responseCh`
  6. Drain dropped clients (Story 5.2)

- Snapshot lifetime: acquired after durability handoff, released before response delivery. Held for entire subscription evaluation duration.

- Synchronous evaluation: executor does not dequeue next command until EvalAndBroadcast returns and deltas are handed to protocol layer.

- Best-effort durability semantics:
  - `EnqueueCommitted` is bounded-queue admission, not fsync completion
  - reducer success response and delta handoff may occur before the commit is durable on disk
  - crash recovery may therefore lose recently acknowledged transactions that never became durable

## Acceptance Criteria

- [ ] Durability handoff happens before subscription evaluation
- [ ] Snapshot acquired after durability handoff
- [ ] EvalAndBroadcast receives committed read view (not tx-local state)
- [ ] Snapshot closed after EvalAndBroadcast, before response
- [ ] Response sent after evaluation completes
- [ ] Durability backpressure in EnqueueCommitted stalls pipeline (does not skip)
- [ ] No command dequeued until full pipeline completes
- [ ] Pipeline called for every successful commit (reducer, scheduled, lifecycle)
- [ ] Acknowledged commit before durability is an allowed state: response/delta delivery does not imply fsync completion
- [ ] Crash after response but before durability can lose the transaction after restart; this behavior is covered by verification/integration tests with SPEC-002

## Design Notes

- v1 tradeoff: synchronous evaluation is correct but limits throughput. For SodorYard's target workload (<10 subscriptions, <20ms evaluation) this is acceptable. High-subscription workloads would need async delta pipelines — out of scope for v1.
- EnqueueCommitted may block if durability worker is backpressured. This is intentional: the executor must not drop committed changesets.
- The snapshot is acquired AFTER durability handoff so that durability gets the changeset as early as possible. The snapshot itself reads committed state which already reflects the commit.
- This story's ordered pipeline intentionally does NOT require a scheduler wakeup/notify step. Scheduled-row pickup correctness is owned by committed-state rescans (Epic 6); an implementation may add a non-blocking notify optimization, but post-commit ordering does not depend on it.
- The important semantic edge is "visible/acknowledged" vs "durable". This story owns that distinction because it is created by the post-commit ordering itself.
