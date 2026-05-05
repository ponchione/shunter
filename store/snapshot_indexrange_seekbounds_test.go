package store

import (
	"slices"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Pins for CommittedSnapshot.IndexRange backed by Index.SeekBounds
// (SPEC-001 §7.2). The v0 impl scanned the full BTree and filtered
// rows with matchesLowerBound / matchesUpperBound after extracting the
// key back out of the materialized row; the fix delegates endpoint
// handling to BTreeIndex.SeekBounds so string / bytes / float
// exclusive-bound predicates hit the binary-search start point.
// Covers inclusive/exclusive control for subscription range predicates and the
// read-view aliasing closure for the collect-at-boundary pattern.

func indexRangeSetup(t *testing.T, n int) (*CommittedState, []types.RowID) {
	t.Helper()
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)
	ids := make([]types.RowID, 0, n)
	for i := 1; i <= n; i++ {
		rid, err := tx.Insert(0, mkRow(uint64(i), "n"))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rid)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}
	return cs, ids
}

func indexRangeCollect(seq func(func(types.RowID, types.ProductValue) bool)) []uint64 {
	var out []uint64
	for _, row := range seq {
		out = append(out, row[0].AsUint64())
	}
	return out
}

func TestCommittedSnapshotIndexRangeCases(t *testing.T) {
	tests := []struct {
		name string
		low  Bound
		high Bound
		want []uint64
	}{
		{
			name: "exclusive low inclusive high",
			low:  Exclusive(types.NewUint64(2)),
			high: Inclusive(types.NewUint64(4)),
			want: []uint64{3, 4},
		},
		{
			name: "exclusive low exclusive high",
			low:  Exclusive(types.NewUint64(2)),
			high: Exclusive(types.NewUint64(4)),
			want: []uint64{3},
		},
		{
			name: "unbounded high",
			low:  Inclusive(types.NewUint64(3)),
			high: UnboundedHigh(),
			want: []uint64{3, 4, 5},
		},
		{
			name: "both unbounded ordered scan",
			low:  UnboundedLow(),
			high: UnboundedHigh(),
			want: []uint64{1, 2, 3, 4, 5},
		},
		{
			name: "low greater than high",
			low:  Inclusive(types.NewUint64(4)),
			high: Inclusive(types.NewUint64(2)),
		},
		{
			name: "exclusive endpoints at same key",
			low:  Exclusive(types.NewUint64(3)),
			high: Exclusive(types.NewUint64(3)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, _ := indexRangeSetup(t, 5)
			snap := cs.Snapshot()
			defer snap.Close()
			got := indexRangeCollect(snap.IndexRange(0, schema.IndexID(0), tt.low, tt.high))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("IndexRange got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommittedSnapshotIndexRangeEarlyBreak(t *testing.T) {
	cs, _ := indexRangeSetup(t, 5)
	snap := cs.Snapshot()
	defer snap.Close()
	var seen []uint64
	for _, row := range snap.IndexRange(
		0, schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	) {
		seen = append(seen, row[0].AsUint64())
		if len(seen) == 2 {
			break
		}
	}
	if len(seen) != 2 {
		t.Fatalf("early-break must yield exactly 2, got %d", len(seen))
	}
}

// Aliasing pin: BTreeIndex.SeekBounds walks b.entries live. Collecting
// the range once at the CommittedReadView boundary must decouple
// iteration from BTree-internal storage, mirroring the
// StateView.SeekIndexBounds precedent
// (state_view_seekindexbounds_test.go::
// TestStateViewSeekIndexBoundsIteratesIndependentRowIDsAfterBTreeMutation).
// Without the collect, a mid-iter BTree mutation could shift b.entries
// in place and the outer loop would skip a row present at
// iter-construction time.
func TestCommittedSnapshotIndexRangeIteratesIndependentRowIDsAfterBTreeMutation(t *testing.T) {
	cs, ids := indexRangeSetup(t, 5)
	tbl, _ := cs.Table(0)
	idx := tbl.IndexByID(schema.IndexID(0))
	if idx == nil {
		t.Fatal("pk index not found")
	}

	snap := cs.Snapshot()
	defer snap.Close()

	var observed []uint64
	mutated := false
	for _, row := range snap.IndexRange(
		0, schema.IndexID(0),
		UnboundedLow(), UnboundedHigh(),
	) {
		observed = append(observed, row[0].AsUint64())
		if !mutated {
			mutated = true
			// Drop id=1's BTree entry mid-iter. An un-collected outer
			// loop would see slices.Delete shift tail entries and skip
			// id=2.
			idx.BTree().Remove(NewIndexKey(types.NewUint64(1)), ids[0])
		}
	}

	want := []uint64{1, 2, 3, 4, 5}
	if !slices.Equal(observed, want) {
		t.Fatalf("BTree mutation leaked into iteration: observed=%v want=%v", observed, want)
	}
}
