package store

import (
	"slices"
	"testing"

	"github.com/ponchione/shunter/types"
)

// OI-010 pins for BTreeIndex.SeekBounds (SPEC-001 §4.6).
// Covers inclusive / exclusive / mixed / unbounded / empty edges
// independently per endpoint, plus the half-open-equivalence contract
// with SeekRange.

func seekBoundsBuildBTree(keys []uint64) *BTreeIndex {
	bt := NewBTreeIndex()
	for _, k := range keys {
		bt.Insert(NewIndexKey(types.NewUint64(k)), types.RowID(k*10))
	}
	return bt
}

func seekBoundsCollect(seq func(func(types.RowID) bool)) []types.RowID {
	var out []types.RowID
	for rid := range seq {
		out = append(out, rid)
	}
	return out
}

func TestBTreeSeekBoundsCases(t *testing.T) {
	tests := []struct {
		name    string
		keys    []uint64
		low     Bound
		high    Bound
		want    []types.RowID
		wantNil bool
	}{
		{
			name: "inclusive inclusive",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  Inclusive(types.NewUint64(2)),
			high: Inclusive(types.NewUint64(4)),
			want: []types.RowID{20, 30, 40},
		},
		{
			name: "exclusive exclusive",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  Exclusive(types.NewUint64(2)),
			high: Exclusive(types.NewUint64(4)),
			want: []types.RowID{30},
		},
		{
			name: "exclusive inclusive",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  Exclusive(types.NewUint64(2)),
			high: Inclusive(types.NewUint64(4)),
			want: []types.RowID{30, 40},
		},
		{
			name: "unbounded low",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  UnboundedLow(),
			high: Inclusive(types.NewUint64(3)),
			want: []types.RowID{10, 20, 30},
		},
		{
			name: "unbounded high",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  Exclusive(types.NewUint64(3)),
			high: UnboundedHigh(),
			want: []types.RowID{40, 50},
		},
		{
			name:    "low greater than high",
			keys:    []uint64{1, 2, 3, 4, 5},
			low:     Inclusive(types.NewUint64(4)),
			high:    Inclusive(types.NewUint64(2)),
			wantNil: true,
		},
		{
			name:    "exclusive same value",
			keys:    []uint64{1, 2, 3, 4, 5},
			low:     Exclusive(types.NewUint64(3)),
			high:    Exclusive(types.NewUint64(3)),
			wantNil: true,
		},
		{
			name: "inclusive same value",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  Inclusive(types.NewUint64(3)),
			high: Inclusive(types.NewUint64(3)),
			want: []types.RowID{30},
		},
		{
			name: "exclusive low at existing key",
			keys: []uint64{1, 2, 3, 4, 5},
			low:  Exclusive(types.NewUint64(3)),
			high: Inclusive(types.NewUint64(5)),
			want: []types.RowID{40, 50},
		},
		{
			name: "exclusive low between existing keys",
			keys: []uint64{10, 20, 30},
			low:  Exclusive(types.NewUint64(15)),
			high: UnboundedHigh(),
			want: []types.RowID{200, 300},
		},
		{
			name:    "empty index",
			low:     UnboundedLow(),
			high:    UnboundedHigh(),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bt := seekBoundsBuildBTree(tt.keys)
			got := seekBoundsCollect(bt.SeekBounds(tt.low, tt.high))
			if tt.wantNil {
				if got != nil {
					t.Fatalf("SeekBounds got %v, want nil", got)
				}
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SeekBounds got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBTreeSeekBoundsInclusiveExclusiveMatchesSeekRange(t *testing.T) {
	bt := seekBoundsBuildBTree([]uint64{1, 2, 3, 4, 5})
	low := NewIndexKey(types.NewUint64(2))
	high := NewIndexKey(types.NewUint64(4))
	half := seekBoundsCollect(bt.SeekRange(&low, &high))
	bounds := seekBoundsCollect(bt.SeekBounds(
		Inclusive(types.NewUint64(2)),
		Exclusive(types.NewUint64(4)),
	))
	if !slices.Equal(half, bounds) {
		t.Fatalf("half-open equivalence broken: SeekRange=%v SeekBounds=%v", half, bounds)
	}
}

func TestBTreeSeekBoundsBothUnboundedEqualsScan(t *testing.T) {
	bt := seekBoundsBuildBTree([]uint64{1, 2, 3, 4, 5})
	got := seekBoundsCollect(bt.SeekBounds(UnboundedLow(), UnboundedHigh()))
	want := seekBoundsCollect(bt.Scan())
	if !slices.Equal(got, want) {
		t.Fatalf("SeekBounds unbounded = %v, want Scan() = %v", got, want)
	}
}

func TestBTreeSeekBoundsMultipleRowIDsPerKeyAscending(t *testing.T) {
	bt := NewBTreeIndex()
	k := NewIndexKey(types.NewUint64(5))
	bt.Insert(k, 300)
	bt.Insert(k, 100)
	bt.Insert(k, 200)
	got := seekBoundsCollect(bt.SeekBounds(
		Inclusive(types.NewUint64(5)),
		Inclusive(types.NewUint64(5)),
	))
	want := []types.RowID{100, 200, 300}
	if !slices.Equal(got, want) {
		t.Fatalf("SeekBounds same-key rowIDs = %v, want ascending %v", got, want)
	}
}

func TestBTreeSeekBoundsEarlyBreak(t *testing.T) {
	bt := seekBoundsBuildBTree([]uint64{1, 2, 3, 4, 5})
	var got []types.RowID
	for rid := range bt.SeekBounds(UnboundedLow(), UnboundedHigh()) {
		got = append(got, rid)
		if len(got) == 2 {
			break
		}
	}
	want := []types.RowID{10, 20}
	if !slices.Equal(got, want) {
		t.Fatalf("early-break iteration = %v, want %v", got, want)
	}
}

// IndexSeekBounds wrapper — confirms Index wraps BTreeIndex.SeekBounds 1:1.
func TestIndexSeekBoundsDelegatesToBTree(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	for i := uint64(1); i <= 5; i++ {
		id := tbl.AllocRowID()
		if err := tbl.InsertRow(id, mkRow(i, "n")); err != nil {
			t.Fatal(err)
		}
	}
	idx := tbl.IndexByID(0)
	if idx == nil {
		t.Fatal("pk index missing")
	}
	via := seekBoundsCollect(idx.SeekBounds(
		Inclusive(types.NewUint64(2)),
		Exclusive(types.NewUint64(5)),
	))
	direct := seekBoundsCollect(idx.BTree().SeekBounds(
		Inclusive(types.NewUint64(2)),
		Exclusive(types.NewUint64(5)),
	))
	if !slices.Equal(via, direct) {
		t.Fatalf("Index.SeekBounds = %v, BTreeIndex.SeekBounds = %v; wrapper must pass through", via, direct)
	}
}
