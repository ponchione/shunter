package store

import (
	"iter"
	"runtime"
	"sync"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// CommittedReadView is a read-only point-in-time snapshot.
// Call Close promptly; open snapshots hold a read lock that blocks commits.
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
	cs       *CommittedState
	observer Observer

	lifecycleMu  sync.Mutex
	closed       bool
	activeReads  uint64
	lockReleased bool
}

// Snapshot acquires a read lock and returns a point-in-time view.
func (cs *CommittedState) Snapshot() *CommittedSnapshot {
	cs.RLock()
	s := &CommittedSnapshot{cs: cs, observer: cs.observer}
	runtime.SetFinalizer(s, finalizeCommittedSnapshot)
	return s
}

func (s *CommittedSnapshot) ensureOpen() {
	s.lifecycleMu.Lock()
	closed := s.closed
	s.lifecycleMu.Unlock()
	if closed {
		panic("store: CommittedSnapshot used after Close")
	}
}

// beginRead registers an operation before it touches state protected by the
// snapshot's CommittedState read lock. Close marks the snapshot closed without
// waiting, and the final active operation releases the underlying lock.
func (s *CommittedSnapshot) beginRead() {
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		panic("store: CommittedSnapshot used after Close")
	}
	s.activeReads++
	s.lifecycleMu.Unlock()
}

func (s *CommittedSnapshot) endRead() {
	release := false
	s.lifecycleMu.Lock()
	if s.activeReads == 0 {
		s.lifecycleMu.Unlock()
		panic("store: CommittedSnapshot read lifecycle underflow")
	}
	s.activeReads--
	if s.closed && s.activeReads == 0 && !s.lockReleased {
		s.lockReleased = true
		release = true
	}
	s.lifecycleMu.Unlock()
	if release {
		s.cs.RUnlock()
	}
}

func finalizeCommittedSnapshot(s *CommittedSnapshot) {
	if s.close(true) && s.observer != nil {
		s.observer.LogStoreSnapshotLeaked("finalizer")
	}
}

func (s *CommittedSnapshot) close(fromFinalizer bool) bool {
	release := false
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return false
	}
	s.closed = true
	if s.activeReads == 0 && !s.lockReleased {
		s.lockReleased = true
		release = true
	}
	s.lifecycleMu.Unlock()

	if !fromFinalizer {
		runtime.SetFinalizer(s, nil)
	}
	if release {
		s.cs.RUnlock()
	}
	return true
}

func (s *CommittedSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	s.beginRead()
	defer s.endRead()
	t, ok := s.cs.tableLocked(id)
	if !ok {
		return s.emptyRows()
	}
	inner := t.Scan()
	return func(yield func(types.RowID, types.ProductValue) bool) {
		s.beginRead()
		var rows uint64
		defer func() {
			s.endRead()
			recordStoreReadRows(s.observer, StoreReadKindTableScan, rows)
			runtime.KeepAlive(s)
		}()
		for rid, row := range inner {
			// Panic promptly if another caller closes the snapshot mid-iteration.
			s.ensureOpen()
			rows++
			if !yield(rid, row) {
				return
			}
			s.ensureOpen()
		}
	}
}

func (s *CommittedSnapshot) IndexScan(tableID schema.TableID, indexID schema.IndexID, value types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	s.beginRead()
	defer s.endRead()
	t, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return s.emptyRows()
	}
	return s.rowsFromRowIDs(t, idx.Seek(NewIndexKey(value)), StoreReadKindIndexScan)
}

func (s *CommittedSnapshot) IndexSeek(tableID schema.TableID, indexID schema.IndexID, key IndexKey) []types.RowID {
	s.beginRead()
	defer s.endRead()
	_, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return nil
	}
	ids := idx.Seek(key)
	recordStoreReadRows(s.observer, StoreReadKindIndexSeek, uint64(len(ids)))
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// SeekIndex yields rows whose index key exactly matches key.
func (s *CommittedSnapshot) SeekIndex(tableID schema.TableID, indexID schema.IndexID, key ...types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	s.beginRead()
	defer s.endRead()
	t, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return s.emptyRows()
	}
	return s.rowsFromRowIDs(t, idx.Seek(NewIndexKey(key...)), StoreReadKindIndexSeek)
}

func (s *CommittedSnapshot) IndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper Bound) iter.Seq2[types.RowID, types.ProductValue] {
	s.beginRead()
	defer s.endRead()
	t, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return s.emptyRows()
	}
	// Collect bounds before yielding so callbacks cannot observe BTree mutation.
	// Existing aliasing tests pin this contract; true streaming requires a
	// different immutable or versioned BTree cursor design.
	return func(yield func(types.RowID, types.ProductValue) bool) {
		s.beginRead()
		var rows uint64
		defer func() {
			s.endRead()
			recordStoreReadRows(s.observer, StoreReadKindIndexRange, rows)
			runtime.KeepAlive(s)
		}()
		for _, rid := range idx.BTree().collectBounds(lower, upper) {
			// Read-view mid-iter-close defense-in-depth: see TableScan.
			s.ensureOpen()
			row, ok := t.GetRow(rid)
			if !ok {
				continue
			}
			rows++
			if !yield(rid, row) {
				return
			}
			s.ensureOpen()
		}
	}
}

// SeekIndexRange yields rows whose index key falls between lower and upper.
func (s *CommittedSnapshot) SeekIndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper Bound) iter.Seq2[types.RowID, types.ProductValue] {
	return s.IndexRange(tableID, indexID, lower, upper)
}

func matchesLowerBound(key IndexKey, bound Bound) bool {
	if bound.Unbounded {
		return true
	}
	cmp := key.comparePrefix(indexKeyForBound(bound))
	if bound.Inclusive {
		return cmp >= 0
	}
	return cmp > 0
}

func matchesUpperBound(key IndexKey, bound Bound) bool {
	if bound.Unbounded {
		return true
	}
	cmp := key.comparePrefix(indexKeyForBound(bound))
	if bound.Inclusive {
		return cmp <= 0
	}
	return cmp < 0
}

func (s *CommittedSnapshot) lookupIndex(tableID schema.TableID, indexID schema.IndexID) (*Table, *Index, bool) {
	t, ok := s.cs.tableLocked(tableID)
	if !ok {
		return nil, nil, false
	}
	idx := t.IndexByID(indexID)
	if idx == nil {
		return nil, nil, false
	}
	return t, idx, true
}

func (s *CommittedSnapshot) rowsFromRowIDs(t *Table, rowIDs []types.RowID, readKind string) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		s.beginRead()
		var rows uint64
		defer func() {
			s.endRead()
			recordStoreReadRows(s.observer, readKind, rows)
			runtime.KeepAlive(s)
		}()
		for _, rid := range rowIDs {
			// Read-view mid-iter-close defense-in-depth: see TableScan.
			s.ensureOpen()
			row, ok := t.GetRow(rid)
			if !ok {
				continue
			}
			rows++
			if !yield(rid, row) {
				return
			}
			s.ensureOpen()
		}
	}
}

func (s *CommittedSnapshot) emptyRows() iter.Seq2[types.RowID, types.ProductValue] {
	return func(func(types.RowID, types.ProductValue) bool) {
		s.beginRead()
		defer s.endRead()
	}
}

func recordStoreReadRows(observer Observer, kind string, rows uint64) {
	if observer == nil || rows == 0 {
		return
	}
	observer.RecordStoreReadRows(kind, rows)
}

func (s *CommittedSnapshot) GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool) {
	s.beginRead()
	defer s.endRead()
	t, ok := s.cs.tableLocked(tableID)
	if !ok {
		return nil, false
	}
	return t.GetRow(rowID)
}

func (s *CommittedSnapshot) RowCount(tableID schema.TableID) int {
	s.beginRead()
	defer s.endRead()
	t, ok := s.cs.tableLocked(tableID)
	if !ok {
		return 0
	}
	return t.RowCount()
}

func (s *CommittedSnapshot) Close() {
	s.close(false)
}
