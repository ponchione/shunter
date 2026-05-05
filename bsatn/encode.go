package bsatn

import (
	"encoding/binary"
	"io"
	"math"

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
	tag := byte(v.Kind())
	if err := writeAll(w, []byte{tag}); err != nil {
		return err
	}
	var buf [8]byte
	switch v.Kind() {
	case types.KindBool:
		if v.AsBool() {
			buf[0] = 1
		}
		return writeAll(w, buf[:1])
	case types.KindInt8:
		buf[0] = byte(v.AsInt8())
		return writeAll(w, buf[:1])
	case types.KindUint8:
		buf[0] = v.AsUint8()
		return writeAll(w, buf[:1])
	case types.KindInt16:
		binary.LittleEndian.PutUint16(buf[:2], uint16(v.AsInt16()))
		return writeAll(w, buf[:2])
	case types.KindUint16:
		binary.LittleEndian.PutUint16(buf[:2], v.AsUint16())
		return writeAll(w, buf[:2])
	case types.KindInt32:
		binary.LittleEndian.PutUint32(buf[:4], uint32(v.AsInt32()))
		return writeAll(w, buf[:4])
	case types.KindUint32:
		binary.LittleEndian.PutUint32(buf[:4], v.AsUint32())
		return writeAll(w, buf[:4])
	case types.KindInt64:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsInt64()))
		return writeAll(w, buf[:8])
	case types.KindUint64:
		binary.LittleEndian.PutUint64(buf[:8], v.AsUint64())
		return writeAll(w, buf[:8])
	case types.KindFloat32:
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(v.AsFloat32()))
		return writeAll(w, buf[:4])
	case types.KindFloat64:
		binary.LittleEndian.PutUint64(buf[:8], math.Float64bits(v.AsFloat64()))
		return writeAll(w, buf[:8])
	case types.KindString:
		s := v.AsString()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(s)))
		if err := writeAll(w, buf[:4]); err != nil {
			return err
		}
		return writeStringAll(w, s)
	case types.KindBytes:
		b := v.BytesView()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(b)))
		if err := writeAll(w, buf[:4]); err != nil {
			return err
		}
		return writeAll(w, b)
	case types.KindInt128:
		hi, lo := v.AsInt128()
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[0:8], lo)
		binary.LittleEndian.PutUint64(wide[8:16], uint64(hi))
		return writeAll(w, wide[:])
	case types.KindUint128:
		hi, lo := v.AsUint128()
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[0:8], lo)
		binary.LittleEndian.PutUint64(wide[8:16], hi)
		return writeAll(w, wide[:])
	case types.KindInt256:
		w0, w1, w2, w3 := v.AsInt256()
		var wide [32]byte
		binary.LittleEndian.PutUint64(wide[0:8], w3)
		binary.LittleEndian.PutUint64(wide[8:16], w2)
		binary.LittleEndian.PutUint64(wide[16:24], w1)
		binary.LittleEndian.PutUint64(wide[24:32], uint64(w0))
		return writeAll(w, wide[:])
	case types.KindUint256:
		w0, w1, w2, w3 := v.AsUint256()
		var wide [32]byte
		binary.LittleEndian.PutUint64(wide[0:8], w3)
		binary.LittleEndian.PutUint64(wide[8:16], w2)
		binary.LittleEndian.PutUint64(wide[16:24], w1)
		binary.LittleEndian.PutUint64(wide[24:32], w0)
		return writeAll(w, wide[:])
	case types.KindTimestamp:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsTimestamp()))
		return writeAll(w, buf[:8])
	case types.KindDuration:
		binary.LittleEndian.PutUint64(buf[:8], uint64(v.AsDurationMicros()))
		return writeAll(w, buf[:8])
	case types.KindArrayString:
		xs := v.ArrayStringView()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(xs)))
		if err := writeAll(w, buf[:4]); err != nil {
			return err
		}
		for _, s := range xs {
			binary.LittleEndian.PutUint32(buf[:4], uint32(len(s)))
			if err := writeAll(w, buf[:4]); err != nil {
				return err
			}
			if err := writeStringAll(w, s); err != nil {
				return err
			}
		}
		return nil
	case types.KindUUID:
		u := v.AsUUID()
		return writeAll(w, u[:])
	case types.KindJSON:
		b := v.JSONView()
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(b)))
		if err := writeAll(w, buf[:4]); err != nil {
			return err
		}
		return writeAll(w, b)
	default:
		return &UnknownValueTagError{Tag: tag}
	}
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

func writeStringAll(w io.Writer, s string) error {
	if len(s) == 0 {
		return nil
	}
	n, err := io.WriteString(w, s)
	if err != nil {
		return err
	}
	if n != len(s) {
		return io.ErrShortWrite
	}
	return nil
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
	for _, v := range pv {
		if err := EncodeValue(w, v); err != nil {
			return err
		}
	}
	return nil
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

// EncodedProductValueSize returns the total encoded size.
func EncodedProductValueSize(pv types.ProductValue) int {
	n := 0
	for _, v := range pv {
		n += EncodedValueSize(v)
	}
	return n
}
