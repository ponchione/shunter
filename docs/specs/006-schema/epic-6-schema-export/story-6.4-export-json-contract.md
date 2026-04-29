# Story 6.4: Export JSON Contract & Snapshot Semantics

**Epic:** [Epic 6 — Schema Export & Codegen Interface](EPIC.md)
**Spec ref:** SPEC-006 §12, §12.1
**Depends on:** Story 6.2
**Blocks:** Story 6.3

---

## Summary

Define the JSON-facing contract of `SchemaExport` and verify that exported schema values behave like detached snapshots that can be safely marshaled, written to disk, and consumed by tooling.

## Deliverables

- JSON contract guarantee: `SchemaExport` is directly `json.Marshal`-able / `json.Unmarshal`-able without a custom marshaler.
- Export snapshot guarantee: mutating the returned `SchemaExport` value must not mutate the underlying `SchemaRegistry`.
- Documentation note or test fixture that demonstrates the canonical `schema.json` shape consumed by `shunter-codegen`.

## Acceptance Criteria

- [ ] `json.Marshal(export)` → `json.Unmarshal` → deep-equal original export value
- [ ] Mutating `Tables`, `Reducers`, or nested column/index slices on the returned export value does not mutate the registry
- [ ] Canonical serialized JSON includes version, tables, columns, indexes, and reducers in the expected shape
- [ ] JSON contract is stable enough for `shunter-codegen` to consume as an external file interface
- [ ] No custom JSON marshaler is required for the v1 export surface

## Design Notes

- Keeping JSON/snapshot semantics separate from registry traversal makes the engine-side export implementation easier to review and test.
- This story is the bridge between the in-memory export value and the external `schema.json` file interface used by tooling.
