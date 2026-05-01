# Story 6.2: Snapshot Selection & Fallback

**Epic:** [Epic 6 — Recovery](EPIC.md)  
**Spec ref:** SPEC-002 §6.1 (steps 3–5), §6.2, §6.3  
**Depends on:** Epic 5 (ListSnapshots, ReadSnapshot)  
**Blocks:** Story 6.4

---

## Summary

Pick the best usable snapshot for recovery. Fall back through older snapshots on corruption. Validate schema match.

## Deliverables

- `func SelectSnapshot(baseDir string, durableHorizon TxID, schema SchemaRegistry) (*SnapshotData, error)`

  **Algorithm:**
  1. List snapshots newest-first via `ListSnapshots` (skips .lock dirs)
  2. Filter to snapshots with tx_id ≤ durableHorizon
  3. For each candidate (newest first):
     a. ReadSnapshot → check for hash/format errors
     b. If read fails → log warning, try next
     c. Compare snapshot schema to registered schema:
        - Schema version match (`SchemaRegistry.Version()`)
        - All table IDs, names, column definitions (`Index`, `Name`, `Type`, `Nullable`, `AutoIncrement` — all five fields of `ColumnSchema`, see SPEC-006 §8), and index definitions (name, columns, unique, primary) must match exactly
        - Snapshots with `Nullable == true` on any column are rejected indirectly: because registry `Nullable` is always `false` in v1 (SPEC-006 §9), the per-column equality check fires `ErrSchemaMismatch` whenever a stored column has `nullable = 1` (SPEC-002 §5.3). Direct `ErrNullableColumn` rejection is deferred (future nullable-builder drift).
        - Mismatch → `ErrSchemaMismatch` with details
     d. If schema matches → return this snapshot
  4. No usable snapshot found:
     - If log starts at tx_id = 1 → return nil (start from empty)
     - Otherwise → `ErrMissingBaseSnapshot`

## Acceptance Criteria

- [ ] Newest valid snapshot selected
- [ ] Corrupt newest snapshot → falls back to next older
- [ ] All snapshots corrupt + log starts at tx 1 → returns nil (fresh start)
- [ ] All snapshots corrupt + log starts at tx > 1 → `ErrMissingBaseSnapshot`
- [ ] Snapshot tx_id > durableHorizon → skipped
- [ ] Schema mismatch (different column type) → `ErrSchemaMismatch` with detail
- [ ] Schema mismatch (different column `Nullable` flag) → `ErrSchemaMismatch` with detail
- [ ] Schema mismatch (different column `AutoIncrement` flag) → `ErrSchemaMismatch` with detail
- [ ] Snapshot column with `Nullable == true` → `ErrSchemaMismatch` via the per-column equality check (registry `Nullable` is always `false` in v1; see SPEC-006 §9). Direct `ErrNullableColumn` rejection is deferred (future nullable-builder drift).
- [ ] Schema mismatch (missing table) → `ErrSchemaMismatch` with detail
- [ ] Schema mismatch (extra index) → `ErrSchemaMismatch` with detail
- [ ] .lock snapshot → already skipped by ListSnapshots

## Design Notes

- Schema comparison is field-by-field, not byte comparison of encoded schema. This is more robust against encoding order differences.
- `ErrSchemaMismatch` should include which table/column/index differs. Helpful for operator debugging.
- A bad snapshot never authorizes replay past a log gap. Fallback only changes the base state; the log replay suffix must still be contiguous from the chosen snapshot's tx_id + 1.
