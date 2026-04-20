# Next session handoff

Use this file to start the next agent on the next real Shunter parity step with no prior context.

## Copy-paste prompt

Continue Shunter from the latest completed TD-142 SQL parity work. The current run (2026-04-20) landed Slices 10, 10.5, 11, and 12:
- Slice 10: aliased self cross-join projection (`SELECT a.* FROM t AS a JOIN t AS b`)
- Slice 10.5: parser-level alias identity and ON cross-relation check alias-based
- Slice 11: runtime alias identity in `subscription.Join` — aliased self equi-join projection (`SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32`) now works end-to-end
- Slice 12: alias-aware WHERE on self-join — `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1` (and the symmetric `WHERE b.id = 1`) now work end-to-end

The next realistic landing is **Slice 13 (multi-join)**: three-way / multi-relation joins such as `SELECT t.* FROM t JOIN s JOIN s AS r WHERE t.u32 = s.u32 AND s.u32 = r.u32`. Reference-accepted by `reference/SpacetimeDB/crates/expr/src/check.rs`. The immediate blocker is the binary-predicate shape of `subscription.Join` (single `Left`, `Right`, one `LeftCol=RightCol` pair). Multi-join requires either a chain/tree of join predicates or a generalized N-relation runtime representation closer to the reference `PhysicalPlan`.

## First, what you are walking into

The repo already has substantial implementation.
Do not treat this as a docs-only project.
Do not do a broad audit.
Do not restart parity analysis from zero.

Your job is to continue the remaining `TD-142` subscription-SQL parity work from the current live state.

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
9. `TECH-DEBT.md` (especially the TD-142 entry, including the Slice 10.5 / 11 / 12 landing notes)
10. `query/sql/parser.go`
11. `query/sql/parser_test.go`
12. `protocol/handle_subscribe.go`
13. `protocol/handle_oneoff.go`
14. `subscription/predicate.go`
15. `subscription/hash.go`
16. `subscription/validate.go`
17. `subscription/register_set.go`
18. `subscription/eval.go`
19. `subscription/delta_join.go`
20. `subscription/delta_dedup.go`
21. `subscription/placement.go`
22. relevant protocol/query/subscription tests before touching code

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
13. **alias-aware WHERE on self-join** (Slice 12, 2026-04-20) — `ColEq` / `ColNe` / `ColRange` gained `Alias uint8`, `MatchRowSide` routes each leaf to the side the user named, `PlaceSubscription` / `RemoveSubscription` short-circuit self-join to Tier 3, parser's `sql.Filter` carries `Alias string`, and `compileSQLQueryString` builds a self-join-aware `aliasTag` closure that maps the right-alias string to `1` and everything else to `0`. Pinned by `TestEvalSelfEquiJoinWithAliasedWhere`, `TestHandleSubscribeSingle_AliasedSelfEquiJoinWithWhere`, `TestHandleOneOffQuery_AliasedSelfEquiJoinWithWhereAside`, and `TestHandleOneOffQuery_AliasedSelfEquiJoinWithWhereBside`.

### Alias-scope parity tighten

- once a relation is aliased, the base table name is out of scope for qualified projection and qualified `WHERE`
- unaliased self-join is rejected (`SELECT t.* FROM t JOIN t`)
- same-alias-both-sides of ON is rejected (`SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = a.u32`)

## Slice 13 — multi-join (three-way, with self-alias)

### Target shapes

Reference-accepted by `reference/SpacetimeDB/crates/expr/src/check.rs`:

- `SELECT t.* FROM t JOIN s JOIN s AS r WHERE t.u32 = s.u32 AND s.u32 = r.u32`

Reference-rejected (unrelated qualifier resolution):
- `SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r`

### Why it's bigger than Slice 12

`subscription.Join` is a binary predicate. `query/sql.JoinClause` is also binary (`LeftTable`/`RightTable` only). Both need to grow to carry N relation instances. Two plausible options:

- **Option A (chain of joins)**: parse as `(t JOIN s) JOIN (s AS r)` and carry `Join` nested in `Filter`, with each `Join` remaining binary. Requires `subscription.Join` itself to become a valid `Predicate` child of another `Join`, and `EvalJoinDeltaFragments` must compose across layers. The 4-fragment IVM combinatorially multiplies with N relations; reference literature uses different tactics here.
- **Option B (explicit N-relation plan)**: introduce a `MultiJoin` predicate shape carrying `[]RelationInstance` (TableID + alias tag + column-set) plus `[]EquiJoinEdge` (left-alias, left-col, right-alias, right-col). Replace `EvalJoinDeltaFragments` with a multi-relation IVM evaluator that pairs each delta relation against the rest via a standard `dR ⋈ V_rest` expansion. Closer to reference `PhysicalPlan` / `Fragments::compile_from_plan`.

Option A is narrower per-commit but composes awkwardly with the 4-fragment IVM. Option B is a larger single landing but matches reference shape.

Recommend scoping: start with parser acceptance + subscribe/one-off admission that constructs a new `MultiJoin` predicate, then land the runtime IVM as a follow-up slice if the admission landing reveals the evaluator shape.

### Live seams

1. `query/sql/parser.go::parseStatement` — currently only accepts a single `JOIN` clause. Needs to accept `JOIN ... JOIN ...` sequences and build a multi-relation binding map (`parseJoinClause` as currently structured is strictly two-relation).
2. `query/sql.JoinClause` — binary shape; needs either a nested form or replacement with a multi-relation statement node.
3. `protocol/handle_subscribe.go::compileSQLQueryString` — only branches on `stmt.Join != nil` and assumes binary join; needs multi-join branch.
4. `protocol/handle_oneoff.go::evaluateOneOffJoin` — binary fallback; needs multi-join executor or one-off-specific evaluator.
5. `subscription.Predicate` — no `MultiJoin` shape; `Join` is binary.
6. `subscription/delta_join.go::EvalJoinDeltaFragments` — fixed 4-fragment shape on binary `Join`; multi-join IVM needs a different fragment decomposition.
7. `subscription/placement.go` — `findJoin` / `joinEdgeFor` assume one join; multi-join pruning needs a walker.

### Suggested execution order

1. Re-read `reference/SpacetimeDB/crates/expr/src/check.rs` (accepted/rejected multi-join shapes) and `reference/SpacetimeDB/crates/subscription/src/lib.rs::Fragments::compile_from_plan` for shape expectations.
2. Decide between Option A (nested binary) and Option B (N-relation `MultiJoin`) on paper before writing code. Document the decision with file:line evidence in TECH-DEBT.md so future slices can audit it.
3. Add parser-level failing tests for the accepted three-way shape.
4. Land parser acceptance + subscribe/one-off admission that constructs a `MultiJoin` predicate rejected at a narrow runtime boundary.
5. Follow-up slice: runtime IVM + pruning + placement.

### Scope discipline

In scope:
- three-way equi-join with at most one self-alias
- matching the narrow reference-accepted shape above

Out of scope:
- four-way and deeper joins
- projection gap (LHS++RHS concat rows vs projected rows) — pre-existing in `subscription/eval.go::evalQuery`
- lag policy
- broader predicate syntax beyond the landed shapes

### Stop / escalate if

1. the parser needs to grow a binding tree that does not fit the current `relationBindings` / `byQualifier` shape without a redesign
2. a multi-relation runtime representation requires rewriting `EvalJoinDeltaFragments` in a way that would break distinct-table binary join semantics
3. the chosen option (A or B) proves materially inconsistent with the reference `PhysicalPlan` at a level that will force a second rewrite

If you stop, leave a grounded blocker report in this file naming the exact representation/runtime mismatch with file:line evidence.

### What not to do

- do not broad-audit the repo
- do not reopen already-landed slices without a real regression (especially Slices 10 / 10.5 / 11 / 12)
- do not silently broaden unrelated boolean or projection syntax
- do not lose the current guarantees around:
  - bare `SELECT *` on joins staying rejected
  - qualified `WHERE` requirements in landed join slices
  - alias-scope rejection behavior after aliasing
  - unaliased self-join rejection
  - same-alias-both-sides of ON rejection
  - self-join per-alias WHERE behavior landed in Slice 12

## Suggested verification commands

Targeted:
- `rtk go test ./query/sql -run '<target parser tests>' -v`
- `rtk go test ./query/sql ./protocol ./subscription -run '<target tests>' -v`
- `rtk go test ./query/sql ./protocol ./subscription -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed target shape was checked directly against reference material
- every newly accepted shape has focused tests
- parser and public runtime coverage both exist
- already-landed accepted shapes still pass (especially aliased self equi-join with and without WHERE)
- already-landed rejected shapes still reject where the reference requires rejection
- full suite still passes (current baseline: `Go test: 1084 passed in 10 packages`)
- docs and handoff reflect the new truth exactly

## Deliverables for the next session

Either:
- code + tests closing the next reference-backed multi-join slice

Or:
- a grounded blocker report naming the exact representation/runtime issue preventing a narrow landing

And in either case:
- update `TECH-DEBT.md`
- update `docs/current-status.md`
- update `docs/parity-phase0-ledger.md`
- update `NEXT_SESSION_HANDOFF.md`

## Final status snapshot right now

As of this handoff:
- targeted parity work continues to be real and cumulative
- `TD-142` is not finished
- Slice 10 (aliased self cross-join, no ON) landed
- Slice 10.5 (parser alias identity + aliased self equi-join parse acceptance) landed 2026-04-20
- Slice 11 (runtime alias identity in `subscription.Join` + filterless aliased self equi-join end-to-end) landed 2026-04-20
- Slice 12 (alias-aware WHERE on self-join) landed 2026-04-20
- the next realistic landing is **Slice 13**: multi-join, which requires a different (larger) representation change
