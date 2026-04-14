# Story 2.1: Value Index (Tier 1)

**Epic:** [Epic 2 — Pruning Indexes](EPIC.md)
**Spec ref:** SPEC-004 §5.1
**Depends on:** Epic 1 (QueryHash, Value)
**Blocks:** Story 2.4 (Index Placement)

---

## Summary

Map-backed equality index from `(table, column, value)` to sets of query hashes. The most common pruning path — `ColEq` predicates land here.

## Deliverables

- `ValueIndex` struct:
  ```go
  type ValueIndex struct {
      // cols[table][col] = refcount of active entries for that column.
      // Used by TrackedColumns for candidate collection.
      cols map[TableID]map[ColID]int
      // args[table][col][encodedValue] = set of query hashes for that
      // (table, column, value) triple.
      args map[TableID]map[ColID]map[string]map[QueryHash]struct{}
  }
  ```

- `NewValueIndex() *ValueIndex`

- `Add(table TableID, col ColID, value Value, hash QueryHash)` — insert mapping

- `Remove(table TableID, col ColID, value Value, hash QueryHash)` — remove mapping; clean up empty entries on the way out

- `Lookup(table TableID, col ColID, value Value) []QueryHash` — return all hashes for exact match

- `TrackedColumns(table TableID) []ColID` — which columns have subscriptions for this table (used by candidate collection)

## Acceptance Criteria

- [ ] Add one entry, Lookup returns it
- [ ] Add two entries same (table, col, value), different hash → Lookup returns both
- [ ] Add entries for different values, Lookup returns only matching value's hashes
- [ ] Remove entry, Lookup no longer returns it
- [ ] Remove last hash for a (table, col, value) → key cleaned up (parent column/table maps shrink when empty)
- [ ] TrackedColumns returns columns with active entries
- [ ] TrackedColumns for untracked table → empty
- [ ] Lookup for untracked (table, col, value) → empty slice, not nil

## Design Notes

- Tier 1 is pure equality lookup. No predicate pattern we accept today requires ordered iteration over value keys, so a nested map is the natural structure. Empty-map cleanup on `Remove` gives the same "entry disappears when no hashes remain" behavior a B-tree range-delete would provide, without the extra dependency.
- `cols` is a denormalized acceleration structure for candidate collection (§7.2 step 3). Without it, you'd have to iterate the full `args[table]` map to find tracked columns, which is O(colCount) per changed table per transaction.
- Values are used as map keys via a canonical encoded-bytes form (see `encodeValueKey`), consistent with SPEC-001 §2.2 ordering semantics for equality.
- Reference: SpacetimeDB's `module_subscription_manager.rs` uses a BTreeMap for the equivalent structure, but primarily for the tier-2 range-for-table iteration on join edges — tier-1 itself is used equality-only. The Go map-backed design is semantically aligned; see Story 2.2 for the tier-2 ordering that does matter.
