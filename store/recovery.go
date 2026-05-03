package store

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// ApplyChangeset replays a changeset directly into committed state.
// Used for crash recovery — bypasses transaction lifecycle.
func ApplyChangeset(cs *CommittedState, changeset *Changeset) error {
	cs.Lock()
	defer cs.Unlock()

	staged := make(map[schema.TableID]*Table, len(changeset.Tables))
	for tableID, tc := range changeset.Tables {
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		stagedTable, err := cloneReplayTable(table)
		if err != nil {
			return err
		}
		if err := applyChangesetToTable(stagedTable, tc); err != nil {
			return err
		}
		staged[tableID] = stagedTable
	}

	for tableID, stagedTable := range staged {
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		*table = *stagedTable
	}
	return nil
}

func cloneReplayTable(table *Table) (*Table, error) {
	clone := NewTable(table.schema)
	for rowID, row := range table.rows {
		if err := clone.InsertRow(rowID, row); err != nil {
			return nil, fmt.Errorf("clone replay table %q: %w", table.schema.Name, err)
		}
	}
	clone.SetNextID(table.NextID())
	if seq, ok := table.SequenceValue(); ok {
		clone.SetSequenceValue(seq)
	}
	return clone, nil
}

func applyChangesetToTable(table *Table, tc *TableChangeset) error {
	if tc == nil {
		return nil
	}
	for _, row := range tc.Deletes {
		rowID, ok := findReplayDeleteRowID(table, row)
		if !ok {
			return fmt.Errorf("%w: replay delete row not found in table %q", ErrRowNotFound, table.schema.Name)
		}
		if _, ok := table.DeleteRow(rowID); !ok {
			return fmt.Errorf("%w: replay delete row not found in table %q", ErrRowNotFound, table.schema.Name)
		}
	}

	for _, row := range tc.Inserts {
		if err := ValidateRow(table.schema, row); err != nil {
			return err
		}
		if err := checkRowIDAvailable(table); err != nil {
			return err
		}
		advanceReplaySequenceForInsert(table, row)
		freshID, err := allocRowIDForInsert(table)
		if err != nil {
			return err
		}
		if err := table.InsertRow(freshID, row); err != nil {
			return err
		}
	}
	return nil
}

func findReplayDeleteRowID(table *Table, row types.ProductValue) (types.RowID, bool) {
	if pk := table.PrimaryIndex(); pk != nil {
		key := pk.ExtractKey(row)
		for _, rid := range pk.Seek(key) {
			committedRow, ok := table.GetRow(rid)
			if ok && committedRow.Equal(row) {
				return rid, true
			}
		}
		return 0, false
	}

	h := row.Hash64()
	for _, rid := range table.rowHashIndex[h] {
		committedRow, ok := table.GetRow(rid)
		if ok && committedRow.Equal(row) {
			return rid, true
		}
	}
	return 0, false
}

func advanceReplaySequenceForInsert(table *Table, row types.ProductValue) {
	if table.sequence == nil || table.sequenceCol < 0 {
		return
	}
	current := table.sequence.Peek()
	value, ok := replayAutoIncrementValueAsUint64(row[table.sequenceCol], table.schema.Columns[table.sequenceCol].Type)
	if !ok || value != current {
		return
	}
	table.sequence.Next()
}

func replayAutoIncrementValueAsUint64(v types.Value, kind schema.ValueKind) (uint64, bool) {
	switch kind {
	case schema.KindInt8, schema.KindInt16, schema.KindInt32, schema.KindInt64:
		n := v.AsInt64()
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case schema.KindUint8, schema.KindUint16, schema.KindUint32, schema.KindUint64:
		return v.AsUint64(), true
	default:
		return 0, false
	}
}

// ExportTableState returns the RowID counter and sequence values for snapshot persistence.
type TableExportState struct {
	TableID       schema.TableID
	NextRowID     types.RowID
	SequenceValue uint64
}
