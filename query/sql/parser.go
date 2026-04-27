// Package sql implements the minimum-viable SQL surface the Shunter
// protocol layer accepts on OneOffQuery / SubscribeSingle / SubscribeMulti.
//
// Grammar:
//
//	stmt   = "SELECT" projection "FROM" ident [ [ "AS" ] ident ] [ [ "INNER" ] "JOIN" ident [ [ "AS" ] ident ] "ON" qcol "=" qcol ] [ where ] [ limit ] [ ";" ]
//	projection = "*" | ident "." "*" | projcol ( "," projcol )* | aggregate
//	projcol = ident | ident "." ident
//	aggregate = "COUNT" "(" "*" ")" [ "AS" ] ident
//	where  = "WHERE" pred
//	limit  = "LIMIT" unsigned-integer
//	pred   = conj ( "OR" conj )*
//	conj   = term ( "AND" term )*
//	term   = cmp | "(" pred ")"
//	cmp    = colref op literal
//	colref = ident | ident "." ident
//	qcol   = ident "." ident
//	op     = "=" | "<" | ">" | "<=" | ">=" | "!=" | "<>"
//	literal = integer | float | bool | string | hex-bytes
//	ident   = [A-Za-z_][A-Za-z0-9_]* | quoted-ident
//	quoted-ident = '"' ( '""' | any-char-except-quote )+ '"'
//
// SQL identifiers are byte-exact after quoted-identifier unescaping. Quoting
// preserves the written spelling and does not enable case-folded table,
// column, alias, or qualifier lookup.
//
// Anything outside this grammar (projection other than "*", unsupported JOIN forms,
// ORDER BY, aggregates other than `COUNT(*) [AS] alias`,
// subqueries, mismatched qualified columns, etc.) is rejected with
// ErrUnsupportedSQL.
//
// Literals carry their lexical category only. Callers coerce them to a
// concrete types.Value against a column kind via Coerce.
package sql

import (
	"errors"
	"fmt"
	"math"
	"math/big"
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
	// LitSender is the parameter marker for `:sender`, the caller identity
	// placeholder accepted on identity / bytes columns. The parser preserves
	// it as a distinct lexical kind so the coercion path resolves it against
	// the caller identity supplied at compile time rather than a literal
	// value from the SQL text. See reference/SpacetimeDB/crates/expr/src/
	// check.rs lines 434-440 for accepted shapes and lines 487-488 for the
	// rejection on non-identity columns.
	LitSender
	// LitBigInt carries an arbitrary-precision integer literal — the branch
	// used when a numeric literal parses as integer-valued (via big.Rat) but
	// does not fit int64. Reference parity target is `u256 = 1e40` at
	// reference/SpacetimeDB/crates/expr/src/check.rs:330-332, where the
	// reference BigDecimal path treats `1e40` as the exact integer 10^40.
	LitBigInt
)

// Literal is a parsed SQL literal in raw lexical form.
//
// Text preserves the source text of the literal token for the categories that
// the reference parser carries through `parse(value, ty)` at expr/src/lib.rs:
// numeric tokens (raw `tokNumber` body, including leading `+`, leading zeros,
// trailing fractional zeros, and scientific-notation exponents that collapse
// at parseNumericLiteral), hex literals (`0x...` / `X'...'` token text), and
// string bodies (the unquoted contents). Bool / :sender literals do not use
// Text. Coerce paths that emit reference `InvalidLiteral` text or widen onto
// `KindString` consume Text via `renderLiteralSourceText` so the source token
// survives parser collapses (e.g. `1e40` → `LitBigInt(10^40)` keeps the
// `1e40` token, `1.10` → `LitFloat(1.1)` keeps the `1.10` token).
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
//
// AliasCollision marks a parser-detected `LeftAlias == RightAlias` shape whose
// `DuplicateName` rejection has been deferred to the compile stage. Reference
// `type_from` (`expr/src/check.rs:79-89`) resolves the left relvar through
// `type_relvar` BEFORE entering the join loop's HashSet duplicate-alias check,
// so missing-table rejections must precede the dup-alias error. The compile
// stage emits `DuplicateNameError{Name: LeftAlias}` after both schema lookups
// succeed; ON-clause / WHERE / projection-column resolution is skipped on the
// parser side because either side's relvar may be absent.
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
// limited to join-scoped query-only lowering for now.
type ColumnComparisonPredicate struct {
	Left  ColumnRef
	Op    string
	Right ColumnRef
}

func (ColumnComparisonPredicate) isPredicate() {}

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

// AggregateProjection is the bounded query-only aggregate surface currently
// accepted by the parser.
type AggregateProjection struct {
	Func  string
	Alias string
}

// Statement is the parsed output.
//
// ProjectedAlias preserves the qualifier token the user wrote for the
// SELECT target (`a` in `SELECT a.*`, `Orders` in `SELECT Orders.*`,
// or empty when the projection was bare `*`). Compile paths consult it to
// distinguish `SELECT a.*` from `SELECT b.*` on aliased self-joins, where
// ProjectedTable alone is insufficient because both aliases resolve to the
// same base table.
//
// ProjectionColumns is populated for explicit column-list projections (for
// example `SELECT u32, name FROM t`, `SELECT o.id, o.product_id FROM Orders o
// JOIN Inventory product ...`, or one-off mixed-relation join projections such
// as `SELECT o.id, product.quantity ...`). Wildcard/full-row projections keep
// this empty and continue using ProjectedTable / ProjectedAlias.
type Statement struct {
	Table                 string
	TableAlias            string
	ProjectedTable        string
	ProjectedAlias        string
	ProjectedAliasUnknown bool
	ProjectionColumns     []ProjectionColumn
	Aggregate             *AggregateProjection
	Join                  *JoinClause
	Predicate             Predicate
	Filters               []Filter
	Limit                 *uint64
	HasLimit              bool
	InvalidLimit          *Literal
	UnsupportedLimit      bool
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
		case c == '"':
			start := i
			i++
			var sb strings.Builder
			closed := false
			for i < len(s) {
				if s[i] == '"' {
					if i+1 < len(s) && s[i+1] == '"' {
						sb.WriteByte('"')
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
				return nil, fmt.Errorf("%w: unterminated quoted identifier at position %d", ErrUnsupportedSQL, start)
			}
			if sb.Len() == 0 {
				return nil, fmt.Errorf("%w: empty quoted identifier at position %d", ErrUnsupportedSQL, start)
			}
			out = append(out, token{kind: tokIdent, text: sb.String(), pos: start, quoted: true})
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
	stmt.Aggregate = aggregate
	bindings := relationBindings{defaultTable: tableName, byQualifier: singleQualifierMap(tableName, leftQualifiers)}
	var onFilter Predicate
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
	}
	if isKeywordToken(p.peek(), "JOIN") {
		join, rightQualifiers, onPred, err := p.parseJoinClause(tableName, leftQualifiers)
		if err != nil {
			return Statement{}, err
		}
		onFilter = onPred
		stmt.Join = join
		bindings = relationBindings{
			requireQualify: true,
			byQualifier:    joinQualifierMap(tableName, leftQualifiers, join.RightTable, rightQualifiers),
		}
		if projectionQualifier != "" {
			if projectedTable, ok := resolveQualifier(projectionQualifier, bindings.byQualifier); ok {
				stmt.ProjectedTable = projectedTable
			} else {
				stmt.ProjectedTable = projectionQualifier
				stmt.ProjectedAliasUnknown = true
			}
			stmt.ProjectedAlias = projectionQualifier
		}
		// Parity rejection: reference subscription runtime at
		// reference/SpacetimeDB/crates/subscription/src/lib.rs:251 bails with
		// "Invalid number of tables in subscription: {N}" for N >= 3. Shunter
		// rejects the chain shape at the parser boundary so the rejection is
		// intentional and pinned, not an incidental "unexpected token" miss.
		if isKeywordToken(p.peek(), "INNER") {
			if p.pos+1 < len(p.toks) && isKeywordToken(p.toks[p.pos+1], "JOIN") {
				return Statement{}, p.unsupported("multi-way join not supported: subscriptions are limited to at most two relations")
			}
			return Statement{}, p.unsupported("expected JOIN after INNER")
		}
		if isKeywordToken(p.peek(), "CROSS") {
			if p.pos+1 < len(p.toks) && isKeywordToken(p.toks[p.pos+1], "JOIN") {
				return Statement{}, p.unsupported("multi-way join not supported: subscriptions are limited to at most two relations")
			}
			return Statement{}, p.unsupported("expected JOIN after CROSS")
		}
		if isUnsupportedJoinStartToken(p.peek()) {
			return Statement{}, UnsupportedJoinTypeError{}
		}
		if isKeywordToken(p.peek(), "JOIN") {
			return Statement{}, p.unsupported("multi-way join not supported: subscriptions are limited to at most two relations")
		}
	} else if projectionQualifier != "" && !matchesQualifier(projectionQualifier, leftQualifiers) {
		stmt.ProjectedAliasUnknown = true
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
	if onFilter != nil {
		if stmt.Predicate != nil {
			stmt.Predicate = AndPredicate{Left: onFilter, Right: stmt.Predicate}
		} else {
			stmt.Predicate = onFilter
		}
		stmt.Filters, _ = flattenAndFilters(stmt.Predicate)
	}
	limit, invalidLimit, hasLimit, unsupportedLimit, err := p.parseLimit()
	if err != nil {
		return Statement{}, err
	}
	stmt.Limit = limit
	stmt.InvalidLimit = invalidLimit
	stmt.HasLimit = hasLimit
	stmt.UnsupportedLimit = unsupportedLimit
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
	// Reference SQL/subscription parsers reject any SELECT carrying a
	// non-None set quantifier (`SELECT ALL ...` / `SELECT DISTINCT ...`)
	// at `parse_select`'s `_ => ...feature(select)` arm
	// (`sql-parser/src/parser/sql.rs:362-394` and
	// `sql-parser/src/parser/sub.rs:120-149`). Detect the modifier here
	// instead of letting `parseProjectionItem` reinterpret the keyword
	// as a column reference (which masks the rejection when a column
	// happens to share the keyword's name). On the subscribe surface,
	// query-level LIMIT rejection precedes parse_select's set-quantifier
	// rejection, so preserve that ordering when both clauses are present.
	if !t.quoted && t.kind == tokIdent && (strings.EqualFold(t.text, "ALL") || strings.EqualFold(t.text, "DISTINCT")) {
		return "", nil, nil, UnsupportedSelectError{SQL: p.sql, HasLimit: p.hasLimitClauseAhead()}
	}
	if t.kind == tokStar {
		p.advance()
		if p.peek().kind == tokComma {
			return "", nil, nil, p.unsupported("cannot mix '*' with explicit projection columns")
		}
		return "", nil, nil, nil
	}
	if isIdentifierToken(t) && strings.EqualFold(t.text, "COUNT") && p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tokLParen {
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

func (p *parser) parseAggregateProjection() (*AggregateProjection, error) {
	fn := p.peek()
	if !isIdentifierToken(fn) || !strings.EqualFold(fn.text, "COUNT") {
		return nil, p.unsupported("aggregate projections not supported")
	}
	p.advance()
	if p.peek().kind != tokLParen {
		return nil, p.unsupported("aggregate projections not supported")
	}
	p.advance()
	if p.peek().kind != tokStar {
		return nil, p.unsupported("only COUNT(*) aggregate projections supported")
	}
	p.advance()
	if p.peek().kind != tokRParen {
		return nil, p.unsupported("only COUNT(*) aggregate projections supported")
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
	return &AggregateProjection{Func: "COUNT", Alias: alias}, nil
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
			resolvedTable, ok := resolveQualifier(qualifier, bindings.byQualifier)
			if ok {
				tableName = resolvedTable
			} else {
				tableName = qualifier
			}
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

func (p *parser) parseJoinClause(leftTable string, leftQualifiers []string) (*JoinClause, []string, Predicate, error) {
	if err := p.expectKeyword("JOIN"); err != nil {
		return nil, nil, nil, err
	}
	rightTok := p.peek()
	if !isIdentifierToken(rightTok) {
		return nil, nil, nil, p.unsupported("expected joined table name")
	}
	p.advance()
	rightTable := rightTok.text
	rightQualifiers, err := p.parseRelationQualifiers(rightTable)
	if err != nil {
		return nil, nil, nil, err
	}
	leftAlias := leftQualifiers[0]
	rightAlias := rightQualifiers[0]
	if leftAlias == rightAlias {
		// Reference `type_from` (expr/src/lib.rs:88-89) rejects a join whose
		// right-side alias collides with the left side using
		// `DuplicateName(alias)`. Same reference text covers both the
		// explicitly-aliased shape (`FROM t AS dup JOIN s AS dup`) and the
		// unaliased self-join shape (`FROM t JOIN t`) because the parser
		// derives each side's alias from its base table when no `AS` is
		// written. `Relvars` is byte-equal `SqlIdent`, so case-distinct
		// aliases (e.g. `"R"` and `r`) do NOT collide.
		//
		// Defer the rejection to the compile stage so reference
		// `type_relvar` ordering holds: if the LEFT or RIGHT base table
		// is missing, the schema lookup at `compileSQLQueryString`
		// emits the missing-table text BEFORE the dup-alias check fires.
		// Skip ON-clause resolution because the qualifier map collapses
		// when both aliases are byte-equal — we don't need it; the
		// compile-stage dup rejection subsumes any ON-clause findings.
		// Drain remaining tokens up to the statement terminator so the
		// outer parseStatement EOF guard sees a clean tail.
		p.consumeUntilStatementEnd()
		return &JoinClause{
			LeftTable:      leftTable,
			RightTable:     rightTable,
			LeftAlias:      leftAlias,
			RightAlias:     rightAlias,
			HasOn:          false,
			AliasCollision: true,
		}, rightQualifiers, nil, nil
	}
	if !isKeywordToken(p.peek(), "ON") {
		return &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: false}, rightQualifiers, nil, nil
	}
	if err := p.expectKeyword("ON"); err != nil {
		return nil, nil, nil, err
	}
	lookup := joinQualifierMap(leftTable, leftQualifiers, rightTable, rightQualifiers)
	leftOn, err := p.parseQualifiedColumnRef(lookup)
	if err != nil {
		return nil, nil, nil, err
	}
	op, err := p.parseOperator()
	if err != nil {
		return nil, nil, nil, err
	}
	if op != "=" {
		return nil, nil, nil, UnsupportedJoinTypeError{}
	}
	rightOn, err := p.parseQualifiedColumnRef(lookup)
	if err != nil {
		return nil, nil, nil, err
	}
	_, leftKnown := resolveQualifier(leftOn.Alias, lookup)
	_, rightKnown := resolveQualifier(rightOn.Alias, lookup)
	if leftKnown && rightKnown {
		if leftOn.Alias == rightOn.Alias {
			return nil, nil, nil, p.unsupported("JOIN ON must compare columns from different relations")
		}
		if leftOn.Alias == rightAlias && rightOn.Alias == leftAlias {
			leftOn, rightOn = rightOn, leftOn
		}
		if leftOn.Alias != leftAlias || rightOn.Alias != rightAlias {
			return nil, nil, nil, p.unsupported("JOIN ON must compare left relation to right relation")
		}
	}
	jc := &JoinClause{LeftTable: leftTable, RightTable: rightTable, LeftAlias: leftAlias, RightAlias: rightAlias, HasOn: true, LeftOn: leftOn, RightOn: rightOn}
	if isKeywordToken(p.peek(), "AND") || isKeywordToken(p.peek(), "OR") {
		return nil, nil, nil, UnsupportedJoinTypeError{}
	}
	return jc, rightQualifiers, nil, nil
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
	tableName, ok := resolveQualifier(qualifierTok.text, lookup)
	if !ok {
		tableName = qualifierTok.text
	}
	return ColumnRef{Table: tableName, Column: columnTok.text, Alias: qualifierTok.text}, nil
}

func (p *parser) parseWhere(bindings relationBindings) (Predicate, []Filter, error) {
	pred, err := p.parseDisjunction(bindings)
	if err != nil {
		return nil, nil, err
	}
	if !p.peek().quoted && p.peek().kind == tokIdent {
		kw := strings.ToUpper(p.peek().text)
		if kw == "ORDER" || kw == "GROUP" || kw == "HAVING" || kw == "JOIN" {
			return nil, nil, p.unsupported(fmt.Sprintf("%s not supported", kw))
		}
	}
	filters, _ := flattenAndFilters(pred)
	return pred, filters, nil
}

func (p *parser) parseLimit() (*uint64, *Literal, bool, bool, error) {
	if !isKeywordToken(p.peek(), "LIMIT") {
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
	left, err := p.parseConjunction(bindings)
	if err != nil {
		return nil, err
	}
	for isKeywordToken(p.peek(), "OR") {
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
	left, err := p.parsePredicateTerm(bindings)
	if err != nil {
		return nil, err
	}
	for isKeywordToken(p.peek(), "AND") {
		p.advance()
		right, err := p.parsePredicateTerm(bindings)
		if err != nil {
			return nil, err
		}
		left = AndPredicate{Left: left, Right: right}
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
	t := p.peek()
	if !isIdentifierToken(t) {
		return ColumnRef{}, p.unsupported(fmt.Sprintf("expected column name, got %q", t.text))
	}
	p.advance()
	columnName := t.text
	tableName := bindings.defaultTable
	alias := ""
	if p.peek().kind == tokDot {
		qualifier := columnName
		p.advance()
		t = p.peek()
		if !isIdentifierToken(t) {
			return ColumnRef{}, p.unsupported(fmt.Sprintf("expected column name after qualifier %q", qualifier))
		}
		resolved, ok := resolveQualifier(qualifier, bindings.byQualifier)
		if !ok {
			resolved = qualifier
		}
		tableName = resolved
		columnName = t.text
		alias = qualifier
		p.advance()
		if p.peek().kind == tokDot {
			return ColumnRef{}, p.unsupported("qualified column names not supported")
		}
	} else if bindings.requireQualify {
		// Reference `SqlSelect::find_unqualified_vars`
		// (sql-parser/src/ast/sql.rs:84-95) routes any unqualified var
		// in a JOIN scope through `SqlUnsupported::UnqualifiedNames`
		// (parser/errors.rs:78-79). LHS-of-WHERE-comparison branch.
		return ColumnRef{}, UnqualifiedNamesError{}
	}
	return ColumnRef{Table: tableName, Column: columnName, Alias: alias}, nil
}

func (p *parser) parseComparison(bindings relationBindings) (Filter, error) {
	ref, err := p.parseColumnRefForPredicate(bindings)
	if err != nil {
		return Filter{}, err
	}
	op, err := p.parseOperator()
	if err != nil {
		return Filter{}, err
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return Filter{}, err
	}
	return Filter{Table: ref.Table, Column: ref.Column, Alias: ref.Alias, Op: op, Literal: lit}, nil
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

// parseNumericLiteral turns a tokenized numeric body into a typed Literal.
// Plain integer bodies attempt strconv.ParseInt; on int64 overflow they
// promote to LitBigInt. Bodies with a fractional or exponent part first go
// through big.Rat so scientific shapes like `1e40` preserve exact-integer
// semantics (the reference BigDecimal is_integer path in
// crates/expr/src/lib.rs::parse_int collapses integer-valued decimals into
// big integers). Integer-valued rationals collapse to LitInt when they fit
// int64, else LitBigInt. Non-integer rationals fall back to LitFloat via
// strconv.ParseFloat so `1e-3` / `0.1` stay float-kinded for the coerce
// boundary to either bind to a float column or reject as a float-on-integer
// mismatch.
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

func matchesQualifier(candidate string, qualifiers []string) bool {
	for _, qualifier := range qualifiers {
		if candidate == qualifier {
			return true
		}
	}
	return false
}

var reservedWords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "AND": {}, "OR": {},
	"ORDER": {}, "BY": {}, "LIMIT": {}, "GROUP": {}, "HAVING": {},
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

func joinQualifierMap(leftTable string, leftQualifiers []string, rightTable string, rightQualifiers []string) map[string]string {
	out := singleQualifierMap(leftTable, leftQualifiers)
	for _, qualifier := range rightQualifiers {
		out[qualifier] = rightTable
	}
	return out
}

func resolveQualifier(qualifier string, lookup map[string]string) (string, bool) {
	if resolved, ok := lookup[qualifier]; ok {
		return resolved, true
	}
	return "", false
}
