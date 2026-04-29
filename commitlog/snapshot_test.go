package commitlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func createSnapshotAt(t testing.TB, writer SnapshotWriter, cs *store.CommittedState, txID types.TxID) {
	t.Helper()
	cs.SetCommittedTxID(txID)
	if err := writer.CreateSnapshot(cs, txID); err != nil {
		t.Fatal(err)
	}
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

func TestDecodeSchemaSnapshotRejectsTrailingBytes(t *testing.T) {
	_, reg := testSchema()
	var buf bytes.Buffer
	if err := EncodeSchemaSnapshot(&buf, reg); err != nil {
		t.Fatal(err)
	}
	buf.Write([]byte{0xde, 0xad, 0xbe, 0xef})

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected trailing schema snapshot bytes to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("trailing schema snapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "trailing schema snapshot bytes") {
		t.Fatalf("trailing schema snapshot error = %v, want explicit trailing-bytes detail", err)
	}
}

func TestCreateAndReadSnapshotRoundTrip(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 77)

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

func TestSnapshotOmitsSequenceEntriesForTablesWithoutAutoincrement(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 78)

	data, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "78"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data.Sequences[0]; ok {
		t.Fatalf("players table should not have a sequence entry: %+v", data.Sequences)
	}
	if _, ok := data.Sequences[1]; ok {
		t.Fatalf("sys_clients table should not have a sequence entry: %+v", data.Sequences)
	}
	if got, ok := data.Sequences[2]; !ok || got == 0 {
		t.Fatalf("autoincrement sys_scheduled table should keep its sequence entry, got %+v", data.Sequences)
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
	createSnapshotAt(t, writer, cs, 88)
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

func TestReadSnapshotRejectsTrailingPayloadBytes(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 90)
	path := filepath.Join(baseDir, "snapshots", "90", snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, 0xde, 0xad, 0xbe, 0xef)
	hash := ComputeSnapshotHash(data[SnapshotHeaderSize:])
	copy(data[20:52], hash[:])
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "90"))
	if err == nil {
		t.Fatal("expected trailing snapshot bytes to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("trailing snapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "trailing snapshot bytes") {
		t.Fatalf("trailing snapshot error = %v, want explicit trailing-bytes detail", err)
	}
}

func TestReadSnapshotRejectsHeaderVersionMismatch(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 89)
	path := filepath.Join(baseDir, "snapshots", "89", snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[4] = SnapshotVersion + 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "89"))
	var versionErr *BadVersionError
	if !errors.As(err, &versionErr) {
		t.Fatalf("expected BadVersionError, got %v", err)
	}
	if versionErr.Got != SnapshotVersion+1 {
		t.Fatalf("bad version got = %d, want %d", versionErr.Got, SnapshotVersion+1)
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
	cs.SetCommittedTxID(91)
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

func TestCreateSnapshotUsesTempFileUntilRename(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	blocking := writer.(*FileSnapshotWriter)
	blocking.beforeWrite = make(chan struct{})
	blocking.continueWrite = make(chan struct{})

	errCh := make(chan error, 1)
	cs.SetCommittedTxID(91)
	go func() { errCh <- writer.CreateSnapshot(cs, 91) }()
	<-blocking.beforeWrite

	released := false
	release := func() {
		if !released {
			close(blocking.continueWrite)
			released = true
		}
	}
	defer release()

	snapshotDir := filepath.Join(baseDir, "snapshots", "91")
	if !HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should exist while temp file is pending")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); err != nil {
		t.Fatalf("snapshot temp file should exist before rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("final snapshot should not exist before rename, stat err=%v", err)
	}

	release()
	if err := <-errCh; err != nil {
		t.Fatalf("snapshot should complete successfully: %v", err)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after completion")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp file should be removed after completion, stat err=%v", err)
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("final snapshot should be readable: %v", err)
	}
}

func TestCreateSnapshotRenameFailureReturnsSnapshotErrorAndCleansArtifacts(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	fileWriter := writer.(*FileSnapshotWriter)
	renameErr := errors.New("rename failed")
	fileWriter.rename = func(oldPath, newPath string) error {
		if filepath.Base(oldPath) != snapshotTempFileName || filepath.Base(newPath) != snapshotFileName {
			t.Fatalf("rename paths = (%q, %q), want temp to final snapshot", oldPath, newPath)
		}
		return renameErr
	}

	cs.SetCommittedTxID(92)
	err := writer.CreateSnapshot(cs, 92)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("rename error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, renameErr) {
		t.Fatalf("rename error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "rename" || completionErr.Path == "" {
		t.Fatalf("completion error = %+v, want rename phase and path", completionErr)
	}

	snapshotDir := filepath.Join(baseDir, "snapshots", "92")
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after rename failure")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp file should be removed after rename failure, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("final snapshot should not exist after rename failure, stat err=%v", err)
	}
}

func TestCreateSnapshotDirectorySyncFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	fileWriter := writer.(*FileSnapshotWriter)
	syncErr := errors.New("sync failed")
	snapshotDir := filepath.Join(baseDir, "snapshots", "93")
	fileWriter.syncDir = func(path string) error {
		if path == snapshotDir {
			return syncErr
		}
		return nil
	}

	cs.SetCommittedTxID(93)
	err := writer.CreateSnapshot(cs, 93)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("sync error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("sync error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-snapshot" || completionErr.Path != snapshotDir {
		t.Fatalf("completion error = %+v, want sync-snapshot phase and snapshot dir", completionErr)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); err != nil {
		t.Fatalf("final snapshot should exist after rename before sync failure: %v", err)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed during sync-failure cleanup")
	}
}

func TestCreateSnapshotRemoveLockFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	fileWriter := writer.(*FileSnapshotWriter)
	removeErr := errors.New("remove lock failed")
	fileWriter.removeLock = func(string) error {
		return removeErr
	}

	cs.SetCommittedTxID(94)
	err := writer.CreateSnapshot(cs, 94)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("remove-lock error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, removeErr) {
		t.Fatalf("remove-lock error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "remove-lock" || filepath.Base(completionErr.Path) != ".lock" {
		t.Fatalf("completion error = %+v, want remove-lock phase and lock path", completionErr)
	}
	snapshotDir := filepath.Join(baseDir, "snapshots", "94")
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); err != nil {
		t.Fatalf("final snapshot should exist before lock removal failure: %v", err)
	}
	if !HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should remain when lock removal fails")
	}
}

func TestCreateSnapshotRejectsTxIDThatDoesNotMatchCommittedHorizon(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	cs.SetCommittedTxID(2)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)

	err := writer.CreateSnapshot(cs, 3)
	var mismatch *SnapshotHorizonMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SnapshotHorizonMismatchError, got %v", err)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("expected snapshot category, got %v", err)
	}
	if mismatch.SnapshotTxID != 3 || mismatch.CommittedTxID != 2 {
		t.Fatalf("mismatch = %+v, want SnapshotTxID=3 CommittedTxID=2", mismatch)
	}
}

func buildLargeSnapshotCommittedState(t testing.TB, rowCount int) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	_, reg := testSchema()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	players, ok := cs.Table(0)
	if !ok {
		t.Fatal("missing players table")
	}
	for i := 1; i <= rowCount; i++ {
		row := types.ProductValue{
			types.NewUint64(uint64(i)),
			types.NewString("player-" + strconv.Itoa(i)),
		}
		if err := players.InsertRow(players.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}
	return cs, reg
}

func TestSnapshotLargeRoundTripAndDeterministicBytes(t *testing.T) {
	cs, reg := buildLargeSnapshotCommittedState(t, 2048)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 101)
	createSnapshotAt(t, writer, cs, 102)

	data101, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "101"))
	if err != nil {
		t.Fatal(err)
	}
	data102, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "102"))
	if err != nil {
		t.Fatal(err)
	}

	playersRows101 := -1
	playersRows102 := -1
	for _, table := range data101.Tables {
		if table.TableID == 0 {
			playersRows101 = len(table.Rows)
			if got := table.Rows[len(table.Rows)-1][1].AsString(); got != "player-2048" {
				t.Fatalf("last player in snapshot 101 = %q, want player-2048", got)
			}
		}
	}
	for _, table := range data102.Tables {
		if table.TableID == 0 {
			playersRows102 = len(table.Rows)
		}
	}
	if playersRows101 != 2048 || playersRows102 != 2048 {
		t.Fatalf("player row counts = (%d, %d), want (2048, 2048)", playersRows101, playersRows102)
	}

	bytes101, err := os.ReadFile(filepath.Join(baseDir, "snapshots", "101", "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	bytes102, err := os.ReadFile(filepath.Join(baseDir, "snapshots", "102", "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes101[:8], bytes102[:8]) {
		t.Fatal("snapshot magic/version prefix differs for identical state")
	}
	if !bytes.Equal(bytes101[16:20], bytes102[16:20]) {
		t.Fatal("schema version bytes differ for identical state")
	}
	if !bytes.Equal(bytes101[20:52], bytes102[20:52]) {
		t.Fatal("hash bytes differ for identical state")
	}
	if !bytes.Equal(bytes101[52:], bytes102[52:]) {
		t.Fatal("snapshot payload differs for identical state")
	}
}

func TestReadSnapshotLargeRoundTrip(t *testing.T) {
	cs, reg := buildLargeSnapshotCommittedState(t, 4096)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 103)

	data, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "103"))
	if err != nil {
		t.Fatal(err)
	}
	if data.TxID != 103 {
		t.Fatalf("snapshot txID = %d, want 103", data.TxID)
	}
	playersRows := -1
	for _, table := range data.Tables {
		if table.TableID == 0 {
			playersRows = len(table.Rows)
			if got := table.Rows[0][1].AsString(); got != "player-1" {
				t.Fatalf("first player = %q, want player-1", got)
			}
			if got := table.Rows[len(table.Rows)-1][1].AsString(); got != "player-4096" {
				t.Fatalf("last player = %q, want player-4096", got)
			}
		}
	}
	if playersRows != 4096 {
		t.Fatalf("players rows = %d, want 4096", playersRows)
	}
}

func BenchmarkCreateSnapshotLarge(b *testing.B) {
	cs, reg := buildLargeSnapshotCommittedState(b, 4096)
	for i := 0; i < b.N; i++ {
		root := b.TempDir()
		writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
		createSnapshotAt(b, writer, cs, types.TxID(i+1))
	}
}

func txIDString(txID uint64) string {
	return fmt.Sprintf("%d", txID)
}
