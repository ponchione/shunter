package protocol

import (
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/types"
)

// EncodeRowList encodes a batch of raw BSATN-encoded rows per SPEC-005
// §3.4: `[row_count: u32 LE] [for each row: [row_len: u32 LE] [row_data]]`.
// A nil or empty input encodes as 4 zero bytes.
func EncodeRowList(rows [][]byte) []byte {
	size := 4
	for _, r := range rows {
		size += 4 + len(r)
	}
	out := make([]byte, size)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(rows)))
	off := 4
	for _, r := range rows {
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(len(r)))
		off += 4
		copy(out[off:off+len(r)], r)
		off += len(r)
	}
	return out
}

// EncodeProductRows encodes schema-aligned ProductValue rows into the RowList
// payload carried by protocol messages. Row payloads are treated as read-only:
// bsatn.EncodeProductValue must not mutate shared ProductValue backing arrays.
func EncodeProductRows(rows []types.ProductValue) ([]byte, error) {
	size := 4
	rowSizes := make([]int, len(rows))
	for i, row := range rows {
		n := bsatn.EncodedProductValueSize(row)
		rowSizes[i] = n
		size += 4 + n
	}
	out := make([]byte, 4, size)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(rows)))
	var scratch [4]byte
	for i, row := range rows {
		binary.LittleEndian.PutUint32(scratch[:], uint32(rowSizes[i]))
		out = append(out, scratch[:]...)
		before := len(out)
		var err error
		out, err = bsatn.AppendProductValue(out, row)
		if err != nil {
			return nil, err
		}
		if got := len(out) - before; got != rowSizes[i] {
			return nil, fmt.Errorf("protocol: encoded row size changed from %d to %d", rowSizes[i], got)
		}
	}
	return out, nil
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
