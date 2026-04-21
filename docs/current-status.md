# Shunter current status

This file is the blunt answer to: what is actually here, how complete is it, and how close is it to SpacetimeDB-style behavior.

## Short version

Shunter is no longer a docs-only clean-room exercise.
It is a substantial Go implementation with working subsystem code, passing tests, and a focused hardening/parity ledger.

It is best described as:
- implementation-present
- architecture-proven enough to be worth keeping
- not parity-complete with SpacetimeDB
- not fully hardened even for private serious use yet

## Grounded evidence

As of the current audit pass:
- Broad verification: `rtk go test ./...`
- Result: `Go test: 1109 passed in 10 packages`
- Broad build verification: `rtk go build ./...`
- Result: `Go build: Success`
- Code inventory (live repo-wide count, excluding `reference/`): `228` Go files, `42217` lines of Go code
- the execution-order implementation slices that mattered most for commitlog, protocol, and fanout integration are already landed in code
- `TECH-DEBT.md` now tracks only the current open backlog instead of resolved audit history
- `docs/spacetimedb-parity-roadmap.md` and `docs/parity-phase0-ledger.md` continue to carry the live parity view and next-slice framing

## Completion by lens

### 1. Execution-order completion
Status: effectively complete

The earlier implementation-plan pass is effectively complete for the major execution-order slices that used to be tracked separately:
- commit log snapshot/recovery/compaction
- protocol server-message delivery / backpressure / reconnect work
- subscription fan-out integration

That means the question is no longer "is there code for the planned subsystems?"
The answer there is mostly yes.

### 2. Operational completeness
Status: substantial prototype / runtime

There is live code in:
- `types/`
- `auth/`
- `bsatn/`
- `schema/`
- `store/`
- `commitlog/`
- `executor/`
- `subscription/`
- `protocol/`
- `query/sql/`

The broad test suite passes, which is strong evidence that the repo is operationally real.
It is not proof of spec-completeness or parity.

### 3. Spec-completeness
Status: mostly implemented, still being reconciled

`TECH-DEBT.md` now shows one reality clearly: the old resolved audit ledger has been stripped out, and the remaining file is a compact list of live open parity/hardening issues.

So the repo is not sitting on obvious missing subsystem epics anymore.
Instead, it is in the harder phase: reconciling live behavior, edge cases, and public contracts.

### 4. SpacetimeDB-emulation closeness
Status: mixed — close in architecture, not close in all semantics

Shunter is clearly modeled on the same high-level architecture:
- in-memory store
- commit log + recovery
- serialized reducer execution
- subscription delta fan-out
- persistent protocol connections

But the live parity docs still record many intentional or currently accepted divergences. The clean-room effort is real; exact behavioral emulation is not finished.

## Where it is still materially different from SpacetimeDB

The parity roadmap and ledger matter because they answer the parity question directly and name the still-open externally visible gaps.
Current important differences include:

### Store / value model
- NaN handling differs
- no composite `Sum` / `Array` / nested `Product` value model
- single-column primary-key / auto-increment model is simpler than the reference
- changeset metadata is thinner than the reference

### Commit log / recovery
- Shunter's BSATN is a rewrite, not the same codec contract
- no offset index file; recovery is linear scan based
- single transaction per record
- replay-horizon / validated-prefix behavior closed 2026-04-20 (Phase 4 Slice 2, `P0-RECOVERY-001`): continue across valid segments, skip below horizon, stop at validated prefix on tail damage, fail-closed on first-commit-of-last-segment corruption, and attach tx/segment context to errors — all externally parity-close. Shunter's segment-level short-circuit (`replay.go:21-23`, skipping a whole segment when `LastTx <= fromTxID`) is pinned as an intentional divergence from reference per-commit `CommitInfo::adjust_initial_offset` (`src/commitlog.rs:834-845`) with the same externally visible outcome. See `docs/parity-p0-recovery-001-replay-horizon.md`.

### Executor / scheduling
- bounded inbox instead of unbounded queue
- server-side timestamping differences
- different fatality model for post-commit failures
- scheduled-reducer startup / firing ordering parity closed 2026-04-20 (Phase 3 Slice 1, `P0-SCHED-001`): existing replay / firing pins held as parity-close; intentional divergences (past-due iteration order, panic-retains-row) pinned with reference citations. Remaining deferrals (`fn_start`-clamped scheduling "now", one-shot panic deletion, intended-time past-due ordering) recorded with reference anchors in `docs/parity-p0-sched-001-startup-firing.md`.

### Subscription engine
- Go predicate builder instead of the reference SQL-oriented surface
- per-client outbound queue depth aligned to reference `CLIENT_CHANNEL_CAPACITY = 16 * 1024` (Phase 2 Slice 3, 2026-04-20); overflow still disconnects the client; Shunter sends a clean `1008 "send buffer full"` close frame, reference aborts the per-client tokio task — intentional mechanism divergence with matching externally visible outcome. See `docs/parity-phase2-slice3-lag-policy.md` and `P0-SUBSCRIPTION-001`.
- no row-level security / per-client predicate filtering
- some delivery metadata is threaded through different seams

### Protocol
- legacy dual-subprotocol admission remains as a compatibility deferral (`v1.bsatn.spacetimedb` preferred; `v1.bsatn.shunter` still accepted)
- brotli remains a reserved-but-unsupported compression tag even though the wire-byte numbering now matches the reference
- outgoing buffer default now matches reference `CLIENT_CHANNEL_CAPACITY = 16 * 1024` (Phase 2 Slice 3)
- `TransactionUpdate` heavy/light split and `UpdateStatus` outcome model match the Phase 1.5 parity target; caller metadata (`CallerIdentity`, `ReducerCall.ReducerName` / `ReducerID` / `Args`, `Timestamp`, `TotalHostExecutionDuration`) is now populated from the executor seam. `EnergyQuantaUsed` remains a permanent zero (no energy model)
- `SubscribeMsg` / `UnsubscribeMsg` and their response envelopes (`SubscribeApplied` / `UnsubscribeApplied` / `SubscriptionError`) now carry `QueryID` (reference `query_id: QueryId`); client/server naming asymmetry closed
- `SubscribeMulti` / `SubscribeSingle` variant split landed; one-QueryID-per-query-set grouping semantics now match reference. Remaining Phase 2 Slice 2 divergences: `TotalHostExecutionDurationMicros` on applied envelopes, `SubscriptionError.TableID` / optional-field shape, SQL-string form for `SubscribeMulti.Queries` (paired with Phase 2 Slice 1 deferral).
- `CallReducer.flags` now carries `FullUpdate=0` / `NoSuccessNotify=1`; remaining divergence is the still-open SQL/query-surface breadth around other message families
- one-off query wire shape now matches the reference Phase 2 target (`query_string` + opaque `MessageID []byte`)
- SQL-string handling now accepts ten narrow parity-backed slices: same-table qualified WHERE columns, case-insensitive resolution of unquoted table/column identifiers against the registered schema (for example `SELECT * FROM USERS WHERE ID = 1 AND users.DISPLAY_NAME = 'alice'`), single-table alias / qualified-star forms such as `SELECT item.* FROM users AS item WHERE item.name = 'alice'`, ordered single-column comparisons using `<`, `<=`, `>`, and `>=`, non-equality comparisons using `<>` / `!=`, narrow same-table `OR` predicates such as `SELECT * FROM metrics WHERE score = 9 OR score = 11` routed coherently through parser, subscribe, and one-off query handling via a real predicate tree, and four narrow join-backed slices for two-table joins: left projection with a qualified joined-table filter such as `SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10`, right-side projection such as `SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id` including a pinned left-side-qualified filtered variant `WHERE o.id = 1`, cross-join projection such as `SELECT o.* FROM Orders o JOIN Inventory product` (no `ON`, no extra `WHERE`), and aliased self cross-join projection such as `SELECT a.* FROM t AS a JOIN t AS b` (no `ON`, no extra `WHERE`) lowered into the existing `subscription.CrossJoinProjected` with `Projected == Other`. Alias scope is now also reference-aligned: once a relation is aliased, the base table name is out of scope for qualified projection / `WHERE` references, and unaliased self cross-joins are rejected unless the self-join is explicitly aliased. Parser-level alias identity was extended (Slice 10.5, 2026-04-20) so aliased self equi-join SQL such as `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32` parses into an alias-aware `JoinClause`; Slice 11 (2026-04-20) lands the runtime counterpart — `subscription.Join` gained `LeftAlias`/`RightAlias` tags, `Tables()` dedupes when `Left == Right`, and the compile path accepts filterless aliased self equi-join end-to-end (initial query + 4-fragment IVM delta evaluation work correctly thanks to bag-semantic reconciliation). Slice 12 (2026-04-20) closes alias-aware WHERE on self-join: `subscription.ColEq`/`ColNe`/`ColRange` gained an `Alias uint8` tag, `MatchRowSide` routes each leaf to the side the user named (`join.LeftAlias` / `join.RightAlias`), and shapes such as `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1` (and the symmetric `WHERE b.id = 1`) now work through parser, subscribe admission, one-off execution, and post-commit IVM delta evaluation. Slice 13 (2026-04-20) closes the multi-join surface as a narrow parity-matched rejection: the reference subscription runtime at `reference/SpacetimeDB/crates/subscription/src/lib.rs:251` itself bails with `"Invalid number of tables in subscription: {N}"` for N≥3, so the externally observable reference behavior on three-way subscribes is also an admission-time error. Shunter's parser now explicitly rejects multi-way join chains with `multi-way join not supported: subscriptions are limited to at most two relations`, pinned at the parser (`TestParseRejectsMultiWayJoinChain`, `TestParseRejectsMultiWayJoinOnForwardReference`) and at the subscribe / one-off admission boundaries (`TestHandleSubscribeSingle_MultiWayJoinRejected`, `TestHandleOneOffQuery_MultiWayJoinRejected`) with both cross-chain and ON-chain sub-cases. No `subscription.MultiJoin` predicate was introduced; `subscription.Join` and the 4-fragment IVM remain binary, matching the reference runtime. Slice 14 (2026-04-20) closes the projection gap on join subscriptions: `subscription.Join` gained `ProjectRight bool` (zero-value projects LHS, true projects RHS), `subscription.SchemaLookup` grew a `ColumnCount(TableID) int` accessor, and `evalQuery` / `initialQuery` / `evaluateOneOffJoin` now slice the IVM's LHS++RHS concat fragments onto the SELECT side so subscribers receive rows shaped like one concrete table (matching reference `SubscriptionPlan::subscribed_table_id` at `reference/SpacetimeDB/crates/subscription/src/lib.rs:367`). Canonical hashing was extended to include `ProjectRight` so `SELECT lhs.*` and `SELECT rhs.*` hash distinctly. Parser-level `Statement.ProjectedAlias` preserves the user-typed qualifier (`a` vs `b` on self-joins) and the compile path at `protocol/handle_subscribe.go::joinProjectsRight` maps it to `ProjectRight` for both distinct-table and aliased self-joins. Pinned by `subscription/hash_test.go::TestQueryHashJoinProjectionDiffers`, `subscription/manager_test.go::TestRegisterJoinBootstrapFallsBackToLeftIndex` (re-asserted projected-width + TableID), `subscription/manager_test.go::TestRegisterJoinBootstrapProjectsRight`, `subscription/eval_test.go::TestEvalJoinSubscription` (re-asserted LHS projection width + TableID), `subscription/eval_test.go::TestEvalJoinSubscriptionProjectsRight`, `query/sql/parser_test.go::TestParseAliasedSelfEquiJoinProjectsRight`, `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_AliasedSelfEquiJoinProjectsRight`, and `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_AliasedSelfEquiJoinProjectsRight`.

### Schema system
- runtime reflection model instead of compile-time macro model
- different lifecycle-reducer conventions
- a much simpler system-table and algebraic-type story

Bottom line: this is architecturally adjacent to SpacetimeDB, but not yet a close semantic clone.

## Open hardening / correctness picture

`TECH-DEBT.md` is still one of the clearest signals that the project is not done, but it now tracks only the live open backlog.

The hot spots are concentrated in:
- `executor/`
- `protocol/`
- `subscription/`
- `schema/`
- `store/`

The most serious remaining themes are not cosmetic. They include:
- protocol connection lifecycle races and unsafe channel-close behavior (`watchReducerResponse` goroutine-leak sub-hazard closed 2026-04-20, see `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`; broader `conn.go` / `lifecycle.go` / `outbound.go` / `sender.go` / `keepalive.go` lifecycle concerns remain open)
- snapshot / read-view lifetime hazards (iterator-GC retention sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-snapshot-iter-retention.md`; iterator use-after-Close sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-snapshot-iter-useafterclose.md`; iterator mid-iter-close defense-in-depth sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`; subscription-seam read-view lifetime sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`; `CommittedSnapshot.IndexSeek` BTree-alias escape closed 2026-04-20, see `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`; broader `state_view.go` / `committed_state.go` lifetime concerns remain open)
- subscription fan-out aliasing / cross-subscriber mutation risk (per-subscriber `Inserts` / `Deletes` slice-header aliasing sub-hazard closed 2026-04-20, see `docs/hardening-oi-006-fanout-aliasing.md`; row-payload sharing and broader fanout assembly hazards remain open)
- recovery / RowID sequencing sharp edges
- API and error-surface roughness that matters when embedding this as a real library

## Best current verdict

If the question is:

### "Is Shunter real?"
Yes.

### "Has the planned architecture been substantially implemented?"
Yes.

### "Is it close enough to SpacetimeDB that I should think of it as an approximate clone already?"
Only at the architectural level, not yet at the behavioral/protocol/parity level.

### "Is it done enough to stop auditing and trust blindly?"
No.

## What would move it meaningfully closer to SpacetimeDB

If the goal is closer emulation rather than generic polish, the highest-leverage work is:

1. Pick a parity target explicitly
- architecture only
- wire/protocol close-enough
- behavioral parity for reducer/subscription/runtime semantics

2. Close the protocol divergences first
- subprotocol naming/negotiation
- compression envelope behavior
- handshake / close semantics

3. Then close the first cross-seam delivery path
- reducer-result/update message shape
- caller/non-caller delivery ordering
- confirmed-read / durability semantics in the public flow

4. Then close the query/subscription-surface gaps
- query surface and predicate semantics
- subscription grouping / identity model
- lag / slow-client policy details

5. Tighten recovery/store semantics
- replay behavior
- row-id / sequence / TxID invariants
- snapshot invariants
- changeset metadata shape

6. Only then spend time on duplication/smells broadly
- because many of those will be churn if parity decisions are not locked first

## Practical recommendation

Treat the repo as a substantial private prototype that still needs a parity pass.
Do not treat it as either:
- a fake research artifact, or
- a finished SpacetimeDB clone

It is in the middle:
real enough to continue, incomplete enough that the next work should be parity-driven and deliberate.

For the concrete development driver, read `docs/spacetimedb-parity-roadmap.md`, then `docs/parity-phase0-ledger.md`.
