# Story 2.1: Schema Structs + Table/Index ID Types

**Epic:** [Epic 2 — Schema & Table Storage](EPIC.md)  
**Spec ref:** SPEC-001 §3.1, §4.2  
**Depends on:** Epic 1 (ValueKind)  
**Blocks:** Story 2.2, Story 2.3

---

## Summary

Define the metadata that describes a table's shape.

## Deliverables

- `type TableID uint32`
- `type IndexID uint32`

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
      Index    int        // position in Columns slice
      Name     string
      Type     ValueKind
      Nullable bool       // always false in v1
  }
  ```

- `IndexSchema` struct:
  ```go
  type IndexSchema struct {
      ID      IndexID
      Name    string
      Columns []int      // column indices into TableSchema.Columns
      Unique  bool
      Primary bool       // at most one per table; implies Unique
  }
  ```

- Schema validation function:
  - Column indices contiguous 0..N-1
  - Index column refs within bounds
  - At most one Primary index
  - Primary implies Unique
  - No duplicate column names
  - No duplicate index names
  - If a primary index exists, its `IndexID` is `0`
  - Remaining index IDs are assigned in declaration order from the `TableSchema.Indexes` slice

## Acceptance Criteria

- [ ] Construct valid TableSchema — no error
- [ ] Two Primary indexes → validation error
- [ ] Primary index with `Unique: false` → validation error
- [ ] Index referencing out-of-bounds column → validation error
- [ ] Duplicate column names → validation error
- [ ] Duplicate index names → validation error
- [ ] Non-contiguous or mismatched `ColumnSchema.Index` values → validation error
- [ ] TableID and IndexID usable as map keys
- [ ] Table with primary index assigns `IndexID(0)` to that primary index
- [ ] Table with additional indexes assigns subsequent `IndexID` values in declaration order
