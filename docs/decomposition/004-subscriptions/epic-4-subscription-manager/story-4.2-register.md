# Story 4.2: Subscription Registration

**Epic:** [Epic 4 — Subscription Manager](EPIC.md)
**Spec ref:** SPEC-004 §4.1, §10.1, §11.2
**Depends on:** Story 4.1 (Query State), Epic 1 (Validation, QueryHash), Epic 2 (Index Placement), SPEC-001 (CommittedReadView)
**Blocks:** Story 4.3

---

## Summary

Full registration flow from validated predicate to initial rows returned. Runs inside executor command — no gap between initial query and delta activation.

## Deliverables

- `Register(req SubscriptionRegisterRequest, view CommittedReadView) (SubscriptionRegisterResult, error)`

- Registration steps (per §4.1):
  1. **Validate** predicate via `ValidatePredicate` (Story 1.2)
  2. **Compute query hash** via `ComputeQueryHash` (Story 1.3)
  3. **Compile** executable plan
     - v1: compiled plan = the validated predicate itself, recorded in query state
  4. **Check dedup**: if `queryRegistry.getQuery(hash)` exists, reuse existing executable state
  5. **Execute initial query** against `CommittedReadView` — scan table(s), apply predicate filter
  6. **Create/reuse query state** in registry
  7. **Place in pruning indexes** via `PlaceSubscription` (Story 2.4)
  8. **Add subscriber** to query state
  9. **Return** `SubscriptionRegisterResult{SubscriptionID, InitialRows}`

- Initial query execution:
  - Single-table: `TableScan` or `IndexScan` + filter
  - Join: full join execution against committed state (not incremental — this is the bootstrap)
  - Optional row limit: if initial result exceeds configurable max → `ErrInitialRowLimit`

- `SubscriptionRegisterRequest` and `SubscriptionRegisterResult` types are declared canonically in Story 4.5 and used here as the behavior owner for registration

## Acceptance Criteria

- [ ] Register with valid predicate → initial rows returned, subscription active
- [ ] Register same predicate from second client → dedup: same query state reused
- [ ] Register with invalid predicate (3 tables) → `ErrTooManyTables`
- [ ] Register with unindexed join → `ErrUnindexedJoin`
- [ ] Register with nonexistent table → `ErrTableNotFound`
- [ ] Register with nonexistent column → `ErrColumnNotFound`
- [ ] Registration records an executable plan before activation (v1: validated predicate as plan)
- [ ] Initial query returns matching rows from committed state
- [ ] Initial query with row limit exceeded → `ErrInitialRowLimit`
- [ ] After register, subscription appears in pruning indexes
- [ ] Registration executes inside one executor command: no commit may slip between initial-row materialization and subscription activation
- [ ] Benchmark: registration < 100 µs (excluding initial query scan time)

## Design Notes

- Registration runs inside an executor command, not as an out-of-band operation. This means the committed state cannot change between “execute initial query” and “subscription is active.” No races.
- The initial query is a full evaluation, not incremental. For large tables with selective predicates, `IndexScan` should be preferred over `TableScan`.
- `ErrInitialRowLimit` is a safety valve against subscribing to massive result sets. The limit is configurable (deployment-level). Default: implementer's choice.
- Dedup check uses query hash equality. Two structurally identical predicates with identical values produce the same hash (Story 1.3) and share evaluation work.
- Respect SPEC-001 snapshot discipline: materialize initial rows promptly and do not hold the `CommittedReadView` across network I/O or other blocking work.
