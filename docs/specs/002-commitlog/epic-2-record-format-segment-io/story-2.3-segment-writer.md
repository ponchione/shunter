# Story 2.3: Segment Writer

**Epic:** [Epic 2 — Record Format & Segment I/O](EPIC.md)  
**Spec ref:** SPEC-002 §2.1, §4.5  
**Depends on:** Stories 2.1, 2.2  
**Blocks:** Epic 4 (Durability Worker)

---

## Summary

Append records to the active segment file. Track file size for rotation decisions.

## Deliverables

- `SegmentWriter` struct:
  ```go
  type SegmentWriter struct {
      file    *os.File
      size    int64      // current file size in bytes
      startTx TxID       // first TX ID in this segment
      lastTx  TxID       // last written TX ID
  }
  ```

- `func CreateSegment(dir string, startTxID TxID) (*SegmentWriter, error)`
  - Create file named `{20-digit zero-padded startTxID}.log`
  - Write segment header
  - Initialize size = SegmentHeaderSize

- `func (sw *SegmentWriter) Append(rec *Record) error`
  - Encode record to file
  - Update size and lastTx
  - tx_id must be > lastTx (caller enforces, writer validates)

- `func (sw *SegmentWriter) Sync() error` — fsync the file

- `func (sw *SegmentWriter) Close() error` — close file handle

- `func (sw *SegmentWriter) Size() int64` — current file size

- `func SegmentFileName(startTxID TxID) string` — format 20-digit name

## Acceptance Criteria

- [ ] CreateSegment creates file with correct name format
- [ ] File begins with valid segment header
- [ ] Append writes record, Size() increases by correct amount
- [ ] Multiple Append calls produce readable records in order
- [ ] tx_id must be strictly increasing — out-of-order → error
- [ ] Sync fsyncs to disk (verifiable via reopen-and-read)
- [ ] Close then read → all records intact
- [ ] File name: `00000000000000000001.log` for TxID 1

## Design Notes

- Writer does NOT handle rotation. That's the durability worker's job (Epic 4). Writer just appends and reports size.
- Buffered writes: use `bufio.Writer` wrapping `os.File` for write batching. Flush before Sync.
- No concurrent access — single durability goroutine owns the writer.
