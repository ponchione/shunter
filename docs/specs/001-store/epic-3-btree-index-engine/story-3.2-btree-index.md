# Story 3.2: BTreeIndex Core

**Epic:** [Epic 3 — B-Tree Index Engine](EPIC.md)  
**Spec ref:** SPEC-001 §4.1, §4.2, §4.6  
**Depends on:** Story 3.1  
**Blocks:** Stories 3.3, 3.4, Epic 4

---

## Summary

The B-tree data structure: insert, remove, point lookup. Backed by an ordered map with IndexKey comparator.

## Deliverables

- `BTreeIndex` struct:
  ```go
  type BTreeIndex struct {
      tree ordered_map[IndexKey][]RowID
  }
  ```
  `ordered_map` is placeholder — use any B-tree package or custom tree supporting comparator-supplied ordering.

- `func NewBTreeIndex() *BTreeIndex`

- `func (idx *BTreeIndex) Insert(key IndexKey, rowID RowID)`
  - For non-unique keys: append RowID to existing entry, maintain ascending RowID order in slice

- `func (idx *BTreeIndex) Remove(key IndexKey, rowID RowID)`
  - Remove specific RowID from key's entry
  - If entry becomes empty, delete key from tree

- `func (idx *BTreeIndex) Seek(key IndexKey) []RowID`
  - Point lookup, returns all RowIDs for exact key match
  - Returns nil if key not found

- `func (idx *BTreeIndex) Len() int` — total number of key→RowID mappings

## Acceptance Criteria

- [ ] Insert key→RowID, Seek returns that RowID
- [ ] Insert same key with two different RowIDs, Seek returns both in ascending RowID order
- [ ] Remove key→RowID, Seek no longer returns that RowID
- [ ] Remove last RowID for a key, Seek returns nil
- [ ] Seek for non-existent key returns nil
- [ ] Insert 10k unique keys, Seek each — all found
- [ ] Len accurate after insert/remove cycles

## Design Notes

- Go B-tree options: `github.com/google/btree`, `github.com/tidwall/btree`, or stdlib-based sorted structures. Pick based on comparator support and iterator API. `tidwall/btree` has good generic support.
- RowID slice per key kept sorted via `slices.Insert` at binary-search position. Small slices (non-unique index fan-out is typically low) make this cheap.
