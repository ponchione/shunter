# JOIN ON equality + single-relation filter widening (P0-SUBSCRIPTION-027)

Date: 2026-04-23
Status: design approved, ready for implementation plan
Related handoff: `NEXT_SESSION_HANDOFF.md` candidate #1
Related issue: `TECH-DEBT.md` OI-002

## Goal

Widen the shunter SQL parser to accept `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface, e.g.:

```sql
SELECT o.id
FROM Orders o JOIN Inventory product
ON o.product_id = product.id AND product.quantity < 10
```

Both one-off and subscribe paths accept the new shape. The widening is a parser-only admission: the ON-extracted filter is transparently folded into `Statement.Predicate` so downstream compile, validate, and runtime paths see input indistinguishable from the semantically equivalent `WHERE`-form.

## Why this is a parser-only change

The shunter runtime already handles the end-to-end semantics for this query when written in WHERE form. Existing pins:

- `query/sql/parser_test.go:609` parses `... ON o.product_id = product.id WHERE product.quantity < 10`.
- `protocol/handle_oneoff_test.go:1369` runs the one-off end-to-end.
- `handle_subscribe.go:261-266` compiles `stmt.Predicate` on joined subscriptions via `compileSQLPredicateForRelations`.
- `subscription/validate.go:173-187` (`validateJoin`) accepts `p.Filter` and routes it through the standard predicate validator, requiring filter tables to be a subset of the two join relations.

Shunter's parser accepts only inner joins (`parser.go:852-853` rejects any non-`=` ON operator; no LEFT/OUTER surface). For inner joins, reference SQL treats `A JOIN B ON eq AND F` as equivalent to `A JOIN B ON eq WHERE F`. The parser-level fold preserves this equivalence without touching any runtime code.

## Divergence-discipline framing

Prior OI-002 slices `P0-SUBSCRIPTION-019` through `P0-SUBSCRIPTION-026` followed a "one-off widens; subscribe still rejects" pattern. This slice breaks that pattern deliberately, because:

- The ON-form is *syntactically* new but *semantically* identical to the already-accepted WHERE-form.
- Rejecting the ON-form on subscribe while accepting the WHERE-form would be a purely textual asymmetry with no runtime basis (users could express the same constraint in WHERE and get it accepted).
- Subscribe's existing join-filter compile path (`compileSQLPredicateForRelations`) already accepts exactly the shape we admit.

The spec pins this equivalence with a parser-level parity test (`TestParseJoinOnEqualityParityWithWhereForm`) so future contributors have a durable check on the B premise.

## Scope

### In scope

- `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface (distinct-table and self-join alias cases both supported, mirroring existing ON-equality support).
- Filter may reference either the left-side or right-side relation (single-relation filter; no column-vs-column).
- Comparison operators and literal types: whatever `parseComparisonPredicate` already accepts in WHERE context.
- Composition with existing WHERE clause: `... ON eq AND F1 WHERE F2` is accepted and folds to `AndPredicate{F1, F2}`.

### Out of scope (deferred)

- Multiple AND conjuncts in ON (`ON eq AND F1 AND F2`) → parser rejects.
- OR in ON (`ON eq AND F1 OR F2`) → parser rejects.
- Non-equality primary ON (already rejected).
- Three-way joins (already rejected).
- Column-vs-column filters in ON (reserved for the join's own equality slot).

## Parser surface change

### Grammar addition

```
join_clause = [INNER] JOIN table-ref [alias]
              [ ON qualified-col = qualified-col [ AND on-filter ] ]
on-filter   = qualified-col comparison-op literal
```

### Acceptance

- `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`
- `SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND o.id = 5`

### Rejections (each with a named, scope-bound error)

| Shape | Error |
|---|---|
| `... AND x < 1 AND y > 2` | `"JOIN ON filter accepts at most one AND-conjunct"` |
| `... AND x < 1 OR y > 2` | `"OR not supported in JOIN ON"` |
| `... AND p.x = o.y` | `"JOIN ON filter must compare a column to a literal"` |
| `... AND quantity < 10` | existing `"join WHERE columns must be qualified"` |
| `... AND z.x < 10` where `z` is neither join relation | existing qualifier-resolution error |

`TRUE`/`FALSE` shorthand is structurally unreachable: the ON-filter slot calls `parseComparisonPredicate` directly, skipping `parsePredicateTerm`'s TRUE/FALSE dispatch.

### Code changes

- `parseJoinClause` signature changes from `(*JoinClause, []string, error)` to `(*JoinClause, []string, Predicate, error)`. The new return is the ON-extracted filter predicate or `nil`.
- Inside `parseJoinClause`, after the alias-swap check at `parser.go:867`, peek for `AND`. If present:
  1. Advance past `AND`.
  2. Build `relationBindings{requireQualify: true, byQualifier: lookup}` from the `lookup` variable already constructed at line 843 for the equality parser.
  3. Call `parseComparisonPredicate(bindings)`.
  4. Type-assert the result is `ComparisonPredicate` (not `ColumnComparisonPredicate`); reject with `"JOIN ON filter must compare a column to a literal"` otherwise.
  5. Peek for a lingering `AND` or `OR` token and reject with the named errors above.
- `parseStatement` at the callsite `parser.go:574` captures the extra predicate. After the WHERE-branch at lines 628-629, if the ON-predicate is non-nil:
  - If `stmt.Predicate == nil`: set `stmt.Predicate = onPred`.
  - Else: set `stmt.Predicate = AndPredicate{Left: onPred, Right: stmt.Predicate}`.
  - Recompute `stmt.Filters = flattenAndFilters(stmt.Predicate)` (reuse the existing helper; discard the `matched` boolean).

### Semantic-equivalence invariant

- `... ON eq AND F` (no WHERE): `Statement.Predicate` is structurally identical to the predicate produced by `... ON eq WHERE F`. Both are a single `ComparisonPredicate`.
- `... ON eq AND F1 WHERE F2` (each a single comparison): `Statement.Predicate` is `AndPredicate{Left: F1, Right: F2}`, structurally identical to the predicate from `... ON eq WHERE F1 AND F2` (since `parseConjunction` builds the same `And(F1, F2)` tree for a two-conjunct WHERE).
- `... ON eq AND F0 WHERE F1 AND F2`: the fold produces a right-leaning `And(F0, And(F1, F2))`, whereas the pure-WHERE equivalent `... WHERE F0 AND F1 AND F2` produces a left-leaning `And(And(F0, F1), F2)`. This lean difference is absorbed by the subscription-side associative-grouping canonicalization (`P0-SUBSCRIPTION-015`) and does not affect dedup or runtime behavior. The parser-level parity pin (`TestParseJoinOnEqualityParityWithWhereForm`) targets the single-filter form where structural identity holds without canonicalization.

## Data flow

```
SQL string
  │
  ▼
sql.Parse  ─►  Statement{
                Join: {LeftOn, RightOn, HasOn: true},
                Predicate: nil,                            // neither ON-filter nor WHERE
                        or ComparisonPredicate{...},        // exactly one of ON-filter or WHERE
                        or AndPredicate{ON-filter, WHERE}, // both present
                Filters:   flattenAndFilters(Predicate),
              }
  │
  ▼
compileSQLQueryString  (handle_subscribe.go:164 — shared by one-off & subscribe)
  │
  ▼
stmt.Join != nil branch  (handle_subscribe.go:185)
  │
  ▼
compileSQLPredicateForRelations(stmt.Predicate, relations, aliasTag, caller)
  ─► subscription.Join{Filter: ...}
  │
  ▼
validateJoin  (subscription/validate.go:149)
  │
  ▼
existing runtime (executor / fanout / manager)
```

No downstream file edits expected. If any non-parser implementation file needs a change to make the tests pass, that is a signal the B (transparent-fold) premise is broken and the design needs revision before proceeding.

## Test surface

### Parser pins — `query/sql/parser_test.go`

- `TestParseJoinOnEqualityWithFilter` — `... ON o.product_id = product.id AND product.quantity < 10`. Asserts `stmt.Join.HasOn`, `stmt.Join.LeftOn`/`RightOn` unchanged, `stmt.Predicate` is `ComparisonPredicate{Filter: {Table: "Inventory", Column: "quantity", Op: "<", Literal: 10}}`, `len(stmt.Filters) == 1`.
- `TestParseJoinOnEqualityWithFilterOnLeftSide` — `... AND o.id = 5`. Asserts filter binds to the left relation.
- `TestParseJoinOnEqualityWithFilterAndWhere` — `... ON a = b AND F1 WHERE F2`. Asserts `stmt.Predicate` is `AndPredicate{Left: F1, Right: F2}`, `len(stmt.Filters) == 2`.
- `TestParseJoinOnEqualityParityWithWhereForm` — parses `... ON a = b AND F` and `... ON a = b WHERE F`; asserts `reflect.DeepEqual(stmt1.Predicate, stmt2.Predicate)` and `reflect.DeepEqual(stmt1.Filters, stmt2.Filters)`. Locks the B invariant.
- `TestParseRejectsJoinOnFilterMultipleConjuncts` — `... AND x < 1 AND y > 2` → `"JOIN ON filter accepts at most one AND-conjunct"`.
- `TestParseRejectsJoinOnFilterOr` — `... AND x < 1 OR y > 2` → `"OR not supported in JOIN ON"`.
- `TestParseRejectsJoinOnFilterColumnVsColumn` — `... AND p.x = o.y` → `"JOIN ON filter must compare a column to a literal"`.
- `TestParseRejectsJoinOnFilterUnqualifiedColumn` — `... AND quantity < 10` → existing qualifier error.
- `TestParseRejectsJoinOnFilterThirdRelation` — `... AND z.x < 10` where `z` is neither join relation → existing qualifier-resolution error.

### One-off pins — `protocol/handle_oneoff_test.go`

- `TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows` — uses the handoff query `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`. Seeds matched + unmatched pairs; asserts only rows where the filter holds come back.
- `TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm` — runs the ON-form and the equivalent WHERE-form against identically seeded tables; asserts the projected row sets are equal.

### Subscribe pins — `protocol/handle_subscribe_test.go`

- `TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted` — subscribes to the handoff query; asserts registration succeeds and initial-state rows match the filter.
- `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected` — same shape with tables lacking the required index on the equality columns; asserts `ErrUnindexedJoin` still fires. Pins independence of the unindexed-join gate from filter presence.

### Tests explicitly *not* added

- No subscribe-side rejection test for the ON-filter shape — subscribe accepts under B; such a test would pin the wrong behavior.

## Documentation updates

### `TECH-DEBT.md` OI-002

- Extend the A2-closures sentence on line 59: append `", and one-off/subscribe JOIN ON equality-plus-single-relation-filter widening as a transparent parser admission"`.
- Add a new bullet after the mixed-relation projection bullet (line 65):
  > one-off and subscribe SQL now also accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface (e.g., `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`); the parser transparently folds the ON-extracted filter into `Statement.Predicate`, producing identical output to the semantically equivalent WHERE-form, so subscribe accepts as a direct consequence of admitting the parser shape, while multi-conjunct / OR / column-vs-column / unqualified / third-relation / three-way-join rejections remain in place.
- Append the new test function names to the authoritative-pins paragraph (line 66): `TestParseJoinOnEqualityWithFilter`, `TestParseJoinOnEqualityWithFilterOnLeftSide`, `TestParseJoinOnEqualityWithFilterAndWhere`, `TestParseJoinOnEqualityParityWithWhereForm`, `TestParseRejectsJoinOnFilterMultipleConjuncts`, `TestParseRejectsJoinOnFilterOr`, `TestParseRejectsJoinOnFilterColumnVsColumn`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected`.
- Update the execution note at line 73: replace the reference to the join-backed `COUNT(*)` closure with a reference to the JOIN ON equality-plus-single-relation-filter widening slice and note that subscribe acceptance is treated as a transparent parser admission rather than a one-off-only divergence.

### `docs/parity-phase0-ledger.md`

Add a new row after `P0-SUBSCRIPTION-026` (line 61):

```
| `P0-SUBSCRIPTION-027` one-off/subscribe JOIN ON equality-plus-single-relation-filter widening | `closed` | `query/sql/parser_test.go`, `protocol/handle_oneoff_test.go`, `protocol/handle_subscribe_test.go`, `query/sql/parser.go` | One-off/ad hoc and subscription SQL now accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface; the parser folds the ON-extracted filter into `Statement.Predicate` identically to the semantically equivalent WHERE-form (verified by parser-level parity pin), so subscribe acceptance is a direct side-effect of admitting the parser shape rather than a new executor/fanout surface. Multi-conjunct, OR, column-vs-column, unqualified, third-relation, and three-way-join rejections remain pinned; unindexed-join rejection still fires for subscriptions regardless of filter presence. |
```

Append to the broad-themes bullet (line 99): `", and one-off/subscribe JOIN ON equality-plus-single-relation-filter widening admitted via transparent parser fold rather than subscribe-side widening"`.

### `NEXT_SESSION_HANDOFF.md`

- Update "Latest closed OI-002 query-only slice" block (lines 38-55):
  - Change the pointer to `P0-SUBSCRIPTION-027`.
  - Replace the representative accepted-shape example with `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`.
  - Rewrite "Behavior now pinned":
    - parser admits ON-equality plus exactly one qualified-column/literal filter
    - fold is semantically identical to the equivalent WHERE-form and pinned as such
    - subscribe-side accepts transparently via the existing WHERE-filter compile path
    - multi-conjunct, OR, column-vs-column, unqualified-column, third-relation, and three-way-join rejections stay
    - unindexed-join rejection for subscriptions stays independent of filter presence
- Update "Primary files touched" (lines 57-66) to the actual touched set: `query/sql/parser.go`, `query/sql/parser_test.go`, `protocol/handle_oneoff_test.go`, `protocol/handle_subscribe_test.go`, `TECH-DEBT.md`, `docs/parity-phase0-ledger.md`.
- Update "Latest validation reported" (lines 68-72):
  - `rtk go test ./query/sql ./protocol -run 'JoinOnEquality.*Filter|ParseRejectsJoinOnFilter' -count=1 -v`
  - `rtk go test ./query/sql ./protocol -count=1`
  - `rtk go test ./query/sql ./protocol ./subscription -count=1`
- Update "Good next OI-002 candidates" (lines 76-92): drop item #1 (now closed); #2 (runtime/fanout lanes) and #3 (RLS) stay unchanged. Add a replacement item #1 as a TBD parser/compile seam continuation, to be chosen from fresh scout.
- Add a framing paragraph under "Latest OI-002 state to preserve":
  > `P0-SUBSCRIPTION-027` is the first OI-002 slice since `P0-SUBSCRIPTION-018` where subscribe's acceptance surface widens alongside one-off. The justification is that the ON-form is syntactically new but semantically identical to the already-accepted WHERE-form — the parser produces indistinguishable output for either. Future slices should default back to the one-off-only pattern; use this precedent only when the new shape has a pinned parser-level parity claim against an already-accepted form.

## Validation plan

Before calling the slice done, run:

```
rtk go test ./query/sql ./protocol -run 'JoinOnEquality.*Filter|ParseRejectsJoinOnFilter' -count=1 -v
rtk go test ./query/sql ./protocol -count=1
rtk go test ./query/sql ./protocol ./subscription -count=1
rtk go vet ./query/sql ./protocol
rtk go fmt ./query/sql ./protocol
```

The full-package runs catch incidental regressions in canonicalization, fanout, or adjacent parser surfaces; the vet run catches any interface/behavior drift introduced by the `parseJoinClause` signature change.

## Risks

- **Canonicalization drift**: if the AND-fold order introduces a canonical-form difference from the WHERE-written equivalent, subscription dedup could split queries that ought to share a group. `TestParseJoinOnEqualityParityWithWhereForm` pins the parser-level equivalence; the subscription-side canonicalization from `P0-SUBSCRIPTION-013`/descendants is input-order-invariant, so identical parser output guarantees identical canonical keys.
- **Signature change blast radius**: `parseJoinClause` is called from exactly one site (`parser.go:574`); the signature change is mechanical.
- **Unindexed-join divergence**: the new shape does not interact with the unindexed-join gate. `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected` pins this.
