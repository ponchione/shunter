package store

import (
	"fmt"
	"iter"
	"sync/atomic"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	transactionActive uint32 = iota
	transactionSealed
	transactionCommitted
	transactionRolledBack
)

// Transaction provides read-write access to the store within a reducer.
type Transaction struct {
	committed       *CommittedState
	tx              *TxState
	registry        schema.SchemaRegistry
	state           atomic.Uint32
	txUniqueIndexes map[txUniqueRef]map[uint64][]txUniqueEntry
	txRowIndexes    map[schema.TableID]map[uint64][]txRowEntry
}

type txUniqueRef struct {
	table schema.TableID
	index int
}

type txUniqueEntry struct {
	rowID types.RowID
	key   IndexKey
}

type txRowEntry struct {
	rowID types.RowID
	row   types.ProductValue
}

// NewTransaction creates a transaction over the committed state.
func NewTransaction(cs *CommittedState, reg schema.SchemaRegistry) *Transaction {
	return &Transaction{
		committed: cs,
		tx:        NewTxState(),
		registry:  reg,
	}
}

// TxState returns the underlying transaction state while the transaction is
// still reducer-owned. It returns nil after the executor seals, commits, or
// rolls back the transaction so transaction-local buffers do not remain a
// public lifetime escape after reducer return.
func (t *Transaction) TxState() *TxState {
	if err := t.checkUsable(); err != nil {
		return nil
	}
	return t.tx
}

func (t *Transaction) txStateForCommit() *TxState { return t.tx }

// Seal closes reducer-facing access while preserving the transaction buffers
// for the executor's internal commit path.
func (t *Transaction) Seal() {
	t.state.CompareAndSwap(transactionActive, transactionSealed)
}

func (t *Transaction) finishCommitted() {
	t.state.Store(transactionCommitted)
}

// checkUsable returns ErrTransactionRolledBack if the transaction has been rolled back.
func (t *Transaction) checkUsable() error {
	switch t.state.Load() {
	case transactionActive:
		return nil
	case transactionRolledBack:
		return ErrTransactionRolledBack
	default:
		return ErrTransactionClosed
	}
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

	for idxOrdinal, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		if err := t.checkCommittedUnique(tableID, table, idx, key); err != nil {
			return 0, err
		}
		if err := t.checkTxUnique(tableID, table, idxOrdinal, idx, key); err != nil {
			return 0, err
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
		if t.hasTxDuplicateRow(tableID, row) {
			return 0, ErrDuplicateRow
		}
	}

	id := table.AllocRowID()
	t.addInsert(tableID, id, row, table)
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

func (t *Transaction) checkTxUnique(tableID schema.TableID, table *Table, idxOrdinal int, idx *Index, key IndexKey) error {
	entries := t.ensureTxUniqueIndex(tableID, idxOrdinal, idx)
	for _, entry := range entries[key.hash64()] {
		if key.Equal(entry.key) {
			if idx.schema.Primary {
				return &PrimaryKeyViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
			}
			return &UniqueConstraintViolationError{TableName: table.schema.Name, IndexName: idx.schema.Name, Key: key}
		}
	}
	return nil
}

func (t *Transaction) ensureTxUniqueIndex(tableID schema.TableID, idxOrdinal int, idx *Index) map[uint64][]txUniqueEntry {
	if t.txUniqueIndexes == nil {
		t.txUniqueIndexes = make(map[txUniqueRef]map[uint64][]txUniqueEntry)
	}
	ref := txUniqueRef{table: tableID, index: idxOrdinal}
	if entries, ok := t.txUniqueIndexes[ref]; ok {
		return entries
	}
	entries := make(map[uint64][]txUniqueEntry, len(t.tx.Inserts(tableID)))
	for rowID, row := range t.tx.Inserts(tableID) {
		key := idx.ExtractKey(row)
		entries[key.hash64()] = append(entries[key.hash64()], txUniqueEntry{rowID: rowID, key: key})
	}
	t.txUniqueIndexes[ref] = entries
	return entries
}

func (t *Transaction) hasTxDuplicateRow(tableID schema.TableID, row types.ProductValue) bool {
	entries := t.ensureTxRowIndex(tableID)
	for _, entry := range entries[row.Hash64()] {
		if entry.row.Equal(row) {
			return true
		}
	}
	return false
}

func (t *Transaction) ensureTxRowIndex(tableID schema.TableID) map[uint64][]txRowEntry {
	if t.txRowIndexes == nil {
		t.txRowIndexes = make(map[schema.TableID]map[uint64][]txRowEntry)
	}
	if entries, ok := t.txRowIndexes[tableID]; ok {
		return entries
	}
	entries := make(map[uint64][]txRowEntry, len(t.tx.Inserts(tableID)))
	for rowID, row := range t.tx.Inserts(tableID) {
		entries[row.Hash64()] = append(entries[row.Hash64()], txRowEntry{rowID: rowID, row: row})
	}
	t.txRowIndexes[tableID] = entries
	return entries
}

func (t *Transaction) addInsert(tableID schema.TableID, rowID types.RowID, row types.ProductValue, table *Table) {
	t.tx.AddInsert(tableID, rowID, row)
	stored := t.tx.Inserts(tableID)[rowID]
	t.trackTxInsert(tableID, rowID, stored, table)
}

func (t *Transaction) removeInsert(tableID schema.TableID, rowID types.RowID, table *Table) {
	row, ok := t.tx.Inserts(tableID)[rowID]
	if ok {
		t.untrackTxInsert(tableID, rowID, row, table)
	}
	t.tx.RemoveInsert(tableID, rowID)
}

func (t *Transaction) trackTxInsert(tableID schema.TableID, rowID types.RowID, row types.ProductValue, table *Table) {
	for idxOrdinal, idx := range table.indexes {
		ref := txUniqueRef{table: tableID, index: idxOrdinal}
		if t.txUniqueIndexes == nil {
			continue
		}
		entries, ok := t.txUniqueIndexes[ref]
		if !ok {
			continue
		}
		key := idx.ExtractKey(row)
		entries[key.hash64()] = append(entries[key.hash64()], txUniqueEntry{rowID: rowID, key: key})
	}
	if t.txRowIndexes != nil {
		if entries, ok := t.txRowIndexes[tableID]; ok {
			entries[row.Hash64()] = append(entries[row.Hash64()], txRowEntry{rowID: rowID, row: row})
		}
	}
}

func (t *Transaction) untrackTxInsert(tableID schema.TableID, rowID types.RowID, row types.ProductValue, table *Table) {
	for idxOrdinal, idx := range table.indexes {
		ref := txUniqueRef{table: tableID, index: idxOrdinal}
		if t.txUniqueIndexes == nil {
			continue
		}
		entries, ok := t.txUniqueIndexes[ref]
		if !ok {
			continue
		}
		key := idx.ExtractKey(row)
		bucket := entries[key.hash64()]
		for i, entry := range bucket {
			if entry.rowID == rowID {
				entries[key.hash64()] = append(bucket[:i], bucket[i+1:]...)
				break
			}
		}
		if len(entries[key.hash64()]) == 0 {
			delete(entries, key.hash64())
		}
	}
	if t.txRowIndexes != nil {
		if entries, ok := t.txRowIndexes[tableID]; ok {
			h := row.Hash64()
			bucket := entries[h]
			for i, entry := range bucket {
				if entry.rowID == rowID {
					entries[h] = append(bucket[:i], bucket[i+1:]...)
					break
				}
			}
			if len(entries[h]) == 0 {
				delete(entries, h)
			}
		}
	}
}

func (t *Transaction) tryUndelete(tableID schema.TableID, table *Table, row types.ProductValue) (types.RowID, bool, error) {
	if pk := table.PrimaryIndex(); pk != nil {
		key := pk.ExtractKey(row)
		if rid, ok := t.tryUndeleteRowIDs(tableID, table, row, pk.btree.Seek(key)); ok {
			return rid, true, nil
		}
	}

	if table.rowHashIndex != nil {
		h := row.Hash64()
		if rid, ok := t.tryUndeleteRowIDs(tableID, table, row, table.rowHashIndex[h]); ok {
			return rid, true, nil
		}
	}

	return 0, false, nil
}

func (t *Transaction) tryUndeleteRowIDs(tableID schema.TableID, table *Table, row types.ProductValue, rowIDs []types.RowID) (types.RowID, bool) {
	for _, rid := range rowIDs {
		if !t.tx.IsDeleted(tableID, rid) {
			continue
		}
		committedRow, ok := table.GetRow(rid)
		if !ok {
			continue
		}
		if committedRow.Equal(row) {
			t.tx.CancelDelete(tableID, rid)
			return rid, true
		}
	}
	return 0, false
}

// Delete removes a row by RowID.
func (t *Transaction) Delete(tableID schema.TableID, rowID types.RowID) error {
	if err := t.checkUsable(); err != nil {
		return err
	}
	table, ok := t.committed.Table(tableID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
	}

	// Check tx-local inserts first (insert-then-delete collapse).
	if t.tx.IsInserted(tableID, rowID) {
		t.removeInsert(tableID, rowID, table)
		return nil
	}

	// Check committed state.
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
			table, ok := t.committed.Table(tableID)
			if ok {
				t.addInsert(tableID, rowID, oldRow, table)
			} else {
				t.tx.AddInsert(tableID, rowID, oldRow)
			}
		} else {
			t.tx.CancelDelete(tableID, rowID)
		}
		return 0, err
	}

	return newID, nil
}

// GetRow returns a row visible in this transaction.
func (t *Transaction) GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool) {
	if err := t.checkUsable(); err != nil {
		return nil, false
	}
	return NewStateView(t.committed, t.tx).GetRow(tableID, rowID)
}

// ScanTable yields all visible rows (committed minus deletes plus tx inserts).
func (t *Transaction) ScanTable(tableID schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	if err := t.checkUsable(); err != nil {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return NewStateView(t.committed, t.tx).ScanTable(tableID)
}
