package sql

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ponchione/shunter/types"
)

func assertUnsupportedSQL(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnsupportedSQL) {
		t.Fatalf("err = %v, want ErrUnsupportedSQL", err)
	}
}

func assertInvalidLiteral(t *testing.T, err error, wantLit, wantType string) {
	t.Helper()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var ilErr InvalidLiteralError
	if !errors.As(err, &ilErr) {
		t.Fatalf("err = %v, want InvalidLiteralError", err)
	}
	if ilErr.Literal != wantLit || ilErr.Type != wantType {
		t.Fatalf("got {%q, %q}, want {%q, %q}", ilErr.Literal, ilErr.Type, wantLit, wantType)
	}
	assertUnsupportedSQL(t, err)
}

func assertUnexpectedType(t *testing.T, err error, wantExpected, wantInferred string) {
	t.Helper()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var utErr UnexpectedTypeError
	if !errors.As(err, &utErr) {
		t.Fatalf("err = %v, want UnexpectedTypeError", err)
	}
	if utErr.Expected != wantExpected || utErr.Inferred != wantInferred {
		t.Fatalf("got {%q, %q}, want {%q, %q}", utErr.Expected, utErr.Inferred, wantExpected, wantInferred)
	}
	assertUnsupportedSQL(t, err)
}

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
	assertUnsupportedSQL(t, err)
}

func TestCoerceIntToSignedRangeCheck(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: 200}, types.KindInt8)
	assertUnsupportedSQL(t, err)
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

// TestCoerceStringDigitsWidensToInteger pins reference parse_int at
// expr/src/lib.rs:168-188 — `BigDecimal::from_str("42")` succeeds and
// flows through `BigDecimal::to_u64` to produce `Uint64(42)`. The 2026-04-
// 24 LitString-on-numeric widening replaces the prior coerce-boundary
// rejection on digit-only strings; non-numeric LitString continues to
// reject (`TestCoerceRejectsStringLiteralOnUint32Column`).
func TestCoerceStringDigitsWidensToInteger(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitString, Str: "42"}, types.KindUint64)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindUint64 || v.AsUint64() != 42 {
		t.Fatalf("got %+v, want Uint64(42)", v)
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
	assertUnsupportedSQL(t, err)
}

// TestCoerceRejectsFloatLiteralOnUint32Column pins the reference type-check
// rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines 502-504
// (`select * from t where t.u32 = 1.3` / "Field u32 is not a float"). A
// LitFloat against an integer column must return ErrUnsupportedSQL at the
// coerce boundary so the admission surface never folds the float into the
// integer kind silently.
func TestCoerceRejectsFloatLiteralOnUint32Column(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.3}, types.KindUint32)
	assertUnsupportedSQL(t, err)
}

// TestCoerceNonBoolLiteralOnBoolEmitsInvalidLiteral pins the reference
// `InvalidLiteral` literal at errors.rs:84 for non-Bool primitive literals
// against a Bool column. Reference path: `parse(value, AlgebraicType::Bool)`
// at lib.rs:99 has no Bool arm and falls to the `bail!` catch-all, which
// the outer `.map_err` folds into InvalidLiteral. Covers LitInt, LitFloat,
// LitString, LitBigInt — each reconstructed via `renderLiteralSourceText`
// (FormatInt / FormatFloat / lit.Str / Big.String). LitBytes is skipped
// because the Shunter Literal does not preserve a canonical hex source
// text; that surface is a separate slice.
func TestCoerceNonBoolLiteralOnBoolEmitsInvalidLiteral(t *testing.T) {
	big128 := new(big.Int)
	big128.SetString("340282366920938463463374607431768211456", 10)
	cases := []struct {
		name    string
		lit     Literal
		wantLit string
	}{
		{"LitInt", Literal{Kind: LitInt, Int: 1}, "1"},
		{"LitFloat", Literal{Kind: LitFloat, Float: 1.3}, "1.3"},
		{"LitString", Literal{Kind: LitString, Str: "foo"}, "foo"},
		{"LitBigInt", Literal{Kind: LitBigInt, Big: big128}, "340282366920938463463374607431768211456"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Coerce(tc.lit, types.KindBool)
			assertInvalidLiteral(t, err, tc.wantLit, "Bool")
		})
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
			assertInvalidLiteral(t, err, tc.wantLit, tc.wantTy)
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
	assertUnsupportedSQL(t, err)
}

func TestCoerceAppParameterWithoutTemplateFails(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitParameter, Param: "channel_id", Text: ":channel_id"}, types.KindString)
	var exprErr UnsupportedExprError
	if !errors.As(err, &exprErr) {
		t.Fatalf("Coerce app parameter error = %T %v, want UnsupportedExprError", err, err)
	}
	if exprErr.Expr != ":channel_id" {
		t.Fatalf("UnsupportedExprError.Expr = %q, want :channel_id", exprErr.Expr)
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

func TestCoerceSenderCallerIdentityDetachmentMetamorphic(t *testing.T) {
	const seed = uint64(0x5e7d3a)
	callers := [][32]byte{
		{0},
		{1, 2, 3, 4},
		{0xab, 0xcd, 0xef, 0x10},
	}
	for opIndex, caller := range callers {
		original := caller
		value, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindBytes, &caller)
		if err != nil {
			t.Fatalf("seed=%#x op_index=%d operation=CoerceSenderBytes: %v", seed, opIndex, err)
		}
		if got := value.AsBytes(); !bytes.Equal(got, original[:]) {
			t.Fatalf("seed=%#x op_index=%d operation=CoerceSenderBytes observed=%x expected=%x", seed, opIndex, got, original)
		}

		got := value.AsBytes()
		got[0] ^= 0xff
		if after := value.AsBytes(); !bytes.Equal(after, original[:]) {
			t.Fatalf("seed=%#x op_index=%d operation=MutateAccessorResult observed=%x expected=%x", seed, opIndex, after, original)
		}
		caller[1] ^= 0xff
		if after := value.AsBytes(); !bytes.Equal(after, original[:]) {
			t.Fatalf("seed=%#x op_index=%d operation=MutateCallerAfterCoerce observed=%x expected=%x", seed, opIndex, after, original)
		}

		stringValue, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindString, &caller)
		if err != nil {
			t.Fatalf("seed=%#x op_index=%d operation=CoerceSenderString: %v", seed, opIndex, err)
		}
		if got, want := stringValue.AsString(), hex.EncodeToString(caller[:]); got != want {
			t.Fatalf("seed=%#x op_index=%d operation=CoerceSenderString observed=%q expected=%q", seed, opIndex, got, want)
		}
	}
}

// TestCoerceSenderResolvesToHexOnStringColumn pins :sender widening to String
// as caller identity hex text.
func TestCoerceSenderResolvesToHexOnStringColumn(t *testing.T) {
	caller := [32]byte{1, 2, 3}
	v, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindString, &caller)
	if err != nil {
		t.Fatalf("CoerceWithCaller error: %v", err)
	}
	want := "010203" + strings.Repeat("00", 29)
	if v.Kind() != types.KindString || v.AsString() != want {
		t.Fatalf("got %+v, want String(%q)", v, want)
	}
}

// TestCoerceSenderResolvesToInvalidLiteralOnBoolColumn pins the reference
// reject text for `:sender` against a Bool column. resolve_sender substitutes
// a Hex source-text literal; lib.rs:359 has no Bool arm so parse falls to
// the `bail!` catch-all and folds to InvalidLiteral with the hex source
// text and type "Bool".
func TestCoerceSenderResolvesToInvalidLiteralOnBoolColumn(t *testing.T) {
	caller := [32]byte{1, 2, 3}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindBool, &caller)
	wantHex := "010203" + strings.Repeat("00", 29)
	assertInvalidLiteral(t, err, wantHex, "Bool")
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
	assertUnsupportedSQL(t, err)
}

// TestCoerceStringDigitsWidensToInt128 pins reference parse_int at the
// 128-bit row of the lib.rs:255-352 dispatch — `BigDecimal::from_str("127")`
// → `BigDecimal::to_i128` → `Int128(127)`. The 2026-04-24 LitString-on-
// numeric widening replaces the prior 128-bit reject; non-numeric forms
// continue to reject through the InvalidLiteral path.
func TestCoerceStringDigitsWidensToInt128(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitString, Str: "127"}, types.KindInt128)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	hi, lo := v.AsInt128()
	if hi != 0 || lo != 127 {
		t.Fatalf("AsInt128 = (%d,%d), want (0,127)", hi, lo)
	}
}

func TestCoerceFloatLiteralOnUint128Rejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.5}, types.KindUint128)
	assertUnsupportedSQL(t, err)
}

// TestCoerceSenderEmitsInvalidLiteralOnInt128Column pins reference parse on
// the I128 arm: resolve_sender substitutes Hex(identity hex) → parse_int →
// BigDecimal::from_str(hex) on a hex string containing a-f digits fails
// immediately, folding to InvalidLiteral{hex, "I128"}. A caller chosen with
// non-decimal hex digits guarantees the BigDecimal parse fails universally
// (a caller with all-digit hex would parse as a huge BigInt and route
// through the 128-bit overflow rejection — same shape, different code path).
func TestCoerceSenderEmitsInvalidLiteralOnInt128Column(t *testing.T) {
	_ = math.MaxInt8 // keep math import live if the underlying file trims it
	caller := [32]byte{0xab, 0xcd, 0xef}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindInt128, &caller)
	wantHex := "abcdef" + strings.Repeat("00", 29)
	assertInvalidLiteral(t, err, wantHex, "I128")
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

// TestCoerceStringDigitsWidensToInt256 pins the 256-bit row of the
// lib.rs:255-352 dispatch; reference `BigDecimal::from_str("127")` flows
// through the I256 to_int closure (lib.rs:238-245) to produce `Int256(127)`.
// Same 2026-04-24 LitString-on-numeric widening that closes the I128
// row.
func TestCoerceStringDigitsWidensToInt256(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitString, Str: "127"}, types.KindInt256)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	_, _, _, w3 := v.AsInt256()
	if w3 != 127 {
		t.Fatalf("AsInt256.lo = %d, want 127", w3)
	}
}

func TestCoerceFloatLiteralOnUint256Rejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.5}, types.KindUint256)
	assertUnsupportedSQL(t, err)
}

// TestCoerceSenderEmitsInvalidLiteralOnInt256Column mirrors the I128 shape
// with the I256 BigDecimal closure (lib.rs:238-245). Caller with non-decimal
// hex digits forces the BigDecimal parse to fail before reaching the I256
// range check.
func TestCoerceSenderEmitsInvalidLiteralOnInt256Column(t *testing.T) {
	caller := [32]byte{0xab, 0xcd, 0xef}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindInt256, &caller)
	wantHex := "abcdef" + strings.Repeat("00", 29)
	assertInvalidLiteral(t, err, wantHex, "I256")
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

// TestCoerceMalformedTimestampRejected pins the reference `InvalidLiteral` shape
// for non-RFC3339 strings against a `KindTimestamp` column. Reference path:
// `parse(value, Timestamp)` at expr/src/lib.rs:359 hits the catch-all
// `bail!("Literal values for type {} are not supported")`, folded by
// lib.rs:99 `.map_err` into `InvalidLiteral::new(v.into_string(), ty)`. The
// type renders through `fmt_algebraic_type` for the Product Timestamp shape
// `(__timestamp_micros_since_unix_epoch__: I64)`.
func TestCoerceMalformedTimestampRejected(t *testing.T) {
	const tsType = "(__timestamp_micros_since_unix_epoch__: I64)"
	for _, s := range []string{"", "2025-02-10", "not-a-timestamp", "2025-02-10T15:45"} {
		_, err := Coerce(Literal{Kind: LitString, Str: s}, types.KindTimestamp)
		assertInvalidLiteral(t, err, s, tsType)
	}
}

// TestCoerceIntLiteralOnTimestampRejected pins LitInt on KindTimestamp ->
// `InvalidLiteral` with the integer source text and the reference Timestamp
// Product type. Mirrors the malformed-string shape; reference parses every
// non-Timestamp literal through the same catch-all.
func TestCoerceIntLiteralOnTimestampRejected(t *testing.T) {
	const tsType = "(__timestamp_micros_since_unix_epoch__: I64)"
	_, err := Coerce(Literal{Kind: LitInt, Int: 42}, types.KindTimestamp)
	assertInvalidLiteral(t, err, "42", tsType)
}

// TestCoerceFloatLiteralOnTimestampRejected pins LitFloat on KindTimestamp ->
// `InvalidLiteral` with the float source text and the reference Timestamp
// Product type.
func TestCoerceFloatLiteralOnTimestampRejected(t *testing.T) {
	const tsType = "(__timestamp_micros_since_unix_epoch__: I64)"
	_, err := Coerce(Literal{Kind: LitFloat, Float: 1.3}, types.KindTimestamp)
	assertInvalidLiteral(t, err, "1.3", tsType)
}

// TestCoerceBoolLiteralOnTimestampRejected pins LitBool on KindTimestamp ->
// `UnexpectedType` with `Bool` expected and the reference Timestamp Product
// type inferred. Reference path: lib.rs:94 routes
// `(SqlExpr::Lit(SqlLiteral::Bool(_)), Some(ty))` directly to
// `UnexpectedType` (errors.rs:100), bypassing the lib.rs:99 InvalidLiteral
// fallback used by other literal kinds.
func TestCoerceBoolLiteralOnTimestampRejected(t *testing.T) {
	const tsType = "(__timestamp_micros_since_unix_epoch__: I64)"
	_, err := Coerce(Literal{Kind: LitBool, Bool: true}, types.KindTimestamp)
	assertUnexpectedType(t, err, "Bool", tsType)
}

func TestCoerceSenderRejectsTimestampColumn(t *testing.T) {
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindTimestamp, &caller)
	assertUnsupportedSQL(t, err)
}

func TestCoerceStringLiteralToDuration(t *testing.T) {
	cases := []struct {
		sql    string
		micros int64
	}{
		{"1s", 1_000_000},
		{"1.5s", 1_500_000},
		{"2h45m", 9_900_000_000},
		{"-3ms", -3_000},
	}
	for _, c := range cases {
		v, err := Coerce(Literal{Kind: LitString, Str: c.sql}, types.KindDuration)
		if err != nil {
			t.Fatalf("Coerce(%q -> Duration) error: %v", c.sql, err)
		}
		if v.Kind() != types.KindDuration {
			t.Fatalf("Kind = %v, want KindDuration", v.Kind())
		}
		if got := v.AsDurationMicros(); got != c.micros {
			t.Fatalf("Coerce(%q) micros = %d, want %d", c.sql, got, c.micros)
		}
	}
}

func TestCoerceMalformedDurationRejected(t *testing.T) {
	const durationType = "(__duration_micros__: I64)"
	for _, s := range []string{"", "not-a-duration", "5fortnights"} {
		_, err := Coerce(Literal{Kind: LitString, Str: s}, types.KindDuration)
		assertInvalidLiteral(t, err, s, durationType)
	}
}

func TestCoerceNonStringLiteralOnDurationRejected(t *testing.T) {
	const durationType = "(__duration_micros__: I64)"
	_, err := Coerce(Literal{Kind: LitInt, Int: 42}, types.KindDuration)
	assertInvalidLiteral(t, err, "42", durationType)
}

func TestCoerceBoolLiteralOnDurationRejected(t *testing.T) {
	const durationType = "(__duration_micros__: I64)"
	_, err := Coerce(Literal{Kind: LitBool, Bool: true}, types.KindDuration)
	assertUnexpectedType(t, err, "Bool", durationType)
}

func TestCoerceStringLiteralToUUID(t *testing.T) {
	const text = "00112233-4455-6677-8899-aabbccddeeff"
	v, err := Coerce(Literal{Kind: LitString, Str: text}, types.KindUUID)
	if err != nil {
		t.Fatalf("Coerce(%q -> UUID) error: %v", text, err)
	}
	if v.Kind() != types.KindUUID {
		t.Fatalf("Kind = %v, want KindUUID", v.Kind())
	}
	if got := v.UUIDString(); got != text {
		t.Fatalf("UUIDString = %q, want %q", got, text)
	}
}

func TestCoerceMalformedUUIDRejected(t *testing.T) {
	for _, s := range []string{
		"",
		"00112233445566778899aabbccddeeff",
		"00112233-4455-6677-8899-AABBCCDDEEFF",
		"00112233-4455-6677-8899-aabbccddeefg",
		"00112233-4455-6677-8899-aabbccddeeff00",
	} {
		_, err := Coerce(Literal{Kind: LitString, Str: s}, types.KindUUID)
		assertInvalidLiteral(t, err, s, "UUID")
	}
}

func TestCoerceNonStringLiteralOnUUIDRejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: 42}, types.KindUUID)
	assertInvalidLiteral(t, err, "42", "UUID")
}

func TestCoerceJSONStringCanonicalizes(t *testing.T) {
	v, err := Coerce(Literal{Kind: LitString, Str: `{"b":2,"a":1}`}, types.KindJSON)
	if err != nil {
		t.Fatalf("Coerce JSON returned error: %v", err)
	}
	if v.Kind() != types.KindJSON {
		t.Fatalf("Kind = %v, want KindJSON", v.Kind())
	}
	if got, want := string(v.AsJSON()), `{"a":1,"b":2}`; got != want {
		t.Fatalf("AsJSON = %q, want %q", got, want)
	}
}

func TestCoerceInvalidJSONRejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitString, Str: `{"a":`}, types.KindJSON)
	assertInvalidLiteral(t, err, `{"a":`, "JSON")
}

func TestCoerceNonStringLiteralOnJSONRejected(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitInt, Int: 42}, types.KindJSON)
	assertInvalidLiteral(t, err, "42", "JSON")
}

// TestCoerceSenderRejectsArrayStringColumn pins the reference-informed shape
// `SELECT * FROM t WHERE arr = :sender` at check.rs:487-489. The :sender
// parameter materializes as a 32-byte identity; an array-of-string column
// cannot accept it, and coerce rejects with ErrUnsupportedSQL.
func TestCoerceSenderRejectsArrayStringColumn(t *testing.T) {
	caller := [32]byte{1}
	_, err := CoerceWithCaller(Literal{Kind: LitSender}, types.KindArrayString, &caller)
	assertUnsupportedSQL(t, err)
}

// TestCoerceLiteralsRejectedOnArrayStringColumn pins reference error class
// routing on `KindArrayString`. Reference path: `parse(value, Array<String>)`
// at expr/src/lib.rs:359 hits the array-kind catch-all
// `bail!("Literal values for type {} are not supported")`, folded by
// lib.rs:99 `.map_err` into `InvalidLiteral::new(v.into_string(), ty)` for
// non-Bool literals. LitBool stays on the lib.rs:94 `UnexpectedType` arm.
// The type renders through `fmt_algebraic_type` for the `Array<...>`
// parameterized form: `Array<String>`.
func TestCoerceLiteralsRejectedOnArrayStringColumn(t *testing.T) {
	const arrType = "Array<String>"
	invalidCases := []struct {
		lit  Literal
		want string
	}{
		{Literal{Kind: LitInt, Int: 1}, "1"},
		{Literal{Kind: LitFloat, Float: 1.3}, "1.3"},
		{Literal{Kind: LitString, Str: "alpha"}, "alpha"},
		{Literal{Kind: LitBytes, Bytes: []byte{0xFF}, Text: "0xFF"}, "0xFF"},
		{Literal{Kind: LitBigInt, Big: bigIntFromStr(t, "10000000000000000000")}, "10000000000000000000"},
	}
	for _, c := range invalidCases {
		_, err := Coerce(c.lit, types.KindArrayString)
		assertInvalidLiteral(t, err, c.want, arrType)
	}
	_, err := Coerce(Literal{Kind: LitBool, Bool: true}, types.KindArrayString)
	assertUnexpectedType(t, err, "Bool", arrType)
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
	assertUnsupportedSQL(t, err)
}

// TestCoerceNegativeBigIntRejectedOnUint256 pins that a negative BigInt
// rejects on an unsigned 256-bit column — mirrors `u8 = -1` / `u256 = -1`
// rejection semantics at the wider-width boundary.
func TestCoerceNegativeBigIntRejectedOnUint256(t *testing.T) {
	x := new(big.Int).Neg(bigIntFromStr(t, "10000000000000000000000000000000000000000"))
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindUint256)
	assertUnsupportedSQL(t, err)
}

// TestCoerceBigIntLiteralOnInt64Rejected pins that a BigInt beyond int64
// range rejects on a narrower integer column — matches `u32 = 1e40` /
// `i64 = 1e40` overflow semantics.
func TestCoerceBigIntLiteralOnInt64Rejected(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000")
	_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindInt64)
	assertUnsupportedSQL(t, err)
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

// TestCoerceLitIntOnStringColumnWidens pins the reference widening at
// expr/src/lib.rs:353 (`AlgebraicType::String => Ok(AlgebraicValue::String(
// value.into()))`). Reference `parse(value, String)` wraps the SqlLiteral
// source text as String for any of `Str | Num | Hex` literal categories;
// Shunter routes LitInt through `renderLiteralSourceText` (FormatInt) so
// `WHERE strcol = 42` binds as the string `"42"` rather than rejecting.
func TestCoerceLitIntOnStringColumnWidens(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want string
	}{
		{"positive", 42, "42"},
		{"zero", 0, "0"},
		{"negative", -7, "-7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Coerce(Literal{Kind: LitInt, Int: tc.in}, types.KindString)
			if err != nil {
				t.Fatalf("Coerce error: %v", err)
			}
			if v.Kind() != types.KindString || v.AsString() != tc.want {
				t.Fatalf("got %+v, want String(%q)", v, tc.want)
			}
		})
	}
}

// TestCoerceLitFloatOnStringColumnWidens mirrors the LitInt widening for
// LitFloat. Reference accepts via the same lib.rs:353 String arm; Shunter
// renders via `strconv.FormatFloat('g', -1, 64)`. Round-trip-lossy forms
// (`1.10` → "1.1") and scientific-notation forms (parser collapses `1e3`
// to LitInt) carry the documented Shunter-canonical form pending source-
// text preservation on `sql.Literal`.
func TestCoerceLitFloatOnStringColumnWidens(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{"plain", 1.3, "1.3"},
		{"negative", -2.5, "-2.5"},
		{"integral_float", 7.0, "7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Coerce(Literal{Kind: LitFloat, Float: tc.in}, types.KindString)
			if err != nil {
				t.Fatalf("Coerce error: %v", err)
			}
			if v.Kind() != types.KindString || v.AsString() != tc.want {
				t.Fatalf("got %+v, want String(%q)", v, tc.want)
			}
		})
	}
}

// TestCoerceLitBigIntOnStringColumnWidens pins that LitBigInt also widens
// onto a KindString column. Reference flows scientific-notation source
// text (`1e40`) through `parse(value, String)` unchanged; Shunter parser
// collapses the source token to `*big.Int` at `parseNumericLiteral` so the
// widened string carries the canonical decimal form (`Big.String()`)
// rather than the original token. Documented Shunter-side divergence
// pending source-text preservation on `sql.Literal` (matches the existing
// pattern for InvalidLiteral 128/256-bit overflow text).
func TestCoerceLitBigIntOnStringColumnWidens(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000") // 10^40
	v, err := Coerce(Literal{Kind: LitBigInt, Big: x}, types.KindString)
	if err != nil {
		t.Fatalf("Coerce error: %v", err)
	}
	if v.Kind() != types.KindString {
		t.Fatalf("Kind = %v, want KindString", v.Kind())
	}
	if got := v.AsString(); got != x.String() {
		t.Fatalf("AsString = %q, want %q", got, x.String())
	}
}

// TestCoerceLitBytesOnStringColumnDeferred pins that LitBytes does not yet
// widen onto a KindString column. Reference accepts the source-text form
// (e.g. `0xdeadbeef` → String("0xdeadbeef")) via lib.rs:353. Shunter's
// parser decodes the hex token into bytes at `parseHexLiteral`, losing
// the original `0x...`/`X'...'` source text; `renderLiteralSourceText`
// returns false for LitBytes for that reason. Closing this case requires
// the source-text preservation slice (separate). Until then, LitBytes →
// KindString must still reject with `ErrUnsupportedSQL` so Shunter never
// invents a rendering that diverges from any reference source token.
func TestCoerceLitBytesOnStringColumnDeferred(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitBytes, Bytes: []byte{0xde, 0xad, 0xbe, 0xef}}, types.KindString)
	assertUnsupportedSQL(t, err)
}

// TestCoerceLitBoolOnStringColumnEmitsUnexpectedType pins that the widening
// onto KindString does not include LitBool. Reference lib.rs:94 routes
// `(SqlLiteral::Bool(_), Some(ty))` (with ty != Bool) to UnexpectedType;
// only `Str | Num | Hex` reach the lib.rs:353 String arm. Shunter mirrors
// this via the existing `mismatch` LitBool branch, which returns
// `UnexpectedTypeError{Expected:"Bool", Inferred:"String"}`. Strengthens
// `TestCoerceBoolToStringFails` (which only asserts ErrUnsupportedSQL).
func TestCoerceLitBoolOnStringColumnEmitsUnexpectedType(t *testing.T) {
	_, err := Coerce(Literal{Kind: LitBool, Bool: true}, types.KindString)
	assertUnexpectedType(t, err, "Bool", "String")
}

// TestCoerceLitStringNumericTokenWidensOntoNumericKinds pins string numeric
// literal widening onto integer and float kinds.
func TestCoerceLitStringNumericTokenWidensOntoNumericKinds(t *testing.T) {
	cases := []struct {
		name    string
		litStr  string
		kind    types.ValueKind
		checkOK func(t *testing.T, v types.Value)
	}{
		{"digits_to_U32", "123", types.KindUint32, func(t *testing.T, v types.Value) {
			if v.AsUint32() != 123 {
				t.Fatalf("AsUint32 = %d, want 123", v.AsUint32())
			}
		}},
		{"digits_to_I32", "-7", types.KindInt32, func(t *testing.T, v types.Value) {
			if v.AsInt32() != -7 {
				t.Fatalf("AsInt32 = %d, want -7", v.AsInt32())
			}
		}},
		{"digits_to_U64", "42", types.KindUint64, func(t *testing.T, v types.Value) {
			if v.AsUint64() != 42 {
				t.Fatalf("AsUint64 = %d, want 42", v.AsUint64())
			}
		}},
		{"scientific_to_U32", "1e3", types.KindUint32, func(t *testing.T, v types.Value) {
			if v.AsUint32() != 1000 {
				t.Fatalf("AsUint32 = %d, want 1000", v.AsUint32())
			}
		}},
		{"digits_to_I128", "127", types.KindInt128, func(t *testing.T, v types.Value) {
			hi, lo := v.AsInt128()
			if hi != 0 || lo != 127 {
				t.Fatalf("AsInt128 = (%d,%d), want (0,127)", hi, lo)
			}
		}},
		{"digits_to_I256", "127", types.KindInt256, func(t *testing.T, v types.Value) {
			_, _, _, w3 := v.AsInt256()
			if w3 != 127 {
				t.Fatalf("AsInt256.lo = %d, want 127", w3)
			}
		}},
		{"digits_to_F32", "1.5", types.KindFloat32, func(t *testing.T, v types.Value) {
			if v.AsFloat32() != 1.5 {
				t.Fatalf("AsFloat32 = %v, want 1.5", v.AsFloat32())
			}
		}},
		{"integer_to_F64", "42", types.KindFloat64, func(t *testing.T, v types.Value) {
			if v.AsFloat64() != 42.0 {
				t.Fatalf("AsFloat64 = %v, want 42", v.AsFloat64())
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Coerce(Literal{Kind: LitString, Str: tc.litStr}, tc.kind)
			if err != nil {
				t.Fatalf("Coerce(%q → %s) error: %v", tc.litStr, tc.kind, err)
			}
			if v.Kind() != tc.kind {
				t.Fatalf("Kind = %v, want %v", v.Kind(), tc.kind)
			}
			tc.checkOK(t, v)
		})
	}
}

// TestCoerceLitStringFailingNumericEmitsInvalidLiteral pins InvalidLiteral
// errors for string literals coerced onto numeric kinds.
func TestCoerceLitStringFailingNumericEmitsInvalidLiteral(t *testing.T) {
	cases := []struct {
		name    string
		litStr  string
		kind    types.ValueKind
		wantTy  string
		wantLit string
	}{
		{"non_numeric_on_U32", "foo", types.KindUint32, "U32", "foo"},
		{"empty_on_U32", "", types.KindUint32, "U32", ""},
		{"fractional_on_U32", "1.3", types.KindUint32, "U32", "1.3"},
		{"negative_on_U32", "-1", types.KindUint32, "U32", "-1"},
		{"overflow_on_U8", "256", types.KindUint8, "U8", "256"},
		{"scientific_overflow_on_U32", "1e40", types.KindUint32, "U32", "1e40"},
		{"non_numeric_on_F32", "foo", types.KindFloat32, "F32", "foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Coerce(Literal{Kind: LitString, Str: tc.litStr}, tc.kind)
			assertInvalidLiteral(t, err, tc.wantLit, tc.wantTy)
		})
	}
}

// TestCoerceLitBigIntOnNarrowIntegerEmitsInvalidLiteral pins InvalidLiteral
// errors for bigint literals on narrow integer kinds.
func TestCoerceLitBigIntOnNarrowIntegerEmitsInvalidLiteral(t *testing.T) {
	x := bigIntFromStr(t, "10000000000000000000000000000000000000000") // 10^40
	cases := []struct {
		name    string
		kind    types.ValueKind
		wantTy  string
		wantLit string
	}{
		{"on_U32", types.KindUint32, "U32", x.String()},
		{"on_I32", types.KindInt32, "I32", x.String()},
		{"on_U64", types.KindUint64, "U64", x.String()},
		{"on_I64", types.KindInt64, "I64", x.String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Coerce(Literal{Kind: LitBigInt, Big: x}, tc.kind)
			assertInvalidLiteral(t, err, tc.wantLit, tc.wantTy)
		})
	}
}

// parseFilterLiteral drives Parse() on a one-filter SELECT and returns the
// parsed Literal. Used by the contract pins below to exercise the full source-
// text-preservation seam (parser → Literal.Text → coerce) rather than
// constructing a Literal directly.
func parseFilterLiteral(t *testing.T, sql string) Literal {
	t.Helper()
	stmt, err := Parse(sql)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", sql, err)
	}
	if len(stmt.Filters) != 1 {
		t.Fatalf("Parse(%q) Filters = %d, want 1", sql, len(stmt.Filters))
	}
	return stmt.Filters[0].Literal
}

// TestCoerceParserPreservedSourceTextOnInvalidLiteral pins the source-text
// preservation seam end-to-end through the parser → coerce path. Reference
// `parse(value, ty)` at expr/src/lib.rs takes the SqlLiteral source-text body
// directly; Shunter's parser preserves the original numeric / hex / string
// token in `Literal.Text` so `renderLiteralSourceText` returns the original
// form even when `parseNumericLiteral` collapses (`1e3` → 1000) or rounds
// (`1.10` → 1.1). Each row drives `Parse(<sql>)` → `Coerce(filter.Literal,
// kind)` and asserts the resulting `InvalidLiteralError.Literal` matches the
// raw source token, not the canonical numeric form.
func TestCoerceParserPreservedSourceTextOnInvalidLiteral(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		kind    types.ValueKind
		wantLit string
		wantTy  string
	}{
		{"u8_scientific_overflows", "SELECT * FROM t WHERE u8 = 1e3", types.KindUint8, "1e3", "U8"},
		{"u8_leading_plus_overflow", "SELECT * FROM t WHERE u8 = +1000", types.KindUint8, "+1000", "U8"},
		{"u8_round_trip_lossy_float", "SELECT * FROM t WHERE u8 = 1.10", types.KindUint8, "1.10", "U8"},
		{"i64_rounded_fractional_boundary", "SELECT * FROM t WHERE i64 = 9223372036854775807.5", types.KindInt64, "9223372036854775807.5", "I64"},
		{"u32_quoted_scientific", "SELECT * FROM t WHERE u32 = '1e40'", types.KindUint32, "1e40", "U32"},
		{"u32_hex_token", "SELECT * FROM t WHERE u32 = 0x01", types.KindUint32, "0x01", "U32"},
		{"bool_hex_token", "SELECT * FROM t WHERE b = 0x01", types.KindBool, "0x01", "Bool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lit := parseFilterLiteral(t, tc.sql)
			_, err := Coerce(lit, tc.kind)
			assertInvalidLiteral(t, err, tc.wantLit, tc.wantTy)
		})
	}
}

// TestCoerceParserPreservedSourceTextWidensOntoString pins the same
// source-text seam onto the `KindString` widening arm at lib.rs:353. Forms
// that the parser collapses or rounds (`1e3` → LitInt(1000), `001` →
// LitInt(1), `1.10` → LitFloat(1.1)) keep the original token through `Text`
// so the widened String value mirrors the reference `String(value.into())`
// renderings. Hex literals (`0xDEADBEEF`) widen as the original token text.
func TestCoerceParserPreservedSourceTextWidensOntoString(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"leading_zeros", "SELECT * FROM t WHERE name = 001", "001"},
		{"round_trip_lossy_float", "SELECT * FROM t WHERE name = 1.10", "1.10"},
		{"scientific_collapses", "SELECT * FROM t WHERE name = 1e3", "1e3"},
		{"hex_literal", "SELECT * FROM t WHERE name = 0xDEADBEEF", "0xDEADBEEF"},
		{"big_int_scientific", "SELECT * FROM t WHERE name = 1e40", "1e40"},
		{"leading_plus", "SELECT * FROM t WHERE name = +1000", "+1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lit := parseFilterLiteral(t, tc.sql)
			v, err := Coerce(lit, types.KindString)
			if err != nil {
				t.Fatalf("Coerce error: %v", err)
			}
			if v.Kind() != types.KindString || v.AsString() != tc.want {
				t.Fatalf("got %+v, want String(%q)", v, tc.want)
			}
		})
	}
}

// TestCoerceParserStrNumHexOnBytesViaFromHexPad pins the reference
// `parse(value, AlgebraicType::Bytes)` arm at expr/src/lib.rs:218 onto the
// parser-driven `KindBytes` path. `from_hex_pad` strips an optional `0x`
// prefix and decodes even-length hex digit pairs; Str / Num / Hex source
// text all flow through the same routing. Decode failure folds to
// `InvalidLiteral` with `Type = "Array<U8>"`.
func TestCoerceParserStrNumHexOnBytesViaFromHexPad(t *testing.T) {
	t.Run("string_with_0x_prefix_binds", func(t *testing.T) {
		lit := parseFilterLiteral(t, "SELECT * FROM t WHERE bytes = '0x0102'")
		v, err := Coerce(lit, types.KindBytes)
		if err != nil {
			t.Fatalf("Coerce error: %v", err)
		}
		got := v.AsBytes()
		if len(got) != 2 || got[0] != 0x01 || got[1] != 0x02 {
			t.Fatalf("AsBytes = %x, want 0102", got)
		}
	})
	t.Run("numeric_token_binds_as_hex", func(t *testing.T) {
		lit := parseFilterLiteral(t, "SELECT * FROM t WHERE bytes = 42")
		v, err := Coerce(lit, types.KindBytes)
		if err != nil {
			t.Fatalf("Coerce error: %v", err)
		}
		got := v.AsBytes()
		if len(got) != 1 || got[0] != 0x42 {
			t.Fatalf("AsBytes = %x, want 42", got)
		}
	})
	t.Run("hex_token_binds_via_decoded_bytes", func(t *testing.T) {
		lit := parseFilterLiteral(t, "SELECT * FROM t WHERE bytes = 0xDEADBEEF")
		v, err := Coerce(lit, types.KindBytes)
		if err != nil {
			t.Fatalf("Coerce error: %v", err)
		}
		got := v.AsBytes()
		want := []byte{0xde, 0xad, 0xbe, 0xef}
		if len(got) != len(want) {
			t.Fatalf("AsBytes len = %d, want %d", len(got), len(want))
		}
		for i, b := range want {
			if got[i] != b {
				t.Fatalf("AsBytes[%d] = %x, want %x", i, got[i], b)
			}
		}
	})
	t.Run("non_hex_string_emits_invalid_literal_array_u8", func(t *testing.T) {
		lit := parseFilterLiteral(t, "SELECT * FROM t WHERE bytes = 'not-hex'")
		_, err := Coerce(lit, types.KindBytes)
		assertInvalidLiteral(t, err, "not-hex", "Array<U8>")
	})
	t.Run("lowercase_x_escaped_string_emits_invalid_literal_array_u8", func(t *testing.T) {
		lit := parseFilterLiteral(t, "SELECT * FROM t WHERE bytes = 'x''AB'")
		_, err := Coerce(lit, types.KindBytes)
		assertInvalidLiteral(t, err, "x'AB", "Array<U8>")
	})
	t.Run("float_emits_invalid_literal_array_u8", func(t *testing.T) {
		lit := parseFilterLiteral(t, "SELECT * FROM t WHERE bytes = 1.3")
		_, err := Coerce(lit, types.KindBytes)
		assertInvalidLiteral(t, err, "1.3", "Array<U8>")
	})
}
