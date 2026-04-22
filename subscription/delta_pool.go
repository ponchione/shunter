package subscription

import (
	"sync"

	"github.com/ponchione/shunter/types"
)

const pooledBufferDefaultCap = 4 * 1024

// dedupState is the reusable bag-dedup scratch state. It holds the insert
// and delete count maps so they can be cleared and reused across transactions
// (SPEC-004 §9.2). Maps are the dominant allocation in ReconcileJoinDelta.
type dedupState struct {
	insertCounts map[string]int
	insertRows   map[string]types.ProductValue
	insertOrder  []string
	deleteCounts map[string]int
	deleteRows   map[string]types.ProductValue
	deleteOrder  []string
}

// candidateScratch is the reusable candidate-collection scratch state used by
// the evaluation loop to avoid re-allocating hot-path maps per transaction.
type candidateScratch struct {
	candidates map[QueryHash]struct{}
	distinct   map[string]Value
}

var dedupPool = sync.Pool{
	New: func() any {
		return &dedupState{
			insertCounts: make(map[string]int),
			insertRows:   make(map[string]types.ProductValue),
			insertOrder:  make([]string, 0, 8),
			deleteCounts: make(map[string]int),
			deleteRows:   make(map[string]types.ProductValue),
			deleteOrder:  make([]string, 0, 8),
		}
	},
}

var pooledBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, pooledBufferDefaultCap)
		return &buf
	},
}

var candidateScratchPool = sync.Pool{
	New: func() any {
		return &candidateScratch{
			candidates: make(map[QueryHash]struct{}),
			distinct:   make(map[string]Value),
		}
	},
}

var productValueSlicePool = sync.Pool{
	New: func() any {
		s := make([]types.ProductValue, 0)
		return &s
	},
}

var tableDeltaIndexPool = sync.Pool{
	New: func() any {
		return make(map[ColID]map[string][]int)
	},
}

var valuePositionIndexPool = sync.Pool{
	New: func() any {
		return make(map[string][]int)
	},
}

var deltaViewPool = sync.Pool{
	New: func() any {
		return &DeltaView{
			inserts: make(map[TableID][]types.ProductValue),
			deletes: make(map[TableID][]types.ProductValue),
			deltaIdx: DeltaIndexes{
				insertIdx: make(map[TableID]map[ColID]map[string][]int),
				deleteIdx: make(map[TableID]map[ColID]map[string][]int),
			},
		}
	},
}

func acquirePooledBuffer() []byte {
	buf := (*pooledBufferPool.Get().(*[]byte))[:0]
	if cap(buf) < pooledBufferDefaultCap {
		return make([]byte, 0, pooledBufferDefaultCap)
	}
	return buf
}

func releasePooledBuffer(buf []byte) {
	if cap(buf) != pooledBufferDefaultCap {
		if cap(buf) > pooledBufferDefaultCap {
			return
		}
		buf = make([]byte, 0, pooledBufferDefaultCap)
	}
	buf = buf[:0]
	pooledBufferPool.Put(&buf)
}

func acquireCandidateScratch() *candidateScratch {
	st := candidateScratchPool.Get().(*candidateScratch)
	return st
}

func releaseCandidateScratch(st *candidateScratch) {
	for h := range st.candidates {
		delete(st.candidates, h)
	}
	for k := range st.distinct {
		delete(st.distinct, k)
	}
	candidateScratchPool.Put(st)
}

func acquireProductValueSlice(minCap int) []types.ProductValue {
	s := (*productValueSlicePool.Get().(*[]types.ProductValue))[:0]
	if cap(s) < minCap {
		return make([]types.ProductValue, 0, minCap)
	}
	return s
}

func releaseProductValueSlice(rows []types.ProductValue) {
	for i := range rows {
		rows[i] = nil
	}
	rows = rows[:0]
	productValueSlicePool.Put(&rows)
}

func acquireTableDeltaIndex() map[ColID]map[string][]int {
	return tableDeltaIndexPool.Get().(map[ColID]map[string][]int)
}

func releaseTableDeltaIndex(byCol map[ColID]map[string][]int) {
	for col, byVal := range byCol {
		releaseValuePositionIndex(byVal)
		delete(byCol, col)
	}
	tableDeltaIndexPool.Put(byCol)
}

func acquireValuePositionIndex() map[string][]int {
	return valuePositionIndexPool.Get().(map[string][]int)
}

func releaseValuePositionIndex(byVal map[string][]int) {
	for key, positions := range byVal {
		byVal[key] = positions[:0]
		delete(byVal, key)
	}
	valuePositionIndexPool.Put(byVal)
}

func acquireDeltaView() *DeltaView {
	dv := deltaViewPool.Get().(*DeltaView)
	dv.committed = nil
	return dv
}

func releaseDeltaView(dv *DeltaView) {
	for table, rows := range dv.inserts {
		releaseProductValueSlice(rows)
		delete(dv.inserts, table)
	}
	for table, rows := range dv.deletes {
		releaseProductValueSlice(rows)
		delete(dv.deletes, table)
	}
	for table, byCol := range dv.deltaIdx.insertIdx {
		releaseTableDeltaIndex(byCol)
		delete(dv.deltaIdx.insertIdx, table)
	}
	for table, byCol := range dv.deltaIdx.deleteIdx {
		releaseTableDeltaIndex(byCol)
		delete(dv.deltaIdx.deleteIdx, table)
	}
	dv.committed = nil
	deltaViewPool.Put(dv)
}

// clear empties all internal maps while preserving capacity.
func (s *dedupState) clear() {
	for k := range s.insertCounts {
		delete(s.insertCounts, k)
	}
	for k := range s.insertRows {
		delete(s.insertRows, k)
	}
	for i := range s.insertOrder {
		s.insertOrder[i] = ""
	}
	s.insertOrder = s.insertOrder[:0]
	for k := range s.deleteCounts {
		delete(s.deleteCounts, k)
	}
	for k := range s.deleteRows {
		delete(s.deleteRows, k)
	}
	for i := range s.deleteOrder {
		s.deleteOrder[i] = ""
	}
	s.deleteOrder = s.deleteOrder[:0]
}
