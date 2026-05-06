package subscription

import (
	"testing"

	"github.com/ponchione/shunter/store"
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

func splitOrMultiHopFilterMultiJoinPredicate() MultiJoin {
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColEq{
			Table:  1,
			Column: 0,
			Alias:  0,
			Value:  types.NewUint64(7),
		},
		Right: ColRange{
			Table:  3,
			Column: 0,
			Alias:  2,
			Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
	return pred
}

func splitOrAllRemoteRangeMultiJoinPredicate() MultiJoin {
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = And{
		Left: AllRows{Table: 1},
		Right: Or{
			Left: ColRange{
				Table:  2,
				Column: 0,
				Alias:  1,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
			Right: ColNe{
				Table:  3,
				Column: 0,
				Alias:  2,
				Value:  types.NewUint64(7),
			},
		},
	}
	return pred
}

func splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  3,
				Column: 0,
				Alias:  2,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 1, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  1,
				Column: 0,
				Alias:  1,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrBranchLocalConnectorMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: And{
				Left: ColEq{
					Table:  1,
					Column: 0,
					Alias:  0,
					Value:  types.NewUint64(7),
				},
				Right: ColEqCol{
					LeftTable:   1,
					LeftColumn:  1,
					LeftAlias:   0,
					RightTable:  2,
					RightColumn: 1,
					RightAlias:  1,
				},
			},
			Right: And{
				Left: ColRange{
					Table:  3,
					Column: 0,
					Alias:  2,
					Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
					Upper:  Bound{Unbounded: true},
				},
				Right: ColEqCol{
					LeftTable:   2,
					LeftColumn:  1,
					LeftAlias:   1,
					RightTable:  3,
					RightColumn: 1,
					RightAlias:  2,
				},
			},
		},
	}
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

func TestMultiJoinPlacementSplitOrMultiHopUsesTransitiveEndpointAndMiddleRelationEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left endpoint split-OR local value placement = %v, want [%v]", got, hash)
	}
	leftEndpointRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftEndpointRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left endpoint split-OR transitive range edge placement = %v, want [%v]", got, hash)
	}
	middleValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(middleValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("middle split-OR value edge placement = %v, want [%v]", got, hash)
	}
	middleRangeEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(middleRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("middle split-OR range edge placement = %v, want [%v]", got, hash)
	}
	rightEndpointValueEdge := JoinEdge{LHSTable: 3, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(rightEndpointValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right endpoint split-OR transitive value edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(3, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right endpoint split-OR local range placement = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("broad condition existence edges = %+v, want none for covered split-OR placement", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrUsesBranchLocalConnectorFilterEdges(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrBranchLocalConnectorMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	valueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(valueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("branch-local connector value edge = %v, want [%v]", got, hash)
	}
	rangeEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(rangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("branch-local connector range edge = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("branch-local connector existence fallback = %+v, want none", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for covered middle relation", got)
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

func TestCollectCandidatesMultiJoinSplitOrBranchLocalConnectorPrunesMismatch(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrBranchLocalConnectorMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(200), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched branch-local connector candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("value branch-local connector candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("range branch-local connector candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrBranchLocalConnectorUsesDeltaRows(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrBranchLocalConnectorMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changedRows := []types.ProductValue{{types.NewUint64(200), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, noOverlap, add)
	if len(candidates) != 0 {
		t.Fatalf("non-overlapping branch-local connector candidates = %v, want empty", candidates)
	}

	valueOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, valueOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("value-overlap branch-local connector candidates = %v, want only %v", candidates, hash)
	}

	rangeOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, rangeOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("range-overlap branch-local connector candidates = %v, want only %v", candidates, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			3: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, deleteOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("delete-overlap branch-local connector candidates = %v, want only %v", candidates, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrMultiHopEndpointPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched left endpoint multi-hop candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote-filter left endpoint multi-hop candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})
	rightMismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 3, rightMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched right endpoint multi-hop candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 3, rightMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote-filter right endpoint multi-hop candidates = %v, want [%v]", got, hash)
	}
}

func TestMultiJoinPlacementAndWrappedSplitOrAllRemoteRangeBranchesUseEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	remoteRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(remoteRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("all-remote range branch edge = %v, want [%v]", got, hash)
	}
	remoteNeEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(remoteNeEdge, types.NewUint64(6)); len(got) != 1 || got[0] != hash {
		t.Fatalf("all-remote ColNe lower branch edge = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(remoteNeEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("all-remote ColNe rejected branch edge = %v, want empty", got)
	}
	if got := idx.JoinRangeEdge.Lookup(remoteNeEdge, types.NewUint64(8)); len(got) != 1 || got[0] != hash {
		t.Fatalf("all-remote ColNe upper branch edge = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("endpoint-local value placement = %v, want empty for all-remote branches", got)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("all-remote branch existence fallback = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for all-remote split-OR placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementAndWrappedSplitOrAllRemoteRangeBranchesFallBackWhenPartiallyCovered(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 1)
	s.addTable(3, cols)
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	coveredBranchEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(coveredBranchEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("partial all-remote range edge = %v, want empty when another branch is uncovered", got)
	}
	uncoveredBranchEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(uncoveredBranchEdge, types.NewUint64(8)); len(got) != 0 {
		t.Fatalf("unindexed all-remote ColNe edge = %v, want empty", got)
	}
	conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
		t.Fatalf("condition-edge fallback missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want condition-edge fallback", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinAndWrappedSplitOrAllRemoteRangeBranchesPruneMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
		3: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched all-remote split-OR candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(60), types.NewUint64(20)}},
		3: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("range all-remote split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
		3: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("ColNe all-remote split-OR candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinAndWrappedSplitOrAllRemoteRangeBranchesUseSameTransactionRows(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, changed, rejected, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx all-remote split-OR candidates = %v, want empty", candidates)
	}

	rangeOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 1, changed, rangeOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("range same-tx all-remote split-OR candidates = %v, want only %v", candidates, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			3: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 1, changed, deleteOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("ColNe delete same-tx all-remote split-OR candidates = %v, want only %v", candidates, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSameTransactionFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrLocalFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{16},
		QueryID:    160,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping split-OR same-tx filter-edge candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping split-OR same-tx filter-edge candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSameTransactionTransitiveFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{17},
		QueryID:    170,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping transitive split-OR same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping transitive split-OR same-tx candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	got = mgr.collectCandidatesInto(deleteOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping transitive split-OR same-tx delete candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrRepeatedAliasUsesSelfAndMiddleRelationEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR value placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR range placement = %v, want [%v]", got, hash)
	}
	selfEdge := JoinEdge{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(selfEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR self value edge = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(selfEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR self range edge = %v, want [%v]", got, hash)
	}
	middleValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(middleValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR middle value edge = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(middleValueEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR middle range edge = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("repeated-alias split-OR existence edges = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for repeated-alias split-OR placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrRepeatedAliasPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(40), types.NewUint64(20)}},
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated-alias split-OR table candidates = %v, want empty", got)
	}

	localMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(99)}}
	got := CollectCandidatesForTable(idx, 1, localMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("local repeated-alias split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	edgeMatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, edgeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("self-edge repeated-alias split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	middleMismatch := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, middleMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated-alias split-OR middle candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, middleMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("value-edge repeated-alias split-OR middle candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, middleMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("range-edge repeated-alias split-OR middle candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrRepeatedAliasSameTransactionRows(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{20},
		QueryID:    200,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{
				{types.NewUint64(8), types.NewUint64(20)},
				{types.NewUint64(40), types.NewUint64(20)},
			}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping repeated-alias split-OR same-tx candidates = %v, want empty", got)
	}

	selfOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{
				{types.NewUint64(8), types.NewUint64(20)},
				{types.NewUint64(60), types.NewUint64(20)},
			}},
		},
	}
	got := mgr.collectCandidatesInto(selfOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("self-overlap repeated-alias split-OR same-tx candidates = %v, want only %v", got, hash)
	}

	middleOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}},
		},
	}
	got = mgr.collectCandidatesInto(middleOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("middle-overlap repeated-alias split-OR same-tx candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrRepeatedAliasUsesConditionEdgesWhenSelfEdgeUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols)
	s.addTable(2, cols, 1)
	idx := NewPruningIndexes()
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	selfEdge := JoinEdge{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(selfEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("unindexed repeated-alias self value edge = %v, want empty", got)
	}
	if got := idx.JoinRangeEdge.Lookup(selfEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("unindexed repeated-alias self range edge = %v, want empty", got)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial repeated-alias local value placement = %v, want empty", got)
	}
	conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
		t.Fatalf("condition fallback edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want condition-edge placement", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[2] = %v, want fallback [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrNonKeyPreservingMultiHopUsesPathEdges(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftEndpointRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftEndpointRangeEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("non-key-preserving transitive range edge placement = %v, want empty", got)
	}
	leftPathEdge := JoinPathEdge{
		LHSTable: 1, MidTable: 2, RHSTable: 3,
		LHSJoinCol: 1, MidFirstCol: 1, MidSecondCol: 0, RHSJoinCol: 1, RHSFilterCol: 0,
	}
	if got := idx.JoinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("non-key-preserving path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("endpoint local placement = %v, want [%v]", got, hash)
	}
	leftConditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[leftConditionEdge][hash]; ok {
		t.Fatalf("fallback condition edge present: %+v, want none", idx.JoinEdge.exists)
	}
	rightPathEdge := JoinPathEdge{
		LHSTable: 3, MidTable: 2, RHSTable: 1,
		LHSJoinCol: 1, MidFirstCol: 0, MidSecondCol: 1, RHSJoinCol: 1, RHSFilterCol: 0,
	}
	if got := idx.JoinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("non-key-preserving path value edge placement = %v, want [%v]", got, hash)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for non-key-preserving path placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrNonKeyPreservingPathEdgesPruneMismatch(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched non-key-preserving path left candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(60), types.NewUint64(30)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching non-key-preserving path left candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	rightMismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}
	if got := CollectCandidatesForTable(idx, 3, rightMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched non-key-preserving path right candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 3, rightMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching non-key-preserving path right candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrNonKeyPreservingPathEdgesUseSameTransactionRows(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{21},
		QueryID:    210,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, buildMockCommitted(s, nil), scratch); len(got) != 0 {
		t.Fatalf("non-overlapping same-tx path candidates = %v, want empty", got)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(30)}}},
		},
	}
	got := mgr.collectCandidatesInto(allChangedOverlap, buildMockCommitted(s, nil), scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("all-changed same-tx path candidates = %v, want only %v", got, hash)
	}

	allChangedDeleteOverlap := &store.Changeset{
		TxID: 5,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Deletes: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(30)}}},
		},
	}
	got = mgr.collectCandidatesInto(allChangedDeleteOverlap, buildMockCommitted(s, nil), scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("all-changed same-tx delete path candidates = %v, want only %v", got, hash)
	}

	midCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	rhsChangedOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(30)}}},
		},
	}
	got = mgr.collectCandidatesInto(rhsChangedOverlap, midCommitted, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("rhs-changed same-tx path candidates = %v, want only %v", got, hash)
	}

	rhsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(60), types.NewUint64(30)}},
	})
	midChangedOverlap := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	got = mgr.collectCandidatesInto(midChangedOverlap, rhsCommitted, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("mid-changed same-tx path candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrNonKeyPreservingPathFallsBackWhenMidUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 0)
	s.addTable(3, cols, 1)
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := JoinPathEdge{
		LHSTable: 1, MidTable: 2, RHSTable: 3,
		LHSJoinCol: 1, MidFirstCol: 1, MidSecondCol: 0, RHSJoinCol: 1, RHSFilterCol: 0,
	}
	if got := idx.JoinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("unindexed non-key-preserving path edge = %v, want empty", got)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial endpoint local placement = %v, want empty when path is uncovered", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrNonKeyPreservingPathFallsBackWhenRHSUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 0, 1)
	s.addTable(3, cols)
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := JoinPathEdge{
		LHSTable: 1, MidTable: 2, RHSTable: 3,
		LHSJoinCol: 1, MidFirstCol: 1, MidSecondCol: 0, RHSJoinCol: 1, RHSFilterCol: 0,
	}
	if got := idx.JoinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("rhs-unindexed non-key-preserving path edge = %v, want empty", got)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial endpoint local placement = %v, want empty when RHS path is uncovered", got)
	}
	conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
		t.Fatalf("condition-edge fallback missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want condition-edge fallback", got)
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

func TestCollectCandidatesMultiJoinSplitOrMultiHopMiddleRelationPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched multi-hop middle candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left endpoint multi-hop middle candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right endpoint multi-hop middle candidates = %v, want [%v]", got, hash)
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
