package sql

import (
	"errors"
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

func TestParseRejectsAliasedBaseTableProjection(t *testing.T) {
	_, err := Parse("SELECT users.* FROM users AS item")
	if err == nil {
		t.Fatal("expected error for base-table projection after alias")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseRejectsAliasedBaseTableQualifiedWhere(t *testing.T) {
	_, err := Parse("SELECT item.* FROM users AS item WHERE users.id = 1")
	if err == nil {
		t.Fatal("expected error for base-table qualified WHERE after alias")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseRejectsAliasedBaseTableJoinProjection(t *testing.T) {
	_, err := Parse("SELECT Orders.* FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err == nil {
		t.Fatal("expected error for base-table projection after join alias")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
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

func TestParseRejectsUnaliasedSelfCrossJoin(t *testing.T) {
	_, err := Parse("SELECT t.* FROM t JOIN t")
	if err == nil {
		t.Fatal("expected error for unaliased self cross join")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
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

func TestParseRejectsSameAliasBothSidesOfEquiJoin(t *testing.T) {
	_, err := Parse("SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = a.u32")
	if err == nil {
		t.Fatal("expected error when both ON qualifiers reference the same alias")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseRejectsJoinBareStarProjection(t *testing.T) {
	_, err := Parse("SELECT * FROM Orders o JOIN Inventory product ON o.product_id = product.id")
	if err == nil {
		t.Fatal("expected error for bare * projection on join")
	}
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestParseRejectsUnsupported(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"projection", "SELECT id FROM users"},
		{"qualified_projection_wrong_alias", "SELECT other.* FROM users AS item"},
		{"order_by", "SELECT * FROM users ORDER BY id"},
		{"limit", "SELECT * FROM users LIMIT 10"},
		{"trailing_garbage", "SELECT * FROM users foo bar"},
		{"missing_from", "SELECT *"},
		{"missing_table", "SELECT * FROM"},
		{"missing_select", "FROM users"},
		{"empty", ""},
		{"unterminated_string", "SELECT * FROM t WHERE s = 'abc"},
		{"malformed_integer", "SELECT * FROM t WHERE n = 12abc"},
		{"qualified_column_other_table", "SELECT * FROM users WHERE posts.id = 1"},
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
