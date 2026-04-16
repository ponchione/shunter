# Autonomous implementation pass: TD-031 / TD-032 / TD-033

Goal
- Fix the next correctness-fatal store commit issues in one narrow pass:
  - TD-031 partial commit mutation / atomicity break
  - TD-032 rollback can still commit
  - TD-033 missing delete target silently skipped during commit

Context reviewed
- `RTK.md`
- `CLAUDE.md`
- `AGENTS.md`
- `docs/project-brief.md`
- `docs/EXECUTION-ORDER.md`
- `TECH-DEBT.md`
- `docs/decomposition/001-store/EPICS.md`
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/EPIC.md`
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.2-commit.md`
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.4-rollback.md`

Observed starting state
- `store/commit.go` already blocks commit after rollback with `ErrTransactionRolledBack`, so TD-032 may already be fixed in code and just need verification/update in TECH-DEBT.
- `Commit(...)` still mutates committed state table-by-table and row-by-row. If an insert fails after some deletes/inserts already applied, committed state is partially mutated.
- Delete application currently ignores missing rows (`if oldRow, ok := table.DeleteRow(rowID); ok { ... }`), so commit can silently skip a bad delete.

Planned execution
1. Add focused failing tests first for:
   - commit-time insert failure leaves committed state unchanged
   - missing delete target causes commit error and leaves state unchanged
   - rollback blocks commit (verify current behavior; keep as regression coverage if already green)
2. Run focused `rtk go test ./store -run ...` and confirm the intended failures.
3. Implement the minimal fix in `store/commit.go`:
   - validate commit-time deletes before mutation
   - stage commit validation so mutation starts only after all failure conditions are known
   - preserve delete-before-insert ordering while keeping the overall operation atomic
4. Re-run focused store tests, then `rtk go test ./store`.
5. Update `TECH-DEBT.md` to mark TD-031/032/033 resolved with exact verification commands.
6. Run full verification:
   - `rtk go build ./...`
   - `rtk go vet ./...`
   - `rtk go test ./...`

Likely files to change
- `store/commit.go`
- `store/store_test.go` and/or `store/audit_regression_test.go`
- `TECH-DEBT.md`

Risks / cautions
- Keep scope strictly inside the store commit slice.
- Do not broaden into TD-034+ even if adjacent issues are visible.
- Preserve existing net-effect/undelete behavior and delete-before-insert semantics.
