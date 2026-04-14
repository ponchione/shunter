# Story 5.2: Candidate Collection

**Epic:** [Epic 5 — Evaluation Loop](EPIC.md)
**Spec ref:** SPEC-004 §7.2 (step 3), §7.3
**Depends on:** Story 5.1 (orchestration), Epic 2 (PruningIndexes), SPEC-001 (CommittedReadView), SPEC-003/SPEC-001 (`*Changeset`)
**Blocks:** Story 5.4 (benchmark / verification story consumes this path)

---

## Summary

Given a changeset, determine which query hashes might be affected. Consults all three pruning tiers and unions results. Includes batched Tier 1 optimization.

## Deliverables

- `CollectCandidates(indexes *PruningIndexes, changeset *Changeset, committed CommittedReadView) map[QueryHash]struct{}`

- Low-level helper owned by Story 2.4 remains per-table:
  - `CollectCandidatesForTable(indexes *PruningIndexes, table TableID, rows []ProductValue, committed CommittedReadView) []QueryHash`

- Per-table collection (per §7.2 step 3):
  ```
  For each table T modified in changeset:
    // Tier 1: ValueIndex — batched optimization (§7.3)
    For each colID tracked for T:
      Collect distinct values for colID across all changed rows (inserts + deletes)
      For each distinct value:
        candidates.AddAll(ValueIndex.Lookup(T, colID, value))

    // Tier 2: JoinEdgeIndex
    For each JoinEdge involving T:
      For each changed row R:
        joinValue := R.Column(edge.LHSJoinCol)
        rhsRow := committed.IndexScan(edge.RHSTable, edge.RHSJoinCol, joinValue)
        if rhsRow exists:
          filterValue := rhsRow.Column(edge.RHSFilterCol)
          candidates.AddAll(JoinEdgeIndex.Lookup(edge, filterValue))

    // Tier 3: TableFallback
    candidates.AddAll(TableIndex.Lookup(T))
  ```

- Batched Tier 1 optimization: collect distinct values per column from all changed rows, one lookup per distinct value instead of per row (§7.3)

- Output: set of QueryHash (no duplicates)

## Acceptance Criteria

- [ ] ColEq subscription, matching value in changeset → in candidates
- [ ] ColEq subscription, non-matching value → not in candidates
- [ ] AllRows subscription → always in candidates (via Tier 3)
- [ ] Join subscription, matching join path → in candidates (via Tier 2)
- [ ] Batched Tier 1: 100 rows with same value → 1 lookup, not 100
- [ ] Batched Tier 1: 100 rows with 5 distinct values → 5 lookups
- [ ] Candidate set has no duplicates
- [ ] Table with no subscriptions → empty candidates for that table
- [ ] Multiple tables modified → candidates from all tables unioned
- [ ] Pruned candidate evaluation over mixed predicates matches evaluate-all baseline results

## Design Notes

- Batching optimization (§7.3) matters for bulk inserts. If a reducer inserts 1000 rows into `messages` all with different `channel_id` values, we do 1000 lookups. But if they all have the same `channel_id`, we do 1 lookup. The optimization reduces O(changed_rows) to O(distinct_values_per_column).
- Tier 2 lookup requires an index scan on committed state to find the RHS row. This is an extra read per changed row per join edge. For most workloads, join edges are few, so this is acceptable.
- The candidate set uses `map[QueryHash]struct{}` — Go's idiomatic set. Map reuse across transactions (§9.2) applies here.
- Story 2.4 owns the per-table tier-union helper; this story owns the whole-changeset orchestration and batching logic.
