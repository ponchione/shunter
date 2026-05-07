package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func testJoinPathEdge() JoinPathEdge {
	return JoinPathEdge{
		LHSTable:     1,
		MidTable:     2,
		RHSTable:     3,
		LHSJoinCol:   0,
		MidFirstCol:  1,
		MidSecondCol: 2,
		RHSJoinCol:   3,
		RHSFilterCol: 4,
	}
}

func TestJoinPathEdgeIndexAddLookup(t *testing.T) {
	idx := NewJoinPathEdgeIndex()
	edge := testJoinPathEdge()
	h := hashN(1)
	idx.Add(edge, types.NewUint64(42), h)

	got := idx.Lookup(edge, types.NewUint64(42))
	if len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup = %v, want [%v]", got, h)
	}
}

func TestJoinPathEdgeIndexWrongFilterValue(t *testing.T) {
	idx := NewJoinPathEdgeIndex()
	edge := testJoinPathEdge()
	idx.Add(edge, types.NewUint64(42), hashN(1))

	if got := idx.Lookup(edge, types.NewUint64(99)); len(got) != 0 {
		t.Fatalf("Lookup wrong value = %v, want empty", got)
	}
}

func TestJoinPathEdgeIndexWrongEdge(t *testing.T) {
	idx := NewJoinPathEdgeIndex()
	edge := testJoinPathEdge()
	other := edge
	other.MidSecondCol = 9
	idx.Add(edge, types.NewUint64(42), hashN(1))

	if got := idx.Lookup(other, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("Lookup wrong edge = %v, want empty", got)
	}
}

func TestJoinPathEdgeIndexMultipleHashes(t *testing.T) {
	idx := NewJoinPathEdgeIndex()
	edge := testJoinPathEdge()
	idx.Add(edge, types.NewUint64(42), hashN(1))
	idx.Add(edge, types.NewUint64(42), hashN(2))

	if got := idx.Lookup(edge, types.NewUint64(42)); len(got) != 2 {
		t.Fatalf("Lookup = %v, want 2 hashes", got)
	}
}

func TestJoinPathEdgeIndexRemoveCleansUp(t *testing.T) {
	idx := NewJoinPathEdgeIndex()
	edge := testJoinPathEdge()
	h := hashN(1)
	idx.Add(edge, types.NewUint64(42), h)
	idx.Remove(edge, types.NewUint64(42), h)

	if got := idx.Lookup(edge, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", got)
	}
	if !idx.emptyForTest() {
		t.Fatalf("index not cleaned up: %+v", idx.inner)
	}
}

func TestJoinPathEdgeIndexForEachEdge(t *testing.T) {
	idx := NewJoinPathEdgeIndex()
	first := testJoinPathEdge()
	second := first
	second.RHSTable = 4
	unrelated := first
	unrelated.LHSTable = 9
	idx.Add(first, types.NewUint64(1), hashN(1))
	idx.Add(second, types.NewUint64(1), hashN(2))
	idx.Add(unrelated, types.NewUint64(1), hashN(3))

	count := 0
	idx.ForEachEdge(1, func(JoinPathEdge) {
		count++
	})
	if count != 2 {
		t.Fatalf("ForEachEdge(1) count = %d, want 2", count)
	}
}

func TestJoinRangePathEdgeIndexAddLookup(t *testing.T) {
	idx := NewJoinRangePathEdgeIndex()
	edge := testJoinPathEdge()
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

func TestJoinRangePathEdgeIndexWrongEdge(t *testing.T) {
	idx := NewJoinRangePathEdgeIndex()
	edge := testJoinPathEdge()
	idx.Add(edge,
		Bound{Value: types.NewUint64(10), Inclusive: true},
		Bound{Value: types.NewUint64(20), Inclusive: true},
		hashN(1),
	)
	other := edge
	other.MidFirstCol = 9

	if got := idx.Lookup(other, types.NewUint64(15)); len(got) != 0 {
		t.Fatalf("Lookup wrong edge = %v, want empty", got)
	}
}

func TestJoinRangePathEdgeIndexOverlappingRangesDedup(t *testing.T) {
	idx := NewJoinRangePathEdgeIndex()
	edge := testJoinPathEdge()
	h := hashN(1)
	idx.Add(edge,
		Bound{Value: types.NewUint64(10), Inclusive: true},
		Bound{Unbounded: true},
		h,
	)
	idx.Add(edge,
		Bound{Value: types.NewUint64(20), Inclusive: true},
		Bound{Unbounded: true},
		h,
	)

	if got := idx.Lookup(edge, types.NewUint64(25)); len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup overlapping ranges = %v, want deduped [%v]", got, h)
	}
}

func TestJoinRangePathEdgeIndexRemoveCleansUp(t *testing.T) {
	idx := NewJoinRangePathEdgeIndex()
	edge := testJoinPathEdge()
	h := hashN(1)
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Unbounded: true}
	idx.Add(edge, lower, upper, h)
	idx.Remove(edge, lower, upper, h)

	if got := idx.Lookup(edge, types.NewUint64(11)); len(got) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", got)
	}
	if !idx.emptyForTest() {
		t.Fatalf("index not cleaned up: %+v", idx.inner)
	}
}

func TestJoinRangePathEdgeIndexForEachEdge(t *testing.T) {
	idx := NewJoinRangePathEdgeIndex()
	first := testJoinPathEdge()
	second := first
	second.RHSTable = 4
	unrelated := first
	unrelated.LHSTable = 9
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Unbounded: true}
	idx.Add(first, lower, upper, hashN(1))
	idx.Add(second, lower, upper, hashN(2))
	idx.Add(unrelated, lower, upper, hashN(3))

	count := 0
	idx.ForEachEdge(1, func(JoinPathEdge) {
		count++
	})
	if count != 2 {
		t.Fatalf("ForEachEdge(1) count = %d, want 2", count)
	}
}
