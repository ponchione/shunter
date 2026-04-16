# Audit Handoff

Objective
- Continue the code-vs-spec audit from `docs/EXECUTION-ORDER.md`.
- Keep appending grounded findings to root `TECH-DEBT.md`.
- Current audit trail is now advanced through:
  - `SPEC-002 E5`
  - `SPEC-002 E4`
- Latest logged debt IDs are now `TD-020` through `TD-026`.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. the specific decomposition docs for the next slice
6. `TECH-DEBT.md` (phase plan + open items near top)

Shell rule
- Prefix every shell command with `rtk`.

Current audit status
Already audited in sequence:
- `SPEC-001 E1`
- `SPEC-006 E2`
- `SPEC-006 E1`
- `SPEC-003 E1.1 + E1.2 + minimal E1.4 contract slice`
- `SPEC-006 E3.1`
- `SPEC-006 E4`
- `SPEC-006 E3.2`
- `SPEC-006 E5`
- `SPEC-006 E6`
- `SPEC-001 E2`
- `SPEC-001 E3`
- `SPEC-001 E4`
- `SPEC-001 E5`
- `SPEC-001 E6`
- `SPEC-001 E7`
- `SPEC-001 E8`
- `SPEC-002 E1`
- `SPEC-002 E2`
- `SPEC-003 E2`
- `SPEC-003 E3`
- `SPEC-003 E4`
- `SPEC-002 E3`
- `SPEC-002 E5`
- `SPEC-002 E4`

Next execution-order slice
- `SPEC-002 E6: Recovery`

Recommended next reading
- `docs/decomposition/002-commitlog/epic-6-recovery/EPIC.md`
- all Epic 6 story docs
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` sections on recovery
- live files likely to matter:
  - `commitlog/durability.go`
  - `commitlog/segment.go`
  - `commitlog/changeset_codec.go`
  - `commitlog/errors.go`
  - any new recovery files if they appear
  - `commitlog/commitlog_test.go`
  - `commitlog/phase4_acceptance_test.go`

Newest findings added this pass
- `TD-020`: `SubmitWithContext` ignores reject-on-full policy and returns context timeout instead of `ErrExecutorBusy`
- `TD-021`: subscription-command dispatch path is still missing
- `TD-022`: `ReducerContext` still exposes `DB` and `Scheduler` as `any`
- `TD-023`: `DecodeChangeset` public signature does not match documented decoder surface
- `TD-024`: snapshot I/O surface is almost entirely unimplemented
- `TD-025`: `NewDurabilityWorker` recreates/truncates an existing active segment instead of opening/resuming it
- `TD-026`: `CommitLogOptions` is missing documented `SnapshotInterval`

Important open themes to keep in mind
- Passing tests often mean operational health only, not spec completeness.
- Public API drift has been common in this repo; compile-only repros have been very effective.
- Some decomposition filenames differ from shorthand expectations; use `search_files` before assuming a path.
- Prefer tool-driven cleanup for temporary audit packages (`execute_code` with `shutil.rmtree`) instead of shell `rm`.

Audit method to continue using
1. Read the exact epic/story docs for the next slice.
2. Read the smallest decisive implementation files.
3. Read the relevant tests.
4. Run targeted verification with `rtk go test ...`.
5. If the gap is public API shape, add a tiny compile-only repro package.
6. If the gap is runtime behavior, add a tiny repro program if needed.
7. Append only grounded findings to `TECH-DEBT.md`.
8. Update the audit phase plan section near the top of `TECH-DEBT.md`.

Useful verification commands already used
- `rtk go test ./executor`
- `rtk go test ./commitlog`
- broader earlier pass: `rtk go test ./types ./bsatn ./schema ./store ./subscription ./executor ./commitlog`

Expected deliverable for next agent
- Audit `SPEC-002 E6`
- append any new grounded debt items to `TECH-DEBT.md`
- update the phase plan/note block
- report the next slice after that
