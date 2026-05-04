package shunter

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
