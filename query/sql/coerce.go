package sql

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strconv"
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

// Coerce turns a parsed Literal into a types.Value matching the target
// column kind. Mismatched categories (string-literal into an integer
// column, negative literal into an unsigned column, integer out of range
// for a narrower signed kind) return ErrUnsupportedSQL.
//
// Float and bytes kinds are reachable from the current SQL literal grammar.
// A LitSender marker cannot be resolved without a caller identity; the
// caller must route through CoerceWithCaller instead.
func Coerce(lit Literal, kind types.ValueKind) (types.Value, error) {
	if lit.Kind == LitSender {
		return types.Value{}, fmt.Errorf("%w: :sender requires caller identity", ErrUnsupportedSQL)
	}
	return coerceValue(lit, kind, nil)
}

// CoerceWithCaller is Coerce with an out-of-band caller identity supplied
// for :sender parameter resolution. `caller` materializes as the 32-byte
// identity payload on KindBytes columns (the Shunter representation of
// both reference `identity()` and `bytes()` columns used on the
// `select * from s where id = :sender` / `bytes = :sender` surface).
// Passing a nil caller while the literal is LitSender returns
// ErrUnsupportedSQL; non-bytes column kinds reject the marker in the
// same way the reference typechecker rejects `arr = :sender`.
func CoerceWithCaller(lit Literal, kind types.ValueKind, caller *[32]byte) (types.Value, error) {
	return coerceValue(lit, kind, caller)
}

func coerceValue(lit Literal, kind types.ValueKind, caller *[32]byte) (types.Value, error) {
	if lit.Kind == LitSender {
		if caller == nil {
			return types.Value{}, fmt.Errorf("%w: :sender requires caller identity", ErrUnsupportedSQL)
		}
		if kind != types.KindBytes {
			return types.Value{}, fmt.Errorf("%w: :sender parameter cannot be coerced to %s", ErrUnsupportedSQL, kind)
		}
		out := make([]byte, len(caller))
		copy(out, caller[:])
		return types.NewBytes(out), nil
	}
	switch kind {
	case types.KindBool:
		if lit.Kind != LitBool {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewBool(lit.Bool), nil
	case types.KindString:
		// Reference `parse(value, AlgebraicType::String)` at
		// expr/src/lib.rs:353 wraps the SqlLiteral source text as
		// `AlgebraicValue::String(value.into())` for any of `Str | Num | Hex`
		// literal categories. Shunter widens LitString / LitInt / LitFloat /
		// LitBigInt onto KindString through `renderLiteralSourceText`
		// (FormatInt / FormatFloat / lit.Str / Big.String). LitBytes is
		// deferred — Shunter's parser decodes the hex source token into bytes
		// at `parseHexLiteral`, so the original `0x...` / `X'...'` form is
		// not recoverable; it falls through to `mismatch` until the
		// source-text-preservation slice lands. LitBool falls through to
		// `mismatch` and emits `UnexpectedType{Bool, String}` matching
		// reference lib.rs:94 (only `Str | Num | Hex` reach the lib.rs:353
		// String arm). LitSender is short-circuited above for non-Bytes
		// columns.
		if text, ok := renderLiteralSourceText(lit); ok {
			return types.NewString(text), nil
		}
		return types.Value{}, mismatch(lit, kind)
	case types.KindBytes:
		if lit.Kind != LitBytes {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewBytes(lit.Bytes), nil
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
		switch lit.Kind {
		case LitInt:
			return types.NewInt128FromInt64(lit.Int), nil
		case LitBigInt:
			return coerceBigIntToInt128(lit.Big, kind)
		default:
			return types.Value{}, mismatch(lit, kind)
		}
	case types.KindUint128:
		switch lit.Kind {
		case LitInt:
			if lit.Int < 0 {
				return types.Value{}, InvalidLiteralError{Literal: strconv.FormatInt(lit.Int, 10), Type: algebraicName(kind)}
			}
			return types.NewUint128FromUint64(uint64(lit.Int)), nil
		case LitBigInt:
			return coerceBigIntToUint128(lit.Big, kind)
		default:
			return types.Value{}, mismatch(lit, kind)
		}
	case types.KindInt256:
		switch lit.Kind {
		case LitInt:
			return types.NewInt256FromInt64(lit.Int), nil
		case LitBigInt:
			return coerceBigIntToInt256(lit.Big, kind)
		default:
			return types.Value{}, mismatch(lit, kind)
		}
	case types.KindUint256:
		switch lit.Kind {
		case LitInt:
			if lit.Int < 0 {
				return types.Value{}, InvalidLiteralError{Literal: strconv.FormatInt(lit.Int, 10), Type: algebraicName(kind)}
			}
			return types.NewUint256FromUint64(uint64(lit.Int)), nil
		case LitBigInt:
			return coerceBigIntToUint256(lit.Big, kind)
		default:
			return types.Value{}, mismatch(lit, kind)
		}
	case types.KindTimestamp:
		if lit.Kind != LitString {
			return types.Value{}, mismatch(lit, kind)
		}
		micros, ok := parseTimestampLiteral(lit.Str)
		if !ok {
			return types.Value{}, fmt.Errorf("%w: malformed timestamp literal %q (expected RFC3339)", ErrUnsupportedSQL, lit.Str)
		}
		return types.NewTimestamp(micros), nil
	case types.KindArrayString:
		return types.Value{}, mismatch(lit, kind)
	default:
		return types.Value{}, fmt.Errorf("%w: column kind %s not supported by SQL literal coercion", ErrUnsupportedSQL, kind)
	}
}

func coerceSigned(lit Literal, kind types.ValueKind, lo, hi int64, mk func(int64) types.Value) (types.Value, error) {
	if lit.Kind != LitInt {
		return types.Value{}, mismatch(lit, kind)
	}
	if lit.Int < lo || lit.Int > hi {
		return types.Value{}, InvalidLiteralError{Literal: strconv.FormatInt(lit.Int, 10), Type: algebraicName(kind)}
	}
	return mk(lit.Int), nil
}

func coerceUnsigned(lit Literal, kind types.ValueKind, hi uint64, mk func(uint64) types.Value) (types.Value, error) {
	if lit.Kind != LitInt {
		return types.Value{}, mismatch(lit, kind)
	}
	if lit.Int < 0 {
		return types.Value{}, InvalidLiteralError{Literal: strconv.FormatInt(lit.Int, 10), Type: algebraicName(kind)}
	}
	u := uint64(lit.Int)
	if u > hi {
		return types.Value{}, InvalidLiteralError{Literal: strconv.FormatInt(lit.Int, 10), Type: algebraicName(kind)}
	}
	return mk(u), nil
}

func mismatch(lit Literal, kind types.ValueKind) error {
	// Bool literal into a non-bool column: emit the reference
	// `UnexpectedType` text verbatim (expr/src/errors.rs:100, emitted at
	// expr/src/lib.rs:94 for `(SqlExpr::Lit(SqlLiteral::Bool(_)), Some(ty))`).
	// Other mismatch categories keep the current coerce-level text; their
	// reference counterparts (`InvalidLiteral` for Str/Hex) are
	// separate parity slices.
	if lit.Kind == LitBool && kind != types.KindBool {
		return UnexpectedTypeError{Expected: "Bool", Inferred: algebraicName(kind)}
	}
	// Float literal into a non-float numeric (integer) column: reference
	// `parse_int(BigDecimal, ty)` (expr/src/lib.rs:99) rejects fractional
	// BigDecimals via `BigDecimal::to_{i,u}{8..256}` returning None, and
	// the outer `.map_err` folds the anyhow into `InvalidLiteral::new`
	// (expr/src/errors.rs:84). Render via `strconv.FormatFloat('g', -1, 64)`
	// so the canonical decimal text of the float carries into the literal
	// slot; source-text preservation for forms that round-trip differently
	// (e.g. `1.10` → "1.1") is a separate future slice.
	if lit.Kind == LitFloat && isIntegerKind(kind) {
		return InvalidLiteralError{Literal: strconv.FormatFloat(lit.Float, 'g', -1, 64), Type: algebraicName(kind)}
	}
	// Non-Bool primitive literal into a Bool column: reference
	// `parse(value, AlgebraicType::Bool)` has no Bool arm in the type-match
	// and falls through to the catch-all `bail!("Literal values for type
	// {} are not supported")`, which the outer `.map_err` at lib.rs:99
	// folds into `InvalidLiteral::new(v.into_string(), Bool)`. Shunter's
	// LitBytes has no preserved source text and is skipped here (separate
	// slice once `Literal` carries a canonical hex or `Text` field).
	if kind == types.KindBool {
		if text, ok := renderLiteralSourceText(lit); ok {
			return InvalidLiteralError{Literal: text, Type: "Bool"}
		}
	}
	return fmt.Errorf("%w: %s literal cannot be coerced to %s", ErrUnsupportedSQL, lit.Kind, kind)
}

// renderLiteralSourceText reconstructs the reference source text for a
// Literal for use in `InvalidLiteralError.Literal`. Matches the reference
// `SqlLiteral::Str(v) | SqlLiteral::Num(v) | SqlLiteral::Hex(v)` →
// `v.into_string()` renderings (expr/src/lib.rs:94). Returns false for
// LitBool (which never reaches InvalidLiteral in reference), LitSender
// (a Shunter-only marker), and LitBytes (no preserved source text — a
// canonical hex or `Text` field on Literal is a separate slice).
func renderLiteralSourceText(lit Literal) (string, bool) {
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

// isIntegerKind reports whether a ValueKind is one of the signed or
// unsigned integer primitives (I8..I256, U8..U256). Used by `mismatch` to
// route LitFloat→integer-column failures onto the reference
// `InvalidLiteral` text path.
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

// UnexpectedTypeError mirrors reference `expr::errors::UnexpectedType`
// (reference/SpacetimeDB/crates/expr/src/errors.rs:99-114). Emitted by the
// coerce boundary when a literal's algebraic type cannot bind to the
// column's algebraic type. Unwrap()s to ErrUnsupportedSQL so callers that
// classify by sentinel still match.
type UnexpectedTypeError struct {
	Expected string
	Inferred string
}

func (e UnexpectedTypeError) Error() string {
	return fmt.Sprintf("Unexpected type: (expected) %s != %s (inferred)", e.Expected, e.Inferred)
}

func (e UnexpectedTypeError) Unwrap() error { return ErrUnsupportedSQL }

// InvalidLiteralError mirrors reference `expr::errors::InvalidLiteral`
// (reference/SpacetimeDB/crates/expr/src/errors.rs:83-97). Emitted by the
// coerce boundary when an integer literal is out of range for the target
// column kind (reference emits this via `lib.rs:99` when `parse(v, ty)`
// rejects the source text). Unwrap()s to ErrUnsupportedSQL so callers that
// classify by sentinel still match. `Literal` carries the reconstructed
// decimal text of the integer value; scientific-notation literals collapse
// to LitInt at the Shunter parser and so render via `strconv.FormatInt`
// rather than the original source token — parity for that surface requires
// preserving the source text on Literal and is out of scope here.
type InvalidLiteralError struct {
	Literal string
	Type    string
}

func (e InvalidLiteralError) Error() string {
	return fmt.Sprintf("The literal expression `%s` cannot be parsed as type `%s`", e.Literal, e.Type)
}

func (e InvalidLiteralError) Unwrap() error { return ErrUnsupportedSQL }

// algebraicName returns the reference `fmt_algebraic_type` short name for a
// ValueKind (reference/SpacetimeDB/crates/sats/src/algebraic_type/fmt.rs
// lines 15-40). Primitives use capitalized short tokens (`Bool`, `U32`,
// `F32`, etc.); bytes renders as `Array<U8>`. Non-primitive kinds
// (Timestamp, ArrayString) are not reachable through the bool-literal
// mismatch path today; they fall back to the ValueKind stringer.
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

// coerceBigIntToInt128 binds a big.Int literal to a 128-bit signed column.
// Rejects |x| that overflows [-2^127, 2^127-1]. For negative values the
// two's-complement encoding is materialized via `x + 2^128` before splitting
// into (hi, lo) uint64 words — matches types.NewInt128's hi(signed)/lo(unsigned)
// layout.
func coerceBigIntToInt128(x *big.Int, kind types.ValueKind) (types.Value, error) {
	if x.Cmp(int128Max) > 0 || x.Cmp(int128Min) < 0 {
		return types.Value{}, InvalidLiteralError{Literal: x.String(), Type: algebraicName(kind)}
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
func coerceBigIntToUint128(x *big.Int, kind types.ValueKind) (types.Value, error) {
	if x.Sign() < 0 {
		return types.Value{}, InvalidLiteralError{Literal: x.String(), Type: algebraicName(kind)}
	}
	if x.Cmp(uint128Max) >= 0 {
		return types.Value{}, InvalidLiteralError{Literal: x.String(), Type: algebraicName(kind)}
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
func coerceBigIntToInt256(x *big.Int, kind types.ValueKind) (types.Value, error) {
	if x.Cmp(int256Max) > 0 || x.Cmp(int256Min) < 0 {
		return types.Value{}, InvalidLiteralError{Literal: x.String(), Type: algebraicName(kind)}
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
// Rejects negative values and values >= 2^256. The reference parity target
// `u256 = 1e40` (check.rs:330-332) goes through this path — 10^40 fits
// comfortably in u256 (max ~1.16e77).
func coerceBigIntToUint256(x *big.Int, kind types.ValueKind) (types.Value, error) {
	if x.Sign() < 0 {
		return types.Value{}, InvalidLiteralError{Literal: x.String(), Type: algebraicName(kind)}
	}
	if x.Cmp(uint256Max) >= 0 {
		return types.Value{}, InvalidLiteralError{Literal: x.String(), Type: algebraicName(kind)}
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
	decoded := make([]byte, len(body)/2)
	for i := 0; i < len(body); i += 2 {
		u, err := strconv.ParseUint(body[i:i+2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed hex literal %q", ErrUnsupportedSQL, text)
		}
		decoded[i/2] = byte(u)
	}
	return decoded, nil
}
