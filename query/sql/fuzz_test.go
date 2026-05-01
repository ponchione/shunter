package sql

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

var parseFuzzSeeds = []string{
	"",
	"SELECT * FROM players",
	"select * from players where id = 1",
	`SELECT "users".* FROM "users" WHERE "users"."name" = 'ada'`,
	"SELECT COUNT(*) AS n FROM players WHERE active = TRUE LIMIT 10",
	"SELECT p.id, team.name FROM players AS p JOIN teams AS team ON p.team_id = team.id WHERE team.active = TRUE",
	"SELECT * FROM t WHERE bytes = 0xDEADBEEF",
	"SELECT * FROM t WHERE id = :sender",
	"SELECT * FROM t WHERE id = 12abc",
	"SELECT * FROM t WHERE name = 'unterminated",
	"SELECT * FROM t WHERE c = 1e999999999",
	"SELECT * FROM t INNER",
	"SELECT * FROM t LEFT JOIN s ON t.id = s.id",
}

var coerceFuzzSeeds = [][]byte{
	{},
	{0, 0, 0, 0, 0},
	{1, 2, 3, 4, 5, 6, 7, 8},
	[]byte("1e40"),
	[]byte("0xDEADBEEF"),
	[]byte(":sender"),
	{0xff, 0, 0x7f, 0x80, 0x40, 0x20, 0x10},
}

const maxSQLFuzzBytes = 8 << 10

func FuzzParse(f *testing.F) {
	for _, seed := range parseFuzzSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > maxSQLFuzzBytes {
			t.Skip("SQL fuzz input above bounded local limit")
		}
		assertParseFuzzInput(t, input)
	})
}

func FuzzCoerce(f *testing.F) {
	for _, seed := range coerceFuzzSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			return
		}
		assertCoerceFuzzInput(t, data)
	})
}

var coerceFuzzKinds = []types.ValueKind{
	types.KindBool,
	types.KindInt8,
	types.KindUint8,
	types.KindInt16,
	types.KindUint16,
	types.KindInt32,
	types.KindUint32,
	types.KindInt64,
	types.KindUint64,
	types.KindFloat32,
	types.KindFloat64,
	types.KindString,
	types.KindBytes,
	types.KindInt128,
	types.KindUint128,
	types.KindInt256,
	types.KindUint256,
	types.KindTimestamp,
	types.KindArrayString,
}

type coerceFuzzReader struct {
	data []byte
	pos  int
}

func newCoerceFuzzReader(data []byte) *coerceFuzzReader {
	return &coerceFuzzReader{data: data}
}

func (r *coerceFuzzReader) byte() byte {
	if r.pos >= len(r.data) {
		b := byte(31 + r.pos*43)
		r.pos++
		return b
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

func (r *coerceFuzzReader) bool() bool {
	return r.byte()%2 == 0
}

func (r *coerceFuzzReader) i64() int64 {
	var out uint64
	for i := 0; i < 8; i++ {
		out = (out << 8) | uint64(r.byte())
	}
	return int64(out)
}

func (r *coerceFuzzReader) bytes(max int) []byte {
	n := int(r.byte() % byte(max+1))
	out := make([]byte, n)
	for i := range out {
		out[i] = r.byte()
	}
	return out
}

func (r *coerceFuzzReader) ascii(max int) string {
	n := int(r.byte() % byte(max+1))
	out := make([]byte, n)
	for i := range out {
		switch b := r.byte(); b % 8 {
		case 0:
			out[i] = '0' + b%10
		case 1:
			out[i] = 'a' + b%26
		case 2:
			out[i] = 'A' + b%26
		case 3:
			out[i] = '+'
		case 4:
			out[i] = '-'
		case 5:
			out[i] = '.'
		case 6:
			out[i] = 'e'
		default:
			out[i] = '_'
		}
	}
	return string(out)
}

func (r *coerceFuzzReader) caller() [32]byte {
	var caller [32]byte
	for i := range caller {
		caller[i] = r.byte()
	}
	return caller
}

func (r *coerceFuzzReader) literal() Literal {
	switch r.byte() % 7 {
	case 0:
		n := r.i64()
		text := r.maybeText(fmt.Sprintf("%d", n))
		return Literal{Kind: LitInt, Int: n, Text: text}
	case 1:
		n := float64(r.i64()%2_000_001) / 1000
		text := r.maybeText(fmt.Sprintf("%g", n))
		return Literal{Kind: LitFloat, Float: n, Text: text}
	case 2:
		return Literal{Kind: LitBool, Bool: r.bool()}
	case 3:
		s := r.ascii(32)
		return Literal{Kind: LitString, Str: s, Text: r.maybeText(s)}
	case 4:
		b := r.bytes(32)
		return Literal{Kind: LitBytes, Bytes: b, Text: r.maybeText(fmt.Sprintf("%x", b))}
	case 5:
		return Literal{Kind: LitSender}
	default:
		return Literal{Kind: LitBigInt, Big: r.bigInt(), Text: r.maybeText("")}
	}
}

func (r *coerceFuzzReader) maybeText(fallback string) string {
	if r.bool() {
		return fallback
	}
	return r.ascii(32)
}

func (r *coerceFuzzReader) bigInt() *big.Int {
	data := r.bytes(40)
	if len(data) == 0 {
		return big.NewInt(0)
	}
	out := new(big.Int).SetBytes(data)
	if r.bool() {
		out.Neg(out)
	}
	return out
}

func assertParseFuzzInput(tb testing.TB, input string) {
	tb.Helper()
	if err := checkParseFuzzInput(input); err != nil {
		tb.Fatal(err)
	}
}

func checkParseFuzzInput(input string) error {
	stmt, err := Parse(input)
	if err != nil {
		if !errors.Is(err, ErrUnsupportedSQL) {
			return fmt.Errorf("Parse(%q) error = %v, want ErrUnsupportedSQL category", input, err)
		}
		return nil
	}
	if stmt.Table == "" {
		return fmt.Errorf("Parse(%q) accepted empty table statement: %+v", input, stmt)
	}

	again, err := Parse(input)
	if err != nil {
		return fmt.Errorf("Parse(%q) accepted once then failed: %v", input, err)
	}
	if !reflect.DeepEqual(again, stmt) {
		return fmt.Errorf("Parse(%q) is not deterministic: first=%#v second=%#v", input, stmt, again)
	}
	return nil
}

func assertCoerceFuzzInput(tb testing.TB, data []byte) {
	tb.Helper()
	if err := checkCoerceFuzzInput(data); err != nil {
		tb.Fatal(err)
	}
}

func checkCoerceFuzzInput(data []byte) error {
	r := newCoerceFuzzReader(data)
	lit := r.literal()
	kind := coerceFuzzKinds[int(r.byte())%len(coerceFuzzKinds)]
	caller := r.caller()
	label := coerceFuzzLabel(data, lit, kind)

	if lit.Kind == LitSender {
		if _, err := Coerce(lit, kind); !errors.Is(err, ErrUnsupportedSQL) {
			return fmt.Errorf("Coerce(:sender without caller) err=%v, want ErrUnsupportedSQL %s", err, label)
		}
	}

	got, err := CoerceWithCaller(lit, kind, &caller)
	if err != nil {
		if err := checkCoerceFuzzError(err); err != nil {
			return fmt.Errorf("%w %s", err, label)
		}
		return nil
	}
	if got.Kind() != kind {
		return fmt.Errorf("CoerceWithCaller returned kind %s, want %s %s", got.Kind(), kind, label)
	}
	again, err := CoerceWithCaller(lit, kind, &caller)
	if err != nil {
		return fmt.Errorf("CoerceWithCaller accepted once then failed: %v %s", err, label)
	}
	if !got.Equal(again) {
		return fmt.Errorf("CoerceWithCaller is not deterministic: first=%+v second=%+v %s", got, again, label)
	}

	if lit.Kind != LitSender {
		withoutCaller, err := Coerce(lit, kind)
		if err != nil {
			return fmt.Errorf("CoerceWithCaller accepted but Coerce failed: %v %s", err, label)
		}
		if !got.Equal(withoutCaller) {
			return fmt.Errorf("Coerce and CoerceWithCaller differ without :sender: caller=%+v direct=%+v %s", got, withoutCaller, label)
		}
	}
	return nil
}

func checkCoerceFuzzError(err error) error {
	var invalid InvalidLiteralError
	var unexpected UnexpectedTypeError
	if errors.Is(err, ErrUnsupportedSQL) ||
		errors.As(err, &invalid) ||
		errors.As(err, &unexpected) {
		return nil
	}
	return fmt.Errorf("CoerceWithCaller returned unclassified error %T: %v", err, err)
}

func coerceFuzzLabel(data []byte, lit Literal, kind types.ValueKind) string {
	if len(data) <= 80 {
		return fmt.Sprintf("seed_len=%d seed=%x kind=%s lit=%#v", len(data), data, kind, lit)
	}
	return fmt.Sprintf("seed_len=%d seed_prefix=%x kind=%s lit=%#v", len(data), data[:80], kind, lit)
}
