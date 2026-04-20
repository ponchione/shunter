// Package sql implements the minimum-viable SQL surface the Shunter
// protocol layer accepts on OneOffQuery / SubscribeSingle / SubscribeMulti.
//
// Grammar:
//
//	stmt   = "SELECT" ( "*" | ident "." "*" ) "FROM" ident [ [ "AS" ] ident ] [ where ] [ ";" ]
//	where  = "WHERE" cmp ( "AND" cmp )*
//	cmp    = colref op literal
//	colref = ident | ident "." ident   // only when qualifier matches FROM table or alias
//	op     = "=" | "<" | ">" | "<=" | ">=" | "!=" | "<>"
//	literal = integer | bool | string
//	ident   = [A-Za-z_][A-Za-z0-9_]*
//
// Anything outside this grammar (projection other than "*", OR, JOIN,
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
type Filter struct {
	Column  string
	Op      string
	Literal Literal
}

// Statement is the parsed output.
type Statement struct {
	Table   string
	Filters []Filter
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
	qualifiers, err := p.parseRelationQualifiers(tableName)
	if err != nil {
		return Statement{}, err
	}
	if projectionQualifier != "" && !matchesQualifier(projectionQualifier, qualifiers) {
		return Statement{}, p.unsupported(fmt.Sprintf("projection qualifier %q does not match table %q", projectionQualifier, tableName))
	}
	stmt := Statement{Table: tableName}
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "WHERE") {
		p.advance()
		filters, err := p.parseWhere(qualifiers)
		if err != nil {
			return Statement{}, err
		}
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
	qualifiers := []string{tableName}
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "AS") {
		p.advance()
		alias, err := p.parseAlias()
		if err != nil {
			return nil, err
		}
		return append(qualifiers, alias), nil
	}
	if p.peek().kind == tokIdent && !isReserved(p.peek().text) {
		alias, err := p.parseAlias()
		if err != nil {
			return nil, err
		}
		return append(qualifiers, alias), nil
	}
	return qualifiers, nil
}

func (p *parser) parseAlias() (string, error) {
	t := p.peek()
	if t.kind != tokIdent || isReserved(t.text) {
		return "", p.unsupported("expected alias name")
	}
	p.advance()
	return t.text, nil
}

func (p *parser) parseWhere(qualifiers []string) ([]Filter, error) {
	var filters []Filter
	f, err := p.parseComparison(qualifiers)
	if err != nil {
		return nil, err
	}
	filters = append(filters, f)
	for p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "AND") {
		p.advance()
		f, err := p.parseComparison(qualifiers)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	if p.peek().kind == tokIdent {
		kw := strings.ToUpper(p.peek().text)
		if kw == "OR" || kw == "ORDER" || kw == "LIMIT" || kw == "GROUP" || kw == "HAVING" || kw == "JOIN" {
			return nil, p.unsupported(fmt.Sprintf("%s not supported", kw))
		}
	}
	return filters, nil
}

func (p *parser) parseComparison(qualifiers []string) (Filter, error) {
	t := p.peek()
	if t.kind != tokIdent || isReserved(t.text) {
		return Filter{}, p.unsupported(fmt.Sprintf("expected column name, got %q", t.text))
	}
	p.advance()
	columnName := t.text
	if p.peek().kind == tokDot {
		qualifier := columnName
		p.advance()
		t = p.peek()
		if t.kind != tokIdent || isReserved(t.text) {
			return Filter{}, p.unsupported(fmt.Sprintf("expected column name after qualifier %q", qualifier))
		}
		if !matchesQualifier(qualifier, qualifiers) {
			return Filter{}, p.unsupported(fmt.Sprintf("qualified column %q does not match relation", qualifier))
		}
		columnName = t.text
		p.advance()
		if p.peek().kind == tokDot {
			return Filter{}, p.unsupported("qualified column names not supported")
		}
	}
	op, err := p.parseOperator()
	if err != nil {
		return Filter{}, err
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return Filter{}, err
	}
	return Filter{Column: columnName, Op: op, Literal: lit}, nil
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
	"JOIN": {}, "ON": {}, "AS": {},
}

func isReserved(s string) bool {
	_, ok := reservedWords[strings.ToUpper(s)]
	return ok
}
