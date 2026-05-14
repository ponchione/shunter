package protocol

import (
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// EncodeRowList encodes a batch of raw BSATN-encoded rows per SPEC-005
// §3.4: `[row_count: u32 LE] [for each row: [row_len: u32 LE] [row_data]]`.
// A nil or empty input encodes as 4 zero bytes. Oversized input panics; use
// EncodeRowListChecked when the caller needs an error return.
func EncodeRowList(rows [][]byte) []byte {
	out, err := EncodeRowListChecked(rows)
	if err != nil {
		panic(err)
	}
	return out
}

// EncodeRowListChecked is EncodeRowList with explicit size errors.
func EncodeRowListChecked(rows [][]byte) ([]byte, error) {
	count, err := checkedProtocolLen("rowlist row count", len(rows))
	if err != nil {
		return nil, err
	}
	size := uint64(4)
	rowSizes := make([]uint32, len(rows))
	for i, r := range rows {
		rowLen, err := checkedProtocolLen("rowlist row", len(r))
		if err != nil {
			return nil, err
		}
		rowSizes[i] = rowLen
		size += 4 + uint64(len(r))
		if size > maxProtocolAlloc() {
			return nil, fmt.Errorf("%w: rowlist payload size %d exceeds max allocation", ErrMessageTooLarge, size)
		}
	}
	out := make([]byte, int(size))
	binary.LittleEndian.PutUint32(out[0:4], count)
	off := 4
	for i, r := range rows {
		binary.LittleEndian.PutUint32(out[off:off+4], rowSizes[i])
		off += 4
		copy(out[off:off+len(r)], r)
		off += len(r)
	}
	return out, nil
}

// EncodeProductRows encodes schema-aligned ProductValue rows into the RowList
// payload carried by protocol messages. Row payloads are treated as read-only:
// bsatn.EncodeProductValue must not mutate shared ProductValue backing arrays.
func EncodeProductRows(rows []types.ProductValue) ([]byte, error) {
	return encodeProductRowsWith(rows, encodedProductValueSize, bsatn.AppendProductValue)
}

// EncodeProductRowsForSchema encodes ProductValue rows using nullable metadata from ts.
func EncodeProductRowsForSchema(rows []types.ProductValue, ts *schema.TableSchema) ([]byte, error) {
	if ts == nil {
		return EncodeProductRows(rows)
	}
	return EncodeProductRowsForColumns(rows, ts.Columns)
}

// EncodeProductRowsForColumns encodes ProductValue rows using nullable metadata from columns.
func EncodeProductRowsForColumns(rows []types.ProductValue, columns []schema.ColumnSchema) ([]byte, error) {
	return encodeProductRowsWith(
		rows,
		func(row types.ProductValue) (int, error) {
			return bsatn.EncodedProductValueSizeForColumns(row, columns)
		},
		func(out []byte, row types.ProductValue) ([]byte, error) {
			return bsatn.AppendProductValueForColumns(out, row, columns)
		},
	)
}

type productRowSizer func(types.ProductValue) (int, error)

type productRowAppender func([]byte, types.ProductValue) ([]byte, error)

func encodedProductValueSize(row types.ProductValue) (int, error) {
	return bsatn.EncodedProductValueSize(row), nil
}

func encodeProductRowsWith(rows []types.ProductValue, rowSize productRowSizer, appendRow productRowAppender) ([]byte, error) {
	count, err := checkedProtocolLen("product row count", len(rows))
	if err != nil {
		return nil, err
	}
	size := uint64(4)
	rowSizes := make([]uint32, len(rows))
	for i, row := range rows {
		n, err := rowSize(row)
		if err != nil {
			return nil, err
		}
		rowLen, err := checkedProtocolLen("product row", n)
		if err != nil {
			return nil, err
		}
		rowSizes[i] = rowLen
		size += 4 + uint64(n)
		if size > maxProtocolAlloc() {
			return nil, fmt.Errorf("%w: encoded product rows size %d exceeds max allocation", ErrMessageTooLarge, size)
		}
	}
	out := make([]byte, 4, int(size))
	binary.LittleEndian.PutUint32(out[0:4], count)
	var scratch [4]byte
	for i, row := range rows {
		binary.LittleEndian.PutUint32(scratch[:], rowSizes[i])
		out = append(out, scratch[:]...)
		before := len(out)
		var err error
		out, err = appendRow(out, row)
		if err != nil {
			return nil, err
		}
		if got := len(out) - before; got != int(rowSizes[i]) {
			return nil, fmt.Errorf("protocol: encoded row size changed from %d to %d", rowSizes[i], got)
		}
	}
	return out, nil
}

func maxProtocolAlloc() uint64 {
	return uint64(int(^uint(0) >> 1))
}

// DecodeRowList parses the wire format emitted by EncodeRowList.
// Truncated headers, partial row-length prefixes, or payloads that
// exceed the remaining buffer all map to ErrMalformedMessage.
func DecodeRowList(data []byte) ([][]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: rowlist truncated at count (have %d, need 4)", ErrMalformedMessage, len(data))
	}
	count := binary.LittleEndian.Uint32(data[0:4])
	off := 4
	if err := requireCountFitsRemaining("rowlist rows", count, data, off, 4); err != nil {
		return nil, err
	}
	rows := make([][]byte, 0, count)
	for i := uint32(0); i < count; i++ {
		if len(data)-off < 4 {
			return nil, fmt.Errorf("%w: row %d length prefix truncated", ErrMalformedMessage, i)
		}
		rowLen := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		if uint64(rowLen) > uint64(len(data)-off) {
			return nil, fmt.Errorf("%w: row %d length %d exceeds remaining %d", ErrMalformedMessage, i, rowLen, len(data)-off)
		}
		row := make([]byte, rowLen)
		copy(row, data[off:off+int(rowLen)])
		off += int(rowLen)
		rows = append(rows, row)
	}
	if off != len(data) {
		return nil, fmt.Errorf("%w: rowlist trailing bytes at offset %d", ErrMalformedMessage, off)
	}
	return rows, nil
}
