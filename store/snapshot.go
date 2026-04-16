package store

import (
	"iter"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// CommittedReadView is a read-only point-in-time snapshot.
// Close() must be called when done to release the read lock.
type CommittedReadView interface {
	TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue]
	IndexScan(tableID schema.TableID, indexID schema.IndexID, value types.Value) iter.Seq2[types.RowID, types.ProductValue]
	IndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper Bound) iter.Seq2[types.RowID, types.ProductValue]
	IndexSeek(tableID schema.TableID, indexID schema.IndexID, key IndexKey) []types.RowID
	GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool)
	RowCount(tableID schema.TableID) int
	Close()
}

// CommittedSnapshot holds a read lock on CommittedState.
type CommittedSnapshot struct {
	cs     *CommittedState
	closed bool
}

// Snapshot acquires a read lock and returns a point-in-time view.
func (cs *CommittedState) Snapshot() *CommittedSnapshot {
	cs.RLock()
	return &CommittedSnapshot{cs: cs}
}

func (s *CommittedSnapshot) ensureOpen() {
	if s.closed {
		panic("store: CommittedSnapshot used after Close")
	}
}

func (s *CommittedSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	s.ensureOpen()
	t, ok := s.cs.Table(id)
	if !ok {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return t.Scan()
}

func (s *CommittedSnapshot) IndexScan(tableID schema.TableID, indexID schema.IndexID, value types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	s.ensureOpen()
	t, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return s.rowsFromRowIDs(t, idx.Seek(NewIndexKey(value)))
}

func (s *CommittedSnapshot) IndexSeek(tableID schema.TableID, indexID schema.IndexID, key IndexKey) []types.RowID {
	s.ensureOpen()
	_, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return nil
	}
	return idx.Seek(key)
}

func (s *CommittedSnapshot) IndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper Bound) iter.Seq2[types.RowID, types.ProductValue] {
	s.ensureOpen()
	t, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid := range idx.BTree().Scan() {
			row, ok := t.GetRow(rid)
			if !ok {
				continue
			}
			key := ExtractKey(row, idx.schema.Columns)
			if !matchesLowerBound(key, lower) || !matchesUpperBound(key, upper) {
				continue
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}

func matchesLowerBound(key IndexKey, bound Bound) bool {
	if bound.Unbounded {
		return true
	}
	cmp := key.Compare(NewIndexKey(bound.Value))
	if bound.Inclusive {
		return cmp >= 0
	}
	return cmp > 0
}

func matchesUpperBound(key IndexKey, bound Bound) bool {
	if bound.Unbounded {
		return true
	}
	cmp := key.Compare(NewIndexKey(bound.Value))
	if bound.Inclusive {
		return cmp <= 0
	}
	return cmp < 0
}

func (s *CommittedSnapshot) lookupIndex(tableID schema.TableID, indexID schema.IndexID) (*Table, *Index, bool) {
	t, ok := s.cs.Table(tableID)
	if !ok {
		return nil, nil, false
	}
	idx := t.IndexByID(indexID)
	if idx == nil {
		return nil, nil, false
	}
	return t, idx, true
}

func (s *CommittedSnapshot) rowsFromRowIDs(t *Table, rowIDs []types.RowID) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for _, rid := range rowIDs {
			row, ok := t.GetRow(rid)
			if !ok {
				continue
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}

func (s *CommittedSnapshot) GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool) {
	s.ensureOpen()
	t, ok := s.cs.Table(tableID)
	if !ok {
		return nil, false
	}
	return t.GetRow(rowID)
}

func (s *CommittedSnapshot) RowCount(tableID schema.TableID) int {
	s.ensureOpen()
	t, ok := s.cs.Table(tableID)
	if !ok {
		return 0
	}
	return t.RowCount()
}

func (s *CommittedSnapshot) Close() {
	if !s.closed {
		s.cs.RUnlock()
		s.closed = true
	}
}
