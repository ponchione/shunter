# Story 8.1: Sequence (Auto-Increment)

**Epic:** [Epic 8 — Auto-Increment & Recovery](EPIC.md)  
**Spec ref:** SPEC-001 §8  
**Depends on:** Epic 5 (Transaction.Insert)  
**Blocks:** Story 8.3

---

## Summary

Per-table auto-increment sequence. On insert, if the designated column's value is zero, replace with next sequence value.

## Deliverables

- `Sequence` struct:
  ```go
  type Sequence struct {
      next uint64
      mu   sync.Mutex
  }
  ```

- `func NewSequence(start uint64) *Sequence`

- `func (s *Sequence) Next() uint64` — returns current value and increments. Thread-safe.

- `func (s *Sequence) Peek() uint64` — current value without incrementing (for snapshot export)

- `func (s *Sequence) Reset(val uint64)` — set next value (for recovery restore)

- Integrate with Table:
  - Table gains optional `sequence *Sequence` field + `sequenceCol int` (column index)
  - Populated when schema has an autoincrement column (from SPEC-006)

- Integrate with Transaction.Insert:
  - Before constraint checks, if row's sequence column value is zero → replace with `sequence.Next()`
  - If row's sequence column is non-zero → use as-is (caller-provided value)

## Acceptance Criteria

- [ ] Insert with zero in sequence column → auto-assigned value, monotonically increasing
- [ ] Insert with non-zero in sequence column → value preserved as-is
- [ ] Three sequential inserts with zero → values are 1, 2, 3 (or start, start+1, start+2)
- [ ] Sequence.Next() is thread-safe under concurrent calls
- [ ] Peek returns current value without side effects
- [ ] Reset sets next value — subsequent Next() returns reset value
- [ ] Table without autoincrement column → no sequence, zero values stored as-is

## Design Notes

- Sequence values are persisted via snapshot (Story 8.3). On recovery, sequence is restored to its last known value to avoid reissuing IDs.
- The Mutex protects against concurrent access, though in v1 the single-writer model means contention is unlikely. Belt-and-suspenders.
- "Zero means auto-assign" is the convention from SPEC-006. Non-zero means the caller explicitly chose a value.
