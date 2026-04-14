package store

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Commit applies the transaction's mutations to committed state and produces a changeset.
// It revalidates constraints under the committed-state write lock before mutating state.
func Commit(cs *CommittedState, tx *Transaction) (*Changeset, error) {
	cs.Lock()
	defer cs.Unlock()

	txState := tx.TxState()
	if err := revalidateCommit(cs, txState); err != nil {
		return nil, err
	}
	changeset := &Changeset{
		TxID:   0,
		Tables: make(map[schema.TableID]*TableChangeset),
	}

	for tableID, dels := range txState.AllDeletes() {
		table, ok := cs.Table(tableID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		tc := ensureTableChangeset(changeset, tableID, table.schema.Name)
		for rowID := range dels {
			if oldRow, ok := table.DeleteRow(rowID); ok {
				tc.Deletes = append(tc.Deletes, oldRow)
			}
		}
	}

	for tableID, ins := range txState.AllInserts() {
		table, ok := cs.Table(tableID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		tc := ensureTableChangeset(changeset, tableID, table.schema.Name)
		for rowID, row := range ins {
			if err := table.InsertRow(rowID, row); err != nil {
				return nil, err
			}
			tc.Inserts = append(tc.Inserts, row)
		}
	}

	return changeset, nil
}

// Rollback discards the transaction. No committed state mutation.
func Rollback(tx *Transaction) {
	tx.rolledBack = true
}

func ensureTableChangeset(cs *Changeset, id schema.TableID, tableName string) *TableChangeset {
	tc := cs.Tables[id]
	if tc == nil {
		tc = &TableChangeset{TableID: id, TableName: tableName}
		cs.Tables[id] = tc
	}
	return tc
}

func revalidateCommit(cs *CommittedState, txState *TxState) error {
	for tableID, inserts := range txState.AllInserts() {
		table, ok := cs.Table(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		for _, row := range inserts {
			if err := revalidateInsertAgainstCommitted(tableID, table, row, txState); err != nil {
				return err
			}
		}
	}
	return nil
}

func revalidateInsertAgainstCommitted(tableID schema.TableID, table *Table, row types.ProductValue, txState *TxState) error {
	for _, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		for _, rid := range idx.btree.Seek(key) {
			if txState.IsDeleted(tableID, rid) {
				continue
			}
			if idx.schema.Primary {
				return &PrimaryKeyViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
			}
			return &UniqueConstraintViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
		}
	}

	if table.rowHashIndex != nil {
		h := row.Hash64()
		for _, rid := range table.rowHashIndex[h] {
			if txState.IsDeleted(tableID, rid) {
				continue
			}
			if existing, ok := table.GetRow(rid); ok && existing.Equal(row) {
				return ErrDuplicateRow
			}
		}
	}

	return nil
}
