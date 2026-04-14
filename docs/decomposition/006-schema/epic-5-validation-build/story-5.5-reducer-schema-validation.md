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
  - Reducer names `"OnConnect"` and `"OnDisconnect"` are reserved
  - At most one `OnConnect` registration and at most one `OnDisconnect` registration
  - Nil reducer/lifecycle handlers are rejected here, not at registration time

  Schema-level checks:
  - `SchemaVersion` must have been called and must be greater than zero → `ErrSchemaVersionNotSet`
  - No user table may be named `sys_clients` or `sys_scheduled` → `ErrReservedTableName`
  - At least one user table must be registered before system tables are added → `ErrNoTables`

## Acceptance Criteria

- [ ] Reducer name `""` or duplicate reducer names → validation error / `ErrDuplicateReducerName` as appropriate
- [ ] Registering a normal reducer with name `"OnConnect"` or `"OnDisconnect"` → validation error
- [ ] Duplicate `OnConnect` or duplicate `OnDisconnect` registrations → validation error
- [ ] Nil reducer or lifecycle handler → validation error
- [ ] Missing `SchemaVersion()` or `SchemaVersion(0)` → `ErrSchemaVersionNotSet`
- [ ] User table named `"sys_clients"` (or `"sys_scheduled"`) → `ErrReservedTableName`
- [ ] No user tables registered → `ErrNoTables`
- [ ] Multiple reducer/schema-level errors are returned in one pass

## Design Notes

- Registration methods stay lightweight; this story is where policy is enforced.
- Splitting reducer/schema validation from structural validation keeps both stories implementable without turning either into a grab-bag.
- `sys_*` name protection is kept with schema-level validation because it is about the global namespace, not any one table's internal structure.
