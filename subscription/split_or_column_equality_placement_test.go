package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestPlaceJoinSplitOrColumnEqualityBranchUsesExistenceEdge(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
		2: types.KindUint64,
	}, 0, 2)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
		2: types.KindUint64,
	}, 0, 2)
	idx := NewPruningIndexes()
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColEqCol{
				LeftTable:   1,
				LeftColumn:  2,
				RightTable:  2,
				RightColumn: 2,
			},
		},
	}
	hash := hashN(1)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 1, types.NewString("active")); len(got) != 1 || got[0] != hash {
		t.Fatalf("left split-OR local value placement = %v, want [%v]", got, hash)
	}
	eqEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 2, RHSJoinCol: 2, RHSFilterCol: 2}
	if _, ok := idx.JoinEdge.exists[eqEdge][hash]; !ok {
		t.Fatalf("column-equality branch edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for split-OR column equality", got)
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewString("other"), types.NewUint64(8)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(1), types.NewString("inactive"), types.NewUint64(7)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched split-OR column-equality candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewString("other"), types.NewUint64(7)}},
	})
	got := CollectCandidatesForTable(idx, 1, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching split-OR column-equality candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrColumnEqualityBranchUsesExistenceEdge(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
		Right: ColEqCol{
			LeftTable:   2,
			LeftColumn:  0,
			LeftAlias:   1,
			RightTable:  3,
			RightColumn: 0,
			RightAlias:  2,
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	valueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(valueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("multi-way split-OR local branch edge = %v, want [%v]", got, hash)
	}
	eqEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 0}
	if _, ok := idx.JoinEdge.exists[eqEdge][hash]; !ok {
		t.Fatalf("multi-way column-equality branch edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for split-OR column equality", got)
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(9), types.NewUint64(99)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched multi-way split-OR column-equality candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(8), types.NewUint64(99)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching multi-way split-OR column-equality candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}
