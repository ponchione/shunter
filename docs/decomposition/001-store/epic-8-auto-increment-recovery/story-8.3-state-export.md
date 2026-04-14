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
  - `func (t *Table) SetSequenceValue(val uint64)` — restore sequence counter

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

## Design Notes

- This story defines the contract between SPEC-001 (store) and SPEC-002 (commit log) for snapshot/recovery. The actual serialization format is SPEC-002's concern.
- These are simple getter/setter methods. The complexity lives in SPEC-002's snapshot writer and recovery reader.
- SetNextID must set the counter to at least the provided value. If current counter is already higher (from ApplyChangeset allocations), keep the higher value.
