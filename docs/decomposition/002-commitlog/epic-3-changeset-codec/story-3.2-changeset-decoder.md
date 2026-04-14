# Story 3.2: Changeset Decoder

**Epic:** [Epic 3 — Changeset Codec](EPIC.md)  
**Spec ref:** SPEC-002 §3.2, §3.4  
**Depends on:** Story 3.1, Epic 1 (BSATN DecodeProductValue)  
**Blocks:** Epic 6 (Recovery)

---

## Summary

Decode payload bytes back to a Changeset. Schema-aware: validates row shape and types during decode.

## Deliverables

- `func DecodeChangeset(data []byte, schema SchemaRegistry) (*Changeset, error)`
  - Read version byte — reject if != 1
  - Read table_count
  - For each table:
    - Read table_id, look up TableSchema from registry
    - Unknown table_id → error
    - Read insert_count, decode each row:
      - Read row_len, enforce MaxRowBytes
      - row_len > MaxRowBytes → `ErrRowTooLarge`
      - Decode ProductValue from row_data using table schema
    - Read delete_count, decode each row (same process)
  - Return populated Changeset

- `ErrRowTooLarge` — struct error, fields: `Size uint32`, `Max uint32`

## Acceptance Criteria

- [ ] Round-trip: encode then decode → identical Changeset
- [ ] Unknown table_id → error with table ID in message
- [ ] Version != 1 → error
- [ ] Row with wrong column count for table → `ErrRowShapeMismatch`
- [ ] Row with wrong type tag → `ErrTypeTagMismatch`
- [ ] row_len > MaxRowBytes → `ErrRowTooLarge` (before allocation)
- [ ] Truncated payload (EOF mid-decode) → error
- [ ] Empty changeset round-trips correctly
- [ ] Changeset with table that has only deletes, no inserts → correct

## Design Notes

- Schema-at-commit-time: v1 schema is static. Decoder receives the current SchemaRegistry and validates against it. Schema evolution (different schema at different TxIDs) is out of scope.
- MaxRowBytes check prevents OOM from malformed row_len. Check happens before allocating the row buffer.
