package bsatn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func mustValueF32(t *testing.T, v float32) types.Value {
	t.Helper()
	out, err := types.NewFloat32(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustValueF64(t *testing.T, v float64) types.Value {
	t.Helper()
	out, err := types.NewFloat64(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestValueEncodeDecodeRoundTripAllKinds(t *testing.T) {
	large := strings.Repeat("a", 70*1024)
	cases := []types.Value{
		types.NewBool(true),
		types.NewInt8(-5),
		types.NewUint8(250),
		types.NewInt16(-0x1234),
		types.NewUint16(0xCAFE),
		types.NewInt32(-0x1234567),
		types.NewUint32(0x89ABCDEF),
		types.NewInt64(-0x102030405060708),
		types.NewUint64(0x0102030405060708),
		mustValueF32(t, math.Float32frombits(0x80000000)),
		mustValueF64(t, math.Float64frombits(0x8000000000000000)),
		types.NewString(large),
		types.NewBytes([]byte{1, 2, 3, 4}),
		types.NewUUID([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}),
	}
	for _, v := range cases {
		var buf bytes.Buffer
		if err := EncodeValue(&buf, v); err != nil {
			t.Fatalf("encode %v: %v", v.Kind(), err)
		}
		got, err := DecodeValue(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("decode %v: %v", v.Kind(), err)
		}
		if !v.Equal(got) {
			t.Fatalf("round-trip mismatch for %v", v.Kind())
		}
		if EncodedValueSize(v) != buf.Len() {
			t.Fatalf("size mismatch for %v: %d vs %d", v.Kind(), EncodedValueSize(v), buf.Len())
		}
	}
}

func TestValueEncodingTagsAndLittleEndianPayloads(t *testing.T) {
	f32 := mustValueF32(t, 1.5)
	f64 := mustValueF64(t, -3.25)
	cases := []struct {
		name string
		v    types.Value
		want []byte
	}{
		{"bool", types.NewBool(true), []byte{TagBool, 0x01}},
		{"int8", types.NewInt8(-1), []byte{TagInt8, 0xff}},
		{"uint8", types.NewUint8(7), []byte{TagUint8, 0x07}},
		{"int16", types.NewInt16(0x1234), []byte{TagInt16, 0x34, 0x12}},
		{"uint16", types.NewUint16(0xBEEF), []byte{TagUint16, 0xEF, 0xBE}},
		{"int32", types.NewInt32(0x12345678), []byte{TagInt32, 0x78, 0x56, 0x34, 0x12}},
		{"uint32", types.NewUint32(0x89ABCDEF), []byte{TagUint32, 0xEF, 0xCD, 0xAB, 0x89}},
		{"int64", types.NewInt64(0x0102030405060708), []byte{TagInt64, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}},
		{"uint64", types.NewUint64(0x0102030405060708), []byte{TagUint64, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}},
		{"float32", f32, func() []byte {
			var out [5]byte
			out[0] = TagFloat32
			binary.LittleEndian.PutUint32(out[1:], math.Float32bits(f32.AsFloat32()))
			return out[:]
		}()},
		{"float64", f64, func() []byte {
			var out [9]byte
			out[0] = TagFloat64
			binary.LittleEndian.PutUint64(out[1:], math.Float64bits(f64.AsFloat64()))
			return out[:]
		}()},
		{"string", types.NewString("go"), []byte{TagString, 0x02, 0x00, 0x00, 0x00, 'g', 'o'}},
		{"bytes", types.NewBytes([]byte{0xde, 0xad}), []byte{TagBytes, 0x02, 0x00, 0x00, 0x00, 0xde, 0xad}},
		{"uuid", types.NewUUID([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}), []byte{TagUUID, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := EncodeValue(&buf, tc.v); err != nil {
			t.Fatalf("encode %s: %v", tc.name, err)
		}
		if !bytes.Equal(buf.Bytes(), tc.want) {
			t.Fatalf("encoded %s = %v, want %v", tc.name, buf.Bytes(), tc.want)
		}
	}
}

func TestDecodeValueErrorsAndEmptyBytes(t *testing.T) {
	_, err := DecodeValue(bytes.NewReader([]byte{99}))
	var unknown *UnknownValueTagError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected UnknownValueTagError, got %v", err)
	}

	_, err = DecodeValueExpecting(bytes.NewReader([]byte{TagUint64, 1, 0, 0, 0, 0, 0, 0, 0}), types.KindString, "name")
	var mismatch *TypeTagMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected TypeTagMismatchError, got %v", err)
	}

	invalidUTF8 := []byte{TagString, 0x01, 0x00, 0x00, 0x00, 0xff}
	_, err = DecodeValue(bytes.NewReader(invalidUTF8))
	if !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("expected ErrInvalidUTF8, got %v", err)
	}

	_, err = DecodeValue(bytes.NewReader([]byte{TagBool, 2}))
	if !errors.Is(err, ErrInvalidBool) {
		t.Fatalf("expected ErrInvalidBool, got %v", err)
	}

	_, err = DecodeValue(bytes.NewReader([]byte{TagUint64, 1, 2, 3}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", err)
	}

	got, err := DecodeValue(bytes.NewReader([]byte{TagBytes, 0, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if got.AsBytes() == nil || len(got.AsBytes()) != 0 {
		t.Fatalf("empty bytes should decode to non-nil empty slice, got %#v", got.AsBytes())
	}
}

func TestDecodeProductValueShapeMismatchAndFromBytesLengthMismatch(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "name", Type: types.KindString},
		},
	}
	var short bytes.Buffer
	if err := EncodeValue(&short, types.NewUint64(1)); err != nil {
		t.Fatal(err)
	}
	_, err := DecodeProductValue(bytes.NewReader(short.Bytes()), ts)
	var shapeErr *RowShapeMismatchError
	if !errors.As(err, &shapeErr) {
		t.Fatalf("expected RowShapeMismatchError, got %v", err)
	}

	var exact bytes.Buffer
	if err := EncodeValue(&exact, types.NewUint64(1)); err != nil {
		t.Fatal(err)
	}
	if err := EncodeValue(&exact, types.NewString("alice")); err != nil {
		t.Fatal(err)
	}

	var extra bytes.Buffer
	if err := EncodeValue(&extra, types.NewUint64(1)); err != nil {
		t.Fatal(err)
	}
	if err := EncodeValue(&extra, types.NewString("alice")); err != nil {
		t.Fatal(err)
	}
	if err := EncodeValue(&extra, types.NewUint64(99)); err != nil {
		t.Fatal(err)
	}
	_, err = DecodeProductValue(bytes.NewReader(extra.Bytes()), ts)
	if !errors.As(err, &shapeErr) {
		t.Fatalf("expected RowShapeMismatchError for extra encoded column, got %v", err)
	}

	_, err = DecodeProductValueFromBytes(append(exact.Bytes(), 0xff), ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("expected ErrRowLengthMismatch for trailing bytes, got %v", err)
	}

	_, err = DecodeProductValueFromBytes(short.Bytes(), ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("expected ErrRowLengthMismatch for short row bytes, got %v", err)
	}
}
