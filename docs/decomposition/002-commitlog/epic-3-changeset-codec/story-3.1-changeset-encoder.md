# Story 3.1: Changeset Encoder

**Epic:** [Epic 3 — Changeset Codec](EPIC.md)  
**Spec ref:** SPEC-002 §3.1, §3.2  
**Depends on:** Epic 1 (BSATN EncodeProductValue)  
**Blocks:** Story 3.2, Epic 4

---

## Summary

Encode a Changeset to the binary payload format used inside log records.

## Deliverables

- `func EncodeChangeset(cs *Changeset) ([]byte, error)`

  **Payload structure:**
  ```
  version      : uint8 = 1
  table_count  : uint32 LE
  [ for each table:
      table_id     : uint32 LE
      insert_count : uint32 LE
      [ for each insert:
          row_len  : uint32 LE
          row_data : [row_len]byte  — BSATN-encoded ProductValue
      ]
      delete_count : uint32 LE
      [ for each delete:
          row_len  : uint32 LE
          row_data : [row_len]byte
      ]
  ]
  ```

- Table iteration order: sorted by TableID ascending (deterministic encoding)

- Row encoding: uses `bsatn.EncodeProductValue` for each row, wrapped with uint32 LE length prefix

## Acceptance Criteria

- [ ] Encode changeset with 2 tables, 3 inserts + 2 deletes → correct binary
- [ ] Empty changeset (0 tables) → version + uint32(0)
- [ ] Table with 0 inserts and 0 deletes → still present with zero counts
- [ ] Tables sorted by TableID in output
- [ ] Row length prefix matches actual encoded row size
- [ ] Version byte is 1
- [ ] Large row (near MaxRowBytes) encodes without error
- [ ] Round-trip with decoder (Story 3.2) produces identical Changeset

## Design Notes

- Pre-compute total size for single allocation if practical. Otherwise `bytes.Buffer` is fine.
- Deterministic table ordering enables byte-stable output for testing. Not required for correctness but simplifies verification.
