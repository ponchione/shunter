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

// checkUsable returns ErrTransactionRolledBack if the transaction has been rolled back.
func (t *Transaction) checkUsable() error {
	if t.rolledBack {
		return ErrTransactionRolledBack
	}
	return nil
}

func (t *Transaction) applyAutoIncrement(table *Table, row types.ProductValue) (types.ProductValue, error) {
	if table.sequence == nil || table.sequenceCol < 0 {
		return row, nil
	}

	col := table.schema.Columns[table.sequenceCol]
	if !isZeroAutoIncrementValue(row[table.sequenceCol], col.Type) {
		return row, nil
	}

	_, max, ok := schema.AutoIncrementBounds(col.Type)
	if !ok {
		return nil, schema.ErrAutoIncrementType
	}
	next := table.sequence.Peek()
	if next > max {
		return nil, schema.ErrSequenceOverflow
	}

	assigned := row.Copy()
	assigned[table.sequenceCol] = newAutoIncrementValue(next, col.Type)
	table.sequence.Next()
	return assigned, nil
}

func isZeroAutoIncrementValue(v types.Value, kind schema.ValueKind) bool {
	switch kind {
	case schema.KindInt8, schema.KindInt16, schema.KindInt32, schema.KindInt64:
		return v.AsInt64() == 0
	case schema.KindUint8, schema.KindUint16, schema.KindUint32, schema.KindUint64:
		return v.AsUint64() == 0
	default:
		return false
	}
}

func newAutoIncrementValue(n uint64, kind schema.ValueKind) types.Value {
	switch kind {
	case schema.KindInt8:
		return types.NewInt8(int8(n))
	case schema.KindUint8:
		return types.NewUint8(uint8(n))
	case schema.KindInt16:
		return types.NewInt16(int16(n))
	case schema.KindUint16:
		return types.NewUint16(uint16(n))
	case schema.KindInt32:
		return types.NewInt32(int32(n))
	case schema.KindUint32:
		return types.NewUint32(uint32(n))
	case schema.KindInt64:
		return types.NewInt64(int64(n))
	case schema.KindUint64:
		return types.NewUint64(n)
	default:
		panic("unsupported autoincrement kind")
	}
}

// Insert validates and inserts a row, returning the provisional RowID.
func (t *Transaction) Insert(tableID schema.TableID, row types.ProductValue) (types.RowID, error) {
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
	table, ok := t.committed.Table(tableID)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
	}

	if err := ValidateRow(table.Schema(), row); err != nil {
		return 0, err
	}
	row, err := t.applyAutoIncrement(table, row)
	if err != nil {
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
	if err := t.checkUsable(); err != nil {
		return err
	}
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
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
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
	if t.rolledBack {
		return nil, false
	}
	return NewStateView(t.committed, t.tx).GetRow(tableID, rowID)
}

// ScanTable yields all visible rows (committed minus deletes plus tx inserts).
func (t *Transaction) ScanTable(tableID schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	if t.rolledBack {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return NewStateView(t.committed, t.tx).ScanTable(tableID)
}
