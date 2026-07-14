package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

var errMaintenanceOutput = errors.New("injected maintenance output failure")

type failingMaintenanceWriter struct{}

func (failingMaintenanceWriter) Write([]byte) (int, error) {
	return 0, errMaintenanceOutput
}

func TestMaintenanceResultWritersPropagateOutputErrors(t *testing.T) {
	writers := []struct {
		name  string
		write func(string) error
	}{
		{name: "preflight", write: func(format string) error {
			return writePreflightReport(failingMaintenanceWriter{}, shunter.DataDirCompatibilityReport{}, format)
		}},
		{name: "migration", write: func(format string) error {
			return writeMigrationResult(failingMaintenanceWriter{}, shunter.MigrationRunResult{}, format)
		}},
		{name: "backup", write: func(format string) error {
			return writeBackupPreparationResult(failingMaintenanceWriter{}, backupPreparationResult{}, format)
		}},
	}
	for _, writer := range writers {
		for _, format := range []string{formatText, formatJSON} {
			t.Run(writer.name+"/"+format, func(t *testing.T) {
				if err := writer.write(format); !errors.Is(err, errMaintenanceOutput) {
					t.Fatalf("write error = %v, want %v", err, errMaintenanceOutput)
				}
			})
		}
	}
}

func TestPreflightOutputErrorReturnsFailure(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), failingMaintenanceWriter{}, &stderr, []string{
		"preflight", "--data-dir", filepath.Join(t.TempDir(), "missing"),
	})
	if code != 1 {
		t.Fatalf("preflight exit code = %d, stderr = %s, want 1", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), errMaintenanceOutput.Error()) {
		t.Fatalf("preflight stderr = %q, want %q", stderr.String(), errMaintenanceOutput)
	}
}

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

func TestPrepareBackupRejectsMissingDataDirWithoutCreatingIt(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{
		"prepare-backup",
		"--data-dir", dataDir,
		"--format", "json",
	})
	if code != 1 {
		t.Fatalf("prepare-backup exit code = %d, stderr = %s, want 1", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("prepare-backup stdout = %s, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "does not exist") {
		t.Fatalf("prepare-backup stderr = %q, want missing DataDir error", stderr.String())
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("prepare-backup stat = %v, want missing DataDir left uncreated", err)
	}
}

func TestPrepareBackupRejectsInvalidFormatBeforeMutation(t *testing.T) {
	dataDir := buildHostedChatTestDataDir(t, app.Module())
	before := snapshotDataDir(t, dataDir)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{
		"prepare-backup",
		"--data-dir", dataDir,
		"--format", "yaml",
	})
	if code != 2 {
		t.Fatalf("prepare-backup exit code = %d, stderr = %s, want 2", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("prepare-backup stdout = %s, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "format must be text or json") {
		t.Fatalf("prepare-backup stderr = %q, want format error", stderr.String())
	}
	assertDataDirUnchanged(t, dataDir, before)
}

func TestPrepareBackupDoesNotRunStartupMigrationHooks(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	hookCalled := false
	rt, err := shunter.Build(app.Module(), shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build hosted-chat runtime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close hosted-chat runtime: %v", err)
	}

	mod := app.Module().MigrationHook(func(context.Context, *shunter.MigrationContext) error {
		hookCalled = true
		return nil
	})
	result, err := prepareBackup(context.Background(), mod, shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("prepareBackup: %v", err)
	}
	if hookCalled {
		t.Fatal("prepareBackup ran a normal runtime startup migration hook")
	}
	if result.Status != "prepared" || result.DataDir != dataDir || result.SnapshotTxID != 0 {
		t.Fatalf("prepareBackup result = %#v, want prepared empty DataDir", result)
	}
}

func TestMaintenanceRecoveryDrill(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backup")
	restoredDir := filepath.Join(root, "restored")

	rt, err := shunter.Build(app.Module(), shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build source runtime: %v", err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Errorf("cleanup source runtime: %v", err)
		}
	})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start source runtime: %v", err)
	}
	args, err := app.EncodeSendMessageArgs("Ada", "maintenance drill")
	if err != nil {
		t.Fatalf("EncodeSendMessageArgs: %v", err)
	}
	result, err := rt.CallReducer(context.Background(), "send_message", args)
	if err != nil {
		t.Fatalf("CallReducer: %v", err)
	}
	if result.Status != shunter.StatusCommitted {
		t.Fatalf("CallReducer status = %v, error = %v, want committed", result.Status, result.Error)
	}
	if err := rt.WaitUntilDurable(context.Background(), result.TxID); err != nil {
		t.Fatalf("WaitUntilDurable: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close source runtime: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{
		"prepare-backup",
		"--data-dir", dataDir,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("prepare-backup exit code = %d, stderr = %s", code, stderr.String())
	}
	var preparation backupPreparationResult
	if err := json.Unmarshal(stdout.Bytes(), &preparation); err != nil {
		t.Fatalf("decode prepare-backup result: %v\n%s", err, stdout.String())
	}
	if preparation.Status != "prepared" || preparation.DataDir != dataDir ||
		preparation.RecoveredTxID != uint64(result.TxID) || preparation.SnapshotTxID != uint64(result.TxID) {
		t.Fatalf("prepare-backup result = %#v, want prepared tx %d", preparation, result.TxID)
	}
	snapshots, err := commitlog.ListSnapshots(dataDir)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	foundSnapshot := false
	for _, snapshotTxID := range snapshots {
		foundSnapshot = foundSnapshot || snapshotTxID == result.TxID
	}
	if !foundSnapshot {
		t.Fatalf("snapshots = %v, want completed tx %d", snapshots, result.TxID)
	}

	if err := shunter.BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir: %v", err)
	}
	if err := shunter.RestoreDataDir(backupDir, restoredDir); err != nil {
		t.Fatalf("RestoreDataDir: %v", err)
	}
	report := runJSONPreflight(t, restoredDir, 0)
	if !report.Compatible || report.Status != shunter.DataDirCompatibilityCompatible {
		t.Fatalf("restored preflight report = %#v, want exact compatible", report)
	}

	restored, err := shunter.Build(app.Module(), shunter.Config{DataDir: restoredDir})
	if err != nil {
		t.Fatalf("Build restored runtime: %v", err)
	}
	t.Cleanup(func() {
		if err := restored.Close(); err != nil {
			t.Errorf("cleanup restored runtime: %v", err)
		}
	})
	if err := restored.Start(context.Background()); err != nil {
		t.Fatalf("Start restored runtime: %v", err)
	}
	queryResult, err := restored.CallQuery(context.Background(), "recent_messages")
	if err != nil {
		t.Fatalf("CallQuery restored runtime: %v", err)
	}
	if len(queryResult.Rows) != 1 || queryResult.Rows[0][2].AsString() != "maintenance drill" {
		t.Fatalf("restored rows = %#v, want maintenance drill message", queryResult.Rows)
	}
	if err := restored.Close(); err != nil {
		t.Fatalf("Close restored runtime: %v", err)
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
