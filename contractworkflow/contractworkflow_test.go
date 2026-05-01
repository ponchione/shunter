package contractworkflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
		"breaking permission query.history: permission requirements added",
		"breaking reducer send_message: reducer removed",
		"metadata module chat: module version changed from \"v1.0.0\" to \"v1.1.0\"",
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

func TestPlanFilesReturnsDeterministicMigrationPlan(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeContractFixture(t, dir, "previous.json", workflowContractFixture())
	current := workflowContractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	currentPath := writeContractFixture(t, dir, "current.json", current)

	plan, err := PlanFiles(previousPath, currentPath, contractdiff.PlanOptions{
		Policy: contractdiff.PolicyOptions{RequirePreviousVersion: true},
	})
	if err != nil {
		t.Fatalf("PlanFiles returned error: %v", err)
	}

	got, err := FormatPlan(plan, FormatText)
	if err != nil {
		t.Fatalf("FormatPlan returned error: %v", err)
	}
	want := strings.Join([]string{
		"review review-required additive column messages.sent_at: column added with type timestamp",
		"warning missing-migration-metadata column messages.sent_at: additive change has no migration metadata",
		"warning missing-previous-version module chat: module migration metadata is missing previous_version",
		"",
	}, "\n")
	if string(got) != want {
		t.Fatalf("plan text =\n%s\nwant:\n%s", got, want)
	}

	jsonOut, err := FormatPlan(plan, FormatJSON)
	if err != nil {
		t.Fatalf("FormatPlan JSON returned error: %v", err)
	}
	assertContains(t, string(jsonOut), `"summary": {`)
	assertContains(t, string(jsonOut), `"entries": [`)
	assertContains(t, string(jsonOut), `"warnings": [`)
	assertContains(t, string(jsonOut), `"action": "review-required"`)
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

func TestGenerateFileRejectsEmptyOutputPathBeforeReadingContract(t *testing.T) {
	const trace = "trace=workflow-codegen-empty-output-path-before-input-read"
	dir := t.TempDir()
	missingContractPath := filepath.Join(dir, "missing-contract.json")

	err := GenerateFile(missingContractPath, " \t", codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for empty output path", trace)
	}
	if !strings.Contains(err.Error(), "generated output path is required") {
		t.Fatalf("%s GenerateFile error = %v, want output path error", trace, err)
	}
	if strings.Contains(err.Error(), "read contract input") {
		t.Fatalf("%s GenerateFile read contract before rejecting empty output path: %v", trace, err)
	}
	assertNoWorkflowTempFiles(t, dir, "client.ts")
}

func TestGenerateFileRejectsUnsupportedLanguageBeforeReadingContract(t *testing.T) {
	const trace = "trace=workflow-codegen-unsupported-language-before-input-read"
	dir := t.TempDir()
	missingContractPath := filepath.Join(dir, "missing-contract.json")
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	err := GenerateFile(missingContractPath, outputPath, codegen.Options{Language: "go"})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for unsupported language", trace)
	}
	if !errors.Is(err, codegen.ErrUnsupportedLanguage) {
		t.Fatalf("%s GenerateFile error = %v, want ErrUnsupportedLanguage", trace, err)
	}
	if strings.Contains(err.Error(), "read contract input") {
		t.Fatalf("%s GenerateFile read contract before rejecting language: %v", trace, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s unsupported language mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileSemanticInvalidContractLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=workflow-codegen-semantic-invalid-contract-output-preservation"
	dir := t.TempDir()
	invalidContract := workflowContractFixture()
	invalidContract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceQuery,
		Name:    "history",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibility("maybe"),
		},
	}}
	contractPath := writeContractFixture(t, dir, "contract.json", invalidContract)
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for semantic-invalid contract", trace)
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Fatalf("%s GenerateFile error = %v, want ErrInvalidContract", trace, err)
	}
	if !strings.Contains(err.Error(), "migrations.query.history.compatibility") {
		t.Fatalf("%s GenerateFile error = %v, want migration metadata context", trace, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s semantic-invalid contract mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileUnknownPermissionTargetLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=workflow-codegen-unknown-permission-target-output-preservation"
	dir := t.TempDir()
	invalidContract := workflowContractFixture()
	invalidContract.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "missing_reducer",
		Required: []string{"messages:send"},
	}}
	contractPath := writeContractFixture(t, dir, "contract.json", invalidContract)
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for unknown permission target", trace)
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Fatalf("%s GenerateFile error = %v, want ErrInvalidContract", trace, err)
	}
	if !strings.Contains(err.Error(), "permissions.reducer.missing_reducer references unknown reducer") {
		t.Fatalf("%s GenerateFile error = %v, want permission reducer target context", trace, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s unknown permission target mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileInvalidTableReadPolicyLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=workflow-codegen-invalid-read-policy-output-preservation"
	dir := t.TempDir()
	invalidContract := workflowContractFixture()
	invalidContract.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPublic,
		Permissions: []string{"messages:read"},
	}
	contractPath := writeContractFixture(t, dir, "contract.json", invalidContract)
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for invalid table read policy", trace)
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Fatalf("%s GenerateFile error = %v, want ErrInvalidContract", trace, err)
	}
	if !strings.Contains(err.Error(), "schema.tables.messages.read_policy invalid") {
		t.Fatalf("%s GenerateFile error = %v, want table read policy context", trace, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s invalid read policy mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileUnknownReadModelTargetLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=workflow-codegen-unknown-read-model-target-output-preservation"
	dir := t.TempDir()
	invalidContract := workflowContractFixture()
	invalidContract.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceQuery,
		Name:    "missing_query",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}
	contractPath := writeContractFixture(t, dir, "contract.json", invalidContract)
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for unknown read model target", trace)
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Fatalf("%s GenerateFile error = %v, want ErrInvalidContract", trace, err)
	}
	if !strings.Contains(err.Error(), "read_model.query.missing_query references unknown query") {
		t.Fatalf("%s GenerateFile error = %v, want read model query target context", trace, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s unknown read model target mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFilePreservesExistingOutputPermissionsAcrossAtomicRewrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bit preservation is POSIX-specific")
	}

	const trace = "trace=workflow-codegen-preserve-existing-output-permissions"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")
	originalMode := os.FileMode(0o640)
	if err := os.WriteFile(outputPath, []byte("previous generated output\n"), 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}
	if err := os.Chmod(outputPath, originalMode); err != nil {
		t.Fatalf("%s chmod existing output: %v", trace, err)
	}

	if err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("%s GenerateFile returned error: %v", trace, err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("%s stat output: %v", trace, err)
	}
	if got := info.Mode().Perm(); got != originalMode {
		t.Fatalf("%s output mode = %v, want %v", trace, got, originalMode)
	}
	got := readTextFile(t, outputPath)
	assertContains(t, got, `export interface MessagesRow {`)
	if strings.Contains(got, "previous generated output") {
		t.Fatalf("%s old output survived atomic rewrite:\n%s", trace, got)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileDirectoryOutputFailsWithoutTempLeak(t *testing.T) {
	const trace = "trace=workflow-codegen-directory-output-fail-loud"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")
	if err := os.Mkdir(outputPath, 0o755); err != nil {
		t.Fatalf("%s create output directory: %v", trace, err)
	}

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error for directory output", trace)
	}
	if !strings.Contains(err.Error(), "write generated output") {
		t.Fatalf("%s GenerateFile error = %v, want write generated output context", trace, err)
	}
	info, statErr := os.Stat(outputPath)
	if statErr != nil {
		t.Fatalf("%s stat output directory: %v", trace, statErr)
	}
	if !info.IsDir() {
		t.Fatalf("%s output path is not still a directory after failed rewrite", trace)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileSymlinkOutputReplacesLinkWithoutMutatingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink replacement semantics are not portable on windows")
	}

	const trace = "trace=workflow-codegen-symlink-output-replaces-link-only"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	targetPath := filepath.Join(dir, "outside-target.ts")
	targetData := []byte("unrelated generated target\n")
	if err := os.WriteFile(targetPath, targetData, 0o600); err != nil {
		t.Fatalf("%s write symlink target: %v", trace, err)
	}
	outputPath := filepath.Join(dir, "client.ts")
	if err := os.Symlink(targetPath, outputPath); err != nil {
		t.Fatalf("%s create output symlink: %v", trace, err)
	}

	if err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("%s GenerateFile returned error: %v", trace, err)
	}
	linkInfo, err := os.Lstat(outputPath)
	if err != nil {
		t.Fatalf("%s lstat output: %v", trace, err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s output path is still a symlink after atomic rewrite", trace)
	}
	assertContains(t, readTextFile(t, outputPath), `export interface MessagesRow {`)
	gotTarget, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("%s read symlink target: %v", trace, err)
	}
	if !bytes.Equal(gotTarget, targetData) {
		t.Fatalf("%s symlink target mutated:\nobserved=%q\nexpected=%q", trace, gotTarget, targetData)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileSyncsParentDirectoryAfterAtomicRename(t *testing.T) {
	const trace = "trace=workflow-codegen-parent-sync-after-rename"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")

	originalSyncDir := syncDir
	var synced []string
	syncDir = func(path string) error {
		synced = append(synced, path)
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("%s syncDir called before output publish: %v", trace, err)
		}
		if !bytes.Contains(data, []byte(`export interface MessagesRow {`)) {
			t.Fatalf("%s output at syncDir = %q, want generated TypeScript", trace, data)
		}
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

	if err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("%s GenerateFile returned error: %v", trace, err)
	}
	if len(synced) != 1 || synced[0] != dir {
		t.Fatalf("%s syncDir calls = %v, want [%s]", trace, synced, dir)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileParentSyncFailureFailsLoudlyWithoutTempLeak(t *testing.T) {
	const trace = "trace=workflow-codegen-parent-sync-failure-fail-loud"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("previous generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	syncErr := errors.New("parent directory sync failed")
	originalSyncDir := syncDir
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("%s syncDir path = %q, want %q", trace, path, dir)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("%s read output during syncDir: %v", trace, err)
		}
		if bytes.Equal(data, original) || !bytes.Contains(data, []byte(`export interface MessagesRow {`)) {
			t.Fatalf("%s output at syncDir = %q, want generated TypeScript published before parent sync", trace, data)
		}
		return syncErr
	}
	defer func() { syncDir = originalSyncDir }()

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error, want parent sync failure", trace)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("%s GenerateFile error = %v, want wrapped parent sync failure", trace, err)
	}
	if !strings.Contains(err.Error(), "write generated output") {
		t.Fatalf("%s GenerateFile error = %v, want write generated output context", trace, err)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileWriteFailureLeavesExistingOutputUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory chmod write denial is not portable on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write through read-only test directories")
	}

	const trace = "trace=workflow-codegen-output-write-failure-preserves-existing-artifact"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("previous generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("%s chmod temp dir read-only: %v", trace, err)
	}
	defer func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatalf("%s restore temp dir mode: %v", trace, err)
		}
	}()

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatalf("%s GenerateFile returned nil error, want write failure", trace)
	}
	if !strings.Contains(err.Error(), "write generated output") {
		t.Fatalf("%s GenerateFile error = %v, want write generated output context", trace, err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s failed write mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
}

func TestWorkflowErrorsRemainClear(t *testing.T) {
	dir := t.TempDir()
	contractPath := filepath.Join(dir, "contract.json")
	malformedPath := filepath.Join(dir, "malformed.json")
	invalidCodegenPath := filepath.Join(dir, "invalid-codegen.json")
	if err := writeFile(contractPath, mustContractJSON(t, workflowContractFixture())); err != nil {
		t.Fatalf("write contract fixture: %v", err)
	}
	if err := writeFile(malformedPath, []byte(`{`)); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}
	invalidCodegen := workflowContractFixture()
	invalidCodegen.Queries[0].SQL = "SELECT * FROM missing_table"
	if err := writeFile(invalidCodegenPath, mustContractJSON(t, invalidCodegen)); err != nil {
		t.Fatalf("write invalid codegen fixture: %v", err)
	}

	_, err := CompareFiles(malformedPath, contractPath)
	if err == nil {
		t.Fatal("CompareFiles returned nil error for malformed input")
	}
	if !errors.Is(err, contractdiff.ErrInvalidContractJSON) {
		t.Fatalf("CompareFiles error = %v, want ErrInvalidContractJSON", err)
	}

	_, err = PlanFiles(malformedPath, contractPath, contractdiff.PlanOptions{})
	if err == nil {
		t.Fatal("PlanFiles returned nil error for malformed input")
	}
	if !errors.Is(err, contractdiff.ErrInvalidContractJSON) {
		t.Fatalf("PlanFiles error = %v, want ErrInvalidContractJSON", err)
	}

	_, err = GenerateFromFile(contractPath, codegen.Options{Language: "go"})
	if err == nil {
		t.Fatal("GenerateFromFile returned nil error for unsupported language")
	}
	if !errors.Is(err, codegen.ErrUnsupportedLanguage) {
		t.Fatalf("GenerateFromFile error = %v, want ErrUnsupportedLanguage", err)
	}

	_, err = GenerateFromFile(malformedPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromFile returned nil error for malformed contract")
	}
	if !errors.Is(err, codegen.ErrInvalidContract) || !strings.Contains(err.Error(), "generate bindings from") {
		t.Fatalf("GenerateFromFile malformed error = %v, want wrapped ErrInvalidContract with workflow context", err)
	}

	_, err = GenerateFromFile(invalidCodegenPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromFile returned nil error for invalid contract")
	}
	if !errors.Is(err, codegen.ErrInvalidContract) || !strings.Contains(err.Error(), "queries.history.sql") {
		t.Fatalf("GenerateFromFile invalid contract error = %v, want wrapped ErrInvalidContract with query SQL context", err)
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

func assertNoWorkflowTempFiles(t *testing.T, dir, outputBase string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output directory %s: %v", dir, err)
	}
	prefix := "." + outputBase + ".tmp-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			t.Fatalf("temporary artifact %s leaked in %s", entry.Name(), dir)
		}
	}
}
