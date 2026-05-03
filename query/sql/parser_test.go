package sql

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseSelectAll(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "users" {
		t.Fatalf("Table = %q, want %q", stmt.Table, "users")
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want none", stmt.Filters)
	}
}

func TestParseSelectAllTrailingSemicolonAllowed(t *testing.T) {
	if _, err := Parse("SELECT * FROM users;"); err != nil {
		t.Fatalf("trailing semicolon should be accepted: %v", err)
	}
}

func TestParseSelectQualifiedStarWithAlias(t *testing.T) {
	stmt, err := Parse("SELECT item.* FROM Inventory item")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Inventory" {
		t.Fatalf("Table = %q, want Inventory", stmt.Table)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want none", stmt.Filters)
	}
}

func TestParseSelectQualifiedStarWithSetQuantifierSpelledTable(t *testing.T) {
	stmt, err := Parse("SELECT All.* FROM All")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "All" || stmt.ProjectedTable != "All" || stmt.ProjectedAlias != "All" {
		t.Fatalf("statement = %+v, want table/projected table/projected alias All", stmt)
	}
}

func TestParseSelectAllSetQuantifierStillRejected(t *testing.T) {
	_, err := Parse("SELECT ALL * FROM users")
	var unsupported UnsupportedSelectError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedSelectError, got %T (%v)", err, err)
	}
}

func TestParseSelectQualifiedStarWithAsAliasAndQualifiedWhereColumns(t *testing.T) {
	stmt, err := Parse("SELECT item.* FROM Inventory AS item WHERE item.id = 7 AND item.active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Inventory" {
		t.Fatalf("Table = %q, want Inventory", stmt.Table)
	}
	if len(stmt.Filters) != 2 {
		t.Fatalf("Filters len = %d, want 2", len(stmt.Filters))
	}
	if stmt.Filters[0].Column != "id" {
		t.Fatalf("first column = %q, want id", stmt.Filters[0].Column)
	}
	if stmt.Filters[1].Column != "active" {
		t.Fatalf("second column = %q, want active", stmt.Filters[1].Column)
	}
}

func TestParseWhereHarmlessParenthesizationMetamorphic(t *testing.T) {
	const seed = uint64(0x5a17cafe)
	cases := []struct {
		name string
		base string
		vars []string
	}{
		{
			name: "and",
			base: "SELECT * FROM users WHERE id = 42 AND active = TRUE",
			vars: []string{
				"SELECT * FROM users WHERE (id = 42) AND ((active = TRUE))",
				"SELECT * FROM users WHERE ((id = 42 AND active = TRUE))",
			},
		},
		{
			name: "or",
			base: "SELECT * FROM users WHERE id = 42 OR active = FALSE",
			vars: []string{
				"SELECT * FROM users WHERE (id = 42) OR ((active = FALSE))",
				"SELECT * FROM users WHERE ((id = 42 OR active = FALSE))",
			},
		},
	}
	for caseIndex, tc := range cases {
		assertParseVariantsEquivalent(t, seed, caseIndex, tc.name, tc.base, tc.vars)
	}
}

func TestParseWhitespaceMetamorphic(t *testing.T) {
	const seed = uint64(0x571c0de)
	cases := []struct {
		name string
		base string
		vars []string
	}{
		{
			name: "single-table",
			base: "SELECT item.* FROM Inventory AS item WHERE item.id = 7 AND item.active = TRUE LIMIT 5",
			vars: []string{
				"\tSELECT\nitem.*\r\nFROM\tInventory\nAS item\nWHERE\titem.id\n=\t7\nAND\nitem.active = TRUE\nLIMIT\t5",
				"SELECT item.* FROM Inventory AS item\nWHERE\n(item.id = 7)\nAND\n(item.active = TRUE)\nLIMIT 5;",
			},
		},
		{
			name: "join-projection",
			base: "SELECT o.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id",
			vars: []string{
				"SELECT\no.id,\nproduct.quantity\nFROM Orders\to\nJOIN\nInventory product\nON\no.product_id\n=\nproduct.id",
				"SELECT o.id , product.quantity FROM Orders o\tJOIN Inventory product ON o.product_id = product.id;",
			},
		},
	}
	for caseIndex, tc := range cases {
		assertParseVariantsEquivalent(t, seed, caseIndex, tc.name, tc.base, tc.vars)
	}
}

func TestParseOptionalSyntaxMetamorphic(t *testing.T) {
	const seed = uint64(0xa50f1a5)
	cases := []struct {
		name string
		base string
		vars []string
	}{
		{
			name: "table-alias-as",
			base: "SELECT msg.* FROM messages AS msg WHERE msg.id = 7 AND msg.active = TRUE LIMIT 3",
			vars: []string{
				"SELECT msg.* FROM messages msg WHERE msg.id = 7 AND msg.active = TRUE LIMIT 3",
			},
		},
		{
			name: "projection-output-as",
			base: "SELECT body AS text FROM messages WHERE id = 7",
			vars: []string{
				"SELECT body text FROM messages WHERE id = 7",
			},
		},
		{
			name: "aggregate-output-as",
			base: "SELECT COUNT(*) AS n FROM messages WHERE active = TRUE LIMIT 9",
			vars: []string{
				"SELECT COUNT(*) n FROM messages WHERE active = TRUE LIMIT 9",
			},
		},
		{
			name: "join-alias-as",
			base: "SELECT o.id FROM orders AS o JOIN users AS u ON o.user_id = u.id WHERE u.active = TRUE",
			vars: []string{
				"SELECT o.id FROM orders o JOIN users u ON o.user_id = u.id WHERE u.active = TRUE",
			},
		},
		{
			name: "inner-join-keyword",
			base: "SELECT o.id FROM orders AS o JOIN users AS u ON o.user_id = u.id WHERE u.active = TRUE",
			vars: []string{
				"SELECT o.id FROM orders AS o INNER JOIN users AS u ON o.user_id = u.id WHERE u.active = TRUE",
			},
		},
	}
	for caseIndex, tc := range cases {
		assertParseVariantsEquivalent(t, seed, caseIndex, tc.name, tc.base, tc.vars)
	}
}

func assertParseVariantsEquivalent(t *testing.T, seed uint64, caseIndex int, caseName, base string, variants []string) {
	t.Helper()
	baseline, err := Parse(base)
	if err != nil {
		t.Fatalf("seed=%#x op_index=%d case=%d runtime_config=case=%s operation=Parse(base) observed_error=%v expected=nil sql=%q",
			seed, caseIndex*10, caseIndex, caseName, err, base)
	}
	for variantIndex, variant := range variants {
		got, err := Parse(variant)
		opIndex := caseIndex*10 + variantIndex + 1
		if err != nil {
			t.Fatalf("seed=%#x op_index=%d case=%d variant=%d runtime_config=case=%s operation=Parse(variant) observed_error=%v expected=nil sql=%q",
				seed, opIndex, caseIndex, variantIndex, caseName, err, variant)
		}
		if !reflect.DeepEqual(got, baseline) {
			t.Fatalf("seed=%#x op_index=%d case=%d variant=%d runtime_config=case=%s operation=compare-parse-tree observed=%#v expected=%#v",
				seed, opIndex, caseIndex, variantIndex, caseName, got, baseline)
		}
	}
}

func TestParseWhereSingleUint(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users WHERE id = 42")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
	f := stmt.Filters[0]
	if f.Column != "id" {
		t.Fatalf("Column = %q, want id", f.Column)
	}
	if f.Literal.Kind != LitInt {
		t.Fatalf("Literal.Kind = %v, want LitInt", f.Literal.Kind)
	}
	if f.Literal.Int != 42 {
		t.Fatalf("Literal.Int = %d, want 42", f.Literal.Int)
	}
}

func TestParseWhereNegativeInt(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = -7")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Int != -7 {
		t.Fatalf("got %d, want -7", stmt.Filters[0].Literal.Int)
	}
}

// TestParseWhereLeadingPlusInt pins the reference valid-literal shape at
// reference/SpacetimeDB/crates/expr/src/check.rs:297-300 (`select * from t
// where u32 = +1` / "Leading `+`"): a leading `+` sign on an integer literal
// is accepted and behaves identically to the unsigned form. Mirrors the
// existing leading `-` support exercised by TestParseWhereNegativeInt.
func TestParseWhereLeadingPlusInt(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = +7")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Kind != LitInt {
		t.Fatalf("Literal.Kind = %v, want LitInt", stmt.Filters[0].Literal.Kind)
	}
	if stmt.Filters[0].Literal.Int != 7 {
		t.Fatalf("got %d, want 7", stmt.Filters[0].Literal.Int)
	}
}

// TestParseWhereScientificNotationUnsignedInteger pins the reference
// valid-literal shape at reference/SpacetimeDB/crates/expr/src/check.rs:302-
// 304 (`select * from t where u32 = 1e3` / "Scientific notation"): an
// exponent-form numeric that evaluates to an integer value must parse as
// LitInt so the coerce boundary can bind it to an integer column.
func TestParseWhereScientificNotationUnsignedInteger(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = 1e3")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Kind != LitInt {
		t.Fatalf("Literal.Kind = %v, want LitInt", stmt.Filters[0].Literal.Kind)
	}
	if stmt.Filters[0].Literal.Int != 1000 {
		t.Fatalf("got %d, want 1000", stmt.Filters[0].Literal.Int)
	}
}

// TestParseWhereScientificNotationCaseInsensitive pins
// reference/SpacetimeDB/crates/expr/src/check.rs:306-308 (`select * from t
// where u32 = 1E3` / "Case insensitive scientific notation"): uppercase `E`
// is accepted identically to lowercase.
func TestParseWhereScientificNotationCaseInsensitive(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = 1E3")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Kind != LitInt {
		t.Fatalf("Literal.Kind = %v, want LitInt", stmt.Filters[0].Literal.Kind)
	}
	if stmt.Filters[0].Literal.Int != 1000 {
		t.Fatalf("got %d, want 1000", stmt.Filters[0].Literal.Int)
	}
}

// TestParseWhereScientificNotationNegativeExponent pins
// reference/SpacetimeDB/crates/expr/src/check.rs:314-316 (`select * from t
// where f32 = 1e-3` / "Negative exponent"): a non-integral exponent-form
// numeric parses as LitFloat so the coerce boundary can bind it to a
// float column.
func TestParseWhereScientificNotationNegativeExponent(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = 1e-3")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Kind != LitFloat {
		t.Fatalf("Literal.Kind = %v, want LitFloat", stmt.Filters[0].Literal.Kind)
	}
	if stmt.Filters[0].Literal.Float != 1e-3 {
		t.Fatalf("got %g, want 1e-3", stmt.Filters[0].Literal.Float)
	}
}

// TestParseWhereLeadingDotFloat pins reference/SpacetimeDB/crates/expr/src/
// check.rs:322-324 (`select * from t where f32 = .1` / "Leading `.`"): a
// leading-dot numeric with no integer part parses as LitFloat.
func TestParseWhereLeadingDotFloat(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = .1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Kind != LitFloat {
		t.Fatalf("Literal.Kind = %v, want LitFloat", stmt.Filters[0].Literal.Kind)
	}
	if stmt.Filters[0].Literal.Float != 0.1 {
		t.Fatalf("got %g, want 0.1", stmt.Filters[0].Literal.Float)
	}
}

// TestParseWhereScientificNotationOverflowBigInt pins
// reference/SpacetimeDB/crates/expr/src/check.rs:326-332 (`select * from t
// where f32 = 1e40` / "Infinity" and `select * from t where u256 = 1e40` /
// "u256"): an integer-valued exponent-form numeric whose magnitude exceeds
// int64 must parse as LitBigInt so the coerce boundary can bind it to a
// 256-bit integer column (via exact BigInt specs) or to a float
// column (via big.Float → f64, which rounds to +Inf on f32). Matches the
// reference BigDecimal is_integer path in
// crates/expr/src/lib.rs::parse_int.
func TestParseWhereScientificNotationOverflowBigInt(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = 1e40")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	lit := stmt.Filters[0].Literal
	if lit.Kind != LitBigInt {
		t.Fatalf("Literal.Kind = %v, want LitBigInt", lit.Kind)
	}
	if lit.Big == nil {
		t.Fatal("Literal.Big = nil, want *big.Int(10^40)")
	}
	want := "10000000000000000000000000000000000000000"
	if got := lit.Big.String(); got != want {
		t.Fatalf("Literal.Big = %s, want %s", got, want)
	}
}

// TestParseWhereIntegerOverflowPromotesToBigInt pins the plain-integer
// overflow path: an integer literal too wide for int64 (no fractional or
// exponent part) promotes to LitBigInt rather than erroring. Supports the
// reference BigDecimal integer literal grammar for wide-column bindings.
func TestParseWhereIntegerOverflowPromotesToBigInt(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE n = 99999999999999999999")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	lit := stmt.Filters[0].Literal
	if lit.Kind != LitBigInt {
		t.Fatalf("Literal.Kind = %v, want LitBigInt", lit.Kind)
	}
	if got := lit.Big.String(); got != "99999999999999999999" {
		t.Fatalf("Literal.Big = %s, want 99999999999999999999", got)
	}
}

// TestParseWhereTrailingDotRejected keeps the malformed-numeric rejection
// on a trailing `.` with no fractional digits (e.g. `1.`). Reference accepts
// only the forms enumerated in check.rs::valid_literals; `1.` is not among
// them and we preserve the existing rejection to avoid a latent ambiguity
// with table.column dot-qualifier syntax.
func TestParseWhereTrailingDotRejected(t *testing.T) {
	if _, err := Parse("SELECT * FROM t WHERE n = 1."); err == nil {
		t.Fatal("Parse should reject trailing-dot numeric `1.`")
	}
}

// TestParseWhereBareExponentRejected ensures `1e` (exponent-letter with no
// digits) remains a malformed-numeric rejection rather than silently
// tokenizing as an identifier that would surface a confusing downstream
// error.
func TestParseWhereBareExponentRejected(t *testing.T) {
	if _, err := Parse("SELECT * FROM t WHERE n = 1e"); err == nil {
		t.Fatal("Parse should reject bare exponent `1e`")
	}
}

// TestParseWhereTrailingIdentifierAfterNumericRejected keeps the existing
// `1efoo` malformed-numeric rejection so the exponent widening does not
// accidentally accept an identifier-suffixed number.
func TestParseWhereTrailingIdentifierAfterNumericRejected(t *testing.T) {
	if _, err := Parse("SELECT * FROM t WHERE n = 1efoo"); err == nil {
		t.Fatal("Parse should reject `1efoo`")
	}
}

func TestParseWhereTwoPredicatesAnd(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users WHERE id = 1 AND name = 'alice'")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(stmt.Filters) != 2 {
		t.Fatalf("Filters len = %d, want 2", len(stmt.Filters))
	}
	if stmt.Filters[1].Column != "name" {
		t.Fatalf("second column = %q, want name", stmt.Filters[1].Column)
	}
	if stmt.Filters[1].Literal.Kind != LitString {
		t.Fatalf("second kind = %v, want LitString", stmt.Filters[1].Literal.Kind)
	}
	if stmt.Filters[1].Literal.Str != "alice" {
		t.Fatalf("second str = %q, want alice", stmt.Filters[1].Literal.Str)
	}
}

func TestParseWhereBoolLiterals(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE flag = TRUE AND other = false")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Filters[0].Literal.Kind != LitBool || !stmt.Filters[0].Literal.Bool {
		t.Fatalf("first filter want true bool, got %+v", stmt.Filters[0].Literal)
	}
	if stmt.Filters[1].Literal.Kind != LitBool || stmt.Filters[1].Literal.Bool {
		t.Fatalf("second filter want false bool, got %+v", stmt.Filters[1].Literal)
	}
}

// Reference expr type-check accepts bare boolean WHERE predicates
// (`select * from t where true`, crates/expr/src/check.rs line 423).
// On Shunter's current narrow surface this should behave the same as a
// filterless single-table query rather than forcing a synthetic comparison.
func TestParseWhereTrueLiteral(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if _, ok := stmt.Predicate.(TruePredicate); !ok {
		t.Fatalf("Predicate = %T, want TruePredicate", stmt.Predicate)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want none", stmt.Filters)
	}
}

func TestParseWhereFalseLiteral(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE FALSE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if _, ok := stmt.Predicate.(FalsePredicate); !ok {
		t.Fatalf("Predicate = %T, want FalsePredicate", stmt.Predicate)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want none", stmt.Filters)
	}
}

func TestParseWhereFalseAndComparison(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE FALSE AND id = 7")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	andPred, ok := stmt.Predicate.(AndPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want AndPredicate", stmt.Predicate)
	}
	if _, ok := andPred.Left.(FalsePredicate); !ok {
		t.Fatalf("Left = %T, want FalsePredicate", andPred.Left)
	}
	if _, ok := andPred.Right.(ComparisonPredicate); !ok {
		t.Fatalf("Right = %T, want ComparisonPredicate", andPred.Right)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want none for grouped predicate tree", stmt.Filters)
	}
}

func TestParseWhereFalseOrComparison(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE FALSE OR id = 7")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	orPred, ok := stmt.Predicate.(OrPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want OrPredicate", stmt.Predicate)
	}
	if _, ok := orPred.Left.(FalsePredicate); !ok {
		t.Fatalf("Left = %T, want FalsePredicate", orPred.Left)
	}
	if _, ok := orPred.Right.(ComparisonPredicate); !ok {
		t.Fatalf("Right = %T, want ComparisonPredicate", orPred.Right)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want none for grouped predicate tree", stmt.Filters)
	}
}

// Reference SQL docs explicitly call out quoted identifiers as the way to use
// reserved SQL keywords as table/column names (for example `SELECT * FROM
// "Order"`). Pin that end-to-end on Shunter's current narrow single-table
// surface using a quoted reserved table plus a quoted column reference.
// Reference SQL docs also call out quoted identifiers with non-alphanumeric
// characters such as `SELECT * FROM "Balance$"`. Pin that narrow single-table
// shape end-to-end so future parser changes do not regress quoted special-char
// table names.
func TestParseQuotedSpecialCharacterIdentifiers(t *testing.T) {
	stmt, err := Parse(`SELECT * FROM "Balance$" WHERE "id" = 7`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Balance$" {
		t.Fatalf("Table = %q, want Balance$", stmt.Table)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "Balance$" || cmp.Filter.Column != "id" || cmp.Filter.Op != "=" {
		t.Fatalf("Filter = %+v, want Balance$.id =", cmp.Filter)
	}
}

func TestParseQuotedReservedIdentifiers(t *testing.T) {
	stmt, err := Parse(`SELECT * FROM "Order" WHERE "id" = 7`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Order" {
		t.Fatalf("Table = %q, want Order", stmt.Table)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "Order" || cmp.Filter.Column != "id" || cmp.Filter.Op != "=" {
		t.Fatalf("Filter = %+v, want Order.id =", cmp.Filter)
	}
	if cmp.Filter.Literal.Kind != LitInt || cmp.Filter.Literal.Int != 7 {
		t.Fatalf("Literal = %+v, want int 7", cmp.Filter.Literal)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
}

func TestParseKeywordsCaseInsensitive(t *testing.T) {
	stmt, err := Parse("select * from Users where Id = 1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Users" {
		t.Fatalf("Table = %q, want Users (identifiers case-preserved)", stmt.Table)
	}
	if stmt.Filters[0].Column != "Id" {
		t.Fatalf("Column = %q, want Id", stmt.Filters[0].Column)
	}
}

func TestParseStringEscapedSingleQuote(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE name = 'O''Brien'")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got := stmt.Filters[0].Literal.Str
	if got != "O'Brien" {
		t.Fatalf("Str = %q, want O'Brien", got)
	}
}

func TestParseWhereQualifiedColumnsSameTable(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users WHERE users.id = 1 AND users.name = 'alice'")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "users" {
		t.Fatalf("Table = %q, want users", stmt.Table)
	}
	if len(stmt.Filters) != 2 {
		t.Fatalf("Filters len = %d, want 2", len(stmt.Filters))
	}
	if stmt.Filters[0].Column != "id" {
		t.Fatalf("first column = %q, want id", stmt.Filters[0].Column)
	}
	if stmt.Filters[1].Column != "name" {
		t.Fatalf("second column = %q, want name", stmt.Filters[1].Column)
	}
}

func TestParseWhereComparisonOperators(t *testing.T) {
	stmt, err := Parse("SELECT * FROM metrics WHERE score > 10 AND score >= 11 AND score < 20 AND score <= 19")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(stmt.Filters) != 4 {
		t.Fatalf("Filters len = %d, want 4", len(stmt.Filters))
	}
	for i, want := range []string{">", ">=", "<", "<="} {
		if stmt.Filters[i].Op != want {
			t.Fatalf("Filters[%d].Op = %q, want %q", i, stmt.Filters[i].Op, want)
		}
	}
}

func TestParseWhereNotEqualOperators(t *testing.T) {
	stmt, err := Parse("SELECT * FROM metrics WHERE score <> 10 AND score != 11")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(stmt.Filters) != 2 {
		t.Fatalf("Filters len = %d, want 2", len(stmt.Filters))
	}
	for i, want := range []string{"<>", "!="} {
		if stmt.Filters[i].Op != want {
			t.Fatalf("Filters[%d].Op = %q, want %q", i, stmt.Filters[i].Op, want)
		}
	}
	if stmt.Filters[0].Literal.Int != 10 || stmt.Filters[1].Literal.Int != 11 {
		t.Fatalf("unexpected literal ints: %+v", stmt.Filters)
	}
}

func TestParseWhereOrPredicates(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users WHERE id = 1 OR id = 2")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	orPred, ok := stmt.Predicate.(OrPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want OrPredicate", stmt.Predicate)
	}
	left, ok := orPred.Left.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Left type = %T, want ComparisonPredicate", orPred.Left)
	}
	right, ok := orPred.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Right type = %T, want ComparisonPredicate", orPred.Right)
	}
	if left.Filter.Column != "id" || right.Filter.Column != "id" {
		t.Fatalf("unexpected OR columns: left=%q right=%q", left.Filter.Column, right.Filter.Column)
	}
	if left.Filter.Literal.Int != 1 || right.Filter.Literal.Int != 2 {
		t.Fatalf("unexpected OR literal ints: left=%d right=%d", left.Filter.Literal.Int, right.Filter.Literal.Int)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want nil/empty for OR tree", stmt.Filters)
	}
}

// Reference expr type-check coverage accepts alias-qualified OR predicates with
// mixed qualified and unqualified column references (`crates/expr/src/check.rs`
// line 451: `select * from s as r where r.bytes = 0xABCD or bytes = X'ABCD'`).
// Pin that exact literal/alias shape now that Shunter's SQL grammar supports
// both 0x-prefixed and X'..' hex byte literals.
func TestParseWhereOrPredicatesWithAliasAndHexBytes(t *testing.T) {
	stmt, err := Parse("SELECT * FROM s AS r WHERE r.bytes = 0xABCD OR bytes = X'ABCD'")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	orPred, ok := stmt.Predicate.(OrPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want OrPredicate", stmt.Predicate)
	}
	left, ok := orPred.Left.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Left type = %T, want ComparisonPredicate", orPred.Left)
	}
	right, ok := orPred.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Right type = %T, want ComparisonPredicate", orPred.Right)
	}
	if left.Filter.Column != "bytes" || left.Filter.Table != "s" || left.Filter.Alias != "r" {
		t.Fatalf("left filter = %+v, want s/r.bytes", left.Filter)
	}
	if right.Filter.Column != "bytes" || right.Filter.Table != "s" || right.Filter.Alias != "" {
		t.Fatalf("right filter = %+v, want bare s.bytes", right.Filter)
	}
	if left.Filter.Literal.Kind != LitBytes || right.Filter.Literal.Kind != LitBytes {
		t.Fatalf("literal kinds = %v/%v, want LitBytes/LitBytes", left.Filter.Literal.Kind, right.Filter.Literal.Kind)
	}
	if got := string(left.Filter.Literal.Bytes); got != string([]byte{0xAB, 0xCD}) {
		t.Fatalf("left bytes = %x, want abcd", left.Filter.Literal.Bytes)
	}
	if got := string(right.Filter.Literal.Bytes); got != string([]byte{0xAB, 0xCD}) {
		t.Fatalf("right bytes = %x, want abcd", right.Filter.Literal.Bytes)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want nil/empty for OR tree", stmt.Filters)
	}
}

func TestParseWhereOrPredicatesWithAlias(t *testing.T) {
	stmt, err := Parse("SELECT item.* FROM users AS item WHERE item.id = 1 OR name = 'alice'")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedAlias != "item" {
		t.Fatalf("ProjectedAlias = %q, want item", stmt.ProjectedAlias)
	}
	orPred, ok := stmt.Predicate.(OrPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want OrPredicate", stmt.Predicate)
	}
	left, ok := orPred.Left.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Left type = %T, want ComparisonPredicate", orPred.Left)
	}
	right, ok := orPred.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Right type = %T, want ComparisonPredicate", orPred.Right)
	}
	if left.Filter.Column != "id" || left.Filter.Table != "users" || left.Filter.Alias != "item" {
		t.Fatalf("left filter = %+v, want users/item.id", left.Filter)
	}
	if right.Filter.Column != "name" || right.Filter.Table != "users" || right.Filter.Alias != "" {
		t.Fatalf("right filter = %+v, want bare users.name", right.Filter)
	}
	if left.Filter.Literal.Int != 1 {
		t.Fatalf("left literal int = %d, want 1", left.Filter.Literal.Int)
	}
	if right.Filter.Literal.Kind != LitString || right.Filter.Literal.Str != "alice" {
		t.Fatalf("right literal = %+v, want string alice", right.Filter.Literal)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %v, want nil/empty for OR tree", stmt.Filters)
	}
}

func TestParseJoinQualifiedProjectionOnAndWhereWithFloatLiteral(t *testing.T) {
	stmt, err := Parse("SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.f32 = 0.1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "t" || cmp.Filter.Column != "f32" || cmp.Filter.Alias != "t" {
		t.Fatalf("WHERE filter = %+v, want t.f32", cmp.Filter)
	}
	if cmp.Filter.Op != "=" || cmp.Filter.Literal.Kind != LitFloat || cmp.Filter.Literal.Float != 0.1 {
		t.Fatalf("WHERE filter op/literal = %+v, want = 0.1 float", cmp.Filter)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
}

func TestParseJoinQualifiedProjectionOnAndWhere(t *testing.T) {
	stmt, err := Parse("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Orders" {
		t.Fatalf("Table = %q, want Orders", stmt.Table)
	}
	if stmt.ProjectedTable != "Orders" {
		t.Fatalf("ProjectedTable = %q, want Orders", stmt.ProjectedTable)
	}
	if stmt.ProjectedAlias != "o" {
		t.Fatalf("ProjectedAlias = %q, want o", stmt.ProjectedAlias)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftTable != "Orders" || stmt.Join.RightTable != "Inventory" {
		t.Fatalf("join tables = %q/%q, want Orders/Inventory", stmt.Join.LeftTable, stmt.Join.RightTable)
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
	if cmp.Filter.Table != "Inventory" || cmp.Filter.Column != "quantity" {
		t.Fatalf("WHERE filter = %+v, want Inventory.quantity", cmp.Filter)
	}
	if cmp.Filter.Op != "<" || cmp.Filter.Literal.Int != 10 {
		t.Fatalf("WHERE filter op/literal = %+v, want < 10", cmp.Filter)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
}

func TestParseQuotedIdentifiersJoinProjectionOnAndWhere(t *testing.T) {
	stmt, err := Parse(`SELECT "Orders".* FROM "Orders" JOIN "Inventory" ON "Orders"."product_id" = "Inventory"."id" WHERE "Inventory"."quantity" < 10`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Orders" {
		t.Fatalf("Table = %q, want Orders", stmt.Table)
	}
	if stmt.ProjectedTable != "Orders" {
		t.Fatalf("ProjectedTable = %q, want Orders", stmt.ProjectedTable)
	}
	if stmt.ProjectedAlias != "Orders" {
		t.Fatalf("ProjectedAlias = %q, want Orders", stmt.ProjectedAlias)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftTable != "Orders" || stmt.Join.RightTable != "Inventory" {
		t.Fatalf("join tables = %q/%q, want Orders/Inventory", stmt.Join.LeftTable, stmt.Join.RightTable)
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
	if cmp.Filter.Table != "Inventory" || cmp.Filter.Column != "quantity" {
		t.Fatalf("WHERE filter = %+v, want Inventory.quantity", cmp.Filter)
	}
	if cmp.Filter.Op != "<" || cmp.Filter.Literal.Int != 10 {
		t.Fatalf("WHERE filter op/literal = %+v, want < 10", cmp.Filter)
	}
}

func TestParseQuotedIdentifiersJoinProjectionOnAndWhereWithParenthesizedConjunction(t *testing.T) {
	stmt, err := Parse(`SELECT "users".* FROM "users" JOIN "other" ON "users"."id" = "other"."uid" WHERE (("users"."id" = 1) AND ("users"."id" > 10))`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	andPred, ok := stmt.Predicate.(AndPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want AndPredicate", stmt.Predicate)
	}
	left, ok := andPred.Left.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Left type = %T, want ComparisonPredicate", andPred.Left)
	}
	right, ok := andPred.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Right type = %T, want ComparisonPredicate", andPred.Right)
	}
	if left.Filter.Table != "users" || left.Filter.Column != "id" || left.Filter.Op != "=" || left.Filter.Literal.Int != 1 {
		t.Fatalf("left filter = %+v, want users.id = 1", left.Filter)
	}
	if right.Filter.Table != "users" || right.Filter.Column != "id" || right.Filter.Op != ">" || right.Filter.Literal.Int != 10 {
		t.Fatalf("right filter = %+v, want users.id > 10", right.Filter)
	}
	if len(stmt.Filters) != 2 {
		t.Fatalf("Filters len = %d, want 2", len(stmt.Filters))
	}
}

func TestParseJoinQualifiedProjectionOnRightTable(t *testing.T) {
	stmt, err := Parse("SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Orders" {
		t.Fatalf("Table = %q, want Orders", stmt.Table)
	}
	if stmt.ProjectedTable != "Inventory" {
		t.Fatalf("ProjectedTable = %q, want Inventory", stmt.ProjectedTable)
	}
	if stmt.ProjectedAlias != "product" {
		t.Fatalf("ProjectedAlias = %q, want product", stmt.ProjectedAlias)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftTable != "Orders" || stmt.Join.RightTable != "Inventory" {
		t.Fatalf("join tables = %q/%q, want Orders/Inventory", stmt.Join.LeftTable, stmt.Join.RightTable)
	}
	if stmt.Join.LeftOn.Table != "Orders" || stmt.Join.LeftOn.Column != "product_id" {
		t.Fatalf("left ON = %+v, want Orders.product_id", stmt.Join.LeftOn)
	}
	if stmt.Join.RightOn.Table != "Inventory" || stmt.Join.RightOn.Column != "id" {
		t.Fatalf("right ON = %+v, want Inventory.id", stmt.Join.RightOn)
	}
	if stmt.Predicate != nil {
		t.Fatalf("Predicate = %T, want nil", stmt.Predicate)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters len = %d, want 0", len(stmt.Filters))
	}
}

func TestParseJoinQualifiedProjectionOnRightTableWithLeftFilter(t *testing.T) {
	stmt, err := Parse("SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE o.id = 1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Orders" {
		t.Fatalf("Table = %q, want Orders", stmt.Table)
	}
	if stmt.ProjectedTable != "Inventory" {
		t.Fatalf("ProjectedTable = %q, want Inventory", stmt.ProjectedTable)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate type = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "Orders" || cmp.Filter.Column != "id" {
		t.Fatalf("WHERE filter = %+v, want Orders.id", cmp.Filter)
	}
	if cmp.Filter.Op != "=" || cmp.Filter.Literal.Int != 1 {
		t.Fatalf("WHERE filter op/literal = %+v, want = 1", cmp.Filter)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
	}
}

func TestParseDefersAliasedBaseTableProjection(t *testing.T) {
	stmt, err := Parse("SELECT users.* FROM users AS item")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !stmt.ProjectedAliasUnknown || stmt.ProjectedAlias != "users" {
		t.Fatalf("ProjectedAliasUnknown/ProjectedAlias = %v/%q, want true/users", stmt.ProjectedAliasUnknown, stmt.ProjectedAlias)
	}
}

func TestParseDefersAliasedBaseTableQualifiedWhere(t *testing.T) {
	stmt, err := Parse("SELECT item.* FROM users AS item WHERE users.id = 1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "users" || cmp.Filter.Alias != "users" || cmp.Filter.Column != "id" {
		t.Fatalf("Filter = %+v, want deferred users.id", cmp.Filter)
	}
}

func TestParseDefersAliasedBaseTableJoinProjection(t *testing.T) {
	stmt, err := Parse("SELECT Orders.* FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !stmt.ProjectedAliasUnknown || stmt.ProjectedAlias != "Orders" {
		t.Fatalf("ProjectedAliasUnknown/ProjectedAlias = %v/%q, want true/Orders", stmt.ProjectedAliasUnknown, stmt.ProjectedAlias)
	}
}

func TestParseJoinQualifiedProjectionOnCrossJoin(t *testing.T) {
	stmt, err := Parse("SELECT o.* FROM Orders o JOIN Inventory product")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "Orders" {
		t.Fatalf("Table = %q, want Orders", stmt.Table)
	}
	if stmt.ProjectedTable != "Orders" {
		t.Fatalf("ProjectedTable = %q, want Orders", stmt.ProjectedTable)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.HasOn {
		t.Fatal("Join.HasOn = true, want false for cross join")
	}
}

func TestParseCrossJoinKeywordIsJoinOperator(t *testing.T) {
	stmt, err := Parse("SELECT t.* FROM t CROSS JOIN s")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftAlias != "t" || stmt.Join.RightAlias != "s" {
		t.Fatalf("aliases = %q/%q, want t/s", stmt.Join.LeftAlias, stmt.Join.RightAlias)
	}
	if stmt.Join.HasOn {
		t.Fatal("Join.HasOn = true, want false for CROSS JOIN")
	}
	if stmt.ProjectedAlias != "t" || stmt.ProjectedAliasUnknown {
		t.Fatalf("ProjectedAlias/Unknown = %q/%v, want t/false", stmt.ProjectedAlias, stmt.ProjectedAliasUnknown)
	}
}

func TestParseRejectsNonInnerJoinKeyword(t *testing.T) {
	_, err := Parse("SELECT LEFT.* FROM t LEFT JOIN s ON LEFT.id = s.id")
	if err == nil {
		t.Fatal("expected LEFT JOIN rejection")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
	want := "Non-inner joins are not supported"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestParseRejectsDanglingInnerJoinKeyword(t *testing.T) {
	_, err := Parse("SELECT * FROM t INNER")
	if err == nil {
		t.Fatal("expected dangling INNER rejection")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestParseDefersUnaliasedSelfCrossJoin pins the parser's
// deferred-rejection behavior for the unaliased self-join shape: both
// sides' alias is derived from the base table `t` so they collide,
// but the rejection is deferred to the compile stage so the reference
// `type_relvar` ordering holds.
func TestParseDefersUnaliasedSelfCrossJoin(t *testing.T) {
	stmt, err := Parse("SELECT t.* FROM t JOIN t")
	if err != nil {
		t.Fatalf("Parse error: %v (parser must defer dup-alias rejection to compile stage)", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata with AliasCollision=true")
	}
	if !stmt.Join.AliasCollision {
		t.Fatal("Join.AliasCollision = false, want true")
	}
	if stmt.Join.LeftAlias != "t" || stmt.Join.RightAlias != "t" {
		t.Fatalf("aliases = %q/%q, want t/t", stmt.Join.LeftAlias, stmt.Join.RightAlias)
	}
}

func TestParseAliasedSelfCrossJoinProjection(t *testing.T) {
	stmt, err := Parse("SELECT a.* FROM t AS a JOIN t AS b")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftTable != "t" || stmt.Join.RightTable != "t" {
		t.Fatalf("join tables = %q/%q, want t/t", stmt.Join.LeftTable, stmt.Join.RightTable)
	}
	if stmt.Join.HasOn {
		t.Fatal("Join.HasOn = true, want false for cross join")
	}
	if stmt.Join.LeftAlias != "a" || stmt.Join.RightAlias != "b" {
		t.Fatalf("join aliases = %q/%q, want a/b", stmt.Join.LeftAlias, stmt.Join.RightAlias)
	}
}

func TestParseAliasedSelfEquiJoinProjection(t *testing.T) {
	stmt, err := Parse("SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" || stmt.ProjectedTable != "t" {
		t.Fatalf("Table/Projected = %q/%q, want t/t", stmt.Table, stmt.ProjectedTable)
	}
	if stmt.ProjectedAlias != "a" {
		t.Fatalf("ProjectedAlias = %q, want a", stmt.ProjectedAlias)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftTable != "t" || stmt.Join.RightTable != "t" {
		t.Fatalf("join tables = %q/%q, want t/t", stmt.Join.LeftTable, stmt.Join.RightTable)
	}
	if !stmt.Join.HasOn {
		t.Fatal("Join.HasOn = false, want true")
	}
	if stmt.Join.LeftAlias != "a" || stmt.Join.RightAlias != "b" {
		t.Fatalf("join aliases = %q/%q, want a/b", stmt.Join.LeftAlias, stmt.Join.RightAlias)
	}
	if stmt.Join.LeftOn.Column != "u32" || stmt.Join.RightOn.Column != "u32" {
		t.Fatalf("ON cols = %q/%q, want u32/u32", stmt.Join.LeftOn.Column, stmt.Join.RightOn.Column)
	}
	if stmt.Join.LeftOn.Table != "t" || stmt.Join.RightOn.Table != "t" {
		t.Fatalf("ON tables = %q/%q, want t/t", stmt.Join.LeftOn.Table, stmt.Join.RightOn.Table)
	}
	if stmt.Join.LeftOn.Alias != "a" || stmt.Join.RightOn.Alias != "b" {
		t.Fatalf("ON aliases = %q/%q, want a/b", stmt.Join.LeftOn.Alias, stmt.Join.RightOn.Alias)
	}
}

func TestParseCaseDistinctRelationAliasesResolveIndependently(t *testing.T) {
	stmt, err := Parse(`SELECT "R".* FROM t AS "R" JOIN s AS r ON "R".u32 = r.u32 WHERE r.u32 = 1`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedAlias != "R" || stmt.ProjectedTable != "t" || stmt.ProjectedAliasUnknown {
		t.Fatalf("projected alias/table/unknown = %q/%q/%v, want R/t/false", stmt.ProjectedAlias, stmt.ProjectedTable, stmt.ProjectedAliasUnknown)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.LeftAlias != "R" || stmt.Join.RightAlias != "r" {
		t.Fatalf("join aliases = %q/%q, want R/r", stmt.Join.LeftAlias, stmt.Join.RightAlias)
	}
	if stmt.Join.LeftOn.Table != "t" || stmt.Join.RightOn.Table != "s" {
		t.Fatalf("ON tables = %q/%q, want t/s", stmt.Join.LeftOn.Table, stmt.Join.RightOn.Table)
	}
	if stmt.Join.LeftOn.Alias != "R" || stmt.Join.RightOn.Alias != "r" {
		t.Fatalf("ON aliases = %q/%q, want R/r", stmt.Join.LeftOn.Alias, stmt.Join.RightOn.Alias)
	}
	filter, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want comparison", stmt.Predicate)
	}
	if filter.Filter.Table != "s" || filter.Filter.Alias != "r" {
		t.Fatalf("WHERE filter table/alias = %q/%q, want s/r", filter.Filter.Table, filter.Filter.Alias)
	}
}

// self-join projection contract: RHS-side self-join projection must carry the b-alias so
// the compile path can thread ProjectRight=true.
func TestParseAliasedSelfEquiJoinProjectsRight(t *testing.T) {
	stmt, err := Parse("SELECT b.* FROM t AS a JOIN t AS b ON a.u32 = b.u32")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if stmt.ProjectedAlias != "b" {
		t.Fatalf("ProjectedAlias = %q, want b", stmt.ProjectedAlias)
	}
}

func TestParseRejectsSameAliasBothSidesOfEquiJoin(t *testing.T) {
	_, err := Parse("SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = a.u32")
	if err == nil {
		t.Fatal("expected error when both ON qualifiers reference the same alias")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseJoinWhereColumnEquality(t *testing.T) {
	stmt, err := Parse("SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want parsed cross join")
	}
	if stmt.Join.HasOn {
		t.Fatal("Join.HasOn = true, want false for cross join")
	}
	pred, ok := stmt.Predicate.(ColumnComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ColumnComparisonPredicate", stmt.Predicate)
	}
	if pred.Op != "=" {
		t.Fatalf("Op = %q, want =", pred.Op)
	}
	if pred.Left.Table != "t" || pred.Left.Alias != "t" || pred.Left.Column != "u32" {
		t.Fatalf("Left = %+v, want t.u32", pred.Left)
	}
	if pred.Right.Table != "s" || pred.Right.Alias != "s" || pred.Right.Column != "u32" {
		t.Fatalf("Right = %+v, want s.u32", pred.Right)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %+v, want none for column comparison predicate", stmt.Filters)
	}
}

func TestParseJoinWhereColumnEqualityAndLiteralFilter(t *testing.T) {
	stmt, err := Parse("SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want parsed cross join")
	}
	if stmt.Join.HasOn {
		t.Fatal("Join.HasOn = true, want false for cross join")
	}
	pred, ok := stmt.Predicate.(AndPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want AndPredicate", stmt.Predicate)
	}
	cmp, ok := pred.Left.(ColumnComparisonPredicate)
	if !ok {
		t.Fatalf("Left predicate = %T, want ColumnComparisonPredicate", pred.Left)
	}
	if cmp.Left.Table != "t" || cmp.Left.Alias != "t" || cmp.Left.Column != "u32" {
		t.Fatalf("Left = %+v, want t.u32", cmp.Left)
	}
	if cmp.Right.Table != "s" || cmp.Right.Alias != "s" || cmp.Right.Column != "u32" {
		t.Fatalf("Right = %+v, want s.u32", cmp.Right)
	}
	filter, ok := pred.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Right predicate = %T, want ComparisonPredicate", pred.Right)
	}
	if filter.Filter.Table != "s" || filter.Filter.Alias != "s" || filter.Filter.Column != "enabled" {
		t.Fatalf("Filter = %+v, want s.enabled", filter.Filter)
	}
	if filter.Filter.Literal.Kind != LitBool || !filter.Filter.Literal.Bool {
		t.Fatalf("Filter literal = %+v, want TRUE bool", filter.Filter.Literal)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %+v, want none because predicate includes column comparison", stmt.Filters)
	}
}

func TestParseRejectsJoinWhereColumnEqualityRequiresQualifiedColumns(t *testing.T) {
	cases := []string{
		"SELECT t.* FROM t JOIN s WHERE u32 = s.u32",
		"SELECT t.* FROM t JOIN s WHERE t.u32 = u32",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := Parse(in)
			if err == nil {
				t.Fatal("expected unqualified column-vs-column join WHERE rejection")
			}
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
			}
			if !strings.Contains(err.Error(), "Names must be qualified when using joins") {
				t.Fatalf("err = %q, want qualified-column message", err.Error())
			}
		})
	}
}

// TestParseRejectsMultiWayJoinChain pins the reference-matched rejection of
// three-way join shapes. The reference type checker accepts this shape
// (reference/SpacetimeDB/crates/expr/src/check.rs tests at line 459) but the
// reference subscription runtime rejects it at
// reference/SpacetimeDB/crates/subscription/src/lib.rs:251 with
// "Invalid number of tables in subscription: 3". Shunter rejects the chain
// shape at the parser boundary. WHERE-based column-vs-column forms are a
// separate widening; the chain itself is the rejection surface.
func TestParseRejectsMultiWayJoinChain(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"cross_chain", "SELECT t.* FROM t JOIN s JOIN s AS r"},
		{"on_chain", "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 JOIN s AS r ON s.u32 = r.u32"},
		{"inner_keyword", "SELECT t.* FROM t INNER JOIN s INNER JOIN s AS r"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.in)
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want multi-way rejection", c.in)
			}
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("Parse(%q) err = %v, want ErrUnsupportedSQL", c.in, err)
			}
			if !strings.Contains(err.Error(), "multi-way join") {
				t.Fatalf("Parse(%q) err = %q, want mention of multi-way join", c.in, err.Error())
			}
		})
	}
}

// TestParseRejectsMultiWayJoinOnForwardReference pins the reference-rejected
// shape `SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r` where the
// second JOIN's alias `r` is referenced by the first JOIN's ON clause before
// it is brought into scope. Reference rejects this at type-check
// (reference/SpacetimeDB/crates/expr/src/check.rs line 527, test
// "Alias r is not in scope when it is referenced"). Shunter's parser rejects
// it via the existing left-to-right qualifier-resolution walk inside
// parseJoinClause; this test pins that behavior so future refactors cannot
// silently loosen it.
func TestParseRejectsMultiWayJoinOnForwardReference(t *testing.T) {
	_, err := Parse("SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r")
	if err == nil {
		t.Fatal("expected rejection for ON clause referencing not-yet-in-scope alias")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestParseAcceptsJoinBareStarProjection pins that the SQL parser accepts
// `SELECT *` on a join at the syntactic layer. Semantic rejection of bare
// `*` on joins lives in protocol.compileSQLQueryString to match reference
// `InvalidWildcard::Join` (reference/SpacetimeDB/crates/expr/src/errors.rs:41,
// emit site lib.rs:56) with the literal text
// "SELECT * is not supported for joins", without the parser's
// `unsupported SQL: ` prefix.
func TestParseAcceptsJoinBareStarProjection(t *testing.T) {
	stmt, err := Parse("SELECT * FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err != nil {
		t.Fatalf("Parse err = %v, want nil (semantic rejection lives at compile stage)", err)
	}
	if stmt.Join == nil {
		t.Fatal("stmt.Join = nil, want non-nil")
	}
	if stmt.ProjectedAlias != "" {
		t.Fatalf("ProjectedAlias = %q, want \"\" (bare * leaves alias empty)", stmt.ProjectedAlias)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("ProjectionColumns len = %d, want 0 (bare * has no explicit columns)", len(stmt.ProjectionColumns))
	}
}

func TestParseSelectAllWithLimit(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users LIMIT 10")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "users" {
		t.Fatalf("Table = %q, want %q", stmt.Table, "users")
	}
	if stmt.ProjectedTable != "users" {
		t.Fatalf("ProjectedTable = %q, want %q", stmt.ProjectedTable, "users")
	}
	if stmt.Predicate != nil {
		t.Fatalf("Predicate = %T, want nil", stmt.Predicate)
	}
	if stmt.Limit == nil {
		t.Fatal("Limit = nil, want 10")
	}
	if *stmt.Limit != 10 {
		t.Fatalf("*Limit = %d, want 10", *stmt.Limit)
	}
}

func TestParseSelectAllWithOrderByAndLimit(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users WHERE active = TRUE ORDER BY name DESC LIMIT 5")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.OrderBy == nil {
		t.Fatal("OrderBy = nil, want name DESC")
	}
	if stmt.OrderBy.Table != "users" || stmt.OrderBy.Column != "name" || stmt.OrderBy.SourceQualifier != "" || !stmt.OrderBy.Desc {
		t.Fatalf("OrderBy = %+v, want users.name DESC", *stmt.OrderBy)
	}
	if stmt.Limit == nil || *stmt.Limit != 5 {
		t.Fatalf("Limit = %v, want 5", stmt.Limit)
	}
}

func TestParseOrderByQualifiedAliasAsc(t *testing.T) {
	stmt, err := Parse("SELECT item.* FROM users AS item ORDER BY item.id ASC")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.OrderBy == nil {
		t.Fatal("OrderBy = nil, want item.id ASC")
	}
	if stmt.OrderBy.Table != "users" || stmt.OrderBy.Column != "id" || stmt.OrderBy.SourceQualifier != "item" || stmt.OrderBy.Desc {
		t.Fatalf("OrderBy = %+v, want users.id qualified by item ASC", *stmt.OrderBy)
	}
}

func TestParseJoinOrderByQualifiedProjectedTableColumn(t *testing.T) {
	stmt, err := Parse("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id ORDER BY o.id DESC")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.OrderBy == nil {
		t.Fatal("OrderBy = nil, want o.id DESC")
	}
	if stmt.OrderBy.Table != "Orders" || stmt.OrderBy.Column != "id" || stmt.OrderBy.SourceQualifier != "o" || !stmt.OrderBy.Desc {
		t.Fatalf("OrderBy = %+v, want Orders.id qualified by o DESC", *stmt.OrderBy)
	}
}

func TestParseRejectsMultiColumnOrderBy(t *testing.T) {
	_, err := Parse("SELECT * FROM users ORDER BY name, id")
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("Parse err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseRejectsUnqualifiedJoinOrderBy(t *testing.T) {
	_, err := Parse("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id ORDER BY id")
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("Parse err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseLimitUint64Boundary(t *testing.T) {
	stmt, err := Parse("SELECT * FROM users LIMIT 18446744073709551615")
	if err != nil {
		t.Fatalf("Parse max uint64 limit error: %v", err)
	}
	if !stmt.HasLimit {
		t.Fatal("HasLimit = false, want true")
	}
	if stmt.UnsupportedLimit {
		t.Fatal("UnsupportedLimit = true, want false")
	}
	if stmt.InvalidLimit != nil {
		t.Fatalf("InvalidLimit = %+v, want nil", *stmt.InvalidLimit)
	}
	if stmt.Limit == nil {
		t.Fatal("Limit = nil, want max uint64")
	}
	if *stmt.Limit != ^uint64(0) {
		t.Fatalf("Limit = %d, want max uint64", *stmt.Limit)
	}

	stmt, err = Parse("SELECT * FROM users LIMIT 18446744073709551616")
	if err != nil {
		t.Fatalf("Parse overflow limit error: %v", err)
	}
	if !stmt.HasLimit {
		t.Fatal("overflow HasLimit = false, want true")
	}
	if stmt.UnsupportedLimit {
		t.Fatal("overflow UnsupportedLimit = true, want false")
	}
	if stmt.Limit != nil {
		t.Fatalf("overflow Limit = %d, want nil", *stmt.Limit)
	}
	if stmt.InvalidLimit == nil {
		t.Fatal("overflow InvalidLimit = nil, want literal metadata")
	}
	if stmt.InvalidLimit.Kind != LitBigInt || stmt.InvalidLimit.Text != "18446744073709551616" {
		t.Fatalf("overflow InvalidLimit = %+v, want LitBigInt with source text", *stmt.InvalidLimit)
	}
}

func TestParseSingleTableColumnProjection(t *testing.T) {
	stmt, err := Parse("SELECT u32 FROM t")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if len(stmt.ProjectionColumns) != 1 {
		t.Fatalf("len(ProjectionColumns) = %d, want 1", len(stmt.ProjectionColumns))
	}
	col := stmt.ProjectionColumns[0]
	if col.Table != "t" || col.Column != "u32" || col.SourceQualifier != "" || col.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[0] = %+v, want table=t column=u32 sourceQualifier='' outputAlias=''", col)
	}
}

func TestParseRejectsUnquotedLiteralKeywordProjectionColumns(t *testing.T) {
	for _, query := range []string{
		"SELECT TRUE FROM t",
		"SELECT FALSE FROM t",
		"SELECT NULL FROM t",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := Parse(query)
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("Parse err = %v, want ErrUnsupportedSQL", err)
			}
		})
	}
}

func TestParseRejectsUnquotedNullAsWhereColumn(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE NULL = 1")
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("Parse err = %v, want ErrUnsupportedSQL", err)
	}
	if len(stmt.Filters) != 0 {
		t.Fatalf("Filters = %+v, want none after rejected NULL LHS", stmt.Filters)
	}
}

func TestParseQuotedLiteralKeywordColumnsStillIdentifiers(t *testing.T) {
	stmt, err := Parse(`SELECT "true" FROM t`)
	if err != nil {
		t.Fatalf("Parse quoted projection: %v", err)
	}
	if len(stmt.ProjectionColumns) != 1 || stmt.ProjectionColumns[0].Column != "true" {
		t.Fatalf("ProjectionColumns = %+v, want quoted column true", stmt.ProjectionColumns)
	}

	stmt, err = Parse(`SELECT * FROM t WHERE "null" = 1`)
	if err != nil {
		t.Fatalf("Parse quoted WHERE column: %v", err)
	}
	if len(stmt.Filters) != 1 || stmt.Filters[0].Column != "null" {
		t.Fatalf("Filters = %+v, want quoted column null", stmt.Filters)
	}
}

func TestParseMultiColumnProjectionWithWhere(t *testing.T) {
	stmt, err := Parse("SELECT u32, name FROM t WHERE active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if len(stmt.ProjectionColumns) != 2 {
		t.Fatalf("len(ProjectionColumns) = %d, want 2", len(stmt.ProjectionColumns))
	}
	if stmt.ProjectionColumns[0].Column != "u32" || stmt.ProjectionColumns[1].Column != "name" {
		t.Fatalf("ProjectionColumns = %+v, want [u32 name]", stmt.ProjectionColumns)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "t" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want t.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
}

func TestParseSingleTableColumnProjectionWithAlias(t *testing.T) {
	stmt, err := Parse("SELECT u32 AS n FROM t")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if stmt.Aggregate != nil {
		t.Fatalf("Aggregate = %+v, want nil", stmt.Aggregate)
	}
	if stmt.Limit != nil {
		t.Fatalf("Limit = %v, want nil", *stmt.Limit)
	}
	if len(stmt.ProjectionColumns) != 1 {
		t.Fatalf("len(ProjectionColumns) = %d, want 1", len(stmt.ProjectionColumns))
	}
	col := stmt.ProjectionColumns[0]
	if col.Table != "t" || col.Column != "u32" || col.SourceQualifier != "" || col.OutputAlias != "n" {
		t.Fatalf("ProjectionColumns[0] = %+v, want table=t column=u32 sourceQualifier='' outputAlias='n'", col)
	}
}

func TestParseSingleTableColumnProjectionWithBareAliasAndWhere(t *testing.T) {
	stmt, err := Parse("SELECT u32 n FROM t WHERE active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if stmt.Aggregate != nil {
		t.Fatalf("Aggregate = %+v, want nil", stmt.Aggregate)
	}
	if stmt.Limit != nil {
		t.Fatalf("Limit = %v, want nil", *stmt.Limit)
	}
	if len(stmt.ProjectionColumns) != 1 {
		t.Fatalf("len(ProjectionColumns) = %d, want 1", len(stmt.ProjectionColumns))
	}
	col := stmt.ProjectionColumns[0]
	if col.Table != "t" || col.Column != "u32" || col.SourceQualifier != "" || col.OutputAlias != "n" {
		t.Fatalf("ProjectionColumns[0] = %+v, want table=t column=u32 sourceQualifier='' outputAlias='n'", col)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "t" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want t.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
}

func TestParseMultiColumnProjectionWithAliasesAndWhere(t *testing.T) {
	stmt, err := Parse("SELECT u32 AS n, active AS enabled FROM t WHERE active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if len(stmt.ProjectionColumns) != 2 {
		t.Fatalf("len(ProjectionColumns) = %d, want 2", len(stmt.ProjectionColumns))
	}
	if got := stmt.ProjectionColumns[0]; got.Table != "t" || got.Column != "u32" || got.SourceQualifier != "" || got.OutputAlias != "n" {
		t.Fatalf("ProjectionColumns[0] = %+v, want table=t column=u32 sourceQualifier='' outputAlias='n'", got)
	}
	if got := stmt.ProjectionColumns[1]; got.Table != "t" || got.Column != "active" || got.SourceQualifier != "" || got.OutputAlias != "enabled" {
		t.Fatalf("ProjectionColumns[1] = %+v, want table=t column=active sourceQualifier='' outputAlias='enabled'", got)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "t" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want t.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
}

func TestParseQualifiedSingleTableColumnProjectionWithAlias(t *testing.T) {
	stmt, err := Parse("SELECT t.u32 AS n FROM t")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if len(stmt.ProjectionColumns) != 1 {
		t.Fatalf("len(ProjectionColumns) = %d, want 1", len(stmt.ProjectionColumns))
	}
	col := stmt.ProjectionColumns[0]
	if col.Table != "t" || col.Column != "u32" || col.SourceQualifier != "t" || col.OutputAlias != "n" {
		t.Fatalf("ProjectionColumns[0] = %+v, want table=t column=u32 sourceQualifier='t' outputAlias='n'", col)
	}
}

func TestParseJoinColumnProjection(t *testing.T) {
	stmt, err := Parse("SELECT o.id, o.product_id FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedTable != "Orders" || stmt.ProjectedAlias != "o" {
		t.Fatalf("Projected = %q/%q, want Orders/o", stmt.ProjectedTable, stmt.ProjectedAlias)
	}
	if len(stmt.ProjectionColumns) != 2 {
		t.Fatalf("len(ProjectionColumns) = %d, want 2", len(stmt.ProjectionColumns))
	}
	if got := stmt.ProjectionColumns[0]; got.Table != "Orders" || got.Column != "id" || got.SourceQualifier != "o" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[0] = %+v", got)
	}
	if got := stmt.ProjectionColumns[1]; got.Table != "Orders" || got.Column != "product_id" || got.SourceQualifier != "o" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[1] = %+v", got)
	}
}

func TestParseJoinColumnProjectionProjectsRight(t *testing.T) {
	stmt, err := Parse("SELECT product.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedTable != "Inventory" || stmt.ProjectedAlias != "product" {
		t.Fatalf("Projected = %q/%q, want Inventory/product", stmt.ProjectedTable, stmt.ProjectedAlias)
	}
	if len(stmt.ProjectionColumns) != 2 {
		t.Fatalf("len(ProjectionColumns) = %d, want 2", len(stmt.ProjectionColumns))
	}
	if got := stmt.ProjectionColumns[0]; got.Table != "Inventory" || got.Column != "id" || got.SourceQualifier != "product" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[0] = %+v", got)
	}
	if got := stmt.ProjectionColumns[1]; got.Table != "Inventory" || got.Column != "quantity" || got.SourceQualifier != "product" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[1] = %+v", got)
	}
}

func TestParseJoinColumnProjectionAllowsMixedRelations(t *testing.T) {
	stmt, err := Parse("SELECT o.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedTable != "Orders" || stmt.ProjectedAlias != "o" {
		t.Fatalf("Projected = %q/%q, want first projected relation Orders/o", stmt.ProjectedTable, stmt.ProjectedAlias)
	}
	if len(stmt.ProjectionColumns) != 2 {
		t.Fatalf("len(ProjectionColumns) = %d, want 2", len(stmt.ProjectionColumns))
	}
	if got := stmt.ProjectionColumns[0]; got.Table != "Orders" || got.Column != "id" || got.SourceQualifier != "o" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[0] = %+v", got)
	}
	if got := stmt.ProjectionColumns[1]; got.Table != "Inventory" || got.Column != "quantity" || got.SourceQualifier != "product" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[1] = %+v", got)
	}
}

func TestParseSelfJoinColumnProjectionProjectsLeft(t *testing.T) {
	stmt, err := Parse("SELECT a.id FROM t AS a JOIN t AS b ON a.u32 = b.u32")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedTable != "t" || stmt.ProjectedAlias != "a" {
		t.Fatalf("Projected = %q/%q, want t/a", stmt.ProjectedTable, stmt.ProjectedAlias)
	}
	if len(stmt.ProjectionColumns) != 1 {
		t.Fatalf("len(ProjectionColumns) = %d, want 1", len(stmt.ProjectionColumns))
	}
	if got := stmt.ProjectionColumns[0]; got.Table != "t" || got.Column != "id" || got.SourceQualifier != "a" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[0] = %+v", got)
	}
}

func TestParseSelfJoinColumnProjectionProjectsRight(t *testing.T) {
	stmt, err := Parse("SELECT b.id FROM t AS a JOIN t AS b ON a.u32 = b.u32")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.ProjectedTable != "t" || stmt.ProjectedAlias != "b" {
		t.Fatalf("Projected = %q/%q, want t/b", stmt.ProjectedTable, stmt.ProjectedAlias)
	}
	if len(stmt.ProjectionColumns) != 1 {
		t.Fatalf("len(ProjectionColumns) = %d, want 1", len(stmt.ProjectionColumns))
	}
	if got := stmt.ProjectionColumns[0]; got.Table != "t" || got.Column != "id" || got.SourceQualifier != "b" || got.OutputAlias != "" {
		t.Fatalf("ProjectionColumns[0] = %+v", got)
	}
}

// TestParseDefersDistinctTableDuplicateJoinAliases pins the parser's
// deferred-rejection behavior: `LeftAlias == RightAlias` no longer
// emits a parser-time `DuplicateName` error. Reference `type_from`
// (`expr/src/check.rs:79-89`) resolves the left relvar BEFORE the
// HashSet duplicate-alias check, so left-table resolution must precede
// the dup-alias error. The compile stage emits
// `DuplicateNameError{Name: LeftAlias}` after the left schema lookup
// succeeds.
func TestParseDefersDistinctTableDuplicateJoinAliases(t *testing.T) {
	stmt, err := Parse("SELECT x.id FROM t AS x JOIN s AS x")
	if err != nil {
		t.Fatalf("Parse error: %v (parser must defer dup-alias rejection to compile stage)", err)
	}
	if stmt.Join == nil {
		t.Fatal("stmt.Join is nil; parser should still build a JoinClause for the deferred case")
	}
	if !stmt.Join.AliasCollision {
		t.Fatal("stmt.Join.AliasCollision = false, want true (parser must mark dup-alias for the compile stage)")
	}
	if stmt.Join.LeftAlias != "x" || stmt.Join.RightAlias != "x" {
		t.Fatalf("aliases = %q/%q, want x/x", stmt.Join.LeftAlias, stmt.Join.RightAlias)
	}
}

func TestParseCountStarAliasProjection(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) AS n FROM t")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) AS n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("len(ProjectionColumns) = %d, want 0", len(stmt.ProjectionColumns))
	}
	if stmt.Limit != nil {
		t.Fatalf("Limit = %v, want nil", *stmt.Limit)
	}
}

func TestParseCountStarBareAliasProjection(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) n FROM t")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Table != "t" {
		t.Fatalf("Table = %q, want t", stmt.Table)
	}
	if stmt.ProjectedTable != "t" {
		t.Fatalf("ProjectedTable = %q, want t", stmt.ProjectedTable)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("len(ProjectionColumns) = %d, want 0", len(stmt.ProjectionColumns))
	}
	if stmt.Limit != nil {
		t.Fatalf("Limit = %v, want nil", *stmt.Limit)
	}
}

func TestParseCountStarAliasProjectionWithWhere(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) AS n FROM t WHERE active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) AS n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "t" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want t.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
}

func TestParseCountStarBareAliasProjectionWithWhere(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) n FROM t WHERE active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "t" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want t.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
}

func TestParseCountStarAliasProjectionWithLimit(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) AS n FROM t LIMIT 1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) AS n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	if stmt.Limit == nil {
		t.Fatal("Limit = nil, want 1")
	}
	if *stmt.Limit != 1 {
		t.Fatalf("Limit = %d, want 1", *stmt.Limit)
	}
}

func TestParseCountStarBareAliasProjectionWithWhereAndLimitZero(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) n FROM t WHERE active = TRUE LIMIT 0")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	if stmt.Limit == nil {
		t.Fatal("Limit = nil, want 0")
	}
	if *stmt.Limit != 0 {
		t.Fatalf("Limit = %d, want 0", *stmt.Limit)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "t" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want t.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
}

func TestParseJoinCountStarAliasProjection(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) AS n FROM t JOIN s ON t.id = s.t_id")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) AS n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if !stmt.Join.HasOn {
		t.Fatalf("Join.HasOn = false, want true")
	}
	if stmt.Join.LeftTable != "t" || stmt.Join.RightTable != "s" || stmt.Join.LeftOn.Column != "id" || stmt.Join.RightOn.Column != "t_id" {
		t.Fatalf("Join = %+v, want t.id = s.t_id", *stmt.Join)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("len(ProjectionColumns) = %d, want 0", len(stmt.ProjectionColumns))
	}
}

func TestParseJoinCountStarBareAliasProjectionWithWhere(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) n FROM t JOIN s ON t.id = s.t_id WHERE s.active = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil {
		t.Fatal("Aggregate = nil, want COUNT(*) n metadata")
	}
	if stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", *stmt.Aggregate)
	}
	if stmt.Join == nil || !stmt.Join.HasOn {
		t.Fatalf("Join = %+v, want ON join", stmt.Join)
	}
	pred, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if pred.Filter.Table != "s" || pred.Filter.Alias != "s" || pred.Filter.Column != "active" || pred.Filter.Op != "=" {
		t.Fatalf("Predicate.Filter = %+v, want s.active = TRUE", pred.Filter)
	}
	if pred.Filter.Literal.Kind != LitBool || !pred.Filter.Literal.Bool {
		t.Fatalf("Predicate.Filter.Literal = %+v, want boolean TRUE", pred.Filter.Literal)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("len(ProjectionColumns) = %d, want 0", len(stmt.ProjectionColumns))
	}
}

// TestParseJoinCountStarAliasProjectionOnCrossJoinWhereEquality pins that the
// parser admits COUNT(*) aggregate projection on the already-accepted
// cross-join WHERE column-equality shape. The statement carries the aggregate
// metadata plus a cross-join (HasOn=false)
// and a ColumnComparisonPredicate that equates the two relations' columns.
func TestParseJoinCountStarAliasProjectionOnCrossJoinWhereEquality(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) AS n FROM t JOIN s WHERE t.u32 = s.u32")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil || stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", stmt.Aggregate)
	}
	if stmt.Join == nil {
		t.Fatal("Join = nil, want join metadata")
	}
	if stmt.Join.HasOn {
		t.Fatalf("Join.HasOn = true, want false (cross-join WHERE form)")
	}
	cmp, ok := stmt.Predicate.(ColumnComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ColumnComparisonPredicate", stmt.Predicate)
	}
	if cmp.Op != "=" {
		t.Fatalf("Predicate.Op = %q, want %q", cmp.Op, "=")
	}
	if cmp.Left.Table != "t" || cmp.Left.Column != "u32" {
		t.Fatalf("Predicate.Left = %+v, want t.u32", cmp.Left)
	}
	if cmp.Right.Table != "s" || cmp.Right.Column != "u32" {
		t.Fatalf("Predicate.Right = %+v, want s.u32", cmp.Right)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("len(ProjectionColumns) = %d, want 0", len(stmt.ProjectionColumns))
	}
}

// TestParseJoinCountStarBareAliasProjectionOnCrossJoinWhereEqualityAndFilter
// pins that the parser admits COUNT(*) aggregate projection with bare alias
// on the already-accepted cross-join WHERE equality-plus-single-literal-filter
// shape. The statement carries the aggregate metadata plus the
// AndPredicate{ColumnComparisonPredicate, ComparisonPredicate} shape without
// flattening.
func TestParseJoinCountStarBareAliasProjectionOnCrossJoinWhereEqualityAndFilter(t *testing.T) {
	stmt, err := Parse("SELECT COUNT(*) n FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Aggregate == nil || stmt.Aggregate.Func != "COUNT" || stmt.Aggregate.Alias != "n" {
		t.Fatalf("Aggregate = %+v, want Func=COUNT Alias=n", stmt.Aggregate)
	}
	if stmt.Join == nil || stmt.Join.HasOn {
		t.Fatalf("Join = %+v, want cross-join (HasOn=false)", stmt.Join)
	}
	and, ok := stmt.Predicate.(AndPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want AndPredicate", stmt.Predicate)
	}
	cmp, ok := and.Left.(ColumnComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate.Left = %T, want ColumnComparisonPredicate", and.Left)
	}
	if cmp.Left.Table != "t" || cmp.Left.Column != "u32" || cmp.Right.Table != "s" || cmp.Right.Column != "u32" {
		t.Fatalf("Predicate.Left = %+v, want t.u32 = s.u32", cmp)
	}
	filter, ok := and.Right.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate.Right = %T, want ComparisonPredicate", and.Right)
	}
	if filter.Filter.Table != "s" || filter.Filter.Column != "enabled" || filter.Filter.Op != "=" {
		t.Fatalf("Predicate.Right.Filter = %+v, want s.enabled =", filter.Filter)
	}
	if filter.Filter.Literal.Kind != LitBool || !filter.Filter.Literal.Bool {
		t.Fatalf("Predicate.Right.Filter.Literal = %+v, want TRUE", filter.Filter.Literal)
	}
	if len(stmt.ProjectionColumns) != 0 {
		t.Fatalf("len(ProjectionColumns) = %d, want 0", len(stmt.ProjectionColumns))
	}
}

func TestParseRejectsUnsupported(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"mixed_wildcard_projection", "SELECT *, u32 FROM t"},
		{"mixed_qualified_wildcard_projection", "SELECT t.*, u32 FROM t"},
		{"join_explicit_projection", "SELECT u32 FROM t JOIN s ON t.id = s.id"},
		{"aggregate_projection", "SELECT COUNT(*) FROM t"},
		{"mixed_aggregate_projection", "SELECT u32, COUNT(*) AS n FROM t"},
		{"aggregate_projection_with_group_by", "SELECT u32, COUNT(*) FROM t GROUP BY u32"},
		{"aggregate_projection_with_group_by_alias", "SELECT COUNT(*) AS n FROM t GROUP BY u32"},
		{"aggregate_multi_way_join", "SELECT COUNT(*) AS n FROM t JOIN s ON t.id = s.id JOIN r ON s.id = r.id"},
		{"trailing_garbage", "SELECT * FROM users foo bar"},
		{"missing_from", "SELECT *"},
		{"missing_table", "SELECT * FROM"},
		{"missing_select", "FROM users"},
		{"empty", ""},
		{"unterminated_string", "SELECT * FROM t WHERE s = 'abc"},
		{"malformed_integer", "SELECT * FROM t WHERE n = 12abc"},
		{"missing_where_rhs", "SELECT * FROM t WHERE id ="},
		{"missing_where_op", "SELECT * FROM t WHERE id 1"},
		{"and_without_lhs", "SELECT * FROM t WHERE AND id = 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.in)
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want error", c.in)
			}
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("Parse(%q) err = %v, want ErrUnsupportedSQL", c.in, err)
			}
		})
	}
}

func TestParseRejectsReservedAsTable(t *testing.T) {
	_, err := Parse("SELECT * FROM where")
	if err == nil {
		t.Fatal("expected error when reserved word used as table name")
	}
}

func TestParseErrorsMentionPosition(t *testing.T) {
	_, err := Parse("SELECT * FROM users WHERE id !~~ 1")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "!") {
		t.Fatalf("error %q should mention unexpected token", err.Error())
	}
}

// Reference expr type-check coverage accepts `:sender` as a caller-identity
// parameter on columns whose algebraic type is `identity()` or
// `bytes()` (`crates/expr/src/check.rs` lines 434-440: `select * from s
// where id = :sender` and `select * from s where bytes = :sender`). Pin the
// parser-level literal-kind surface so the subsequent coercion path sees a
// dedicated sender-parameter marker instead of a bare identifier.
func TestParseWhereSenderParameterOnIdentityColumn(t *testing.T) {
	stmt, err := Parse("SELECT * FROM s WHERE id = :sender")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "s" || cmp.Filter.Column != "id" || cmp.Filter.Op != "=" {
		t.Fatalf("Filter = %+v, want s.id =", cmp.Filter)
	}
	if cmp.Filter.Literal.Kind != LitSender {
		t.Fatalf("Literal.Kind = %v, want LitSender", cmp.Filter.Literal.Kind)
	}
}

func TestParseWhereSenderParameterOnBytesColumn(t *testing.T) {
	stmt, err := Parse("SELECT * FROM s WHERE bytes = :sender")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "s" || cmp.Filter.Column != "bytes" || cmp.Filter.Op != "=" {
		t.Fatalf("Filter = %+v, want s.bytes =", cmp.Filter)
	}
	if cmp.Filter.Literal.Kind != LitSender {
		t.Fatalf("Literal.Kind = %v, want LitSender", cmp.Filter.Literal.Kind)
	}
}

// TestParseWhereSenderParameterIsCaseSensitive pins reference `parse_expr`
// (sql-parser/src/parser/mod.rs:223) byte-equal `":sender"` admission.
// Any other casing falls through to `SqlUnsupported::Expr` rendered as
// `Unsupported expression: {expr}` (parser/errors.rs:38-39).
func TestParseWhereSenderParameterIsCaseSensitive(t *testing.T) {
	_, err := Parse("SELECT * FROM s WHERE id = :SENDER")
	if err == nil {
		t.Fatal("expected error for non-byte-equal :sender placeholder")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
	want := "Unsupported expression: :SENDER"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestParseWhereRejectsUnknownParameter(t *testing.T) {
	_, err := Parse("SELECT * FROM s WHERE id = :other")
	if err == nil {
		t.Fatal("expected error for unknown SQL parameter")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestParseWhereSenderParameterOnAliasedSingleTable pins the aliased single-
// table shape of the reference :sender parameter at the parser seam. The
// reference expression typechecker accepts alias-qualified :sender on an
// identity/bytes column in the same way as the unaliased form (see
// reference/SpacetimeDB/crates/expr/src/check.rs lines 435-440 for positive
// shapes and 487-488 for the rejection on non-identity/non-bytes columns).
// The alias resolver must produce Filter.Table = base table and
// Filter.Alias = the user-typed qualifier so the compile path can route
// caller identity through the coercion seam unchanged.
func TestParseWhereSenderParameterOnAliasedSingleTable(t *testing.T) {
	stmt, err := Parse("SELECT * FROM s AS r WHERE r.bytes = :sender")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "s" || cmp.Filter.Column != "bytes" || cmp.Filter.Alias != "r" || cmp.Filter.Op != "=" {
		t.Fatalf("Filter = %+v, want s.bytes = with alias r", cmp.Filter)
	}
	if cmp.Filter.Literal.Kind != LitSender {
		t.Fatalf("Literal.Kind = %v, want LitSender", cmp.Filter.Literal.Kind)
	}
}

// TestParseWhereSenderParameterInJoinFilter pins the :sender parameter in a
// join-backed WHERE leaf. Reference positive shapes live at
// reference/SpacetimeDB/crates/expr/src/check.rs lines 435-440 (standalone
// single-table) and line 462-464 (`select t.* from t join s on t.u32 = s.u32
// where t.f32 = 0.1`) — the :sender case here is the join analogue.
// Join WHERE leaves must stay qualified (parser.go requireQualify), and the
// qualifier is preserved in Filter.Alias so the compile path's aliasTag can
// route the leaf to the correct join side.
func TestParseWhereSenderParameterInJoinFilter(t *testing.T) {
	stmt, err := Parse("SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE s.bytes = :sender")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if stmt.Join == nil {
		t.Fatal("Join clause missing")
	}
	cmp, ok := stmt.Predicate.(ComparisonPredicate)
	if !ok {
		t.Fatalf("Predicate = %T, want ComparisonPredicate", stmt.Predicate)
	}
	if cmp.Filter.Table != "s" || cmp.Filter.Column != "bytes" || cmp.Filter.Alias != "s" || cmp.Filter.Op != "=" {
		t.Fatalf("Filter = %+v, want s.bytes = with alias s", cmp.Filter)
	}
	if cmp.Filter.Literal.Kind != LitSender {
		t.Fatalf("Literal.Kind = %v, want LitSender", cmp.Filter.Literal.Kind)
	}
}

func TestParseRejectsJoinOnNonPureEquality(t *testing.T) {
	for _, sqlText := range []string{
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
		"SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND o.id = 5",
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10 WHERE o.id > 0",
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10 AND o.id > 0",
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10 OR o.id > 0",
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.id = o.id",
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND quantity < 10",
		"SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND z.x < 10",
	} {
		t.Run(sqlText, func(t *testing.T) {
			_, err := Parse(sqlText)
			if err == nil {
				t.Fatal("expected error for non-pure JOIN ON equality")
			}
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
			}
			want := "Non-inner joins are not supported"
			if err.Error() != want {
				t.Fatalf("err = %q, want %q", err.Error(), want)
			}
		})
	}
}
