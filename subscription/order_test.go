package subscription

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestBoundedOrderedInitialRowsKeepsOnlyTopWindow(t *testing.T) {
	orderBy := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}
	bounded := newBoundedOrderedInitialRows(orderBy, 3)
	for _, id := range []uint64{9, 1, 7, 2, 3, 8, 4} {
		if err := bounded.add(types.ProductValue{types.NewUint64(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(bounded.rows) != 3 {
		t.Fatalf("bounded rows = %d, want 3", len(bounded.rows))
	}
	got := bounded.productRows()
	want := []uint64{1, 2, 3}
	for i, row := range got {
		if id := row[0].AsUint64(); id != want[i] {
			t.Fatalf("bounded row ids = %v, want %v", rowIDs(got), want)
		}
	}
}

func TestBoundedOrderedInitialRowsPreservesScanOrderForTies(t *testing.T) {
	orderBy := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "rank", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}
	bounded := newBoundedOrderedInitialRows(orderBy, 2)
	rows := []types.ProductValue{
		{types.NewUint64(1), types.NewString("first")},
		{types.NewUint64(1), types.NewString("second")},
		{types.NewUint64(1), types.NewString("third")},
	}
	for _, row := range rows {
		if err := bounded.add(row); err != nil {
			t.Fatal(err)
		}
	}
	got := bounded.productRows()
	if len(got) != 2 {
		t.Fatalf("bounded rows = %d, want 2", len(got))
	}
	if got[0][1].AsString() != "first" || got[1][1].AsString() != "second" {
		t.Fatalf("tie order = %v, want first then second", got)
	}
}

func rowIDs(rows []types.ProductValue) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[0].AsUint64())
	}
	return out
}
