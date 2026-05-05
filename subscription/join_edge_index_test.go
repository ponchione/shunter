package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestJoinEdgeIndexAddLookup(t *testing.T) {
	ji := NewJoinEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 1}
	h := hashN(1)
	ji.Add(edge, types.NewUint64(42), h)
	got := ji.Lookup(edge, types.NewUint64(42))
	if len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup = %v, want [h]", got)
	}
}

func TestJoinEdgeIndexWrongFilterValue(t *testing.T) {
	ji := NewJoinEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 1}
	ji.Add(edge, types.NewUint64(42), hashN(1))
	if got := ji.Lookup(edge, types.NewUint64(99)); len(got) != 0 {
		t.Fatalf("Lookup wrong value = %v, want empty", got)
	}
}

func TestJoinEdgeIndexWrongEdge(t *testing.T) {
	ji := NewJoinEdgeIndex()
	e1 := JoinEdge{LHSTable: 1, RHSTable: 2}
	e2 := JoinEdge{LHSTable: 3, RHSTable: 4}
	ji.Add(e1, types.NewUint64(42), hashN(1))
	if got := ji.Lookup(e2, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("Lookup wrong edge = %v, want empty", got)
	}
}

func TestJoinEdgeIndexMultipleHashes(t *testing.T) {
	ji := NewJoinEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2}
	ji.Add(edge, types.NewUint64(42), hashN(1))
	ji.Add(edge, types.NewUint64(42), hashN(2))
	if got := ji.Lookup(edge, types.NewUint64(42)); len(got) != 2 {
		t.Fatalf("Lookup = %v, want 2 hashes", got)
	}
}

func TestJoinEdgeIndexRemoveCleansUp(t *testing.T) {
	ji := NewJoinEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2}
	ji.Add(edge, types.NewUint64(42), hashN(1))
	ji.Remove(edge, types.NewUint64(42), hashN(1))
	if len(ji.edges) != 0 {
		t.Fatalf("edges not cleaned up: %+v", ji.edges)
	}
	if len(ji.byTable) != 0 {
		t.Fatalf("byTable not cleaned up: %+v", ji.byTable)
	}
}

func TestJoinEdgeIndexExistenceAddRemoveCleansUp(t *testing.T) {
	ji := NewJoinEdgeIndex()
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 1}
	h := hashN(1)
	ji.AddExistence(edge, h)

	var got []QueryHash
	ji.ForEachExistenceHash(edge, func(found QueryHash) {
		got = append(got, found)
	})
	if len(got) != 1 || got[0] != h {
		t.Fatalf("ForEachExistenceHash = %v, want [%v]", got, h)
	}
	if edges := ji.EdgesForTable(1); len(edges) != 1 || edges[0] != edge {
		t.Fatalf("EdgesForTable = %v, want [%v]", edges, edge)
	}

	ji.RemoveExistence(edge, h)
	if len(ji.exists) != 0 {
		t.Fatalf("exists not cleaned up: %+v", ji.exists)
	}
	if len(ji.byTable) != 0 {
		t.Fatalf("byTable not cleaned up: %+v", ji.byTable)
	}
}

func TestJoinEdgeIndexEdgesForTable(t *testing.T) {
	ji := NewJoinEdgeIndex()
	e1 := JoinEdge{LHSTable: 1, RHSTable: 2}
	e2 := JoinEdge{LHSTable: 1, RHSTable: 3}
	e3 := JoinEdge{LHSTable: 5, RHSTable: 2}
	ji.Add(e1, types.NewUint64(1), hashN(1))
	ji.Add(e2, types.NewUint64(1), hashN(2))
	ji.Add(e3, types.NewUint64(1), hashN(3))

	got := ji.EdgesForTable(1)
	if len(got) != 2 {
		t.Fatalf("EdgesForTable(1) = %v, want 2 edges", got)
	}
}

func TestJoinEdgeIndexEdgesForTableUnrelated(t *testing.T) {
	ji := NewJoinEdgeIndex()
	if got := ji.EdgesForTable(999); len(got) != 0 {
		t.Fatalf("EdgesForTable unrelated = %v, want empty", got)
	}
}
