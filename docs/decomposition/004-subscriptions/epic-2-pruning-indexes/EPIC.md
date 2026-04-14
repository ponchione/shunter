# Epic 2: Pruning Indexes

**Parent:** [SPEC-004-subscriptions.md](../SPEC-004-subscriptions.md) §5
**Blocked by:** Epic 1 (Predicate types, QueryHash)
**Blocks:** Epic 4 (Subscription Manager — index placement on register), Epic 5 (Evaluation Loop — candidate collection)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 2.1 | [story-2.1-value-index.md](story-2.1-value-index.md) | Tier 1: (table, column, value) → query hashes via B-tree |
| 2.2 | [story-2.2-join-edge-index.md](story-2.2-join-edge-index.md) | Tier 2: JoinEdge → (rhs_filter_value → query hashes) |
| 2.3 | [story-2.3-table-fallback-index.md](story-2.3-table-fallback-index.md) | Tier 3: table → query hashes (pessimistic fallback) |
| 2.4 | [story-2.4-index-placement.md](story-2.4-index-placement.md) | Placement logic: route each (query, table) pair to exactly one tier |

## Implementation Order

```
Story 2.1 (ValueIndex)
Story 2.2 (JoinEdgeIndex)    — parallel with 2.1
Story 2.3 (TableFallback)    — parallel with 2.1, 2.2
  └── Story 2.4 (Placement)  — needs all three indexes
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 2.1 | `value_index.go`, `value_index_test.go` |
| 2.2 | `join_edge_index.go`, `join_edge_index_test.go` |
| 2.3 | `table_index.go`, `table_index_test.go` |
| 2.4 | `index_placement.go`, `index_placement_test.go` |
