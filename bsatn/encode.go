package bsatn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Tag constants for each ValueKind.
const (
	TagBool        byte = 0
	TagInt8        byte = 1
	TagUint8       byte = 2
	TagInt16       byte = 3
	TagUint16      byte = 4
	TagInt32       byte = 5
	TagUint32      byte = 6
	TagInt64       byte = 7
	TagUint64      byte = 8
	TagFloat32     byte = 9
	TagFloat64     byte = 10
	TagString      byte = 11
	TagBytes       byte = 12
	TagInt128      byte = 13
	TagUint128     byte = 14
	TagInt256      byte = 15
	TagUint256     byte = 16
	TagTimestamp   byte = 17
	TagArrayString byte = 18
	TagUUID        byte = 19
	TagDuration    byte = 20
	TagJSON        byte = 21
)

// AppendValue appends v in BSATN format to dst and returns the extended slice.
func AppendValue(dst []byte, v types.Value) ([]byte, error) {
	start := len(dst)
	if v.IsNull() {
		return dst, ErrNullWithoutSchema
	}
	tag := byte(v.Kind())
	dst = append(dst, tag)
	var buf [8]byte
	switch v.Kind() {
	case types.KindBool:
		if v.AsBool() {
			dst = append(dst, 1)
		} else {
			dst = append(dst, 0)
		}
	case types.KindInt8:
		dst = append(dst, byte(v.AsInt8()))
	case types.KindUint8:
		dst = append(dst, v.AsUint8())
	case types.KindInt16:
		binary.LittleEndian.PutUint16(buf[:2], uint16(v.AsInt16()))
		dst = append(dst, buf[:2]...)
	case types.KindUint16:
		binary.LittleEndian.PutUint16(buf[:2], v.AsUint16())
		dst = append(dst, buf[:2]...)
	case types.KindInt32:
		binary.LittleEndian.PutUint32(buf[:4], uint32(v.AsInt32()))
		dst = append(dst, buf[:4]...)
	case types.KindUint32:
		binary.LittleEndian.PutUint32(buf[:4], v.AsUint32())
		dst = append(dst, buf[:4]...)
	case types.KindInt64:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsInt64()))
		dst = append(dst, buf[:8]...)
	case types.KindUint64:
		binary.LittleEndian.PutUint64(buf[:8], v.AsUint64())
		dst = append(dst, buf[:8]...)
	case types.KindFloat32:
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(v.AsFloat32()))
		dst = append(dst, buf[:4]...)
	case types.KindFloat64:
		binary.LittleEndian.PutUint64(buf[:8], math.Float64bits(v.AsFloat64()))
		dst = append(dst, buf[:8]...)
	case types.KindString:
		s := v.AsString()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(s)))
		dst = append(dst, buf[:4]...)
		dst = append(dst, s...)
	case types.KindBytes:
		b := v.BytesView()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(b)))
		dst = append(dst, buf[:4]...)
		dst = append(dst, b...)
	case types.KindInt128:
		hi, lo := v.AsInt128()
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[0:8], lo)
		binary.LittleEndian.PutUint64(wide[8:16], uint64(hi))
		dst = append(dst, wide[:]...)
	case types.KindUint128:
		hi, lo := v.AsUint128()
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[0:8], lo)
		binary.LittleEndian.PutUint64(wide[8:16], hi)
		dst = append(dst, wide[:]...)
	case types.KindInt256:
		w0, w1, w2, w3 := v.AsInt256()
		var wide [32]byte
		binary.LittleEndian.PutUint64(wide[0:8], w3)
		binary.LittleEndian.PutUint64(wide[8:16], w2)
		binary.LittleEndian.PutUint64(wide[16:24], w1)
		binary.LittleEndian.PutUint64(wide[24:32], uint64(w0))
		dst = append(dst, wide[:]...)
	case types.KindUint256:
		w0, w1, w2, w3 := v.AsUint256()
		var wide [32]byte
		binary.LittleEndian.PutUint64(wide[0:8], w3)
		binary.LittleEndian.PutUint64(wide[8:16], w2)
		binary.LittleEndian.PutUint64(wide[16:24], w1)
		binary.LittleEndian.PutUint64(wide[24:32], w0)
		dst = append(dst, wide[:]...)
	case types.KindTimestamp:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsTimestamp()))
		dst = append(dst, buf[:8]...)
	case types.KindDuration:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsDurationMicros()))
		dst = append(dst, buf[:8]...)
	case types.KindArrayString:
		xs := v.ArrayStringView()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(xs)))
		dst = append(dst, buf[:4]...)
		for _, s := range xs {
			binary.LittleEndian.PutUint32(buf[:4], uint32(len(s)))
			dst = append(dst, buf[:4]...)
			dst = append(dst, s...)
		}
	case types.KindUUID:
		u := v.AsUUID()
		dst = append(dst, u[:]...)
	case types.KindJSON:
		b := v.JSONView()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(b)))
		dst = append(dst, buf[:4]...)
		dst = append(dst, b...)
	default:
		return dst[:start], &UnknownValueTagError{Tag: tag}
	}
	return dst, nil
}

// EncodeValue writes a Value in BSATN format: tag byte + LE payload.
func EncodeValue(w io.Writer, v types.Value) error {
	encoded, err := AppendValue(nil, v)
	if err != nil {
		return err
	}
	return writeAll(w, encoded)
}

func writeAll(w io.Writer, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	n, err := w.Write(p)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}

// EncodedValueSize returns the encoded size of a Value.
func EncodedValueSize(v types.Value) int {
	if v.IsNull() {
		return 0
	}
	switch v.Kind() {
	case types.KindBool, types.KindInt8, types.KindUint8:
		return 2 // tag + 1
	case types.KindInt16, types.KindUint16:
		return 3
	case types.KindInt32, types.KindUint32, types.KindFloat32:
		return 5
	case types.KindInt64, types.KindUint64, types.KindFloat64:
		return 9
	case types.KindString:
		return 1 + 4 + len(v.AsString())
	case types.KindBytes:
		return 1 + 4 + len(v.BytesView())
	case types.KindInt128, types.KindUint128:
		return 17
	case types.KindInt256, types.KindUint256:
		return 33
	case types.KindTimestamp, types.KindDuration:
		return 9
	case types.KindArrayString:
		xs := v.ArrayStringView()
		n := 1 + 4
		for _, s := range xs {
			n += 4 + len(s)
		}
		return n
	case types.KindUUID:
		return 17
	case types.KindJSON:
		return 1 + 4 + len(v.JSONView())
	default:
		return 0
	}
}

// EncodeProductValue writes all columns in order.
func EncodeProductValue(w io.Writer, pv types.ProductValue) error {
	encoded, err := AppendProductValue(nil, pv)
	if err != nil {
		return err
	}
	return writeAll(w, encoded)
}

// EncodeProductValueForSchema writes all columns using nullable metadata from ts.
func EncodeProductValueForSchema(w io.Writer, pv types.ProductValue, ts *schema.TableSchema) error {
	encoded, err := AppendProductValueForSchema(nil, pv, ts)
	if err != nil {
		return err
	}
	return writeAll(w, encoded)
}

// EncodeProductValueForColumns writes all columns using nullable metadata from columns.
func EncodeProductValueForColumns(w io.Writer, pv types.ProductValue, columns []schema.ColumnSchema) error {
	encoded, err := AppendProductValueForColumns(nil, pv, columns)
	if err != nil {
		return err
	}
	return writeAll(w, encoded)
}

// AppendProductValue appends all columns in order.
func AppendProductValue(dst []byte, pv types.ProductValue) ([]byte, error) {
	var err error
	for _, v := range pv {
		dst, err = AppendValue(dst, v)
		if err != nil {
			return dst, err
		}
	}
	return dst, nil
}

// AppendProductValueForSchema appends all columns using nullable metadata from ts.
func AppendProductValueForSchema(dst []byte, pv types.ProductValue, ts *schema.TableSchema) ([]byte, error) {
	if ts == nil {
		return AppendProductValue(dst, pv)
	}
	return AppendProductValueForColumns(dst, pv, ts.Columns)
}

// AppendProductValueForColumns appends all columns using nullable metadata from columns.
func AppendProductValueForColumns(dst []byte, pv types.ProductValue, columns []schema.ColumnSchema) ([]byte, error) {
	if len(pv) != len(columns) {
		return dst, errors.Join(
			ErrRowLengthMismatch,
			&RowShapeMismatchError{Expected: len(columns), Got: len(pv)},
		)
	}
	var err error
	for i, col := range columns {
		dst, err = appendColumnValue(dst, pv[i], col)
		if err != nil {
			return dst, err
		}
	}
	return dst, nil
}

func appendColumnValue(dst []byte, v types.Value, col schema.ColumnSchema) ([]byte, error) {
	if v.Kind() != col.Type {
		return dst, &TypeTagMismatchError{Column: col.Name, Expected: col.Type, Got: byte(v.Kind())}
	}
	if !col.Nullable {
		if v.IsNull() {
			return dst, fmt.Errorf("%w: column %q", ErrNullWithoutSchema, col.Name)
		}
		return AppendValue(dst, v)
	}
	dst = append(dst, byte(col.Type))
	if v.IsNull() {
		return append(dst, 0), nil
	}
	before := len(dst)
	dst, err := AppendValue(dst, v)
	if err != nil {
		return dst, err
	}
	// AppendValue emitted a tag at before. Replace that tag with the nullable
	// presence marker; the column tag is already present immediately before it.
	dst[before] = 1
	return dst, nil
}

// EncodedProductValueSize returns the total encoded size.
func EncodedProductValueSize(pv types.ProductValue) int {
	n := 0
	for _, v := range pv {
		n += EncodedValueSize(v)
	}
	return n
}

// EncodedProductValueSizeForSchema returns the encoded size using nullable metadata from ts.
func EncodedProductValueSizeForSchema(pv types.ProductValue, ts *schema.TableSchema) (int, error) {
	if ts == nil {
		return EncodedProductValueSize(pv), nil
	}
	return EncodedProductValueSizeForColumns(pv, ts.Columns)
}

// EncodedProductValueSizeForColumns returns the encoded size using nullable metadata from columns.
func EncodedProductValueSizeForColumns(pv types.ProductValue, columns []schema.ColumnSchema) (int, error) {
	if len(pv) != len(columns) {
		return 0, errors.Join(
			ErrRowLengthMismatch,
			&RowShapeMismatchError{Expected: len(columns), Got: len(pv)},
		)
	}
	n := 0
	for i, col := range columns {
		v := pv[i]
		if v.Kind() != col.Type {
			return 0, &TypeTagMismatchError{Column: col.Name, Expected: col.Type, Got: byte(v.Kind())}
		}
		if col.Nullable {
			if v.IsNull() {
				n += 2
			} else {
				n += EncodedValueSize(v) + 1
			}
			continue
		}
		if v.IsNull() {
			return 0, fmt.Errorf("%w: column %q", ErrNullWithoutSchema, col.Name)
		}
		n += EncodedValueSize(v)
	}
	return n, nil
}
