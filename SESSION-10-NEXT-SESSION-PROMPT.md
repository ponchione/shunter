You are resuming Lane B of the Shunter spec-audit reconciliation at Session 10 — SPEC-004 + SPEC-005 residue cleanup. Continue from the current docs-only worktree state. Do not restart from scratch; pick up after Session 9 closed SPEC-003 and advance the next residue pass.

Required reading, in order
1. `CLAUDE.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `AUDIT_HANDOFF.md`
   - Lane B intro cursor
   - §B.1 closed clusters A–E for locked cross-spec decisions
   - §B.2 SPEC-004 table
   - §B.2 SPEC-005 table
   - §B.4 Session 10 kickoff note
   - §B.5 tracking-update rules
6. `SPEC-AUDIT.md`
   - SPEC-004 open findings
   - SPEC-005 open findings
7. Current edited files under:
   - `docs/decomposition/004-subscriptions/**`
   - `docs/decomposition/005-protocol/**`

Non-negotiable rules
- Lane B only. Do not touch Lane A except to update the Lane B cursor/status text inside `AUDIT_HANDOFF.md` when Session 10 is actually complete.
- Docs only. Do NOT touch implementation code.
- Every shell/git command must be prefixed with `rtk`.
- Do not read `reference/SpacetimeDB/`.
- Respect locked Cluster A–E resolutions already recorded in `AUDIT_HANDOFF.md`.
- Keep edits tight and implementation-facing.

What Session 9 just finished
- Commit landed: `c37a2da docs: close Lane B SPEC-003 residue — Session 9`
- `AUDIT_HANDOFF.md` was updated so every SPEC-003 row is now closed.
- Lane B cursor was advanced to Session 10.
- `SESSION-9-NEXT-SESSION-PROMPT.md` was deleted after completion.
- New docs-only owner stories were added:
  - `docs/decomposition/003-executor/epic-3-executor-core/story-3.6-startup-orchestration.md`
  - `docs/decomposition/003-executor/epic-7-lifecycle-reducers/story-7.5-startup-dangling-client-sweep.md`
- SPEC-003 landed the following contracts/cleanup:
  - scheduled-call carrier fields (`ScheduleID`, `IntendedFireAt`)
  - startup sequencing and dangling-client sweep ownership
  - rollback / read-view lifetime / nil ResponseCh / submit-vs-shutdown race wording
  - divergence block in SPEC-003 and `Performance Targets` moved to §17

Current repo state to assume
- Branch was `main` and ahead of origin by 35 commits immediately after Session 9 commit.
- Worktree was clean except for untracked `.hermes/plans/` scratch files.
- Do not delete those plan files unless explicitly asked.

Your job now
1. Inspect repo state and confirm Session 9 landed cleanly.
2. Walk every `open` row for SPEC-004 and SPEC-005 in `AUDIT_HANDOFF.md` §B.2.
3. For each row:
   - read the cited detail in `SPEC-AUDIT.md`
   - patch the owning spec/story/epic docs in `docs/decomposition/004-subscriptions/**` and/or `docs/decomposition/005-protocol/**`
   - decide whether the row should be `closed` or `deferred — <reason>`
4. Coordinate the cross-spec pairs carefully instead of fixing only one side when the row clearly spans both specs.
5. Update `AUDIT_HANDOFF.md`:
   - flip all resolved Session 10 rows from `open` to `closed`
   - if something truly cannot be closed in docs now, mark it `deferred — <reason>`
   - replace placeholder “Files to edit” cells with actual landing sites where useful
   - advance the Lane B intro cursor and footer cursor from Session 10 to Session 11
6. Re-run doc sanity checks and inspect the final diff.
7. Commit at logical boundaries with the required Lane B commit style.

Priority targets for Session 10
Start with the highest-value cross-spec residue, not random nits. A good order is:
1. SPEC-004 critical rows:
   - §1.2 client identity on subscription registration
   - §1.5 join-safe `SubscriptionUpdate.TableID`
   - §1.6 multi-subscription-per-connection manager shape
2. SPEC-005 critical/gap rows that interact with those surfaces:
   - §1.4 outbound close vs concurrent send race
   - §1.6 error catalog completeness if still not fully covered post-Cluster E
   - §2.1 wire-format cross-ref for `SubscriptionUpdate.TableID`
   - §2.3 confirmed-read wire representation
3. Then sweep the open GAP/NIT rows in dependency order:
   - read-view lifetime / dropped-clients semantics / caller-result-on-empty-fanout / Manager↔FanOutWorker wiring
   - protocol timeout/compression/query-param semantics and related story ownership
   - divergence blocks and benchmark/perf-title cleanup

Specific things to check before editing tracking rows
- Whether SPEC-004 §2.1 (`CommittedReadView` lifetime) is already partially implied by the SPEC-003 Session 9 register-view ownership fix and now only needs the subscription-side half pinned.
- Whether SPEC-004 §2.10 and §5.4 can be closed together through one explicit caller-result-on-empty-fanout owner story.
- Whether SPEC-004 §5.2 / §5.3 need new stories versus widening an existing manager/fanout story.
- Whether SPEC-005 §1.6 should close fully via explicit §14 additions or remain partially deferred.
- Whether SPEC-005 §2.1 (`SubscriptionUpdate.TableID`) is just a wire cross-ref/naming closure once SPEC-004 decides the authoritative join semantics.
- Whether any rows are really duplicates of already locked Cluster E decisions and should be closed by cross-reference rather than new text.

Verification commands to run
- `rtk git diff --check`
- `rtk git diff --stat`
- `rtk git diff`
- `rtk git status --short`

Commit guidance
- Use explicit `rtk git add <paths>`
- Keep commits small and reviewable
- Message style:
  - mid-session: `docs: Lane B SPEC-004/005 residue — <theme>`
  - final tracking refresh: `docs: close Lane B SPEC-004/005 residue — Session 10`
- HEREDOC body
- include:
  `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`

Stop rule
- No `open` statuses remain in the SPEC-004 and SPEC-005 tables in `AUDIT_HANDOFF.md`
- Every Session 10 row is `closed` or `deferred — <reason>`
- Cursor advanced to Session 11
- Any new drift/debt notes cite concrete file:line evidence
- Worktree is committed cleanly except for untracked `.hermes/plans/` files
