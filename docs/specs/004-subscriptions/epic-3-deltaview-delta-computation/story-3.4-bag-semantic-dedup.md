# Story 3.4: Bag-Semantic Deduplication

**Epic:** [Epic 3 — DeltaView & Delta Computation](EPIC.md)
**Spec ref:** SPEC-004 §6.3
**Depends on:** Story 3.3 (Join Delta Fragments)
**Blocks:** Epic 5 (Evaluation Loop), Story 3.5

---

## Summary

Reconcile the 8 join fragments using bag semantics — count-based insert/delete cancellation that preserves multiplicity.

## Deliverables

- `ReconcileJoinDelta(insertFragments, deleteFragments [][]ProductValue) (inserts, deletes []ProductValue)`

- Algorithm (per §6.3):
  ```
  Phase 1: Count inserts
    insertCounts := map[key]int{}
    for each row in all insert fragments: insertCounts[key(row)]++

  Phase 2: Count deletes with cancellation
    deleteCounts := map[key]int{}
    for each row in all delete fragments:
      if insertCounts[key(row)] > 0: insertCounts[key(row)]--
      else: deleteCounts[key(row)]++

  Phase 3: Materialize
    inserts = rows with insertCounts > 0 (repeated per count)
    deletes = rows with deleteCounts > 0 (repeated per count)
  ```

- Row keying: direct byte comparison of encoded rows (not `interface{}` equality). Rows encoded to bytes once, used as map key.

- `encodeRowKey(row ProductValue) []byte` — deterministic byte encoding for map key use

## Acceptance Criteria

- [ ] Row in insert fragments only → appears in final inserts
- [ ] Row in delete fragments only → appears in final deletes
- [ ] Row in both insert and delete (1 each) → cancels, neither output
- [ ] Row in insert ×3, delete ×1 → appears 2× in final inserts
- [ ] Row in insert ×1, delete ×3 → appears 2× in final deletes
- [ ] Multiplicity preserved: T1 row joining 3 T2 rows, delete 1 T2 → 1 delete in output
- [ ] Empty fragments → empty output
- [ ] Byte-encoded key comparison: structurally equal rows from different fragments match
- [ ] Negative counts never occur (invariant — panic if detected per §11.3)

## Design Notes

- Bag semantics (not set semantics) is required because a semijoin may produce multiple copies of the same LHS row (one per matching RHS row). Deduplicating to set semantics would cause client views to diverge.
- `encodeRowKey` uses the same encoding as SPEC-002's BSATN or a simpler fixed format — only requirement is determinism within a process. Not a wire format.
- The negative-count panic is a correctness invariant: if the IVM algebra is correct, delete counts can never exceed the multiplicity of a row in the current view. A negative count means a bug in fragment computation.
- Map reuse across transactions (§9.2): these maps should be allocated once and cleared, not reallocated. Deferred to Story 3.5.
