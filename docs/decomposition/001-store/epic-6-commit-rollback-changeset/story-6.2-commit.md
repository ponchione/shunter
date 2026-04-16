# Story 6.2: Commit Algorithm

**Epic:** [Epic 6 — Commit, Rollback & Changeset](EPIC.md)  
**Spec ref:** SPEC-001 §5.6  
**Depends on:** Story 6.1, Epic 5 (Transaction)  
**Blocks:** Story 6.3

---

## Summary

Apply a transaction's mutations to committed state and produce a changeset. Atomic from the executor's perspective.

## Deliverables

- `func Commit(committed *CommittedState, tx *Transaction) (*Changeset, error)`

  **Algorithm:**
  1. Acquire write lock on CommittedState
  2. Validate that all remaining commit-time checks still pass against the current committed state
     - Re-check uniqueness / set-semantics constraints against the latest committed state
     - If validation fails, return error before mutating committed state
  3. For each table with deletes in tx.deletes:
     a. Read deleted row values from committed state → append to changeset Deletes
     b. Populate `TableChangeset.TableID` and `TableChangeset.TableName`
     c. Remove rows from `committed.tables[tableID].rows`
     d. Remove from all committed indexes (via Table.removeFromIndexes)
     e. Remove from rowHashIndex if applicable
  4. For each table with inserts in tx.inserts:
     a. Insert rows into `committed.tables[tableID].rows` using provisional RowIDs
     b. Insert into all committed indexes (via Table.insertIntoIndexes)
     c. Insert into rowHashIndex if applicable
     d. Populate `TableChangeset.TableID` and `TableChangeset.TableName` if not already set
     e. Append to changeset Inserts
  5. Build and return Changeset with `TxID` zero-valued — the executor stamps it after return
  6. Release write lock

- **TxID allocation is not the store's responsibility** (Model A). The executor owns the monotonic `TxID` counter, restored from SPEC-002's `max_applied_tx_id` at recovery and advanced atomically per successful commit (SPEC-003 §13.2). `CommittedState` carries no TxID counter in v1.

- **Atomicity invariant:** If Commit returns error, committed state MUST be unchanged. The intended implementation shape is validate-first, then mutate under the write lock; callers should not rely on compensating rollback of partially applied mutations as the primary design.

- **Delete-before-insert order:** Critical for update/replace flows. If a TX deletes key=A and inserts key=A (different row), applying deletes first frees the key before the insert checks uniqueness.

## Acceptance Criteria

- [ ] Commit 100 inserts → all rows in committed state, changeset has 100 Inserts
- [ ] Commit 5 deletes → rows removed from committed state, changeset has 5 Deletes
- [ ] Commit mix: 3 deletes + 7 inserts → committed state correct, changeset correct
- [ ] After commit, indexes reflect new state (seek finds inserted rows, doesn't find deleted)
- [ ] `Commit` returns `*Changeset, error` — no TxID in the signature; returned `changeset.TxID` is the zero value until stamped by the caller
- [ ] Produced TableChangeset has correct TableID and TableName for each changed table
- [ ] Delete-before-insert: update flow (delete old PK, insert new PK) succeeds
- [ ] Atomicity: if failure occurs, committed state unchanged
- [ ] Commit-time revalidation failure against newer committed state returns an error before mutation and leaves committed state unchanged
- [ ] Write lock held for duration — concurrent snapshot (RLock) blocks until commit completes

## Design Notes

- Commit-time failure is rare in v1 because constraint checks already happened during the transaction. The main failure mode would be a bug or invariant violation. Still, atomicity must be preserved.
- The write lock blocks all concurrent snapshots. This is acceptable in v1 because commits should be fast (small changesets). Long-running commits would require a different concurrency model (v2).
