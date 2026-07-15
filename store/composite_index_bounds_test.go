package store

import (
	"slices"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func collectCompositeRangeIDs(rows func(func(types.RowID, types.ProductValue) bool)) []uint64 {
	var ids []uint64
	for _, row := range rows {
		ids = append(ids, row[0].AsUint64())
	}
	slices.Sort(ids)
	return ids
}

func newCompositeRangeState(t *testing.T, rows ...types.ProductValue) *CommittedState {
	t.Helper()
	cs := NewCommittedState()
	cs.RegisterTable(0, NewTable(compositeIndexedSchema(false)))
	tx := NewTransaction(cs, nil)
	for _, row := range rows {
		if _, err := tx.Insert(0, row); err != nil {
			t.Fatalf("insert committed row: %v", err)
		}
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatalf("commit rows: %v", err)
	}
	return cs
}

func TestCommittedSnapshotCompositeIndexBounds(t *testing.T) {
	cs := newCompositeRangeState(t,
		compositeRow(1, "blue", 10, "before"),
		compositeRow(2, "red", 10, "first"),
		compositeRow(3, "red", 11, "second"),
		compositeRow(4, "yellow", 10, "after"),
	)
	snapshot := cs.Snapshot()
	defer snapshot.Close()

	tests := []struct {
		name       string
		lower      Bound
		upper      Bound
		wantRowIDs []uint64
	}{
		{
			name:       "inclusive tuple endpoints",
			lower:      Inclusive(types.NewString("red"), types.NewUint64(10)),
			upper:      Inclusive(types.NewString("red"), types.NewUint64(11)),
			wantRowIDs: []uint64{2, 3},
		},
		{
			name:       "exclusive tuple endpoints",
			lower:      Exclusive(types.NewString("red"), types.NewUint64(10)),
			upper:      Exclusive(types.NewString("yellow"), types.NewUint64(10)),
			wantRowIDs: []uint64{3},
		},
		{
			name:       "exclusive lower prefix skips prefix group",
			lower:      Exclusive(types.NewString("red")),
			upper:      UnboundedHigh(),
			wantRowIDs: []uint64{4},
		},
		{
			name:       "inclusive upper prefix includes prefix group",
			lower:      UnboundedLow(),
			upper:      Inclusive(types.NewString("red")),
			wantRowIDs: []uint64{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectCompositeRangeIDs(snapshot.IndexRange(0, schema.IndexID(1), tt.lower, tt.upper))
			if !slices.Equal(got, tt.wantRowIDs) {
				t.Fatalf("IndexRange row IDs = %v, want %v", got, tt.wantRowIDs)
			}
		})
	}
}

func TestTransactionCompositeIndexBoundsIncludeCommittedAndLocalRows(t *testing.T) {
	cs := newCompositeRangeState(t,
		compositeRow(1, "blue", 10, "before"),
		compositeRow(2, "red", 10, "committed"),
		compositeRow(4, "yellow", 10, "after"),
	)
	tx := NewTransaction(cs, nil)
	if _, err := tx.Insert(0, compositeRow(3, "red", 11, "local")); err != nil {
		t.Fatalf("insert transaction-local row: %v", err)
	}

	tests := []struct {
		name       string
		lower      Bound
		upper      Bound
		wantRowIDs []uint64
	}{
		{
			name:       "inclusive tuple endpoints",
			lower:      Inclusive(types.NewString("red"), types.NewUint64(10)),
			upper:      Inclusive(types.NewString("red"), types.NewUint64(11)),
			wantRowIDs: []uint64{2, 3},
		},
		{
			name:       "exclusive tuple endpoints",
			lower:      Exclusive(types.NewString("red"), types.NewUint64(10)),
			upper:      Exclusive(types.NewString("yellow"), types.NewUint64(10)),
			wantRowIDs: []uint64{3},
		},
		{
			name:       "exclusive lower prefix skips committed and local prefix group",
			lower:      Exclusive(types.NewString("red")),
			upper:      UnboundedHigh(),
			wantRowIDs: []uint64{4},
		},
		{
			name:       "inclusive upper prefix includes committed and local prefix group",
			lower:      UnboundedLow(),
			upper:      Inclusive(types.NewString("red")),
			wantRowIDs: []uint64{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectCompositeRangeIDs(tx.SeekIndexRange(0, schema.IndexID(1), tt.lower, tt.upper))
			if !slices.Equal(got, tt.wantRowIDs) {
				t.Fatalf("SeekIndexRange row IDs = %v, want %v", got, tt.wantRowIDs)
			}
		})
	}
}
