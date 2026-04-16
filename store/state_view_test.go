package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestStateViewGetRowPrefersTxInsertAndDelete(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	committedID := tbl.AllocRowID()
	if err := tbl.InsertRow(committedID, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	tx := NewTxState()
	tx.AddDelete(0, committedID)
	txInsertID := types.RowID(999)
	tx.AddInsert(0, txInsertID, mkRow(2, "bob"))

	sv := NewStateView(cs, tx)

	if _, ok := sv.GetRow(0, committedID); ok {
		t.Fatal("committed row marked deleted in tx should be hidden")
	}
	row, ok := sv.GetRow(0, txInsertID)
	if !ok || row[1].AsString() != "bob" {
		t.Fatal("tx-local insert should be visible")
	}
	if _, ok := sv.GetRow(0, 123456); ok {
		t.Fatal("unknown row should be absent")
	}
}

func TestStateViewScanTableMergesCommittedMinusDeletesPlusInserts(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	keepID := tbl.AllocRowID()
	deleteID := tbl.AllocRowID()
	if err := tbl.InsertRow(keepID, mkRow(1, "keep")); err != nil {
		t.Fatal(err)
	}
	if err := tbl.InsertRow(deleteID, mkRow(2, "delete")); err != nil {
		t.Fatal(err)
	}

	tx := NewTxState()
	tx.AddDelete(0, deleteID)
	insertID := types.RowID(999)
	tx.AddInsert(0, insertID, mkRow(3, "insert"))

	sv := NewStateView(cs, tx)
	got := map[types.RowID]string{}
	for id, row := range sv.ScanTable(0) {
		got[id] = row[1].AsString()
	}
	if len(got) != 2 {
		t.Fatalf("ScanTable size = %d, want 2", len(got))
	}
	if got[keepID] != "keep" {
		t.Fatalf("ScanTable keep row = %q, want keep", got[keepID])
	}
	if _, ok := got[deleteID]; ok {
		t.Fatal("deleted committed row should not be yielded")
	}
	if got[insertID] != "insert" {
		t.Fatalf("ScanTable insert row = %q, want insert", got[insertID])
	}
}

func TestStateViewSeekIndexAndRangeIncludeTxRowsAndFilterDeletes(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	id1 := tbl.AllocRowID()
	id2 := tbl.AllocRowID()
	id3 := tbl.AllocRowID()
	for _, item := range []struct {
		id  types.RowID
		row types.ProductValue
	}{{id1, mkRow(1, "a")}, {id2, mkRow(2, "b")}, {id3, mkRow(3, "c")}} {
		if err := tbl.InsertRow(item.id, item.row); err != nil {
			t.Fatal(err)
		}
	}

	tx := NewTxState()
	tx.AddDelete(0, id2)
	insertID := types.RowID(999)
	tx.AddInsert(0, insertID, mkRow(2, "tx-b"))

	sv := NewStateView(cs, tx)
	exact := collectRowIDs(sv.SeekIndex(0, schema.IndexID(0), NewIndexKey(types.NewUint64(2))))
	if len(exact) != 1 || exact[0] != insertID {
		t.Fatalf("SeekIndex exact = %v, want [%d]", exact, insertID)
	}

	rangeIDs := collectRowIDs(sv.SeekIndexRange(
		0,
		schema.IndexID(0),
		ptrIndexKey(NewIndexKey(types.NewUint64(2))),
		ptrIndexKey(NewIndexKey(types.NewUint64(4))),
	))
	want := map[types.RowID]bool{id3: true, insertID: true}
	if len(rangeIDs) != 2 {
		t.Fatalf("SeekIndexRange len = %d, want 2 (%v)", len(rangeIDs), rangeIDs)
	}
	for _, id := range rangeIDs {
		if !want[id] {
			t.Fatalf("unexpected range row id %d in %v", id, rangeIDs)
		}
	}
}

func TestStateViewHandlesNilPerTableMapsGracefully(t *testing.T) {
	cs, _ := buildTestState()
	tx := NewTxState()
	sv := NewStateView(cs, tx)

	if _, ok := sv.GetRow(0, 1); ok {
		t.Fatal("empty StateView should not find missing row")
	}
	for range sv.ScanTable(0) {
		t.Fatal("empty StateView should not yield rows")
	}
	for range sv.SeekIndex(0, schema.IndexID(0), NewIndexKey(types.NewUint64(1))) {
		t.Fatal("empty StateView should not yield exact index rows")
	}
	for range sv.SeekIndexRange(0, schema.IndexID(0), nil, nil) {
		t.Fatal("empty StateView should not yield range rows")
	}
}

func collectRowIDs(seq func(func(types.RowID) bool)) []types.RowID {
	var out []types.RowID
	for id := range seq {
		out = append(out, id)
	}
	return out
}

func ptrIndexKey(k IndexKey) *IndexKey { return &k }
