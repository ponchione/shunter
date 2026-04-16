# Story 4.1: Index Wrapper + Table Index Initialization

**Epic:** [Epic 4 — Table Indexes & Constraints](EPIC.md)  
**Spec ref:** SPEC-001 §4.2, §4.5  
**Depends on:** Epic 2 (Table), Epic 3 (BTreeIndex, IndexKey)  
**Blocks:** Stories 4.2, 4.3, 4.4

---

## Summary

Wrap BTreeIndex with schema metadata. Wire indexes into Table at construction time.

## Deliverables

- `Index` struct:
  ```go
  type Index struct {
      schema  *IndexSchema
      btree   *BTreeIndex
  }
  ```
  - uniqueness and primary-ness are derived from `schema.Unique` / `schema.Primary`

- `func NewIndex(schema *IndexSchema) *Index`
  - Stores the schema pointer as the single source of truth for uniqueness/primary metadata
  - Creates empty BTreeIndex

- `func (idx *Index) ExtractKey(row ProductValue) IndexKey`
  - Builds IndexKey from `row[schema.Columns[0]], row[schema.Columns[1]], ...`
  - Reuses `ExtractKey` utility from Story 3.4

- Extend `Table` struct with `indexes` field:
  ```go
  type Table struct {
      schema       *TableSchema
      rows         map[RowID]ProductValue
      nextID       uint64
      indexes      []*Index    // one per IndexSchema, same order
  }
  ```

- Update `NewTable` to create indexes from `schema.Indexes`

- `func (t *Table) IndexByID(id IndexID) *Index` — lookup by IndexID

- `func (t *Table) PrimaryIndex() *Index` — returns primary index or nil

## Acceptance Criteria

- [ ] NewTable with 3 IndexSchemas creates 3 Index instances
- [ ] Each Index has the correct schema; uniqueness and primary-ness are derived from `schema.Unique` / `schema.Primary`
- [ ] IndexByID returns correct index
- [ ] PrimaryIndex returns the primary one, or nil if none
- [ ] ExtractKey for single-column index produces 1-part key
- [ ] ExtractKey for multi-column index produces N-part key in correct order
- [ ] Table with no indexes: indexes slice is empty, PrimaryIndex returns nil
