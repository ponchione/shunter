# Story 3.3: Range Scan + Full Scan

**Epic:** [Epic 3 — B-Tree Index Engine](EPIC.md)  
**Spec ref:** SPEC-001 §4.4, §4.6  
**Depends on:** Story 3.2  
**Blocks:** Epic 4, Epic 5 (SeekIndexRange), Epic 7 (IndexRange)

---

## Summary

Bounded range iteration and full ordered scan over the B-tree.

## Deliverables

- `func (idx *BTreeIndex) SeekRange(low, high *IndexKey) iter.Seq[RowID]`
  - Nil low → unbounded below
  - Nil high → unbounded above
  - Yields RowIDs for all keys in range, in key order
  - Within same key, RowIDs in ascending order

- `func (idx *BTreeIndex) Scan() iter.Seq[RowID]`
  - Full ordered iteration over all keys
  - Equivalent to `SeekRange(nil, nil)`

- Range boundary semantics follow Bound type from Story 3.1:
  - When using `*IndexKey` (nil = unbounded), the range is `[low, high)` — low inclusive, high exclusive
  - This matches spec §4.6: `SeekRange returns all RowIDs with key in [low, high)`

## Acceptance Criteria

- [ ] Range [A, C) over keys A,B,C,D → yields A and B entries
- [ ] Nil low, high=C → yields everything below C
- [ ] Low=B, nil high → yields B and everything above
- [ ] Both nil → full scan, same as Scan()
- [ ] Empty range (low >= high) → yields nothing
- [ ] RowIDs within same key yielded in ascending order
- [ ] Keys yielded in comparator order
- [ ] Scan over 10k entries yields all in order
- [ ] Iterator is lazy (doesn't materialize full result)

## Design Notes

- `iter.Seq[RowID]` (Go 1.23+ range-over-func) is the right return type. Callers can break early without materializing the full scan.
- The `[low, high)` half-open convention matches the spec. If Bound-based semantics (inclusive/exclusive per endpoint) are needed later, add a `SeekBounds(low, high Bound)` variant in a future story.
