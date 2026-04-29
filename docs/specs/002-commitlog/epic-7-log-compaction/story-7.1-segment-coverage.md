# Story 7.1: Segment Coverage Analysis

**Epic:** [Epic 7 — Log Compaction](EPIC.md)  
**Spec ref:** SPEC-002 §7  
**Depends on:** Story 6.1 (SegmentInfo from segment scanning)  
**Blocks:** Story 7.2

---

## Summary

Determine the TX ID range covered by each sealed segment. Used to decide which segments are safe to delete after a snapshot.

## Deliverables

- `func SegmentCoverage(segments []SegmentInfo) []SegmentRange`

  ```go
  type SegmentRange struct {
      Path    string
      MinTxID TxID
      MaxTxID TxID
      Active  bool   // true if this is the current writable segment
  }
  ```

- MinTxID = first record's tx_id (from segment filename / SegmentInfo.StartTx)
- MaxTxID = last valid record's tx_id (from SegmentInfo.LastTx)
- Active segment identified as the last segment in the sorted list

## Acceptance Criteria

- [ ] 3 segments → 3 SegmentRange entries with correct min/max
- [ ] Last segment marked Active
- [ ] Single-record segment: MinTxID == MaxTxID
- [ ] Empty segment (header only): MinTxID = StartTx, MaxTxID = 0 (or StartTx - 1)
- [ ] Ranges computed from SegmentInfo, not by re-reading files

## Design Notes

- SegmentInfo already has StartTx and LastTx from scanning (Epic 6 Story 6.1). This story just wraps that data into the coverage model used by compaction.
- Could be a thin function over existing data. Exists as its own story to keep compaction logic testable in isolation.
