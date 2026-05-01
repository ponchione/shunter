package sql

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

type rapidSQLQuery struct {
	SQL              string
	Table            string
	TableAlias       string
	ProjectedAlias   string
	ProjectedTable   string
	Filters          []rapidSQLFilter
	Limit            *uint64
	HasLimit         bool
	InvalidLimit     *Literal
	UnsupportedLimit bool
}

type rapidSQLFilter struct {
	Column  string
	Alias   string
	Op      string
	Literal Literal
}

type rapidSQLLiteral struct {
	SQL     string
	Literal Literal
}

type rapidTestFataler interface {
	Helper()
	Fatalf(string, ...any)
}

func rapidIdentifier() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z_][A-Za-z0-9_]{0,8}`).Filter(func(s string) bool {
		return !isReserved(s)
	})
}

func rapidSQLLiteralToken() *rapid.Generator[rapidSQLLiteral] {
	return rapid.OneOf(
		rapid.Map(rapid.Int64Range(-1_000_000, 1_000_000), func(n int64) rapidSQLLiteral {
			text := strconv.FormatInt(n, 10)
			return rapidSQLLiteral{SQL: text, Literal: Literal{Kind: LitInt, Int: n, Text: text}}
		}),
		rapid.Map(rapid.SampledFrom([]string{"0.5", "1.25", "-7.5", "1e-3", "1e3", "1E3"}), func(text string) rapidSQLLiteral {
			lit, err := parseNumericLiteral(text)
			if err != nil {
				panic(err)
			}
			return rapidSQLLiteral{SQL: text, Literal: lit}
		}),
		rapid.Map(rapid.Bool(), func(b bool) rapidSQLLiteral {
			if b {
				return rapidSQLLiteral{SQL: "TRUE", Literal: Literal{Kind: LitBool, Bool: true}}
			}
			return rapidSQLLiteral{SQL: "FALSE", Literal: Literal{Kind: LitBool, Bool: false}}
		}),
		rapid.Map(rapid.StringMatching(`[A-Za-z0-9_]{0,8}`), func(s string) rapidSQLLiteral {
			return rapidSQLLiteral{SQL: "'" + s + "'", Literal: Literal{Kind: LitString, Str: s, Text: s}}
		}),
		rapid.Custom(func(t *rapid.T) rapidSQLLiteral {
			b := rapid.SliceOfN(rapid.Byte(), 1, 4).Draw(t, "bytes")
			text := "0x" + strings.ToUpper(hex.EncodeToString(b))
			return rapidSQLLiteral{SQL: text, Literal: Literal{Kind: LitBytes, Bytes: b, Text: text}}
		}),
		rapid.Just(rapidSQLLiteral{SQL: ":sender", Literal: Literal{Kind: LitSender}}),
	)
}

func rapidSingleTableQuery() *rapid.Generator[rapidSQLQuery] {
	return rapid.Custom(func(t *rapid.T) rapidSQLQuery {
		return drawRapidSingleTableQuery(t, strings.ToUpper)
	})
}

func drawRapidSingleTableQuery(t *rapid.T, kw func(string) string) rapidSQLQuery {
	table := rapidIdentifier().Draw(t, "table")
	aliasMode := rapid.IntRange(0, 2).Draw(t, "aliasMode")
	alias := ""
	fromAlias := ""
	switch aliasMode {
	case 1:
		alias = rapidIdentifier().Draw(t, "alias")
		fromAlias = " " + kw("AS") + " " + alias
	case 2:
		alias = rapidIdentifier().Draw(t, "alias")
		fromAlias = " " + alias
	}
	tableAlias := table
	if alias != "" {
		tableAlias = alias
	}

	projection := "*"
	projectedAlias := ""
	if rapid.Bool().Draw(t, "qualifiedProjection") {
		projectedAlias = table
		if alias != "" {
			projectedAlias = alias
		}
		projection = projectedAlias + ".*"
	}

	q := rapidSQLQuery{
		Table:          table,
		TableAlias:     tableAlias,
		ProjectedAlias: projectedAlias,
		ProjectedTable: table,
	}

	var b strings.Builder
	b.WriteString(kw("SELECT"))
	b.WriteByte(' ')
	b.WriteString(projection)
	b.WriteByte(' ')
	b.WriteString(kw("FROM"))
	b.WriteByte(' ')
	b.WriteString(table)
	b.WriteString(fromAlias)

	if rapid.Bool().Draw(t, "hasWhere") {
		filterCount := rapid.IntRange(1, 4).Draw(t, "filterCount")
		b.WriteByte(' ')
		b.WriteString(kw("WHERE"))
		b.WriteByte(' ')
		for i := range filterCount {
			if i > 0 {
				b.WriteByte(' ')
				b.WriteString(kw("AND"))
				b.WriteByte(' ')
			}
			column := rapidIdentifier().Draw(t, "column")
			qualifier := ""
			columnSQL := column
			if rapid.Bool().Draw(t, "qualifiedColumn") {
				qualifier = table
				if alias != "" {
					qualifier = alias
				}
				columnSQL = qualifier + "." + column
			}
			op := rapid.SampledFrom([]string{"=", "<", ">", "<=", ">=", "!=", "<>"}).Draw(t, "op")
			lit := rapidSQLLiteralToken().Draw(t, "literal")
			q.Filters = append(q.Filters, rapidSQLFilter{Column: column, Alias: qualifier, Op: op, Literal: lit.Literal})
			b.WriteString(columnSQL)
			b.WriteByte(' ')
			b.WriteString(op)
			b.WriteByte(' ')
			if lit.Literal.Kind == LitBool {
				b.WriteString(kw(lit.SQL))
			} else {
				b.WriteString(lit.SQL)
			}
		}
	}

	switch rapid.IntRange(0, 3).Draw(t, "limitMode") {
	case 1:
		limit := rapid.Uint64Range(0, 1024).Draw(t, "limit")
		q.Limit = &limit
		q.HasLimit = true
		b.WriteByte(' ')
		b.WriteString(kw("LIMIT"))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(limit, 10))
	case 2:
		q.HasLimit = true
		q.InvalidLimit = &Literal{Kind: LitFloat, Float: 1.5, Text: "1.5"}
		b.WriteByte(' ')
		b.WriteString(kw("LIMIT"))
		b.WriteString(" 1.5")
	case 3:
		q.HasLimit = true
		q.UnsupportedLimit = true
		b.WriteByte(' ')
		b.WriteString(kw("LIMIT"))
		b.WriteString(" +1")
	}

	if rapid.Bool().Draw(t, "semicolon") {
		b.WriteByte(';')
	}
	q.SQL = b.String()
	return q
}

func TestRapidParseGeneratedSingleTableQueries(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		q := rapidSingleTableQuery().Draw(t, "query")

		stmt, err := Parse(q.SQL)
		if err != nil {
			t.Fatalf("Parse(%q): %v", q.SQL, err)
		}
		assertRapidQueryStatement(t, stmt, q)
	})
}

func TestRapidParseKeywordCaseDoesNotAffectSyntax(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		upper := drawRapidSingleTableQuery(t, strings.ToUpper)
		lower := upper
		lower.SQL = renderRapidQueryWithKeywordCase(upper.SQL, strings.ToLower)

		upperStmt, err := Parse(upper.SQL)
		if err != nil {
			t.Fatalf("Parse uppercase query %q: %v", upper.SQL, err)
		}
		lowerStmt, err := Parse(lower.SQL)
		if err != nil {
			t.Fatalf("Parse lowercase query %q: %v", lower.SQL, err)
		}
		if !rapidStatementsEquivalent(upperStmt, lowerStmt) {
			t.Fatalf("keyword case changed semantics:\nupper=%+v\nlower=%+v", upperStmt, lowerStmt)
		}
	})
}

func TestParseAndPredicateReorderingPreservesFilterSet(t *testing.T) {
	const seed = uint64(0x51a71d)
	cases := []struct {
		name      string
		original  string
		reordered string
	}{
		{
			name:      "unqualified",
			original:  "SELECT * FROM messages WHERE sender = 'ada' AND id >= 7 AND payload = 0xDEADBEEF LIMIT 10",
			reordered: "SELECT * FROM messages WHERE payload = 0xDEADBEEF AND sender = 'ada' AND id >= 7 LIMIT 10",
		},
		{
			name:      "qualified-alias",
			original:  "SELECT m.* FROM messages AS m WHERE m.id = 1 AND m.active = TRUE AND m.body <> 'done'",
			reordered: "SELECT m.* FROM messages AS m WHERE m.body <> 'done' AND m.id = 1 AND m.active = TRUE",
		},
	}

	for opIndex, tc := range cases {
		original, err := Parse(tc.original)
		if err != nil {
			t.Fatalf("seed=%#x op_index=%d case=%s Parse original: %v", seed, opIndex, tc.name, err)
		}
		reordered, err := Parse(tc.reordered)
		if err != nil {
			t.Fatalf("seed=%#x op_index=%d case=%s Parse reordered: %v", seed, opIndex, tc.name, err)
		}
		if !rapidStatementsEquivalentIgnoringFilterOrder(original, reordered) {
			t.Fatalf("seed=%#x op_index=%d case=%s reordered AND predicates changed parsed semantics:\noriginal=%#v\nreordered=%#v",
				seed, opIndex, tc.name, original, reordered)
		}
	}
}

func renderRapidQueryWithKeywordCase(sql string, kw func(string) string) string {
	var out strings.Builder
	for i := 0; i < len(sql); {
		if sql[i] == '\'' {
			start := i
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					i++
					if i < len(sql) && sql[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			out.WriteString(sql[start:i])
			continue
		}
		if isIdentStart(sql[i]) {
			start := i
			i++
			for i < len(sql) && isIdentCont(sql[i]) {
				i++
			}
			token := sql[start:i]
			if isReserved(token) {
				out.WriteString(kw(token))
			} else {
				out.WriteString(token)
			}
			continue
		}
		out.WriteByte(sql[i])
		i++
	}
	return out.String()
}

func TestRapidParsePreservesLiteralSourceText(t *testing.T) {
	cases := []rapidSQLLiteral{
		{SQL: "+001", Literal: Literal{Kind: LitInt, Text: "+001"}},
		{SQL: "001", Literal: Literal{Kind: LitInt, Text: "001"}},
		{SQL: "1.10", Literal: Literal{Kind: LitFloat, Text: "1.10"}},
		{SQL: "1e3", Literal: Literal{Kind: LitInt, Text: "1e3"}},
		{SQL: "1E3", Literal: Literal{Kind: LitInt, Text: "1E3"}},
		{SQL: "1e-3", Literal: Literal{Kind: LitFloat, Text: "1e-3"}},
		{SQL: "1e40", Literal: Literal{Kind: LitBigInt, Text: "1e40"}},
		{SQL: "'plain'", Literal: Literal{Kind: LitString, Text: "plain"}},
		{SQL: "'O''Brien'", Literal: Literal{Kind: LitString, Text: "O'Brien"}},
		{SQL: "0xDEADBEEF", Literal: Literal{Kind: LitBytes, Text: "0xDEADBEEF"}},
		{SQL: "X'01AF'", Literal: Literal{Kind: LitBytes, Text: "X'01AF'"}},
	}

	rapid.Check(t, func(t *rapid.T) {
		tc := rapid.SampledFrom(cases).Draw(t, "literal")
		stmt, err := Parse("SELECT * FROM t WHERE c = " + tc.SQL)
		if err != nil {
			t.Fatalf("Parse literal %q: %v", tc.SQL, err)
		}
		if len(stmt.Filters) != 1 {
			t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
		}
		got := stmt.Filters[0].Literal
		if got.Kind != tc.Literal.Kind {
			t.Fatalf("Literal.Kind = %v, want %v", got.Kind, tc.Literal.Kind)
		}
		if got.Text != tc.Literal.Text {
			t.Fatalf("Literal.Text = %q, want %q", got.Text, tc.Literal.Text)
		}
	})
}

func assertRapidQueryStatement(t rapidTestFataler, stmt Statement, q rapidSQLQuery) {
	t.Helper()
	if stmt.Table != q.Table {
		t.Fatalf("Table = %q, want %q for %q", stmt.Table, q.Table, q.SQL)
	}
	if stmt.TableAlias != q.TableAlias {
		t.Fatalf("TableAlias = %q, want %q for %q", stmt.TableAlias, q.TableAlias, q.SQL)
	}
	if stmt.ProjectedTable != q.ProjectedTable || stmt.ProjectedAlias != q.ProjectedAlias || stmt.ProjectedAliasUnknown {
		t.Fatalf("projection = table %q alias %q unknown %v, want table %q alias %q known for %q",
			stmt.ProjectedTable, stmt.ProjectedAlias, stmt.ProjectedAliasUnknown, q.ProjectedTable, q.ProjectedAlias, q.SQL)
	}
	if len(stmt.Filters) != len(q.Filters) {
		t.Fatalf("Filters len = %d, want %d for %q", len(stmt.Filters), len(q.Filters), q.SQL)
	}
	assertRapidPredicateShape(t, stmt.Predicate, len(q.Filters))
	for i, want := range q.Filters {
		got := stmt.Filters[i]
		if got.Table != q.Table || got.Column != want.Column || got.Alias != want.Alias || got.Op != want.Op {
			t.Fatalf("Filters[%d] = %+v, want table=%q column=%q alias=%q op=%q for %q", i, got, q.Table, want.Column, want.Alias, want.Op, q.SQL)
		}
		if !rapidLiteralsEqual(got.Literal, want.Literal) {
			t.Fatalf("Filters[%d].Literal = %+v, want %+v for %q", i, got.Literal, want.Literal, q.SQL)
		}
	}
	if stmt.HasLimit != q.HasLimit || stmt.UnsupportedLimit != q.UnsupportedLimit {
		t.Fatalf("limit flags = has %v unsupported %v, want has %v unsupported %v for %q",
			stmt.HasLimit, stmt.UnsupportedLimit, q.HasLimit, q.UnsupportedLimit, q.SQL)
	}
	if !rapidUint64PtrEqual(stmt.Limit, q.Limit) {
		t.Fatalf("Limit = %v, want %v for %q", stmt.Limit, q.Limit, q.SQL)
	}
	if !rapidLiteralPtrEqual(stmt.InvalidLimit, q.InvalidLimit) {
		t.Fatalf("InvalidLimit = %+v, want %+v for %q", stmt.InvalidLimit, q.InvalidLimit, q.SQL)
	}
}

func assertRapidPredicateShape(t rapidTestFataler, pred Predicate, wantLeaves int) {
	t.Helper()
	if wantLeaves == 0 {
		if pred != nil {
			t.Fatalf("Predicate = %T, want nil", pred)
		}
		return
	}
	if got := rapidComparisonLeafCount(pred); got != wantLeaves {
		t.Fatalf("comparison leaf count = %d, want %d in %T", got, wantLeaves, pred)
	}
}

func rapidComparisonLeafCount(pred Predicate) int {
	switch p := pred.(type) {
	case ComparisonPredicate:
		return 1
	case AndPredicate:
		return rapidComparisonLeafCount(p.Left) + rapidComparisonLeafCount(p.Right)
	default:
		return 0
	}
}

func rapidStatementsEquivalent(a, b Statement) bool {
	if a.Table != b.Table ||
		a.TableAlias != b.TableAlias ||
		a.ProjectedTable != b.ProjectedTable ||
		a.ProjectedAlias != b.ProjectedAlias ||
		a.ProjectedAliasUnknown != b.ProjectedAliasUnknown ||
		a.HasLimit != b.HasLimit ||
		a.UnsupportedLimit != b.UnsupportedLimit ||
		!rapidUint64PtrEqual(a.Limit, b.Limit) ||
		!rapidLiteralPtrEqual(a.InvalidLimit, b.InvalidLimit) ||
		len(a.Filters) != len(b.Filters) {
		return false
	}
	if rapidComparisonLeafCount(a.Predicate) != rapidComparisonLeafCount(b.Predicate) {
		return false
	}
	for i := range a.Filters {
		af, bf := a.Filters[i], b.Filters[i]
		if af.Table != bf.Table || af.Column != bf.Column || af.Alias != bf.Alias || af.Op != bf.Op || !rapidLiteralsEqual(af.Literal, bf.Literal) {
			return false
		}
	}
	return true
}

func rapidStatementsEquivalentIgnoringFilterOrder(a, b Statement) bool {
	if a.Table != b.Table ||
		a.TableAlias != b.TableAlias ||
		a.ProjectedTable != b.ProjectedTable ||
		a.ProjectedAlias != b.ProjectedAlias ||
		a.ProjectedAliasUnknown != b.ProjectedAliasUnknown ||
		a.HasLimit != b.HasLimit ||
		a.UnsupportedLimit != b.UnsupportedLimit ||
		!rapidUint64PtrEqual(a.Limit, b.Limit) ||
		!rapidLiteralPtrEqual(a.InvalidLimit, b.InvalidLimit) ||
		!reflect.DeepEqual(a.ProjectionColumns, b.ProjectionColumns) ||
		!reflect.DeepEqual(a.Aggregate, b.Aggregate) ||
		!reflect.DeepEqual(a.Join, b.Join) ||
		rapidComparisonLeafCount(a.Predicate) != rapidComparisonLeafCount(b.Predicate) {
		return false
	}
	return reflect.DeepEqual(rapidFilterMultiset(a.Filters), rapidFilterMultiset(b.Filters))
}

func rapidFilterMultiset(filters []Filter) map[string]int {
	out := make(map[string]int, len(filters))
	for _, filter := range filters {
		out[rapidFilterSignature(filter)]++
	}
	return out
}

func rapidFilterSignature(filter Filter) string {
	return fmt.Sprintf("%q\x00%q\x00%q\x00%q\x00%#v", filter.Table, filter.Alias, filter.Column, filter.Op, filter.Literal)
}

func rapidUint64PtrEqual(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func rapidLiteralPtrEqual(a, b *Literal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return rapidLiteralsEqual(*a, *b)
}

func rapidLiteralsEqual(a, b Literal) bool {
	if a.Kind != b.Kind || a.Int != b.Int || a.Float != b.Float || a.Bool != b.Bool || a.Str != b.Str || a.Text != b.Text || !bytes.Equal(a.Bytes, b.Bytes) {
		return false
	}
	if a.Big == nil || b.Big == nil {
		return a.Big == nil && b.Big == nil
	}
	return a.Big.Cmp(b.Big) == 0
}
