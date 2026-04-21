package store

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// Tests in this file pin the OI-005 use-after-Close sub-hazard: the three
// iterator entry points on *CommittedSnapshot (TableScan, IndexScan,
// IndexRange) check s.ensureOpen() at iter-body entry, so a caller who
// calls Close() between iter construction and range silently races the
// freed RLock instead of hitting a deterministic panic. The fix adds a
// closed-state check at iter-body entry and converts the mis-use into a
// consistent "CommittedSnapshot used after Close" panic matching the
// existing construction-time contract.

func expectUseAfterClosePanic(t *testing.T, iter func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from iter body after Close, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if msg != "store: CommittedSnapshot used after Close" {
			t.Fatalf("unexpected panic message: %q", msg)
		}
	}()
	iter()
}

func TestCommittedSnapshotTableScanPanicsAfterClose(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	snap := cs.Snapshot()
	it := snap.TableScan(0)
	snap.Close()

	expectUseAfterClosePanic(t, func() {
		for range it {
			t.Fatal("iter body yielded a row after Close")
		}
	})
}

func TestCommittedSnapshotIndexScanPanicsAfterClose(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	ts, _ := reg.Table(0)
	pkIdx, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("no primary index on table 0")
	}

	snap := cs.Snapshot()
	it := snap.IndexScan(0, pkIdx.ID, types.NewUint64(1))
	snap.Close()

	expectUseAfterClosePanic(t, func() {
		for range it {
			t.Fatal("iter body yielded a row after Close")
		}
	})
}

func TestCommittedSnapshotIndexRangePanicsAfterClose(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	ts, _ := reg.Table(0)
	pkIdx, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("no primary index on table 0")
	}

	snap := cs.Snapshot()
	it := snap.IndexRange(0, pkIdx.ID, Bound{Unbounded: true}, Bound{Unbounded: true})
	snap.Close()

	expectUseAfterClosePanic(t, func() {
		for range it {
			t.Fatal("iter body yielded a row after Close")
		}
	})
}
