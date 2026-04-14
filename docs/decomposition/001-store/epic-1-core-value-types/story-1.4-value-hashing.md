# Story 1.4: Value Hashing

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.2 (Hashing rules)  
**Depends on:** Story 1.1  
**Blocks:** Story 1.5

---

## Summary

Hash function for set-semantics duplicate detection.

## Deliverables

- `func (v Value) Hash(h hash.Hash64)` — feeds canonical bytes into provided hasher
  - Hash over `(kind, canonical payload bytes)`:
    - Bool: kind byte + 0x00 or 0x01
    - Signed ints: kind byte + 8-byte big-endian of `i64`
    - Unsigned ints: kind byte + 8-byte big-endian of `u64`
    - Float32: kind byte + 4-byte `math.Float32bits` encoding
    - Float64: kind byte + 8-byte `math.Float64bits` encoding
    - String: kind byte + raw UTF-8 bytes
    - Bytes: kind byte + raw bytes

- Convenience: `func (v Value) Hash64() uint64` — creates hasher, feeds value, returns sum

## Acceptance Criteria

- [ ] Equal values produce equal hashes
- [ ] Different kinds with same bit pattern produce different hashes (kind is part of hash input)
- [ ] String("abc") and Bytes([]byte("abc")) produce different hashes
- [ ] Empty string and empty bytes produce different hashes
- [ ] Hashing is deterministic across calls

## Design Notes

- Use `maphash` or `fnv` — doesn't matter for v1, just be consistent. `maphash` is faster but non-deterministic across processes. Since hashes are only used in-memory (never persisted), `maphash` is fine.
- If using `maphash`, seed must be fixed per-store instance for consistency within a process lifetime.
