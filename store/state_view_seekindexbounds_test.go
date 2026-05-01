package store

import (
	"slices"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// OI-010 pins for StateView.SeekIndexBounds (SPEC-001 §5.4).
// Committed rows queried via BTreeIndex.SeekBounds and filtered through
// tx.deletes; tx-local inserts linear-scanned with the same Bound
// semantics as §4.4.

func seekIndexBoundsSetup(t *testing.T, n int) (*CommittedState, []types.RowID) {
	t.Helper()
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	ids := make([]types.RowID, 0, n)
	for i := 1; i <= n; i++ {
		id := tbl.AllocRowID()
		if err := tbl.InsertRow(id, mkRow(uint64(i), "n")); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return cs, ids
}

func seekIndexBoundsCollectSorted(seq func(func(types.RowID) bool)) []types.RowID {
	out := collectRowIDs(seq)
	slices.Sort(out)
	return out
}

func TestStateViewSeekIndexBoundsExclusiveLowInclusiveHigh(t *testing.T) {
	cs, ids := seekIndexBoundsSetup(t, 5)
	sv := NewStateView(cs, NewTxState())
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Exclusive(types.NewUint64(2)),
		Inclusive(types.NewUint64(4)),
	))
	want := []types.RowID{ids[2], ids[3]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("(2,4] = %v, want %v", got, want)
	}
}

func TestStateViewSeekIndexBoundsInclusiveLowExclusiveHigh(t *testing.T) {
	cs, ids := seekIndexBoundsSetup(t, 5)
	sv := NewStateView(cs, NewTxState())
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Inclusive(types.NewUint64(2)),
		Exclusive(types.NewUint64(4)),
	))
	want := []types.RowID{ids[1], ids[2]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("[2,4) = %v, want %v", got, want)
	}
}

func TestStateViewSeekIndexBoundsBothUnboundedEqualsScanAll(t *testing.T) {
	cs, ids := seekIndexBoundsSetup(t, 5)
	sv := NewStateView(cs, NewTxState())
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	))
	want := append([]types.RowID(nil), ids...)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("unbounded = %v, want all committed rows %v", got, want)
	}
}

func TestStateViewSeekIndexBoundsFiltersTxDeletes(t *testing.T) {
	cs, ids := seekIndexBoundsSetup(t, 5)
	tx := NewTxState()
	tx.AddDelete(0, ids[2]) // hide id=3
	sv := NewStateView(cs, tx)
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Inclusive(types.NewUint64(2)),
		Inclusive(types.NewUint64(4)),
	))
	want := []types.RowID{ids[1], ids[3]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("deletes must hide id=3: got %v, want %v", got, want)
	}
}

func TestStateViewSeekIndexBoundsIncludesTxInsertsInRange(t *testing.T) {
	cs, ids := seekIndexBoundsSetup(t, 5)
	tx := NewTxState()
	txID := types.RowID(9001)
	tx.AddInsert(0, txID, mkRow(3, "tx"))
	sv := NewStateView(cs, tx)
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Inclusive(types.NewUint64(3)),
		Inclusive(types.NewUint64(3)),
	))
	want := []types.RowID{ids[2], txID}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("tx insert with key=3 must be included: got %v, want %v", got, want)
	}
}

func TestStateViewSeekIndexBoundsExcludesTxInsertsOutOfRange(t *testing.T) {
	cs, _ := seekIndexBoundsSetup(t, 5)
	tx := NewTxState()
	txID := types.RowID(9001)
	tx.AddInsert(0, txID, mkRow(100, "tx-out-of-range"))
	sv := NewStateView(cs, tx)
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Inclusive(types.NewUint64(1)),
		Inclusive(types.NewUint64(5)),
	))
	if slices.Contains(got, txID) {
		t.Fatalf("tx insert with key=100 leaked into [1,5]: got %v", got)
	}
}

func TestStateViewSeekIndexBoundsTxInsertExclusiveBoundary(t *testing.T) {
	cs, _ := seekIndexBoundsSetup(t, 0)
	tx := NewTxState()
	txAtLow := types.RowID(9001)
	txAboveLow := types.RowID(9002)
	tx.AddInsert(0, txAtLow, mkRow(3, "at-low"))
	tx.AddInsert(0, txAboveLow, mkRow(4, "above-low"))
	sv := NewStateView(cs, tx)
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Exclusive(types.NewUint64(3)),
		UnboundedHigh(),
	))
	want := []types.RowID{txAboveLow}
	if !slices.Equal(got, want) {
		t.Fatalf("exclusive low must drop tx row at boundary: got %v, want %v", got, want)
	}
}

func TestStateViewSeekIndexBoundsEmptyTxMatchesCommitted(t *testing.T) {
	cs, _ := seekIndexBoundsSetup(t, 5)
	sv := NewStateView(cs, NewTxState())
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		Inclusive(types.NewUint64(2)),
		Inclusive(types.NewUint64(4)),
	))
	tbl, _ := cs.Table(0)
	idx := tbl.IndexByID(0)
	direct := seekBoundsCollect(idx.BTree().SeekBounds(
		Inclusive(types.NewUint64(2)),
		Inclusive(types.NewUint64(4)),
	))
	slices.Sort(direct)
	if !slices.Equal(got, direct) {
		t.Fatalf("empty tx should match committed BTree: sv=%v direct=%v", got, direct)
	}
}

func TestStateViewSeekIndexBoundsUnknownTableEmpty(t *testing.T) {
	cs, _ := seekIndexBoundsSetup(t, 3)
	sv := NewStateView(cs, NewTxState())
	for range sv.SeekIndexBounds(
		schema.TableID(99), schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	) {
		t.Fatal("unknown table must yield empty iterator")
	}
}

func TestStateViewSeekIndexBoundsUnknownIndexEmpty(t *testing.T) {
	cs, _ := seekIndexBoundsSetup(t, 3)
	sv := NewStateView(cs, NewTxState())
	for range sv.SeekIndexBounds(
		0, schema.IndexID(42),
		UnboundedLow(), UnboundedHigh(),
	) {
		t.Fatal("unknown index must yield empty iterator")
	}
}

func TestStateViewSeekIndexBoundsFiltersDeletedCommittedMidIter(t *testing.T) {
	// Row deleted from Table.rows between iter construction and yield is
	// filtered by the GetRow visibility check. The SeekIndexRange precedent
	// establishes the pattern; mirror it here.
	cs, ids := seekIndexBoundsSetup(t, 3)
	tbl, _ := cs.Table(0)
	sv := NewStateView(cs, NewTxState())
	// Delete id=2's underlying row + index entry before iteration starts.
	if _, ok := tbl.DeleteRow(ids[1]); !ok {
		t.Fatal("committed row delete failed")
	}
	got := seekIndexBoundsCollectSorted(sv.SeekIndexBounds(
		0, schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	))
	want := []types.RowID{ids[0], ids[2]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("deleted row must be filtered: got %v, want %v", got, want)
	}
}

// Aliasing pin: BTreeIndex.SeekBounds walks b.entries live. Collecting at
// the StateView boundary must decouple iteration from BTree-internal
// storage, mirroring the SeekIndexRange pin
// (state_view_seekindexrange_aliasing_test.go).
func TestStateViewSeekIndexBoundsIteratesIndependentRowIDsAfterBTreeMutation(t *testing.T) {
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
	for rid := range sv.SeekIndexBounds(
		0, schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	) {
		observed = append(observed, rid)
		if !mutated {
			mutated = true
			// Drop id=1 entry mid-iter. If BTree walk were not collected
			// at the StateView boundary, slices.Delete would shift tail
			// entries and the outer loop would skip id=2.
			idx.BTree().Remove(NewIndexKey(types.NewUint64(1)), rows[0].rid)
		}
	}

	if len(observed) != len(rows) {
		t.Fatalf("observed = %v, want all %d entries present at iter-construction time", observed, len(rows))
	}
	want := make(map[types.RowID]bool, len(rows))
	for _, r := range rows {
		want[r.rid] = true
	}
	for _, rid := range observed {
		if !want[rid] {
			t.Fatalf("observed rowID %d not present at iter-construction; BTree mutation leaked into iteration", rid)
		}
		delete(want, rid)
	}
	if len(want) != 0 {
		t.Fatalf("iteration dropped rowIDs present at iter-construction: missing=%v observed=%v", want, observed)
	}
}

func TestStateViewSeekIndexBoundsEarlyBreak(t *testing.T) {
	cs, ids := seekIndexBoundsSetup(t, 5)
	sv := NewStateView(cs, NewTxState())
	var count int
	for range sv.SeekIndexBounds(
		0, schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	) {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("early-break must yield exactly 2, got %d over %d committed rows", count, len(ids))
	}
}
