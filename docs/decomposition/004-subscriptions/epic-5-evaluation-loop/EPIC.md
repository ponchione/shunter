# Epic 5: Evaluation Loop

**Parent:** [SPEC-004-subscriptions.md](../SPEC-004-subscriptions.md) §7
**Blocked by:** Epic 2 (Pruning Indexes), Epic 3 (Delta Computation), Epic 4 (Subscription Manager)
**Blocks:** Epic 6 (Fan-Out — receives CommitFanout)

**Cross-spec:** Depends on SPEC-001 `CommittedReadView` and SPEC-003/SPEC-001 `Changeset` / executor post-commit trigger.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 5.1 | [story-5.1-eval-transaction.md](story-5.1-eval-transaction.md) | Main EvalTransaction algorithm: build DeltaView, collect candidates, evaluate, assemble fanout |
| 5.2 | [story-5.2-candidate-collection.md](story-5.2-candidate-collection.md) | Row-level and batched candidate collection across all 3 pruning tiers |
| 5.3 | [story-5.3-memoized-encoding.md](story-5.3-memoized-encoding.md) | Shared encoding for multiple clients on same query |
| 5.4 | [story-5.4-eval-benchmarks.md](story-5.4-eval-benchmarks.md) | Benchmark suite for §9.1 performance targets |

## Implementation Order

```
Story 5.1 (EvalTransaction) — orchestration skeleton
  ├── Story 5.2 (Candidate collection) — called by 5.1
  └── Story 5.3 (Memoized encoding) — called by 5.1 during fanout assembly
Story 5.4 (Benchmarks) — after 5.1–5.3 complete
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 5.1 | `eval.go`, `eval_test.go` |
| 5.2 | `candidates.go`, `candidates_test.go` |
| 5.3 | `memoized.go`, `memoized_test.go` |
| 5.4 | `eval_bench_test.go` |
