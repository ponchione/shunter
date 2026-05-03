package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Pins that StateView.ScanTable materializes committed rows before yielding.

func TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
		},
	}
	cs := NewCommittedState()
	cs.RegisterTable(0, NewTable(ts))
	tbl, _ := cs.Table(0)

	// Five rows. Exact RowIDs are what we assert over: all five RowIDs
	// present at iter-construction time must be yielded, regardless of
	// Go's map iteration order.
	rowIDs := make([]types.RowID, 0, 5)
	for i := uint64(1); i <= 5; i++ {
		rid := tbl.AllocRowID()
		if err := tbl.InsertRow(rid, types.ProductValue{types.NewUint64(i)}); err != nil {
			t.Fatal(err)
		}
		rowIDs = append(rowIDs, rid)
	}

	sv := NewStateView(cs, NewTxState())

	var observed []types.RowID
	mutated := false
	for id := range sv.ScanTable(0) {
		observed = append(observed, id)
		if !mutated {
			mutated = true
			// Delete a not-yet-yielded row directly from the committed
			// table's t.rows map. Without materialization the outer
			// live map iteration would obey Go spec §6.3: the deleted
			// entry, not yet reached, would not be produced — yielded
			// count would be four. With materialization the iter call
			// already collected five (id, rowCopy) pairs, so the
			// delete does not leak into the yield loop.
			for _, rid := range rowIDs {
				if rid != id {
					if _, ok := tbl.DeleteRow(rid); !ok {
						t.Fatalf("DeleteRow(%d) returned !ok", rid)
					}
					break
				}
			}
		}
	}

	if len(observed) != len(rowIDs) {
		t.Fatalf("observed rowIDs = %v, want all %d pre-iter entries (mid-iter committed delete must not leak into iteration)", observed, len(rowIDs))
	}
	want := make(map[types.RowID]bool, len(rowIDs))
	for _, rid := range rowIDs {
		want[rid] = true
	}
	for _, rid := range observed {
		if !want[rid] {
			t.Fatalf("observed rowID %d was not present at iter-construction time; committed delete leaked into iteration", rid)
		}
		delete(want, rid)
	}
	if len(want) != 0 {
		t.Fatalf("iteration dropped rowIDs present at iter-construction time: missing=%v observed=%v", want, observed)
	}
}
