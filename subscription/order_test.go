package subscription

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOrderedKeepLimitBoundariesAndSafeHints(t *testing.T) {
	limit := uint64(1)
	offset := uint64(math.MaxInt - 1)
	window := initialRowWindow{
		orderBy: []OrderByColumn{{}},
		limit:   &limit,
		offset:  &offset,
	}
	keep, err := window.orderedKeepLimit(newInitialRowCollector(context.Background(), 0), math.MaxInt)
	if err != nil || keep != math.MaxInt {
		t.Fatalf("orderedKeepLimit boundary = (%d, %v), want (%d, nil)", keep, err, math.MaxInt)
	}
	offset++
	if _, err := window.orderedKeepLimit(newInitialRowCollector(context.Background(), 0), math.MaxInt); !errors.Is(err, ErrOrderedWindowLimit) {
		t.Fatalf("orderedKeepLimit overflow error = %v, want ErrOrderedWindowLimit", err)
	}
	if got := orderedKeyCapacityHint(math.MaxInt, boundedOrderedRowKeyCapHint); got != orderedSafeKeyCapHint {
		t.Fatalf("orderedKeyCapacityHint = %d, want %d", got, orderedSafeKeyCapHint)
	}
	bounded := newBoundedOrderedInitialRows([]OrderByColumn{{}}, math.MaxInt)
	if got := cap(bounded.rows); got != orderedSafePreallocationRows {
		t.Fatalf("bounded ordered row capacity = %d, want %d", got, orderedSafePreallocationRows)
	}
}

func TestOrderedKeepLimitSeparatesRowOverflowSentinelFromWorkingSet(t *testing.T) {
	orderBy := []OrderByColumn{{}}

	tests := []struct {
		name     string
		limit    *uint64
		offset   *uint64
		rowLimit int
		maxRows  int
		want     int
		wantErr  bool
	}{
		{name: "unbounded equal caps", rowLimit: 3, maxRows: 3, want: 3},
		{name: "unbounded offset clamps to cap", offset: uint64Pointer(1), rowLimit: 3, maxRows: 3, want: 3},
		{name: "explicit below cap", limit: uint64Pointer(2), rowLimit: 10, maxRows: 3, want: 2},
		{name: "explicit equal cap", limit: uint64Pointer(3), rowLimit: 10, maxRows: 3, want: 3},
		{name: "explicit above cap", limit: uint64Pointer(4), rowLimit: 10, maxRows: 3, wantErr: true},
		{name: "explicit above row cap uses effective cap", limit: uint64Pointer(4), rowLimit: 3, maxRows: 3, want: 3},
		{name: "limit zero", limit: uint64Pointer(0), rowLimit: 3, maxRows: 3, want: 0},
		{name: "offset plus limit below cap", limit: uint64Pointer(1), offset: uint64Pointer(1), rowLimit: 10, maxRows: 3, want: 2},
		{name: "offset plus limit equal cap", limit: uint64Pointer(2), offset: uint64Pointer(1), rowLimit: 10, maxRows: 3, want: 3},
		{name: "offset plus limit above cap", limit: uint64Pointer(2), offset: uint64Pointer(2), rowLimit: 10, maxRows: 3, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			window := initialRowWindow{orderBy: orderBy, limit: tt.limit, offset: tt.offset}
			collector := newInitialRowCollector(context.Background(), tt.rowLimit)
			got, err := window.orderedKeepLimit(collector, tt.maxRows)
			if tt.wantErr {
				if !errors.Is(err, ErrOrderedWindowLimit) {
					t.Fatalf("orderedKeepLimit error = %v, want ErrOrderedWindowLimit", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("orderedKeepLimit = (%d, %v), want (%d, nil)", got, err, tt.want)
			}
		})
	}
}

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

func TestBoundedOrderedInitialRowsUsesRowPayloadTieBreak(t *testing.T) {
	orderBy := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "rank", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}
	bounded := newBoundedOrderedInitialRows(orderBy, 2)
	rows := []types.ProductValue{
		{types.NewUint64(1), types.NewString("c")},
		{types.NewUint64(1), types.NewString("a")},
		{types.NewUint64(1), types.NewString("b")},
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
	if got[0][1].AsString() != "a" || got[1][1].AsString() != "b" {
		t.Fatalf("tie order = %v, want a then b", got)
	}
}

func TestBoundedOrderedInitialRowsRetainsOnlyLiveTieBreakKeys(t *testing.T) {
	orderBy := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "rank", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}
	const keep = 4
	bounded := newBoundedOrderedInitialRows(orderBy, keep)
	for id := uint64(1_000); id > 0; id-- {
		row := types.ProductValue{types.NewUint64(1), types.NewUint64(id)}
		if err := bounded.add(row); err != nil {
			t.Fatal(err)
		}
	}
	got := bounded.productRows()
	if ids := rowUint64Column(got, 1); !slices.Equal(ids, []uint64{1, 2, 3, 4}) {
		t.Fatalf("retained tie rows = %v, want [1 2 3 4]", ids)
	}
	keyBytes := 0
	for i := range bounded.rows {
		keyBytes += len(bounded.rows[i].key)
	}
	oneKeyBytes := len(encodeOrderedRowKey(nil, got[0]))
	if keyBytes > keep*oneKeyBytes {
		t.Fatalf("retained key bytes = %d, want at most %d", keyBytes, keep*oneKeyBytes)
	}
	if len(bounded.itemKey.buf) > oneKeyBytes {
		t.Fatalf("candidate scratch key bytes = %d, want at most %d", len(bounded.itemKey.buf), oneKeyBytes)
	}
}

func uint64Pointer(v uint64) *uint64 { return &v }

func rowUint64Column(rows []types.ProductValue, column int) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[column].AsUint64())
	}
	return out
}

func rowIDs(rows []types.ProductValue) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[0].AsUint64())
	}
	return out
}
