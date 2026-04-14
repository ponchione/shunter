package subscription

import (
	"sync"

	"github.com/ponchione/shunter/types"
)

// dedupState is the reusable bag-dedup scratch state. It holds the insert
// and delete count maps so they can be cleared and reused across transactions
// (SPEC-004 §9.2). Maps are the dominant allocation in ReconcileJoinDelta.
type dedupState struct {
	insertCounts map[string]int
	insertRows   map[string]types.ProductValue
	deleteCounts map[string]int
	deleteRows   map[string]types.ProductValue
}

var dedupPool = sync.Pool{
	New: func() any {
		return &dedupState{
			insertCounts: make(map[string]int),
			insertRows:   make(map[string]types.ProductValue),
			deleteCounts: make(map[string]int),
			deleteRows:   make(map[string]types.ProductValue),
		}
	},
}

// clear empties all internal maps while preserving capacity.
func (s *dedupState) clear() {
	for k := range s.insertCounts {
		delete(s.insertCounts, k)
	}
	for k := range s.insertRows {
		delete(s.insertRows, k)
	}
	for k := range s.deleteCounts {
		delete(s.deleteCounts, k)
	}
	for k := range s.deleteRows {
		delete(s.deleteRows, k)
	}
}
