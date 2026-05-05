package bsatn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

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
		mustFloat32Value(t, 3.14),
		mustFloat64Value(t, 2.718281828),
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
		types.NewTimestamp(0),
		types.NewTimestamp(-1),
		types.NewTimestamp(math.MinInt64),
		types.NewTimestamp(math.MaxInt64),
		types.NewTimestamp(1_739_201_130_000_000),
		types.NewDuration(0),
		types.NewDuration(-1),
		types.NewDuration(math.MinInt64),
		types.NewDuration(math.MaxInt64),
		types.NewDuration(12_345_678),
		types.NewArrayString(nil),
		types.NewArrayString([]string{}),
		types.NewArrayString([]string{""}),
		types.NewArrayString([]string{"alpha"}),
		types.NewArrayString([]string{"alpha", "beta", "γ"}),
		types.NewUUID([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}),
		mustJSONValue(t, `{"b":2,"a":1}`),
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
			{Index: 3, Name: "uuid", Type: types.KindUUID},
			{Index: 4, Name: "ttl", Type: types.KindDuration},
			{Index: 5, Name: "metadata", Type: types.KindJSON},
		},
	}
	pv := types.ProductValue{
		types.NewUint64(42),
		types.NewString("alice"),
		types.NewInt64(-100),
		types.NewUUID([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}),
		types.NewDuration(90_000_000),
		mustJSONValue(t, `{"tier":"gold"}`),
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

func TestNullableProductValueRoundTripAndEncoding(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "nickname", Type: types.KindString, Nullable: true},
			{Index: 2, Name: "metadata", Type: types.KindJSON, Nullable: true},
		},
	}
	row := types.ProductValue{
		types.NewUint64(7),
		types.NewNull(types.KindString),
		mustJSONValue(t, `{"b":2,"a":1}`),
	}
	encoded, err := AppendProductValueForSchema(nil, row, ts)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []byte{TagUint64, 7, 0, 0, 0, 0, 0, 0, 0, TagString, 0, TagJSON, 1}
	if !bytes.Equal(encoded[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("nullable encoded prefix = % x, want % x", encoded[:len(wantPrefix)], wantPrefix)
	}
	got, err := DecodeProductValueFromBytes(encoded, ts)
	if err != nil {
		t.Fatal(err)
	}
	if !row.Equal(got) {
		t.Fatalf("nullable round-trip = %+v, want %+v", got, row)
	}
	size, err := EncodedProductValueSizeForSchema(row, ts)
	if err != nil {
		t.Fatal(err)
	}
	if size != len(encoded) {
		t.Fatalf("EncodedProductValueSizeForSchema = %d, encoded length = %d", size, len(encoded))
	}
}

func TestAppendProductValueMatchesWriterEncoding(t *testing.T) {
	pv := types.ProductValue{
		types.NewUint64(42),
		types.NewString("alice"),
		types.NewBytes([]byte{1, 2, 3}),
		types.NewArrayString([]string{"red", "blue"}),
		mustJSONValue(t, `{"b":2,"a":1}`),
	}
	var buf bytes.Buffer
	if err := EncodeProductValue(&buf, pv); err != nil {
		t.Fatal(err)
	}
	got, err := AppendProductValue(make([]byte, 0, EncodedProductValueSize(pv)), pv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Fatalf("AppendProductValue bytes = %x, want %x", got, buf.Bytes())
	}
}

func TestEncodeValueDetectsShortWrites(t *testing.T) {
	cases := []types.Value{
		types.NewUint64(42),
		types.NewString("alice"),
		types.NewBytes([]byte{1, 2, 3}),
		types.NewArrayString([]string{"red", "blue"}),
		types.NewUUID([16]byte{1, 2, 3}),
		mustJSONValue(t, `{"a":1}`),
	}
	for _, v := range cases {
		w := shortWriter{max: 1}
		err := EncodeValue(&w, v)
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("EncodeValue(%s) error = %v, want io.ErrShortWrite", v.Kind(), err)
		}
	}
}

type shortWriter struct {
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) <= w.max {
		return len(p), nil
	}
	return w.max, nil
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

func TestDecodeProductValueReaderShapeMismatchPreservesSentinel(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "name", Type: types.KindString},
		},
	}

	var short bytes.Buffer
	EncodeValue(&short, types.NewUint64(1))
	_, err := DecodeProductValue(bytes.NewReader(short.Bytes()), ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("short reader error = %v, want ErrRowLengthMismatch", err)
	}
	var shapeErr *RowShapeMismatchError
	if !errors.As(err, &shapeErr) {
		t.Fatalf("short reader error = %T, want RowShapeMismatchError details", err)
	}
	if shapeErr.TableName != "players" || shapeErr.Expected != 2 || shapeErr.Got != 1 {
		t.Fatalf("unexpected short reader row shape details: %+v", shapeErr)
	}

	var trailing bytes.Buffer
	EncodeValue(&trailing, types.NewUint64(1))
	EncodeValue(&trailing, types.NewString("alice"))
	trailing.WriteByte(0xff)
	shapeErr = nil
	_, err = DecodeProductValue(bytes.NewReader(trailing.Bytes()), ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("trailing reader error = %v, want ErrRowLengthMismatch", err)
	}
	if !errors.As(err, &shapeErr) {
		t.Fatalf("trailing reader error = %T, want RowShapeMismatchError details", err)
	}
	if shapeErr.TableName != "players" || shapeErr.Expected != 2 || shapeErr.Got != 3 {
		t.Fatalf("unexpected trailing reader row shape details: %+v", shapeErr)
	}
}

func TestDecodeProductValueFromBytesDetachesBytesColumnFromInput(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "files",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "payload", Type: types.KindBytes},
		},
	}
	row := types.ProductValue{
		types.NewUint64(7),
		types.NewBytes([]byte{0xde, 0xad, 0xbe, 0xef}),
	}
	encoded, err := AppendProductValue(nil, row)
	if err != nil {
		t.Fatalf("AppendProductValue: %v", err)
	}

	decoded, err := DecodeProductValueFromBytes(encoded, ts)
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes: %v", err)
	}
	payload := decoded[1].AsBytes()
	if !bytes.Equal(payload, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("decoded payload = %x, want deadbeef", payload)
	}
	for i := range encoded {
		encoded[i] ^= 0xff
	}
	if !bytes.Equal(payload, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("decoded payload aliases input after mutation: %x", payload)
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

func TestEncodedValueSizeTimestamp(t *testing.T) {
	v := types.NewTimestamp(1_739_201_130_000_000)
	var buf bytes.Buffer
	if err := EncodeValue(&buf, v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if EncodedValueSize(v) != buf.Len() {
		t.Fatalf("Timestamp size prediction %d != actual %d", EncodedValueSize(v), buf.Len())
	}
	if buf.Len() != 9 {
		t.Fatalf("Timestamp: expected 9 bytes (tag + 8 LE i64), got %d", buf.Len())
	}
}

// TestEncodeTimestampLittleEndian pins the on-wire byte order: tag + 8 bytes
// little-endian signed microseconds since the Unix epoch.
func TestEncodeTimestampLittleEndian(t *testing.T) {
	v := types.NewTimestamp(int64(0x0102030405060708))
	var buf bytes.Buffer
	if err := EncodeValue(&buf, v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{
		TagTimestamp,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("encoded = %x\nwant     = %x", buf.Bytes(), want)
	}
}

// TestEncodedValueSizeArrayString pins the predicted size for an ArrayString
// payload: tag + u32 count + per-element u32 length + utf8 bytes.
func TestEncodedValueSizeArrayString(t *testing.T) {
	v := types.NewArrayString([]string{"a", "bcd", ""})
	var buf bytes.Buffer
	if err := EncodeValue(&buf, v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if EncodedValueSize(v) != buf.Len() {
		t.Fatalf("ArrayString size prediction %d != actual %d", EncodedValueSize(v), buf.Len())
	}
	// 1 tag + 4 count + (4+1) + (4+3) + (4+0) = 1 + 4 + 5 + 7 + 4 = 21
	if buf.Len() != 21 {
		t.Fatalf("ArrayString: expected 21 bytes, got %d", buf.Len())
	}
}

// TestEncodeArrayStringLittleEndianLayout pins the on-wire byte order:
// tag + LE u32 count + [LE u32 length + utf8 bytes]* per element.
func TestEncodeArrayStringLittleEndianLayout(t *testing.T) {
	v := types.NewArrayString([]string{"ab", "cde"})
	var buf bytes.Buffer
	if err := EncodeValue(&buf, v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{
		TagArrayString,
		0x02, 0x00, 0x00, 0x00, // count = 2
		0x02, 0x00, 0x00, 0x00, // len("ab")
		'a', 'b',
		0x03, 0x00, 0x00, 0x00, // len("cde")
		'c', 'd', 'e',
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("encoded = %x\nwant     = %x", buf.Bytes(), want)
	}
}

func TestEncodeJSONLittleEndianLayout(t *testing.T) {
	v := mustJSONValue(t, `{"b":2,"a":1}`)
	var buf bytes.Buffer
	if err := EncodeValue(&buf, v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{
		TagJSON,
		0x0d, 0x00, 0x00, 0x00,
		'{', '"', 'a', '"', ':', '1', ',', '"', 'b', '"', ':', '2', '}',
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("encoded = %x\nwant     = %x", buf.Bytes(), want)
	}
	if EncodedValueSize(v) != len(want) {
		t.Fatalf("EncodedValueSize(JSON) = %d, want %d", EncodedValueSize(v), len(want))
	}
}

// TestDecodeArrayStringRejectsInvalidUTF8 pins that element payloads are
// validated as utf8, matching the single-KindString rule.
func TestDecodeArrayStringRejectsInvalidUTF8(t *testing.T) {
	raw := []byte{
		TagArrayString,
		0x01, 0x00, 0x00, 0x00, // count = 1
		0x01, 0x00, 0x00, 0x00, // len = 1
		0xFF, // invalid utf8
	}
	_, err := DecodeValue(bytes.NewReader(raw))
	if !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("err = %v, want ErrInvalidUTF8", err)
	}
}

func TestDecodeJSONRejectsInvalidJSON(t *testing.T) {
	raw := []byte{
		TagJSON,
		0x07, 0x00, 0x00, 0x00,
		'{', '"', 'a', '"', ':', '1', ',',
	}
	_, err := DecodeValue(bytes.NewReader(raw))
	if !errors.Is(err, types.ErrInvalidJSON) {
		t.Fatalf("err = %v, want ErrInvalidJSON", err)
	}
}

func TestDecodeValueRejectsHugeTruncatedLengthWithoutAllocation(t *testing.T) {
	for _, tag := range []byte{TagString, TagBytes, TagJSON} {
		raw := []byte{tag, 0xff, 0xff, 0xff, 0xff}
		_, err := DecodeValue(bytes.NewReader(raw))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("DecodeValue tag %d err = %v, want io.ErrUnexpectedEOF", tag, err)
		}
	}

	raw := []byte{TagArrayString, 0xff, 0xff, 0xff, 0xff}
	_, err := DecodeValue(bytes.NewReader(raw))
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("DecodeValue ArrayString err = %v, want EOF", err)
	}
}

func TestDecodeProductValueFromBytesRejectsImpossibleArrayStringCount(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "labels",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "tags", Type: types.KindArrayString},
		},
	}
	raw := []byte{TagArrayString, 0xff, 0xff, 0xff, 0xff}

	_, err := DecodeProductValueFromBytes(raw, ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("DecodeProductValueFromBytes err = %v, want ErrRowLengthMismatch", err)
	}

	raw = []byte{TagArrayString, 2, 0, 0, 0}
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], 0)
	raw = append(raw, u32[:]...)
	_, err = DecodeProductValueFromBytes(raw, ts)
	if !errors.Is(err, ErrRowLengthMismatch) {
		t.Fatalf("DecodeProductValueFromBytes partial array err = %v, want ErrRowLengthMismatch", err)
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
