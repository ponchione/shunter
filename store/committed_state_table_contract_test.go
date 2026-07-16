package store

import (
	"runtime"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// These tests pin the pointer-lifetime constraints documented on
// CommittedState.Table.

func TestCommittedStateTableSameEnvelopeReturnsSamePointer(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
		},
	}
	cs := NewCommittedState()
	original := NewTable(ts)
	cs.RegisterTable(0, original)

	a, okA := cs.Table(0)
	b, okB := cs.Table(0)
	if !okA || !okB {
		t.Fatalf("Table(0) ok=%v,%v — want both true", okA, okB)
	}
	if a != b {
		t.Fatalf("Table(0) returned different pointers for the same envelope: a=%p b=%p", a, b)
	}
	if a != original {
		t.Fatalf("Table(0) returned pointer that is not the registered table: got=%p want=%p", a, original)
	}
}

func TestCommittedStateTableRetainedPointerIsStaleAfterReRegister(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
		},
	}
	cs := NewCommittedState()
	original := NewTable(ts)
	cs.RegisterTable(0, original)

	retained, _ := cs.Table(0)

	// Re-register swaps the map entry. The retained pointer is now stale:
	// it no longer tracks the committed table-of-record.
	replacement := NewTable(ts)
	cs.RegisterTable(0, replacement)

	current, ok := cs.Table(0)
	if !ok {
		t.Fatal("Table(0) ok=false after re-register")
	}
	if current != replacement {
		t.Fatalf("Table(0) after re-register = %p, want replacement %p", current, replacement)
	}
	if retained == current {
		t.Fatal("retained *Table pointer tracked the re-register; contract says retained pointers are stale after RegisterTable swap")
	}

	// A write committed via the replacement must not appear on the
	// retained pointer — pin the stale-after-re-register hazard shape.
	rid := replacement.AllocRowID()
	if err := replacement.InsertRow(rid, types.ProductValue{types.NewUint64(42)}); err != nil {
		t.Fatal(err)
	}
	if retained.RowCount() != 0 {
		t.Fatalf("retained.RowCount = %d, want 0 — retained pointer must not observe writes to the replacement table", retained.RowCount())
	}
	if replacement.RowCount() != 1 {
		t.Fatalf("replacement.RowCount = %d, want 1", replacement.RowCount())
	}
}

func TestCommittedStateTableSnapshotEnvelopeHoldsRLockUntilClose(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
		},
	}
	cs := NewCommittedState()
	cs.RegisterTable(0, NewTable(ts))

	snap := cs.Snapshot()
	defer func() {
		if snap != nil {
			snap.Close()
		}
	}()

	// While the snapshot is open, *Table access via snap.cs.Table is
	// covered by the snapshot's RLock. A concurrent writer attempting
	// cs.Lock() must block until snap.Close() releases the RLock.
	lockAttempted := make(chan struct{})
	gotLock := make(chan struct{})
	go func() {
		close(lockAttempted)
		cs.Lock()
		_ = cs.committedTxID
		cs.Unlock()
		close(gotLock)
	}()
	defer func() {
		if snap != nil {
			snap.Close()
			snap = nil
		}
		select {
		case <-gotLock:
		case <-time.After(2 * time.Second):
			t.Error("writer goroutine did not finish during cleanup")
		}
	}()

	select {
	case <-lockAttempted:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine did not attempt cs.Lock")
	}
	runtime.Gosched()
	select {
	case <-gotLock:
		t.Fatal("writer acquired cs.Lock() while CommittedSnapshot was open — snapshot envelope did not hold RLock")
	default:
	}

	snap.Close()
	snap = nil

	select {
	case <-gotLock:
	case <-time.After(2 * time.Second):
		t.Fatal("writer never acquired cs.Lock() after snapshot Close — RLock leaked past Close")
	}
}

func TestCommittedStateLockedAccessDoesNotReenterRWMutexBehindPendingWriter(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	ts := &schema.TableSchema{
		ID:   0,
		Name: "rows",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
		},
	}
	cs := NewCommittedState()
	table := NewTable(ts)
	cs.RegisterTable(0, table)

	cs.RLock()
	readLocked := true
	writerAttempted := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(writerAttempted)
		cs.Lock()
		_ = cs.committedTxID
		cs.Unlock()
		close(writerDone)
	}()
	defer func() {
		if readLocked {
			cs.RUnlock()
		}
		select {
		case <-writerDone:
		case <-time.After(2 * time.Second):
			t.Error("writer goroutine did not finish during cleanup")
		}
	}()
	select {
	case <-writerAttempted:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine never attempted the lock")
	}
	// With one runnable P, yielding here runs the writer until its Lock call
	// blocks on the held read lock. This establishes writer preference without
	// guessing how long the scheduler needs.
	runtime.Gosched()
	select {
	case <-writerDone:
		t.Fatal("writer completed while the read lock was still held")
	default:
	}

	accessDone := make(chan string, 1)
	accessExited := make(chan struct{})
	go func() {
		defer close(accessExited)
		ids := cs.TableIDsLocked()
		if len(ids) != 1 || ids[0] != 0 {
			accessDone <- "locked table IDs did not return registered table"
			return
		}
		got, ok := cs.TableLocked(0)
		if !ok || got != table {
			accessDone <- "locked table lookup did not return registered table"
			return
		}
		accessDone <- ""
	}()
	defer func() {
		select {
		case <-accessExited:
		case <-time.After(2 * time.Second):
			t.Error("locked-access goroutine did not finish during cleanup")
		}
	}()

	select {
	case msg := <-accessDone:
		cs.RUnlock()
		readLocked = false
		if msg != "" {
			t.Fatal(msg)
		}
	case <-time.After(250 * time.Millisecond):
		cs.RUnlock()
		readLocked = false
		t.Fatal("locked committed-state access blocked behind pending writer")
	}

	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writer never acquired lock after read lock released")
	}
}
