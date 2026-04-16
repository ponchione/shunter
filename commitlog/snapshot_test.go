package commitlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func buildSnapshotCommittedState(t *testing.T) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	_, reg := testSchema()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	players, _ := cs.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")}); err != nil {
		t.Fatal(err)
	}
	return cs, reg
}

func TestSnapshotPublicAPIContractCompiles(t *testing.T) {
	var _ = SnapshotMagic
	var _ uint8 = SnapshotVersion
	var _ int = SnapshotHeaderSize
	_ = ComputeSnapshotHash
	_ = HasLockFile
	_ = CreateLockFile
	_ = RemoveLockFile
	_ = EncodeSchemaSnapshot
	_ = DecodeSchemaSnapshot
	_ = ReadSnapshot
	_ = ListSnapshots
}

func TestSnapshotHashAndLockHelpers(t *testing.T) {
	a := ComputeSnapshotHash([]byte("same"))
	b := ComputeSnapshotHash([]byte("same"))
	c := ComputeSnapshotHash([]byte("different"))
	if a != b {
		t.Fatal("same snapshot data should hash identically")
	}
	if a == c {
		t.Fatal("different snapshot data should hash differently")
	}

	dir := t.TempDir()
	if HasLockFile(dir) {
		t.Fatal("fresh dir should not have lockfile")
	}
	if err := CreateLockFile(dir); err != nil {
		t.Fatal(err)
	}
	if !HasLockFile(dir) {
		t.Fatal("CreateLockFile should create visible lock")
	}
	if err := RemoveLockFile(dir); err != nil {
		t.Fatal(err)
	}
	if HasLockFile(dir) {
		t.Fatal("RemoveLockFile should remove visible lock")
	}
}

func TestSchemaSnapshotCodecRoundTrip(t *testing.T) {
	_, reg := testSchema()
	var buf bytes.Buffer
	if err := EncodeSchemaSnapshot(&buf, reg); err != nil {
		t.Fatal(err)
	}
	tables, version, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if version != reg.Version() {
		t.Fatalf("decoded version = %d, want %d", version, reg.Version())
	}
	if len(tables) != len(reg.Tables()) {
		t.Fatalf("decoded tables = %d, want %d", len(tables), len(reg.Tables()))
	}
}

func TestCreateAndReadSnapshotRoundTrip(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	if err := writer.CreateSnapshot(cs, 77); err != nil {
		t.Fatal(err)
	}

	data, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "77"))
	if err != nil {
		t.Fatal(err)
	}
	if data.TxID != 77 {
		t.Fatalf("snapshot txID = %d, want 77", data.TxID)
	}
	if data.SchemaVersion != reg.Version() {
		t.Fatalf("snapshot schema version = %d, want %d", data.SchemaVersion, reg.Version())
	}
	playerRows := -1
	for _, table := range data.Tables {
		if table.TableID == 0 {
			playerRows = len(table.Rows)
			if len(table.Rows) >= 1 && table.Rows[0][1].AsString() != "alice" {
				t.Fatalf("first player snapshot row = %v, want alice", table.Rows[0])
			}
		}
	}
	if playerRows != 2 {
		t.Fatalf("players snapshot row count = %d, want 2 (tables=%+v)", playerRows, data.Tables)
	}
}

func TestListSnapshotsSkipsLockAndSortsNewestFirst(t *testing.T) {
	baseDir := t.TempDir()
	for _, txID := range []uint64{10, 20} {
		dir := filepath.Join(baseDir, txIDString(txID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "snapshot"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(baseDir, txIDString(30)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CreateLockFile(filepath.Join(baseDir, txIDString(30))); err != nil {
		t.Fatal(err)
	}

	ids, err := ListSnapshots(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 20 || ids[1] != 10 {
		t.Fatalf("ListSnapshots = %v, want [20 10]", ids)
	}
}

func TestReadSnapshotHashMismatch(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	if err := writer.CreateSnapshot(cs, 88); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(baseDir, "snapshots", "88", "snapshot")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "88"))
	var hashErr *SnapshotHashMismatchError
	if !errors.As(err, &hashErr) {
		t.Fatalf("expected SnapshotHashMismatchError, got %v", err)
	}
}

func TestConcurrentSnapshotReturnsInProgress(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	blocking := writer.(*FileSnapshotWriter)
	blocking.beforeWrite = make(chan struct{})
	blocking.continueWrite = make(chan struct{})

	errCh := make(chan error, 1)
	go func() { errCh <- writer.CreateSnapshot(cs, 91) }()
	<-blocking.beforeWrite
	if err := writer.CreateSnapshot(cs, 92); !errors.Is(err, ErrSnapshotInProgress) {
		t.Fatalf("second snapshot error = %v, want ErrSnapshotInProgress", err)
	}
	close(blocking.continueWrite)
	if err := <-errCh; err != nil {
		t.Fatalf("first snapshot should complete successfully: %v", err)
	}
}

func txIDString(txID uint64) string {
	return fmt.Sprintf("%d", txID)
}
