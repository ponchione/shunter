# Story 1.1: Value Encoder

**Epic:** [Epic 1 — BSATN Codec](EPIC.md)  
**Spec ref:** SPEC-002 §3.3  
**Depends on:** SPEC-001 Epic 1 (Value, ValueKind)  
**Blocks:** Story 1.2, Story 1.3

---

## Summary

Encode a single Value to its BSATN binary representation: tag byte + type-specific payload.

## Deliverables

- `func EncodeValue(w io.Writer, v Value) error`
  - Writes tag byte (ValueKind → uint8 mapping from §3.3 table)
  - Writes type-specific payload:
    - Bool: 1 byte (0x00 or 0x01)
    - Int8: 1 byte signed
    - Uint8: 1 byte unsigned
    - Int16: 2 bytes LE
    - Uint16: 2 bytes LE
    - Int32: 4 bytes LE
    - Uint32: 4 bytes LE
    - Int64: 8 bytes LE
    - Uint64: 8 bytes LE
    - Float32: 4 bytes IEEE-754 LE
    - Float64: 8 bytes IEEE-754 LE
    - String: uint32 LE byte count + UTF-8 bytes
    - Bytes: uint32 LE byte count + raw bytes

- `func EncodedValueSize(v Value) int` — predicted byte size without allocation (for buffer pre-sizing)

- Tag constant table:
  ```go
  const (
      TagBool    uint8 = 0
      TagInt8    uint8 = 1
      // ... through TagBytes = 12
  )
  ```

## Acceptance Criteria

- [ ] Each of 13 ValueKinds encodes to correct tag + payload bytes
- [ ] Little-endian byte order for all multi-byte integers and floats
- [ ] String: length prefix is byte count, not rune count
- [ ] Empty string encodes as tag + uint32(0)
- [ ] Empty bytes encodes as tag + uint32(0)
- [ ] EncodedValueSize matches actual encoded length for all kinds
- [ ] Large string (> 64KB) encodes correctly with uint32 length prefix

## Design Notes

- All writes use `encoding/binary.LittleEndian`. No big-endian anywhere.
- Tags are stable across versions. Adding a new type requires a new tag value.
- This is the canonical reference encoding — SPEC-005 wire protocol reuses it.
