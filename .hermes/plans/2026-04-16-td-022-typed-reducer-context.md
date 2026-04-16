# TD-022 typed ReducerContext plan

Goal
- Replace `types.ReducerContext`'s erased `DB any` and `Scheduler any` fields with cycle-safe typed interfaces that let reducer authors call the documented runtime methods directly.

Scope
- Stay inside SPEC-003 Epic 4 reducer execution and the public reducer contract surface.
- Use a narrow shared-interface approach to avoid package cycles instead of widening scope into larger executor/store ownership moves.

Files
- Modify: types/reducer.go
- Modify: executor/executor.go
- Modify: executor/lifecycle.go
- Modify: executor/contracts_test.go
- Modify: executor/executor_test.go
- Modify: executor/phase4_acceptance_test.go
- Modify: executor/scheduler_test.go
- Modify: executor/pipeline_test.go
- Modify: TECH-DEBT.md

Plan
1. Add failing tests first that prove reducers can call `ctx.DB.Insert(...)` and `ctx.Scheduler.Cancel(...)` directly without type assertions.
2. Run focused executor tests and confirm they fail for the expected reason (`any`-typed fields).
3. Implement cycle-safe public interfaces in `types/reducer.go`:
   - `ReducerDB` with the reducer-facing transaction methods
   - `ReducerScheduler` with the scheduler-facing methods
   - update `ReducerContext` fields to those interfaces
4. Add executor-side adapters/wrappers that bind a real `*store.Transaction` and scheduler handle to those interfaces when building reducer contexts.
5. Update executor tests to stop type-asserting `ctx.DB` / `ctx.Scheduler` for normal reducer usage; keep only narrow internal assertions if still needed through adapter escape hatches.
6. Re-run focused executor tests. Then update TECH-DEBT.md to mark TD-022 resolved.
7. Run repo verification commands and report any unrelated pre-existing blockers separately.

Verification commands
- rtk go test ./executor -run 'TestReducerContractsMatchPhase1dSpec|TestPhase4HandleCallReducerBeginExecuteCommitRollback|TestSchedulerHandleCommitPersistsRow|TestSchedulerHandleRollbackDiscardsSchedule' -count=1
- rtk go test ./executor
- rtk go build ./...
- rtk go vet ./...
- rtk go test ./...
