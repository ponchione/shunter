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

	for tableID, tc := range changeset.Tables {
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
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
			freshID := table.AllocRowID()
			if err := table.InsertRow(freshID, row); err != nil {
				return err
			}
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

// ExportTableState returns the RowID counter and sequence values for snapshot persistence.
type TableExportState struct {
	TableID       schema.TableID
	NextRowID     types.RowID
	SequenceValue uint64
}
