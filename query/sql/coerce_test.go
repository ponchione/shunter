package sql

import (
	"errors"
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

func TestCoerceUnsupportedKind(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: 1}, types.KindFloat64)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL (floats deferred)", err)
	}
}
