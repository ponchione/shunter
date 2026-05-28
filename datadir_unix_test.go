//go:build unix

package shunter

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBackupDataDirRejectsUnsupportedSpecialFileEntry(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	pipePath := filepath.Join(dataDir, "entry.pipe")
	makeDataDirNamedPipe(t, pipePath)

	backupDir := filepath.Join(dir, "backup")
	err := BackupDataDir(dataDir, backupDir)
	if err == nil {
		t.Fatal("BackupDataDir returned nil for unsupported special file entry")
	}
	assertErrorContains(t, err, "entry.pipe")
	assertErrorContains(t, err, "unsupported mode")
	if _, statErr := os.Lstat(filepath.Join(backupDir, "entry.pipe")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup special-file entry stat = %v, want not exist", statErr)
	}
}

func TestRestoreDataDirRejectsUnsupportedSpecialFileEntry(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	pipePath := filepath.Join(backupDir, "entry.pipe")
	makeDataDirNamedPipe(t, pipePath)

	restoreDir := filepath.Join(dir, "restore")
	err := RestoreDataDir(backupDir, restoreDir)
	if err == nil {
		t.Fatal("RestoreDataDir returned nil for unsupported special file entry")
	}
	assertErrorContains(t, err, "entry.pipe")
	assertErrorContains(t, err, "unsupported mode")
	if _, statErr := os.Lstat(filepath.Join(restoreDir, "entry.pipe")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("restore special-file entry stat = %v, want not exist", statErr)
	}
}

func makeDataDirNamedPipe(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("create named pipe: %v", err)
	}
}
