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

Slices A, B, C, D, E.1, E.2, F.1, F.2, F.3, F.4, G.1, G.2, G.3, H, I, `sender-exact-case`, scout cleanup, boolean-constant WHERE masking, signed-LIMIT feature rejection, join-keyword handling, SubscribeSingle unindexed-join rejection text, SubscribeSingle LIMIT-before-set-quantifier ordering, `lowercase-x-string-bytes-prefix`, literal-keyword identifier rejection, case-distinct relation-alias routing, ambiguous case-folded table lookup rejection, dead eval memoization cleanup, and dead structured-query helper cleanup are landed / confirmed green (source-text seam, reference parse routing, compound algebraic names + Timestamp / Array<String> error class routing, compile-stage `DuplicateName` + join `UnexpectedType` / `InvalidOp` parity, `Unresolved::Var` text for missing-field lookups, SubscribeSingle projection-column reorder, base-table-after-alias `Unresolved::Var`, SELECT ALL/DISTINCT set-quantifier rejection, WHERE-precedes-projection on single-table SELECT, JOIN ON resolution precedes wildcard guard + WHERE FALSE pruning, missing-table precedes duplicate-join-alias, qualified projection / wildcard qualifier not in scope, unqualified names in joins, strict JOIN ON equality, exact-case `:sender`, subscription LIMIT text, one-off LIMIT numeric parsing, qualified-name ordering, logical-WHERE branch typechecking, signed LIMIT feature-text ordering, explicit join keyword parsing, indexed-join plan enforcement on SubscribeSingle, subscribe LIMIT rejection before SELECT set-quantifier rejection, lowercase `x'` string bytes-coercion rejection, unquoted `TRUE`/`FALSE`/`NULL` rejection in identifier positions, preserving explicit case-distinct aliases such as `"R"` vs `r` through parser/protocol join routing, rejecting ambiguous case-folded table names before one-off execution or subscription registration, removing the unused per-evaluation memoization map from subscription evaluation, and removing the unused `compileQuery` / `parseQueryString` / one-off matcher helpers).

No fixed implementation slice is queued after the ambiguous table-lookup and dead-helper cleanups. Choose the next OI-002 batch from `TECH-DEBT.md::OI-002` by a fresh scout unless the user supplies a narrower focus. Priority order still starts with remaining identifier case handling; treat it as a scout first because the remaining blast radius spans schema/table lookup, column lookup, quoted-vs-unquoted identifier policy, OneOff, SubscribeSingle, and SubscribeMulti.

Keep case-preservation and broader join/cross-join runtime semantics separate unless the chosen fix naturally requires them.

## Confirmed Work Queue

Add failing tests first, then implement. Batch only when the locus overlaps; commit per slice if the working tree is clean enough to do so without sweeping unrelated user changes into the commit. If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Adjacent OI-002 Candidates

Recorded in `TECH-DEBT.md::OI-002`. Group only if the change locus overlaps; otherwise keep them as separate slices.

- Quoted-identifier case preservation (`SELECT * FROM "T"`, `SELECT * FROM t WHERE "U32" = 7`, alias case preservation, etc.). Reference `SqlIdent` is byte-equal case-sensitive; Shunter currently uses `strings.EqualFold` across schema lookup, column lookup, and alias matching. Larger blast radius; keep separate.
- SubscribeSingle / OneOff cross-join WHERE Bool-expression admission. Broader runtime/parser surface than fixed-literal — keep separate.
- Inner-join WHERE column comparisons (field-vs-field) admission. Same broader surface as cross-join WHERE.


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
