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
rtk go doc ./query/sql.UnresolvedVarError
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Slices A, B, C, D, E.1, E.2, F.1, F.2, F.3, F.4 are landed (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error class routing, compile-stage `DuplicateName` + join `UnexpectedType` / `InvalidOp` parity, `Unresolved::Var` text for missing-field lookups, SubscribeSingle projection-column reorder, base-table-after-alias `Unresolved::Var`, SELECT ALL/DISTINCT set-quantifier rejection, WHERE-precedes-projection on single-table SELECT, JOIN ON resolution precedes wildcard guard + WHERE FALSE pruning — see closure summary below).

The next batch is **Slice G**: pick one of the following well-scouted, well-bounded shapes from `TECH-DEBT.md::OI-002`. They are small, self-contained, and do not require additional reference scouting:

1. **G.1 `missing-left-table-precedence`** — `SELECT dup.* FROM missing AS dup JOIN s AS dup ON dup.id = dup.id` should return `` no such table: `missing`. If the table exists, it may be marked private. `` instead of `` Duplicate name `dup` ``. Reorder `query/sql/parser.go::parseJoinClause` (or `parseStatement`) so left-table schema lookup fires BEFORE duplicate-alias detection. Reference `type_from` resolves the left relvar (`type_relvar`) before entering the join-loop duplicate-alias check (`expr/src/check.rs:79-89`). Note: schema lookup currently happens in `protocol/handle_subscribe.go::compileSQLQueryString` AFTER parsing — but the duplicate-alias rejection happens at parse time. Closing the parity gap likely requires either deferring the parser-side dup-alias check or threading a typed `MissingTableError` through that pre-empts the dup-alias arm.

2. **G.2 `qualified-projection-qualifier-not-in-scope`** — `SELECT x.u32 FROM t` should return `` `x` is not in scope `` instead of `parse: unsupported SQL: projection qualifier "x" does not match relation`. Parser-side: route `parser.go::resolveProjectionColumns` (or wherever the projection qualifier mismatch fires) through `UnresolvedVarError{Name: qualifier}`. Reference `type_proj::Exprs` (`expr/src/lib.rs:65-78`) sends the field expression through `type_expr`, whose relvar lookup miss emits `Unresolved::var(&table)` (`expr/src/lib.rs:103`). Mirrors Slice E.2's parser-side reroute pattern.

3. **G.3 `qualified-wildcard-qualifier-not-in-scope`** — `SELECT x.* FROM t` should return `` `x` is not in scope ``. Parser-side: route `parser.go`'s wildcard-qualifier mismatch through `UnresolvedVarError{Name: qualifier}`. Reference `type_proj` checks `input.has_field(&var)` for `Project::Star(Some(var))` and otherwise emits `Unresolved::var(&var)`. Companion to G.2.

G.2 + G.3 share the same parser locus (projection qualifier resolution) and naturally bundle together. G.1 is independent.

## Confirmed Work Queue

The above (G.1, G.2, G.3) are already live-code scouted and recorded in `TECH-DEBT.md::OI-002`. Add failing tests first, then implement.

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

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Adjacent OI-002 Candidates

Recorded in `TECH-DEBT.md::OI-002`. Group with Slice G only if the change locus overlaps; otherwise keep them as separate slices.

- Quoted-identifier case preservation (`SELECT * FROM "T"`, `SELECT * FROM t WHERE "U32" = 7`, alias case preservation, etc.). Reference `SqlIdent` is byte-equal case-sensitive; Shunter currently uses `strings.EqualFold` across schema lookup, column lookup, and alias matching. Larger blast radius; keep separate.
- `JOIN ON` strict-equality guard (`SELECT t.* FROM t JOIN s ON t.id = s.id AND s.id = 7` → `Non-inner joins are not supported`).
- Unqualified-names-in-join unified text (`Names must be qualified when using joins` reference literal across three Shunter-local strings).
- Boolean-constant simplification masking type errors (`SELECT * FROM t WHERE FALSE AND missing = 1` should still emit `` `missing` is not in scope ``). Different from F.3+F.4 (which closed the JOIN ON variant); this is the WHERE-only variant in non-join scope.
- `:sender` exact-case parameter matching (`:SENDER` should reject as `Unsupported expression: :SENDER`).
- SubscribeSingle / OneOff cross-join WHERE Bool-expression admission.
- Inner-join WHERE column comparisons (field-vs-field) admission.
- SubscribeSingle's projection-return guard masking FROM/WHERE errors on projection queries (`SELECT u32 FROM missing_table` should emit the table-not-found text, not the column-projection guard).
- OneOff `LIMIT` numeric parsing (`SELECT * FROM t LIMIT 1e3` should parse as `1000`).
- SubscribeSingle `LIMIT` rejection text (`Unsupported: SELECT * FROM t LIMIT 5` from the reference subscription parser).

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
