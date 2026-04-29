# Story 5.4: Evaluation Benchmarks

**Epic:** [Epic 5 — Evaluation Loop](EPIC.md)
**Spec ref:** SPEC-004 §9.1, §9.3, §13.1, §13.2, §13.3
**Depends on:** Stories 5.1–5.3
**Blocks:** Nothing (verification story)

---

## Summary

Benchmark suite and property/unit tests validating performance targets (§9.1, §13.3), scaling claims (§9.3), and correctness invariants (§13.1–§13.2). This story is primarily tests, not new code.

## Deliverables

### Unit-test scenarios (§13.1)

- Single-table delta fixture: insert/delete rows and verify delta matches filter application.
- Join delta correctness fixture: known T1/T2/dT1/dT2 case compared against full re-evaluation of the join.
- Bag-semantics fixture: one LHS row joining three RHS rows, then deleting one RHS row, yields exactly one delete.
- Pruning fixture: pruned evaluation matches evaluate-all baseline for known predicates.
- Dedup fixture: identical predicate registered by multiple clients evaluates once and fans out identically.

### Property Tests (§13.2)

- **IVM invariant**: For any sequence of transactions, accumulated deltas applied to the initial snapshot must equal re-evaluating the full query from scratch. Test with randomized transaction sequences.

- **Pruning safety**: Evaluation with pruning enabled must produce identical results to evaluation with all subscriptions evaluated (pruning disabled). Test with mixed predicate types.

- **Registration/deregistration symmetry**: After registering and deregistering a subscription, all pruning indexes must return to their prior state. Test with randomized register/unregister sequences.

### Benchmarks (§9.1, §13.3)

- Benchmark: **1,000 equality subscriptions, 1 table change**
  - Setup: 1K `ColEq` subscriptions on same table+column
  - Changeset: 1 insert matching 1 subscription
  - Target: evaluate in < 1 ms

- Benchmark: **10,000 equality subscriptions, 1 table change**
  - Setup: 10K `ColEq` subscriptions with distinct values on same table+column
  - Changeset: 1 insert matching 1 subscription
  - Target: evaluate in < 5 ms

- Benchmark: **Join fragment evaluation**
  - Setup: committed state with matching rows and affected join subscriptions
  - Changeset: inserts/deletes on one or both sides of the join
  - Target: < 10 ms per affected subscription

- Benchmark: **Subscription registration/deregistration**
  - 1000 register + unregister cycles
  - Target: < 100 µs per operation

- Benchmark: **Fan-out to 1,000 clients (same query)**
  - 1K clients subscribed to identical query
  - Changeset triggers 1 delta
  - Target: < 1 ms (encode once, replicate pointers)

- Benchmark: **Delta index construction**
  - 100 changed rows, 5 indexed columns
  - Target: < 1 ms for typical transactions

## Acceptance Criteria

- [ ] Single-table evaluation benchmark: 1K subscriptions, 1 table change → < 1 ms
- [ ] Single-table evaluation benchmark: 10K subscriptions, 1 table change → < 5 ms
- [ ] Join fragment benchmark: < 10 ms per affected join subscription
- [ ] Registration/deregistration benchmark: < 100 µs per operation
- [ ] Fan-out 1K clients benchmark: < 1 ms
- [ ] Delta index construction benchmark: < 1 ms for typical transactions
- [ ] All benchmarks run via `go test -bench` with stable results (low variance)
- [ ] Unit-test scenario: single-table delta fixture passes
- [ ] Unit-test scenario: join delta fixture matches full re-evaluation
- [ ] Unit-test scenario: bag-semantics multiplicity fixture passes
- [ ] Unit-test scenario: pruning fixture matches evaluate-all baseline
- [ ] Unit-test scenario: dedup fixture proves one evaluation shared across multiple clients
- [ ] IVM invariant property test: accumulated deltas = full re-evaluation (randomized)
- [ ] Pruning safety property test: pruned eval = full eval (randomized)
- [ ] Registration symmetry property test: register+unregister = no residual state (randomized)

## Design Notes

- Benchmarks should use `testing.B` with `b.ResetTimer()` after setup.
- Targets are from the spec — they define “fast enough for v1.” If a target is missed, the implementation may need optimization (allocation discipline, algorithm change) but the target itself is the spec contract.
- Wall-clock benchmarks are machine-dependent. CI should run on consistent hardware. Local development should treat these as guidelines, not hard gates.
- Consider `b.ReportAllocs()` to track allocation counts alongside timing.
