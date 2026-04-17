# Claude Handoff

Short operational note for the next agent. Read `CLAUDE.md` + `RTK.md` first; this file just says where we stopped.

## Repo state

- Tree clean; `main` ahead of `origin/main` by 19 commits (local only).
- Latest commits (newest → older):
  - `<new>` docs: close Lane B Cluster E — Session 6 tracking update
  - `e76ccfe` docs: Lane B Cluster E — E7 per-query eval-error vs fatal panic
  - `f7554c1` docs: Lane B Cluster E — E4 ReducerCallResult authority + status DIVERGE
  - `b8a8588` docs: Lane B Cluster E — E3 SubscriptionError shape + delivery
  - `ca9332d` docs: Lane B Cluster E — E2 FanOutMessage xref + E5 ClientSender.Send
  - `8bf7bf3` docs: Lane B Cluster E — E1 EvalAndBroadcast signature + TxDurable contract
  - `7917829` docs: Lane B Cluster E — E6 DurabilityHandle.WaitUntilDurable
  - `9362e01` docs: add CLAUDE-HANDOFF.md for next-agent pickup
  - `22541f9` docs: close Lane B Cluster D — lifecycle commands + init deferral
- Untracked working plans (not checked in by convention):
  - `.hermes/plans/2026-04-16_190420-spec004-e6-remainder-plan.md` (Lane A SPEC-004 E6, not yet executed)
  - `.hermes/plans/2026-04-17_072922-lane-b-session-6-cluster-e-plan.md` (Lane B Session 6, now executed)

## Two lanes coexist

`AUDIT_HANDOFF.md` is the authoritative tracker. Pick the lane that matches your kickoff prompt; don't interleave.

- **Lane A — per-slice code-vs-spec audit** (feeds `TECH-DEBT.md`). Cursor: `SPEC-004 E6 remainder: Fan-Out & Delivery`. Plan drafted at `.hermes/plans/2026-04-16_190420-spec004-e6-remainder-plan.md`. Touches live code.
- **Lane B — spec-audit reconciliation** (edits `docs/decomposition/**`). Cursor: **Session 7 — SPEC-001 residue cleanup**. Docs-only.

## Recommended next move: Lane B Session 7 (SPEC-001 residue)

`AUDIT_HANDOFF.md` §B.2 SPEC-001 table — walk every row still marked `open`. For each, read the cited audit section in `SPEC-AUDIT.md`, edit the file(s) listed in the row's `Files to edit` column in `docs/decomposition/001-store/**`, flip status to `closed`. Skip `in-cluster` / already-`closed` rows.

Open SPEC-001 rows at session start:

- `§1.1` CRIT — Value equality / hash invariant broken for float ±0 (Story 1.1)
- `§1.2` CRIT — `CommittedReadView.IndexRange` lacks Bound semantics in BTreeIndex (Stories 3.3/5.3/7.1, §7.2)
- `§1.4` CRIT — Undelete-match rule contradicts §5.5 vs Story 5.4 (Story 5.4, §5.5, §6.2)
- `§1.5` CRIT — `AsBytes` return contract undefined; can break immutability (Story 1.1)
- `§2.1` GAP — Sequence recovery: replay does not advance `Sequence.next` (Story 8.2; also SPEC-002 Story 6.4)
- `§2.2` GAP — `ErrTableNotFound` no production site
- `§2.4` GAP — `ErrInvalidFloat` no production site (Story 1.1)
- `§2.5` GAP — Snapshot close state not enforced (Story 7.x snapshot lifecycle)
- `§2.6` GAP — `StateView.SeekIndexRange` may be insufficient for SPEC-004 predicates (§7.2, Story 7.1)
- `§2.7` GAP — `ApplyChangeset` idempotency / partial-replay undefined (§6.x, replay story)
- `§2.8` GAP — Row-shape validation error name unreferenced in §9
- `§2.9` GAP — Write-lock vs read-lock scope restated inconsistently (§6.2 / §7.x)
- `§3.1–§3.6` DIVERGE — NaN handling, no composites, PK variations, Changeset flags, etc. (divergence block)
- `§4.3` NIT — ColID exists but schema uses raw int (schema sections)
- `§4.4` NIT — Performance section title vs open-question framing
- `§4.5` NIT — Story 1.1 zero-initialized Value status
- `§4.7` NIT — Primary IndexID=0 rule ambiguous for no-PK tables
- `§4.8` NIT — Epic 7 blocks "Nothing" but other specs consume it (EPICS.md)
- `§4.9` NIT — §11 executor contract restates `(cs).Snapshot()` outside Epic-7
- `§5.2` GAP — §6.3 consumers receive same Changeset — no concurrency contract
- `§5.3` GAP — No story covers `Bytes` copy at Insert boundary (Story 5.4)
- `§5.4` GAP — Story 8.3 `SetNextID` / `SetSequenceValue` semantics asymmetric

Kickoff template: `AUDIT_HANDOFF.md` §B.4 "Session 7". Stop rule: all open SPEC-001 rows either `closed` or marked `deferred` with one-line reason.

## Ground rules (from this session pass)

- **Shell:** prefix every command with `rtk` per `RTK.md`.
- **Clean-room:** do not open `reference/SpacetimeDB/` unless the session explicitly allows it.
- **Live code is off-limits in Lane B.** `store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/` only change in Sessions 11+ drift batches. If a Lane B edit outruns live code: soften the spec OR log a Session 12+ drift item in `TECH-DEBT.md`. Session 6 did not need a new drift entry — live already had `WaitUntilDurable` and `ClientSender.Send`; spec was catching up.
- **Commits:** commit at logical boundaries without re-asking; message style matches the existing `docs: <Lane B cluster> — <summary>` pattern; HEREDOC body; standard `Co-Authored-By: Claude Opus 4.7 (1M context)` trailer.
- **Lane discipline:** if the tree has unrelated dirty state at session start, leave it alone inside the session; commit it as a separate follow-up commit right after your session lands.

## Option decisions already locked (don't revisit)

These came from earlier clusters; Session 7+ should assume them.

- **Commit/TxID Model A** (Cluster B): executor owns the monotonic counter; `store.Commit` returns `(*Changeset, error)`; executor stamps `changeset.TxID` before post-commit.
- **Per-column trailer Option A** (Cluster C): 3 bytes `(type_tag, nullable, auto_increment)` — matches live `commitlog/snapshot_io.go:87`.
- **Lifecycle commands Option A** (Cluster D): bespoke `OnConnectCmd` / `OnDisconnectCmd` alongside `CallReducerCmd`; `CallSourceLifecycle` reused for the OnDisconnect cleanup tx; `OnDisconnectCmd` still runs when `e.fatal == true`.
- **`init` / `update`** (Cluster D): deferred to v2; names NOT reserved in v1; applications use deployment-time reducer calls.
- **Canonical Go-package homes** (Cluster B): `types/` owns `RowID` / `Identity` / `ConnectionID` / `TxID` / `ColID` / `SubscriptionID` / `ScheduleID` and `ReducerHandler` / `ReducerContext` / `CallerContext` / `ReducerDB` / `ReducerScheduler`.
- **Five fan-out shapes canonical homes** (Cluster E — this session):
  - `PostCommitMeta` → SPEC-004 §10.1
  - `FanOutMessage` → SPEC-004 §8.1
  - `SubscriptionError` → SPEC-004 §10.2 (Go) / SPEC-005 §8.4 (wire)
  - `ReducerCallResult` → SPEC-004 §10.2 (Go forward-decl) / SPEC-005 §8.7 (wire)
  - `FanOutSender` → SPEC-004 §8.1 / `ClientSender` → SPEC-005 §13 (with `Send(connID, any)`); adapter pattern pinned
- **`DurabilityHandle` four-method shape** (Cluster E): `EnqueueCommitted` / `DurableTxID` / `WaitUntilDurable` / `Close` in SPEC-002 §4.2 and SPEC-003 §7.
- **Post-commit fatal vs recoverable** (Cluster E): fatal = subsystem panic/invariant violation; recoverable = per-query eval error caught by the manager (SPEC-004 §11.1) and surfaced via `SubscriptionError` with wire `request_id = 0`.

## After Session 7

`AUDIT_HANDOFF.md` §B.3 cadence table shows the rest: Sessions 8–11 walk per-spec residue (one spec per session — SPEC-002 / SPEC-003 / SPEC-004+SPEC-005 / SPEC-006); Sessions 12+ are live-code drift batches (where TD-125/126/127 will eventually land code changes). Don't run those out of order.
