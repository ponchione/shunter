# Story 5.3: Build() Orchestration & TableID Assignment

**Epic:** [Epic 5 — Validation, Build & SchemaRegistry](EPIC.md)
**Spec ref:** SPEC-006 §5, §5.1 (Freeze Semantics), §5.2 (Engine Boot Ordering), §7
**Depends on:** Story 5.1, Story 5.2, Story 5.5
**Blocks:** Story 5.4

**Cross-spec:** `Build()` is the freeze step in the engine boot sequence (§5.2). The immutable registry it returns is consumed in steps 4–7 by SPEC-001/002/003/004/005. `Start()` remains the runtime integration boundary for SPEC-002 and SPEC-003.

---

## Summary

The `Build()` method validates all registrations, assigns stable IDs, and constructs the immutable `Engine` with its `SchemaRegistry`. This is the terminal step of the registration phase.

## Deliverables

- `func (b *Builder) Build(opts EngineOptions) (*Engine, error)`:

  Algorithm:
  1. If the builder is already frozen (a prior `Build()` succeeded or any mutator was called after `Build()`), return `ErrAlreadyBuilt` without mutating state.
  2. Run `validateStructure(b)` (Story 5.1) and `validateReducerAndSchemaRules(b)` (Story 5.5). If either reports errors → return joined multi-error.
  3. Call `registerSystemTables(b)` (Story 5.2) to append `sys_clients` and `sys_scheduled`.
  4. Assign `TableID` to each table:
     - User tables receive IDs starting from 0, in registration order
     - `sys_clients` receives the next ID after user tables
     - `sys_scheduled` receives the next ID after `sys_clients`
     - Same registration inputs → same IDs across runs
  5. For each table, synthesize the primary `IndexSchema` from the PK column (if any).
  6. Assign `IndexID` to each index per table (starting from 0 per table, PK index first if present).
  7. Build `[]TableSchema` from `[]TableDefinition` + assigned IDs.
  8. Construct `SchemaRegistry` (Story 5.4). The registry must satisfy `SchemaLookup`, `IndexResolver`, and `SchemaRegistry` (SPEC-006 §7) without further wrapping.
  9. Construct `Engine` with registry, options, and builder state.
  10. **Set the freeze flag.** From this point on every mutator (`TableDef`, `Reducer`, `OnConnect`, `OnDisconnect`, `SchemaVersion`) returns `ErrAlreadyBuilt` on the same `*Builder` instance and a second `Build()` call also returns `ErrAlreadyBuilt`.
  11. Return `(*Engine, nil)`.

- `Engine` struct (minimal — subsystem wiring happens at `Start()`):
  ```go
  type Engine struct {
      registry SchemaRegistry
      opts     EngineOptions
      // ... subsystem fields populated by Start()
  }
  ```

- `func (e *Engine) Start(ctx context.Context) error` — deferred to SPEC-002/003 integration. Stub here.

## Acceptance Criteria

- [ ] Valid builder → `Build()` returns non-nil `*Engine`, nil error
- [ ] Validation error from either structural or reducer/schema validation → `Build()` returns nil `*Engine`, non-nil multi-error
- [ ] Reflection-path registration with all supported field types followed by `Build()` → nil error
- [ ] User tables get IDs in registration order; `sys_clients` and `sys_scheduled` follow afterward deterministically
- [ ] PK column produces synthesized primary `IndexSchema`; no PK column produces none
- [ ] IndexIDs are assigned per table with the PK index at ID 0 when present
- [ ] `Build()` is synchronous and starts no goroutines / opens no files
- [ ] Second `Build()` call on the same builder returns `ErrAlreadyBuilt` and does not append duplicate system tables
- [ ] After a successful `Build()`, calling `TableDef`, `Reducer`, `OnConnect`, `OnDisconnect`, or `SchemaVersion` on the same `*Builder` returns `ErrAlreadyBuilt` and does not mutate state
- [ ] The returned `SchemaRegistry` satisfies the `SchemaLookup` and `IndexResolver` sub-interfaces (SPEC-006 §7) without additional adapters

## Design Notes

- `Build()` is deliberately synchronous and side-effect-free apart from freezing in-memory configuration. Runtime initialization belongs to `Start()`.
- TableID assignment depends only on registration order, preserving deterministic snapshot compatibility.
- Returning an error on repeated `Build()` is simpler and safer than trying to make system-table insertion idempotent.
