package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestHelpDocumentsAppOwnedContractExport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--help"})
	if code != 0 {
		t.Fatalf("run --help exit code = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Runtime.ExportContractJSON")
	assertContains(t, out, "app-owned binary")
	assertContains(t, out, "No dynamic module loading")
}

func TestContractDiffCommandReadsJSONFiles(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	currentPath := writeCLIContract(t, dir, "current.json", current)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "diff",
		"--previous", previousPath,
		"--current", currentPath,
	})
	if code != 0 {
		t.Fatalf("contract diff exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract diff stderr = %s, want empty", stderr.String())
	}
	assertContains(t, stdout.String(), "additive column messages.sent_at: column added with type timestamp")
}

func TestContractPolicyCommandFailsInStrictMode(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	currentPath := writeCLIContract(t, dir, "current.json", current)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "policy",
		"--previous", previousPath,
		"--current", currentPath,
		"--strict",
	})
	if code != 1 {
		t.Fatalf("contract policy exit code = %d, stderr = %s", code, stderr.String())
	}
	assertContains(t, stdout.String(), "missing-migration-metadata query recent_messages")
}

func TestContractPlanCommandReadsJSONFiles(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	currentPath := writeCLIContract(t, dir, "current.json", current)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "plan",
		"--previous", previousPath,
		"--current", currentPath,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("contract plan exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract plan stderr = %s, want empty", stderr.String())
	}
	assertContains(t, stdout.String(), `"entries": [`)
	assertContains(t, stdout.String(), `"action": "review-required"`)
	assertContains(t, stdout.String(), `"warnings": [`)
}

func TestContractCodegenCommandWritesTypeScript(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())
	outputPath := filepath.Join(dir, "client.ts")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--out", outputPath,
	})
	if code != 0 {
		t.Fatalf("contract codegen exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read generated output: %v", err)
	}
	assertContains(t, string(data), "export interface MessagesRow {")
	assertContains(t, stdout.String(), "wrote "+outputPath)
}

func cliContractFixture() shunter.ModuleContract {
	return shunter.ModuleContract{
		ContractVersion: shunter.ModuleContractVersion,
		Module: shunter.ModuleContractIdentity{
			Name:     "chat",
			Version:  "v1.0.0",
			Metadata: map[string]string{},
		},
		Schema: schema.SchemaExport{
			Version: 1,
			Tables: []schema.TableExport{
				{
					Name: "messages",
					Columns: []schema.ColumnExport{
						{Name: "id", Type: "uint64"},
						{Name: "body", Type: "string"},
					},
					Indexes: []schema.IndexExport{{Name: "messages_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
				},
			},
			Reducers: []schema.ReducerExport{{Name: "send_message"}},
		},
		Queries: []shunter.QueryDescription{{Name: "history"}},
		Views:   []shunter.ViewDescription{},
		Permissions: shunter.PermissionContract{
			Reducers: []shunter.PermissionContractDeclaration{},
			Queries:  []shunter.PermissionContractDeclaration{},
			Views:    []shunter.PermissionContractDeclaration{},
		},
		ReadModel: shunter.ReadModelContract{Declarations: []shunter.ReadModelContractDeclaration{}},
		Migrations: shunter.MigrationContract{
			Module:       shunter.MigrationMetadata{Classifications: []shunter.MigrationClassification{}},
			Declarations: []shunter.MigrationContractDeclaration{},
		},
		Codegen: shunter.CodegenContractMetadata{
			ContractFormat:          shunter.ModuleContractFormat,
			ContractVersion:         shunter.ModuleContractVersion,
			DefaultSnapshotFilename: shunter.DefaultContractSnapshotFilename,
		},
	}
}

func writeCLIContract(t *testing.T, dir, name string, contract shunter.ModuleContract) string {
	t.Helper()
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o666); err != nil {
		t.Fatalf("write contract fixture: %v", err)
	}
	return path
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}
