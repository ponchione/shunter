Continue Shunter implementation work in a fresh session. This is not an audit pass.

Context
- `REMAINING.md` says all currently tracked execution-order implementation slices are complete.
- The next useful implementation slice is therefore debt reduction on the highest-severity open runtime bugs, not more audit work.
- Pick a narrow regression-first fix slice.

Chosen next slice
- SPEC-002 E6 recovery hardening
- Fix `TD-114` and `TD-115` from `TECH-DEBT.md`
- Scope: active-tail corruption / append-reopen recovery behavior only

Why this slice next
- It is the highest-severity open implementation debt currently tracked (`high`).
- Both items are in the same execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4i (`SPEC-002 E6: Recovery`).
- The fixes are tightly related and should be handled together with focused regression coverage.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `REMAINING.md`
6. `TECH-DEBT.md`
   - `TD-114`
   - `TD-115`
7. `docs/decomposition/002-commitlog/SPEC-002-commitlog.md`
   - recovery sections around the replay horizon / damaged-tail rules
8. `docs/decomposition/002-commitlog/epic-6-recovery/EPIC.md`
9. `docs/decomposition/002-commitlog/epic-6-recovery/story-6.1-segment-scanning.md`
10. `docs/decomposition/002-commitlog/epic-6-recovery/story-6.4-open-and-recover.md`

Implementation target
Make recovery/append behavior match the current spec contract:

1. `TD-114`
- Active-tail checksum mismatch after a valid prefix should be treated like a damaged partial tail.
- Recovery should stop at the last valid contiguous record.
- Resume mode should become fresh-next-segment, not hard failure.
- Keep sealed-segment checksum mismatches fatal.
- Keep first-record corruption in the active segment fatal when there is no valid prefix.

2. `TD-115`
- `OpenSegmentForAppend(...)` must fail closed on corrupt-first-record / no-valid-prefix cases.
- Do not silently truncate a segment back to header-only size when the first record is corrupt.
- Only allow truncate-and-resume behavior when a valid prefix exists.
- Preserve the append-forbidden edge case through the durability resume path.

Working style
- Narrow regression-first pass only.
- Add or update focused tests before or alongside code changes.
- Stay inside the recovery/append-open slice; do not widen into unrelated commitlog cleanup.
- Use RTK for every shell/git command.

Likely files to inspect/change
- `commitlog/segment_scan.go`
- `commitlog/segment_scan_test.go`
- `commitlog/segment.go`
- `commitlog/recovery.go`
- `commitlog/recovery_test.go`
- `commitlog/durability.go`
- any recovery/append reopen tests under `commitlog/*_test.go`

Suggested approach
1. Read the two TECH-DEBT entries and the recovery stories carefully.
2. Add failing regression tests for:
   - active-segment checksum mismatch after a valid prefix => fresh-next-segment / valid horizon
   - sealed-segment checksum mismatch => still fatal
   - active-segment first-record corruption => hard error, append forbidden
   - `OpenSegmentForAppend(...)` does not truncate corrupt-first-record segments
   - resume-plan / durability path preserves the append-forbidden case
3. Implement the minimal code changes to satisfy those tests.
4. Re-run focused package tests, then broader commitlog/repo tests if clean.
5. Update `TECH-DEBT.md` statuses/details for `TD-114` / `TD-115` if fixed.
6. Report what remains in SPEC-002 E6 after this slice, if anything.

Verification commands
- `rtk go test ./commitlog`
- `rtk go test ./...`
- if needed, add a focused single-test run while iterating:
  - `rtk go test ./commitlog -run 'TestScanSegments|TestOpenAndRecover|TestOpenSegmentForAppend'`

Expected deliverable
- code + tests landing the recovery behavior required by `TD-114` and `TD-115`
- `TECH-DEBT.md` updated if either item is resolved
- concise handoff note naming the next highest-leverage implementation debt slice after this one

Stop rule
- Stop when both debt items are either fixed with regression coverage, or one is fixed and the other is blocked by a clearly identified design constraint backed by test evidence.
- Do not pivot into a new audit pass.
