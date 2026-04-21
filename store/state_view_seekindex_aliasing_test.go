package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Pins the OI-005 StateView.SeekIndex BTree-alias sub-hazard closure.
// The underlying BTreeIndex.Seek(key) returns a live alias of the entry's
// []RowID. If that slice is ranged over directly, an in-place mutation
// of the backing array (slices.Delete of a middle element shifts the
// tail down inside the same backing) is visible to the iteration even
// though the iteration's captured len/cap are stale. Cloning at the
// seek boundary decouples iteration from BTree-internal storage.
// Mirrors docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md.
//
// Under executor single-writer discipline no writer runs during a real
// iteration, so this test drives the mutation directly via the BTree
// handle to simulate a contract-violating path (e.g. a future refactor
// letting a yield callback reach into committed state).

func TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "color", Type: types.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "by_color", Columns: []int{1}, Unique: false},
		},
	}
	cs := NewCommittedState()
	cs.RegisterTable(0, NewTable(ts))
	tbl, _ := cs.Table(0)

	rids := make([]types.RowID, 0, 5)
	for i := 1; i <= 5; i++ {
		rid := tbl.AllocRowID()
		rids = append(rids, rid)
		if err := tbl.InsertRow(rid, types.ProductValue{types.NewUint64(uint64(i)), types.NewString("red")}); err != nil {
			t.Fatal(err)
		}
	}

	sv := NewStateView(cs, NewTxState())
	idx := tbl.IndexByID(schema.IndexID(1))
	if idx == nil {
		t.Fatal("index not found")
	}
	key := NewIndexKey(types.NewString("red"))

	var observed []types.RowID
	firstSeen := false
	for rid := range sv.SeekIndex(0, schema.IndexID(1), key) {
		observed = append(observed, rid)
		if !firstSeen {
			firstSeen = true
			// Remove a middle rowID from the BTree entry directly. This
			// mirrors a contract-violating yield callback that reaches into
			// CommittedState mid-iter. slices.Delete shifts the tail down
			// in place inside the entry's backing array — a
			// pre-clone iteration would read those shifted positions and
			// yield drifted RowIDs (or a zero from the zeroed trailing slot)
			// instead of the entries that were present at seek time. The
			// row itself is left in the table's rows map so the GetRow
			// filter does not mask the shift.
			idx.BTree().Remove(key, rids[2])
		}
	}

	if len(observed) != len(rids) {
		t.Fatalf("observed rowIDs = %v, want all %d pre-iter entries (mid-iter BTree mutation must not leak into iteration)", observed, len(rids))
	}
	want := make(map[types.RowID]bool, len(rids))
	for _, rid := range rids {
		want[rid] = true
	}
	for _, rid := range observed {
		if !want[rid] {
			t.Fatalf("observed rowID %d was not present at iter-construction time; BTree mutation leaked into iteration", rid)
		}
		delete(want, rid)
	}
	if len(want) != 0 {
		t.Fatalf("iteration dropped rowIDs present at iter-construction time: missing=%v observed=%v", want, observed)
	}
}
