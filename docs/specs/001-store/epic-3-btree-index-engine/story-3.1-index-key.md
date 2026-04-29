# Story 3.1: IndexKey + Bound Types

**Epic:** [Epic 3 — B-Tree Index Engine](EPIC.md)  
**Spec ref:** SPEC-001 §4.2, §4.3, §4.4  
**Depends on:** Epic 1 (Value, Value.Compare)  
**Blocks:** Stories 3.2, 3.3, 3.4

---

## Summary

Key representation and comparison for B-tree entries. Bound type for range scan endpoints.

## Deliverables

- `IndexKey` struct:
  ```go
  type IndexKey struct {
      parts []Value
  }
  ```

- `func NewIndexKey(parts ...Value) IndexKey`

- `func (k IndexKey) Compare(other IndexKey) int`
  - Lexicographic: compare position 0, if equal compare position 1, etc.
  - Uses `Value.Compare` per position
  - If all compared positions equal and lengths match → equal
  - Shorter key < longer key when all shared positions equal

- `func (k IndexKey) Equal(other IndexKey) bool` — convenience over Compare

- `func (k IndexKey) Len() int` — number of parts

- `Bound` struct:
  ```go
  type Bound struct {
      Value     Value
      Inclusive bool   // true = closed (<=/>= ); false = open (</>)
      Unbounded bool   // true = no limit; Value ignored
  }
  ```

- Convenience constructors:
  - `UnboundedLow() Bound` — no lower limit
  - `UnboundedHigh() Bound` — no upper limit
  - `Inclusive(v Value) Bound`
  - `Exclusive(v Value) Bound`

## Acceptance Criteria

- [ ] Single-part key: comparison matches Value.Compare
- [ ] Multi-part key: `(A,1) < (A,2) < (B,1)` ordering
- [ ] Equal keys: `(A,1).Compare((A,1)) == 0`
- [ ] Prefix ordering: `(A) < (A,1)` when shorter key is prefix
- [ ] Bound construction: Unbounded ignores Value field
- [ ] Bound construction: Inclusive/Exclusive set correct flags
