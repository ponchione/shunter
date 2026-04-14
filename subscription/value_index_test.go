package subscription

import (
	"sort"
	"testing"

	"github.com/ponchione/shunter/types"
)

func hashN(n byte) QueryHash {
	var h QueryHash
	h[0] = n
	return h
}

func sortHashes(hs []QueryHash) []QueryHash {
	out := append([]QueryHash(nil), hs...)
	sort.Slice(out, func(i, j int) bool {
		for k := 0; k < len(out[i]); k++ {
			if out[i][k] != out[j][k] {
				return out[i][k] < out[j][k]
			}
		}
		return false
	})
	return out
}

func TestValueIndexAddLookup(t *testing.T) {
	v := NewValueIndex()
	h := hashN(1)
	v.Add(1, 0, types.NewUint64(42), h)
	got := v.Lookup(1, 0, types.NewUint64(42))
	if len(got) != 1 || got[0] != h {
		t.Fatalf("Lookup = %v, want [%v]", got, h)
	}
}

func TestValueIndexMultipleHashesSameKey(t *testing.T) {
	v := NewValueIndex()
	h1, h2 := hashN(1), hashN(2)
	v.Add(1, 0, types.NewUint64(42), h1)
	v.Add(1, 0, types.NewUint64(42), h2)
	got := sortHashes(v.Lookup(1, 0, types.NewUint64(42)))
	want := sortHashes([]QueryHash{h1, h2})
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Lookup = %v, want %v", got, want)
	}
}

func TestValueIndexDifferentValues(t *testing.T) {
	v := NewValueIndex()
	h1, h2 := hashN(1), hashN(2)
	v.Add(1, 0, types.NewUint64(1), h1)
	v.Add(1, 0, types.NewUint64(2), h2)
	if got := v.Lookup(1, 0, types.NewUint64(1)); len(got) != 1 || got[0] != h1 {
		t.Fatalf("Lookup(1) = %v, want [h1]", got)
	}
	if got := v.Lookup(1, 0, types.NewUint64(2)); len(got) != 1 || got[0] != h2 {
		t.Fatalf("Lookup(2) = %v, want [h2]", got)
	}
}

func TestValueIndexRemove(t *testing.T) {
	v := NewValueIndex()
	h := hashN(1)
	v.Add(1, 0, types.NewUint64(42), h)
	v.Remove(1, 0, types.NewUint64(42), h)
	if got := v.Lookup(1, 0, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("after remove: Lookup = %v, want empty", got)
	}
}

func TestValueIndexRemoveCleansUp(t *testing.T) {
	v := NewValueIndex()
	h := hashN(1)
	v.Add(1, 0, types.NewUint64(42), h)
	v.Remove(1, 0, types.NewUint64(42), h)
	if len(v.args) != 0 {
		t.Fatalf("args not cleaned up: %+v", v.args)
	}
	if len(v.cols) != 0 {
		t.Fatalf("cols not cleaned up: %+v", v.cols)
	}
}

func TestValueIndexTrackedColumns(t *testing.T) {
	v := NewValueIndex()
	h := hashN(1)
	v.Add(1, 0, types.NewUint64(1), h)
	v.Add(1, 2, types.NewUint64(2), h)
	cols := v.TrackedColumns(1)
	if len(cols) != 2 {
		t.Fatalf("TrackedColumns = %v, want 2 cols", cols)
	}
}

func TestValueIndexTrackedColumnsEmpty(t *testing.T) {
	v := NewValueIndex()
	if cols := v.TrackedColumns(999); len(cols) != 0 {
		t.Fatalf("TrackedColumns untracked = %v, want empty", cols)
	}
}

func TestValueIndexLookupEmptyNotNil(t *testing.T) {
	v := NewValueIndex()
	got := v.Lookup(1, 0, types.NewUint64(1))
	if got == nil {
		t.Fatal("Lookup should return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Fatalf("Lookup = %v, want empty", got)
	}
}
