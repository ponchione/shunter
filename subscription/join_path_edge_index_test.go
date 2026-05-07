package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func testJoinPathTraversalEdge(t *testing.T) joinPathTraversalEdge {
	t.Helper()
	return mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{0, 2},
		[]ColID{1, 3},
		4,
	)
}

func TestJoinPathTraversalIndexAddLookup(t *testing.T) {
	idx := newJoinPathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	h := hashN(1)
	idx.Add(edge, types.NewUint64(42), h)

	got := idx.Lookup(edge, types.NewUint64(42))
	if len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup = %v, want [%v]", got, h)
	}
}

func TestJoinPathTraversalIndexWrongFilterValue(t *testing.T) {
	idx := newJoinPathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	idx.Add(edge, types.NewUint64(42), hashN(1))

	if got := idx.Lookup(edge, types.NewUint64(99)); len(got) != 0 {
		t.Fatalf("Lookup wrong value = %v, want empty", got)
	}
}

func TestJoinPathTraversalIndexWrongEdge(t *testing.T) {
	idx := newJoinPathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	other := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{0, 9},
		[]ColID{1, 3},
		4,
	)
	idx.Add(edge, types.NewUint64(42), hashN(1))

	if got := idx.Lookup(other, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("Lookup wrong edge = %v, want empty", got)
	}
}

func TestJoinPathTraversalIndexMultipleHashes(t *testing.T) {
	idx := newJoinPathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	idx.Add(edge, types.NewUint64(42), hashN(1))
	idx.Add(edge, types.NewUint64(42), hashN(2))

	if got := idx.Lookup(edge, types.NewUint64(42)); len(got) != 2 {
		t.Fatalf("Lookup = %v, want 2 hashes", got)
	}
}

func TestJoinPathTraversalIndexRemoveCleansUp(t *testing.T) {
	idx := newJoinPathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	h := hashN(1)
	idx.Add(edge, types.NewUint64(42), h)
	idx.Remove(edge, types.NewUint64(42), h)

	if got := idx.Lookup(edge, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", got)
	}
	if !idx.emptyForTest() {
		t.Fatalf("index not cleaned up: %+v", idx)
	}
}

func TestJoinPathTraversalIndexForEachEdge(t *testing.T) {
	idx := newJoinPathTraversalIndex()
	first := testJoinPathTraversalEdge(t)
	second := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 4},
		[]ColID{0, 2},
		[]ColID{1, 3},
		4,
	)
	unrelated := mustJoinPathTraversalEdge(t,
		[]TableID{9, 2, 3},
		[]ColID{0, 2},
		[]ColID{1, 3},
		4,
	)
	idx.Add(first, types.NewUint64(1), hashN(1))
	idx.Add(second, types.NewUint64(1), hashN(2))
	idx.Add(unrelated, types.NewUint64(1), hashN(3))

	count := 0
	idx.ForEachEdge(1, func(joinPathTraversalEdge) {
		count++
	})
	if count != 2 {
		t.Fatalf("ForEachEdge(1) count = %d, want 2", count)
	}
}

func TestJoinRangePathTraversalIndexAddLookup(t *testing.T) {
	idx := newJoinRangePathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
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

func TestJoinRangePathTraversalIndexWrongEdge(t *testing.T) {
	idx := newJoinRangePathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	idx.Add(edge,
		Bound{Value: types.NewUint64(10), Inclusive: true},
		Bound{Value: types.NewUint64(20), Inclusive: true},
		hashN(1),
	)
	other := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{0, 2},
		[]ColID{9, 3},
		4,
	)

	if got := idx.Lookup(other, types.NewUint64(15)); len(got) != 0 {
		t.Fatalf("Lookup wrong edge = %v, want empty", got)
	}
}

func TestJoinRangePathTraversalIndexOverlappingRangesDedup(t *testing.T) {
	idx := newJoinRangePathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
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

func TestJoinRangePathTraversalIndexRemoveCleansUp(t *testing.T) {
	idx := newJoinRangePathTraversalIndex()
	edge := testJoinPathTraversalEdge(t)
	h := hashN(1)
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Unbounded: true}
	idx.Add(edge, lower, upper, h)
	idx.Remove(edge, lower, upper, h)

	if got := idx.Lookup(edge, types.NewUint64(11)); len(got) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", got)
	}
	if !idx.emptyForTest() {
		t.Fatalf("index not cleaned up: %+v", idx)
	}
}

func TestJoinRangePathTraversalIndexForEachEdge(t *testing.T) {
	idx := newJoinRangePathTraversalIndex()
	first := testJoinPathTraversalEdge(t)
	second := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 4},
		[]ColID{0, 2},
		[]ColID{1, 3},
		4,
	)
	unrelated := mustJoinPathTraversalEdge(t,
		[]TableID{9, 2, 3},
		[]ColID{0, 2},
		[]ColID{1, 3},
		4,
	)
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Unbounded: true}
	idx.Add(first, lower, upper, hashN(1))
	idx.Add(second, lower, upper, hashN(2))
	idx.Add(unrelated, lower, upper, hashN(3))

	count := 0
	idx.ForEachEdge(1, func(joinPathTraversalEdge) {
		count++
	})
	if count != 2 {
		t.Fatalf("ForEachEdge(1) count = %d, want 2", count)
	}
}
