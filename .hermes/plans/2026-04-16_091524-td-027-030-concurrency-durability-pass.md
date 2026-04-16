# Autonomous implementation pass: TD-027, TD-028, TD-029, TD-030

Goal
- Complete the highest-priority remaining executor and commitlog correctness fixes in the requested order:
  1. TD-027 in `executor/executor.go`
  2. TD-028/TD-029/TD-030 in `commitlog/`
- Use regression-first changes and update `TECH-DEBT.md` when each slice is resolved.

Required context reviewed
- `RTK.md`
- `CLAUDE.md`
- `AGENTS.md`
- `docs/project-brief.md`
- `docs/EXECUTION-ORDER.md`
- `TECH-DEBT.md`
- Relevant decomposition docs for executor Epic 3 and commitlog Epic 4 / recovery stories

Observed starting state
- Repo has many unrelated tracked modifications in multiple packages.
- I must keep scope narrow and only touch the requested executor/commitlog debt slice plus the plan/TECH-DEBT updates.
- Existing executor code still uses unsynchronized bool flags (`fatal`, `shutdown`) and closes `inbox` directly while submitters may still be sending.
- Existing commitlog durability code still has enqueue/close races and a closed-channel drain bug; reopen logic exists but needs restart/resume regression coverage aligned to the requested slice.

Implementation plan

1. TD-027 executor slice
- Inspect `executor/executor.go` and existing executor tests.
- Add focused failing regression tests for:
  - submit after post-commit fatal latches fatal state
  - shutdown/submit boundary returns `ErrExecutorShutdown` without send-on-closed panic
  - double shutdown remains safe
- Run focused executor tests and confirm failure.
- Implement minimal fix shape:
  - replace unsynchronized shutdown/fatal bool reads with atomic state
  - add explicit close discipline so submissions never send directly to a possibly closed inbox
  - keep shutdown stop-admit -> close -> wait ordering intact
- Re-run focused executor tests, then broader `rtk go test ./executor`.
- Update `TECH-DEBT.md` to mark TD-027 resolved with exact behavior and commands.

2. TD-028/029/030 commitlog slice
- Inspect `commitlog/durability.go`, `commitlog/segment.go`, and existing package/acceptance tests.
- Add focused failing regression tests for:
  - enqueue during/after close does not panic via send-on-closed-channel race
  - closed channel drain exits cleanly instead of spinning zero values
  - reopen/resume existing active segment preserves prior data and durable tx state
- Run focused commitlog tests and confirm failure.
- Implement minimal fixes:
  - serialize enqueue/close admission so close cannot race a sender into panic
  - restructure drain loop to exit immediately on closed channel
  - keep/create safe reopen-or-create segment path and validate restored last tx/size
- Re-run focused tests, then broader `rtk go test ./commitlog`.
- Update `TECH-DEBT.md` to mark TD-028/029/030 resolved with exact behavior and commands.

3. Final verification
- Run:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`
- Summarize exact files changed, focused commands run, and final verification results.

Likely files to change
- `executor/executor.go`
- `executor/executor_test.go`
- possibly a new/adjacent focused executor test file if needed
- `commitlog/durability.go`
- `commitlog/segment.go`
- `commitlog/commitlog_test.go`
- `commitlog/phase4_acceptance_test.go`
- `TECH-DEBT.md`

Risks / cautions
- Do not trample unrelated local changes already present in the repo.
- Keep the executor fix narrow: preserve public API and existing semantics except for the race/panic corrections.
- Keep commitlog resume behavior aligned with current phase scope: safe reopen/resume without broad recovery-architecture expansion.
