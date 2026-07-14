package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Tests in this file pin deterministic panic behavior when snapshots close
// during iteration.

func expectMidIterClosePanic(t *testing.T, body func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from iter body after mid-iter Close, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if msg != "store: CommittedSnapshot used after Close" {
			t.Fatalf("unexpected panic message: %q", msg)
		}
	}()
	body()
}

func seedMultiRowTestState(t *testing.T) (*CommittedState, schema.SchemaRegistry) {
	t.Helper()
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	for i := uint64(1); i <= 3; i++ {
		if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(i, fmt.Sprintf("u%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	return cs, reg
}

func primaryIndexID(t *testing.T, reg schema.SchemaRegistry) schema.IndexID {
	t.Helper()
	ts, ok := reg.Table(0)
	if !ok {
		t.Fatal("no table 0 in registry")
	}
	pk, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("no primary index on table 0")
	}
	return pk.ID
}

func TestCommittedSnapshotTableScanPanicsOnMidIterClose(t *testing.T) {
	cs, _ := seedMultiRowTestState(t)

	snap := cs.Snapshot()
	it := snap.TableScan(0)

	expectMidIterClosePanic(t, func() {
		first := true
		for range it {
			if first {
				snap.Close()
				first = false
				continue
			}
			t.Fatal("iter continued yielding after mid-iter Close")
		}
	})
}

func TestCommittedSnapshotIndexRangePanicsOnMidIterClose(t *testing.T) {
	cs, reg := seedMultiRowTestState(t)
	pkIdx := primaryIndexID(t, reg)

	snap := cs.Snapshot()
	it := snap.IndexRange(0, pkIdx, Bound{Unbounded: true}, Bound{Unbounded: true})

	expectMidIterClosePanic(t, func() {
		first := true
		for range it {
			if first {
				snap.Close()
				first = false
				continue
			}
			t.Fatal("iter continued yielding after mid-iter Close")
		}
	})
}

func TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose(t *testing.T) {
	cs, reg := seedMultiRowTestState(t)
	pkIdx := primaryIndexID(t, reg)

	snap := cs.Snapshot()

	// Collect multiple RowIDs via IndexSeek per pk value, then feed
	// the aggregated slice through rowsFromRowIDs to cover the
	// IndexScan → rowsFromRowIDs iter path with a multi-step loop.
	var rowIDs []types.RowID
	for i := uint64(1); i <= 3; i++ {
		ids := snap.IndexSeek(0, pkIdx, NewIndexKey(types.NewUint64(i)))
		rowIDs = append(rowIDs, ids...)
	}
	if len(rowIDs) < 2 {
		t.Fatalf("expected at least 2 rowIDs to exercise mid-iter-close, got %d", len(rowIDs))
	}
	tbl, _ := cs.Table(0)
	it := snap.rowsFromRowIDs(tbl, rowIDs, StoreReadKindIndexScan)

	expectMidIterClosePanic(t, func() {
		first := true
		for range it {
			if first {
				snap.Close()
				first = false
				continue
			}
			t.Fatal("iter continued yielding after mid-iter Close")
		}
	})
}

func assertCloseDefersWriterUntilIteratorExits(t *testing.T, cs *CommittedState, snap *CommittedSnapshot, it RowIterator) {
	t.Helper()

	yielded := make(chan struct{})
	continueRead := make(chan struct{})
	iterResult := make(chan any, 1)
	go func() {
		defer func() { iterResult <- recover() }()
		for range it {
			close(yielded)
			<-continueRead
		}
	}()

	select {
	case <-yielded:
	case <-time.After(2 * time.Second):
		t.Fatal("iterator did not yield")
	}
	snap.Close()

	writerDone := make(chan struct{})
	go func() {
		cs.Lock()
		close(writerDone)
		cs.Unlock()
	}()
	select {
	case <-writerDone:
		t.Fatal("writer acquired state lock while snapshot iterator was active")
	case <-time.After(100 * time.Millisecond):
	}

	close(continueRead)
	select {
	case got := <-iterResult:
		if got != "store: CommittedSnapshot used after Close" {
			t.Fatalf("iterator panic = %v, want closed-snapshot panic", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("iterator did not exit after close")
	}
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not acquire state lock after iterator exited")
	}
}

func TestCommittedSnapshotConcurrentCloseRetainsLockForTableScan(t *testing.T) {
	cs, _ := seedMultiRowTestState(t)
	snap := cs.Snapshot()
	assertCloseDefersWriterUntilIteratorExits(t, cs, snap, snap.TableScan(0))
}

func TestCommittedSnapshotConcurrentCloseRetainsLockForIndexRange(t *testing.T) {
	cs, reg := seedMultiRowTestState(t)
	snap := cs.Snapshot()
	assertCloseDefersWriterUntilIteratorExits(t, cs, snap, snap.IndexRange(
		0,
		primaryIndexID(t, reg),
		Bound{Unbounded: true},
		Bound{Unbounded: true},
	))
}

func TestCommittedSnapshotConcurrentCloseRetainsLockForIndexSeekIterator(t *testing.T) {
	cs, reg := seedMultiRowTestState(t)
	snap := cs.Snapshot()
	assertCloseDefersWriterUntilIteratorExits(t, cs, snap, snap.SeekIndex(
		0,
		primaryIndexID(t, reg),
		types.NewUint64(1),
	))
}

func TestCommittedSnapshotConcurrentCloseRetainsLockForPointReadLease(t *testing.T) {
	cs, _ := seedMultiRowTestState(t)
	snap := cs.Snapshot()

	// Point reads use the same lease around their entire state access. Hold one
	// explicitly so the writer-blocking guarantee can be coordinated without a
	// timing-dependent race against a very short GetRow call.
	snap.beginRead()
	snap.Close()
	writerDone := make(chan struct{})
	go func() {
		cs.Lock()
		close(writerDone)
		cs.Unlock()
	}()
	select {
	case <-writerDone:
		t.Fatal("writer acquired state lock while point-read lease was active")
	case <-time.After(100 * time.Millisecond):
	}
	snap.endRead()
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not acquire state lock after point-read lease exited")
	}
}
