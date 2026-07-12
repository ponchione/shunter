package store

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/internal/autoincrement"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

var (
	errRecoveryBatchClosed = errors.New("store: recovery batch is closed")
	errRecoveryStateNil    = errors.New("store: recovery committed state is required")
)

// RecoveryBatch stages a sequence of recovery changesets and publishes all
// touched tables only after the complete sequence succeeds. It is intended for
// single-goroutine startup recovery and is not safe for concurrent use.
type RecoveryBatch struct {
	state  *CommittedState
	staged map[schema.TableID]*Table
	failed error
	closed bool
}

// NewRecoveryBatch creates a recovery-scoped changeset batch.
func NewRecoveryBatch(state *CommittedState) *RecoveryBatch {
	b := &RecoveryBatch{
		state:  state,
		staged: make(map[schema.TableID]*Table),
	}
	if state == nil {
		b.failed = errRecoveryStateNil
	}
	return b
}

// Apply stages one changeset. A failed apply poisons the batch; Commit will
// return the same error without publishing any staged table.
func (b *RecoveryBatch) Apply(changeset *Changeset) error {
	if err := b.usable(); err != nil {
		return err
	}
	if changeset == nil {
		return nil
	}
	for tableID, tc := range changeset.Tables {
		stagedTable, ok := b.staged[tableID]
		if !ok {
			b.state.RLock()
			table, exists := b.state.tableLocked(tableID)
			if !exists {
				b.state.RUnlock()
				return b.fail(fmt.Errorf("%w: %d", ErrTableNotFound, tableID))
			}
			if table.schema.IsEvent {
				b.state.RUnlock()
				continue
			}
			var err error
			stagedTable, err = cloneReplayTable(table)
			b.state.RUnlock()
			if err != nil {
				return b.fail(err)
			}
			b.staged[tableID] = stagedTable
		}
		if err := applyChangesetToTable(stagedTable, tc); err != nil {
			return b.fail(err)
		}
	}
	return nil
}

// Commit atomically publishes all successfully staged tables.
func (b *RecoveryBatch) Commit() error {
	if err := b.usable(); err != nil {
		return err
	}
	b.state.Lock()
	defer b.state.Unlock()
	for tableID := range b.staged {
		if _, ok := b.state.tableLocked(tableID); !ok {
			return b.fail(fmt.Errorf("%w: %d", ErrTableNotFound, tableID))
		}
	}
	for tableID, stagedTable := range b.staged {
		table, _ := b.state.tableLocked(tableID)
		*table = *stagedTable
	}
	b.closed = true
	b.staged = nil
	return nil
}

// Discard abandons staged tables without modifying committed state.
func (b *RecoveryBatch) Discard() {
	if b == nil || b.closed {
		return
	}
	b.closed = true
	b.staged = nil
}

func (b *RecoveryBatch) usable() error {
	if b == nil {
		return errRecoveryBatchClosed
	}
	if b.failed != nil {
		return b.failed
	}
	if b.closed {
		return errRecoveryBatchClosed
	}
	return nil
}

func (b *RecoveryBatch) fail(err error) error {
	if b.failed == nil {
		b.failed = err
	}
	return b.failed
}

// ApplyChangeset replays a changeset directly into committed state.
// Used for crash recovery — bypasses transaction lifecycle.
func ApplyChangeset(cs *CommittedState, changeset *Changeset) error {
	if changeset == nil {
		return nil
	}
	cs.Lock()
	defer cs.Unlock()

	staged := make(map[schema.TableID]*Table, len(changeset.Tables))
	for tableID, tc := range changeset.Tables {
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		if table.schema.IsEvent {
			continue
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
		for _, rid := range pk.btree.rowIDs(key) {
			committedRow, ok := table.rowView(rid)
			if ok && committedRow.Equal(row) {
				return rid, true
			}
		}
		return 0, false
	}

	h := row.Hash64()
	for _, rid := range table.rowHashIndex[h] {
		committedRow, ok := table.rowView(rid)
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
	value, ok := autoincrement.ValueAsUint64(row[table.sequenceCol], table.schema.Columns[table.sequenceCol].Type)
	if !ok {
		return
	}
	current := table.sequence.Peek()
	next := nextAutoIncrementSequenceValue(value)
	if current != 0 && next > current {
		table.sequence.Reset(next)
	}
}

// ExportTableState returns the RowID counter and sequence values for snapshot persistence.
type TableExportState struct {
	TableID       schema.TableID
	NextRowID     types.RowID
	SequenceValue uint64
}
