# Story 1.2: Value Equality

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.2 (Equality rules)  
**Depends on:** Story 1.1  
**Blocks:** Story 1.5

---

## Summary

Equality comparison following §2.2 rules.

## Deliverables

- `func (v Value) Equal(other Value) bool`
  - Different kinds → not equal (no cross-kind coercion)
  - Bool: `v.b == other.b`
  - Signed ints: `v.i64 == other.i64`
  - Unsigned ints: `v.u64 == other.u64`
  - Float32: `v.f32 == other.f32` (NaN excluded by construction)
  - Float64: `v.f64 == other.f64`
  - String: `v.str == other.str` (byte-for-byte UTF-8)
  - Bytes: `bytes.Equal(v.buf, other.buf)`

## Acceptance Criteria

- [ ] Same kind, same value → equal
- [ ] Same kind, different value → not equal
- [ ] Different kind, same numeric value (e.g., Int32(1) vs Uint32(1)) → not equal
- [ ] String: empty vs empty → equal
- [ ] Bytes: empty vs empty → equal
- [ ] Bytes: identical content → equal
- [ ] Float equality is numeric (no NaN edge case since construction rejects it)
