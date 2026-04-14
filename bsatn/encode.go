package bsatn

import (
	"encoding/binary"
	"io"
	"math"

	"github.com/ponchione/shunter/types"
)

// Tag constants for each ValueKind.
const (
	TagBool    byte = 0
	TagInt8    byte = 1
	TagUint8   byte = 2
	TagInt16   byte = 3
	TagUint16  byte = 4
	TagInt32   byte = 5
	TagUint32  byte = 6
	TagInt64   byte = 7
	TagUint64  byte = 8
	TagFloat32 byte = 9
	TagFloat64 byte = 10
	TagString  byte = 11
	TagBytes   byte = 12
)

// EncodeValue writes a Value in BSATN format: tag byte + LE payload.
func EncodeValue(w io.Writer, v types.Value) error {
	tag := byte(v.Kind())
	if _, err := w.Write([]byte{tag}); err != nil {
		return err
	}
	var buf [8]byte
	switch v.Kind() {
	case types.KindBool:
		if v.AsBool() {
			buf[0] = 1
		}
		_, err := w.Write(buf[:1])
		return err
	case types.KindInt8:
		buf[0] = byte(v.AsInt8())
		_, err := w.Write(buf[:1])
		return err
	case types.KindUint8:
		buf[0] = v.AsUint8()
		_, err := w.Write(buf[:1])
		return err
	case types.KindInt16:
		binary.LittleEndian.PutUint16(buf[:2], uint16(v.AsInt16()))
		_, err := w.Write(buf[:2])
		return err
	case types.KindUint16:
		binary.LittleEndian.PutUint16(buf[:2], v.AsUint16())
		_, err := w.Write(buf[:2])
		return err
	case types.KindInt32:
		binary.LittleEndian.PutUint32(buf[:4], uint32(v.AsInt32()))
		_, err := w.Write(buf[:4])
		return err
	case types.KindUint32:
		binary.LittleEndian.PutUint32(buf[:4], v.AsUint32())
		_, err := w.Write(buf[:4])
		return err
	case types.KindInt64:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsInt64()))
		_, err := w.Write(buf[:8])
		return err
	case types.KindUint64:
		binary.LittleEndian.PutUint64(buf[:8], v.AsUint64())
		_, err := w.Write(buf[:8])
		return err
	case types.KindFloat32:
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(v.AsFloat32()))
		_, err := w.Write(buf[:4])
		return err
	case types.KindFloat64:
		binary.LittleEndian.PutUint64(buf[:8], math.Float64bits(v.AsFloat64()))
		_, err := w.Write(buf[:8])
		return err
	case types.KindString:
		s := v.AsString()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(s)))
		if _, err := w.Write(buf[:4]); err != nil {
			return err
		}
		_, err := io.WriteString(w, s)
		return err
	case types.KindBytes:
		b := v.AsBytes()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(b)))
		if _, err := w.Write(buf[:4]); err != nil {
			return err
		}
		_, err := w.Write(b)
		return err
	default:
		return &UnknownValueTagError{Tag: tag}
	}
}

// EncodedValueSize returns the encoded size of a Value.
func EncodedValueSize(v types.Value) int {
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
		return 1 + 4 + len(v.AsBytes())
	default:
		return 0
	}
}

// EncodeProductValue writes all columns in order.
func EncodeProductValue(w io.Writer, pv types.ProductValue) error {
	for _, v := range pv {
		if err := EncodeValue(w, v); err != nil {
			return err
		}
	}
	return nil
}

// EncodedProductValueSize returns the total encoded size.
func EncodedProductValueSize(pv types.ProductValue) int {
	n := 0
	for _, v := range pv {
		n += EncodedValueSize(v)
	}
	return n
}
