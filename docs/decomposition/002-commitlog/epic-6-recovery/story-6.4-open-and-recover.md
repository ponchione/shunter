# Story 6.4: OpenAndRecover Orchestration

**Epic:** [Epic 6 — Recovery](EPIC.md)  
**Spec ref:** SPEC-002 §6.1  
**Depends on:** Stories 6.1, 6.2, 6.3, SPEC-001 Story 8.3 (state export hooks)  
**Blocks:** Nothing (top-level entry point)

---

## Summary

The full startup recovery sequence. Orchestrates segment scanning, snapshot selection, state reconstruction, and log replay.

## Deliverables

- `func OpenAndRecover(dir string, schema SchemaRegistry) (*CommittedState, TxID, error)`

  **Algorithm:**
  1. Scan segments → `ScanSegments(dir)` → segment list + durable horizon
  2. Select snapshot → `SelectSnapshot(dir, durableHorizon, schema)` → snapshot data or nil
  3. Build initial CommittedState (using SPEC-001 Story 8.3 bulk-restore surface):
     - If snapshot: for each table in the snapshot — `committed.RegisterTable(schema)`; for each row in the snapshot — `table.InsertRow(allocatedID, row)` (which also rebuilds index entries via SPEC-001 Story 4.2 `insertIntoIndexes`); `table.SetSequenceValue(snapshot.Sequences[tableID])`; `table.SetNextID(snapshot.NextIDs[tableID])`. No separate `RebuildIndexes` step is required because `InsertRow` handles indexes per-row.
     - If no snapshot and segments begin at tx 1: register tables from schema (empty state)
     - If no snapshot and there are no segments and no snapshots: return `ErrNoData`
   4. Replay log → `ReplayLog(committed, segments, snapshotTxID, schema)` → max_applied_tx_id
   5. Use `ScanSegments` append-mode result to determine the next writable segment strategy:
      - clean tail → append/open normally at `max_applied_tx_id + 1`
      - damaged tail with valid prefix → force durability startup to create a fresh next segment at `max_applied_tx_id + 1`
      - append forbidden → return hard recovery error
   6. Handle edge cases:
     - No segments + valid snapshot → use snapshot as final state (snapshot_tx_id is the durable point)
   7. Return `(committed, max_applied_tx_id, nil)`
     - Executor resumes issuing TX IDs from `max_applied_tx_id + 1`

- Index rebuild after snapshot load:
  - Indexes are rebuilt incidentally during snapshot restore: SPEC-001 Story 2.2 `Table.InsertRow` calls `insertIntoIndexes` (Story 4.2) on every row, so the per-row restore loop already populates all indexes.
  - Cost is O(rows × indexes) but happens only once at startup as part of the restore loop, not as a second pass.

## Acceptance Criteria

- [ ] Snapshot at 1000 + log 1001–1500 → committed state has all 1500 TXs applied
- [ ] No snapshot + log 1–500 → correct from-scratch state
- [ ] No snapshot + log starting at tx > 1 → `ErrMissingBaseSnapshot`
- [ ] Corrupt newest snapshot + valid older → uses older, replays longer log suffix
- [ ] No segments + no snapshots → `ErrNoData`
- [ ] Schema registered on CommittedState matches input SchemaRegistry
- [ ] Indexes rebuilt correctly after snapshot restore
- [ ] Sequences restored from snapshot, then advanced by replay (mechanism: SPEC-001 Story 8.2 `ApplyChangeset` advances `Sequence.next` per insert; SPEC-001 Story 8.3 `SetSequenceValue` uses `max(current, provided)` so snapshot restore never rewinds replay-advanced values)
- [ ] nextID restored from snapshot/export state so future RowID allocation resumes without collision
- [ ] Returns correct max TxID for executor to resume from
- [ ] Crash during snapshot (.lock) + valid prior snapshot → recovery succeeds
- [ ] Two consecutive crashes → recovery still works
- [ ] Damaged tail with valid prefix causes future writes to start in a fresh next segment instead of appending into trailing garbage

## Design Notes

- Index rebuild during recovery is the most expensive restore step for large datasets. Cost is O(N × I) where N = row count, I = indexes per table. For 1M rows with 4 indexes, that is 4M `insertIntoIndexes` calls, all fired from the per-row `InsertRow` loop. Acceptable at startup. SPEC-001 Story 8.3 names the surface; this story names the orchestration.
- Keep the recovery contract deterministic: if no segments and no snapshots exist, return `ErrNoData`.
- This is an integration story — it orchestrates Stories 6.1–6.3. Most logic is already implemented; this story wires them together and handles edge cases.
- Sequence-advance ownership: SPEC-001 Story 8.2 `ApplyChangeset` is the single point that advances `Sequence.next` during replay. Story 6.4 does not run a separate post-replay sweep. The snapshot-restore order (load snapshot sequences via `SetSequenceValue` → replay → values further advanced by `ApplyChangeset`) and `SetSequenceValue`'s `max()` semantics together guarantee post-recovery `next` ≥ any value previously emitted, regardless of which side (snapshot or replay) saw the larger value.
