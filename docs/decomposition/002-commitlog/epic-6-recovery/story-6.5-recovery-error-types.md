# Story 6.5: Recovery Error Types

**Epic:** [Epic 6 — Recovery](EPIC.md)  
**Spec ref:** SPEC-002 §9  
**Depends on:** Nothing  
**Blocks:** Nothing (consumed by Stories 6.1–6.4)

---

## Summary

Error types specific to the recovery path.

## Deliverables

| Error | Type | Trigger |
|---|---|---|
| `ErrSchemaMismatch` | struct | snapshot schema differs from registered schema |
| `ErrHistoryGap` | struct | missing/overlapping/out-of-order segment TX range |
| `ErrMissingBaseSnapshot` | sentinel | no usable snapshot and log doesn't start at tx 1 |
| `ErrNoData` | sentinel | no segments and no snapshots found |

- `ErrSchemaMismatch` fields:
  - `Detail string` — human-readable description of what differs (table name, column type, etc.)

- `ErrHistoryGap` fields:
  - `Expected TxID` — what the next contiguous tx_id should be
  - `Got TxID` — what was found instead
  - `Segment string` — path of the problematic segment

## Acceptance Criteria

- [ ] All error types satisfy `error` interface
- [ ] `errors.Is` works for sentinels
- [ ] `errors.As` works for struct errors
- [ ] ErrSchemaMismatch.Detail includes specific field that differs
- [ ] ErrHistoryGap includes segment path and expected/got TX IDs
