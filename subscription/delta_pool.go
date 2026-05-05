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
	distinct   map[valueKey]Value
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
			distinct:   make(map[valueKey]Value),
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
		return make(map[ColID]map[valueKey][]int)
	},
}

var valuePositionIndexPool = sync.Pool{
	New: func() any {
		return make(map[valueKey][]int)
	},
}

var deltaViewPool = sync.Pool{
	New: func() any {
		return &DeltaView{
			inserts: make(map[TableID][]types.ProductValue),
			deletes: make(map[TableID][]types.ProductValue),
			deltaIdx: DeltaIndexes{
				insertIdx: make(map[TableID]map[ColID]map[valueKey][]int),
				deleteIdx: make(map[TableID]map[ColID]map[valueKey][]int),
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
	clear(st.candidates)
	clear(st.distinct)
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
	clear(rows)
	rows = rows[:0]
	productValueSlicePool.Put(&rows)
}

func acquireTableDeltaIndex() map[ColID]map[valueKey][]int {
	return tableDeltaIndexPool.Get().(map[ColID]map[valueKey][]int)
}

func releaseTableDeltaIndex(byCol map[ColID]map[valueKey][]int) {
	for _, byVal := range byCol {
		releaseValuePositionIndex(byVal)
	}
	clear(byCol)
	tableDeltaIndexPool.Put(byCol)
}

func acquireValuePositionIndex() map[valueKey][]int {
	return valuePositionIndexPool.Get().(map[valueKey][]int)
}

func releaseValuePositionIndex(byVal map[valueKey][]int) {
	clear(byVal)
	valuePositionIndexPool.Put(byVal)
}

func acquireDeltaView() *DeltaView {
	dv := deltaViewPool.Get().(*DeltaView)
	dv.committed = nil
	return dv
}

func releaseDeltaView(dv *DeltaView) {
	for _, rows := range dv.inserts {
		releaseProductValueSlice(rows)
	}
	clear(dv.inserts)
	for _, rows := range dv.deletes {
		releaseProductValueSlice(rows)
	}
	clear(dv.deletes)
	for _, byCol := range dv.deltaIdx.insertIdx {
		releaseTableDeltaIndex(byCol)
	}
	clear(dv.deltaIdx.insertIdx)
	for _, byCol := range dv.deltaIdx.deleteIdx {
		releaseTableDeltaIndex(byCol)
	}
	clear(dv.deltaIdx.deleteIdx)
	dv.committed = nil
	deltaViewPool.Put(dv)
}

// clear empties all internal maps while preserving capacity.
func (s *dedupState) clear() {
	clear(s.insertCounts)
	clear(s.insertRows)
	clear(s.insertOrder)
	s.insertOrder = s.insertOrder[:0]
	clear(s.deleteCounts)
	clear(s.deleteRows)
	clear(s.deleteOrder)
	s.deleteOrder = s.deleteOrder[:0]
}
