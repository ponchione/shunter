package store

import (
	"iter"
	"log"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// CommittedReadView is a read-only point-in-time snapshot.
// Close() must be called promptly when done to release the read lock.
// While a snapshot remains open, commits block on the CommittedState write
// lock. A leaked snapshot can therefore stall all commits until it is closed.
// v1 installs a best-effort finalizer to mitigate unreachable leaks, but that
// is only a last resort after GC runs — callers must still close explicitly.
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
	cs        *CommittedState
	closed    atomic.Bool
	closeOnce sync.Once
}

// Snapshot acquires a read lock and returns a point-in-time view.
func (cs *CommittedState) Snapshot() *CommittedSnapshot {
	cs.RLock()
	s := &CommittedSnapshot{cs: cs}
	runtime.SetFinalizer(s, finalizeCommittedSnapshot)
	return s
}

func (s *CommittedSnapshot) ensureOpen() {
	if s.closed.Load() {
		panic("store: CommittedSnapshot used after Close")
	}
}

func finalizeCommittedSnapshot(s *CommittedSnapshot) {
	log.Printf("store: leaked CommittedSnapshot finalized without Close(); commits may have been blocked until GC")
	s.close(true)
}

func (s *CommittedSnapshot) close(fromFinalizer bool) {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if !fromFinalizer {
			runtime.SetFinalizer(s, nil)
		}
		s.cs.RUnlock()
	})
}

func (s *CommittedSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	s.ensureOpen()
	t, ok := s.cs.Table(id)
	if !ok {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	inner := t.Scan()
	return func(yield func(types.RowID, types.ProductValue) bool) {
		defer runtime.KeepAlive(s)
		s.ensureOpen()
		for rid, row := range inner {
			// OI-005 mid-iter-close defense-in-depth: if another
			// caller has Closed this snapshot after iter body entry,
			// halt with the same deterministic panic rather than
			// continuing to yield rows against a released RLock.
			s.ensureOpen()
			if !yield(rid, row) {
				return
			}
		}
	}
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
	// OI-005 shared-state escape route closure: the underlying BTreeIndex.Seek
	// returns a live alias of the index entry's internal []RowID. A caller
	// that retained that slice past Close() would race any subsequent writer's
	// Insert/Remove on the same key (slices.Insert / slices.Delete mutate the
	// backing array). Clone at this public read-view boundary so callers
	// cannot alias BTree-internal storage. Pinned by
	// snapshot_indexseek_aliasing_test.go.
	ids := idx.Seek(key)
	if len(ids) == 0 {
		return nil
	}
	return slices.Clone(ids)
}

func (s *CommittedSnapshot) IndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper Bound) iter.Seq2[types.RowID, types.ProductValue] {
	s.ensureOpen()
	t, idx, ok := s.lookupIndex(tableID, indexID)
	if !ok {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	// SPEC-001 §7.2: IndexRange delegates to Index.SeekBounds so
	// string/bytes/float exclusive-bound predicates hit the BTree's
	// binary-search start point instead of a full ordered scan with
	// per-row Bound filter.
	//
	// OI-005 sub-hazard pin: BTreeIndex.SeekBounds is an iter.Seq walking
	// b.entries live. Under single-writer discipline no concurrent writer
	// runs while a CommittedSnapshot holds RLock, but a yield callback
	// reaching into a mutating path (future refactor, direct CommittedState
	// access from within a reducer) could shift b.entries in place
	// (slices.Delete when a key's last RowID is removed) and drift the
	// outer iteration. Collecting the range once at the CommittedReadView
	// boundary decouples iteration from BTree-internal storage, mirroring
	// the StateView.SeekIndexBounds precedent
	// (state_view_seekindexbounds_test.go aliasing pin).
	return func(yield func(types.RowID, types.ProductValue) bool) {
		defer runtime.KeepAlive(s)
		s.ensureOpen()
		for _, rid := range slices.Collect(idx.BTree().SeekBounds(lower, upper)) {
			// OI-005 mid-iter-close defense-in-depth: see TableScan.
			s.ensureOpen()
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
		defer runtime.KeepAlive(s)
		s.ensureOpen()
		for _, rid := range rowIDs {
			// OI-005 mid-iter-close defense-in-depth: see TableScan.
			s.ensureOpen()
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
	s.close(false)
}
