package bsatn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
	"pgregory.net/rapid"
)

var rapidValueKinds = []types.ValueKind{
	types.KindBool,
	types.KindInt8,
	types.KindUint8,
	types.KindInt16,
	types.KindUint16,
	types.KindInt32,
	types.KindUint32,
	types.KindInt64,
	types.KindUint64,
	types.KindFloat32,
	types.KindFloat64,
	types.KindString,
	types.KindBytes,
	types.KindInt128,
	types.KindUint128,
	types.KindInt256,
	types.KindUint256,
	types.KindTimestamp,
	types.KindArrayString,
}

func rapidValue() *rapid.Generator[types.Value] {
	return rapid.Custom(func(t *rapid.T) types.Value {
		kind := rapid.SampledFrom(rapidValueKinds).Draw(t, "kind")
		return rapidValueOfKind(kind).Draw(t, "value")
	})
}

func rapidValueOfKind(kind types.ValueKind) *rapid.Generator[types.Value] {
	switch kind {
	case types.KindBool:
		return rapid.Map(rapid.Bool(), types.NewBool)
	case types.KindInt8:
		return rapid.Map(rapid.Int8(), types.NewInt8)
	case types.KindUint8:
		return rapid.Map(rapid.Uint8(), types.NewUint8)
	case types.KindInt16:
		return rapid.Map(rapid.Int16(), types.NewInt16)
	case types.KindUint16:
		return rapid.Map(rapid.Uint16(), types.NewUint16)
	case types.KindInt32:
		return rapid.Map(rapid.Int32(), types.NewInt32)
	case types.KindUint32:
		return rapid.Map(rapid.Uint32(), types.NewUint32)
	case types.KindInt64:
		return rapid.Map(rapid.Int64(), types.NewInt64)
	case types.KindUint64:
		return rapid.Map(rapid.Uint64(), types.NewUint64)
	case types.KindFloat32:
		return rapid.Map(rapid.Float32Range(-math.MaxFloat32, math.MaxFloat32), func(f float32) types.Value {
			v, err := types.NewFloat32(f)
			if err != nil {
				panic(err)
			}
			return v
		})
	case types.KindFloat64:
		return rapid.Map(rapid.Float64Range(-math.MaxFloat64, math.MaxFloat64), func(f float64) types.Value {
			v, err := types.NewFloat64(f)
			if err != nil {
				panic(err)
			}
			return v
		})
	case types.KindString:
		return rapid.Map(rapid.StringN(0, 32, 64), types.NewString)
	case types.KindBytes:
		return rapid.Map(rapid.SliceOfN(rapid.Byte(), 0, 64), types.NewBytes)
	case types.KindInt128:
		return rapid.Custom(func(t *rapid.T) types.Value {
			return types.NewInt128(
				rapid.Int64().Draw(t, "hi"),
				rapid.Uint64().Draw(t, "lo"),
			)
		})
	case types.KindUint128:
		return rapid.Custom(func(t *rapid.T) types.Value {
			return types.NewUint128(
				rapid.Uint64().Draw(t, "hi"),
				rapid.Uint64().Draw(t, "lo"),
			)
		})
	case types.KindInt256:
		return rapid.Custom(func(t *rapid.T) types.Value {
			return types.NewInt256(
				rapid.Int64().Draw(t, "w0"),
				rapid.Uint64().Draw(t, "w1"),
				rapid.Uint64().Draw(t, "w2"),
				rapid.Uint64().Draw(t, "w3"),
			)
		})
	case types.KindUint256:
		return rapid.Custom(func(t *rapid.T) types.Value {
			return types.NewUint256(
				rapid.Uint64().Draw(t, "w0"),
				rapid.Uint64().Draw(t, "w1"),
				rapid.Uint64().Draw(t, "w2"),
				rapid.Uint64().Draw(t, "w3"),
			)
		})
	case types.KindTimestamp:
		return rapid.Map(rapid.Int64(), types.NewTimestamp)
	case types.KindArrayString:
		return rapid.Map(rapid.SliceOfN(rapid.StringN(0, 16, 32), 0, 8), types.NewArrayString)
	default:
		panic("unsupported rapid value kind")
	}
}

func rapidTableSchema(kinds []types.ValueKind) *schema.TableSchema {
	columns := make([]schema.ColumnSchema, len(kinds))
	for i, kind := range kinds {
		columns[i] = schema.ColumnSchema{
			Index: i,
			Name:  "c" + string(rune('0'+i)),
			Type:  kind,
		}
	}
	return &schema.TableSchema{
		ID:      0,
		Name:    "rapid_rows",
		Columns: columns,
	}
}

func rapidProductValueForSchema(ts *schema.TableSchema) *rapid.Generator[types.ProductValue] {
	if len(ts.Columns) == 0 {
		return rapid.Just(types.ProductValue{})
	}
	return rapid.Custom(func(t *rapid.T) types.ProductValue {
		row := make(types.ProductValue, len(ts.Columns))
		for i, col := range ts.Columns {
			row[i] = rapidValueOfKind(col.Type).Draw(t, col.Name)
		}
		return row
	})
}

func TestRapidValueRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := rapidValue().Draw(t, "v")

		var buf bytes.Buffer
		if err := EncodeValue(&buf, v); err != nil {
			t.Fatalf("EncodeValue(%s): %v", v.Kind(), err)
		}
		if gotSize := EncodedValueSize(v); gotSize != buf.Len() {
			t.Fatalf("EncodedValueSize(%s) = %d, encoded length = %d", v.Kind(), gotSize, buf.Len())
		}

		got, err := DecodeValue(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("DecodeValue(%s): %v", v.Kind(), err)
		}
		if !v.Equal(got) {
			t.Fatalf("round trip mismatch for %s", v.Kind())
		}

		var encodedAgain bytes.Buffer
		if err := EncodeValue(&encodedAgain, got); err != nil {
			t.Fatalf("re-encode %s: %v", got.Kind(), err)
		}
		if !bytes.Equal(buf.Bytes(), encodedAgain.Bytes()) {
			t.Fatalf("encoding is not deterministic for %s: first=%x second=%x", v.Kind(), buf.Bytes(), encodedAgain.Bytes())
		}
	})
}

func TestRapidProductValueRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kinds := rapid.SliceOfN(rapid.SampledFrom(rapidValueKinds), 0, 12).Draw(t, "kinds")
		ts := rapidTableSchema(kinds)
		row := rapidProductValueForSchema(ts).Draw(t, "row")

		var buf bytes.Buffer
		if err := EncodeProductValue(&buf, row); err != nil {
			t.Fatalf("EncodeProductValue: %v", err)
		}
		if gotSize := EncodedProductValueSize(row); gotSize != buf.Len() {
			t.Fatalf("EncodedProductValueSize = %d, encoded length = %d", gotSize, buf.Len())
		}

		got, err := DecodeProductValueFromBytes(buf.Bytes(), ts)
		if err != nil {
			t.Fatalf("DecodeProductValueFromBytes: %v", err)
		}
		if !row.Equal(got) {
			t.Fatalf("product value round trip mismatch")
		}
	})
}

func TestRapidProductValueRejectsTrailingBytes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kinds := rapid.SliceOfN(rapid.SampledFrom(rapidValueKinds), 0, 12).Draw(t, "kinds")
		ts := rapidTableSchema(kinds)
		row := rapidProductValueForSchema(ts).Draw(t, "row")

		var buf bytes.Buffer
		if err := EncodeProductValue(&buf, row); err != nil {
			t.Fatalf("EncodeProductValue: %v", err)
		}
		buf.WriteByte(rapid.Byte().Draw(t, "trailing"))

		_, err := DecodeProductValueFromBytes(buf.Bytes(), ts)
		if !errors.Is(err, ErrRowLengthMismatch) {
			t.Fatalf("DecodeProductValueFromBytes trailing err = %v, want ErrRowLengthMismatch", err)
		}
	})
}

func TestRapidDecodeValueRejectsInvalidUTF8(t *testing.T) {
	invalidUTF8 := [][]byte{
		{0xff},
		{0xc0, 0xaf},
		{0xe2, 0x28, 0xa1},
		{0xf0, 0x28, 0x8c, 0x28},
	}

	rapid.Check(t, func(t *rapid.T) {
		payload := rapid.SampledFrom(invalidUTF8).Draw(t, "payload")

		rawString := make([]byte, 1+4+len(payload))
		rawString[0] = TagString
		binary.LittleEndian.PutUint32(rawString[1:5], uint32(len(payload)))
		copy(rawString[5:], payload)
		if _, err := DecodeValue(bytes.NewReader(rawString)); !errors.Is(err, ErrInvalidUTF8) {
			t.Fatalf("DecodeValue invalid string err = %v, want ErrInvalidUTF8", err)
		}

		rawArrayString := make([]byte, 1+4+4+len(payload))
		rawArrayString[0] = TagArrayString
		binary.LittleEndian.PutUint32(rawArrayString[1:5], 1)
		binary.LittleEndian.PutUint32(rawArrayString[5:9], uint32(len(payload)))
		copy(rawArrayString[9:], payload)
		if _, err := DecodeValue(bytes.NewReader(rawArrayString)); !errors.Is(err, ErrInvalidUTF8) {
			t.Fatalf("DecodeValue invalid array string err = %v, want ErrInvalidUTF8", err)
		}
	})
}
