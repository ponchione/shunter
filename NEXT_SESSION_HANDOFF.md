# Next session handoff

Use this file to start the next agent on the next real Shunter parity step with no prior context.

## Copy-paste prompt

Continue Shunter from the latest completed TD-142 SQL parity work. The current run (2026-04-20) landed Slices 10, 10.5, 11, 12, 13, and 14:
- Slice 10: aliased self cross-join projection (`SELECT a.* FROM t AS a JOIN t AS b`)
- Slice 10.5: parser-level alias identity and ON cross-relation check alias-based
- Slice 11: runtime alias identity in `subscription.Join` — aliased self equi-join projection (`SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32`) now works end-to-end
- Slice 12: alias-aware WHERE on self-join — `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1` (and the symmetric `WHERE b.id = 1`) now work end-to-end
- Slice 13 (narrow parity-matched rejection): three-way/multi-way join chains (`SELECT t.* FROM t JOIN s JOIN s AS r ...`) are now explicitly rejected at the parser with a reference-citing error and pinned at the subscribe / one-off admission boundaries
- Slice 14 (projection gap closed): join subscriptions now emit rows shaped like the SELECT table instead of LHS++RHS concat

## Slice 14 — what landed

Grounded problem: prior to Slice 14, `subscription/eval.go::evalQuery` and `subscription/register_set.go::initialQuery` returned LHS++RHS concatenated rows for every join subscription. One-off query at `protocol/handle_oneoff.go::evaluateOneOffJoin` already projected correctly but used the ambiguous `projectedTable == join.Left` equality — which collapses for self-joins where Left == Right. The reference at `reference/SpacetimeDB/crates/subscription/src/lib.rs:367` (`SubscriptionPlan::subscribed_table_id`) emits one concrete table's row shape per plan.

Resolution:
- `subscription.Join` gained `ProjectRight bool`. Zero-value projects LHS (matches existing default). True projects RHS.
- `subscription.SchemaLookup` gained `ColumnCount(TableID) int` so the evaluator knows the LHS width and can slice the reconciled IVM fragments at the LHS/RHS boundary.
- `subscription/eval.go::projectJoinedRows` runs post-`ReconcileJoinDelta` and slices each row; the IVM bag arithmetic is unchanged (it still reconciles on full concat rows — projection is the final emission step).
- `subscription/register_set.go::initialQuery` projects per matched pair inside the re-evaluation loop and uses a new `emittedTableID` helper for `SubscriptionUpdate.TableID`.
- `protocol/handle_oneoff.go::evaluateOneOffJoin` now trusts `Join.ProjectRight`, not the ambiguous `projectedTable == join.Left` equality.
- Canonical hashing (`subscription/hash.go`) encodes `ProjectRight` so `SELECT lhs.*` and `SELECT rhs.*` register as distinct queries.
- Parser: `Statement.ProjectedAlias` preserves the user-typed qualifier (`a` vs `b` on self-joins, `product` vs `o` on distinct-table joins). Compile at `protocol/handle_subscribe.go::joinProjectsRight` maps it to `ProjectRight`.

Pins (all new tests over the 1095 baseline):
- `subscription/hash_test.go::TestQueryHashJoinProjectionDiffers`
- `subscription/manager_test.go::TestRegisterJoinBootstrapFallsBackToLeftIndex` (re-asserted projected-width + TableID)
- `subscription/manager_test.go::TestRegisterJoinBootstrapProjectsRight`
- `subscription/eval_test.go::TestEvalJoinSubscription` (re-asserted LHS projection width + TableID)
- `subscription/eval_test.go::TestEvalJoinSubscriptionProjectsRight`
- `query/sql/parser_test.go::TestParseAliasedSelfEquiJoinProjectsRight`
- `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_AliasedSelfEquiJoinProjectsRight`
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_AliasedSelfEquiJoinProjectsRight`

Baseline after Slice 14: `Go test: 1101 passed in 10 packages`.

## What is no longer open under TD-142

Every named reference-backed SQL parity shape inside TD-142 is now resolved — accepted (Slices 1–12), rejected with parity-matched outcome (Slice 13), or structurally corrected (Slice 14 projection). Further SQL widenings are new parity work, not TD-142 follow-up.

## Next realistic parity anchors

With TD-142 fully drained, the grounded options are:

### Option α — Lag / slow-client policy parity (Phase 2 Slice 3)

Primary code surfaces: `subscription/fanout_worker.go`, `subscription/fanout.go`, `protocol/outbound.go`, `protocol/sender.go`, `protocol/fanout_adapter.go`. Decision point: emulate SpacetimeDB's deeper queue / lazy slow-client semantics, or keep Shunter's bounded disconnect-on-lag policy and mark this divergence explicitly permanent. This is now the largest remaining externally visible subscription-behavior divergence.

### Option β — Scheduled reducer startup/firing ordering (`P0-SCHED-001`)

Still `in_progress` per `docs/parity-phase0-ledger.md`. Compare Shunter's recovered scheduled-reducer startup ordering against the reference's visible firing semantics and close the timing decisions deliberately. Primary code: `executor/scheduler.go`, `executor/scheduler_worker.go`, `executor/scheduler_replay_test.go`.

### Option γ — Replay-horizon / validated-prefix behavior (`P0-RECOVERY-001`)

Still `in_progress`. Phase 4 decision about replay tolerance vs fail-fast. Primary code: `commitlog/replay.go`, `commitlog/recovery.go`.

### Option δ — Close a Tier-B hardening item

`TECH-DEBT.md` still carries OI-004 (protocol lifecycle / goroutine ownership), OI-005 (snapshot / read-view lifetime), OI-006 (fanout aliasing), OI-007 (recovery sequencing). Pick one and land a narrow fix with a focused test.

## First, what you are walking into

The repo already has substantial implementation.
Do not treat this as a docs-only project.
Do not do a broad audit.
Do not restart parity analysis from zero.

Your job is to continue from the current live state. Pick the next grounded parity anchor from `docs/spacetimedb-parity-roadmap.md` and `docs/parity-phase0-ledger.md`.

## Mandatory reading order

Read in this order before changing code:

1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `docs/parity-phase0-ledger.md`
9. `TECH-DEBT.md`
10. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

## Shell discipline

Use `rtk` for shell commands.
Examples:
- `rtk git status --short --branch`
- `rtk go test ./query/sql ./protocol -run 'TestNameA|TestNameB' -v`
- `rtk go test ./...`

## Important repo note

Keep `.hermes/plans/2026-04-18_073534-phase1-wire-level-parity.md` unless you deliberately update the contract that depends on it. A test expects it.

## What is already landed (do not reopen)

### Non-join TD-142 slices

1. same-table qualified `WHERE` columns — `SELECT * FROM users WHERE users.id = 1`
2. case-insensitive identifier resolution — `SELECT * FROM USERS WHERE ID = 1 AND users.DISPLAY_NAME = 'alice'`
3. single-table alias / qualified-star — `SELECT item.* FROM users AS item WHERE item.name = 'alice'`
4. ordered comparisons `<`, `<=`, `>`, `>=` — lowered into `subscription.ColRange`
5. non-equality comparisons `<>` / `!=` — lowered into `subscription.ColNe`
6. narrow same-table `OR` — `SELECT * FROM metrics WHERE score = 9 OR score = 11`

### Join-backed TD-142 slices

7. left-projected equi-join — `SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10`
8. right-projected equi-join — `SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id [WHERE o.id = 1]`
9. narrow cross-join projection (distinct tables) — `SELECT o.* FROM Orders o JOIN Inventory product`
10. aliased self cross-join projection (same table) — `SELECT a.* FROM t AS a JOIN t AS b`
11. parser alias identity + aliased self equi-join parse acceptance (Slice 10.5)
12. runtime alias identity in `subscription.Join` + filterless aliased self equi-join end-to-end (Slice 11)
13. alias-aware WHERE on self-join (Slice 12) — `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1` and symmetric
14. multi-way join rejection (Slice 13) — reference-matched admission rejection pinned at parser and admission boundaries
15. **join projection semantics (Slice 14, 2026-04-20)** — `subscription.Join.ProjectRight` + canonical hash + `projectJoinedRows` + `emittedTableID` + `Statement.ProjectedAlias` + `joinProjectsRight` compile path. Join subscriptions now emit rows shaped like the SELECT table at both subscribe-initial, post-commit delta eval, and one-off paths.

### Alias-scope parity tighten

- once a relation is aliased, the base table name is out of scope for qualified projection and qualified `WHERE`
- unaliased self-join is rejected (`SELECT t.* FROM t JOIN t`)
- same-alias-both-sides of ON is rejected (`SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = a.u32`)
- multi-way (≥3 relation) JOIN chains are rejected with a reference-citing error (Slice 13)

## Suggested verification commands

Targeted:
- `rtk go test ./query/sql -run '<target parser tests>' -v`
- `rtk go test ./query/sql ./protocol ./subscription -run '<target tests>' -v`
- `rtk go test ./query/sql ./protocol ./subscription -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed target shape was checked directly against reference material
- every newly accepted or rejected shape has focused tests
- parser, subscribe, one-off, and post-commit-delta coverage all exist where applicable
- already-landed accepted shapes still pass (especially aliased self equi-join with and without WHERE, and the new Slice 14 projection width/TableID pins)
- already-landed rejected shapes still reject where the reference requires rejection
- full suite still passes (current baseline: `Go test: 1101 passed in 10 packages`)
- docs and handoff reflect the new truth exactly

## Deliverables for the next session

Either:
- code + tests closing the next reference-backed parity slice

Or:
- a grounded blocker report naming the exact representation/runtime issue preventing a narrow landing

And in either case:
- update `TECH-DEBT.md` if any OI changes state
- update `docs/current-status.md`
- update `docs/parity-phase0-ledger.md`
- update `NEXT_SESSION_HANDOFF.md`

## Final status snapshot right now

As of this handoff:
- targeted parity work continues to be real and cumulative
- `TD-142` is fully drained — accepted (Slices 1–12), rejected with parity-matched outcome (Slice 13), and projection-corrected (Slice 14)
- the next realistic parity anchors are outside TD-142: lag / slow-client policy (Option α), scheduled-reducer startup ordering (Option β), replay-horizon behavior (Option γ), or a Tier-B hardening item (Option δ)
- 10 packages, 1101 tests passing as of 2026-04-20
