// Package sql implements the minimum-viable SQL surface the Shunter
// protocol layer accepts on OneOffQuery / SubscribeSingle / SubscribeMulti.
//
// Grammar:
//
//	stmt   = "SELECT" "*" "FROM" ident [ where ] [ ";" ]
//	where  = "WHERE" eq ( "AND" eq )*
//	eq     = ident "=" literal
//	literal = integer | bool | string
//	ident   = [A-Za-z_][A-Za-z0-9_]*
//
// Anything outside this grammar (projection other than "*", comparison
// operators other than "=", OR, JOIN, ORDER BY, LIMIT, qualified columns,
// subqueries, aggregates, etc.) is rejected with ErrUnsupportedSQL.
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

// Filter is a single column = literal equality test.
type Filter struct {
	Column  string
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
	if p.peek().kind != tokStar {
		return Statement{}, p.unsupported("projection must be '*'")
	}
	p.advance()
	if err := p.expectKeyword("FROM"); err != nil {
		return Statement{}, err
	}
	t := p.peek()
	if t.kind != tokIdent || isReserved(t.text) {
		return Statement{}, p.unsupported("expected table name")
	}
	p.advance()
	stmt := Statement{Table: t.text}
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "WHERE") {
		p.advance()
		filters, err := p.parseWhere()
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

func (p *parser) parseWhere() ([]Filter, error) {
	var filters []Filter
	f, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	filters = append(filters, f)
	for p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "AND") {
		p.advance()
		f, err := p.parseEquality()
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

func (p *parser) parseEquality() (Filter, error) {
	t := p.peek()
	if t.kind != tokIdent || isReserved(t.text) {
		return Filter{}, p.unsupported(fmt.Sprintf("expected column name, got %q", t.text))
	}
	p.advance()
	if p.peek().kind == tokDot {
		return Filter{}, p.unsupported("qualified column names not supported")
	}
	if p.peek().kind != tokEq {
		return Filter{}, p.unsupported(fmt.Sprintf("expected '=', got %q", p.peek().text))
	}
	p.advance()
	lit, err := p.parseLiteral()
	if err != nil {
		return Filter{}, err
	}
	return Filter{Column: t.text, Literal: lit}, nil
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

var reservedWords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "AND": {}, "OR": {},
	"ORDER": {}, "BY": {}, "LIMIT": {}, "GROUP": {}, "HAVING": {},
	"JOIN": {}, "ON": {},
}

func isReserved(s string) bool {
	_, ok := reservedWords[strings.ToUpper(s)]
	return ok
}
