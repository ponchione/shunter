# Story 4.3: RegisterTable[T] Integration

**Epic:** [Epic 4 — Reflection-Path Registration](EPIC.md)
**Spec ref:** SPEC-006 §4, §4.1
**Depends on:** Story 4.2, Epic 3 (Builder.TableDef)
**Blocks:** Epic 5 (tables registered via this path need Build validation)

---

## Summary

Expose the developer-facing generic `RegisterTable[T]` API: validate the type parameter shape, invoke the reflection pipeline, and register the resulting `TableDefinition` with the builder.

## Deliverables

- `func RegisterTable[T any](b *Builder, opts ...TableOption) error`:
  1. Verify `T` is a non-pointer struct type
  2. Call `discoverFields(t, t.Name())`
  3. Call `buildTableDefinition(...)`
  4. Call `b.TableDef(def, opts...)`
  5. Return nil on success or the first construction error on failure

## Acceptance Criteria

- [ ] `RegisterTable[Player](b)` with a valid struct succeeds and calls `b.TableDef` internally
- [ ] `RegisterTable[PlayerSession](b, WithTableName("sessions"))` registers table name `"sessions"`
- [ ] `T` that is not a struct → error
- [ ] `T` that is a pointer-to-struct → error
- [ ] Result of reflection path matches equivalent builder-path `TableDef` for the same table
- [ ] Reflection-path registration with all supported field types followed by `Build()` succeeds

## Design Notes

- `RegisterTable` is a free function, not a method on `Builder`, because Go methods cannot introduce fresh type parameters.
- The `reflect.TypeOf((*T)(nil)).Elem()` pattern is the standard way to obtain `reflect.Type` from a type parameter without requiring a value.
