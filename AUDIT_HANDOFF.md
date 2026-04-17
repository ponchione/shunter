# Audit Handoff

> **Two lanes coexist in this file.**
> **Lane A (below)** — original per-slice code-vs-spec audit feeding `TECH-DEBT.md`. Slice cursor: `SPEC-004 E6 remainder`.
> **Lane B (bottom of file, "## Spec-Audit Reconciliation Lane")** — multi-session reconciliation of `SPEC-AUDIT.md` findings into spec/story edits. Cursor: Session 7 (SPEC-001 residue cleanup).
> Future sessions pick the lane that matches the kickoff prompt; do not interleave.

## Lane A — Per-Slice Code-vs-Spec Audit (TECH-DEBT.md feed)

Objective
- Continue the code-vs-spec audit from `docs/EXECUTION-ORDER.md`.
- Keep appending grounded findings to root `TECH-DEBT.md`.
- The audit trail is now advanced through `SPEC-004 E5`.
- `REMAINING.md` currently says all tracked implementation slices are complete; keep this lane audit-only unless a tiny doc correction is required.
- Latest newly logged audit findings are now `TD-123` and `TD-124` from the `SPEC-004 E5` pass.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `REMAINING.md`
6. `TECH-DEBT.md` (phase plan + open items near top)
7. the specific decomposition docs for the next slice

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
- `SPEC-002 E6`
- `SPEC-002 E7`
- `SPEC-004 E1`
- `SPEC-004 E2`
- `SPEC-004 E3`
- `SPEC-004 E4`
- `SPEC-004 E6.1-enabling contract slice`
- `SPEC-004 E5`

Next execution-order slice
- `SPEC-004 E6 remainder: Fan-Out & Delivery`

Recommended next reading
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md`
- `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/EPIC.md`
- all remaining Epic 6 story docs after 6.1
- live files likely to matter:
  - `subscription/fanout_worker.go`
  - `subscription/fanout_worker_test.go`
  - `subscription/fanout.go`
  - `subscription/eval.go`
  - any delivery/error/backpressure-related tests under `subscription/*_test.go`

Newest findings added this pass
- `TD-123`: Story 5.3 memoized encoding is still placeholder-only; no real encode-once reuse path exists
- `TD-124`: Story 5.2 still documents a standalone `CollectCandidates(...)` helper the package does not expose

Important open themes to keep in mind
- Passing tests mean operational health only, not spec completeness.
- Public API drift and recovery-edge-case drift have both been common; compile-only repros and tiny runtime repros are effective when the docs promise a sharper boundary.
- Some decomposition filenames differ from shorthand expectations; use `search_files` before assuming a path.
- Prefer tool-driven cleanup for temporary audit packages/files (`execute_code` / file tools) instead of shell `rm`.

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
- `rtk go test ./subscription`
- `rtk go test ./commitlog`
- `rtk go test ./...`
- earlier focused passes recorded in `TECH-DEBT.md`

Expected deliverable for next agent
- Audit the remaining `SPEC-004 E6` fan-out/delivery slice
- append any new grounded debt items to `TECH-DEBT.md`
- update the phase plan/note block
- report the next slice after that

---

## Lane B — Spec-Audit Reconciliation

Objective
- Walk `SPEC-AUDIT.md` (~2564 lines, six top-level specs) and convert findings into spec/story edits across multiple ≤150k-token sessions.
- This lane edits `docs/decomposition/**`, not `TECH-DEBT.md`. Live `store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/` only touched in dedicated drift sessions (Session 11+).
- `SPEC-AUDIT.md` is source of truth; this section is the index. Cite finding IDs (e.g. `SPEC-006 §1.1`) so the audit can be re-read for full context.

### B.0 Operating rules

Every Lane B session MUST honor these. They are not negotiable mid-session.

- **Shell:** prefix every shell/git command with `rtk` per `RTK.md`. (Lane A §"Shell rule" applies to the whole file.)
- **Clean-room:** do not open `reference/SpacetimeDB/` unless the session kickoff prompt explicitly allows it. Lane B is spec-vs-spec reconciliation, not clean-room research.
- **Live code is off-limits.** `store/`, `commitlog/`, `executor/`, `subscription/`, `protocol/`, `schema/`, `types/`, `bsatn/` only change in Sessions 12+ drift batches. If a Lane B edit's spec contract outruns live code, pick one:
  - **Soften the spec** to match live, OR
  - **Leave the aspirational contract in the spec and log a Session 12+ drift item in `TECH-DEBT.md`.**

  Precedent: Session 4.5 repair pass landed `TD-125` / `TD-126` / `TD-127` when three Cluster C spec claims had outrun `commitlog/` behavior. Session 6 required no new drift entries — live led the spec.
- **Pick one lane per session.** If you are kicked off for Lane B, do not touch Lane A artifacts (`TECH-DEBT.md` items, `REMAINING.md`, live-code audits), and vice versa.
- **Commits:**
  - Commit at logical boundaries without re-asking.
  - One commit per resolved finding / closed cluster / tracking-doc refresh — small, reviewable bundles; do not stack a whole session into one commit.
  - Message style: `docs: close Lane B <cluster> — <summary>` for cluster closes; `docs: Lane B <cluster> — <finding summary>` for mid-cluster landings. HEREDOC body. Standard `Co-Authored-By: Claude Opus 4.7 (1M context)` trailer.
  - Prefer `rtk git add <explicit paths>` over `rtk git add -A`.
- **Dirty-state discipline:** if the tree has unrelated dirty state at session start, leave it alone inside the session; commit it as a separate follow-up commit right after your session lands. Never leave unrelated dirt across multiple sessions.
- **Working-plans convention:** session plans live under `.hermes/plans/<UTC-timestamp>-<name>.md` and are deliberately untracked. Do not commit them. The `.hermes/plans/` directory is intentionally outside git; use it as a scratch pad for plan-writing-skill output.
- **Option decisions are locked.** Prior-session resolutions (Clusters A–E) are recorded inline with each cluster's "Resolved:" note in §B.1. Do not revisit them in later sessions unless the audit surfaces a genuinely new contradiction — in which case open a new cluster letter per §B.5 procedure.

### B.1 Bleed-item clusters

Bleed-items are findings that span ≥2 specs and share one fix. Resolve clusters first; per-spec residue afterward.

#### Cluster A — Schema-contract surfaces (closed Session 2)
Single SPEC-006 §7 edit unblocks four downstream consumers.

- **A1 `SchemaLookup` interface** — SPEC-006 §1.1, SPEC-005 §4.2 callout, SPEC-004 §2.14, SPEC-005 §4.2 (front-matter dep). Three live homes (`subscription/validate.go:9`, `protocol/handle_subscribe.go:16`, `protocol/upgrade.go:46`) need consolidation. Pick `TableByName` 3-tuple form per SPEC-005 Story 4.2. **Resolved:** SPEC-006 §7 declares `SchemaLookup` as the union narrow read-only surface (Table / TableByName 3-tuple / TableExists / TableName / ColumnExists / ColumnType / HasIndex). `SchemaRegistry` embeds it. Consumer-side narrow declarations in `subscription/`, `protocol/` are now documentation, not new types.
- **A2 `IndexResolver` interface** — SPEC-006 §1.2, SPEC-004 §2.7. Single live home (`subscription/placement.go:27`); declare in SPEC-006 §7 as `SchemaRegistry` capability. **Resolved:** SPEC-006 §7 declares `IndexResolver`; `SchemaRegistry` embeds it. SPEC-004 §10.4 cross-refs.
- **A3 `SchemaRegistry.Version()` semantics** — SPEC-006 §1.5, SPEC-002 §2.7. Pin meaning. **Resolved:** SPEC-006 §6.1 pins Version() as application-supplied uint32, opaque to engine, never derived/mutated. Snapshot-header authoritative on header-vs-body disagreement; full dual-storage collapse deferred to Session 8 (SPEC-002 §4.1). SPEC-002 §6.1 step 4b updated with explicit cross-ref.
- **A4 Freeze / registration-order lifecycle** — SPEC-006 §1.4, SPEC-003 §5.5, SPEC-006 §5.3 (epic gap). **Resolved:** SPEC-006 §5.1 names Build()=freeze, lists post-freeze rejection rule + ErrAlreadyBuilt sentinel. §5.2 spells the eight-step engine boot ordering (registration → freeze → subsystem construction → recovery → scheduler replay → dangling-client sweep → run). Story 5.3 algorithm updated with explicit freeze step 1 and step 10; acceptance criteria extended. SPEC-003 §13.5 cross-refs.

Edits landed in: `docs/decomposition/006-schema/SPEC-006-schema.md` §5/§5.1/§5.2/§6.1/§7; `docs/decomposition/006-schema/epic-5-validation-build/story-5.3-build-orchestration.md`; cross-refs in `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §6.1, `docs/decomposition/003-executor/SPEC-003-executor.md` §13.5, `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §10.4 (added), `docs/decomposition/005-protocol/SPEC-005-protocol.md` §13 (added SPEC-006 subsection).

#### Cluster B — Error-sentinel ownership + canonical types (closed Session 3)
Untangle error-home and type-home bleeds.

- **B1 `ErrReducerArgsDecode` / typed-adapter sentinel** — SPEC-006 §1.3, SPEC-003 §2.3. **Resolved:** SPEC-006 §4.3 reserves the name for a future typed-adapter layer and states v1 ships no such adapter. SPEC-003 §3.1 + §11 cross-ref the deferral; any non-nil handler error classifies as `StatusFailedUser` via the generic path.
- **B2 `ErrColumnNotFound` three-home** — SPEC-006 §2.6, SPEC-001 §2.3. Two live homes (`store/errors.go:12`, `subscription/errors.go:16`); SPEC-006 §13 owns. **Resolved:** SPEC-006 §13 declares the canonical sentinel; SPEC-001 §9 / Story 2.4 re-export with cross-refs; SPEC-001 EPICS + SPEC-004 EPICS point back to SPEC-006.
- **B3 `TxID` ownership + Commit signature** — SPEC-001 §1.3 (CRIT), SPEC-001 §4.1 (front matter), SPEC-001 §4.2 (returned twice), SPEC-002 §1.2 (TxID stamping), SPEC-002 §2.5 (front matter), SPEC-002 §4.2 (`uint64` leak), SPEC-003 §1.1 (Commit sig 3-way contradiction), SPEC-003 §4.1 (front matter mis-declares SPEC-002 dep), SPEC-005 §2.2 (TxID=0 sentinel). **Resolved (Model A):** executor owns the monotonic counter; `store.Commit` returns `(*Changeset, error)`; executor stamps `changeset.TxID` before the post-commit pipeline. SPEC-001 §5.6/§6.1/§11, Stories 6.1/6.2; SPEC-002 §3.3 + §6.1 step 6b + Story 6.3 (stamp on decode); SPEC-003 §4.4/§6/§13.1 + Story 4.3; SPEC-005 §8.7 cross-ref to SPEC-002 §3.5 `TxID(0)` reservation. Live drift flagged: `commitlog/durability.go:135` `EnqueueCommitted(txID uint64, …)` should take `types.TxID` — deferred to later drift session.
- **B4 Canonical types-package home** — SPEC-005 §1.3 (Identity re-declared), SPEC-006 §8 drift (`ReducerHandler`/`ReducerContext` re-exported via `schema/types.go`). **Resolved:** `types/` is the canonical Go-package home for `RowID`/`Identity`/`ConnectionID`/`TxID`/`ColID`/`SubscriptionID`/`ScheduleID` (SPEC-001 §1.1, §2.4) and `ReducerHandler`/`ReducerContext`/`CallerContext`/`ReducerDB`/`ReducerScheduler` (SPEC-003 §3.1). `schema/` re-exports for builder ergonomics (SPEC-006 §1.1). SPEC-005 §15 OQ#4 closed; Story 2.1 retitled; Story 3.1 imports `ConnectionID` from `types/`.
- **B5 Front-matter dependency completeness** — SPEC-001 §4.1, SPEC-002 §2.5, SPEC-003 §4.1, SPEC-004 §2.14, SPEC-005 §4.2, SPEC-006 §2.13. **Resolved:** each spec's `Depends on:` / `Depended on by:` lines now list every spec referenced in its body or stories. SPEC-004 gained the missing front-matter block outright.

#### Cluster C — BSATN naming + per-column trailer (closed Session 4)
SPEC-002 encoding edits drag SPEC-005/006 along.

- **C1 BSATN naming disclaimer** — SPEC-002 §3.1, SPEC-002 §6.1, SPEC-003 §6 (clean-room note), SPEC-004 §6 (caveat), SPEC-005 §4.1, SPEC-005 §6.1, SPEC-006 §2.9. **Resolved:** canonical disclaimer paragraph landed in SPEC-002 §3.1 as `BSATN naming disclaimer (canonical)`; §3.3 Canonical reference callout notes the name is non-standard with a back-reference. Cross-refs added at SPEC-003 §3.1 (under the `argBSATN` reducer signature), SPEC-004 §6 (row-payload note at the head of Delta Computation), SPEC-005 §3.1 (naming callout beneath the existing BSATN section), and SPEC-006 §1.2 (new "Wire encoding terminology" subsection). SPEC-003/004 have no dedicated clean-room sidebar at §6; the cross-refs land at the most natural encoding site in each.
- **C2 `Nullable` / `AutoIncrement` per-column trailer** — SPEC-002 §2.3, SPEC-001 §4.6 (Nullable decorative), SPEC-006 §2.1 (ColumnSchema inconsistency), SPEC-006 §2.2 (Nullable v1 policy), SPEC-006 §8 drift (`schema/types.go:47` AutoIncrement). **Resolved (Option A — match live 3-byte trailer):** SPEC-002 §5.3 and Story 5.1 (schema snapshot codec) pin the per-column trailer at `(type_tag, nullable, auto_increment)`, all three bytes, matching `commitlog/snapshot_io.go:87`. SPEC-002 §6.1 step 4b and Story 6.2 (snapshot selection) add `Nullable` + `AutoIncrement` to the schema-equality check and reject snapshots with `nullable = 1`. SPEC-006 §8 `ColumnSchema` grows `AutoIncrement bool`; §9 column-level validation pins the v1 Nullable rule with `ErrNullableColumn`; §13 adds the `ErrNullableColumn` sentinel; Story 5.1 (validation rules) adds the acceptance. SPEC-001 §3.1 `ColumnSchema` aligns with SPEC-006 (five fields; cross-ref for canonical); Story 2.1 ColumnSchema block + acceptance updated. Option B (strip trailer / external SequenceSchema) explicitly not chosen — would require tearing out shipped schema format. **Session 4.5 repair pass:** three hallucinated claims (H1 `ErrNullableColumn` enforcement in `Build()`, H2 sentinel-wrapping on recovery, H3 direct `nullable = 1` rejection at snapshot select) softened to match live code; aspirational behavior logged as Session 12+ drift entries `TD-125` / `TD-126` / `TD-127`.

#### Cluster D — Lifecycle reducer / OnConnect / OnDisconnect / init (closed Session 5)
Cross-spec lifecycle model had three+ open seams.

- **D1 `init` lifecycle** — SPEC-003 §2.1, SPEC-003 §3.5, SPEC-006 §2.4. Adopt or formally defer. **Resolved (defer):** SPEC-006 §9 Reducer-level rules now state v1 has no `init`/`update` lifecycle reducer — applications use a normal reducer invoked from deployment tooling; `init`/`update` names are NOT reserved in v1; reintroduction is a v2 target. SPEC-003 §10 preamble names the v1 lifecycle set as `OnConnect`/`OnDisconnect` only and cross-refs SPEC-006 §9.
- **D2 OnConnect/OnDisconnect command identity** — SPEC-003 §1.5 (OnDisconnect tx unbounded), SPEC-003 §2.6 (single-command model conflict), SPEC-005 §4.7 (described as reducers vs §2.4 model). Decide: bespoke commands vs reducer-shaped commands; coordinate `OnConnectCmd`/`OnDisconnectCmd` (live `executor/command.go:61-79`) into spec. **Resolved (Option A — spec matches live bespoke commands):** SPEC-003 §2.4 now declares `OnConnectCmd` / `OnDisconnectCmd` as executor commands separate from `CallReducerCmd`; the trailing sentence is split (scheduled reducers keep using `CallReducerCmd` with `CallSourceScheduled`; lifecycle reducers use their own command types). SPEC-003 §10 preamble explains why (`sys_clients` insert / guaranteed cleanup tx are not expressible through `CallReducerCmd`). SPEC-003 §10.3 / §10.4 rewritten; §10.4 now pins the four contracts from SPEC-AUDIT SPEC-003 §1.5: (1) CallSource for cleanup = `CallSourceLifecycle` (reuse, not a new `CallSourceSystem`); (2) rolled-back reducer tx allocates no TxID, cleanup commit allocates exactly one — one TxID per failed OnDisconnect; (3) cleanup post-commit panics fall under §5.4 (fatal); (4) `OnDisconnectCmd` is NOT short-circuited when `e.fatal == true` — cleanup still attempts because leaking `sys_clients` rows is worse than rejecting writes; `CallReducerCmd` remains rejected in the same state. Story 7.3 acceptance criteria extended with the four pinned items. SPEC-005 §5.2/§5.3 rewritten to dispatch via `OnConnectCmd`/`OnDisconnectCmd` and cross-ref SPEC-003 §10.3/§10.4 instead of saying "the executor runs the OnConnect reducer".

Edits landed in: `docs/decomposition/003-executor/SPEC-003-executor.md` §2.4/§10/§10.3/§10.4; `docs/decomposition/003-executor/epic-7-lifecycle-reducers/story-7.3-on-disconnect.md`; `docs/decomposition/005-protocol/SPEC-005-protocol.md` §5.2/§5.3; `docs/decomposition/006-schema/SPEC-006-schema.md` §9.

#### Cluster E — Post-commit fan-out shapes (closed Session 6)
Coordinated declaration across SPEC-002/003/004/005.

- **E1 `PostCommitMeta` shape** — **Resolved:** canonical declaration at SPEC-004 §10.1 (unchanged shape `{TxDurable, CallerConnID, CallerResult}`). SPEC-003 §8 `SubscriptionManager.EvalAndBroadcast` signature aligned to 4-arg form (was 3-arg; live already had 4). Stories 4.5, 5.1 (subscriptions), 1.4, 5.1 (executor), executor EPICS step list updated. Audit §2.12 closed: SPEC-004 §10.1 now pins TxDurable-on-empty-fanout contract (non-nil for every production post-commit invocation; `nil` reserved for test paths).
- **E2 `FanOutMessage` shape** — **Resolved:** canonical at SPEC-004 §8.1 (unchanged). SPEC-005 §13 adds a cross-ref sentence declaring SPEC-004 §8.1 authoritative and noting SPEC-005 does not redeclare the Go struct.
- **E3 `SubscriptionError` shape + delivery** — **Resolved:** Go shape at SPEC-004 §10.2, wire at SPEC-005 §8.4. SPEC-005 §8.4 gains Go↔wire mapping paragraph and pins `request_id = 0` semantics (spontaneous post-register failures; correlated failures echo triggering `request_id != 0`; clients using `request_id = 0` accept correlated/spontaneous indistinguishability — recommend `>= 1`). SPEC-004 §11.1 step 2 now names the delivery path: `FanOutSender.SendSubscriptionError` → protocol adapter → `ClientSender.Send` with `request_id = 0`. Audit §2.4 closed.
- **E4 `ReducerCallResult`** — **Resolved:** wire authoritative at SPEC-005 §8.7; Go forward-decl at SPEC-004 §10.2 now explicitly names §8.7 as authority and names the protocol-adapter encoder (`FanOutSenderAdapter.SendReducerResult`). SPEC-005 §8.7 gains inline status-enum Divergence-from-SpacetimeDB note closing audit §3.9 (flat `uint8` {0,1,2,3} = committed/failed_user/failed_panic/not_found vs tagged-union {Committed, Failed, OutOfEnergy}; `not_found` is first-class because Shunter's registry model treats it distinctly; no energy model in v1).
- **E5 `ClientSender`/`FanOutSender` naming + `Send(connID, any)`** — **Resolved:** SPEC-005 §13 `ClientSender` now declares `Send(connID, msg any) error` (closes audit §1.5 gap). SPEC-005 §13 also gains a normative paragraph documenting the distinct-contracts split (ClientSender is protocol-owned cross-subsystem surface; FanOutSender is subscription-side seam) and the `FanOutSenderAdapter` pattern mapping protocol errors (`ErrClientBufferFull`, `ErrConnNotFound`) to subscription sentinels (`ErrSendBufferFull`, `ErrSendConnGone`). SPEC-004 §10.2 cross-refs the adapter.
- **E6 `DurabilityHandle` contract + `WaitUntilDurable`** — **Resolved (add-to-spec, not remove-from-impl):** SPEC-002 §4.2 and SPEC-003 §7 `DurabilityHandle` interfaces both gain `WaitUntilDurable(txID TxID) <-chan TxID` as the fourth method. Contract: `WaitUntilDurable(0)` returns nil; `WaitUntilDurable(txID>0)` returns a channel that receives exactly one value and is closed. SPEC-002 §4.2 notes executor's narrow consumer only uses `EnqueueCommitted` + `WaitUntilDurable`; full handle is commitlog-lifecycle-owned. Stories 4.1 (commitlog), 1.4 (executor), SPEC-002 EPICS scope list updated. Audit §2.9 closed.
- **E7 Per-subscription eval-error vs SPEC-003 fatal post-commit** — **Resolved:** SPEC-003 §5.4 normative rule rewritten with two bullets + dividing-line rule: fatal = panic/invariant-violation from subsystem; recoverable = per-query eval error caught by the manager and converted to `SubscriptionError`. `EvalAndBroadcast` normal return ⇒ executor continues; panic ⇒ executor fatal. SPEC-004 §11.1 gains a trailing paragraph cross-referencing §5.4 and §11.3 to make the contract symmetric. Audit §1.4 closed.

Edits landed in: `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §4.2, `docs/decomposition/002-commitlog/EPICS.md`, `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md`; `docs/decomposition/003-executor/SPEC-003-executor.md` §5.4/§7/§8, `docs/decomposition/003-executor/EPICS.md`, `docs/decomposition/003-executor/epic-1-core-types/story-1.4-subsystem-interfaces.md`, `docs/decomposition/003-executor/epic-5-post-commit-pipeline/story-5.1-ordered-pipeline.md`; `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` §10.1/§10.2/§11.1, `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.5-manager-interface.md`, `docs/decomposition/004-subscriptions/epic-5-evaluation-loop/story-5.1-eval-transaction.md`; `docs/decomposition/005-protocol/SPEC-005-protocol.md` §8.4/§8.7/§13.

#### Cluster F — front matter only (rolled into B5)
Dropped as standalone; tracked under B5.

### B.2 Per-spec residue

After clusters A–E pull their findings, what remains per spec.
Status legend: `open` (default), `in-cluster` (resolved via cluster — listed for trace), `dropped` (use only if reconciliation determines no edit needed).

#### SPEC-001 — In-Memory Relational Store

| ID | Sev | Summary | Files to edit | Status |
|---|---|---|---|---|
| §1.1 | CRIT | Value equality / hash invariant broken for float ±0 | Story 1.1 | open |
| §1.2 | CRIT | `CommittedReadView.IndexRange` lacks Bound semantics in `BTreeIndex` | Stories 3.3 / 5.3 / 7.1, §7.2 | open |
| §1.3 | CRIT | TxID ownership contradictory | — | closed (B3) |
| §1.4 | CRIT | Undelete-match rule contradicts §5.5 vs Story 5.4 | Story 5.4, §5.5, §6.2 | open |
| §1.5 | CRIT | `AsBytes` return contract undefined; can break immutability | Story 1.1 | open |
| §2.1 | GAP | Sequence recovery: replay does not advance `Sequence.next` | Story 8.2 (and SPEC-002 Story 6.4) | open |
| §2.2 | GAP | `ErrTableNotFound` no production site | Story 5.4 / store boundary | open |
| §2.3 | GAP | `ErrColumnNotFound` declared but unused | — | closed (B2) |
| §2.4 | GAP | `ErrInvalidFloat` no production site (Story 1.1) | Story 1.1 | open |
| §2.5 | GAP | Snapshot close state not enforced | Story 7.x snapshot lifecycle | open |
| §2.6 | GAP | `StateView.SeekIndexRange` may be insufficient for SPEC-004 predicates | §7.2, Story 7.1 | open |
| §2.7 | GAP | `ApplyChangeset` idempotency / partial-replay undefined | §6.x, replay story | open |
| §2.8 | GAP | Row-shape validation error name unreferenced in §9 | §9 | open |
| §2.9 | GAP | Write-lock vs read-lock scope restated inconsistently | §6.2 / §7.x | open |
| §3.1 | DIVERGE | NaN rejected vs SpacetimeDB total-ordering | §1 or §12 divergence block | open |
| §3.2 | DIVERGE | No composite types; RowID stable; rows decoded in memory | divergence block | open |
| §3.3 | DIVERGE | `rowHashIndex` "no PK" vs SpacetimeDB "no unique index" | divergence block | open |
| §3.4 | DIVERGE | Multi-column PK allowed | divergence block | open |
| §3.5 | DIVERGE | Replay constraint violations fatal vs silent skip | divergence block | open |
| §3.6 | DIVERGE | `Changeset` lacks `truncated`/`ephemeral`/`tx_offset` | divergence block | open |
| §4.1 | NIT | SPEC-001 front matter omits SPEC-003 dep | — | closed (B5) |
| §4.2 | NIT | Commit signature returns TxID twice | — | closed (B3) |
| §4.3 | NIT | `ColID` exists but schema uses raw `int` | schema sections | open |
| §4.4 | NIT | Performance section title vs open-question framing | §perf | open |
| §4.5 | NIT | Story 1.1 zero-initialized Value status | Story 1.1 | open |
| §4.6 | NIT | `Nullable` decorative but not marked | — | closed (C2) |
| §4.7 | NIT | Primary IndexID=0 rule ambiguous for no-PK tables | §index section | open |
| §4.8 | NIT | Epic 7 blocks "Nothing" but other specs consume it | EPICS.md | open |
| §4.9 | NIT | §11 executor contract restates `(cs).Snapshot()` outside Epic-7 | §11 | open |
| §5.2 | GAP | §6.3 consumers receive same Changeset — no concurrency contract | §6.3 | open |
| §5.3 | GAP | No story covers `Bytes` copy at Insert boundary | Story 5.4 | open |
| §5.4 | GAP | Story 8.3 `SetNextID` / `SetSequenceValue` semantics asymmetric | Story 8.3 | open |

#### SPEC-002 — Commit Log

| ID | Sev | Summary | Files to edit | Status |
|---|---|---|---|---|
| §1.1 | CRIT | `SnapshotInterval` default contradicts itself (§8 vs §5.6/Story 4.1) | §8, §5.6, Story 4.1 | open |
| §1.2 | CRIT | Decoded `Changeset.TxID` never stamped | Story 3.2 / 6.3, §6.x | closed (B3) |
| §1.3 | CRIT | Snapshot file layout §5.2 omits per-table `nextID` | §5.2, Stories 5.2/5.3 | open |
| §1.4 | CRIT | Recovery sequence-advance step undefined | Story 6.4 (or SPEC-001 Story 8.2) | open |
| §2.1 | GAP | `ErrSnapshotInProgress` omitted from §9 catalog | §9 | open |
| §2.2 | GAP | `ErrTruncatedRecord` omitted from §9 / §2.3 / §6.4 | §9, §2.3, §6.4 | open |
| §2.3 | GAP | Schema snapshot §5.3 lacks per-column `Nullable`/`AutoIncrement` | — | closed (C2) |
| §2.4 | GAP | `row_count` width spec `uint64` vs Story+impl `uint32` | §5.3, Story 5.2 | open |
| §2.5 | GAP | Front matter omits SPEC-003 / SPEC-006 deps | — | closed (B5) |
| §2.6 | GAP | Snapshot→CommittedState restore API not named | SPEC-001 Story 8.3/8.4, Story 6.4 | open |
| §2.7 | GAP | `SchemaRegistry.Version()` contract used but undefined | — | closed (A3) |
| §2.8 | GAP | `durable_horizon` undefined when segments empty + snapshot exists | §6.x | open |
| §2.9 | GAP | Per-TxID durability ack (`WaitUntilDurable`) not in §4.2 | — | closed (E6) |
| §2.10 | GAP | `AppendMode` lives in Story 6.1 but not §6.4 | §6.4, EPICS.md | open |
| §2.11 | GAP | No story owns "schema is static for data-dir lifetime" invariant | new write-path story | open |
| §2.12 | GAP | Snapshot retention deferred but no story owns | new story | open |
| §2.13 | GAP | Graceful-shutdown snapshot orchestration unowned | new story | open |
| §3.1 | DIVERGE | "BSATN" name imported but encoding is rewrite | — | closed (C1) |
| §3.2 | DIVERGE | No offset index file; recovery linear-scan | divergence block | open |
| §3.3 | DIVERGE | Single TX per record vs 1–65535-TX commits | divergence block | open |
| §3.4 | DIVERGE | Replay strictness — `ApplyChangeset` errors fatal | divergence block | open |
| §3.5 | DIVERGE | First TxID is 1, not 0 | divergence block | open |
| §3.6 | DIVERGE | Single auto-increment sequence per table (implicit) | divergence block | open |
| §3.7 | DIVERGE | No segment compression / sealed-immutable marker | divergence block | open |
| §4.1 | NIT | `schema_version` stored twice (§5.2 vs §5.3) | §5.2 / §5.3 | open |
| §4.2 | NIT | `TxID` leaks as bare `uint64` in Story 2.2 + impl | — | closed (B3/B4) — spec aligned on `types.TxID`; live drift at `commitlog/durability.go:135` deferred to Session 12+ |
| §4.3 | NIT | §9 catalog missing four error sentinels stories use | §9 | open |
| §4.4 | NIT | `Record` struct docs imply CRC field but isn't | Story 2.x | open |
| §4.5 | NIT | §4.4 atomic.Uint64 claim vs live mutex | §4.4 | open |
| §4.6 | NIT | EPICS.md dep graph omits recovery → durability AppendMode | EPICS.md | open |
| §4.7 | NIT | §2.1 `.log` extension vs §6.1 segment naming | §2.1 / §6.1 | open |
| §4.8 | NIT | §8 lacks fsync-policy knob; §12 OQ #3 hints | §8 / §12 | open |
| §5.2 | GAP | §5.2 has no story for `nextID` section | new story | open |
| §5.3 | GAP | Sequence-advance-on-replay no story | overlaps §1.4 / §2.1 | open |
| §5.4 | GAP | Snapshot retention no story | overlaps §2.12 | open |
| §5.5 | GAP | Graceful-shutdown snapshot orchestration no story | overlaps §2.13 | open |
| §5.6 | GAP | Snapshot→CommittedState bulk-restore no owner | overlaps §2.6 | open |

#### SPEC-003 — Transaction Executor

| ID | Sev | Summary | Files to edit | Status |
|---|---|---|---|---|
| §1.1 | CRIT | Commit signature contradicted three ways | — | closed (B3) |
| §1.2 | CRIT | Scheduled-reducer firing has no carrier for `schedule_id`/`IntendedFireAt` | Story 1.2, §3.3 | open |
| §1.3 | CRIT | DurabilityHandle contract mismatches §7 + SPEC-002 | — | closed (E6) |
| §1.4 | CRIT | §5 post-commit step order vs Story 5.1 snapshot timing | §5.2, Story 5.1 | open |
| §1.5 | CRIT | OnDisconnect cleanup tx unbounded TxID sink, no identity/CallSource/panic | — | closed (D2) |
| §2.1 | GAP | `init` lifecycle absent | — | closed (D1) |
| §2.2 | GAP | Dangling-client cleanup on restart undefined | new Epic 7 story | open |
| §2.3 | GAP | Typed-adapter error mapping unowned | — | closed (B1) |
| §2.4 | GAP | Scheduler→executor wakeup ordering inconsistent | §5 / Story 5.1 | open |
| §2.5 | GAP | Startup orchestration owner unspecified | new Epic 3 story | open (overlaps A4) |
| §2.6 | GAP | OnConnect/OnDisconnect command identity vs §2.4 single-command | — | closed (D2) |
| §2.7 | GAP | No pre-handler scheduled-row validation on firing | Story 6.x | open |
| §2.8 | GAP | `Schedule`/`ScheduleRepeat` first-fire timing disagreement | Story 6.x | open |
| §2.9 | GAP | `Rollback` not in SPEC-001 contract listed by §13.1 | §13.1 | open |
| §2.10 | GAP | `ErrReducerNotFound` status classification inconsistent | §11 | open |
| §2.11 | GAP | Inbox close-vs-shutdown-flag race not described | Story 3.3 / 3.5 | open |
| §2.12 | GAP | No guidance for scheduler-response dump channel | Story 6.3 | open |
| §3.1 | DIVERGE | Fixed-rate repeat vs SpacetimeDB explicit-reschedule | divergence block | open |
| §3.2 | DIVERGE | Unbounded reducer dispatch queue vs bounded inbox | divergence block | open |
| §3.3 | DIVERGE | Server-stamped timestamp at dequeue vs supplied-at-call | divergence block | open |
| §3.4 | DIVERGE | Post-commit failure always fatal vs per-step recoverable | divergence block | open (E7) |
| §3.5 | DIVERGE | Shunter owns `init` semantics via "no init" | — | closed (D1) |
| §3.6 | DIVERGE | Scheduled-row mutation atomic with reducer writes vs pre-fire delete | divergence block | open |
| §4.1 | NIT | Front matter misdeclares SPEC-002 as "depended on by" | — | closed (B5) |
| §4.2 | NIT | `CallerContext.Timestamp` type vs SPEC-005 wire format | Story 1.x | open |
| §4.3 | NIT | §11 catalog omits sentinels stories imply | §11 | open |
| §4.4 | NIT | `Executor` struct names `store` but §13.1 names `CommittedState` | §13.1 / Story 3.1 | open |
| §4.5 | NIT | `SubscriptionManager.Register` read-view ownership | Story 4.x | open |
| §4.6 | NIT | `Executor.fatal` lock scope vs struct declaration | Story 3.1 | open |
| §4.7 | NIT | `ScheduleID`/`SubscriptionID` no SPEC-005/SPEC-001 home cite | — | closed (B4) |
| §4.8 | NIT | Performance section title mirrors SPEC-001 §4.4 | §perf | open |
| §4.9 | NIT | Story 1.3 `ResponseCh` on every command | Story 1.3 | open |
| §5.2 | GAP | No story owns `max_applied_tx_id` hand-off from SPEC-002 | new story | open |
| §5.3 | GAP | No story owns dangling-client sweep on startup | overlaps §2.2 | open |
| §5.4 | GAP | No story owns read-routing documentation placement | new story | open |
| §5.5 | GAP | No story on reducer/schema registration ordering at engine-boot | — | closed (A4) |

#### SPEC-004 — Subscription Evaluator

| ID | Sev | Summary | Files to edit | Status |
|---|---|---|---|---|
| §1.1 | CRIT | `EvalAndBroadcast` cannot populate §8.1 `FanOutMessage` | — | closed (E1/E2) |
| §1.2 | CRIT | `SubscriptionRegisterRequest` lacks client identity → §3.4 hashing unreachable | §4.1, Story 4.5 | open |
| §1.3 | CRIT | `FanOutMessage` shape omits `TxID` and `Errors` | — | closed (E2) |
| §1.4 | CRIT | §11.1 per-sub eval-error recovery contradicts SPEC-003 §5.4 fatal | — | closed (E7) |
| §1.5 | CRIT | `SubscriptionUpdate.TableID` undefined for joins | §10.2, Story 3.3 | open |
| §1.6 | CRIT | Story 4.1 `subscribers` map cannot hold multi-sub-per-conn | Story 4.1 | open |
| §2.1 | GAP | `CommittedReadView` lifetime across Register/EvalAndBroadcast unpinned | §10.1 | open |
| §2.2 | GAP | `DroppedClients()` channel capacity/close/blocking/dedup missing | §8.5, Story 4.5 | open |
| §2.3 | GAP | Five types not declared in §10 (PostCommitMeta, FanOutMessage, SubscriptionError, ReducerCallResult, IndexResolver) | — | closed (A2/E1/E2/E3/E4) |
| §2.4 | GAP | `SubscriptionError` delivery + payload undefined | — | closed (E3) |
| §2.5 | GAP | `ReducerCallResult` forward-decl shape unpinned | — | closed (E4) |
| §2.6 | GAP | `FanOutSender` / `ClientSender` naming + method-surface split | — | closed (E5) |
| §2.7 | GAP | `IndexResolver` no declared home | — | closed (A2) |
| §2.8 | GAP | `ErrJoinIndexUnresolved`/`ErrSendBufferFull`/`ErrSendConnGone` not in §11 | §11, Story 4.5, EPICS.md | open |
| §2.9 | GAP | Story 5.2 `CollectCandidates` doc-only; live inlines tiering | Story 5.2, §6.x | open |
| §2.10 | GAP | Caller-result delivery when caller's `Fanout` empty unspecified | Story 5.1 | open |
| §2.11 | GAP | Initial row-limit meaning for joins undefined | §x, Story 4.x | open |
| §2.12 | GAP | `PostCommitMeta.TxDurable` for empty-fanout transactions | — | closed (E1) |
| §2.13 | GAP | `PruningIndexes.CollectCandidatesForTable` tier-2 silent skip when resolver nil | Story 2.4 | open |
| §2.14 | GAP | SPEC-004 has no "Depends on" front matter | — | closed (B5) |
| §3.1 | DIVERGE | Go predicate builder vs SpacetimeDB SQL subset | divergence block | open |
| §3.2 | DIVERGE | Bounded fan-out + disconnect-on-lag vs unbounded MPSC + lazy-mark | divergence block | open |
| §3.3 | DIVERGE | No row-level security / per-client predicate filtering | divergence block | open |
| §3.4 | DIVERGE | Post-fragment bag dedup vs in-fragment count tracking | divergence block | open |
| §3.5 | DIVERGE | `PostCommitMeta.TxDurable` flows through subscription seam | — | closed (E1) |
| §4.1 | NIT | §10.1 + Story 4.5 mirror wrong `EvalAndBroadcast` sig | — | closed (E1) |
| §4.2 | NIT | §8.1 `ClientSender` vs live `FanOutSender` | — | closed (E5) |
| §4.3 | NIT | §3.4 hash input vs Story 1.3 byte-append | §3.4, Story 1.3 | open |
| §4.4 | NIT | `CommitFanout` ownership across channel | Story 5.1 | open |
| §4.5 | NIT | `SubscriptionUpdate` carries `TableName` but `TableChangeset` already has | §10.2 | open |
| §4.6 | NIT | §7 `EvalTransaction` vs §10.1 `EvalAndBroadcast` vs live naming | §7 / §10.1 | open |
| §4.7 | NIT | `QueryHash` not listed in §10 type catalog | §10 | open |
| §4.8 | NIT | §9.1 latency targets vs Story 5.4 benchmark labels | §9.1 / Story 5.4 | open |
| §4.9 | NIT | `activeColumns` type mismatch §6.4 vs §7.2 | §6.4 / §7.2 | open |
| §5.2 | GAP | No story owns Manager ↔ FanOutWorker wiring | new story | open |
| §5.3 | GAP | No story for `activeColumns` policy on mid-eval unregister | new story | open |
| §5.4 | GAP | No story for empty-fanout caller-response | overlaps §2.10 | open |
| §5.5 | GAP | `SubscriptionError` delivery no owner story | — | closed (E3) |

#### SPEC-005 — Client Protocol

| ID | Sev | Summary | Files to edit | Status |
|---|---|---|---|---|
| §1.1 | CRIT | §13 `ClientSender` missing `SendSubscriptionError` | — | closed (E3/E5) |
| §1.2 | CRIT | §13 `FanOutMessage` desc stale vs SPEC-004 §8.1 | — | closed (E2) |
| §1.3 | CRIT | `Identity` re-declared despite SPEC-001 §2.4 ownership | — | closed (B4) |
| §1.4 | CRIT | `OutboundCh` close vs concurrent `Send` race | Stories 3.6, 5.1 | open |
| §1.5 | CRIT | `ClientSender.Send(connID, any)` not in §13 | — | closed (E5) |
| §1.6 | CRIT | §14 error catalog incomplete | §14 | open (overlaps E5) |
| §2.1 | GAP | `SubscriptionUpdate` wire format drops `TableID` w/o cross-ref | §8.5, §7.1.1 | open |
| §2.2 | GAP | `ReducerCallResult.TxID = 0` sentinel conflicts with SPEC-002 reservation | — | closed (B3) |
| §2.3 | GAP | Confirmed-read opt-in has no wire representation | new section | open |
| §2.4 | GAP | `SubscriptionError.RequestID = 0` collides with client-chosen 0 | — | closed (E3) |
| §2.5 | GAP | Anonymous-mode mint flooding unbounded | Story 1.x | open |
| §2.6 | GAP | OnConnect has no timeout, idle timer not started | Stories 3.x, 5.x | open |
| §2.7 | GAP | Compression query-param accepted-values not normative | §3.3 | open |
| §2.8 | GAP | Buffer-overflow Close-reason strings have no contract | §11.x | open |
| §2.9 | GAP | `ExecutorInbox` referenced but never declared | §13 / new section | open |
| §2.10 | GAP | `Predicate.Value` wire encoding undefined | §8.x | open |
| §2.11 | GAP | Unsubscribe-while-pending diverges + race undocumented | Story 3.x | open |
| §2.12 | GAP | Subscribe argument size / predicate count bounds undefined | §8.x | open |
| §2.13 | GAP | Subscribe activation timing vs Story 5.2 unclear | Story 5.2 | open |
| §2.14 | GAP | `SubscribeApplied`/`UnsubscribeApplied` activation vs E5 tracker removal | Stories 4.3 / 5.2 | open |
| §2.15 | GAP | `PingInterval`/`IdleTimeout` silent during OnConnect | §12 / §11.1 | open |
| §3.1 | DIVERGE | Subprotocol token `v1.bsatn.shunter` forks namespace | divergence block | open |
| §3.2 | DIVERGE | Compression tag values collide with reference | divergence block | open |
| §3.3 | DIVERGE | Outgoing buffer 256 vs SpacetimeDB 16384 | divergence block | open |
| §3.4 | DIVERGE | No TransactionUpdate light/heavy split | divergence block | open |
| §3.5 | DIVERGE | No SubscribeMulti/Single/QuerySetId | divergence block | open |
| §3.6 | DIVERGE | No `CallReducer.flags` byte | divergence block | open |
| §3.7 | DIVERGE | OneOffQuery uses structured predicates not SQL | divergence block | open |
| §3.8 | DIVERGE | Close codes differ slightly | divergence block | open |
| §3.9 | DIVERGE | `ReducerCallResult.status` enum maps neither way | — | closed (E4) |
| §3.10 | DIVERGE | No `OutOfEnergy` / `Energy` | divergence block | open |
| §3.11 | DIVERGE | ConnectionId reuse on reconnect no server-side meaning | divergence block | open |
| §4.1 | NIT | BSATN naming disclaimer missing | — | closed (C1) |
| §4.2 | NIT | "Depends on:" front matter underclaims | — | closed (B5) |
| §4.3 | NIT | §9.1 names "pending removal" state Story 3.3 doesn't model | Story 3.3 / §9.1 | open |
| §4.4 | NIT | §15 OQ #4 resolvable; should close | §15 | closed (B4) |
| §4.5 | NIT | `CloseHandshakeTimeout` in §12 but §11.1 silent | §11.1 | open |
| §4.6 | NIT | §8.5 `SubscriptionUpdate` shape comment refs nonexistent struct | §8.5 | open |
| §4.7 | NIT | §5.2/§5.3 OnConnect/OnDisconnect described as reducers vs §2.4 | — | closed (D2) |
| §4.8 | NIT | `ErrZeroConnectionID` listed but validation duplicated | §14 / Story 1.x | open |
| §4.9 | NIT | `Energy` always 0 but no decode-side tolerance documented | §x | open |
| §4.10 | NIT | `Conn.OutboundCh` close rule vs Story 3.6 (see §1.4) | overlaps §1.4 | open |
| §4.11 | NIT | `ConnectionID` hex-encoding format on wire underspecified | §x | open |
| §5.2 | GAP | No story owns `SendSubscriptionError` fan-out | overlaps §1.1 | open |
| §5.3 | GAP | No story for `OutboundCh` close-race sync | overlaps §1.4 | open |
| §5.4 | GAP | No story for `ExecutorInbox` shape | overlaps §2.9 | open |
| §5.5 | GAP | No story for `Predicate.Value` wire encoding | overlaps §2.10 | open |
| §5.6 | GAP | No story for confirmed-read opt-in | overlaps §2.3 | open |
| §5.7 | GAP | Story 5.2 `SendSubscribeApplied` disconnect-race | overlaps §2.13 | open |
| §5.8 | GAP | Double-removal of subscription tracker entries | overlaps §2.14 | open |

#### SPEC-006 — Schema Definition

| ID | Sev | Summary | Files to edit | Status |
|---|---|---|---|---|
| §1.1 | CRIT | `SchemaLookup` interface no home | — | closed (A1) |
| §1.2 | CRIT | `IndexResolver` interface no home | — | closed (A2) |
| §1.3 | CRIT | `ErrReducerArgsDecode` typed-adapter sentinel unowned | — | closed (B1) |
| §1.4 | CRIT | Reducer registration / freeze lifecycle unspecified | — | closed (A4) |
| §1.5 | CRIT | `SchemaRegistry.Version()` semantics undefined | — | closed (A3) |
| §2.1 | GAP | `ColumnSchema` inconsistent spec §8 vs live | — | closed (C2) |
| §2.2 | GAP | `Nullable` preemptive-only but §9/§13 silent | — | closed (C2) |
| §2.3 | GAP | Reducer-arg schema unreachable from `ReducerExport` | §8 / Story 6.x | open |
| §2.4 | GAP | `init` lifecycle not declared/deferred | — | closed (D1) |
| §2.5 | GAP | `ErrReservedReducerName`/nil-handler/dup-lifecycle no sentinel | §13 | open |
| §2.6 | GAP | `ErrColumnNotFound` defined three times | — | closed (B2) |
| §2.7 | GAP | No "v1 simplifications vs SpacetimeDB" block | divergence block | open |
| §2.8 | GAP | `ScheduleID` width divergence | divergence block | open |
| §2.9 | GAP | BSATN naming disclaimer not propagated | — | closed (C1) |
| §2.10 | GAP | `Engine.Start(ctx)` contract vs live stub | §5, Story x | open (overlaps A4) |
| §2.11 | GAP | Multi-column PK enforcement implicit | §x | open |
| §2.12 | GAP | Named composite index uniqueness check not on builder path | Story x | open |
| §2.13 | GAP | Front matter understates dependencies | — | closed (B5) |
| §2.14 | GAP | `cmd/shunter-codegen` does not exist | Story 6.3 / spec | open |
| §3.1 | DIVERGE | Registration model: runtime reflect vs proc-macros | divergence block | open |
| §3.2 | DIVERGE | Lifecycle reducer convention | divergence block (overlaps D1) | closed (D1) |
| §3.3 | DIVERGE | System tables minimal vs reflective | divergence block | open |
| §3.4 | DIVERGE | No `SequenceSchema` — auto-increment is column flag | divergence block | open |
| §3.5 | DIVERGE | Column-type enum vs `AlgebraicType` | divergence block | open |
| §4.1 | NIT | Story 5.4 `Table(id)` returns clone — perf vs immutability undocumented | Story 5.4 | open |
| §4.2 | NIT | `Reducers()` ordering "stable" but ambiguous | §7 | open |
| §4.3 | NIT | `SchemaRegistry.Tables()` ordering not in §7 | §7 | open |
| §4.4 | NIT | Story 4.1 `discoverFields` signature drift | Story 4.1 | open |
| §4.5 | NIT | Story 2.2 `ValidateTag` split vs ParseTag fold | Story 2.2 | open |
| §4.6 | NIT | `DefaultIndexName` signature inconsistency | Story x | open |
| §4.7 | NIT | Story 5.3 TableID assignment determinism undocumented | Story 5.3 | open |
| §4.8 | NIT | `validateStructure` doesn't check PK-vs-named-composite-participant | Story x | open |
| §4.9 | NIT | `ExportSchema` lifecycle ordering regardless of version | Story 6.2 | open |
| §5.1 | GAP | No story owns `SchemaLookup`/`IndexResolver` declaration | — | closed (A1/A2) |
| §5.2 | GAP | No story owns `ErrReservedReducerName` etc | overlaps §2.5 | open |
| §5.3 | GAP | No story owns registration-order freeze | — | closed (A4) |
| §5.4 | NIT | Epic 4 implementation order glosses Story 4.2 mixed-unique check | Epic 4 | open |
| §5.5 | NIT | Story 6.3 acceptance lacks generated TS shape | Story 6.3 | open |

### B.3 Session cadence

Each session targets ≤150k tokens. Edits land on `docs/decomposition/**` only (Sessions 2–10). Live-code drift reconciliation deferred to Sessions 11+.

| # | Scope | Inputs | Stop rule |
|---|---|---|---|
| 1 | This tracking doc (current) | full SPEC-AUDIT.md headings | tracking doc committed |
| 2 | Cluster A — schema contracts (`SchemaLookup`, `IndexResolver`, `Version()`, freeze) | SPEC-006 §1.1–1.5; SPEC-002 §2.7; SPEC-003 §5.5; SPEC-004 §2.7/§2.14; SPEC-005 §4.2 | **(closed)** SPEC-006 §7 + §5 + §6.1 carry the four declarations; cross-refs added in SPEC-002/003/004/005 |
| 3 | Cluster B — error sentinels + types canonicalization + Commit/TxID | SPEC-006 §1.3/§2.6; SPEC-001 §1.3/§2.3/§4.1/§4.2; SPEC-002 §1.2/§2.5/§4.2; SPEC-003 §1.1/§2.3/§4.1/§4.7; SPEC-005 §1.3/§2.2/§4.2; SPEC-004 §2.14 | **(closed)** Model A pinned (executor allocates TxID, stamps `changeset.TxID`); `ErrReducerArgsDecode` deferred to SPEC-006; `ErrColumnNotFound` canonicalized in SPEC-006 §13; `types/` named as canonical Go-package home; front-matter deps completed across all six specs |
| 4 | Cluster C — BSATN disclaimer + per-column trailer | SPEC-002 §2.3/§3.1/§6.1; SPEC-001 §4.6; SPEC-005 §4.1/§6.1; SPEC-006 §2.1/§2.2/§2.9; SPEC-003/004 clean-room caveats | **(closed)** SPEC-002 §3.1 carries canonical disclaimer; cross-refs in SPEC-003 §3.1 / SPEC-004 §6 / SPEC-005 §3.1 / SPEC-006 §1.2. Per-column trailer pinned at `(type_tag, nullable, auto_increment)` (Option A — match live); SPEC-006 §8 ColumnSchema gets `AutoIncrement`; `ErrNullableColumn` landed in §13 + Story 5.1 acceptance. |
| 5 | Cluster D — lifecycle reducer / OnConnect / OnDisconnect / init | SPEC-003 §1.5/§2.1/§2.6/§3.5; SPEC-005 §4.7; SPEC-006 §2.4/§3.2 | **(closed)** SPEC-006 §9 defers `init`/`update` (not reserved; use deployment-time reducer; v2 target). SPEC-003 §2.4 declares `OnConnectCmd` / `OnDisconnectCmd` as bespoke commands (Option A — match live); §10.3/§10.4 rewritten; §10.4 pins the four SPEC-AUDIT §1.5 contracts (CallSource reuse of `CallSourceLifecycle`; one TxID per failed OnDisconnect; cleanup post-commit panics fatal per §5.4; cleanup runs even when `e.fatal`). Story 7.3 acceptance extended. SPEC-005 §5.2/§5.3 cross-ref `OnConnectCmd`/`OnDisconnectCmd`. |
| 6 | Cluster E — post-commit fan-out shapes (PostCommitMeta, FanOutMessage, SubscriptionError, ReducerCallResult, ClientSender, DurabilityHandle, eval-error vs fatal) | SPEC-002 §2.9; SPEC-003 §1.3/§3.4/§5.4; SPEC-004 §1.1/§1.3/§1.4/§2.3/§2.4/§2.5/§2.6/§2.12/§3.5/§4.1/§4.2; SPEC-005 §1.1/§1.2/§1.5/§1.6/§2.4/§3.9/§5.2 | **(closed)** Five shapes canonicalized: PostCommitMeta (SPEC-004 §10.1), FanOutMessage (SPEC-004 §8.1), SubscriptionError (SPEC-004 §10.2 Go / SPEC-005 §8.4 wire), ReducerCallResult (SPEC-004 §10.2 Go forward-decl / SPEC-005 §8.7 wire), ClientSender+FanOutSender (SPEC-005 §13 with `Send(connID, any)` + adapter pattern / SPEC-004 §8.1). E6 `WaitUntilDurable` added to SPEC-002 §4.2 + SPEC-003 §7. E7 per-query recovery (SPEC-004 §11.1) vs fatal-panic (SPEC-003 §5.4) dividing line pinned. SPEC-003 §8 `EvalAndBroadcast` signature aligned to 4-arg `PostCommitMeta` form. Audit §2.4 `request_id = 0` collision closed; §3.9 status-enum DIVERGE landed inline at SPEC-005 §8.7. |
| 7 | SPEC-001 residue cleanup | SPEC-001 §1.1/1.2/1.4/1.5, §2.1/2.2/2.4–2.9, §3.x, §4.3–4.5/4.7–4.9, §5.2–5.4 | All "open" SPEC-001 rows resolved or explicitly deferred |
| 8 | SPEC-002 residue cleanup | SPEC-002 §1.1/1.3/1.4, §2.1/2.2/2.4/2.6/2.8/2.10–2.13, §3.x, §4.1/4.3–4.8, §5.2–5.6 | All open SPEC-002 rows resolved/deferred |
| 9 | SPEC-003 residue cleanup | SPEC-003 §1.2/1.4, §2.2/2.4/2.5/2.7–2.12, §3.x, §4.2–4.6/4.8/4.9, §5.2/5.4 | All open SPEC-003 rows resolved/deferred |
| 10 | SPEC-004/005 residue cleanup | SPEC-004 §1.2/1.5/1.6, §2.1/2.2/2.8–2.11/2.13, §3.x, §4.3–4.9, §5.2–5.4; SPEC-005 §1.4/1.6, §2.1/2.3/2.5–2.15, §3.1–3.8/3.10/3.11, §4.3–4.6/4.8/4.9/4.11, §5.2–5.8 | All open SPEC-004 and SPEC-005 rows resolved/deferred |
| 11 | SPEC-006 residue cleanup | SPEC-006 §2.3/2.5/2.7/2.8/2.10–2.12/2.14, §3.x, §4.1–4.9, §5.2/5.4/5.5 | All open SPEC-006 rows resolved/deferred |
| 12+ | Spec-to-code drift batches (per-spec §8) | live impl files cited in SPEC-AUDIT.md §8 of each spec | drift either upstreamed into spec or impl realigned |

### B.4 Kickoff templates

Paste one of the following at the start of the named session.

**Session 2 (Cluster A — schema contracts):**
> Continue Lane B reconciliation. This session resolves Cluster A from `AUDIT_HANDOFF.md` §B.1: `SchemaLookup` (SPEC-006 §1.1, SPEC-004 §2.14, SPEC-005 §4.2), `IndexResolver` (SPEC-006 §1.2, SPEC-004 §2.7), `SchemaRegistry.Version()` (SPEC-006 §1.5, SPEC-002 §2.7), freeze lifecycle (SPEC-006 §1.4, SPEC-003 §5.5). Read those audit sections in `SPEC-AUDIT.md` for full detail. Edit only `docs/decomposition/006-schema/SPEC-006-schema.md` (§5, §6.1, §7), `docs/decomposition/006-schema/epic-5-*/story-5.3-*.md`, and add cross-refs in SPEC-002 §5.6, SPEC-003 §13.1, SPEC-004 §10, SPEC-005 §13. Stop when: SPEC-006 §7 declares both interfaces with one canonical signature, §5 names `Build()`=freeze with explicit ordering, §6.1 pins `Version()` semantics, and downstream specs cross-ref instead of redeclaring. Update §B.2 status from `in-cluster` to `closed` for each finding ID resolved.

**Session 3 (Cluster B — error sentinels + canonical types + Commit/TxID):**
> Continue Lane B. Resolve Cluster B from `AUDIT_HANDOFF.md` §B.1: `ErrReducerArgsDecode` home (SPEC-006 §1.3, SPEC-003 §2.3); `ErrColumnNotFound` 3-home (SPEC-006 §2.6, SPEC-001 §2.3); TxID/Commit signature (SPEC-001 §1.3/§4.2, SPEC-002 §1.2/§4.2, SPEC-003 §1.1/§4.1/§4.7, SPEC-005 §2.2); canonical `types/` home for `Identity`/`ConnectionID`/`TxID`/`ReducerHandler`/`ReducerContext` (SPEC-005 §1.3, SPEC-006 §8 drift); front-matter deps (SPEC-001 §4.1, SPEC-002 §2.5, SPEC-003 §4.1, SPEC-004 §2.14, SPEC-005 §4.2, SPEC-006 §2.13). Files: `docs/decomposition/{001,002,003,005,006}-*/SPEC-*.md` plus stories 6.1/6.2 in SPEC-001, story 6.3 in SPEC-002, story 4.3 in SPEC-003. Decide Commit Model A or B before editing. Stop when: one TxID model is in all four specs; one error-sentinel home per sentinel; types-package home documented in each spec's §1; front matter deps complete.

**Session 4 (Cluster C — BSATN + per-column trailer):**
> Continue Lane B. Resolve Cluster C: BSATN naming disclaimer (SPEC-002 §3.1/§6.1, SPEC-005 §4.1/§6.1, SPEC-006 §2.9, plus cross-refs noted in SPEC-003/004 clean-room sections); per-column `Nullable`/`AutoIncrement` trailer (SPEC-002 §2.3, SPEC-001 §4.6, SPEC-006 §2.1/§2.2). Decide trailer = match live (3-byte form) or strip; reflect in SPEC-002 §5.3, SPEC-006 §8 ColumnSchema, SPEC-001 Story 1.1. Disclaimer goes in SPEC-002 §3.1 once and is cross-ref'd from SPEC-005 §3.1 + SPEC-006 §1. Stop when: disclaimer present in originator + cross-ref'd in 4 places; ColumnSchema trailer policy stated normatively in three specs.

**Session 5 (Cluster D — lifecycle reducers / OnConnect/OnDisconnect / init):**
> Continue Lane B. Resolve Cluster D: `init` lifecycle (SPEC-003 §2.1/§3.5, SPEC-006 §2.4); OnConnect/OnDisconnect command identity (SPEC-003 §1.5/§2.6, SPEC-005 §4.7). Live `executor/command.go:61-79` has bespoke `OnConnectCmd`/`OnDisconnectCmd` — decide spec adopts these or unifies under reducer-shaped commands. Files: `docs/decomposition/003-executor/SPEC-003-executor.md` §2.4/§5/§10.4 + Story 7.3; `docs/decomposition/005-protocol/SPEC-005-protocol.md` §2.4/§5.2/§5.3; `docs/decomposition/006-schema/SPEC-006-schema.md` §9. Stop when: `init` decision is normative in SPEC-006; OnConnect/Disconnect identity (CallSource/TxID/panic handling) pinned in SPEC-003; SPEC-005 §5.2/§5.3 wording matches SPEC-003 model.

**Session 6 (Cluster E — post-commit fan-out shapes):**
> Continue Lane B. Resolve Cluster E across SPEC-002/003/004/005: `PostCommitMeta`, `FanOutMessage`, `SubscriptionError`, `ReducerCallResult`, `ClientSender`/`FanOutSender`, `DurabilityHandle`+`WaitUntilDurable`, per-sub-eval-error vs fatal post-commit. Audit refs: SPEC-002 §2.9; SPEC-003 §1.3/§3.4/§5.4; SPEC-004 §1.1/§1.3/§1.4/§2.3/§2.4/§2.5/§2.6/§2.12/§3.5/§4.1/§4.2; SPEC-005 §1.1/§1.2/§1.5/§1.6/§2.4/§3.9/§5.2. Files: SPEC-004 §8/§10/§11; SPEC-005 §13/§14; SPEC-002 §4.2/§7; SPEC-003 §7/§8. Stop when: all five type shapes are declared in one spec each with cross-refs from consumers; eval-error recovery model resolved (E7); `WaitUntilDurable` either added to SPEC-002 §4.2 or removed from impl with a deferred-debt note.

**Session 7 (SPEC-001 residue):**
> Continue Lane B. Walk SPEC-001 rows in `AUDIT_HANDOFF.md` §B.2 with status `open`. For each, read the cited audit section in `SPEC-AUDIT.md`, edit `docs/decomposition/001-store/**` per the prescribed file column, mark status `closed`. Skip any `in-cluster` rows — those belong to Sessions 2–6. Stop when: every open SPEC-001 row is `closed` or marked `deferred` with one-line reason.

**Session 8 (SPEC-002 residue):** *(same template as Session 7, swap to SPEC-002 / `docs/decomposition/002-commitlog/**`)*

**Session 9 (SPEC-003 residue):** *(same template, SPEC-003 / `docs/decomposition/003-executor/**`)*

**Session 10 (SPEC-004 + SPEC-005 residue):** *(same template, both 004-subscriptions and 005-protocol; coordinate the cross-spec NIT pairs flagged in §B.2)*

**Session 11 (SPEC-006 residue):** *(same template, SPEC-006 / `docs/decomposition/006-schema/**`)*

**Session 12 (drift batch — SPEC-001/002):**
> Lane B drift pass. SPEC-AUDIT.md §8 of SPEC-001 and SPEC-002 list spec-to-code drift items where the live impl in `store/` and `commitlog/` diverges from the now-repaired spec. For each item: decide whether to upstream live behavior into the spec (write a Story addendum) or realign impl (open a TECH-DEBT.md item, do not fix in this session). Stop after both spec sections are walked. Defer SPEC-003+ drift to Session 13+.

*(Sessions 13/14 mirror Session 12 for SPEC-003/004 and SPEC-005/006 respectively.)*

### B.5 How to update this tracking doc

When closing a finding:
- Change Status column entry from `open` / `in-cluster` to `closed`.
- If finding is split or rephrased during edit, leave a one-line note in the Summary cell with new audit-section pointer.
- Do not delete rows — `closed` is the audit trail.
- When a cluster is fully resolved, mark its §B.1 heading with `(closed Session N)`.

When a new bleed-item surfaces during a session:
- Add it as a new cluster letter in §B.1 with cited finding IDs.
- Push affected spec residue rows from `open` to `in-cluster <letter>`.

Cursor: Session 7 (SPEC-001 residue cleanup).
