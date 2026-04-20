// Package sql implements the minimum-viable SQL surface the Shunter
// protocol layer accepts on OneOffQuery / SubscribeSingle / SubscribeMulti.
//
// Grammar:
//
//	stmt   = "SELECT" ( "*" | ident "." "*" ) "FROM" ident [ [ "AS" ] ident ] [ [ "INNER" ] "JOIN" ident [ [ "AS" ] ident ] "ON" qcol "=" qcol ] [ where ] [ ";" ]
//	where  = "WHERE" cmp ( ( "AND" | "OR" ) cmp )*
//	cmp    = colref op literal
//	colref = ident | ident "." ident
//	qcol   = ident "." ident
//	op     = "=" | "<" | ">" | "<=" | ">=" | "!=" | "<>"
//	literal = integer | bool | string
//	ident   = [A-Za-z_][A-Za-z0-9_]*
//
// Anything outside this grammar (projection other than "*", unsupported JOIN forms,
// ORDER BY, LIMIT, aggregates,
// subqueries, mismatched qualified columns, etc.) is rejected with
// ErrUnsupportedSQL.
//
// Literals carry their lexical category only. Callers coerce them to a
// concrete types.Value against a column kind via Coerce.
package sql

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrUnsupportedSQL is the sentinel for any input outside the grammar.
// Wrap with fmt.Errorf("%w: ...", ErrUnsupportedSQL, ...) for specifics.
var ErrUnsupportedSQL = errors.New("unsupported SQL")

// LitKind tags a parsed literal's lexical category.
type LitKind int

const (
	LitInt LitKind = iota
	LitBool
	LitString
)

// Literal is a parsed SQL literal in raw lexical form.
type Literal struct {
	Kind LitKind
	Int  int64
	Bool bool
	Str  string
}

// Filter is a single column comparison against a literal value.
//
// Alias preserves the exact qualifier token the user wrote (e.g. "a", "b",
// or the base table name in an unaliased single-table query). Compile paths
// map it to a relation-instance tag so self-join WHERE filters can be routed
// to the side the user named. Empty means the column reference was bare and
// the caller may fall back to the default relation.
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

// JoinClause is the parsed two-table join metadata for a narrow join-backed SQL slice.
// LeftAlias / RightAlias carry the relation-instance identity separately from the
// physical table name so callers can detect aliased self-joins.
type JoinClause struct {
	LeftTable  string
	RightTable string
	LeftAlias  string
	RightAlias string
	HasOn      bool
	LeftOn     ColumnRef
	RightOn    ColumnRef
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

// Statement is the parsed output.
//
// ProjectedAlias preserves the qualifier token the user wrote for the
// SELECT target (`a` in `SELECT a.*`, `Orders` in `SELECT Orders.*`,
// or empty when the projection was bare `*`). Compile paths consult it to
// distinguish `SELECT a.*` from `SELECT b.*` on aliased self-joins, where
// ProjectedTable alone is insufficient because both aliases resolve to the
// same base table.
type Statement struct {
	Table           string
	ProjectedTable  string
	ProjectedAlias  string
	Join            *JoinClause
	Predicate       Predicate
	Filters         []Filter
}

type relationBindings struct {
	defaultTable   string
	requireQualify bool
	byQualifier    map[string]string
}

// Parse parses the minimum-viable SELECT surface.
func Parse(input string) (Statement, error) {
	toks, err := tokenize(input)
	if err != nil {
		return Statement{}, err
	}
	p := &parser{toks: toks}
	stmt, err := p.parseStatement()
	if err != nil {
		return Statement{}, err
	}
	return stmt, nil
}

// --- tokenizer ---

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokNumber
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
	tokSymbol // any other single char — always unsupported
)

type token struct {
	kind tokKind
	text string // original slice for idents/numbers/symbols; unescaped body for strings
	pos  int
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
		case c == '.':
			out = append(out, token{kind: tokDot, text: ".", pos: i})
			i++
		case c == '\'':
			start := i
			i++
			var sb strings.Builder
			closed := false
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						sb.WriteByte('\'')
						i += 2
						continue
					}
					closed = true
					i++
					break
				}
				sb.WriteByte(s[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("%w: unterminated string literal at position %d", ErrUnsupportedSQL, start)
			}
			out = append(out, token{kind: tokString, text: sb.String(), pos: start})
		case c == '-' || (c >= '0' && c <= '9'):
			start := i
			if c == '-' {
				i++
				if i >= len(s) || !(s[i] >= '0' && s[i] <= '9') {
					return nil, fmt.Errorf("%w: unexpected '-' at position %d", ErrUnsupportedSQL, start)
				}
			}
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i < len(s) && (isIdentStart(s[i]) || s[i] == '.') {
				return nil, fmt.Errorf("%w: malformed numeric literal at position %d", ErrUnsupportedSQL, start)
			}
			out = append(out, token{kind: tokNumber, text: s[start:i], pos: start})
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

func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// --- parser ---

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token   { return p.toks[p.pos] }
func (p *parser) advance() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) parseStatement() (Statement, error) {
	if err := p.expectKeyword("SELECT"); err != nil {
		return Statement{}, err
	}
	projectionQualifier, err := p.parseProjection()
	if err != nil {
		return Statement{}, err
	}
	if err := p.expectKeyword("FROM"); err != nil {
		return Statement{}, err
	}
	tableTok := p.peek()
	if tableTok.kind != tokIdent || isReserved(tableTok.text) {
		return Statement{}, p.unsupported("expected table name")
	}
	p.advance()
	tableName := tableTok.text
	leftQualifiers, err := p.parseRelationQualifiers(tableName)
	if err != nil {
		return Statement{}, err
	}
	stmt := Statement{Table: tableName, ProjectedTable: tableName, ProjectedAlias: projectionQualifier}
	bindings := relationBindings{defaultTable: tableName, byQualifier: singleQualifierMap(tableName, leftQualifiers)}
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "INNER") {
		p.advance()
	}
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "JOIN") {
		join, rightQualifiers, err := p.parseJoinClause(tableName, leftQualifiers)
		if err != nil {
			return Statement{}, err
		}
		stmt.Join = join
		bindings = relationBindings{
			requireQualify: true,
			byQualifier:    joinQualifierMap(tableName, leftQualifiers, join.RightTable, rightQualifiers),
		}
		if projectionQualifier == "" {
			return Statement{}, p.unsupported("join queries require a qualified projection")
		}
		projectedTable, ok := resolveQualifier(projectionQualifier, bindings.byQualifier)
		if !ok {
			return Statement{}, p.unsupported(fmt.Sprintf("projection qualifier %q does not match joined relations", projectionQualifier))
		}
		stmt.ProjectedTable = projectedTable
		stmt.ProjectedAlias = projectionQualifier
		// Parity rejection: reference subscription runtime at
		// reference/SpacetimeDB/crates/subscription/src/lib.rs:251 bails with
		// "Invalid number of tables in subscription: {N}" for N >= 3. Shunter
		// rejects the chain shape at the parser boundary so the rejection is
		// intentional and pinned, not an incidental "unexpected token" miss.
		if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "INNER") {
			if p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tokIdent && strings.EqualFold(p.toks[p.pos+1].text, "JOIN") {
				return Statement{}, p.unsupported("multi-way join not supported: subscriptions are limited to at most two relations")
			}
		}
		if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "JOIN") {
			return Statement{}, p.unsupported("multi-way join not supported: subscriptions are limited to at most two relations")
		}
	} else if projectionQualifier != "" && !matchesQualifier(projectionQualifier, leftQualifiers) {
		return Statement{}, p.unsupported(fmt.Sprintf("projection qualifier %q does not match table %q", projectionQualifier, tableName))
	}
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "WHERE") {
		p.advance()
		pred, filters, err := p.parseWhere(bindings)
		if err != nil {
			return Statement{}, err
		}
		stmt.Predicate = pred
		stmt.Filters = filters
	}
	if p.peek().kind == tokSemicolon {
		p.advance()
	}
	if p.peek().kind != tokEOF {
		return Statement{}, p.unsupported(fmt.Sprintf("unexpected token %q", p.peek().text))
	}
	return stmt, nil
}

func (p *parser) parseProjection() (string, error) {
	t := p.peek()
	if t.kind == tokStar {
		p.advance()
		return "", nil
	}
	if t.kind != tokIdent || isReserved(t.text) {
		return "", p.unsupported("projection must be '*' or 'table.*'")
	}
	qualifier := t.text
	p.advance()
	if p.peek().kind != tokDot {
		return "", p.unsupported("projection must be '*' or 'table.*'")
	}
	p.advance()
	if p.peek().kind != tokStar {
		return "", p.unsupported("projection must be '*' or 'table.*'")
	}
	p.advance()
	return qualifier, nil
}

func (p *parser) parseRelationQualifiers(tableName string) ([]string, error) {
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "AS") {
		p.advance()
		alias, err := p.parseAlias()
		if err != nil {
			return nil, err
		}
		return []string{alias}, nil
	}
	if p.peek().kind == tokIdent && !isReserved(p.peek().text) {
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
	if t.kind != tokIdent || isReserved(t.text) {
		return "", p.unsupported("expected alias name")
	}
	p.advance()
	return t.text, nil
}

func (p *parser) parseJoinClause(leftTable string, leftQualifiers []string) (*JoinClause, []string, error) {
	if err := p.expectKeyword("JOIN"); err != nil {
		return nil, nil, err
	}
	rightTok := p.peek()
	if rightTok.kind != tokIdent || isReserved(rightTok.text) {
		return nil, nil, p.unsupported("expected joined table name")
	}
	p.advance()
	rightTable := rightTok.text
	rightQualifiers, err := p.parseRelationQualifiers(rightTable)
	if err != nil {
		return nil, nil, err
	}
	leftAlias := leftQualifiers[0]
	rightAlias := rightQualifiers[0]
	if strings.EqualFold(leftTable, rightTable) && strings.EqualFold(leftAlias, rightAlias) {
		return nil, nil, p.unsupported("self join requires aliases")
	}
	if !(p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "ON")) {
		return &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: false}, rightQualifiers, nil
	}
	if err := p.expectKeyword("ON"); err != nil {
		return nil, nil, err
	}
	lookup := joinQualifierMap(leftTable, leftQualifiers, rightTable, rightQualifiers)
	leftOn, err := p.parseQualifiedColumnRef(lookup)
	if err != nil {
		return nil, nil, err
	}
	op, err := p.parseOperator()
	if err != nil {
		return nil, nil, err
	}
	if op != "=" {
		return nil, nil, p.unsupported("JOIN ON only supports '='")
	}
	rightOn, err := p.parseQualifiedColumnRef(lookup)
	if err != nil {
		return nil, nil, err
	}
	if strings.EqualFold(leftOn.Alias, rightOn.Alias) {
		return nil, nil, p.unsupported("JOIN ON must compare columns from different relations")
	}
	if strings.EqualFold(leftOn.Alias, rightAlias) && strings.EqualFold(rightOn.Alias, leftAlias) {
		leftOn, rightOn = rightOn, leftOn
	}
	if !strings.EqualFold(leftOn.Alias, leftAlias) || !strings.EqualFold(rightOn.Alias, rightAlias) {
		return nil, nil, p.unsupported("JOIN ON must compare left relation to right relation")
	}
	return &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: true, LeftOn: leftOn, RightOn: rightOn}, rightQualifiers, nil
}

func (p *parser) parseQualifiedColumnRef(lookup map[string]string) (ColumnRef, error) {
	qualifierTok := p.peek()
	if qualifierTok.kind != tokIdent || isReserved(qualifierTok.text) {
		return ColumnRef{}, p.unsupported("expected qualified column reference")
	}
	p.advance()
	if p.peek().kind != tokDot {
		return ColumnRef{}, p.unsupported("expected qualified column reference")
	}
	p.advance()
	columnTok := p.peek()
	if columnTok.kind != tokIdent || isReserved(columnTok.text) {
		return ColumnRef{}, p.unsupported("expected column name after qualifier")
	}
	p.advance()
	tableName, ok := resolveQualifier(qualifierTok.text, lookup)
	if !ok {
		return ColumnRef{}, p.unsupported(fmt.Sprintf("qualified column %q does not match relation", qualifierTok.text))
	}
	return ColumnRef{Table: tableName, Column: columnTok.text, Alias: qualifierTok.text}, nil
}

func (p *parser) parseWhere(bindings relationBindings) (Predicate, []Filter, error) {
	pred, err := p.parseDisjunction(bindings)
	if err != nil {
		return nil, nil, err
	}
	if p.peek().kind == tokIdent {
		kw := strings.ToUpper(p.peek().text)
		if kw == "ORDER" || kw == "LIMIT" || kw == "GROUP" || kw == "HAVING" || kw == "JOIN" {
			return nil, nil, p.unsupported(fmt.Sprintf("%s not supported", kw))
		}
	}
	filters, _ := flattenAndFilters(pred)
	return pred, filters, nil
}

func (p *parser) parseDisjunction(bindings relationBindings) (Predicate, error) {
	left, err := p.parseConjunction(bindings)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "OR") {
		p.advance()
		right, err := p.parseConjunction(bindings)
		if err != nil {
			return nil, err
		}
		left = OrPredicate{Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseConjunction(bindings relationBindings) (Predicate, error) {
	left, err := p.parseComparisonPredicate(bindings)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "AND") {
		p.advance()
		right, err := p.parseComparisonPredicate(bindings)
		if err != nil {
			return nil, err
		}
		left = AndPredicate{Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseComparisonPredicate(bindings relationBindings) (Predicate, error) {
	f, err := p.parseComparison(bindings)
	if err != nil {
		return nil, err
	}
	return ComparisonPredicate{Filter: f}, nil
}

func flattenAndFilters(pred Predicate) ([]Filter, bool) {
	switch p := pred.(type) {
	case nil:
		return nil, true
	case ComparisonPredicate:
		return []Filter{p.Filter}, true
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

func (p *parser) parseComparison(bindings relationBindings) (Filter, error) {
	t := p.peek()
	if t.kind != tokIdent || isReserved(t.text) {
		return Filter{}, p.unsupported(fmt.Sprintf("expected column name, got %q", t.text))
	}
	p.advance()
	columnName := t.text
	tableName := bindings.defaultTable
	alias := ""
	if p.peek().kind == tokDot {
		qualifier := columnName
		p.advance()
		t = p.peek()
		if t.kind != tokIdent || isReserved(t.text) {
			return Filter{}, p.unsupported(fmt.Sprintf("expected column name after qualifier %q", qualifier))
		}
		resolved, ok := resolveQualifier(qualifier, bindings.byQualifier)
		if !ok {
			return Filter{}, p.unsupported(fmt.Sprintf("qualified column %q does not match relation", qualifier))
		}
		tableName = resolved
		columnName = t.text
		alias = qualifier
		p.advance()
		if p.peek().kind == tokDot {
			return Filter{}, p.unsupported("qualified column names not supported")
		}
	} else if bindings.requireQualify {
		return Filter{}, p.unsupported("join WHERE columns must be qualified")
	}
	op, err := p.parseOperator()
	if err != nil {
		return Filter{}, err
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return Filter{}, err
	}
	return Filter{Table: tableName, Column: columnName, Alias: alias, Op: op, Literal: lit}, nil
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
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return Literal{}, fmt.Errorf("%w: integer literal %q out of range", ErrUnsupportedSQL, t.text)
		}
		return Literal{Kind: LitInt, Int: n}, nil
	case tokString:
		p.advance()
		return Literal{Kind: LitString, Str: t.text}, nil
	case tokIdent:
		if strings.EqualFold(t.text, "TRUE") {
			p.advance()
			return Literal{Kind: LitBool, Bool: true}, nil
		}
		if strings.EqualFold(t.text, "FALSE") {
			p.advance()
			return Literal{Kind: LitBool, Bool: false}, nil
		}
		return Literal{}, p.unsupported(fmt.Sprintf("expected literal, got identifier %q", t.text))
	default:
		return Literal{}, p.unsupported(fmt.Sprintf("expected literal, got %q", t.text))
	}
}

func (p *parser) expectKeyword(kw string) error {
	t := p.peek()
	if t.kind == tokEOF {
		return p.unsupported(fmt.Sprintf("expected %s, got end of input", kw))
	}
	if t.kind != tokIdent || !strings.EqualFold(t.text, kw) {
		return p.unsupported(fmt.Sprintf("expected %s, got %q", kw, t.text))
	}
	p.advance()
	return nil
}

func (p *parser) unsupported(msg string) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedSQL, msg)
}

func matchesQualifier(candidate string, qualifiers []string) bool {
	for _, qualifier := range qualifiers {
		if strings.EqualFold(candidate, qualifier) {
			return true
		}
	}
	return false
}

var reservedWords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "AND": {}, "OR": {},
	"ORDER": {}, "BY": {}, "LIMIT": {}, "GROUP": {}, "HAVING": {},
	"JOIN": {}, "ON": {}, "AS": {}, "INNER": {},
}

func isReserved(s string) bool {
	_, ok := reservedWords[strings.ToUpper(s)]
	return ok
}

func singleQualifierMap(tableName string, qualifiers []string) map[string]string {
	out := make(map[string]string, len(qualifiers))
	for _, qualifier := range qualifiers {
		out[strings.ToUpper(qualifier)] = tableName
	}
	return out
}

func joinQualifierMap(leftTable string, leftQualifiers []string, rightTable string, rightQualifiers []string) map[string]string {
	out := singleQualifierMap(leftTable, leftQualifiers)
	for _, qualifier := range rightQualifiers {
		out[strings.ToUpper(qualifier)] = rightTable
	}
	return out
}

func resolveQualifier(qualifier string, lookup map[string]string) (string, bool) {
	resolved, ok := lookup[strings.ToUpper(qualifier)]
	return resolved, ok
}

