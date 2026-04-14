# Story 3.2: Reducer Registration

**Epic:** [Epic 3 — Builder & Builder-Path Registration](EPIC.md)
**Spec ref:** SPEC-006 §4, §4.3
**Depends on:** Story 3.1
**Blocks:** Epic 5 (validation of reducer registrations)

**Cross-spec:** `ReducerHandler` type and `ReducerContext` from SPEC-003 §10.

---

## Summary

Register named reducers and lifecycle hooks (OnConnect, OnDisconnect) on the builder. Accumulates registrations for later validation in `Build()`.

## Deliverables

- `ReducerHandler` type alias (re-exported from SPEC-003 or defined here if SPEC-003 package not yet built):
  ```go
  type ReducerHandler func(ctx *ReducerContext, argBSATN []byte) ([]byte, error)
  ```

- `func (b *Builder) Reducer(name string, h ReducerHandler) *Builder`
  — stores handler by name. No validation here (deferred to Build).

- `func (b *Builder) OnConnect(h func(*ReducerContext) error) *Builder`
  — stores the OnConnect lifecycle handler and increments an internal registration count so `Build()` can reject duplicate lifecycle registrations deterministically.

- `func (b *Builder) OnDisconnect(h func(*ReducerContext) error) *Builder`
  — stores the OnDisconnect lifecycle handler and increments an internal registration count so `Build()` can reject duplicate lifecycle registrations deterministically.

## Acceptance Criteria

- [ ] `Reducer("CreatePlayer", handler)` stores a handler retrievable by name; multiple reducer names accumulate
- [ ] Calling `Reducer` twice with the same name preserves duplicate registration state for later `Build()` validation
- [ ] `OnConnect` and `OnDisconnect` store their lifecycle handlers
- [ ] Duplicate `OnConnect` / `OnDisconnect` registrations preserve the latest handler and duplicate-registration count for later validation
- [ ] All methods return `*Builder` for chaining
- [ ] `nil` reducer or lifecycle handlers are stored as-is and rejected later during validation

## Design Notes

- Lifecycle reducers have a different signature (`func(*ReducerContext) error`) than regular reducers (`ReducerHandler`). This is intentional: lifecycle reducers receive no caller arguments and return no data, only success/failure.
- The spec reserves `"OnConnect"` and `"OnDisconnect"` as reducer names — registering a regular reducer with those names is an error caught at Build time, not here.
- Duplicate lifecycle registrations must remain observable until `Build()`. A simple overwrite-only implementation is insufficient because it would erase the information needed for deterministic validation.
- Typed reducer helpers (auto-deserialize arguments, serialize return) are explicitly out of scope for v1.
