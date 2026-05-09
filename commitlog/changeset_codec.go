package commitlog

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

const changesetVersion byte = 1

// EncodeChangeset serializes a Changeset to bytes.
func EncodeChangeset(cs *store.Changeset) ([]byte, error) {
	opts := DefaultCommitLogOptions()
	return encodeChangesetWithLimits(cs, opts.MaxRowBytes, opts.MaxRecordPayloadBytes)
}

func encodeChangesetWithLimits(cs *store.Changeset, maxRowBytes uint32, maxRecordPayloadBytes uint32) ([]byte, error) {
	// Sort table IDs for deterministic output.
	tableIDs := make([]schema.TableID, 0, len(cs.Tables))
	for id := range cs.Tables {
		tableIDs = append(tableIDs, id)
	}
	slices.Sort(tableIDs)

	size := uint64(1 + 4)
	for _, id := range tableIDs {
		tc := cs.Tables[id]
		size += 4
		insertSize, err := encodedChangesetRowsSize(tc.Inserts, tc.Schema, maxRowBytes)
		if err != nil {
			return nil, err
		}
		deleteSize, err := encodedChangesetRowsSize(tc.Deletes, tc.Schema, maxRowBytes)
		if err != nil {
			return nil, err
		}
		size += insertSize + deleteSize
		if err := validateRecordPayloadLen(size, maxRecordPayloadBytes); err != nil {
			return nil, err
		}
	}
	maxAlloc := uint64(int(^uint(0) >> 1))
	if size > maxAlloc {
		return nil, fmt.Errorf("%w: changeset payload %d exceeds max allocation %d", ErrTraversal, size, maxAlloc)
	}
	tableCount, err := checkedChangesetCount("table count", len(tableIDs))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, int(size))
	out = append(out, changesetVersion)
	out = appendUint32LE(out, tableCount)

	for _, id := range tableIDs {
		tc := cs.Tables[id]
		out = appendUint32LE(out, uint32(id))

		// Inserts.
		var err error
		out, err = appendChangesetRows(out, tc.Inserts, tc.Schema, maxRowBytes)
		if err != nil {
			return nil, err
		}
		if err := validateRecordPayloadLen(uint64(len(out)), maxRecordPayloadBytes); err != nil {
			return nil, err
		}

		// Deletes.
		out, err = appendChangesetRows(out, tc.Deletes, tc.Schema, maxRowBytes)
		if err != nil {
			return nil, err
		}
		if err := validateRecordPayloadLen(uint64(len(out)), maxRecordPayloadBytes); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func encodedChangesetRowsSize(rows []types.ProductValue, ts *schema.TableSchema, maxRowBytes uint32) (uint64, error) {
	if _, err := checkedChangesetCount("row count", len(rows)); err != nil {
		return 0, err
	}
	size := uint64(4)
	for _, row := range rows {
		rowLen, err := bsatn.EncodedProductValueSizeForSchema(row, ts)
		if err != nil {
			return 0, err
		}
		if err := validateRowPayloadLen(rowLen, maxRowBytes); err != nil {
			return 0, err
		}
		size += 4 + uint64(rowLen)
	}
	return size, nil
}

func appendChangesetRows(out []byte, rows []types.ProductValue, ts *schema.TableSchema, maxRowBytes uint32) ([]byte, error) {
	count, err := checkedChangesetCount("row count", len(rows))
	if err != nil {
		return out, err
	}
	out = appendUint32LE(out, count)
	for _, row := range rows {
		rowLen, err := bsatn.EncodedProductValueSizeForSchema(row, ts)
		if err != nil {
			return out, err
		}
		if err := validateRowPayloadLen(rowLen, maxRowBytes); err != nil {
			return out, err
		}
		out = appendUint32LE(out, uint32(rowLen))
		before := len(out)
		out, err = bsatn.AppendProductValueForSchema(out, row, ts)
		if err != nil {
			return out, err
		}
		if got := len(out) - before; got != rowLen {
			return out, fmt.Errorf("commitlog: encoded row size changed from %d to %d", rowLen, got)
		}
	}
	return out, nil
}

func checkedChangesetCount(label string, n int) (uint32, error) {
	if n < 0 || uint64(n) > math.MaxUint32 {
		return 0, fmt.Errorf("%w: changeset %s %d exceeds uint32 length", ErrTraversal, label, n)
	}
	return uint32(n), nil
}

func validateRowPayloadLen(rowLen int, maxRowBytes uint32) error {
	if rowLen < 0 {
		return fmt.Errorf("%w: negative encoded row size %d", ErrTraversal, rowLen)
	}
	if uint64(rowLen) > math.MaxUint32 {
		return fmt.Errorf("%w: row payload %d exceeds uint32 length", ErrTraversal, rowLen)
	}
	if maxRowBytes > 0 && uint64(rowLen) > uint64(maxRowBytes) {
		return &RowTooLargeError{Size: uint32(rowLen), Max: maxRowBytes}
	}
	return nil
}

func validateRecordPayloadLen(payloadLen uint64, maxRecordPayloadBytes uint32) error {
	if payloadLen > math.MaxUint32 {
		return fmt.Errorf("%w: record payload %d exceeds uint32 length", ErrTraversal, payloadLen)
	}
	if maxRecordPayloadBytes > 0 && payloadLen > uint64(maxRecordPayloadBytes) {
		return &RecordTooLargeError{Size: uint32(payloadLen), Max: maxRecordPayloadBytes}
	}
	return nil
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
		return nil, changesetDecodeErrorf("commitlog: changeset too short")
	}
	if data[0] != changesetVersion {
		return nil, changesetDecodeErrorf("commitlog: unsupported changeset version %d", data[0])
	}

	pos := 1
	tableCount := binary.LittleEndian.Uint32(data[pos:])
	pos += 4

	cs := &store.Changeset{
		Tables: make(map[schema.TableID]*store.TableChangeset),
	}

	for range tableCount {
		if pos+4 > len(data) {
			return nil, changesetDecodeErrorf("commitlog: truncated changeset")
		}
		tableID := schema.TableID(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4

		if _, exists := cs.Tables[tableID]; exists {
			return nil, changesetDecodeErrorf("commitlog: duplicate table ID %d", tableID)
		}
		ts, ok := reg.Table(tableID)
		if !ok {
			return nil, changesetDecodeErrorf("commitlog: unknown table ID %d", tableID)
		}

		tc := &store.TableChangeset{TableID: tableID, TableName: ts.Name, Schema: ts}

		// Inserts.
		if pos+4 > len(data) {
			return nil, changesetDecodeErrorf("commitlog: truncated changeset")
		}
		insertCount := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		for range insertCount {
			row, n, err := decodeRow(data[pos:], ts, maxRowBytes)
			if err != nil {
				return nil, changesetDecodeErrorf("commitlog: decode insert row for table %d: %w", tableID, err)
			}
			tc.Inserts = append(tc.Inserts, row)
			pos += n
		}

		// Deletes.
		if pos+4 > len(data) {
			return nil, changesetDecodeErrorf("commitlog: truncated changeset")
		}
		deleteCount := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		for range deleteCount {
			row, n, err := decodeRow(data[pos:], ts, maxRowBytes)
			if err != nil {
				return nil, changesetDecodeErrorf("commitlog: decode delete row for table %d: %w", tableID, err)
			}
			tc.Deletes = append(tc.Deletes, row)
			pos += n
		}

		cs.Tables[tableID] = tc
	}

	if pos != len(data) {
		return nil, changesetDecodeErrorf("commitlog: trailing changeset bytes")
	}
	return cs, nil
}

func changesetDecodeErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrTraversal}, args...)...)
}

func decodeRow(data []byte, ts *schema.TableSchema, maxRowBytes uint32) (types.ProductValue, int, error) {
	if len(data) < 4 {
		return nil, 0, fmt.Errorf("commitlog: truncated row length")
	}
	rowLen := binary.LittleEndian.Uint32(data[:4])
	if maxRowBytes > 0 && rowLen > maxRowBytes {
		return nil, 0, &RowTooLargeError{Size: rowLen, Max: maxRowBytes}
	}
	if uint64(rowLen) > uint64(len(data)-4) {
		return nil, 0, fmt.Errorf("commitlog: truncated row data")
	}
	rowData := data[4 : 4+rowLen]
	pv, err := bsatn.DecodeProductValueFromBytes(rowData, ts)
	if err != nil {
		return nil, 0, err
	}
	return pv, 4 + int(rowLen), nil
}
