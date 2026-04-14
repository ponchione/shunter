# Story 1.3: Value Ordering

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.2 (Ordering rules)  
**Depends on:** Story 1.1  
**Blocks:** Epic 3 (B-Tree Index Engine)

---

## Summary

Total order over Values, used by B-tree indexes.

## Deliverables

- `func (v Value) Compare(other Value) int` — returns -1, 0, +1
  - **Precondition:** `v.kind == other.kind`. Cross-kind comparison is a caller bug — panic.
  - Bool: `false < true`
  - Signed ints: numeric comparison on `i64`
  - Unsigned ints: numeric comparison on `u64`
  - Float32: numeric comparison on `f32` (total because no NaN)
  - Float64: numeric comparison on `f64`
  - String: `strings.Compare` (lexicographic UTF-8 bytes)
  - Bytes: `bytes.Compare` (lexicographic raw bytes)

## Acceptance Criteria

- [ ] `false.Compare(true) == -1`
- [ ] `Int64(-1).Compare(Int64(1)) == -1`
- [ ] `Uint64(0).Compare(Uint64(MAX)) == -1`
- [ ] `Float64(-0.0).Compare(Float64(0.0)) == 0` (IEEE: -0 == +0)
- [ ] String: `"abc" < "abd"`, `"ab" < "abc"`
- [ ] Bytes: `[]byte{0x00} < []byte{0x01}`, `[]byte{} < []byte{0x00}`
- [ ] Cross-kind Compare panics
- [ ] Symmetry: `a.Compare(b) == -b.Compare(a)`
- [ ] Transitivity: if `a < b` and `b < c` then `a < c`
