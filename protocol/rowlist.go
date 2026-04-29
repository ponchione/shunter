package protocol

import (
	"bytes"
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
	encoded := make([][]byte, len(rows))
	for i, row := range rows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, row); err != nil {
			return nil, err
		}
		encoded[i] = buf.Bytes()
	}
	return EncodeRowList(encoded), nil
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
	return rows, nil
}
