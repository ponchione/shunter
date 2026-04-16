package types

import (
	"errors"
	"math"
	"testing"
)

func mustFloat32(t *testing.T, x float32) Value {
	t.Helper()
	v, err := NewFloat32(x)
	if err != nil {
		t.Fatalf("NewFloat32(%v): %v", x, err)
	}
	return v
}

func mustFloat64(t *testing.T, x float64) Value {
	t.Helper()
	v, err := NewFloat64(x)
	if err != nil {
		t.Fatalf("NewFloat64(%v): %v", x, err)
	}
	return v
}

// --- Round-trip tests for all 13 kinds ---

func TestRoundTripBool(t *testing.T) {
	for _, x := range []bool{false, true} {
		v := NewBool(x)
		if v.Kind() != KindBool {
			t.Fatalf("Kind = %v, want Bool", v.Kind())
		}
		if v.AsBool() != x {
			t.Fatalf("AsBool = %v, want %v", v.AsBool(), x)
		}
	}
}

func TestRoundTripInt8(t *testing.T) {
	for _, x := range []int8{-128, -1, 0, 1, 127} {
		v := NewInt8(x)
		if v.Kind() != KindInt8 {
			t.Fatalf("Kind = %v, want Int8", v.Kind())
		}
		if v.AsInt8() != x {
			t.Fatalf("AsInt8 = %v, want %v", v.AsInt8(), x)
		}
	}
}

func TestRoundTripUint8(t *testing.T) {
	for _, x := range []uint8{0, 1, 255} {
		v := NewUint8(x)
		if v.Kind() != KindUint8 {
			t.Fatalf("Kind = %v, want Uint8", v.Kind())
		}
		if v.AsUint8() != x {
			t.Fatalf("AsUint8 = %v, want %v", v.AsUint8(), x)
		}
	}
}

func TestRoundTripInt16(t *testing.T) {
	for _, x := range []int16{-32768, 0, 32767} {
		v := NewInt16(x)
		if v.Kind() != KindInt16 {
			t.Fatalf("Kind = %v, want Int16", v.Kind())
		}
		if v.AsInt16() != x {
			t.Fatalf("AsInt16 = %v, want %v", v.AsInt16(), x)
		}
	}
}

func TestRoundTripUint16(t *testing.T) {
	for _, x := range []uint16{0, 65535} {
		v := NewUint16(x)
		if v.Kind() != KindUint16 {
			t.Fatalf("Kind = %v, want Uint16", v.Kind())
		}
		if v.AsUint16() != x {
			t.Fatalf("AsUint16 = %v, want %v", v.AsUint16(), x)
		}
	}
}

func TestRoundTripInt32(t *testing.T) {
	for _, x := range []int32{-2147483648, 0, 2147483647} {
		v := NewInt32(x)
		if v.Kind() != KindInt32 {
			t.Fatalf("Kind = %v, want Int32", v.Kind())
		}
		if v.AsInt32() != x {
			t.Fatalf("AsInt32 = %v, want %v", v.AsInt32(), x)
		}
	}
}

func TestRoundTripUint32(t *testing.T) {
	for _, x := range []uint32{0, 4294967295} {
		v := NewUint32(x)
		if v.Kind() != KindUint32 {
			t.Fatalf("Kind = %v, want Uint32", v.Kind())
		}
		if v.AsUint32() != x {
			t.Fatalf("AsUint32 = %v, want %v", v.AsUint32(), x)
		}
	}
}

func TestRoundTripInt64(t *testing.T) {
	for _, x := range []int64{-9223372036854775808, 0, 9223372036854775807} {
		v := NewInt64(x)
		if v.Kind() != KindInt64 {
			t.Fatalf("Kind = %v, want Int64", v.Kind())
		}
		if v.AsInt64() != x {
			t.Fatalf("AsInt64 = %v, want %v", v.AsInt64(), x)
		}
	}
}

func TestRoundTripUint64(t *testing.T) {
	for _, x := range []uint64{0, 18446744073709551615} {
		v := NewUint64(x)
		if v.Kind() != KindUint64 {
			t.Fatalf("Kind = %v, want Uint64", v.Kind())
		}
		if v.AsUint64() != x {
			t.Fatalf("AsUint64 = %v, want %v", v.AsUint64(), x)
		}
	}
}

func TestRoundTripFloat32(t *testing.T) {
	for _, x := range []float32{0, -1.5, 3.14, math.MaxFloat32, math.SmallestNonzeroFloat32} {
		v, err := NewFloat32(x)
		if err != nil {
			t.Fatalf("NewFloat32(%v) error: %v", x, err)
		}
		if v.Kind() != KindFloat32 {
			t.Fatalf("Kind = %v, want Float32", v.Kind())
		}
		if v.AsFloat32() != x {
			t.Fatalf("AsFloat32 = %v, want %v", v.AsFloat32(), x)
		}
	}
}

func TestRoundTripFloat64(t *testing.T) {
	for _, x := range []float64{0, -1.5, 3.14, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		v, err := NewFloat64(x)
		if err != nil {
			t.Fatalf("NewFloat64(%v) error: %v", x, err)
		}
		if v.Kind() != KindFloat64 {
			t.Fatalf("Kind = %v, want Float64", v.Kind())
		}
		if v.AsFloat64() != x {
			t.Fatalf("AsFloat64 = %v, want %v", v.AsFloat64(), x)
		}
	}
}

func TestRoundTripString(t *testing.T) {
	for _, x := range []string{"", "hello", "日本語"} {
		v := NewString(x)
		if v.Kind() != KindString {
			t.Fatalf("Kind = %v, want String", v.Kind())
		}
		if v.AsString() != x {
			t.Fatalf("AsString = %q, want %q", v.AsString(), x)
		}
	}
}

func TestRoundTripBytes(t *testing.T) {
	for _, x := range [][]byte{{}, {0x00}, {0xDE, 0xAD, 0xBE, 0xEF}} {
		v := NewBytes(x)
		if v.Kind() != KindBytes {
			t.Fatalf("Kind = %v, want Bytes", v.Kind())
		}
		got := v.AsBytes()
		if len(got) != len(x) {
			t.Fatalf("AsBytes len = %d, want %d", len(got), len(x))
		}
		for i := range got {
			if got[i] != x[i] {
				t.Fatalf("AsBytes[%d] = %x, want %x", i, got[i], x[i])
			}
		}
	}
}

// --- NaN rejection ---

func TestFloat32RejectsNaN(t *testing.T) {
	_, err := NewFloat32(float32(math.NaN()))
	if err == nil {
		t.Fatal("NewFloat32(NaN) should return error")
	}
	if !errors.Is(err, ErrInvalidFloat) {
		t.Fatalf("NewFloat32(NaN) error = %v, want errors.Is(..., ErrInvalidFloat)", err)
	}
}

func TestFloat64RejectsNaN(t *testing.T) {
	_, err := NewFloat64(math.NaN())
	if err == nil {
		t.Fatal("NewFloat64(NaN) should return error")
	}
	if !errors.Is(err, ErrInvalidFloat) {
		t.Fatalf("NewFloat64(NaN) error = %v, want errors.Is(..., ErrInvalidFloat)", err)
	}
}

// --- Bytes copy isolation ---

func TestBytesCopyIsolation(t *testing.T) {
	orig := []byte{1, 2, 3}
	v := NewBytes(orig)

	// Mutate original — stored value must not change.
	orig[0] = 0xFF
	got := v.AsBytes()
	if got[0] == 0xFF {
		t.Fatal("mutating original slice affected stored Value")
	}
	if got[0] != 1 {
		t.Fatalf("AsBytes[0] = %d, want 1", got[0])
	}
}

func TestAsBytesReturnsCopy(t *testing.T) {
	v := NewBytes([]byte{1, 2, 3})
	got := v.AsBytes()
	got[0] = 0xFF

	if v.AsBytes()[0] == 0xFF {
		t.Fatal("AsBytes returned mutable internal storage")
	}
}

// --- Kind mismatch panics ---

func TestAccessorPanicsOnKindMismatch(t *testing.T) {
	v := NewBool(true)

	accessors := []struct {
		name string
		fn   func()
	}{
		{"AsInt8", func() { v.AsInt8() }},
		{"AsUint8", func() { v.AsUint8() }},
		{"AsInt16", func() { v.AsInt16() }},
		{"AsUint16", func() { v.AsUint16() }},
		{"AsInt32", func() { v.AsInt32() }},
		{"AsUint32", func() { v.AsUint32() }},
		{"AsInt64", func() { v.AsInt64() }},
		{"AsUint64", func() { v.AsUint64() }},
		{"AsFloat32", func() { v.AsFloat32() }},
		{"AsFloat64", func() { v.AsFloat64() }},
		{"AsString", func() { v.AsString() }},
		{"AsBytes", func() { v.AsBytes() }},
	}
	for _, a := range accessors {
		t.Run(a.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%s on Bool value did not panic", a.name)
				}
			}()
			a.fn()
		})
	}
}

// --- ValueKind.String() ---

func TestValueKindString(t *testing.T) {
	cases := []struct {
		k    ValueKind
		want string
	}{
		{KindBool, "Bool"},
		{KindInt8, "Int8"},
		{KindUint8, "Uint8"},
		{KindInt16, "Int16"},
		{KindUint16, "Uint16"},
		{KindInt32, "Int32"},
		{KindUint32, "Uint32"},
		{KindInt64, "Int64"},
		{KindUint64, "Uint64"},
		{KindFloat32, "Float32"},
		{KindFloat64, "Float64"},
		{KindString, "String"},
		{KindBytes, "Bytes"},
		{ValueKind(-1), "ValueKind(-1)"},
		{ValueKind(len(kindNames)), "ValueKind(13)"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("ValueKind(%d).String() = %q, want %q", int(c.k), got, c.want)
		}
	}
}

// --- Named ID types (Story 1.6) ---

func TestRowIDBasics(t *testing.T) {
	var a RowID = 42
	var b RowID = 42
	if a != b {
		t.Fatal("RowID should be comparable")
	}
	m := map[RowID]bool{a: true}
	if !m[b] {
		t.Fatal("RowID should be usable as map key")
	}
}

func TestIdentityBasics(t *testing.T) {
	var zero Identity
	for _, b := range zero {
		if b != 0 {
			t.Fatal("Identity zero value should be all zeros")
		}
	}
	var a, b Identity
	a[0] = 1
	b[0] = 1
	if a != b {
		t.Fatal("Identity should be comparable")
	}
	m := map[Identity]bool{a: true}
	if !m[b] {
		t.Fatal("Identity should be usable as map key")
	}
}

func TestColIDBasics(t *testing.T) {
	var c ColID = 3
	s := []string{"a", "b", "c", "d"}
	if s[c] != "d" {
		t.Fatal("ColID should be usable as slice index")
	}
}

// =============================================================
// Story 1.2: Value.Equal
// =============================================================

func TestEqualSameKindSameValue(t *testing.T) {
	pairs := [][2]Value{
		{NewBool(true), NewBool(true)},
		{NewInt8(-1), NewInt8(-1)},
		{NewUint8(255), NewUint8(255)},
		{NewInt16(1000), NewInt16(1000)},
		{NewUint16(65535), NewUint16(65535)},
		{NewInt32(-42), NewInt32(-42)},
		{NewUint32(42), NewUint32(42)},
		{NewInt64(0), NewInt64(0)},
		{NewUint64(0), NewUint64(0)},
		{mustFloat32(t, 3.14), mustFloat32(t, 3.14)},
		{mustFloat64(t, 2.718), mustFloat64(t, 2.718)},
		{NewString("hello"), NewString("hello")},
		{NewBytes([]byte{1, 2}), NewBytes([]byte{1, 2})},
	}
	for _, p := range pairs {
		if !p[0].Equal(p[1]) {
			t.Errorf("%s: same value should be equal", p[0].Kind())
		}
	}
}

func TestEqualSameKindDifferentValue(t *testing.T) {
	pairs := [][2]Value{
		{NewBool(true), NewBool(false)},
		{NewInt8(1), NewInt8(2)},
		{NewUint64(0), NewUint64(1)},
		{mustFloat64(t, 1.0), mustFloat64(t, 2.0)},
		{NewString("a"), NewString("b")},
		{NewBytes([]byte{1}), NewBytes([]byte{2})},
	}
	for _, p := range pairs {
		if p[0].Equal(p[1]) {
			t.Errorf("%s: different values should not be equal", p[0].Kind())
		}
	}
}

func TestEqualCrossKindNotEqual(t *testing.T) {
	// Int32(1) vs Uint32(1) — same numeric value, different kind
	if NewInt32(1).Equal(NewUint32(1)) {
		t.Fatal("Int32(1) and Uint32(1) should not be equal (different kinds)")
	}
}

func TestEqualEmptyStringAndBytes(t *testing.T) {
	if !NewString("").Equal(NewString("")) {
		t.Fatal("empty strings should be equal")
	}
	if !NewBytes([]byte{}).Equal(NewBytes([]byte{})) {
		t.Fatal("empty byte slices should be equal")
	}
}

func TestEqualFloat(t *testing.T) {
	a := mustFloat64(t, 1.0)
	b := mustFloat64(t, 1.0)
	if !a.Equal(b) {
		t.Fatal("equal float64 values should be equal")
	}
}

// =============================================================
// Story 1.3: Value.Compare
// =============================================================

func TestCompareBool(t *testing.T) {
	f, tr := NewBool(false), NewBool(true)
	if f.Compare(tr) != -1 {
		t.Fatal("false.Compare(true) should be -1")
	}
	if tr.Compare(f) != 1 {
		t.Fatal("true.Compare(false) should be 1")
	}
	if f.Compare(f) != 0 {
		t.Fatal("false.Compare(false) should be 0")
	}
}

func TestCompareSignedInt(t *testing.T) {
	a, b := NewInt64(-1), NewInt64(1)
	if a.Compare(b) != -1 {
		t.Fatal("Int64(-1).Compare(Int64(1)) should be -1")
	}
}

func TestCompareUnsignedInt(t *testing.T) {
	a := NewUint64(0)
	b := NewUint64(math.MaxUint64)
	if a.Compare(b) != -1 {
		t.Fatal("Uint64(0).Compare(Uint64(MAX)) should be -1")
	}
}

func TestCompareFloat64NegZero(t *testing.T) {
	a := mustFloat64(t, math.Copysign(0, -1))
	b := mustFloat64(t, 0.0)
	if a.Compare(b) != 0 {
		t.Fatal("Float64(-0.0).Compare(Float64(0.0)) should be 0")
	}
}

func TestCompareString(t *testing.T) {
	if NewString("abc").Compare(NewString("abd")) != -1 {
		t.Fatal(`"abc" should be < "abd"`)
	}
	if NewString("ab").Compare(NewString("abc")) != -1 {
		t.Fatal(`"ab" should be < "abc"`)
	}
}

func TestCompareBytes(t *testing.T) {
	if NewBytes([]byte{0x00}).Compare(NewBytes([]byte{0x01})) != -1 {
		t.Fatal("[0x00] should be < [0x01]")
	}
	if NewBytes([]byte{}).Compare(NewBytes([]byte{0x00})) != -1 {
		t.Fatal("[] should be < [0x00]")
	}
}

func TestCompareCrossKindPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("cross-kind Compare should panic")
		}
	}()
	NewInt32(1).Compare(NewUint32(1))
}

func TestCompareSymmetry(t *testing.T) {
	a, b := NewInt64(10), NewInt64(20)
	if a.Compare(b) != -b.Compare(a) {
		t.Fatal("a.Compare(b) should equal -b.Compare(a)")
	}
}

func TestCompareTransitivity(t *testing.T) {
	a, b, c := NewInt64(1), NewInt64(2), NewInt64(3)
	if a.Compare(b) >= 0 || b.Compare(c) >= 0 {
		t.Fatal("precondition: a < b < c")
	}
	if a.Compare(c) >= 0 {
		t.Fatal("transitivity: a < c")
	}
}

// =============================================================
// Story 1.4: Value.Hash / Hash64
// =============================================================

func TestHashEqualValuesProduceEqualHashes(t *testing.T) {
	pairs := [][2]Value{
		{NewBool(true), NewBool(true)},
		{NewInt64(42), NewInt64(42)},
		{NewString("hello"), NewString("hello")},
		{NewBytes([]byte{1, 2, 3}), NewBytes([]byte{1, 2, 3})},
		{mustFloat64(t, 1.5), mustFloat64(t, 1.5)},
	}
	for _, p := range pairs {
		if p[0].Hash64() != p[1].Hash64() {
			t.Errorf("%s: equal values should produce equal hashes", p[0].Kind())
		}
	}
}

func TestHashDifferentKindsSameBits(t *testing.T) {
	// Int64(1) and Uint64(1) have different kinds but same numeric 1
	a := NewInt64(1)
	b := NewUint64(1)
	if a.Hash64() == b.Hash64() {
		t.Fatal("different kinds should produce different hashes (kind is part of input)")
	}
}

func TestHashStringVsBytes(t *testing.T) {
	s := NewString("abc")
	b := NewBytes([]byte("abc"))
	if s.Hash64() == b.Hash64() {
		t.Fatal(`String("abc") and Bytes("abc") should hash differently`)
	}
}

func TestHashEmptyStringVsEmptyBytes(t *testing.T) {
	s := NewString("")
	b := NewBytes([]byte{})
	if s.Hash64() == b.Hash64() {
		t.Fatal("empty String and empty Bytes should hash differently")
	}
}

func TestHashDeterministic(t *testing.T) {
	v := NewString("determinism")
	h1 := v.Hash64()
	h2 := v.Hash64()
	if h1 != h2 {
		t.Fatal("Hash64 should be deterministic across calls")
	}
}
