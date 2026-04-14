# Story 1.3: Query Hash

**Epic:** [Epic 1 — Predicate Types & Query Hash](EPIC.md)
**Spec ref:** SPEC-004 §3.4
**Depends on:** Story 1.1
**Blocks:** Epic 2 (Pruning Indexes — keyed by hash), Epic 4 (Subscription Manager — dedup)

---

## Summary

Deterministic blake3 hash of a predicate's canonical form. Used for deduplication: identical predicates share evaluation work.

## Deliverables

- `QueryHash` type: `[32]byte`

- `ComputeQueryHash(pred Predicate, clientID *Identity) QueryHash`
  - `clientID` nil → non-parameterized hash (structure only)
  - `clientID` non-nil → parameterized hash (structure + client identity)

- Canonical serialization:
  - Deterministic byte encoding of predicate tree (type tag + fields in fixed order)
  - Recursive for `And` and `Join.Filter`
  - `Value` serialized per its kind (fixed-size for numerics, length-prefixed for string/bytes)
  - `Bound` serialized as (unbounded flag, inclusive flag, value)
  - For parameterized: append client identity bytes after predicate bytes

- blake3 digest of canonical bytes → `[32]byte`

- `QueryHash.String()` for debug/logging (hex encoding)

## Acceptance Criteria

- [ ] Same predicate, no client → same hash
- [ ] Same predicate structure, different `Value` → different hash
- [ ] Same predicate, client A vs client B → different hash (parameterized)
- [ ] Same predicate, same client → same hash (parameterized)
- [ ] `And{Left: A, Right: B}` vs `And{Left: B, Right: A}` — different hash (order matters)
- [ ] `Join` with nil filter vs non-nil filter → different hash
- [ ] Round-trip: serialize → hash is deterministic across calls
- [ ] `QueryHash.String()` produces 64-char hex string

## Design Notes

- Canonical serialization is internal — not a wire format. Only requirement is determinism within a single binary version. No cross-version compatibility needed.
- `And` is not commutative for hashing. The predicate builder (§12.1) should produce a canonical ordering, but the hash function itself does not sort. This is intentional — it keeps hashing simple and pushes normalization to construction time.
- blake3 chosen for speed. It's a single dependency (`lukechampine.com/blake3` or `zeebo/blake3` in Go). Alternative: xxhash for speed if collision resistance not needed — but 32 bytes of blake3 is plenty fast and collision-resistant.
- v2 consideration: if a SQL parser is added later, it compiles to the same predicate structs, so hashing is unchanged.
