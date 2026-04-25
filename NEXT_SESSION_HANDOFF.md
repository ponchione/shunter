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

Slices A, B, and C are landed (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error class routing, compile-stage `DuplicateName` + join `UnexpectedType` / `InvalidOp` parity — see closure summary below). The next batch is **Slice D: `Unresolved::Var` parity for missing-field lookups**. Slice D touches the same compile-stage seams as Slice C (parser admission and `protocol/handle_subscribe.go::compileSQLQueryString`) but routes through a different reference error class — `Unresolved::Var` rather than `Unresolved::Field` / `DuplicateName`.

The handoff intentionally lists the Slice D shapes by reject text rather than implementation sketch. Scout the live code first (parser → compile → predicate validation) before deciding emit sites; the order in which Shunter currently resolves table-then-field changes the cheapest cut point.

Slice D confirmed test shapes (write failing first on coerce + at least one OneOff raw / SubscribeSingle WithSql pair per shape):

- `SELECT * FROM t WHERE missing = 1` -> raw OneOff `` `missing` is not in scope `` (currently `` `t` does not have a field `missing` ``).
- `SELECT missing FROM t` -> raw OneOff `` `missing` is not in scope `` (currently OneOff emits `` `t` does not have a field `missing` ``; SubscribeSingle still rejects earlier with `Column projections are not supported in subscriptions; Subscriptions must return a table type` — pin OneOff first).
- `SELECT t.* FROM t JOIN s ON t.missing = s.id` -> raw OneOff `` `missing` is not in scope `` (currently `` `t` does not have a field `missing` ``).
- `SELECT t.* FROM t JOIN s ON t.id = s.t_id WHERE s.missing = 1` -> raw OneOff `` `missing` is not in scope `` (currently `` `s` does not have a field `missing` ``).

Reference emit shape: `Unresolved::Var(field)` at `expr/src/lib.rs:103` and `:107` — both are field-lookup-failure branches inside `_type_expr`. Format string: `` `{0}` is not in scope `` (errors.rs:12-13). The shared seam in Shunter is the `` `%s` does not have a field `%s` `` text repeated across `protocol/handle_subscribe.go` (compile-stage emits at :265, :270, :358, :363, :461, :573, :616 per the current file) and `protocol/handle_oneoff.go`. Each emit site currently carries the table name in slot 0 and the column name in slot 1; reference reports only the missing field name.

Decision point for the slice: introduce a typed `sql.UnresolvedVarError{Name string}` (Unwrap → `ErrUnsupportedSQL`) and re-route the seven emit sites through it, OR convert each `fmt.Errorf` into a literal `` `%s` is not in scope `` with no wrapping (since the existing pre-fix wrappers like `coerce column %q:` already bypass typed errors at `normalizeSQLFilterForRelations`). The typed-error route is closer to the Slice B/C precedent and makes future bypasses uniform; the inline route is smaller but loses the typed-error classification on cross-package consumers. Prefer the typed-error route unless a smaller surface lands cleanly first.

## Adjacent Slice D Candidates (Optional Bundle)

Recorded in `TECH-DEBT.md::OI-002`. Group with Slice D only if the change locus overlaps; otherwise keep them as separate slices.

- `SubscribeSingle SELECT missing FROM t` — needs the column-projection guard at `compileSQLQueryString`'s `allowProjection=false` branch to defer until AFTER the column-resolution pass. Reference `type_proj::Exprs` resolves each projection element through `type_expr` (which would emit Unresolved::Var) before the `expect_table_type` / `Unsupported::ReturnType` guard fires. Order swap is small but changes existing `TestHandleSubscribeSingle_Parity{Aggregate,ColumnList}ReturnTypeRejectText` pins which would need adjusting to use a column reference whose name DOES exist on the table.
- Base-table qualifier after alias: `SELECT * FROM t AS r WHERE t.u32 = 5` -> `` `t` is not in scope `` instead of the current parser admission rejection (`parse: unsupported SQL: qualified column "t" does not match relation`). This is a narrower change in `parser.go::parseQualifiedColumnRef` plus a typed-error bypass in `compileSQLQueryString`. Same seam as the Slice D body.

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
- `Unresolved::Field` literal (`` `{table}` does not have a field `{field}` ``) across subscribe (WithSql-wrapped) and one-off (raw) — argument order flipped to `(table, field)` to match reference `errors.rs:16`
- `Unsupported::ReturnType` literal (`Column projections are not supported in subscriptions; Subscriptions must return a table type`) unified across the aggregate and column-list subscribe-projection guards at `protocol/handle_subscribe.go::compileSQLQueryString`
- `UnexpectedType` literal (`Unexpected type: (expected) Bool != {ty} (inferred)`) for bool literals against non-bool primitive columns. Emitted at the Shunter coerce boundary via `sql.UnexpectedTypeError` (Unwrap → `ErrUnsupportedSQL`) and passed through the `normalizeSQLFilterForRelations` wrapper bypass so both OneOff (raw) and SubscribeSingle/Multi (WithSql-wrapped) carry the reference text. Shared infrastructure: `sql.algebraicName(kind)` renders reference-style algebraic tokens (primitives like `Bool`, `U32`, `F32`; arrays as `Array<U8>` / `Array<String>`; the Timestamp Product as `(__timestamp_micros_since_unix_epoch__: I64)`); now exported as `sql.AlgebraicName` for cross-package use.
- `InvalidLiteral` literal (`` The literal expression `{literal}` cannot be parsed as type `{ty}` ``) for integer-range overflow and negative-on-unsigned across the 32/64-bit (`coerceSigned`/`coerceUnsigned`) and 128/256-bit (`coerceValue` Uint128/256 negative-LitInt branches, plus `coerceBigIntTo{Int,Uint}{128,256}`) paths. Emitted via `sql.InvalidLiteralError` (Unwrap → `ErrUnsupportedSQL`) with the literal rendered through `renderLiteralSourceText` (preferring `Literal.Text` for parser-preserved tokens, falling back to `strconv.FormatInt` for LitInt and `big.Int.String()` for LitBigInt).
- `InvalidLiteral` literal for **LitFloat → integer column** (`WHERE u32 = 1.3` → `` The literal expression `1.3` cannot be parsed as type `U32` ``). Extends `sql.mismatch` with a `LitFloat && isIntegerKind(kind)` branch rendering via `renderLiteralSourceText` (`Literal.Text` when set, `strconv.FormatFloat('g', -1, 64)` otherwise). Covers the `coerceSigned` / `coerceUnsigned` / 128/256-bit default-arm entry points.
- `InvalidLiteral` literal for **non-Bool primitive literal → KindBool** (`WHERE b = 1`, `WHERE b = 1.3`, `WHERE b = 'foo'`, `WHERE b = 0x01` all emit `` The literal expression `{v}` cannot be parsed as type `Bool` ``). Reference has no Bool arm in `parse(value, ty)` and hits the catch-all `bail!`. Shunter routes through `renderLiteralSourceText(lit)` — the same helper is reusable by any future target kind that uniformly rejects its non-matching literal categories.
- **Widening** parity for LitInt / LitFloat / LitBigInt / LitBytes (parser-source) → `KindString` (every `Str | Num | Hex` SqlLiteral category at lib.rs:353 wraps the source text as String). Shunter's `case types.KindString:` reuses `renderLiteralSourceText`. LitBool still rejects via `mismatch` → `UnexpectedType{Bool, String}` (matches reference lib.rs:94 — only Str/Num/Hex reach the lib.rs:353 String arm).
- **Widening** parity for LitString numeric token / LitBytes-with-Text → integer/float kinds via `parseNumericLiteral`. Parse failure folds to `InvalidLiteralError`.
- **Source-text seam** on `sql.Literal.Text` populated at `parseLiteral` and `parseNumericLiteral`. `renderLiteralSourceText` prefers `Text` over canonical numeric formatting.
- **`KindBytes` reference routing** through `decodeReferenceHex` (mirrors `from_hex_pad`).
- **`:sender` reference resolution** at the top of `coerceValue` mirrors reference `resolve_sender`.
- **`KindTimestamp` / `KindArrayString` algebraic-name + error-class parity (Slice B, 2026-04-25).** `algebraicName` renders the SATS Product `(__timestamp_micros_since_unix_epoch__: I64)` for `KindTimestamp` and the parameterized `Array<String>` for `KindArrayString`. The `KindTimestamp` coerce arm preserves the LitString happy path (RFC3339 → micros) but routes every other source-text-bearing literal kind to `InvalidLiteralError`; LitBool routes through `mismatch` → `UnexpectedTypeError`. The `KindArrayString` arm rejects every literal — LitBool through `mismatch` → `UnexpectedTypeError`, every other source-text-bearing kind through `InvalidLiteralError`.
- **Compile-stage `DuplicateName` / `UnexpectedType` / `InvalidOp` parity (Slice C, 2026-04-25).** Three new typed errors land in `query/sql/coerce.go`: `sql.DuplicateNameError` (renders `` Duplicate name `{name}` ``), `sql.InvalidOpError` (renders `` Invalid binary operator `{op}` for type `{type}` ``), and the cross-package helper `sql.AlgebraicName(kind)`. Emit sites: `parseJoinClause` returns `DuplicateNameError{Name: rightAlias}` for both explicitly-aliased dup-alias joins and unaliased self-joins; `compileSQLQueryString` adds an `errors.As(err, &sql.DuplicateNameError{})` bypass on the `parse:` wrap; `compileProjectionColumns` / `compileJoinProjectionColumns` interleave a `seen[effectiveName]` HashSet check (effective name = `OutputAlias` if non-empty else `Column`) so OneOff projection-list duplicates emit reference `DuplicateName` text (SubscribeSingle still hits `Unsupported::ReturnType` first, so projection dup-name parity is OneOff-only); the JOIN ON kind-mismatch and Array/Product equality arms emit `UnexpectedTypeError` / `InvalidOpError` from the join branch BEFORE `subscription/validate.go::validateJoin` would otherwise emit `subscription: invalid predicate: join column kinds differ`. Slot ordering matches reference `UnexpectedType::new(col_type, ty)` at `lib.rs:111-112` — RIGHT side's column type renders in `(expected)` slot, LEFT side's column type in `(inferred)` slot. Same routing applied to `compileCrossJoinWhereColumnEquality`; SubscribeSingle cross-join WHERE still rejects earlier on the admission gate (separate slice). Pinned by `TestHandleOneOffQuery_Parity{DuplicateProjectionAlias,DuplicateImplicitProjection,DuplicateJoinAlias,DuplicateSelfJoin,JoinColumnKindMismatch,JoinArrayColumnInvalidOp}RejectText` and the SubscribeSingle counterparts for the four scenarios that survive the column-projection guard.

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Confirmed Work Queue

These items are already live-code scouted and recorded in `TECH-DEBT.md::OI-002`. Do **not** spend the next turn rescouting them unless an implementation detail becomes ambiguous. Add failing tests first, then implement.

### Slice D: `Unresolved::Var` Parity for Missing-Field Lookups

This is the next implementation slice. It touches compile-stage validation (parser admission + `compileSQLQueryString` field-lookup emit sites) — same change locus as Slice C.

Confirmed test shapes:
- `SELECT * FROM t WHERE missing = 1` -> raw OneOff `` `missing` is not in scope `` and SubscribeSingle WithSql counterpart.
- `SELECT missing FROM t` -> raw OneOff `` `missing` is not in scope `` (SubscribeSingle still hits `Unsupported::ReturnType` first; pin OneOff only unless the projection-guard order swap is bundled).
- `SELECT t.* FROM t JOIN s ON t.missing = s.id` -> raw OneOff and SubscribeSingle WithSql pair.
- `SELECT t.* FROM t JOIN s ON t.id = s.t_id WHERE s.missing = 1` -> raw OneOff and SubscribeSingle WithSql pair.

Suggested targeted packages (confirm by scout):
- `query/sql`: introduce `sql.UnresolvedVarError{Name string}` typed error (Unwrap → `ErrUnsupportedSQL`) alongside the existing `DuplicateNameError` / `InvalidLiteralError` / `UnexpectedTypeError` / `InvalidOpError` cluster.
- `protocol/handle_subscribe.go`: re-route the seven `` `%s` does not have a field `%s` `` emit sites onto `UnresolvedVarError{Name: f.Column}`. Each emit currently carries the table name; the reference shape carries only the field name.
- `protocol/handle_oneoff.go`: same.
- `subscription/validate.go::ErrColumnNotFound`: separate code path; defense-in-depth, do not reopen unless the SQL surface routes hit it.

### Adjacent OI-002 Candidates (May or May Not Bundle)

Recorded in `TECH-DEBT.md::OI-002`. Group with Slice D only if the change locus overlaps; otherwise keep them as separate slices.

- SubscribeSingle column-projection guard reorder (resolve column existence BEFORE rejecting on `allowProjection=false`).
- Base-table qualifier after alias: `SELECT * FROM t AS r WHERE t.u32 = 5` -> `` `t` is not in scope ``.
- Quoted-table-identifier case preservation (`SELECT * FROM "T"` should emit `` no such table: `T`... ``); pinned by `protocol/oi002_scout_tmp_test.go::TestOI002Scout_QuotedTableIdentifierCaseStaysExact`.
- SubscribeSingle `LIMIT` rejection text (`Unsupported: ...` from the reference subscription parser); pinned by `protocol/oi002_scout_tmp_test.go::TestOI002Scout_SubscribeSingleLimitRejectText`.
- Cross-join WHERE column equality SubscribeSingle path (the OneOff side closed in Slice C; the SubscribeSingle side still hits the admission gate); pinned by `protocol/oi002_scout_tmp_test.go::TestOI002Scout_CrossJoinWhereColumnMismatchText` (OneOff sub-assertion now passes; SubscribeSingle sub-assertion still fails).

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
