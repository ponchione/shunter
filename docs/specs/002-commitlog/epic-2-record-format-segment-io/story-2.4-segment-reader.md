# Story 2.4: Segment Reader

**Epic:** [Epic 2 — Record Format & Segment I/O](EPIC.md)  
**Spec ref:** SPEC-002 §2.3, §6.4  
**Depends on:** Stories 2.1, 2.2  
**Blocks:** Epic 6 (Recovery)

---

## Summary

Iterate records from a segment file. Validate header, framing, CRC. Handle truncated tail records gracefully.

## Deliverables

- `SegmentReader` struct:
  ```go
  type SegmentReader struct {
      file    *os.File
      startTx TxID    // from filename
      lastTx  TxID    // last successfully read tx_id
  }
  ```

- `func OpenSegment(path string) (*SegmentReader, error)`
  - Open file, read and validate segment header
  - Parse startTx from filename

- `func (sr *SegmentReader) Next() (*Record, error)`
  - Read next record
  - On valid record: return it, update lastTx
  - On EOF after complete record: return `(nil, io.EOF)` — clean end
  - On truncated record (partial header, partial payload, or missing CRC):
    - Return `(nil, ErrTruncatedRecord)` — signals truncated tail
  - On CRC mismatch: return `(nil, ErrChecksumMismatch)`
  - On other framing errors: return appropriate error

- `func (sr *SegmentReader) Close() error`

- `func (sr *SegmentReader) StartTxID() TxID`

- `func (sr *SegmentReader) LastTxID() TxID` — last successfully read

- `ErrTruncatedRecord` — sentinel, distinguishes truncated tail from corruption

## Acceptance Criteria

- [ ] Read all records written by SegmentWriter — all match
- [ ] Clean EOF after last record → `io.EOF`
- [ ] Truncate file mid-record → `ErrTruncatedRecord`
- [ ] Truncate file mid-CRC → `ErrTruncatedRecord`
- [ ] Corrupt CRC byte → `ErrChecksumMismatch`
- [ ] Bad segment header → `ErrBadMagic` or `ErrBadVersion`
- [ ] Empty segment (header only, no records) → immediate `io.EOF`
- [ ] LastTxID accurate after reading N records
- [ ] Records with data_len > max → `ErrRecordTooLarge` (no allocation)

## Design Notes

- Truncated record vs corrupt record distinction matters for recovery (§6.4). Truncated tail is recoverable (stop there). Mid-segment corruption in a sealed segment is a hard error.
- Reader does NOT validate tx_id contiguity. That's recovery's job (Epic 6) across multiple segments.
