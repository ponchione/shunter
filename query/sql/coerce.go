package sql

import (
	"fmt"
	"math"
	"strconv"

	"github.com/ponchione/shunter/types"
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
		if lit.Kind != LitString {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewString(lit.Str), nil
	case types.KindBytes:
		if lit.Kind != LitBytes {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewBytes(lit.Bytes), nil
	case types.KindFloat32:
		if lit.Kind != LitFloat {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewFloat32(float32(lit.Float))
	case types.KindFloat64:
		if lit.Kind != LitFloat {
			return types.Value{}, mismatch(lit, kind)
		}
		return types.NewFloat64(lit.Float)
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
	default:
		return types.Value{}, fmt.Errorf("%w: column kind %s not supported by SQL literal coercion", ErrUnsupportedSQL, kind)
	}
}

func coerceSigned(lit Literal, kind types.ValueKind, lo, hi int64, mk func(int64) types.Value) (types.Value, error) {
	if lit.Kind != LitInt {
		return types.Value{}, mismatch(lit, kind)
	}
	if lit.Int < lo || lit.Int > hi {
		return types.Value{}, fmt.Errorf("%w: literal %d out of range for %s", ErrUnsupportedSQL, lit.Int, kind)
	}
	return mk(lit.Int), nil
}

func coerceUnsigned(lit Literal, kind types.ValueKind, hi uint64, mk func(uint64) types.Value) (types.Value, error) {
	if lit.Kind != LitInt {
		return types.Value{}, mismatch(lit, kind)
	}
	if lit.Int < 0 {
		return types.Value{}, fmt.Errorf("%w: negative literal %d cannot fit unsigned %s", ErrUnsupportedSQL, lit.Int, kind)
	}
	u := uint64(lit.Int)
	if u > hi {
		return types.Value{}, fmt.Errorf("%w: literal %d out of range for %s", ErrUnsupportedSQL, lit.Int, kind)
	}
	return mk(u), nil
}

func mismatch(lit Literal, kind types.ValueKind) error {
	return fmt.Errorf("%w: %s literal cannot be coerced to %s", ErrUnsupportedSQL, lit.Kind, kind)
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
	default:
		return "unknown"
	}
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
