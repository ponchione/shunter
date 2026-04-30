package commitlog

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

const changesetVersion byte = 1

// EncodeChangeset serializes a Changeset to bytes.
func EncodeChangeset(cs *store.Changeset) ([]byte, error) {
	// Sort table IDs for deterministic output.
	tableIDs := make([]schema.TableID, 0, len(cs.Tables))
	for id := range cs.Tables {
		tableIDs = append(tableIDs, id)
	}
	slices.Sort(tableIDs)

	size := 1 + 4
	for _, id := range tableIDs {
		tc := cs.Tables[id]
		size += 4
		size += encodedChangesetRowsSize(tc.Inserts)
		size += encodedChangesetRowsSize(tc.Deletes)
	}
	out := make([]byte, 0, size)
	out = append(out, changesetVersion)
	out = appendUint32LE(out, uint32(len(tableIDs)))

	for _, id := range tableIDs {
		tc := cs.Tables[id]
		out = appendUint32LE(out, uint32(id))

		// Inserts.
		var err error
		out, err = appendChangesetRows(out, tc.Inserts)
		if err != nil {
			return nil, err
		}

		// Deletes.
		out, err = appendChangesetRows(out, tc.Deletes)
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

func encodedChangesetRowsSize(rows []types.ProductValue) int {
	size := 4
	for _, row := range rows {
		size += 4 + bsatn.EncodedProductValueSize(row)
	}
	return size
}

func appendChangesetRows(out []byte, rows []types.ProductValue) ([]byte, error) {
	out = appendUint32LE(out, uint32(len(rows)))
	for _, row := range rows {
		rowLen := bsatn.EncodedProductValueSize(row)
		out = appendUint32LE(out, uint32(rowLen))
		before := len(out)
		var err error
		out, err = bsatn.AppendProductValue(out, row)
		if err != nil {
			return out, err
		}
		if got := len(out) - before; got != rowLen {
			return out, fmt.Errorf("commitlog: encoded row size changed from %d to %d", rowLen, got)
		}
	}
	return out, nil
}

func appendUint32LE(out []byte, v uint32) []byte {
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], v)
	return append(out, scratch[:]...)
}

// DecodeChangeset deserializes a Changeset from bytes using the default row-size limit.
func DecodeChangeset(data []byte, reg schema.SchemaRegistry) (*store.Changeset, error) {
	return decodeChangesetWithMax(data, reg, DefaultCommitLogOptions().MaxRowBytes)
}

func decodeChangesetWithMax(data []byte, reg schema.SchemaRegistry, maxRowBytes uint32) (*store.Changeset, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("commitlog: changeset too short")
	}
	if data[0] != changesetVersion {
		return nil, fmt.Errorf("commitlog: unsupported changeset version %d", data[0])
	}

	pos := 1
	tableCount := binary.LittleEndian.Uint32(data[pos:])
	pos += 4

	cs := &store.Changeset{
		Tables: make(map[schema.TableID]*store.TableChangeset),
	}

	for range tableCount {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("commitlog: truncated changeset")
		}
		tableID := schema.TableID(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4

		if _, exists := cs.Tables[tableID]; exists {
			return nil, fmt.Errorf("commitlog: duplicate table ID %d", tableID)
		}
		ts, ok := reg.Table(tableID)
		if !ok {
			return nil, fmt.Errorf("commitlog: unknown table ID %d", tableID)
		}

		tc := &store.TableChangeset{TableID: tableID, TableName: ts.Name}

		// Inserts.
		if pos+4 > len(data) {
			return nil, fmt.Errorf("commitlog: truncated changeset")
		}
		insertCount := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		for range insertCount {
			row, n, err := decodeRow(data[pos:], ts, maxRowBytes)
			if err != nil {
				return nil, err
			}
			tc.Inserts = append(tc.Inserts, row)
			pos += n
		}

		// Deletes.
		if pos+4 > len(data) {
			return nil, fmt.Errorf("commitlog: truncated changeset")
		}
		deleteCount := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		for range deleteCount {
			row, n, err := decodeRow(data[pos:], ts, maxRowBytes)
			if err != nil {
				return nil, err
			}
			tc.Deletes = append(tc.Deletes, row)
			pos += n
		}

		cs.Tables[tableID] = tc
	}

	if pos != len(data) {
		return nil, fmt.Errorf("commitlog: trailing changeset bytes")
	}
	return cs, nil
}

func decodeRow(data []byte, ts *schema.TableSchema, maxRowBytes uint32) (types.ProductValue, int, error) {
	if len(data) < 4 {
		return nil, 0, fmt.Errorf("commitlog: truncated row length")
	}
	rowLen := binary.LittleEndian.Uint32(data[:4])
	if maxRowBytes > 0 && rowLen > maxRowBytes {
		return nil, 0, &RowTooLargeError{Size: rowLen, Max: maxRowBytes}
	}
	if int(rowLen)+4 > len(data) {
		return nil, 0, fmt.Errorf("commitlog: truncated row data")
	}
	rowData := data[4 : 4+rowLen]
	pv, err := bsatn.DecodeProductValueFromBytes(rowData, ts)
	if err != nil {
		return nil, 0, err
	}
	return pv, int(4 + rowLen), nil
}
