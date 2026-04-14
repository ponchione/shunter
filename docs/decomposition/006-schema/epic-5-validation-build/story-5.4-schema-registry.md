# Story 5.4: SchemaRegistry Implementation

**Epic:** [Epic 5 — Validation, Build & SchemaRegistry](EPIC.md)
**Spec ref:** SPEC-006 §7
**Depends on:** Story 5.3 (constructed from Build output)
**Blocks:** Story 5.6, Epic 6

**Cross-spec:** Consumed by SPEC-001 (store table creation), SPEC-002 (snapshot schema), SPEC-003 (reducer lookup).

---

## Summary

Implement the `SchemaRegistry` interface — the read-only, concurrent-safe, immutable view of all registered tables, indexes, and reducers. This is the primary contract consumed by other subsystems.

## Deliverables

- `SchemaRegistry` interface (as specified in §7).
- `schemaRegistry` concrete struct with immutable lookup maps/slices for tables, names, reducers, lifecycle hooks, and version.
- `func newSchemaRegistry(...) SchemaRegistry` — builds the registry from `Build()` output. All maps are populated once and never modified.

## Acceptance Criteria

- [ ] `Table(id)` / `TableByName(name)` return the correct schema when present and `nil, false` when absent
- [ ] `Tables()` returns user table IDs first, then system table IDs, in stable order
- [ ] `Tables()` returns a fresh slice each call so callers cannot mutate internal state
- [ ] `Reducer(name)` returns the registered handler or `nil, false`
- [ ] `Reducers()` returns names in registration order, excluding lifecycle reducers
- [ ] `OnConnect()` / `OnDisconnect()` return the registered lifecycle handler or nil
- [ ] `Version()` returns the schema version
- [ ] Registry is safe for concurrent reads because it is immutable after construction

## Design Notes

- Immutability is the concurrency strategy. No mutex is needed because nothing mutates after construction.
- `Tables()` and `Reducers()` return fresh slices to preserve internal ordering invariants.
- The interface is intentionally narrow so downstream subsystems cannot depend on mutable builder internals.
