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
rtk go doc ./query/sql.Coerce
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Slices A, B, C, and D are landed (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error class routing, compile-stage `DuplicateName` + join `UnexpectedType` / `InvalidOp` parity, `Unresolved::Var` text for missing-field lookups — see closure summary below). The next batch is **Slice E: SubscribeSingle column-projection guard reorder + base-table-after-alias `Unresolved::Var` parity**. Both shapes touch the same compile-stage seam in `protocol/handle_subscribe.go::compileSQLQueryString` (the `allowProjection=false` projection-column-list rejection at lines 187-192) and in `query/sql/parser.go::parseQualifiedColumnRef` respectively, so the slice naturally batches as one parser-side change plus one ordering swap on the protocol side.

The handoff intentionally lists the Slice E shapes by reject text rather than implementation sketch. Scout the live code first before deciding emit sites; the order in which Shunter currently rejects each shape changes the cheapest cut point.

Slice E confirmed test shapes (write failing first; pin OneOff raw + SubscribeSingle WithSql per shape):

- `SELECT missing FROM t` on the SubscribeSingle / SubscribeMulti surfaces -> `` `missing` is not in scope `` (currently SubscribeSingle rejects earlier with `Column projections are not supported in subscriptions; Subscriptions must return a table type`). OneOff is already pinned (Slice D). Need to swap the column-resolution pass to fire BEFORE the `allowProjection=false` guard — reference `type_proj::Exprs` (check.rs:67-80) walks each projection element through `type_expr` (which would emit `Unresolved::Var`) BEFORE `expect_table_type` runs the `Unsupported::ReturnType` check at check.rs:174.
- `SELECT * FROM t AS r WHERE t.u32 = 5` -> raw OneOff `` `t` is not in scope `` and SubscribeSingle `` `t` is not in scope, executing: ... `` (currently both surfaces reject at the parser with `parse: unsupported SQL: qualified column "t" does not match relation`). The base-table qualifier `t` is no longer in scope once the alias `r` is declared; reference `_type_expr` lib.rs:103 emits `Unresolved::var(&table)` when `vars.deref().get(&*table)` returns None.

Both shapes route through the existing `sql.UnresolvedVarError` typed error landed in Slice D. The SubscribeSingle column-projection guard reorder requires updating the existing `TestHandleSubscribeSingle_Parity{Aggregate,ColumnList}ReturnTypeRejectText` pins to use a column reference whose name DOES exist on the table (the new flow: missing column → `Unresolved::Var`; existing column with column-list projection → `Unsupported::ReturnType`).

## Adjacent Slice E Candidates (Optional Bundle)

Recorded in `TECH-DEBT.md::OI-002`. Group with Slice E only if the change locus overlaps; otherwise keep them as separate slices.

- Quoted-identifier case preservation (`SELECT * FROM "T"`, `SELECT * FROM t WHERE "U32" = 7`, `SELECT * FROM t AS "R" WHERE r.u32 = 7`, `SELECT t.* FROM t AS "R" JOIN s AS r ON "R".id = r.id`). Reference `SqlIdent` is case-sensitive; Shunter currently uses `strings.EqualFold` across schema lookup, column lookup, and alias matching. Pinned by `protocol/oi002_case_alias_duplicate_scout_tmp_test.go::TestOI002Scout_CaseDistinctQuotedAliasesDoNotCollide` (and presumably the other two scout files that disappeared during the last session — re-add if still relevant). This is a parser-side identifier-handling slice; large blast radius across alias/column/table resolution. Larger than Slice E.

## Closed Guardrails

Do not reopen `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033` without fresh failing regression evidence.

Also treat these recently closed surfaces as done unless a new failing test proves otherwise:

- Same-connection reused subscription-hash initial-snapshot elision
- `SubscriptionError.table_id` on request-origin subscribe/unsubscribe errors
- SubscribeSingle / SubscribeMulti compile-origin error text parity
- SubscribeSingle / SubscribeMulti initial-eval error text parity
- UnsubscribeSingle / UnsubscribeMulti final-eval error text parity
- `SELECT *` on `JOIN` rejection text across subscribe and one-off paths
- `Unresolved::Table` literal (`` no such table: `{t}`. If the table exists, it may be marked private. ``) across subscribe (WithSql-wrapped) and one-off (raw)
- `Unresolved::Var` literal (`` `{name}` is not in scope ``) across all eight compile-stage emit sites in `protocol/handle_subscribe.go` (Slice D, 2026-04-25). The previous `Unresolved::Field`-shape text (`` `{table}` does not have a field `{column}` ``) was a misclassification — reference does NOT reach `Unresolved::Field` from any subscription/one-off SELECT path.
- `Unsupported::ReturnType` literal (`Column projections are not supported in subscriptions; Subscriptions must return a table type`) unified across the aggregate and column-list subscribe-projection guards at `protocol/handle_subscribe.go::compileSQLQueryString`. **Slice E will reorder this guard so missing-column resolution fires first; the existing pins use a column name that DOES exist on the table, so the reorder will not change their text — but new test shapes for missing columns under SubscribeSingle column-list projection are needed.**
- `UnexpectedType` literal (`Unexpected type: (expected) Bool != {ty} (inferred)`) for bool literals against non-bool primitive columns. Emitted at the Shunter coerce boundary via `sql.UnexpectedTypeError` (Unwrap → `ErrUnsupportedSQL`) and passed through the `normalizeSQLFilterForRelations` wrapper bypass.
- `InvalidLiteral` literal (`` The literal expression `{literal}` cannot be parsed as type `{ty}` ``) for integer-range overflow / negative-on-unsigned (32/64/128/256-bit), LitFloat → integer, and non-Bool primitive → KindBool. Routed through `sql.InvalidLiteralError`.
- **Widening** parity for LitInt / LitFloat / LitBigInt / LitBytes (parser-source) → `KindString` and LitString numeric token / LitBytes-with-Text → integer/float kinds via `parseNumericLiteral`.
- **Source-text seam** on `sql.Literal.Text` populated at `parseLiteral` and `parseNumericLiteral`. `renderLiteralSourceText` prefers `Text` over canonical numeric formatting.
- **`KindBytes` reference routing** through `decodeReferenceHex` (mirrors `from_hex_pad`).
- **`:sender` reference resolution** at the top of `coerceValue` mirrors reference `resolve_sender`.
- **`KindTimestamp` / `KindArrayString` algebraic-name + error-class parity (Slice B, 2026-04-25).** `algebraicName` renders the SATS Product `(__timestamp_micros_since_unix_epoch__: I64)` for `KindTimestamp` and the parameterized `Array<String>` for `KindArrayString`.
- **Compile-stage `DuplicateName` / `UnexpectedType` / `InvalidOp` parity (Slice C, 2026-04-25).** Three new typed errors land in `query/sql/coerce.go`. Slot ordering for `UnexpectedType` matches reference `UnexpectedType::new(col_type, ty)` at `lib.rs:111-112` — RIGHT side's column type renders in `(expected)` slot, LEFT side's column type in `(inferred)` slot.
- **`Unresolved::Var` parity for missing-field lookups (Slice D, 2026-04-25).** New typed error `sql.UnresolvedVarError{Name string}` mirrors `expr/src/errors.rs:11-13` (renders `` `{name}` is not in scope ``). All eight compile-stage `` does not have a field `` emit sites in `protocol/handle_subscribe.go` re-routed onto `UnresolvedVarError{Name: column}`. SubscribeSingle column-list projection still hits `Unsupported::ReturnType` first for `SELECT missing FROM t` (this is the Slice E target). Pinned by `TestHandleOneOffQuery_ParityUnresolvedVar{UnqualifiedWhere,ProjectionColumn,JoinOnMissing,JoinWhereQualifiedMissing}RejectText` and the SubscribeSingle counterparts for the three scenarios that survive the column-projection guard.

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Confirmed Work Queue

These items are already live-code scouted and recorded in `TECH-DEBT.md::OI-002`. Do **not** spend the next turn rescouting them unless an implementation detail becomes ambiguous. Add failing tests first, then implement.

### Slice E: SubscribeSingle Projection-Column Reorder + Base-Table-After-Alias Parity

This is the next implementation slice. It touches compile-stage validation in `protocol/handle_subscribe.go::compileSQLQueryString` and parser-stage qualified-column resolution in `query/sql/parser.go`.

Confirmed test shapes:
- `SELECT missing FROM t` on SubscribeSingle / SubscribeMulti -> `` `missing` is not in scope, executing: `...` ``. Requires reordering the `allowProjection=false` column-list rejection to fire AFTER `compileProjectionColumns` resolves each column through `compileProjectionColumn` (which now emits `UnresolvedVarError`).
- `SELECT * FROM t AS r WHERE t.u32 = 5` on OneOff (raw) and SubscribeSingle (WithSql) -> `` `t` is not in scope ``. Requires routing `parser.go::parseQualifiedColumnRef`'s `qualified column %q does not match relation` rejection through `UnresolvedVarError{Name: qualifier}` instead of the parser `unsupported` text. Mirrors reference `_type_expr` lib.rs:103 which raises `Unresolved::var(&table)` when the qualifier is absent from `Relvars`.

Suggested targeted packages (confirm by scout):
- `query/sql/parser.go::parseQualifiedColumnRef`: convert the `qualified column %q does not match relation` rejection into a typed `UnresolvedVarError`. The existing `compileSQLQueryString` `errors.As(err, &sql.DuplicateNameError{})` bypass on the `parse:` wrap should be extended to also bypass `UnresolvedVarError`.
- `protocol/handle_subscribe.go::compileSQLQueryString` (lines 187-192): swap the order so `compileProjectionColumns` runs before the `allowProjection=false` column-list guard, OR pre-resolve each projection column's existence inside the guard branch. Update existing pins for `Aggregate,ColumnList` ReturnType tests to use a column name that exists.
- Existing pins to update: `TestHandleSubscribeSingle_Parity{Aggregate,ColumnList}ReturnTypeRejectText` should keep using existing column names so they continue testing the `Unsupported::ReturnType` text; new pins cover the missing-column case under SubscribeSingle column-list projection.

### Adjacent OI-002 Candidates (May or May Not Bundle)

Recorded in `TECH-DEBT.md::OI-002`. Group with Slice E only if the change locus overlaps; otherwise keep them as separate slices.

- Quoted-identifier case preservation across schema lookup, column lookup, and alias matching. Reference `SqlIdent` is byte-equal case-sensitive; Shunter uses `strings.EqualFold` throughout. Pinned by `protocol/oi002_case_alias_duplicate_scout_tmp_test.go::TestOI002Scout_CaseDistinctQuotedAliasesDoNotCollide`. Larger blast radius than Slice E; keep separate.
- SubscribeSingle `LIMIT` rejection text (`Unsupported: SELECT * FROM t LIMIT 5, executing: ...` from the reference subscription parser). The previous scout pin was at `protocol/oi002_scout_tmp_test.go::TestOI002Scout_SubscribeSingleLimitRejectText` but that file was deleted during the last session — re-add if still relevant.
- Cross-join WHERE column equality on the SubscribeSingle path (the OneOff side closed in Slice C; SubscribeSingle still hits the admission gate). Wider work — separate slice.

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
