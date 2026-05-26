package subscription

import (
	"sync"

	"github.com/ponchione/shunter/types"
)

const (
	pooledBufferDefaultCap        = 4 * 1024
	pooledProductValueSliceMaxCap = 4096
	pooledScratchMapMaxLen        = 4096
	distinctChangedValueLinearMax = 16
	rowCountMapHintMax            = 256
)

// dedupState is the reusable bag-dedup scratch state. It holds the insert
// and delete count maps so they can be cleared and reused across transactions
// (SPEC-004 §9.2). Maps are the dominant allocation in ReconcileJoinDelta.
type dedupState struct {
	insertRows  map[uint64]countedRowBucket
	insertOrder []countedRowRef
	deleteRows  map[uint64]countedRowBucket
	deleteOrder []countedRowRef
}

// candidateScratch is the reusable candidate-collection scratch state used by
// the evaluation loop to avoid re-allocating hot-path maps per transaction.
type candidateScratch struct {
	candidates   map[QueryHash]struct{}
	distinct     map[valueKey]Value
	distinctKeys []valueKey
}

var dedupPool = sync.Pool{
	New: func() any {
		return &dedupState{
			insertRows:  make(map[uint64]countedRowBucket),
			insertOrder: make([]countedRowRef, 0, 8),
			deleteRows:  make(map[uint64]countedRowBucket),
			deleteOrder: make([]countedRowRef, 0, 8),
		}
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
			events:  make(map[TableID]bool),
			deltaIdx: DeltaIndexes{
				insertIdx: make(map[TableID]map[ColID]map[valueKey][]int),
				deleteIdx: make(map[TableID]map[ColID]map[valueKey][]int),
			},
		}
	},
}

func acquireCandidateScratch() *candidateScratch {
	st := candidateScratchPool.Get().(*candidateScratch)
	return st
}

func releaseCandidateScratch(st *candidateScratch) {
	if len(st.candidates) > pooledScratchMapMaxLen {
		st.candidates = make(map[QueryHash]struct{})
	} else {
		clear(st.candidates)
	}
	if len(st.distinct) > pooledScratchMapMaxLen {
		st.distinct = make(map[valueKey]Value)
	} else {
		clear(st.distinct)
	}
	if cap(st.distinctKeys) > pooledProductValueSliceMaxCap {
		st.distinctKeys = nil
	} else {
		clear(st.distinctKeys)
		st.distinctKeys = st.distinctKeys[:0]
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
	if cap(rows) > pooledProductValueSliceMaxCap {
		return
	}
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
	if len(byCol) > pooledScratchMapMaxLen {
		return
	}
	clear(byCol)
	tableDeltaIndexPool.Put(byCol)
}

func acquireValuePositionIndex() map[valueKey][]int {
	return valuePositionIndexPool.Get().(map[valueKey][]int)
}

func releaseValuePositionIndex(byVal map[valueKey][]int) {
	if len(byVal) > pooledScratchMapMaxLen {
		return
	}
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
	clear(dv.events)
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
	if len(s.insertRows) > pooledScratchMapMaxLen {
		s.insertRows = make(map[uint64]countedRowBucket)
	} else {
		clear(s.insertRows)
	}
	if cap(s.insertOrder) > pooledProductValueSliceMaxCap {
		s.insertOrder = make([]countedRowRef, 0, 8)
	} else {
		clear(s.insertOrder)
		s.insertOrder = s.insertOrder[:0]
	}
	if len(s.deleteRows) > pooledScratchMapMaxLen {
		s.deleteRows = make(map[uint64]countedRowBucket)
	} else {
		clear(s.deleteRows)
	}
	if cap(s.deleteOrder) > pooledProductValueSliceMaxCap {
		s.deleteOrder = make([]countedRowRef, 0, 8)
	} else {
		clear(s.deleteOrder)
		s.deleteOrder = s.deleteOrder[:0]
	}
}
