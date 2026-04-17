# Lane B Session 9 — SPEC-003 residue cleanup plan

Goal
- Close or explicitly defer every open SPEC-003 residue row in `AUDIT_HANDOFF.md` §B.2 without touching live code.
- Keep edits inside `docs/decomposition/003-executor/**` unless a cross-spec citation or drift entry is strictly required.
- Advance Lane B cursor from Session 9 to Session 10.

Locked decisions / guardrails
- Follow `AUDIT_HANDOFF.md` §B.0 exactly.
- Do not revisit Cluster A–E resolutions.
- Do not edit live `executor/` or other code packages.
- If a stronger spec claim would outrun live code, either soften the spec to match live or add `TECH-DEBT.md` drift items starting at TD-129.
- Commit in small logical bundles with explicit paths.

Source inputs
- `CLAUDE.md`, `RTK.md`, `docs/project-brief.md`
- `AUDIT_HANDOFF.md` Lane B sections, especially SPEC-003 table and Session 9 cadence
- `SPEC-AUDIT.md` SPEC-003 section (lines 748–1180)
- `docs/decomposition/003-executor/SPEC-003-executor.md`
- `docs/decomposition/003-executor/EPICS.md`
- relevant story docs under `docs/decomposition/003-executor/epic-{1,3,5,6,7}/`
- style anchors from Session 7/8 commits (`2d1117f`, `0d193ee`)

Work bundles
1. Core type / command surface bundle
   - Rows: §1.2, §4.2, §4.9
   - Files: `SPEC-003-executor.md`, `EPICS.md`, `epic-1-core-types/story-1.2-reducer-types.md`, `story-1.3-command-types.md`, maybe `story-1.5-error-types.md`
   - Decisions:
     - add scheduled-call metadata carrier (`ScheduleID`, `IntendedFireAt`) to request surface for `CallSourceScheduled`
     - document `CallerContext.Timestamp` serialization semantics
     - pin nil-`ResponseCh` semantics

2. Post-commit + executor-core ownership bundle
   - Rows: §1.4, §2.4, §2.5, §2.11, §4.4, §4.5, §4.6, §4.8, §5.2, §5.4
   - Files: `SPEC-003-executor.md`, `EPICS.md`, `epic-3-executor-core/story-3.1-executor-struct.md`, `story-3.3-submit-methods.md`, `story-3.4-command-dispatch.md`, `story-3.5-shutdown.md`, `epic-5-post-commit-pipeline/story-5.1-ordered-pipeline.md`, maybe new Epic 3 story for startup/read-routing ownership
   - Decisions:
     - clarify snapshot timing after `EnqueueCommitted`
     - decide whether scheduler notify is normative or deferred/softened
     - add startup orchestration owner and `max_applied_tx_id` hand-off receiver story if needed
     - pin read-routing documentation home
     - align naming (`committed` vs `store`), `fatal atomic.Bool`, performance section title
     - document Register(view) lifetime

3. Scheduled reducers bundle
   - Rows: §2.7, §2.8, §2.12, §3.1, §3.2, §3.3, §3.4, §3.6, maybe §2.10 if tied to status handling
   - Files: `SPEC-003-executor.md`, `EPICS.md`, `epic-6-scheduled-reducers/story-6.2-transactional-schedule.md`, `story-6.3-timer-wakeup.md`, `story-6.4-firing-semantics.md`, `story-6.5-startup-replay.md`
   - Decisions:
     - make first-fire timing explicit (`now + interval`)
     - define scheduler response-drain channel
     - define missing-row / reducer-not-found / decode-failure firing policy
     - create §12 divergence block and move open questions / verification to §13 / §14 shape matching Sessions 7/8
     - confirm §3.4 closes via existing E7 cross-ref instead of a new divergence entry, or keep a divergence note if still needed

4. Lifecycle + error-catalog bundle
   - Rows: §2.2, §2.9, §2.10, §4.3, §5.3
   - Files: `SPEC-003-executor.md`, `EPICS.md`, `epic-4-reducer-execution/story-4.1-begin-phase.md`, `story-4.4-rollback-and-failure.md`, `epic-7-lifecycle-reducers/EPIC.md`, existing Epic 7 stories or a new startup-sweep story
   - Decisions:
     - add dangling-client sweep after recovery / before first accept, cross-ref Cluster A4 ordering and Session 8 recovery hand-off
     - add `Rollback` to §13.1 and Story 4.4 explicitly
     - classify `ErrReducerNotFound` as `StatusFailedUser`
     - either add missing sentinels to §11 or mark them as deliberate programming-error cases

5. Tracking / drift / verification / commit bundle
   - Update `AUDIT_HANDOFF.md` SPEC-003 statuses and files-to-edit cells
   - Update lane cursor to Session 10 in intro block and footer
   - Add `TECH-DEBT.md` TD-129+ only if a spec claim intentionally stays ahead of live behavior
   - Run at least doc-targeted sanity reads plus `rtk git diff --stat` / `rtk git diff --check`; use `rtk git status` before finalizing
   - Commit content bundles, then tracking-doc refresh commit

Self-review checklist
- Every SPEC-003 row is `closed` or `deferred — reason`
- No live code touched
- No Cluster A–E decisions undone
- SPEC-003 section numbering remains coherent after adding divergences / ownership stories
- Any drift items cite live `file:line`
- `AUDIT_HANDOFF.md` cursor advances to Session 10
