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
- Result: `Go test: 1339 passed in 10 packages`
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
- 128-bit integer column kinds `KindInt128` / `KindUint128` landed 2026-04-21 (first column-kind widening slice). BSATN primitive-type tags 13 / 14 (16 bytes LE), coerce promotes `LitInt` via `NewInt128FromInt64` / `NewUint128FromUint64`, subscription canonical hashing writes 16 bytes. Two reference `check.rs:360-370` rows now accept end-to-end (`i128 = 127`, `u128 = 127`). `i256` / `u256` / timestamp / array / product kinds remain unrealizable pending further widening slices.

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
- `SubscribeMulti` / `SubscribeSingle` variant split landed; one-QueryID-per-query-set grouping semantics now match reference, the four applied envelopes now carry reference-style `TotalHostExecutionDurationMicros`, and `SubscriptionError` now uses explicit optional `RequestID` / `QueryID` plus optional `TableID` on the wire. `TableID` is populated conservatively only when exactly one obvious table can be attached from the predicate/update surface. Remaining Phase 2 Slice 2 divergence is the still-open broader SQL/query-surface breadth around message families.
- `CallReducer.flags` now carries `FullUpdate=0` / `NoSuccessNotify=1`; remaining divergence is the still-open SQL/query-surface breadth around other message families
- one-off query wire shape now matches the reference Phase 2 target (`query_string` + opaque `MessageID []byte`)
- SQL-string handling now accepts multiple narrow parity-backed slices: same-table qualified WHERE columns, case-insensitive resolution of unquoted table/column identifiers against the registered schema (for example `SELECT * FROM USERS WHERE ID = 1 AND users.DISPLAY_NAME = 'alice'`), reference-style double-quoted identifiers — now explicitly pinned end-to-end for reserved-keyword and special-character table names too (for example `SELECT * FROM "Order" WHERE "id" = 7` and `SELECT * FROM "Balance$" WHERE "id" = 7`, matching the SQL reference's quoted-identifier guidance) — query-builder-style parenthesized WHERE predicates, alias-qualified `OR` predicates with mixed qualified/unqualified column references, 0x-prefixed and `X'..'` hex byte literals, float literals on the same narrow single-table / join-backed surfaces (for example `SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.f32 = 0.1`), the `:sender` caller-identity parameter on KindBytes columns across the narrow single-table shape (`SELECT * FROM s WHERE id = :sender`, `SELECT * FROM s WHERE bytes = :sender`), the aliased single-table shape (`SELECT * FROM s AS r WHERE r.bytes = :sender`), and the narrow join-backed shape as a qualified WHERE leaf on the joined relation (`SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE s.bytes = :sender`); rejected on non-bytes columns on all three surfaces so `WHERE arr = :sender` / `WHERE name = :sender` and the join-side equivalent `WHERE s.label = :sender` still surface as admission errors; reference type-check rejection shapes at `reference/SpacetimeDB/crates/expr/src/check.rs` lines 498-501 (`select * from t where u32 = 'str'`) and 502-504 (`select * from t where t.u32 = 1.3`) are now pinned at the coerce boundary and at the SubscribeSingle / OneOffQuery admission surfaces (2026-04-21 follow-through) so a string literal or a float literal against an integer column is an explicit admission error rather than an incidentally-enforced one, additional `check.rs` rejection shapes at lines 483-485 (`select * from r` / unknown FROM table), 491-493 (`select * from t where t.a = 1` / qualified unknown WHERE column), and 495-497 (`select * from t as r where r.a = 1` / alias-qualified unknown WHERE column) are now also pinned at the SubscribeSingle / OneOffQuery admission surfaces (2026-04-21 follow-through) as named parity contracts — all three already fire incidentally through `SchemaLookup.TableByName` / `rel.ts.Column` in `compileSQLQueryString` / `normalizeSQLFilterForRelations`, no runtime widening was required, and a further `check.rs` rejection bundle at lines 506-509 (`select * from t as r where t.u32 = 5` / base-table qualifier out of scope after alias), 510-513 (`select u32 from t` / bare column projection), 515-517 (`select * from t join s` / join without qualified projection), 519-521 (`select t.* from t join t` / self-join without aliases), 526-528 (`select t.* from t join s on t.u32 = r.u32 join s as r` / forward alias reference), 530-533 (`select * from t limit 5` / LIMIT clause), and 534-537 (`select t.* from t join s on t.u32 = s.u32 where bytes = 0xABCD` / unqualified WHERE column inside join) is also now pinned on the SubscribeSingle / OneOffQuery admission surfaces (2026-04-21 follow-through) — all seven are already rejected at the SQL parser boundary (`parseProjection`, `parseStatement` EOF-check, `parseStatement` joined-projection check, `parseJoinClause` self-join guard, `parseQualifiedColumnRef` / `parseComparison` via `resolveQualifier`, and `parseComparison` requireQualify), no runtime widening was required; `check.rs:523-525` (`select t.* from t join s on t.arr = s.arr` / product-value comparison) is not realizable against the Shunter column-kind enum (no array/product kind in `schema.ValueKind`) and is deliberately not pinned, and the reference valid-literal shape at `check.rs:297-300` (`select * from t where u32 = +1` / "Leading `+`") is now supported — the lexer extension mirrors the already-landed leading `-` handling on numeric literals, pinned at parser (`TestParseWhereLeadingPlusInt`), subscribe admission (`TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral`), and one-off (`TestHandleOneOffQuery_ParityLeadingPlusIntLiteral`), the reference `invalid_literals` bundle at `check.rs:382-401` (`u8 = -1` / negative integer on unsigned, `u8 = 1e3` / out-of-bounds scientific collapse, `u8 = 0.1` / float on unsigned, `u32 = 1e-3` / non-integral scientific on unsigned, `i32 = 1e-3` / non-integral scientific on signed), and `check.rs:360-370` `valid_literals_for_type` column-width breadth bundle (`i8 = 127`, `u8 = 127`, `i16 = 127`, `u16 = 127`, `i32 = 127`, `u32 = 127`, `i64 = 127`, `u64 = 127`, `f32 = 127`, `f64 = 127`; `i128`/`u128`/`i256`/`u256` not realizable against `schema.ValueKind`) are also pinned at the SubscribeSingle / OneOffQuery admission surfaces (2026-04-21 follow-through) — all five fire incidentally through `coerceUnsigned` / `coerceSigned` inside `compileSQLQueryString` / `parseQueryString`, no runtime widening was required, and the reference valid-literal bundle at `check.rs:302-328` (scientific notation `u32 = 1e3` / `u32 = 1E3`, integer-shaped scientific notation on a float column `f32 = 1e3`, negative exponent `f32 = 1e-3`, leading-dot float `f32 = .1`, and overflow-to-infinity `f32 = 1e40`) is now supported end-to-end (2026-04-21 follow-through) — the lexer consumes `[eE][+-]?[digits]+` exponents plus leading-dot floats (`tokenizeNumeric`), `parseNumericLiteral` collapses integer-valued scientific notation to `LitInt` so it binds to integer columns while non-integral or out-of-int64-range values stay `LitFloat`, and the coerce boundary now promotes `LitInt` to float columns (matching reference `parse_float` BigDecimal promotion); `f32 = 1e40` rides existing `types.NewFloat32(+Inf)` acceptance. Pinned at parser (`TestParseWhereScientificNotationUnsignedInteger`, `TestParseWhereScientificNotationCaseInsensitive`, `TestParseWhereScientificNotationNegativeExponent`, `TestParseWhereLeadingDotFloat`, `TestParseWhereScientificNotationOverflowFloat`, plus malformed-input rejections `TestParseWhereTrailingDotRejected` / `TestParseWhereBareExponentRejected` / `TestParseWhereTrailingIdentifierAfterNumericRejected`), coerce (`TestCoerceIntegerLiteralPromotesToFloat64`, `TestCoerceIntegerLiteralPromotesToFloat32`, `TestCoerceFloatLiteralOverflowsToFloat32Infinity`), subscribe admission (`TestHandleSubscribeSingle_ParityScientificNotationUnsignedInteger`, `TestHandleSubscribeSingle_ParityScientificNotationFloatNegativeExponent`, `TestHandleSubscribeSingle_ParityLeadingDotFloatLiteral`, `TestHandleSubscribeSingle_ParityScientificNotationOverflowInfinity`), and one-off (`TestHandleOneOffQuery_ParityScientificNotationUnsignedInteger`, `TestHandleOneOffQuery_ParityScientificNotationFloatNegativeExponent`, `TestHandleOneOffQuery_ParityLeadingDotFloatLiteral`, `TestHandleOneOffQuery_ParityScientificNotationOverflowInfinity`), and bare boolean `WHERE TRUE` predicates on the same narrow single-table / join-backed surfaces (for example `SELECT "Orders".* FROM "Orders" JOIN "Inventory" ON "Orders"."product_id" = "Inventory"."id" WHERE "Inventory"."quantity" < 10` and `SELECT "users".* FROM "users" JOIN "other" ON "users"."id" = "other"."uid" WHERE (("users"."id" = 1) AND ("users"."id" > 10))`), single-table alias / qualified-star forms such as `SELECT item.* FROM users AS item WHERE item.name = 'alice'`, ordered single-column comparisons using `<`, `<=`, `>`, and `>=`, non-equality comparisons using `<>` / `!=`, narrow same-table `OR` predicates such as `SELECT * FROM metrics WHERE score = 9 OR score = 11` routed coherently through parser, subscribe, and one-off query handling via a real predicate tree, and four narrow join-backed slices for two-table joins: left projection with a qualified joined-table filter such as `SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10`, right-side projection such as `SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id` including a pinned left-side-qualified filtered variant `WHERE o.id = 1`, cross-join projection such as `SELECT o.* FROM Orders o JOIN Inventory product` (no `ON`, no extra `WHERE`), and aliased self cross-join projection such as `SELECT a.* FROM t AS a JOIN t AS b` (no `ON`, no extra `WHERE`) lowered into the existing `subscription.CrossJoinProjected` with `Projected == Other`. Alias scope is now also reference-aligned: once a relation is aliased, the base table name is out of scope for qualified projection / `WHERE` references, and unaliased self cross-joins are rejected unless the self-join is explicitly aliased. Parser-level alias identity was extended (Slice 10.5, 2026-04-20) so aliased self equi-join SQL such as `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32` parses into an alias-aware `JoinClause`; Slice 11 (2026-04-20) lands the runtime counterpart — `subscription.Join` gained `LeftAlias`/`RightAlias` tags, `Tables()` dedupes when `Left == Right`, and the compile path accepts filterless aliased self equi-join end-to-end (initial query + 4-fragment IVM delta evaluation work correctly thanks to bag-semantic reconciliation). Slice 12 (2026-04-20) closes alias-aware WHERE on self-join: `subscription.ColEq`/`ColNe`/`ColRange` gained an `Alias uint8` tag, `MatchRowSide` routes each leaf to the side the user named (`join.LeftAlias` / `join.RightAlias`), and shapes such as `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1` (and the symmetric `WHERE b.id = 1`) now work through parser, subscribe admission, one-off execution, and post-commit IVM delta evaluation. Slice 13 (2026-04-20) closes the multi-join surface as a narrow parity-matched rejection: the reference subscription runtime at `reference/SpacetimeDB/crates/subscription/src/lib.rs:251` itself bails with `"Invalid number of tables in subscription: {N}"` for N≥3, so the externally observable reference behavior on three-way subscribes is also an admission-time error. Shunter's parser now explicitly rejects multi-way join chains with `multi-way join not supported: subscriptions are limited to at most two relations`, pinned at the parser (`TestParseRejectsMultiWayJoinChain`, `TestParseRejectsMultiWayJoinOnForwardReference`) and at the subscribe / one-off admission boundaries (`TestHandleSubscribeSingle_MultiWayJoinRejected`, `TestHandleOneOffQuery_MultiWayJoinRejected`) with both cross-chain and ON-chain sub-cases. No `subscription.MultiJoin` predicate was introduced; `subscription.Join` and the 4-fragment IVM remain binary, matching the reference runtime. Slice 14 (2026-04-20) closes the projection gap on join subscriptions: `subscription.Join` gained `ProjectRight bool` (zero-value projects LHS, true projects RHS), `subscription.SchemaLookup` grew a `ColumnCount(TableID) int` accessor, and `evalQuery` / `initialQuery` / `evaluateOneOffJoin` now slice the IVM's LHS++RHS concat fragments onto the SELECT side so subscribers receive rows shaped like one concrete table (matching reference `SubscriptionPlan::subscribed_table_id` at `reference/SpacetimeDB/crates/subscription/src/lib.rs:367`). Canonical hashing was extended to include `ProjectRight` so `SELECT lhs.*` and `SELECT rhs.*` hash distinctly. Parser-level `Statement.ProjectedAlias` preserves the user-typed qualifier (`a` vs `b` on self-joins) and the compile path at `protocol/handle_subscribe.go::joinProjectsRight` maps it to `ProjectRight` for both distinct-table and aliased self-joins. Pinned by `subscription/hash_test.go::TestQueryHashJoinProjectionDiffers`, `subscription/manager_test.go::TestRegisterJoinBootstrapFallsBackToLeftIndex` (re-asserted projected-width + TableID), `subscription/manager_test.go::TestRegisterJoinBootstrapProjectsRight`, `subscription/eval_test.go::TestEvalJoinSubscription` (re-asserted LHS projection width + TableID), `subscription/eval_test.go::TestEvalJoinSubscriptionProjectsRight`, `query/sql/parser_test.go::TestParseAliasedSelfEquiJoinProjectsRight`, `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_AliasedSelfEquiJoinProjectsRight`, and `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_AliasedSelfEquiJoinProjectsRight`.

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
- protocol connection lifecycle races and unsafe channel-close behavior (`watchReducerResponse` goroutine-leak sub-hazard closed 2026-04-20, see `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`; `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard closed 2026-04-21, see `docs/hardening-oi-004-sender-disconnect-context.md`; `superviseLifecycle` disconnect-ctx sub-hazard closed 2026-04-21, see `docs/hardening-oi-004-supervise-disconnect-context.md`; `ConnManager.CloseAll` disconnect-ctx sub-hazard closed 2026-04-21, see `docs/hardening-oi-004-closeall-disconnect-context.md` — closes the `Background`-rooted `Conn.Disconnect` call-site family: supervisor, sender overflow, and CloseAll now all derive a bounded ctx at the spawn point; `forwardReducerResponse` ctx / Done lifecycle sub-hazard closed 2026-04-21, see `docs/hardening-oi-004-forward-reducer-response-context.md` — executor-adapter forwarder now selects on `req.Done` (wired from `conn.closed`) so an executor that never feeds the internal respCh no longer leaks the forwarder goroutine; dispatch-handler ctx sub-hazard closed 2026-04-21, see `docs/hardening-oi-004-dispatch-handler-context.md` — `runDispatchLoop` now derives a `handlerCtx` that cancels on `c.closed`, so handler goroutines parked on `executor.SubmitWithContext` under a wedged executor exit promptly at conn teardown instead of leaking past disconnect; outbound-writer supervision sub-hazard closed 2026-04-21, see `docs/hardening-oi-004-outbound-writer-supervision.md` — the default supervisor now watches `outboundDone` as well as dispatch/keepalive, so a write-side websocket failure triggers the same bounded disconnect/reap path instead of leaving delivery-dead conns registered; broader `conn.go` / `lifecycle.go` / `keepalive.go` lifecycle concerns remain open)
- snapshot / read-view lifetime hazards (iterator-GC retention sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-snapshot-iter-retention.md`; iterator use-after-Close sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-snapshot-iter-useafterclose.md`; iterator mid-iter-close defense-in-depth sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`; subscription-seam read-view lifetime sub-hazard closed 2026-04-20, see `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`; `CommittedSnapshot.IndexSeek` BTree-alias escape closed 2026-04-20, see `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`; `StateView.SeekIndex` BTree-alias escape closed 2026-04-20, see `docs/hardening-oi-005-state-view-seekindex-aliasing.md`; `StateView.SeekIndexRange` BTree-alias escape closed 2026-04-20, see `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`; `StateView.ScanTable` iterator surface closed 2026-04-21, see `docs/hardening-oi-005-state-view-scan-aliasing.md` — the `StateView` iter-surface escape routes are now all pinned; `CommittedState.Table(id) *Table` raw-pointer contract pin closed 2026-04-21, see `docs/hardening-oi-005-committed-state-table-raw-pointer.md` — closes the last enumerated OI-005 sub-hazard; OI-005 stays open as a theme because the envelope rule is enforced by discipline and observational pins)
- subscription fan-out aliasing / cross-subscriber mutation risk (per-subscriber `Inserts` / `Deletes` slice-header aliasing sub-hazard closed 2026-04-20, see `docs/hardening-oi-006-fanout-aliasing.md`; row-payload sharing contract pin closed 2026-04-21, see `docs/hardening-oi-006-row-payload-sharing.md` — contract comments on `subscription/eval.go::evaluate`, `subscription/fanout_worker.go::FanOutSender`, and `protocol/fanout_adapter.go::encodeRows` name the read-only discipline and two pin tests assert backing-array identity plus the mutation-leak hazard shape; broader fanout assembly hazards in `fanout.go` / `fanout_worker.go` / `fanout_adapter.go` remain open)
- recovery / RowID sequencing sharp edges
- API and error-surface roughness that matters when embedding this as a real library
- pre-existing `subscription/delta_pool_test.go` sync.Pool flake class was cleaned up 2026-04-21 by dropping pointer-identity assertions and pinning only deterministic observable reuse behavior; `subscription/TestProjectedRowsBeforeAppendsDeletesAfterBagSubtraction` was also stabilized 2026-04-21 by rewriting the assertion to check bag semantics instead of depending on unordered `TableScan` iteration; the scheduler replay flake was closed 2026-04-21 by replacing the map-iteration-sensitive parity pin with deterministic helper-level coverage in `executor/scheduler_replay_test.go::TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting`, which still proves replay preserves scan order without sorting by `next_run_at_ns`

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
