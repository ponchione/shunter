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

func TestParseRejectsUnsupported(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"projection", "SELECT id FROM users"},
		{"join", "SELECT * FROM a JOIN b ON a.id = b.id"},
		{"comparison_gt", "SELECT * FROM users WHERE id > 1"},
		{"or", "SELECT * FROM users WHERE id = 1 OR id = 2"},
		{"order_by", "SELECT * FROM users ORDER BY id"},
		{"limit", "SELECT * FROM users LIMIT 10"},
		{"trailing_garbage", "SELECT * FROM users foo"},
		{"missing_from", "SELECT *"},
		{"missing_table", "SELECT * FROM"},
		{"missing_select", "FROM users"},
		{"empty", ""},
		{"unterminated_string", "SELECT * FROM t WHERE s = 'abc"},
		{"malformed_integer", "SELECT * FROM t WHERE n = 12abc"},
		{"qualified_column", "SELECT * FROM users WHERE users.id = 1"},
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
	_, err := Parse("SELECT * FROM users WHERE id > 1")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "'>'") && !strings.Contains(err.Error(), ">") {
		t.Fatalf("error %q should mention unexpected token", err.Error())
	}
}
