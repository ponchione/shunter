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

func TestDeltaViewIgnoresNegativeIndexColumn(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(1), types.NewString("a")}},
		nil,
	)
	dv := NewDeltaView(nil, cs, map[TableID][]ColID{1: {-1}})
	defer dv.Release()

	if got := dv.DeltaIndexScan(1, -1, types.NewUint64(1), true); len(got) != 0 {
		t.Fatalf("DeltaIndexScan negative column = %v, want no rows", got)
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

func TestDistinctChangedValueIgnoresNegativeColumn(t *testing.T) {
	st := acquireCandidateScratch()
	defer releaseCandidateScratch(st)

	tc := &store.TableChangeset{
		Inserts: []types.ProductValue{{types.NewUint64(1)}},
		Deletes: []types.ProductValue{{types.NewUint64(2)}},
	}
	calls := 0
	forEachDistinctChangedValue(st, -1, tc, func(Value) {
		calls++
	})
	if calls != 0 {
		t.Fatalf("linear distinct callback count = %d, want 0", calls)
	}

	rows := make([]types.ProductValue, distinctChangedValueLinearMax+1)
	for i := range rows {
		rows[i] = types.ProductValue{types.NewUint64(uint64(i))}
	}
	tc = &store.TableChangeset{Inserts: rows}
	forEachDistinctChangedValue(st, -1, tc, func(Value) {
		calls++
	})
	if calls != 0 {
		t.Fatalf("map distinct callback count = %d, want 0", calls)
	}
}
