# Story 6.2: ExportSchema() Traversal

**Epic:** [Epic 6 — Schema Export & Codegen Interface](EPIC.md)
**Spec ref:** SPEC-006 §12, §12.1
**Depends on:** Story 6.1, Story 5.4 (SchemaRegistry), Story 1.4 (ValueKindExportString)
**Blocks:** Stories 6.3, 6.4

**Cross-spec:** Produces the in-memory `SchemaExport` value consumed by the JSON contract and codegen stories.

---

## Summary

Walk the immutable `SchemaRegistry` and produce a `SchemaExport` value suitable for serialization and client code generation.

## Deliverables

- `func (e *Engine) ExportSchema() *SchemaExport`:
  1. Create `SchemaExport` with `Version` from the registry
  2. For each `TableID` in `registry.Tables()`:
     - Build `TableExport` with name, columns, and indexes
  3. For each reducer name in `registry.Reducers()`:
     - Build `ReducerExport{Name: name, Lifecycle: false}`
  4. Append lifecycle reducers when `OnConnect()` / `OnDisconnect()` are present
  5. Return the export value

## Acceptance Criteria

- [ ] `ExportSchema()` includes all user tables in registration order
- [ ] `ExportSchema()` includes system tables (`sys_clients`, `sys_scheduled`)
- [ ] Column types are lowercase strings (`"uint64"`, `"string"`, `"bytes"`, etc.)
- [ ] Index export uses column names, not numeric indices
- [ ] Primary index is exported with `Primary: true` and `Unique: true`
- [ ] Non-lifecycle reducers export with `Lifecycle: false`; registered lifecycle reducers export with `Lifecycle: true`
- [ ] Export version matches `SchemaRegistry.Version()`
- [ ] Returned export is a detached value snapshot, not shared mutable registry state

## Design Notes

- Export ordering mirrors `SchemaRegistry.Tables()` and `SchemaRegistry.Reducers()`, both of which are stable by design.
- This story owns the in-memory value traversal only. JSON round-trip guarantees and CLI consumption live in Stories 6.4 and 6.3 respectively.
