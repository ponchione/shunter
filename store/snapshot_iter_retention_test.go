package store

import (
	"runtime"
	"testing"
	"time"
)

// TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration pins that
// iterators keep the snapshot alive while ranging.
func TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}
	if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(2, "bob")); err != nil {
		t.Fatal(err)
	}

	snap := cs.Snapshot()
	it := snap.TableScan(0)

	snap = nil //nolint:ineffassign,wastedassign // intentional: drop caller's reference to exercise GC

	for range 5 {
		runtime.GC()
	}

	gotLock := make(chan struct{})
	go func() {
		cs.Lock()
		close(gotLock)
		cs.Unlock()
	}()

	select {
	case <-gotLock:
		t.Fatal("write lock acquired while iterator was alive — snapshot finalizer fired mid-iteration and released RLock")
	case <-time.After(100 * time.Millisecond):
	}

	count := 0
	for range it {
		count++
	}
	if count != 2 {
		t.Fatalf("iterator yielded %d rows, want 2", count)
	}

	it = nil //nolint:ineffassign,wastedassign // intentional: drop iter reference so finalizer can run

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		select {
		case <-gotLock:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("write lock never acquired after iterator released — RLock leaked")
}
