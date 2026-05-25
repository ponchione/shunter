package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
)

func TestPreflightReportsFreshMissingDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{
		"preflight",
		"--data-dir", dataDir,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("preflight exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("preflight stderr = %s, want empty", stderr.String())
	}
	var report shunter.DataDirCompatibilityReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode preflight report: %v\n%s", err, stdout.String())
	}
	if !report.Compatible || report.Status != shunter.DataDirCompatibilityFresh {
		t.Fatalf("report = %#v, want compatible fresh", report)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("preflight stat = %v, want missing DataDir left uncreated", err)
	}
}

func TestPreflightReportsCompatibleDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rt, err := shunter.Build(app.Module(), shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build hosted-chat runtime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close hosted-chat runtime: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{
		"preflight",
		"--data-dir", dataDir,
	})
	if code != 0 {
		t.Fatalf("preflight exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("preflight stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "status: compatible") {
		t.Fatalf("preflight output = %q, want compatible status", out)
	}
	if !strings.Contains(out, "compatible: true") {
		t.Fatalf("preflight output = %q, want compatible true", out)
	}
}

func TestPreflightReportsBlockedMetadataMismatch(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rt, err := shunter.Build(app.Module(), shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build hosted-chat runtime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close hosted-chat runtime: %v", err)
	}
	metadataPath := filepath.Join(dataDir, "shunter.datadir.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	data = bytes.Replace(data, []byte(`"name": "hosted_chat"`), []byte(`"name": "other_app"`), 1)
	if err := os.WriteFile(metadataPath, data, 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{
		"preflight",
		"--data-dir", dataDir,
		"--format", "json",
	})
	if code != 1 {
		t.Fatalf("preflight exit code = %d, stderr = %s, stdout = %s", code, stderr.String(), stdout.String())
	}
	var report shunter.DataDirCompatibilityReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode preflight report: %v\n%s", err, stdout.String())
	}
	if report.Compatible || report.Status != shunter.DataDirCompatibilityBlocked {
		t.Fatalf("report = %#v, want blocked", report)
	}
	if !strings.Contains(report.BlockingError, "module name") {
		t.Fatalf("blocking error = %q, want module name detail", report.BlockingError)
	}
}

func TestMigrateWithoutHooksDoesNotBootstrapMissingDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{
		"migrate",
		"--data-dir", dataDir,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("migrate exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("migrate stderr = %s, want empty", stderr.String())
	}
	var result shunter.MigrationRunResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode migration result: %v\n%s", err, stdout.String())
	}
	if result.DataDir != dataDir || len(result.Hooks) != 0 {
		t.Fatalf("migration result = %#v, want no-hook result for data dir", result)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("migrate stat = %v, want missing DataDir left uncreated", err)
	}
}
