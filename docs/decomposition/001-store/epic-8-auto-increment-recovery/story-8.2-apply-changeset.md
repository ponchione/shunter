# Story 8.2: ApplyChangeset (Recovery Replay)

**Epic:** [Epic 8 — Auto-Increment & Recovery](EPIC.md)  
**Spec ref:** SPEC-001 §5.8  
**Depends on:** Story 5.1, Epic 6 (Changeset types), Epic 4 (Table indexes)  
**Blocks:** Story 8.3

---

## Summary

Replay a changeset directly to committed state during crash recovery. Bypasses the transaction lifecycle entirely.

## Deliverables

- `func ApplyChangeset(committed *CommittedState, cs *Changeset) error`

  **Algorithm:**
  1. Acquire write lock on CommittedState
  2. For each `TableChangeset` in `cs.Tables`:
     a. Look up table by TableID — error if unknown
     b. For each delete row:
        - PK table: look up by primary key, find RowID, remove from table + all indexes
        - No-PK table: look up by row hash + equality, find RowID, remove from table + all indexes + rowHashIndex
        - Error if row not found (corrupt log)
     c. For each insert row:
        - Validate row schema (column count, types)
        - Assign fresh RowID from table's counter
        - Insert into table + all indexes + rowHashIndex if applicable
        - Error if constraint violation (corrupt log)
  3. Release write lock
  4. Return error if any step fails

- **No TxState, no transaction lifecycle.** Direct mutation of committed state.

- **Constraint violations are fatal.** They indicate a corrupt commit log or schema mismatch. Recovery cannot resolve these — return error, let caller abort.

## Acceptance Criteria

- [ ] ApplyChangeset with inserts → rows appear in committed state + indexes
- [ ] ApplyChangeset with deletes → rows removed from committed state + indexes
- [ ] ApplyChangeset with both → deletes applied, then inserts applied
- [ ] Unknown TableID → error
- [ ] Delete of non-existent row (PK lookup fails) → error
- [ ] Delete of non-existent row (hash lookup fails, no-PK table) → error
- [ ] Insert with wrong column count → error
- [ ] Insert with wrong ValueKind for a column → error
- [ ] Insert that would violate unique constraint → error (indicates corrupt log)
- [ ] Fresh RowIDs assigned — not reusing IDs from the changeset
- [ ] After ApplyChangeset, table.nextID reflects new allocations
- [ ] Multiple ApplyChangeset calls in sequence (replaying a log) → cumulative state correct

## Design Notes

- ApplyChangeset is called only by SPEC-002 `OpenAndRecover`. It replays committed transactions from the commit log to rebuild in-memory state after a crash.
- Delete lookup by PK is straightforward (index seek). Delete lookup by row hash for no-PK tables uses the same rowHashIndex mechanism from Epic 4 Story 4.4.
- RowIDs in the changeset are NOT reused. ApplyChangeset allocates fresh RowIDs because the original RowIDs are internal to the pre-crash process and not persisted.
- `ApplyChangeset` is NOT idempotent. Replaying the same changeset twice causes uniqueness-constraint violations (the second delete fails because the row is already gone; the second insert collides with the first). SPEC-002's recovery path is responsible for replaying each committed changeset exactly once — the "replay the log" acceptance criterion assumes no overlap. A boundary bug that replays a segment twice is itself a fatal corrupt-log condition.
