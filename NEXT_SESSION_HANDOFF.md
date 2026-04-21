# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-21, column-kind widening Slice 1 — `i128` / `u128` realized)

First column-kind widening slice. `types.ValueKind` gained `KindInt128` / `KindUint128`; `types.Value` grew two uint64 storage slots (`hi128`, `lo128`); BSATN added tags 13 (Int128) and 14 (Uint128) encoding 16 bytes LE (lo then hi); `query/sql/coerce.go` promotes `LitInt` to 128-bit via `NewInt128FromInt64` / `NewUint128FromUint64` — int64 always fits both widths so the coerce branches are one-liners, and `u128 = -1` still rejects on the existing negative-LitInt guard; subscription canonical hashing writes hi then lo (16 bytes big-endian). Autoincrement remains 64-bit-only by design — `schema.AutoIncrementBounds` returns `ok=false` for 128-bit, so `store/transaction.go::newAutoIncrementValue` / `store/recovery.go::replayAutoIncrementValueAsUint64` / `commitlog/recovery.go::autoIncrementValueAsUint64` never see a 128-bit kind. `schema.GoTypeToValueKind` continues to reject 128-bit (Go has no native `int128` / `uint128`); column-kind instantiation is library-API driven.

Unlocked reference rows:
- `check.rs:360-370` `valid_literals_for_type` rows `i128 = 127` and `u128 = 127` now accept end-to-end. Pinned by extending the existing `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` / `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth` bundle with two new subtests each (no structural change, just new rows in the `cases` slice).
- `check.rs:382-385` `invalid_literals` negative-on-unsigned extended to the u128 row via dedicated `TestHandleSubscribeSingle_ParityUint128NegativeRejected` / `TestHandleOneOffQuery_ParityUint128NegativeRejected`.

Verification:
- `rtk go test ./types -count=1` → 9 new round-trip / Equal / Compare / accessor-panic tests
- `rtk go test ./bsatn -count=1` → 7 new round-trip values + `TestEncodedValueSize128`
- `rtk go test ./query/sql -count=1` → 6 new coerce pins (`TestCoerceIntLiteralToInt128`, `TestCoerceNegativeIntLiteralToInt128`, `TestCoerceIntLiteralToUint128`, `TestCoerceNegativeIntoUint128Fails`, `TestCoerceStringLiteralOnInt128Rejected`, `TestCoerceFloatLiteralOnUint128Rejected`, `TestCoerceSenderRejectsInt128Column`)
- `rtk go test ./subscription -count=1` → 2 new hash pins (`TestQueryHashInt128VsUint128`, `TestQueryHashInt128DiffersByPayload`) plus 4 new entries in `TestQueryHashAllKindsRoundTrip`
- `rtk go test ./schema -count=1` → `valuekind_export_test.go` extended for 128-bit in both `TestValueKindExportStringAll` and `TestAutoIncrementBoundsNonInteger`
- `rtk go test ./protocol -run 'ParityValidLiteralOnEachIntegerWidth|ParityUint128NegativeRejected' -count=1 -v` → 28 passed (24 subtests + 4 top-level tests)
- `rtk go fmt ./...`, `rtk go vet ./...` → clean
- `rtk go test ./...` → `Go test: 1339 passed in 10 packages`

Clean-tree baseline: `Go test: 1339 passed in 10 packages` (previous 1315 + 24 new tests).

Remaining `check.rs:360-370` / `check.rs:284-332` shapes still deferred:
- `i256` / `u256` column kinds — need 32-byte storage (`[4]uint64` or similar) and BigDecimal-style literal widening to accept `u256 = 1e40` (the ref test case; `1e40` overflows `int64` so today's `parseNumericLiteral` collapses it to `LitFloat`)
- timestamp column kind — needs RFC3339 SQL literal grammar (today's numeric lexer cannot produce `'2025-02-10T15:45:30Z'` as a timestamp literal; string-literal on timestamp column rejects)
- array / product column kinds — recursive `Value` representation, also unblocks `check.rs:523-525` product-value comparison

Next candidate slices (pick one, keep scope narrow):
1. **i256 / u256 without `1e40`** — 32-byte storage (`hi, midhi, midlo, lo uint64`), BSATN tags 15 / 16 (32 bytes LE), coerce from `LitInt`; pin `i256 = 127` / `u256 = 127` (leaving `u256 = 1e40` as a separate follow-up that needs BigDecimal literal support).
2. **Timestamp column kind** — reuse `i64` slot for microseconds since unix epoch, add RFC3339 literal grammar (`LitTimestamp` or extend `LitString` parsing to attempt timestamp on KindTimestamp columns); pin `ts = '2025-02-10T15:45:30Z'`.
3. **Array column kind (narrow: KindString elements only)** — recursive Value with element kind embedded in the column schema, new BSATN tag, minimal coerce surface; pin `SELECT * FROM t WHERE arr = :sender` rejection (which today fires incidentally via `Coerce` default branch) as a **positive** parity contract with array support. Biggest representation change.

## What landed earlier (2026-04-21, `sql.rs:457-476` `parse_sql::invalid` pure-syntax rejection pin bundle)

Reference `parse_sql::invalid` test block at `reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:457-476` asserts seven shapes reject at the parser boundary:

- `select from t` — Empty SELECT
- `select a from where b = 1` — Empty FROM
- `select a from t where` — Empty WHERE
- `select a, count(*) from t group by` — Empty GROUP BY
- `select count(*) from t` — Aggregate without alias
- `` — Empty string
- ` ` — Whitespace only

Empty-string and whitespace-only shapes were already pinned by the earlier `sub.rs::unsupported` bundle (`ParityEmptyStatementRejected` / `ParityWhitespaceOnlyStatementRejected`), so this slice adds five new pins at each admission surface.

All five new shapes reject incidentally inside `parseProjection` at `query/sql/parser.go:553-572`. Shunter's SELECT-only parser requires the projection to be `*` or `<qualifier>.*`, so:
- `SELECT FROM t` — `parseProjection` reads `FROM` as identifier qualifier, expects `.` next, finds `t` → rejects with "projection must be '*' or 'table.*'"
- `SELECT a FROM WHERE b = 1` — reads `a` as identifier qualifier, expects `.` next, finds `FROM` → same rejection
- `SELECT a FROM t WHERE` — same as above, rejects on `a` before empty WHERE is examined
- `SELECT a, COUNT(*) FROM t GROUP BY` — rejects on leading `a` before the empty GROUP BY is examined
- `SELECT COUNT(*) FROM t` — reads `count` as identifier qualifier, expects `.` next, finds `(` → same rejection

No runtime widening was required — the shapes land at the parser boundary before the reference-style empty-FROM / empty-WHERE / empty-GROUP BY / aggregate-without-alias conditions are ever reached.

New pins landed (10 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParitySqlInvalidEmptySelectRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyFromRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyWhereRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyGroupByRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidAggregateWithoutAliasRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: matching five `TestHandleOneOffQuery_ParitySqlInvalid*` pins in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParitySqlInvalid|TestHandleOneOffQuery_ParitySqlInvalid' -count=1 -v` → `Go test: 10 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1315 passed in 10 packages`

Clean-tree baseline: `Go test: 1315 passed in 10 packages` (previous 1305 + 10 new pin rows).

## What landed earlier (2026-04-21, `sql.rs:411-436` `parse_sql::unsupported` rejection pin bundle)

Reference parse_sql rejection block at `reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:411-436` lists ten shapes the reference general-SQL parser rejects before type-checking. Eight of them are SELECT-level (the remaining two are DML `update ... join ... set` / `update t set a = 1 from s where ...` which are already covered by the existing `ParityDMLStatementRejected` pins):

- `select 1` — SELECT with literal projection, no FROM
- `select a from s.t` — multi-part table name
- `select * from t where a = B'1010'` — bit-string literal
- `select a.*, b, c from t` — wildcard projection mixed with bare columns
- `select * from t order by a limit b` — ORDER BY with LIMIT expression
- `select a, count(*) from t group by a` — aggregate with GROUP BY
- `select a.* from t as a, s as b where a.id = b.id and b.c = 1` — implicit comma join
- `select t.* from t join s on int = u32` — unqualified JOIN ON vars

All eight were already rejected incidentally at Shunter's SELECT-only parser boundary:
- `SELECT 1` / `SELECT a ...` / `SELECT a, COUNT(*)` — `parseProjection` rejects non-`*` / non-`table.*` projection at `query/sql/parser.go:553-572`
- `SELECT t.*, b, c FROM t` — after `t.*` parseStatement expects FROM, finds `,`, rejects with "expected FROM, got \",\""
- `SELECT * FROM t ORDER BY u32 LIMIT u32` — ORDER BY trips parseStatement's EOF guard at `parser.go:547-549` with "unexpected token \"ORDER\"" before the LIMIT identifier is examined
- `SELECT * FROM t WHERE u32 = B'1010'` — lexer tokenizes `B` as identifier, `parseLiteral` rejects with "expected literal, got identifier \"B\""
- `SELECT a.* FROM t AS a, s AS b ...` — after `t AS a` parseStatement's EOF/keyword guard hits `,` and rejects with "unexpected token \",\""
- `SELECT t.* FROM t JOIN s ON int = u32` — `parseJoinClause` calls `parseQualifiedColumnRef` for the left side of ON (`parser.go:629`); the bare identifier `int` fails with "expected qualified column reference"
- `SELECT a FROM s.t` — parseProjection rejects bare `a` before FROM parsing reaches `s.t`

No runtime widening was required. The new pins latch the reference parity contract at the protocol admission boundary.

New pins landed (16 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParitySqlUnsupportedSelectLiteralWithoutFromRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedMultiPartTableNameRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedBitStringLiteralRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedWildcardWithBareColumnsRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedOrderByWithLimitExpressionRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedAggregateWithGroupByRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedImplicitCommaJoinRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: matching eight `TestHandleOneOffQuery_ParitySqlUnsupported*` pins in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParitySqlUnsupported|TestHandleOneOffQuery_ParitySqlUnsupported' -count=1 -v` → `Go test: 16 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean (no new issues)
- `rtk go test ./...` → `Go test: 1305 passed in 10 packages`

Clean-tree baseline: `Go test: 1305 passed in 10 packages` (previous 1289 + 16 new pin rows).

Intentional divergence from earlier slice remains recorded: Shunter unifies admission behind one subscription-shape contract; reference `parse_sql` path also accepts DML (`insert`, `delete`, `update`) in its `supported` block, which Shunter rejects under the unified contract. The DML shapes are pinned as rejections by the existing `ParityDMLStatementRejected` pins.

## What landed earlier (2026-04-21, `sub.rs:157-168` `unsupported` rejection pin bundle + intentional one-off-vs-SQL divergence recorded)

Reference subscription-parser `unsupported` test block at `reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs:157-168` covers five shapes the reference rejects before type-checking:
- `delete from t` — DML not allowed in subscription parse path
- `` (empty string) — empty after skip
- ` ` (whitespace only) — same as empty after tokenizer skip
- `select distinct a from t` — DISTINCT projection
- `select * from (select * from t) join (select * from s) on a = b` — subquery in FROM

All five were already rejected incidentally by Shunter's SELECT-only parser:
- DML / empty / whitespace fail at `query/sql/parser.go:475-477` `expectKeyword("SELECT")` (non-SELECT leading token or EOF-only token stream)
- DISTINCT fails at `parseProjection` (`query/sql/parser.go:553-572`) which only accepts `*` / `table.*`; DISTINCT is consumed as a qualifier candidate, the next token is `a` not `.`, and the parser emits "projection must be '*' or 'table.*'"
- subquery-in-FROM fails at `parseStatement` `tableTok := p.peek(); if !isIdentifierToken(tableTok)` (`query/sql/parser.go:485-488`) — the `(` token is `tokLParen`, not an identifier

No runtime widening was required. The new pins latch the reference parity contract at the protocol admission boundary.

New pins landed (10 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityDMLStatementRejected`, `TestHandleSubscribeSingle_ParityEmptyStatementRejected`, `TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected`, `TestHandleSubscribeSingle_ParityDistinctProjectionRejected`, `TestHandleSubscribeSingle_ParitySubqueryInFromRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityDMLStatementRejected`, `TestHandleOneOffQuery_ParityEmptyStatementRejected`, `TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected`, `TestHandleOneOffQuery_ParityDistinctProjectionRejected`, `TestHandleOneOffQuery_ParitySubqueryInFromRejected` in `protocol/handle_oneoff_test.go`

**Intentional divergence recorded (one-off vs reference SQL statement path):** Reference splits `parse_and_type_sub` (subscription, narrow) and `parse_and_type_sql` (one-off SQL, wider). The SQL path at `reference/SpacetimeDB/crates/expr/src/statement.rs:521-551` accepts `select str from t` (bare column projection), `select str, arr from t` (multi-col bare projection), `select t.str, arr from t` (mixed qualified/unqualified), and `select * from t limit 5` (LIMIT). Shunter unifies both behind one `compileSQLQueryString` admission surface that enforces the subscription-shape contract for SubscribeSingle / SubscribeMulti / OneOffQuery, so those four shapes are rejected on all surfaces — pinned as rejections by `TestHandleOneOffQuery_ParityBareColumnProjectionRejected` and `TestHandleOneOffQuery_ParityLimitClauseRejected`. Widening would require LIMIT runtime support, bare / mixed projection plumbing, and reversing the already-landed pins — out of scope for a narrow slice. Divergence recorded in `docs/parity-phase0-ledger.md` (under the `sub.rs::unsupported` paragraph) and `TECH-DEBT.md`. If workload evidence surfaces a real need for ref-style one-off SQL semantics, promote it to its own multi-slice anchor.

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityDMLStatementRejected|TestHandleSubscribeSingle_ParityEmptyStatementRejected|TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected|TestHandleSubscribeSingle_ParityDistinctProjectionRejected|TestHandleSubscribeSingle_ParitySubqueryInFromRejected|TestHandleOneOffQuery_ParityDMLStatementRejected|TestHandleOneOffQuery_ParityEmptyStatementRejected|TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected|TestHandleOneOffQuery_ParityDistinctProjectionRejected|TestHandleOneOffQuery_ParitySubqueryInFromRejected' -count=1 -v` → `Go test: 10 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1289 passed in 10 packages`

Clean-tree baseline: `Go test: 1289 passed in 10 packages` (previous 1279 + 10 new pin rows).

## What landed earlier (2026-04-21, `check.rs:360-370` `valid_literals_for_type` column-width breadth pin bundle)

Reference `valid_literals_for_type` at `reference/SpacetimeDB/crates/expr/src/check.rs:360-370` iterates every numeric column kind (`i8, u8, i16, u16, i32, u32, i64, u64, f32, f64, i128, u128, i256, u256`) and asserts `SELECT * FROM t WHERE {ty} = 127` type-checks. Shunter realizes the subset that maps to `schema.ValueKind` — the 10 widths `i8/u8/i16/u16/i32/u32/i64/u64/f32/f64`; `i128`, `u128`, `i256`, `u256` are not realizable (no `schema.ValueKind` variant) and are deliberately skipped.

All 10 realizable widths were already rejected-or-accepted incidentally via `query/sql/coerce.go`:
- `coerceSigned` at `coerce.go:105-113` accepts LitInt within range for `KindInt8/Int16/Int32/Int64`
- `coerceUnsigned` at `coerce.go:115-127` accepts LitInt within range for `KindUint8/Uint16/Uint32/Uint64`
- `KindFloat32` / `KindFloat64` branches at `coerce.go:66-83` promote LitInt via `float32(lit.Int)` / `float64(lit.Int)` (integer-literal-to-float promotion landed with the 2026-04-21 scientific-notation slice)

`= 127` fits every kind's range (i8's max is 127 exactly), so no runtime widening was needed. The new pins latch the reference column-width parity contract at the protocol admission boundary.

New pins landed (2 top-level tests, 20 subtests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` (10 subtests: i8, u8, i16, u16, i32, u32, i64, u64, f32, f64) in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth` (10 subtests: same 10 widths) in `protocol/handle_oneoff_test.go`

Each subtest builds a single-column table of the given kind, runs `SELECT * FROM t WHERE {colname} = 127`, and asserts admission success. SubscribeSingle pins the executor's ColEq predicate carries a width-native value; OneOff pins Status == 0 and stores a matching row.

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth|TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth' -count=1 -v` → `Go test: 22 passed in 1 packages` (2 parents + 20 subtests)
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1279 passed in 10 packages`

Clean-tree baseline: `Go test: 1279 passed in 10 packages` (previous 1257 + 22 new pin rows)

## What landed earlier (2026-04-21, `check.rs:382-401` `invalid_literals` rejection pin bundle)

Reference `invalid_literals` block `reference/SpacetimeDB/crates/expr/src/check.rs:373-406` tests five shapes that must reject at the type-check boundary:
- `u8 = -1` (lines 382-385) — negative integer against unsigned column
- `u8 = 1e3` (lines 386-389) — scientific-notation collapses to LitInt(1000), out of range for u8 (max 255)
- `u8 = 0.1` (lines 390-393) — non-integral decimal stays LitFloat, rejected against integer column
- `u32 = 1e-3` (lines 394-397) — `1e-3 = 0.001` non-integral, LitFloat, rejected against unsigned column
- `i32 = 1e-3` (lines 398-401) — same shape, rejected against signed column

All five were already rejected incidentally inside `compileSQLQueryString` / `parseQueryString` via `coerceUnsigned` / `coerceSigned` in `query/sql/coerce.go`:
- negative LitInt → `coerceUnsigned` line 119 rejects
- out-of-range LitInt → `coerceUnsigned` line 123 rejects
- LitFloat against integer column → `coerceUnsigned` line 116 / `coerceSigned` line 106 `mismatch()`

No runtime widening needed; coerce-layer already has broad mechanism tests (`TestCoerceNegativeIntoUnsignedFails`, `TestCoerceIntToSignedRangeCheck`, `TestCoerceRejectsFloatLiteralOnUint32Column`) so no new coerce pins were added (precedent from `check.rs:483-497` bundle). The new pins latch the reference parity contract at the protocol admission boundary.

New pins landed (10 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeIntOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralScientificOverflowRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralFloatOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnSignedRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityInvalidLiteralNegativeIntOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralScientificOverflowRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralFloatOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnSignedRejected` in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityInvalidLiteral|TestHandleOneOffQuery_ParityInvalidLiteral' -count=1 -v` → `Go test: 10 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1257 passed in 10 packages`

Clean-tree baseline after this slice: `Go test: 1257 passed in 10 packages` (previous 1247 + 10 new pins; now superseded by the 2026-04-21 `valid_literals_for_type` breadth bundle above — current baseline 1279)

## Prior slice (2026-04-21, scientific-notation + leading-dot float literal parity bundle)

Reference valid-literal bundle `reference/SpacetimeDB/crates/expr/src/check.rs:302-328` is now supported end-to-end on the Shunter SQL surface:
- `u32 = 1e3` / `u32 = 1E3` — scientific notation, integer-valued, binds to unsigned integer column (lines 302-308)
- `f32 = 1e3` — integer-shaped scientific notation on float column (lines 310-312)
- `f32 = 1e-3` — negative exponent, non-integral, binds to float column (lines 314-316)
- `f32 = .1` — leading-dot float, no integer part (lines 322-324)
- `f32 = 1e40` — overflow to `+Inf` on float32 (lines 326-328; `types.NewFloat32` accepts `+Inf`, only `NaN` rejected)

`f32 = 0.1` (lines 318-320) was already supported.

Implementation:
- `query/sql/parser.go`: extracted numeric body parsing into `tokenizeNumeric(s, i, start)` so both the signed/digit-started and leading-`.digit` entry points share the exponent/fractional logic. Added a new `case c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'` above the existing `tokDot` dispatch so `.1` routes into numeric rather than a dangling dot. Signed-prefix branch now also accepts `+.1` / `-.1`. Exponent tail is `[eE][+-]?[digits]+`; missing digits after `e`/`E`/sign still error as `malformed numeric literal`.
- `query/sql/parser.go`: replaced the old `strings.Contains(t.text, ".")` int/float split in `parseLiteral` with `parseNumericLiteral(text)` — when the body contains `.`, `e`, or `E`, parse via `strconv.ParseFloat(64)`, then collapse to `LitInt` iff the result is finite, `math.Trunc(f) == f`, and within `[math.MinInt64, math.MaxInt64]` (mirroring the reference `BigDecimal::is_integer()` filter in `crates/expr/src/lib.rs::parse_int`). Non-integral or out-of-range stays `LitFloat`.
- `query/sql/coerce.go`: widened `KindFloat32` / `KindFloat64` to accept `LitInt` (promoted via `float32(lit.Int)` / `float64(lit.Int)`), matching reference `parse_float` BigDecimal promotion. `LitFloat` still rejected on integer columns at the existing `coerceSigned` / `coerceUnsigned` seams — `u32 = 1.3` (non-integral) stays pinned as an admission error.
- `query/sql/coerce_test.go`: dropped the stale `TestCoerceUnsupportedKind` (comment said "floats deferred" but floats have worked since the 2026-04-21 float-literal slice). Replaced with `TestCoerceIntegerLiteralPromotesToFloat64`, `TestCoerceIntegerLiteralPromotesToFloat32`, and `TestCoerceFloatLiteralOverflowsToFloat32Infinity`.

Malformed-input guards preserved:
- `TestParseWhereTrailingDotRejected` (`1.`) — trailing dot with no fractional digits still errors.
- `TestParseWhereBareExponentRejected` (`1e`) — exponent letter with no digits still errors.
- `TestParseWhereTrailingIdentifierAfterNumericRejected` (`1efoo`) — numeric followed by identifier still errors (guards against the exponent widening accidentally consuming `1e` then leaving `foo` as a dangling identifier).

New pins landed (18 tests net, clean-tree baseline 1229 → 1247):
- query/sql parser: 8 new tests (5 accept, 3 reject) in `query/sql/parser_test.go`
- query/sql coerce: 3 new tests net (3 added, `TestCoerceUnsupportedKind` dropped as stale)
- protocol subscribe-single: 4 new tests in `protocol/handle_subscribe_test.go`
- protocol one-off: 4 new tests in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./query/sql -count=1` → `Go test: 88 passed in 1 packages`
- `rtk go test ./protocol -count=1` → `Go test: 335 passed in 1 packages`
- `rtk go fmt ./query/sql ./protocol`, `rtk go vet ./query/sql ./protocol` → clean
- `rtk go test ./...` → `Go test: 1247 passed in 10 packages`

Remaining `check.rs:284-332` valid-literal shapes still open (both not realizable against Shunter's column-kind enum, so they are deferred, not next slices):
- 128/256-bit integer column kinds (`i128`, `u128`, `i256`, `u256`) — no such `schema.ValueKind` variant
- timestamp columns — no such `schema.ValueKind` variant

Clean-tree baseline: `Go test: 1247 passed in 10 packages` (previous 1229 + 18 new pins)

## Prior slice (2026-04-21, leading-`+` numeric literal parity micro-slice)

Reference valid-literal shape `reference/SpacetimeDB/crates/expr/src/check.rs:297-300` (`select * from t where u32 = +1` / "Leading `+`") is now supported end-to-end. Probe of `check.rs::valid_literals` (`check.rs:284-332`) showed Shunter already accepted leading `-` but rejected leading `+` at the lexer: `parser.go::tokenize` line 362 matched `c == '-' || (c >= '0' && c <= '9')` only, so `+7` fell through to `tokSymbol` and `parseLiteral` errored.

One-line lexer widening: the numeric-literal case in `tokenize` now matches `c == '-' || c == '+' || (c >= '0' && c <= '9')` and mirrors the leading-sign dispatch symmetrically. `strconv.ParseInt(s, 10, 64)` accepts the `+` prefix natively, so `parseLiteral` and `coerce.go::coerceUnsigned` / `coerceSigned` required no changes.

New pins landed (3 tests):
- query/sql parser: `TestParseWhereLeadingPlusInt` in `query/sql/parser_test.go`
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityLeadingPlusIntLiteral` in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./query/sql -run 'TestParseWhereLeadingPlusInt|TestParseWhereNegativeInt' -count=1 -v` → 2 passed
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral|TestHandleOneOffQuery_ParityLeadingPlusIntLiteral' -count=1 -v` → 2 passed
- `rtk go fmt ./query/sql ./protocol`, `rtk go vet ./query/sql ./protocol` → clean
- `rtk go test ./...` → `Go test: 1229 passed in 10 packages`

Remaining `check.rs:284-332` valid-literal shapes still open (all require real lexer + coerce widening):
- scientific notation: `u32 = 1e3` (→ 1000 as integer), `u32 = 1E3` (case-insensitive), `f32 = 1e3` (integer parses as float), `f32 = 1e-3` (negative exponent), `f32 = 1e40` (overflow → +Inf)
- leading-dot floats: `f32 = .1`
- 128/256-bit integer column kinds (`i128`, `u128`, `i256`, `u256`) and timestamp columns: not realizable against Shunter's `schema.ValueKind` enum — skip.

Clean-tree baseline: `Go test: 1229 passed in 10 packages` (previous 1226 + 3 new pins)

## Previous slice (2026-04-21, parser-surface check.rs negative-shape pin bundle)

Reference type-check rejection shapes at `reference/SpacetimeDB/crates/expr/src/check.rs` lines 506-509 (`select * from t as r where t.u32 = 5` / base-table qualifier out of scope after alias), 510-513 (`select u32 from t` / bare column projection), 515-517 (`select * from t join s` / join without qualified projection), 519-521 (`select t.* from t join t` / self-join without aliases), 526-528 (`select t.* from t join s on t.u32 = r.u32 join s as r` / forward alias reference), 530-533 (`select * from t limit 5` / LIMIT clause), and 534-537 (`select t.* from t join s on t.u32 = s.u32 where bytes = 0xABCD` / unqualified WHERE column inside join) are now explicitly pinned at both the SubscribeSingle and OneOffQuery admission surfaces (14 new tests). All seven shapes were already rejected incidentally at the SQL parser boundary (`parseProjection`, `parseStatement` EOF-check, `parseStatement` joined-projection guard, `parseJoinClause` self-join guard, `parseQualifiedColumnRef` / `parseComparison` via `resolveQualifier`, and `parseComparison` requireQualify under a join binding) — no runtime widening was required. The pins promote the rejections from incidental parser-level errors to named reference-parity contracts latched on the protocol admission boundary.

Grounded anchors walked before edits:
- `check.rs:506-509`: `SELECT * FROM t AS r WHERE t.u32 = 5` — `parser.go::parseComparison` calls `resolveQualifier("t", {R: t})` which returns `!ok` → `qualified column "t" does not match relation`.
- `check.rs:510-513`: `SELECT u32 FROM t` — `parser.go::parseProjection` rejects any projection other than `*` / `table.*` at lines 517-528.
- `check.rs:515-517`: `SELECT * FROM t JOIN s` — `parser.go::parseStatement` line 468-469 rejects a join query whose projection qualifier is empty: `join queries require a qualified projection`.
- `check.rs:519-521`: `SELECT t.* FROM t JOIN t` — `parser.go::parseJoinClause` line 577-578 detects `leftTable==rightTable && leftAlias==rightAlias` → `self join requires aliases`.
- `check.rs:526-528`: `SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r` — `parser.go::parseQualifiedColumnRef` rejects the `r.u32` qualifier at line 629-631; the forward-alias reference fails before the multi-way-join guard at lines 482-489 fires.
- `check.rs:530-533`: `SELECT * FROM t LIMIT 5` — `parser.go::parseStatement` reaches the EOF check at line 505-506 with `LIMIT` still in the token stream and rejects with `unexpected token "LIMIT"`. The already-existing trailing-keyword fast-path in `parseWhere` (lines 641-645) only fires when a WHERE clause precedes the keyword; the standalone `LIMIT` case was already rejected by the EOF guard.
- `check.rs:534-537`: `SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE bytes = 0xABCD` — `parser.go::parseComparison` at lines 761-762 enforces `bindings.requireQualify` under a join binding: `join WHERE columns must be qualified`.

Pinned but deliberately skipped:
- `check.rs:523-525` (`SELECT t.* FROM t JOIN s ON t.arr = s.arr` / product-value comparison): not realizable against the Shunter column-kind enum. `schema.ValueKind` (re-exported from `types`) enumerates only `KindBool`, `KindInt{8,16,32,64}`, `KindUint{8,16,32,64}`, `KindFloat{32,64}`, `KindString`, `KindBytes` — there is no array / product kind — so the shape reference rejects cannot arise at the Shunter admission boundary. Skipping is intentional; if Shunter ever adds a composite column kind, this shape becomes a fresh landing candidate for either a runtime widening (accept + reject in join-ON compile) or a named parser rejection.

New pins landed (14 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityBaseTableQualifierAfterAliasRejected`, `TestHandleSubscribeSingle_ParityBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected`, `TestHandleSubscribeSingle_ParitySelfJoinWithoutAliasesRejected`, `TestHandleSubscribeSingle_ParityForwardAliasReferenceRejected`, `TestHandleSubscribeSingle_ParityLimitClauseRejected`, `TestHandleSubscribeSingle_ParityUnqualifiedWhereInJoinRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityBaseTableQualifierAfterAliasRejected`, `TestHandleOneOffQuery_ParityBareColumnProjectionRejected`, `TestHandleOneOffQuery_ParityJoinWithoutQualifiedProjectionRejected`, `TestHandleOneOffQuery_ParitySelfJoinWithoutAliasesRejected`, `TestHandleOneOffQuery_ParityForwardAliasReferenceRejected`, `TestHandleOneOffQuery_ParityLimitClauseRejected`, `TestHandleOneOffQuery_ParityUnqualifiedWhereInJoinRejected` in `protocol/handle_oneoff_test.go`

Scope kept narrow: no parser changes, no runtime widening, no new test files. `SubscribeMulti` inherits the compile path through shared parser+`compileSQLQueryString`; no dedicated pin added (covered by the same parser mechanism as SubscribeSingle). Existing parity pins from earlier in the calendar week are unchanged — the new 506-537 pins sit alongside the `check.rs:498-504` type-mismatch pins and the `check.rs:483-497` unknown-table / unknown-column pins as named reference-parity contracts.

Docs follow-through: `docs/current-status.md`, `docs/parity-phase0-ledger.md`, and `TECH-DEBT.md` now record the fourteen new pins as landed and call out `check.rs:523-525` as not realizable against the Shunter column-kind enum.

Verification run after landing the slice:
- `rtk go test ./protocol -run '<all 14 new test names>' -count=1 -v` → `Go test: 14 passed in 1 packages`
- `rtk go fmt ./protocol`
- `rtk go vet ./protocol` → `No issues found`
- `rtk go test ./...` → `Go test: 1226 passed in 10 packages`

Current clean-tree baseline:
- `Go test: 1226 passed in 10 packages` (previous 1212 + 14 new pins)

## Previous slice (2026-04-21, unknown-table / unknown-column parity pin bundle)

Reference type-check rejection shapes `check.rs:483-485` (`select * from r` / unknown FROM table), `check.rs:491-493` (`select * from t where t.a = 1` / qualified unknown WHERE column), and `check.rs:495-497` (`select * from t as r where r.a = 1` / alias-qualified unknown WHERE column) are now explicitly pinned at the SubscribeSingle and OneOffQuery admission surfaces. Previously the rejection was incidental — `SchemaLookup.TableByName` returned `!ok` in `compileSQLQueryString` (`protocol/handle_subscribe.go:152-154`) and `rel.ts.Column` returned `!ok` in `normalizeSQLFilterForRelations` (`protocol/handle_subscribe.go:250-253`), but nothing named the reference parity contract.

- Grounded anchors before edits:
  - `reference/SpacetimeDB/crates/expr/src/check.rs:483-485` for the unknown FROM table rejection (`"Table r does not exist"`).
  - `reference/SpacetimeDB/crates/expr/src/check.rs:491-493` for the qualified unknown WHERE column rejection (`"Field a does not exist on table t"`).
  - `reference/SpacetimeDB/crates/expr/src/check.rs:495-497` for the alias-qualified unknown WHERE column rejection (same message; alias resolves back to base table in Shunter's parser `relationBindings`).
- No production code widening was required. `compileSQLQueryString` shared between SubscribeSingle / SubscribeMulti / OneOffQuery already rejects all three shapes incidentally; walking the path confirmed:
  - `SELECT * FROM r` fails at `sl.TableByName(stmt.ProjectedTable)` (`handle_subscribe.go:152-154`)
  - `SELECT * FROM t WHERE t.a = 1` fails at `rel.ts.Column(f.Column)` inside `normalizeSQLFilterForRelations` (`handle_subscribe.go:250-253`)
  - `SELECT * FROM t AS r WHERE r.a = 1` — parser's `resolveQualifier` maps alias `r` back to base table `t`, then the filter lookup fails the same way
- New pins landed (6 tests):
  - protocol subscribe-single: `TestHandleSubscribeSingle_ParityUnknownTableRejected`, `TestHandleSubscribeSingle_ParityUnknownColumnRejected`, `TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected` in `protocol/handle_subscribe_test.go`
  - protocol one-off: `TestHandleOneOffQuery_ParityUnknownTableRejected`, `TestHandleOneOffQuery_ParityUnknownColumnRejected`, `TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected` in `protocol/handle_oneoff_test.go`
- Scope kept narrow: no parser changes, no runtime widening, no new test file. `SubscribeMulti` inherits the compile path through shared `compileSQLQueryString`; no dedicated pin added (covered by same mechanism as SubscribeSingle). Existing `TestHandleSubscribeSingle_UnknownTable` / `TestHandleOneOffQuery_UnknownTable` / `TestHandleOneOffQuery_UnknownColumn` tests left unchanged — the new pins are named reference-parity contracts alongside them rather than replacements.
- Docs follow-through: `docs/current-status.md`, `docs/parity-phase0-ledger.md`, and `TECH-DEBT.md` now record the three new pins as landed; the pinned tests are named in the ledger.

Verification run after landing the slice:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityUnknownTableRejected|TestHandleSubscribeSingle_ParityUnknownColumnRejected|TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected|TestHandleOneOffQuery_ParityUnknownTableRejected|TestHandleOneOffQuery_ParityUnknownColumnRejected|TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected' -count=1 -v`
- `rtk go fmt ./protocol`
- `rtk go vet ./protocol`
- `rtk go test ./...`

Current clean-tree baseline:
- `Go test: 1212 passed in 10 packages` (previous 1206 + 6 new pins — this was the baseline before the 506-537 bundle landed; see the top-of-file 1226 figure for the current clean-tree truth)

Flaky test note: no known clean-tree intermittent tests remain after the 2026-04-21 subscription, scheduler, protocol lifecycle, message-family, and SQL/query-surface follow-through.

## Recommended next slice

All five reference subscription test blocks in `check.rs` (`valid_literals`, `valid_literals_for_type`, `invalid_literals`, `valid`, `invalid`) are drained at the subscribe-single / one-off admission surfaces for every shape realizable against `schema.ValueKind`. `statement.rs:521-551` probe closed. `rls.rs` probe closed. `sub.rs::unsupported` (157-168) pinned. `sub.rs::supported` (170-184) audited — all 8 positive shapes covered incidentally by existing TD-142 / literal / `:sender` pins; no named-parity gap worth re-pinning. `sql.rs::unsupported` (411-436) pinned — 8 SELECT-level shapes (DML shapes covered by prior bundle). `sql.rs::invalid` (457-476) pin bundle just landed — 5 new pure-syntax shapes (empty SELECT / FROM / WHERE / GROUP BY, aggregate without alias); empty-string and whitespace-only already covered by the `sub.rs::unsupported` bundle.

Remaining reference shapes all need runtime widening first:
- 128/256-bit integer column kinds (`i128`, `u128`, `i256`, `u256`) and timestamp column kinds — need a `schema.ValueKind` extension
- array / product column kinds — same
- RLS rule resolution — whole-feature slice, not a narrow pin

With the `sql.rs::invalid` bundle landed, the named-reference SQL parser / subscription parity ledger is now effectively drained for shapes realizable against Shunter's `schema.ValueKind` enum. No narrow reference-backed SQL/query-surface slice remains that can land without runtime widening or whole-feature work.

Candidate next slices (pick one, do not try to widen scope):

1. **Tier-B hardening** — `TECH-DEBT.md` still carries the remaining OI-004 watch items (`ClientSender.Send` no-ctx follow-on, other detached goroutines in `protocol/conn.go` / `lifecycle.go` / `keepalive.go`) and the OI-008 top-level bootstrap gap. Do not force an OI-004 sub-slice unless a specific leak site surfaces in live code or a failing test.

2. **Format-level commitlog parity follow-on** (Phase 4 Slice 2 α/γ) — offset index file, typed error enums, record / log shape compatibility. Larger than a single narrow slice; each would need its own decision doc.

3. **One of the `P0-SCHED-001` deferrals** (`fn_start`-clamped schedule "now", one-shot panic deletion, intended-time past-due ordering) if workload evidence surfaces.

4. **Column-kind widening** (`i128`/`u128`/`i256`/`u256` / timestamp / array / product kinds in `schema.ValueKind`) to unblock the currently-skipped `check.rs:284-332` `valid_literals` / `valid_literals_for_type` rows and `check.rs:523-525` product-value comparison — representation-level work, not a narrow pin.

Recommendation: no obvious next narrow-and-pinned SQL anchor. Pick option 1/2/3/4 driven by workload or reference evidence, or wait for the user to name the next concrete target.

Do not reopen the landed literal / quoted-identifier / `:sender` / type-mismatch / unknown-table-column / parser-surface negative / leading-`+` / scientific-notation / leading-dot / infinity / column-width-breadth / `sub.rs::unsupported` / `sql.rs::unsupported` / `sql.rs::invalid` slices unless a regression appears.

## Expected shape of the next session

1. Read the required startup docs in the listed order.
2. Treat the current worktree as landed SQL/query parity truth (quoted special-character identifiers, hex byte literals, float literals, `:sender` on narrow single-table, aliased single-table, and narrow join-backed KindBytes columns), not as unfinished envelope work.
3. Start with the next grounded SQL anchor from `reference/SpacetimeDB/crates/expr/src/check.rs` and the parity docs.
4. Preferred next slice: pin a specific reference-backed rejection shape (e.g. `check.rs:498-504` type-mismatch cases) on the protocol admission surface, or choose a different narrow reference-backed SQL shape.
   - add failing parser/protocol/runtime pins first
   - verify the failure
   - implement the smallest parser/coercion/runtime widening that keeps unrelated SQL shapes rejected; widening may not be needed if the rejection is already incidentally enforced
   - re-run targeted tests, then `rtk go test ./...`
5. If the chosen slice turns out blocked by a wider runtime contract than expected, stop and choose the next narrow reference-backed SQL shape instead of broadening opportunistically.
6. Only after the suite is green, update the docs and this handoff again.


Prior closed anchors in the same calendar week (still landed, included here for continuity):
- broader SQL/query-surface parity follow-through (2026-04-21): quoted special-character identifiers, hex byte literals, float literals, `:sender` caller-identity parameter on narrow single-table / aliased single-table / narrow join-backed KindBytes columns, `check.rs:498-504` type-mismatch rejection pins (string-lit and float-lit against an integer column) at coerce + subscribe-single + one-off admission seams, `check.rs:483-497` unknown-table / unknown-column rejection pins (unknown FROM table, qualified unknown WHERE column, alias-qualified unknown WHERE column) at subscribe-single + one-off admission seams, `check.rs:506-537` parser-surface negative-shape pin bundle (base-table qualifier out of scope after alias, bare column projection, join without qualified projection, self-join without aliases, forward alias reference, LIMIT clause, unqualified WHERE column inside join) at subscribe-single + one-off admission seams, and `check.rs:297-300` leading-`+` numeric-literal support (lexer mirrors existing leading-`-` handling; parser + coerce unchanged; pinned at parser + subscribe-single + one-off) — `check.rs:523-525` (product-value comparison in join ON) deliberately skipped as not realizable against Shunter's column-kind enum
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard — `docs/hardening-oi-006-fanout-aliasing.md`
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin — `docs/hardening-oi-005-committed-state-table-raw-pointer.md`
- OI-005 `StateView.ScanTable` iterator surface — `docs/hardening-oi-005-state-view-scan-aliasing.md`
- OI-004 dispatch-handler ctx sub-hazard — `docs/hardening-oi-004-dispatch-handler-context.md`
- OI-004 `forwardReducerResponse` ctx / Done lifecycle — `docs/hardening-oi-004-forward-reducer-response-context.md`
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard — `docs/hardening-oi-004-closeall-disconnect-context.md`
- OI-004 outbound-writer supervision sub-hazard — `docs/hardening-oi-004-outbound-writer-supervision.md`
- OI-004 `superviseLifecycle` disconnect-ctx — `docs/hardening-oi-004-supervise-disconnect-context.md`
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx — `docs/hardening-oi-004-sender-disconnect-context.md`
- OI-004 `watchReducerResponse` goroutine-leak escape route — `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
- OI-005 `StateView.SeekIndex` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route — `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
- OI-005 subscription-seam read-view lifetime sub-hazard — `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard — `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
- OI-005 snapshot iterator use-after-Close sub-hazard — `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
- OI-005 snapshot iterator GC retention sub-hazard — `docs/hardening-oi-005-snapshot-iter-retention.md`
- Phase 4 Slice 2 replay-horizon / validated-prefix (`P0-RECOVERY-001`) — `docs/parity-p0-recovery-001-replay-horizon.md`
- Phase 3 Slice 1 scheduled-reducer startup / firing ordering (`P0-SCHED-001`) — `docs/parity-p0-sched-001-startup-firing.md`
- Phase 2 Slice 3 lag / slow-client policy (`P0-SUBSCRIPTION-001`) — `docs/parity-phase2-slice3-lag-policy.md`

## Next realistic parity / hardening anchors

With `P0-RECOVERY-001`, `P0-SCHED-001`, `P0-SUBSCRIPTION-001` closed, all nine OI-005 enumerated sub-hazards closed, both enumerated OI-006 sub-hazards closed, and six OI-004 sub-hazards closed, the grounded options are:

### Option α — Broader SQL/query-surface parity beyond TD-142

This is still the best next grounded parity path.

What is still open:
- `docs/current-status.md`, `docs/spacetimedb-parity-roadmap.md`, and `docs/parity-phase0-ledger.md` now all agree that the remaining externally visible message-family follow-through is broader SQL/query-surface breadth rather than another `SubscriptionError` envelope tweak
- TD-142 plus the 2026-04-21 SQL/query follow-through (quoted special-character identifiers, hex byte literals, float literals, `:sender` on narrow single-table / aliased single-table / narrow join-backed KindBytes columns) drained the named narrow positive slices; the remaining grounded extension is pinning the reference *rejection* cases not yet directly pinned (e.g. `check.rs:498-504` type-mismatch cases) at the protocol admission boundary

Why prefer this now:
- the just-landed alias / join `:sender` slice closed the last named narrow-SQL positive gap tied to `check.rs:435-440`
- externally visible parity still outranks speculative Tier-B watch items
- this keeps effort on client-visible behavior rather than reopening already-green message-family work

Likely code surfaces:
- `query/sql/parser.go`
- `query/sql/coerce.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`

Concrete shape:
- choose one exact remaining reference-backed SQL/query scenario from `check.rs` `invalid()` cases
- add parser + public protocol/runtime pins first
- keep the slice narrow; do not reopen unrelated lifecycle or envelope work in the same session

### Option β — Continue Tier-B hardening

`TECH-DEBT.md` still carries:
- OI-004 remaining sub-hazards (other detached goroutines in `protocol/conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`; `ClientSender.Send` no-ctx follow-on)
- OI-005: enumerated sub-hazards list now empty; OI-005 remains open as a theme because the envelope rule for raw `*Table` access is enforced by discipline and observational pins rather than machine-enforced lifetime.
- OI-006: enumerated sub-hazards list now empty; OI-006 remains open as a theme because the read-only row-payload contract is enforced by discipline and observational pins rather than machine-enforced immutability at the `types.ProductValue` boundary.
- OI-008 (top-level bootstrap missing)

Current judgment:
- do not force another OI-004 sub-slice unless a specific concrete leak site surfaces in live code or a failing test
- `ClientSender.Send` no-ctx remains a follow-on with no concrete consumer today
- the remaining detached-goroutine bullets are now watch items, not the best immediate slice

### Option γ — Format-level commitlog parity (Phase 4 Slice 2 follow-on)

With the replay-horizon / validated-prefix slice closed, the remaining commitlog parity work is format-level:
- offset index file (reference `src/index/indexfile.rs`, `src/index/mod.rs`)
- record / log shape compatibility (reference `src/commit.rs`, `src/payload/txdata.rs`)
- typed `error::Traversal` / `error::Open` enums
- snapshot / compaction visibility vs reference `repo::resume_segment_writer` contract

These are larger scope than a single narrow slice; each would need its own decision doc.

### Option δ — Pick one of the `P0-SCHED-001` deferrals

Each remaining scheduler deferral is a candidate for its own focused slice if workload evidence surfaces:
- `fn_start`-clamped schedule "now" (plumb reducer dispatch timestamp into `schedulerHandle`; ref `scheduler.rs:211-215`)
- one-shot panic deletion (second-commit post-rollback path; ref `scheduler.rs:445-455`)
- past-due ordering by intended time (sort in `scanAndTrackMaxWithContext`)

Prefer Option α over β/γ/δ unless live workload or reference evidence surfaces a stronger blocker.

## First, what you are walking into

The repo already has substantial implementation. Do not treat this as a docs-only project. Do not do a broad audit. Do not restart parity analysis from zero.

Your job is to continue from the current live state. Pick the next grounded anchor from `docs/spacetimedb-parity-roadmap.md`, `docs/parity-phase0-ledger.md`, or `TECH-DEBT.md`.

Clean-room reminder:
- parity target means matching externally meaningful behavior where required, not translating Rust source into Go
- `reference/SpacetimeDB/` stays research-only and read-only; do not copy, transliterate, or mechanically port code from it
- re-derive behavior from public docs, reference outcomes, and live Shunter contracts, then implement natively in Go

## Mandatory reading order

1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `docs/parity-phase0-ledger.md`
9. `TECH-DEBT.md`
10. `docs/hardening-oi-006-row-payload-sharing.md` (closed slice — contract pin at the row-payload sharing seam)
11. `docs/hardening-oi-006-fanout-aliasing.md` (prior OI-006 sub-slice — slice-header isolation precedent)
12. `docs/hardening-oi-005-committed-state-table-raw-pointer.md` (prior OI-005 contract-pin precedent)
13. `docs/hardening-oi-005-state-view-scan-aliasing.md` (prior OI-005 sub-slice)
14. `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
15. `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
16. `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
17. `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
18. `docs/hardening-oi-004-dispatch-handler-context.md`
19. `docs/hardening-oi-004-forward-reducer-response-context.md`
20. `docs/hardening-oi-004-closeall-disconnect-context.md`
21. `docs/hardening-oi-004-supervise-disconnect-context.md`
22. `docs/hardening-oi-004-sender-disconnect-context.md`
23. `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
24. `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
25. `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
26. `docs/hardening-oi-005-snapshot-iter-retention.md`
27. `docs/parity-p0-recovery-001-replay-horizon.md`
28. `docs/parity-p0-sched-001-startup-firing.md`
29. `docs/parity-phase2-slice3-lag-policy.md`
30. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

## Shell discipline

Use `rtk` for shell commands. Examples:
- `rtk git status --short --branch`
- `rtk go test ./store -run 'TestName' -v`
- `rtk go test ./...`

## Important repo note

Keep `.hermes/plans/2026-04-18_073534-phase1-wire-level-parity.md` unless you deliberately update the contract that depends on it. A test expects it.

## What is already landed (do not reopen)

- Protocol conformance P0-PROTOCOL-001..004
- Delivery parity P0-DELIVERY-001..002
- Recovery invariant P0-RECOVERY-002
- TD-142 Slices 1–14 (all narrow SQL parity shapes, including join projection emitted onto the SELECT side)
- Phase 1.5 outcome model + caller metadata wiring
- Phase 2 Slice 3 lag / slow-client policy (2026-04-20) — `P0-SUBSCRIPTION-001`
- Phase 3 Slice 1 scheduled reducer startup / firing ordering (2026-04-20) — `P0-SCHED-001`
- Phase 4 Slice 2 replay-horizon / validated-prefix behavior (2026-04-20) — `P0-RECOVERY-001`
- OI-005 snapshot iterator GC retention sub-hazard (2026-04-20)
- OI-005 snapshot iterator use-after-Close sub-hazard (2026-04-20)
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard (2026-04-20)
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard (2026-04-20)
- OI-005 subscription-seam read-view lifetime sub-hazard (2026-04-20)
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route (2026-04-20)
- OI-004 `watchReducerResponse` goroutine-leak escape route (2026-04-20)
- OI-005 `StateView.SeekIndex` BTree-alias escape route (2026-04-20)
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route (2026-04-20)
- P1-07 executor response-channel contract + protocol-forwarding cancel-safe + Submit-time validation (2026-04-20, landed in commit `40b2152 baseline`)
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard (2026-04-21)
- OI-004 `superviseLifecycle` disconnect-ctx sub-hazard (2026-04-21)
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard (2026-04-21)
- OI-004 `forwardReducerResponse` ctx / Done lifecycle sub-hazard (2026-04-21)
- OI-004 dispatch-handler ctx sub-hazard (2026-04-21)
- OI-005 `StateView.ScanTable` iterator surface (2026-04-21)
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin (2026-04-21)
- OI-006 row-payload sharing contract pin (2026-04-21)
- `sub.rs:157-168` subscription-parser `unsupported` rejection pin bundle (2026-04-21): DML (`delete from t`), empty string, whitespace-only, DISTINCT projection (`select distinct a from t`), subquery-in-FROM (`select * from (select * from t) join (select * from s) on a = b`) pinned at subscribe-single + one-off admission seams; all five reject incidentally at Shunter's SELECT-only parser (`expectKeyword("SELECT")`, `parseProjection`, `parseStatement` identifier-after-FROM), pins promote to named reference-parity contracts. Intentional divergence between Shunter's unified admission surface and reference's split `parse_and_type_sub` / `parse_and_type_sql` paths recorded in `docs/parity-phase0-ledger.md` and `TECH-DEBT.md`.
- `sql.rs:411-436` `parse_sql::unsupported` rejection pin bundle (2026-04-21): SELECT-level shapes (`select 1`, `select a from s.t`, `select * from t where a = B'1010'`, `select a.*, b, c from t`, `select * from t order by a limit b`, `select a, count(*) from t group by a`, `select a.* from t as a, s as b where a.id = b.id and b.c = 1`, `select t.* from t join s on int = u32`) pinned at subscribe-single + one-off admission seams; all eight reject incidentally at Shunter's SELECT-only parser boundary, pins promote to named reference-parity contracts. DML shapes in the same reference block are covered by existing `ParityDMLStatementRejected` pins.
- broader SQL/query-surface parity follow-through (2026-04-21): quoted special-character identifiers, hex byte literals, float literals, `:sender` caller-identity parameter on narrow single-table / aliased single-table / narrow join-backed KindBytes columns, `check.rs:498-504` type-mismatch rejection pins (string-lit and float-lit against an integer column) at coerce + subscribe-single + one-off admission seams, `check.rs:483-497` unknown-table / unknown-column rejection pins (unknown FROM table, qualified unknown WHERE column, alias-qualified unknown WHERE column) at subscribe-single + one-off admission seams, `check.rs:506-537` parser-surface negative-shape pin bundle (base-table qualifier out of scope after alias, bare column projection, join without qualified projection, self-join without aliases, forward alias reference, LIMIT clause, unqualified WHERE column inside join) at subscribe-single + one-off admission seams, `check.rs:297-300` leading-`+` numeric-literal support (lexer mirrors existing leading-`-` handling; pinned at parser + subscribe-single + one-off), `check.rs:302-328` scientific-notation + leading-dot + infinity bundle (`u32 = 1e3`, `u32 = 1E3`, `f32 = 1e3`, `f32 = 1e-3`, `f32 = .1`, `f32 = 1e40 → +Inf`; lexer numeric branch grew exponent tail + leading-dot entry via `tokenizeNumeric`, `parseNumericLiteral` collapses integer-valued finite results to `LitInt`, coerce `KindFloat32`/`KindFloat64` now accept `LitInt` via promotion; pinned at parser + coerce + subscribe-single + one-off), `check.rs:382-401` `invalid_literals` rejection pin bundle (`u8 = -1`, `u8 = 1e3`, `u8 = 0.1`, `u32 = 1e-3`, `i32 = 1e-3`; all five reject incidentally through `coerceUnsigned` / `coerceSigned`, pinned at subscribe-single + one-off admission seams), and `check.rs:360-370` `valid_literals_for_type` column-width breadth pin bundle (`{ty} = 127` for each of `i8/u8/i16/u16/i32/u32/i64/u64/f32/f64`; all 10 ride existing `coerceSigned` / `coerceUnsigned` / `KindFloat32`-`KindFloat64` promotion with no runtime widening, pinned as table-driven subtests at subscribe-single + one-off admission seams; `i128`/`u128`/`i256`/`u256` not realizable)

## Suggested verification commands

Targeted:
- `rtk go test ./query/sql -run 'TestParseWhereSenderParameter|TestParseWhereRejectsUnknownParameter|TestCoerceSender|TestCoerceRejects' -count=1 -v`
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_SenderParameter|TestHandleOneOffQuery_SenderParameter|TestHandleSubscribeSingle_StringLiteralOnIntegerColumnRejected|TestHandleSubscribeSingle_FloatLiteralOnIntegerColumnRejected|TestHandleOneOffQuery_StringLiteralOnIntegerColumnRejected|TestHandleOneOffQuery_FloatLiteralOnIntegerColumnRejected|TestHandleSubscribeSingle_ParityUnknownTableRejected|TestHandleSubscribeSingle_ParityUnknownColumnRejected|TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected|TestHandleOneOffQuery_ParityUnknownTableRejected|TestHandleOneOffQuery_ParityUnknownColumnRejected|TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected|TestHandleSubscribeSingle_ParityBaseTableQualifierAfterAliasRejected|TestHandleSubscribeSingle_ParityBareColumnProjectionRejected|TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected|TestHandleSubscribeSingle_ParitySelfJoinWithoutAliasesRejected|TestHandleSubscribeSingle_ParityForwardAliasReferenceRejected|TestHandleSubscribeSingle_ParityLimitClauseRejected|TestHandleSubscribeSingle_ParityUnqualifiedWhereInJoinRejected|TestHandleOneOffQuery_ParityBaseTableQualifierAfterAliasRejected|TestHandleOneOffQuery_ParityBareColumnProjectionRejected|TestHandleOneOffQuery_ParityJoinWithoutQualifiedProjectionRejected|TestHandleOneOffQuery_ParitySelfJoinWithoutAliasesRejected|TestHandleOneOffQuery_ParityForwardAliasReferenceRejected|TestHandleOneOffQuery_ParityLimitClauseRejected|TestHandleOneOffQuery_ParityUnqualifiedWhereInJoinRejected' -count=1 -v`
- `rtk go test ./subscription -run 'TestEvalFanoutRowPayloadsSharedAcrossSubscribers' -race -count=3 -v`
- `rtk go test ./subscription -run 'TestEvalFanout' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedStateTable' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestDispatchLoop_HandlerCtx' -race -count=3 -v`
- `rtk go test ./executor -run 'TestProtocolInboxAdapter_ForwardReducerResponse' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestCloseAll' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestSuperviseLifecycle' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestEnqueueOnConnOverflowDisconnect' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestWatchReducerResponse' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityDMLStatementRejected|TestHandleSubscribeSingle_ParityEmptyStatementRejected|TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected|TestHandleSubscribeSingle_ParityDistinctProjectionRejected|TestHandleSubscribeSingle_ParitySubqueryInFromRejected|TestHandleOneOffQuery_ParityDMLStatementRejected|TestHandleOneOffQuery_ParityEmptyStatementRejected|TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected|TestHandleOneOffQuery_ParityDistinctProjectionRejected|TestHandleOneOffQuery_ParitySubqueryInFromRejected' -count=1 -v`
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParitySqlUnsupported|TestHandleOneOffQuery_ParitySqlUnsupported' -count=1 -v`
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParitySqlInvalid|TestHandleOneOffQuery_ParitySqlInvalid' -count=1 -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed or debt-anchored target shape was checked directly against reference material or current live code
- every newly accepted or rejected shape has focused tests
- already-landed parity pins still pass (including the `:sender` parser/coerce/protocol pins listed in `docs/parity-phase0-ledger.md`)
- full suite still passes. Clean-tree baseline is `Go test: 1339 passed in 10 packages`. No known clean-tree intermittent test remains after the 2026-04-21 follow-through.
- docs and handoff reflect the new truth exactly

## Deliverables for the next session

Either:
- code + tests closing the next reference-backed parity slice or Tier-B hardening sub-hazard

Or:
- a grounded blocker report naming the exact representation/runtime issue preventing a narrow landing

And in either case:
- update `TECH-DEBT.md` if any OI changes state
- update `docs/current-status.md`
- update `docs/parity-phase0-ledger.md` if a parity scenario moves
- update `NEXT_SESSION_HANDOFF.md`

## Final status snapshot right now

As of this handoff:
- `TD-142` fully drained
- Phase 2 Slice 3 closed — per-client outbound queue aligned to reference `CLIENT_CHANNEL_CAPACITY`; close-frame mechanism retained as intentional divergence
- Phase 3 Slice 1 closed — `P0-SCHED-001` scheduled-reducer startup / firing ordering narrow-and-pinned
- Phase 4 Slice 2 closed — `P0-RECOVERY-001` replay-horizon / validated-prefix behavior narrow-and-pinned
- P1-07 executor response-channel contract + protocol-forwarding cancel-safe + Submit-time validation landed
- OI-005 enumerated sub-hazards drained (iter GC retention, iter use-after-Close, iter mid-iter-close, subscription-seam read-view lifetime, IndexSeek BTree-alias, SeekIndex BTree-alias, SeekIndexRange BTree-alias, ScanTable iterator surface, `CommittedState.Table` raw-pointer contract pin)
- OI-006 enumerated sub-hazards drained (slice-header aliasing, row-payload sharing contract pin)
- OI-004 six sub-hazards closed (watchReducerResponse, sender overflow-disconnect ctx, superviseLifecycle disconnect-ctx, CloseAll disconnect-ctx, forwardReducerResponse ctx/Done lifecycle, dispatch-handler ctx, outbound-writer supervision)
- Phase 2 Slice 2 applied-envelope host execution duration + `SubscriptionError` optional-field / `TableID` follow-through closed
- broader SQL/query-surface parity follow-through (2026-04-21): reference-style double-quoted identifiers, query-builder-style parenthesized WHERE predicates, alias-qualified mixed-qualified/unqualified OR, hex byte literals, float literals on the narrow single-table / join-backed SQL surface, `:sender` caller-identity parameter on narrow single-table / aliased single-table / narrow join-backed KindBytes columns, bare boolean `WHERE TRUE`, `check.rs:498-504` type-mismatch rejections (string-lit and float-lit against an integer column), `check.rs:483-497` unknown-table / unknown-column rejections (unknown FROM table, qualified / alias-qualified unknown WHERE column), `check.rs:506-537` parser-surface negative-shape bundle (base-table qualifier out of scope after alias, bare column projection, join without qualified projection, self-join without aliases, forward alias reference, LIMIT clause, unqualified WHERE column inside join), `check.rs:297-300` leading-`+` numeric-literal support, `check.rs:302-328` scientific-notation + leading-dot + infinity bundle (`u32 = 1e3`, `u32 = 1E3`, `f32 = 1e3`, `f32 = 1e-3`, `f32 = .1`, `f32 = 1e40 → +Inf`), `check.rs:382-401` `invalid_literals` rejection bundle (`u8 = -1`, `u8 = 1e3`, `u8 = 0.1`, `u32 = 1e-3`, `i32 = 1e-3`), and `check.rs:360-370` `valid_literals_for_type` column-width breadth bundle (`{ty} = 127` for each of `i8/u8/i16/u16/i32/u32/i64/u64/f32/f64`; table-driven subtest bundle at subscribe-single + one-off) all work end-to-end and are pinned; `check.rs:523-525` (product-value comparison in join ON) deliberately skipped as not realizable against Shunter's column-kind enum, and `check.rs:284-332` `u256` / `i128`/`u128`/`i256` / timestamp column shapes plus the `i128`/`u128`/`i256`/`u256` rows of `valid_literals_for_type` are skipped for the same reason
- Other detached-goroutine surfaces in `conn.go` / `lifecycle.go` / `keepalive.go` and the `ClientSender.Send` no-ctx follow-on remain open under OI-004
- `sub.rs:157-168` `unsupported` rejection pin bundle landed (2026-04-21) — 10 pins across subscribe-single + one-off for DML, empty, whitespace-only, DISTINCT projection, subquery-in-FROM. Intentional divergence between Shunter's unified admission surface and reference's split `parse_and_type_sub` / `parse_and_type_sql` paths recorded in `docs/parity-phase0-ledger.md` and `TECH-DEBT.md`.
- `sql.rs:411-436` `parse_sql::unsupported` rejection pin bundle landed (2026-04-21) — 16 pins across subscribe-single + one-off for SELECT-level shapes: `select 1` (FROM-required), multi-part table, bit-string literal, wildcard with bare columns, ORDER BY + LIMIT expression, aggregate + GROUP BY, implicit comma join, unqualified JOIN ON vars. All eight shapes already rejected incidentally at Shunter's SELECT-only parser boundary, pins promote to named parity contracts. The two DML shapes in that reference block (`update ... join ... set`, `update t set a = 1 from s where ...`) are covered by the existing `ParityDMLStatementRejected` pins.
- `sql.rs:457-476` `parse_sql::invalid` pure-syntax rejection pin bundle landed (2026-04-21, this session) — 10 pins across subscribe-single + one-off for the five new shapes: empty SELECT (`select from t`), empty FROM (`select a from where b = 1`), empty WHERE (`select a from t where`), empty GROUP BY (`select a, count(*) from t group by`), aggregate without alias (`select count(*) from t`). All five reject incidentally inside `parseProjection` at `query/sql/parser.go:553-572` before the reference-style conditions are ever checked; no runtime widening was required. Empty-string and whitespace-only shapes in that reference block are covered by the existing `sub.rs::unsupported` pin bundle.
- next realistic anchors: no narrow reference-backed SQL/query-surface slice remains without runtime widening. Candidates are further Tier-B hardening (α), format-level commitlog parity (β), individual scheduler deferrals (γ), or column-kind widening for `i128`/`u128`/`i256`/`u256`/timestamp/array/product kinds (δ, representation-level). All five `check.rs` reference subscription test blocks plus `statement.rs::invalid`, `sub.rs::unsupported`, `sub.rs::supported` (audited), `sql.rs::unsupported`, and `sql.rs::invalid` are drained for realizable column kinds.
- targeted flaky-test cleanup is closed; no known clean-tree intermittent test remains
- 10 packages, clean-tree full-suite baseline `Go test: 1305 passed in 10 packages`
