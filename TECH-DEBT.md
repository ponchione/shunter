# TECH-DEBT

This file tracks open issues only.
Resolved audit history belongs in git history, not here.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup after parity direction is locked

Parity principles:
- parity is judged by named client-visible scenarios, not helper-level resemblance
- same observable outcome beats same internal mechanism
- every parity change needs an observable test
- divergences must stay explicit
- closed slice history belongs in tests and git history, not in startup docs

Closed parity baselines are not startup context and should not be reopened
without fresh failing regression evidence:
- protocol subprotocol/compression/lifecycle/message-family baselines
- canonical reducer delivery and empty-fanout caller outcomes
- subscription rows through `P0-SUBSCRIPTION-033`
- same-connection reused subscription-hash initial-snapshot elision
- scheduler startup/firing narrow slice
- recovery replay horizon and snapshot/replay invariant slices
- Phase 4 Slice 2 offset-index, typed error category, and record/log documented-divergence slices

Active audit note (2026-04-24):
- hosted-runtime V1 is landed and verified; `docs/hosted-runtime-planning/V1/` is no longer the active implementation campaign
- OI-004 and OI-006 were removed after the post-V1 audit found no concrete remaining open lifecycle or fanout-aliasing defect on the hosted-runtime path
- OI-005 remains open but narrowed to lower-level raw read-view/snapshot lifetime discipline as an accepted expert-API risk
- OI-002 is the expected next parity/runtime-model campaign unless a fresh post-V1 scout changes priority
- do not close parity items solely because they are reachable through the hosted-runtime API; close or narrow them only when the underlying parity/correctness gap is pinned by live tests

## Open issues

### OI-001: Protocol surface is still not wire-close enough to SpacetimeDB

Status: open
Severity: high

Summary:
- all OI-001 A1 wire-shape and measured-duration parity slices identified to date are closed and pinned
- legacy `v1.bsatn.shunter` admission is still accepted as a compatibility deferral
- brotli remains recognized-but-unsupported
- several message-family and envelope details remain intentionally divergent
- client-message decoders still need a body-consumption audit: some decoder paths can accept a valid prefix while ignoring trailing bytes, even though tests/comments around legacy payload rejection imply stricter behavior
- subscribe/unsubscribe handler logic still has avoidable duplication around parsing, lifecycle checks, and response shaping; clean it after the protocol behavior target is pinned
- reducer failure-arm collapse remains an explicit outcome-model follow-up; see `docs/parity-decisions.md#outcome-model`
- rows-shape wrapper-chain parity (`SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `BsatnRowList`) is closed as a documented divergence — see `docs/parity-decisions.md#protocol-rows-shape`. Carried-forward deferral: a coordinated close of the wrapper chain together with the SPEC-005 §3.4 row-list format is a separate multi-slice phase, not an OI-001 A1 wire-close slice.

Why this matters:
- protocol behavior is still one of the biggest blockers to serious parity claims
- even where semantics are close, the wire contract is still visibly Shunter-specific

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
- `docs/parity-decisions.md#outcome-model`
- `docs/parity-decisions.md#protocol-rows-shape`

Execution note:
- With hosted-runtime V1 landed, the next parity execution target is expected to be OI-002 subscription-runtime parity unless a fresh post-V1 audit changes priority. The remaining OI-001 items are narrower compatibility/divergence follow-ons unless a user explicitly asks to reopen protocol wire-close work.

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- A2 is still open, but the closed SQL/query slice history is intentionally not repeated here.
- Slice A (source-text seam + reference parse routing) closed 2026-04-25 (`sql.Literal.Text` + reference `parse(value, ty)` routing on `KindString`, `KindBytes`, `KindBool`, integer / float ranges, plus `:sender` → identity-hex resolver). Closes the source-text-preservation cluster: hex / scientific / leading-sign / leading-zero / round-trip-lossy float forms survive `InvalidLiteral` rendering and `KindString` widening, hex tokens reject on Bool / numeric kinds with the original token, `LitString` and numeric literals route through reference `from_hex_pad` on `KindBytes`, and `:sender` resolves to the identity hex literal before target coerce so non-bytes column kinds route through the same reference shapes (`String(hex)`, `InvalidLiteral{hex, "Bool"}`, etc.).
- Slice B (algebraic-name compound rendering + compound error classes) closed 2026-04-25. `algebraicName` now renders `KindTimestamp` as the SATS Product `(__timestamp_micros_since_unix_epoch__: I64)` and `KindArrayString` as the parameterized `Array<String>`. `KindTimestamp` reject branches route LitString / LitInt / LitFloat / LitBigInt / LitBytes (source-text-bearing) through `InvalidLiteralError` carrying `renderLiteralSourceText(lit)`; LitBool stays on the lib.rs:94 `UnexpectedType` arm. `KindArrayString` routes the same source-text categories to `InvalidLiteralError{Type: Array<String>}` and LitBool to `UnexpectedTypeError{Bool, Array<String>}`. Pinned by coerce-layer unit tests and OneOff raw + SubscribeSingle `WithSql` pairs per error class (`TestHandleOneOffQuery_Parity{TimestampMalformed,BoolLiteralOnTimestamp,StringLiteralOnArrayString,BoolLiteralOnArrayString}RejectText` and the SubscribeSingle counterparts). The `normalizeSQLFilterForRelations` wrapper bypass already passes both error classes through unwrapped, so no protocol-seam change was needed.
- Slice C (compile-stage validation text) closed 2026-04-25. Three new typed errors land alongside the existing `InvalidLiteralError` / `UnexpectedTypeError` cluster: `sql.DuplicateNameError` (renders `` Duplicate name `{name}` ``, mirrors `expr/src/errors.rs:119-121`), `sql.InvalidOpError` (renders `` Invalid binary operator `{op}` for type `{type}` ``, mirrors `expr/src/errors.rs:71-85`), and a re-exported `sql.AlgebraicName(kind)` helper for cross-package compile-stage rendering. Emit sites: (1) `query/sql/parser.go::parseJoinClause` returns `DuplicateNameError{Name: rightAlias}` for both the explicitly-aliased shape (`FROM t AS dup JOIN s AS dup`) and the unaliased self-join shape (`FROM t JOIN t`) — reference treats both identically because `parse_relvar` derives the alias from the base table when no `AS` is written. (2) `protocol/handle_subscribe.go::compileSQLQueryString` adds an `errors.As(err, &sql.DuplicateNameError{})` bypass on the `parse:` wrap so the typed error flows through unwrapped on both OneOff and SubscribeSingle/Multi surfaces. (3) `compileProjectionColumns` and `compileJoinProjectionColumns` interleave a `seen[effectiveName]` HashSet check (effective name = `OutputAlias` if non-empty, else `Column`) so OneOff `SELECT u32 AS dup, i32 AS dup FROM t` and `SELECT u32, u32 FROM t` emit reference `DuplicateName` text. SubscribeSingle still rejects column projections earlier at `Unsupported::ReturnType`, so projection dup-name parity is OneOff-only by design. (4) The JOIN ON kind-mismatch and Array/Product equality arms now emit `UnexpectedTypeError` / `InvalidOpError` from `compileSQLQueryString`'s join branch, BEFORE `subscription/validate.go::validateJoin` would otherwise emit `subscription: invalid predicate: join column kinds differ`. Slot ordering matches reference `UnexpectedType::new(col_type, ty)` at `lib.rs:111-112` — the RIGHT side's column type renders in the `(expected)` slot and the LEFT side's column type (which was passed as the expected for the right) renders in the `(inferred)` slot. The `cross join WHERE column equality` lowering at `compileCrossJoinWhereColumnEquality` mirrors the same routing; the parallel cross-join WHERE admission gating still fires earlier on SubscribeSingle so cross-join WHERE parity remains an open scout candidate. Pinned by OneOff raw + SubscribeSingle WithSql pairs (`TestHandleOneOffQuery_Parity{DuplicateProjectionAlias,DuplicateImplicitProjection,DuplicateJoinAlias,DuplicateSelfJoin,JoinColumnKindMismatch,JoinArrayColumnInvalidOp}RejectText` and the SubscribeSingle counterparts for the four scenarios that survive the column-projection guard).
- Slice F.1 (SELECT ALL / SELECT DISTINCT set-quantifier parser rejection) closed 2026-04-25. New typed error `sql.UnsupportedSelectError{SQL string}` mirrors reference parser/errors.rs:15-19 (`SubscriptionUnsupported::Select` → `Unsupported SELECT: {select}`) and parser/errors.rs:25-26 (`SqlUnsupported::feature` → `Unsupported: {select}`). Two render forms: `Error()` returns the OneOff `Unsupported: ...` shape; `SubscribeError()` returns the subscribe `Unsupported SELECT: ...` shape. `query/sql/parser.go::parseProjection` now rejects an unquoted `ALL` or `DISTINCT` first-projection token through `UnsupportedSelectError{SQL: p.sql}` before `parseProjectionItem` could reinterpret the keyword as a column reference (the reinterpretation previously made OneOff succeed when a column named `ALL`/`DISTINCT` existed and made SubscribeSingle fall through to the column-projection return-type guard). `compileSQLQueryString` lets the typed error flow through unwrapped via the existing `parse:` bypass cluster, and `wrapSubscribeCompileErrorSQL` switches to `SubscribeError()` before applying the `DBError::WithSql` `, executing: ...` wrap. Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_ParityAllModifierRejected` and the existing `TestHandle{OneOffQuery,SubscribeSingle}_ParityDistinctProjectionRejected` pins were tightened from a non-empty-error assertion to the exact reference text.
- Slice F.3+F.4 (JOIN ON resolution precedes bare-wildcard guard and WHERE-FALSE pruning) closed 2026-04-25. `compileSQLQueryString`'s join branch now resolves the ON columns + the JOIN ON type-mismatch / Array-type checks BEFORE the `InvalidWildcard::Join` guard at the bare-`SELECT *` site and BEFORE the `FalsePredicate` short-circuit that was rewriting `WHERE FALSE` to `NoRows`. Mirrors reference `SubChecker::type_from` (check.rs:99-104) which types the ON binop through `type_expr` (lib.rs:101-102) BEFORE `type_proj` runs the bare-wildcard rejection or `type_select` folds WHERE. Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVarBareJoinWildcardOnMissingRejectText` (`SELECT * FROM t JOIN s ON t.missing = s.id`) and `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVarJoinOnMissingNotHiddenByWhereFalseRejectText` (`SELECT t.* FROM t JOIN s ON t.missing = s.id WHERE FALSE`).
- Slice F.2 (WHERE-error precedes projection-error on single-table explicit projections) closed 2026-04-25. `compileSQLQueryString`'s single-table branch now resolves the WHERE predicate (`compileSQLPredicateForRelations`) BEFORE projection-column resolution (`compileProjectionColumns`), mirroring reference `SubChecker::type_set` (check.rs:139-146) `type_proj(type_select(input, expr, vars)?, project, vars)`. Pinned by OneOff raw + SubscribeSingle WithSql pair `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVarWherePrecedesProjectionRejectText` (`SELECT missing FROM t WHERE other_missing = 1` → `` `other_missing` is not in scope ``).
- Slice D (`Unresolved::Var` text parity for missing-field lookups) closed 2026-04-25. New typed error `sql.UnresolvedVarError{Name string}` mirrors reference `expr::errors::Unresolved::Var` at `errors.rs:11-13` (renders `` `{name}` is not in scope ``, Unwrap → `ErrUnsupportedSQL`). Reference emit sites: `_type_expr` lib.rs:103 (relvar lookup miss) and lib.rs:107 (column lookup miss inside an existing relvar). All eight Shunter compile-stage `` `{table}` does not have a field `{column}` `` emit sites in `protocol/handle_subscribe.go` re-routed onto `UnresolvedVarError{Name: column}` — the JOIN ON resolver, cross-join WHERE column-equality resolver, projection-column resolver, `parseQueryString` filter resolver, `normalizeSQLFilterForRelations`, and `NormalizePredicates`. Reference does NOT emit `Unresolved::Field` from any subscription/one-off SELECT path (the two `Unresolved::field` emit sites at `statement.rs:220,340` belong to DML / sub_query statements, out of scope for this slice). Existing pins flipped: `TestNormalizePredicates_UnknownColumn` at handle_subscribe_test.go:357, `TestHandleSubscribe{Single,Multi}_ParityUnknownFieldRejectText`, and `TestHandleOneOffQuery_ParityUnknownFieldRejectText` updated to assert the new `Unresolved::Var` text. New pins: OneOff raw + SubscribeSingle WithSql pairs `TestHandleOneOffQuery_ParityUnresolvedVar{UnqualifiedWhere,ProjectionColumn,JoinOnMissing,JoinWhereQualifiedMissing}RejectText` and the SubscribeSingle counterparts for the three scenarios that survive the column-projection guard (`UnqualifiedWhere`, `JoinOnMissing`, `JoinWhereQualifiedMissing`). Subscription-domain `subscription.ErrColumnNotFound` and `subscription.ErrTableNotFound` paths in `subscription/validate.go` are unchanged — they're a defense-in-depth check on programmatic predicates that don't go through the SQL surface.
- Other open scout candidates (separate, not-yet-bundled):
  - quoted table identifiers should preserve case through table resolution, e.g. with only table `t` registered, `SELECT * FROM "T"` should return raw OneOff ``no such table: `T`. If the table exists, it may be marked private.`` and SubscribeSingle ``no such table: `T`. If the table exists, it may be marked private., executing: `SELECT * FROM "T"` ``. Current Shunter accepts/registers the query through the case-insensitive schema lookup. Reference `SqlIdent` is explicitly case-sensitive and `type_relvar` routes the preserved name through `Unresolved::table`.
  - quoted column identifiers should preserve case through column resolution, e.g. with only column `u32` registered on `t`, `SELECT * FROM t WHERE "U32" = 7` should return raw OneOff `` `U32` is not in scope `` and SubscribeSingle `` `U32` is not in scope, executing: `SELECT * FROM t WHERE "U32" = 7` ``. Current Shunter accepts/registers the query through the case-insensitive column lookup. Reference `SqlIdent` is case-sensitive and `type_expr` field lookup routes the preserved field name through `Unresolved::var`.
  - quoted relation aliases should preserve case through qualifier resolution, e.g. with alias `"R"` declared in `SELECT * FROM t AS "R" WHERE r.u32 = 7`, the lowercase qualifier `r` should return raw OneOff `` `r` is not in scope `` and SubscribeSingle `` `r` is not in scope, executing: `SELECT * FROM t AS "R" WHERE r.u32 = 7` ``. Current Shunter accepts/registers the query through the case-insensitive qualifier map. Reference `SqlIdent` is case-sensitive and unresolved qualifiers are reported through the `Unresolved::var` surface.
  - unquoted identifiers should also preserve case through table resolution, e.g. with only table `t` registered, `SELECT * FROM T` should return raw OneOff ``no such table: `T`. If the table exists, it may be marked private.`` and SubscribeSingle ``no such table: `T`. If the table exists, it may be marked private., executing: `SELECT * FROM T` ``. Current Shunter accepts/registers the query through the case-insensitive schema lookup. Reference `SqlIdent` is explicitly case-sensitive (`sql-parser/src/ast/mod.rs` says case insensitivity would belong in `SqlIdent::from`, but it is not implemented).
  - unquoted column identifiers should preserve case through column resolution, e.g. with only column `u32` registered on `t`, `SELECT * FROM t WHERE U32 = 7` should return raw OneOff `` `U32` is not in scope `` and SubscribeSingle `` `U32` is not in scope, executing: `SELECT * FROM t WHERE U32 = 7` ``. Current Shunter accepts/registers the query through the case-insensitive column lookup. Reference `type_expr` field lookup routes the preserved field name through `Unresolved::var`.
  - unquoted relation aliases should preserve case through qualifier resolution, e.g. with alias `R` declared in `SELECT * FROM t AS R WHERE r.u32 = 7`, the lowercase qualifier `r` should return raw OneOff `` `r` is not in scope `` and SubscribeSingle `` `r` is not in scope, executing: `SELECT * FROM t AS R WHERE r.u32 = 7` ``. Current Shunter accepts/registers the query through the case-insensitive qualifier map. Reference relation variables are keyed by case-sensitive `RawIdentifier`.
  - aliases that differ only by case should not collide, e.g. `SELECT "R".* FROM t AS "R" JOIN s AS r ON "R".id = r.id` and `SELECT R.* FROM t AS R JOIN s AS r ON R.id = r.id` should be accepted when schemas/columns exist. Current Shunter rejects both on OneOff raw and SubscribeSingle `WithSql` with ``Duplicate name `r` `` because `parseJoinClause` uses `strings.EqualFold` for alias collisions. Reference `type_from` checks a `HashMap<RawIdentifier, ...>` (`expr/src/check.rs:82-89`), and `RawIdentifier` derives case-sensitive `Eq` / `Hash`.
  - missing left-table resolution should precede duplicate join-alias validation, e.g. with only table `s` registered, `SELECT dup.* FROM missing AS dup JOIN s AS dup ON dup.id = dup.id` should return raw OneOff ``no such table: `missing`. If the table exists, it may be marked private.`` and SubscribeSingle ``no such table: `missing`. If the table exists, it may be marked private., executing: `SELECT dup.* FROM missing AS dup JOIN s AS dup ON dup.id = dup.id` ``. Current Shunter rejects at the parser with ``Duplicate name `dup` `` before schema lookup. Reference `type_from` resolves the left relvar (`type_relvar`) before entering the join-loop duplicate-alias check (`expr/src/check.rs:79-89`).
  - duplicate join-alias validation should precede right-table resolution, e.g. with only table `t` registered, `SELECT dup.* FROM t AS dup JOIN missing AS dup ON dup.id = dup.id` should return raw OneOff ``Duplicate name `dup` `` and SubscribeSingle ``Duplicate name `dup`, executing: `SELECT dup.* FROM t AS dup JOIN missing AS dup ON dup.id = dup.id` ``. Current Shunter returns ``no such table: `missing`. If the table exists, it may be marked private.`` because `compileSQLQueryString` looks up the right table before emitting the deferred `AliasCollision`. Reference `type_from` checks `vars.contains_key(&alias)` before `Self::type_relvar(tx, &name)` for each joined RHS (`expr/src/check.rs:82-94`).
  - unquoted boolean keywords in projection should stay literal expressions rather than falling through as column names, e.g. with a real lowercase column `true` on table `t`, `SELECT true FROM t` should return raw OneOff `Unsupported projection expression: true` and SubscribeSingle ``Unsupported projection expression: true, executing: `SELECT true FROM t` ``. Current Shunter accepts the OneOff query when the column exists and SubscribeSingle reports the column-projection return guard. Reference `parse_project_elem` / `parse_proj` routes `Expr::Value(Value::Boolean(_))` through `SqlUnsupported::ProjectionExpr` (`sql-parser/src/parser/mod.rs:186-206`); scope is unquoted `TRUE` / `FALSE` projection tokens only, not quoted `"true"` / `"false"` identifiers.
  - explicit qualified column projection qualifiers that are not in scope should use `Unresolved::Var`, e.g. `SELECT x.u32 FROM t` should return raw OneOff `` `x` is not in scope `` and SubscribeSingle `` `x` is not in scope, executing: `SELECT x.u32 FROM t` ``. Current Shunter rejects at parser qualifier resolution with `parse: unsupported SQL: projection qualifier "x" does not match relation` on OneOff and the `WithSql`-wrapped counterpart on SubscribeSingle. Reference `type_proj::Exprs` (`expr/src/lib.rs:65-78`) sends the field expression through `type_expr`, whose relvar lookup miss emits `Unresolved::var(&table)` (`expr/src/lib.rs:103`).
  - qualified projection qualifier checks should not mask earlier FROM/WHERE type errors, e.g. with only table `t` registered, `SELECT x.id FROM missing_table` should return raw OneOff ``no such table: `missing_table`. If the table exists, it may be marked private.`` and `SELECT x.id FROM t WHERE missing = 1` should return raw OneOff `` `missing` is not in scope ``; SubscribeSingle should carry the corresponding `WithSql` suffixes. Current Shunter emits `` `x` is not in scope `` for both because `parseStatement` resolves `ProjectionColumns` before schema lookup and before parsing/typing WHERE. Reference `SqlChecker::type_set` / `SubChecker::type_set` run `type_from` and `type_select` before `type_proj` (`expr/src/statement.rs:419`, `expr/src/check.rs:139-146`).
  - `JOIN ON` must stay limited to a pure qualified-column equality, e.g. `SELECT t.* FROM t JOIN s ON t.id = s.id AND s.id = 7` should return raw OneOff `Non-inner joins are not supported` and SubscribeSingle `Non-inner joins are not supported, executing: ...`. Current Shunter accepts/registers the query by treating the extra `AND` conjunct as a join filter. Reference `sql-parser/src/parser/mod.rs::parse_join` only admits `JoinConstraint::On` when the top-level expression is `qualified_identifier = qualified_identifier`; every other join operator/constraint routes to `SqlUnsupported::JoinType`.
  - explicit `CROSS JOIN` should be parsed as a join operator, not as a left-table alias, e.g. `SELECT t.* FROM t CROSS JOIN s` should be accepted as a cross join, while `SELECT CROSS.* FROM t CROSS JOIN s` should return raw OneOff `` `CROSS` is not in scope `` and SubscribeSingle `` `CROSS` is not in scope, executing: `SELECT CROSS.* FROM t CROSS JOIN s` ``. Current Shunter returns `` `t` is not in scope `` for the first shape and accepts the second because `parseRelationQualifiers` consumes `CROSS` as an alias before `parseJoinClause` sees `JOIN`. Reference `RelParser::parse_join` accepts `JoinOperator::CrossJoin` as `SqlJoin { on: None }` (`sql-parser/src/parser/mod.rs:50-53`).
  - explicit non-inner join keywords should not be swallowed as aliases, e.g. `SELECT LEFT.* FROM t LEFT JOIN s ON LEFT.id = s.id` should return raw OneOff `Non-inner joins are not supported` and SubscribeSingle ``Non-inner joins are not supported, executing: `SELECT LEFT.* FROM t LEFT JOIN s ON LEFT.id = s.id` ``. Current Shunter accepts the query by treating `LEFT` as the left relation alias and the following `JOIN` as a supported inner/cross join. Reference `RelParser::parse_join` routes every non-cross/non-inner join operator through `SqlUnsupported::JoinType` (`sql-parser/src/parser/mod.rs:50-72`, `parser/errors.rs:66`).
  - a standalone `INNER` after the FROM relation should be rejected instead of ignored, e.g. `SELECT * FROM t INNER` should fail admission on OneOff and SubscribeSingle. Current Shunter accepts it as if the query were `SELECT * FROM t` because `parseStatement` advances an optional `INNER` before checking whether `JOIN` follows. Reference parsing only admits `INNER` as part of a `JoinOperator::Inner(...)` produced by the SQL parser and has no standalone-`INNER` relation form (`sql-parser/src/parser/mod.rs:50-72`, `parser/sql.rs:121-125`).
  - SubscribeSingle unindexed join rejection should use the reference compile-stage text and `WithSql` wrapper, e.g. with no index on either join column, `SELECT t.* FROM t JOIN s ON t.id = s.id` should return ``Subscriptions require indexes on join columns, executing: `SELECT t.* FROM t JOIN s ON t.id = s.id` ``. Current Shunter compiles the SQL, submits to subscription validation, and returns `subscription: join column has no index on either side: join ...` without the SQL suffix. Reference `SubscriptionPlan::compile` rejects non-index join plans with `bail!("Subscriptions require indexes on join columns")` (`subscription/src/lib.rs:518-519`), and SubscribeSingle wraps `compile_query_with_hashes` failures with `DBError::WithSql` (`module_subscription_actor.rs:277-285,643-650`).
  - unknown qualified wildcard projection qualifiers should use `Unresolved::Var`, e.g. `SELECT x.* FROM t` should return raw OneOff `` `x` is not in scope `` and SubscribeSingle `` `x` is not in scope, executing: `SELECT x.* FROM t` ``. Current Shunter rejects at parser qualifier matching with `parse: unsupported SQL: projection qualifier "x" does not match table "t"`. Reference `type_proj` checks `input.has_field(&var)` for `Project::Star(Some(var))` and otherwise emits `Unresolved::var(&var)`.
  - boolean constant simplification must not hide type-checking errors in the other branch, e.g. `SELECT * FROM t WHERE FALSE AND missing = 1` / `SELECT * FROM t WHERE TRUE OR missing = 1` should still emit raw OneOff `` `missing` is not in scope `` and SubscribeSingle `WithSql`, and `SELECT * FROM t WHERE FALSE AND u32 = 1.5` should still emit ``The literal expression `1.5` cannot be parsed as type `U32` ``. Current Shunter normalizes those predicates to `FalsePredicate` / `TruePredicate` before column and literal resolution, so OneOff accepts and SubscribeSingle registers. Reference `_type_expr` types both sides of `SqlExpr::Log` before lowering the boolean operator.
  - inner-join `WHERE` column comparisons should be admitted and evaluated against the joined row, e.g. `SELECT t.* FROM t JOIN s ON t.id = s.t_id WHERE t.code = s.code` should return only joined pairs whose `code` columns match. Current Shunter parses the shape but OneOff raw returns `unsupported SQL predicate sql.ColumnComparisonPredicate`, and SubscribeSingle wraps the same text with `WithSql`. Reference `type_select` sends the full `WHERE` expression through Bool `type_expr` on the joined `RelExpr`, and `type_expr` supports field-vs-field binary comparisons.
  - join `WHERE` filters with `OR` across both relation sides should evaluate the boolean expression against each joined pair, e.g. `SELECT t.* FROM t JOIN s ON t.id = s.t_id WHERE t.keep = TRUE OR s.keep = TRUE` should reject the `(FALSE, FALSE)` pair. Current Shunter evaluates `Join.Filter` side-by-side with `MatchRowSide`; other-side leaves pass through as true under `Or`, so the `(FALSE, FALSE)` joined row is admitted. Reference `type_select` routes the whole `WHERE` expression through Bool `type_expr` on the joined `RelExpr`; scope is bounded to `OR` disjuncts that reference different join sides.
  - cross joins with ordinary boolean `WHERE` filters should be admitted and evaluated, e.g. `SELECT t.* FROM t JOIN s WHERE t.keep = TRUE` should return the cartesian rows for matching `t` rows. Current Shunter's cross-join lowering requires exactly one qualified column equality and raw OneOff returns `cross join WHERE only supports qualified column equality` (SubscribeSingle wraps the related `cross join WHERE not supported` admission path with `WithSql`). Reference `SubChecker` / `SqlChecker` applies `type_select` to filtered joins before projection, so this is a parser-admitted query/runtime gap separate from the existing cross-join column-equality lowering.
  - OneOff cross joins with mixed explicit projection columns should source each projected field from its named relation, e.g. with rows `t.value = 10` and `s.value = 20`, `SELECT t.value AS tv, s.value AS sv FROM t JOIN s` should emit `(10, 20)`. Current Shunter emits `(10, 10)` because the CrossJoin one-off path materializes only the projected side's rows and then `projectOneOffRows` indexes both projection columns into that same row. Reference `SqlChecker::type_set` applies `type_proj(Self::type_from(...), project, vars)` for SELECT without WHERE (`expr/src/statement.rs:419`) and `type_proj::Exprs` resolves each `ProjectExpr::Field` against the full joined relvar set (`expr/src/lib.rs:65-78`).
  - cross-join `WHERE` column equality type mismatches now emit the reference `UnexpectedType` text on the OneOff surface (closed by Slice C — `compileCrossJoinWhereColumnEquality` mirrors the JOIN ON pre-check). SubscribeSingle still rejects earlier with `cross join WHERE not supported, executing: ...` because the cross-join WHERE admission gate is tied to `allowProjection`. Closing the SubscribeSingle side requires routing cross-join WHERE through the same compile path that OneOff uses, which is the broader cross-join-WHERE-as-Bool-expression slice above and not a fixed-literal parity slice.
  - OneOff `LIMIT` numeric parsing should route through reference `type_limit` / `parse_int(..., U64)`: `SELECT * FROM t LIMIT 1e3` should parse as limit `1000`, while `SELECT * FROM t LIMIT 1.5` should emit ``The literal expression `1.5` cannot be parsed as type `U64` ``. Current Shunter `parseLimit` uses `strconv.ParseUint` directly and raw OneOff returns `parse: unsupported SQL: LIMIT requires an unsigned integer literal` for both shapes. Scope is one-off/ad hoc SQL only; SubscribeSingle's general limit rejection is a separate subscription-surface guard.
  - base-table qualifiers used after an alias should emit reference `Unresolved::Var` text, e.g. `SELECT * FROM t AS r WHERE t.u32 = 5` should return raw OneOff `` `t` is not in scope `` and SubscribeSingle `` `t` is not in scope, executing: `SELECT * FROM t AS r WHERE t.u32 = 5` ``. Current Shunter rejects at the parser with `parse: unsupported SQL: qualified column "t" does not match relation` on both surfaces.
- No queued active child issue; same-connection reused subscription-hash initial-snapshot elision is closed and pinned by `subscription/register_set_test.go::TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate` and `TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot`. `SubscriptionError.table_id` on request-origin error paths now always emits `None` (reference v1 parity); pinned by `executor/protocol_inbox_adapter_test.go::TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleTableErrorEmitsNilTableID` alongside the pre-existing multi-table nil pin.
- Remaining broad risks: the supported SQL surface is still narrower than the reference path, row-level security / per-client filtering is absent, and subscription behavior still spans several seams rather than one fully parity-locked contract.
- Legacy structured-query remnants remain alongside the SQL path: `Query` / `Predicate` wire types, `compileQuery`, `parseQueryString`, and one-off column match helpers make the live query model harder to reason about.
- One-off and subscription tests duplicate large scenario blocks; this makes future query behavior changes more expensive and increases the chance that one path drifts from the other.
- `subscription/eval.go` contains a dead per-evaluation memoization map: it stores query hash results but never reads them. The actual useful duplicate suppression appears to live in fanout batching, so this should be removed or reconnected deliberately.

Execution note:
- `NEXT_SESSION_HANDOFF.md` owns the immediate OI-002 startup path.
- Do not read or reproduce the closed `P0-SUBSCRIPTION-*` sequence for new work; tests and git history are the archive.
- Choose the next OI-002 batch by a fresh scout; do not carry forward historical handoff targets.

Why this matters:
- the system can look architecturally right while still behaving differently under realistic subscription workloads
- query-surface limitations still cap how close clients can get to reference behavior

Primary code surfaces:
- `query/sql/parser.go`
- `protocol/handle_subscribe.go`
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

### OI-003: Recovery and store semantics still differ in user-visible ways

Status: open
Severity: high

Summary:
- value-model and changeset semantics remain simpler than the reference
- commitlog/recovery behavior is intentionally rewritten rather than format-compatible
- replay tolerance, sequencing, and snapshot/recovery behavior still need follow-through

Why this matters:
- storage and recovery semantics are central to the operational-replacement claim
- sequencing and replay mismatches are the kind of differences users feel after crash/restart

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
- `docs/parity-decisions.md#commitlog-record-shape`

### OI-005: Lower-level read-view/snapshot lifetime discipline remains an expert-API contract

Status: open — narrowed to accepted lower-level/expert API risk
Severity: low

Summary:
- hosted-runtime V1-F closes the normal root-runtime read-path concern: `Runtime.Read(ctx, fn)` exposes a callback-scoped `LocalReadView`, defers committed snapshot close before returning, and is pinned by tests for readiness/closed-state behavior, committed-row access, and post-read commit progress
- the previously identified snapshot/StateView aliasing and use-after-close sub-hazards are closed and pinned by store, subscription, and executor regression tests
- the concrete executor post-commit panic-close gap is now closed: `executor.postCommit` defers the acquired committed read-view close immediately after `snapshotFn()`, and `TestPostCommitPanicInEvalSetsFatal` asserts the view is closed even when `EvalAndBroadcast` panics
- remaining risk is intentionally lower-level and specific: raw `store.CommittedState.Snapshot()` / `store.CommittedReadView` still require caller-owned explicit close; `CommittedState.Table` and `StateView` still rely on documented envelope/single-executor discipline; subscription committed views remain borrowed and must not escape
- `Runtime.Read` callbacks remain snapshot-scoped and should not synchronously wait on reducer/write work while holding the snapshot; treat that as expert API discipline unless a concrete normal-runtime deadlock reproducer appears

Why this matters:
- leaked raw committed snapshots can stall commits until explicitly closed or until the best-effort finalizer runs
- the root runtime API and executor post-commit path no longer expose a known unclosed-snapshot path
- the remaining lower-level APIs preserve v1 simplicity but require callers to honor explicit read-view ownership rules

Primary code surfaces:
- `runtime_local.go`
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/delta_view.go`
- `executor/executor.go`

Source docs:
- `docs/hosted-runtime-planning/V1/V1-F/`
- `docs/decomposition/hosted-runtime-v1-contract.md`
- `docs/hosted-runtime-implementation-roadmap.md`

Audit note:
- keep OI-005 as the accepted lower-level/expert API discipline marker; do not reopen it for the now-pinned executor post-commit panic-close gap unless a fresh concrete leak/reproducer appears

### OI-007: Recovery sequencing and replay-edge behavior is narrowed to remaining format/scheduler deferrals

Status: open — narrowed after reader-side zero-header EOS closure
Severity: medium

Summary:
- reader-side zero-header EOS / preallocated-zero-tail tolerance is now closed and pinned: `DecodeRecord` and recovery scanning treat an all-zero Shunter record header as end-of-stream, so `ScanSegments` / `ReplayLog` stop at the last real tx instead of classifying preallocated zero tails as damaged user data
- authoritative pins: `commitlog/replay_test.go::TestReplayLogPreallocatedZeroTailStopsAtLastRecord` and `commitlog/wire_shape_test.go::TestWireShapeShunterZeroRecordHeaderActsAsEOS`
- remaining live carried-forward deferrals from Phase 4 Slice 2γ (no broader wire-format rewrite landed; 2γ remains a documented-divergence slice):
  - reference byte-compatible magic (`(ds)^2` vs `SHNT`)
  - commit grouping (N-tx framing unit)
  - `epoch` field + `set_epoch` API
  - V0/V1 version split
  - writer-side preallocation/fallocate support (reader tolerance is in place, but Shunter still does not emit preallocated segment files)
  - checksum-algorithm negotiation rename
  - forked-offset detection (`Traversal::Forked`)
  - full records-buffer format parity (couples to BSATN / types / schema / subscription / executor)
  - `Append<T>` payload-return API
- remaining scheduler deferrals stay open (see `docs/parity-decisions.md#scheduler-startup-and-firing`)

Why this matters:
- these gaps mainly show up under restart, crash, and replay conditions
- they materially affect the operational-replacement claim

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-decisions.md#scheduler-startup-and-firing`
- `docs/parity-decisions.md#commitlog-record-shape`

### OI-008: Cleanup-only test and label debt obscures the live behavior

Status: open
Severity: medium

Summary:
- stale test names and labels still point at retired docs or closed audit slices, including `OI-004`, `OI-006`, `TD-057`, `P0-DELIVERY-*`, and phase-style acceptance labels
- `commitlog/phase4_acceptance_test.go::TestDurabilityWorkerBatchesAndFsyncs` has dead fsync-count instrumentation (`countingSegmentWriter`, `syncCount`, and `_ = counting`) that no longer validates the behavior its name implies
- several async tests rely on fixed `time.Sleep` windows, especially in fanout-worker coverage; these should move to condition/event based waits before the suite grows more parallel or slower
- duplicated protocol scenario tests should be collapsed where they are testing shared behavior rather than genuinely different one-off vs subscription contracts
- historical hosted-runtime planning files still contain superseded sequencing notes, such as older V1-G plans describing V1-H as the immediate next slice; prune or archive these when hosted-runtime planning resumes
- dead-code tooling is not part of the local validation path yet; `rtk staticcheck ./...` was unavailable during the sweep, and `go vet` does not catch several of these cleanup issues

Why this matters:
- stale labels make failure output point maintainers toward closed or nonexistent work
- duplicated tests and fixed sleeps slow down behavior changes while still missing some real regressions
- dead instrumentation gives a false sense that low-level durability behavior is being asserted

Primary code surfaces:
- `commitlog/phase4_acceptance_test.go`
- `protocol/*_test.go`
- `subscription/fanout_worker_test.go`
- `subscription/eval.go`
- `docs/hosted-runtime-planning/`

## Deferred issues

### DI-001: Energy accounting remains a permanent parity deferral

Status: deferred
Severity: low

Summary:
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem

Why this matters:
- this is an intentional parity gap and should stay explicit

Source docs:
- `docs/parity-decisions.md#outcome-model`
