package commitlog

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ponchione/shunter/types"
)

func requirePOSIXPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are POSIX-specific")
	}
}

func requirePathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	requirePOSIXPermissions(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", filepath.Base(path), got, want)
	}
}

func TestCreateSegmentCreatesPrivateFile(t *testing.T) {
	dir := t.TempDir()
	writer, err := CreateSegment(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	requirePathMode(t, filepath.Join(dir, SegmentFileName(1)), 0o600)
}

func TestCreateOffsetIndexCreatesPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	index, err := CreateOffsetIndex(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	requirePathMode(t, path, 0o600)
}

func TestCreateLockFileCreatesPrivateMarker(t *testing.T) {
	dir := t.TempDir()
	if err := CreateLockFile(dir); err != nil {
		t.Fatal(err)
	}
	requirePathMode(t, filepath.Join(dir, ".lock"), 0o600)
}

func TestCreateSnapshotCreatesPrivateDirectoryAndFile(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	cs.SetCommittedTxID(1)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	if err := writer.CreateSnapshot(cs, types.TxID(1)); err != nil {
		t.Fatal(err)
	}

	snapshotDir := filepath.Join(root, "snapshots", "1")
	requirePathMode(t, snapshotDir, 0o700)
	requirePathMode(t, filepath.Join(snapshotDir, snapshotFileName), 0o600)
}
