# P0-01 subscription initial-query streaming plan

> For Hermes: planning only. Do not implement in this turn.

Goal
- Land the smallest high-payoff slice from `docs/performance-audit-punchlist-2026-04-20.md`: stop materializing full table scans in the subscription initial-query path while preserving current registration semantics and row-limit behavior.

Why this slice
- It is the cleanest bounded P0 item.
- Scope is mostly one production file plus tests/benchmarks.
- The spec path is clear: SPEC-004 registration requires initial query materialization inside one executor command, but does not require eager whole-table slice assembly.
- It avoids the larger file-format and recovery risks in the snapshot P0 items.

Grounded context
- Punchlist item: `docs/performance-audit-punchlist-2026-04-20.md` P0-01.
- Spec: `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md:142-152` and `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.2-register.md:25-41`.
- Current implementation hotspot: `subscription/register_set.go:55-195`.
- Current eager helper: `iterateAll(view, t)` copies `view.TableScan(t)` into a slice, then callers often range that slice immediately.
- Join bootstrap path currently adds a second avoidable slice layer via inline closures building `ls` / `rs` before probing the indexed side.
- Existing repo state is dirty in unrelated files (`NEXT_SESSION_HANDOFF.md`, `TECH-DEBT.md`, `docs/current-status.md`, `docs/performance-audit-punchlist-2026-04-20.md`, `store/snapshot.go`, new snapshot tests). Keep this slice narrowly confined to subscription files.

Chosen slice
- P0-01: `subscription/register_set.go`
- Verification support: `subscription/register_set_test.go`, `subscription/bench_test.go`

Non-goals
- No protocol changes.
- No query-planner/index-selection redesign.
- No changes to register/unregister semantics, dedup, or wire shape.
- No work on P0-02/P0-03/P0-04 in the same pass.

Implementation approach
1. Remove `iterateAll` from the initial-query hot path.
2. Stream directly over `view.TableScan(...)` in all three branches:
   - `Join`
   - `CrossJoinProjected`
   - default single-table path
3. In join branches, preserve the existing “scan one side, probe the indexed other side” logic, but iterate the scan side directly instead of first building `ls` / `rs` slices.
4. Keep row-limit enforcement exactly where rows are appended to `out` via `add(row)`.
5. Preserve existing join filter behavior, including `tryJoinFilter` invocation and the current projection semantics.
6. Delete `iterateAll` if it becomes unused.
7. Add/extend a benchmark that exercises `RegisterSet(..., committedView)` with a non-nil view so allocation changes are measurable on the initial-query path.

Files likely to change
- Modify: `subscription/register_set.go`
- Modify: `subscription/register_set_test.go`
- Modify: `subscription/bench_test.go`

Execution plan

## Task 1: Pin current behavior with narrow tests
Objective
- Make the initial-query semantics explicit before refactoring the scan shape.

Steps
1. Read `subscription/register_set_test.go` and identify existing coverage for:
   - all-rows registration with committed rows
   - join registration with committed rows
   - `ErrInitialRowLimit`
2. Add targeted tests if coverage is weak:
   - join registration returns projected rows from committed state
   - cross-join-projected registration returns one row per projected-side row when the other side is non-empty
   - row-limit failure still trips at the same logical boundary
3. Keep tests in `subscription/register_set_test.go` unless an existing benchmark/test helper file is a better fit.

Likely new tests
- `TestRegisterSetJoinInitialQueryProjectsCommittedRows`
- `TestRegisterSetCrossJoinProjectedInitialQueryStreamsProjectedRows`
- If needed, strengthen `TestRegisterSetUnwindsPartialStateOnInitialQueryError`

## Task 2: Refactor `initialQuery` to stream scans
Objective
- Remove avoidable full-table materialization while keeping exact behavior.

Steps
1. Edit `subscription/register_set.go`.
2. In the `Join` branch:
   - Replace the inline `func() []types.ProductValue { ... iterateAll ... }()` closures with direct `for _, lrow := range view.TableScan(p.Left)` / `for _, rrow := range view.TableScan(p.Right)` loops.
   - Leave index lookup and `GetRow` probing unchanged.
   - Keep the current `tryJoinFilter` check and `project(...)` helper unchanged except for any minimal cleanup required by the new loop shape.
3. In the `CrossJoinProjected` branch:
   - Iterate directly over `view.TableScan(p.Projected)`.
4. In the default branch:
   - Iterate directly over `view.TableScan(t)` and retain `MatchRow` filtering.
5. Remove `iterateAll` if no callers remain.
6. Do not change `RegisterSet` orchestration.

## Task 3: Add a benchmark for the initial-query registration path
Objective
- Make the punchlist claim measurable for future before/after comparison.

Steps
1. Extend `subscription/bench_test.go` with a benchmark that uses a populated committed view and calls `RegisterSet` on a fresh manager or unique `QueryID`s.
2. Prefer a benchmark shape that isolates registration bootstrap work, for example:
   - many committed rows in one table
   - `AllRows{Table: ...}` or a simple filtered predicate with non-nil view
   - one benchmark for single-table register path
   - optional second benchmark for join bootstrap if still cheap to maintain
3. Call `b.ReportAllocs()`.
4. Avoid benchmark setups that accidentally measure only nil-view registration, since that misses the targeted path.

Suggested benchmark names
- `BenchmarkRegisterSetInitialQueryAllRows`
- optional: `BenchmarkRegisterSetInitialQueryJoin`

## Task 4: Verify narrowly
Objective
- Confirm correctness and get allocation evidence without widening scope.

Commands
1. `rtk go test ./subscription`
2. If exported behavior or interfaces changed materially, also run: `rtk go vet ./subscription`
3. Run focused benchmarks for evidence:
   - `rtk go test ./subscription -run '^$' -bench 'RegisterSetInitialQuery|RegisterUnregister' -benchmem`
4. Format touched package:
   - `rtk go fmt ./subscription`
5. Re-run `rtk go test ./subscription`

Expected verification outcome
- Subscription package tests remain green.
- New benchmark runs with `-benchmem` and gives a baseline showing initial-query allocations.
- No spec-visible behavior changes.

Risks and watchouts
- `iter.Seq2` loops must preserve existing row emission behavior; do not introduce early termination except through existing `add(row)` limit failures.
- Join branch control flow currently uses `break` after the RHS-index-backed path. Preserve the “use RHS index if available, else require LHS index” behavior exactly.
- Tests use `mockCommitted.TableScan` over a map, so row ordering is intentionally unstable. Assertions should compare sets or lengths unless order is already guaranteed elsewhere.
- Do not accidentally widen this into helper extraction or larger `initialQuery` cleanup from P1-06.
- Because the worktree is already dirty, verify final diffs stay confined to the planned subscription files.

Ready-to-execute checklist
- Relevant spec and decomposition context read: yes.
- Hot code path inspected: yes.
- Existing tests/bench files identified: yes.
- Dirty-worktree constraint noted: yes.
- Proposed slice is narrow and implementation-ready: yes.

First execution move
- Start with Task 1 by adding the missing behavior-pinning tests in `subscription/register_set_test.go`, then refactor `subscription/register_set.go` under that safety net.