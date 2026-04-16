# Story 6.3: Log Replay

**Epic:** [Epic 6 — Recovery](EPIC.md)  
**Spec ref:** SPEC-002 §6.1 (step 6)  
**Depends on:** Epic 2 (SegmentReader), Epic 3 (DecodeChangeset), SPEC-001 (ApplyChangeset)  
**Blocks:** Story 6.4

---

## Summary

Replay log records from snapshot_tx_id + 1 through durable horizon. Decode changesets and apply to committed state.

## Deliverables

- `func ReplayLog(committed *CommittedState, segments []SegmentInfo, fromTxID TxID, schema SchemaRegistry) (TxID, error)`

  **Algorithm:**
  1. For each segment in order:
     a. Open segment reader
     b. For each record:
        - Skip records with tx_id ≤ fromTxID
        - Decode payload to Changeset via `DecodeChangeset`
        - Stamp `cs.TxID = record.tx_id` before apply (payload carries no TxID — SPEC-002 §3.3)
        - Call `store.ApplyChangeset(committed, cs)`
        - Fatal if ApplyChangeset returns error (corrupt log or schema mismatch)
        - Track max_applied_tx_id
  2. Return max_applied_tx_id

- Decode errors during replay are fatal — return immediately with error context (tx_id, segment path)

- ApplyChangeset errors during replay are fatal — corrupt log state, can't recover

## Acceptance Criteria

- [ ] Replay 100 records → committed state matches expected
- [ ] Skip records ≤ fromTxID — only later records applied
- [ ] fromTxID = 0 → replay everything
- [ ] Replay across multiple segments works correctly
- [ ] Decode error in record → fatal error with tx_id context
- [ ] ApplyChangeset error → fatal error with tx_id context
- [ ] Empty replay (all records ≤ fromTxID) → returns fromTxID
- [ ] Returns correct max_applied_tx_id

## Design Notes

- Replay is the only code path that calls `store.ApplyChangeset`. Both are SPEC-001 constructs. This story bridges SPEC-002 (log) and SPEC-001 (store) during recovery.
- Fatal on any error is correct: recovery must produce a consistent state or fail entirely. Partial replay is worse than no recovery.
