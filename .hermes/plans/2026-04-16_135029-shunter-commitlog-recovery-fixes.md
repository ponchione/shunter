# Shunter Commitlog Recovery Fix Plan

> For Hermes: plan only. Do not implement from this document without a separate execution request.

Goal
- Fix the Epic 6 recovery issues found in the strict repo-grounded audit:
  1. active-segment corruption after a valid prefix is incorrectly downgraded into damaged-tail recovery
  2. replay currently swallows read errors for `AppendByFreshNextSegment` instead of avoiding the damaged suffix entirely
  3. the test suite falsely claims coverage for the “after valid prefix” corruption case
  4. `OpenAndRecover` computes append-resume planning but does not expose enough information for later durability startup wiring

Relevant docs
- `RTK.md`
- `docs/project-brief.md`
- `docs/EXECUTION-ORDER.md`
- `docs/decomposition/002-commitlog/epic-6-recovery/EPIC.md`
- `docs/decomposition/002-commitlog/epic-6-recovery/story-6.1-segment-scanning.md`
- `docs/decomposition/002-commitlog/epic-6-recovery/story-6.3-log-replay.md`
- `docs/decomposition/002-commitlog/epic-6-recovery/story-6.4-open-and-recover.md`
- `docs/decomposition/002-commitlog/epic-6-recovery/story-6.5-recovery-error-types.md`

Relevant code surface
- `commitlog/segment_scan.go`
- `commitlog/segment_scan_test.go`
- `commitlog/replay.go`
- `commitlog/replay_test.go`
- `commitlog/recovery.go`
- `commitlog/recovery_test.go`
- nearby behavior references:
  - `commitlog/segment.go`
  - `commitlog/durability.go`

Current repo-grounded findings
- `commitlog/segment.go:107-146` already distinguishes true truncation (`ErrTruncatedRecord`) from checksum corruption (`ChecksumMismatchError`) and other hard errors.
- `commitlog/segment_scan.go:161-165` currently treats any non-EOF read error in the last segment after at least one valid record as resumable damaged tail.
- `commitlog/replay.go:28-30` currently suppresses later read errors once `maxAppliedTxID >= segment.LastTx` for `AppendByFreshNextSegment`.
- `commitlog/segment_scan_test.go:154-163` claims to cover corruption “after valid prefix”, but its corruption offset lands in the first record, not a later record.
- `commitlog/durability.go:63-64` shows later startup needs a `startTxID` to decide whether to reopen an active segment or create a fresh next one.
- `commitlog/recovery.go:65-67` computes a resume plan and drops it, so the current API cannot carry clean-tail-vs-fresh-next information forward.

Non-goal / caution
- Do not widen into unrelated snapshot work. `rtk go test ./commitlog` and `rtk go test ./...` currently fail on `commitlog/snapshot_test.go`, which is adjacent but outside the audited recovery issue set.
- Do not mutate durability worker behavior unless recovery API exposure truly requires it. The immediate fix is to make recovery produce correct metadata and tests.

Proposed approach
- Fix the root-cause classification bug in scanning first.
- Then simplify replay so it never needs to “forgive” a read error after the valid prefix; it should stop before probing damaged bytes.
- Then repair and expand tests so the subtle corruption-vs-truncation distinction is actually proven.
- Finally, expose resume planning in a recovery-facing API that preserves the current story contract while making later durability wiring possible.

## Workstream 1: Tighten active-segment damaged-tail classification

Objective
- Make `ScanSegments` degrade only on true truncated-tail damage, never on checksum mismatch or other non-tail corruption.

Files likely to change
- Modify: `commitlog/segment_scan.go`
- Modify: `commitlog/segment_scan_test.go`

Implementation plan
1. In `commitlog/segment_scan.go`, isolate the tolerated-tail decision into a small helper or explicit branch.
   - Allowed downgrade case:
     - segment is last
     - at least one valid record was already scanned
     - error is specifically `ErrTruncatedRecord` (or another explicitly approved EOF-style tail truncation if introduced later)
   - Hard-error cases:
     - `ChecksumMismatchError`
     - `ErrBadFlags`
     - `UnknownRecordTypeError`
     - any other framing/CRC error that is not a truncated tail
2. Keep the existing first-record behavior strict.
   - If `recordCount == 0`, any read error remains fatal.
3. Keep sealed segments strict.
   - Any non-EOF error in a non-last segment remains fatal.
4. Do not change contiguity logic in this pass.

Why this matches the docs
- Story 6.1 only allows “Partial record at end of last segment” to stop at the valid prefix and set `AppendByFreshNextSegment`.
- Story 6.1 explicitly says non-tail corruption is a hard error.

Recommended tests
1. Repair the misleading test.
   - Replace the current corruption offset in `TestScanSegmentsCorruptActiveSegmentAfterValidPrefixIsHardError` so it corrupts the second record’s payload or CRC, not the first record.
   - The file should contain at least two valid records before corruption is injected.
2. Add a dedicated checksum-mismatch test after a valid prefix.
   - Create a segment with tx 1, 2, 3.
   - Corrupt tx 3’s CRC or payload while leaving tx 1 and tx 2 intact.
   - Expect hard error, not `AppendByFreshNextSegment`.
3. Keep the existing truncated-tail case.
   - Ensure truncation still produces `AppendByFreshNextSegment` and horizon at the last valid tx.

Validation commands
- `rtk go test ./commitlog -run TestScanSegments -v`

## Workstream 2: Make replay stop at the validated prefix instead of swallowing read errors

Objective
- Replay should use `SegmentInfo.LastTx` and `AppendMode` as a bound, not as a reason to forgive a later read failure.

Files likely to change
- Modify: `commitlog/replay.go`
- Modify: `commitlog/replay_test.go`

Implementation plan
1. Remove the current read-error suppression branch in `ReplayLog`:
   - current branch: `if segment.AppendMode == AppendByFreshNextSegment && maxAppliedTxID >= segment.LastTx { break }`
   - this is too broad because it converts real corruption into success.
2. Add an up-front segment skip optimization:
   - if `segment.LastTx <= fromTxID`, skip opening or replaying that segment entirely.
   - this also avoids probing a damaged active tail when the caller already starts after the valid prefix.
3. Bound replay within a damaged-tail segment using metadata, not error suppression.
   - While replaying a segment with `AppendByFreshNextSegment`, break immediately after successfully reading/applying the record whose `TxID == segment.LastTx`.
   - Do not call `reader.Next()` again after that point, because the next bytes are known to be outside the validated prefix.
4. Keep all read/decode/apply errors fatal before the valid prefix is complete.
   - Any read error before `record.TxID == segment.LastTx` in a damaged-tail segment must still fail.
5. Preserve context wrapping for open/read/decode/apply failures.

Why this matches the docs
- Story 6.3 says replay errors are fatal.
- The recovery design intent is not “ignore read failures”; it is “trust the scan metadata and stop at the validated prefix”.

Recommended tests
1. Add a replay test for damaged-tail truncation.
   - Build a real truncated last segment via `ScanSegments` metadata or construct a segment plus matching `SegmentInfo`.
   - Verify replay returns `segment.LastTx` and does not error.
2. Add a replay test for corruption after valid prefix.
   - Use a segment that has valid tx 1 and tx 2, then corrupted tx 3.
   - Ensure replay fails with segment-path context if `SegmentInfo` still says the segment should be replayed through tx 3.
   - More importantly, ensure the scan layer never marks this case as `AppendByFreshNextSegment`; the combined scan+replay path should fail.
3. Add a skip-all damaged-tail test.
   - If `fromTxID >= segment.LastTx`, replay should skip the segment without hitting the damaged suffix.

Validation commands
- `rtk go test ./commitlog -run ReplayLog -v`
- `rtk go test ./commitlog -run 'TestScanSegments|TestReplayLog' -v`

## Workstream 3: Expose recovery resume planning without breaking the existing story contract

Objective
- Make recovery produce usable resume metadata for later durability startup wiring while preserving the current `OpenAndRecover` contract for existing callers/tests.

Files likely to change
- Modify: `commitlog/recovery.go`
- Modify: `commitlog/recovery_test.go`
- Optional new file if desired for API clarity: `commitlog/recovery_plan.go`

Design constraint
- Story 6.4 currently specifies `OpenAndRecover(dir string, schema SchemaRegistry) (*CommittedState, TxID, error)`.
- The repo currently has no non-test call sites for `OpenAndRecover`, so an API extension is feasible, but the most conservative path is to preserve the old function as a wrapper.

Preferred API plan
1. Introduce an exported resume-plan type.
   Example shape:
   - `type RecoveryResumePlan struct { SegmentStartTx types.TxID; NextTxID types.TxID; AppendMode AppendMode }`
2. Introduce a new exported detailed recovery entrypoint, for example:
   - `OpenAndRecoverDetailed(dir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, RecoveryResumePlan, error)`
   or
   - `Recover(dir string, reg schema.SchemaRegistry) (*RecoveryResult, error)` where `RecoveryResult` bundles state, max tx, and plan.
3. Re-implement `OpenAndRecover` as a thin wrapper around the detailed API.
   - It should discard the plan only in the wrapper, preserving backward compatibility.
4. Keep `planRecoveryResume` as the single decision point.
   - Either export it (renamed) or keep it private and expose only the result through the detailed recovery API.
5. Do not wire `NewDurabilityWorker` in this change unless the user explicitly asks for startup integration.
   - For now, expose enough data so the next slice can choose:
     - clean tail: reopen active segment using `SegmentStartTx`
     - damaged tail: create fresh next segment at `NextTxID`

Why this is the least risky fix
- It resolves the identified design gap without forcing an immediate broader startup refactor.
- It keeps the existing `OpenAndRecover` signature usable for tests and current code.
- It aligns with `commitlog/durability.go`, where startup behavior depends on `startTxID`, not just max tx.

Recommended tests
1. Add a clean-tail resume-plan test.
   - Create a healthy active segment ending at tx N.
   - Verify detailed recovery returns `AppendInPlace`, `SegmentStartTx == lastSegment.StartTx`, `NextTxID == N+1`.
2. Keep and possibly migrate the current damaged-tail resume-plan test.
   - Verify the detailed recovery API returns `AppendByFreshNextSegment`, `SegmentStartTx == maxTx+1`, `NextTxID == maxTx+1`.
3. Keep `OpenAndRecover` compatibility tests.
   - Existing tests should continue to pass when calling the wrapper.

Validation commands
- `rtk go test ./commitlog -run 'TestOpenAndRecover|TestRecoveryResumePlan' -v`

## Workstream 4: Tighten the test suite so it proves the intended behavior

Objective
- Eliminate false confidence and make the acceptance criteria observable in tests.

Files likely to change
- Modify: `commitlog/segment_scan_test.go`
- Modify: `commitlog/replay_test.go`
- Modify: `commitlog/recovery_test.go`

Test additions / corrections
1. Rename tests only after their mechanics are correct.
   - Any test with “after valid prefix” in its name must corrupt a later record, not the first record.
2. Prefer corruption helpers that target a specific record section.
   - Today’s byte offsets are too opaque and easy to misplace.
   - Add tiny test-local helpers that compute offsets for:
     - record payload byte
     - CRC byte
     - nth record in file
3. Add one end-to-end scan + recovery regression test for the exact audit bug.
   - healthy tx 1, healthy tx 2, corrupt tx 3 in active last segment
   - `OpenAndRecover` should fail
   - this is the highest-value regression because it proves the misclassification is gone
4. Add one end-to-end truncation regression test.
   - healthy tx 1, healthy tx 2, truncated tx 3 tail in active last segment
   - `OpenAndRecover` should succeed at tx 2 and expose fresh-next resume planning

Validation commands
- `rtk go test ./commitlog -run 'TestScanSegments|TestReplayLog|TestOpenAndRecover' -v`

## Suggested execution order
1. Fix and expand `ScanSegments` classification/tests first.
2. Update `ReplayLog` to use metadata-bounded stopping instead of error swallowing.
3. Add the detailed recovery API and wire existing `OpenAndRecover` through it.
4. Add/repair the end-to-end recovery tests.
5. Run package and repo verification.

## Verification checklist
- `rtk go test ./commitlog -run TestScanSegments -v`
- `rtk go test ./commitlog -run ReplayLog -v`
- `rtk go test ./commitlog -run 'TestOpenAndRecover|TestRecoveryResumePlan' -v`
- `rtk go test ./commitlog`
- `rtk go test ./...`

Expected outcome after these fixes
- A truncated active tail after a valid prefix still recovers safely and plans a fresh next segment.
- A checksum-mismatched or otherwise corrupt later record in the active segment fails recovery instead of being silently downgraded.
- Replay no longer “forgives” read errors; it stops at the validated prefix by construction.
- The recovery API exposes enough metadata for later durability startup wiring to distinguish reopen-in-place from start-fresh-next.
- The test suite proves the subtle edge cases instead of only appearing to cover them.

Risks / open questions
- API naming: whether to expose a new detailed recovery function or a result struct. Prefer a wrapper-preserving approach unless the docs are being updated in the same slice.
- If the user wants strict spec lockstep with Story 6.4’s current signature, document the additional API as an additive extension rather than a replacement.
- Full repo green may still be blocked by the existing snapshot test failure outside this recovery scope; treat that as adjacent cleanup, not part of the core recovery fix slice.
