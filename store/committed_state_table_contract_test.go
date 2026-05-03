package store

import (
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
	gotLock := make(chan struct{})
	go func() {
		cs.Lock()
		close(gotLock)
		cs.Unlock()
	}()

	select {
	case <-gotLock:
		t.Fatal("writer acquired cs.Lock() while CommittedSnapshot was open — snapshot envelope did not hold RLock")
	case <-time.After(100 * time.Millisecond):
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
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(writerStarted)
		cs.Lock()
		_ = cs.committedTxID
		cs.Unlock()
		close(writerDone)
	}()
	<-writerStarted
	time.Sleep(25 * time.Millisecond)

	accessDone := make(chan string, 1)
	go func() {
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

	select {
	case msg := <-accessDone:
		cs.RUnlock()
		if msg != "" {
			t.Fatal(msg)
		}
	case <-time.After(250 * time.Millisecond):
		cs.RUnlock()
		t.Fatal("locked committed-state access blocked behind pending writer")
	}

	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writer never acquired lock after read lock released")
	}
}
