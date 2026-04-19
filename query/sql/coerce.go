package sql

import (
	"fmt"
	"math"

	"github.com/ponchione/shunter/types"
)

// Coerce turns a parsed Literal into a types.Value matching the target
// column kind. Mismatched categories (string-literal into an integer
// column, negative literal into an unsigned column, integer out of range
// for a narrower signed kind) return ErrUnsupportedSQL.
//
// Float and bytes kinds are not yet reachable from the minimum-viable
// grammar — they return ErrUnsupportedSQL until the grammar widens.
func Coerce(lit Literal, kind types.ValueKind) (types.Value, error) {
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
	case LitBool:
		return "bool"
	case LitString:
		return "string"
	default:
		return "unknown"
	}
}
