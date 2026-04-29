# Story 4.3: Commit Path

**Epic:** [Epic 4 — Reducer Transaction Lifecycle](EPIC.md)  
**Spec ref:** SPEC-003 §4.4, §6  
**Depends on:** Story 4.2  
**Blocks:** Story 5.1

---

## Summary

On successful reducer return: commit transaction, assign TxID, hand off to post-commit pipeline.

## Deliverables

- Commit algorithm (Model A: executor allocates TxID):
  ```go
  changeset, commitErr := store.Commit(e.committed, tx)
  if commitErr != nil {
      // go to failure path (Story 4.4)
  }

  txID := e.nextTxID
  e.nextTxID++
  changeset.TxID = txID
  ```

- On successful commit:
  - Hand `(txID, changeset, ret)` to post-commit pipeline (Epic 5)
  - Post-commit pipeline sends ReducerResponse to caller

- TxID assignment:
  - Monotonically increasing from `nextTxID` (initialized in Story 3.1)
  - Assigned after `store.Commit` succeeds, before post-commit steps
  - Never reused, even if executor restarts

## Acceptance Criteria

- [ ] Successful commit assigns next TxID
- [ ] TxID incremented after assignment (next commit gets TxID+1)
- [ ] Commit failure → no TxID assigned, no increment
- [ ] Changeset carries correct TxID
- [ ] Committed state reflects transaction mutations after Commit
- [ ] Post-commit pipeline receives changeset and TxID
- [ ] **Benchmark:** commit of 100 inserts (excluding subscription eval) < 500 µs (§17)

## Design Notes

- TxID is assigned by the executor, not by `store.Commit`. The store's `Commit` is responsible for atomically applying mutations and producing the changeset. The executor wraps the changeset with TxID for downstream consumers.
- `store.Commit` may return an error (uniqueness violation, internal error). Story 4.4 handles the failure path.
- The post-commit pipeline (Epic 5) is called inline. Until Epic 5 is implemented, this story sends the ReducerResponse directly.
