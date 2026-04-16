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
	if got := idx.Table.Lookup(1); len(got) != 1 {
		t.Fatalf("TableIndex = %v, want 1", got)
	}
	if cols := idx.Value.TrackedColumns(1); len(cols) != 0 {
		t.Fatalf("ValueIndex should be empty: %v", cols)
	}
}

func TestPlaceColRangeGoesToTableIndex(t *testing.T) {
	idx := NewPruningIndexes()
	p := ColRange{Table: 1, Column: 0, Lower: Bound{Value: types.NewUint64(1)}, Upper: Bound{Unbounded: true}}
	h := hashN(1)
	PlaceSubscription(idx, p, h)
	if got := idx.Table.Lookup(1); len(got) != 1 {
		t.Fatalf("TableIndex = %v, want 1", got)
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
	if len(idx.JoinEdge.edges) != 0 {
		t.Fatalf("JoinEdgeIndex not empty")
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

func TestCollectCandidatesValueMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	PlaceSubscription(idx, ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}, hashN(1))
	rows := []types.ProductValue{{types.NewUint64(99)}}
	cands := CollectCandidatesForTable(idx, 1, rows, nil, nil)
	if len(cands) != 0 {
		t.Fatalf("unmatched value should produce 0 candidates: %v", cands)
	}
}
