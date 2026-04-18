# Plan: parity drift cleanup after Phase 1.5 / Phase 2 Slice 2 audit

## Goal
Align the live code comments, parity docs, and handoff references with the current repository reality so the audited slices are accurate and ready to commit.

## Current context
Verified from the live tree:
- `executor/executor.go` still contains a stale Phase 1.5 comment claiming caller metadata is stubbed/deferred even though the code now populates `CallerIdentity`, `ReducerName`, `ReducerID`, `Args`, `Timestamp`, and `TotalHostExecutionDuration`.
- `SPEC-AUDIT.md` still claims all numeric metadata is zero in Phase 1.5; that is only still true for `EnergyQuantaUsed`.
- `SPEC-AUDIT.md` still references old test name `TestPhase1DeferralSubscribeNoQueryIdOrMultiVariants`; live test is `TestPhase2DeferralSubscribeNoMultiOrSingleVariants`.
- `NEXT-SESSION-PROMPT.md` still references the old test name.
- There is an existing uncommitted cosmetic doc-comment edit in `subscription/fanout_worker.go`; do not disturb unrelated work beyond preserving it.

## Constraints
- Stay narrow: fix only the identified drift and directly adjacent stale references.
- Do not alter protocol/runtime semantics.
- Keep edits minimal and documentation-facing except for the stale executor comment.

## Proposed approach
1. Patch the stale executor comment so it describes the actual post-commit metadata wiring.
2. Patch `SPEC-AUDIT.md` to:
   - say that `Timestamp` and `TotalHostExecutionDuration` are now live,
   - keep `EnergyQuantaUsed` as the only remaining permanent zero,
   - update the stale parity test reference for the SubscribeMulti/Single deferral.
3. Patch `NEXT-SESSION-PROMPT.md` to reference the current parity test name.
4. Search for any other occurrences of the old stale test name / obsolete “stubbed as zero” language in the touched parity docs and update only if they are clearly wrong.
5. Run validation:
   - targeted grep/readback of touched lines,
   - `rtk go test ./...`,
   - `rtk go vet ./...`.
6. Run an independent review over the resulting diff before declaring ready to commit.

## Files expected to change
- `executor/executor.go`
- `SPEC-AUDIT.md`
- `NEXT-SESSION-PROMPT.md`

## Verification
- Confirm touched comments/docs match current code behavior.
- `rtk go test ./...`
- `rtk go vet ./...`
- independent reviewer subagent on final diff

## Risks / tradeoffs
- Main risk is over-editing handoff/docs beyond the identified drift. Keep patches surgical.
- Because there are pre-existing untracked handoff files and a separate cosmetic diff in `subscription/fanout_worker.go`, verification should focus on the intended diff and avoid modifying unrelated work.
