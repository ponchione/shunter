# Story 1.3: ProductValue Codec

**Epic:** [Epic 1 — BSATN Codec](EPIC.md)  
**Spec ref:** SPEC-002 §3.3  
**Depends on:** Stories 1.1, 1.2  
**Blocks:** Epic 3 (Changeset Codec), Epic 5 (Snapshot I/O)

---

## Summary

Encode/decode a full ProductValue (row) with schema validation. This is the row-level codec used in both changeset payloads and snapshot files.

## Deliverables

- `func EncodeProductValue(w io.Writer, row ProductValue) error`
  - Encode each Value in column order using EncodeValue
  - No framing — caller provides row_len wrapper if needed

- `func DecodeProductValue(r io.Reader, schema *TableSchema) (ProductValue, error)`
  - Decode `len(schema.Columns)` values using `DecodeValueExpecting` with each column's type
  - Fewer values than columns → `ErrRowShapeMismatch`
  - More values than columns → `ErrRowShapeMismatch`

- `func DecodeProductValueFromBytes(data []byte, schema *TableSchema) (ProductValue, error)`
  - Same as above but from byte slice
  - After decoding all columns, if bytes remain → `ErrRowLengthMismatch`
  - If bytes exhausted before all columns decoded → `ErrRowLengthMismatch`

- `func EncodedProductValueSize(row ProductValue) int` — sum of EncodedValueSize per column

## Acceptance Criteria

- [ ] Round-trip: 5-column row with mixed types — all values match
- [ ] Schema expects 3 columns, encoded row has 2 → `ErrRowShapeMismatch`
- [ ] Schema expects 3 columns, encoded row has 4 → `ErrRowShapeMismatch`
- [ ] Schema expects Int32 in column 0, encoded has String tag → `ErrTypeTagMismatch`
- [ ] DecodeFromBytes with trailing bytes → `ErrRowLengthMismatch`
- [ ] DecodeFromBytes with insufficient bytes → `ErrRowLengthMismatch`
- [ ] Empty row (0 columns, matching schema) → encodes to empty bytes, decodes back
- [ ] Large row (many columns, large strings) → correct round-trip

## Design Notes

- `DecodeProductValueFromBytes` wraps the byte slice in a `bytes.Reader` and checks position after decode. Simpler than manual offset tracking.
- No framing (row_len) in encode/decode — the changeset codec (Epic 3) handles the row_len prefix. BSATN is just the content encoding.
