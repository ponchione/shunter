package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Tests in this file pin the OI-005 CommittedSnapshot.IndexSeek shared-state
// escape route closure. The underlying BTreeIndex.Seek returns a live alias
// of the index entry's internal []RowID. A caller that retained the returned
// slice past snapshot.Close() would race any subsequent writer's Insert /
// Remove on the same key — slices.Insert / slices.Delete mutate the backing
// array in place (or replace it, leaving the aliased header stale). The fix
// clones the slice at the public read-view boundary so callers cannot alias
// BTree-internal storage. These tests exercise both Insert (append at key)
// and Remove (delete at key) post-Close writer mutations and assert the
// returned slice stays stable.

func buildAliasingIndexState(t *testing.T) (*CommittedState, schema.IndexID) {
	t.Helper()
	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "color", Type: types.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "by_color", Columns: []int{1}, Unique: false},
		},
	}
	cs := NewCommittedState()
	cs.RegisterTable(0, NewTable(ts))
	return cs, schema.IndexID(1)
}

func mkColorRow(id uint64, color string) types.ProductValue {
	return types.ProductValue{types.NewUint64(id), types.NewString(color)}
}

func TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert(t *testing.T) {
	cs, colorIdxID := buildAliasingIndexState(t)
	tbl, _ := cs.Table(0)

	for _, row := range []types.ProductValue{
		mkColorRow(1, "red"),
		mkColorRow(2, "red"),
	} {
		if err := tbl.InsertRow(tbl.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}

	snap := cs.Snapshot()
	key := NewIndexKey(types.NewString("red"))
	got := snap.IndexSeek(0, colorIdxID, key)
	if len(got) != 2 {
		t.Fatalf("expected 2 rowIDs for red, got %d", len(got))
	}
	before := append([]types.RowID(nil), got...)
	snap.Close()

	// Post-close writer appends another row at the same key. This mutates
	// the BTree entry's internal []RowID. If IndexSeek aliased instead of
	// cloned, `got` would observe the mutation.
	if err := tbl.InsertRow(tbl.AllocRowID(), mkColorRow(3, "red")); err != nil {
		t.Fatal(err)
	}

	if len(got) != len(before) {
		t.Fatalf("returned slice length drifted post-Close: before=%d after=%d", len(before), len(got))
	}
	for i := range got {
		if got[i] != before[i] {
			t.Fatalf("returned slice element drifted post-Close at [%d]: before=%d after=%d", i, before[i], got[i])
		}
	}

	// Sanity: the writer did land — a fresh snapshot sees all three rows.
	snap2 := cs.Snapshot()
	defer snap2.Close()
	now := snap2.IndexSeek(0, colorIdxID, key)
	if len(now) != 3 {
		t.Fatalf("expected writer to land 3 red rows, got %d", len(now))
	}
}

func TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove(t *testing.T) {
	cs, colorIdxID := buildAliasingIndexState(t)
	tbl, _ := cs.Table(0)

	rid1 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid1, mkColorRow(1, "blue")); err != nil {
		t.Fatal(err)
	}
	rid2 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid2, mkColorRow(2, "blue")); err != nil {
		t.Fatal(err)
	}
	rid3 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid3, mkColorRow(3, "blue")); err != nil {
		t.Fatal(err)
	}

	snap := cs.Snapshot()
	key := NewIndexKey(types.NewString("blue"))
	got := snap.IndexSeek(0, colorIdxID, key)
	if len(got) != 3 {
		t.Fatalf("expected 3 rowIDs for blue, got %d", len(got))
	}
	before := append([]types.RowID(nil), got...)
	snap.Close()

	// Post-close writer removes one row at the same key. slices.Delete
	// shifts later elements down inside the backing array — an aliased
	// header would see the shift.
	if _, ok := tbl.DeleteRow(rid2); !ok {
		t.Fatal("DeleteRow failed")
	}

	if len(got) != len(before) {
		t.Fatalf("returned slice length drifted post-Close: before=%d after=%d", len(before), len(got))
	}
	for i := range got {
		if got[i] != before[i] {
			t.Fatalf("returned slice element drifted post-Close at [%d]: before=%d after=%d", i, before[i], got[i])
		}
	}

	// Sanity: the remove did land — a fresh snapshot sees two rows.
	snap2 := cs.Snapshot()
	defer snap2.Close()
	now := snap2.IndexSeek(0, colorIdxID, key)
	if len(now) != 2 {
		t.Fatalf("expected writer to leave 2 blue rows, got %d", len(now))
	}
}
