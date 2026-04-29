# Story 5.5: Reducer & Schema-Level Validation

**Epic:** [Epic 5 — Validation, Build & SchemaRegistry](EPIC.md)
**Spec ref:** SPEC-006 §9, §13
**Depends on:** Epic 3 (Builder state to validate)
**Blocks:** Story 5.3 (Build calls validation)

**Cross-spec:** Validates reducer/lifecycle registration rules before SPEC-003 consumes the frozen registry.

---

## Summary

Validate reducer registrations and top-level schema configuration rules that are independent of per-table structure.

## Deliverables

- `func validateReducerAndSchemaRules(b *Builder) []error` — returns all reducer/schema-level validation errors.

  Reducer-level checks:
  - Reducer name must be non-empty
  - Reducer name must be unique → `ErrDuplicateReducerName`
  - Reducer names `"OnConnect"` and `"OnDisconnect"` are reserved → `ErrReservedReducerName`
  - At most one `OnConnect` registration and at most one `OnDisconnect` registration → `ErrDuplicateLifecycleReducer`
  - Nil reducer/lifecycle handlers are rejected here, not at registration time → `ErrNilReducerHandler`

  Schema-level checks:
  - `SchemaVersion` must have been called and must be greater than zero → `ErrSchemaVersionNotSet`
  - No user table may be named `sys_clients` or `sys_scheduled` → `ErrReservedTableName`
  - At least one user table must be registered before system tables are added → `ErrNoTables`

## Acceptance Criteria

All "→ ErrX" rows below are asserted by `errors.Is(err, ErrX)` against the error returned by `Build()`. The Build-time validation gates were canonicalized as part of **OI-011** (SPEC-006 §7/§13 closure, 2026-04-22) — prior to OI-011 several paths returned bare `fmt.Errorf` strings that did not match the sentinels. Post-OI-011, the paths below MUST return the canonical sentinel wrapped only via `fmt.Errorf("%w", ...)` so `errors.Is` matches through the wrap.

- [ ] Reducer name `""` → validation error (non-empty-name rule); duplicate reducer names → `errors.Is(err, ErrDuplicateReducerName)`
- [ ] Registering a normal reducer with name `"OnConnect"` or `"OnDisconnect"` → `errors.Is(err, ErrReservedReducerName)`
- [ ] Duplicate `OnConnect` or duplicate `OnDisconnect` registrations → `errors.Is(err, ErrDuplicateLifecycleReducer)`
- [ ] Nil reducer handler (`RegisterReducer(name, nil)`) OR nil lifecycle handler (`OnConnect(nil)` / `OnDisconnect(nil)`) → `errors.Is(err, ErrNilReducerHandler)`
- [ ] Missing `SchemaVersion()` or `SchemaVersion(0)` → `errors.Is(err, ErrSchemaVersionNotSet)`
- [ ] User table named `"sys_clients"` (or `"sys_scheduled"`) → `errors.Is(err, ErrReservedTableName)`
- [ ] No user tables registered → `errors.Is(err, ErrNoTables)`
- [ ] Multiple reducer/schema-level errors are returned in one pass

Authoritative pins (OI-011):
- `schema/oi011_pins_test.go` — reserved name, nil reducer, nil lifecycle (both `OnConnect(nil)` and `OnDisconnect(nil)`), duplicate `OnConnect`, duplicate `OnDisconnect`
- `schema/audit_regression_test.go` — migrated from `strings.Contains` assertions to `errors.Is` against these sentinels

## Design Notes

- Registration methods stay lightweight; this story is where policy is enforced.
- This story is the canonical owner for the reducer-oriented sentinels added in SPEC-006 §13: `ErrReservedReducerName`, `ErrNilReducerHandler`, and `ErrDuplicateLifecycleReducer`. OI-011 closed the final gap where those sentinels existed in `schema/errors.go` but were not consistently returned from `Build()`.
- Splitting reducer/schema validation from structural validation keeps both stories implementable without turning either into a grab-bag.
- `sys_*` name protection is kept with schema-level validation because it is about the global namespace, not any one table's internal structure.
