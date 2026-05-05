package sql

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ponchione/shunter/types"
)

// Bound big integers for 128/256-bit coerce range checks. Computed once at
// package init so the coerce hot path only does big.Int comparisons.
var (
	uint128Max = new(big.Int).Lsh(big.NewInt(1), 128)
	uint256Max = new(big.Int).Lsh(big.NewInt(1), 256)
	int128Max  = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	int128Min  = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
	int256Max  = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
	int256Min  = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
)

// Coerce converts a parsed literal into a value for the target column kind.
// Use CoerceWithCaller for :sender literals.
func Coerce(lit Literal, kind types.ValueKind) (types.Value, error) {
	return coerceValue(lit, kind, nil)
}

// CoerceWithCaller resolves :sender using the supplied caller identity.
// A nil caller or non-bytes target rejects :sender with ErrUnsupportedSQL.
func CoerceWithCaller(lit Literal, kind types.ValueKind, caller *[32]byte) (types.Value, error) {
	return coerceValue(lit, kind, caller)
}

func coerceValue(lit Literal, kind types.ValueKind, caller *[32]byte) (types.Value, error) {
	if lit.Kind == LitSender {
		if caller == nil {
			return types.Value{}, fmt.Errorf("%w: :sender requires caller identity", ErrUnsupportedSQL)
		}
		// Preserve both raw bytes and hex source text so :sender follows the
		// same coercion paths as a parsed hex literal.
		buf := make([]byte, len(caller))
		copy(buf, caller[:])
		resolved := Literal{Kind: LitBytes, Bytes: buf, Text: hex.EncodeToString(caller[:])}
		return coerceValue(resolved, kind, nil)
	}
	// Numeric columns parse string/hex source text first, then reuse the normal
	// integer and float coercion paths.
	if lit.Kind == LitString && isNumericKind(kind) {
		parsed, err := parseNumericLiteral(lit.Str)
		if err != nil {
			return types.Value{}, invalidLiteralText(lit.Str, kind)
		}
		return coerceValue(parsed, kind, caller)
	}
	if lit.Kind == LitBytes && lit.Text != "" && isNumericKind(kind) {
		parsed, err := parseNumericLiteral(lit.Text)
		if err != nil {
			return types.Value{}, invalidLiteralText(lit.Text, kind)
		}
		return coerceValue(parsed, kind, caller)
	}
	switch kind {
	case types.KindBool:
		if lit.Kind != LitBool {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewBool(lit.Bool), nil
	case types.KindString:
		// Strings preserve source text for string and numeric literals.
		if text, ok := renderLiteralSourceText(lit); ok {
			return types.NewString(text), nil
		}
		return types.Value{}, mismatch(lit, kind)
	case types.KindBytes:
		// Bytes literals bind directly; other source-text literals are decoded
		// as hex and reject as InvalidLiteral on decode failure.
		if lit.Kind == LitBytes {
			return types.NewBytes(lit.Bytes), nil
		}
		if text, ok := renderLiteralSourceText(lit); ok {
			b, err := decodeReferenceHex(text)
			if err == nil {
				return types.NewBytes(b), nil
			}
			return types.Value{}, invalidLiteralText(text, kind)
		}
		return types.Value{}, mismatch(lit, kind)
	case types.KindFloat32:
		switch lit.Kind {
		case LitFloat:
			return types.NewFloat32(float32(lit.Float))
		case LitInt:
			return types.NewFloat32(float32(lit.Int))
		case LitBigInt:
			f, _ := new(big.Float).SetInt(lit.Big).Float64()
			return types.NewFloat32(float32(f))
		default:
			return types.Value{}, mismatch(lit, kind)
		}
	case types.KindFloat64:
		switch lit.Kind {
		case LitFloat:
			return types.NewFloat64(lit.Float)
		case LitInt:
			return types.NewFloat64(float64(lit.Int))
		case LitBigInt:
			f, _ := new(big.Float).SetInt(lit.Big).Float64()
			return types.NewFloat64(f)
		default:
			return types.Value{}, mismatch(lit, kind)
		}
	case types.KindInt8:
		return coerceSigned(lit, kind, math.MinInt8, math.MaxInt8, func(n int64) types.Value { return types.NewInt8(int8(n)) })
	case types.KindInt16:
		return coerceSigned(lit, kind, math.MinInt16, math.MaxInt16, func(n int64) types.Value { return types.NewInt16(int16(n)) })
	case types.KindInt32:
		return coerceSigned(lit, kind, math.MinInt32, math.MaxInt32, func(n int64) types.Value { return types.NewInt32(int32(n)) })
	case types.KindInt64:
		return coerceSigned(lit, kind, math.MinInt64, math.MaxInt64, func(n int64) types.Value { return types.NewInt64(n) })
	case types.KindUint8:
		return coerceUnsigned(lit, kind, math.MaxUint8, func(u uint64) types.Value { return types.NewUint8(uint8(u)) })
	case types.KindUint16:
		return coerceUnsigned(lit, kind, math.MaxUint16, func(u uint64) types.Value { return types.NewUint16(uint16(u)) })
	case types.KindUint32:
		return coerceUnsigned(lit, kind, math.MaxUint32, func(u uint64) types.Value { return types.NewUint32(uint32(u)) })
	case types.KindUint64:
		return coerceUnsigned(lit, kind, math.MaxUint64, func(u uint64) types.Value { return types.NewUint64(u) })
	case types.KindInt128:
		return coerceWideSigned(lit, kind, types.NewInt128FromInt64, coerceBigIntToInt128)
	case types.KindUint128:
		return coerceWideUnsigned(lit, kind, types.NewUint128FromUint64, coerceBigIntToUint128)
	case types.KindInt256:
		return coerceWideSigned(lit, kind, types.NewInt256FromInt64, coerceBigIntToInt256)
	case types.KindUint256:
		return coerceWideUnsigned(lit, kind, types.NewUint256FromUint64, coerceBigIntToUint256)
	case types.KindTimestamp:
		// RFC3339 strings are accepted; other source-text literals reject as
		// InvalidLiteral for the timestamp algebraic type.
		if lit.Kind == LitString {
			if micros, ok := parseTimestampLiteral(lit.Str); ok {
				return types.NewTimestamp(micros), nil
			}
		}
		return invalidSourceTextLiteral(lit, kind)
	case types.KindDuration:
		// Duration string literals use Go's duration grammar and store signed
		// microseconds, matching the runtime duration unit.
		if lit.Kind == LitString {
			if micros, ok := parseDurationLiteral(lit.Str); ok {
				return types.NewDuration(micros), nil
			}
		}
		return invalidSourceTextLiteral(lit, kind)
	case types.KindArrayString:
		// Shunter SQL has no array literal grammar; source-text literals reject.
		return invalidSourceTextLiteral(lit, kind)
	case types.KindUUID:
		if lit.Kind == LitString {
			v, err := types.ParseUUID(lit.Str)
			if err == nil {
				return v, nil
			}
			return types.Value{}, invalidLiteralText(lit.Str, kind)
		}
		return invalidSourceTextLiteral(lit, kind)
	case types.KindJSON:
		if lit.Kind == LitString {
			v, err := types.NewJSON([]byte(lit.Str))
			if err == nil {
				return v, nil
			}
			return types.Value{}, invalidLiteralText(lit.Str, kind)
		}
		return invalidSourceTextLiteral(lit, kind)
	default:
		return types.Value{}, fmt.Errorf("%w: column kind %s not supported by SQL literal coercion", ErrUnsupportedSQL, kind)
	}
}

func coerceSigned(lit Literal, kind types.ValueKind, lo, hi int64, mk func(int64) types.Value) (types.Value, error) {
	// LitBigInt always overflows narrow signed kinds; preserve source text in
	// the InvalidLiteral error when available.
	if lit.Kind == LitBigInt {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	if lit.Kind != LitInt {
		return types.Value{}, mismatch(lit, kind)
	}
	if lit.Int < lo || lit.Int > hi {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	return mk(lit.Int), nil
}

func coerceUnsigned(lit Literal, kind types.ValueKind, hi uint64, mk func(uint64) types.Value) (types.Value, error) {
	// LitBigInt always overflows narrow unsigned kinds; preserve source text in
	// the InvalidLiteral error when available.
	if lit.Kind == LitBigInt {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	if lit.Kind != LitInt {
		return types.Value{}, mismatch(lit, kind)
	}
	if lit.Int < 0 {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	u := uint64(lit.Int)
	if u > hi {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	return mk(u), nil
}

func coerceWideSigned(
	lit Literal,
	kind types.ValueKind,
	mkInt func(int64) types.Value,
	mkBig func(Literal, types.ValueKind) (types.Value, error),
) (types.Value, error) {
	switch lit.Kind {
	case LitInt:
		return mkInt(lit.Int), nil
	case LitBigInt:
		return mkBig(lit, kind)
	default:
		return types.Value{}, mismatch(lit, kind)
	}
}

func coerceWideUnsigned(
	lit Literal,
	kind types.ValueKind,
	mkInt func(uint64) types.Value,
	mkBig func(Literal, types.ValueKind) (types.Value, error),
) (types.Value, error) {
	switch lit.Kind {
	case LitInt:
		if lit.Int < 0 {
			return types.Value{}, invalidLiteralFromSource(lit, kind)
		}
		return mkInt(uint64(lit.Int)), nil
	case LitBigInt:
		return mkBig(lit, kind)
	default:
		return types.Value{}, mismatch(lit, kind)
	}
}

func mismatch(lit Literal, kind types.ValueKind) error {
	// Bool mismatches use the reference UnexpectedType shape.
	if lit.Kind == LitBool && kind != types.KindBool {
		return UnexpectedTypeError{Expected: "Bool", Inferred: algebraicName(kind)}
	}
	// Float-to-integer mismatches report InvalidLiteral with preserved text.
	if lit.Kind == LitFloat && isIntegerKind(kind) {
		return invalidLiteralFromSource(lit, kind)
	}
	// Non-bool source-text literals into Bool report InvalidLiteral.
	if kind == types.KindBool {
		if text, ok := renderLiteralSourceText(lit); ok {
			return invalidLiteralText(text, kind)
		}
	}
	return fmt.Errorf("%w: %s literal cannot be coerced to %s", ErrUnsupportedSQL, lit.Kind, kind)
}

func invalidSourceTextLiteral(lit Literal, kind types.ValueKind) (types.Value, error) {
	if lit.Kind == LitBool {
		return types.Value{}, mismatch(lit, kind)
	}
	text, ok := renderLiteralSourceText(lit)
	if !ok {
		return types.Value{}, mismatch(lit, kind)
	}
	return types.Value{}, invalidLiteralText(text, kind)
}

func invalidLiteralFromSource(lit Literal, kind types.ValueKind) error {
	text, _ := renderLiteralSourceText(lit)
	return invalidLiteralText(text, kind)
}

func invalidLiteralText(text string, kind types.ValueKind) error {
	return InvalidLiteralError{Literal: text, Type: algebraicName(kind)}
}

// renderLiteralSourceText returns preserved parser text when available, then
// falls back to canonical text for numeric and string literals.
func renderLiteralSourceText(lit Literal) (string, bool) {
	if lit.Text != "" {
		return lit.Text, true
	}
	switch lit.Kind {
	case LitInt:
		return strconv.FormatInt(lit.Int, 10), true
	case LitFloat:
		return strconv.FormatFloat(lit.Float, 'g', -1, 64), true
	case LitString:
		return lit.Str, true
	case LitBigInt:
		return lit.Big.String(), true
	default:
		return "", false
	}
}

// decodeReferenceHex decodes the SQL source-text forms accepted for bytes
// columns. Decode errors are returned for the caller to wrap.
func decodeReferenceHex(text string) ([]byte, error) {
	body := text
	if strings.HasPrefix(body, "0x") {
		body = body[2:]
	} else if len(body) >= 2 && body[0] == 'X' && body[1] == '\'' {
		body = body[2:]
	}
	return hex.DecodeString(body)
}

// isIntegerKind reports whether k is any signed or unsigned integer kind.
func isIntegerKind(k types.ValueKind) bool {
	switch k {
	case types.KindInt8, types.KindInt16, types.KindInt32, types.KindInt64,
		types.KindInt128, types.KindInt256,
		types.KindUint8, types.KindUint16, types.KindUint32, types.KindUint64,
		types.KindUint128, types.KindUint256:
		return true
	default:
		return false
	}
}

// isNumericKind reports whether k is an integer or float kind.
func isNumericKind(k types.ValueKind) bool {
	return isIntegerKind(k) || k == types.KindFloat32 || k == types.KindFloat64
}

// UnexpectedTypeError reports a literal algebraic type mismatch.
type UnexpectedTypeError struct {
	Expected string
	Inferred string
}

func (e UnexpectedTypeError) Error() string {
	return fmt.Sprintf("Unexpected type: (expected) %s != %s (inferred)", e.Expected, e.Inferred)
}

func (e UnexpectedTypeError) Unwrap() error { return ErrUnsupportedSQL }

// InvalidLiteralError reports a literal that cannot parse as the target type.
type InvalidLiteralError struct {
	Literal string
	Type    string
}

func (e InvalidLiteralError) Error() string {
	return fmt.Sprintf("The literal expression `%s` cannot be parsed as type `%s`", e.Literal, e.Type)
}

func (e InvalidLiteralError) Unwrap() error { return ErrUnsupportedSQL }

// DuplicateNameError reports a duplicate projection or join alias.
type DuplicateNameError struct {
	Name string
}

func (e DuplicateNameError) Error() string {
	return fmt.Sprintf("Duplicate name `%s`", e.Name)
}

func (e DuplicateNameError) Unwrap() error { return ErrUnsupportedSQL }

// UnresolvedVarError reports an identifier that is not in SQL scope.
type UnresolvedVarError struct {
	Name string
}

func (e UnresolvedVarError) Error() string {
	return fmt.Sprintf("`%s` is not in scope", e.Name)
}

func (e UnresolvedVarError) Unwrap() error { return ErrUnsupportedSQL }

// InvalidOpError reports an unsupported binary operator/type combination.
type InvalidOpError struct {
	Op   string
	Type string
}

func (e InvalidOpError) Error() string {
	return fmt.Sprintf("Invalid binary operator `%s` for type `%s`", e.Op, e.Type)
}

func (e InvalidOpError) Unwrap() error { return ErrUnsupportedSQL }

// UnsupportedSelectError reports unsupported SELECT set quantifiers.
// HasLimit preserves subscription reject ordering for LIMIT plus quantifier.
type UnsupportedSelectError struct {
	SQL      string
	HasLimit bool
}

// Error renders the OneOff/raw form.
func (e UnsupportedSelectError) Error() string {
	return "Unsupported: " + e.SQL
}

// SubscribeError renders the subscription-surface form.
func (e UnsupportedSelectError) SubscribeError() string {
	return "Unsupported SELECT: " + e.SQL
}

func (e UnsupportedSelectError) Unwrap() error { return ErrUnsupportedSQL }

// UnqualifiedNamesError reports a bare identifier where a join requires
// qualified names.
type UnqualifiedNamesError struct{}

func (e UnqualifiedNamesError) Error() string {
	return "Names must be qualified when using joins"
}

func (e UnqualifiedNamesError) Unwrap() error { return ErrUnsupportedSQL }

// UnsupportedJoinTypeError reports a join shape outside Shunter's SQL subset.
type UnsupportedJoinTypeError struct{}

func (e UnsupportedJoinTypeError) Error() string {
	return "Non-inner joins are not supported"
}

func (e UnsupportedJoinTypeError) Unwrap() error { return ErrUnsupportedSQL }

// UnsupportedExprError reports an expression outside the supported grammar.
type UnsupportedExprError struct {
	Expr string
}

func (e UnsupportedExprError) Error() string {
	return "Unsupported expression: " + e.Expr
}

func (e UnsupportedExprError) Unwrap() error { return ErrUnsupportedSQL }

// UnsupportedFeatureError reports a SQL feature outside the active surface.
type UnsupportedFeatureError struct {
	SQL string
}

func (e UnsupportedFeatureError) Error() string {
	return "Unsupported: " + e.SQL
}

func (e UnsupportedFeatureError) Unwrap() error { return ErrUnsupportedSQL }

// AlgebraicName returns the wire/error short name for a ValueKind.
func AlgebraicName(k types.ValueKind) string {
	return algebraicName(k)
}

// algebraicName returns the protocol/error short name for a ValueKind.
func algebraicName(k types.ValueKind) string {
	switch k {
	case types.KindBool:
		return "Bool"
	case types.KindInt8:
		return "I8"
	case types.KindUint8:
		return "U8"
	case types.KindInt16:
		return "I16"
	case types.KindUint16:
		return "U16"
	case types.KindInt32:
		return "I32"
	case types.KindUint32:
		return "U32"
	case types.KindInt64:
		return "I64"
	case types.KindUint64:
		return "U64"
	case types.KindInt128:
		return "I128"
	case types.KindUint128:
		return "U128"
	case types.KindInt256:
		return "I256"
	case types.KindUint256:
		return "U256"
	case types.KindFloat32:
		return "F32"
	case types.KindFloat64:
		return "F64"
	case types.KindString:
		return "String"
	case types.KindBytes:
		return "Array<U8>"
	case types.KindTimestamp:
		return "(__timestamp_micros_since_unix_epoch__: I64)"
	case types.KindDuration:
		return "(__duration_micros__: I64)"
	case types.KindArrayString:
		return "Array<String>"
	case types.KindUUID:
		return "UUID"
	case types.KindJSON:
		return "JSON"
	default:
		return k.String()
	}
}

// String returns a human-readable label for a LitKind.
func (k LitKind) String() string {
	switch k {
	case LitInt:
		return "integer"
	case LitFloat:
		return "float"
	case LitBool:
		return "bool"
	case LitString:
		return "string"
	case LitBytes:
		return "bytes"
	case LitSender:
		return ":sender"
	case LitBigInt:
		return "bigint"
	default:
		return "unknown"
	}
}

// timestampLayouts mirror the reference chrono::DateTime::parse_from_rfc3339
// surface that accepts both `T` and space as the date/time separator and
// accepts variable-precision fractional seconds (up to nanoseconds). Go's
// `2006-01-02T15:04:05.999999999Z07:00` layout treats trailing 9s as optional
// fractional-second digits, so the same layout covers `Z`-suffixed and
// numeric-offset forms with or without a fractional component.
var timestampLayouts = [...]string{
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
}

// parseTimestampLiteral converts an RFC3339-style string into microseconds
// since the Unix epoch. Nanosecond precision is truncated via time.UnixMicro,
// matching reference `Timestamp::parse_from_rfc3339` -> `timestamp_micros()`.
func parseTimestampLiteral(s string) (int64, bool) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMicro(), true
		}
	}
	return 0, false
}

func parseDurationLiteral(s string) (int64, bool) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return d.Microseconds(), true
}

// coerceBigIntToInt128 binds a big.Int literal to a 128-bit signed column.
// Rejects |x| that overflows [-2^127, 2^127-1]. For negative values the
// two's-complement encoding is materialized via `x + 2^128` before splitting
// into (hi, lo) uint64 words — matches types.NewInt128's hi(signed)/lo(unsigned)
// layout. InvalidLiteral text routes through `renderLiteralSourceText` so
// the preserved numeric token (`1e40` etc.) survives over canonical decimal.
func coerceBigIntToInt128(lit Literal, kind types.ValueKind) (types.Value, error) {
	x := lit.Big
	if x.Cmp(int128Max) > 0 || x.Cmp(int128Min) < 0 {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	t := new(big.Int).Set(x)
	if t.Sign() < 0 {
		t.Add(t, uint128Max)
	}
	var buf [16]byte
	t.FillBytes(buf[:])
	hi := int64(binary.BigEndian.Uint64(buf[0:8]))
	lo := binary.BigEndian.Uint64(buf[8:16])
	return types.NewInt128(hi, lo), nil
}

// coerceBigIntToUint128 binds a big.Int literal to a 128-bit unsigned column.
// Rejects negative values and values >= 2^128.
func coerceBigIntToUint128(lit Literal, kind types.ValueKind) (types.Value, error) {
	x := lit.Big
	if x.Sign() < 0 || x.Cmp(uint128Max) >= 0 {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	var buf [16]byte
	x.FillBytes(buf[:])
	hi := binary.BigEndian.Uint64(buf[0:8])
	lo := binary.BigEndian.Uint64(buf[8:16])
	return types.NewUint128(hi, lo), nil
}

// coerceBigIntToInt256 binds a big.Int literal to a 256-bit signed column.
// Rejects |x| that overflows [-2^255, 2^255-1]. Negative values materialize
// through `x + 2^256` for two's-complement encoding.
func coerceBigIntToInt256(lit Literal, kind types.ValueKind) (types.Value, error) {
	x := lit.Big
	if x.Cmp(int256Max) > 0 || x.Cmp(int256Min) < 0 {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	t := new(big.Int).Set(x)
	if t.Sign() < 0 {
		t.Add(t, uint256Max)
	}
	var buf [32]byte
	t.FillBytes(buf[:])
	w0 := int64(binary.BigEndian.Uint64(buf[0:8]))
	w1 := binary.BigEndian.Uint64(buf[8:16])
	w2 := binary.BigEndian.Uint64(buf[16:24])
	w3 := binary.BigEndian.Uint64(buf[24:32])
	return types.NewInt256(w0, w1, w2, w3), nil
}

// coerceBigIntToUint256 binds a big.Int literal to a 256-bit unsigned column.
// Rejects negative values and values >= 2^256. The reference-informed target
// `u256 = 1e40` (check.rs:330-332) goes through this path — 10^40 fits
// comfortably in u256 (max ~1.16e77).
func coerceBigIntToUint256(lit Literal, kind types.ValueKind) (types.Value, error) {
	x := lit.Big
	if x.Sign() < 0 || x.Cmp(uint256Max) >= 0 {
		return types.Value{}, invalidLiteralFromSource(lit, kind)
	}
	var buf [32]byte
	x.FillBytes(buf[:])
	w0 := binary.BigEndian.Uint64(buf[0:8])
	w1 := binary.BigEndian.Uint64(buf[8:16])
	w2 := binary.BigEndian.Uint64(buf[16:24])
	w3 := binary.BigEndian.Uint64(buf[24:32])
	return types.NewUint256(w0, w1, w2, w3), nil
}

func parseHexLiteral(text string) ([]byte, error) {
	body := text
	if len(body) >= 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') {
		body = body[2:]
	} else if len(body) >= 3 && (body[0] == 'X' || body[0] == 'x') && body[1] == '\'' && body[len(body)-1] == '\'' {
		body = body[2 : len(body)-1]
	}
	if len(body) == 0 || len(body)%2 != 0 {
		return nil, fmt.Errorf("%w: malformed hex literal %q", ErrUnsupportedSQL, text)
	}
	decoded, err := hex.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed hex literal %q", ErrUnsupportedSQL, text)
	}
	return decoded, nil
}
