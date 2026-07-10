# Set Live-Query Admission Policy

Status: recommended after a product operating envelope exists

Promotion trigger: measured product views establish realistic relation counts,
cardinalities, initial result sizes, and fanout distributions.

Owners: root config, protocol admission, subscription, diagnostics, docs

## Why

Multi-way views are correctness-first and current relation/row guardrails
default to unlimited. That preserves compatibility but makes an accidental
high-cardinality join capable of consuming disproportionate CPU and memory.
Policy should come from real workloads rather than arbitrary constants.

## Outcome

An operator can predict and control the cost of admitted live queries, and
clients receive stable diagnostics when a query exceeds policy.

## Work

1. Measure relation count, committed rows per relation, initial result rows and
   bytes, delta expansion, aggregate state, and fanout for product views.
2. Decide whether safe defaults, an explicit unbounded opt-in, or per-view
   declaration metadata best matches the product contract.
3. Apply limits before expensive materialization and before registration can
   leave partial state.
4. Add structured diagnostics naming the exceeded dimension without leaking
   private table information.
5. Record rejected-admission metrics separately from evaluation failures.
6. Consider incremental/index-backed evaluation only for a promoted hot path
   that remains too expensive within the chosen limits.

## Non-Goals

- promising broad SQL live-query compatibility
- adding a cost-based optimizer without a measured need
- changing query semantics to make an expensive view appear cheaper
- raising limits to make a test pass

## Completion Evidence

- workload-derived policy rationale
- boundary tests for every configured dimension
- no partial subscription registration on rejection
- product canary verifies expected views remain admitted
- benchmark evidence shows rejection occurs before dominant allocation/work
