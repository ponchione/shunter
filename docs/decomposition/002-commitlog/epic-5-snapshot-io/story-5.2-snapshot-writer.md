# Story 5.2: Snapshot Writer

**Epic:** [Epic 5 — Snapshot I/O](EPIC.md)  
**Spec ref:** SPEC-002 §5.1, §5.2, §5.5, §5.6  
**Depends on:** Story 5.1, Story 5.4, Epic 1 (BSATN), SPEC-001 Story 8.3 (state export hooks)  
**Blocks:** Epic 6, Epic 7

---

## Summary

Write a full-state snapshot to disk with Blake3 integrity, lockfile protocol, and deterministic ordering.

## Deliverables

- `SnapshotWriter` implementing:
  ```go
  type SnapshotWriter interface {
      CreateSnapshot(committed *CommittedState, txID TxID) error
  }
  ```
  - Returns an error if another snapshot is already in progress

- `CreateSnapshot` algorithm:
  1. Create `snapshots/{tx_id}/` directory
  2. Create `.lock` file
  3. Build snapshot content in memory (or streaming to temp file):
     - Header: magic `SHSN`, version=1, pad, tx_id, schema_version
     - Hash placeholder (32 zero bytes)
     - `schema_len : uint32 LE` followed by schema bytes (Story 5.1 encoder)
     - Sequence section: seq_count + sorted `(table_id, next_id)` pairs for auto-increment sequence state
     - Table allocation section from SPEC-001 export hooks: sorted `(table_id, next_id)` pairs so future internal `RowID` allocation resumes correctly after restore
     - Table section: table_count + sorted tables, each with row_count + rows in PK order
  4. Compute Blake3 hash over everything after the hash field
  5. Write hash into placeholder position
  6. Fsync snapshot file
  7. Fsync snapshot directory
  8. Remove `.lock`
  9. Fsync directory again

- Snapshot exclusivity:
  - `CreateSnapshot` must fail fast with a dedicated snapshot-in-progress error if another snapshot is already running
  - Do not allow two concurrent writers to the snapshot tree

- Row ordering: for tables with PK, iterate in primary key order. For no-PK tables, iterate in RowID order (deterministic within a process).

- Rows encoded using BSATN (Epic 1), each prefixed with uint32 LE row_len.

- Acquires RLock on CommittedState for duration of serialization.

## Acceptance Criteria

- [ ] Snapshot file starts with magic `SHSN` + version 1
- [ ] Blake3 hash covers all bytes after hash field
- [ ] Reload snapshot (Story 5.3) → row count and values match
- [ ] Sequence state matches committed state at snapshot time
- [ ] RowID allocation state (`nextID`) matches committed state at snapshot time
- [ ] Schema section matches registered schema
- [ ] Snapshot contains `schema_len` immediately before schema bytes
- [ ] .lock file removed after successful write
- [ ] .lock file present if crash mid-write (simulate by not completing)
- [ ] Two snapshots of same state produce identical bytes (deterministic)
- [ ] Tables and sequences sorted by table_id
- [ ] Second concurrent CreateSnapshot call returns snapshot-in-progress error
- [ ] Crash-safety ordering is tested explicitly: file fsync, directory fsync, lock removal, second directory fsync

## Design Notes

- Synchronous snapshot holds RLock on CommittedState for full duration. This blocks commits. Acceptable in v1 — the recommended policy is snapshot on graceful shutdown only.
- Blake3 hash: write content to buffer, compute hash, splice hash into header, then write to file. Alternatively, write to file with placeholder, seek back and patch. Buffer approach is simpler for v1.
- RowIDs are NOT stored. They're internal and rebuilt during recovery.
- SPEC-001 still requires snapshot/recovery to preserve each table's future internal RowID allocator position (`nextID`). This story therefore owns serializing allocation state even though individual row IDs are not persisted.
- Graceful-shutdown ordering is owned by SPEC-003, not this story. SPEC-002 §5.6 pins the two-call contract (final `CreateSnapshot` → `DurabilityHandle.Close`); the engine-level orchestration that decides when to fire it (executor quiesce + in-flight flush) lands in SPEC-003 Session 9.
