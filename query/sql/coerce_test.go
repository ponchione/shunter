package sql

import (
	"errors"
	"math"
	"math/big"
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

// TestCoerceFloatLiteralOnIntegerEmitsInvalidLiteral pins the reference
// `InvalidLiteral` literal at errors.rs:84 for LitFloat against integer
// column kinds. Reference path: `parse_int(BigDecimal, ty)` at lib.rs:99
// where `BigDecimal::to_{i,u}{8..256}` returns None for fractional values
// and the outer `.map_err` folds the anyhow into InvalidLiteral. Rendered
// via `strconv.FormatFloat('g', -1, 64)` so `1.3` carries verbatim. Covers
// 32-bit signed, 32-bit unsigned, and a 128-bit default-branch kind to
// exercise the three `mismatch` entry points (`coerceSigned`,
// `coerceUnsigned`, 128/256-bit default arms).
func TestCoerceFloatLiteralOnIntegerEmitsInvalidLiteral(t *testing.T) {
	cases := []struct {
		name    string
		kind    types.ValueKind
		wantTy  string
		float   float64
		wantLit string
	}{
		{"U32", types.KindUint32, "U32", 1.3, "1.3"},
		{"I32", types.KindInt32, "I32", -1.3, "-1.3"},
		{"U128", types.KindUint128, "U128", 1.5, "1.5"},
		{"I256", types.KindInt256, "I256", 2.5, "2.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Coerce(Literal{Kind: LitFloat, Float: tc.float}, tc.kind)
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			var ilErr InvalidLiteralError
			if !errors.As(err, &ilErr) {
				t.Fatalf("err = %v, want InvalidLiteralError", err)
			}
			if ilErr.Literal != tc.wantLit || ilErr.Type != tc.wantTy {
				t.Fatalf("got {%q, %q}, want {%q, %q}", ilErr.Literal, ilErr.Type, tc.wantLit, tc.wantTy)
			}
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("err does not unwrap to ErrUnsupportedSQL: %v", err)
			}
		})
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

// TestCoerceIntLiteralToInt128 pins reference valid_literals_for_type at
// check.rs:360-370 for the i128 row — `= 127` on an i128 column must
// type-check. LitInt always fits Int128 via sign-extension from int64.
func TestCoerceIntLiteralToInt128(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 127}, types.KindInt128)
	if err != nil {
		t.Fatalf("Coerce(127 → Int128) error: %v", err)
	}
	if v.Kind() != types.KindInt128 {
		t.Fatalf("Kind = %v, want Int128", v.Kind())
	}
	hi, lo := v.AsInt128()
	if hi != 0 || lo != 127 {
		t.Fatalf("AsInt128 = (%d,%d), want (0,127)", hi, lo)
	}
}

func TestCoerceNegativeIntLiteralToInt128(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: -1}, types.KindInt128)
	if err != nil {
		t.Fatalf("Coerce(-1 → Int128) error: %v", err)
	}
	hi, lo := v.AsInt128()
	if hi != -1 || lo != ^uint64(0) {
		t.Fatalf("AsInt128 = (%d,%d), want (-1,^0)", hi, lo)
	}
}

// TestCoerceIntLiteralToUint128 pins the u128 row of valid_literals_for_type.
func TestCoerceIntLiteralToUint128(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 127}, types.KindUint128)
	if err != nil {
		t.Fatalf("Coerce(127 → Uint128) error: %v", err)
	}
	if v.Kind() != types.KindUint128 {
		t.Fatalf("Kind = %v, want Uint128", v.Kind())
	}
	hi, lo := v.AsUint128()
	if hi != 0 || lo != 127 {
		t.Fatalf("AsUint128 = (%d,%d), want (0,127)", hi, lo)
	}
}

// TestCoerceNegativeIntoUint128Fails mirrors the reference invalid_literals
// rejection for `u8 = -1` extended to u128.
func TestCoerceNegativeIntoUint128Fails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: -1}, types.KindUint128)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceStringLiteralOnInt128Rejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitString, Str: "127"}, types.KindInt128)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceFloatLiteralOnUint128Rejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.5}, types.KindUint128)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceSenderRejectsInt128Column pins that :sender is rejected on 128-bit
// integer columns — they are not KindBytes, so the existing :sender guard
// applies.
func TestCoerceSenderRejectsInt128Column(t *testing.T) {
	_ = math.MaxInt8 // keep math import live if the underlying file trims it
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindInt128, &caller)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceIntLiteralToInt256 pins reference valid_literals_for_type at
// check.rs:360-370 for the i256 row — `= 127` on an i256 column must
// type-check. LitInt always fits Int256 via sign-extension from int64.
func TestCoerceIntLiteralToInt256(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 127}, types.KindInt256)
	if err != nil {
		t.Fatalf("Coerce(127 → Int256) error: %v", err)
	}
	if v.Kind() != types.KindInt256 {
		t.Fatalf("Kind = %v, want Int256", v.Kind())
	}
	w0, w1, w2, w3 := v.AsInt256()
	if w0 != 0 || w1 != 0 || w2 != 0 || w3 != 127 {
		t.Fatalf("AsInt256 = (%d,%d,%d,%d), want (0,0,0,127)", w0, w1, w2, w3)
	}
}

func TestCoerceNegativeIntLiteralToInt256(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: -1}, types.KindInt256)
	if err != nil {
		t.Fatalf("Coerce(-1 → Int256) error: %v", err)
	}
	w0, w1, w2, w3 := v.AsInt256()
	if w0 != -1 || w1 != ^uint64(0) || w2 != ^uint64(0) || w3 != ^uint64(0) {
		t.Fatalf("AsInt256 = (%d,%d,%d,%d), want all-ones", w0, w1, w2, w3)
	}
}

// TestCoerceIntLiteralToUint256 pins the u256 row of valid_literals_for_type.
func TestCoerceIntLiteralToUint256(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitInt, Int: 127}, types.KindUint256)
	if err != nil {
		t.Fatalf("Coerce(127 → Uint256) error: %v", err)
	}
	if v.Kind() != types.KindUint256 {
		t.Fatalf("Kind = %v, want Uint256", v.Kind())
	}
	w0, w1, w2, w3 := v.AsUint256()
	if w0 != 0 || w1 != 0 || w2 != 0 || w3 != 127 {
		t.Fatalf("AsUint256 = (%d,%d,%d,%d), want (0,0,0,127)", w0, w1, w2, w3)
	}
}

// TestCoerceNegativeIntoUint256Fails mirrors the reference invalid_literals
// rejection for `u8 = -1` extended to u256.
func TestCoerceNegativeIntoUint256Fails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: -1}, types.KindUint256)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceStringLiteralOnInt256Rejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitString, Str: "127"}, types.KindInt256)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceFloatLiteralOnUint256Rejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.5}, types.KindUint256)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceSenderRejectsInt256Column pins that :sender is rejected on 256-bit
// integer columns for the same reason the 128-bit case rejects.
func TestCoerceSenderRejectsInt256Column(t *testing.T) {
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindInt256, &caller)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceStringLiteralToTimestamp pins check.rs:334-352 — RFC3339-shaped
// string literals bind to KindTimestamp columns. Nanosecond precision is
// truncated to microseconds (reference: chrono::DateTime::timestamp_micros).
func TestCoerceStringLiteralToTimestamp(t *testing.T) {
	cases := []struct {
		sql   string
		micro int64
	}{
		{"2025-02-10T15:45:30Z", 1_739_202_330_000_000},
		{"2025-02-10T15:45:30.123Z", 1_739_202_330_123_000},
		{"2025-02-10T15:45:30.123456789Z", 1_739_202_330_123_456},
		{"2025-02-10 15:45:30+02:00", 1_739_195_130_000_000},
		{"2025-02-10 15:45:30.123+02:00", 1_739_195_130_123_000},
	}
	for _, c := range cases {
		v, err := Coerce(Literal{Kind: LitString, Str: c.sql}, types.KindTimestamp)
		if err != nil {
			t.Fatalf("Coerce(%q → Timestamp) error: %v", c.sql, err)
		}
		if v.Kind() != types.KindTimestamp {
			t.Fatalf("Kind = %v, want Timestamp", v.Kind())
		}
		if got := v.AsTimestamp(); got != c.micro {
			t.Fatalf("Coerce(%q) micros = %d, want %d", c.sql, got, c.micro)
		}
	}
}

// TestCoerceMalformedTimestampRejected pins that non-RFC3339 strings fail on a
// Timestamp column rather than silently becoming zero micros.
func TestCoerceMalformedTimestampRejected(t *testing.T) {
	for _, s := range []string{"", "2025-02-10", "not-a-timestamp", "2025-02-10T15:45"} {
		_, err := Coerce(Literal{Kind: LitString, Str: s}, types.KindTimestamp)
		if !errors.Is(err, ErrUnsupportedSQL) {
			t.Fatalf("Coerce(%q) err = %v, want ErrUnsupportedSQL", s, err)
		}
	}
}

func TestCoerceIntLiteralOnTimestampRejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: 0}, types.KindTimestamp)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceFloatLiteralOnTimestampRejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 0}, types.KindTimestamp)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func TestCoerceSenderRejectsTimestampColumn(t *testing.T) {
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindTimestamp, &caller)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceSenderRejectsArrayStringColumn pins the reference parity shape
// `SELECT * FROM t WHERE arr = :sender` at check.rs:487-489. The :sender
// parameter materializes as a 32-byte identity; an array-of-string column
// cannot accept it, and coerce rejects with ErrUnsupportedSQL.
func TestCoerceSenderRejectsArrayStringColumn(t *testing.T) {
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindArrayString, &caller)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceLiteralsRejectedOnArrayStringColumn pins that no SQL literal
// grammar can bind to an ArrayString column — there is no array literal in
// the Shunter grammar, and every scalar literal shape fails with a
// type-mismatch error.
func TestCoerceLiteralsRejectedOnArrayStringColumn(t *testing.T) {
	cases := []Literal{
		{Kind: LitInt, Int: 0},
		{Kind: LitFloat, Float: 1.0},
		{Kind: LitBool, Bool: true},
		{Kind: LitString, Str: "alpha"},
		{Kind: LitBytes, Bytes: []byte{0x01}},
	}
	for _, lit := range cases {
		_, err := Coerce(lit, types.KindArrayString)
		if !errors.Is(err, ErrUnsupportedSQL) {
			t.Fatalf("literal %v on KindArrayString: err = %v, want ErrUnsupportedSQL", lit.Kind, err)
		}
	}
}

// bigIntFromStr is a test helper — panics on parse failure, which catches
// typos in literal strings at test authoring time.
func bigIntFromStr(t *testing.T, s string) *big.Int {
	t.Helper()
	b, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("bigIntFromStr(%q) failed", s)
	}
	return b
}

// TestCoerceBigIntLiteralToUint256 pins the reference `u256 = 1e40` shape at
// reference/SpacetimeDB/crates/expr/src/check.rs:330-332. A LitBigInt with
// value 10^40 decomposes into four uint64 words matching the 256-bit
// little-significant-word-first layout and binds to a KindUint256 column.
func TestCoerceBigIntLiteralToUint256(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000") // 10^40
	v, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindUint256)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindUint256 {
		t.Fatalf("Kind = %v, want KindUint256", v.Kind())
	}
	w0, w1, w2, w3 := v.AsUint256()
	if w0 != 0 || w1 != 0x1d { // 10^40 = 0x1d...; top nonzero word is index 1
		t.Fatalf("AsUint256 top words = (%x, %x, ...), want (0, 0x1d, ...) for 10^40", w0, w1)
	}
	// low words: confirm round-trip back to big.Int reconstructs 10^40.
	var buf [32]byte
	// reassemble big-endian bytes
	for i, w := range [4]uint64{w0, w1, w2, w3} {
		for j := range 8 {
			buf[i*8+j] = byte(w >> (56 - 8*j))
		}
	}
	got := new(big.Int).SetBytes(buf[:])
	if got.Cmp(x) != 0 {
		t.Fatalf("round-trip = %s, want %s", got.String(), x.String())
	}
}

// TestCoerceBigIntLiteralToInt256 pins that a BigInt within Int256 range
// binds correctly including for negative values via two's-complement
// materialization.
func TestCoerceBigIntLiteralToInt256(t *testing.T) {
	// Positive: 10^40
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	v, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindInt256)
	if err != nil {
		t.Fatalf("positive Coerce error: %v", err)
	}
	if v.Kind() != types.KindInt256 {
		t.Fatalf("Kind = %v, want KindInt256", v.Kind())
	}

	// Negative: -10^40. Must materialize as two's-complement (sign-extend
	// 0xFFFF.. into the high words).
	neg := new(big.Int).Neg(x)
	v2, err := Coerce(Literal{Kind: LitBigInt, Big: neg}, types.KindInt256)
	if err != nil {
		t.Fatalf("negative Coerce error: %v", err)
	}
	w0, _, _, _ := v2.AsInt256()
	if w0 != -1 { // sign-extended high word
		t.Fatalf("negative AsInt256 w0 = %d, want -1", w0)
	}
}

// TestCoerceBigIntLiteralOverflowsUint128 pins that 10^40 (a value > 2^128)
// rejects when targeted at a Uint128 column — u128 max is ~3.4e38, below
// the 10^40 literal.
func TestCoerceBigIntLiteralOverflowsUint128(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindUint128)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceBigIntLiteralOverflowsUint256 pins that a BigInt > 2^256-1
// rejects at the Uint256 seam — covers the reference "Out of bounds" shape
// scaled up to 256-bit.
func TestCoerceBigIntLiteralOverflowsUint256(t *testing.T) {
	// 2^256 exactly — one past u256 max.
	x := new(big.Int).Lsh(big.NewInt(1), 256)
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindUint256)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceNegativeBigIntRejectedOnUint256 pins that a negative BigInt
// rejects on an unsigned 256-bit column — mirrors `u8 = -1` / `u256 = -1`
// rejection semantics at the wider-width boundary.
func TestCoerceNegativeBigIntRejectedOnUint256(t *testing.T) {
	x := new(big.Int).Neg(bigIntFromStr(t, "10000000000000000000000000000000000000000"))
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindUint256)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceBigIntLiteralOnInt64Rejected pins that a BigInt beyond int64
// range rejects on a narrower integer column — matches `u32 = 1e40` /
// `i64 = 1e40` overflow semantics.
func TestCoerceBigIntLiteralOnInt64Rejected(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindInt64)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

// TestCoerceBigIntLiteralToFloat32Infinity pins that the f32 = 1e40 path
// continues to accept overflow-to-+Inf after the parser promotes 1e40 to
// LitBigInt. The coerce path materializes the BigInt as float64 (1e40)
// then NewFloat32 rounds to +Inf.
func TestCoerceBigIntLiteralToFloat32Infinity(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	v, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindFloat32)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if !math.IsInf(float64(v.AsFloat32()), 1) {
		t.Fatalf("AsFloat32 = %v, want +Inf", v.AsFloat32())
	}
}

// TestCoerceBigIntLiteralToFloat64 pins that a BigInt within f64 range
// materializes exactly as the rounded f64 representation.
func TestCoerceBigIntLiteralToFloat64(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	v, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindFloat64)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.AsFloat64() != 1e40 {
		t.Fatalf("AsFloat64 = %v, want 1e40", v.AsFloat64())
	}
}

// TestCoerceBigIntLiteralOnStringColumnRejected pins that a BigInt literal
// rejects on non-numeric column kinds (string/bytes/bool/timestamp).
func TestCoerceBigIntLiteralOnStringColumnRejected(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindString)
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}
