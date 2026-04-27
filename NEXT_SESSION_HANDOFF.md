# Next Session Handoff

Use this file to start the next Shunter correctness / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go list -json ./query/sql ./protocol ./schema ./subscription
rtk go doc ./query/sql
rtk go doc ./query/sql.Statement
rtk go doc ./schema.TableSchema.Column
rtk go doc ./schema.SchemaLookup
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Project framing was clarified on 2026-04-26:

- SpacetimeDB is an architectural reference, not a wire/client/business compatibility target.
- Shunter is for self-hosted / personally operated apps with Shunter-owned Go APIs and clients.
- Energy accounting is not a Shunter product goal; the protocol no longer carries energy fields or an `OutOfEnergy` outcome arm.
- OI-001 Shunter-native protocol cleanup is narrowed to conditional follow-ups: strict decoder body consumption is pinned, subscribe/unsubscribe response shaping is consolidated, and only `v1.bsatn.shunter` is accepted.
- Use reference behavior as evidence for runtime semantics, but prefer Shunter's own simpler contract when compatibility-only details add cost without value.

Slices A, B, C, D, E.1, E.2, F.1, F.2, F.3, F.4, G.1, G.2, G.3, H, I, `sender-exact-case`, scout cleanup, boolean-constant WHERE masking, signed-LIMIT feature rejection, join-keyword handling, SubscribeSingle unindexed-join rejection text, SubscribeSingle LIMIT-before-set-quantifier ordering, `lowercase-x-string-bytes-prefix`, literal-keyword identifier rejection, case-distinct relation-alias routing, ambiguous case-folded table lookup rejection, exact SQL identifier lookup, explicit join-WHERE field-vs-field rejection, cross-join WHERE policy pins, dead eval memoization cleanup, and dead structured-query helper cleanup are landed / confirmed green (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error class routing, compile-stage `DuplicateName` + join `UnexpectedType` / `InvalidOp` parity, `Unresolved::Var` text for missing-field lookups, SubscribeSingle projection-column reorder, base-table-after-alias `Unresolved::Var`, SELECT ALL/DISTINCT set-quantifier rejection, WHERE-precedes-projection on single-table SELECT, JOIN ON resolution precedes wildcard guard + WHERE FALSE pruning, missing-table precedes duplicate-join-alias, qualified projection / wildcard qualifier not in scope, unqualified names in joins, strict JOIN ON equality, exact-case `:sender`, subscription LIMIT text, one-off LIMIT numeric parsing, qualified-name ordering, logical-WHERE branch typechecking, signed LIMIT feature-text ordering, explicit join keyword parsing, indexed-join plan enforcement on SubscribeSingle, subscribe LIMIT rejection before SELECT set-quantifier rejection, lowercase `x'` string bytes-coercion rejection, unquoted `TRUE`/`FALSE`/`NULL` rejection in identifier positions, preserving explicit case-distinct aliases such as `"R"` vs `r` through parser/protocol join routing, rejecting ambiguous case-folded table names before one-off execution or subscription registration, enforcing byte-exact SQL table/column/alias/qualifier lookup while leaving schema's non-SQL case-folding helpers intact, rejecting base-table qualifiers after relation aliasing in join WHERE/projection scopes, rejecting inner-join WHERE column comparisons with a stable diagnostic, keeping cross-join WHERE query-only for the documented narrow shapes, removing the unused per-evaluation memoization map from subscription evaluation, and removing the unused `compileQuery` / `parseQueryString` / one-off matcher helpers).

No fixed implementation slice is queued after the exact-identifier, join-WHERE policy, and dead structured-query remnant cleanups. Choose the next OI-002 batch from `TECH-DEBT.md::OI-002` by a fresh scout unless the user supplies a narrower focus. Remaining likely work is evidence-driven: projection regressions only with a fresh failing scenario, validation ordering only when it changes acceptance/rows/errors, or shared fixture/test deduplication.

## Confirmed Work Queue

Add failing tests first, then implement. Batch only when the locus overlaps; commit per slice if the working tree is clean enough to do so without sweeping unrelated user changes into the commit. If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

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
