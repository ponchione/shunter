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
	return CommitWithValidation(cs, tx, nil)
}

// CommitWithValidation applies the transaction only after validate accepts the
// exact changeset that would be committed. Validation runs under the
// committed-state write lock, after constraint revalidation and before any
// visible state mutation.
func CommitWithValidation(cs *CommittedState, tx *Transaction, validate func(*Changeset) error) (*Changeset, error) {
	return commitWithValidation(cs, tx, 0, false, validate)
}

// CommitWithValidationAtTxID applies the transaction and records txID on both
// the returned changeset and committed state under one write-lock envelope.
// Validation sees the assigned transaction ID and still runs before any
// visible state mutation.
func CommitWithValidationAtTxID(cs *CommittedState, tx *Transaction, txID types.TxID, validate func(*Changeset) error) (*Changeset, error) {
	return commitWithValidation(cs, tx, txID, true, validate)
}

func commitWithValidation(cs *CommittedState, tx *Transaction, txID types.TxID, advanceHorizon bool, validate func(*Changeset) error) (*Changeset, error) {
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
	changeset, err := prepareChangeset(cs, txState)
	if err != nil {
		return nil, err
	}
	if advanceHorizon {
		changeset.TxID = txID
	}
	if validate != nil {
		if err := validate(changeset); err != nil {
			return nil, err
		}
	}
	if err := applyCommit(cs, tx); err != nil {
		return nil, err
	}
	if advanceHorizon {
		cs.committedTxID = txID
	}

	tx.finishCommitted()
	return changeset, nil
}

func prepareChangeset(cs *CommittedState, txState *TxState) (*Changeset, error) {
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
			oldRow, ok := table.rowView(rowID)
			if !ok {
				return nil, fmt.Errorf("%w: %d", ErrRowNotFound, rowID)
			}
			tc.Deletes = append(tc.Deletes, oldRow.Copy())
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
			tc.Inserts = append(tc.Inserts, ins[rowID].Copy())
		}
	}
	return changeset, nil
}

func applyCommit(cs *CommittedState, tx *Transaction) error {
	txState := tx.tx
	deletesByTable := txState.allTableDeletes()
	for _, tableID := range sortedCommitMapKeys(deletesByTable) {
		dels := deletesByTable[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		for _, rowID := range sortedCommitMapKeys(dels) {
			if _, ok := table.DeleteRow(rowID); !ok {
				return fmt.Errorf("%w: %d", ErrRowNotFound, rowID)
			}
		}
	}

	insertsByTable := txState.allTableInserts()
	for _, tableID := range sortedCommitMapKeys(insertsByTable) {
		ins := insertsByTable[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		if table.schema.IsEvent {
			continue
		}
		for _, rowID := range sortedCommitMapKeys(ins) {
			if err := table.InsertRow(rowID, ins[rowID]); err != nil {
				return err
			}
		}
	}

	for _, tableID := range sortedCommitMapKeys(tx.txNextRowIDs) {
		next := tx.txNextRowIDs[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		if table.schema.IsEvent {
			continue
		}
		table.SetNextID(next)
	}

	for _, tableID := range sortedCommitMapKeys(tx.txSequences) {
		next := tx.txSequences[tableID]
		table, ok := cs.tableLocked(tableID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrTableNotFound, tableID)
		}
		if table.schema.IsEvent {
			continue
		}
		table.SetSequenceValue(next)
	}
	return nil
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
	if table.schema.IsEvent {
		return nil
	}
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
		hash := key.hash64()
		if slices.ContainsFunc(buckets[hash], key.Equal) {
			return uniqueViolationError(table, idx, key)
		}
		if buckets == nil {
			buckets = make(map[uint64][]IndexKey)
			pendingUnique[ref] = buckets
		}
		buckets[hash] = append(buckets[hash], key)
	}
	if table.rowHashIndex != nil {
		hash := row.Hash64()
		if buckets := pendingRows[tableID]; buckets != nil {
			for _, prior := range buckets[hash] {
				if !prior.Equal(row) {
					continue
				}
				return ErrDuplicateRow
			}
		}
		buckets := pendingRows[tableID]
		if buckets == nil {
			buckets = make(map[uint64][]types.ProductValue)
			pendingRows[tableID] = buckets
		}
		buckets[hash] = append(buckets[hash], row)
	}
	return nil
}

func revalidateInsertAgainstCommitted(tableID schema.TableID, table *Table, row types.ProductValue, txState *TxState) error {
	if table.schema.IsEvent {
		return nil
	}
	for _, idx := range table.indexes {
		if !idx.schema.Unique {
			continue
		}
		key := idx.ExtractKey(row)
		for _, rid := range idx.btree.rowIDs(key) {
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
