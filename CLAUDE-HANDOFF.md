# Claude Handoff

Short operational note for the next agent. Read `CLAUDE.md` + `RTK.md` first; this file just says where we stopped.

## Repo state

- Tree clean; `main` ahead of `origin/main` by 12 commits (local only).
- Latest commits (newest → older):
  - `22541f9` docs: close Lane B Cluster D — lifecycle commands + init deferral
  - `bce95dc` docs updateds (user-made; bundles the Lane A Session 4 post-work that was sitting dirty)
  - `78a86cd` docs: Lane B Session 4.5 — reconcile Cluster C spec claims with live code
  - `2a07654` docs: close Lane B C2 — per-column trailer + ErrNullableColumn
  - `f36216d` docs: close Lane B C1 — BSATN naming disclaimer + cross-refs
- No untracked work aside from `.hermes/plans/2026-04-16_190420-spec004-e6-remainder-plan.md` (Lane A SPEC-004 E6 plan, not yet executed).

## Two lanes coexist

`AUDIT_HANDOFF.md` is the authoritative tracker. Pick the lane that matches your kickoff prompt; don't interleave.

- **Lane A — per-slice code-vs-spec audit** (feeds `TECH-DEBT.md`). Cursor: `SPEC-004 E6 remainder: Fan-Out & Delivery`. Plan already drafted at `.hermes/plans/2026-04-16_190420-spec004-e6-remainder-plan.md`. Touches live code.
- **Lane B — spec-audit reconciliation** (edits `docs/decomposition/**`). Cursor: **Session 6 — Cluster E — post-commit fan-out shapes**. Docs-only.

## Recommended next move: Lane B Session 6 (Cluster E)

Cluster E is seven coordinated shape decisions across SPEC-002/003/004/005. Audit refs:

- **E1** `PostCommitMeta` shape — SPEC-003 §1.3, SPEC-004 §1.1/§2.3/§2.12/§3.5.
- **E2** `FanOutMessage` shape — SPEC-004 §1.3/§2.3, SPEC-005 §1.2.
- **E3** `SubscriptionError` shape + delivery — SPEC-004 §2.4, SPEC-005 §1.1/§2.4/§5.2.
- **E4** `ReducerCallResult` — SPEC-004 §2.5, SPEC-005 §3.9 (status enum DIVERGE), SPEC-005 §2.2 (closed via B3 already; re-verify in this session).
- **E5** `ClientSender`/`FanOutSender` naming + `Send(connID, any)` — SPEC-004 §2.6, SPEC-005 §1.1/§1.5. Live: `protocol/sender.go:30`, `protocol/fanout_adapter.go:16-47`.
- **E6** `DurabilityHandle` contract + `WaitUntilDurable` — SPEC-002 §2.9, SPEC-003 §1.3. Live: `executor/interfaces.go:21`, `commitlog/durability.go:181`.
- **E7** Per-sub eval-error vs SPEC-003 §5.4 fatal post-commit — SPEC-004 §1.4 contradicts SPEC-003 §5.4 / §3.4.

Kickoff template: `AUDIT_HANDOFF.md` §B.4 "Session 6". Stop rule: all five type shapes declared in one spec each with cross-refs from consumers; eval-error recovery model resolved (E7); `WaitUntilDurable` either added to SPEC-002 §4.2 or removed from impl with a deferred-debt note.

## Ground rules (from this session pass)

- **Shell:** prefix every command with `rtk` per `RTK.md`.
- **Clean-room:** do not open `reference/SpacetimeDB/` unless the session explicitly allows it.
- **Live code is off-limits in Lane B.** `store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/` only change in Sessions 11+ drift batches. If a Lane B edit outruns live code: soften the spec OR log a Session 12+ drift item in `TECH-DEBT.md`. Recent precedent: Session 4.5 repair pass landed `TD-125` / `TD-126` / `TD-127` for three such cases.
- **Commits:** commit at logical boundaries without re-asking; message style matches the existing `docs: close Lane B <cluster> — <summary>` pattern; HEREDOC body; standard `Co-Authored-By: Claude Opus 4.7 (1M context)` trailer.
- **Lane discipline:** if the tree has unrelated dirty state at session start, leave it alone inside the session; commit it as a separate follow-up commit right after your session lands (don't leave it sitting across multiple sessions).

## Option decisions already locked (don't revisit)

These came from earlier clusters; Session 6 should assume them.

- **Commit/TxID Model A** (Cluster B): executor owns the monotonic counter; `store.Commit` returns `(*Changeset, error)`; executor stamps `changeset.TxID` before post-commit.
- **Per-column trailer Option A** (Cluster C): 3 bytes `(type_tag, nullable, auto_increment)` — matches live `commitlog/snapshot_io.go:87`.
- **Lifecycle commands Option A** (Cluster D): bespoke `OnConnectCmd` / `OnDisconnectCmd` alongside `CallReducerCmd`; `CallSourceLifecycle` reused for the OnDisconnect cleanup tx; `OnDisconnectCmd` still runs when `e.fatal == true`.
- **`init` / `update`** (Cluster D): deferred to v2; names NOT reserved in v1; applications use deployment-time reducer calls.
- **Canonical Go-package homes** (Cluster B): `types/` owns `RowID` / `Identity` / `ConnectionID` / `TxID` / `ColID` / `SubscriptionID` / `ScheduleID` and `ReducerHandler` / `ReducerContext` / `CallerContext` / `ReducerDB` / `ReducerScheduler`.

## After Session 6

`AUDIT_HANDOFF.md` §B.3 cadence table shows the rest: Sessions 7–11 walk per-spec residue (one spec per session); Sessions 12+ are live-code drift batches (where TD-125/126/127 will eventually land code changes). Don't run those out of order.
