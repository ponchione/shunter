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
	txNextRowIDs    map[schema.TableID]types.RowID
	txSequences     map[schema.TableID]uint64
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

func (t *Transaction) applyAutoIncrement(tableID schema.TableID, table *Table, row types.ProductValue) (types.ProductValue, uint64, bool, error) {
	if table.sequence == nil || table.sequenceCol < 0 {
		return row, 0, false, nil
	}

	col := table.schema.Columns[table.sequenceCol]
	if !isZeroAutoIncrementValue(row[table.sequenceCol], col.Type) {
		value, ok := autoIncrementValueAsUint64(row[table.sequenceCol], col.Type)
		return row, value, ok, nil
	}

	_, max, ok := schema.AutoIncrementBounds(col.Type)
	if !ok {
		return nil, 0, false, schema.ErrAutoIncrementType
	}
	next := t.nextSequenceValue(tableID, table)
	// Sequence value 0 is the exhausted sentinel. A uint64 autoincrement
	// cannot safely consume MaxUint64 because the next sequence value would
	// wrap to that sentinel and snapshot/recovery cannot represent max+1.
	if next == 0 || next > max || (next == max && max == ^uint64(0)) {
		return nil, 0, false, schema.ErrSequenceOverflow
	}

	assigned := row.Copy()
	assigned[table.sequenceCol] = newAutoIncrementValue(next, col.Type)
	return assigned, next, true, nil
}

func (t *Transaction) nextSequenceValue(tableID schema.TableID, table *Table) uint64 {
	if t.txSequences != nil {
		if next, ok := t.txSequences[tableID]; ok {
			return next
		}
	}
	return table.sequence.Peek()
}

func (t *Transaction) setNextSequenceValue(tableID schema.TableID, next uint64) {
	if t.txSequences == nil {
		t.txSequences = make(map[schema.TableID]uint64)
	}
	t.txSequences[tableID] = next
}

func (t *Transaction) advanceTxSequencePastValue(tableID schema.TableID, table *Table, observed uint64) {
	current := t.nextSequenceValue(tableID, table)
	next := nextAutoIncrementSequenceValue(observed)
	if current != 0 && next > current {
		t.setNextSequenceValue(tableID, next)
	}
}

func nextAutoIncrementSequenceValue(observed uint64) uint64 {
	if observed == ^uint64(0) {
		return observed
	}
	return observed + 1
}

func isZeroAutoIncrementValue(v types.Value, kind schema.ValueKind) bool {
	n, ok := autoIncrementValueAsUint64(v, kind)
	return ok && n == 0
}

func autoIncrementValueAsUint64(v types.Value, kind schema.ValueKind) (uint64, bool) {
	if v.IsNull() {
		return 0, false
	}
	switch kind {
	case schema.KindInt8:
		n := int64(v.AsInt8())
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case schema.KindInt16:
		n := int64(v.AsInt16())
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case schema.KindInt32:
		n := int64(v.AsInt32())
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case schema.KindInt64:
		n := v.AsInt64()
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case schema.KindUint8:
		return uint64(v.AsUint8()), true
	case schema.KindUint16:
		return uint64(v.AsUint16()), true
	case schema.KindUint32:
		return uint64(v.AsUint32()), true
	case schema.KindUint64:
		return v.AsUint64(), true
	default:
		return 0, false
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
	if err := checkRowIDAvailable(table); err != nil {
		return 0, err
	}
	row, sequenceValue, advanceSequence, err := t.applyAutoIncrement(tableID, table, row)
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

	id, err := t.allocRowIDForInsert(tableID, table)
	if err != nil {
		return 0, err
	}
	if advanceSequence {
		t.advanceTxSequencePastValue(tableID, table, sequenceValue)
	}
	t.addInsert(tableID, id, row, table)
	return id, nil
}

func (t *Transaction) allocRowIDForInsert(tableID schema.TableID, table *Table) (types.RowID, error) {
	next := t.nextRowID(tableID, table)
	if err := checkRowIDValueAvailable(next); err != nil {
		return 0, err
	}
	t.setNextRowID(tableID, next+1)
	return next, nil
}

func (t *Transaction) nextRowID(tableID schema.TableID, table *Table) types.RowID {
	if t.txNextRowIDs != nil {
		if next, ok := t.txNextRowIDs[tableID]; ok {
			return next
		}
	}
	return table.NextID()
}

func (t *Transaction) setNextRowID(tableID schema.TableID, next types.RowID) {
	if t.txNextRowIDs == nil {
		t.txNextRowIDs = make(map[schema.TableID]types.RowID)
	}
	t.txNextRowIDs[tableID] = next
}

func allocRowIDForInsert(table *Table) (types.RowID, error) {
	if err := checkRowIDAvailable(table); err != nil {
		return 0, err
	}
	return table.AllocRowID(), nil
}

func checkRowIDAvailable(table *Table) error {
	return checkRowIDValueAvailable(table.NextID())
}

func checkRowIDValueAvailable(next types.RowID) error {
	// RowID 0 is outside the allocator range. Consuming MaxUint64 would
	// wrap the next counter to 0, which snapshots reject as an invalid next_id.
	if next == 0 || next == ^types.RowID(0) {
		return ErrRowIDOverflow
	}
	return nil
}

func (t *Transaction) checkCommittedUnique(tableID schema.TableID, table *Table, idx *Index, key IndexKey) error {
	for _, rid := range idx.btree.Seek(key) {
		if t.tx.IsDeleted(tableID, rid) {
			continue
		}
		return uniqueViolationError(table, idx, key)
	}
	return nil
}

func (t *Transaction) checkTxUnique(tableID schema.TableID, table *Table, idxOrdinal int, idx *Index, key IndexKey) error {
	entries := t.ensureTxUniqueIndex(tableID, idxOrdinal, idx)
	for _, entry := range entries[key.hash64()] {
		if key.Equal(entry.key) {
			return uniqueViolationError(table, idx, key)
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
	rows := t.tx.tableInserts(tableID)
	entries := make(map[uint64][]txUniqueEntry, len(rows))
	for rowID, row := range rows {
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
	rows := t.tx.tableInserts(tableID)
	entries := make(map[uint64][]txRowEntry, len(rows))
	for rowID, row := range rows {
		entries[row.Hash64()] = append(entries[row.Hash64()], txRowEntry{rowID: rowID, row: row})
	}
	t.txRowIndexes[tableID] = entries
	return entries
}

func (t *Transaction) addInsert(tableID schema.TableID, rowID types.RowID, row types.ProductValue, table *Table) {
	t.tx.AddInsert(tableID, rowID, row)
	stored, _ := t.tx.insert(tableID, rowID)
	t.trackTxInsert(tableID, rowID, stored, table)
}

func (t *Transaction) removeInsert(tableID schema.TableID, rowID types.RowID, table *Table) {
	row, ok := t.tx.insert(tableID, rowID)
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
		if rid, ok, err := t.tryUndeleteRowIDs(tableID, table, row, pk.btree.Seek(key)); err != nil || ok {
			return rid, ok, err
		}
	}

	if table.rowHashIndex != nil {
		h := row.Hash64()
		if rid, ok, err := t.tryUndeleteRowIDs(tableID, table, row, table.rowHashIndex[h]); err != nil || ok {
			return rid, ok, err
		}
	}

	return 0, false, nil
}

func (t *Transaction) tryUndeleteRowIDs(tableID schema.TableID, table *Table, row types.ProductValue, rowIDs []types.RowID) (types.RowID, bool, error) {
	for _, rid := range rowIDs {
		if !t.tx.IsDeleted(tableID, rid) {
			continue
		}
		committedRow, ok := table.GetRow(rid)
		if !ok {
			continue
		}
		if committedRow.Equal(row) {
			if err := t.checkTxInsertConflicts(tableID, table, row); err != nil {
				return 0, false, err
			}
			t.tx.CancelDelete(tableID, rid)
			return rid, true, nil
		}
	}
	return 0, false, nil
}

func (t *Transaction) checkTxInsertConflicts(tableID schema.TableID, table *Table, row types.ProductValue) error {
	for idxOrdinal, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		if err := t.checkTxUnique(tableID, table, idxOrdinal, idx, key); err != nil {
			return err
		}
	}
	if table.rowHashIndex != nil && t.hasTxDuplicateRow(tableID, row) {
		return ErrDuplicateRow
	}
	return nil
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
		oldRow, _ = t.tx.insert(tableID, rowID)
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

// SeekIndex yields rows visible in this transaction whose index key exactly
// matches key.
func (t *Transaction) SeekIndex(tableID schema.TableID, indexID schema.IndexID, key ...types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	if err := t.checkUsable(); err != nil {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	view := NewStateView(t.committed, t.tx)
	indexKey := NewIndexKey(key...)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid := range view.SeekIndex(tableID, indexID, indexKey) {
			row, ok := view.GetRow(tableID, rid)
			if !ok {
				continue
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}

// SeekIndexRange yields rows visible in this transaction whose index key falls
// between lower and upper.
func (t *Transaction) SeekIndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper types.IndexBound) iter.Seq2[types.RowID, types.ProductValue] {
	if err := t.checkUsable(); err != nil {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	view := NewStateView(t.committed, t.tx)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid := range view.SeekIndexBounds(tableID, indexID, Bound(lower), Bound(upper)) {
			row, ok := view.GetRow(tableID, rid)
			if !ok {
				continue
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}
