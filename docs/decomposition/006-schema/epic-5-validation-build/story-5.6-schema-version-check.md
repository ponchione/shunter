# Story 5.6: Schema Version Checking at Startup

**Epic:** [Epic 5 — Validation, Build & SchemaRegistry](EPIC.md)
**Spec ref:** SPEC-006 §6, §13 (`ErrSchemaMismatch`)
**Depends on:** Story 5.4 (SchemaRegistry for comparison)
**Blocks:** Nothing (terminal story)

**Cross-spec:** Schema version is stored in snapshots per SPEC-002 §5.3. Comparison runs during `Engine.Start()`.

---

## Summary

At startup, compare the registered schema (version + full structure) against the schema embedded in the latest snapshot. Reject mismatches with a descriptive structural diff.

## Deliverables

- `func CheckSchemaCompatibility(registered SchemaRegistry, snapshot *SnapshotSchema) error`:
  1. Compare `registered.Version()` against `snapshot.Version`
  2. Compare full structural equality:
     - Same set of TableIDs
     - For each table: same name, same column count, columns match (name, type, order), same indexes (name, columns, unique, primary)
  3. If either check fails → return `ErrSchemaMismatch` with details

- `ErrSchemaMismatch` includes a human-readable structural diff.

- `SnapshotSchema` struct — the schema as stored in a SPEC-002 snapshot. Mirrors `SchemaRegistry` data but as a serializable value type.

## Acceptance Criteria

- [ ] Matching version + identical structure → nil error (compatible)
- [ ] Different version, same structure → `ErrSchemaMismatch` mentioning version numbers
- [ ] Same version but different table / column / index structure → `ErrSchemaMismatch` with specific diff details
- [ ] Error message includes concrete diff details, not just a generic "mismatch"
- [ ] Fresh start with no snapshot → compatible by definition
- [ ] Comparison runs during `Engine.Start()`, not during `Build()`

## Design Notes

- v1 has no online schema migration. Mismatch means wipe data or migrate manually.
- `Build()` remains pure registration-time validation; `Start()` owns runtime snapshot comparison.
- The `SnapshotSchema` types are defined here for the comparison interface. SPEC-002 owns the serialization format.
