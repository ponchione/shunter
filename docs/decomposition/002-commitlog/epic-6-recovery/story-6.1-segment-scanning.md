# Story 6.1: Segment Scanning & Validation

**Epic:** [Epic 6 — Recovery](EPIC.md)  
**Spec ref:** SPEC-002 §6.1 (steps 1–2), §6.4, §6.5  
**Depends on:** Epic 2 (SegmentReader)  
**Blocks:** Story 6.4

---

## Summary

List segment files, validate headers and record integrity, determine the durable replay horizon (highest contiguous valid tx_id).

## Deliverables

- `SegmentInfo` struct:
  ```go
  type SegmentInfo struct {
      Path    string
      StartTx TxID
      LastTx  TxID    // last valid tx_id in segment
      Valid   bool    // all records valid up to LastTx
      AppendMode AppendMode
  }
  ```

  ```go
  type AppendMode uint8

  const (
      AppendInPlace AppendMode = iota
      AppendByFreshNextSegment
      AppendForbidden
  )
  ```

- `func ScanSegments(dir string) ([]SegmentInfo, TxID, error)`
  - List `commitlog/*.log` sorted by name (= sorted by start TX)
  - For each segment:
    - Open, validate header
    - Read all records, validate framing + CRC
    - Track last valid tx_id
  - Validate cross-segment contiguity:
    - Segment N's last tx_id + 1 must equal segment N+1's first tx_id
    - Gap → `ErrHistoryGap`
    - Overlap → `ErrHistoryGap`
    - Out-of-order tx_id within segment → `ErrHistoryGap`
  - Truncated tail handling (active segment only):
    - Partial record at end of last segment → stop at last valid record
    - If at least one valid record precedes the damage, mark the last segment `AppendByFreshNextSegment`
    - Corrupt first record in last segment (no valid prefix) → hard error
    - Corrupt first record in last segment with no valid prefix marks append as forbidden / hard error
  - Non-tail corruption (sealed segment) → hard error
  - Return segment list + durable replay horizon (highest valid contiguous tx_id)

## Acceptance Criteria

- [ ] 3 segments with contiguous tx_ids → returns all with correct ranges
- [ ] Missing middle segment → `ErrHistoryGap`
- [ ] Overlapping segment ranges → `ErrHistoryGap`
- [ ] Out-of-order tx_id within segment → `ErrHistoryGap`
- [ ] Truncated tail record in active segment → horizon stops at last valid
- [ ] Corrupt sealed segment → hard error
- [ ] Empty commitlog dir → returns empty list, horizon = 0
- [ ] Single segment with 1 record → horizon = that tx_id
- [ ] Corrupt first record in active segment with no valid prefix → hard error
- [ ] Truncated active tail after valid prefix marks append mode as fresh-next-segment, not append-in-place

## Design Notes

- "Sealed segment" = any segment that is not the last file. Only the active (last) segment can have a truncated tail from a crash.
- ScanSegments does NOT decode payloads. It only validates framing and CRC. Payload decode happens during replay (Story 6.3).
