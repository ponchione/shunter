package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func filterColumnEqualityMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: ColEqCol{
			LeftTable:   1,
			LeftColumn:  1,
			LeftAlias:   0,
			RightTable:  2,
			RightColumn: 1,
			RightAlias:  1,
		},
	}
}

func repeatedFilterColumnEqualityMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: ColEqCol{
			LeftTable:   1,
			LeftColumn:  1,
			LeftAlias:   0,
			RightTable:  1,
			RightColumn: 1,
			RightAlias:  1,
		},
	}
}

func TestMultiJoinPlacementUsesFilterColumnEqualityExistenceEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := filterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing filter column-equality edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for filter column-equality placement", table, got)
		}
	}
	if got := idx.Table.Lookup(3); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[3] = %v, want fallback [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinFilterColumnEqualityPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := filterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(1), types.NewUint64(9)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched filter column-equality candidates = %v, want empty", got)
	}

	match := []types.ProductValue{{types.NewUint64(1), types.NewUint64(20)}}
	got := CollectCandidatesForTable(idx, 1, match, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching filter column-equality candidates = %v, want [%v]", got, hash)
	}
}

func TestMultiJoinPlacementUsesRepeatedFilterColumnEqualityExistenceEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedFilterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing repeated filter column-equality edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for repeated filter column-equality placement", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[2] = %v, want fallback [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinRepeatedFilterColumnEqualityPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedFilterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(1), types.NewUint64(9)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated filter column-equality candidates = %v, want empty", got)
	}

	match := []types.ProductValue{{types.NewUint64(1), types.NewUint64(20)}}
	got := CollectCandidatesForTable(idx, 1, match, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching repeated filter column-equality candidates = %v, want [%v]", got, hash)
	}
}

func TestMultiJoinPlacementKeepsDisjunctiveFilterColumnEqualityFallback(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := filterColumnEqualityMultiJoinPredicate()
	pred.Filter = Or{
		Left: pred.Filter,
		Right: ColEq{
			Table:  3,
			Column: 0,
			Alias:  2,
			Value:  types.NewUint64(7),
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[edge][hash]; ok {
		t.Fatalf("disjunctive filter column-equality edge present: %+v", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want fallback [%v]", table, got, hash)
		}
	}
}
