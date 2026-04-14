package subscription

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func simpleChangeset(tableID TableID, inserts, deletes []types.ProductValue) *store.Changeset {
	return &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			tableID: {
				TableID:   tableID,
				TableName: "t",
				Inserts:   inserts,
				Deletes:   deletes,
			},
		},
	}
}

func TestDeltaViewBasicAccess(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(1), types.NewString("a")}},
		[]types.ProductValue{{types.NewUint64(2), types.NewString("b")}},
	)
	dv := NewDeltaView(nil, cs, nil)
	if got := dv.InsertedRows(1); len(got) != 1 {
		t.Fatalf("InsertedRows = %v, want 1", got)
	}
	if got := dv.DeletedRows(1); len(got) != 1 {
		t.Fatalf("DeletedRows = %v, want 1", got)
	}
}

func TestDeltaViewBuildsIndexesOnlyForActiveColumns(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(1), types.NewString("b")},
		}, nil,
	)
	dv := NewDeltaView(nil, cs, map[TableID][]ColID{1: {0}})
	got := dv.DeltaIndexScan(1, 0, types.NewUint64(1), true)
	if len(got) != 2 {
		t.Fatalf("DeltaIndexScan = %v, want 2 rows", got)
	}
}

func TestDeltaViewPanicsOnUnindexedColumn(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(1), types.NewString("a")}},
		nil,
	)
	dv := NewDeltaView(nil, cs, map[TableID][]ColID{1: {0}})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("DeltaIndexScan on non-indexed column should panic")
		}
	}()
	_ = dv.DeltaIndexScan(1, 1, types.NewString("a"), true)
}

func TestDeltaViewEmptyTable(t *testing.T) {
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{}}
	dv := NewDeltaView(nil, cs, map[TableID][]ColID{1: {0}})
	if got := dv.InsertedRows(1); got != nil {
		t.Fatalf("empty = %v, want nil", got)
	}
}

func TestDeltaViewNilChangeset(t *testing.T) {
	dv := NewDeltaView(nil, nil, nil)
	if got := dv.InsertedRows(1); got != nil {
		t.Fatalf("nil changeset = %v, want nil", got)
	}
}
