# Story 2.2: Table Struct + Row Storage

**Epic:** [Epic 2 — Schema & Table Storage](EPIC.md)  
**Spec ref:** SPEC-001 §3.2  
**Depends on:** Story 2.1  
**Blocks:** Story 2.3, Epic 4, Epic 5

---

## Summary

Bare table: stores rows by RowID, no indexes.

## Deliverables

- `Table` struct (initial, pre-index version):
  ```go
  type Table struct {
      schema  *TableSchema
      rows    map[RowID]ProductValue
      nextID  uint64
  }
  ```

- `NewTable(schema *TableSchema) *Table`

- `func (t *Table) AllocRowID() RowID` — monotonic, never reused

- `func (t *Table) InsertRow(id RowID, row ProductValue)` — store in map (no constraint checks yet; those come in Epic 4)

- `func (t *Table) DeleteRow(id RowID) (ProductValue, bool)` — remove from map, return old row

- `func (t *Table) GetRow(id RowID) (ProductValue, bool)` — lookup

- `func (t *Table) Scan() iter.Seq2[RowID, ProductValue]` — all rows, unordered

- `func (t *Table) RowCount() uint64`

## Acceptance Criteria

- [ ] Insert row, GetRow — matches
- [ ] Delete row, GetRow — not found
- [ ] AllocRowID returns strictly increasing values
- [ ] Delete does not reset counter — next alloc still increases
- [ ] Scan yields all live rows, none deleted
- [ ] RowCount accurate after insert/delete cycles
- [ ] Empty table: Scan yields nothing, RowCount is 0

## Design Notes

- This is the raw storage layer. No type validation, no constraint checks. Those layer on top in Stories 2.3 and Epic 4.
- `indexes` and `rowHashIndex` fields added in Epic 4 when indexes are wired in.
