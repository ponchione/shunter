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

- `JoinEdgeIndex` struct:
  ```go
  type JoinEdgeIndex struct {
      // edges[edge][encodedFilterValue] = set of query hashes
      edges map[JoinEdge]map[string]map[QueryHash]struct{}
      // byTable[LHSTable][edge] = refcount, so EdgesForTable returns the
      // LHSTable-rooted edges without scanning the full edges map.
      byTable map[TableID]map[JoinEdge]int
  }
  ```

- `NewJoinEdgeIndex() *JoinEdgeIndex`

- `Add(edge JoinEdge, filterValue Value, hash QueryHash)` — inserts into both `edges` and `byTable`

- `Remove(edge JoinEdge, filterValue Value, hash QueryHash)` — cleans up empty entries on the way out, in both structures

- `Lookup(edge JoinEdge, filterValue Value) []QueryHash`

- `EdgesForTable(table TableID) []JoinEdge` — all edges where `LHSTable` matches (used during candidate collection)

## Acceptance Criteria

- [ ] Add entry, Lookup with matching edge + filterValue → returns hash
- [ ] Lookup with wrong filterValue → empty
- [ ] Lookup with wrong edge → empty
- [ ] Multiple hashes per (edge, filterValue) → all returned
- [ ] Remove last hash → entry cleaned up from both `edges` and `byTable`
- [ ] EdgesForTable returns only edges where LHSTable matches
- [ ] EdgesForTable for unrelated table → empty

## Design Notes

- The lookup during candidate collection (§7.2) requires an index scan on the RHS table to extract the filter value. That index scan uses `CommittedReadView.IndexSeek` from SPEC-001, not this index. This index just maps the filter value to query hashes.
- `EdgesForTable` is served by the `byTable` denormalization, not by prefix-iterating an ordered key structure. This gives the same "edges for a given LHSTable" contract an ordered B-tree would provide, with straightforward map semantics and without an external btree dependency.
- Symmetric edges (LHS↔RHS) are stored as separate entries. A join subscription touching T1 and T2 may have an edge for changes on T1 (LHS=T1, RHS=T2) and a separate edge for changes on T2 (LHS=T2, RHS=T1). Add/Remove must handle both independently.
- Reference: SpacetimeDB's `module_subscription_manager.rs` uses a BTreeMap keyed by JoinEdge specifically to support the range-for-table iteration the Go `byTable` denormalization replaces here. Both approaches preserve the same external contract — ordered iteration is not itself a requirement of tier-2 semantics.
