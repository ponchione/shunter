package bsatn

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"unicode/utf8"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// DecodeValue reads a BSATN-encoded Value.
func DecodeValue(r io.Reader) (types.Value, error) {
	var tag [1]byte
	if _, err := io.ReadFull(r, tag[:]); err != nil {
		return types.Value{}, err
	}
	return decodePayload(r, tag[0])
}

// DecodeValueExpecting reads a Value and validates the tag matches expected.
func DecodeValueExpecting(r io.Reader, expected types.ValueKind, colName string) (types.Value, error) {
	var tag [1]byte
	if _, err := io.ReadFull(r, tag[:]); err != nil {
		return types.Value{}, err
	}
	if tag[0] != byte(expected) {
		return types.Value{}, &TypeTagMismatchError{Column: colName, Expected: expected, Got: tag[0]}
	}
	return decodePayload(r, tag[0])
}

func decodePayload(r io.Reader, tag byte) (types.Value, error) {
	var buf [8]byte
	switch tag {
	case TagBool:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return types.Value{}, err
		}
		return types.NewBool(buf[0] != 0), nil
	case TagInt8:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return types.Value{}, err
		}
		return types.NewInt8(int8(buf[0])), nil
	case TagUint8:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return types.Value{}, err
		}
		return types.NewUint8(buf[0]), nil
	case TagInt16:
		if _, err := io.ReadFull(r, buf[:2]); err != nil {
			return types.Value{}, err
		}
		return types.NewInt16(int16(binary.LittleEndian.Uint16(buf[:2]))), nil
	case TagUint16:
		if _, err := io.ReadFull(r, buf[:2]); err != nil {
			return types.Value{}, err
		}
		return types.NewUint16(binary.LittleEndian.Uint16(buf[:2])), nil
	case TagInt32:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		return types.NewInt32(int32(binary.LittleEndian.Uint32(buf[:4]))), nil
	case TagUint32:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		return types.NewUint32(binary.LittleEndian.Uint32(buf[:4])), nil
	case TagInt64:
		if _, err := io.ReadFull(r, buf[:8]); err != nil {
			return types.Value{}, err
		}
		return types.NewInt64(int64(binary.LittleEndian.Uint64(buf[:8]))), nil
	case TagUint64:
		if _, err := io.ReadFull(r, buf[:8]); err != nil {
			return types.Value{}, err
		}
		return types.NewUint64(binary.LittleEndian.Uint64(buf[:8])), nil
	case TagFloat32:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		return types.NewFloat32(math.Float32frombits(binary.LittleEndian.Uint32(buf[:4])))
	case TagFloat64:
		if _, err := io.ReadFull(r, buf[:8]); err != nil {
			return types.Value{}, err
		}
		return types.NewFloat64(math.Float64frombits(binary.LittleEndian.Uint64(buf[:8])))
	case TagString:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		n := binary.LittleEndian.Uint32(buf[:4])
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return types.Value{}, err
		}
		if !utf8.Valid(data) {
			return types.Value{}, ErrInvalidUTF8
		}
		return types.NewString(string(data)), nil
	case TagBytes:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		n := binary.LittleEndian.Uint32(buf[:4])
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return types.Value{}, err
		}
		return types.NewBytesOwned(data), nil
	case TagInt128:
		var wide [16]byte
		if _, err := io.ReadFull(r, wide[:]); err != nil {
			return types.Value{}, err
		}
		lo := binary.LittleEndian.Uint64(wide[0:8])
		hi := binary.LittleEndian.Uint64(wide[8:16])
		return types.NewInt128(int64(hi), lo), nil
	case TagUint128:
		var wide [16]byte
		if _, err := io.ReadFull(r, wide[:]); err != nil {
			return types.Value{}, err
		}
		lo := binary.LittleEndian.Uint64(wide[0:8])
		hi := binary.LittleEndian.Uint64(wide[8:16])
		return types.NewUint128(hi, lo), nil
	case TagInt256:
		var wide [32]byte
		if _, err := io.ReadFull(r, wide[:]); err != nil {
			return types.Value{}, err
		}
		w3 := binary.LittleEndian.Uint64(wide[0:8])
		w2 := binary.LittleEndian.Uint64(wide[8:16])
		w1 := binary.LittleEndian.Uint64(wide[16:24])
		w0 := binary.LittleEndian.Uint64(wide[24:32])
		return types.NewInt256(int64(w0), w1, w2, w3), nil
	case TagUint256:
		var wide [32]byte
		if _, err := io.ReadFull(r, wide[:]); err != nil {
			return types.Value{}, err
		}
		w3 := binary.LittleEndian.Uint64(wide[0:8])
		w2 := binary.LittleEndian.Uint64(wide[8:16])
		w1 := binary.LittleEndian.Uint64(wide[16:24])
		w0 := binary.LittleEndian.Uint64(wide[24:32])
		return types.NewUint256(w0, w1, w2, w3), nil
	case TagTimestamp:
		if _, err := io.ReadFull(r, buf[:8]); err != nil {
			return types.Value{}, err
		}
		return types.NewTimestamp(int64(binary.LittleEndian.Uint64(buf[:8]))), nil
	case TagArrayString:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		count := binary.LittleEndian.Uint32(buf[:4])
		xs := make([]string, count)
		for i := range count {
			if _, err := io.ReadFull(r, buf[:4]); err != nil {
				return types.Value{}, err
			}
			n := binary.LittleEndian.Uint32(buf[:4])
			data := make([]byte, n)
			if _, err := io.ReadFull(r, data); err != nil {
				return types.Value{}, err
			}
			if !utf8.Valid(data) {
				return types.Value{}, ErrInvalidUTF8
			}
			xs[i] = string(data)
		}
		return types.NewArrayStringOwned(xs), nil
	default:
		return types.Value{}, &UnknownValueTagError{Tag: tag}
	}
}

// DecodeProductValue reads a schema-validated row.
func DecodeProductValue(r io.Reader, ts *schema.TableSchema) (types.ProductValue, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	pv := make(types.ProductValue, len(ts.Columns))
	for i, col := range ts.Columns {
		v, err := DecodeValueExpecting(br, col.Type, col.Name)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, errors.Join(
					ErrRowLengthMismatch,
					&RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: i},
				)
			}
			return nil, err
		}
		pv[i] = v
	}

	if _, err := br.Peek(1); err == nil {
		return nil, errors.Join(
			ErrRowLengthMismatch,
			&RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: len(ts.Columns) + 1},
		)
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}

	return pv, nil
}

// DecodeProductValueFromBytes decodes and rejects trailing bytes.
func DecodeProductValueFromBytes(data []byte, ts *schema.TableSchema) (types.ProductValue, error) {
	d := byteDecoder{data: data}
	pv := make(types.ProductValue, len(ts.Columns))
	for i, col := range ts.Columns {
		v, err := d.decodeValueExpecting(col.Type, col.Name)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				shapeErr := &RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: i}
				return nil, errors.Join(ErrRowLengthMismatch, shapeErr)
			}
			return nil, err
		}
		pv[i] = v
	}
	if d.pos < len(d.data) {
		return nil, errors.Join(
			ErrRowLengthMismatch,
			&RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: len(ts.Columns) + 1},
		)
	}
	return pv, nil
}

type byteDecoder struct {
	data []byte
	pos  int
}

func (d *byteDecoder) read(n int) ([]byte, error) {
	if n < 0 || len(d.data)-d.pos < n {
		if d.pos >= len(d.data) {
			return nil, io.EOF
		}
		d.pos = len(d.data)
		return nil, io.ErrUnexpectedEOF
	}
	out := d.data[d.pos : d.pos+n]
	d.pos += n
	return out, nil
}

func (d *byteDecoder) readU32() (uint32, error) {
	data, err := d.read(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data), nil
}

func (d *byteDecoder) decodeValueExpecting(expected types.ValueKind, colName string) (types.Value, error) {
	tag, err := d.read(1)
	if err != nil {
		return types.Value{}, err
	}
	if tag[0] != byte(expected) {
		return types.Value{}, &TypeTagMismatchError{Column: colName, Expected: expected, Got: tag[0]}
	}
	return d.decodePayload(tag[0])
}

func (d *byteDecoder) decodePayload(tag byte) (types.Value, error) {
	switch tag {
	case TagBool:
		data, err := d.read(1)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewBool(data[0] != 0), nil
	case TagInt8:
		data, err := d.read(1)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewInt8(int8(data[0])), nil
	case TagUint8:
		data, err := d.read(1)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewUint8(data[0]), nil
	case TagInt16:
		data, err := d.read(2)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewInt16(int16(binary.LittleEndian.Uint16(data))), nil
	case TagUint16:
		data, err := d.read(2)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewUint16(binary.LittleEndian.Uint16(data)), nil
	case TagInt32:
		data, err := d.read(4)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewInt32(int32(binary.LittleEndian.Uint32(data))), nil
	case TagUint32:
		data, err := d.read(4)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewUint32(binary.LittleEndian.Uint32(data)), nil
	case TagInt64:
		data, err := d.read(8)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewInt64(int64(binary.LittleEndian.Uint64(data))), nil
	case TagUint64:
		data, err := d.read(8)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewUint64(binary.LittleEndian.Uint64(data)), nil
	case TagFloat32:
		data, err := d.read(4)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewFloat32(math.Float32frombits(binary.LittleEndian.Uint32(data)))
	case TagFloat64:
		data, err := d.read(8)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewFloat64(math.Float64frombits(binary.LittleEndian.Uint64(data)))
	case TagString:
		n, err := d.readU32()
		if err != nil {
			return types.Value{}, err
		}
		data, err := d.read(int(n))
		if err != nil {
			return types.Value{}, err
		}
		if !utf8.Valid(data) {
			return types.Value{}, ErrInvalidUTF8
		}
		return types.NewString(string(data)), nil
	case TagBytes:
		n, err := d.readU32()
		if err != nil {
			return types.Value{}, err
		}
		data, err := d.read(int(n))
		if err != nil {
			return types.Value{}, err
		}
		owned := make([]byte, len(data))
		copy(owned, data)
		return types.NewBytesOwned(owned), nil
	case TagInt128:
		data, err := d.read(16)
		if err != nil {
			return types.Value{}, err
		}
		lo := binary.LittleEndian.Uint64(data[0:8])
		hi := binary.LittleEndian.Uint64(data[8:16])
		return types.NewInt128(int64(hi), lo), nil
	case TagUint128:
		data, err := d.read(16)
		if err != nil {
			return types.Value{}, err
		}
		lo := binary.LittleEndian.Uint64(data[0:8])
		hi := binary.LittleEndian.Uint64(data[8:16])
		return types.NewUint128(hi, lo), nil
	case TagInt256:
		data, err := d.read(32)
		if err != nil {
			return types.Value{}, err
		}
		w3 := binary.LittleEndian.Uint64(data[0:8])
		w2 := binary.LittleEndian.Uint64(data[8:16])
		w1 := binary.LittleEndian.Uint64(data[16:24])
		w0 := binary.LittleEndian.Uint64(data[24:32])
		return types.NewInt256(int64(w0), w1, w2, w3), nil
	case TagUint256:
		data, err := d.read(32)
		if err != nil {
			return types.Value{}, err
		}
		w3 := binary.LittleEndian.Uint64(data[0:8])
		w2 := binary.LittleEndian.Uint64(data[8:16])
		w1 := binary.LittleEndian.Uint64(data[16:24])
		w0 := binary.LittleEndian.Uint64(data[24:32])
		return types.NewUint256(w0, w1, w2, w3), nil
	case TagTimestamp:
		data, err := d.read(8)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewTimestamp(int64(binary.LittleEndian.Uint64(data))), nil
	case TagArrayString:
		count, err := d.readU32()
		if err != nil {
			return types.Value{}, err
		}
		xs := make([]string, count)
		for i := range count {
			n, err := d.readU32()
			if err != nil {
				return types.Value{}, err
			}
			data, err := d.read(int(n))
			if err != nil {
				return types.Value{}, err
			}
			if !utf8.Valid(data) {
				return types.Value{}, ErrInvalidUTF8
			}
			xs[i] = string(data)
		}
		return types.NewArrayStringOwned(xs), nil
	default:
		return types.Value{}, &UnknownValueTagError{Tag: tag}
	}
}
