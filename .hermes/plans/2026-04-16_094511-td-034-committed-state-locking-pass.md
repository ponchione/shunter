# Autonomous implementation pass: TD-034 committed_state locking

Goal
- Fix TD-034 in `store/committed_state.go`: unsynchronized `RegisterTable` and `Table()` access to the `tables` map.

Context reviewed
- `RTK.md`
- `CLAUDE.md`
- `AGENTS.md`
- `docs/project-brief.md`
- `docs/EXECUTION-ORDER.md`
- `TECH-DEBT.md`
- `docs/decomposition/001-store/epic-5-transaction-layer/EPIC.md`
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.1-committed-state.md`
- `store/committed_state.go`
- `store/state_view.go`
- relevant store tests

Observed starting state
- `CommittedState` already owns an `RWMutex`, but `RegisterTable`, `Table`, and `TableIDs` access `tables` without taking it.
- This violates Story 5.1's locking contract and is race-detector visible even if intended usage is mostly startup-only.

Plan
1. Add a focused failing regression test that races `RegisterTable` and `Table`/`TableIDs` under `rtk go test -race ./store -run ...`.
2. Run the focused race test and observe failure.
3. Implement the narrow fix in `store/committed_state.go`:
   - `RegisterTable` uses `cs.mu.Lock()` / `Unlock()`
   - `Table` and `TableIDs` use `RLock()` / `RUnlock()`
4. Re-run the focused race test, then `rtk go test ./store`.
5. Update `TECH-DEBT.md` to mark TD-034 resolved with exact verification commands.
6. Run full verification:
   - `rtk go build ./...`
   - `rtk go vet ./...`
   - `rtk go test ./...`

Likely files to change
- `store/committed_state.go`
- `store/audit_regression_test.go` or `store/state_view_test.go`
- `TECH-DEBT.md`

Scope guard
- Stay strictly on TD-034.
- Do not broaden into snapshot ownership or other store concurrency items in this pass.
