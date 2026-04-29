# Story 3.3: Join Delta Fragments (IVM)

**Epic:** [Epic 3 — DeltaView & Delta Computation](EPIC.md)
**Spec ref:** SPEC-004 §6.2, §6.4
**Depends on:** Story 3.1 (DeltaView)
**Blocks:** Story 3.4 (Bag Dedup), Story 3.5

---

## Summary

Incremental view maintenance for two-table join subscriptions. Expands `(T1 + dT1) join (T2 + dT2)` into 4 insert and 4 delete fragments.

## Deliverables

- `type JoinFragments struct { Inserts [4][]ProductValue; Deletes [4][]ProductValue }`
  - fixed 8-fragment output in I1..I4 / D1..D4 order

- `EvalJoinDeltaFragments(dv *DeltaView, join *Join, resolver IndexResolver) JoinFragments`
  - returns all 8 fragment result sets in one named struct rather than two parallel slices
  - `resolver` supplies committed-side join-column `IndexID` values for the committed probes used by the T' fragments

- Fragment definitions (per §6.2):
  ```
  Insert:
    I1: dT1(+) join T2'       — delta index scan on dT1 inserts, committed+delta on T2
    I2: T1'    join dT2(+)    — committed+delta on T1, delta index scan on dT2 inserts
    I3: dT1(+) join dT2(-)   — delta-only both sides
    I4: dT1(-) join dT2(+)   — delta-only both sides

  Delete:
    D1: dT1(-) join T2'
    D2: T1'    join dT2(-)
    D3: dT1(+) join dT2(+)   — cancellation fragment
    D4: dT1(-) join dT2(-)   — cancellation fragment
  ```

- Each fragment:
  1. Iterate the "driving" side (delta rows or committed rows)
  2. For each row, extract join column value
  3. Look up matching rows on the "probe" side via index scan (delta or committed)
  4. For each match, apply optional `Join.Filter` if non-nil
  5. Produce joined output row

- Use `DeltaView.DeltaIndexScan` for delta-side lookups
- Use `DeltaView.CommittedIndexSeek` plus `CommittedView().GetRow(...)` for committed-side lookups
- For fragments referencing T' (post-transaction state): use committed seek/materialization on the post-commit view already attached to `DeltaView`

## Acceptance Criteria

- [ ] dT1(+) with matching T2 row → I1 produces joined row
- [ ] dT1(+) with no matching T2 row → I1 empty
- [ ] T1 committed row with dT2(+) match → I2 produces joined row
- [ ] Both sides have inserts → I3 and D3 produce cancellation pairs
- [ ] Both sides have deletes → I4 and D4 produce cancellation pairs
- [ ] Join.Filter applied: matching rows pass, non-matching excluded
- [ ] All 8 fragments produced even when some are empty
- [ ] Fragment execution uses index lookups (not full table scans) when delta indexes available
- [ ] Known T1/T2/dT1/dT2 fixture → reconciled join delta matches full re-evaluation of the join exactly
- [ ] Benchmark: 8 fragments for 1 join subscription, 10 changed rows → < 10 ms

## Design Notes

- "T1'" (post-transaction T1) in fragments I2 and D1 means the committed state *after* this transaction committed. The DeltaView's committed view already reflects this (it's acquired after commit).
- Fragments I3, I4, D3, D4 involve only delta rows on both sides. These are typically small but necessary for correctness — they handle the edge case where both tables change in the same transaction.
- The 4+4 structure is algebraically derived. Skipping any fragment produces incorrect deltas. The bag dedup step (Story 3.4) resolves the apparent contradictions.
- v2 deferral: semijoin optimization (only return LHS rows, not joined pairs) is not needed for v1.
