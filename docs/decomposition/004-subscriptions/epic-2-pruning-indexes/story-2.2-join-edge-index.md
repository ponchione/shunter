# Story 2.2: Join Edge Index (Tier 2)

**Epic:** [Epic 2 — Pruning Indexes](EPIC.md)
**Spec ref:** SPEC-004 §5.2
**Depends on:** Epic 1 (QueryHash, predicate types)
**Blocks:** Story 2.4 (Index Placement)

---

## Summary

Index for join subscriptions with a filter on the joined table. Prunes by checking whether a changed row could satisfy the join + filter condition.

## Deliverables

- `JoinEdge` struct:
  ```go
  type JoinEdge struct {
      LHSTable     TableID
      RHSTable     TableID
      LHSJoinCol   ColID
      RHSJoinCol   ColID
      RHSFilterCol ColID
  }
  ```
  With ordering for B-tree key.

- `JoinEdgeIndex` struct:
  ```go
  type JoinEdgeIndex struct {
      edges *btree.Map[JoinEdge, map[Value]map[QueryHash]struct{}]
  }
  ```

- `NewJoinEdgeIndex() *JoinEdgeIndex`

- `Add(edge JoinEdge, filterValue Value, hash QueryHash)`

- `Remove(edge JoinEdge, filterValue Value, hash QueryHash)` — clean up empty entries

- `Lookup(edge JoinEdge, filterValue Value) []QueryHash`

- `EdgesForTable(table TableID) []JoinEdge` — all edges where `LHSTable` matches (used during candidate collection)

## Acceptance Criteria

- [ ] Add entry, Lookup with matching edge + filterValue → returns hash
- [ ] Lookup with wrong filterValue → empty
- [ ] Lookup with wrong edge → empty
- [ ] Multiple hashes per (edge, filterValue) → all returned
- [ ] Remove last hash → entry cleaned up
- [ ] EdgesForTable returns only edges where LHSTable matches
- [ ] EdgesForTable for unrelated table → empty

## Design Notes

- The lookup during candidate collection (§7.2) requires an index scan on the RHS table to extract the filter value. That index scan uses `CommittedReadView.IndexScan` from SPEC-001, not this index. This index just maps the filter value to query hashes.
- `EdgesForTable` iterates the B-tree prefix for a given LHSTable. The B-tree ordering puts LHSTable first, making this a range scan.
- Symmetric edges (LHS↔RHS) are stored as separate entries. A join subscription touching T1 and T2 may have an edge for changes on T1 (LHS=T1, RHS=T2) and a separate edge for changes on T2 (LHS=T2, RHS=T1).
