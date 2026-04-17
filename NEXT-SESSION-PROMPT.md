Continue Shunter in a fresh agent session.

This is not a docs-softening pass and not a new broad audit-from-scratch pass.

Primary decision
- Treat the reconciled specs/epics/stories in `docs/decomposition/**` as the intended source of truth.
- Do not update specs downward to match stale code unless you find a clear internal contradiction in the docs themselves.
- Do not start a new broad code-audit pass yet; that would mostly rediscover already-tracked mismatches.
- Best next course of action: execute a narrow implementation-reconciliation slice against the highest-severity open runtime debt.

Chosen next slice
- SPEC-002 E6 recovery hardening
- Fix `TD-114` and `TD-115` from `TECH-DEBT.md`
- Scope: active-tail corruption / append-reopen recovery behavior only

Why this is the best next move
- `REMAINING.md` says all tracked implementation slices are already complete, so the highest-value work is reconciliation/fix work, not more decomposition writing.
- `AUDIT_HANDOFF.md` Lane B has already carried the spec-audit insight propagation through Session 11. The docs pass is effectively complete enough to guide implementation.
- `TECH-DEBT.md` already contains concrete, grounded implementation mismatches. Starting another full audit pass now would duplicate work instead of reducing risk.
- `TD-114` and `TD-115` are both high-severity, in the same narrow recovery slice, and directly affect fail-closed / recoverable-tail behavior.

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
   - recovery sections around replay horizon, damaged tail, and append-open behavior
8. `docs/decomposition/002-commitlog/epic-6-recovery/EPIC.md`
9. `docs/decomposition/002-commitlog/epic-6-recovery/story-6.1-segment-scanning.md`
10. `docs/decomposition/002-commitlog/epic-6-recovery/story-6.4-open-and-recover.md`

Hard rules
- Use RTK for every shell/git command.
- Stay narrowly inside the recovery/append-open slice.
- Regression-first: add or update focused tests before or alongside code changes.
- Do not broaden into a docs drift cleanup pass.
- Do not soften specs to match live behavior unless you find a true doc contradiction that blocks implementation.

Implementation target
Bring code into line with the intended recovery contract already reflected in the updated docs.

1. `TD-114`
- Active-tail checksum mismatch after a valid prefix should be treated like damaged partial tail state.
- Recovery should stop at the last valid contiguous record.
- Resume mode should become `AppendByFreshNextSegment`, not a hard failure.
- Keep sealed-segment checksum mismatches fatal.
- Keep first-record corruption in the active segment fatal when there is no valid prefix.

2. `TD-115`
- `OpenSegmentForAppend(...)` must fail closed on corrupt-first-record / no-valid-prefix cases.
- Do not silently truncate a segment back to header-only size when the first record is corrupt.
- Only allow truncate-and-resume behavior when a valid prefix exists.
- Preserve the append-forbidden edge case through the durability resume path.

Likely files to inspect/change
- `commitlog/segment_scan.go`
- `commitlog/segment_scan_test.go`
- `commitlog/segment.go`
- `commitlog/recovery.go`
- `commitlog/recovery_test.go`
- `commitlog/durability.go`
- related focused tests under `commitlog/*_test.go`

Suggested execution plan
1. Read `TD-114` and `TD-115` in full.
2. Read the SPEC-002 recovery stories listed above.
3. Add failing regression tests for:
   - active-segment checksum mismatch after a valid prefix => fresh-next-segment / valid replay horizon
   - sealed-segment checksum mismatch => still fatal
   - active-segment first-record corruption => hard error, append forbidden
   - `OpenSegmentForAppend(...)` does not truncate corrupt-first-record segments
   - durability resume path preserves append-forbidden behavior
4. Implement the minimum code changes to satisfy those tests.
5. Run focused tests, then broader package/repo verification.
6. Update `TECH-DEBT.md` statuses/details for `TD-114` / `TD-115` if fixed.
7. End with a short note naming the next best implementation-reconciliation slice.

Verification commands
- `rtk go test ./commitlog`
- `rtk go test ./...`
- while iterating, if helpful:
  - `rtk go test ./commitlog -run 'TestScanSegments|TestOpenAndRecover|TestOpenSegmentForAppend'`

Expected deliverable
- code + tests landing the recovery behavior required by `TD-114` and `TD-115`
- `TECH-DEBT.md` updated if either item is resolved
- concise note on the next best slice after this one

What not to do in this session
- do not start Lane B Session 12 docs drift triage
- do not start Lane A’s remaining broad audit slice
- do not rewrite docs to match stale code
- do not widen into unrelated commitlog cleanup

Decision for future sessions after this one
- If `TD-114`/`TD-115` land cleanly, continue implementation reconciliation on the next highest-value open runtime debt, not a fresh broad audit.
- Only return to docs work if you discover an actual contradiction in the reconciled specs, or after a code fix materially changes the intended contract.

Stop rule
- Stop when both debt items are either fixed with regression coverage, or one is fixed and the other is blocked by a clearly identified design constraint backed by test evidence.
- Do not pivot into a new audit pass.