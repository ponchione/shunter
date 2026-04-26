# Next Session Handoff

Use this file to start the next parity / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go list -json ./query/sql ./subscription ./protocol ./executor
rtk go doc ./query/sql.UnsupportedSelectError
rtk go doc ./query/sql.UnsupportedFeatureError
rtk go doc ./query/sql.UnresolvedVarError
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Slices A, B, C, D, E.1, E.2, F.1, F.2, F.3, F.4, G.1, G.2, G.3, H, I, `sender-exact-case`, scout cleanup, boolean-constant WHERE masking, signed-LIMIT feature rejection, and join-keyword handling are landed / confirmed green (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error class routing, compile-stage `DuplicateName` + join `UnexpectedType` / `InvalidOp` parity, `Unresolved::Var` text for missing-field lookups, SubscribeSingle projection-column reorder, base-table-after-alias `Unresolved::Var`, SELECT ALL/DISTINCT set-quantifier rejection, WHERE-precedes-projection on single-table SELECT, JOIN ON resolution precedes wildcard guard + WHERE FALSE pruning, missing-table precedes duplicate-join-alias, qualified projection / wildcard qualifier not in scope, unqualified names in joins, strict JOIN ON equality, exact-case `:sender`, subscription LIMIT text, one-off LIMIT numeric parsing, qualified-name ordering, logical-WHERE branch typechecking, signed LIMIT feature-text ordering, and explicit join keyword parsing — see closure summary below).

Pick the next batch from `TECH-DEBT.md::OI-002`. The remaining well-bounded fixed-literal shape that has already been scouted and does not require fresh reference research:

1. **`subscribe-unindexed-join-rejection-text`** — SubscribeSingle unindexed join rejection should use `Subscriptions require indexes on join columns, executing: ...` instead of the current late `subscription: join column has no index on either side: join ...` text. Locus likely crosses `protocol/handle_subscribe*.go` and `subscription/validate.go`; keep this separate from parser-only slices.

Keep case-preservation and broader join/cross-join runtime semantics separate unless the chosen fix naturally requires them.

## Confirmed Work Queue

The above are recorded in `TECH-DEBT.md::OI-002`. Add failing tests first, then implement. Batch only when the locus overlaps; commit per slice if the working tree is clean enough to do so without sweeping unrelated user changes into the commit.

## Closed Guardrails

Do not reopen `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033` without fresh failing regression evidence.

Also treat these recently closed surfaces as done unless a new failing test proves otherwise:

- Same-connection reused subscription-hash initial-snapshot elision
- `SubscriptionError.table_id` on request-origin subscribe/unsubscribe errors
- SubscribeSingle / SubscribeMulti compile-origin error text parity
- SubscribeSingle / SubscribeMulti initial-eval error text parity
- UnsubscribeSingle / UnsubscribeMulti final-eval error text parity
- `SELECT *` on `JOIN` rejection text across subscribe and one-off paths
- `Unresolved::Table` literal across subscribe (WithSql-wrapped) and one-off (raw)
- `Unresolved::Var` literal across all eight compile-stage emit sites in `protocol/handle_subscribe.go` (Slice D)
- `Unsupported::ReturnType` literal unified across the aggregate and column-list subscribe-projection guards
- `UnexpectedType` literal for bool literals against non-bool primitive columns
- `InvalidLiteral` literal for integer-range overflow / negative-on-unsigned (32/64/128/256-bit), LitFloat → integer, and non-Bool primitive → KindBool
- **Widening** parity for LitInt / LitFloat / LitBigInt / LitBytes (parser-source) → `KindString` and LitString numeric token / LitBytes-with-Text → integer/float kinds
- **Source-text seam** on `sql.Literal.Text` populated at `parseLiteral` and `parseNumericLiteral`
- **`KindBytes` reference routing** through `decodeReferenceHex`
- **`:sender` reference resolution** at the top of `coerceValue` mirrors reference `resolve_sender`
- **`KindTimestamp` / `KindArrayString` algebraic-name + error-class parity (Slice B)**
- **Compile-stage `DuplicateName` / `UnexpectedType` / `InvalidOp` parity (Slice C)**
- **`Unresolved::Var` parity for missing-field lookups (Slice D)**
- **SubscribeSingle `SELECT missing FROM t` projection-column reorder (Slice E.1)** — `compileProjectionColumns` runs BEFORE the `allowProjection=false` column-list guard. The `TestHandleSubscribeSingle_Parity{Aggregate,ColumnList}ReturnTypeRejectText` pins now use existing column names; new pin `TestHandleSubscribeSingle_ParityUnresolvedVarProjectionColumnRejectText` covers the missing-column case under SubscribeSingle column-list projection.
- **Base-table-after-alias `Unresolved::Var` parity (Slice E.2)** — `query/sql/parser.go::parseQualifiedColumnRef` and `parseColumnRefForPredicate` route the `qualified column %q does not match relation` rejection through `UnresolvedVarError{Name: qualifier}`. Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVarBaseTableAfterAliasRejectText`.
- **`SELECT ALL` / `SELECT DISTINCT` set-quantifier rejection (Slice F.1, 2026-04-25)** — New typed error `sql.UnsupportedSelectError{SQL string}` with two render forms (`Error()` for OneOff `Unsupported: ...`, `SubscribeError()` for subscribe `Unsupported SELECT: ...`). `parseProjection` rejects unquoted `ALL`/`DISTINCT` first-projection token through the typed error before column reinterpretation. `wrapSubscribeCompileErrorSQL` switches to `SubscribeError()` before applying the WithSql wrap. Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_Parity{AllModifierRejected,DistinctProjectionRejected}`.
- **WHERE precedes projection on single-table SELECT (Slice F.2, 2026-04-25)** — `compileSQLQueryString`'s single-table branch resolves WHERE BEFORE projection-column resolution. Pinned by `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVarWherePrecedesProjectionRejectText` (`SELECT missing FROM t WHERE other_missing = 1` → `` `other_missing` is not in scope ``).
- **JOIN ON resolution precedes bare-wildcard guard + WHERE FALSE pruning (Slice F.3+F.4, 2026-04-25)** — `compileSQLQueryString`'s join branch resolves the ON columns + ON type-mismatch / Array-type checks BEFORE `InvalidWildcard::Join` and BEFORE the `FalsePredicate` short-circuit. Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVar{BareJoinWildcardOnMissing,JoinOnMissingNotHiddenByWhereFalse}RejectText`.
- **Missing-table precedes duplicate join alias (Slice G.1, 2026-04-25)** — `JoinClause.AliasCollision` flag added; parser-side `LeftAlias == RightAlias` no longer emits `DuplicateNameError` directly. Parser drains remaining tokens via `consumeUntilStatementEnd()` and returns JoinClause with `AliasCollision: true, HasOn: false`; compile-stage `compileSQLQueryString` emits `sql.DuplicateNameError{Name: stmt.Join.LeftAlias}` AFTER both schema lookups succeed. Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_ParityMissingLeftTablePrecedesDuplicateJoinAliasRejectText`. Existing parser-level pins renamed `TestParseDefers{DistinctTableDuplicateJoinAliases,UnaliasedSelfCrossJoin}` (no parse error; assert `AliasCollision: true`).
- **Qualified projection / wildcard qualifier not in scope (Slice G.2+G.3, 2026-04-25)** — All three projection-qualifier mismatch sites in `query/sql/parser.go` reroute to `UnresolvedVarError{Name: qualifier}`: `resolveProjectionColumns` (qualified column shape `SELECT x.u32 FROM t`); `parseStatement` JOIN-arm and single-table-arm wildcard mismatches (`SELECT x.* FROM t [JOIN s ON ...]`). Pinned by OneOff raw + SubscribeSingle WithSql pairs `TestHandle{OneOffQuery,SubscribeSingle}_ParityUnresolvedVarQualified{Projection,Wildcard}QualifierRejectText`.
- **Unqualified names in joins (Slice H, confirmed 2026-04-25)** — Live code already routes bare join projection, bare join WHERE, and bare JOIN ON operand shapes through `sql.UnqualifiedNamesError` (`Names must be qualified when using joins`). Confirmed by `rtk go test ./protocol ./query/sql -count=1 -run 'ParityUnqualifiedNames' -v`.
- **Strict JOIN ON equality (Slice I, 2026-04-25)** — New typed error `sql.UnsupportedJoinTypeError` mirrors `SqlUnsupported::JoinType` (`Non-inner joins are not supported`). `parseJoinClause` rejects non-`=` JOIN ON operators and trailing `AND`/`OR` ON expressions instead of folding them into filters; `compileSQLQueryString` lets the typed error bypass `parse:` wrapping. Pinned by `TestParseRejectsJoinOnNonPureEquality`, `TestHandleOneOffQuery_ParityJoinOnStrictEqualityRejectText`, and `TestHandleSubscribeSingle_ParityJoinOnStrictEqualityRejectText`.
- **Exact-case `:sender` (confirmed 2026-04-25)** — Live code already accepts only byte-equal `:sender` and routes `:SENDER` through `sql.UnsupportedExprError` (`Unsupported expression: :SENDER`). Confirmed by `rtk go test ./protocol ./query/sql -count=1 -run 'ParitySenderParameterCaseSensitive' -v`.
- **Scout cleanup slice (qualified-name / LIMIT / bytes-hex ordering, 2026-04-25)** — Parser resolution carries table aliases, unresolved projection / WHERE / JOIN ON qualifiers, and invalid/unsupported LIMIT clauses through to `compileSQLQueryString` so `type_from` / duplicate-alias / JOIN ON / WHERE / projection / LIMIT ordering matches reference. `compileSQLQueryString` resolves JOIN WHERE before projection/aggregate return guards, validates qualified wildcard projections after FROM/JOIN checks, routes unknown qualified filters/projections through `Unresolved::Var`, applies one-off LIMIT parsing through the reference U64 literal path, and rejects subscription LIMIT through `sql.UnsupportedFeatureError`. `KindBytes` string-literal hex routing strips only lowercase `0x` or a leading `X'` prefix. Pinned by tracked `protocol/oi002_*_scout_tmp_test.go` files plus `TestHandleOneOffQuery_Parity{ScientificLimitLiteralApplies,FractionalLimitLiteralRejected}` and parser deferral pins.
- **Boolean constant WHERE branch typechecking (2026-04-25)** — `compileSQLQueryString` validates the original logical predicate tree before using `normalizeSQLPredicate` for the final subscription predicate shape. `FALSE AND missing = 1`, `TRUE OR missing = 1`, and `FALSE AND u32 = 1.5` now emit the branch `Unresolved::Var` / `InvalidLiteral` text on OneOff and SubscribeSingle. Pinned by `TestHandle{OneOffQuery,SubscribeSingle}_ParityBooleanConstantWhereDoesNotMaskBranchErrors`.
- **LIMIT feature rejection ordering (2026-04-25)** — `query/sql/parser.go::parseLimit` marks leading `+` / `-` LIMIT tokens and non-numeric LIMIT expressions as `UnsupportedLimit`, and `compileSQLQueryString` emits `sql.UnsupportedFeatureError` before schema/type checking on the OneOff path. Pinned by `TestOI002LimitScout_{LeadingPlusLimitRejectedByReferenceParser,NegativeLimitRejectedByReferenceParser,SignedLimitRejectedBeforeMissingTable,NonNumericLimitRejectedBeforeProjection}`.
- **Join keywords not aliases (2026-04-25)** — `parseRelationQualifiers` no longer consumes explicit join modifiers as implicit aliases; `parseStatement` admits `CROSS JOIN` as a cross join, routes non-inner join starts through `sql.UnsupportedJoinTypeError`, and rejects dangling `INNER` / `CROSS` modifiers that are not followed by `JOIN`. Pinned by `TestParse{CrossJoinKeywordIsJoinOperator,RejectsNonInnerJoinKeyword,RejectsDanglingInnerJoinKeyword}` and `TestHandleOneOffQuery_Parity{CrossJoinKeywordNotAlias,LeftJoinKeywordRejected}`.

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Adjacent OI-002 Candidates

Recorded in `TECH-DEBT.md::OI-002`. Group only if the change locus overlaps; otherwise keep them as separate slices.

- Quoted-identifier case preservation (`SELECT * FROM "T"`, `SELECT * FROM t WHERE "U32" = 7`, alias case preservation, etc.). Reference `SqlIdent` is byte-equal case-sensitive; Shunter currently uses `strings.EqualFold` across schema lookup, column lookup, and alias matching. Larger blast radius; keep separate.
- SubscribeSingle / OneOff cross-join WHERE Bool-expression admission. Broader runtime/parser surface than fixed-literal — keep separate.
- Inner-join WHERE column comparisons (field-vs-field) admission. Same broader surface as cross-join WHERE.

### Remaining Scout Budget

Only scout further after at least one confirmed slice above is implemented and green. If you do scout, keep it to one parser-admitted SQL shape and immediately either add it to `TECH-DEBT.md` or discard it as a pass.

## Prior Dead Ends

Do not rescout these without fresh evidence:

- Subscription-manager bookkeeping admission text parity. Reference emit sites mostly format Rust tuples with Debug output, so this is not a clean fixed-literal parity slice.
- `TotalHostExecutionDurationMicros` measurement accuracy. Both implementations measure elapsed time from request receipt.
- Cardinality row-limit text parity. Reference uses planner estimates while Shunter uses actual runtime row counts; parity would require a cardinality estimator.
- One-off mutation rejection text. Shunter has no current view-write failure mode that triggers the reference literal.
- Event-table websocket-v2 rejection text. Shunter has no event-table equivalent yet.
- `InvalidOp` text parity for primitive types. Reference `op_supports_type` accepts every primitive, so the emit sites at `lib.rs:130,138` are unreachable on primitive inputs. (Slice C closed the Array<…> emit path; Product columns remain unreachable through Shunter's column-storage surface because `schema/validate_structure.go::isValidValueKind` rejects Product as a column type.)
- `UnexpectedType` from `lib.rs:112` for column-vs-column binop mismatch in WHERE clauses (not JOIN ON) is unreachable through Shunter's `cmp = colref op literal` WHERE grammar. JOIN ON is reachable and was closed in Slice C.
- `Unresolved::Field` literal — reference does NOT reach `Unresolved::Field` from any subscription/one-off SELECT path. The two `Unresolved::field` emit sites at `statement.rs:220,340` belong to DML / sub_query statements which Shunter does not surface. All field-lookup miss paths now emit `Unresolved::Var` (Slice D).

## Out Of Scope

- SQL surface widening beyond what the parser already admits
- Fanout/QueryID correlation redesign
- Reopening closed parity rows without fresh failing evidence
- Non-OI-002 tech-debt

## Validation

```bash
rtk go test <touched packages> -count=1 -v
rtk go fmt <touched packages>
rtk go vet <touched packages>
rtk go test ./... -count=1
```

## Doc Follow-Through

After the implementation is green:

- update `TECH-DEBT.md::OI-002` summary only if the closure removes a risk listed there
- rewrite this handoff to the next live target, keeping startup reading minimal and only future-relevant state

## Working Tree Caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves. Do not mix those into a TECH-DEBT / OI-002 implementation slice unless the user explicitly asks.

At this handoff update, `rtk git status --short` shows only tracked modified files from the OI-002 bounded-fix group. The `protocol/oi002_*_scout_tmp_test.go` files are tracked protocol tests now, and `rtk go test ./protocol ./query/sql -count=1` passes with them included.
