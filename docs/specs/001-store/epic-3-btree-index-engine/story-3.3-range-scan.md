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

- `func (idx *BTreeIndex) SeekBounds(low, high Bound) iter.Seq[RowID]`
  - Bound-parameterized range scan for callers that need explicit inclusive/exclusive control per endpoint (SPEC-001 §7.2 `CommittedReadView.IndexRange`, SPEC-004 predicate scans on string/bytes/float keys where "strictly greater than v" cannot be expressed through `*IndexKey` alone).
  - `Bound.Unbounded = true` → no limit on that side; `Bound.Value` ignored.
  - `Bound.Inclusive = true` → closed endpoint (`<=` / `>=`); `Inclusive = false` → open (`<` / `>`).
  - Yields RowIDs in key order; within same key, RowIDs in ascending order.

- `func (idx *BTreeIndex) Scan() iter.Seq[RowID]`
  - Full ordered iteration over all keys
  - Equivalent to `SeekRange(nil, nil)`

- Range boundary semantics:
  - `SeekRange(low, high *IndexKey)` is the half-open `[low, high)` convenience wrapper: `nil` low/high = unbounded; matches spec §4.6 `SeekRange returns all RowIDs with key in [low, high)`.
  - `SeekBounds(low, high Bound)` is the general primitive; `SeekRange(a, b)` is equivalent to `SeekBounds(Bound{Value: *a, Inclusive: true}, Bound{Value: *b, Inclusive: false})` with nil endpoints mapping to `Bound{Unbounded: true}`.

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
- [ ] SeekBounds with `Bound{Value: v, Inclusive: false}` on both sides yields keys in `(low, high)`
- [ ] SeekBounds with one `Bound{Unbounded: true}` endpoint yields unbounded-on-that-side
- [ ] SeekBounds and SeekRange produce identical results for the half-open case (`Inclusive: true` low, `Inclusive: false` high)

## Design Notes

- `iter.Seq[RowID]` (Go 1.23+ range-over-func) is the right return type. Callers can break early without materializing the full scan.
- The `[low, high)` half-open `SeekRange` stays as a convenience wrapper. `SeekBounds` is the v1 primitive for callers that need explicit inclusive/exclusive control — SPEC-004 predicate scans on string/bytes/float keys cannot express "strictly greater than v" through `*IndexKey` alone (SPEC-AUDIT SPEC-001 §1.2).
