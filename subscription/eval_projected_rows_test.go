package subscription

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestSubtractProjectedRowsByKeyPreservesBagSemantics(t *testing.T) {
	rowA := types.ProductValue{types.NewUint64(1), types.NewString("a")}
	rowB := types.ProductValue{types.NewUint64(2), types.NewString("b")}
	rowC := types.ProductValue{types.NewUint64(3), types.NewString("c")}

	current := []types.ProductValue{rowA, rowA, rowB, rowC}
	inserted := []types.ProductValue{rowA, rowB, rowB}

	got := subtractProjectedRowsByKey(current, inserted)
	if len(got) != 2 {
		t.Fatalf("remaining len = %d, want 2 (%v)", len(got), got)
	}
	if encodeRowKey(got[0]) != encodeRowKey(rowA) {
		t.Fatalf("remaining[0] = %v, want rowA", got[0])
	}
	if encodeRowKey(got[1]) != encodeRowKey(rowC) {
		t.Fatalf("remaining[1] = %v, want rowC", got[1])
	}
}

func TestProjectedRowsBeforeAppendsDeletesAfterBagSubtraction(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
	})
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			1: {
				TableID: 1,
				Inserts: []types.ProductValue{
					{types.NewUint64(1), types.NewString("a")},
				},
				Deletes: []types.ProductValue{
					{types.NewUint64(9), types.NewString("gone")},
				},
			},
		},
	}
	dv := NewDeltaView(view, cs, nil)
	defer dv.Release()

	got := projectedRowsBefore(dv, 1)
	if len(got) != 3 {
		t.Fatalf("projectedRowsBefore len = %d, want 3 (%v)", len(got), got)
	}

	gotCounts := make(map[string]int, len(got))
	for _, row := range got {
		gotCounts[encodeRowKey(row)]++
	}
	wantCounts := map[string]int{
		encodeRowKey(types.ProductValue{types.NewUint64(1), types.NewString("a")}):    1,
		encodeRowKey(types.ProductValue{types.NewUint64(2), types.NewString("b")}):    1,
		encodeRowKey(types.ProductValue{types.NewUint64(9), types.NewString("gone")}): 1,
	}
	if len(gotCounts) != len(wantCounts) {
		t.Fatalf("projectedRowsBefore distinct rows = %v, want %v", gotCounts, wantCounts)
	}
	for key, want := range wantCounts {
		if gotCounts[key] != want {
			t.Fatalf("projectedRowsBefore counts[%q] = %d, want %d (all=%v)", key, gotCounts[key], want, got)
		}
	}
}
