# Story 3.5: Allocation Discipline

**Epic:** [Epic 3 — DeltaView & Delta Computation](EPIC.md)
**Spec ref:** SPEC-004 §9.2
**Depends on:** Stories 3.1–3.4
**Blocks:** Nothing (optimization layer)

---

## Summary

Hot-path allocation minimization. Applied across DeltaView, delta computation, and dedup after correctness is proven.

## Deliverables

- `sync.Pool` for `[]byte` buffers used in delta encoding:
  - Default buffer size: 4 KiB
  - Oversized buffers returned to runtime (not pooled)

- Slice reuse for `DeltaView.inserts` and `DeltaView.deletes`:
  - Pool of `[]ProductValue` slices, reset (length=0, capacity retained) between transactions

- Map reuse for candidate `HashSet` and bag-dedup count maps:
  - Allocated once per evaluation goroutine
  - Cleared (not reallocated) per transaction

- Byte comparison for row dedup:
  - `encodeRowKey` (from Story 3.4) produces `[]byte` used as map key via `string(bytes)` conversion
  - No `interface{}` equality on hot path

## Acceptance Criteria

- [ ] Buffer pool: get buffer, return, get again → reuses same allocation (verify via capacity check)
- [ ] Buffer pool: oversized buffer not retained (returned to GC)
- [ ] Slice reuse: DeltaView slices cleared and reused across sequential transactions
- [ ] Map reuse: dedup maps cleared, not reallocated between transactions
- [ ] No `reflect.DeepEqual` or `interface{}` comparison in delta computation hot path
- [ ] Benchmark: 1000 sequential transaction evaluations — allocation count stable (no growth)

## Design Notes

- This story is intentionally last in the epic. Get correctness right in Stories 3.1–3.4, then optimize allocations. Premature pooling makes debugging harder.
- `sync.Pool` is per-goroutine (well, per-P). Since the evaluator runs on the executor goroutine (single-threaded), pool contention is zero. But `sync.Pool` is still the right abstraction — it handles GC pressure correctly.
- `string(bytes)` conversion for map keys in Go does not allocate when used as a map key directly (compiler optimization). Verify this with benchmarks.
- v2 consideration: arena allocation for per-transaction temporaries could further reduce GC pressure, but is not needed for v1 targets.
