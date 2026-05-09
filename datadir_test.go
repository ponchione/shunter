package shunter

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestBackupAndRestoreDataDirHelpersCopyCompleteDirectory(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "snapshots", "7"), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	writeDataDirTestBytes(t, dataDir, "00000000000000000001.log", []byte("segment-1"))
	writeDataDirTestBytes(t, filepath.Join(dataDir, "snapshots", "7"), "snapshot", []byte("snapshot-7"))

	backupDir := filepath.Join(dir, "backup")
	if err := BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir returned error: %v", err)
	}
	assertDataDirFileBytes(t, filepath.Join(backupDir, "00000000000000000001.log"), []byte("segment-1"))
	assertDataDirFileBytes(t, filepath.Join(backupDir, "snapshots", "7", "snapshot"), []byte("snapshot-7"))

	restoreDir := filepath.Join(dir, "restored")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatalf("create empty restore dir: %v", err)
	}
	if err := RestoreDataDir(backupDir, restoreDir); err != nil {
		t.Fatalf("RestoreDataDir returned error: %v", err)
	}
	assertDataDirFileBytes(t, filepath.Join(restoreDir, "00000000000000000001.log"), []byte("segment-1"))
	assertDataDirFileBytes(t, filepath.Join(restoreDir, "snapshots", "7", "snapshot"), []byte("snapshot-7"))
}

func TestBackupAndRestoreCleanShutdownRuntimeRecoversState(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	backupDir := filepath.Join(dir, "backup")
	restoreDir := filepath.Join(dir, "restored")

	rt, err := Build(dataDirBackupTestModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build source runtime: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start source runtime: %v", err)
	}
	res, err := rt.CallReducer(context.Background(), "insert_message", []byte("before-backup"))
	if err != nil {
		t.Fatalf("CallReducer source insert: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("source insert status = %v, err = %v, want committed", res.Status, res.Error)
	}
	if err := rt.WaitUntilDurable(context.Background(), res.TxID); err != nil {
		t.Fatalf("WaitUntilDurable source tx: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close source runtime: %v", err)
	}

	if err := BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir returned error: %v", err)
	}
	if err := RestoreDataDir(backupDir, restoreDir); err != nil {
		t.Fatalf("RestoreDataDir returned error: %v", err)
	}

	restored, err := Build(dataDirBackupTestModule(), Config{DataDir: restoreDir})
	if err != nil {
		t.Fatalf("Build restored runtime: %v", err)
	}
	if err := restored.Start(context.Background()); err != nil {
		t.Fatalf("Start restored runtime: %v", err)
	}
	defer restored.Close()
	assertDataDirRestoredMessageBodies(t, restored, []string{"before-backup"})
}

func TestReducerCommitDurabilityFailureBeforeAppendDoesNotRecoverCommit(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	rt, err := Build(dataDirBackupTestModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build runtime: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start runtime: %v", err)
	}
	rt.mu.Lock()
	durability := rt.durability
	rt.mu.Unlock()
	if durability == nil {
		t.Fatal("runtime durability worker is nil")
	}
	if _, err := durability.Close(); err != nil {
		t.Fatalf("close durability worker before reducer commit: %v", err)
	}

	res, err := rt.CallReducer(context.Background(), "insert_message", []byte("lost-before-append"))
	if err != nil {
		t.Fatalf("CallReducer admission error: %v", err)
	}
	if res.Status != StatusFailedInternal {
		t.Fatalf("status = %v, err = %v, want failed internal", res.Status, res.Error)
	}
	if res.Error == nil {
		t.Fatal("CallReducer result error is nil, want post-commit failure")
	}
	assertErrorContains(t, res.Error, "post-commit panic")
	assertErrorContains(t, res.Error, "enqueue after close")
	assertDataDirRuntimeStateMessageBodies(t, rt, []string{"lost-before-append"})

	if err := rt.Close(); err != nil {
		t.Fatalf("Close runtime after durability failure: %v", err)
	}
	recovered, err := Build(dataDirBackupTestModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build recovered runtime: %v", err)
	}
	if err := recovered.Start(context.Background()); err != nil {
		t.Fatalf("Start recovered runtime: %v", err)
	}
	defer recovered.Close()
	assertDataDirRestoredMessageBodies(t, recovered, nil)
}

func TestRestoreWithIncompatibleContractFailsBuildWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	backupDir := filepath.Join(dir, "backup")
	restoreDir := filepath.Join(dir, "restored")

	rt, err := Build(dataDirBackupTestModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build source runtime: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start source runtime: %v", err)
	}
	res, err := rt.CallReducer(context.Background(), "insert_message", []byte("before-mismatch"))
	if err != nil {
		t.Fatalf("CallReducer source insert: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("source insert status = %v, err = %v, want committed", res.Status, res.Error)
	}
	if err := rt.WaitUntilDurable(context.Background(), res.TxID); err != nil {
		t.Fatalf("WaitUntilDurable source tx: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close source runtime: %v", err)
	}
	if err := BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir returned error: %v", err)
	}
	if err := RestoreDataDir(backupDir, restoreDir); err != nil {
		t.Fatalf("RestoreDataDir returned error: %v", err)
	}

	mismatch := messagesTableDef()
	mismatch.Columns[1].Name = "text"
	_, err = Build(NewModule("chat").SchemaVersion(1).TableDef(mismatch), Config{DataDir: restoreDir})
	if err == nil {
		t.Fatal("Build with incompatible restored contract returned nil error")
	}
	var schemaErr *commitlog.SchemaMismatchError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("Build error = %v, want SchemaMismatchError", err)
	}
	assertErrorContains(t, err, "name mismatch")
	assertErrorContains(t, err, "body")
	assertErrorContains(t, err, "text")

	compatible, err := Build(dataDirBackupTestModule(), Config{DataDir: restoreDir})
	if err != nil {
		t.Fatalf("Build compatible runtime after mismatch: %v", err)
	}
	if err := compatible.Start(context.Background()); err != nil {
		t.Fatalf("Start compatible runtime after mismatch: %v", err)
	}
	defer compatible.Close()
	assertDataDirRestoredMessageBodies(t, compatible, []string{"before-mismatch"})
}

func TestBackupDataDirRejectsExistingOutputWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	outputDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	original := []byte("existing backup data")
	writeDataDirTestBytes(t, outputDir, "existing", original)

	err := BackupDataDir(dataDir, outputDir)
	if err == nil {
		t.Fatal("BackupDataDir returned nil, want existing-output error")
	}
	assertErrorContains(t, err, "backup output "+outputDir+" already exists")
	assertDataDirFileBytes(t, filepath.Join(outputDir, "existing"), original)
}

func TestRestoreDataDirRejectsNonEmptyDestinationWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	writeDataDirTestBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))
	restoreDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatalf("create restore dir: %v", err)
	}
	original := []byte("existing runtime data")
	writeDataDirTestBytes(t, restoreDir, "existing", original)

	err := RestoreDataDir(backupDir, restoreDir)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil, want non-empty destination error")
	}
	assertErrorContains(t, err, "restore destination "+restoreDir+" is not empty")
	assertDataDirFileBytes(t, filepath.Join(restoreDir, "existing"), original)
}

func TestRestoreDataDirErrorsNameBackupSource(t *testing.T) {
	dir := t.TempDir()
	restoreDir := filepath.Join(dir, "restore")
	missingBackup := filepath.Join(dir, "missing-backup")

	err := RestoreDataDir(missingBackup, restoreDir)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil for missing backup")
	}
	assertErrorContains(t, err, "read backup "+missingBackup)
	if strings.Contains(err.Error(), "source data dir") {
		t.Fatalf("RestoreDataDir error = %q, should name backup source", err)
	}

	backupFile := writeDataDirTestBytes(t, dir, "backup-file", []byte("not a backup directory"))
	err = RestoreDataDir(backupFile, restoreDir)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil for file backup")
	}
	assertErrorContains(t, err, "backup "+backupFile+" is not a directory")
}

func TestDataDirHelpersRejectUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}

	for _, tc := range []struct {
		name    string
		run     func() error
		wantErr string
	}{
		{
			name: "backup blank source",
			run: func() error {
				return BackupDataDir(" \t", filepath.Join(dir, "backup"))
			},
			wantErr: "data dir path is required",
		},
		{
			name: "backup blank output",
			run: func() error {
				return BackupDataDir(dataDir, "\n")
			},
			wantErr: "backup output path is required",
		},
		{
			name: "restore blank backup",
			run: func() error {
				return RestoreDataDir(" ", filepath.Join(dir, "restore"))
			},
			wantErr: "backup path is required",
		},
		{
			name: "restore blank data dir",
			run: func() error {
				return RestoreDataDir(dataDir, "\t")
			},
			wantErr: "data dir path is required",
		},
		{
			name: "backup destination inside source",
			run: func() error {
				return BackupDataDir(dataDir, filepath.Join(dataDir, "backup"))
			},
			wantErr: "must not be inside source data dir",
		},
		{
			name: "restore destination inside backup",
			run: func() error {
				return RestoreDataDir(dataDir, filepath.Join(dataDir, "restore"))
			},
			wantErr: "must not be inside source data dir",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatal("helper returned nil, want error")
			}
			assertErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestDataDirHelpersRejectSymlinkSourcesAndEntries(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}

	sourceLink := filepath.Join(dir, "data-link")
	if err := os.Symlink(dataDir, sourceLink); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	err := BackupDataDir(sourceLink, filepath.Join(dir, "backup-from-link"))
	if err == nil {
		t.Fatal("BackupDataDir returned nil for symlink source")
	}
	assertErrorContains(t, err, "is a symlink; refusing to copy")

	target := writeDataDirTestBytes(t, dataDir, "target", []byte("target"))
	if err := os.Symlink(target, filepath.Join(dataDir, "entry-link")); err != nil {
		t.Fatalf("create entry symlink: %v", err)
	}
	err = BackupDataDir(dataDir, filepath.Join(dir, "backup-with-entry-link"))
	if err == nil {
		t.Fatal("BackupDataDir returned nil for symlink entry")
	}
	assertErrorContains(t, err, "entry-link")
	assertErrorContains(t, err, "is a symlink; refusing to copy")
}

func TestRestoreDataDirRejectsSymlinkBackupSourcesAndEntries(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}

	backupLink := filepath.Join(dir, "backup-link")
	if err := os.Symlink(backupDir, backupLink); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	err := RestoreDataDir(backupLink, filepath.Join(dir, "restore-from-link"))
	if err == nil {
		t.Fatal("RestoreDataDir returned nil for symlink backup")
	}
	assertErrorContains(t, err, "is a symlink; refusing to restore")

	target := writeDataDirTestBytes(t, backupDir, "target", []byte("target"))
	if err := os.Symlink(target, filepath.Join(backupDir, "entry-link")); err != nil {
		t.Fatalf("create entry symlink: %v", err)
	}
	restoreDir := filepath.Join(dir, "restore-with-entry-link")
	err = RestoreDataDir(backupDir, restoreDir)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil for symlink backup entry")
	}
	assertErrorContains(t, err, "entry-link")
	assertErrorContains(t, err, "is a symlink; refusing to copy")
	if _, statErr := os.Stat(restoreDir); statErr != nil {
		t.Fatalf("restore destination should exist for failed partial copy investigation: %v", statErr)
	}
}

func dataDirBackupTestModule() *Module {
	return validChatModule().Reducer("insert_message", func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
		_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString(string(args))})
		return nil, err
	})
}

func assertDataDirRestoredMessageBodies(t *testing.T, rt *Runtime, want []string) {
	t.Helper()
	var got []string
	if err := rt.Read(context.Background(), func(view LocalReadView) error {
		for _, row := range view.TableScan(schema.TableID(0)) {
			got = append(got, row[1].AsString())
		}
		return nil
	}); err != nil {
		t.Fatalf("Read restored runtime: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("restored message bodies = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("restored message bodies = %#v, want %#v", got, want)
		}
	}
}

func assertDataDirRuntimeStateMessageBodies(t *testing.T, rt *Runtime, want []string) {
	t.Helper()
	snapshot := rt.state.Snapshot()
	defer snapshot.Close()
	var got []string
	for _, row := range snapshot.TableScan(schema.TableID(0)) {
		got = append(got, row[1].AsString())
	}
	if len(got) != len(want) {
		t.Fatalf("runtime state message bodies = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("runtime state message bodies = %#v, want %#v", got, want)
		}
	}
}

func writeDataDirTestBytes(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o666); err != nil {
		t.Fatalf("write test fixture: %v", err)
	}
	return path
}

func assertDataDirFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes = %q, want %q", path, got, want)
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err, want)
	}
}
