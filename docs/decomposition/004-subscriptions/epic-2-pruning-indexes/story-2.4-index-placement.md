# Story 2.4: Index Placement Logic

**Epic:** [Epic 2 — Pruning Indexes](EPIC.md)
**Spec ref:** SPEC-004 §5.4
**Depends on:** Stories 2.1, 2.2, 2.3
**Blocks:** Epic 4 (Subscription Manager — uses placement on register/unregister)

---

## Summary

Route each `(query, table)` pair to exactly one pruning tier. A subscription touching two tables may land in different tiers for each table.

## Deliverables

- `PruningIndexes` composite struct holding all three indexes:
  ```go
  type PruningIndexes struct {
      Value    *ValueIndex
      JoinEdge *JoinEdgeIndex
      Table    *TableIndex
  }
  ```

- `PlaceSubscription(indexes *PruningIndexes, pred Predicate, hash QueryHash)` — for each table in `pred.Tables()`, place into exactly one tier:
  1. If predicate has a `ColEq` on this table → **ValueIndex** (add per ColEq value)
  2. Else if predicate is a `Join` with a filterable edge involving this table → **JoinEdgeIndex**
  3. Else → **TableIndex** fallback

- `RemoveSubscription(indexes *PruningIndexes, pred Predicate, hash QueryHash)` — reverse of placement, removes from the correct tier for each table

- `CollectCandidatesForTable(indexes *PruningIndexes, table TableID, rows []ProductValue, committed CommittedReadView) []QueryHash` — union results from all three tiers for a changed table

## Acceptance Criteria

- [ ] `ColEq` predicate → placed in ValueIndex only
- [ ] `AllRows` predicate → placed in TableIndex only
- [ ] `ColRange` predicate → placed in TableIndex (no equality, falls through)
- [ ] `Join` with `ColEq` filter on RHS → LHS changes: JoinEdgeIndex; RHS changes: ValueIndex
- [ ] `And{ColEq on T1, ColEq on T2}` → each table in ValueIndex
- [ ] Place then remove → all indexes return to empty state
- [ ] Two-table subscription: different tiers for each table allowed
- [ ] CollectCandidates unions results from all three tiers (no duplicates in output)
- [ ] Registration/deregistration symmetry: place + remove = no residual state

## Design Notes

- Placement decision is per `(query, table)` pair, not per query. A join subscription may use ValueIndex for one side and JoinEdgeIndex for the other.
- "Filterable edge" means the join has a `ColEq` filter on one side that can be used to parameterize the JoinEdge lookup. A join with only `ColRange` or no filter falls through to Tier 3.
- `CollectCandidatesForTable` is the low-level entry point used by the evaluation loop (Epic 5). Story 5.2 wraps it for whole-changeset orchestration so the evaluator doesn't need to know about tier internals.
- Deduplication in `CollectCandidates`: a query touching two tables may be returned from two different tier lookups for the same changeset. The output should be a set (no duplicate hashes).
