package store

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// TxState holds transaction-local insert and delete buffers.
type TxState struct {
	inserts map[schema.TableID]map[types.RowID]types.ProductValue
	deletes map[schema.TableID]map[types.RowID]struct{}
}

// NewTxState creates an empty transaction state.
func NewTxState() *TxState {
	return &TxState{
		inserts: make(map[schema.TableID]map[types.RowID]types.ProductValue),
		deletes: make(map[schema.TableID]map[types.RowID]struct{}),
	}
}

// AddInsert records a tx-local insert.
func (tx *TxState) AddInsert(tableID schema.TableID, id types.RowID, row types.ProductValue) {
	m := tx.inserts[tableID]
	if m == nil {
		m = make(map[types.RowID]types.ProductValue)
		tx.inserts[tableID] = m
	}
	m[id] = row
}

// RemoveInsert removes a tx-local insert (for delete-of-tx-insert collapse).
func (tx *TxState) RemoveInsert(tableID schema.TableID, id types.RowID) {
	if m := tx.inserts[tableID]; m != nil {
		delete(m, id)
	}
}

// AddDelete records that a committed row should be deleted.
func (tx *TxState) AddDelete(tableID schema.TableID, id types.RowID) {
	m := tx.deletes[tableID]
	if m == nil {
		m = make(map[types.RowID]struct{})
		tx.deletes[tableID] = m
	}
	m[id] = struct{}{}
}

// CancelDelete removes a pending delete (for undelete optimization).
func (tx *TxState) CancelDelete(tableID schema.TableID, id types.RowID) {
	if m := tx.deletes[tableID]; m != nil {
		delete(m, id)
	}
}

// IsInserted returns whether a row was inserted in this tx.
func (tx *TxState) IsInserted(tableID schema.TableID, id types.RowID) bool {
	m := tx.inserts[tableID]
	if m == nil {
		return false
	}
	_, ok := m[id]
	return ok
}

// IsDeleted returns whether a committed row is marked for deletion.
func (tx *TxState) IsDeleted(tableID schema.TableID, id types.RowID) bool {
	m := tx.deletes[tableID]
	if m == nil {
		return false
	}
	_, ok := m[id]
	return ok
}

// Inserts returns all tx-local inserts for a table.
func (tx *TxState) Inserts(tableID schema.TableID) map[types.RowID]types.ProductValue {
	return tx.inserts[tableID]
}

// Deletes returns all pending deletes for a table.
func (tx *TxState) Deletes(tableID schema.TableID) map[types.RowID]struct{} {
	return tx.deletes[tableID]
}

// AllInserts returns all tables' inserts.
func (tx *TxState) AllInserts() map[schema.TableID]map[types.RowID]types.ProductValue {
	return tx.inserts
}

// AllDeletes returns all tables' deletes.
func (tx *TxState) AllDeletes() map[schema.TableID]map[types.RowID]struct{} {
	return tx.deletes
}
