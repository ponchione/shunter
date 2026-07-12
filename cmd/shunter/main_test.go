package main

import (
	"bytes"
	"encoding/json"
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
	assertContains(t, out, "shunter describe --contract shunter.contract.json")
	assertContains(t, out, "shunter describe --url http://127.0.0.1:3000")
	assertContains(t, out, "shunter health --contract shunter.contract.json")
	assertContains(t, out, "shunter health --url http://127.0.0.1:3000")
	assertContains(t, out, "shunter contract validate --contract shunter.contract.json")
	assertContains(t, out, "shunter contract assert --contract shunter.contract.json")
	assertContains(t, out, "--profile internal|full|public")
	assertContains(t, out, "--section all|tables|reducers|procedures|queries|views|visibility")
	assertContains(t, out, "shunter backup --data-dir ./data --out ./backup")
	assertContains(t, out, "shunter restore --backup ./backup --data-dir ./data")
	assertContains(t, out, "offline DataDir")
	assertContains(t, out, "No dynamic module loading")
}

func TestContractHelpDocumentsAssertExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"contract", "--help"})
	if code != 0 {
		t.Fatalf("run contract --help exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("run contract --help stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Examples:")
	assertContains(t, out, "shunter contract assert --contract shunter.contract.json --module chat --module-version v0.1.0 --contract-version 1 --tables 1 --reducers 1 --format json")
	assertContains(t, out, "shunter contract assert --contract shunter.contract.json")
	assertContains(t, out, "--profile internal|full|public")
}

func TestContractAssertHelpDocumentsExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"contract", "assert", "--help"})
	if code != 0 {
		t.Fatalf("run contract assert --help exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("run contract assert --help stdout = %s, want empty", stdout.String())
	}
	out := stderr.String()
	assertContains(t, out, "Usage:")
	assertContains(t, out, "shunter contract assert --contract shunter.contract.json [assertions] [--format text|json]")
	assertContains(t, out, "Examples:")
	assertContains(t, out, "shunter contract assert --contract shunter.contract.json --module chat --module-version v0.1.0 --contract-version 1 --tables 1 --reducers 1 --format json")
	assertContains(t, out, "assertion_count and failure_count aggregate fields")
	assertContains(t, out, "-contract")
	assertContains(t, out, "-module-version")
	assertContains(t, out, "-visibility-filters")
}

func TestDescribeHelpDocumentsAllSections(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"describe", "--help"})
	if code != 0 {
		t.Fatalf("run describe --help exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("run describe --help stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "all, tables, reducers, procedures, queries, views, or visibility")
}

func TestRunningAppCommandsAcceptInterspersedFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "flag before positional",
			args: []string{"call", "--url", "http://127.0.0.1", "--contract", "missing.json", "--format", "yaml", "send_message", `{}`},
		},
		{
			name: "flag between positionals",
			args: []string{"procedure", "--url", "http://127.0.0.1", "--contract", "missing.json", "send_system_message", "--format", "yaml", `{}`},
		},
		{
			name: "flag after positionals",
			args: []string{"query", "--url", "http://127.0.0.1", "--contract", "missing.json", "recent_messages", `{}`, "--format", "yaml"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(&stdout, &stderr, tc.args); code != 2 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
			}
			assertContains(t, stderr.String(), "output format")
			if strings.Contains(stderr.String(), "requires") {
				t.Fatalf("stderr = %s, flag was treated as a positional", stderr.String())
			}
		})
	}
}

func TestRunningAppInterspersedFlagsHonorDoubleDashAndMalformedValues(t *testing.T) {
	t.Run("double dash", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(&stdout, &stderr, []string{
			"call", "--url", "http://127.0.0.1", "--contract", "missing.json",
			"--", "send_message", `{}`, "--format", "yaml",
		})
		if code != 2 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
		}
		assertContains(t, stderr.String(), "call requires reducer name")
		if strings.Contains(stderr.String(), "output format") {
			t.Fatalf("stderr = %s, flag after -- was parsed", stderr.String())
		}
	})

	t.Run("malformed value after positional", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(&stdout, &stderr, []string{
			"call", "--url", "http://127.0.0.1", "--contract", "missing.json",
			"send_message", `{}`, "--timeout", "not-a-duration",
		})
		if code != 2 {
			t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
		}
		assertContains(t, stderr.String(), "invalid value")
		assertContains(t, stderr.String(), "timeout")
	})
}

func TestInterspersedArgumentSourceFlagsRemainExclusive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"call", "--url", "http://127.0.0.1", "--contract", "missing.json",
		"send_message", `{}`, "--args", `{}`,
	})
	if code != 2 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	assertContains(t, stderr.String(), "provide only one of positional JSON, --args, --args-file, or --args-hex")
}

func TestDescribeCommandReadsContractText(t *testing.T) {
	dir := t.TempDir()
	contract := cliContractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_messages",
		SQL:  "SELECT * FROM messages",
		ResultShape: &shunter.ReadResultShape{
			Kind:  shunter.ReadResultShapeTable,
			Table: "messages",
		},
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		}},
	})
	contractPath := writeCLIContract(t, dir, "contract.json", contract)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"describe", "--contract", contractPath})
	if code != 0 {
		t.Fatalf("describe exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("describe stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Module: chat v1.0.0")
	assertContains(t, out, "Contract version: 1")
	assertContains(t, out, "Schema version: 1")
	assertContains(t, out, "Tables (1):")
	assertContains(t, out, "  - messages: 2 columns, 1 indexes")
	assertContains(t, out, "Reducers (1):")
	assertContains(t, out, "  - send_message")
	assertContains(t, out, "Queries (1):")
	assertContains(t, out, "  - history metadata-only")
	assertContains(t, out, "Views (1):")
	assertContains(t, out, "  - live_messages executable, table messages, row columns 2")
}

func TestDescribeCommandReadsContractJSON(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"describe", "--contract", contractPath, "--format", "json"})
	if code != 0 {
		t.Fatalf("describe --format json exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("describe --format json stderr = %s, want empty", stderr.String())
	}
	var summary struct {
		Module struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"module"`
		Section string `json:"section"`
		Counts  struct {
			Tables            int `json:"tables"`
			Columns           int `json:"columns"`
			Indexes           int `json:"indexes"`
			Reducers          int `json:"reducers"`
			Queries           int `json:"queries"`
			Views             int `json:"views"`
			VisibilityFilters int `json:"visibility_filters"`
		} `json:"counts"`
		Tables []struct {
			Name    string   `json:"name"`
			Columns []string `json:"columns"`
		} `json:"tables"`
		Reducers []struct {
			Name string `json:"name"`
		} `json:"reducers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode describe JSON: %v\n%s", err, stdout.String())
	}
	if summary.Module.Name != "chat" || summary.Module.Version != "v1.0.0" {
		t.Fatalf("module summary = %+v", summary.Module)
	}
	if summary.Section != "all" {
		t.Fatalf("section = %q, want all", summary.Section)
	}
	if summary.Counts.Tables != 1 || summary.Counts.Columns != 2 || summary.Counts.Indexes != 1 ||
		summary.Counts.Reducers != 1 || summary.Counts.Queries != 1 || summary.Counts.Views != 0 ||
		summary.Counts.VisibilityFilters != 0 {
		t.Fatalf("counts = %+v", summary.Counts)
	}
	if len(summary.Tables) != 1 || summary.Tables[0].Name != "messages" || len(summary.Tables[0].Columns) != 2 {
		t.Fatalf("table summary = %+v", summary.Tables)
	}
	if len(summary.Reducers) != 1 || summary.Reducers[0].Name != "send_message" {
		t.Fatalf("reducer summary = %+v", summary.Reducers)
	}
}

func TestDescribeCommandFiltersSections(t *testing.T) {
	dir := t.TempDir()
	contract := cliContractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_messages",
		SQL:  "SELECT * FROM messages",
		ResultShape: &shunter.ReadResultShape{
			Kind:  shunter.ReadResultShapeTable,
			Table: "messages",
		},
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		}},
	})
	contractPath := writeCLIContract(t, dir, "contract.json", contract)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"describe", "--contract", contractPath, "--section", "reducers"})
	if code != 0 {
		t.Fatalf("describe --section reducers exit code = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Section: reducers")
	assertContains(t, out, "Reducers (1):")
	assertContains(t, out, "  - send_message")
	if strings.Contains(out, "Tables (") || strings.Contains(out, "Queries (") || strings.Contains(out, "Views (") {
		t.Fatalf("filtered text output includes unrelated sections:\n%s", out)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{"describe", "--contract", contractPath, "--section", "views", "--format", "json"})
	if code != 0 {
		t.Fatalf("describe --section views --format json exit code = %d, stderr = %s", code, stderr.String())
	}
	var summary struct {
		Section string `json:"section"`
		Counts  struct {
			Tables int `json:"tables"`
			Views  int `json:"views"`
		} `json:"counts"`
		Tables []struct {
			Name string `json:"name"`
		} `json:"tables"`
		Views []struct {
			Name string `json:"name"`
		} `json:"views"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode describe section JSON: %v\n%s", err, stdout.String())
	}
	if summary.Section != "views" {
		t.Fatalf("section = %q, want views", summary.Section)
	}
	if summary.Counts.Tables != 1 || summary.Counts.Views != 1 {
		t.Fatalf("counts = %+v", summary.Counts)
	}
	if len(summary.Views) != 1 || summary.Views[0].Name != "live_messages" {
		t.Fatalf("views = %+v", summary.Views)
	}
	if summary.Tables != nil {
		t.Fatalf("tables = %+v, want nil for filtered JSON section", summary.Tables)
	}
}

func TestDescribeCommandRejectsInvalidInputsBeforeFileIO(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")

	for _, tc := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "missing-contract-flag",
			args:       []string{"describe", "--format", "json"},
			wantCode:   2,
			wantStderr: "provide exactly one of --contract or --url",
		},
		{
			name:       "unexpected-arg",
			args:       []string{"describe", "--contract", missingPath, "extra"},
			wantCode:   2,
			wantStderr: `unexpected argument "extra"`,
		},
		{
			name:       "unsupported-format",
			args:       []string{"describe", "--contract", missingPath, "--format", "yaml"},
			wantCode:   2,
			wantStderr: `unsupported contract workflow output format "yaml"`,
		},
		{
			name:       "unsupported-section",
			args:       []string{"describe", "--contract", missingPath, "--section", "health"},
			wantCode:   2,
			wantStderr: `unsupported describe section "health"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != tc.wantCode {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			if strings.Contains(stderr.String(), "read contract") {
				t.Fatalf("%s read contract before validation: %s", tc.name, stderr.String())
			}
		})
	}
}

func TestDescribeCommandRejectsMissingAndInvalidContracts(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	invalidPath := writeCLIBytes(t, dir, "invalid.json", []byte(`{"contract_version":0}`))

	for _, tc := range []struct {
		name       string
		path       string
		wantStderr string
	}{
		{
			name:       "missing",
			path:       missingPath,
			wantStderr: "read contract",
		},
		{
			name:       "invalid",
			path:       invalidPath,
			wantStderr: "invalid module contract JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, []string{"describe", "--contract", tc.path})
			if code != 1 {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
		})
	}
}

func TestHealthCommandReadsContractText(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"health", "--contract", contractPath})
	if code != 0 {
		t.Fatalf("health exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("health stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Status: ok")
	assertContains(t, out, "Scope: contract")
	assertContains(t, out, "Running server checked: false")
	assertContains(t, out, "local contract artifact is valid")
	assertContains(t, out, "Module: chat v1.0.0")
	assertContains(t, out, "Counts: 1 tables, 2 columns, 1 indexes, 1 reducers, 0 procedures, 1 queries, 0 views, 0 visibility filters")
}

func TestHealthCommandReadsContractJSON(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"health", "--contract", contractPath, "--format", "json"})
	if code != 0 {
		t.Fatalf("health --format json exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("health --format json stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status               string `json:"status"`
		Scope                string `json:"scope"`
		RunningServerChecked bool   `json:"running_server_checked"`
		Message              string `json:"message"`
		Describe             struct {
			Module struct {
				Name string `json:"name"`
			} `json:"module"`
			Counts struct {
				Tables   int `json:"tables"`
				Reducers int `json:"reducers"`
			} `json:"counts"`
		} `json:"describe"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode health JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "ok" || report.Scope != "contract" || report.RunningServerChecked {
		t.Fatalf("health report = %+v", report)
	}
	if !strings.Contains(report.Message, "no running server was checked") {
		t.Fatalf("health message = %q", report.Message)
	}
	if report.Describe.Module.Name != "chat" || report.Describe.Counts.Tables != 1 || report.Describe.Counts.Reducers != 1 {
		t.Fatalf("health describe = %+v", report.Describe)
	}
}

func TestHealthCommandRejectsInvalidInputsBeforeFileIO(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")

	for _, tc := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "missing-contract-flag",
			args:       []string{"health", "--format", "json"},
			wantCode:   2,
			wantStderr: "provide exactly one of --contract or --url",
		},
		{
			name:       "unexpected-arg",
			args:       []string{"health", "--contract", missingPath, "extra"},
			wantCode:   2,
			wantStderr: `unexpected argument "extra"`,
		},
		{
			name:       "unsupported-format",
			args:       []string{"health", "--contract", missingPath, "--format", "yaml"},
			wantCode:   2,
			wantStderr: `unsupported contract workflow output format "yaml"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != tc.wantCode {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			if strings.Contains(stderr.String(), "read contract") {
				t.Fatalf("%s read contract before validation: %s", tc.name, stderr.String())
			}
		})
	}
}

func TestHealthCommandRejectsMissingAndInvalidContracts(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	invalidPath := writeCLIBytes(t, dir, "invalid.json", []byte(`{"contract_version":0}`))

	for _, tc := range []struct {
		name       string
		path       string
		wantStderr string
	}{
		{
			name:       "missing",
			path:       missingPath,
			wantStderr: "read contract",
		},
		{
			name:       "invalid",
			path:       invalidPath,
			wantStderr: "invalid module contract JSON: health contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, []string{"health", "--contract", tc.path})
			if code != 1 {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
		})
	}
}

func TestContractValidateCommandReadsContractText(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"contract", "validate", "--contract", contractPath})
	if code != 0 {
		t.Fatalf("contract validate exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract validate stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Status: valid")
	assertContains(t, out, "Scope: contract")
	assertContains(t, out, "module contract JSON is valid")
	assertContains(t, out, "Module: chat v1.0.0")
	assertContains(t, out, "Counts: 1 tables, 2 columns, 1 indexes, 1 reducers, 0 procedures, 1 queries, 0 views, 0 visibility filters")
}

func TestContractValidateCommandReadsContractJSON(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"contract", "validate", "--contract", contractPath, "--format", "json"})
	if code != 0 {
		t.Fatalf("contract validate --format json exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract validate --format json stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status   string `json:"status"`
		Scope    string `json:"scope"`
		Message  string `json:"message"`
		Describe struct {
			Module struct {
				Name string `json:"name"`
			} `json:"module"`
			Counts struct {
				Tables   int `json:"tables"`
				Reducers int `json:"reducers"`
			} `json:"counts"`
		} `json:"describe"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode contract validate JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "valid" || report.Scope != "contract" {
		t.Fatalf("contract validate report = %+v", report)
	}
	if !strings.Contains(report.Message, "module contract JSON is valid") {
		t.Fatalf("contract validate message = %q", report.Message)
	}
	if report.Describe.Module.Name != "chat" || report.Describe.Counts.Tables != 1 || report.Describe.Counts.Reducers != 1 {
		t.Fatalf("contract validate describe = %+v", report.Describe)
	}
}

func TestContractValidateCommandRejectsInvalidInputsBeforeFileIO(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")

	for _, tc := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "missing-contract-flag",
			args:       []string{"contract", "validate", "--format", "json"},
			wantCode:   2,
			wantStderr: "--contract is required",
		},
		{
			name:       "unexpected-arg",
			args:       []string{"contract", "validate", "--contract", missingPath, "extra"},
			wantCode:   2,
			wantStderr: `unexpected argument "extra"`,
		},
		{
			name:       "unsupported-format",
			args:       []string{"contract", "validate", "--contract", missingPath, "--format", "yaml"},
			wantCode:   2,
			wantStderr: `unsupported contract workflow output format "yaml"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != tc.wantCode {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			if strings.Contains(stderr.String(), "read contract") {
				t.Fatalf("%s read contract before validation: %s", tc.name, stderr.String())
			}
		})
	}
}

func TestContractValidateCommandRejectsMissingAndInvalidContracts(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	invalidPath := writeCLIBytes(t, dir, "invalid.json", []byte(`{"contract_version":0}`))

	for _, tc := range []struct {
		name       string
		path       string
		wantStderr string
	}{
		{
			name:       "missing",
			path:       missingPath,
			wantStderr: "read contract",
		},
		{
			name:       "invalid",
			path:       invalidPath,
			wantStderr: "invalid module contract JSON: validate contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, []string{"contract", "validate", "--contract", tc.path})
			if code != 1 {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
		})
	}
}

func TestContractAssertCommandPassesText(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "assert",
		"--contract", contractPath,
		"--module", "chat",
		"--module-version", "v1.0.0",
		"--contract-version", "1",
		"--schema-version", "1",
		"--tables", "1",
		"--columns", "2",
		"--indexes", "1",
		"--reducers", "1",
		"--queries", "1",
		"--views", "0",
		"--visibility-filters", "0",
	})
	if code != 0 {
		t.Fatalf("contract assert exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract assert stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Status: passed")
	assertContains(t, out, "Scope: contract")
	assertContains(t, out, "11 contract assertion(s) passed")
	assertContains(t, out, "Module: chat v1.0.0")
	assertContains(t, out, "  - module: ok expected chat actual chat")
	assertContains(t, out, "  - module-version: ok expected v1.0.0 actual v1.0.0")
	assertContains(t, out, "  - contract-version: ok expected 1 actual 1")
	assertContains(t, out, "  - tables: ok expected 1 actual 1")
}

func TestContractAssertCommandPassesJSON(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "assert",
		"--contract", contractPath,
		"--module", "chat",
		"--module-version", "v1.0.0",
		"--contract-version", "1",
		"--tables", "1",
		"--reducers", "1",
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("contract assert --format json exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract assert --format json stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status         string `json:"status"`
		Scope          string `json:"scope"`
		Message        string `json:"message"`
		AssertionCount int    `json:"assertion_count"`
		FailureCount   int    `json:"failure_count"`
		Module         struct {
			Name string `json:"name"`
		} `json:"module"`
		Counts struct {
			Tables   int `json:"tables"`
			Reducers int `json:"reducers"`
		} `json:"counts"`
		Assertions []struct {
			Name           string  `json:"name"`
			ValueType      string  `json:"value_type"`
			ExpectedString *string `json:"expected_string"`
			ActualString   *string `json:"actual_string"`
			ExpectedNumber *int    `json:"expected_number"`
			ActualNumber   *int    `json:"actual_number"`
			Passed         bool    `json:"passed"`
		} `json:"assertions"`
		Failures []struct {
			Name string `json:"name"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode contract assert JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "passed" || report.Scope != "contract" || report.Module.Name != "chat" {
		t.Fatalf("contract assert report = %+v", report)
	}
	if report.Counts.Tables != 1 || report.Counts.Reducers != 1 {
		t.Fatalf("contract assert counts = %+v", report.Counts)
	}
	if report.AssertionCount != 5 || report.FailureCount != 0 ||
		len(report.Assertions) != 5 || len(report.Failures) != 0 {
		t.Fatalf("contract assert assertions = %+v failures = %+v", report.Assertions, report.Failures)
	}
	for _, assertion := range report.Assertions {
		if !assertion.Passed {
			t.Fatalf("assertion failed unexpectedly: %+v", assertion)
		}
	}
	moduleAssertion := report.Assertions[0]
	if moduleAssertion.Name != "module" || moduleAssertion.ValueType != "string" ||
		moduleAssertion.ExpectedString == nil || *moduleAssertion.ExpectedString != "chat" ||
		moduleAssertion.ActualString == nil || *moduleAssertion.ActualString != "chat" ||
		moduleAssertion.ExpectedNumber != nil || moduleAssertion.ActualNumber != nil {
		t.Fatalf("module assertion JSON shape = %+v", moduleAssertion)
	}
	contractVersionAssertion := report.Assertions[2]
	if contractVersionAssertion.Name != "contract-version" || contractVersionAssertion.ValueType != "number" ||
		contractVersionAssertion.ExpectedNumber == nil || *contractVersionAssertion.ExpectedNumber != 1 ||
		contractVersionAssertion.ActualNumber == nil || *contractVersionAssertion.ActualNumber != 1 ||
		contractVersionAssertion.ExpectedString != nil || contractVersionAssertion.ActualString != nil {
		t.Fatalf("contract-version assertion JSON shape = %+v", contractVersionAssertion)
	}
	assertNotContains(t, stdout.String(), `"expected":`)
	assertNotContains(t, stdout.String(), `"actual":`)
}

func TestContractAssertCommandAllowsZeroAssertions(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"contract", "assert", "--contract", contractPath})
	if code != 0 {
		t.Fatalf("contract assert without assertions exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract assert without assertions stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "Status: passed")
	assertContains(t, out, "0 contract assertion(s) passed")
	assertContains(t, out, "Assertions: none")

	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{"contract", "assert", "--contract", contractPath, "--format", "json"})
	if code != 0 {
		t.Fatalf("contract assert without assertions JSON exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract assert without assertions JSON stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status         string                `json:"status"`
		Message        string                `json:"message"`
		AssertionCount int                   `json:"assertion_count"`
		FailureCount   int                   `json:"failure_count"`
		Assertions     []contractAssertCheck `json:"assertions"`
		Failures       []contractAssertCheck `json:"failures"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode contract assert zero-assertion JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "passed" || report.Message != "0 contract assertion(s) passed" ||
		report.AssertionCount != 0 || report.FailureCount != 0 ||
		len(report.Assertions) != 0 || len(report.Failures) != 0 {
		t.Fatalf("zero-assertion report = %+v", report)
	}
}

func TestContractAssertCommandFailsMismatches(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "assert",
		"--contract", contractPath,
		"--module", "messages",
		"--module-version", "v9.9.9",
		"--contract-version", "2",
		"--tables", "2",
		"--format", "json",
	})
	if code != 1 {
		t.Fatalf("contract assert mismatch exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("contract assert mismatch stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status         string `json:"status"`
		Message        string `json:"message"`
		AssertionCount int    `json:"assertion_count"`
		FailureCount   int    `json:"failure_count"`
		Failures       []struct {
			Name           string  `json:"name"`
			ValueType      string  `json:"value_type"`
			ExpectedString *string `json:"expected_string"`
			ActualString   *string `json:"actual_string"`
			ExpectedNumber *int    `json:"expected_number"`
			ActualNumber   *int    `json:"actual_number"`
			Passed         bool    `json:"passed"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode contract assert mismatch JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "failed" || report.AssertionCount != 4 ||
		report.FailureCount != 4 || len(report.Failures) != 4 {
		t.Fatalf("contract assert mismatch report = %+v", report)
	}
	assertContains(t, report.Message, "4 contract assertion(s) failed")
	if report.Failures[0].Passed || report.Failures[1].Passed {
		t.Fatalf("failures marked passed: %+v", report.Failures)
	}
	moduleFailure := report.Failures[0]
	if moduleFailure.Name != "module" || moduleFailure.ValueType != "string" ||
		moduleFailure.ExpectedString == nil || *moduleFailure.ExpectedString != "messages" ||
		moduleFailure.ActualString == nil || *moduleFailure.ActualString != "chat" ||
		moduleFailure.ExpectedNumber != nil || moduleFailure.ActualNumber != nil {
		t.Fatalf("module failure JSON shape = %+v", moduleFailure)
	}
	contractVersionFailure := report.Failures[2]
	if contractVersionFailure.Name != "contract-version" || contractVersionFailure.ValueType != "number" ||
		contractVersionFailure.ExpectedNumber == nil || *contractVersionFailure.ExpectedNumber != 2 ||
		contractVersionFailure.ActualNumber == nil || *contractVersionFailure.ActualNumber != 1 ||
		contractVersionFailure.ExpectedString != nil || contractVersionFailure.ActualString != nil {
		t.Fatalf("contract-version failure JSON shape = %+v", contractVersionFailure)
	}
	assertNotContains(t, stdout.String(), `"expected":`)
	assertNotContains(t, stdout.String(), `"actual":`)
}

func TestContractAssertCommandRejectsInvalidInputsBeforeFileIO(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")

	for _, tc := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "missing-contract-flag",
			args:       []string{"contract", "assert", "--tables", "1"},
			wantCode:   2,
			wantStderr: "--contract is required",
		},
		{
			name:       "unexpected-arg",
			args:       []string{"contract", "assert", "--contract", missingPath, "extra"},
			wantCode:   2,
			wantStderr: `unexpected argument "extra"`,
		},
		{
			name:       "unsupported-format",
			args:       []string{"contract", "assert", "--contract", missingPath, "--format", "yaml"},
			wantCode:   2,
			wantStderr: `unsupported contract workflow output format "yaml"`,
		},
		{
			name:       "negative-count",
			args:       []string{"contract", "assert", "--contract", missingPath, "--tables", "-2"},
			wantCode:   2,
			wantStderr: "--tables must be >= 0",
		},
		{
			name:       "negative-contract-version",
			args:       []string{"contract", "assert", "--contract", missingPath, "--contract-version", "-2"},
			wantCode:   2,
			wantStderr: "--contract-version must be >= 0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != tc.wantCode {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			if strings.Contains(stderr.String(), "read contract") {
				t.Fatalf("%s read contract before validation: %s", tc.name, stderr.String())
			}
		})
	}
}

func TestContractAssertCommandRejectsMissingAndInvalidContracts(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	invalidPath := writeCLIBytes(t, dir, "invalid.json", []byte(`{"contract_version":0}`))

	for _, tc := range []struct {
		name       string
		path       string
		wantStderr string
	}{
		{
			name:       "missing",
			path:       missingPath,
			wantStderr: "read contract",
		},
		{
			name:       "invalid",
			path:       invalidPath,
			wantStderr: "invalid module contract JSON: assert contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, []string{"contract", "assert", "--contract", tc.path, "--tables", "1"})
			if code != 1 {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
		})
	}
}

func TestVersionCommandPrintsBuildInfo(t *testing.T) {
	oldVersion := shunter.Version
	oldCommit := shunter.Commit
	oldDate := shunter.Date
	shunter.Version = "v9.8.7"
	shunter.Commit = "abc123"
	shunter.Date = "2026-05-03T12:34:56Z"
	defer func() {
		shunter.Version = oldVersion
		shunter.Commit = oldCommit
		shunter.Date = oldDate
	}()

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"version"})
	if code != 0 {
		t.Fatalf("run version exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("version stderr = %s, want empty", stderr.String())
	}
	out := stdout.String()
	assertContains(t, out, "shunter v9.8.7\n")
	assertContains(t, out, "commit abc123\n")
	assertContains(t, out, "date 2026-05-03T12:34:56Z\n")
	assertContains(t, out, "go ")
}

func TestVersionFlagPrintsBuildInfo(t *testing.T) {
	oldVersion := shunter.Version
	shunter.Version = "v9.8.7"
	defer func() {
		shunter.Version = oldVersion
	}()

	for _, arg := range []string{"--version", "-version"} {
		var stdout, stderr bytes.Buffer
		code := run(&stdout, &stderr, []string{arg})
		if code != 0 {
			t.Fatalf("run %s exit code = %d, stderr = %s", arg, code, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("%s stderr = %s, want empty", arg, stderr.String())
		}
		assertContains(t, stdout.String(), "shunter v9.8.7\n")
	}
}

func TestBackupAndRestoreCommandsCopyDataDir(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "7"), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	writeCLIBytes(t, dataDir, "00000000000000000001.log", []byte("segment-1"))
	writeCLIBytes(t, filepath.Join(dataDir, "7"), "snapshot", []byte("snapshot-7"))

	backupDir := filepath.Join(dir, "backup")
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", dataDir,
		"--out", backupDir,
	})
	if code != 0 {
		t.Fatalf("backup exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("backup stderr = %s, want empty", stderr.String())
	}
	assertContains(t, stdout.String(), "backed up "+dataDir+" to "+backupDir)
	assertFileBytes(t, filepath.Join(backupDir, "00000000000000000001.log"), []byte("segment-1"))
	assertFileBytes(t, filepath.Join(backupDir, "7", "snapshot"), []byte("snapshot-7"))

	restoreDir := filepath.Join(dir, "restored")
	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{
		"restore",
		"--backup", backupDir,
		"--data-dir", restoreDir,
	})
	if code != 0 {
		t.Fatalf("restore exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("restore stderr = %s, want empty", stderr.String())
	}
	assertContains(t, stdout.String(), "restored "+backupDir+" to "+restoreDir)
	assertFileBytes(t, filepath.Join(restoreDir, "00000000000000000001.log"), []byte("segment-1"))
	assertFileBytes(t, filepath.Join(restoreDir, "7", "snapshot"), []byte("snapshot-7"))
}

func TestBackupRejectsExistingOutputWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	outputDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	original := []byte("existing backup data")
	writeCLIBytes(t, outputDir, "existing", original)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", dataDir,
		"--out", outputDir,
	})
	if code != 1 {
		t.Fatalf("backup exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("backup stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "backup output "+outputDir+" already exists")
	assertFileBytes(t, filepath.Join(outputDir, "existing"), original)
}

func TestBackupCommandErrorsNameSourceDataDir(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "backup")
	missingDataDir := filepath.Join(dir, "missing-data")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", missingDataDir,
		"--out", outputDir,
	})
	if code != 1 {
		t.Fatalf("backup missing source exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("backup missing source stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "read source data dir "+missingDataDir)
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("backup output stat after missing source = %v, want not exist", statErr)
	}

	dataFile := writeCLIBytes(t, dir, "data-file", []byte("not a data dir"))
	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", dataFile,
		"--out", outputDir,
	})
	if code != 1 {
		t.Fatalf("backup file source exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("backup file source stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "source data dir "+dataFile+" is not a directory")
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("backup output stat after file source = %v, want not exist", statErr)
	}
}

func TestRestoreRejectsNonEmptyDestinationWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	writeCLIBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))
	restoreDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatalf("create restore dir: %v", err)
	}
	original := []byte("existing runtime data")
	writeCLIBytes(t, restoreDir, "existing", original)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"restore",
		"--backup", backupDir,
		"--data-dir", restoreDir,
	})
	if code != 1 {
		t.Fatalf("restore exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "restore destination "+restoreDir+" is not empty")
	assertFileBytes(t, filepath.Join(restoreDir, "existing"), original)
}

func TestRestoreRejectsFileDestinationWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	writeCLIBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))
	restorePath := filepath.Join(dir, "data-file")
	original := []byte("existing file data")
	if err := os.WriteFile(restorePath, original, 0o666); err != nil {
		t.Fatalf("write restore destination file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"restore",
		"--backup", backupDir,
		"--data-dir", restorePath,
	})
	if code != 1 {
		t.Fatalf("restore file destination exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore file destination stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "restore destination "+restorePath+" is not a directory")
	assertFileBytes(t, restorePath, original)
}

func TestRestoreCommandErrorsNameBackupSource(t *testing.T) {
	dir := t.TempDir()
	restoreDir := filepath.Join(dir, "restore")
	missingBackup := filepath.Join(dir, "missing-backup")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"restore",
		"--backup", missingBackup,
		"--data-dir", restoreDir,
	})
	if code != 1 {
		t.Fatalf("restore missing backup exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore missing backup stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "read backup "+missingBackup)
	if strings.Contains(stderr.String(), "source data dir") {
		t.Fatalf("restore missing backup stderr = %s, should name backup source", stderr.String())
	}

	backupFile := writeCLIBytes(t, dir, "backup-file", []byte("not a backup directory"))
	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{
		"restore",
		"--backup", backupFile,
		"--data-dir", restoreDir,
	})
	if code != 1 {
		t.Fatalf("restore file backup exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore file backup stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "backup "+backupFile+" is not a directory")
}

func TestBackupRestoreRejectBlankPathsBeforeFileIO(t *testing.T) {
	dir := t.TempDir()
	nearby := filepath.Join(dir, "nearby")
	original := []byte("nearby data")
	writeCLIBytes(t, dir, "nearby", original)

	for _, tc := range []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "backup-data-dir",
			args:       []string{"backup", "--data-dir", " \t", "--out", filepath.Join(dir, "backup")},
			wantStderr: "--data-dir is required",
		},
		{
			name:       "backup-out",
			args:       []string{"backup", "--data-dir", dir, "--out", "\n"},
			wantStderr: "--out is required",
		},
		{
			name:       "restore-backup",
			args:       []string{"restore", "--backup", " \t", "--data-dir", filepath.Join(dir, "restore")},
			wantStderr: "--backup is required",
		},
		{
			name:       "restore-data-dir",
			args:       []string{"restore", "--backup", dir, "--data-dir", "\n"},
			wantStderr: "--data-dir is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 2 {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			assertFileBytes(t, nearby, original)
		})
	}
}

func TestBackupRejectsDestinationInsideSource(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	outputDir := filepath.Join(dataDir, "backup")
	code := run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", dataDir,
		"--out", outputDir,
	})
	if code != 1 {
		t.Fatalf("backup exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("backup stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "must not be inside source data dir")
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("nested output stat err = %v, want not exist", err)
	}
}

func TestBackupRestoreCommandsRejectSymlinkPaths(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	writeCLIBytes(t, dataDir, "00000000000000000001.log", []byte("segment-1"))
	backupTarget := filepath.Join(dir, "backup-target")
	if err := os.MkdirAll(backupTarget, 0o755); err != nil {
		t.Fatalf("create backup target: %v", err)
	}
	backupTargetData := []byte("existing backup target data")
	writeCLIBytes(t, backupTarget, "existing", backupTargetData)
	backupDir := filepath.Join(dir, "backup-source")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup source: %v", err)
	}
	writeCLIBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))
	restoreTarget := filepath.Join(dir, "restore-target")
	if err := os.MkdirAll(restoreTarget, 0o755); err != nil {
		t.Fatalf("create restore target: %v", err)
	}
	dataLink := filepath.Join(dir, "data-link")
	if err := os.Symlink(dataDir, dataLink); err != nil {
		t.Skipf("create data symlink: %v", err)
	}
	backupLink := filepath.Join(dir, "backup-link")
	if err := os.Symlink(backupTarget, backupLink); err != nil {
		t.Skipf("create backup symlink: %v", err)
	}
	restoreLink := filepath.Join(dir, "restore-link")
	if err := os.Symlink(restoreTarget, restoreLink); err != nil {
		t.Skipf("create restore symlink: %v", err)
	}

	for _, tc := range []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "backup",
			args:       []string{"backup", "--data-dir", dataLink, "--out", filepath.Join(dir, "backup")},
			wantStderr: "is a symlink; refusing to copy",
		},
		{
			name:       "backup-output",
			args:       []string{"backup", "--data-dir", dataDir, "--out", backupLink},
			wantStderr: "backup output " + backupLink + " already exists",
		},
		{
			name:       "restore",
			args:       []string{"restore", "--backup", dataLink, "--data-dir", filepath.Join(dir, "restore")},
			wantStderr: "is a symlink; refusing to restore",
		},
		{
			name:       "restore-destination",
			args:       []string{"restore", "--backup", backupDir, "--data-dir", restoreLink},
			wantStderr: "restore destination " + restoreLink + " is a symlink; refusing to restore",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 1 {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
		})
	}
	if entries, err := os.ReadDir(restoreTarget); err != nil {
		t.Fatalf("read restore target after rejected restore: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("restore target entries after rejected restore = %#v, want empty", entries)
	}
	assertFileBytes(t, filepath.Join(backupTarget, "existing"), backupTargetData)
}

func TestBackupRestoreCommandsRejectSymlinkEntries(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	sourceTarget := writeCLIBytes(t, dataDir, "target", []byte("target"))
	if err := os.Symlink(sourceTarget, filepath.Join(dataDir, "entry-link")); err != nil {
		t.Skipf("create data dir entry symlink: %v", err)
	}

	var stdout, stderr bytes.Buffer
	backupDir := filepath.Join(dir, "backup")
	code := run(&stdout, &stderr, []string{
		"backup",
		"--data-dir", dataDir,
		"--out", backupDir,
	})
	if code != 1 {
		t.Fatalf("backup symlink entry exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("backup symlink entry stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "entry-link")
	assertContains(t, stderr.String(), "is a symlink; refusing to copy")
	if _, statErr := os.Lstat(filepath.Join(backupDir, "entry-link")); !os.IsNotExist(statErr) {
		t.Fatalf("backup symlink entry stat = %v, want not exist", statErr)
	}

	restoreBackup := filepath.Join(dir, "restore-backup")
	if err := os.MkdirAll(restoreBackup, 0o755); err != nil {
		t.Fatalf("create restore backup dir: %v", err)
	}
	restoreTarget := writeCLIBytes(t, restoreBackup, "target", []byte("target"))
	if err := os.Symlink(restoreTarget, filepath.Join(restoreBackup, "entry-link")); err != nil {
		t.Skipf("create backup entry symlink: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	restoreDir := filepath.Join(dir, "restore")
	code = run(&stdout, &stderr, []string{
		"restore",
		"--backup", restoreBackup,
		"--data-dir", restoreDir,
	})
	if code != 1 {
		t.Fatalf("restore symlink entry exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore symlink entry stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "entry-link")
	assertContains(t, stderr.String(), "is a symlink; refusing to copy")
	if _, statErr := os.Lstat(filepath.Join(restoreDir, "entry-link")); !os.IsNotExist(statErr) {
		t.Fatalf("restore symlink entry stat = %v, want not exist", statErr)
	}
}

func TestRestoreRejectsDestinationInsideBackup(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	writeCLIBytes(t, backupDir, "00000000000000000001.log", []byte("segment-1"))

	var stdout, stderr bytes.Buffer
	restoreDir := filepath.Join(backupDir, "restore")
	code := run(&stdout, &stderr, []string{
		"restore",
		"--backup", backupDir,
		"--data-dir", restoreDir,
	})
	if code != 1 {
		t.Fatalf("restore exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("restore stdout = %s, want empty", stdout.String())
	}
	assertContains(t, stderr.String(), "must not be inside source data dir")
	if _, err := os.Stat(restoreDir); !os.IsNotExist(err) {
		t.Fatalf("nested restore stat err = %v, want not exist", err)
	}
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

func TestContractCommandsRejectWhitespaceRequiredPathsBeforeFileIO(t *testing.T) {
	const trace = "trace=cli-contract-whitespace-required-path-before-file-io"
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("%s write existing output: %v", trace, err)
	}

	for _, tc := range []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "diff-previous",
			args:       []string{"contract", "diff", "--previous", " \t", "--current", missingPath},
			wantStderr: "--previous is required",
		},
		{
			name:       "diff-current",
			args:       []string{"contract", "diff", "--previous", missingPath, "--current", "\n"},
			wantStderr: "--current is required",
		},
		{
			name:       "codegen-contract",
			args:       []string{"contract", "codegen", "--contract", " \t", "--out", outputPath},
			wantStderr: "--contract is required",
		},
		{
			name:       "codegen-out",
			args:       []string{"contract", "codegen", "--contract", missingPath, "--out", "\n"},
			wantStderr: "--out is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 2 {
				t.Fatalf("%s case=%s exit code = %d, stderr = %s", trace, tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s case=%s stdout = %s, want empty", trace, tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			if strings.Contains(stderr.String(), "read previous contract") ||
				strings.Contains(stderr.String(), "read current contract") ||
				strings.Contains(stderr.String(), "read contract input") ||
				strings.Contains(stderr.String(), missingPath) {
				t.Fatalf("%s case=%s performed file I/O before rejecting blank path: %s", trace, tc.name, stderr.String())
			}
			got, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("%s case=%s read existing output: %v", trace, tc.name, err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("%s case=%s blank path mutated output:\nobserved=%q\nexpected=%q", trace, tc.name, got, original)
			}
		})
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

func TestContractCodegenCommandAcceptsTypeScriptRuntimeImport(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", cliContractFixture())
	outputPath := filepath.Join(dir, "client.ts")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--runtime-import", "@app/shunter-runtime",
		"--out", outputPath,
	})
	if code != 0 {
		t.Fatalf("contract codegen exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read generated output: %v", err)
	}
	assertContains(t, string(data), `} from "@app/shunter-runtime";`)
	assertNotContains(t, string(data), `} from "@shunter/client";`)
	assertContains(t, string(data), `runtimeImport: "@app/shunter-runtime",`)
	assertContains(t, stdout.String(), "wrote "+outputPath)
}

func TestContractCodegenCommandAcceptsProfile(t *testing.T) {
	dir := t.TempDir()
	contract := cliContractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables, schema.TableExport{
		Name: "private_messages",
		SDK:  &schema.TableSDKMetadata{Visibility: schema.TableSDKVisibilityPrivate},
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		},
		Indexes: []schema.IndexExport{{Name: "private_messages_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
	})
	contractPath := writeCLIContract(t, dir, "contract.json", contract)
	defaultOutputPath := filepath.Join(dir, "client.default.ts")
	publicOutputPath := filepath.Join(dir, "client.public.ts")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--out", defaultOutputPath,
	})
	if code != 0 {
		t.Fatalf("default contract codegen exit code = %d, stderr = %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{
		"contract", "codegen",
		"--contract", contractPath,
		"--language", "typescript",
		"--profile", "public",
		"--out", publicOutputPath,
	})
	if code != 0 {
		t.Fatalf("profile contract codegen exit code = %d, stderr = %s", code, stderr.String())
	}
	defaultData, err := os.ReadFile(defaultOutputPath)
	if err != nil {
		t.Fatalf("read default output: %v", err)
	}
	publicData, err := os.ReadFile(publicOutputPath)
	if err != nil {
		t.Fatalf("read public output: %v", err)
	}
	assertContains(t, string(defaultData), `privateMessages: "private_messages",`)
	assertContains(t, string(defaultData), `export function subscribePrivateMessages(`)
	assertContains(t, string(defaultData), `generationProfile: "internal",`)
	assertContains(t, string(publicData), `messages: "messages",`)
	assertContains(t, string(publicData), `generationProfile: "public",`)
	assertNotContains(t, string(publicData), `privateMessages: "private_messages",`)
	assertNotContains(t, string(publicData), `export function subscribePrivateMessages(`)
	assertContains(t, stdout.String(), "wrote "+publicOutputPath)
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
		profile      string
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
			name:         "unsupported-profile",
			trace:        "trace=cli-codegen-rejected-profile-output-preservation",
			contractData: validContract,
			language:     "typescript",
			profile:      "private",
			wantStderr:   `unsupported codegen profile "private"`,
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
			args := []string{
				"contract", "codegen",
				"--contract", contractPath,
				"--language", tc.language,
				"--out", outputPath,
			}
			if tc.profile != "" {
				args = append(args, "--profile", tc.profile)
			}
			code := run(&stdout, &stderr, args)
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

func TestContractCodegenUnknownMigrationTargetLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=cli-codegen-unknown-migration-target-output-preservation"
	dir := t.TempDir()
	contract := cliContractFixture()
	contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceTable,
		Name:    "missing_table",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
		},
	}}
	contractPath := writeCLIContract(t, dir, "contract.json", contract)
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
	assertContains(t, stderr.String(), "migrations.table.missing_table references unknown table")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s unknown migration target mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
	}
	assertNoCLITempFiles(t, dir, filepath.Base(outputPath))
}

func TestContractCodegenInvalidSchemaColumnTypeLeavesOutputUntouched(t *testing.T) {
	const trace = "trace=cli-codegen-invalid-schema-column-type-output-preservation"
	dir := t.TempDir()
	contract := cliContractFixture()
	contract.Schema.Tables[0].Columns[1].Type = "notAType"
	contractPath := writeCLIContract(t, dir, "contract.json", contract)
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
	assertContains(t, stderr.String(), `schema.tables.messages.columns.body type "notAType" is invalid`)
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s read existing output: %v", trace, err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("%s invalid schema column type mutated output:\nobserved=%q\nexpected=%q", trace, got, original)
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

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("unexpected %q in:\n%s", needle, haystack)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes = %q, want %q", path, got, want)
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
