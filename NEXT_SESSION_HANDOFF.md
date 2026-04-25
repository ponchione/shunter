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

Slices A and B are landed (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error classes — see closure summary below). The next batch is **Slice C: compile-stage validation text parity**. Slice C touches parser/compile validation more than literal coercion; do not bundle it with the literal-routing slices that just landed.

The handoff intentionally lists the Slice C shapes by reject text rather than implementation sketch. Scout the live code first (parser → admission → predicate validation) before deciding emit sites; the order in which Shunter currently rejects each shape changes the cheapest cut point.

Slice C confirmed test shapes (write failing first on coerce + at least one OneOff raw / SubscribeSingle WithSql pair per error class):

- `SELECT u32 AS dup, i32 AS dup FROM t` -> raw OneOff ``Duplicate name `dup` ``; SubscribeSingle column projections still reject with `Unsupported::ReturnType` before duplicate-name details, so pin OneOff first unless a reference subscribe shape proves otherwise.
- `SELECT u32, u32 FROM t` -> raw OneOff ``Duplicate name `u32` ``.
- `FROM t AS dup JOIN s AS dup` -> ``Duplicate name `dup` `` instead of parser unsupported qualifier text.
- `FROM t JOIN t` -> ``Duplicate name `t` `` instead of `self join requires aliases`.
- `JOIN s ON t.u32 = s.name` -> `UnexpectedType` text, currently delayed to subscription predicate validation.
- `JOIN s ON t.arr = s.arr` -> fixed literal `Product values are not comparable`, not contextual `join ON ...`.

Two adjacent OI-002 candidates remain open in `TECH-DEBT.md` and may be worth grouping with Slice C once you have scouted the parser admission cut points (only group if the change locus overlaps; otherwise keep them separate slices):

- `Unresolved::Var` text for unknown unqualified single-table columns (`SELECT * FROM t WHERE missing = 1`), unknown projection columns (`SELECT missing FROM t`), and unknown qualified join WHERE columns (`s.missing` after a join).
- Base-table qualifiers used after an alias (`SELECT * FROM t AS r WHERE t.u32 = 5`) -> `` `t` is not in scope `` instead of the current parser admission rejection.

Write the failing parity test first on the OneOff surface (raw) and the SubscribeSingle surface (WithSql-wrapped) before touching the parser/admission/predicate validation seam. The `normalizeSQLFilterForRelations` bypass already passes `InvalidLiteralError` / `UnexpectedTypeError` through unwrapped; for compile-stage text parity, expect to introduce a similar narrow wrapper bypass (or a typed error) so the reference text reaches both surfaces verbatim.

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
- `UnexpectedType` literal (`Unexpected type: (expected) Bool != {ty} (inferred)`) for bool literals against non-bool primitive columns. Emitted at the Shunter coerce boundary via `sql.UnexpectedTypeError` (Unwrap → `ErrUnsupportedSQL`) and passed through the `normalizeSQLFilterForRelations` wrapper bypass so both OneOff (raw) and SubscribeSingle/Multi (WithSql-wrapped) carry the reference text. Shared infrastructure: `sql.algebraicName(kind)` renders reference-style algebraic tokens (primitives like `Bool`, `U32`, `F32`; arrays as `Array<U8>` / `Array<String>`; the Timestamp Product as `(__timestamp_micros_since_unix_epoch__: I64)`).
- `InvalidLiteral` literal (`` The literal expression `{literal}` cannot be parsed as type `{ty}` ``) for integer-range overflow and negative-on-unsigned across the 32/64-bit (`coerceSigned`/`coerceUnsigned`) and 128/256-bit (`coerceValue` Uint128/256 negative-LitInt branches, plus `coerceBigIntTo{Int,Uint}{128,256}`) paths. Emitted via `sql.InvalidLiteralError` (Unwrap → `ErrUnsupportedSQL`) with the literal rendered through `renderLiteralSourceText` (preferring `Literal.Text` for parser-preserved tokens, falling back to `strconv.FormatInt` for LitInt and `big.Int.String()` for LitBigInt).
- `InvalidLiteral` literal for **LitFloat → integer column** (`WHERE u32 = 1.3` → `` The literal expression `1.3` cannot be parsed as type `U32` ``). Extends `sql.mismatch` with a `LitFloat && isIntegerKind(kind)` branch rendering via `renderLiteralSourceText` (`Literal.Text` when set, `strconv.FormatFloat('g', -1, 64)` otherwise). Covers the `coerceSigned` / `coerceUnsigned` / 128/256-bit default-arm entry points.
- `InvalidLiteral` literal for **non-Bool primitive literal → KindBool** (`WHERE b = 1`, `WHERE b = 1.3`, `WHERE b = 'foo'`, `WHERE b = 0x01` all emit `` The literal expression `{v}` cannot be parsed as type `Bool` ``). Reference has no Bool arm in `parse(value, ty)` and hits the catch-all `bail!`. Shunter routes through `renderLiteralSourceText(lit)` — the same helper is reusable by any future target kind that uniformly rejects its non-matching literal categories.
- **Widening** parity for LitInt / LitFloat / LitBigInt / LitBytes (parser-source) → `KindString` (`WHERE strcol = 42` widens to `strcol = "42"`; `WHERE strcol = 1.3` widens to `strcol = "1.3"`; `WHERE strcol = 1e40` widens to `strcol = "1e40"`; `WHERE strcol = 0xDEADBEEF` widens to `strcol = "0xDEADBEEF"`; `WHERE strcol = +1000` widens to `strcol = "+1000"`; `WHERE strcol = 001` widens to `strcol = "001"`; `WHERE strcol = 1.10` widens to `strcol = "1.10"`). Reference `parse(value, AlgebraicType::String)` at lib.rs:353 wraps source text as String for any of `Str | Num | Hex` SqlLiteral categories. Shunter's `case types.KindString:` in `coerceValue` reuses `renderLiteralSourceText` directly so the same renderings used for InvalidLiteral text parity carry the widened String value. LitBool still rejects via `mismatch` → `UnexpectedType{Bool, String}` (matches reference lib.rs:94 — only `Str | Num | Hex` reach the lib.rs:353 String arm).
- **Widening** parity for LitString numeric token / LitBytes-with-Text → integer/float kinds (`WHERE u32 = '42'` widens to U32(42); `WHERE u32 = 'foo'` rejects with `` The literal expression `foo` cannot be parsed as type `U32` ``; `WHERE u32 = 1e40` direct token emits InvalidLiteral). Reference parse_int / parse_float at lib.rs:255-352 route source text through `BigDecimal::from_str`. Shunter routes LitString-on-numeric / LitBytes-on-numeric at the top of `coerceValue` through `parseNumericLiteral(text)` and recurses with the parsed numeric Literal; parse failure folds to `InvalidLiteralError{Literal: text, Type: algebraicName(kind)}`.
- **Source-text seam** on `sql.Literal.Text` populated at `parseLiteral` (numeric token raw text, hex token raw text, string body) and at `parseNumericLiteral` (every parse path sets `Text = text` on the returned Literal so collapses survive). `renderLiteralSourceText` prefers `Text` over canonical numeric formatting; falls back to `FormatInt` / `FormatFloat` / `Big.String()` for test-constructed Literals with empty Text. Closes the long-deferred cluster: scientific-notation tokens (`1e3` / `1e40`), leading-sign / leading-zero tokens (`+1000` / `001`), round-trip-lossy float tokens (`1.10`), and hex tokens (`0xDEADBEEF` / `X'01'`) all survive coerce-time renderings (InvalidLiteral, KindString widening, KindBytes routing).
- **`KindBytes` reference routing** through `decodeReferenceHex` (mirrors reference `from_hex_pad` at lib/src/lib.rs:310). Strips optional `0x`/`0X` prefix or `X'..'` wrapper, then decodes via `encoding/hex.DecodeString`. LitString / LitInt / LitFloat / LitBigInt / LitBytes all route through the same helper; decode failure folds to `InvalidLiteralError{Literal: text, Type: "Array<U8>"}`. Pinned by `WHERE bytes = '0x0102'` (binds), `WHERE bytes = 42` (binds as `[]byte{0x42}`), `WHERE bytes = 'not-hex'` and `WHERE bytes = 1.3` (InvalidLiteral with `Array<U8>` type).
- **`:sender` reference resolution** at the top of `coerceValue` mirrors reference `resolve_sender` at sql-parser/src/ast/mod.rs:159. The AST step replaces `Param(Sender)` with `Lit(SqlLiteral::Hex(identity.to_hex()))` BEFORE type-checking; Shunter materializes the same shape by swapping LitSender for `Literal{Kind: LitBytes, Bytes: caller, Text: hex(caller)}` and recursing with `caller=nil`. Downstream renderings then carry the reference shapes: `WHERE bytes = :sender` binds the 32-byte caller (via the `lit.Kind == LitBytes` fast path), `WHERE name = :sender` widens onto `KindString` as the 64-char hex (via `renderLiteralSourceText`), `WHERE b = :sender` rejects with `InvalidLiteralError{hex, "Bool"}`, and numeric-column targets route the hex through `parseNumericLiteral` which produces either an out-of-range LitBigInt (digit-only hex) or a parse error (hex containing a-f) — both fold to `InvalidLiteralError`.
- **`KindTimestamp` / `KindArrayString` algebraic-name + error-class parity (Slice B, 2026-04-25).** `algebraicName` renders the SATS Product `(__timestamp_micros_since_unix_epoch__: I64)` for `KindTimestamp` and the parameterized `Array<String>` for `KindArrayString`. The `KindTimestamp` coerce arm preserves the LitString happy path (RFC3339 → micros) but routes every other source-text-bearing literal kind to `InvalidLiteralError{Literal: renderLiteralSourceText(lit), Type: timestamp Product}`; LitBool routes through `mismatch` → `UnexpectedTypeError{Bool, timestamp Product}`. The `KindArrayString` arm rejects every literal — LitBool through `mismatch` → `UnexpectedTypeError{Bool, Array<String>}`, every other source-text-bearing kind through `InvalidLiteralError{Type: Array<String>}`. Pinned by `TestCoerce{Malformed,Int,Float,Bool}LiteralOnTimestamp*Rejected` and `TestCoerceLiteralsRejectedOnArrayStringColumn`, plus OneOff raw and SubscribeSingle WithSql pairs (`TestHandleOneOffQuery_Parity{TimestampMalformed,BoolLiteralOnTimestamp,StringLiteralOnArrayString,BoolLiteralOnArrayString}RejectText` and the SubscribeSingle counterparts). `normalizeSQLFilterForRelations` already passes both error classes through unwrapped, so no protocol-seam change was needed.

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Confirmed Work Queue

These items are already live-code scouted and recorded in `TECH-DEBT.md::OI-002`. Do **not** spend the next turn rescouting them unless an implementation detail becomes ambiguous. Add failing tests first, then implement.

### Slice C: Compile-Stage DuplicateName and Join Error Text

This is the next implementation slice. It touches parser/compile validation more than literal coercion — different change locus than Slices A/B.

Confirmed test shapes:
- `SELECT u32 AS dup, i32 AS dup FROM t` -> raw OneOff ``Duplicate name `dup` ``; SubscribeSingle column projections still reject with `Unsupported::ReturnType` before duplicate-name details, so pin OneOff first unless a reference subscribe shape proves otherwise.
- `SELECT u32, u32 FROM t` -> raw OneOff ``Duplicate name `u32` ``.
- `FROM t AS dup JOIN s AS dup` -> ``Duplicate name `dup` `` instead of parser unsupported qualifier text.
- `FROM t JOIN t` -> ``Duplicate name `t` `` instead of `self join requires aliases`.
- `JOIN s ON t.u32 = s.name` -> `UnexpectedType` text, currently delayed to subscription predicate validation.
- `JOIN s ON t.arr = s.arr` -> fixed literal `Product values are not comparable`, not contextual `join ON ...`.

Suggested targeted packages (confirm by scout):
- `query/sql`: parser-stage validation that runs after parse but before admission, or a typed error class similar to `InvalidLiteralError`.
- `protocol`: OneOff raw and SubscribeSingle WithSql pins per error class. Some shapes (column-list projections) reject earlier on the SubscribeSingle surface and may need a OneOff-only pin until SubscribeSingle reorders.

### Adjacent OI-002 Candidates (May or May Not Bundle)

Recorded in `TECH-DEBT.md::OI-002`. Group with Slice C only if the change locus overlaps; otherwise keep them as separate slices.

- `Unresolved::Var` text for unknown columns: unqualified single-table (`WHERE missing = 1`), projection (`SELECT missing FROM t`), qualified join WHERE (`WHERE s.missing = 1` after a join).
- Base-table qualifier after alias: `SELECT * FROM t AS r WHERE t.u32 = 5` -> `` `t` is not in scope ``.

### Remaining Scout Budget

Only scout further after at least one confirmed slice above is implemented and green. If you do scout, keep it to one parser-admitted SQL shape and immediately either add it to `TECH-DEBT.md` or discard it as a pass.

## Prior Dead Ends

Do not rescout these without fresh evidence:

- Subscription-manager bookkeeping admission text parity. Reference emit sites mostly format Rust tuples with Debug output, so this is not a clean fixed-literal parity slice.
- `TotalHostExecutionDurationMicros` measurement accuracy. Both implementations measure elapsed time from request receipt.
- Cardinality row-limit text parity. Reference uses planner estimates while Shunter uses actual runtime row counts; parity would require a cardinality estimator.
- One-off mutation rejection text. Shunter has no current view-write failure mode that triggers the reference literal.
- Event-table websocket-v2 rejection text. Shunter has no event-table equivalent yet.
- `InvalidOp` text parity. Reference `op_supports_type` accepts every primitive, so the emit sites at `lib.rs:130,138` are practically unreachable on realistic inputs.
- `UnexpectedType` from `lib.rs:112` (column-vs-column binop mismatch) and `lib.rs:142` (non-bool expected at bin/log root) are unreachable through Shunter's `cmp = colref op literal` WHERE grammar.

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
