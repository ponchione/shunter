package shunter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/types"
)

func TestRuntimeCreateSnapshotWritesCommittedHorizon(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.state.SetCommittedTxID(7)

	txID, err := rt.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot returned error: %v", err)
	}
	if txID != 7 {
		t.Fatalf("CreateSnapshot txID = %d, want 7", txID)
	}

	snapshots, err := commitlog.ListSnapshots(rt.dataDir)
	if err != nil {
		t.Fatalf("ListSnapshots returned error: %v", err)
	}
	if !runtimeSnapshotListed(snapshots, txID) {
		t.Fatalf("snapshots = %v, want tx %d", snapshots, txID)
	}
}

func TestRuntimeCompactCommitLogDeletesCoveredSegments(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.state.SetCommittedTxID(2)
	snapshotTxID, err := rt.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot returned error: %v", err)
	}

	covered := makeRuntimeCompactionSegment(t, rt.dataDir, 1, 1, 2)
	active := makeRuntimeCompactionSegment(t, rt.dataDir, 3, 3)

	if err := rt.CompactCommitLog(snapshotTxID); err != nil {
		t.Fatalf("CompactCommitLog returned error: %v", err)
	}
	runtimeAssertFileMissing(t, covered)
	runtimeAssertFileExists(t, active)
}

func TestRuntimeCompactCommitLogRequiresCompletedSnapshot(t *testing.T) {
	rt := buildValidTestRuntime(t)

	err := rt.CompactCommitLog(99)
	if !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("CompactCommitLog error = %v, want ErrSnapshotNotFound", err)
	}
}

func runtimeSnapshotListed(snapshots []types.TxID, want types.TxID) bool {
	for _, got := range snapshots {
		if got == want {
			return true
		}
	}
	return false
}

func makeRuntimeCompactionSegment(t *testing.T, dir string, startTx uint64, txs ...uint64) string {
	t.Helper()

	sw, err := commitlog.CreateSegment(dir, startTx)
	if err != nil {
		t.Fatalf("CreateSegment returned error: %v", err)
	}
	for _, tx := range txs {
		rec := &commitlog.Record{
			TxID:       tx,
			RecordType: commitlog.RecordTypeChangeset,
			Payload:    []byte{byte(tx)},
		}
		if err := sw.Append(rec); err != nil {
			t.Fatalf("Append(%d) returned error: %v", tx, err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close segment returned error: %v", err)
	}
	return filepath.Join(dir, commitlog.SegmentFileName(startTx))
}

func runtimeAssertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err=%v", filepath.Base(path), err)
	}
}

func runtimeAssertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", filepath.Base(path), err)
	}
}
