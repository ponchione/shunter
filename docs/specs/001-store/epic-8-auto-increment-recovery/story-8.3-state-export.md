# Story 8.3: State Export for Snapshot Persistence

**Epic:** [Epic 8 — Auto-Increment & Recovery](EPIC.md)  
**Spec ref:** SPEC-001 §11 (SPEC-002 interface)  
**Depends on:** Story 8.1  
**Blocks:** Nothing (consumed by SPEC-002 snapshot serialization)

---

## Summary

Export internal state needed by the commit log to reconstruct the store after recovery. Not a serialization format — just accessors for the data SPEC-002 needs.

## Deliverables

The commit log (SPEC-002) needs to persist and restore:

- **All committed rows** per table — already accessible via Table.Scan()

- **Per-table nextID** — so RowID allocation continues without collisions after recovery:
  - `func (t *Table) NextID() uint64`

- **Per-table sequence state** — so auto-increment continues without reissuing values:
  - `func (t *Table) SequenceValue() (uint64, bool)` — returns (current next value, has sequence)

- **Restore functions** for recovery:
  - `func (t *Table) SetNextID(id uint64)` — called after replaying all changesets
  - `func (t *Table) SetSequenceValue(val uint64)` — restore sequence counter. Sets the counter to `max(current, val)`, matching `SetNextID` semantics. Rationale: if replay has already advanced the sequence past the snapshot-stored value (see Story 8.2 sequence-advance-on-replay step), the higher value wins.

- **Bulk-restore primitives** for SPEC-002 recovery (Story 6.4):
  - `func (cs *CommittedState) RegisterTable(schema *TableSchema) error` (Story 5.1) — register tables from the snapshot's schema before populating rows.
  - `func (t *Table) InsertRow(id RowID, row ProductValue) error` (Story 2.2) — bulk-restore primitive. Each call rebuilds index entries for `row` via `insertIntoIndexes` (Story 4.2), so indexes do not need a separate rebuild step. Recovery loops `InsertRow` over snapshot rows in order; SPEC-001 does not expose a dedicated `RestoreRow` or `RebuildIndexes` surface because `InsertRow` already covers both responsibilities.
  - `func (t *Table) SetNextID(id uint64)` — call after restoring all snapshot rows so future `AllocRowID()` resumes past the snapshot horizon.
  - `func (t *Table) SetSequenceValue(val uint64)` — call once per snapshot-recorded sequence value before replay; replay's `ApplyChangeset` further advances via Story 8.2.

- Cross-spec contract note:
  - SPEC-002 snapshot/recovery stories that serialize committed store state must depend on these accessors explicitly, because future RowID allocation after restore is undefined unless `nextID` is restored alongside sequence state

## Acceptance Criteria

- [ ] NextID returns current counter value
- [ ] After inserting 5 rows, NextID returns value > 5
- [ ] SequenceValue returns (next, true) for table with autoincrement
- [ ] SequenceValue returns (0, false) for table without autoincrement
- [ ] SetNextID followed by AllocRowID → returns the set value
- [ ] SetSequenceValue followed by Sequence.Next() → returns the set value
- [ ] Round-trip: export state → new empty table → restore state → insert → IDs continue correctly
- [ ] Round-trip: export → restore → auto-increment values continue without gap or reuse
- [ ] SetSequenceValue with val < current → counter unchanged
- [ ] SetSequenceValue with val > current → counter set to val
- [ ] Round-trip snapshot-restore → replay → SetSequenceValue → counter reflects max of snapshot value and replay-advanced value

## Design Notes

- This story defines the contract between SPEC-001 (store) and SPEC-002 (commit log) for snapshot/recovery. The actual serialization format is SPEC-002's concern.
- These are simple getter/setter methods. The complexity lives in SPEC-002's snapshot writer and recovery reader.
- `SetNextID` and `SetSequenceValue` both take `max(current, provided)`. Symmetric by design: if ApplyChangeset has already advanced the counter during replay, the restore setter must not rewind it.
- The bulk-restore path is `RegisterTable → loop InsertRow → SetNextID → SetSequenceValue`. SPEC-002 Story 6.4 documents the orchestration; SPEC-001 owns the surface. No dedicated `RestoreRow` / `RebuildIndexes` methods exist — `InsertRow` is the single entry point and handles row + index in one call.
