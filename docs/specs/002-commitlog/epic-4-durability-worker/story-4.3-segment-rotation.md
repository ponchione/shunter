# Story 4.3: Segment Rotation

**Epic:** [Epic 4 — Durability Worker](EPIC.md)  
**Spec ref:** SPEC-002 §4.5  
**Depends on:** Story 4.2  
**Blocks:** Nothing

---

## Summary

When active segment exceeds MaxSegmentSize, or recovery determines the writable tail must not be appended in place, sync/close the old segment and open a fresh one.

## Deliverables

- `func (w *durabilityWorker) maybeRotate() error`
  - Check `seg.Size() >= opts.MaxSegmentSize`
  - If not exceeded, return nil
  - Sync current segment
  - Close current segment file
  - Open new segment with `startTxID = nextTxID` (TX ID of next record to be written)

- Resume-after-crash ownership for this story:
  - If recovery reports that the prior active tail ended with invalid trailing bytes but still has a valid contiguous prefix, the worker must start a fresh next segment at `last_valid_tx_id + 1` rather than appending into the damaged file
  - Rotation/opening logic therefore owns the "fresh next segment" half of SPEC-002 §6.4's resume contract

- Rotation happens BETWEEN records, never mid-record. Check after each batch.

- New segment file named by its first TX ID.

## Acceptance Criteria

- [ ] Write records past MaxSegmentSize → new segment file created
- [ ] Old segment file is synced and closed before new one opens
- [ ] New segment starts with valid header
- [ ] New segment name = 20-digit zero-padded first TX ID
- [ ] Records span two segments → both readable in sequence
- [ ] Rotation with MaxSegmentSize=0 → rotates after every batch (edge case)
- [ ] No rotation when size < max
- [ ] Recovery resume mode with damaged writable tail opens a fresh next segment at `last_valid_tx_id + 1`

## Design Notes

- Rotation is checked once per batch, not per record. A single large record could push a segment slightly past MaxSegmentSize. This is acceptable — the limit is advisory, not hard.
- Sync before close ensures the old segment is durable before the new one is created.
