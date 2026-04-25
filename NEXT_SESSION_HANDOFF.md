# Next Session Handoff

Use this file to start the next parity / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go doc ./subscription.Manager
rtk go list -json ./subscription ./protocol ./query/sql ./executor
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

`InvalidLiteral` text parity has been extended to every bounded no-widening-trap target through `sql.mismatch`, AND the first KindString-target widening has landed:

- **LitFloat → integer kinds** (I8..I256, U8..U256) — closed 2026-04-24 (77d0873). Reference `parse_int` on a fractional BigDecimal fails and folds to InvalidLiteral; Shunter renders via `strconv.FormatFloat('g', -1, 64)` and reuses `algebraicName(kind)`.
- **Non-Bool primitive literal → KindBool** (LitInt/LitFloat/LitString/LitBigInt) — closed 2026-04-24 (f00e17e). Reference `parse(v, Bool)` has no Bool arm and hits the catch-all `bail!`; Shunter routes through a new `renderLiteralSourceText` helper that dispatches per LitKind. LitBytes deferred (no preserved source text).
- **LitInt / LitFloat / LitBigInt → KindString widening** — closed 2026-04-24. Reference `parse(value, AlgebraicType::String)` at lib.rs:353 wraps source text as String for any of `Str | Num | Hex` SqlLiteral categories. Shunter's `case types.KindString:` in `coerceValue` now routes through `renderLiteralSourceText` and accepts LitString/LitInt/LitFloat/LitBigInt. LitBool stays rejecting via `mismatch` → `UnexpectedType{Bool, String}` matching reference lib.rs:94. **LitBytes deferred** under the same source-text-preservation seam blocking the prior InvalidLiteral slice.

Every further candidate carries either an additional **widening trap** (LitString numeric → integer/float column), needs the same **source-text seam** (LitBytes wherever it appears, scientific-notation source preservation), or needs **compound-type Product rendering** in `algebraicName` (LitFloat/LitString → KindTimestamp/KindBytes/Identity/etc.). Pick the next slice from **Scout Directions** below. Do not widen beyond a single reference anchor per slice.

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
- `InvalidLiteral` literal (`` The literal expression `{literal}` cannot be parsed as type `{ty}` ``) for integer-range overflow and negative-on-unsigned across the 32/64-bit (`coerceSigned`/`coerceUnsigned`) and 128/256-bit (`coerceValue` Uint128/256 negative-LitInt branches, plus `coerceBigIntTo{Int,Uint}{128,256}`) paths. Emitted via `sql.InvalidLiteralError` (Unwrap → `ErrUnsupportedSQL`) with the literal rendered through `strconv.FormatInt` for LitInt and `big.Int.String` for LitBigInt. Plain-integer literals carry parity verbatim; scientific-notation literals collapse to LitInt/LitBigInt at the parser and render as the canonical decimal form — preserving source text is a separate slice.
- `InvalidLiteral` literal for **LitFloat → integer column** (`WHERE u32 = 1.3` → `` The literal expression `1.3` cannot be parsed as type `U32` ``). Extends `sql.mismatch` with a `LitFloat && isIntegerKind(kind)` branch rendering via `strconv.FormatFloat('g', -1, 64)`. Covers the `coerceSigned` / `coerceUnsigned` / 128/256-bit default-arm entry points. Round-trip-lossy forms (`1.10` → "1.1") stay deferred under source-text preservation.
- `InvalidLiteral` literal for **non-Bool primitive literal → KindBool** (`WHERE b = 1`, `WHERE b = 1.3`, `WHERE b = 'foo'` all emit `` The literal expression `{v}` cannot be parsed as type `Bool` ``). Reference has no Bool arm in `parse(value, ty)` and hits the catch-all `bail!`. Shunter routes through `renderLiteralSourceText(lit)` (FormatInt / FormatFloat / lit.Str / Big.String) — the same helper is reusable by any future target kind that uniformly rejects its non-matching literal categories. LitBytes deferred (no preserved source text).
- **Widening** parity for LitInt / LitFloat / LitBigInt → `KindString` (`WHERE strcol = 42` widens to `strcol = "42"`; `WHERE strcol = 1.3` widens to `strcol = "1.3"`; `WHERE strcol = 1e40` widens to `strcol = "10000000000000000000000000000000000000000"`). Reference `parse(value, AlgebraicType::String)` at lib.rs:353 wraps source text as String for any of `Str | Num | Hex` SqlLiteral categories. Shunter's `case types.KindString:` in `coerceValue` now reuses `renderLiteralSourceText` directly so the same renderings used for InvalidLiteral text parity carry the widened String value. LitBool stays rejecting via `mismatch` → `UnexpectedType{Bool, String}` (matches reference lib.rs:94 — only `Str | Num | Hex` reach the lib.rs:353 String arm). LitBytes still rejects under the deferred source-text seam. Scientific-notation and round-trip-lossy float source forms collapse at the parser and so the widened String carries the canonical Shunter form rather than the original token — identical documented divergence to the prior InvalidLiteral 128/256-bit slice. Pins: `TestCoerceLitIntOnStringColumnWidens`, `TestCoerceLitFloatOnStringColumnWidens`, `TestCoerceLitBigIntOnStringColumnWidens`, `TestCoerceLitBytesOnStringColumnDeferred`, `TestCoerceLitBoolOnStringColumnEmitsUnexpectedType`, `TestHandleOneOffQuery_ParityNumericLiteralOnStringColumnWidens`, `TestHandleSubscribeSingle_ParityNumericLiteralOnStringColumnWidens`.

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Scout Directions

Pick one of these only if live-code evidence produces a concrete failing test:

- `InvalidLiteral` for **LitFloat / LitString → compound-typed column** (`KindTimestamp`, `KindBytes`, and any future `KindIdentity`/`KindConnectionID`/`KindUUID`). Reference `parse(value, ty)` branches on `ty.is_timestamp()` / `ty.is_bytes()` / `ty.is_identity()` / etc. (lib.rs, same `parse` match); each specific parser fails on non-matching literals and the outer `.map_err` at lib.rs:99 folds to `InvalidLiteral::new(v.into_string(), ty)`. The blocker is `{ty}`: reference `fmt_algebraic_type` renders Timestamp/Identity/etc. as **Product types** (`(__timestamp_micros_since_unix_epoch__: I64)` style — reference/SpacetimeDB/crates/sats/src/algebraic_type/fmt.rs:15-38). Shunter's `algebraicName` returns the primitive short token for these via the default arm (`KindTimestamp.String()` → `"timestamp"`). Closing this slice requires either (a) extending `algebraicName` to produce the reference Product rendering for the known compound kinds, or (b) accepting a documented text divergence on compound-typed InvalidLiteral output. Scout which compound kinds Shunter actually exposes through the literal-coerce boundary before opening. LitString on `KindBytes` has a **widening trap** (reference `from_hex_pad` accepts valid hex) — scope to non-hex inputs only.
- **Widening** slice: LitString containing a numeric token → integer/float column (`WHERE u32 = '123'`). Reference `parse_int`/`parse_float` invokes `BigDecimal::from_str(value)` which accepts digit-only strings; Shunter rejects any non-LitInt/LitBigInt/LitFloat on numeric columns via the existing `coerceSigned`/`coerceUnsigned`/LitFloat arms. Closing this slice is a behavior change. Reject-path stays — non-numeric LitString → integer continues to emit `InvalidLiteral` via the existing `mismatch` branch + `renderLiteralSourceText` (same infra the closed widening uses). Two pre-existing tests pin the current rejection (`TestHandleOneOffQuery_StringLiteralOnIntegerColumnRejected`, `TestHandleSubscribeSingle_StringLiteralOnIntegerColumnRejected`); they must flip to widening assertions for digit-only strings while staying-rejecting for non-numeric strings.
- Preserving source text on `sql.Literal`. Today scientific-notation literals (`1e3`, `1e40`) collapse to LitInt/LitBigInt at `parseNumericLiteral`, round-trip-lossy float forms (`1.10` → "1.1") canonicalize at `strconv.FormatFloat`, and hex source tokens (`0xdeadbeef`, `X'DEADBEEF'`) decode at `parseHexLiteral` losing the original casing/syntax. Adding a `Text string` field populated at `parseLiteral` call sites (tokNumber → `t.text`, tokHex → `t.text`, tokString → quoted `t.text`, TRUE/FALSE, `:sender`) would let `InvalidLiteralError.Literal` carry the exact reference rendering AND let the closed KindString widening accept LitBytes by passing the preserved hex source token through. This single seam unblocks both surfaces. Non-trivial parser touch — scope separately. The follow-on for the closed widening is `LitBytes → KindString` (reference accepts; Shunter currently rejects via the deferred fall-through in `case types.KindString:`); pinned today by `TestCoerceLitBytesOnStringColumnDeferred`.
- Remaining `expr/src/errors.rs` templated literals. Fixed-literal candidates from `Unresolved`/`InvalidWildcard`/`Unsupported`/`UnexpectedType`(Bool) are closed; `InvalidLiteral` integer paths are closed. Still open: `InvalidOp` (`:72` `` Invalid binary operator `{op}` for type `{ty}` ``) — practically unreachable because `op_supports_type` accepts every primitive; not worth scouting without a reference input shape that actually fires it. `UnexpectedType` column-vs-column (lib.rs:112) and bin/log-at-non-bool (lib.rs:142) — both unreachable through Shunter's `cmp = colref op literal` WHERE grammar. `DuplicateName` (`:120`) still rejected at parser with `ErrUnsupportedSQL` prefix — needs parser→compile-stage move; verify a concrete input that currently diverges before opening.
- Parser-admitted SQL operator combinations that compile cleanly but lack one-off or subscribe protocol pins. Scout a specific input shape, not the whole SQL surface.
- Post-commit fanout, QueryID stamping, or durability-gate edge cases in one combined scenario. Existing tests already cover disjoint-table multi-sub fanout and same-connection reused-hash stamping.

Keep the slice bounded: one reference anchor, one input shape, one failing test family.

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
