# Story 2.5: Segment Error Types

**Epic:** [Epic 2 — Record Format & Segment I/O](EPIC.md)  
**Spec ref:** SPEC-002 §9  
**Depends on:** Nothing  
**Blocks:** Nothing (consumed by Stories 2.2, 2.3, 2.4)

---

## Summary

Error types for segment file and record-level failures.

## Deliverables

| Error | Type | Trigger |
|---|---|---|
| `ErrBadMagic` | sentinel | segment header magic != `SHNT` |
| `ErrBadVersion` | struct | version byte != 1 |
| `ErrBadFlags` | sentinel | record flags != 0 in v1 |
| `ErrUnknownRecordType` | struct | record_type not in defined set |
| `ErrChecksumMismatch` | struct | CRC32C doesn't match |
| `ErrRecordTooLarge` | struct | data_len > MaxRecordPayloadBytes |
| `ErrTruncatedRecord` | sentinel | partial record at segment tail |

- `ErrBadVersion` fields: `Got uint8`
- `ErrUnknownRecordType` fields: `Type uint8`
- `ErrChecksumMismatch` fields: `Expected uint32`, `Got uint32`, `TxID TxID`
- `ErrRecordTooLarge` fields: `Size uint32`, `Max uint32`

## Acceptance Criteria

- [ ] All error types satisfy `error` interface
- [ ] `errors.Is` works for sentinels
- [ ] `errors.As` works for struct errors
- [ ] ErrChecksumMismatch message includes TxID for debugging
- [ ] ErrRecordTooLarge message includes size and max
