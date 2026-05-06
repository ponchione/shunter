// Package sql parses the SQL subset accepted by the protocol query surfaces.
// Unsupported joins, projections, predicates, and literals reject with
// ErrUnsupportedSQL. Identifiers are byte-exact after quoted unescaping.
package sql

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// ErrUnsupportedSQL is the sentinel for any input outside the grammar.
// Wrap with fmt.Errorf("%w: ...", ErrUnsupportedSQL, ...) for specifics.
var ErrUnsupportedSQL = errors.New("unsupported SQL")

const maxSQLLength = 50_000

// LitKind tags a parsed literal's lexical category.
type LitKind int

const (
	LitInt LitKind = iota
	LitFloat
	LitBool
	LitString
	LitBytes
	// LitSender is the :sender caller-identity placeholder.
	LitSender
	// LitBigInt carries an integer literal that does not fit int64.
	LitBigInt
)

// Literal is a parsed SQL literal in raw lexical form.
// Text preserves the original token text when coercion needs exact rendering.
type Literal struct {
	Kind  LitKind
	Int   int64
	Float float64
	Bool  bool
	Str   string
	Bytes []byte
	Big   *big.Int
	Text  string
}

// Filter is a single column comparison against a literal value.
// Alias preserves the qualifier token for self-join routing.
type Filter struct {
	Table   string
	Column  string
	Alias   string
	Op      string
	Literal Literal
}

// ColumnRef is a qualified column reference resolved to its owning table
// and to the relation-instance alias the qualifier named. Alias is the
// exact identifier the user typed (or the base table name when no alias
// was declared); it distinguishes two aliased instances of the same
// underlying table.
type ColumnRef struct {
	Table  string
	Column string
	Alias  string
}

// JoinClause is the parsed two-table join metadata.
// AliasCollision defers duplicate-alias rejection until compile-time ordering
// can account for missing left tables.
type JoinClause struct {
	LeftTable      string
	RightTable     string
	LeftAlias      string
	RightAlias     string
	HasOn          bool
	LeftOn         ColumnRef
	RightOn        ColumnRef
	AliasCollision bool
}

// Predicate is the structured WHERE tree for parsed SQL.
type Predicate interface {
	isPredicate()
}

// ComparisonPredicate is a single column comparison leaf.
type ComparisonPredicate struct {
	Filter Filter
}

func (ComparisonPredicate) isPredicate() {}

// ColumnComparisonPredicate compares two qualified columns. It is intentionally
// limited to join-scoped lowering.
type ColumnComparisonPredicate struct {
	Left  ColumnRef
	Op    string
	Right ColumnRef
}

func (ColumnComparisonPredicate) isPredicate() {}

// NullPredicate is a column IS NULL / IS NOT NULL predicate.
type NullPredicate struct {
	Column ColumnRef
	Not    bool
}

func (NullPredicate) isPredicate() {}

// AndPredicate combines two child predicates with AND.
type AndPredicate struct {
	Left  Predicate
	Right Predicate
}

func (AndPredicate) isPredicate() {}

// OrPredicate combines two child predicates with OR.
type OrPredicate struct {
	Left  Predicate
	Right Predicate
}

func (OrPredicate) isPredicate() {}

// TruePredicate is a bare boolean WHERE term that acts as a no-op filter.
// It exists to accept reference-backed shapes such as `WHERE TRUE` without
// inventing a synthetic comparison.
type TruePredicate struct{}

func (TruePredicate) isPredicate() {}

// FalsePredicate is a bare boolean WHERE term that can never emit rows.
// It exists to accept reference-backed shapes such as `WHERE FALSE` without
// inventing a synthetic comparison.
type FalsePredicate struct{}

func (FalsePredicate) isPredicate() {}

// ProjectionColumn is one explicit SELECT-list column on the bounded
// one-relation projection surface.
type ProjectionColumn struct {
	Table           string
	Column          string
	SourceQualifier string
	OutputAlias     string
}

// AggregateProjection is the bounded aggregate surface currently accepted by
// the parser.
type AggregateProjection struct {
	Func string
	// Column is nil for COUNT(*). Non-nil aggregate arguments are resolved
	// after FROM/JOIN relation bindings are known.
	Column   *ColumnRef
	Distinct bool
	Alias    string
}

// OrderByColumn is one bounded query-only ORDER BY term. It is limited to a
// column reference or unqualified projection output name; callers decide which
// protocol surfaces may execute it.
type OrderByColumn struct {
	Table           string
	Column          string
	SourceQualifier string
	Desc            bool
}

// Statement is the parsed output.
// ProjectedAlias and ProjectionColumns preserve projection identity for joins
// and explicit column lists.
type Statement struct {
	Table                 string
	TableAlias            string
	ProjectedTable        string
	ProjectedAlias        string
	ProjectedAliasUnknown bool
	ProjectionColumns     []ProjectionColumn
	Aggregate             *AggregateProjection
	// OrderBy is the first parsed ORDER BY term, retained for compatibility.
	// OrderByColumns preserves the complete ordered term list.
	OrderBy           *OrderByColumn
	OrderByColumns    []OrderByColumn
	Join              *JoinClause
	Joins             []JoinClause
	Predicate         Predicate
	Filters           []Filter
	Limit             *uint64
	HasLimit          bool
	InvalidLimit      *Literal
	UnsupportedLimit  bool
	Offset            *uint64
	HasOffset         bool
	InvalidOffset     *Literal
	UnsupportedOffset bool
}

type relationBindings struct {
	defaultTable   string
	requireQualify bool
	byQualifier    map[string]string
}

// Parse parses the minimum-viable SELECT surface.
func Parse(input string) (Statement, error) {
	if len(input) > maxSQLLength {
		return Statement{}, fmt.Errorf("%w: SQL query exceeds maximum allowed length: %q", ErrUnsupportedSQL, previewSQL(input, 120))
	}
	toks, err := tokenize(input)
	if err != nil {
		return Statement{}, err
	}
	p := &parser{toks: toks, sql: input}
	stmt, err := p.parseStatement()
	if err != nil {
		return Statement{}, err
	}
	return stmt, nil
}

func previewSQL(input string, limit int) string {
	if limit <= 0 || len(input) <= limit {
		return input
	}
	return input[:limit] + "..."
}

// --- tokenizer ---

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokNumber
	tokHex
	tokString
	tokStar
	tokEq
	tokLt
	tokGt
	tokLe
	tokGe
	tokComma
	tokSemicolon
	tokDot
	tokLParen
	tokRParen
	tokParam  // :name parameter placeholder (only :sender accepted downstream)
	tokSymbol // any other single char — always unsupported
)

type token struct {
	kind   tokKind
	text   string // original slice for idents/numbers/symbols; unescaped body for strings / quoted identifiers
	pos    int
	quoted bool
}

func tokenize(s string) ([]token, error) {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '*':
			out = append(out, token{kind: tokStar, text: "*", pos: i})
			i++
		case c == '=':
			out = append(out, token{kind: tokEq, text: "=", pos: i})
			i++
		case c == '<':
			start := i
			if i+1 < len(s) {
				switch s[i+1] {
				case '=':
					out = append(out, token{kind: tokLe, text: "<=", pos: start})
					i += 2
					continue
				case '>':
					out = append(out, token{kind: tokSymbol, text: "<>", pos: start})
					i += 2
					continue
				}
			}
			out = append(out, token{kind: tokLt, text: "<", pos: start})
			i++
		case c == '>':
			start := i
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, token{kind: tokGe, text: ">=", pos: start})
				i += 2
				continue
			}
			out = append(out, token{kind: tokGt, text: ">", pos: start})
			i++
		case c == '!':
			start := i
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, token{kind: tokSymbol, text: "!=", pos: start})
				i += 2
				continue
			}
			out = append(out, token{kind: tokSymbol, text: string(c), pos: i})
			i++
		case c == ',':
			out = append(out, token{kind: tokComma, text: ",", pos: i})
			i++
		case c == ';':
			out = append(out, token{kind: tokSemicolon, text: ";", pos: i})
			i++
		case c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			start := i
			tok, next, err := tokenizeNumeric(s, i, start)
			if err != nil {
				return nil, err
			}
			out = append(out, tok)
			i = next
		case c == '.':
			out = append(out, token{kind: tokDot, text: ".", pos: i})
			i++
		case c == ':':
			start := i
			i++
			nameStart := i
			for i < len(s) && isIdentCont(s[i]) {
				i++
			}
			if i == nameStart {
				return nil, fmt.Errorf("%w: unexpected ':' at position %d", ErrUnsupportedSQL, start)
			}
			out = append(out, token{kind: tokParam, text: s[nameStart:i], pos: start})
		case c == '(':
			out = append(out, token{kind: tokLParen, text: "(", pos: i})
			i++
		case c == ')':
			out = append(out, token{kind: tokRParen, text: ")", pos: i})
			i++
		case c == '\'':
			start := i
			text, next, err := scanDelimitedSQLText(s, i, '\'', "string literal", true)
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tokString, text: text, pos: start})
			i = next
		case c == '"':
			start := i
			text, next, err := scanDelimitedSQLText(s, i, '"', "quoted identifier", false)
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tokIdent, text: text, pos: start, quoted: true})
			i = next
		case c == '0' && i+1 < len(s) && (s[i+1] == 'x' || s[i+1] == 'X'):
			start := i
			i += 2
			hexStart := i
			for i < len(s) && isHexDigit(s[i]) {
				i++
			}
			if i == hexStart {
				return nil, fmt.Errorf("%w: malformed hex literal at position %d", ErrUnsupportedSQL, start)
			}
			if i < len(s) && (isIdentStart(s[i]) || s[i] == '.') {
				return nil, fmt.Errorf("%w: malformed hex literal at position %d", ErrUnsupportedSQL, start)
			}
			out = append(out, token{kind: tokHex, text: s[start:i], pos: start})
		case (c == 'X' || c == 'x') && i+1 < len(s) && s[i+1] == '\'':
			start := i
			i += 2
			hexStart := i
			for i < len(s) && isHexDigit(s[i]) {
				i++
			}
			if i == hexStart || i >= len(s) || s[i] != '\'' {
				return nil, fmt.Errorf("%w: malformed hex literal at position %d", ErrUnsupportedSQL, start)
			}
			i++
			if i < len(s) && (isIdentStart(s[i]) || s[i] == '.') {
				return nil, fmt.Errorf("%w: malformed hex literal at position %d", ErrUnsupportedSQL, start)
			}
			out = append(out, token{kind: tokHex, text: s[start:i], pos: start})
		case c == '-' || c == '+' || (c >= '0' && c <= '9'):
			start := i
			if c == '-' || c == '+' {
				i++
				if i >= len(s) {
					return nil, fmt.Errorf("%w: unexpected '%c' at position %d", ErrUnsupportedSQL, c, start)
				}
				d := s[i]
				leadingDotAfterSign := d == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'
				if !((d >= '0' && d <= '9') || leadingDotAfterSign) {
					return nil, fmt.Errorf("%w: unexpected '%c' at position %d", ErrUnsupportedSQL, c, start)
				}
			}
			tok, next, err := tokenizeNumeric(s, i, start)
			if err != nil {
				return nil, err
			}
			out = append(out, tok)
			i = next
		case isIdentStart(c):
			start := i
			for i < len(s) && isIdentCont(s[i]) {
				i++
			}
			out = append(out, token{kind: tokIdent, text: s[start:i], pos: start})
		default:
			out = append(out, token{kind: tokSymbol, text: string(c), pos: i})
			i++
		}
	}
	out = append(out, token{kind: tokEOF, pos: len(s)})
	return out, nil
}

func scanDelimitedSQLText(s string, start int, quote byte, label string, allowEmpty bool) (string, int, error) {
	i := start + 1
	var sb strings.Builder
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				sb.WriteByte(quote)
				i += 2
				continue
			}
			i++
			if !allowEmpty && sb.Len() == 0 {
				return "", 0, fmt.Errorf("%w: empty %s at position %d", ErrUnsupportedSQL, label, start)
			}
			return sb.String(), i, nil
		}
		sb.WriteByte(s[i])
		i++
	}
	return "", 0, fmt.Errorf("%w: unterminated %s at position %d", ErrUnsupportedSQL, label, start)
}

// tokenizeNumeric consumes a numeric literal body starting at i. `start` is
// the position of the first character of the token (possibly a leading sign
// or a leading `.`). The caller has already validated that the lookahead at
// i is either a digit or a `.digit` leading-dot shape, so the body parses
// integer-part, optional fractional-part, and optional `[eE][+-]?digits`
// exponent. Trailing identifier or `.` characters are rejected as malformed.
func tokenizeNumeric(s string, i, start int) (token, int, error) {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == '.' {
		j := i + 1
		if j >= len(s) || !(s[j] >= '0' && s[j] <= '9') {
			return token{}, 0, fmt.Errorf("%w: malformed numeric literal at position %d", ErrUnsupportedSQL, start)
		}
		i = j + 1
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j >= len(s) || !(s[j] >= '0' && s[j] <= '9') {
			return token{}, 0, fmt.Errorf("%w: malformed numeric literal at position %d", ErrUnsupportedSQL, start)
		}
		i = j + 1
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (isIdentStart(s[i]) || s[i] == '.') {
		return token{}, 0, fmt.Errorf("%w: malformed numeric literal at position %d", ErrUnsupportedSQL, start)
	}
	return token{kind: tokNumber, text: s[start:i], pos: start}, i, nil
}

func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isIdentifierToken(t token) bool {
	return t.kind == tokIdent && (t.quoted || !isReserved(t.text))
}

func isKeywordToken(t token, kw string) bool {
	return !t.quoted && t.kind == tokIdent && strings.EqualFold(t.text, kw)
}

func isLiteralKeywordToken(t token) bool {
	return isKeywordToken(t, "TRUE") ||
		isKeywordToken(t, "FALSE") ||
		isKeywordToken(t, "NULL")
}

func isJoinModifierToken(t token) bool {
	return isKeywordToken(t, "INNER") ||
		isKeywordToken(t, "CROSS") ||
		isUnsupportedJoinStartToken(t)
}

func isUnsupportedJoinStartToken(t token) bool {
	return isKeywordToken(t, "LEFT") ||
		isKeywordToken(t, "RIGHT") ||
		isKeywordToken(t, "FULL") ||
		isKeywordToken(t, "OUTER") ||
		isKeywordToken(t, "NATURAL")
}

// --- parser ---

type parser struct {
	toks []token
	pos  int
	// sql holds the original input string passed to Parse. Used by
	// rejection arms whose reference text is the offending SELECT
	// rendered verbatim (e.g. UnsupportedSelectError for the
	// `SELECT ALL ...` / `SELECT DISTINCT ...` set quantifiers).
	sql string
}

func (p *parser) peek() token    { return p.toks[p.pos] }
func (p *parser) advance() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) peekNext() token {
	if p.pos+1 >= len(p.toks) {
		return token{kind: tokEOF}
	}
	return p.toks[p.pos+1]
}

func (p *parser) hasLimitClauseAhead() bool {
	seenFrom := false
	for i := p.pos + 1; i < len(p.toks); i++ {
		if p.toks[i].kind == tokEOF || p.toks[i].kind == tokSemicolon {
			return false
		}
		if isKeywordToken(p.toks[i], "FROM") {
			seenFrom = true
			continue
		}
		if isKeywordToken(p.toks[i], "LIMIT") {
			return seenFrom
		}
	}
	return false
}

func (p *parser) parseStatement() (Statement, error) {
	if err := p.expectKeyword("SELECT"); err != nil {
		return Statement{}, err
	}
	projectionQualifier, projectionColumns, aggregate, err := p.parseProjection()
	if err != nil {
		return Statement{}, err
	}
	if err := p.expectKeyword("FROM"); err != nil {
		return Statement{}, err
	}
	tableTok := p.peek()
	if !isIdentifierToken(tableTok) {
		return Statement{}, p.unsupported("expected table name")
	}
	p.advance()
	tableName := tableTok.text
	leftQualifiers, err := p.parseRelationQualifiers(tableName)
	if err != nil {
		return Statement{}, err
	}
	stmt := Statement{Table: tableName, TableAlias: leftQualifiers[0], ProjectedTable: tableName, ProjectedAlias: projectionQualifier}
	bindings := relationBindings{defaultTable: tableName, byQualifier: singleQualifierMap(tableName, leftQualifiers)}
	for {
		if isKeywordToken(p.peek(), "INNER") {
			p.advance()
			if !isKeywordToken(p.peek(), "JOIN") {
				return Statement{}, p.unsupported("expected JOIN after INNER")
			}
		} else if isKeywordToken(p.peek(), "CROSS") {
			p.advance()
			if !isKeywordToken(p.peek(), "JOIN") {
				return Statement{}, p.unsupported("expected JOIN after CROSS")
			}
		} else if isUnsupportedJoinStartToken(p.peek()) {
			return Statement{}, UnsupportedJoinTypeError{}
		} else if !isKeywordToken(p.peek(), "JOIN") {
			break
		}
		join, rightQualifiers, err := p.parseJoinClause(bindings, tableName, leftQualifiers[0])
		if err != nil {
			return Statement{}, err
		}
		if stmt.Join == nil {
			stmt.Join = join
		}
		stmt.Joins = append(stmt.Joins, *join)
		bindings.requireQualify = true
		addRelationQualifiers(bindings.byQualifier, join.RightTable, rightQualifiers)
	}
	if len(stmt.Joins) != 0 {
		if projectionQualifier != "" {
			if projectedTable, ok := bindings.byQualifier[projectionQualifier]; ok {
				stmt.ProjectedTable = projectedTable
			} else {
				stmt.ProjectedTable = projectionQualifier
				stmt.ProjectedAliasUnknown = true
			}
			stmt.ProjectedAlias = projectionQualifier
		}
	} else if projectionQualifier != "" && !slices.Contains(leftQualifiers, projectionQualifier) {
		stmt.ProjectedAliasUnknown = true
	}
	if aggregate != nil {
		resolvedAggregate, err := resolveAggregateProjection(aggregate, bindings)
		if err != nil {
			return Statement{}, err
		}
		stmt.Aggregate = resolvedAggregate
	}
	if len(projectionColumns) != 0 {
		resolvedProjectionColumns, err := resolveProjectionColumns(projectionColumns, bindings)
		if err != nil {
			return Statement{}, err
		}
		if stmt.Join != nil && stmt.ProjectedAlias == "" {
			stmt.ProjectedAlias = resolvedProjectionColumns[0].SourceQualifier
			stmt.ProjectedTable = resolvedProjectionColumns[0].Table
		}
		stmt.ProjectionColumns = resolvedProjectionColumns
	}
	if isKeywordToken(p.peek(), "WHERE") {
		p.advance()
		pred, filters, err := p.parseWhere(bindings)
		if err != nil {
			return Statement{}, err
		}
		stmt.Predicate = pred
		stmt.Filters = filters
	}
	orderByColumns, err := p.parseOrderBy(bindings, len(stmt.ProjectionColumns) != 0 || stmt.Aggregate != nil)
	if err != nil {
		return Statement{}, err
	}
	stmt.OrderByColumns = orderByColumns
	if len(stmt.OrderByColumns) != 0 {
		stmt.OrderBy = &stmt.OrderByColumns[0]
	}
	limit, invalidLimit, hasLimit, unsupportedLimit, err := p.parseUnsignedClause("LIMIT")
	if err != nil {
		return Statement{}, err
	}
	stmt.Limit = limit
	stmt.InvalidLimit = invalidLimit
	stmt.HasLimit = hasLimit
	stmt.UnsupportedLimit = unsupportedLimit
	offset, invalidOffset, hasOffset, unsupportedOffset, err := p.parseUnsignedClause("OFFSET")
	if err != nil {
		return Statement{}, err
	}
	stmt.Offset = offset
	stmt.InvalidOffset = invalidOffset
	stmt.HasOffset = hasOffset
	stmt.UnsupportedOffset = unsupportedOffset
	if p.peek().kind == tokSemicolon {
		p.advance()
	}
	if p.peek().kind != tokEOF {
		return Statement{}, p.unsupported(fmt.Sprintf("unexpected token %q", p.peek().text))
	}
	return stmt, nil
}

func (p *parser) parseProjection() (string, []ProjectionColumn, *AggregateProjection, error) {
	t := p.peek()
	// Reject SELECT ALL/DISTINCT before treating the keyword as a column name.
	if !t.quoted && t.kind == tokIdent && p.peekNext().kind != tokDot && (strings.EqualFold(t.text, "ALL") || strings.EqualFold(t.text, "DISTINCT")) {
		return "", nil, nil, UnsupportedSelectError{SQL: p.sql, HasLimit: p.hasLimitClauseAhead()}
	}
	if t.kind == tokStar {
		p.advance()
		if p.peek().kind == tokComma {
			return "", nil, nil, p.unsupported("cannot mix '*' with explicit projection columns")
		}
		return "", nil, nil, nil
	}
	if p.isAggregateProjectionStart(t) {
		agg, err := p.parseAggregateProjection()
		if err != nil {
			return "", nil, nil, err
		}
		if p.peek().kind == tokComma {
			return "", nil, nil, p.unsupported("cannot mix aggregate and non-aggregate projections")
		}
		return "", nil, agg, nil
	}
	col, qualifier, err := p.parseProjectionItem()
	if err != nil {
		return "", nil, nil, err
	}
	if qualifier != "" {
		if p.peek().kind == tokComma {
			return "", nil, nil, p.unsupported("cannot mix 'table.*' with explicit projection columns")
		}
		return qualifier, nil, nil, nil
	}
	cols := []ProjectionColumn{col}
	for p.peek().kind == tokComma {
		p.advance()
		col, qualifier, err := p.parseProjectionItem()
		if err != nil {
			return "", nil, nil, err
		}
		if qualifier != "" {
			return "", nil, nil, p.unsupported("cannot mix wildcard and explicit projection columns")
		}
		cols = append(cols, col)
	}
	return "", cols, nil, nil
}

func (p *parser) isAggregateProjectionStart(t token) bool {
	if !isIdentifierToken(t) || p.pos+1 >= len(p.toks) || p.toks[p.pos+1].kind != tokLParen {
		return false
	}
	return strings.EqualFold(t.text, "COUNT") || strings.EqualFold(t.text, "SUM")
}

func (p *parser) parseAggregateProjection() (*AggregateProjection, error) {
	fn := p.peek()
	if !isIdentifierToken(fn) {
		return nil, p.unsupported("aggregate projections not supported")
	}
	funcName := strings.ToUpper(fn.text)
	if funcName != "COUNT" && funcName != "SUM" {
		return nil, p.unsupported("aggregate projections not supported")
	}
	p.advance()
	if p.peek().kind != tokLParen {
		return nil, p.unsupported("aggregate projections not supported")
	}
	p.advance()
	distinct := false
	if isKeywordToken(p.peek(), "DISTINCT") {
		if funcName != "COUNT" {
			return nil, p.unsupported("only COUNT(DISTINCT column) aggregate projections supported")
		}
		distinct = true
		p.advance()
	}
	var column *ColumnRef
	if p.peek().kind == tokStar {
		if distinct {
			return nil, p.unsupported("COUNT(DISTINCT *) aggregate projections not supported")
		}
		if funcName != "COUNT" {
			return nil, p.unsupported("SUM(*) aggregate projections not supported")
		}
		p.advance()
	} else if isIdentifierToken(p.peek()) {
		ref, err := p.parseAggregateColumnRef()
		if err != nil {
			return nil, err
		}
		column = &ref
	} else {
		return nil, p.unsupported("only COUNT(*), COUNT(column), COUNT(DISTINCT column), or SUM(column) aggregate projections supported")
	}
	if p.peek().kind != tokRParen {
		return nil, p.unsupported("only COUNT(*), COUNT(column), COUNT(DISTINCT column), or SUM(column) aggregate projections supported")
	}
	p.advance()
	if isKeywordToken(p.peek(), "AS") {
		p.advance()
	}
	if !isIdentifierToken(p.peek()) {
		return nil, p.unsupported("aggregate projections require alias")
	}
	alias, err := p.parseAlias()
	if err != nil {
		return nil, err
	}
	return &AggregateProjection{Func: funcName, Column: column, Distinct: distinct, Alias: alias}, nil
}

func (p *parser) parseAggregateColumnRef() (ColumnRef, error) {
	t := p.peek()
	if !isIdentifierToken(t) {
		return ColumnRef{}, p.unsupported("expected aggregate column name")
	}
	p.advance()
	return p.parseColumnRefAfterIdentifier(t.text)
}

func (p *parser) parseColumnRefAfterIdentifier(columnName string) (ColumnRef, error) {
	if p.peek().kind != tokDot {
		return ColumnRef{Column: columnName}, nil
	}
	qualifier := columnName
	p.advance()
	t := p.peek()
	if !isIdentifierToken(t) {
		return ColumnRef{}, p.unsupported(fmt.Sprintf("expected column name after qualifier %q", qualifier))
	}
	p.advance()
	if p.peek().kind == tokDot {
		return ColumnRef{}, p.unsupported("qualified column names not supported")
	}
	return ColumnRef{Table: qualifier, Column: t.text, Alias: qualifier}, nil
}

func (p *parser) parseProjectionItem() (ProjectionColumn, string, error) {
	t := p.peek()
	if !isIdentifierToken(t) {
		return ProjectionColumn{}, "", p.unsupported("projection must be '*', 'table.*', or a single-table column list")
	}
	p.advance()
	ident := t.text
	if p.peek().kind == tokLParen {
		return ProjectionColumn{}, "", p.unsupported("aggregate projections not supported")
	}
	if p.peek().kind != tokDot {
		alias, err := p.parseProjectionOutputAlias()
		if err != nil {
			return ProjectionColumn{}, "", err
		}
		return ProjectionColumn{Column: ident, OutputAlias: alias}, "", nil
	}
	p.advance()
	t = p.peek()
	if t.kind == tokStar {
		p.advance()
		return ProjectionColumn{}, ident, nil
	}
	if !isIdentifierToken(t) {
		return ProjectionColumn{}, "", p.unsupported(fmt.Sprintf("expected column name after qualifier %q", ident))
	}
	p.advance()
	if p.peek().kind == tokDot {
		return ProjectionColumn{}, "", p.unsupported("qualified column names not supported")
	}
	alias, err := p.parseProjectionOutputAlias()
	if err != nil {
		return ProjectionColumn{}, "", err
	}
	return ProjectionColumn{Column: t.text, SourceQualifier: ident, OutputAlias: alias}, "", nil
}

func (p *parser) parseProjectionOutputAlias() (string, error) {
	if isKeywordToken(p.peek(), "AS") {
		p.advance()
		return p.parseAlias()
	}
	if isIdentifierToken(p.peek()) {
		return p.parseAlias()
	}
	return "", nil
}

func resolveProjectionColumns(columns []ProjectionColumn, bindings relationBindings) ([]ProjectionColumn, error) {
	resolved := make([]ProjectionColumn, 0, len(columns))
	for _, col := range columns {
		tableName := bindings.defaultTable
		qualifier := col.SourceQualifier
		if qualifier != "" {
			tableName = resolveRelationQualifier(bindings, qualifier)
		} else if bindings.requireQualify {
			// Reference `SqlSelect::find_unqualified_vars`
			// (sql-parser/src/ast/sql.rs:84-95) routes any unqualified
			// var in a JOIN scope through
			// `SqlUnsupported::UnqualifiedNames`
			// (parser/errors.rs:78-79). The projection branch fires
			// when a join projection column has no qualifier.
			return nil, UnqualifiedNamesError{}
		}
		resolved = append(resolved, ProjectionColumn{Table: tableName, Column: col.Column, SourceQualifier: qualifier, OutputAlias: col.OutputAlias})
	}
	return resolved, nil
}

func resolveAggregateProjection(agg *AggregateProjection, bindings relationBindings) (*AggregateProjection, error) {
	if agg == nil || agg.Column == nil {
		return agg, nil
	}
	ref := *agg.Column
	if ref.Alias != "" {
		ref.Table = resolveRelationQualifier(bindings, ref.Alias)
	} else if bindings.requireQualify {
		return nil, UnqualifiedNamesError{}
	} else {
		ref.Table = bindings.defaultTable
	}
	out := *agg
	out.Column = &ref
	return &out, nil
}

func (p *parser) parseRelationQualifiers(tableName string) ([]string, error) {
	if isKeywordToken(p.peek(), "AS") {
		p.advance()
		alias, err := p.parseAlias()
		if err != nil {
			return nil, err
		}
		return []string{alias}, nil
	}
	if isJoinModifierToken(p.peek()) {
		return []string{tableName}, nil
	}
	if isIdentifierToken(p.peek()) {
		alias, err := p.parseAlias()
		if err != nil {
			return nil, err
		}
		return []string{alias}, nil
	}
	return []string{tableName}, nil
}

func (p *parser) parseAlias() (string, error) {
	t := p.peek()
	if !isIdentifierToken(t) {
		return "", p.unsupported("expected alias name")
	}
	p.advance()
	return t.text, nil
}

func (p *parser) parseJoinClause(bindings relationBindings, fallbackLeftTable string, fallbackLeftAlias string) (*JoinClause, []string, error) {
	if err := p.expectKeyword("JOIN"); err != nil {
		return nil, nil, err
	}
	rightTok := p.peek()
	if !isIdentifierToken(rightTok) {
		return nil, nil, p.unsupported("expected joined table name")
	}
	p.advance()
	rightTable := rightTok.text
	rightQualifiers, err := p.parseRelationQualifiers(rightTable)
	if err != nil {
		return nil, nil, err
	}
	rightAlias := rightQualifiers[0]
	if qualifierCollides(bindings.byQualifier, rightQualifiers) {
		// Defer duplicate-alias rejection so compile-time table lookup ordering
		// can report a missing left table first.
		p.consumeUntilStatementEnd()
		return &JoinClause{
			LeftTable:      fallbackLeftTable,
			RightTable:     rightTable,
			LeftAlias:      fallbackLeftAlias,
			RightAlias:     rightAlias,
			HasOn:          false,
			AliasCollision: true,
		}, rightQualifiers, nil
	}
	if !isKeywordToken(p.peek(), "ON") {
		return &JoinClause{LeftTable: fallbackLeftTable, RightTable: rightTable, LeftAlias: fallbackLeftAlias, RightAlias: rightAlias, HasOn: false}, rightQualifiers, nil
	}
	if err := p.expectKeyword("ON"); err != nil {
		return nil, nil, err
	}
	lookup := maps.Clone(bindings.byQualifier)
	addRelationQualifiers(lookup, rightTable, rightQualifiers)
	leftOn, err := p.parseQualifiedColumnRef(lookup)
	if err != nil {
		return nil, nil, err
	}
	op, err := p.parseOperator()
	if err != nil {
		return nil, nil, err
	}
	if op != "=" {
		return nil, nil, UnsupportedJoinTypeError{}
	}
	rightOn, err := p.parseQualifiedColumnRef(lookup)
	if err != nil {
		return nil, nil, err
	}
	_, leftExisting := bindings.byQualifier[leftOn.Alias]
	_, rightExisting := bindings.byQualifier[rightOn.Alias]
	leftNew := slices.Contains(rightQualifiers, leftOn.Alias)
	rightNew := slices.Contains(rightQualifiers, rightOn.Alias)
	leftKnown := leftExisting || leftNew
	rightKnown := rightExisting || rightNew
	if leftKnown && rightKnown {
		if leftOn.Alias == rightOn.Alias {
			return nil, nil, p.unsupported("JOIN ON must compare columns from different relations")
		}
		if leftNew && rightExisting {
			leftOn, rightOn = rightOn, leftOn
			leftExisting, rightExisting = rightExisting, leftExisting
			leftNew, rightNew = rightNew, leftNew
		}
		if !leftExisting || !rightNew || leftNew || rightExisting {
			return nil, nil, p.unsupported("JOIN ON must compare an existing relation to the joined relation")
		}
	}
	leftTable := fallbackLeftTable
	leftAlias := fallbackLeftAlias
	if leftExisting {
		leftTable = bindings.byQualifier[leftOn.Alias]
		leftAlias = leftOn.Alias
	}
	jc := &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: true, LeftOn: leftOn, RightOn: rightOn}
	if isKeywordToken(p.peek(), "AND") || isKeywordToken(p.peek(), "OR") {
		return nil, nil, UnsupportedJoinTypeError{}
	}
	return jc, rightQualifiers, nil
}

func (p *parser) parseQualifiedColumnRef(lookup map[string]string) (ColumnRef, error) {
	qualifierTok := p.peek()
	if !isIdentifierToken(qualifierTok) {
		return ColumnRef{}, p.unsupported("expected qualified column reference")
	}
	p.advance()
	if p.peek().kind != tokDot {
		// Reference `SqlSelect::find_unqualified_vars`
		// (sql-parser/src/ast/sql.rs:84-95) routes any unqualified var
		// in a JOIN scope through `SqlUnsupported::UnqualifiedNames`
		// (parser/errors.rs:78-79). Bare-identifier ON-operand —
		// `JOIN s ON id = s.id` parses an `Expr::Identifier` on the
		// left, which `find_unqualified_vars` flags.
		return ColumnRef{}, UnqualifiedNamesError{}
	}
	p.advance()
	columnTok := p.peek()
	if !isIdentifierToken(columnTok) {
		return ColumnRef{}, p.unsupported("expected column name after qualifier")
	}
	p.advance()
	tableName := resolveQualifier(lookup, qualifierTok.text)
	return ColumnRef{Table: tableName, Column: columnTok.text, Alias: qualifierTok.text}, nil
}

func (p *parser) parseWhere(bindings relationBindings) (Predicate, []Filter, error) {
	pred, err := p.parseDisjunction(bindings)
	if err != nil {
		return nil, nil, err
	}
	if !p.peek().quoted && p.peek().kind == tokIdent {
		kw := strings.ToUpper(p.peek().text)
		if kw == "GROUP" || kw == "HAVING" || kw == "JOIN" {
			return nil, nil, p.unsupported(fmt.Sprintf("%s not supported", kw))
		}
	}
	filters, _ := flattenAndFilters(pred)
	return pred, filters, nil
}

func (p *parser) parseOrderBy(bindings relationBindings, allowUnqualifiedOutputName bool) ([]OrderByColumn, error) {
	if !isKeywordToken(p.peek(), "ORDER") {
		return nil, nil
	}
	p.advance()
	if err := p.expectKeyword("BY"); err != nil {
		return nil, err
	}
	var columns []OrderByColumn
	for {
		ref, err := p.parseColumnRefForOrderBy(bindings, allowUnqualifiedOutputName)
		if err != nil {
			return nil, err
		}
		desc := false
		if isKeywordToken(p.peek(), "ASC") {
			p.advance()
		} else if isKeywordToken(p.peek(), "DESC") {
			p.advance()
			desc = true
		}
		columns = append(columns, OrderByColumn{
			Table:           ref.Table,
			Column:          ref.Column,
			SourceQualifier: ref.Alias,
			Desc:            desc,
		})
		if p.peek().kind != tokComma {
			break
		}
		p.advance()
	}
	return columns, nil
}

func (p *parser) parseColumnRefForOrderBy(bindings relationBindings, allowUnqualifiedOutputName bool) (ColumnRef, error) {
	return p.parseColumnRef(bindings, "expected ORDER BY column name, got %q", allowUnqualifiedOutputName)
}

func (p *parser) parseColumnRef(bindings relationBindings, firstTokenError string, allowUnqualifiedInJoin bool) (ColumnRef, error) {
	t := p.peek()
	if !isIdentifierToken(t) {
		return ColumnRef{}, p.unsupported(fmt.Sprintf(firstTokenError, t.text))
	}
	p.advance()
	ref, err := p.parseColumnRefAfterIdentifier(t.text)
	if err != nil {
		return ColumnRef{}, err
	}
	if ref.Alias != "" {
		ref.Table = resolveRelationQualifier(bindings, ref.Alias)
	} else if bindings.requireQualify {
		if allowUnqualifiedInJoin {
			return ref, nil
		}
		return ColumnRef{}, UnqualifiedNamesError{}
	} else {
		ref.Table = bindings.defaultTable
	}
	return ref, nil
}

func resolveRelationQualifier(bindings relationBindings, qualifier string) string {
	return resolveQualifier(bindings.byQualifier, qualifier)
}

func resolveQualifier(lookup map[string]string, qualifier string) string {
	if resolved, ok := lookup[qualifier]; ok {
		return resolved
	}
	return qualifier
}

func (p *parser) parseUnsignedClause(keyword string) (*uint64, *Literal, bool, bool, error) {
	if !isKeywordToken(p.peek(), keyword) {
		return nil, nil, false, false, nil
	}
	p.advance()
	t := p.peek()
	if t.kind != tokNumber {
		p.consumeUntilStatementEnd()
		return nil, nil, true, true, nil
	}
	p.advance()
	if strings.HasPrefix(t.text, "+") {
		return nil, nil, true, true, nil
	}
	if strings.HasPrefix(t.text, "-") {
		return nil, nil, true, true, nil
	}
	lit, err := parseNumericLiteral(t.text)
	if err != nil {
		return nil, nil, false, false, err
	}
	switch lit.Kind {
	case LitInt:
		if lit.Int < 0 {
			return nil, &lit, true, false, nil
		}
		limit := uint64(lit.Int)
		return &limit, nil, true, false, nil
	case LitBigInt:
		if lit.Big.Sign() < 0 || !lit.Big.IsUint64() {
			return nil, &lit, true, false, nil
		}
		limit := lit.Big.Uint64()
		return &limit, nil, true, false, nil
	default:
		return nil, &lit, true, false, nil
	}
}

func (p *parser) parseDisjunction(bindings relationBindings) (Predicate, error) {
	return p.parseBinaryPredicate(bindings, "OR", p.parseConjunction, func(left, right Predicate) Predicate {
		return OrPredicate{Left: left, Right: right}
	})
}

func (p *parser) parseConjunction(bindings relationBindings) (Predicate, error) {
	return p.parseBinaryPredicate(bindings, "AND", p.parsePredicateTerm, func(left, right Predicate) Predicate {
		return AndPredicate{Left: left, Right: right}
	})
}

func (p *parser) parseBinaryPredicate(
	bindings relationBindings,
	operator string,
	parseOperand func(relationBindings) (Predicate, error),
	join func(Predicate, Predicate) Predicate,
) (Predicate, error) {
	left, err := parseOperand(bindings)
	if err != nil {
		return nil, err
	}
	for isKeywordToken(p.peek(), operator) {
		p.advance()
		right, err := parseOperand(bindings)
		if err != nil {
			return nil, err
		}
		left = join(left, right)
	}
	return left, nil
}

func (p *parser) parsePredicateTerm(bindings relationBindings) (Predicate, error) {
	if p.peek().kind == tokLParen {
		p.advance()
		pred, err := p.parseDisjunction(bindings)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, p.unsupported("expected ')' to close parenthesized predicate")
		}
		p.advance()
		return pred, nil
	}
	if t := p.peek(); t.kind == tokIdent && !t.quoted {
		if strings.EqualFold(t.text, "TRUE") {
			p.advance()
			return TruePredicate{}, nil
		}
		if strings.EqualFold(t.text, "FALSE") {
			p.advance()
			return FalsePredicate{}, nil
		}
	}
	return p.parseComparisonPredicate(bindings)
}

func (p *parser) parseComparisonPredicate(bindings relationBindings) (Predicate, error) {
	left, err := p.parseColumnRefForPredicate(bindings)
	if err != nil {
		return nil, err
	}
	if isKeywordToken(p.peek(), "IS") {
		p.advance()
		not := false
		if isKeywordToken(p.peek(), "NOT") {
			p.advance()
			not = true
		}
		if !isKeywordToken(p.peek(), "NULL") {
			return nil, p.unsupported("expected NULL after IS")
		}
		p.advance()
		return NullPredicate{Column: left, Not: not}, nil
	}
	op, err := p.parseOperator()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == tokIdent && p.peekNext().kind == tokDot {
		if !bindings.requireQualify {
			return nil, p.unsupported("column-vs-column WHERE predicates are only supported in join contexts")
		}
		if op != "=" {
			return nil, p.unsupported("join WHERE column comparisons only support '='")
		}
		right, err := p.parseColumnRefForPredicate(bindings)
		if err != nil {
			return nil, err
		}
		return ColumnComparisonPredicate{Left: left, Op: op, Right: right}, nil
	}
	if bindings.requireQualify && p.peek().kind == tokIdent && !isLiteralKeywordToken(p.peek()) {
		// Reference `SqlSelect::find_unqualified_vars`
		// (sql-parser/src/ast/sql.rs:84-95) routes any unqualified var
		// in a JOIN scope through `SqlUnsupported::UnqualifiedNames`
		// (parser/errors.rs:78-79). RHS-of-WHERE-comparison branch.
		return nil, UnqualifiedNamesError{}
	}
	if isKeywordToken(p.peek(), "NULL") {
		return nil, p.unsupported("NULL comparisons must use IS NULL or IS NOT NULL")
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return ComparisonPredicate{Filter: Filter{Table: left.Table, Column: left.Column, Alias: left.Alias, Op: op, Literal: lit}}, nil
}

func flattenAndFilters(pred Predicate) ([]Filter, bool) {
	switch p := pred.(type) {
	case nil:
		return nil, true
	case TruePredicate:
		return nil, true
	case FalsePredicate:
		return nil, false
	case ComparisonPredicate:
		return []Filter{p.Filter}, true
	case NullPredicate:
		return nil, false
	case AndPredicate:
		left, ok := flattenAndFilters(p.Left)
		if !ok {
			return nil, false
		}
		right, ok := flattenAndFilters(p.Right)
		if !ok {
			return nil, false
		}
		return append(left, right...), true
	default:
		return nil, false
	}
}

func (p *parser) parseColumnRefForPredicate(bindings relationBindings) (ColumnRef, error) {
	return p.parseColumnRef(bindings, "expected column name, got %q", false)
}

func (p *parser) parseOperator() (string, error) {
	switch t := p.peek(); t.kind {
	case tokEq, tokLt, tokGt, tokLe, tokGe:
		p.advance()
		return t.text, nil
	case tokSymbol:
		if t.text != "!=" && t.text != "<>" {
			return "", p.unsupported(fmt.Sprintf("expected comparison operator, got %q", t.text))
		}
		p.advance()
		return t.text, nil
	default:
		return "", p.unsupported(fmt.Sprintf("expected comparison operator, got %q", t.text))
	}
}

func (p *parser) parseLiteral() (Literal, error) {
	t := p.peek()
	switch t.kind {
	case tokNumber:
		p.advance()
		return parseNumericLiteral(t.text)
	case tokHex:
		p.advance()
		b, err := parseHexLiteral(t.text)
		if err != nil {
			return Literal{}, err
		}
		return Literal{Kind: LitBytes, Bytes: b, Text: t.text}, nil
	case tokString:
		p.advance()
		return Literal{Kind: LitString, Str: t.text, Text: t.text}, nil
	case tokIdent:
		if !t.quoted && strings.EqualFold(t.text, "TRUE") {
			p.advance()
			return Literal{Kind: LitBool, Bool: true}, nil
		}
		if !t.quoted && strings.EqualFold(t.text, "FALSE") {
			p.advance()
			return Literal{Kind: LitBool, Bool: false}, nil
		}
		if !t.quoted && strings.EqualFold(t.text, "NULL") {
			return Literal{}, p.unsupported("NULL literal not supported")
		}
		return Literal{}, p.unsupported(fmt.Sprintf("expected literal, got identifier %q", t.text))
	case tokParam:
		// Reference `parse_expr`
		// (sql-parser/src/parser/mod.rs:223) only accepts the exact
		// byte-equal placeholder `:sender`; any other placeholder
		// (different casing, unknown name) falls through to
		// `_ => SqlUnsupported::Expr(expr)` (line 270), which renders
		// `Unsupported expression: {expr}` via parser/errors.rs:38-39.
		if t.text != "sender" {
			return Literal{}, UnsupportedExprError{Expr: ":" + t.text}
		}
		p.advance()
		return Literal{Kind: LitSender}, nil
	default:
		return Literal{}, p.unsupported(fmt.Sprintf("expected literal, got %q", t.text))
	}
}

// parseNumericLiteral converts a numeric token into int, bigint, or float form.
// Integer-valued scientific notation stays exact when possible.
func parseNumericLiteral(text string) (Literal, error) {
	if strings.ContainsAny(text, ".eE") {
		r, ok := new(big.Rat).SetString(text)
		if ok && r.IsInt() {
			n := r.Num()
			if n.IsInt64() {
				return Literal{Kind: LitInt, Int: n.Int64(), Text: text}, nil
			}
			return Literal{Kind: LitBigInt, Big: new(big.Int).Set(n), Text: text}, nil
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return Literal{}, fmt.Errorf("%w: numeric literal %q out of range", ErrUnsupportedSQL, text)
		}
		if !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) && f >= math.MinInt64 && f <= math.MaxInt64 {
			return Literal{Kind: LitInt, Int: int64(f), Text: text}, nil
		}
		return Literal{Kind: LitFloat, Float: f, Text: text}, nil
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return Literal{Kind: LitInt, Int: n, Text: text}, nil
	}
	b, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return Literal{}, fmt.Errorf("%w: integer literal %q out of range", ErrUnsupportedSQL, text)
	}
	return Literal{Kind: LitBigInt, Big: b, Text: text}, nil
}

func (p *parser) expectKeyword(kw string) error {
	t := p.peek()
	if t.kind == tokEOF {
		return p.unsupported(fmt.Sprintf("expected %s, got end of input", kw))
	}
	if !isKeywordToken(t, kw) {
		return p.unsupported(fmt.Sprintf("expected %s, got %q", kw, t.text))
	}
	p.advance()
	return nil
}

func (p *parser) unsupported(msg string) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedSQL, msg)
}

// consumeUntilStatementEnd advances the parser past every remaining token in
// the current statement (any optional trailing semicolon and the EOF marker
// are left in place for the outer parseStatement EOF guard). Used after a
// deferred parser rejection (alias collision) to keep the outer EOF check
// from emitting an `unexpected token` parse error before the deferred
// compile-stage rejection has a chance to fire.
func (p *parser) consumeUntilStatementEnd() {
	for {
		k := p.peek().kind
		if k == tokEOF || k == tokSemicolon {
			return
		}
		p.advance()
	}
}

var reservedWords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "AND": {}, "OR": {},
	"ORDER": {}, "BY": {}, "LIMIT": {}, "OFFSET": {}, "GROUP": {}, "HAVING": {},
	"JOIN": {}, "ON": {}, "AS": {}, "INNER": {},
	"TRUE": {}, "FALSE": {}, "NULL": {},
}

func isReserved(s string) bool {
	_, ok := reservedWords[strings.ToUpper(s)]
	return ok
}

func singleQualifierMap(tableName string, qualifiers []string) map[string]string {
	out := make(map[string]string, len(qualifiers))
	for _, qualifier := range qualifiers {
		out[qualifier] = tableName
	}
	return out
}

func addRelationQualifiers(lookup map[string]string, tableName string, qualifiers []string) {
	for _, qualifier := range qualifiers {
		lookup[qualifier] = tableName
	}
}

func qualifierCollides(lookup map[string]string, qualifiers []string) bool {
	for _, qualifier := range qualifiers {
		if _, ok := lookup[qualifier]; ok {
			return true
		}
	}
	return false
}
