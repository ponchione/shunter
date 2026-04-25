# Next Session Handoff

Use this file to start the next parity / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go doc ./query/sql.Coerce
rtk go list -json ./subscription ./protocol ./query/sql ./executor
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Slice A (source-text seam + reference parse routing) landed 2026-04-25 — see closure summary below. The next batch is **Slice B**: rendering compound algebraic types in `algebraicName` and routing the timestamp / array-of-string error classes through `InvalidLiteralError` and `UnexpectedTypeError`. Slice B reuses the source-text seam Slice A introduced; no parser change is needed.

Highest-leverage first slice (Slice B): extend `sql.algebraicName` to render compound kinds with reference shapes, then route the matching coerce paths to `InvalidLiteralError` / `UnexpectedTypeError`:

- `KindTimestamp` should render as `(__timestamp_micros_since_unix_epoch__: I64)` (reference `Timestamp` is a Product type, not a primitive).
- `KindArrayString` should render as `Array<String>`.
- `KindBytes` already renders as `Array<U8>` and is unchanged.

Then on the coerce side:

- Replace the timestamp `lit.Kind != LitString` mismatch and the `parseTimestampLiteral` failure path with `InvalidLiteralError{Literal: <source text>, Type: algebraicName(KindTimestamp)}`. Source text is already preserved via `Literal.Text` for parser-driven inputs; for synthetic LitFloat / LitInt without Text, fall back to `renderLiteralSourceText` which still produces a canonical decimal.
- Bool literal on KindTimestamp should emit `UnexpectedTypeError{Expected: "Bool", Inferred: algebraicName(KindTimestamp)}` (the `mismatch` LitBool branch already routes through `algebraicName`, so updating the helper is enough).
- KindArrayString currently routes everything to `mismatch` and emits a generic ErrUnsupportedSQL. For LitString / LitInt / LitFloat / LitBigInt / LitBytes (all source-text-bearing categories) it should emit `InvalidLiteralError{Literal: <source text>, Type: "Array<String>"}`. For LitBool it should emit `UnexpectedTypeError{Expected: "Bool", Inferred: "Array<String>"}`. Reference path: `parse(value, Array<String>)` falls to `bail!("Literal values for type {} are not supported")` at lib.rs:359, folded by lib.rs:99 `.map_err` into InvalidLiteral; the Bool case stays on the lib.rs:94 UnexpectedType arm.

Slice B confirmed test shapes (write failing first on coerce + at least one OneOff raw / SubscribeSingle WithSql pair per error class):

- `WHERE ts = 'not-a-timestamp'` -> `InvalidLiteral` type `(__timestamp_micros_since_unix_epoch__: I64)`.
- `WHERE ts = 42` and `WHERE ts = 1.3` -> `InvalidLiteral` with timestamp Product type. Source text preserved through `Literal.Text` (the parser sets it on tokNumber).
- `WHERE ts = TRUE` -> `UnexpectedType` with inferred `(__timestamp_micros_since_unix_epoch__: I64)`, not `Timestamp`.
- `WHERE arr = 'x'`, `WHERE arr = 1`, `WHERE arr = 0xFF` -> `InvalidLiteral` type `Array<String>`.
- `WHERE arr = TRUE` -> `UnexpectedType` inferred `Array<String>`, not `ArrayString`.

Slice C (compile-stage SQL validation text parity) follows Slice B; do not bundle them. Slice C touches parser/compile validation more than literal coercion.

Slice C confirmed test shapes:

- `SELECT u32 AS dup, i32 AS dup FROM t` -> raw OneOff ``Duplicate name `dup` ``; SubscribeSingle column projections still reject with `Unsupported::ReturnType` before duplicate-name details, so pin OneOff first unless a reference subscribe shape proves otherwise.
- `SELECT u32, u32 FROM t` -> raw OneOff ``Duplicate name `u32` ``.
- `FROM t AS dup JOIN s AS dup` -> ``Duplicate name `dup` `` instead of parser unsupported qualifier text.
- `FROM t JOIN t` -> ``Duplicate name `t` `` instead of `self join requires aliases`.
- `JOIN s ON t.u32 = s.name` -> `UnexpectedType` text, currently delayed to subscription predicate validation.
- `JOIN s ON t.arr = s.arr` -> fixed literal `Product values are not comparable`, not contextual `join ON ...`.

Write the failing parity test first on the OneOff surface (raw) and the SubscribeSingle surface (WithSql-wrapped) before touching `coerce.go`. The `normalizeSQLFilterForRelations` bypass already passes `InvalidLiteralError` / `UnexpectedTypeError` through unwrapped, so no protocol-seam changes are needed for text-parity-only slices.

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
- `UnexpectedType` literal (`Unexpected type: (expected) Bool != {ty} (inferred)`) for bool literals against non-bool primitive columns. Emitted at the Shunter coerce boundary via `sql.UnexpectedTypeError` (Unwrap → `ErrUnsupportedSQL`) and passed through the `normalizeSQLFilterForRelations` wrapper bypass so both OneOff (raw) and SubscribeSingle/Multi (WithSql-wrapped) carry the reference text. Shared infrastructure: `sql.algebraicName(kind)` renders reference-style primitive tokens (`Bool`, `U32`, `F32`, `Array<U8>`, etc.) for reuse by related parity slices.
- `InvalidLiteral` literal (`` The literal expression `{literal}` cannot be parsed as type `{ty}` ``) for integer-range overflow and negative-on-unsigned across the 32/64-bit (`coerceSigned`/`coerceUnsigned`) and 128/256-bit (`coerceValue` Uint128/256 negative-LitInt branches, plus `coerceBigIntTo{Int,Uint}{128,256}`) paths. Emitted via `sql.InvalidLiteralError` (Unwrap → `ErrUnsupportedSQL`) with the literal rendered through `renderLiteralSourceText` (preferring `Literal.Text` for parser-preserved tokens, falling back to `strconv.FormatInt` for LitInt and `big.Int.String()` for LitBigInt).
- `InvalidLiteral` literal for **LitFloat → integer column** (`WHERE u32 = 1.3` → `` The literal expression `1.3` cannot be parsed as type `U32` ``). Extends `sql.mismatch` with a `LitFloat && isIntegerKind(kind)` branch rendering via `renderLiteralSourceText` (`Literal.Text` when set, `strconv.FormatFloat('g', -1, 64)` otherwise). Covers the `coerceSigned` / `coerceUnsigned` / 128/256-bit default-arm entry points.
- `InvalidLiteral` literal for **non-Bool primitive literal → KindBool** (`WHERE b = 1`, `WHERE b = 1.3`, `WHERE b = 'foo'`, `WHERE b = 0x01` all emit `` The literal expression `{v}` cannot be parsed as type `Bool` ``). Reference has no Bool arm in `parse(value, ty)` and hits the catch-all `bail!`. Shunter routes through `renderLiteralSourceText(lit)` — the same helper is reusable by any future target kind that uniformly rejects its non-matching literal categories.
- **Widening** parity for LitInt / LitFloat / LitBigInt / LitBytes (parser-source) → `KindString` (`WHERE strcol = 42` widens to `strcol = "42"`; `WHERE strcol = 1.3` widens to `strcol = "1.3"`; `WHERE strcol = 1e40` widens to `strcol = "1e40"`; `WHERE strcol = 0xDEADBEEF` widens to `strcol = "0xDEADBEEF"`; `WHERE strcol = +1000` widens to `strcol = "+1000"`; `WHERE strcol = 001` widens to `strcol = "001"`; `WHERE strcol = 1.10` widens to `strcol = "1.10"`). Reference `parse(value, AlgebraicType::String)` at lib.rs:353 wraps source text as String for any of `Str | Num | Hex` SqlLiteral categories. Shunter's `case types.KindString:` in `coerceValue` reuses `renderLiteralSourceText` directly so the same renderings used for InvalidLiteral text parity carry the widened String value. LitBool still rejects via `mismatch` → `UnexpectedType{Bool, String}` (matches reference lib.rs:94 — only `Str | Num | Hex` reach the lib.rs:353 String arm).
- **Widening** parity for LitString numeric token / LitBytes-with-Text → integer/float kinds (`WHERE u32 = '42'` widens to U32(42); `WHERE u32 = 'foo'` rejects with `` The literal expression `foo` cannot be parsed as type `U32` ``; `WHERE u32 = 1e40` direct token emits InvalidLiteral). Reference parse_int / parse_float at lib.rs:255-352 route source text through `BigDecimal::from_str`. Shunter routes LitString-on-numeric / LitBytes-on-numeric at the top of `coerceValue` through `parseNumericLiteral(text)` and recurses with the parsed numeric Literal; parse failure folds to `InvalidLiteralError{Literal: text, Type: algebraicName(kind)}`.
- **Source-text seam** on `sql.Literal.Text` populated at `parseLiteral` (numeric token raw text, hex token raw text, string body) and at `parseNumericLiteral` (every parse path sets `Text = text` on the returned Literal so collapses survive). `renderLiteralSourceText` prefers `Text` over canonical numeric formatting; falls back to `FormatInt` / `FormatFloat` / `Big.String()` for test-constructed Literals with empty Text. Closes the long-deferred cluster: scientific-notation tokens (`1e3` / `1e40`), leading-sign / leading-zero tokens (`+1000` / `001`), round-trip-lossy float tokens (`1.10`), and hex tokens (`0xDEADBEEF` / `X'01'`) all survive coerce-time renderings (InvalidLiteral, KindString widening, KindBytes routing).
- **`KindBytes` reference routing** through `decodeReferenceHex` (mirrors reference `from_hex_pad` at lib/src/lib.rs:310). Strips optional `0x`/`0X` prefix or `X'..'` wrapper, then decodes via `encoding/hex.DecodeString`. LitString / LitInt / LitFloat / LitBigInt / LitBytes all route through the same helper; decode failure folds to `InvalidLiteralError{Literal: text, Type: "Array<U8>"}`. Pinned by `WHERE bytes = '0x0102'` (binds), `WHERE bytes = 42` (binds as `[]byte{0x42}`), `WHERE bytes = 'not-hex'` and `WHERE bytes = 1.3` (InvalidLiteral with `Array<U8>` type).
- **`:sender` reference resolution** at the top of `coerceValue` mirrors reference `resolve_sender` at sql-parser/src/ast/mod.rs:159. The AST step replaces `Param(Sender)` with `Lit(SqlLiteral::Hex(identity.to_hex()))` BEFORE type-checking; Shunter materializes the same shape by swapping LitSender for `Literal{Kind: LitBytes, Bytes: caller, Text: hex(caller)}` and recursing with `caller=nil`. Downstream renderings then carry the reference shapes: `WHERE bytes = :sender` binds the 32-byte caller (via the `lit.Kind == LitBytes` fast path), `WHERE name = :sender` widens onto `KindString` as the 64-char hex (via `renderLiteralSourceText`), `WHERE b = :sender` rejects with `InvalidLiteralError{hex, "Bool"}`, and numeric-column targets route the hex through `parseNumericLiteral` which produces either an out-of-range LitBigInt (digit-only hex) or a parse error (hex containing a-f) — both fold to `InvalidLiteralError`.

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Confirmed Work Queue

These items are already live-code scouted and recorded in `TECH-DEBT.md::OI-002`. Do **not** spend the next turn rescouting them unless an implementation detail becomes ambiguous. Add failing tests first, then implement.

### Slice B: Algebraic Type Rendering + Compound Error Classes

This is the next implementation slice. It touches `sql.algebraicName` and the coerce arms for `KindTimestamp` / `KindArrayString`. No parser change. The source-text seam from Slice A carries the literal rendering; the work is the type-name rendering and the error-class routing.

Implementation sketch:
- Extend `algebraicName` to return `(__timestamp_micros_since_unix_epoch__: I64)` for `KindTimestamp` and `Array<String>` for `KindArrayString`. Reference rendering at sats/src/algebraic_type/fmt.rs flows through `fmt_algebraic_type` for the Product Timestamp shape and the parameterized `Array<...>` shape.
- For `KindTimestamp`: replace the existing `if lit.Kind != LitString { return mismatch(lit, kind) }` and `parseTimestampLiteral` failure branches with `InvalidLiteralError` carrying `renderLiteralSourceText(lit)` and the new compound type name. Bool routes through `mismatch` → `UnexpectedTypeError{Expected: "Bool", Inferred: algebraicName(kind)}` automatically once the helper is updated.
- For `KindArrayString`: route LitString / LitInt / LitFloat / LitBigInt / LitBytes (with Text) to `InvalidLiteralError` carrying `renderLiteralSourceText(lit)`. Keep LitBool routing through `mismatch` so the bool-vs-non-bool path emits `UnexpectedTypeError`. Update `TestCoerceLiteralsRejectedOnArrayStringColumn` to check the more specific error classes; the existing test's `errors.Is(err, ErrUnsupportedSQL)` continues to hold via `Unwrap`.
- Keep `KindBytes` rendering as `Array<U8>`; that is already the primitive-array reference form.

Confirmed test shapes to pin in this slice:

- `WHERE ts = 'not-a-timestamp'` -> `InvalidLiteral` type `(__timestamp_micros_since_unix_epoch__: I64)`.
- `WHERE ts = 42` and `WHERE ts = 1.3` -> `InvalidLiteral` with timestamp Product type.
- `WHERE ts = TRUE` -> `UnexpectedType` with inferred `(__timestamp_micros_since_unix_epoch__: I64)`, not `Timestamp`.
- `WHERE arr = 'x'` -> `InvalidLiteral` type `Array<String>`.
- `WHERE arr = TRUE` -> `UnexpectedType` inferred `Array<String>`, not `ArrayString`.

Suggested targeted packages:
- `query/sql`: coerce unit tests for the new error classes and the algebraicName helper.
- `protocol`: OneOff raw and SubscribeSingle WithSql pins for one timestamp shape and one ArrayString shape (the wrapper-bypass already passes the new errors through unwrapped).

### Slice C: Compile-Stage DuplicateName and Join Error Text

Keep this separate from Slice B unless those changes are already green. It touches parser/compile validation more than literal coercion.

Confirmed test shapes:
- `SELECT u32 AS dup, i32 AS dup FROM t` -> raw OneOff ``Duplicate name `dup` ``; SubscribeSingle column projections still reject with `Unsupported::ReturnType` before duplicate-name details, so pin OneOff first unless a reference subscribe shape proves otherwise.
- `SELECT u32, u32 FROM t` -> raw OneOff ``Duplicate name `u32` ``.
- `FROM t AS dup JOIN s AS dup` -> ``Duplicate name `dup` `` instead of parser unsupported qualifier text.
- `FROM t JOIN t` -> ``Duplicate name `t` `` instead of `self join requires aliases`.
- `JOIN s ON t.u32 = s.name` -> `UnexpectedType` text, currently delayed to subscription predicate validation.
- `JOIN s ON t.arr = s.arr` -> fixed literal `Product values are not comparable`, not contextual `join ON ...`.

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
