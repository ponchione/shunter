package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestPlaceColEqGoesToValueIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	h := hashN(1)
	PlaceSubscription(idx, p, h)
	if got := idx.Value.Lookup(1, 0, types.NewUint64(42)); len(got) != 1 {
		t.Fatalf("ValueIndex lookup = %v, want 1", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex should be empty: %v", got)
	}
	if got := idx.JoinEdge.EdgesForTable(1); len(got) != 0 {
		t.Fatalf("JoinEdgeIndex should be empty: %v", got)
	}
}

func TestPlaceAllRowsGoesToTableIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := AllRows{Table: 1}
	h := hashN(1)
	PlaceSubscription(idx, p, h)
	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != h {
		t.Fatalf("Table.Lookup(1) = %v, want [%v]", got, h)
	}
	if got := idx.Value.TrackedColumns(1); len(got) != 0 {
		t.Fatalf("ValueIndex should be empty, got tracked columns %v", got)
	}
}

func TestPlaceNoRowsSkipsIndexes(t *testing.T) {
	idx := NewPruningIndexes()
	p := NoRows{Table: 1}
	h := hashN(11)
	PlaceSubscription(idx, p, h)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("NoRows placement should leave indexes empty: %+v", idx)
	}

	RemoveSubscription(idx, p, h)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("NoRows removal should leave indexes empty: %+v", idx)
	}
}

func TestPlaceAndWithNoRowsSkipsIndexes(t *testing.T) {
	idx := NewPruningIndexes()
	p := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)},
		Right: NoRows{Table: 2},
	}
	h := hashN(12)
	PlaceSubscription(idx, p, h)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("AND with NoRows should leave indexes empty: %+v", idx)
	}
}

func TestPlaceJoinWithNoRowsFilterSkipsIndexes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	idx := NewPruningIndexes()
	p := Join{
		Left:     1,
		Right:    2,
		LeftCol:  0,
		RightCol: 0,
		Filter:   NoRows{Table: 1},
	}
	h := hashN(13)
	placeSubscriptionForResolver(idx, p, h, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("join with NoRows filter should leave indexes empty: %+v", idx)
	}
	removeSubscriptionForResolver(idx, p, h, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("join with NoRows filter removal should leave indexes empty: %+v", idx)
	}
}

func TestPlaceCrossJoinWithNoRowsFilterSkipsIndexes(t *testing.T) {
	idx := NewPruningIndexes()
	p := CrossJoin{
		Left:   1,
		Right:  2,
		Filter: NoRows{Table: 1},
	}
	h := hashN(14)
	PlaceSubscription(idx, p, h)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("cross join with NoRows filter should leave indexes empty: %+v", idx)
	}
}

func TestPlaceColRangeGoesToRangeIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := ColRange{Table: 1, Column: 0, Lower: Bound{Value: types.NewUint64(1)}, Upper: Bound{Unbounded: true}}
	h := hashN(1)
	PlaceSubscription(idx, p, h)
	if got := idx.Range.Lookup(1, 0, types.NewUint64(2)); len(got) != 1 || got[0] != h {
		t.Fatalf("RangeIndex = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex should be empty: %v", got)
	}
}

func TestPlaceColNeGoesToRangeIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := ColNe{Table: 1, Column: 0, Value: types.NewUint64(42)}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	if got := idx.Range.Lookup(1, 0, types.NewUint64(41)); len(got) != 1 || got[0] != h {
		t.Fatalf("ColNe lower-side range candidate = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(43)); len(got) != 1 || got[0] != h {
		t.Fatalf("ColNe upper-side range candidate = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("ColNe rejected value candidate = %v, want empty", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex should be empty for ColNe: %v", got)
	}

	RemoveSubscription(idx, p, h)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("ColNe removal should clean indexes: %+v", idx)
	}
}

func TestPlaceJoinWithFilterOnRHS(t *testing.T) {
	// Join{L=1, R=2} with ColEq filter on T2.
	// LHS (table 1) → JoinEdgeIndex
	// RHS (table 2) → ValueIndex (because ColEq on T2 is present)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: ColEq{Table: 2, Column: 1, Value: types.NewUint64(99)},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	if edges := idx.JoinEdge.EdgesForTable(1); len(edges) != 1 {
		t.Fatalf("Expected 1 JoinEdge for T1, got %v", edges)
	}
	if got := idx.Value.Lookup(2, 1, types.NewUint64(99)); len(got) != 1 {
		t.Fatalf("ValueIndex on T2 = %v, want 1", got)
	}
}

func TestPlaceJoinWithRangeFilterOnRHS(t *testing.T) {
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if edges := idx.JoinRangeEdge.EdgesForTable(1); len(edges) != 1 || edges[0] != edge {
		t.Fatalf("JoinRangeEdgeIndex edges for LHS = %v, want [%v]", edges, edge)
	}
	if got := idx.JoinRangeEdge.Lookup(edge, types.NewUint64(15)); len(got) != 1 || got[0] != h {
		t.Fatalf("JoinRangeEdgeIndex lookup = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(2, 2, types.NewUint64(15)); len(got) != 1 || got[0] != h {
		t.Fatalf("RangeIndex on T2 = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex on changed LHS should be empty: %v", got)
	}
}

func TestPlaceJoinWithNeFilterOnRHSUsesRangeEdges(t *testing.T) {
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColNe{Table: 2, Column: 2, Value: types.NewUint64(10)},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := idx.JoinRangeEdge.Lookup(edge, types.NewUint64(9)); len(got) != 1 || got[0] != h {
		t.Fatalf("JoinRangeEdge lower-side ColNe candidate = %v, want [%v]", got, h)
	}
	if got := idx.JoinRangeEdge.Lookup(edge, types.NewUint64(11)); len(got) != 1 || got[0] != h {
		t.Fatalf("JoinRangeEdge upper-side ColNe candidate = %v, want [%v]", got, h)
	}
	if got := idx.JoinRangeEdge.Lookup(edge, types.NewUint64(10)); len(got) != 0 {
		t.Fatalf("JoinRangeEdge rejected ColNe value candidate = %v, want empty", got)
	}
	if got := idx.Range.Lookup(2, 2, types.NewUint64(11)); len(got) != 1 || got[0] != h {
		t.Fatalf("changed-side ColNe range candidate = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex on changed LHS should be empty: %v", got)
	}
}

func TestPlaceJoinWithOppositeSideOrFilterAddsEveryJoinEdge(t *testing.T) {
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left:  ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
			Right: ColEq{Table: 2, Column: 3, Value: types.NewString("large")},
		},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	edges := idx.JoinEdge.EdgesForTable(1)
	if len(edges) != 2 {
		t.Fatalf("JoinEdgeIndex edges for LHS = %v, want 2", edges)
	}
	first := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	second := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 3}
	if got := idx.JoinEdge.Lookup(first, types.NewString("red")); len(got) != 1 || got[0] != h {
		t.Fatalf("first OR branch lookup = %v, want [%v]", got, h)
	}
	if got := idx.JoinEdge.Lookup(second, types.NewString("large")); len(got) != 1 || got[0] != h {
		t.Fatalf("second OR branch lookup = %v, want [%v]", got, h)
	}
}

func TestPlaceJoinWithFilterOnLHSStillTracksRHSChangesViaJoinEdge(t *testing.T) {
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: ColEq{Table: 1, Column: 1, Value: types.NewString("red")},
	}
	PlaceSubscription(idx, p, hashN(1))

	if edges := idx.JoinEdge.EdgesForTable(2); len(edges) != 1 {
		t.Fatalf("expected join-edge placement for RHS-driven changes, got %v", edges)
	}
	if got := idx.Value.Lookup(1, 1, types.NewString("red")); len(got) != 1 {
		t.Fatalf("expected LHS filter to stay in ValueIndex, got %v", got)
	}
}

func TestPlaceUnfilteredJoinUsesExistenceEdgesWhenOppositeIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	idx := NewPruningIndexes()
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 1}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)

	leftEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 1}
	rightEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 0, RHSFilterCol: 0}
	if _, ok := idx.JoinEdge.exists[leftEdge][h]; !ok {
		t.Fatalf("left existence edge missing: %+v", idx.JoinEdge.exists)
	}
	if _, ok := idx.JoinEdge.exists[rightEdge][h]; !ok {
		t.Fatalf("right existence edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex for left table = %v, want empty", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex for right table = %v, want empty", got)
	}
}

func TestPlaceAndTwoColEqs(t *testing.T) {
	// And{ColEq T1.col0=1, ColEq T2.col0=2} — each lands in ValueIndex.
	idx := NewPruningIndexes()
	p := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 2, Column: 0, Value: types.NewUint64(2)},
	}
	PlaceSubscription(idx, p, hashN(1))
	if got := idx.Value.Lookup(1, 0, types.NewUint64(1)); len(got) != 1 {
		t.Fatalf("ValueIndex T1 = %v", got)
	}
	if got := idx.Value.Lookup(2, 0, types.NewUint64(2)); len(got) != 1 {
		t.Fatalf("ValueIndex T2 = %v", got)
	}
}

func TestPlaceAndRemoveSymmetric(t *testing.T) {
	idx := NewPruningIndexes()
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	PlaceSubscription(idx, p, hashN(1))
	RemoveSubscription(idx, p, hashN(1))
	if len(idx.Value.args) != 0 || len(idx.Value.cols) != 0 {
		t.Fatalf("ValueIndex not empty after remove")
	}
	if len(idx.Range.ranges) != 0 || len(idx.Range.cols) != 0 {
		t.Fatalf("RangeIndex not empty after remove")
	}
	if len(idx.JoinEdge.edges) != 0 {
		t.Fatalf("JoinEdgeIndex not empty")
	}
	if len(idx.JoinRangeEdge.edges) != 0 {
		t.Fatalf("JoinRangeEdgeIndex not empty")
	}
	if len(idx.Table.tables) != 0 {
		t.Fatalf("TableIndex not empty")
	}
}

func TestPlaceAndRemoveJoinSymmetric(t *testing.T) {
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: ColEq{Table: 2, Column: 1, Value: types.NewUint64(99)},
	}
	PlaceSubscription(idx, p, hashN(1))
	RemoveSubscription(idx, p, hashN(1))
	if len(idx.Value.args) != 0 || len(idx.Value.cols) != 0 {
		t.Fatalf("ValueIndex not empty after remove")
	}
	if len(idx.JoinEdge.edges) != 0 {
		t.Fatalf("JoinEdgeIndex not empty after remove")
	}
	if len(idx.JoinRangeEdge.edges) != 0 {
		t.Fatalf("JoinRangeEdgeIndex not empty after remove")
	}
	if len(idx.Range.ranges) != 0 || len(idx.Range.cols) != 0 {
		t.Fatalf("RangeIndex not empty after remove")
	}
	if len(idx.Table.tables) != 0 {
		t.Fatalf("TableIndex not empty after remove")
	}
}

func TestPlaceAndRemoveJoinRangeSymmetric(t *testing.T) {
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	PlaceSubscription(idx, p, hashN(1))
	RemoveSubscription(idx, p, hashN(1))
	if len(idx.Value.args) != 0 || len(idx.Value.cols) != 0 {
		t.Fatalf("ValueIndex not empty after remove")
	}
	if len(idx.Range.ranges) != 0 || len(idx.Range.cols) != 0 {
		t.Fatalf("RangeIndex not empty after remove")
	}
	if len(idx.JoinEdge.edges) != 0 {
		t.Fatalf("JoinEdgeIndex not empty after remove")
	}
	if len(idx.JoinRangeEdge.edges) != 0 || len(idx.JoinRangeEdge.byTable) != 0 {
		t.Fatalf("JoinRangeEdgeIndex not empty after remove")
	}
	if len(idx.Table.tables) != 0 {
		t.Fatalf("TableIndex not empty after remove")
	}
}

func TestCollectCandidatesTier1AndTier3(t *testing.T) {
	idx := NewPruningIndexes()
	// Tier 1 subscription on T1.col0=42
	PlaceSubscription(idx, ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}, hashN(1))
	// Tier 3 AllRows on T1
	PlaceSubscription(idx, AllRows{Table: 1}, hashN(2))
	// Unrelated T2 subscription
	PlaceSubscription(idx, ColEq{Table: 2, Column: 0, Value: types.NewUint64(7)}, hashN(3))

	rows := []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 2 {
		t.Fatalf("candidates = %v, want 2 (hashN(1)+hashN(2))", cands)
	}
}

func TestCollectCandidatesNoDuplicates(t *testing.T) {
	idx := NewPruningIndexes()
	// Subscription appears in both Tier 1 and Tier 3 artificially (rare in real placement,
	// but worth asserting dedup in the union).
	idx.Value.Add(1, 0, types.NewUint64(42), hashN(1))
	idx.Table.Add(1, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(42)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 1 || cands[0] != hashN(1) {
		t.Fatalf("dedup failed: %v", cands)
	}
}

func TestCollectCandidatesEmptyTable(t *testing.T) {
	idx := NewPruningIndexes()
	cands := CollectCandidatesForTable(idx, 1, nil, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("empty index candidates = %v", cands)
	}
}

func TestCollectCandidatesNoRowsSkipped(t *testing.T) {
	idx := NewPruningIndexes()
	PlaceSubscription(idx, NoRows{Table: 1}, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(42)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("NoRows candidates = %v, want empty", cands)
	}
}

func TestCollectCandidatesJoinNoRowsFilterSkipped(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	idx := NewPruningIndexes()
	p := Join{
		Left:     1,
		Right:    2,
		LeftCol:  0,
		RightCol: 0,
		Filter:   NoRows{Table: 1},
	}
	placeSubscriptionForResolver(idx, p, hashN(1), s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(7)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 0 {
		t.Fatalf("join NoRows filter candidates = %v, want empty", cands)
	}
}

func TestCollectCandidatesValueMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	PlaceSubscription(idx, ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}, hashN(1))
	rows := []types.ProductValue{{types.NewUint64(99)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("unmatched value should produce 0 candidates: %v", cands)
	}
}

func TestCollectCandidatesRangeMatch(t *testing.T) {
	idx := NewPruningIndexes()
	PlaceSubscription(idx, ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
		Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
	}, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(15)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 1 || cands[0] != hashN(1) {
		t.Fatalf("range candidate = %v, want [%v]", cands, hashN(1))
	}
}

func TestCollectCandidatesRangeMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	PlaceSubscription(idx, ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
		Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
	}, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(25)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("unmatched range should produce 0 candidates: %v", cands)
	}
}

func TestCollectCandidatesColNeRangeMatchAndMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	PlaceSubscription(idx, ColNe{Table: 1, Column: 0, Value: types.NewUint64(42)}, hashN(1))

	matchRows := []types.ProductValue{{types.NewUint64(43)}}
	cands := CollectCandidatesForTable(idx, 1, matchRows, nil, nil)
	if len(cands) != 1 || cands[0] != hashN(1) {
		t.Fatalf("ColNe candidate = %v, want [%v]", cands, hashN(1))
	}

	mismatchRows := []types.ProductValue{{types.NewUint64(42)}}
	cands = CollectCandidatesForTable(idx, 1, mismatchRows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("ColNe rejected value should produce no candidates: %v", cands)
	}
}

func TestCollectCandidatesMixedEqNeOrUsesIndexes(t *testing.T) {
	idx := NewPruningIndexes()
	p := Or{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)},
		Right: ColNe{Table: 1, Column: 1, Value: types.NewUint64(42)},
	}
	PlaceSubscription(idx, p, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(8), types.NewUint64(43)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 1 || cands[0] != hashN(1) {
		t.Fatalf("mixed Eq/Ne OR range candidate = %v, want [%v]", cands, hashN(1))
	}
	rows = []types.ProductValue{{types.NewUint64(8), types.NewUint64(42)}}
	cands = CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("mixed Eq/Ne OR mismatch candidates = %v, want empty", cands)
	}
}

func TestCollectCandidatesJoinRangeEdgeMatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64, 2: types.KindUint64}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(7), types.NewUint64(15)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 1 || cands[0] != h {
		t.Fatalf("join range-edge candidate = %v, want [%v]", cands, h)
	}
}

func TestCollectCandidatesJoinRangeEdgeMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64, 2: types.KindUint64}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	placeSubscriptionForResolver(idx, p, hashN(1), s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(7), types.NewUint64(25)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 0 {
		t.Fatalf("out-of-range join edge should produce 0 candidates: %v", cands)
	}
}

func TestCollectCandidatesJoinNeRangeEdgeMatchAndMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64, 2: types.KindUint64}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColNe{Table: 2, Column: 2, Value: types.NewUint64(10)},
	}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {
			{types.NewUint64(100), types.NewUint64(7), types.NewUint64(11)},
			{types.NewUint64(101), types.NewUint64(8), types.NewUint64(10)},
		},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 1 || cands[0] != h {
		t.Fatalf("join ColNe range-edge candidate = %v, want [%v]", cands, h)
	}
	rows = []types.ProductValue{{types.NewUint64(8)}}
	cands = CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 0 {
		t.Fatalf("join ColNe rejected value candidates = %v, want empty", cands)
	}
}

func TestCollectCandidatesJoinExistenceEdgeMatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	idx := NewPruningIndexes()
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 1}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(7)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 1 || cands[0] != h {
		t.Fatalf("join existence candidate = %v, want [%v]", cands, h)
	}
}

func TestCollectCandidatesJoinExistenceEdgeMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	idx := NewPruningIndexes()
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 1}
	placeSubscriptionForResolver(idx, p, hashN(1), s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(8)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 0 {
		t.Fatalf("join existence mismatch candidates = %v, want empty", cands)
	}
}

func TestPlaceOrRangeBranchesUseRangeIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := Or{
		Left: ColRange{Table: 1, Column: 0,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
		Right: ColRange{Table: 1, Column: 0,
			Lower: Bound{Value: types.NewUint64(30), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(40), Inclusive: true},
		},
	}
	PlaceSubscription(idx, p, hashN(1))

	if got := idx.Range.Lookup(1, 0, types.NewUint64(35)); len(got) != 1 || got[0] != hashN(1) {
		t.Fatalf("OR range branch candidate = %v, want [%v]", got, hashN(1))
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex should be empty for fully range-constrained OR: %v", got)
	}
}

func TestPlaceOrMixedEqRangeBranchesUseIndexes(t *testing.T) {
	idx := NewPruningIndexes()
	p := Or{
		Left: ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)},
		Right: ColRange{Table: 1, Column: 1,
			Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper: Bound{Unbounded: true},
		},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != h {
		t.Fatalf("mixed OR equality branch candidate = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(1, 1, types.NewUint64(60)); len(got) != 1 || got[0] != h {
		t.Fatalf("mixed OR range branch candidate = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex should be empty for fully indexable mixed OR: %v", got)
	}
}

func TestCollectCandidatesMixedEqRangeOrPrunesMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	p := Or{
		Left: ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)},
		Right: ColRange{Table: 1, Column: 1,
			Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper: Bound{Unbounded: true},
		},
	}
	PlaceSubscription(idx, p, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(8), types.NewUint64(40)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("mixed OR mismatch candidates = %v, want empty", cands)
	}
}

func TestPlaceJoinWithOppositeSideMixedOrFilterAddsValueAndRangeEdges(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
		3: types.KindUint64,
	}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
			Right: ColRange{Table: 2, Column: 3,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)

	valueEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	rangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 3}
	if got := idx.JoinEdge.Lookup(valueEdge, types.NewString("red")); len(got) != 1 || got[0] != h {
		t.Fatalf("mixed OR join equality edge = %v, want [%v]", got, h)
	}
	if got := idx.JoinRangeEdge.Lookup(rangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != h {
		t.Fatalf("mixed OR join range edge = %v, want [%v]", got, h)
	}
	if got := idx.Value.Lookup(2, 2, types.NewString("red")); len(got) != 1 || got[0] != h {
		t.Fatalf("changed-side equality branch = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(2, 3, types.NewUint64(60)); len(got) != 1 || got[0] != h {
		t.Fatalf("changed-side range branch = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex for LHS should be empty for mixed OR join edge: %v", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex for RHS should be empty for mixed OR changed side: %v", got)
	}
}

func TestPlaceAndRemoveJoinMixedOrSymmetric(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
		3: types.KindUint64,
	}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
			Right: ColRange{Table: 2, Column: 3,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)
	removeSubscriptionForResolver(idx, p, h, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes not empty after mixed OR remove: %+v", idx)
	}
}

func TestPlaceJoinWithCrossSideOrFilterAddsSplitBranchIndexes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColRange{Table: 2, Column: 2,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)

	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 0, RHSFilterCol: 1}
	if got := idx.Value.Lookup(1, 1, types.NewString("active")); len(got) != 1 || got[0] != h {
		t.Fatalf("left branch value candidate = %v, want [%v]", got, h)
	}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != h {
		t.Fatalf("left-driven range edge = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(2, 2, types.NewUint64(60)); len(got) != 1 || got[0] != h {
		t.Fatalf("right branch range candidate = %v, want [%v]", got, h)
	}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewString("active")); len(got) != 1 || got[0] != h {
		t.Fatalf("right-driven value edge = %v, want [%v]", got, h)
	}
	if got := idx.JoinEdge.exists[leftRangeEdge]; len(got) != 0 {
		t.Fatalf("left broad existence candidates = %v, want none", got)
	}
	if got := idx.JoinEdge.exists[rightValueEdge]; len(got) != 0 {
		t.Fatalf("right broad existence candidates = %v, want none", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex for left should be empty for split OR: %v", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex for right should be empty for split OR: %v", got)
	}
}

func TestCollectCandidatesJoinCrossSideOrPrunesExistenceOnlyMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColRange{Table: 2, Column: 2,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	placeSubscriptionForResolver(idx, p, hashN(1), s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(7), types.NewUint64(40)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7), types.NewString("inactive")}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 0 {
		t.Fatalf("cross-side OR mismatch candidates = %v, want empty", cands)
	}
}

func TestPlaceAndRemoveJoinCrossSideOrSymmetric(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColRange{Table: 2, Column: 2,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	h := hashN(1)
	placeSubscriptionForResolver(idx, p, h, s)
	removeSubscriptionForResolver(idx, p, h, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes not empty after cross-side OR remove: %+v", idx)
	}
}

func TestCollectCandidatesJoinMixedOrRangeEdgePrunesMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
		3: types.KindUint64,
	}, 1)
	idx := NewPruningIndexes()
	p := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
			Right: ColRange{Table: 2, Column: 3,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	placeSubscriptionForResolver(idx, p, hashN(1), s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(7), types.NewString("blue"), types.NewUint64(40)}},
	})

	rows := []types.ProductValue{{types.NewUint64(7)}}
	cands := CollectCandidatesForTable(idx, 1, rows, committed, s)
	if len(cands) != 0 {
		t.Fatalf("mixed OR join edge mismatch candidates = %v, want empty", cands)
	}
}

func TestPlaceFilteredCrossJoinUsesLocalValueIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := CrossJoin{
		Left:  1,
		Right: 2,
		Filter: ColEq{
			Table:  1,
			Column: 1,
			Value:  types.NewString("active"),
		},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	if got := idx.Value.Lookup(1, 1, types.NewString("active")); len(got) != 1 || got[0] != h {
		t.Fatalf("cross join value candidate = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex for locally filtered side = %v, want empty", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 1 || got[0] != h {
		t.Fatalf("TableIndex for unfiltered cross side = %v, want [%v]", got, h)
	}
}

func TestPlaceFilteredCrossJoinUsesLocalMixedEqRangeIndexes(t *testing.T) {
	idx := NewPruningIndexes()
	p := CrossJoin{
		Left:  1,
		Right: 2,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColRange{Table: 1, Column: 2,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	if got := idx.Value.Lookup(1, 1, types.NewString("active")); len(got) != 1 || got[0] != h {
		t.Fatalf("cross join mixed equality candidate = %v, want [%v]", got, h)
	}
	if got := idx.Range.Lookup(1, 2, types.NewUint64(60)); len(got) != 1 || got[0] != h {
		t.Fatalf("cross join mixed range candidate = %v, want [%v]", got, h)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex for locally filtered mixed side = %v, want empty", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 1 || got[0] != h {
		t.Fatalf("TableIndex for unfiltered cross side = %v, want [%v]", got, h)
	}
}

func TestCollectCandidatesFilteredCrossJoinPrunesLocalMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	p := CrossJoin{
		Left:  1,
		Right: 2,
		Filter: ColEq{
			Table:  1,
			Column: 1,
			Value:  types.NewString("active"),
		},
	}
	PlaceSubscription(idx, p, hashN(1))

	rows := []types.ProductValue{{types.NewUint64(7), types.NewString("inactive")}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("filtered cross join mismatch candidates = %v, want empty", cands)
	}
}

func TestPlaceSelfCrossJoinKeepsTableFallback(t *testing.T) {
	idx := NewPruningIndexes()
	p := CrossJoin{
		Left:       1,
		Right:      1,
		LeftAlias:  0,
		RightAlias: 1,
		Filter: ColEq{
			Table:  1,
			Column: 1,
			Alias:  0,
			Value:  types.NewString("active"),
		},
	}
	h := hashN(1)
	PlaceSubscription(idx, p, h)

	if got := idx.Value.Lookup(1, 1, types.NewString("active")); len(got) != 0 {
		t.Fatalf("self-cross join should not use value index, got %v", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != h {
		t.Fatalf("self-cross join table fallback = %v, want [%v]", got, h)
	}
}
