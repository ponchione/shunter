package schema

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestValueKindExportStringAll(t *testing.T) {
	cases := []struct {
		k    ValueKind
		want string
	}{
		{KindBool, "bool"},
		{KindInt8, "int8"},
		{KindUint8, "uint8"},
		{KindInt16, "int16"},
		{KindUint16, "uint16"},
		{KindInt32, "int32"},
		{KindUint32, "uint32"},
		{KindInt64, "int64"},
		{KindUint64, "uint64"},
		{KindFloat32, "float32"},
		{KindFloat64, "float64"},
		{KindString, "string"},
		{KindBytes, "bytes"},
		{KindInt128, "int128"},
		{KindUint128, "uint128"},
		{KindInt256, "int256"},
		{KindUint256, "uint256"},
		{KindTimestamp, "timestamp"},
		{KindArrayString, "arrayString"},
		{KindUUID, "uuid"},
		{KindDuration, "duration"},
	}
	for _, c := range cases {
		got := ValueKindExportString(c.k)
		if got != c.want {
			t.Errorf("ValueKindExportString(%v) = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestValueKindExportStringInvalid(t *testing.T) {
	for _, k := range []ValueKind{ValueKind(-1), ValueKind(99)} {
		if got := ValueKindExportString(k); got != "" {
			t.Fatalf("ValueKindExportString(%v) = %q, want empty string", k, got)
		}
	}
}

func TestAutoIncrementBoundsInt8(t *testing.T) {
	min, max, ok := AutoIncrementBounds(KindInt8)
	if !ok {
		t.Fatal("Int8 should be auto-increment eligible")
	}
	if min != math.MinInt8 {
		t.Errorf("Int8 min = %d, want %d", min, int64(math.MinInt8))
	}
	if max != math.MaxInt8 {
		t.Errorf("Int8 max = %d, want %d", max, uint64(math.MaxInt8))
	}
}

func TestAutoIncrementBoundsUint64(t *testing.T) {
	min, max, ok := AutoIncrementBounds(KindUint64)
	if !ok {
		t.Fatal("Uint64 should be auto-increment eligible")
	}
	if min != 0 {
		t.Errorf("Uint64 min = %d, want 0", min)
	}
	if max != math.MaxUint64 {
		t.Errorf("Uint64 max = %d, want %d", max, uint64(math.MaxUint64))
	}
}

func TestAutoIncrementBoundsNonInteger(t *testing.T) {
	nonInt := []ValueKind{
		KindBool, KindFloat32, KindFloat64,
		KindString, KindBytes,
		KindInt128, KindUint128,
		KindInt256, KindUint256,
		KindTimestamp,
		KindArrayString,
		KindUUID,
		KindDuration,
	}
	for _, k := range nonInt {
		_, _, ok := AutoIncrementBounds(k)
		if ok {
			t.Errorf("AutoIncrementBounds(%v) should return ok=false", k)
		}
	}
}

func TestAutoIncrementBoundsAllIntegers(t *testing.T) {
	intKinds := []ValueKind{
		KindInt8, KindUint8,
		KindInt16, KindUint16,
		KindInt32, KindUint32,
		KindInt64, KindUint64,
	}
	for _, k := range intKinds {
		_, _, ok := AutoIncrementBounds(k)
		if !ok {
			t.Errorf("AutoIncrementBounds(%v) should return ok=true", k)
		}
	}
}

func TestErrSequenceOverflowSentinelExists(t *testing.T) {
	err := fmt.Errorf("wrap: %w", ErrSequenceOverflow)
	if !errors.Is(err, ErrSequenceOverflow) {
		t.Fatal("ErrSequenceOverflow should be a usable sentinel")
	}
}
