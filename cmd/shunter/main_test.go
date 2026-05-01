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

func TestContractCommandsRoundTripReadPolicyMetadata(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}
	current.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:          "published_messages",
		SQL:           "SELECT * FROM messages WHERE body = 'published'",
		ReturnTable:   "messages",
		ReturnTableID: 0,
	}}
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
	assertContains(t, stdout.String(), "table_read_policy messages")
	assertContains(t, stdout.String(), "visibility_filter published_messages")

	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{
		"contract", "plan",
		"--previous", previousPath,
		"--current", currentPath,
	})
	if code != 0 {
		t.Fatalf("contract plan exit code = %d, stderr = %s", code, stderr.String())
	}
	assertContains(t, stdout.String(), "table_read_policy messages")
	assertContains(t, stdout.String(), "visibility_filter published_messages")
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

func TestContractPolicyCommandStrictJSONPinsRCGate(t *testing.T) {
	const trace = "trace=cli-contract-policy-strict-json-rc-gate"
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Module.Version = "v1.1.0"
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	currentPath := writeCLIContract(t, dir, "current.json", current)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "policy",
		"--previous", previousPath,
		"--current", currentPath,
		"--strict",
		"--require-previous-version",
		"--format", "json",
	})
	if code != 1 {
		t.Fatalf("%s contract policy exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("%s stderr = %s, want empty", trace, stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, `"failed": true`)
	assertContains(t, out, `"code": "missing-migration-metadata"`)
	assertContains(t, out, `"surface": "query"`)
	assertContains(t, out, `"name": "recent_messages"`)
	assertContains(t, out, `"code": "missing-previous-version"`)
	assertContains(t, out, `"surface": "module"`)
	assertContains(t, out, `"name": "chat"`)
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

func TestContractPlanValidateSurfacesDeclarationMetadataWarnings(t *testing.T) {
	const trace = "trace=cli-contract-plan-validate-declaration-metadata"
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Module.Version = "v1.1.0"
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceQuery,
			Name:    "history",
			Metadata: shunter.MigrationMetadata{
				ModuleVersion:   "v2.0.0",
				SchemaVersion:   99,
				ContractVersion: 99,
				PreviousVersion: "v0.9.0",
			},
		},
	}
	currentPath := writeCLIContract(t, dir, "current.json", current)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "plan",
		"--previous", previousPath,
		"--current", currentPath,
		"--strict",
		"--validate",
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("%s contract plan --validate exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("%s stderr = %s, want empty", trace, stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, `"code": "migration-metadata-module-version-mismatch"`)
	assertContains(t, out, `"surface": "query"`)
	assertContains(t, out, `"name": "history"`)
	assertContains(t, out, `query history migration metadata version`)
	assertContains(t, out, `"policy_failed": false`)

	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{
		"contract", "plan",
		"--previous", previousPath,
		"--current", currentPath,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("%s contract plan without --validate exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if strings.Contains(stdout.String(), "migration-metadata-module-version-mismatch") {
		t.Fatalf("%s validation warning appeared without --validate:\n%s", trace, stdout.String())
	}
}

func TestContractReadCommandsRejectUnsupportedFormats(t *testing.T) {
	const trace = "trace=cli-contract-read-unsupported-format"
	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	currentPath := writeCLIContract(t, dir, "current.json", current)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "diff",
			args: []string{"contract", "diff", "--previous", previousPath, "--current", currentPath, "--format", "yaml"},
		},
		{
			name: "policy",
			args: []string{"contract", "policy", "--previous", previousPath, "--current", currentPath, "--format", "yaml"},
		},
		{
			name: "plan",
			args: []string{"contract", "plan", "--previous", previousPath, "--current", currentPath, "--format", "yaml"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 2 {
				t.Fatalf("%s command=%s exit code = %d, stderr = %s", trace, tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s command=%s stdout = %s, want empty", trace, tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), `unsupported contract workflow output format "yaml"`)
		})
	}
}

func TestContractReadCommandsRejectUnsupportedFormatBeforeReadingFiles(t *testing.T) {
	const trace = "trace=cli-contract-read-unsupported-format-before-file-io"
	dir := t.TempDir()
	missingPrevious := filepath.Join(dir, "missing-previous.json")
	missingCurrent := filepath.Join(dir, "missing-current.json")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "diff",
			args: []string{"contract", "diff", "--previous", missingPrevious, "--current", missingCurrent, "--format", "yaml"},
		},
		{
			name: "policy",
			args: []string{"contract", "policy", "--previous", missingPrevious, "--current", missingCurrent, "--format", "yaml"},
		},
		{
			name: "plan",
			args: []string{"contract", "plan", "--previous", missingPrevious, "--current", missingCurrent, "--format", "yaml"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 2 {
				t.Fatalf("%s command=%s exit code = %d, stderr = %s", trace, tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s command=%s stdout = %s, want empty", trace, tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), `unsupported contract workflow output format "yaml"`)
			if strings.Contains(stderr.String(), "read previous contract") || strings.Contains(stderr.String(), "read current contract") {
				t.Fatalf("%s command=%s read contract before rejecting format: %s", trace, tc.name, stderr.String())
			}
		})
	}
}

func TestContractReadCommandsRejectUnexpectedArgBeforeFileIO(t *testing.T) {
	const trace = "trace=cli-contract-read-unexpected-arg-before-file-io"
	dir := t.TempDir()
	missingPrevious := filepath.Join(dir, "missing-previous.json")
	missingCurrent := filepath.Join(dir, "missing-current.json")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "diff",
			args: []string{"contract", "diff", "--previous", missingPrevious, "--current", missingCurrent, "unexpected"},
		},
		{
			name: "policy",
			args: []string{"contract", "policy", "--previous", missingPrevious, "--current", missingCurrent, "unexpected"},
		},
		{
			name: "plan",
			args: []string{"contract", "plan", "--previous", missingPrevious, "--current", missingCurrent, "unexpected"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 2 {
				t.Fatalf("%s command=%s exit code = %d, stderr = %s", trace, tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s command=%s stdout = %s, want empty", trace, tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), `unexpected argument "unexpected"`)
			if strings.Contains(stderr.String(), "read previous contract") || strings.Contains(stderr.String(), "read current contract") {
				t.Fatalf("%s command=%s read contract before rejecting unexpected arg: %s", trace, tc.name, stderr.String())
			}
		})
	}
}

func TestContractReadCommandsRejectMissingRequiredPathBeforeFileIO(t *testing.T) {
	const trace = "trace=cli-contract-read-missing-required-path-before-file-io"
	dir := t.TempDir()
	missingPrevious := filepath.Join(dir, "missing-previous.json")
	missingCurrent := filepath.Join(dir, "missing-current.json")

	for _, command := range []struct {
		name string
		args []string
	}{
		{
			name: "diff",
			args: []string{"contract", "diff", "--format", "json"},
		},
		{
			name: "policy",
			args: []string{"contract", "policy", "--strict", "--format", "json"},
		},
		{
			name: "plan",
			args: []string{"contract", "plan", "--validate", "--format", "json"},
		},
	} {
		for _, input := range []struct {
			name           string
			args           []string
			wantStderr     string
			forbiddenReads []string
		}{
			{
				name:           "missing-previous",
				args:           []string{"--current", missingCurrent},
				wantStderr:     "--previous is required",
				forbiddenReads: []string{"read current contract", missingCurrent},
			},
			{
				name:           "missing-current",
				args:           []string{"--previous", missingPrevious},
				wantStderr:     "--current is required",
				forbiddenReads: []string{"read previous contract", missingPrevious},
			},
		} {
			t.Run(command.name+"/"+input.name, func(t *testing.T) {
				args := append(append([]string{}, command.args...), input.args...)
				var stdout, stderr bytes.Buffer
				code := run(&stdout, &stderr, args)
				if code != 2 {
					t.Fatalf("%s command=%s input=%s exit code = %d, stderr = %s", trace, command.name, input.name, code, stderr.String())
				}
				if stdout.Len() != 0 {
					t.Fatalf("%s command=%s input=%s stdout = %s, want empty", trace, command.name, input.name, stdout.String())
				}
				assertContains(t, stderr.String(), input.wantStderr)
				for _, forbidden := range input.forbiddenReads {
					if strings.Contains(stderr.String(), forbidden) {
						t.Fatalf("%s command=%s input=%s read file before rejecting required path: %s", trace, command.name, input.name, stderr.String())
					}
				}
			})
		}
	}
}

func TestContractReadCommandsRejectInvalidContractInputs(t *testing.T) {
	const trace = "trace=cli-contract-read-invalid-input-rc-gate"
	dir := t.TempDir()
	validPath := writeCLIContract(t, dir, "valid.json", cliContractFixture())
	malformedPath := writeCLIBytes(t, dir, "malformed.json", []byte(`{`))
	semanticInvalidPath := writeCLIBytes(t, dir, "semantic-invalid.json", []byte(`{"contract_version":0}`))

	for _, command := range []struct {
		name string
		args []string
	}{
		{
			name: "diff",
			args: []string{"contract", "diff", "--format", "json"},
		},
		{
			name: "policy",
			args: []string{"contract", "policy", "--strict", "--format", "json"},
		},
		{
			name: "plan",
			args: []string{"contract", "plan", "--validate", "--format", "json"},
		},
	} {
		for _, input := range []struct {
			name        string
			previous    string
			current     string
			wantContext string
		}{
			{
				name:        "malformed-previous",
				previous:    malformedPath,
				current:     validPath,
				wantContext: "previous contract",
			},
			{
				name:        "semantic-invalid-current",
				previous:    validPath,
				current:     semanticInvalidPath,
				wantContext: "current contract",
			},
		} {
			t.Run(command.name+"/"+input.name, func(t *testing.T) {
				args := append(append([]string{}, command.args...), "--previous", input.previous, "--current", input.current)
				var stdout, stderr bytes.Buffer
				code := run(&stdout, &stderr, args)
				if code != 1 {
					t.Fatalf("%s command=%s input=%s exit code = %d, stderr = %s", trace, command.name, input.name, code, stderr.String())
				}
				if stdout.Len() != 0 {
					t.Fatalf("%s command=%s input=%s stdout = %s, want empty", trace, command.name, input.name, stdout.String())
				}
				assertContains(t, stderr.String(), "invalid module contract JSON")
				assertContains(t, stderr.String(), input.wantContext)
			})
		}
	}
}

func TestContractReadCommandsRejectMissingFilesWithSideContext(t *testing.T) {
	const trace = "trace=cli-contract-read-missing-file-context"
	dir := t.TempDir()
	validPath := writeCLIContract(t, dir, "valid.json", cliContractFixture())
	missingPath := filepath.Join(dir, "missing.json")

	for _, command := range []struct {
		name string
		args []string
	}{
		{
			name: "diff",
			args: []string{"contract", "diff", "--format", "json"},
		},
		{
			name: "policy",
			args: []string{"contract", "policy", "--strict", "--format", "json"},
		},
		{
			name: "plan",
			args: []string{"contract", "plan", "--validate", "--format", "json"},
		},
	} {
		for _, input := range []struct {
			name        string
			previous    string
			current     string
			wantContext string
		}{
			{
				name:        "missing-previous",
				previous:    missingPath,
				current:     validPath,
				wantContext: "read previous contract",
			},
			{
				name:        "missing-current",
				previous:    validPath,
				current:     missingPath,
				wantContext: "read current contract",
			},
		} {
			t.Run(command.name+"/"+input.name, func(t *testing.T) {
				args := append(append([]string{}, command.args...), "--previous", input.previous, "--current", input.current)
				var stdout, stderr bytes.Buffer
				code := run(&stdout, &stderr, args)
				if code != 1 {
					t.Fatalf("%s command=%s input=%s exit code = %d, stderr = %s", trace, command.name, input.name, code, stderr.String())
				}
				if stdout.Len() != 0 {
					t.Fatalf("%s command=%s input=%s stdout = %s, want empty", trace, command.name, input.name, stdout.String())
				}
				assertContains(t, stderr.String(), input.wantContext)
				assertContains(t, stderr.String(), missingPath)
			})
		}
	}
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

func TestContractCodegenRejectedInputsLeaveOutputUntouched(t *testing.T) {
	validContract, err := cliContractFixture().MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	for _, tc := range []struct {
		name         string
		trace        string
		contractData []byte
		language     string
		wantStderr   string
	}{
		{
			name:         "unsupported-language",
			trace:        "trace=cli-codegen-rejected-language-output-preservation",
			contractData: validContract,
			language:     "go",
			wantStderr:   `unsupported language "go"`,
		},
		{
			name:         "malformed-contract-json",
			trace:        "trace=cli-codegen-malformed-contract-output-preservation",
			contractData: []byte(`{`),
			language:     "typescript",
			wantStderr:   "invalid module contract",
		},
		{
			name:         "semantic-invalid-contract",
			trace:        "trace=cli-codegen-invalid-contract-output-preservation",
			contractData: []byte(`{"contract_version":0}`),
			language:     "typescript",
			wantStderr:   "invalid module contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			contractPath := filepath.Join(dir, "contract.json")
			if err := os.WriteFile(contractPath, tc.contractData, 0o666); err != nil {
				t.Fatalf("%s write contract input: %v", tc.trace, err)
			}
			outputPath := filepath.Join(dir, "client.ts")
			original := []byte("existing generated output\n")
			if err := os.WriteFile(outputPath, original, 0o666); err != nil {
				t.Fatalf("%s write existing output: %v", tc.trace, err)
			}

			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, []string{
				"contract", "codegen",
				"--contract", contractPath,
				"--language", tc.language,
				"--out", outputPath,
			})
			if code != 1 {
				t.Fatalf("%s contract codegen exit code = %d, stderr = %s", tc.trace, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.trace, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			got, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("%s read existing output: %v", tc.trace, err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("%s rejected codegen mutated output:\nobserved=%q\nexpected=%q", tc.trace, got, original)
			}
		})
	}
}

func TestContractCodegenMetadataMismatchLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=cli-codegen-metadata-mismatch-output-preservation"
	dir := t.TempDir()
	contract := cliContractFixture()
	contract.Codegen.ContractFormat = "unexpected.format"
	contract.Codegen.ContractVersion = shunter.ModuleContractVersion + 1
	contractData, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("%s MarshalCanonicalJSON returned error: %v", trace, err)
	}
	contractPath := writeCLIBytes(t, dir, "contract.json", contractData)
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--out", outputPath,
	})
	if code != 1 {
		t.Fatalf("%s contract codegen exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s stdout = %s, want empty", trace, stdout.String())
	}
	assertContains(t, stderr.String(), "invalid module contract")
	assertContains(t, stderr.String(), `codegen.contract_format = "unexpected.format"`)
	assertContains(t, stderr.String(), "codegen.contract_version")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s metadata mismatch mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
}

func TestContractCodegenRejectsUnexpectedArgBeforeFileIO(t *testing.T) {
	const trace = "trace=cli-codegen-unexpected-arg-before-file-io"
	dir := t.TempDir()
	missingContractPath := filepath.Join(dir, "missing-contract.json")
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", missingContractPath,
		"--language", "typescript",
		"--out", outputPath,
		"unexpected",
	})
	if code != 2 {
		t.Fatalf("%s contract codegen exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s stdout = %s, want empty", trace, stdout.String())
	}
	assertContains(t, stderr.String(), `unexpected argument "unexpected"`)
	if strings.Contains(stderr.String(), "read contract input") {
		t.Fatalf("%s read contract before rejecting unexpected arg: %s", trace, stderr.String())
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s unexpected arg mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
}

func TestContractCodegenRejectsMissingOutputPathBeforeFileIO(t *testing.T) {
	const trace = "trace=cli-codegen-missing-output-before-file-io"
	dir := t.TempDir()
	missingContractPath := filepath.Join(dir, "missing-contract.json")
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", missingContractPath,
		"--language", "typescript",
	})
	if code != 2 {
		t.Fatalf("%s contract codegen exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s stdout = %s, want empty", trace, stdout.String())
	}
	assertContains(t, stderr.String(), "--out is required")
	if strings.Contains(stderr.String(), "read contract input") || strings.Contains(stderr.String(), missingContractPath) {
		t.Fatalf("%s read contract before rejecting missing output path: %s", trace, stderr.String())
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s missing output path mutated nearby output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
}

func TestContractCodegenRejectsMissingContractPathBeforeFileIO(t *testing.T) {
	const trace = "trace=cli-codegen-missing-contract-before-file-io"
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--language", "typescript",
		"--out", outputPath,
	})
	if code != 2 {
		t.Fatalf("%s contract codegen exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s stdout = %s, want empty", trace, stdout.String())
	}
	assertContains(t, stderr.String(), "--contract is required")
	if strings.Contains(stderr.String(), "read contract input") {
		t.Fatalf("%s read contract before rejecting missing contract path: %s", trace, stderr.String())
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s missing contract path mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
}

func TestContractCodegenDirectoryOutputFailsWithoutMutationOrTempLeak(t *testing.T) {
	const trace = "trace=cli-codegen-directory-output-non-mutating"
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())
	outputPath := filepath.Join(dir, "client.ts")
	if err := os.Mkdir(outputPath, 0o755); err != nil {
		t.Fatalf("%s create output directory: %v", trace, err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--out", outputPath,
	})
	if code != 1 {
		t.Fatalf("%s contract codegen exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s stdout = %s, want empty", trace, stdout.String())
	}
	assertContains(t, stderr.String(), "write generated output")
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("%s stat output path: %v", trace, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s output path is not still a directory after failed codegen", trace)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
}

func TestContractCodegenMissingInputLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=cli-codegen-missing-input-output-preservation"
	dir := t.TempDir()
	contractPath := filepath.Join(dir, "missing-contract.json")
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--out", outputPath,
	})
	if code != 1 {
		t.Fatalf("%s contract codegen exit code = %d, stderr = %s", trace, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s stdout = %s, want empty", trace, stdout.String())
	}
	assertContains(t, stderr.String(), "read contract input")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s missing input mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
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
	return writeCLIBytes(t, dir, name, data)
}

func writeCLIBytes(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o666); err != nil {
		t.Fatalf("write CLI fixture: %v", err)
	}
	return path
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}

func assertNoCLITempFiles(t *testing.T, dir, outputBase string) {
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
