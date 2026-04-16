# Story 5.1: Schema Snapshot Codec

**Epic:** [Epic 5 — Snapshot I/O](EPIC.md)  
**Spec ref:** SPEC-002 §5.3  
**Depends on:** SPEC-001 Epic 2 (TableSchema, ColumnSchema, IndexSchema)  
**Blocks:** Stories 5.2, 5.3

---

## Summary

Encode and decode the schema section of a snapshot. Captures all table/column/index definitions.

## Deliverables

- `func EncodeSchemaSnapshot(w io.Writer, schema SchemaRegistry) error`

  **Binary format:**
  ```
  schema_version : uint32 LE
  table_count    : uint32 LE
  [ for each table (sorted by table_id):
      table_id   : uint32 LE
      name_len   : uint32 LE
      name       : [name_len]byte
      col_count  : uint32 LE
      [ for each column:
          col_idx        : uint32 LE
          name_len       : uint32 LE
          name           : [name_len]byte
          type_tag       : uint8       — ValueKind
          nullable       : uint8       — 0 or 1; v1 writers MUST emit 0
          auto_increment : uint8       — 0 or 1
      ]
      idx_count  : uint32 LE
      [ for each index:
          idx_name_len : uint32 LE
          idx_name     : [idx_name_len]byte
          unique       : uint8 (0 or 1)
          primary      : uint8 (0 or 1)
          col_count    : uint32 LE
          [ for each column:
              col_idx  : uint32 LE
          ]
      ]
  ]
  ```
  The three-byte per-column trailer (`type_tag`, `nullable`, `auto_increment`) matches the live encoder at `commitlog/snapshot_io.go`. See SPEC-002 §5.3 for the v1 nullable policy and SPEC-006 §8 for the canonical `ColumnSchema` shape.

- `func DecodeSchemaSnapshot(r io.Reader) ([]TableSchema, uint32, error)`
  - Returns table schemas + schema_version
  - col_idx > math.MaxInt32 → hard error

- Tables sorted by table_id in encoded output (deterministic)

## Acceptance Criteria

- [ ] Round-trip: encode 3 tables with columns and indexes → decode matches
- [ ] Table with 0 indexes encodes/decodes correctly
- [ ] Table with multi-column index → column indices preserved
- [ ] Primary + unique flags round-trip
- [ ] Schema version round-trips
- [ ] col_idx overflow (> MaxInt32) → error on decode
- [ ] Empty schema (0 tables) → valid encode/decode
- [ ] Tables sorted by table_id in output
- [ ] Column `nullable` and `auto_increment` flags round-trip
- [ ] Encoding a column with `nullable = true` is a programmer error in v1 — the writer MUST either reject upstream at schema build time (SPEC-006 §9 / `ErrNullableColumn`) or emit 0 unconditionally; encoder does not silently strip a `true` flag

## Design Notes

- Schema snapshot is embedded in the main snapshot file. This codec handles just the schema section — the snapshot writer (Story 5.2) calls it at the right offset.
- Deterministic ordering enables byte-stable snapshots for testing.
