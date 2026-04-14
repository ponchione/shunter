# Story 1.2: Value Decoder

**Epic:** [Epic 1 — BSATN Codec](EPIC.md)  
**Spec ref:** SPEC-002 §3.3  
**Depends on:** Story 1.1  
**Blocks:** Story 1.3

---

## Summary

Decode a single Value from BSATN bytes. Validate tag, payload integrity, and UTF-8.

## Deliverables

- `func DecodeValue(r io.Reader) (Value, error)`
  - Read tag byte
  - Dispatch to type-specific decoder based on tag
  - Unknown tag → `ErrUnknownValueTag`
  - String payload: validate UTF-8 → `ErrInvalidUTF8` if invalid
  - All multi-byte reads are little-endian
  - EOF mid-value → `io.ErrUnexpectedEOF`

- `func DecodeValueExpecting(r io.Reader, expected ValueKind) (Value, error)`
  - Decode then verify tag matches expected kind
  - Mismatch → `ErrTypeTagMismatch`
  - Used by schema-aware ProductValue decoder (Story 1.3)

## Acceptance Criteria

- [ ] Round-trip: encode then decode each of 13 kinds — value matches
- [ ] Unknown tag byte (e.g., 99) → `ErrUnknownValueTag`
- [ ] Tag mismatch in DecodeValueExpecting → `ErrTypeTagMismatch{Expected, Got}`
- [ ] Invalid UTF-8 in string payload → `ErrInvalidUTF8`
- [ ] Truncated payload (EOF mid-read) → `io.ErrUnexpectedEOF`
- [ ] String with length 0 decodes to empty string
- [ ] Bytes with length 0 decodes to empty byte slice (not nil)
- [ ] Float32/Float64 round-trip preserves exact bits (including -0.0)

## Design Notes

- `DecodeValueExpecting` is the hot path — schema-validated row decoding calls it per column. Keep allocation-free for fixed-size types.
- String/Bytes decode allocates a new slice. Caller owns the memory.
