package sql

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/ponchione/shunter/types"
	"pgregory.net/rapid"
)

type rapidIntegerCoerceCase struct {
	Kind types.ValueKind
	N    int64
}

type rapidIntegerRejectCase struct {
	Kind types.ValueKind
	Lit  Literal
}

var rapidIntegerKinds = []types.ValueKind{
	types.KindInt8,
	types.KindUint8,
	types.KindInt16,
	types.KindUint16,
	types.KindInt32,
	types.KindUint32,
	types.KindInt64,
	types.KindUint64,
}

var rapidNumericCoerceKinds = []types.ValueKind{
	types.KindInt8,
	types.KindUint8,
	types.KindInt16,
	types.KindUint16,
	types.KindInt32,
	types.KindUint32,
	types.KindInt64,
	types.KindUint64,
	types.KindInt128,
	types.KindUint128,
	types.KindInt256,
	types.KindUint256,
	types.KindFloat32,
	types.KindFloat64,
}

func rapidIntegerWithinRangeCase() *rapid.Generator[rapidIntegerCoerceCase] {
	return rapid.Custom(func(t *rapid.T) rapidIntegerCoerceCase {
		kind := rapid.SampledFrom(rapidIntegerKinds).Draw(t, "kind")
		lo, hi := rapidLiteralIntRange(kind)
		return rapidIntegerCoerceCase{
			Kind: kind,
			N:    rapid.Int64Range(lo, hi).Draw(t, "n"),
		}
	})
}

func rapidIntegerOutsideRangeCase() *rapid.Generator[rapidIntegerRejectCase] {
	return rapid.Custom(func(t *rapid.T) rapidIntegerRejectCase {
		kind := rapid.SampledFrom([]types.ValueKind{
			types.KindInt8,
			types.KindUint8,
			types.KindInt16,
			types.KindUint16,
			types.KindInt32,
			types.KindUint32,
		}).Draw(t, "kind")
		lo, hi := rapidLiteralIntRange(kind)
		var n int64
		if rapid.Bool().Draw(t, "below") {
			n = lo - 1
		} else {
			n = hi + 1
		}
		text := strconv.FormatInt(n, 10)
		return rapidIntegerRejectCase{
			Kind: kind,
			Lit:  Literal{Kind: LitInt, Int: n, Text: text},
		}
	})
}

func rapidLiteralIntRange(kind types.ValueKind) (int64, int64) {
	switch kind {
	case types.KindInt8:
		return math.MinInt8, math.MaxInt8
	case types.KindUint8:
		return 0, math.MaxUint8
	case types.KindInt16:
		return math.MinInt16, math.MaxInt16
	case types.KindUint16:
		return 0, math.MaxUint16
	case types.KindInt32:
		return math.MinInt32, math.MaxInt32
	case types.KindUint32:
		return 0, math.MaxUint32
	case types.KindInt64:
		return math.MinInt64, math.MaxInt64
	case types.KindUint64:
		return 0, math.MaxInt64
	default:
		panic("not an integer literal range kind")
	}
}

func TestRapidCoerceIntegerWithinRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tc := rapidIntegerWithinRangeCase().Draw(t, "case")
		lit := Literal{Kind: LitInt, Int: tc.N, Text: strconv.FormatInt(tc.N, 10)}

		got, err := Coerce(lit, tc.Kind)
		if err != nil {
			t.Fatalf("Coerce(%+v, %s): %v", lit, tc.Kind, err)
		}
		if got.Kind() != tc.Kind {
			t.Fatalf("Kind = %s, want %s", got.Kind(), tc.Kind)
		}
		if !rapidIntegerValueMatches(got, tc.N) {
			t.Fatalf("coerced value = %+v, want literal integer %d", got, tc.N)
		}
	})
}

func TestRapidCoerceIntegerOutsideRangeRejects(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tc := rapidIntegerOutsideRangeCase().Draw(t, "case")

		_, err := Coerce(tc.Lit, tc.Kind)
		if !errors.Is(err, ErrUnsupportedSQL) {
			t.Fatalf("Coerce(%+v, %s) err = %v, want ErrUnsupportedSQL", tc.Lit, tc.Kind, err)
		}
		var invalid InvalidLiteralError
		if !errors.As(err, &invalid) {
			t.Fatalf("Coerce(%+v, %s) err = %v, want InvalidLiteralError", tc.Lit, tc.Kind, err)
		}
		if invalid.Literal != tc.Lit.Text || invalid.Type != AlgebraicName(tc.Kind) {
			t.Fatalf("InvalidLiteralError = {%q, %q}, want {%q, %q}", invalid.Literal, invalid.Type, tc.Lit.Text, AlgebraicName(tc.Kind))
		}
	})
}

func TestRapidCoerceStringNumericMatchesParsedNumeric(t *testing.T) {
	tokens := []string{
		"0",
		"42",
		"-7",
		"001",
		"+1000",
		"1e3",
		"1E3",
		"1.5",
		"1e-3",
		"1e40",
	}

	rapid.Check(t, func(t *rapid.T) {
		token := rapid.SampledFrom(tokens).Draw(t, "token")
		kind := rapid.SampledFrom(rapidNumericCoerceKinds).Draw(t, "kind")

		stmt, err := Parse("SELECT * FROM t WHERE c = " + token)
		if err != nil {
			t.Fatalf("Parse numeric token %q: %v", token, err)
		}
		if len(stmt.Filters) != 1 {
			t.Fatalf("Filters len = %d, want 1", len(stmt.Filters))
		}

		parsedValue, parsedErr := Coerce(stmt.Filters[0].Literal, kind)
		stringValue, stringErr := Coerce(Literal{Kind: LitString, Str: token, Text: token}, kind)
		if parsedErr != nil || stringErr != nil {
			if parsedErr == nil || stringErr == nil {
				t.Fatalf("parsed/string coerce disagreement for %q -> %s: parsed=(%+v,%v) string=(%+v,%v)", token, kind, parsedValue, parsedErr, stringValue, stringErr)
			}
			if !errors.Is(parsedErr, ErrUnsupportedSQL) || !errors.Is(stringErr, ErrUnsupportedSQL) {
				t.Fatalf("coerce errors for %q -> %s = %v / %v, want ErrUnsupportedSQL", token, kind, parsedErr, stringErr)
			}
			return
		}
		if !parsedValue.Equal(stringValue) {
			t.Fatalf("coerce values for %q -> %s differ: parsed=%+v string=%+v", token, kind, parsedValue, stringValue)
		}
	})
}

func rapidIntegerValueMatches(v types.Value, n int64) bool {
	switch v.Kind() {
	case types.KindInt8:
		return int64(v.AsInt8()) == n
	case types.KindUint8:
		return int64(v.AsUint8()) == n
	case types.KindInt16:
		return int64(v.AsInt16()) == n
	case types.KindUint16:
		return int64(v.AsUint16()) == n
	case types.KindInt32:
		return int64(v.AsInt32()) == n
	case types.KindUint32:
		return int64(v.AsUint32()) == n
	case types.KindInt64:
		return v.AsInt64() == n
	case types.KindUint64:
		return v.AsUint64() == uint64(n)
	default:
		return false
	}
}
