package types

import (
	"bytes"
	"errors"
	"hash/fnv"
	"math"
	"testing"
	"time"
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

func mustParseUUID(t *testing.T, s string) Value {
	t.Helper()
	v, err := ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return v
}

func mustJSON(t *testing.T, s string) Value {
	t.Helper()
	v, err := NewJSON([]byte(s))
	if err != nil {
		t.Fatalf("NewJSON(%q): %v", s, err)
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

func TestRoundTripInt128(t *testing.T) {
	cases := []struct {
		hi int64
		lo uint64
	}{
		{0, 0},
		{0, 127},
		{-1, ^uint64(0)},            // -1
		{-1, 0},                     // i128 minimum-ish (hi=-1, lo=0 = -2^64)
		{math.MinInt64, 0},          // i128 minimum
		{math.MaxInt64, ^uint64(0)}, // i128 maximum
	}
	for _, c := range cases {
		v := NewInt128(c.hi, c.lo)
		if v.Kind() != KindInt128 {
			t.Fatalf("Kind = %v, want Int128", v.Kind())
		}
		hi, lo := v.AsInt128()
		if hi != c.hi || lo != c.lo {
			t.Fatalf("AsInt128 = (%d,%d), want (%d,%d)", hi, lo, c.hi, c.lo)
		}
	}
}

func TestRoundTripUint128(t *testing.T) {
	cases := []struct{ hi, lo uint64 }{
		{0, 0},
		{0, 127},
		{0, ^uint64(0)},          // 2^64-1
		{1, 0},                   // 2^64
		{^uint64(0), ^uint64(0)}, // u128 maximum
	}
	for _, c := range cases {
		v := NewUint128(c.hi, c.lo)
		if v.Kind() != KindUint128 {
			t.Fatalf("Kind = %v, want Uint128", v.Kind())
		}
		hi, lo := v.AsUint128()
		if hi != c.hi || lo != c.lo {
			t.Fatalf("AsUint128 = (%d,%d), want (%d,%d)", hi, lo, c.hi, c.lo)
		}
	}
}

func TestInt128FromInt64SignExtends(t *testing.T) {
	v := NewInt128FromInt64(-1)
	hi, lo := v.AsInt128()
	if hi != -1 || lo != ^uint64(0) {
		t.Fatalf("NewInt128FromInt64(-1) = (%d,%d), want (-1,^0)", hi, lo)
	}
	v = NewInt128FromInt64(127)
	hi, lo = v.AsInt128()
	if hi != 0 || lo != 127 {
		t.Fatalf("NewInt128FromInt64(127) = (%d,%d), want (0,127)", hi, lo)
	}
}

func TestUint128FromUint64ZeroExtends(t *testing.T) {
	v := NewUint128FromUint64(^uint64(0))
	hi, lo := v.AsUint128()
	if hi != 0 || lo != ^uint64(0) {
		t.Fatalf("NewUint128FromUint64(^0) = (%d,%d), want (0,^0)", hi, lo)
	}
}

func TestEqualInt128AndUint128(t *testing.T) {
	if !NewInt128(1, 2).Equal(NewInt128(1, 2)) {
		t.Fatal("Int128 Equal: identical not equal")
	}
	if NewInt128(1, 2).Equal(NewInt128(1, 3)) {
		t.Fatal("Int128 Equal: differing lo reported equal")
	}
	if NewInt128(1, 2).Equal(NewUint128(1, 2)) {
		t.Fatal("Int128 and Uint128 with same payload should not be Equal — cross-kind")
	}
	if !NewUint128(0, 127).Equal(NewUint128(0, 127)) {
		t.Fatal("Uint128 Equal: identical not equal")
	}
}

func TestCompareInt128(t *testing.T) {
	// -1 < 0
	if NewInt128(-1, ^uint64(0)).Compare(NewInt128(0, 0)) >= 0 {
		t.Fatal("Int128 Compare: -1 should be < 0")
	}
	// Same hi, larger lo
	if NewInt128(0, 10).Compare(NewInt128(0, 20)) >= 0 {
		t.Fatal("Int128 Compare: (0,10) should be < (0,20)")
	}
	// Smaller hi wins
	if NewInt128(-5, ^uint64(0)).Compare(NewInt128(-1, 0)) >= 0 {
		t.Fatal("Int128 Compare: (-5,^0) should be < (-1,0)")
	}
	// Equal
	if NewInt128(3, 5).Compare(NewInt128(3, 5)) != 0 {
		t.Fatal("Int128 Compare: equal reported non-zero")
	}
}

func TestCompareUint128(t *testing.T) {
	if NewUint128(0, 10).Compare(NewUint128(0, 20)) >= 0 {
		t.Fatal("Uint128 Compare: (0,10) should be < (0,20)")
	}
	if NewUint128(0, ^uint64(0)).Compare(NewUint128(1, 0)) >= 0 {
		t.Fatal("Uint128 Compare: (0,^0) should be < (1,0)")
	}
	if NewUint128(3, 5).Compare(NewUint128(3, 5)) != 0 {
		t.Fatal("Uint128 Compare: equal reported non-zero")
	}
}

func TestRoundTripInt256(t *testing.T) {
	cases := []struct {
		w0         int64
		w1, w2, w3 uint64
	}{
		{0, 0, 0, 0},
		{0, 0, 0, 127},
		{-1, ^uint64(0), ^uint64(0), ^uint64(0)}, // -1
		{math.MinInt64, 0, 0, 0},                 // i256 minimum
		{math.MaxInt64, ^uint64(0), ^uint64(0), ^uint64(0)},
	}
	for _, c := range cases {
		v := NewInt256(c.w0, c.w1, c.w2, c.w3)
		if v.Kind() != KindInt256 {
			t.Fatalf("Kind = %v, want Int256", v.Kind())
		}
		w0, w1, w2, w3 := v.AsInt256()
		if w0 != c.w0 || w1 != c.w1 || w2 != c.w2 || w3 != c.w3 {
			t.Fatalf("AsInt256 = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				w0, w1, w2, w3, c.w0, c.w1, c.w2, c.w3)
		}
	}
}

func TestRoundTripUint256(t *testing.T) {
	cases := []struct{ w0, w1, w2, w3 uint64 }{
		{0, 0, 0, 0},
		{0, 0, 0, 127},
		{0, 0, 0, ^uint64(0)},
		{0, 0, 1, 0},
		{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)}, // u256 maximum
	}
	for _, c := range cases {
		v := NewUint256(c.w0, c.w1, c.w2, c.w3)
		if v.Kind() != KindUint256 {
			t.Fatalf("Kind = %v, want Uint256", v.Kind())
		}
		w0, w1, w2, w3 := v.AsUint256()
		if w0 != c.w0 || w1 != c.w1 || w2 != c.w2 || w3 != c.w3 {
			t.Fatalf("AsUint256 = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				w0, w1, w2, w3, c.w0, c.w1, c.w2, c.w3)
		}
	}
}

func TestInt256FromInt64SignExtends(t *testing.T) {
	v := NewInt256FromInt64(-1)
	w0, w1, w2, w3 := v.AsInt256()
	if w0 != -1 || w1 != ^uint64(0) || w2 != ^uint64(0) || w3 != ^uint64(0) {
		t.Fatalf("NewInt256FromInt64(-1) = (%d,%d,%d,%d), want all-ones", w0, w1, w2, w3)
	}
	v = NewInt256FromInt64(127)
	w0, w1, w2, w3 = v.AsInt256()
	if w0 != 0 || w1 != 0 || w2 != 0 || w3 != 127 {
		t.Fatalf("NewInt256FromInt64(127) = (%d,%d,%d,%d), want (0,0,0,127)", w0, w1, w2, w3)
	}
}

func TestUint256FromUint64ZeroExtends(t *testing.T) {
	v := NewUint256FromUint64(^uint64(0))
	w0, w1, w2, w3 := v.AsUint256()
	if w0 != 0 || w1 != 0 || w2 != 0 || w3 != ^uint64(0) {
		t.Fatalf("NewUint256FromUint64(^0) = (%d,%d,%d,%d), want (0,0,0,^0)", w0, w1, w2, w3)
	}
}

func TestEqualInt256AndUint256(t *testing.T) {
	if !NewInt256(1, 2, 3, 4).Equal(NewInt256(1, 2, 3, 4)) {
		t.Fatal("Int256 Equal: identical not equal")
	}
	if NewInt256(1, 2, 3, 4).Equal(NewInt256(1, 2, 3, 5)) {
		t.Fatal("Int256 Equal: differing w3 reported equal")
	}
	if NewInt256(0, 0, 0, 127).Equal(NewUint256(0, 0, 0, 127)) {
		t.Fatal("Int256 and Uint256 with same payload should not be Equal — cross-kind")
	}
	if !NewUint256(0, 0, 0, 127).Equal(NewUint256(0, 0, 0, 127)) {
		t.Fatal("Uint256 Equal: identical not equal")
	}
}

func TestCompareInt256(t *testing.T) {
	// -1 < 0
	if NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)).Compare(NewInt256(0, 0, 0, 0)) >= 0 {
		t.Fatal("Int256 Compare: -1 should be < 0")
	}
	// Same high words, larger low word
	if NewInt256(0, 0, 0, 10).Compare(NewInt256(0, 0, 0, 20)) >= 0 {
		t.Fatal("Int256 Compare: (…,10) should be < (…,20)")
	}
	// Smaller signed high wins
	if NewInt256(-5, ^uint64(0), ^uint64(0), ^uint64(0)).Compare(NewInt256(-1, 0, 0, 0)) >= 0 {
		t.Fatal("Int256 Compare: (-5,…) should be < (-1,…)")
	}
	if NewInt256(3, 5, 7, 9).Compare(NewInt256(3, 5, 7, 9)) != 0 {
		t.Fatal("Int256 Compare: equal reported non-zero")
	}
}

func TestCompareUint256(t *testing.T) {
	if NewUint256(0, 0, 0, 10).Compare(NewUint256(0, 0, 0, 20)) >= 0 {
		t.Fatal("Uint256 Compare: (…,10) should be < (…,20)")
	}
	if NewUint256(0, 0, 0, ^uint64(0)).Compare(NewUint256(0, 0, 1, 0)) >= 0 {
		t.Fatal("Uint256 Compare: (…,0,^0) should be < (…,1,0)")
	}
	if NewUint256(3, 5, 7, 9).Compare(NewUint256(3, 5, 7, 9)) != 0 {
		t.Fatal("Uint256 Compare: equal reported non-zero")
	}
}

func TestRoundTripTimestamp(t *testing.T) {
	for _, m := range []int64{math.MinInt64, -1, 0, 1, 1_739_201_130_000_000, math.MaxInt64} {
		v := NewTimestamp(m)
		if v.Kind() != KindTimestamp {
			t.Fatalf("Kind = %v, want Timestamp", v.Kind())
		}
		if got := v.AsTimestamp(); got != m {
			t.Fatalf("AsTimestamp = %d, want %d", got, m)
		}
	}
}

func TestTimestampTimeHelpersUseUTCMicroseconds(t *testing.T) {
	in := time.Date(2026, time.May, 4, 12, 34, 56, 789_123_456, time.FixedZone("app", -5*60*60))
	v := NewTimestampFromTime(in)
	if v.Kind() != KindTimestamp {
		t.Fatalf("Kind = %v, want Timestamp", v.Kind())
	}
	wantMicros := in.UTC().UnixMicro()
	if got := v.AsTimestamp(); got != wantMicros {
		t.Fatalf("AsTimestamp = %d, want %d", got, wantMicros)
	}
	wantTime := time.UnixMicro(wantMicros).UTC()
	if got := v.AsTime(); !got.Equal(wantTime) || got.Location() != time.UTC {
		t.Fatalf("AsTime = %v (%v), want %v UTC", got, got.Location(), wantTime)
	}
}

func TestEqualTimestamp(t *testing.T) {
	if !NewTimestamp(1).Equal(NewTimestamp(1)) {
		t.Fatal("identical timestamps not equal")
	}
	if NewTimestamp(1).Equal(NewTimestamp(2)) {
		t.Fatal("differing timestamps reported equal")
	}
	if NewTimestamp(0).Equal(NewInt64(0)) {
		t.Fatal("Timestamp and Int64 with same payload should not be Equal — cross-kind")
	}
}

func TestCompareTimestamp(t *testing.T) {
	if NewTimestamp(1).Compare(NewTimestamp(2)) >= 0 {
		t.Fatal("Timestamp Compare: 1 should be < 2")
	}
	if NewTimestamp(-5).Compare(NewTimestamp(1)) >= 0 {
		t.Fatal("Timestamp Compare: -5 should be < 1")
	}
	if NewTimestamp(3).Compare(NewTimestamp(3)) != 0 {
		t.Fatal("Timestamp Compare: equal reported non-zero")
	}
}

func TestRoundTripDuration(t *testing.T) {
	for _, micros := range []int64{math.MinInt64, -1, 0, 1, 12_345_678, math.MaxInt64} {
		v := NewDuration(micros)
		if v.Kind() != KindDuration {
			t.Fatalf("Kind = %v, want Duration", v.Kind())
		}
		if got := v.AsDurationMicros(); got != micros {
			t.Fatalf("AsDurationMicros = %d, want %d", got, micros)
		}
	}
}

func TestDurationTimeHelperTruncatesToMicroseconds(t *testing.T) {
	in := 5*time.Second + 123_456*time.Microsecond + 789*time.Nanosecond
	v := NewDurationFromTime(in)
	if v.Kind() != KindDuration {
		t.Fatalf("Kind = %v, want Duration", v.Kind())
	}
	if got, want := v.AsDurationMicros(), int64(5_123_456); got != want {
		t.Fatalf("AsDurationMicros = %d, want %d", got, want)
	}
	if got, want := v.AsDuration(), 5*time.Second+123_456*time.Microsecond; got != want {
		t.Fatalf("AsDuration = %v, want %v", got, want)
	}
}

func TestEqualAndCompareDuration(t *testing.T) {
	if !NewDuration(1).Equal(NewDuration(1)) {
		t.Fatal("identical durations not equal")
	}
	if NewDuration(1).Equal(NewDuration(2)) {
		t.Fatal("differing durations reported equal")
	}
	if NewDuration(1).Equal(NewTimestamp(1)) {
		t.Fatal("Duration and Timestamp with same payload should not be Equal")
	}
	if NewDuration(-5).Compare(NewDuration(1)) >= 0 {
		t.Fatal("Duration Compare: -5 should be < 1")
	}
}

func TestRoundTripArrayString(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{""},
		{"alpha"},
		{"alpha", "beta", "γ"},
	}
	for _, xs := range cases {
		v := NewArrayString(xs)
		if v.Kind() != KindArrayString {
			t.Fatalf("Kind = %v, want ArrayString", v.Kind())
		}
		got := v.AsArrayString()
		if len(got) != len(xs) {
			t.Fatalf("len(AsArrayString) = %d, want %d", len(got), len(xs))
		}
		for i := range xs {
			if got[i] != xs[i] {
				t.Fatalf("AsArrayString[%d] = %q, want %q", i, got[i], xs[i])
			}
		}
	}
}

func TestArrayStringDefensiveCopyOnConstruct(t *testing.T) {
	src := []string{"alpha", "beta"}
	v := NewArrayString(src)
	src[0] = "mutated"
	got := v.AsArrayString()
	if got[0] != "alpha" {
		t.Fatalf("mutation of constructor input leaked into Value: got[0] = %q", got[0])
	}
}

func TestArrayStringDefensiveCopyOnAccess(t *testing.T) {
	v := NewArrayString([]string{"alpha", "beta"})
	got := v.AsArrayString()
	got[0] = "mutated"
	if v.AsArrayString()[0] != "alpha" {
		t.Fatal("mutation of accessor result leaked back into Value")
	}
}

func TestEqualArrayString(t *testing.T) {
	a := NewArrayString([]string{"x", "y"})
	b := NewArrayString([]string{"x", "y"})
	c := NewArrayString([]string{"x", "z"})
	d := NewArrayString([]string{"x"})
	empty := NewArrayString(nil)
	if !a.Equal(b) {
		t.Fatal("identical ArrayString values not Equal")
	}
	if a.Equal(c) {
		t.Fatal("differing ArrayString values reported Equal")
	}
	if a.Equal(d) {
		t.Fatal("different-length ArrayString values reported Equal")
	}
	if a.Equal(empty) {
		t.Fatal("non-empty vs empty reported Equal")
	}
	if a.Equal(NewString("x,y")) {
		t.Fatal("ArrayString and String should not be cross-kind Equal")
	}
}

func TestCompareArrayString(t *testing.T) {
	a := NewArrayString([]string{"x", "y"})
	b := NewArrayString([]string{"x", "y"})
	if a.Compare(b) != 0 {
		t.Fatal("equal ArrayString Compare != 0")
	}
	short := NewArrayString([]string{"x"})
	if short.Compare(a) >= 0 {
		t.Fatal("shorter prefix should compare less")
	}
	smaller := NewArrayString([]string{"x", "x"})
	if smaller.Compare(a) >= 0 {
		t.Fatal("lexicographic element compare failed")
	}
}

func TestJSONCanonicalizesWhitespaceAndObjectKeys(t *testing.T) {
	v := mustJSON(t, `{"z": 1, "a": [true, null, {"b":2}]}`)
	if v.Kind() != KindJSON {
		t.Fatalf("Kind = %v, want JSON", v.Kind())
	}
	want := []byte(`{"a":[true,null,{"b":2}],"z":1}`)
	if got := v.AsJSON(); !bytes.Equal(got, want) {
		t.Fatalf("AsJSON = %s, want %s", got, want)
	}

	again := mustJSON(t, `{"a":[true,null,{"b":2}],"z":1}`)
	if !v.Equal(again) {
		t.Fatal("equivalent JSON payloads should canonicalize equal")
	}
}

func TestJSONRejectsInvalidAndDuplicateKeys(t *testing.T) {
	for _, raw := range []string{
		``,
		`{"a":`,
		`{"a":1} trailing`,
		`{"a":1,"a":2}`,
		`{"outer":{"a":1,"a":2}}`,
	} {
		if _, err := NewJSON([]byte(raw)); !errors.Is(err, ErrInvalidJSON) {
			t.Fatalf("NewJSON(%q) err = %v, want ErrInvalidJSON", raw, err)
		}
	}
}

func TestJSONAccessorsDoNotExposeMutableAliases(t *testing.T) {
	src := []byte(`{"b":2,"a":1}`)
	v, err := NewJSON(src)
	if err != nil {
		t.Fatal(err)
	}
	src[2] = 'X'
	if got := v.AsJSON(); !bytes.Equal(got, []byte(`{"a":1,"b":2}`)) {
		t.Fatalf("constructor input mutation leaked into JSON value: %s", got)
	}
	got := v.AsJSON()
	got[1] = 'z'
	if again := v.AsJSON(); !bytes.Equal(again, []byte(`{"a":1,"b":2}`)) {
		t.Fatalf("AsJSON mutation leaked into Value: %s", again)
	}
}

func TestJSONEqualCompareAndHash(t *testing.T) {
	a := mustJSON(t, `{"b":2,"a":1}`)
	a2 := mustJSON(t, `{"a":1,"b":2}`)
	b := mustJSON(t, `{"a":1,"b":3}`)
	if !a.Equal(a2) {
		t.Fatal("canonical-equivalent JSON values not Equal")
	}
	if a.Equal(b) {
		t.Fatal("different JSON values reported Equal")
	}
	if a.Compare(b) >= 0 {
		t.Fatal("JSON Compare should use canonical byte ordering")
	}
	if a.Hash64() != a2.Hash64() {
		t.Fatal("equal JSON values should hash identically")
	}
	if a.Hash64() == NewBytes(a.JSONView()).Hash64() {
		t.Fatal("JSON and Bytes with same payload should hash differently")
	}
}

func TestUUIDParseAndCanonicalString(t *testing.T) {
	v, err := ParseUUID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatalf("ParseUUID returned error: %v", err)
	}
	if v.Kind() != KindUUID {
		t.Fatalf("Kind = %v, want UUID", v.Kind())
	}
	want := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	if got := v.AsUUID(); got != want {
		t.Fatalf("AsUUID = %x, want %x", got, want)
	}
	if got := v.UUIDString(); got != "00112233-4455-6677-8899-aabbccddeeff" {
		t.Fatalf("UUIDString = %q, want canonical lowercase text", got)
	}
}

func TestParseUUIDRejectsMalformed(t *testing.T) {
	for _, s := range []string{
		"",
		"00112233445566778899aabbccddeeff",
		"00112233-4455-6677-8899-aabbccddeef",
		"00112233-4455-6677-8899-aabbccddeeff00",
		"00112233_4455-6677-8899-aabbccddeeff",
		"00112233-4455-6677-8899-AABBCCDDEEFF",
		"00112233-4455-6677-8899-aabbccddeegf",
	} {
		if _, err := ParseUUID(s); !errors.Is(err, ErrInvalidUUID) {
			t.Fatalf("ParseUUID(%q) err = %v, want ErrInvalidUUID", s, err)
		}
	}
}

func TestUUIDAccessorsDoNotExposeMutableAliases(t *testing.T) {
	u := [16]byte{0: 1, 15: 2}
	v := NewUUID(u)
	u[0] = 9
	if got := v.AsUUID(); got[0] != 1 {
		t.Fatalf("constructor input mutation leaked into Value: got[0] = %d", got[0])
	}
	got := v.AsUUID()
	got[15] = 9
	if again := v.AsUUID(); again[15] != 2 {
		t.Fatalf("AsUUID mutation leaked into Value: got[15] = %d", again[15])
	}
}

func TestUUIDEqualCompareAndHash(t *testing.T) {
	a := mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff")
	a2 := NewUUID(a.AsUUID())
	b := mustParseUUID(t, "00112233-4455-6677-8899-aabbccddef00")
	if !a.Equal(a2) {
		t.Fatal("identical UUID values not Equal")
	}
	if a.Equal(b) {
		t.Fatal("different UUID values reported Equal")
	}
	if a.Compare(b) >= 0 {
		t.Fatal("UUID Compare should use lexicographic byte order")
	}
	if a.Hash64() != a2.Hash64() {
		t.Fatal("equal UUID values should hash identically")
	}
	if a.Hash64() == b.Hash64() {
		t.Fatal("different UUID payloads should hash differently")
	}
	raw := a.AsUUID()
	if a.Hash64() == NewBytes(raw[:]).Hash64() {
		t.Fatal("UUID and Bytes with same 16 bytes should hash differently")
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
		{"AsInt128", func() { v.AsInt128() }},
		{"AsInt256", func() { v.AsInt256() }},
		{"AsTimestamp", func() { v.AsTimestamp() }},
		{"AsArrayString", func() { v.AsArrayString() }},
		{"AsUUID", func() { v.AsUUID() }},
		{"AsDurationMicros", func() { v.AsDurationMicros() }},
		{"AsJSON", func() { v.AsJSON() }},
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

func TestNullValueSemantics(t *testing.T) {
	nullString := NewNull(KindString)
	if nullString.Kind() != KindString || !nullString.IsNull() {
		t.Fatalf("NewNull Kind/IsNull = %s/%t, want String/true", nullString.Kind(), nullString.IsNull())
	}
	if !nullString.Equal(NewNull(KindString)) {
		t.Fatal("null values of the same kind should be equal")
	}
	if nullString.Equal(NewString("")) {
		t.Fatal("null string must not equal empty string")
	}
	if got := nullString.Compare(NewString("")); got >= 0 {
		t.Fatalf("null Compare empty string = %d, want null before non-null", got)
	}
	if got := NewString("").Compare(nullString); got <= 0 {
		t.Fatalf("empty string Compare null = %d, want non-null after null", got)
	}
	if nullString.Hash64() == NewString("").Hash64() {
		t.Fatal("null and empty string hashes must differ")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AsString on null value did not panic")
		}
	}()
	_ = nullString.AsString()
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
		{KindInt128, "Int128"},
		{KindUint128, "Uint128"},
		{KindInt256, "Int256"},
		{KindUint256, "Uint256"},
		{KindTimestamp, "Timestamp"},
		{KindArrayString, "ArrayString"},
		{KindUUID, "UUID"},
		{KindDuration, "Duration"},
		{KindJSON, "JSON"},
		{ValueKind(-1), "ValueKind(-1)"},
		{ValueKind(len(kindNames)), "ValueKind(22)"},
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
		{mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff"), mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff")},
		{mustJSON(t, `{"b":2,"a":1}`), mustJSON(t, `{"a":1,"b":2}`)},
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
		{mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff"), mustParseUUID(t, "00112233-4455-6677-8899-aabbccddef00")},
		{mustJSON(t, `{"a":1}`), mustJSON(t, `{"a":2}`)},
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
		{mustFloat32(t, float32(math.Copysign(0, -1))), mustFloat32(t, 0)},
		{mustFloat64(t, math.Copysign(0, -1)), mustFloat64(t, 0)},
		{mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff"), mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff")},
		{mustJSON(t, `{"b":2,"a":1}`), mustJSON(t, `{"a":1,"b":2}`)},
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

func TestHash64MatchesStreamingHash(t *testing.T) {
	values := []Value{
		NewBool(true),
		NewInt64(-42),
		NewUint64(42),
		mustFloat32(t, 1.25),
		mustFloat64(t, 2.5),
		NewString("hello"),
		NewBytes([]byte{1, 2, 3}),
		NewArrayString([]string{"a", "bc"}),
		mustParseUUID(t, "00112233-4455-6677-8899-aabbccddeeff"),
		mustJSON(t, `{"a":1}`),
		NewNull(KindString),
	}
	for _, v := range values {
		h := fnv.New64a()
		v.Hash(h)
		if got, want := v.Hash64(), h.Sum64(); got != want {
			t.Fatalf("%s Hash64 = %d, want streaming hash %d", v.Kind(), got, want)
		}
	}
}
