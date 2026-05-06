package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestPlaceJoinWithRequiredAndWrappedCrossSideOrUsesSplitBranchIndexes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
		3: types.KindString,
	}, 1)
	idx := NewPruningIndexes()
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: And{
			Left: Or{
				Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
				Right: ColRange{Table: 2, Column: 2,
					Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
					Upper: Bound{Unbounded: true},
				},
			},
			Right: AllRows{Table: 1},
		},
	}
	hash := hashN(1)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 0, RHSFilterCol: 1}
	if got := idx.Value.Lookup(1, 1, types.NewString("active")); len(got) != 1 || got[0] != hash {
		t.Fatalf("AND-wrapped split-OR left value placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("AND-wrapped split-OR left range-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(2, 2, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("AND-wrapped split-OR right range placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewString("active")); len(got) != 1 || got[0] != hash {
		t.Fatalf("AND-wrapped split-OR right value-edge placement = %v, want [%v]", got, hash)
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for AND-wrapped split-OR placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinAndWrappedSplitOrPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = And{
		Left: Or{
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
		},
		Right: ColEq{
			Table:  3,
			Column: 0,
			Alias:  2,
			Value:  types.NewUint64(100),
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("multi-way AND-wrapped split-OR left value placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("multi-way AND-wrapped split-OR left range-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(2, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("multi-way AND-wrapped split-OR right range placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("multi-way AND-wrapped split-OR right value-edge placement = %v, want [%v]", got, hash)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for AND-wrapped split-OR placement", table, got)
		}
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched AND-wrapped split-OR left candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 1, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching AND-wrapped split-OR left candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}
