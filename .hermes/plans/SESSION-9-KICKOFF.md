# Lane B Session 9 Kickoff Prompt

> Paste the body below into a fresh session. Everything above this line is index — not part of the prompt.

---

You are picking up Lane B of the Shunter spec-audit reconciliation at Session 9 — SPEC-003 residue cleanup. A prior agent just closed Session 8 (SPEC-002 residue); you are continuing that workstream. `main` is local-only and is now ~34 commits ahead of `origin/main`.

## Required reading (in order)

1. `CLAUDE.md` — project-level rules + pointer to `RTK.md`.
2. `RTK.md` — shell command rules. Every shell/git command must be prefixed with `rtk`. Non-negotiable.
3. `docs/project-brief.md` — product + architectural intent.
4. `AUDIT_HANDOFF.md` — the Lane B tracker. Read in full, but especially:
   - §B.0 Operating rules — not negotiable. Locked option decisions, clean-room boundary, live-code-off-limits, commit style, drift-log rule with TD-125 / TD-126 / TD-127 / TD-128 precedent.
   - §B.1 Cluster close-notes A–E — locked resolutions; do not revisit.
   - §B.2 SPEC-003 table — your work queue (rows with status `open` only; skip `in-cluster` and `closed`).
   - §B.3 Session 9 cadence row + §B.4 Session 9 kickoff template.
5. `SPEC-AUDIT.md` — the underlying finding catalog. Read individual SPEC-003 finding sections as you walk each row (file lines 748–1180 cover SPEC-003: §1 Critical, §2 Gaps, §3 Divergences, §4 Internal consistency, §5 Epic/story coverage, §6 Clean-room boundary, §7 Quick wins, §8 Spec-to-code drift).

Do **NOT** read `reference/SpacetimeDB/` — Lane B is docs-vs-docs reconciliation, not clean-room research.

Do **NOT** touch live code: `store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/`. Those are Sessions 12+. SPEC-003 especially tempts drift: live `executor/` exists with a fully-built scheduled-reducer + post-commit pipeline that may not match the now-repaired spec. If a spec edit's contract outruns live code, follow the §B.0 drift rule: soften the spec to match live OR keep the aspirational contract and log a Session 12+ entry in `TECH-DEBT.md` (precedent: TD-125 / TD-126 / TD-127 landed during Session 4.5; TD-128 landed Session 8 for the FsyncMode placeholder).

## Cursor

SPEC-003 residue. Open rows at session start (source: `AUDIT_HANDOFF.md` §B.2 SPEC-003):

**CRIT**
- §1.2 — Scheduled-reducer firing has no carrier for `schedule_id` / `IntendedFireAt` → Story 1.2, §3.3
- §1.4 — §5 post-commit step order vs Story 5.1 snapshot timing → §5.2, Story 5.1

**GAP**
- §2.2 — Dangling-client cleanup on restart undefined → new Epic 7 story (also see §5.3 overlap)
- §2.4 — Scheduler→executor wakeup ordering inconsistent → §5 / Story 5.1
- §2.5 — Startup orchestration owner unspecified → new Epic 3 story (overlaps Cluster A4 — already closed by SPEC-006 §5.2 eight-step boot ordering; decide whether §2.5 closes via cross-ref to A4 or genuinely needs a new SPEC-003 story)
- §2.7 — No pre-handler scheduled-row validation on firing → Story 6.x
- §2.8 — `Schedule` / `ScheduleRepeat` first-fire timing disagreement → Story 6.x
- §2.9 — `Rollback` not in SPEC-001 contract listed by §13.1 → §13.1
- §2.10 — `ErrReducerNotFound` status classification inconsistent → §11
- §2.11 — Inbox close-vs-shutdown-flag race not described → Story 3.3 / 3.5
- §2.12 — No guidance for scheduler-response dump channel → Story 6.3

**DIVERGE** (all target SPEC-003's divergence block — needs creating; SPEC-001 §12 Session 7 + SPEC-002 §12 Session 8 are precedents. Push Open Questions → §13, Verification → §14; follow the same shape)
- §3.1 — Fixed-rate repeat vs SpacetimeDB explicit-reschedule
- §3.2 — Unbounded reducer dispatch queue vs bounded inbox
- §3.3 — Server-stamped timestamp at dequeue vs supplied-at-call
- §3.4 — Post-commit failure always fatal vs per-step recoverable (audit notes "(E7)" — Cluster E E7 already pinned the dividing line in SPEC-003 §5.4 / SPEC-004 §11.1; verify whether §3.4 still needs a divergence entry or closes via cross-ref to E7)
- §3.6 — Scheduled-row mutation atomic with reducer writes vs pre-fire delete

**NIT**
- §4.2 — `CallerContext.Timestamp` type vs SPEC-005 wire format → Story 1.x
- §4.3 — §11 catalog omits sentinels stories imply → §11
- §4.4 — `Executor` struct names `store` but §13.1 names `CommittedState` → §13.1 / Story 3.1
- §4.5 — `SubscriptionManager.Register` read-view ownership → Story 4.x
- §4.6 — `Executor.fatal` lock scope vs struct declaration → Story 3.1
- §4.8 — Performance section title mirrors SPEC-001 §4.4 → §perf (matches SPEC-001 Session 7 §4.4 NIT — rename to "Performance Targets")
- §4.9 — Story 1.3 `ResponseCh` on every command → Story 1.3

**GAP (§5.x)** — overlap with §2.x; resolve at the same edit sites:
- §5.2 — No story owns `max_applied_tx_id` hand-off from SPEC-002 → new story (SPEC-002 Session 8 closed §6.1 step 6c referencing this hand-off; SPEC-003 needs the receiver-side story)
- §5.3 — No story owns dangling-client sweep on startup → overlaps §2.2
- §5.4 — No story owns read-routing documentation placement → new story
- (§5.5 already closed via Cluster A4 — registration ordering at engine boot)

Target tree: `docs/decomposition/003-executor/**` (SPEC-003 + its stories + EPICS.md).

## Cross-spec coordination (Sessions 7 + 8 closures that may bear on SPEC-003)

Several SPEC-003 rows touch state that prior sessions already pinned. Read what landed before duplicating or contradicting:

- **SPEC-001 Story 8.2** (`docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.2-apply-changeset.md`) — Session 7 added the sequence-advance-on-replay rule (`max(next, observed+1)` in insert branch). Relevant to SPEC-003 §5.2 `max_applied_tx_id` hand-off — recovery's TxID resume is closely tied.
- **SPEC-001 §11** — Session 8 added the bulk-restore surface subsection naming `RegisterTable` / `InsertRow` / `SetNextID` / `SetSequenceValue` as the SPEC-002-consumed restore surface. SPEC-003 startup orchestration (§2.5) sequences these calls.
- **SPEC-002 §5.6 + Story 5.2** — Session 8 closed §2.13 by cross-referencing SPEC-003 for graceful-shutdown ordering ("quiesce executor → flush in-flight commits → final `CreateSnapshot` → `DurabilityHandle.Close`"). SPEC-003 audit §2.5 must own this orchestration; Session 9 either lands a new Epic 3 startup-and-shutdown story or extends an existing Story 3.x.
- **SPEC-002 §6.1 step 6c** — Session 8 added the cross-ref to SPEC-001 Story 8.2 for sequence-advance during recovery. SPEC-003 §5.2 (`max_applied_tx_id` hand-off) consumes the recovery output — the executor's TxID counter must initialize from `OpenAndRecover`'s second return value.
- **Cluster A4** (closed Session 2) — SPEC-006 §5.2 pins the eight-step engine boot ordering: registration → freeze → subsystem construction → recovery → scheduler replay → dangling-client sweep → run. SPEC-003 §2.5 (startup orchestration owner) and §5.5 (registration ordering) overlap directly. Decide whether §2.5 closes by cross-ref or genuinely needs a SPEC-003 story; §5.5 already closed (A4) and is correctly marked.
- **Cluster D2** (closed Session 5) — SPEC-003 §10.3 / §10.4 + Story 7.3 already pin OnConnect/OnDisconnect bespoke commands, including the four contracts (CallSourceLifecycle, one TxID per failed OnDisconnect, cleanup post-commit panics fatal, OnDisconnectCmd not short-circuited when fatal). §2.2 dangling-client cleanup on restart is the recovery-side complement — Session 9 should either add a new Epic 7 Story 7.4 or extend Story 7.3 to cover restart-time `sys_clients` sweep.
- **Cluster E E7** (closed Session 6) — SPEC-003 §5.4 normative rule already pins fatal-vs-recoverable post-commit dividing line. §3.4 DIVERGE may already be substantively closed; verify before adding a new entry to the §12 divergence block.
- **Cluster E E1/E6** (closed Session 6) — SPEC-003 §8 `EvalAndBroadcast` signature is 4-arg `PostCommitMeta` form; §7 `DurabilityHandle` carries `WaitUntilDurable`. SPEC-003 §5 post-commit pipeline step order (§1.4 CRIT) must read against the locked Cluster E shapes — do not undo Session 6.

Check what SPEC-001 + SPEC-002 sessions landed before duplicating: `rtk git log --oneline 5dd79db..7ff4ac8` (SPEC-001 Session 7 + SPEC-002 Session 8 = 24 commits).

## Procedure (per row)

1. **Read the cited SPEC-AUDIT.md section in full** — do not work from just the row summary. SPEC-003 audit lives at `SPEC-AUDIT.md` lines 748–1180.
2. **Decide:** edit to resolve, OR mark `deferred — <one-line reason>` if resolution requires info not yet available (e.g., a downstream cluster dependency), OR log drift if the spec contract would outrun live `executor/` and you choose to soften-match instead.
3. **If editing:** land the smallest correct fix. Match the prose style of Session 7/8 commits — read `rtk git log --oneline 5dd79db..7ff4ac8` then `rtk git show <hash>` on a representative one (e.g., `2d1117f` SPEC-002 §2.6/§5.6 restore-API or `0d193ee` SPEC-002 ownership bundle) for shape.
4. **Flip status in `AUDIT_HANDOFF.md` §B.2** from `open` to `closed` (or `deferred — <reason>`). Update the "Files to edit" cell with the actual landing site.
5. **If you log drift,** add `TD-129` / `TD-130` / … entries to `TECH-DEBT.md` using the TD-125..TD-128 pattern (status / severity / first-found / spec ref / summary / why-this-matters / related code / related docs / recommended resolution). One-line live `file:line` citation in the "Related code" section is non-negotiable.
6. **Commit at logical boundaries.** Style: `docs: Lane B SPEC-003 residue — <row-id or theme>` for mid-session commits; `docs: close Lane B SPEC-003 residue — Session 9` for the final tracking-doc refresh. HEREDOC body. Standard `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` trailer. `rtk git add <explicit paths>` (never `-A`).

Natural commit groupings: one per CRIT row; one per coherent GAP theme (e.g., bundle the three startup/shutdown/registration ownership rows §2.5 + §5.2 + §5.4 if they share an edit site); divergence block as a single commit; NITs bundled by file or by theme. Sessions 7 + 8 each landed 11–12 content commits + 1 tracking commit at this cadence — aim for a similar shape.

## Stop rule

- Every open SPEC-003 row in `AUDIT_HANDOFF.md` §B.2 is now `closed` or `deferred — <reason>`.
- No `open` status remains in the SPEC-003 table.
- Any new TD-* entries in `TECH-DEBT.md` carry the live `file:line` citation.
- Top-of-file cursor line (Lane B intro block) + §B.5 footer `Cursor:` line advanced from Session 9 to Session 10 (SPEC-004/005 residue cleanup — joint session per §B.3).

## Workflow

Use the superpowers skills:
1. `superpowers:writing-plans` — draft a plan at `.hermes/plans/<UTC-timestamp>-lane-b-session-9-spec003-residue-plan.md`. Working-plan convention: this file stays untracked, do not commit it. References:
   - `.hermes/plans/2026-04-17_115110-lane-b-session-7-spec001-residue-plan.md` — Session 7 plan (still on disk).
   - `.hermes/plans/2026-04-17_121057-lane-b-session-8-spec002-residue-plan.md` — Session 8 plan (still on disk).
   Mirror their structure: upfront decisions, per-task files/edits/grep/commit, drift-log section, self-review.
2. `superpowers:executing-plans` or inline execution — land edits commit-by-commit per the procedure above.

Mark tasks via `TaskCreate` / `TaskUpdate` as you work so progress is visible.

Begin by reading the required-reading files, then draft and save the plan, then execute.
