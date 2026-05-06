package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func splitOrLocalFilterMultiJoinPredicate() MultiJoin {
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColEq{
			Table:  1,
			Column: 0,
			Alias:  0,
			Value:  types.NewUint64(7),
		},
		Right: ColRange{
			Table:  2,
			Column: 0,
			Alias:  1,
			Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
	return pred
}

func TestMultiJoinPlacementUsesSplitOrLocalFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrLocalFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left split-OR local value placement = %v, want [%v]", got, hash)
	}
	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left split-OR range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(2, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right split-OR local range placement = %v, want [%v]", got, hash)
	}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right split-OR value edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinEdge.exists[leftRangeEdge]; len(got) != 0 {
		t.Fatalf("left split-OR broad existence candidates = %v, want none", got)
	}
	if got := idx.JoinEdge.exists[rightValueEdge]; len(got) != 0 {
		t.Fatalf("right split-OR broad existence candidates = %v, want none", got)
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for split-OR filter placement", table, got)
		}
	}
	if got := idx.Table.Lookup(3); len(got) != 0 {
		t.Fatalf("TableIndex[3] = %v, want existing condition-edge placement", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrFallsBackWhenDirectEdgeUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols)
	s.addTable(3, cols, 1)
	idx := NewPruningIndexes()
	pred := splitOrLocalFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial left local split-OR placement = %v, want empty", got)
	}
	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("unindexed left range-edge placement = %v, want empty", got)
	}
	if got := idx.Range.Lookup(2, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right local split-OR range placement = %v, want [%v]", got, hash)
	}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("indexed right value-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for covered relation", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrColNeBranchUsesRangeEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColNe{
			Table:  1,
			Column: 0,
			Alias:  0,
			Value:  types.NewUint64(7),
		},
		Right: ColRange{
			Table:  2,
			Column: 0,
			Alias:  1,
			Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Range.Lookup(1, 0, types.NewUint64(6)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left ColNe lower range placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("left ColNe rejected value placement = %v, want empty", got)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(8)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left ColNe upper range placement = %v, want [%v]", got, hash)
	}
	rightRangeEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(rightRangeEdge, types.NewUint64(6)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right ColNe lower range-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(rightRangeEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("right ColNe rejected range-edge placement = %v, want empty", got)
	}
	if got := idx.JoinRangeEdge.Lookup(rightRangeEdge, types.NewUint64(8)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right ColNe upper range-edge placement = %v, want [%v]", got, hash)
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("right ColNe edge rejected candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right ColNe edge candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrLocalFilterPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrLocalFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched split-OR left candidates = %v, want empty", got)
	}

	localMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(99)}}
	got := CollectCandidatesForTable(idx, 1, localMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left local split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	edgeMatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, edgeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left edge split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	mismatch = []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched split-OR right candidates = %v, want empty", got)
	}

	localRangeMatch := []types.ProductValue{{types.NewUint64(60), types.NewUint64(99)}}
	got = CollectCandidatesForTable(idx, 2, localRangeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right local split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	edgeMatch = []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 2, edgeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right edge split-OR candidates = %v, want [%v]", got, hash)
	}
}
