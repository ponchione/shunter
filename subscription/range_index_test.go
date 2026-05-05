package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestRangeIndexAddLookup(t *testing.T) {
	idx := NewRangeIndex()
	h := hashN(1)
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Value: types.NewUint64(20), Inclusive: false}
	idx.Add(1, 0, lower, upper, h)

	if got := idx.Lookup(1, 0, types.NewUint64(10)); len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup lower inclusive = %v, want [%v]", got, h)
	}
	if got := idx.Lookup(1, 0, types.NewUint64(20)); len(got) != 0 {
		t.Fatalf("Lookup upper exclusive = %v, want empty", got)
	}
}

func TestRangeIndexMultipleRanges(t *testing.T) {
	idx := NewRangeIndex()
	lowHash, highHash := hashN(1), hashN(2)
	idx.Add(1, 0,
		Bound{Value: types.NewUint64(1), Inclusive: true},
		Bound{Value: types.NewUint64(10), Inclusive: true},
		lowHash,
	)
	idx.Add(1, 0,
		Bound{Value: types.NewUint64(20), Inclusive: true},
		Bound{Value: types.NewUint64(30), Inclusive: true},
		highHash,
	)

	if got := idx.Lookup(1, 0, types.NewUint64(25)); len(got) != 1 || got[0] != highHash {
		t.Fatalf("Lookup high range = %v, want [%v]", got, highHash)
	}
	if got := idx.Lookup(1, 0, types.NewUint64(15)); len(got) != 0 {
		t.Fatalf("Lookup gap = %v, want empty", got)
	}
}

func TestRangeIndexRemoveCleansUp(t *testing.T) {
	idx := NewRangeIndex()
	h := hashN(1)
	lower := Bound{Value: types.NewUint64(10), Inclusive: true}
	upper := Bound{Unbounded: true}
	idx.Add(1, 0, lower, upper, h)
	idx.Remove(1, 0, lower, upper, h)

	if got := idx.Lookup(1, 0, types.NewUint64(11)); len(got) != 0 {
		t.Fatalf("Lookup after remove = %v, want empty", got)
	}
	if len(idx.ranges) != 0 {
		t.Fatalf("ranges not cleaned up: %+v", idx.ranges)
	}
	if len(idx.cols) != 0 {
		t.Fatalf("cols not cleaned up: %+v", idx.cols)
	}
}

func TestRangeIndexLookupEmptyNotNil(t *testing.T) {
	idx := NewRangeIndex()
	got := idx.Lookup(1, 0, types.NewUint64(1))
	if got == nil {
		t.Fatal("Lookup should return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Fatalf("Lookup = %v, want empty", got)
	}
}
