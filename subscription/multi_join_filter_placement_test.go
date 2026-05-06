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

func dualIndexedMultiJoinTestSchema() *fakeSchema {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 0, 1)
	s.addTable(2, cols, 0, 1)
	s.addTable(3, cols, 0, 1)
	return s
}

func disjunctiveFilterColumnEqualityMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEqCol{
				LeftTable:   1,
				LeftColumn:  1,
				LeftAlias:   0,
				RightTable:  2,
				RightColumn: 1,
				RightAlias:  1,
			},
			Right: ColEqCol{
				LeftTable:   1,
				LeftColumn:  0,
				LeftAlias:   0,
				RightTable:  2,
				RightColumn: 0,
				RightAlias:  1,
			},
		},
	}
}

func partiallyDisjunctiveFilterColumnEqualityMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEqCol{
				LeftTable:   1,
				LeftColumn:  1,
				LeftAlias:   0,
				RightTable:  2,
				RightColumn: 1,
				RightAlias:  1,
			},
			Right: ColEqCol{
				LeftTable:   1,
				LeftColumn:  0,
				LeftAlias:   0,
				RightTable:  3,
				RightColumn: 0,
				RightAlias:  2,
			},
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

func TestMultiJoinPlacementDisjunctiveFilterColumnEqualityFallsBackPerUncoveredRelation(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := partiallyDisjunctiveFilterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 1, RHSTable: 3, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 0},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing partially disjunctive filter edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for covered relation", got)
	}
	for _, table := range []TableID{2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want fallback [%v]", table, got, hash)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementUsesSameRelationSetDisjunctiveFilterColumnEqualityEdges(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := disjunctiveFilterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 0},
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 0},
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing disjunctive filter column-equality edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for disjunctive filter column-equality placement", table, got)
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

func TestCollectCandidatesMultiJoinSameRelationSetDisjunctiveFilterColumnEqualityPrunesMismatch(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := disjunctiveFilterColumnEqualityMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {
			{types.NewUint64(42), types.NewUint64(200)},
			{types.NewUint64(100), types.NewUint64(20)},
		},
	})

	mismatch := []types.ProductValue{{types.NewUint64(9), types.NewUint64(99)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched disjunctive filter column-equality candidates = %v, want empty", got)
	}

	firstColumnMatch := []types.ProductValue{{types.NewUint64(42), types.NewUint64(99)}}
	got := CollectCandidatesForTable(idx, 1, firstColumnMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("first-column disjunctive filter column-equality candidates = %v, want [%v]", got, hash)
	}

	secondColumnMatch := []types.ProductValue{{types.NewUint64(9), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, secondColumnMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("second-column disjunctive filter column-equality candidates = %v, want [%v]", got, hash)
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

func TestMultiJoinPlacementKeepsMixedBranchKindDisjunctiveFilterColumnEqualityFallback(t *testing.T) {
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
		t.Fatalf("mixed branch-kind disjunctive edge present: %+v", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want fallback [%v]", table, got, hash)
		}
	}
}

func TestMultiJoinPlacementUsesCommonRelationDisjunctiveFilterColumnEqualityEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := filterColumnEqualityMultiJoinPredicate()
	pred.Filter = Or{
		Left: pred.Filter,
		Right: ColEqCol{
			LeftTable:   2,
			LeftColumn:  1,
			LeftAlias:   1,
			RightTable:  3,
			RightColumn: 1,
			RightAlias:  2,
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing common-relation disjunctive edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for common relation", got)
	}
	for _, table := range []TableID{1, 3} {
		if got := idx.Table.Lookup(table); len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want fallback [%v]", table, got, hash)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinCommonRelationDisjunctiveFilterColumnEqualityPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := filterColumnEqualityMultiJoinPredicate()
	pred.Filter = Or{
		Left: pred.Filter,
		Right: ColEqCol{
			LeftTable:   2,
			LeftColumn:  1,
			LeftAlias:   1,
			RightTable:  3,
			RightColumn: 1,
			RightAlias:  2,
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewUint64(20)}},
		3: {{types.NewUint64(3), types.NewUint64(30)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(2), types.NewUint64(99)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched common-relation disjunctive candidates = %v, want empty", got)
	}

	leftBranchMatch := []types.ProductValue{{types.NewUint64(2), types.NewUint64(20)}}
	got := CollectCandidatesForTable(idx, 2, leftBranchMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left-branch common-relation disjunctive candidates = %v, want [%v]", got, hash)
	}

	rightBranchMatch := []types.ProductValue{{types.NewUint64(2), types.NewUint64(30)}}
	got = CollectCandidatesForTable(idx, 2, rightBranchMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right-branch common-relation disjunctive candidates = %v, want [%v]", got, hash)
	}
}
