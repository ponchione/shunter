package store

import (
	"fmt"
	"slices"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Commit applies the transaction's mutations to committed state and produces a changeset.
// It revalidates constraints under the committed-state write lock before mutating state.
func Commit(cs *CommittedState, tx *Transaction) (*Changeset, error) {
	switch tx.state.Load() {
	case transactionRolledBack:
		return nil, ErrTransactionRolledBack
	case transactionCommitted:
		return nil, ErrTransactionClosed
	}
	cs.Lock()
	defer cs.Unlock()

	txState := tx.tx
	if err := revalidateCommit(cs, txState); err != nil {
		return nil, err
	}
	changeset := &Changeset{
		TxID:   0,
		Tables: make(map[schema.TableID]*TableChangeset),
	}

	deletesByTable := txState.allTableDeletes()
	for _, tableID := range sortedCommitMapKeys(deletesByTable) {
		dels := deletesByTable[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		tc := ensureTableChangeset(changeset, tableID, table.schema)
		for _, rowID := range sortedCommitMapKeys(dels) {
			oldRow, ok := table.DeleteRow(rowID)
			if !ok {
				return nil, fmt.Errorf("%w: %d", ErrRowNotFound, rowID)
			}
			tc.Deletes = append(tc.Deletes, oldRow)
		}
	}

	insertsByTable := txState.allTableInserts()
	for _, tableID := range sortedCommitMapKeys(insertsByTable) {
		ins := insertsByTable[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		tc := ensureTableChangeset(changeset, tableID, table.schema)
		for _, rowID := range sortedCommitMapKeys(ins) {
			row := ins[rowID]
			if err := table.InsertRow(rowID, row); err != nil {
				return nil, err
			}
			tc.Inserts = append(tc.Inserts, row.Copy())
		}
	}

	for _, tableID := range sortedCommitMapKeys(tx.txNextRowIDs) {
		next := tx.txNextRowIDs[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		table.SetNextID(next)
	}

	for _, tableID := range sortedCommitMapKeys(tx.txSequences) {
		next := tx.txSequences[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		table.SetSequenceValue(next)
	}

	tx.finishCommitted()
	return changeset, nil
}

// Rollback discards the transaction. No committed state mutation.
// After rollback, all Transaction methods return errors or zero values.
func Rollback(tx *Transaction) {
	tx.state.Store(transactionRolledBack)
}

func ensureTableChangeset(cs *Changeset, id schema.TableID, ts *schema.TableSchema) *TableChangeset {
	tc := cs.Tables[id]
	if tc == nil {
		tableName := ""
		if ts != nil {
			tableName = ts.Name
		}
		tc = &TableChangeset{TableID: id, TableName: tableName, Schema: ts}
		cs.Tables[id] = tc
	} else if tc.Schema == nil {
		tc.Schema = ts
	}
	return tc
}

func revalidateCommit(cs *CommittedState, txState *TxState) error {
	deletesByTable := txState.allTableDeletes()
	for _, tableID := range sortedCommitMapKeys(deletesByTable) {
		deletes := deletesByTable[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		for _, rowID := range sortedCommitMapKeys(deletes) {
			if _, ok := table.rowView(rowID); !ok {
				return fmt.Errorf("%w: %d", ErrRowNotFound, rowID)
			}
		}
	}

	pendingUnique := make(map[txUniqueRef]map[uint64][]IndexKey)
	pendingRows := make(map[schema.TableID]map[uint64][]types.ProductValue)
	insertsByTable := txState.allTableInserts()
	for _, tableID := range sortedCommitMapKeys(insertsByTable) {
		inserts := insertsByTable[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		for _, rowID := range sortedCommitMapKeys(inserts) {
			row := inserts[rowID]
			if err := ValidateRow(table.Schema(), row); err != nil {
				return err
			}
			if err := revalidateInsertRowID(tableID, table, rowID, txState); err != nil {
				return err
			}
			if err := revalidateInsertAgainstCommitted(tableID, table, row, txState); err != nil {
				return err
			}
			if err := revalidateInsertAgainstPending(tableID, table, row, pendingUnique, pendingRows); err != nil {
				return err
			}
		}
	}
	return nil
}

func revalidateInsertRowID(tableID schema.TableID, table *Table, rowID types.RowID, txState *TxState) error {
	if txState.IsDeleted(tableID, rowID) {
		return nil
	}
	if _, exists := table.rowView(rowID); exists {
		return fmt.Errorf("%w: %d", ErrDuplicateRowID, rowID)
	}
	return nil
}

func revalidateInsertAgainstPending(tableID schema.TableID, table *Table, row types.ProductValue, pendingUnique map[txUniqueRef]map[uint64][]IndexKey, pendingRows map[schema.TableID]map[uint64][]types.ProductValue) error {
	for idxOrdinal, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		ref := txUniqueRef{table: tableID, index: idxOrdinal}
		buckets := pendingUnique[ref]
		if buckets == nil {
			continue
		}
		for _, priorKey := range buckets[key.hash64()] {
			if key.Equal(priorKey) {
				return uniqueViolationError(table, idx, key)
			}
		}
	}
	if table.rowHashIndex != nil {
		if buckets := pendingRows[tableID]; buckets != nil {
			for _, prior := range buckets[row.Hash64()] {
				if !prior.Equal(row) {
					continue
				}
				return ErrDuplicateRow
			}
		}
	}

	for idxOrdinal, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		ref := txUniqueRef{table: tableID, index: idxOrdinal}
		buckets := pendingUnique[ref]
		if buckets == nil {
			buckets = make(map[uint64][]IndexKey)
			pendingUnique[ref] = buckets
		}
		buckets[key.hash64()] = append(buckets[key.hash64()], key)
	}
	if table.rowHashIndex != nil {
		buckets := pendingRows[tableID]
		if buckets == nil {
			buckets = make(map[uint64][]types.ProductValue)
			pendingRows[tableID] = buckets
		}
		buckets[row.Hash64()] = append(buckets[row.Hash64()], row)
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
			return uniqueViolationError(table, idx, key)
		}
	}

	if table.rowHashIndex != nil {
		h := row.Hash64()
		for _, rid := range table.rowHashIndex[h] {
			if txState.IsDeleted(tableID, rid) {
				continue
			}
			if existing, ok := table.rowView(rid); ok && existing.Equal(row) {
				return ErrDuplicateRow
			}
		}
	}

	return nil
}

type orderedCommitMapKey interface {
	~uint32 | ~uint64
}

func sortedCommitMapKeys[K orderedCommitMapKey, V any](m map[K]V) []K {
	if len(m) == 0 {
		return nil
	}
	keys := make([]K, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
