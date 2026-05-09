package shunter

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkBackupRestoreDataDirWorkflow(b *testing.B) {
	root := b.TempDir()
	source := filepath.Join(root, "source")
	buildBackupRestoreBenchmarkDataDir(b, source)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backup := filepath.Join(root, "backup", fmt.Sprintf("%06d", i))
		restore := filepath.Join(root, "restore", fmt.Sprintf("%06d", i))
		if err := BackupDataDir(source, backup); err != nil {
			b.Fatalf("BackupDataDir: %v", err)
		}
		if err := RestoreDataDir(backup, restore); err != nil {
			b.Fatalf("RestoreDataDir: %v", err)
		}
		if _, err := os.Stat(filepath.Join(restore, "snapshots", "2", "snapshot")); err != nil {
			b.Fatalf("stat restored snapshot: %v", err)
		}

		b.StopTimer()
		if err := os.RemoveAll(backup); err != nil {
			b.Fatalf("remove backup: %v", err)
		}
		if err := os.RemoveAll(restore); err != nil {
			b.Fatalf("remove restore: %v", err)
		}
		b.StartTimer()
	}
}

func buildBackupRestoreBenchmarkDataDir(b *testing.B, root string) {
	b.Helper()
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("%020d.log", i)
		writeBenchmarkDataDirFile(b, filepath.Join(root, name), 64*1024, byte(i))
	}
	for i := 1; i <= 2; i++ {
		writeBenchmarkDataDirFile(b, filepath.Join(root, "snapshots", fmt.Sprint(i), "snapshot"), 128*1024, byte(10+i))
	}
	writeBenchmarkDataDirFile(b, filepath.Join(root, "shunter.datadir.json"), 512, 42)
}

func writeBenchmarkDataDirFile(b *testing.B, path string, size int, seed byte) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("create benchmark data dir: %v", err)
	}
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = seed + byte(i%251)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		b.Fatalf("write benchmark data dir file: %v", err)
	}
}
