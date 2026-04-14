# Story 2.1: Value Index (Tier 1)

**Epic:** [Epic 2 — Pruning Indexes](EPIC.md)
**Spec ref:** SPEC-004 §5.1
**Depends on:** Epic 1 (QueryHash, Value)
**Blocks:** Story 2.4 (Index Placement)

---

## Summary

B-tree mapping `(table, column, value)` to sets of query hashes. The most common pruning path — equality predicates land here.

## Deliverables

- `ValueIndex` struct:
  ```go
  type ValueIndex struct {
      cols map[TableID]map[ColID]struct{}
      args *btree.Map[valueIndexKey, map[QueryHash]struct{}]
  }
  ```

- `valueIndexKey` struct: `{Table TableID, Column ColID, Value Value}` with ordering (table, column, value lexicographic)

- `NewValueIndex() *ValueIndex`

- `Add(table TableID, col ColID, value Value, hash QueryHash)` — insert mapping

- `Remove(table TableID, col ColID, value Value, hash QueryHash)` — remove mapping; clean up empty entries

- `Lookup(table TableID, col ColID, value Value) []QueryHash` — return all hashes for exact match

- `TrackedColumns(table TableID) []ColID` — which columns have subscriptions for this table (used by candidate collection)

## Acceptance Criteria

- [ ] Add one entry, Lookup returns it
- [ ] Add two entries same (table, col, value), different hash → Lookup returns both
- [ ] Add entries for different values, Lookup returns only matching value's hashes
- [ ] Remove entry, Lookup no longer returns it
- [ ] Remove last hash for a (table, col, value) → key cleaned up from B-tree
- [ ] TrackedColumns returns columns with active entries
- [ ] TrackedColumns for untracked table → empty
- [ ] Lookup for untracked (table, col, value) → empty slice, not nil

## Design Notes

- B-tree chosen over hash map for `args` because the sorted key structure enables efficient cleanup: removing all entries for a given (table, column) prefix is a range delete.
- `cols` is a denormalized acceleration structure for candidate collection (§7.2 step 3). Without it, you'd have to scan the B-tree for all keys matching a table, which defeats the purpose.
- Value comparison in the B-tree key uses the same ordering as SPEC-001 §2.2.
