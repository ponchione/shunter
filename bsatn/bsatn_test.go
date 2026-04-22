package bsatn

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func mustF32(t *testing.T, v float32) types.Value {
	t.Helper()
	val, err := types.NewFloat32(v)
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func mustF64(t *testing.T, v float64) types.Value {
	t.Helper()
	val, err := types.NewFloat64(v)
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func TestValueRoundTrip(t *testing.T) {
	cases := []types.Value{
		types.NewBool(true),
		types.NewBool(false),
		types.NewInt8(-128),
		types.NewUint8(255),
		types.NewInt16(-32768),
		types.NewUint16(65535),
		types.NewInt32(-2147483648),
		types.NewUint32(4294967295),
		types.NewInt64(math.MinInt64),
		types.NewUint64(math.MaxUint64),
		mustF32(t, 3.14),
		mustF64(t, 2.718281828),
		types.NewString("hello"),
		types.NewString(""),
		types.NewBytes([]byte{0xDE, 0xAD}),
		types.NewBytes([]byte{}),
		types.NewInt128(0, 127),
		types.NewInt128(-1, ^uint64(0)),
		types.NewInt128(math.MinInt64, 0),
		types.NewInt128(math.MaxInt64, ^uint64(0)),
		types.NewUint128(0, 0),
		types.NewUint128(0, ^uint64(0)),
		types.NewUint128(^uint64(0), ^uint64(0)),
		types.NewInt256(0, 0, 0, 127),
		types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)),
		types.NewInt256(math.MinInt64, 0, 0, 0),
		types.NewInt256(math.MaxInt64, ^uint64(0), ^uint64(0), ^uint64(0)),
		types.NewUint256(0, 0, 0, 0),
		types.NewUint256(0, 0, 0, ^uint64(0)),
		types.NewUint256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)),
	}
	for _, v := range cases {
		var buf bytes.Buffer
		if err := EncodeValue(&buf, v); err != nil {
			t.Fatalf("encode %v: %v", v.Kind(), err)
		}
		got, err := DecodeValue(&buf)
		if err != nil {
			t.Fatalf("decode %v: %v", v.Kind(), err)
		}
		if !v.Equal(got) {
			t.Fatalf("round-trip %v: got %v", v.Kind(), got.Kind())
		}
	}
}

func TestUnknownTag(t *testing.T) {
	_, err := DecodeValue(bytes.NewReader([]byte{99}))
	if err == nil {
		t.Fatal("expected unknown tag error")
	}
	var ute *UnknownValueTagError
	if !errors.As(err, &ute) {
		t.Fatalf("expected UnknownValueTagError, got %T", err)
	}
}

func TestProductValueRoundTrip(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "name", Type: types.KindString},
			{Index: 2, Name: "score", Type: types.KindInt64},
		},
	}
	pv := types.ProductValue{
		types.NewUint64(42),
		types.NewString("alice"),
		types.NewInt64(-100),
	}

	var buf bytes.Buffer
	if err := EncodeProductValue(&buf, pv); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeProductValue(&buf, ts)
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Equal(got) {
		t.Fatal("ProductValue round-trip mismatch")
	}
}

func TestDecodeProductValueFromBytesTrailing(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
		},
	}
	var buf bytes.Buffer
	EncodeValue(&buf, types.NewUint64(1))
	buf.WriteByte(0xFF) // trailing byte

	_, err := DecodeProductValueFromBytes(buf.Bytes(), ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("expected ErrRowLengthMismatch, got %v", err)
	}
	var shapeErr *RowShapeMismatchError
	if !errors.As(err, &shapeErr) {
		t.Fatalf("expected trailing-byte error to preserve RowShapeMismatchError details, got %T", err)
	}
	if shapeErr.TableName != "players" || shapeErr.Expected != 1 || shapeErr.Got != 2 {
		t.Fatalf("unexpected row shape details: %+v", shapeErr)
	}
}

func TestDecodeProductValueFromBytesShortPreservesRowShapeMismatch(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "name", Type: types.KindString},
		},
	}
	var buf bytes.Buffer
	EncodeValue(&buf, types.NewUint64(1))

	_, err := DecodeProductValueFromBytes(buf.Bytes(), ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("expected ErrRowLengthMismatch, got %v", err)
	}
	var shapeErr *RowShapeMismatchError
	if !errors.As(err, &shapeErr) {
		t.Fatalf("expected short-row error to preserve RowShapeMismatchError details, got %T", err)
	}
	if shapeErr.TableName != "players" || shapeErr.Expected != 2 || shapeErr.Got != 1 {
		t.Fatalf("unexpected row shape details: %+v", shapeErr)
	}
}

func TestEncodedValueSize(t *testing.T) {
	v := types.NewString("hello")
	var buf bytes.Buffer
	EncodeValue(&buf, v)
	if EncodedValueSize(v) != buf.Len() {
		t.Fatalf("size prediction %d != actual %d", EncodedValueSize(v), buf.Len())
	}
}

func TestEncodedValueSize128(t *testing.T) {
	cases := []types.Value{
		types.NewInt128(0, 127),
		types.NewUint128(^uint64(0), ^uint64(0)),
	}
	for _, v := range cases {
		var buf bytes.Buffer
		if err := EncodeValue(&buf, v); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if EncodedValueSize(v) != buf.Len() {
			t.Fatalf("%v size prediction %d != actual %d", v.Kind(), EncodedValueSize(v), buf.Len())
		}
		if buf.Len() != 17 {
			t.Fatalf("%v: expected 17 bytes, got %d", v.Kind(), buf.Len())
		}
	}
}

func TestEncodedValueSize256(t *testing.T) {
	cases := []types.Value{
		types.NewInt256(0, 0, 0, 127),
		types.NewUint256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)),
	}
	for _, v := range cases {
		var buf bytes.Buffer
		if err := EncodeValue(&buf, v); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if EncodedValueSize(v) != buf.Len() {
			t.Fatalf("%v size prediction %d != actual %d", v.Kind(), EncodedValueSize(v), buf.Len())
		}
		if buf.Len() != 33 {
			t.Fatalf("%v: expected 33 bytes, got %d", v.Kind(), buf.Len())
		}
	}
}

// TestEncode256LittleEndian pins the on-wire byte order: the least-significant
// word must land first and the (signed) most-significant word last.
func TestEncode256LittleEndian(t *testing.T) {
	v := types.NewUint256(0x0102030405060708, 0x1112131415161718, 0x2122232425262728, 0x3132333435363738)
	var buf bytes.Buffer
	if err := EncodeValue(&buf, v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := buf.Bytes()
	// tag + 32 bytes little-endian, lowest word first.
	want := []byte{
		TagUint256,
		0x38, 0x37, 0x36, 0x35, 0x34, 0x33, 0x32, 0x31,
		0x28, 0x27, 0x26, 0x25, 0x24, 0x23, 0x22, 0x21,
		0x18, 0x17, 0x16, 0x15, 0x14, 0x13, 0x12, 0x11,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded = %x\nwant     = %x", got, want)
	}
}
