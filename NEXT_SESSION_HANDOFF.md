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

Continue closing `InvalidLiteral` text parity on the remaining literal-category mismatches routed through `sql.mismatch` (`query/sql/coerce.go`). The integer-range paths are closed; the next bounded candidate is **LitFloat → non-float numeric column** (e.g. `SELECT * FROM t WHERE u32 = 1.3`), because it reconstructs cleanly via `strconv.FormatFloat(lit.Float, 'g', -1, 64)` and has no behavioral-divergence trap.

Reference anchor: `expr/src/errors.rs:84` + emit site `expr/src/lib.rs:99` via `parse_int` on a non-integer `BigDecimal`. Reuse the existing `sql.InvalidLiteralError` and `sql.algebraicName` helpers landed 2026-04-24; extend `sql.mismatch` to branch on LitFloat into non-float kinds. The `handle_subscribe.go::normalizeSQLFilterForRelations` bypass already passes `InvalidLiteralError` through unwrapped.

Do **not** widen to LitString/LitBytes in the same slice — LitString carries a behavioral-divergence trap on `KindString` (reference `parse(text, String)` succeeds where Shunter rejects), and LitBytes has no preserved source text so its `{literal}` rendering would require either a canonical-hex reconstruction choice or a `Text string` field on `sql.Literal`. Both are called out separately under Scout Directions.

Write the failing parity test first on the OneOff surface (raw) and the SubscribeSingle surface (WithSql-wrapped) before touching `coerce.go`.

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

If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Scout Directions

Pick one of these only if live-code evidence produces a concrete failing test:

- Remaining `InvalidLiteral` sites on other literal categories. The integer path is closed (LitInt, LitBigInt). Still diverging: LitString, LitFloat, LitBytes, LitBigInt into non-matching kinds all route through `sql.mismatch` and still emit the old `"{kind} literal cannot be coerced to {kind}"` text. Reference emits `InvalidLiteral` via `lib.rs:99` for these (parse-time failures). Text reconstruction per LitKind: LitString uses `lit.Str`; LitFloat uses `strconv.FormatFloat(lit.Float, 'g', -1, 64)`; LitBytes has no preserved source text (raw bytes only) and needs a canonical `0xHEX` or a `Text` field on `Literal`; LitBigInt uses `lit.Big.String`. Note the **behavioral divergence** on LitInt/LitFloat/LitBytes into `KindString`: reference `parse(text, String)` wraps the text as `AlgebraicValue::String` and succeeds, while Shunter rejects via mismatch — that is a widening, not a text slice, and should be scoped separately.
- Preserving source text on `sql.Literal`. Today scientific-notation literals (`1e3`, `1e40`) collapse to LitInt/LitBigInt at `parseNumericLiteral`, which is why the existing InvalidLiteral parity renders the decimal canonical form instead of the original source token. Adding a `Text string` field populated at `parseLiteral` call sites (tokNumber → `t.text`, tokHex → `t.text`, tokString → quoted `t.text`, TRUE/FALSE, `:sender`) would let `InvalidLiteralError.Literal` carry the exact reference rendering. Non-trivial parser touch — scope separately.
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
