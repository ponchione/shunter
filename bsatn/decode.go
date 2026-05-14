package bsatn

import (
	"bufio"
	"bytes"
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

func decodeColumnValue(r io.Reader, col schema.ColumnSchema) (types.Value, error) {
	var tag [1]byte
	if _, err := io.ReadFull(r, tag[:]); err != nil {
		return types.Value{}, err
	}
	if tag[0] != byte(col.Type) {
		return types.Value{}, &TypeTagMismatchError{Column: col.Name, Expected: col.Type, Got: tag[0]}
	}
	if !col.Nullable {
		return decodePayload(r, tag[0])
	}
	var presence [1]byte
	if _, err := io.ReadFull(r, presence[:]); err != nil {
		return types.Value{}, err
	}
	switch presence[0] {
	case 0:
		return types.NewNull(col.Type), nil
	case 1:
		return decodePayload(r, tag[0])
	default:
		return types.Value{}, ErrInvalidPresence
	}
}

func decodePayload(r io.Reader, tag byte) (types.Value, error) {
	var buf [8]byte
	switch tag {
	case TagBool:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return types.Value{}, err
		}
		return decodeBoolByte(buf[0])
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
		data, err := readLengthPrefixedPayload(r, n)
		if err != nil {
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
		data, err := readLengthPrefixedPayload(r, n)
		if err != nil {
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
	case TagDuration:
		if _, err := io.ReadFull(r, buf[:8]); err != nil {
			return types.Value{}, err
		}
		return types.NewDuration(int64(binary.LittleEndian.Uint64(buf[:8]))), nil
	case TagArrayString:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		count := binary.LittleEndian.Uint32(buf[:4])
		xs := make([]string, 0, initialArrayStringCap(count))
		for range count {
			if _, err := io.ReadFull(r, buf[:4]); err != nil {
				return types.Value{}, err
			}
			n := binary.LittleEndian.Uint32(buf[:4])
			data, err := readLengthPrefixedPayload(r, n)
			if err != nil {
				return types.Value{}, err
			}
			if !utf8.Valid(data) {
				return types.Value{}, ErrInvalidUTF8
			}
			xs = append(xs, string(data))
		}
		return types.NewArrayStringOwned(xs), nil
	case TagUUID:
		var u [16]byte
		if _, err := io.ReadFull(r, u[:]); err != nil {
			return types.Value{}, err
		}
		return types.NewUUID(u), nil
	case TagJSON:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return types.Value{}, err
		}
		n := binary.LittleEndian.Uint32(buf[:4])
		data, err := readLengthPrefixedPayload(r, n)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewJSON(data)
	default:
		return types.Value{}, &UnknownValueTagError{Tag: tag}
	}
}

func readLengthPrefixedPayload(r io.Reader, n uint32) ([]byte, error) {
	if n == 0 {
		return []byte{}, nil
	}
	const maxDirectPayloadRead = 64 << 20
	if n > maxDirectPayloadRead {
		lr := &io.LimitedReader{R: r, N: int64(n)}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(lr); err != nil {
			return nil, err
		}
		if lr.N != 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return buf.Bytes(), nil
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return data, nil
}

func initialArrayStringCap(count uint32) int {
	const maxInitialCap = 1024
	if count > maxInitialCap {
		return maxInitialCap
	}
	return int(count)
}

// DecodeProductValue reads a schema-validated row.
func DecodeProductValue(r io.Reader, ts *schema.TableSchema) (types.ProductValue, error) {
	if r == nil {
		return nil, errors.New("bsatn: reader is required")
	}
	if ts == nil {
		return nil, errors.New("bsatn: table schema is required")
	}
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	pv := make(types.ProductValue, len(ts.Columns))
	for i, col := range ts.Columns {
		v, err := decodeColumnValue(br, col)
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
	return DecodeProductValue(bytes.NewReader(data), ts)
}

func decodeBoolByte(b byte) (types.Value, error) {
	switch b {
	case 0:
		return types.NewBool(false), nil
	case 1:
		return types.NewBool(true), nil
	default:
		return types.Value{}, ErrInvalidBool
	}
}
