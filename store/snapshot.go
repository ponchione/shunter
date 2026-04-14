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
	IndexSeek(tableID schema.TableID, indexID schema.IndexID, key IndexKey) []types.RowID
	IndexRange(tableID schema.TableID, indexID schema.IndexID, low, high *IndexKey) iter.Seq[types.RowID]
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

func (s *CommittedSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	t, ok := s.cs.Table(id)
	if !ok {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return t.Scan()
}

func (s *CommittedSnapshot) IndexSeek(tableID schema.TableID, indexID schema.IndexID, key IndexKey) []types.RowID {
	t, ok := s.cs.Table(tableID)
	if !ok {
		return nil
	}
	idx := t.IndexByID(indexID)
	if idx == nil {
		return nil
	}
	return idx.Seek(key)
}

func (s *CommittedSnapshot) IndexRange(tableID schema.TableID, indexID schema.IndexID, low, high *IndexKey) iter.Seq[types.RowID] {
	t, ok := s.cs.Table(tableID)
	if !ok {
		return func(func(types.RowID) bool) {}
	}
	idx := t.IndexByID(indexID)
	if idx == nil {
		return func(func(types.RowID) bool) {}
	}
	return idx.BTree().SeekRange(low, high)
}

func (s *CommittedSnapshot) GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool) {
	t, ok := s.cs.Table(tableID)
	if !ok {
		return nil, false
	}
	return t.GetRow(rowID)
}

func (s *CommittedSnapshot) RowCount(tableID schema.TableID) int {
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
