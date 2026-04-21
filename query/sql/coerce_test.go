package sql

import (
	"errors"
	"math"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestCoerceIntToUnsigned(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 7}, types.KindUint32)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindUint32 || v.AsUint32() != 7 {
		t.Fatalf("got %+v", v)
	}
}

func TestCoerceNegativeIntoUnsignedFails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: -1}, types.KindUint64)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceIntToSignedRangeCheck(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: 200}, types.KindInt8)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
	v, err := Coerce(Literal{Kind: LitInt, Int: -128}, types.KindInt8)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.AsInt8() != -128 {
		t.Fatalf("got %d", v.AsInt8())
	}
}

func TestCoerceStringToString(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitString, Str: "abc"}, types.KindString)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.AsString() != "abc" {
		t.Fatalf("got %q", v.AsString())
	}
}

func TestCoerceStringToIntFails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitString, Str: "42"}, types.KindUint64)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceRejectsStringLiteralOnUint32Column pins the reference type-check
// rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines 498-501
// (`select * from t where u32 = 'str'` / "Field u32 is not a string"). Shunter
// surfaces the rejection at the coerce boundary rather than a dedicated
// type-check pass, so a LitString against a KindUint32 column must return
// ErrUnsupportedSQL and must not quietly produce a value.
func TestCoerceRejectsStringLiteralOnUint32Column(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitString, Str: "str"}, types.KindUint32)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceRejectsFloatLiteralOnUint32Column pins the reference type-check
// rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines 502-504
// (`select * from t where t.u32 = 1.3` / "Field u32 is not a float"). A
// LitFloat against an integer column must return ErrUnsupportedSQL at the
// coerce boundary so the admission surface never folds the float into the
// integer kind silently.
func TestCoerceRejectsFloatLiteralOnUint32Column(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.3}, types.KindUint32)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceBoolToBool(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitBool, Bool: true}, types.KindBool)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !v.AsBool() {
		t.Fatal("want true")
	}
}

func TestCoerceBoolToStringFails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitBool, Bool: true}, types.KindString)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceIntegerLiteralPromotesToFloat64 pins the reference behavior at
// crates/expr/src/lib.rs (parse_float via BigDecimal) where an integer-shaped
// literal binds to a float column by promotion. Under the 2026-04-21
// scientific-notation slice, `select * from t where f32 = 1e3` parses to
// LitInt(1000) (integral collapse) and must coerce to a float column as
// 1000.0 rather than a kind-mismatch error.
func TestCoerceIntegerLiteralPromotesToFloat64(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 1}, types.KindFloat64)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindFloat64 || v.AsFloat64() != 1.0 {
		t.Fatalf("got %+v, want 1.0 float64", v)
	}
}

// TestCoerceIntegerLiteralPromotesToFloat32 mirrors the Float64 case for a
// f32 column. Integer-shaped scientific notation literals (`1e3`) must bind
// to f32 columns as 1000.0.
func TestCoerceIntegerLiteralPromotesToFloat32(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 1000}, types.KindFloat32)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindFloat32 || v.AsFloat32() != 1000.0 {
		t.Fatalf("got %+v, want 1000.0 float32", v)
	}
}

// TestCoerceFloatLiteralOverflowsToFloat32Infinity pins the reference
// Infinity path at check.rs:326-328 (`select * from t where f32 = 1e40`):
// a magnitude beyond float32 range is accepted as +Inf on the f32 column
// rather than rejected.
func TestCoerceFloatLiteralOverflowsToFloat32Infinity(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitFloat, Float: 1e40}, types.KindFloat32)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindFloat32 {
		t.Fatalf("Kind = %v, want KindFloat32", v.Kind())
	}
	got := v.AsFloat32()
	if !math.IsInf(float64(got), 1) {
		t.Fatalf("AsFloat32 = %v, want +Inf", got)
	}
}

func TestCoerceSenderWithoutCallerFails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitSender}, types.KindBytes)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceSenderWithCallerToBytes(t *testing.T) {
	caller := [32]byte{1, 2, 3}
	v, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindBytes, &caller)
	if err != nil {
		t.Fatalf("CoerceWithCaller error: %v", err)
	}
	if v.Kind() != types.KindBytes {
		t.Fatalf("Kind = %v, want KindBytes", v.Kind())
	}
	got := v.AsBytes()
	if len(got) != 32 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("AsBytes = %x, want caller identity bytes", got)
	}
}

func TestCoerceSenderRejectsNonBytesColumn(t *testing.T) {
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindString, &caller)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}
