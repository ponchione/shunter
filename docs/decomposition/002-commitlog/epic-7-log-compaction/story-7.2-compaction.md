# Story 7.2: Compaction

**Epic:** [Epic 7 — Log Compaction](EPIC.md)  
**Spec ref:** SPEC-002 §7  
**Depends on:** Story 7.1, Epic 5 (snapshot creation must be complete)  
**Blocks:** Nothing

---

## Summary

Delete segments fully covered by a snapshot. Retain boundary segments and the active segment.

## Deliverables

- `func Compact(segments []SegmentRange, snapshotTxID TxID) (deleted []string, retained []string)`

  **Algorithm:**
  1. For each segment:
     - If `Active` → retain (never delete active segment)
     - If `MaxTxID <= snapshotTxID` → eligible for deletion (fully covered)
     - If `MinTxID <= snapshotTxID < MaxTxID` → retain (boundary segment, spans snapshot point)
     - If `MinTxID > snapshotTxID` → retain (entirely after snapshot)
  2. Return lists of deleted and retained segment paths

- `func RunCompaction(dir string, snapshotTxID TxID) error`
  - Compute coverage from on-disk segments
  - Run Compact logic
  - Delete eligible segment files via `os.Remove`
  - Fsync the commitlog directory after deletions

- Safety: MUST NOT run compaction until snapshot is fully written and fsynced

## Acceptance Criteria

- [ ] Segment [1, 900] with snapshot at 1000 → deleted
- [ ] Segment [900, 1100] with snapshot at 1000 → retained (boundary)
- [ ] Segment [1001, 1500] with snapshot at 1000 → retained (after snapshot)
- [ ] Active segment → never deleted regardless of coverage
- [ ] No snapshot (snapshotTxID = 0) → nothing deleted
- [ ] Multiple segments all fully covered → all deleted
- [ ] Compact is pure function (testable without filesystem)
- [ ] RunCompaction fsyncs directory after deletes
- [ ] Deletion by segment start offset alone is forbidden — must use actual [min, max] coverage

## Design Notes

- `Compact` is a pure function over data: takes SegmentRanges + snapshotTxID, returns what to delete/retain. `RunCompaction` does the filesystem work. Separation enables unit testing without I/O.
- "Deletion by segment start offset alone is forbidden" — a segment starting at tx 900 might contain records up to tx 1100. Can't delete it just because 900 < 1000.
- v1 runs compaction only after snapshot completion and only against sealed segments. No concurrent reader safety needed.
