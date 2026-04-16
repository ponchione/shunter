package store

import (
	"iter"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// RowIterator is the unified row-iteration contract used by StateView.
type RowIterator = iter.Seq2[types.RowID, types.ProductValue]

// StateView merges committed state and transaction-local state into a single
// read path representing what the transaction can observe.
type StateView struct {
	committed *CommittedState
	tx        *TxState
}

// NewStateView constructs a unified read view over committed and tx-local data.
func NewStateView(committed *CommittedState, tx *TxState) *StateView {
	if tx == nil {
		tx = NewTxState()
	}
	return &StateView{committed: committed, tx: tx}
}

// GetRow returns the visible row for rowID, if any.
func (sv *StateView) GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool) {
	if row, ok := sv.tx.Inserts(tableID)[rowID]; ok {
		return row.Copy(), true
	}
	if sv.tx.IsDeleted(tableID, rowID) {
		return nil, false
	}
	if sv.committed == nil {
		return nil, false
	}
	table, ok := sv.committed.Table(tableID)
	if !ok {
		return nil, false
	}
	return table.GetRow(rowID)
}

// ScanTable yields all rows visible through the merged state.
func (sv *StateView) ScanTable(tableID schema.TableID) RowIterator {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		if sv.committed != nil {
			if table, ok := sv.committed.Table(tableID); ok {
				for id, row := range table.Scan() {
					if sv.tx.IsDeleted(tableID, id) {
						continue
					}
					if !yield(id, row) {
						return
					}
				}
			}
		}
		for id, row := range sv.tx.Inserts(tableID) {
			if !yield(id, row.Copy()) {
				return
			}
		}
	}
}

// SeekIndex returns visible row IDs whose index key exactly matches key.
func (sv *StateView) SeekIndex(tableID schema.TableID, indexID schema.IndexID, key IndexKey) iter.Seq[types.RowID] {
	return func(yield func(types.RowID) bool) {
		if sv.committed != nil {
			if table, idx, ok := sv.lookupIndex(tableID, indexID); ok {
				for _, rid := range idx.Seek(key) {
					if sv.tx.IsDeleted(tableID, rid) {
						continue
					}
					if _, ok := table.GetRow(rid); !ok {
						continue
					}
					if !yield(rid) {
						return
					}
				}
				for rid, row := range sv.tx.Inserts(tableID) {
					if idx.ExtractKey(row).Equal(key) {
						if !yield(rid) {
							return
						}
					}
				}
				return
			}
		}
	}
}

// SeekIndexRange returns visible row IDs whose keys fall in [low, high).
func (sv *StateView) SeekIndexRange(tableID schema.TableID, indexID schema.IndexID, low, high *IndexKey) iter.Seq[types.RowID] {
	return func(yield func(types.RowID) bool) {
		if sv.committed != nil {
			if table, idx, ok := sv.lookupIndex(tableID, indexID); ok {
				for rid := range idx.BTree().SeekRange(low, high) {
					if sv.tx.IsDeleted(tableID, rid) {
						continue
					}
					if _, ok := table.GetRow(rid); !ok {
						continue
					}
					if !yield(rid) {
						return
					}
				}
				for rid, row := range sv.tx.Inserts(tableID) {
					key := idx.ExtractKey(row)
					if indexKeyInRange(key, low, high) {
						if !yield(rid) {
							return
						}
					}
				}
				return
			}
		}
	}
}

func (sv *StateView) lookupIndex(tableID schema.TableID, indexID schema.IndexID) (*Table, *Index, bool) {
	if sv.committed == nil {
		return nil, nil, false
	}
	table, ok := sv.committed.Table(tableID)
	if !ok {
		return nil, nil, false
	}
	idx := table.IndexByID(indexID)
	if idx == nil {
		return nil, nil, false
	}
	return table, idx, true
}

func indexKeyInRange(key IndexKey, low, high *IndexKey) bool {
	if low != nil && key.Compare(*low) < 0 {
		return false
	}
	if high != nil && key.Compare(*high) >= 0 {
		return false
	}
	return true
}
