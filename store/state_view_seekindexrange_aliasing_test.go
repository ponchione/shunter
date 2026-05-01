package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Pins the read-view StateView.SeekIndexRange BTree-alias sub-hazard closure.
// BTreeIndex.SeekRange is an iter.Seq that walks b.entries live: the outer
// loop uses len(b.entries) and indexes b.entries[i] each step, reading the
// backing array directly. If a yield callback reaches into the entry and
// drops a key (e.g. removes the last rowID of an entry that sits behind
// the current cursor), slices.Delete(b.entries, idx, idx+1) shifts the
// tail of b.entries down in place. The outer loop's i++ then skips over
// an entry that would otherwise have been yielded. Materializing the
// range at the StateView boundary decouples iteration from BTree-internal
// storage. Mirrors the SeekIndex and CommittedSnapshot IndexSeek
// regression contracts.
//
// Under executor single-writer discipline no concurrent writer runs
// during a reducer's synchronous iteration, so this test drives the
// contract-violating mutation directly via the BTree handle to simulate
// a future path in which a yield callback reaches into committed state.

func TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation(t *testing.T) {
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

	// Five rows, each under a distinct PK value — one b.entries entry
	// per row. Insert-order-independent assertions below, but we keep
	// ids [1..5] for readability.
	type pair struct {
		id  uint64
		rid types.RowID
	}
	rows := make([]pair, 0, 5)
	for i := uint64(1); i <= 5; i++ {
		rid := tbl.AllocRowID()
		if err := tbl.InsertRow(rid, types.ProductValue{types.NewUint64(i)}); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, pair{id: i, rid: rid})
	}

	sv := NewStateView(cs, NewTxState())
	idx := tbl.IndexByID(schema.IndexID(0))
	if idx == nil {
		t.Fatal("pk index not found")
	}

	var observed []types.RowID
	mutated := false
	for rid := range sv.SeekIndexRange(0, schema.IndexID(0), nil, nil) {
		observed = append(observed, rid)
		if !mutated {
			mutated = true
			// Drop the entry for id=1 — its rowID matches the first
			// yielded rid. slices.Delete(b.entries, 0, 1) shifts [2,3,4,5]
			// to [0,1,2,3] in place inside the same backing array. The
			// outer loop then advances i from 0 to 1, reading what was
			// b.entries[2] (id=3), skipping id=2 entirely. The row is
			// NOT removed from Table.rows, so the GetRow visibility
			// filter in StateView.SeekIndexRange cannot mask the drift.
			idx.BTree().Remove(NewIndexKey(types.NewUint64(1)), rows[0].rid)
		}
	}

	if len(observed) != len(rows) {
		t.Fatalf("observed rowIDs = %v, want all %d pre-iter entries (mid-iter BTree mutation must not leak into iteration)", observed, len(rows))
	}
	want := make(map[types.RowID]bool, len(rows))
	for _, r := range rows {
		want[r.rid] = true
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
