# SPEC-004 Phase 5 Deferred / Follow-up Items

This note records SPEC-004 subscription-core items intentionally left out of the current Phase 5 landing, plus nearby audit findings that still need explicit follow-up.

Scope fixed in earlier sessions:
- parameterized query-hash reachability in registration
- multiple same-query subscriptions on one connection
- eval-time client error/drop signaling contract slice
- `ReducerCallResult` forward-declaration alignment with SPEC-005 §8.7

Scope fixed in the 2026-04-14 follow-up session:
- join-bootstrap hard-failure on missing `IndexResolver` or unresolved RHS
  join index (`ErrJoinIndexUnresolved`; register.go)
- Story 5.4 join-fragment benchmark (`BenchmarkJoinFragmentEval`)
- Story 5.4 IVM invariant property test (single-table ColEq, 50 seeds)
- Story 5.4 pruning-safety property test (ColEq / ColRange / AllRows mix,
  30 seeds, baseline = no-pruning full eval)
- Story 5.4 registration/deregistration symmetry property test (mixed
  predicates, random register/unregister sequences, 30 seeds)
- Story 2.4 / Story 5.2 `CollectCandidatesForTable` documented signature
  now includes the `IndexResolver` parameter the code already takes
  (§A sub-item)
- Story 1.2 `SchemaLookup` interface now documents `TableName(TableID) string`
  with a note that it is not consulted by validation, only by eval/wire-side
  callers (§E)

Everything below remains deferred.

---

## Intentional cross-phase deferrals

### 1. Epic 6 remainder waits on SPEC-005 `ClientSender`

Still deferred by execution order:
- fan-out worker delivery loop
- per-connection `TransactionUpdate` / reducer-result assembly
- backpressure handling
- confirmed-read durability gating
- protocol integration / wire encoding of `SubscriptionError`

Dependency source:
- `docs/EXECUTION-ORDER.md` (Phase 8)
- `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/EPIC.md`

### 2. Story 5.3 memoized encoding remains a hook only

Current code keeps the evaluation-cycle cache hook, but protocol-backed lazy binary/JSON encoding is still deferred until SPEC-005 delivery surfaces exist.

Files:
- `subscription/eval.go`
- `docs/decomposition/004-subscriptions/epic-5-evaluation-loop/story-5.3-memoized-encoding.md`

---

## Audit follow-ups not fixed in this session

### A. Epic 2 contract/shape follow-ups — FIXED 2026-04-14 (docs revised)

Decision: the Go map-backed design for `ValueIndex` and `JoinEdgeIndex` is
correct. Spec/story text was revised to describe the actual shape rather
than prescribe a B-tree. Reference work confirmed SpacetimeDB's own
`module_subscription_manager.rs` uses BTreeMap primarily for tier-2's
range-for-table iteration, not because tier-1 needs ordering. Shunter's
`byTable` denormalization in `JoinEdgeIndex` serves the same external
contract.

Sub-item resolved 2026-04-14: Story 2.4 and Story 5.2 now document the
`IndexResolver` parameter on `CollectCandidatesForTable`, matching the
real code in `subscription/placement.go`. Pure doc fix.

Revised files:
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §5.1–5.2
- `docs/decomposition/004-subscriptions/EPICS.md` (Epic 2 scope bullets)
- `docs/decomposition/004-subscriptions/epic-2-pruning-indexes/story-2.1-value-index.md`
- `docs/decomposition/004-subscriptions/epic-2-pruning-indexes/story-2.2-join-edge-index.md`

### B. Epic 3 DeltaView contract reconciliation — FIXED 2026-04-14 (docs revised)

Decision: the Go `(TableID, ColID)`-keyed `DeltaIndexes` is correct.
Spec/story text was revised to describe the actual shape rather than
prescribe `IndexID`-keyed scratch indexes. This is a deliberate divergence
from SpacetimeDB's `DeltaStore` trait (which unifies delta and committed
lookups under a single `IndexId`); Shunter trades that symmetry for a
delta view that does not depend on `SchemaRegistry` / `IndexResolver` at
construction time. Committed-side access still uses the real `IndexID`.

Revised files:
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §6.4
- `docs/decomposition/004-subscriptions/EPICS.md` (Epic 3 scope bullets)
- `docs/decomposition/004-subscriptions/epic-3-deltaview-delta-computation/story-3.1-delta-view.md`

### C. Epic 3.5 allocation-discipline work is incomplete

Still missing:
- 4 KiB byte-buffer pool / oversized-buffer drop policy
- pooled/reused `DeltaView` insert/delete slices
- broader map reuse outside join-dedup scratch state

Files:
- `subscription/delta_pool.go`
- `subscription/delta_view.go`
- `docs/decomposition/004-subscriptions/epic-3-deltaview-delta-computation/story-3.5-allocation-discipline.md`

### D. Epic 4 join bootstrap error handling — FIXED 2026-04-14

Join registration now returns `ErrJoinIndexUnresolved` when the manager lacks
an `IndexResolver` or the resolver cannot produce an `IndexID` for the RHS
join column. See `subscription/register.go` and the new tests
`TestRegisterJoinNilResolverFails` and
`TestRegisterJoinResolverMissingIndexFails`.

Two sibling callsites keep the old "return nil on missing resolver" behavior
for different reasons:

- `CollectCandidatesForTable` / `collectCandidates` tier-2 edge scans
  (`placement.go`, `eval.go`): this is pruning. The contract explicitly
  allows tier-2 to be skipped when a resolver isn't available — the
  candidate will still be picked up by tier 3, or the test path is simply
  running without tier-2 coverage. Silent skip is intentional.
- `joinDriveCommitted` / `joinDriveCommittedReversed` in `delta_join.go`:
  this is actual fragment evaluation, not pruning. Returning empty I1/I2
  fragments when the resolver disagrees would produce silently incorrect
  deltas, not a pruning false negative. Post-A3, the A3 guard in
  `Register` makes this code path unreachable in practice (a join
  subscription can no longer register with an unresolved resolver). The
  `delta_join.go` branches remain as defense-in-depth; the documented
  justification is "unreachable given A3," not "semantically fine."

### E. Epic 1 interface-width drift — FIXED 2026-04-14 (docs revised)

Story 1.2 now documents `TableName(TableID) string` on `SchemaLookup`
with an inline note that validation itself does not consult it —
the method exists so the same lookup serves eval-time wire/debug paths
(SubscriptionError, FanOut labels). No code change.

Revised files:
- `docs/decomposition/004-subscriptions/epic-1-predicate-types-query-hash/story-1.2-predicate-validation.md`

### F. Story 5.4 verification backlog — FIXED 2026-04-14

All four items landed:
- `BenchmarkJoinFragmentEval` in `subscription/bench_test.go` (measured
  ~33 µs/op on the reference machine, well under the 10 ms/affected-sub
  target). Note: despite the name, this is an end-to-end one-affected-join-sub
  `EvalAndBroadcast` benchmark, not an isolated `EvalJoinDeltaFragments`
  microbenchmark.
- `TestIVMInvariantPropertySingleTable` in
  `subscription/property_test.go` (50 seeds).
- `TestPruningSafetyProperty` in `subscription/property_test.go` (30 seeds,
  baseline = full no-pruning re-eval).
- `TestRegistrationSymmetryProperty` in `subscription/property_test.go`
  (30 seeds, pruning-index emptiness checked against raw tier state).
- Focused join-edge cleanup coverage also exists in
  `TestUnregisterLastCleansJoinEdgeIndexes` (`subscription/manager_test.go`),
  since the symmetry property itself still randomizes only single-table
  predicates.

Open extensions (not story blockers):
- Property coverage still limited to single-table predicates. Join
  predicates are exercised by `TestEvalJoinSubscription`, the new bench, and
  focused join-edge cleanup coverage, but not by the property harness yet.
- Benchmarks report allocs/op but there is no regression gate.

---

## Recommended next-order after 2026-04-14 session

1. Complete Epic 3.5 allocation-discipline work (§C; only relevant if
   Phase 6 or Phase 8 surfaces allocation pressure).
2. Start Phase 6 — SPEC-003 E5 post-commit pipeline. Gate satisfied: Phase
   4 durability worker, Phase 5 subscription manager/eval, and SPEC-001 E7
   committed snapshots are all in place.
3. Resume Epic 6 only after SPEC-005 `ClientSender` / backpressure contracts
   land (Phase 7).
