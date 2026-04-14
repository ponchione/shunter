# Story 5.3: Snapshot Reader

**Epic:** [Epic 5 — Snapshot I/O](EPIC.md)  
**Spec ref:** SPEC-002 §5.2, §5.4, §6.1  
**Depends on:** Story 5.1, Story 5.4, Epic 1 (BSATN), SPEC-001 Story 8.3 (state export hooks)  
**Blocks:** Epic 6 (Recovery)

---

## Summary

Read and validate a snapshot file. Produces table rows, sequence state, and schema for recovery.

## Deliverables

- `SnapshotData` struct:
  ```go
  type SnapshotData struct {
      TxID          TxID
      SchemaVersion uint32
      Tables        []SnapshotTableData
      Sequences     map[TableID]uint64
      NextIDs       map[TableID]uint64
      Schema        []TableSchema
  }

  type SnapshotTableData struct {
      TableID  TableID
      Rows     []ProductValue
  }
  ```

- `func ReadSnapshot(dir string) (*SnapshotData, error)`
  - Read snapshot file from `{dir}/snapshot`
  - Validate magic `SHSN`, version=1
  - Read and verify Blake3 hash against content
  - Hash mismatch → `ErrSnapshotHashMismatch`
  - Decode `schema_len : uint32 LE` before decoding schema bytes
  - Decode schema section (Story 5.1)
  - Decode sequence entries
  - Decode per-table `nextID` allocation state
  - Decode table rows using BSATN with decoded schema
  - Return populated SnapshotData

- `func ListSnapshots(baseDir string) ([]TxID, error)`
  - List snapshot subdirectories sorted by TxID descending (newest first)
  - Skip directories with `.lock` file (→ log `ErrSnapshotIncomplete`, don't fail)

## Acceptance Criteria

- [ ] Read snapshot written by Story 5.2 → all data matches
- [ ] Bad magic → error
- [ ] Bad version → error
- [ ] Blake3 mismatch (corrupt 1 byte) → `ErrSnapshotHashMismatch`
- [ ] .lock file present → skipped by ListSnapshots
- [ ] ListSnapshots returns newest first
- [ ] Sequences restored correctly
- [ ] nextID allocation state restored correctly
- [ ] Schema extracted for comparison during recovery
- [ ] Snapshot with missing/truncated `schema_len` or schema section → error
- [ ] Truncated snapshot file → error (hash verification fails or EOF)
- [ ] Empty snapshot (0 tables, 0 sequences) → valid

## Design Notes

- ListSnapshots is used by recovery to find candidate snapshots. It returns TxIDs, not loaded data — loading is separate (try newest, fall back on failure).
- Schema from snapshot is used for validation in recovery (Epic 6), not for table reconstruction. Tables are rebuilt from registered schema + row data.
