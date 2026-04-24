# JOIN ON equality + single-relation filter widening — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admit `JOIN ... ON col = col AND <qualified-column op literal>` at the parser, fold the ON-extracted filter transparently into `Statement.Predicate`, so both one-off and subscribe paths accept it through the already-working WHERE-form compile seam with no runtime changes.

**Architecture:** Parser-only change. `parseJoinClause` grows one return value (the ON-extracted predicate); `parseStatement` folds it into `stmt.Predicate` via AND. Every downstream path (`compileSQLQueryString`, `compileSQLPredicateForRelations`, `validateJoin`, executor, fanout) receives input indistinguishable from the WHERE-form and requires no edits.

**Tech Stack:** Go 1.x, standard-library `testing`, `errors`, `reflect`, `strings`. Shell via `rtk` prefix (see `RTK.md`).

**Design spec:** `docs/superpowers/specs/2026-04-23-join-on-filter-widening-design.md`.

---

## File map

| File | Role | Change |
|---|---|---|
| `query/sql/parser.go` | SQL surface parser | grow `parseJoinClause` return; fold in `parseStatement`; new rejections |
| `query/sql/parser_test.go` | parser pin surface | 9 new tests (5 acceptances, 4 rejections) |
| `protocol/handle_oneoff_test.go` | one-off end-to-end pin surface | 2 new tests |
| `protocol/handle_subscribe_test.go` | subscribe end-to-end pin surface | 2 new tests |
| `TECH-DEBT.md` | OI-002 tracking | append closure bullet + test pins + execution note edit |
| `docs/parity-phase0-ledger.md` | parity scenario ledger | add `P0-SUBSCRIPTION-027` row + update broad-themes bullet |
| `NEXT_SESSION_HANDOFF.md` | next-session pointer | update "Latest closed" block + files + validation + candidates + framing |

No edits to `handle_subscribe.go`, `handle_oneoff.go`, `subscription/validate.go`, `subscription/predicate.go`, `subscription/eval.go`, executor, or fanout. If a test in this plan can't pass without editing those files, stop and re-read the spec's "transparent fold" argument — an edit there means the B premise broke and the design needs revision.

---

## Task 1: Scaffold — grow `parseJoinClause` signature, caller wires `nil` through

**Files:**
- Modify: `query/sql/parser.go:815-868` (the `parseJoinClause` function)
- Modify: `query/sql/parser.go:574` (the only caller)

No new tests — this task is a pure signature change that must leave behavior identical.

- [ ] **Step 1: Change the function signature**

Edit `query/sql/parser.go:815`:

```go
func (p *parser) parseJoinClause(leftTable string, leftQualifiers []string) (*JoinClause, []string, error) {
```

becomes:

```go
func (p *parser) parseJoinClause(leftTable string, leftQualifiers []string) (*JoinClause, []string, Predicate, error) {
```

Every `return ..., err` and `return ..., nil` in the function body must grow one additional `nil` for the new `Predicate` return slot. There are six return statements in this function today (lines 823, 825, 833, 835, 838, 841, 846, 850, 853, 857, 860, 866, 868 — scan the full body). Each needs exactly one extra `nil` inserted before the error slot.

Concretely, for each current shape `return X, Y, err` → `return X, Y, nil, err`, and `return &JoinClause{...}, rightQualifiers, nil` → `return &JoinClause{...}, rightQualifiers, nil, nil`.

- [ ] **Step 2: Wire the caller to receive-and-ignore the new return**

Edit `query/sql/parser.go:574`. Change:

```go
		join, rightQualifiers, err := p.parseJoinClause(tableName, leftQualifiers)
```

to:

```go
		join, rightQualifiers, _, err := p.parseJoinClause(tableName, leftQualifiers)
```

Using `_` documents intent: we know there's a slot, we're not using it yet.

- [ ] **Step 3: Run the full parser and protocol tests to verify behavior is identical**

```
rtk go test ./query/sql ./protocol -count=1
```

Expected: PASS across the board. Any failure means a `return` site was missed or the caller signature is wrong.

- [ ] **Step 4: Run vet**

```
rtk go vet ./query/sql ./protocol
```

Expected: no output (clean).

- [ ] **Step 5: Commit**

```
rtk git add query/sql/parser.go
rtk git commit -m "$(cat <<'EOF'
refactor(sql/parser): grow parseJoinClause return to carry optional ON-extracted predicate

Adds a nil-valued Predicate return slot to parseJoinClause and wires the sole caller to ignore it. Scaffolding for the P0-SUBSCRIPTION-027 JOIN ON equality + single-relation filter widening; no behavior change this commit.
EOF
)"
```

---

## Task 2: Accept baseline shape — `ON eq AND <qualified right-side comparison>` + fold

**Files:**
- Modify: `query/sql/parser.go:815-868` (parseJoinClause — add the AND-filter arm)
- Modify: `query/sql/parser.go:622-630` (parseStatement — fold the ON-filter into stmt.Predicate/Filters)
- Test: `query/sql/parser_test.go` (append `TestParseJoinOnEqualityWithFilter`)

- [ ] **Step 1: Write the failing test**

Append to `query/sql/parser_test.go`:

```go
func TestParseJoinOnEqualityWithFilter(t *testing.T) {
	stmt, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if !stmt.Join.HasOn {
		t.Fatal("Join.HasOn = false, want true")
	}
	if stmt.Join.LeftOn.Table != "Orders" || stmt.Join.LeftOn.Column != "product_id" {
		t.Fatalf("left ON = %+v, want Orders.product_id", stmt.Join.LeftOn)
	}
	if stmt.Join.RightOn.Table != "Inventory" || stmt.Join.RightOn.Column != "id" {
		t.Fatalf("right ON = %+v, want Inventory.id", stmt.Join.RightOn)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "Inventory" || cmp.Filter.Column != "quantity" || cmp.Filter.Alias != "product" {
		t.Fatalf("ON-filter = %+v, want Inventory.quantity (alias product)", cmp.Filter)
	}
	if cmp.Filter.Op != "<" || cmp.Filter.Literal.Int != 10 {
		t.Fatalf("ON-filter op/literal = %+v, want < 10", cmp.Filter)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```
rtk go test ./query/sql -run TestParseJoinOnEqualityWithFilter -count=1 -v
```

Expected: FAIL with a parse error about an unexpected `AND` token (the parser returns before consuming `AND` today, and the caller's EOF check at `parser.go:639-641` errors on the leftover token).

- [ ] **Step 3: Implement the AND-filter arm in `parseJoinClause`**

Edit `query/sql/parser.go`. Find the final `return` statement in `parseJoinClause` at line 868:

```go
		return &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: true, LeftOn: leftOn, RightOn: rightOn}, rightQualifiers, nil, nil
```

Replace with:

```go
		jc := &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: true, LeftOn: leftOn, RightOn: rightOn}
		if !isKeywordToken(p.peek(), "AND") {
			return jc, rightQualifiers, nil, nil
		}
		p.advance()
		onBindings := relationBindings{requireQualify: true, byQualifier: lookup}
		onPred, err := p.parseComparisonPredicate(onBindings)
		if err != nil {
			return nil, nil, nil, err
		}
		return jc, rightQualifiers, onPred, nil
```

- [ ] **Step 4: Wire the caller to capture the ON predicate and fold it**

Edit `query/sql/parser.go:574`. Change:

```go
		join, rightQualifiers, _, err := p.parseJoinClause(tableName, leftQualifiers)
```

to:

```go
		join, rightQualifiers, onFilter, err := p.parseJoinClause(tableName, leftQualifiers)
```

Then insert the fold block immediately after the WHERE branch at `parser.go:630` (after `stmt.Filters = filters`, before `limit, err := p.parseLimit()` at line 631):

```go
	if onFilter != nil {
		if stmt.Predicate != nil {
			stmt.Predicate = AndPredicate{Left: onFilter, Right: stmt.Predicate}
		} else {
			stmt.Predicate = onFilter
		}
		stmt.Filters, _ = flattenAndFilters(stmt.Predicate)
	}
```

- [ ] **Step 5: Run the test to verify it passes**

```
rtk go test ./query/sql -run TestParseJoinOnEqualityWithFilter -count=1 -v
```

Expected: PASS.

- [ ] **Step 6: Run the full package to verify no regressions**

```
rtk go test ./query/sql ./protocol -count=1
```

Expected: PASS across the board.

- [ ] **Step 7: Commit**

```
rtk git add query/sql/parser.go query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
feat(sql/parser): admit JOIN ON col = col AND <qualified-column op literal>

Parser accepts one trailing AND-comparison after the ON-equality and returns it as an extra predicate. parseStatement folds it into stmt.Predicate (ANDing with any WHERE predicate) and reflattens stmt.Filters, so every downstream path sees input indistinguishable from the semantically equivalent WHERE-form. One-off and subscribe compile/validate/runtime paths unchanged.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 3: Acceptance pin — filter on the left-side relation

**Files:**
- Test: `query/sql/parser_test.go` (append `TestParseJoinOnEqualityWithFilterOnLeftSide`)

This test passes without further implementation because `bindings.byQualifier` maps both relations symmetrically.

- [ ] **Step 1: Write the failing test (well, the sanity pin)**

Append to `query/sql/parser_test.go`:

```go
func TestParseJoinOnEqualityWithFilterOnLeftSide(t *testing.T) {
	stmt, err := Parse("SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND o.id = 5")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "Orders" || cmp.Filter.Column != "id" || cmp.Filter.Alias != "o" {
		t.Fatalf("ON-filter = %+v, want Orders.id (alias o)", cmp.Filter)
	}
	if cmp.Filter.Op != "=" || cmp.Filter.Literal.Int != 5 {
		t.Fatalf("ON-filter op/literal = %+v, want = 5", cmp.Filter)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
}
```

- [ ] **Step 2: Run the test**

```
rtk go test ./query/sql -run TestParseJoinOnEqualityWithFilterOnLeftSide -count=1 -v
```

Expected: PASS (Task 2's implementation is symmetric).

If FAIL: check `parseColumnRefForPredicate` / `bindings.byQualifier` propagation. If the test fails here, the symmetry assumption is wrong and the implementation needs revision.

- [ ] **Step 3: Commit**

```
rtk git add query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
test(sql/parser): pin JOIN ON AND-filter on the left-side relation

Symmetric counterpart to TestParseJoinOnEqualityWithFilter. Confirms bindings.byQualifier routes either-side qualifiers through parseComparisonPredicate identically.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 4: Acceptance pin — ON-filter composed with WHERE

**Files:**
- Test: `query/sql/parser_test.go` (append `TestParseJoinOnEqualityWithFilterAndWhere`)

- [ ] **Step 1: Write the test**

Append to `query/sql/parser_test.go`:

```go
func TestParseJoinOnEqualityWithFilterAndWhere(t *testing.T) {
	stmt, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10 WHERE o.id > 0")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	andPred, ok := stmt.Predicate.(AndPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want AndPredicate", stmt.Predicate)
	}
	leftCmp, ok := andPred.Left.(ComparisonPredicate)
	if !ok {
		t.Fatalf("AndPredicate.Left type = %T, want ComparisonPredicate (ON-filter)", andPred.Left)
	}
	if leftCmp.Filter.Table != "Inventory" || leftCmp.Filter.Column != "quantity" || leftCmp.Filter.Op != "<" || leftCmp.Filter.Literal.Int != 10 {
		t.Fatalf("AndPredicate.Left filter = %+v, want Inventory.quantity < 10", leftCmp.Filter)
	}
	rightCmp, ok := andPred.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("AndPredicate.Right type = %T, want ComparisonPredicate (WHERE-filter)", andPred.Right)
	}
	if rightCmp.Filter.Table != "Orders" || rightCmp.Filter.Column != "id" || rightCmp.Filter.Op != ">" || rightCmp.Filter.Literal.Int != 0 {
		t.Fatalf("AndPredicate.Right filter = %+v, want Orders.id > 0", rightCmp.Filter)
	}
	if len(stmt.Filters) != 2 {
		t.Fatalf("Filters len = %d, want 2", len(stmt.Filters))
	}
}
```

- [ ] **Step 2: Run the test**

```
rtk go test ./query/sql -run TestParseJoinOnEqualityWithFilterAndWhere -count=1 -v
```

Expected: PASS. Task 2's fold already handles `stmt.Predicate != nil`.

- [ ] **Step 3: Commit**

```
rtk git add query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
test(sql/parser): pin JOIN ON AND-filter composed with WHERE

Verifies stmt.Predicate = AndPredicate{Left: ON-filter, Right: WHERE-filter} and stmt.Filters contains both leaves after flattening.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 5: Parity pin — ON-form produces identical Statement to WHERE-form

**Files:**
- Test: `query/sql/parser_test.go` (append `TestParseJoinOnEqualityParityWithWhereForm`)
- Modify: `query/sql/parser_test.go:3-7` (import block — add `reflect`)

- [ ] **Step 1: Add `reflect` to the test file's import block**

Edit `query/sql/parser_test.go:3-7`. Change:

```go
import (
	"errors"
	"strings"
	"testing"
)
```

to:

```go
import (
	"errors"
	"reflect"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Write the test**

Append to `query/sql/parser_test.go`:

```go
// TestParseJoinOnEqualityParityWithWhereForm locks the B (transparent-fold)
// invariant: for a single-filter case, the ON-form parses to a Statement
// structurally identical to the equivalent WHERE-form. See
// docs/superpowers/specs/2026-04-23-join-on-filter-widening-design.md §
// "Semantic-equivalence invariant".
func TestParseJoinOnEqualityParityWithWhereForm(t *testing.T) {
	onForm, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10")
	if err != nil {
		t.Fatalf("ON-form Parse error: %v", err)
	}
	whereForm, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10")
	if err != nil {
		t.Fatalf("WHERE-form Parse error: %v", err)
	}
	if !reflect.DeepEqual(onForm.Predicate, whereForm.Predicate) {
		t.Fatalf("Predicate divergence:\n  ON-form    = %+v\n  WHERE-form = %+v", onForm.Predicate, whereForm.Predicate)
	}
	if !reflect.DeepEqual(onForm.Filters, whereForm.Filters) {
		t.Fatalf("Filters divergence:\n  ON-form    = %+v\n  WHERE-form = %+v", onForm.Filters, whereForm.Filters)
	}
}
```

- [ ] **Step 3: Run the test**

```
rtk go test ./query/sql -run TestParseJoinOnEqualityParityWithWhereForm -count=1 -v
```

Expected: PASS. Both forms produce a `Statement` with `Predicate = ComparisonPredicate{Filter: {Inventory.quantity < 10, alias product}}` and `Filters = []Filter{that one filter}`.

- [ ] **Step 4: Commit**

```
rtk git add query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
test(sql/parser): pin parser-level parity between JOIN ON AND-filter and WHERE-filter

Locks the transparent-fold invariant: for a single-filter case, the ON-form parses to a Statement structurally identical to the WHERE-form.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 6: Reject multi-conjunct AND in ON

**Files:**
- Modify: `query/sql/parser.go` (parseJoinClause — add lingering-AND check)
- Test: `query/sql/parser_test.go` (append `TestParseRejectsJoinOnFilterMultipleConjuncts`)

- [ ] **Step 1: Write the failing test**

Append to `query/sql/parser_test.go`:

```go
func TestParseRejectsJoinOnFilterMultipleConjuncts(t *testing.T) {
	_, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10 AND o.id > 0")
	if err == nil {
		t.Fatal("expected error for multi-conjunct ON filter")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
	if !strings.Contains(err.Error(), "JOIN ON filter accepts at most one AND-conjunct") {
		t.Fatalf("err = %q, want substring 'JOIN ON filter accepts at most one AND-conjunct'", err.Error())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```
rtk go test ./query/sql -run TestParseRejectsJoinOnFilterMultipleConjuncts -count=1 -v
```

Expected: FAIL. Without the lingering-AND check, the parser returns with the single-comparison predicate; the second `AND` surfaces after `parseJoinClause` returns, and the caller's logic at line 605+ / parseWhere-detection produces either a successful-but-wrong parse or a weakly-scoped error — either way, the substring assertion fails.

- [ ] **Step 3: Add the lingering-AND check in `parseJoinClause`**

In `query/sql/parser.go`, inside `parseJoinClause`, replace the AND-arm block from Task 2:

```go
		p.advance()
		onBindings := relationBindings{requireQualify: true, byQualifier: lookup}
		onPred, err := p.parseComparisonPredicate(onBindings)
		if err != nil {
			return nil, nil, nil, err
		}
		return jc, rightQualifiers, onPred, nil
```

with:

```go
		p.advance()
		onBindings := relationBindings{requireQualify: true, byQualifier: lookup}
		onPred, err := p.parseComparisonPredicate(onBindings)
		if err != nil {
			return nil, nil, nil, err
		}
		if isKeywordToken(p.peek(), "AND") {
			return nil, nil, nil, p.unsupported("JOIN ON filter accepts at most one AND-conjunct")
		}
		return jc, rightQualifiers, onPred, nil
```

- [ ] **Step 4: Run the test to verify it passes**

```
rtk go test ./query/sql -run TestParseRejectsJoinOnFilterMultipleConjuncts -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Run the full parser test suite to verify no regressions**

```
rtk go test ./query/sql -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```
rtk git add query/sql/parser.go query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
feat(sql/parser): reject multi-conjunct AND in JOIN ON filter

Keeps the ON-suffix bounded to exactly one comparison. Multi-conjunct composition remains available via the WHERE clause (multi-AND WHERE is already accepted today).

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 7: Reject OR in ON

**Files:**
- Modify: `query/sql/parser.go` (parseJoinClause — add lingering-OR check)
- Test: `query/sql/parser_test.go` (append `TestParseRejectsJoinOnFilterOr`)

- [ ] **Step 1: Write the failing test**

Append to `query/sql/parser_test.go`:

```go
func TestParseRejectsJoinOnFilterOr(t *testing.T) {
	_, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10 OR o.id > 0")
	if err == nil {
		t.Fatal("expected error for OR in ON filter")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
	if !strings.Contains(err.Error(), "OR not supported in JOIN ON") {
		t.Fatalf("err = %q, want substring 'OR not supported in JOIN ON'", err.Error())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```
rtk go test ./query/sql -run TestParseRejectsJoinOnFilterOr -count=1 -v
```

Expected: FAIL. The parser returns normally after the single-comparison; the trailing `OR` surfaces after `parseJoinClause`, and neither the WHERE-branch nor the EOF check matches the expected substring.

- [ ] **Step 3: Add the lingering-OR check in `parseJoinClause`**

In `query/sql/parser.go`, extend the block from Task 6 — after the lingering-AND check, insert the lingering-OR check:

```go
		if isKeywordToken(p.peek(), "AND") {
			return nil, nil, nil, p.unsupported("JOIN ON filter accepts at most one AND-conjunct")
		}
		if isKeywordToken(p.peek(), "OR") {
			return nil, nil, nil, p.unsupported("OR not supported in JOIN ON")
		}
		return jc, rightQualifiers, onPred, nil
```

- [ ] **Step 4: Run the test to verify it passes**

```
rtk go test ./query/sql -run TestParseRejectsJoinOnFilterOr -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```
rtk git add query/sql/parser.go query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
feat(sql/parser): reject OR in JOIN ON filter

Keeps the ON-suffix bounded to AND-only composition. OR in predicates remains available via the WHERE clause for the existing disjunction surface.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 8: Reject column-vs-column filter in ON

**Files:**
- Modify: `query/sql/parser.go` (parseJoinClause — type-assert on ComparisonPredicate)
- Test: `query/sql/parser_test.go` (append `TestParseRejectsJoinOnFilterColumnVsColumn`)

- [ ] **Step 1: Write the failing test**

Append to `query/sql/parser_test.go`:

```go
func TestParseRejectsJoinOnFilterColumnVsColumn(t *testing.T) {
	_, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.id = o.id")
	if err == nil {
		t.Fatal("expected error for column-vs-column ON filter")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
	if !strings.Contains(err.Error(), "JOIN ON filter must compare a column to a literal") {
		t.Fatalf("err = %q, want substring 'JOIN ON filter must compare a column to a literal'", err.Error())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```
rtk go test ./query/sql -run TestParseRejectsJoinOnFilterColumnVsColumn -count=1 -v
```

Expected: FAIL. `parseComparisonPredicate` currently returns a `ColumnComparisonPredicate` for `col = col`, the parser folds that into `stmt.Predicate`, and no error is emitted. (If the EOF check surfaces something else, the substring assertion still fails.)

- [ ] **Step 3: Add the type-assert after `parseComparisonPredicate`**

In `query/sql/parser.go`, inside `parseJoinClause`, after `parseComparisonPredicate` returns (right before the lingering-AND check from Task 6), insert:

```go
		if _, ok := onPred.(ComparisonPredicate); !ok {
			return nil, nil, nil, p.unsupported("JOIN ON filter must compare a column to a literal")
		}
```

The final structure of the AND-arm block is then:

```go
		p.advance()
		onBindings := relationBindings{requireQualify: true, byQualifier: lookup}
		onPred, err := p.parseComparisonPredicate(onBindings)
		if err != nil {
			return nil, nil, nil, err
		}
		if _, ok := onPred.(ComparisonPredicate); !ok {
			return nil, nil, nil, p.unsupported("JOIN ON filter must compare a column to a literal")
		}
		if isKeywordToken(p.peek(), "AND") {
			return nil, nil, nil, p.unsupported("JOIN ON filter accepts at most one AND-conjunct")
		}
		if isKeywordToken(p.peek(), "OR") {
			return nil, nil, nil, p.unsupported("OR not supported in JOIN ON")
		}
		return jc, rightQualifiers, onPred, nil
```

- [ ] **Step 4: Run the test to verify it passes**

```
rtk go test ./query/sql -run TestParseRejectsJoinOnFilterColumnVsColumn -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Run the full parser suite to ensure other acceptances (including Task 3/Task 4) still pass**

```
rtk go test ./query/sql -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```
rtk git add query/sql/parser.go query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
feat(sql/parser): reject column-vs-column comparisons in JOIN ON filter

Type-asserts the ON-filter is a literal comparison (ComparisonPredicate). Column-vs-column is reserved for the join's own equality slot.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 9: Sanity pins — existing qualifier errors still fire in the ON-filter path

**Files:**
- Test: `query/sql/parser_test.go` (append two tests)

These pin pre-existing error paths through the new ON-filter surface. No new implementation.

- [ ] **Step 1: Write the unqualified-column rejection test**

Append to `query/sql/parser_test.go`:

```go
func TestParseRejectsJoinOnFilterUnqualifiedColumn(t *testing.T) {
	_, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND quantity < 10")
	if err == nil {
		t.Fatal("expected error for unqualified column in ON filter")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}
```

- [ ] **Step 2: Write the third-relation qualifier rejection test**

Append to `query/sql/parser_test.go`:

```go
func TestParseRejectsJoinOnFilterThirdRelation(t *testing.T) {
	_, err := Parse("SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND z.x < 10")
	if err == nil {
		t.Fatal("expected error for third-relation qualifier in ON filter")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}
```

- [ ] **Step 3: Run both tests**

```
rtk go test ./query/sql -run 'TestParseRejectsJoinOnFilterUnqualifiedColumn|TestParseRejectsJoinOnFilterThirdRelation' -count=1 -v
```

Expected: PASS for both. The first hits `parseColumnRefForPredicate`'s existing "join WHERE columns must be qualified" path; the second hits the existing qualifier-resolution error from `resolveQualifier`. Both paths are reached because `parseComparisonPredicate` uses the same helpers as the WHERE-form.

- [ ] **Step 4: Commit**

```
rtk git add query/sql/parser_test.go
rtk git commit -m "$(cat <<'EOF'
test(sql/parser): pin unqualified-column and third-relation rejections in JOIN ON filter

Sanity pins that the existing parseColumnRefForPredicate / resolveQualifier error paths fire through the new ON-filter surface. No new implementation.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 10: One-off end-to-end — ON-filter restricts the returned row set

**Files:**
- Test: `protocol/handle_oneoff_test.go` (append `TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows`)

- [ ] **Step 1: Write the test**

Append to `protocol/handle_oneoff_test.go`:

```go
func TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows(t *testing.T) {
	conn := testConnDirect(nil)
	ordersTS := &schema.TableSchema{
		ID:   1,
		Name: "Orders",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "product_id", Type: schema.KindUint32},
		},
	}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "Inventory",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "quantity", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	ordersTS.ID = ordersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			ordersReg.ID: {
				{types.NewUint32(1), types.NewUint32(100)},
				{types.NewUint32(2), types.NewUint32(101)},
				{types.NewUint32(3), types.NewUint32(102)},
			},
			inventoryReg.ID: {
				{types.NewUint32(100), types.NewUint32(9)},
				{types.NewUint32(101), types.NewUint32(10)},
				{types.NewUint32(102), types.NewUint32(3)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1e},
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (ON-filter is query-only accepted)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2 (orders 1 and 3 match the quantity<10 filter)", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected order ids returned: %v, %v (want 1 and 3)", pvs[0][0], pvs[1][0])
	}
}
```

- [ ] **Step 2: Run the test**

```
rtk go test ./protocol -run TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows -count=1 -v
```

Expected: PASS. The query seeds three orders and three inventory rows; orders 1 and 3 point at inventory rows with `quantity < 10` (9 and 3), order 2 points at quantity 10 which is excluded. Mirrors the existing WHERE-form test at `handle_oneoff_test.go:1369`.

- [ ] **Step 3: Commit**

```
rtk git add protocol/handle_oneoff_test.go
rtk git commit -m "$(cat <<'EOF'
test(protocol/oneoff): pin JOIN ON AND-filter returns the correctly filtered row set

Uses the exact handoff query shape and verifies the row set matches the filter (orders 1 and 3 via quantity<10, order 2 excluded). No implementation change — the ON-filter flows through the existing WHERE-filter compile path.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 11: One-off equivalence — ON-form row set equals WHERE-form row set

**Files:**
- Test: `protocol/handle_oneoff_test.go` (append `TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm`)

- [ ] **Step 1: Write the test**

Append to `protocol/handle_oneoff_test.go`:

```go
func TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm(t *testing.T) {
	buildSnap := func() (*mockSnapshot, *schema.TableSchema, schema.SchemaRegistry) {
		b := schema.NewBuilder().SchemaVersion(1)
		b.TableDef(schema.TableDefinition{
			Name: "Orders",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
				{Name: "product_id", Type: schema.KindUint32},
			},
		})
		b.TableDef(schema.TableDefinition{
			Name: "Inventory",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
				{Name: "quantity", Type: schema.KindUint32},
			},
		})
		eng, err := b.Build(schema.EngineOptions{})
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		_, ordersReg, ok := eng.Registry().TableByName("Orders")
		if !ok {
			t.Fatal("Orders table missing from registry")
		}
		_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
		if !ok {
			t.Fatal("Inventory table missing from registry")
		}
		ordersTS := &schema.TableSchema{ID: ordersReg.ID, Name: "Orders", Columns: ordersReg.Columns}
		snap := &mockSnapshot{
			rows: map[schema.TableID][]types.ProductValue{
				ordersReg.ID: {
					{types.NewUint32(1), types.NewUint32(100)},
					{types.NewUint32(2), types.NewUint32(101)},
					{types.NewUint32(3), types.NewUint32(102)},
				},
				inventoryReg.ID: {
					{types.NewUint32(100), types.NewUint32(9)},
					{types.NewUint32(101), types.NewUint32(10)},
					{types.NewUint32(102), types.NewUint32(3)},
				},
			},
		}
		return snap, ordersTS, eng.Registry()
	}

	runQuery := func(q string, id byte) []types.ProductValue {
		conn := testConnDirect(nil)
		snap, ordersTS, reg := buildSnap()
		sl := registrySchemaLookup{reg: reg}
		stateAccess := &mockStateAccess{snap: snap}
		msg := &OneOffQueryMsg{MessageID: []byte{id}, QueryString: q}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q", q, *result.Error)
		}
		return decodeRows(t, firstTableRows(result), ordersTS)
	}

	onRows := runQuery("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10", 0x20)
	whereRows := runQuery("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10", 0x21)

	if len(onRows) != len(whereRows) {
		t.Fatalf("row count diverges: ON=%d, WHERE=%d", len(onRows), len(whereRows))
	}
	for i := range onRows {
		if len(onRows[i]) != len(whereRows[i]) {
			t.Fatalf("row %d column count diverges: ON=%d, WHERE=%d", i, len(onRows[i]), len(whereRows[i]))
		}
		for j := range onRows[i] {
			if !onRows[i][j].Equal(whereRows[i][j]) {
				t.Fatalf("row %d col %d diverges: ON=%v, WHERE=%v", i, j, onRows[i][j], whereRows[i][j])
			}
		}
	}
}
```

- [ ] **Step 2: Run the test**

```
rtk go test ./protocol -run TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm -count=1 -v
```

Expected: PASS. Both forms produce the same two-row result set over identical seed data.

- [ ] **Step 3: Commit**

```
rtk git add protocol/handle_oneoff_test.go
rtk git commit -m "$(cat <<'EOF'
test(protocol/oneoff): pin ON-form row set equals WHERE-form row set end-to-end

Runs the ON-form and the equivalent WHERE-form against identically seeded tables and asserts the projected row sets are equal. Locks the transparent-fold invariant at the runtime layer.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 12: Subscribe acceptance — ON-filter registers and exposes the filter in the predicate tree

**Files:**
- Test: `protocol/handle_subscribe_test.go` (append `TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted`)

- [ ] **Step 1: Write the test**

Append to `protocol/handle_subscribe_test.go`:

```go
// TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted pins the
// subscribe-side acceptance of the new ON-filter shape. Subscribe accepts
// because the parser transparently folds the ON-extracted filter into
// Statement.Predicate, producing output indistinguishable from the already-
// accepted WHERE-form (see design
// docs/superpowers/specs/2026-04-23-join-on-filter-widening-design.md §
// "Divergence-discipline framing"). Mirrors the WHERE-form pin at
// TestHandleSubscribeSingle_JoinFilterOnRightTable.
func TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "Inventory",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "quantity", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   18,
		QueryID:     15,
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	joinPred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	_, orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventory, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	if joinPred.Left != orders.ID || joinPred.Right != inventory.ID {
		t.Fatalf("join tables = %d/%d, want %d/%d", joinPred.Left, joinPred.Right, orders.ID, inventory.ID)
	}
	rng, ok := joinPred.Filter.(subscription.ColRange)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColRange", joinPred.Filter)
	}
	if rng.Table != inventory.ID || rng.Column != 1 {
		t.Fatalf("range table/column = %d/%d, want %d/1", rng.Table, rng.Column, inventory.ID)
	}
	if !rng.Upper.Value.Equal(types.NewUint32(10)) || rng.Upper.Inclusive || rng.Upper.Unbounded {
		t.Fatalf("upper bound = %+v, want exclusive bounded 10", rng.Upper)
	}
}
```

- [ ] **Step 2: Run the test**

```
rtk go test ./protocol -run TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted -count=1 -v
```

Expected: PASS. The subscribe registration receives a `subscription.Join` with the ON-extracted filter as a `ColRange` on `Inventory.quantity < 10` (exclusive upper bound at 10).

If FAIL with a SubscriptionError: verify the schema has a primary key on `Inventory.id` (provides the join index); without it, the unindexed-join gate fires first and the test never reaches the ColRange assertion.

- [ ] **Step 3: Commit**

```
rtk git add protocol/handle_subscribe_test.go
rtk git commit -m "$(cat <<'EOF'
test(protocol/subscribe): pin subscribe-side acceptance of JOIN ON AND-filter

Mirrors the WHERE-form pin at TestHandleSubscribeSingle_JoinFilterOnRightTable using the ON-form query shape. Notable: this is the first OI-002 slice since P0-SUBSCRIPTION-018 where subscribe's acceptance surface widens alongside one-off; the justification is that the ON-form and WHERE-form produce indistinguishable parser output (see design doc § "Divergence-discipline framing").

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 13: Subscribe — unindexed-join rejection still fires regardless of ON-filter presence

**Files:**
- Test: `protocol/handle_subscribe_test.go` (append `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected`)

- [ ] **Step 1: Write the test**

Append to `protocol/handle_subscribe_test.go`:

```go
// TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected pins
// that the subscription unindexed-join gate (validate.go:170) is independent
// of filter presence. The shape admitted by P0-SUBSCRIPTION-027 does not
// weaken the index requirement; it only opens a parser-level surface.
func TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "Inventory",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "quantity", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}
	executor := &validatingSubExecutor{schema: sl}

	msg := &SubscribeSingleMsg{
		RequestID:   19,
		QueryID:     52,
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 52, "SubscriptionError.QueryID")
	requireOptionalUint32(t, se.RequestID, 19, "SubscriptionError.RequestID")
	if !strings.Contains(se.Error, "join column has no index on either side") {
		t.Fatalf("Error = %q, want subscription unindexed-join rejection", se.Error)
	}
}
```

- [ ] **Step 2: Run the test**

```
rtk go test ./protocol -run TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected -count=1 -v
```

Expected: PASS. Inventory here has NO primary key and no index on `id`; Orders.product_id also has no index; the join equality is unindexed on both sides and `validateJoin` at `subscription/validate.go:170` emits `ErrUnindexedJoin`. The ON-filter presence is independent of this rejection.

- [ ] **Step 3: Commit**

```
rtk git add protocol/handle_subscribe_test.go
rtk git commit -m "$(cat <<'EOF'
test(protocol/subscribe): pin unindexed-join rejection independence from ON-filter presence

The subscription unindexed-join gate fires regardless of whether the predicate carries a filter. P0-SUBSCRIPTION-027 only opens the parser surface; it does not weaken the index requirement.

Part of P0-SUBSCRIPTION-027.
EOF
)"
```

---

## Task 14: Documentation updates — TECH-DEBT, parity ledger, next-session handoff

**Files:**
- Modify: `TECH-DEBT.md:59-74`
- Modify: `docs/parity-phase0-ledger.md:61`, `:99`
- Modify: `NEXT_SESSION_HANDOFF.md:38-92`

- [ ] **Step 1: Update `TECH-DEBT.md` OI-002 — extend the A2-closures sentence**

Open `TECH-DEBT.md`. Locate line 59, which ends with:

```
... and one-off-only mixed-relation explicit join-column projections with subscribe-side rejection retained
```

Change the sentence's tail (still on line 59, before the period) to append:

```
..., and one-off/subscribe JOIN ON equality-plus-single-relation-filter widening as a transparent parser admission
```

The full sentence reads `one-off-only ... mixed-relation explicit join-column projections ... retained, and one-off/subscribe JOIN ON equality-plus-single-relation-filter widening as a transparent parser admission`.

- [ ] **Step 2: Add the new bullet after the mixed-relation projection bullet**

In `TECH-DEBT.md`, locate the bullet ending at line 65 (the mixed-relation explicit column projection bullet). Immediately after that bullet, insert:

```
- one-off and subscribe SQL now also accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface (e.g., `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`); the parser transparently folds the ON-extracted filter into `Statement.Predicate`, producing identical output to the semantically equivalent WHERE-form, so subscribe accepts as a direct consequence of admitting the parser shape, while multi-conjunct / OR / column-vs-column / unqualified / third-relation / three-way-join rejections remain in place
```

- [ ] **Step 3: Append new test function names to the authoritative-pins paragraph**

In `TECH-DEBT.md`, locate the long single-line paragraph at line 66 that begins with "authoritative pins for the latest A2 query-only closures now include ...". Before the final period, insert (keeping comma-separation consistent):

```
, `TestParseJoinOnEqualityWithFilter`, `TestParseJoinOnEqualityWithFilterOnLeftSide`, `TestParseJoinOnEqualityWithFilterAndWhere`, `TestParseJoinOnEqualityParityWithWhereForm`, `TestParseRejectsJoinOnFilterMultipleConjuncts`, `TestParseRejectsJoinOnFilterOr`, `TestParseRejectsJoinOnFilterColumnVsColumn`, `TestParseRejectsJoinOnFilterUnqualifiedColumn`, `TestParseRejectsJoinOnFilterThirdRelation`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected`
```

- [ ] **Step 4: Update the execution note at line 73**

In `TECH-DEBT.md`, find the line that currently reads:

```
- the join-backed `COUNT(*) [AS] alias` one-off query-only slice is now closed and pinned while subscribe-side aggregate rejection is retained
```

Replace with:

```
- the JOIN ON equality-plus-single-relation-filter widening slice is now closed and pinned; subscribe acceptance is treated as a transparent parser admission rather than a one-off-only divergence because the ON-form and WHERE-form produce indistinguishable parser output
```

- [ ] **Step 5: Add the `P0-SUBSCRIPTION-027` row to the parity ledger**

Open `docs/parity-phase0-ledger.md`. Locate line 61 (the `P0-SUBSCRIPTION-026` row). Immediately after it, insert one new table row:

```
| `P0-SUBSCRIPTION-027` one-off/subscribe JOIN ON equality-plus-single-relation-filter widening | `closed` | `query/sql/parser_test.go`, `protocol/handle_oneoff_test.go`, `protocol/handle_subscribe_test.go`, `query/sql/parser.go` | One-off/ad hoc and subscription SQL now accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface; the parser folds the ON-extracted filter into `Statement.Predicate` identically to the semantically equivalent WHERE-form (verified by parser-level parity pin), so subscribe acceptance is a direct side-effect of admitting the parser shape rather than a new executor/fanout surface. Multi-conjunct, OR, column-vs-column, unqualified, third-relation, and three-way-join rejections remain pinned; unindexed-join rejection still fires for subscriptions regardless of filter presence. |
```

- [ ] **Step 6: Append to the broad-themes bullet at `docs/parity-phase0-ledger.md:99`**

In the broad-themes paragraph at line 99 (the long `broader query/subscription parity beyond the narrow landed shapes (now after ... , and one-off-only mixed-relation explicit join-column projection support with subscribe-side rejection retained)`), before the closing `)`, append:

```
, and one-off/subscribe JOIN ON equality-plus-single-relation-filter widening admitted via transparent parser fold rather than subscribe-side widening
```

- [ ] **Step 7: Update `NEXT_SESSION_HANDOFF.md` — the "Latest closed" block**

Open `NEXT_SESSION_HANDOFF.md`. Replace the block from line 38 through line 55 (inclusive of the `P0-SUBSCRIPTION-026` reference and the five "Behavior now pinned" bullets) with the block below. Note that the replacement contains a nested ```` ```sql ```` fence — the four-backtick outer fence here keeps the plan file parseable; the text to actually paste into the handoff is everything *between* the four-backtick fences (i.e., starting at `Latest closed OI-002 query-only slice:` and ending at `- unindexed-join rejection for subscriptions remains independent of filter presence`, including the inner ```` ```sql ... ``` ```` fence as literal text):

````
Latest closed OI-002 query-only slice:

- `P0-SUBSCRIPTION-027`: one-off and subscription SQL now accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface.

Representative accepted shape:

```sql
SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10
```

Behavior now pinned:

- parser admits ON-equality plus exactly one qualified-column/literal filter; multi-conjunct, OR, column-vs-column, unqualified-column, and third-relation qualifiers remain rejected at the parser gate
- the parser transparently folds the ON-extracted filter into `Statement.Predicate`, producing output structurally identical to the semantically equivalent WHERE-form (verified by `TestParseJoinOnEqualityParityWithWhereForm`)
- one-off/ad hoc returns correctly filtered rows end-to-end (mirrors the existing WHERE-form pin)
- subscribe-side accepts the new shape transparently via the existing WHERE-filter compile path; this is the first OI-002 slice since `P0-SUBSCRIPTION-018` where subscribe widens alongside one-off, justified by the ON↔WHERE parser-level parity
- unindexed-join rejection for subscriptions remains independent of filter presence
````

- [ ] **Step 8: Update `NEXT_SESSION_HANDOFF.md` — Primary files touched**

Replace the existing "Primary files touched by the latest OI-002 work:" list (lines 57-66) with:

```
Primary files touched by the latest OI-002 work:

- `query/sql/parser.go`
- `query/sql/parser_test.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`
```

- [ ] **Step 9: Update `NEXT_SESSION_HANDOFF.md` — Latest validation reported**

Replace the validation command list (lines 68-72) with:

```
Latest validation reported for that slice:

- `rtk go test ./query/sql ./protocol -run 'JoinOnEquality.*Filter|ParseRejectsJoinOnFilter' -count=1 -v`
- `rtk go test ./query/sql ./protocol -count=1`
- `rtk go test ./query/sql ./protocol ./subscription -count=1`
```

- [ ] **Step 10: Update `NEXT_SESSION_HANDOFF.md` — Good next OI-002 candidates**

Replace the numbered candidate list (lines 76-92 — the "1. One-off JOIN ON predicate widening..." through "3. Row-level security..." block) with:

```
Choose from fresh live evidence. The next bounded candidate should be chosen by scouting live code/docs/tests after the `P0-SUBSCRIPTION-027` landing; do not carry forward older candidate notes without re-verification.

Candidates carried forward from prior handoffs:

1. Runtime/fanout lanes.
   - QueryID-level fanout correlation / SubscriptionID wire cleanup.
   - Confirmed-read durability gating for `SubscriptionError`.
   - Deterministic per-connection update ordering.

2. Row-level security / per-client filtering.
   - This remains real but is too large to mix with a narrow SQL slice unless the user explicitly requests that broader work.

3. A TBD parser/compile seam continuation, to be chosen from fresh scout — for example, additional bounded widenings that can be admitted via transparent parser-level parity against an already-accepted shape (use this route only when the parity claim is exactly as tight as P0-SUBSCRIPTION-027's).
```

- [ ] **Step 11: Add the framing paragraph under "Latest OI-002 state to preserve"**

In `NEXT_SESSION_HANDOFF.md`, find the paragraph that currently reads (near line 36):

```
Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-026 rows without new failing regression evidence.
```

Change `P0-SUBSCRIPTION-026` to `P0-SUBSCRIPTION-027`, and immediately after this sentence, append on a new paragraph:

```
`P0-SUBSCRIPTION-027` is the first OI-002 slice since `P0-SUBSCRIPTION-018` where subscribe's acceptance surface widens alongside one-off. The justification is that the ON-form is syntactically new but semantically identical to the already-accepted WHERE-form — the parser produces indistinguishable output for either. Future slices should default back to the one-off-only pattern; use this precedent only when the new shape has a pinned parser-level parity claim against an already-accepted form.
```

- [ ] **Step 12: Commit**

```
rtk git add TECH-DEBT.md docs/parity-phase0-ledger.md NEXT_SESSION_HANDOFF.md
rtk git commit -m "$(cat <<'EOF'
docs: close P0-SUBSCRIPTION-027 JOIN ON equality + single-relation filter widening

Marks the closure in TECH-DEBT.md OI-002, adds the ledger row in docs/parity-phase0-ledger.md, and updates NEXT_SESSION_HANDOFF.md with the new "Latest closed" block, touched-files list, validation commands, forward candidates, and the framing paragraph justifying subscribe-side acceptance as a transparent parser admission.
EOF
)"
```

---

## Task 15: Final validation — full-suite, vet, fmt

**Files:** none (verification only; commit only if fixes are required)

- [ ] **Step 1: Run the scoped regex suite to confirm all new tests pass**

```
rtk go test ./query/sql ./protocol -run 'JoinOnEquality.*Filter|ParseRejectsJoinOnFilter' -count=1 -v
```

Expected: all 12 named tests PASS (5 parser acceptances, 4 parser rejections, 2 one-off, 2 subscribe — one of the parser rejections `TestParseRejectsJoinOnFilterUnqualifiedColumn` is in the suite, and `TestParseRejectsJoinOnFilterThirdRelation` is also covered).

- [ ] **Step 2: Run the touched packages in full**

```
rtk go test ./query/sql ./protocol -count=1
```

Expected: PASS. Catches any regression in existing parser/subscribe tests from the signature change or fold.

- [ ] **Step 3: Run the extended package set including subscription**

```
rtk go test ./query/sql ./protocol ./subscription -count=1
```

Expected: PASS. The `subscription` package has no expected edits — this run proves the validateJoin filter path is still exercised correctly by the existing WHERE-form subscribe tests.

- [ ] **Step 4: Run vet**

```
rtk go vet ./query/sql ./protocol
```

Expected: no output.

- [ ] **Step 5: Run fmt**

```
rtk go fmt ./query/sql ./protocol
```

Expected: no output (no files reformatted). If fmt rewrites a file, inspect the diff; if the change is a trivial whitespace normalization of code you edited, stage and commit it:

```
rtk git add -u
rtk git commit -m "style: gofmt pass after P0-SUBSCRIPTION-027 edits"
```

- [ ] **Step 6: Final parity cross-check — full-tree test run (time-permitting)**

```
rtk go test ./... -count=1
```

Expected: PASS across the whole repo. If a package outside `query/sql` and `protocol` fails, the B premise is broken — do not paper over. Re-read the design spec's "transparent fold" section and escalate to the user.

---

## Spec coverage check

| Spec section | Implementing task(s) |
|---|---|
| Goal / representative shape | Tasks 2, 10 |
| Why parser-only change | Tasks 12, 13 (subscribe accept + unindexed-rejection independence) |
| Divergence-discipline framing | Tasks 5, 12, 14 (parity pin, subscribe-accept pin, handoff framing paragraph) |
| In-scope — either-side filter | Tasks 2, 3 |
| In-scope — comparison operators/literals via parseComparisonPredicate | Task 2 (reuses parseComparisonPredicate) |
| In-scope — composition with WHERE | Task 4 |
| Out-of-scope — multiple AND conjuncts | Task 6 |
| Out-of-scope — OR in ON | Task 7 |
| Out-of-scope — column-vs-column filter | Task 8 |
| Out-of-scope — non-equality primary ON | already rejected today, unchanged |
| Out-of-scope — three-way joins | already rejected today, unchanged |
| Grammar addition | Tasks 2, 6, 7, 8 |
| Acceptance cases | Tasks 2, 3, 4 |
| Rejection cases | Tasks 6, 7, 8, 9 |
| Code change — parseJoinClause signature | Task 1 |
| Code change — AND-filter arm in parseJoinClause | Tasks 2, 6, 7, 8 (layered) |
| Code change — fold in parseStatement | Task 2 |
| Semantic-equivalence invariant | Task 5 (parser), Task 11 (runtime) |
| Data-flow (zero downstream edits) | Tasks 1 Step 3, Task 2 Step 6, Task 15 |
| Parser pins (all 9 tests) | Tasks 2, 3, 4, 5, 6, 7, 8, 9 |
| One-off pins (2 tests) | Tasks 10, 11 |
| Subscribe pins (2 tests) | Tasks 12, 13 |
| Tests explicitly not added | respected by absence |
| TECH-DEBT.md updates | Task 14 Steps 1-4 |
| parity-phase0-ledger.md updates | Task 14 Steps 5-6 |
| NEXT_SESSION_HANDOFF.md updates | Task 14 Steps 7-11 |
| Validation plan | Task 15 |
| Risks — canonicalization drift | Task 5 (parity pin is the mitigation) |
| Risks — signature change blast radius | Task 1 (scaffolding commit exercises it) |
| Risks — unindexed-join divergence | Task 13 |

No gaps.
