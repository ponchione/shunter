package store

import (
	"fmt"
	"iter"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Transaction provides read-write access to the store within a reducer.
type Transaction struct {
	committed *CommittedState
	tx        *TxState
	registry  schema.SchemaRegistry
	rolledBack bool
}

// NewTransaction creates a transaction over the committed state.
func NewTransaction(cs *CommittedState, reg schema.SchemaRegistry) *Transaction {
	return &Transaction{
		committed: cs,
		tx:        NewTxState(),
		registry:  reg,
	}
}

// TxState returns the underlying transaction state (for commit).
func (t *Transaction) TxState() *TxState { return t.tx }

// Insert validates and inserts a row, returning the provisional RowID.
func (t *Transaction) Insert(tableID schema.TableID, row types.ProductValue) (types.RowID, error) {
	table, ok := t.committed.Table(tableID)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
	}

	if err := ValidateRow(table.Schema(), row); err != nil {
		return 0, err
	}

	if rowID, undeleted, err := t.tryUndelete(tableID, table, row); err != nil {
		return 0, err
	} else if undeleted {
		return rowID, nil
	}

	for _, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		if err := t.checkCommittedUnique(tableID, table, idx, key); err != nil {
			return 0, err
		}
		for _, txRow := range t.tx.Inserts(tableID) {
			if key.Equal(idx.ExtractKey(txRow)) {
				if idx.schema.Primary {
					return 0, &PrimaryKeyViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
				}
				return 0, &UniqueConstraintViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
			}
		}
	}

	if table.rowHashIndex != nil {
		h := row.Hash64()
		for _, rid := range table.rowHashIndex[h] {
			if t.tx.IsDeleted(tableID, rid) {
				continue
			}
			if committedRow, ok := table.GetRow(rid); ok && committedRow.Equal(row) {
				return 0, ErrDuplicateRow
			}
		}
		for _, txRow := range t.tx.Inserts(tableID) {
			if txRow.Equal(row) {
				return 0, ErrDuplicateRow
			}
		}
	}

	id := table.AllocRowID()
	t.tx.AddInsert(tableID, id, row)
	return id, nil
}

func (t *Transaction) checkCommittedUnique(tableID schema.TableID, table *Table, idx *Index, key IndexKey) error {
	for _, rid := range idx.btree.Seek(key) {
		if t.tx.IsDeleted(tableID, rid) {
			continue
		}
		if idx.schema.Primary {
			return &PrimaryKeyViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
		}
		return &UniqueConstraintViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
	}
	return nil
}

func (t *Transaction) tryUndelete(tableID schema.TableID, table *Table, row types.ProductValue) (types.RowID, bool, error) {
	if pk := table.PrimaryIndex(); pk != nil {
		key := pk.ExtractKey(row)
		for _, rid := range pk.btree.Seek(key) {
			if !t.tx.IsDeleted(tableID, rid) {
				continue
			}
			committedRow, ok := table.GetRow(rid)
			if !ok {
				continue
			}
			if committedRow.Equal(row) {
				t.tx.CancelDelete(tableID, rid)
				return rid, true, nil
			}
		}
	}

	if table.rowHashIndex != nil {
		h := row.Hash64()
		for _, rid := range table.rowHashIndex[h] {
			if !t.tx.IsDeleted(tableID, rid) {
				continue
			}
			committedRow, ok := table.GetRow(rid)
			if !ok {
				continue
			}
			if committedRow.Equal(row) {
				t.tx.CancelDelete(tableID, rid)
				return rid, true, nil
			}
		}
	}

	return 0, false, nil
}

// Delete removes a row by RowID.
func (t *Transaction) Delete(tableID schema.TableID, rowID types.RowID) error {
	if _, ok := t.committed.Table(tableID); !ok {
		return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
	}

	// Check tx-local inserts first (insert-then-delete collapse).
	if t.tx.IsInserted(tableID, rowID) {
		t.tx.RemoveInsert(tableID, rowID)
		return nil
	}

	// Check committed state.
	table, _ := t.committed.Table(tableID)
	if _, ok := table.GetRow(rowID); !ok {
		return fmt.Errorf("%w: %d", ErrRowNotFound, rowID)
	}

	// Already deleted in this tx?
	if t.tx.IsDeleted(tableID, rowID) {
		return fmt.Errorf("%w: %d (already deleted in this transaction)", ErrRowNotFound, rowID)
	}

	t.tx.AddDelete(tableID, rowID)
	return nil
}

// Update deletes the old row and inserts the new one.
// On insert failure, the delete is rolled back.
func (t *Transaction) Update(tableID schema.TableID, rowID types.RowID, newRow types.ProductValue) (types.RowID, error) {
	// Remember if this was a committed row vs tx-local.
	wasTxInsert := t.tx.IsInserted(tableID, rowID)
	var oldRow types.ProductValue

	if wasTxInsert {
		oldRow = t.tx.Inserts(tableID)[rowID]
	} else {
		table, ok := t.committed.Table(tableID)
		if !ok {
			return 0, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		oldRow, ok = table.GetRow(rowID)
		if !ok || t.tx.IsDeleted(tableID, rowID) {
			return 0, fmt.Errorf("%w: %d", ErrRowNotFound, rowID)
		}
	}

	// Delete old row.
	if err := t.Delete(tableID, rowID); err != nil {
		return 0, err
	}

	// Insert new row.
	newID, err := t.Insert(tableID, newRow)
	if err != nil {
		// Rollback delete.
		if wasTxInsert {
			t.tx.AddInsert(tableID, rowID, oldRow)
		} else {
			t.tx.CancelDelete(tableID, rowID)
		}
		return 0, err
	}

	return newID, nil
}

// GetRow returns a row visible in this transaction.
func (t *Transaction) GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool) {
	// Check tx-local inserts.
	if row, ok := t.tx.Inserts(tableID)[rowID]; ok {
		return row, true
	}
	// Check if deleted in this tx.
	if t.tx.IsDeleted(tableID, rowID) {
		return nil, false
	}
	// Check committed.
	table, ok := t.committed.Table(tableID)
	if !ok {
		return nil, false
	}
	return table.GetRow(rowID)
}

// ScanTable yields all visible rows (committed minus deletes plus tx inserts).
func (t *Transaction) ScanTable(tableID schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		table, ok := t.committed.Table(tableID)
		if !ok {
			return
		}
		// Committed rows minus deletes.
		for id, row := range table.Scan() {
			if t.tx.IsDeleted(tableID, id) {
				continue
			}
			if !yield(id, row) {
				return
			}
		}
		// Tx-local inserts.
		for id, row := range t.tx.Inserts(tableID) {
			if !yield(id, row) {
				return
			}
		}
	}
}
