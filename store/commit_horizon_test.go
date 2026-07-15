package store

import (
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func TestCommitWithValidationAtTxIDPublishesRowsAndHorizonAtomically(t *testing.T) {
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
	commitDone := make(chan error, 1)
	go func() {
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
	<-validationEntered

	snapshotReady := make(chan *CommittedSnapshot, 1)
	go func() { snapshotReady <- cs.Snapshot() }()
	select {
	case snapshot := <-snapshotReady:
		snapshot.Close()
		t.Fatal("snapshot acquired while atomic commit held the state lock")
	case <-time.After(25 * time.Millisecond):
	}

	close(allowCommit)
	if err := <-commitDone; err != nil {
		t.Fatalf("CommitWithValidationAtTxID: %v", err)
	}
	snapshot := <-snapshotReady
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
