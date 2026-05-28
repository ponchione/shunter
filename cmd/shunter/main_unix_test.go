//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBackupRestoreCommandsRejectUnsupportedSpecialFileEntries(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	makeCLINamedPipe(t, filepath.Join(dataDir, "entry.pipe"))

	var stdout, stderr bytes.Buffer
	backupDir := filepath.Join(dir, "backup")
	code := run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", dataDir,
		"--out", backupDir,
	})
	if code != 1 {
		t.Fatalf("backup special-file entry exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("backup special-file entry stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "entry.pipe")
	assertContains(t, stderr.String(), "unsupported mode")
	if _, statErr := os.Lstat(filepath.Join(backupDir, "entry.pipe")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup special-file entry stat = %v, want not exist", statErr)
	}

	restoreBackup := filepath.Join(dir, "restore-backup")
	if err := os.MkdirAll(restoreBackup, 0o755); err != nil {
		t.Fatalf("create restore backup dir: %v", err)
	}
	makeCLINamedPipe(t, filepath.Join(restoreBackup, "entry.pipe"))

	stdout.Reset()
	stderr.Reset()
	restoreDir := filepath.Join(dir, "restore")
	code = run(&stdout, &stderr, []string{
		"restore",
		"--backup", restoreBackup,
		"--data-dir", restoreDir,
	})
	if code != 1 {
		t.Fatalf("restore special-file entry exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore special-file entry stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "entry.pipe")
	assertContains(t, stderr.String(), "unsupported mode")
	if _, statErr := os.Lstat(filepath.Join(restoreDir, "entry.pipe")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("restore special-file entry stat = %v, want not exist", statErr)
	}
}

func makeCLINamedPipe(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("create named pipe: %v", err)
	}
}
