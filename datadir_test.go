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

func TestBackupDataDirRejectsSymlinkOutputWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	writeDataDirTestBytes(t, dataDir, "00000000000000000001.log", []byte("segment-1"))
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	original := []byte("existing target data")
	writeDataDirTestBytes(t, targetDir, "existing", original)
	outputLink := filepath.Join(dir, "backup-link")
	if err := os.Symlink(targetDir, outputLink); err != nil {
		t.Skipf("create backup output symlink: %v", err)
	}

	err := BackupDataDir(dataDir, outputLink)
	if err == nil {
		t.Fatal("BackupDataDir returned nil, want symlink output error")
	}
	assertErrorContains(t, err, "backup output "+outputLink+" already exists")
	assertDataDirFileBytes(t, filepath.Join(targetDir, "existing"), original)
	if _, statErr := os.Lstat(outputLink); statErr != nil {
		t.Fatalf("backup output symlink stat after rejected backup: %v", statErr)
	}
}

func TestBackupDataDirRejectsSourceChangedDuringCopy(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	sourcePath := writeDataDirTestBytes(t, dataDir, "00000000000000000001.log", []byte("segment-1"))
	backupDir := filepath.Join(dir, "backup")

	previous := copyRegularFileAfterCopyHook
	copyRegularFileAfterCopyHook = func(path string) {
		if path != sourcePath {
			return
		}
		if err := os.WriteFile(path, []byte("mutated-segment"), 0o666); err != nil {
			t.Fatalf("mutate source during backup: %v", err)
		}
	}
	defer func() { copyRegularFileAfterCopyHook = previous }()

	err := BackupDataDir(dataDir, backupDir)
	if err == nil {
		t.Fatal("BackupDataDir returned nil, want source mutation error")
	}
	assertErrorContains(t, err, "changed while copying")
	if _, statErr := os.Lstat(backupDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup root stat after rejected source mutation = %v, want not exist", statErr)
	}
	assertDataDirFileBytes(t, sourcePath, []byte("mutated-segment"))
}

func TestBackupDataDirFailurePhasesNeverPublishConsumableArtifact(t *testing.T) {
	failure := errors.New("injected offline-copy failure")
	for _, tc := range []struct {
		name    string
		install func(t *testing.T)
	}{
		{
			name: "create staging",
			install: func(t *testing.T) {
				previous := makeOfflineCopyStagingDir
				makeOfflineCopyStagingDir = func(string, string) (string, error) { return "", failure }
				t.Cleanup(func() { makeOfflineCopyStagingDir = previous })
			},
		},
		{
			name: "sync staged tree",
			install: func(t *testing.T) {
				installOfflineCopySyncFailure(t, 1, failure)
			},
		},
		{
			name: "prepare staging permissions",
			install: func(t *testing.T) {
				previous := chmodOfflineCopyPath
				chmodOfflineCopyPath = func(string, os.FileMode) error { return failure }
				t.Cleanup(func() { chmodOfflineCopyPath = previous })
			},
		},
		{
			name: "publish rename",
			install: func(t *testing.T) {
				previous := renameOfflineCopy
				renameOfflineCopy = func(string, string) error { return failure }
				t.Cleanup(func() { renameOfflineCopy = previous })
			},
		},
		{
			name: "sync publication",
			install: func(t *testing.T) {
				installOfflineCopySyncFailure(t, 2, failure)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "data")
			if err := os.Mkdir(source, 0o755); err != nil {
				t.Fatalf("create source data dir: %v", err)
			}
			writeDataDirTestBytes(t, source, "segment", []byte("complete"))
			output := filepath.Join(dir, "backup")
			tc.install(t)

			err := BackupDataDir(source, output)
			if err == nil || !errors.Is(err, failure) {
				t.Fatalf("BackupDataDir error = %v, want injected failure", err)
			}
			assertPathMissing(t, output)
			assertNoOfflineCopyStagingEntries(t, dir, "backup")
			if restoreErr := RestoreDataDir(output, filepath.Join(dir, "restore")); restoreErr == nil {
				t.Fatal("RestoreDataDir accepted failed backup path")
			}
		})
	}
}

func TestRestoreDataDirFailurePhasesPreserveInitiallyEmptyDestination(t *testing.T) {
	failure := errors.New("injected restore failure")
	for _, tc := range []struct {
		name     string
		syncCall int
		rename   bool
		remove   bool
	}{
		{name: "sync staged tree", syncCall: 1},
		{name: "remove empty destination", remove: true},
		{name: "sync empty removal", syncCall: 2},
		{name: "publish rename", rename: true},
		{name: "sync publication", syncCall: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			backup := filepath.Join(dir, "backup")
			if err := os.Mkdir(backup, 0o755); err != nil {
				t.Fatalf("create backup data dir: %v", err)
			}
			writeDataDirTestBytes(t, backup, "segment", []byte("complete"))
			destination := filepath.Join(dir, "data")
			if err := os.Mkdir(destination, 0o750); err != nil {
				t.Fatalf("create empty restore destination: %v", err)
			}
			if tc.syncCall != 0 {
				installOfflineCopySyncFailure(t, tc.syncCall, failure)
			}
			if tc.rename {
				previous := renameOfflineCopy
				renameOfflineCopy = func(string, string) error { return failure }
				t.Cleanup(func() { renameOfflineCopy = previous })
			}
			if tc.remove {
				previous := removeOfflineCopyEmpty
				removeOfflineCopyEmpty = func(string) error { return failure }
				t.Cleanup(func() { removeOfflineCopyEmpty = previous })
			}

			err := RestoreDataDir(backup, destination)
			if err == nil || !errors.Is(err, failure) {
				t.Fatalf("RestoreDataDir error = %v, want injected failure", err)
			}
			info, statErr := os.Stat(destination)
			if statErr != nil {
				t.Fatalf("stat restored empty destination: %v", statErr)
			}
			if !info.IsDir() || info.Mode().Perm() != 0o750 {
				t.Fatalf("destination after failure mode = %v, want empty directory mode 0750", info.Mode())
			}
			entries, readErr := os.ReadDir(destination)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("destination entries after failure = %#v, err = %v, want empty", entries, readErr)
			}
			assertNoOfflineCopyStagingEntries(t, dir, "data")
		})
	}
}

func TestRestoreDataDirFailureLeavesInitiallyMissingDestinationMissing(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	if err := os.Mkdir(backup, 0o755); err != nil {
		t.Fatalf("create backup data dir: %v", err)
	}
	writeDataDirTestBytes(t, backup, "segment", []byte("complete"))
	destination := filepath.Join(dir, "data")
	failure := errors.New("injected publication failure")
	installOfflineCopySyncFailure(t, 2, failure)

	err := RestoreDataDir(backup, destination)
	if err == nil || !errors.Is(err, failure) {
		t.Fatalf("RestoreDataDir error = %v, want injected failure", err)
	}
	assertPathMissing(t, destination)
	assertNoOfflineCopyStagingEntries(t, dir, "data")
}

func TestBackupDataDirSyncsNestedDirectoriesBeforeDurablePublication(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatalf("create nested source data dir: %v", err)
	}
	writeDataDirTestBytes(t, filepath.Join(source, "nested"), "segment", []byte("complete"))
	output := filepath.Join(dir, "backup")
	previous := syncOfflineCopyDir
	var synced []string
	syncOfflineCopyDir = func(path string) error {
		synced = append(synced, path)
		return nil
	}
	t.Cleanup(func() { syncOfflineCopyDir = previous })

	if err := BackupDataDir(source, output); err != nil {
		t.Fatalf("BackupDataDir: %v", err)
	}
	if len(synced) < 3 {
		t.Fatalf("synced directories = %#v, want nested staging, staging root, and publication parent", synced)
	}
	if filepath.Base(synced[0]) != "nested" {
		t.Fatalf("first synced directory = %s, want nested staging directory", synced[0])
	}
	if synced[len(synced)-1] != dir {
		t.Fatalf("last synced directory = %s, want publication parent %s", synced[len(synced)-1], dir)
	}
}

func TestBackupDataDirFailureDurablySyncsStagingRemoval(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "data")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatalf("create source data dir: %v", err)
	}
	writeDataDirTestBytes(t, source, "segment", []byte("complete"))
	output := filepath.Join(dir, "backup")
	failure := errors.New("injected staging permission failure")
	previousChmod := chmodOfflineCopyPath
	chmodOfflineCopyPath = func(string, os.FileMode) error { return failure }
	t.Cleanup(func() { chmodOfflineCopyPath = previousChmod })
	previousSync := syncOfflineCopyDir
	var synced []string
	syncOfflineCopyDir = func(path string) error {
		synced = append(synced, path)
		return previousSync(path)
	}
	t.Cleanup(func() { syncOfflineCopyDir = previousSync })

	err := BackupDataDir(source, output)
	if !errors.Is(err, failure) {
		t.Fatalf("BackupDataDir error = %v, want injected failure", err)
	}
	assertPathMissing(t, output)
	assertNoOfflineCopyStagingEntries(t, dir, "backup")
	if len(synced) != 1 || synced[0] != dir {
		t.Fatalf("synced directories after cleanup = %#v, want publication parent %s", synced, dir)
	}
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

func TestRestoreDataDirRejectsFileDestinationWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	writeDataDirTestBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))
	restorePath := filepath.Join(dir, "data-file")
	original := []byte("existing file data")
	if err := os.WriteFile(restorePath, original, 0o666); err != nil {
		t.Fatalf("write restore destination file: %v", err)
	}

	err := RestoreDataDir(backupDir, restorePath)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil, want file destination error")
	}
	assertErrorContains(t, err, "restore destination "+restorePath+" is not a directory")
	assertDataDirFileBytes(t, restorePath, original)
}

func TestRestoreDataDirRejectsSymlinkDestinationWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	writeDataDirTestBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	restoreLink := filepath.Join(dir, "restore-link")
	if err := os.Symlink(targetDir, restoreLink); err != nil {
		t.Skipf("create restore destination symlink: %v", err)
	}

	err := RestoreDataDir(backupDir, restoreLink)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil, want symlink destination error")
	}
	assertErrorContains(t, err, "restore destination "+restoreLink+" is a symlink; refusing to restore")
	if entries, readErr := os.ReadDir(targetDir); readErr != nil {
		t.Fatalf("read target dir after rejected restore: %v", readErr)
	} else if len(entries) != 0 {
		t.Fatalf("target dir entries after rejected restore = %#v, want empty", entries)
	}
	if _, statErr := os.Lstat(restoreLink); statErr != nil {
		t.Fatalf("restore destination symlink stat after rejected restore: %v", statErr)
	}
}

func TestBackupDataDirErrorsNameSourceDataDir(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "backup")
	missingDataDir := filepath.Join(dir, "missing-data")

	err := BackupDataDir(missingDataDir, outputDir)
	if err == nil {
		t.Fatal("BackupDataDir returned nil for missing data dir")
	}
	assertErrorContains(t, err, "read source data dir "+missingDataDir)
	if _, statErr := os.Stat(outputDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup output stat after missing source = %v, want not exist", statErr)
	}

	dataFile := writeDataDirTestBytes(t, dir, "data-file", []byte("not a data dir"))
	err = BackupDataDir(dataFile, outputDir)
	if err == nil {
		t.Fatal("BackupDataDir returned nil for file data dir")
	}
	assertErrorContains(t, err, "source data dir "+dataFile+" is not a directory")
	if _, statErr := os.Stat(outputDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup output stat after file source = %v, want not exist", statErr)
	}
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

func TestDataDirHelpersRejectDestinationsInsideSourceThroughSymlinkedParent(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	dataLink := filepath.Join(dir, "data-link")
	if err := os.Symlink(dataDir, dataLink); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	backupViaLink := filepath.Join(dataLink, "backup")
	err := BackupDataDir(dataDir, backupViaLink)
	if err == nil {
		t.Fatal("BackupDataDir returned nil for destination routed back through source symlink")
	}
	assertErrorContains(t, err, "must not be inside source data dir")
	if _, statErr := os.Lstat(filepath.Join(dataDir, "backup")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("nested backup path stat = %v, want not exist", statErr)
	}

	restoreViaLink := filepath.Join(dataLink, "restore")
	err = RestoreDataDir(dataDir, restoreViaLink)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil for destination routed back through backup symlink")
	}
	assertErrorContains(t, err, "must not be inside source data dir")
	if _, statErr := os.Lstat(filepath.Join(dataDir, "restore")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("nested restore path stat = %v, want not exist", statErr)
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

func TestCopyRegularFileRejectsReplacedSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "copied")
	if err := os.WriteFile(src, []byte("original"), 0o666); err != nil {
		t.Fatalf("write source: %v", err)
	}
	sourceInfo, err := os.Lstat(src)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}

	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0o666); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	if err := os.Symlink(target, src); err != nil {
		t.Skipf("create source replacement symlink: %v", err)
	}

	err = copyRegularFile(src, dst, sourceInfo.Mode().Perm(), sourceInfo)
	if err == nil {
		t.Fatal("copyRegularFile returned nil for replaced source")
	}
	assertErrorContains(t, err, "changed while copying")
	if _, statErr := os.Lstat(dst); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination stat after rejected copy = %v, want not exist", statErr)
	}
}

func TestCopyRegularFileRejectsSourceChangedDuringCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "copied")
	if err := os.WriteFile(src, []byte("original"), 0o666); err != nil {
		t.Fatalf("write source: %v", err)
	}
	sourceInfo, err := os.Lstat(src)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}

	previous := copyRegularFileAfterCopyHook
	copyRegularFileAfterCopyHook = func(path string) {
		if err := os.WriteFile(path, []byte("mutated-after-copy"), 0o666); err != nil {
			t.Fatalf("mutate source: %v", err)
		}
	}
	defer func() { copyRegularFileAfterCopyHook = previous }()

	err = copyRegularFile(src, dst, sourceInfo.Mode().Perm(), sourceInfo)
	if err == nil {
		t.Fatal("copyRegularFile returned nil for source changed during copy")
	}
	assertErrorContains(t, err, "changed while copying")
	if _, statErr := os.Lstat(dst); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination stat after rejected copy = %v, want not exist", statErr)
	}
}

func TestCopyRegularFileRejectsSourceReplacedDuringCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "copied")
	if err := os.WriteFile(src, []byte("original"), 0o666); err != nil {
		t.Fatalf("write source: %v", err)
	}
	sourceInfo, err := os.Lstat(src)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}

	previous := copyRegularFileAfterCopyHook
	copyRegularFileAfterCopyHook = func(path string) {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove source: %v", err)
		}
		if err := os.WriteFile(path, []byte("replacement"), 0o666); err != nil {
			t.Fatalf("replace source: %v", err)
		}
	}
	defer func() { copyRegularFileAfterCopyHook = previous }()

	err = copyRegularFile(src, dst, sourceInfo.Mode().Perm(), sourceInfo)
	if err == nil {
		t.Fatal("copyRegularFile returned nil for source replaced during copy")
	}
	assertErrorContains(t, err, "changed while copying")
	if _, statErr := os.Lstat(dst); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination stat after rejected copy = %v, want not exist", statErr)
	}
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
	assertPathMissing(t, restoreDir)
}

func installOfflineCopySyncFailure(t *testing.T, failCall int, failure error) {
	t.Helper()
	previous := syncOfflineCopyDir
	call := 0
	syncOfflineCopyDir = func(path string) error {
		call++
		if call == failCall {
			return failure
		}
		return previous(path)
	}
	t.Cleanup(func() { syncOfflineCopyDir = previous })
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %s stat = %v, want not exist", path, err)
	}
}

func assertNoOfflineCopyStagingEntries(t *testing.T, parent, destinationBase string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(parent, "."+destinationBase+".staging-*"))
	if err != nil {
		t.Fatalf("glob staging paths: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("staging paths after failure = %#v, want none", matches)
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
