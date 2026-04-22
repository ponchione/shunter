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
		return types.NewBytes(data), nil
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
				return nil, &RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: i}
			}
			return nil, err
		}
		pv[i] = v
	}

	if _, err := br.Peek(1); err == nil {
		return nil, &RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: len(ts.Columns) + 1}
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}

	return pv, nil
}

// DecodeProductValueFromBytes decodes and rejects trailing bytes.
func DecodeProductValueFromBytes(data []byte, ts *schema.TableSchema) (types.ProductValue, error) {
	r := &countingReader{data: data}
	pv, err := DecodeProductValue(r, ts)
	if err != nil {
		var shapeErr *RowShapeMismatchError
		if errors.As(err, &shapeErr) {
			return nil, errors.Join(ErrRowLengthMismatch, shapeErr)
		}
		return nil, err
	}
	if r.pos < len(r.data) {
		return nil, errors.Join(
			ErrRowLengthMismatch,
			&RowShapeMismatchError{TableName: ts.Name, Expected: len(ts.Columns), Got: len(ts.Columns) + 1},
		)
	}
	return pv, nil
}

type countingReader struct {
	data []byte
	pos  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
