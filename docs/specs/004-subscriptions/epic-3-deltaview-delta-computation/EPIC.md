# Epic 3: DeltaView & Delta Computation

**Parent:** [SPEC-004-subscriptions.md](../SPEC-004-subscriptions.md) §6
**Blocked by:** Epic 1 (Predicate types for filter application)
**Blocks:** Epic 5 (Evaluation Loop — calls delta computation per candidate query)

**Cross-spec:** Depends on SPEC-001 `CommittedReadView` for base table access.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 3.1 | [story-3.1-delta-view.md](story-3.1-delta-view.md) | DeltaView struct, delta indexes, eager construction |
| 3.2 | [story-3.2-single-table-delta.md](story-3.2-single-table-delta.md) | Filter inserts/deletes for single-table subscriptions |
| 3.3 | [story-3.3-join-delta-fragments.md](story-3.3-join-delta-fragments.md) | 4+4 IVM fragments for join subscriptions |
| 3.4 | [story-3.4-bag-semantic-dedup.md](story-3.4-bag-semantic-dedup.md) | Insert/delete count reconciliation for join deltas |
| 3.5 | [story-3.5-allocation-discipline.md](story-3.5-allocation-discipline.md) | Buffer pooling, slice/map reuse, byte comparison |

## Implementation Order

```
Story 3.1 (DeltaView)
  ├── Story 3.2 (Single-table delta)
  └── Story 3.3 (Join delta fragments)
        └── Story 3.4 (Bag dedup)
Story 3.5 (Allocation discipline) — applied across 3.1–3.4 after correctness proven
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 3.1 | `delta_view.go`, `delta_view_test.go` |
| 3.2 | `delta_single.go`, `delta_single_test.go` |
| 3.3 | `delta_join.go`, `delta_join_test.go` |
| 3.4 | `bag_dedup.go`, `bag_dedup_test.go` |
| 3.5 | `pool.go`, `pool_test.go` |
