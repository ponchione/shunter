# Story 1.1: Schema Struct Types

**Epic:** [Epic 1 — Schema Types & Type Mapping](EPIC.md)
**Spec ref:** SPEC-006 §8
**Depends on:** Nothing
**Blocks:** Stories 1.2, Epic 3, Epic 5

**Cross-spec:** Re-exports `ValueKind` from SPEC-001 §2.1 and defines `TableSchema` / `IndexSchema` consumed by SPEC-001 §3.

---

## Summary

Core data structures that describe table schemas. These types are constructed during registration and consumed read-only by the store, commit log, executor, and protocol subsystems.

## Deliverables

- `TableID` — `uint32`, stable identifier assigned by the builder

- `IndexID` — `uint32`, stable identifier assigned by the builder

- `TableSchema` struct:
  ```go
  type TableSchema struct {
      ID      TableID
      Name    string
      Columns []ColumnSchema
      Indexes []IndexSchema
  }
  ```

- `ColumnSchema` struct:
  ```go
  type ColumnSchema struct {
      Index    int       // 0-based position in Columns
      Name     string
      Type     ValueKind // from SPEC-001
      Nullable bool      // always false in v1
  }
  ```

- `IndexSchema` struct:
  ```go
  type IndexSchema struct {
      ID      IndexID
      Name    string
      Columns []int  // column indices into TableSchema.Columns, in key order
      Unique  bool
      Primary bool   // at most one per table; implies Unique
  }
  ```

- Convenience methods:
  - `TableSchema.Column(name string) (*ColumnSchema, bool)` — lookup by name
  - `TableSchema.PrimaryIndex() (*IndexSchema, bool)` — returns the primary index if one exists

## Acceptance Criteria

- [ ] `TableID` and `IndexID` are distinct named types over `uint32`
- [ ] `TableSchema` stores columns in ordered slice, indexes in ordered slice
- [ ] `ColumnSchema.Index` matches position in parent `Columns` slice
- [ ] `IndexSchema.Primary` implies `Unique` (struct invariant documented, enforced at construction)
- [ ] `Column("name")` returns correct `ColumnSchema`; unknown name returns `false`
- [ ] `PrimaryIndex()` returns the primary index or `false` when none declared
- [ ] All struct types are exported and JSON-serializable (for schema export in Epic 6)

## Design Notes

- `Nullable` is always `false` in v1. The field exists to avoid a breaking type change when nullable columns are added in v2.
- These types are value-like: they are constructed once during `Build()` and never mutated. Concurrent reads are safe without synchronization.
