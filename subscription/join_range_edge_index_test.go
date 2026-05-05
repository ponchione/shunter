package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestJoinRangeEdgeIndexAddLookup(t *testing.T) {
	idx := NewJoinRangeEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	h := hashN(1)
	idx.Add(edge,
		Bound{Value: types.NewUint64(10), Inclusive: true},
		Bound{Value: types.NewUint64(20), Inclusive: false},
		h,
	)

	if got := idx.Lookup(edge, types.NewUint64(10)); len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup lower inclusive = %v, want [%v]", got, h)
	}
	if got := idx.Lookup(edge, types.NewUint64(20)); len(got) != 0 {
		t.Fatalf("Lookup upper exclusive = %v, want empty", got)
	}
}

func TestJoinRangeEdgeIndexWrongEdge(t *testing.T) {
	idx := NewJoinRangeEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	idx.Add(edge,
		Bound{Value: types.NewUint64(10), Inclusive: true},
		Bound{Value: types.NewUint64(20), Inclusive: true},
		hashN(1),
	)

	other := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := idx.Lookup(other, types.NewUint64(15)); len(got) != 0 {
		t.Fatalf("Lookup wrong edge = %v, want empty", got)
	}
}

func TestJoinRangeEdgeIndexRemoveCleansUp(t *testing.T) {
	idx := NewJoinRangeEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	h := hashN(1)
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Unbounded: true}
	idx.Add(edge, lower, upper, h)
	idx.Remove(edge, lower, upper, h)

	if got := idx.Lookup(edge, types.NewUint64(11)); len(got) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", got)
	}
	if len(idx.edges) != 0 {
		t.Fatalf("edges not cleaned up: %+v", idx.edges)
	}
	if len(idx.byTable) != 0 {
		t.Fatalf("byTable not cleaned up: %+v", idx.byTable)
	}
}
