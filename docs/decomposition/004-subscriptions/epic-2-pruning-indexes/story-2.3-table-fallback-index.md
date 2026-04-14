# Story 2.3: Table Fallback Index (Tier 3)

**Epic:** [Epic 2 — Pruning Indexes](EPIC.md)
**Spec ref:** SPEC-004 §5.3
**Depends on:** Epic 1 (QueryHash)
**Blocks:** Story 2.4 (Index Placement)

---

## Summary

Pessimistic fallback: any change to a table triggers evaluation of all queries in this index for that table. Used for complex predicates, range-only predicates, and `AllRows`.

## Deliverables

- `TableIndex` struct:
  ```go
  type TableIndex struct {
      tables map[TableID]map[QueryHash]struct{}
  }
  ```

- `NewTableIndex() *TableIndex`

- `Add(table TableID, hash QueryHash)`

- `Remove(table TableID, hash QueryHash)` — clean up empty entries

- `Lookup(table TableID) []QueryHash` — all hashes for this table

## Acceptance Criteria

- [ ] Add entry, Lookup returns it
- [ ] Multiple hashes per table → all returned
- [ ] Lookup for table with no entries → empty slice
- [ ] Remove entry, Lookup no longer returns it
- [ ] Remove last hash for table → table key cleaned up

## Design Notes

- Simplest of the three indexes. Goal is to minimize what lands here — the predicate model (§3) is designed so common patterns (equality filters) land in Tier 1 instead.
- No B-tree needed. Plain map is sufficient since lookups are always by exact table ID.
- Performance concern: if many subscriptions fall into Tier 3 for a given table, every change to that table evaluates all of them. This is O(n) and intentionally pessimistic.
