# Story 3.2: Single-Table Delta Evaluation

**Epic:** [Epic 3 — DeltaView & Delta Computation](EPIC.md)
**Spec ref:** SPEC-004 §6.1
**Depends on:** Story 3.1 (DeltaView)
**Blocks:** Epic 5 (Evaluation Loop), Story 3.5

---

## Summary

For `V = filter(T)`: apply filter to inserted rows → delta inserts, apply filter to deleted rows → delta deletes. No deduplication needed for single-table subscriptions.

## Deliverables

- `EvalSingleTableDelta(dv *DeltaView, pred Predicate, table TableID) (inserts, deletes []ProductValue)`
  - Iterate `dv.InsertedRows(table)`, apply predicate filter, collect matches
  - Iterate `dv.DeletedRows(table)`, apply predicate filter, collect matches
  - Return both slices (may be empty)

- Predicate filter evaluation:
  - `ColEq`: row column value == predicate value
  - `ColRange`: row column value within bounds
  - `And`: both sub-predicates match
  - `AllRows`: always matches

- `MatchRow(pred Predicate, row ProductValue) bool` — exported for reuse in join fragment evaluation

## Acceptance Criteria

- [ ] Insert rows matching filter → appear in delta inserts
- [ ] Insert rows not matching filter → excluded
- [ ] Delete rows matching filter → appear in delta deletes
- [ ] Delete rows not matching filter → excluded
- [ ] `AllRows` predicate → all inserts/deletes pass through
- [ ] `ColRange` with inclusive bound → boundary value included
- [ ] `ColRange` with exclusive bound → boundary value excluded
- [ ] `And` predicate → both conditions must match
- [ ] No inserts or deletes for table → both slices empty
- [ ] No deduplication applied (single-table cannot produce duplicates)

## Design Notes

- `MatchRow` is a simple recursive evaluator over the predicate tree. For single-table predicates, the predicate references one table, and the row comes from that table's changeset.
- Performance: filter evaluation is O(changed_rows) per subscription. Pruning (Epic 2) ensures this function is only called for subscriptions that are likely affected.
- `ColRange` with unbounded lower or upper works as expected: unbounded lower means no minimum, unbounded upper means no maximum.
