package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
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

func TestPreflightJSONReportsAdditiveOlderHostedChatDataDir(t *testing.T) {
	dataDir := buildHostedChatTestDataDir(t, olderHostedChatCompatibleModule())
	before := snapshotDataDir(t, dataDir)

	report := runJSONPreflight(t, dataDir, 0)

	if !report.Compatible || report.Status != shunter.DataDirCompatibilityAdditive {
		t.Fatalf("report = %#v, want additive compatible", report)
	}
	if !report.RequiresBackup || report.RequiresOfflineHook {
		t.Fatalf("backup/offline flags = %t/%t, want backup without required hook", report.RequiresBackup, report.RequiresOfflineHook)
	}
	if report.BlockingError != "" {
		t.Fatalf("blocking error = %q, want empty", report.BlockingError)
	}
	if !report.Schema.Compatible || report.Schema.Status != schema.SchemaCompatibilityAdditive {
		t.Fatalf("schema report = %#v, want additive compatible", report.Schema)
	}
	if len(report.Schema.Changes) != 1 || len(report.Schema.Issues) != 0 {
		t.Fatalf("schema report = %#v, want event-table change only", report.Schema)
	}
	assertDataDirUnchanged(t, dataDir, before)
}

func TestPreflightJSONReportsBlockedRowShapeChange(t *testing.T) {
	dataDir := buildHostedChatTestDataDir(t, rowShapeDriftHostedChatModule())
	before := snapshotDataDir(t, dataDir)

	report := runJSONPreflight(t, dataDir, 1)

	if report.Compatible || report.Status != shunter.DataDirCompatibilityBlocked {
		t.Fatalf("report = %#v, want blocked incompatible", report)
	}
	if !report.RequiresBackup || !report.RequiresOfflineHook {
		t.Fatalf("backup/offline flags = %t/%t, want both required", report.RequiresBackup, report.RequiresOfflineHook)
	}
	if !strings.Contains(report.BlockingError, "row-shape changes require an app-owned migration") {
		t.Fatalf("blocking error = %q, want row-shape migration detail", report.BlockingError)
	}
	if report.Schema.Compatible || report.Schema.Status != schema.SchemaCompatibilityBlocked || len(report.Schema.Issues) == 0 {
		t.Fatalf("schema report = %#v, want blocked issues", report.Schema)
	}
	assertDataDirUnchanged(t, dataDir, before)
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

func TestMigrateWithoutHooksDoesNotMutateExistingDataDir(t *testing.T) {
	dataDir := buildHostedChatTestDataDir(t, app.Module())
	before := snapshotDataDir(t, dataDir)
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
	if result.DataDir != dataDir || result.RecoveredTxID != 0 || result.DurableTxID != 0 || len(result.Hooks) != 0 {
		t.Fatalf("migration result = %#v, want no-hook result for existing data dir", result)
	}
	assertDataDirUnchanged(t, dataDir, before)
}

func buildHostedChatTestDataDir(t *testing.T, mod *shunter.Module) string {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	rt, err := shunter.Build(mod, shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build hosted-chat test runtime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close hosted-chat test runtime: %v", err)
	}
	return dataDir
}

func runJSONPreflight(t *testing.T, dataDir string, wantCode int) shunter.DataDirCompatibilityReport {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{
		"preflight",
		"--data-dir", dataDir,
		"--format", "json",
	})
	if code != wantCode {
		t.Fatalf("preflight exit code = %d, want %d, stderr = %s, stdout = %s", code, wantCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("preflight stderr = %s, want empty", stderr.String())
	}
	var report shunter.DataDirCompatibilityReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode preflight report: %v\n%s", err, stdout.String())
	}
	if report.DataDir != dataDir {
		t.Fatalf("report data dir = %q, want %q", report.DataDir, dataDir)
	}
	return report
}

func olderHostedChatCompatibleModule() *shunter.Module {
	return shunter.NewModule("hosted_chat").
		Version("v0.0.9").
		SchemaVersion(1).
		TableDef(hostedChatMessagesTableDef())
}

func rowShapeDriftHostedChatModule() *shunter.Module {
	messages := hostedChatMessagesTableDef()
	messages.Columns = []schema.ColumnDefinition{
		{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
		{Name: "body", Type: types.KindString},
	}
	return shunter.NewModule("hosted_chat").
		Version("v0.0.9").
		SchemaVersion(1).
		TableDef(messages)
}

func hostedChatMessagesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "author", Type: types.KindString},
			{Name: "body", Type: types.KindString},
		},
	}
}

type dataDirEntry struct {
	Mode    fs.FileMode
	Size    int64
	ModTime int64
	Hash    [32]byte
}

func snapshotDataDir(t *testing.T, root string) map[string]dataDirEntry {
	t.Helper()
	entries := make(map[string]dataDirEntry)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := dataDirEntry{
			Mode:    info.Mode(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry.Hash = sha256.Sum256(data)
		}
		entries[rel] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot DataDir: %v", err)
	}
	return entries
}

func assertDataDirUnchanged(t *testing.T, dataDir string, before map[string]dataDirEntry) {
	t.Helper()
	after := snapshotDataDir(t, dataDir)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("DataDir changed after preflight\nbefore: %#v\nafter: %#v", before, after)
	}
}
