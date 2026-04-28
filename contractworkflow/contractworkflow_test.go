package contractworkflow

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/codegen"
	"github.com/ponchione/shunter/contractdiff"
	"github.com/ponchione/shunter/schema"
)

func TestCompareFilesReturnsDeterministicContractChanges(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeContractFixture(t, dir, "previous.json", workflowContractFixture())
	current := workflowContractFixture()
	current.Module.Version = "v1.1.0"
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Schema.Reducers = nil
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{Name: "history", Required: []string{"messages:read"}}}
	currentPath := writeContractFixture(t, dir, "current.json", current)

	report, err := CompareFiles(previousPath, currentPath)
	if err != nil {
		t.Fatalf("CompareFiles returned error: %v", err)
	}

	got, err := FormatDiff(report, FormatText)
	if err != nil {
		t.Fatalf("FormatDiff returned error: %v", err)
	}
	want := strings.Join([]string{
		"additive column messages.sent_at: column added with type timestamp",
		"additive query recent_messages: query added",
		"breaking reducer send_message: reducer removed",
		"metadata module chat: module version changed from \"v1.0.0\" to \"v1.1.0\"",
		"metadata permission query.history: permission metadata added",
		"",
	}, "\n")
	if string(got) != want {
		t.Fatalf("diff text =\n%s\nwant:\n%s", got, want)
	}

	jsonOut, err := FormatDiff(report, FormatJSON)
	if err != nil {
		t.Fatalf("FormatDiff JSON returned error: %v", err)
	}
	assertContains(t, string(jsonOut), `"changes": [`)
	assertContains(t, string(jsonOut), `"kind": "additive"`)
	assertContains(t, string(jsonOut), `"surface": "column"`)
}

func TestCheckPolicyFilesReturnsDeterministicWarningsAndStrictFailure(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeContractFixture(t, dir, "previous.json", workflowContractFixture())
	current := workflowContractFixture()
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	currentPath := writeContractFixture(t, dir, "current.json", current)

	result, err := CheckPolicyFiles(previousPath, currentPath, contractdiff.PolicyOptions{
		RequirePreviousVersion: true,
		Strict:                 true,
	})
	if err != nil {
		t.Fatalf("CheckPolicyFiles returned error: %v", err)
	}
	if !result.Failed {
		t.Fatal("strict policy result did not fail on warnings")
	}

	got, err := FormatPolicy(result, FormatText)
	if err != nil {
		t.Fatalf("FormatPolicy returned error: %v", err)
	}
	want := strings.Join([]string{
		"missing-migration-metadata query recent_messages: additive change has no migration metadata",
		"missing-previous-version module chat: module migration metadata is missing previous_version",
		"",
	}, "\n")
	if string(got) != want {
		t.Fatalf("policy text =\n%s\nwant:\n%s", got, want)
	}

	jsonOut, err := FormatPolicy(result, FormatJSON)
	if err != nil {
		t.Fatalf("FormatPolicy JSON returned error: %v", err)
	}
	assertContains(t, string(jsonOut), `"failed": true`)
	assertContains(t, string(jsonOut), `"code": "missing-migration-metadata"`)
}

func TestGenerateFileWritesDeterministicTypeScriptFromContractJSON(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")

	if err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("GenerateFile returned error: %v", err)
	}
	first := readTextFile(t, outputPath)

	if err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("second GenerateFile returned error: %v", err)
	}
	second := readTextFile(t, outputPath)
	if first != second {
		t.Fatalf("generated TypeScript was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	assertContains(t, first, `export interface MessagesRow {`)
	assertContains(t, first, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
}

func TestWorkflowErrorsRemainClear(t *testing.T) {
	dir := t.TempDir()
	contractPath := filepath.Join(dir, "contract.json")
	malformedPath := filepath.Join(dir, "malformed.json")
	if err := writeFile(contractPath, mustContractJSON(t, workflowContractFixture())); err != nil {
		t.Fatalf("write contract fixture: %v", err)
	}
	if err := writeFile(malformedPath, []byte(`{`)); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}

	_, err := CompareFiles(malformedPath, contractPath)
	if err == nil {
		t.Fatal("CompareFiles returned nil error for malformed input")
	}
	if !errors.Is(err, contractdiff.ErrInvalidContractJSON) {
		t.Fatalf("CompareFiles error = %v, want ErrInvalidContractJSON", err)
	}

	_, err = GenerateFromFile(contractPath, codegen.Options{Language: "go"})
	if err == nil {
		t.Fatal("GenerateFromFile returned nil error for unsupported language")
	}
	if !errors.Is(err, codegen.ErrUnsupportedLanguage) {
		t.Fatalf("GenerateFromFile error = %v, want ErrUnsupportedLanguage", err)
	}

	_, err = GenerateFromFile(filepath.Join(dir, "missing.json"), codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil || !strings.Contains(err.Error(), "read contract input") {
		t.Fatalf("missing input error = %v, want clear read contract input error", err)
	}

	err = GenerateFile(contractPath, filepath.Join(dir, "missing-parent", "client.ts"), codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil || !strings.Contains(err.Error(), "write generated output") {
		t.Fatalf("unwritable output error = %v, want clear write generated output error", err)
	}
}

func workflowContractFixture() shunter.ModuleContract {
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

func writeContractFixture(t *testing.T, dir, name string, contract shunter.ModuleContract) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := writeFile(path, mustContractJSON(t, contract)); err != nil {
		t.Fatalf("write contract fixture %s: %v", name, err)
	}
	return path
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := readFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func mustContractJSON(t *testing.T, contract shunter.ModuleContract) []byte {
	t.Helper()
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	return data
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}
