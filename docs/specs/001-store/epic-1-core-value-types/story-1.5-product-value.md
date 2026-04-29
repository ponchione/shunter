# Story 1.5: ProductValue + Row Operations

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.2  
**Depends on:** Story 1.2 (equality), Story 1.4 (hashing)  
**Blocks:** Epic 2 (Schema & Table Storage)

---

## Summary

The row type: an ordered slice of Values.

## Deliverables

- `type ProductValue []Value`

- `func (pv ProductValue) Equal(other ProductValue) bool`
  - Length mismatch → not equal
  - Element-wise `Value.Equal`

- `func (pv ProductValue) Hash(h hash.Hash64)`
  - Feed each Value's hash input sequentially into hasher
  - Include column count or separator to avoid collisions: `("a", "bc")` vs `("ab", "c")`

- `func (pv ProductValue) Hash64() uint64` — convenience

- `func (pv ProductValue) Copy() ProductValue`
  - Deep copy: new slice, each Value's `buf` field copied for Bytes kind
  - String values share underlying memory (Go strings immutable)

## Acceptance Criteria

- [ ] Two ProductValues with same values in same order → equal
- [ ] Different order → not equal
- [ ] Different length → not equal
- [ ] Hash: equal rows produce equal hashes
- [ ] Hash: `("a", "bc")` vs `("ab", "c")` produce different hashes
- [ ] Copy: mutating copy does not affect original
- [ ] Copy: mutating original Bytes column does not affect copy
- [ ] Empty ProductValue (len 0) — equality and hashing work

## Design Notes

- Hash collision resistance for ProductValue: hash each column's canonical bytes with a length prefix or fixed-width column separator. Simple approach: for each column, write `(kind_byte, len_u32, payload_bytes)`. The length prefix prevents the `("a","bc")` vs `("ab","c")` collision.
