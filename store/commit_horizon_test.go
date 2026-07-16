package store

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func TestCommitWithValidationAtTxIDPublishesRowsAndHorizonAtomically(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	cs := NewCommittedState()
	ts := pkSchema()
	cs.RegisterTable(ts.ID, NewTable(ts))
	tx := NewTransaction(cs, nil)
	if _, err := tx.Insert(ts.ID, mkRow(1, "alice")); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	tx.Seal()

	validationEntered := make(chan struct{})
	allowCommit := make(chan struct{})
	var allowCommitOnce sync.Once
	commitDone := make(chan error, 1)
	commitExited := make(chan struct{})
	go func() {
		defer close(commitExited)
		_, err := CommitWithValidationAtTxID(cs, tx, 9, func(changeset *Changeset) error {
			if changeset.TxID != 9 {
				t.Errorf("validation changeset TxID = %d, want 9", changeset.TxID)
			}
			close(validationEntered)
			<-allowCommit
			return nil
		})
		commitDone <- err
	}()

	snapshotAttempted := make(chan struct{})
	snapshotReady := make(chan *CommittedSnapshot, 1)
	snapshotExited := make(chan struct{})
	snapshotStarted := false
	defer func() {
		allowCommitOnce.Do(func() { close(allowCommit) })
		select {
		case <-commitExited:
		case <-time.After(2 * time.Second):
			t.Error("commit goroutine did not finish during cleanup")
		}
		if snapshotStarted {
			select {
			case <-snapshotExited:
			case <-time.After(2 * time.Second):
				t.Error("snapshot goroutine did not finish during cleanup")
			}
			select {
			case pending := <-snapshotReady:
				pending.Close()
			default:
			}
		}
	}()

	select {
	case <-validationEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("commit validation callback was not entered")
	}

	snapshotStarted = true
	go func() {
		defer close(snapshotExited)
		close(snapshotAttempted)
		snapshotReady <- cs.Snapshot()
	}()
	select {
	case <-snapshotAttempted:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot goroutine did not attempt Snapshot")
	}
	// With a single P, this yield runs the competing goroutine through its
	// Snapshot call until it blocks on the commit's write lock.
	runtime.Gosched()
	select {
	case snapshot := <-snapshotReady:
		snapshot.Close()
		t.Fatal("snapshot acquired while atomic commit held the state lock")
	default:
	}

	allowCommitOnce.Do(func() { close(allowCommit) })
	var commitErr error
	select {
	case commitErr = <-commitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("commit did not finish after validation was released")
	}
	<-commitExited
	if commitErr != nil {
		t.Fatalf("CommitWithValidationAtTxID: %v", commitErr)
	}
	var snapshot *CommittedSnapshot
	select {
	case snapshot = <-snapshotReady:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot did not acquire after commit completed")
	}
	<-snapshotExited
	if snapshot == nil {
		t.Fatal("Snapshot returned nil")
	}
	defer snapshot.Close()
	if got := cs.CommittedTxIDLocked(); got != 9 {
		t.Fatalf("snapshot horizon = %d, want 9", got)
	}
	if got := snapshot.RowCount(ts.ID); got != 1 {
		t.Fatalf("snapshot row count = %d, want 1", got)
	}
	if row, ok := snapshot.GetRow(ts.ID, types.RowID(1)); !ok || !row.Equal(mkRow(1, "alice")) {
		t.Fatalf("snapshot row = %#v, %t; want alice", row, ok)
	}
}
