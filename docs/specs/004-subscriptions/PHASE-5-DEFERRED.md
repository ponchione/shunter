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

Everything below remains deferred except where explicitly marked fixed.

---

## Intentional cross-phase deferrals

### 1. Epic 6 remainder — FIXED 2026-04-16

The former Phase 8 deferral is now implemented and verified.

Landed runtime pieces:
- `FanOutWorker` delivery loop with protocol-backed `FanOutSender`
- per-connection `TransactionUpdate` assembly with caller diversion into `ReducerCallResult`
- disconnect-on-lag handling via `ErrSendBufferFull` / dropped-client signaling
- confirmed-read durability gating via `PostCommitMeta.TxDurable`
- protocol integration / wire encoding of `SubscriptionError`
- executor follow-through for external-caller metadata only
- commitlog readiness channels via `WaitUntilDurable(txID)`

Validation run for the completion pass:
- `rtk go test ./subscription/... ./protocol/... ./executor/... ./commitlog/...`
- `rtk go test -race ./subscription/... ./protocol/... ./executor/...`
- `rtk go vet ./subscription/... ./protocol/... ./executor/... ./commitlog/...`

Primary files:
- `subscription/fanout_worker.go`
- `subscription/fanout.go`
- `subscription/eval.go`
- `executor/executor.go`
- `commitlog/durability.go`
- `protocol/fanout_adapter.go`
- `protocol/send_reducer_result.go`

### 2. Story 5.3 memoized encoding remains a hook only

Current code keeps the evaluation-cycle cache hook, but protocol-backed lazy binary/JSON encoding is still deferred until SPEC-005 delivery surfaces exist.

Files:
- `subscription/eval.go`
- `docs/specs/004-subscriptions/epic-5-evaluation-loop/story-5.3-memoized-encoding.md`

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
- `docs/specs/004-subscriptions/SPEC-004-subscriptions.md` §5.1–5.2
- `docs/specs/004-subscriptions/EPICS.md` (Epic 2 scope bullets)
- `docs/specs/004-subscriptions/epic-2-pruning-indexes/story-2.1-value-index.md`
- `docs/specs/004-subscriptions/epic-2-pruning-indexes/story-2.2-join-edge-index.md`

### B. Epic 3 DeltaView contract reconciliation — FIXED 2026-04-14 (docs revised)

Decision: the Go `(TableID, ColID)`-keyed `DeltaIndexes` is correct.
Spec/story text was revised to describe the actual shape rather than
prescribe `IndexID`-keyed scratch indexes. This is a deliberate divergence
from SpacetimeDB's `DeltaStore` trait (which unifies delta and committed
lookups under a single `IndexId`); Shunter trades that symmetry for a
delta view that does not depend on `SchemaRegistry` / `IndexResolver` at
construction time. Committed-side access still uses the real `IndexID`.

Revised files:
- `docs/specs/004-subscriptions/SPEC-004-subscriptions.md` §6.4
- `docs/specs/004-subscriptions/EPICS.md` (Epic 3 scope bullets)
- `docs/specs/004-subscriptions/epic-3-deltaview-delta-computation/story-3.1-delta-view.md`

### C. Epic 3.5 allocation-discipline work is incomplete

Still missing:
- 4 KiB byte-buffer pool / oversized-buffer drop policy
- pooled/reused `DeltaView` insert/delete slices
- broader map reuse outside join-dedup scratch state

Files:
- `subscription/delta_pool.go`
- `subscription/delta_view.go`
- `docs/specs/004-subscriptions/epic-3-deltaview-delta-computation/story-3.5-allocation-discipline.md`

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
- `docs/specs/004-subscriptions/epic-1-predicate-types-query-hash/story-1.2-predicate-validation.md`

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

## Recommended next-order after the 2026-04-16 completion pass

1. Complete Epic 3.5 allocation-discipline work (§C) if profiling shows
   subscription-core pressure on the hot path.
2. Keep any future SPEC-004 work focused on doc/audit reconciliation rather
   than fan-out runtime wiring; Epic 6 remainder is complete.
