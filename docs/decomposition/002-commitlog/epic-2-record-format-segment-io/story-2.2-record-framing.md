# Story 2.2: Record Framing & CRC

**Epic:** [Epic 2 — Record Format & Segment I/O](EPIC.md)  
**Spec ref:** SPEC-002 §2.3, §2.4  
**Depends on:** Story 2.1  
**Blocks:** Stories 2.3, 2.4

---

## Summary

Record structure: tx_id + record_type + flags + data_len + payload + CRC32C. Encode/decode with integrity check.

## Deliverables

- Record constants:
  ```go
  const RecordTypeChangeset uint8 = 1
  const RecordHeaderSize = 14  // 8 + 1 + 1 + 4
  const RecordCRCSize = 4
  const RecordOverhead = 18    // header + CRC, excluding payload
  ```

- `Record` struct:
  ```go
  type Record struct {
      TxID       TxID
      RecordType uint8
      Flags      uint8
      Payload    []byte
  }
  ```

- `func ComputeRecordCRC(r *Record) uint32`
  - CRC32C (Castagnoli) over tx_id(8) + record_type(1) + flags(1) + data_len(4) + payload
  - Uses `crc32.New(crc32.MakeTable(crc32.Castagnoli))`

- `func EncodeRecord(w io.Writer, r *Record) error`
  - Write header fields (all LE), payload, then CRC

- `func DecodeRecord(r io.Reader) (*Record, error)`
  - Read header, validate record_type and flags, read payload, read CRC, verify
  - Unknown record_type → `ErrUnknownRecordType`
  - Non-zero flags → `ErrBadFlags`
  - data_len > MaxRecordPayloadBytes → `ErrRecordTooLarge`
  - CRC mismatch → `ErrChecksumMismatch`

## Acceptance Criteria

- [ ] Encode then decode → identical Record
- [ ] CRC covers tx_id through payload, not segment header
- [ ] Flip one payload byte → `ErrChecksumMismatch`
- [ ] Flip one header byte (e.g., flags) → `ErrChecksumMismatch`
- [ ] record_type=2 → `ErrUnknownRecordType`
- [ ] flags=1 → `ErrBadFlags`
- [ ] data_len > max → `ErrRecordTooLarge` (rejected before allocation)
- [ ] Empty payload (data_len=0) → valid record
- [ ] All multi-byte fields are little-endian

## Design Notes

- `ErrRecordTooLarge` check happens BEFORE allocating the payload buffer. Prevents OOM from malformed data_len.
- CRC32C chosen for hardware acceleration on modern CPUs (SSE 4.2). Go's `hash/crc32` uses hardware when available.
