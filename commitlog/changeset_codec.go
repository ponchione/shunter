package commitlog

import (
	"bytes"
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
	var buf bytes.Buffer
	buf.WriteByte(changesetVersion)

	// Sort table IDs for deterministic output.
	tableIDs := make([]schema.TableID, 0, len(cs.Tables))
	for id := range cs.Tables {
		tableIDs = append(tableIDs, id)
	}
	slices.Sort(tableIDs)

	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], uint32(len(tableIDs)))
	buf.Write(scratch[:])

	for _, id := range tableIDs {
		tc := cs.Tables[id]
		binary.LittleEndian.PutUint32(scratch[:], uint32(id))
		buf.Write(scratch[:])

		// Inserts.
		binary.LittleEndian.PutUint32(scratch[:], uint32(len(tc.Inserts)))
		buf.Write(scratch[:])
		for _, row := range tc.Inserts {
			rowBytes, err := encodeRow(row)
			if err != nil {
				return nil, err
			}
			binary.LittleEndian.PutUint32(scratch[:], uint32(len(rowBytes)))
			buf.Write(scratch[:])
			buf.Write(rowBytes)
		}

		// Deletes.
		binary.LittleEndian.PutUint32(scratch[:], uint32(len(tc.Deletes)))
		buf.Write(scratch[:])
		for _, row := range tc.Deletes {
			rowBytes, err := encodeRow(row)
			if err != nil {
				return nil, err
			}
			binary.LittleEndian.PutUint32(scratch[:], uint32(len(rowBytes)))
			buf.Write(scratch[:])
			buf.Write(rowBytes)
		}
	}

	return buf.Bytes(), nil
}

// DecodeChangeset deserializes a Changeset from bytes.
func DecodeChangeset(data []byte, reg schema.SchemaRegistry, maxRowBytes uint32) (*store.Changeset, error) {
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

	return cs, nil
}

func encodeRow(row types.ProductValue) ([]byte, error) {
	var buf bytes.Buffer
	if err := bsatn.EncodeProductValue(&buf, row); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
