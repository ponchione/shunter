# TECH-DEBT

This file tracks open issues only.

Resolved and doc-drift-only audit entries were intentionally removed during the 2026-04-20 cleanup so this file can stay focused on live work. Use git history if you need the old resolved ledger.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup that should wait until parity decisions are locked

## Open issues

### OI-001: Protocol surface is still not wire-close enough to SpacetimeDB

Status: open
Severity: high

Summary:
- The reference subprotocol token is preferred, but the legacy `v1.bsatn.shunter` token is still accepted.
- Brotli remains a recognized-but-unsupported compression mode.
- Several message-family and envelope details remain intentionally divergent.

Why this matters:
- Client-visible protocol behavior is still one of the biggest blockers to serious parity claims.
- Even where semantics are close, the wire contract is still visibly Shunter-specific in important places.

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/compression.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A1
- `docs/parity-phase0-ledger.md` protocol conformance bucket

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- The SQL/query surface is still deliberately narrow, but reference-style double-quoted identifiers (including reserved-keyword and special-character table names such as `SELECT * FROM "Order" WHERE "id" = 7` and `SELECT * FROM "Balance$" WHERE "id" = 7`), query-builder-style parenthesized WHERE predicates, alias-qualified `OR` predicates with mixed qualified/unqualified column references, 0x-prefixed / `X'..'` hex byte literals, float literals on the currently supported single-table and narrow join-backed shapes, the `:sender` caller-identity parameter on KindBytes columns — on both the narrow single-table shape (`select * from s where id = :sender` / `select * from s where bytes = :sender`), the aliased single-table shape (`select * from s as r where r.bytes = :sender`), and the narrow join-backed shape as a qualified WHERE leaf on the joined relation (`select t.* from t join s on t.u32 = s.u32 where s.bytes = :sender`), rejected on any non-bytes column per reference `crates/expr/src/check.rs:487-488` on both single-table and join surfaces — and bare boolean `WHERE TRUE` predicates now work on the currently supported single-table and narrow join-backed shapes. Reference type-check rejection shapes `check.rs:498-501` (`select * from t where u32 = 'str'`) and `check.rs:502-504` (`select * from t where t.u32 = 1.3`) are now explicitly pinned (2026-04-21 follow-through) at the coerce seam (`TestCoerceRejectsStringLiteralOnUint32Column`, `TestCoerceRejectsFloatLiteralOnUint32Column`) and on both subscribe and one-off admission surfaces (`TestHandleSubscribeSingle_StringLiteralOnIntegerColumnRejected`, `TestHandleSubscribeSingle_FloatLiteralOnIntegerColumnRejected`, `TestHandleOneOffQuery_StringLiteralOnIntegerColumnRejected`, `TestHandleOneOffQuery_FloatLiteralOnIntegerColumnRejected`); no runtime widening was required because `query/sql/coerce.go` already rejected both shapes through its `mismatch()` branches — the pins promote the rejection from incidental to explicit parity contract. Reference type-check rejection shapes `check.rs:483-485` (`select * from r` / unknown FROM table), `check.rs:491-493` (`select * from t where t.a = 1` / qualified unknown WHERE column), and `check.rs:495-497` (`select * from t as r where r.a = 1` / alias-qualified unknown WHERE column) are now explicitly pinned (2026-04-21 follow-through) at the SubscribeSingle and OneOffQuery admission surfaces (`TestHandleSubscribeSingle_ParityUnknownTableRejected`, `TestHandleSubscribeSingle_ParityUnknownColumnRejected`, `TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected`, `TestHandleOneOffQuery_ParityUnknownTableRejected`, `TestHandleOneOffQuery_ParityUnknownColumnRejected`, `TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected`); no runtime widening was required — all three shapes fire through `SchemaLookup.TableByName` / `rel.ts.Column` in `compileSQLQueryString` / `normalizeSQLFilterForRelations` (`protocol/handle_subscribe.go:152-154,250-253`), the pins promote the rejection from incidental to explicit parity contract. A further `check.rs` rejection bundle at lines 506-509 (`select * from t as r where t.u32 = 5` / base-table qualifier out of scope after alias), 510-513 (`select u32 from t` / bare column projection), 515-517 (`select * from t join s` / join without qualified projection), 519-521 (`select t.* from t join t` / self-join without aliases), 526-528 (`select t.* from t join s on t.u32 = r.u32 join s as r` / forward alias reference), 530-533 (`select * from t limit 5` / LIMIT clause), and 534-537 (`select t.* from t join s on t.u32 = s.u32 where bytes = 0xABCD` / unqualified WHERE column inside join) is also now explicitly pinned (2026-04-21 follow-through) at the SubscribeSingle and OneOffQuery admission surfaces (`TestHandleSubscribeSingle_ParityBaseTableQualifierAfterAliasRejected`, `TestHandleSubscribeSingle_ParityBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected`, `TestHandleSubscribeSingle_ParitySelfJoinWithoutAliasesRejected`, `TestHandleSubscribeSingle_ParityForwardAliasReferenceRejected`, `TestHandleSubscribeSingle_ParityLimitClauseRejected`, `TestHandleSubscribeSingle_ParityUnqualifiedWhereInJoinRejected`, `TestHandleOneOffQuery_ParityBaseTableQualifierAfterAliasRejected`, `TestHandleOneOffQuery_ParityBareColumnProjectionRejected`, `TestHandleOneOffQuery_ParityJoinWithoutQualifiedProjectionRejected`, `TestHandleOneOffQuery_ParitySelfJoinWithoutAliasesRejected`, `TestHandleOneOffQuery_ParityForwardAliasReferenceRejected`, `TestHandleOneOffQuery_ParityLimitClauseRejected`, `TestHandleOneOffQuery_ParityUnqualifiedWhereInJoinRejected`); no runtime widening was required — all seven shapes already reject at the SQL parser boundary (`parseProjection`, `parseStatement` EOF-check, `parseStatement` joined-projection guard, `parseJoinClause` self-join guard, `parseQualifiedColumnRef` / `parseComparison` via `resolveQualifier`, and `parseComparison` requireQualify under a join binding). `check.rs:523-525` (`select t.* from t join s on t.arr = s.arr` / product-value comparison) is not realizable against the Shunter column-kind enum (no array/product kind in `schema.ValueKind`) and is deliberately not pinned. Reference valid-literal shape `check.rs:297-300` (`select * from t where u32 = +1` / "Leading `+`") is now supported — the numeric-literal lexer accepts leading `+` the same way it accepts leading `-` (one-line widening), pinned at parser (`TestParseWhereLeadingPlusInt`), subscribe admission (`TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral`), and one-off (`TestHandleOneOffQuery_ParityLeadingPlusIntLiteral`). Reference valid-literal bundle `check.rs:302-328` (scientific notation `u32 = 1e3` / `u32 = 1E3`, integer-shaped scientific notation on a float column `f32 = 1e3`, negative exponent `f32 = 1e-3`, leading-dot float `f32 = .1`, and overflow-to-infinity `f32 = 1e40`) is now supported end-to-end (2026-04-21 follow-through): the numeric lexer grew a `[eE][+-]?[digits]+` exponent tail and a leading-dot entry via a new `tokenizeNumeric` helper, `parseNumericLiteral` collapses integer-valued finite results within int64 range to `LitInt` (mirroring the reference BigDecimal `is_integer` filter) while non-integral / out-of-range values stay `LitFloat`, and the coerce boundary now promotes `LitInt` to `KindFloat32` / `KindFloat64` columns (mirroring reference `parse_float` BigDecimal promotion). Malformed shapes `1.`, `1e`, `1efoo` remain rejected. Pinned at parser (`TestParseWhereScientificNotationUnsignedInteger`, `TestParseWhereScientificNotationCaseInsensitive`, `TestParseWhereScientificNotationNegativeExponent`, `TestParseWhereLeadingDotFloat`, `TestParseWhereScientificNotationOverflowFloat`, `TestParseWhereTrailingDotRejected`, `TestParseWhereBareExponentRejected`, `TestParseWhereTrailingIdentifierAfterNumericRejected`), coerce (`TestCoerceIntegerLiteralPromotesToFloat64`, `TestCoerceIntegerLiteralPromotesToFloat32`, `TestCoerceFloatLiteralOverflowsToFloat32Infinity`), subscribe admission (`TestHandleSubscribeSingle_ParityScientificNotationUnsignedInteger`, `TestHandleSubscribeSingle_ParityScientificNotationFloatNegativeExponent`, `TestHandleSubscribeSingle_ParityLeadingDotFloatLiteral`, `TestHandleSubscribeSingle_ParityScientificNotationOverflowInfinity`), and one-off (`TestHandleOneOffQuery_ParityScientificNotationUnsignedInteger`, `TestHandleOneOffQuery_ParityScientificNotationFloatNegativeExponent`, `TestHandleOneOffQuery_ParityLeadingDotFloatLiteral`, `TestHandleOneOffQuery_ParityScientificNotationOverflowInfinity`). Remaining `check.rs:284-332` valid-literal shapes not supported: 128/256-bit integer (`i128`, `u128`, `i256`, `u256`) and timestamp column kinds — not realizable against `schema.ValueKind`. Reference `invalid_literals` bundle `check.rs:382-401` (`u8 = -1`, `u8 = 1e3`, `u8 = 0.1`, `u32 = 1e-3`, `i32 = 1e-3`) is now explicitly pinned (2026-04-21 follow-through) at the SubscribeSingle and OneOffQuery admission surfaces (`TestHandleSubscribeSingle_ParityInvalidLiteralNegativeIntOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralScientificOverflowRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralFloatOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnSignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeIntOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralScientificOverflowRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralFloatOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnSignedRejected`); no runtime widening was required — all five shapes already reject incidentally through `coerceUnsigned` / `coerceSigned` in `query/sql/coerce.go` (negative-int branch, out-of-range branch, and non-LitInt-on-integer-column branches); the pins promote the rejection from incidental to explicit parity contract. Reference `valid_literals_for_type` column-width breadth test at `check.rs:360-370` is now drained at the SubscribeSingle / OneOffQuery admission surfaces for every realizable Shunter column kind (`i8 = 127`, `u8 = 127`, `i16 = 127`, `u16 = 127`, `i32 = 127`, `u32 = 127`, `i64 = 127`, `u64 = 127`, `f32 = 127`, `f64 = 127`; `i128`/`u128`/`i256`/`u256` not realizable against `schema.ValueKind`) via table-driven subtest bundles `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` and `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth`; no runtime widening was required — all 10 widths ride existing `coerceSigned` / `coerceUnsigned` / `KindFloat32`-`KindFloat64` LitInt-promotion branches.
- Row-level security / per-client filtering remains absent.
- Join projection semantics now emit projected-width rows end-to-end (TD-142 Slice 14, 2026-04-20): `subscription.Join` carries `ProjectRight bool`, the canonical hash distinguishes `SELECT lhs.*` from `SELECT rhs.*`, and `evalQuery` / `initialQuery` / `evaluateOneOffJoin` all slice the LHS++RHS IVM fragments onto the SELECT side.
- Lag / slow-client policy closed 2026-04-20 (Phase 2 Slice 3): `DefaultOutgoingBufferMessages` aligned to reference `CLIENT_CHANNEL_CAPACITY = 16 * 1024`; overflow-disconnect semantics preserved; close-frame mechanism (`1008 "send buffer full"`) retained as an intentional divergence from the reference `abort_handle.abort()` path. See `docs/parity-phase2-slice3-lag-policy.md` and `docs/parity-phase0-ledger.md` row `P0-SUBSCRIPTION-001`.
- Scheduled-reducer startup / firing ordering closed 2026-04-20 (Phase 3 Slice 1, `P0-SCHED-001`): existing startup-replay / firing pins kept as parity-close; new parity pins lock the intentional divergences (past-due iteration order, panic-retains-row) with reference citations. Remaining deferrals recorded with reference anchors in `docs/parity-p0-sched-001-startup-firing.md`.
- Reference subscription-parser `unsupported` bundle `reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs:157-168` (DML, empty, whitespace-only, DISTINCT projection, subquery-in-FROM) is now explicitly pinned (2026-04-21 follow-through) at the SubscribeSingle and OneOffQuery admission surfaces (`TestHandleSubscribeSingle_ParityDMLStatementRejected`, `TestHandleSubscribeSingle_ParityEmptyStatementRejected`, `TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected`, `TestHandleSubscribeSingle_ParityDistinctProjectionRejected`, `TestHandleSubscribeSingle_ParitySubqueryInFromRejected`, and the five matching `TestHandleOneOffQuery_Parity*` pins). No runtime widening was required — all five shapes reject at the SELECT-only parser boundary (`expectKeyword("SELECT")` on DML / empty / whitespace, `parseProjection` on DISTINCT, `parseStatement` identifier-after-FROM on subquery).
- Reference `parse_sql` rejection bundle `reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:411-436` (SELECT-level shapes only: `select 1` / FROM-required, `select a from s.t` / multi-part table, `select * from t where a = B'1010'` / bit-string literal, `select a.*, b, c from t` / wildcard with bare columns, `select * from t order by a limit b` / ORDER BY + LIMIT expression, `select a, count(*) from t group by a` / aggregate + GROUP BY, `select a.* from t as a, s as b where a.id = b.id and b.c = 1` / implicit comma join, `select t.* from t join s on int = u32` / unqualified JOIN ON vars) is now explicitly pinned (2026-04-21 follow-through) at the SubscribeSingle and OneOffQuery admission surfaces (`TestHandleSubscribeSingle_ParitySqlUnsupportedSelectLiteralWithoutFromRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedMultiPartTableNameRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedBitStringLiteralRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedWildcardWithBareColumnsRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedOrderByWithLimitExpressionRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedAggregateWithGroupByRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedImplicitCommaJoinRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected`, and the eight matching `TestHandleOneOffQuery_ParitySqlUnsupported*` pins). No runtime widening was required — all eight shapes reject at the SELECT-only parser boundary (`parseProjection` rejects integer literal / bare column / multi-item projection; `parseStatement` expects `FROM` after projection and the EOF guard rejects trailing `,` / `ORDER`; `parseLiteral` rejects the `B` identifier on the RHS; `parseQualifiedColumnRef` rejects bare identifiers in JOIN ON). The two DML shapes in that reference block (`update ... join ... set`, `update t set a = 1 from s where ...`) are covered by the existing `ParityDMLStatementRejected` pins.
- Reference `parse_sql::invalid` pure-syntax rejection bundle `reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:457-476` (`select from t` / Empty SELECT, `select a from where b = 1` / Empty FROM, `select a from t where` / Empty WHERE, `select a, count(*) from t group by` / Empty GROUP BY, `select count(*) from t` / Aggregate without alias; empty-string and whitespace-only already covered by the `sub.rs::unsupported` pins above) is now explicitly pinned (2026-04-21 follow-through) at the SubscribeSingle and OneOffQuery admission surfaces (`TestHandleSubscribeSingle_ParitySqlInvalidEmptySelectRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyFromRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyWhereRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyGroupByRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidAggregateWithoutAliasRejected`, and the five matching `TestHandleOneOffQuery_ParitySqlInvalid*` pins). No runtime widening was required — all five shapes reject inside `parseProjection` at `query/sql/parser.go:553-572` before the reference-style empty-FROM / empty-WHERE / empty-GROUP BY / aggregate-without-alias conditions are ever checked, because Shunter's SELECT-only parser requires the projection to be `*` or `<qualifier>.*`.
- Intentional divergence recorded: Shunter unifies subscription and one-off admission behind one subscription-shape contract, while reference splits `parse_and_type_sub` (narrow) and `parse_and_type_sql` (wider, accepts bare / mixed projection and LIMIT per `statement.rs:521-551`). Widening one-off to ref SQL semantics would require LIMIT runtime support and reversing already-landed pins — out of scope for a narrow slice. Recorded in `docs/parity-phase0-ledger.md` under the `sub.rs::unsupported` pin-bundle paragraph.
- Column-kind widening Slice 1 (2026-04-21): `KindInt128` / `KindUint128` now realizable end-to-end. `types.Value` grew `hi128`/`lo128` storage slots; BSATN tags 13 / 14 carry 16 bytes LE; `query/sql/coerce.go` promotes `LitInt` via `NewInt128FromInt64` / `NewUint128FromUint64` (int64 always fits); subscription canonical hashing writes 16 bytes (hi then lo, big-endian); autoincrement remains 64-bit-only (`schema.AutoIncrementBounds` returns `ok=false` for 128-bit); `schema.GoTypeToValueKind` rejects 128-bit (no native Go type). Unlocks `check.rs:360-370` rows `i128 = 127` and `u128 = 127`, pinned by extending `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` / `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth` with two new subtests each; plus dedicated `TestHandleSubscribeSingle_ParityUint128NegativeRejected` / `TestHandleOneOffQuery_ParityUint128NegativeRejected` pins for the `invalid_literals` extension.
- Column-kind widening Slice 2 (2026-04-21): `KindInt256` / `KindUint256` now realizable end-to-end without the BigDecimal literal path. `types.Value` grew a `w256 [4]uint64` slot (index 0 most-significant, signed for Int256; index 3 least-significant); BSATN tags 15 / 16 carry 32 bytes LE with the least-significant word first; `query/sql/coerce.go` promotes `LitInt` via `NewInt256FromInt64` / `NewUint256FromUint64` (int64 always fits, negative-LitInt rejected on Uint256); subscription canonical hashing writes 32 bytes big-endian (word 0 through word 3); autoincrement remains 64-bit-only; `schema.GoTypeToValueKind` rejects 256-bit (no native Go type). Unlocks `check.rs:360-370` rows `i256 = 127` and `u256 = 127`, pinned by extending the same `ParityValidLiteralOnEachIntegerWidth` bundles with two more subtests each; plus dedicated `TestHandleSubscribeSingle_ParityUint256NegativeRejected` / `TestHandleOneOffQuery_ParityUint256NegativeRejected` pins.
- Column-kind widening Slice 3 (2026-04-21): `KindTimestamp` now realizable end-to-end. `types.Value` reuses the existing `i64` slot as microseconds since the Unix epoch; BSATN tag 17 carries 8 bytes LE; `query/sql/coerce.go` accepts `LitString` on a `KindTimestamp` column and parses RFC3339 shapes with either `T` or space separator, with or without fractional seconds up to nanoseconds (truncated to micros via `time.UnixMicro`, matching reference `chrono::timestamp_micros`), and with either `Z` or a numeric offset; subscription canonical hashing writes 8 bytes big-endian with the `KindTimestamp` tag separating identical raw micros from an `Int64` payload; autoincrement excludes Timestamp; `schema.GoTypeToValueKind` keeps rejecting Timestamp (no native Go auto-map). Unlocks `check.rs:334-352` rows `ts = '2025-02-10T15:45:30Z'`, `ts = '2025-02-10T15:45:30.123Z'`, `ts = '2025-02-10T15:45:30.123456789Z'`, `ts = '2025-02-10 15:45:30+02:00'`, and `ts = '2025-02-10 15:45:30.123+02:00'`, pinned by `TestHandleSubscribeSingle_ParityTimestampLiteralAccepted` and `TestHandleOneOffQuery_ParityTimestampLiteralAccepted` (5 subtests each); plus malformed-literal rejection pins `TestHandleSubscribeSingle_ParityTimestampMalformedRejected` / `TestHandleOneOffQuery_ParityTimestampMalformedRejected`.
- Column-kind widening Slice 4 (2026-04-21): BigDecimal literal path. `query/sql/parser.go` gained a `LitBigInt` literal carrying an arbitrary-precision `*big.Int`; `parseNumericLiteral` routes `.eE` bodies through `big.Rat.SetString` so integer-valued scientific shapes promote to `LitBigInt` when they overflow int64 (non-integer rationals fall back to `LitFloat` via `strconv.ParseFloat`). Plain integer overflow (no `.eE`) also promotes to `LitBigInt`. Coerce widened `KindInt128` / `KindUint128` / `KindInt256` / `KindUint256` to accept `LitBigInt` — the helpers decompose the big.Int into 2 / 4 uint64 words via `FillBytes` with two's-complement materialization for negatives and range-check against width bounds. `KindFloat32` / `KindFloat64` accept `LitBigInt` via `new(big.Float).SetInt(x).Float64()` so `f32 = 1e40 → +Inf` behavior is preserved after the parser promotes `1e40` to `LitBigInt`. Unlocks `check.rs:330-332` row `u256 = 1e40`, pinned by `TestHandleSubscribeSingle_ParityValidLiteralU256Scientific` / `TestHandleOneOffQuery_ParityValidLiteralU256Scientific`. Coerce pins: `TestCoerceBigIntLiteralToUint256`, `TestCoerceBigIntLiteralToInt256`, `TestCoerceBigIntLiteralOverflowsUint128`, `TestCoerceBigIntLiteralOverflowsUint256`, `TestCoerceNegativeBigIntRejectedOnUint256`, `TestCoerceBigIntLiteralOnInt64Rejected`, `TestCoerceBigIntLiteralToFloat32Infinity`, `TestCoerceBigIntLiteralToFloat64`, `TestCoerceBigIntLiteralOnStringColumnRejected`. Parser pins: `TestParseWhereScientificNotationOverflowBigInt` (supersedes the earlier `OverflowFloat` pin), `TestParseWhereIntegerOverflowPromotesToBigInt`. With this slice the `check.rs:284-332` `valid_literals` block is drained for every shape realizable against `schema.ValueKind`.
- Column-kind widening Slice 5 (2026-04-21): `KindArrayString` (narrow — string element type only). `types.Value` grew a `strArr []string` slot with defensive-copy constructor/accessor; BSATN tag 18 carries u32 LE count + per-element (u32 LE length + utf8 bytes); `query/sql/coerce.go` rejects every scalar literal shape on `KindArrayString` (no array literal grammar exists; `:sender` already rejects non-bytes); subscription canonical hashing writes kind tag + u32 BE count + per-element (u32 BE length + utf8 bytes); `protocol/handle_subscribe.go::compileSQLQueryString` refuses to build `subscription.Join` when either ON column is an array kind via a new `isArrayKind` helper. Autoincrement remains integer-only (`schema.AutoIncrementBounds` returns `ok=false` for array kind). `schema.GoTypeToValueKind` continues to reject generic slices other than `[]byte`. Unlocks reference rejection shapes `check.rs:487-489` (`select * from t where arr = :sender` / "The :sender param is an identity") and `check.rs:523-525` (`select t.* from t join s on t.arr = s.arr` / "Product values are not comparable") as positive parity contracts at the protocol admission surface, pinned by `TestHandle{SubscribeSingle,OneOffQuery}_ParityArraySenderRejected` and `TestHandle{SubscribeSingle,OneOffQuery}_ParityArrayJoinOnRejected`. Types pins: `TestRoundTripArrayString`, `TestArrayStringDefensiveCopyOn{Construct,Access}`, `TestEqualArrayString`, `TestCompareArrayString`, `TestAccessorArrayStringPanicsOnWrongKind`. BSATN pins: `TestEncodedValueSizeArrayString`, `TestEncodeArrayStringLittleEndianLayout`, `TestDecodeArrayStringRejectsInvalidUTF8`, plus 5 new entries in `TestValueRoundTrip`. Coerce pins: `TestCoerceSenderRejectsArrayStringColumn`, `TestCoerceLiteralsRejectedOnArrayStringColumn`. Subscription hash pins: `TestQueryHashArrayStringVsString`, `TestQueryHashArrayStringDiffersByPayload`, plus 3 new entries in `TestQueryHashAllKindsRoundTrip`. Remaining column-kind widening: non-String array element types, product kinds.
- Remaining anchors: broader SQL/query-surface parity and RLS. See `docs/parity-phase0-ledger.md`.

Why this matters:
- The system can look architecturally right while still behaving differently under realistic subscription workloads.
- Query-surface limitations still cap how close clients can get to reference behavior.

Primary code surfaces:
- `query/sql/parser.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/eval.go`
- `subscription/manager.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `executor/executor.go`
- `executor/scheduler.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A2
- `docs/parity-phase0-ledger.md` scheduler / recovery parity scenarios

### OI-003: Recovery and store semantics still differ in user-visible ways

Status: open
Severity: high

Summary:
- Value-model and changeset semantics remain simpler than the reference.
- Commitlog/recovery behavior is intentionally rewritten rather than format-compatible.
- Replay tolerance, sequencing, and snapshot/recovery behavior still need parity decisions and follow-through.

Why this matters:
- Storage and recovery semantics are central to the operational-replacement claim.
- Sequencing and replay mismatches are the kind of differences users feel only after a crash or restart.

Primary code surfaces:
- `types/`
- `bsatn/encode.go`
- `bsatn/decode.go`
- `store/commit.go`
- `store/recovery.go`
- `store/snapshot.go`
- `store/transaction.go`
- `commitlog/changeset_codec.go`
- `commitlog/segment.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/snapshot_io.go`
- `commitlog/compaction.go`
- `executor/executor.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A3
- `docs/parity-phase0-ledger.md` recovery parity scenarios

### OI-004: Protocol lifecycle still needs hardening around goroutine ownership and shutdown

Status: open
Severity: high

Summary:
- Connection lifecycle code still relies on detached background goroutines and shutdown paths that are harder to reason about than a single owned lifecycle context.
- This is the main correctness/hardening theme still called out by the current-status and parity docs.
- `watchReducerResponse` goroutine-leak sub-hazard closed 2026-04-20: `protocol/async_responses.go::watchReducerResponse` previously blocked unconditionally on `<-respCh`, so if the executor accepted a CallReducer but never sent on or closed the response channel (executor crash mid-commit, hung reducer, engine shutdown with in-flight work) the goroutine leaked for the lifetime of the process and held its `*Conn` alive past disconnect. The goroutine body is now split into `runReducerResponseWatcher` and selects on both `respCh` and `conn.closed`, tying the watcher to the owning `Conn`'s SPEC-005 §5.3 teardown. Pinned by `protocol/async_responses_test.go::{TestWatchReducerResponseExitsOnConnClose, TestWatchReducerResponseDeliversOnRespCh, TestWatchReducerResponseExitsOnRespChClose}`. See `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`.
- `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard closed 2026-04-21: the SPEC-005 §10.1 overflow path in `protocol/sender.go:106` previously spawned `go conn.Disconnect(context.Background(), ...)`. `Conn.Disconnect` threads the ctx into `inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` (both honor ctx cancellation via the adapter's select arm in `executor/protocol_inbox_adapter.go:58-63` and `awaitReducerStatus` at `executor/protocol_inbox_adapter.go:133-145`), so with a Background ctx either hang — executor dispatch deadlock, inbox-drain stall, executor crash waiting on never-fed respCh — left the detached goroutine holding the `*Conn` and its transitive state forever. `closeOnce.Do` had latched but the body never reached `close(c.closed)`, so dispatch / keepalive / write loops for that conn could not exit either. The overflow path now derives a bounded ctx from `context.WithTimeout(context.Background(), conn.opts.DisconnectTimeout)` (default 5 s) and defers its cancel; a hung inbox call returns `ctx.Err()` after the timeout and Disconnect proceeds to steps 3-5 of the SPEC-005 §5.3 teardown unconditionally. Pinned by `protocol/sender_disconnect_timeout_test.go::{TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang, TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK}`. See `docs/hardening-oi-004-sender-disconnect-context.md`.
- `superviseLifecycle` disconnect-ctx sub-hazard closed 2026-04-21: the per-connection supervisor at `protocol/disconnect.go::superviseLifecycle` received `context.Background()` hardcoded by the only production call site (`protocol/upgrade.go:211`) and forwarded it directly into `c.Disconnect(ctx, ...)`. Same hang class as the overflow site: a hung `inbox.DisconnectClientSubscriptions` or `inbox.OnDisconnect` left the supervisor goroutine (and therefore the `*Conn` via `closeOnce` latched without `close(c.closed)`) pinned for the process lifetime. Supervisor now derives `context.WithTimeout(ctx, c.opts.DisconnectTimeout)` (reuses the existing 5 s default) and defers its cancel before calling `Disconnect`; Disconnect still proceeds to steps 3-5 of the teardown after the bounded step 1/2 returns `ctx.Err()`. Pinned by `protocol/supervise_disconnect_timeout_test.go::{TestSuperviseLifecycleBoundsDisconnectOnInboxHang, TestSuperviseLifecycleDeliversOnInboxOK}`. See `docs/hardening-oi-004-supervise-disconnect-context.md`.
- `ConnManager.CloseAll` disconnect-ctx sub-hazard closed 2026-04-21: `protocol/conn.go:137` is the graceful-shutdown entry point (SPEC-005 §11.1); each per-conn goroutine called `c.Disconnect(ctx, ...)` with the caller-supplied ctx threaded directly through. The caller contract was unpinned — a Background-rooted caller (tests today, future OI-008 server lifecycle) could pin the shutdown `WaitGroup` indefinitely when any single inbox seam hung. Closes the `Background`-rooted `Conn.Disconnect` call-site family: supervisor, sender overflow, and CloseAll now all derive a bounded ctx at the spawn point. Each per-conn goroutine now derives `context.WithTimeout(ctx, c.opts.DisconnectTimeout)` (reuses the existing 5 s default) and defers cancel; the outer ctx is still honored (a cancellation propagates through the derived ctx immediately) but a Background root caps per-conn teardown at `DisconnectTimeout`. Pinned by `protocol/closeall_disconnect_timeout_test.go::{TestCloseAllBoundsDisconnectOnInboxHang, TestCloseAllDeliversOnInboxOK}`. See `docs/hardening-oi-004-closeall-disconnect-context.md`.
- `forwardReducerResponse` ctx / Done lifecycle sub-hazard closed 2026-04-21: `executor/protocol_inbox_adapter.go:128` spawns `go a.forwardReducerResponse(ctx, req, respCh)` with the dispatch ctx hardcoded to `context.Background()` at `protocol/upgrade.go:201`. The forwarder previously selected only on `<-respCh` and `<-ctx.Done()`; with a Background root, an executor that accepted the CallReducer but never fed the internal `ProtocolCallReducerResponse` channel (crash mid-commit, hung reducer, engine shutdown mid-flight) leaked the goroutine forever and held the owning `*Conn` alive past disconnect. Direct analog to the 2026-04-20 `watchReducerResponse` hardening on the protocol-side watcher. `protocol.CallReducerRequest` grew a `Done <-chan struct{}` field, `handleCallReducer` wires `Done: conn.closed`, and `forwardReducerResponse` adds a third select arm `case <-req.Done:`. A nil Done blocks forever on its arm, preserving pre-wire behavior for callers that do not attach a lifecycle signal. Pinned by `executor/forward_reducer_response_done_test.go::{TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs, TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneAlreadyClosed}`. See `docs/hardening-oi-004-forward-reducer-response-context.md`.
- dispatch-handler ctx sub-hazard closed 2026-04-21: `protocol/dispatch.go:192` spawned one goroutine per inbound message, each capturing the outer ctx received by `runDispatchLoop` (hardcoded `context.Background()` at `protocol/upgrade.go:201`). Every handler except `handleOneOffQuery` forwarded that ctx into `ExecutorInbox.CallReducer` / `RegisterSubscriptionSet` / `UnregisterSubscriptionSet`, which route through `executor.SubmitWithContext` — the default non-reject path blocks on `e.inbox <- cmd` until ctx cancels. With Background root and a wedged executor (inbox full from a hung reducer or engine stall), handler goroutines parked on the submit arm indefinitely, holding their `inflightSem` slot and captured `*Conn` past disconnect. Disconnect's bounded-ctx sub-slices protect the teardown path but do not unblock request-side handler goroutines. Request-side analog to the earlier-same-day `forwardReducerResponse` response-side slice. `runDispatchLoop` now derives a `handlerCtx` from the outer ctx with an additional watcher that cancels it on `c.closed`; every handler closure captures `handlerCtx` in place of `ctx`. Read ctx is untouched. Pinned by `protocol/dispatch_handler_ctx_test.go::{TestDispatchLoop_HandlerCtxCancelsOnConnClose, TestDispatchLoop_HandlerCtxCancelsOnOuterCtx}`. See `docs/hardening-oi-004-dispatch-handler-context.md`.
- outbound-writer supervision sub-hazard closed 2026-04-21: `protocol/upgrade.go:208-211` spawned `runOutboundWriter` beside `runDispatchLoop`, `runKeepalive`, and `superviseLifecycle`, but the supervisor only watched dispatch/keepalive completion. If the outbound writer exited first on a write-side WebSocket failure (`protocol/outbound.go:29` / `:37`), no disconnect was driven until some other goroutine happened to exit; `ConnManager` retained the `*Conn`, subscriptions were not reaped, and `c.closed` stayed open even though delivery was already dead. `upgrade.go` now wraps the writer with `outboundDone`, and `disconnect.go::superviseLifecycle` treats `outboundDone` as a first-exit trigger and drains it after `Disconnect` alongside `dispatchDone` / `keepaliveDone`. Pinned by `protocol/disconnect_test.go::TestSuperviseLifecycleInvokesDisconnectOnOutboundWriterExit` plus updated supervisor happy-path pins. See `docs/hardening-oi-004-outbound-writer-supervision.md`.

Why this matters:
- Lifecycle races and unsafe close behavior undermine confidence in the protocol even when nominal tests pass.
- This is one of the main blockers to calling the runtime trustworthy for serious private use.

Remaining sub-hazards:
- other detached goroutines in the protocol lifecycle surface (`protocol/conn.go`, `protocol/lifecycle.go`, `protocol/keepalive.go`) if a specific leak site surfaces
- `ClientSender.Send` is still synchronous without its own ctx; a Send-ctx parameter would let callers propagate a shorter cancellation scope than `DisconnectTimeout` into the overflow path, but no concrete consumer needs that today

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`
- `protocol/sender.go`
- `protocol/async_responses.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md` (watchReducerResponse sub-hazard closure)
- `docs/hardening-oi-004-sender-disconnect-context.md` (sender overflow-disconnect background-ctx sub-hazard closure)
- `docs/hardening-oi-004-supervise-disconnect-context.md` (supervise-lifecycle disconnect-ctx sub-hazard closure)
- `docs/hardening-oi-004-closeall-disconnect-context.md` (CloseAll disconnect-ctx sub-hazard closure)
- `docs/hardening-oi-004-forward-reducer-response-context.md` (forwardReducerResponse ctx / Done lifecycle sub-hazard closure)
- `docs/hardening-oi-004-dispatch-handler-context.md` (dispatch-handler ctx sub-hazard closure)

### OI-005: Snapshot and committed-read-view lifetime rules still need stronger safety guarantees

Status: open
Severity: high

Summary:
- Snapshot/read-view lifetime discipline is still treated as a sharp edge in the surrounding docs.
- This is an architectural correctness concern, not cosmetic cleanup.
- Snapshot iterator GC retention sub-hazard closed 2026-04-20: `*CommittedSnapshot.TableScan` / `IndexScan` / `IndexRange` returned closures that captured `*Table` but not `*CommittedSnapshot`, so a caller holding only the iter could let the snapshot become unreachable, fire the finalizer, release the RLock mid-`range`, and race a concurrent writer on `Table.rows`. Each iterator now `defer runtime.KeepAlive(s)`s the snapshot so the closure retains it for the iter's lifetime. Pinned by `store/snapshot_iter_retention_test.go::TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`. See `docs/hardening-oi-005-snapshot-iter-retention.md`.
- Snapshot iterator use-after-Close sub-hazard closed 2026-04-20: the same three iterator bodies previously did not re-check `s.ensureOpen()` at iter-body entry, so a sequential `construct → Close → iterate` pattern silently raced the freed RLock. Each iterator body now calls `s.ensureOpen()` after the `KeepAlive` defer, converting the mis-use into a deterministic `"store: CommittedSnapshot used after Close"` panic matching the construction-time contract. Pinned by `store/snapshot_iter_useafterclose_test.go::{TestCommittedSnapshotTableScanPanicsAfterClose, TestCommittedSnapshotIndexScanPanicsAfterClose, TestCommittedSnapshotIndexRangePanicsAfterClose}`. See `docs/hardening-oi-005-snapshot-iter-useafterclose.md`.
- Snapshot iterator mid-iter-close sub-hazard closed 2026-04-20: the three iterator bodies previously checked `s.ensureOpen()` only once at iter-body entry, so a partially consumed iter whose owner called `Close()` mid-iteration (same goroutine caller body or another goroutine holding a reference) continued yielding subsequent rows against a released RLock. Each iter-body for-loop now re-calls `s.ensureOpen()` per-iteration so the next step after `Close()` panics with the construction-time contract message rather than silently yielding. Pinned by `store/snapshot_iter_mid_iter_close_test.go::{TestCommittedSnapshotTableScanPanicsOnMidIterClose, TestCommittedSnapshotIndexRangePanicsOnMidIterClose, TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose}`. Defense-in-depth only — cannot eliminate the machine-level race window between the check and an in-flight `t.rows` read; full ownership discipline still required from callers. See `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`.
- Subscription-seam read-view lifetime sub-hazard closed 2026-04-20: `subscription/eval.go::EvalAndBroadcast` receives a borrowed `store.CommittedReadView`, and `executor/executor.go:540-541` calls `view.Close()` immediately after the synchronous return. The no-view-escape-past-return contract was load-bearing but unpinned; today's code keeps it (the view reference does not land in `FanOutMessage`, no goroutine spawned from `evaluate` outlives the call, `DeltaView.Release` fires in `defer`), but nothing asserted it. A contract comment on `EvalAndBroadcast` and a `trackingView` wrapper pin the invariant: after `EvalAndBroadcast` returns and the test closes the tracker, the fan-out inbox is drained and the tracker asserts zero post-close method invocations — under both Join (Tier-2 + join delta) and single-table eval paths. Pinned by `subscription/eval_view_lifetime_test.go::{TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join, TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable}`. No production-code behavior change. See `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`.
- `CommittedSnapshot.IndexSeek` BTree-alias escape route closed 2026-04-20: `store/snapshot.go::CommittedSnapshot.IndexSeek` forwarded `BTreeIndex.Seek` which returns a live alias of the index entry's internal `[]types.RowID`. A caller that retained the slice past `Close()` would race any subsequent writer's `slices.Insert` / `slices.Delete` on the same key — either in-place-shifted `Delete` or capacity-case `Insert`. Current callers (`subscription/eval.go:286`, `subscription/register_set.go:{92,117}`, `subscription/delta_join.go:{85,122}` via `subscription/delta_view.go:165`, `subscription/placement.go:162`) use the slice synchronously in a for-range and did not retain, but the contract was unpinned. `IndexSeek` now returns `slices.Clone(idx.Seek(key))` so callers cannot alias BTree-internal storage past the public read-view boundary. Pinned by `store/snapshot_indexseek_aliasing_test.go::{TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert, TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove}`. See `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`.
- `StateView.SeekIndex` BTree-alias escape route closed 2026-04-20: the `StateView.SeekIndex` iterator body ranged over `idx.Seek(key)` directly — the same aliased `[]RowID` from the BTree entry. Go's `for _, v := range` captures the slice header once but reads from the backing array every iteration; a mid-iter in-place `slices.Delete` on the entry (yield callback reaching into a contract-violating path) shifts the tail down and drifts the yielded RowIDs. Today no caller triggers this under executor single-writer discipline, but the contract was unpinned. The iterator now ranges over `slices.Clone(idx.Seek(key))` so iteration is decoupled from BTree-internal storage, mirroring the `CommittedSnapshot.IndexSeek` fix. Pinned by `store/state_view_seekindex_aliasing_test.go::TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`. See `docs/hardening-oi-005-state-view-seekindex-aliasing.md`.
- `StateView.SeekIndexRange` BTree-alias escape route closed 2026-04-20: `StateView.SeekIndexRange` ranged over `idx.BTree().SeekRange(low, high)` directly — an `iter.Seq` that walks `b.entries` live (outer loop reads `len(b.entries)` and indexes the backing array each step). A yield callback that reaches into the BTree and drops the last RowID of an entry behind the cursor fires `slices.Delete(b.entries, idx, idx+1)` and shifts the tail down in place; the outer `i++` then skips one entry that was present at seek time. Today no caller triggers this under executor single-writer discipline, but the contract was unpinned. The iterator now ranges over `slices.Collect(idx.BTree().SeekRange(low, high))` so iteration walks an independent materialized copy of the range, mirroring the `StateView.SeekIndex` fix. Pinned by `store/state_view_seekindexrange_aliasing_test.go::TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`. See `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`.
- `StateView.ScanTable` iterator surface closed 2026-04-21: `StateView.ScanTable` drove its yield loop from `Table.Scan()`, an `iter.Seq2` that ranges `t.rows` live — the outer map iteration spanned the full yield loop. Under executor single-writer discipline no concurrent writer runs during a reducer's synchronous iteration, but the contract was unpinned at the `StateView` boundary: a yield callback that reached a future path mutating `t.rows` (direct `CommittedState` access from a reducer refactor, a new narrow API that borrows the view for a follow-on mutation), or a caller that retained the iterator past the single-writer window, would race the live map iteration. Per Go spec §6.3 an unreached-entry deletion during map iteration does not produce the entry — the observable drift is the iteration silently skipping rows present at iter-construction time. `StateView.ScanTable` now collects the committed scan into an `[]entry{id, row}` slice pre-sized via `table.RowCount()` before entering the yield loop, so the yield loop iterates the materialized slice and a mid-iter `t.rows` mutation cannot drift the outer iteration. Mirrors the `StateView.SeekIndex` / `StateView.SeekIndexRange` fixes. Pinned by `store/state_view_scan_aliasing_test.go::TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete`. See `docs/hardening-oi-005-state-view-scan-aliasing.md`.
- `CommittedState.Table(id) *Table` raw-pointer contract pin closed 2026-04-21: `store/committed_state.go::CommittedState.Table` acquired `cs.mu.RLock()` only for the map lookup and returned a raw `*Table` pointer after releasing. Callers use the pointer — including mutating calls such as `AllocRowID`, `InsertRow`, and sequence `Next` via `applyAutoIncrement` — after the RLock is released. Three legal envelopes bounded this safely (`CommittedSnapshot` open→Close RLock lifetime, executor single-writer discipline for `Transaction` / `StateView`, single-threaded `commitlog/recovery.go` bootstrap), but the envelope rule was unwritten; a future caller that stashed `*Table` past its envelope, retained it across `RegisterTable(id, replacement)`, or read from a non-executor goroutine without RLock would silently violate the safety model. Contract-pin slice only (no production-code semantic change): `Table()` and `TableIDs()` now carry substantive contract comments enumerating the three envelopes and the three hazards, and three pin tests in `store/committed_state_table_contract_test.go` assert the observable invariants — same-envelope pointer identity (`TestCommittedStateTableSameEnvelopeReturnsSamePointer`), stale-after-re-register hazard shape (`TestCommittedStateTableRetainedPointerIsStaleAfterReRegister`), and snapshot envelope RLock lifetime (`TestCommittedStateTableSnapshotEnvelopeHoldsRLockUntilClose`). Closes the last enumerated OI-005 sub-hazard; OI-005 remains open as a theme because the envelope rule is enforced by discipline and observational pins, not machine-enforced lifetime. See `docs/hardening-oi-005-committed-state-table-raw-pointer.md`.

Why this matters:
- Long-lived or misused read views can distort concurrency assumptions and make correctness depend on caller discipline.
- It also weakens confidence in subscription evaluation and recovery-side read paths.

Remaining sub-hazards:
- none enumerated; OI-005 remains open as a theme because the envelope rule for raw `*Table` access is enforced by discipline and observational pins rather than machine-enforced lifetime. Promoting to a narrower interface wrapper that re-checks snapshot openness on every access, or a generation-counter invalidation model on `*Table` itself, would be its own decision doc.

Primary code surfaces:
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/eval.go`
- `executor/executor.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-005-snapshot-iter-retention.md` (iter-retention sub-hazard closure)
- `docs/hardening-oi-005-snapshot-iter-useafterclose.md` (iter use-after-Close sub-hazard closure)
- `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md` (iter mid-iter-close sub-hazard closure)
- `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md` (subscription-seam read-view lifetime sub-hazard closure)
- `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md` (IndexSeek BTree-alias escape route closure)
- `docs/hardening-oi-005-state-view-seekindex-aliasing.md` (StateView.SeekIndex BTree-alias escape route closure)
- `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md` (StateView.SeekIndexRange BTree-alias escape route closure)
- `docs/hardening-oi-005-state-view-scan-aliasing.md` (StateView.ScanTable iterator surface closure)
- `docs/hardening-oi-005-committed-state-table-raw-pointer.md` (CommittedState.Table raw-pointer contract pin closure)

### OI-006: Subscription fanout still carries aliasing and cross-subscriber mutation risk concerns

Status: open
Severity: medium

Summary:
- Fanout and update assembly remain a live hardening concern around shared slices/maps and per-subscriber isolation.
- The parity docs treat this as one of the main non-cosmetic remaining risks.
- Per-subscriber `Inserts` / `Deletes` slice-header aliasing sub-hazard closed 2026-04-20: `subscription/eval.go::evaluate` previously distributed the same slice header across every subscriber of a query, so any downstream replace/append on one subscriber's slice would silently corrupt every other subscriber's view of the same commit. Each subscriber now receives an independent slice header for `Inserts` / `Deletes`; row payloads (`types.ProductValue`) remain shared under the post-commit row-immutability contract. Pinned by `subscription/eval_fanout_aliasing_test.go::{TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers, TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers}`. See `docs/hardening-oi-006-fanout-aliasing.md`.
- Row-payload sharing contract pin closed 2026-04-21: `types.ProductValue` (itself `[]Value`) backing arrays are shared across subscribers of the same query for both `Inserts` and `Deletes` — the 2026-04-20 slice-header fix copies ProductValue slice-header values into independent outer backing arrays, but each copied header still references the original `[]Value` backing array. Sharing is governed by the post-commit row-immutability contract: rows produced by the store are not mutated in place after commit, and downstream consumers (`subscription/fanout_worker.go`, `protocol/fanout_adapter.go::encodeRows`) only read row payloads. The contract was load-bearing but unwritten — a future consumer that mutated `Value` elements in place during delivery / encoding would silently corrupt every other subscriber's view of the same commit. Contract-pin slice only (no production-code semantic change): contract comments on `subscription/eval.go::evaluate` per-subscriber fanout loop, `subscription/fanout_worker.go::FanOutSender`, and `protocol/fanout_adapter.go::encodeRows` name the read-only discipline; two pin tests in `subscription/eval_fanout_row_payload_sharing_test.go` assert backing-array identity across subscribers and the mutation-leak hazard shape. Pinned by `TestEvalFanoutRowPayloadsSharedAcrossSubscribersFor{Inserts,Deletes}`. See `docs/hardening-oi-006-row-payload-sharing.md`.

Why this matters:
- Cross-subscriber mutation or aliasing bugs are subtle and can silently corrupt delivery behavior.
- This weakens confidence in both parity and basic correctness claims.

Remaining sub-hazards:
- broader fanout assembly hazards in `subscription/fanout.go`, `subscription/fanout_worker.go`, and `protocol/fanout_adapter.go` if any future path introduces in-place mutation. The contract-pin comments on `FanOutSender` and `encodeRows` name the read-only discipline so a future in-place mutation is visibly unsafe, but enforcement is by discipline and observational pins rather than machine-enforced immutability at the `types.ProductValue` boundary.

Primary code surfaces:
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-006-fanout-aliasing.md` (slice-header aliasing sub-hazard closure)
- `docs/hardening-oi-006-row-payload-sharing.md` (row-payload sharing contract pin closure)

### OI-007: Recovery sequencing and replay-edge behavior still needs targeted parity closure

Status: open
Severity: medium

Summary:
- Replay-horizon / validated-prefix behavior (`P0-RECOVERY-001`) closed 2026-04-20 via narrow-and-pin (`docs/parity-p0-recovery-001-replay-horizon.md`). All four ledger sub-behaviors are parity-close under observation; the internal-mechanism difference (segment-level short-circuit vs reference per-commit `adjust_initial_offset`) is pinned as intentional. Remaining commitlog parity work — typed error enums, offset index file, format-level log / changeset parity — rolls up under `OI-003` as broader Phase 4 scope.
- Scheduler startup / firing ordering (`P0-SCHED-001`) closed 2026-04-20 via narrow-and-pin (`docs/parity-p0-sched-001-startup-firing.md`). Remaining scheduler deferrals (`fn_start`-clamped schedule "now", one-shot panic deletion, intended-time past-due ordering) are recorded there with reference anchors; reopen if workload evidence surfaces.
- The already-closed snapshot+replay invariant work did not eliminate the broader sequencing/replay parity backlog (format-level, offset index, etc.).

Why this matters:
- These are the kinds of gaps that only show up under restart, crash, and replay conditions.
- They materially affect the “operational replacement” claim.

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-p0-recovery-001-replay-horizon.md` (replay-horizon closure)
- `docs/parity-p0-sched-001-startup-firing.md` (scheduler deferrals)
- `docs/parity-phase0-ledger.md` row `P0-RECOVERY-001` (closed)

### OI-008: The repo still lacks a coherent top-level engine/bootstrap story

Status: open
Severity: medium

Summary:
- There is still no `main` package, `cmd/` entrypoint, example app, or single polished bootstrap surface.
- `schema.Engine.Start(...)` is a startup schema-compatibility check, not the unified runtime bootstrap implied by the original architecture sketches.

Why this matters:
- The subsystem work is real, but the developer-facing embedding story is still weaker than the implementation depth underneath it.
- This makes it harder to judge the project as a usable replacement even if many internals are already substantial.

Primary code surfaces:
- `schema/version.go`
- `README.md`
- repo root package layout

Source docs:
- `README.md`
- `docs/current-status.md`

## Deferred issues

### DI-001: Energy accounting remains a permanent parity deferral

Status: deferred
Severity: low

Summary:
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem.

Why this matters:
- This is an intentional parity gap, but it should remain explicit so it does not get mistaken for accidental completeness.

Source docs:
- `docs/parity-phase1.5-outcome-model.md`
- `docs/parity-phase0-ledger.md`
