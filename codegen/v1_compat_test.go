package codegen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	shunter "github.com/ponchione/shunter"
)

func TestV1CompatibilityTypeScriptGolden(t *testing.T) {
	contractJSON, err := os.ReadFile(filepath.Join("..", "testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	got, err := GenerateFromJSON(contractJSON, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("GenerateFromJSON returned error: %v", err)
	}

	assertCodegenGoldenBytes(t, filepath.Join("testdata", "v1_module_contract.ts"), got)
}

func TestV1CompatibilityTypeScriptEntryPointsMatchGolden(t *testing.T) {
	contractJSON, err := os.ReadFile(filepath.Join("..", "testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.ts"))
	if err != nil {
		t.Fatalf("read v1 TypeScript fixture: %v", err)
	}
	var contract shunter.ModuleContract
	if err := json.Unmarshal(contractJSON, &contract); err != nil {
		t.Fatalf("Unmarshal v1 contract fixture: %v", err)
	}

	cases := []struct {
		name     string
		generate func() ([]byte, error)
	}{
		{
			name: "GenerateTypeScript",
			generate: func() ([]byte, error) {
				return GenerateTypeScript(contract)
			},
		},
		{
			name: "Generate",
			generate: func() ([]byte, error) {
				return Generate(contract, Options{Language: LanguageTypeScript})
			},
		},
		{
			name: "GenerateFromJSON",
			generate: func() ([]byte, error) {
				return GenerateFromJSON(contractJSON, Options{Language: LanguageTypeScript})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.generate()
			if err != nil {
				t.Fatalf("%s returned error: %v", tc.name, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s output differs from v1 TypeScript golden\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, want)
			}
		})
	}
}

func TestV1CompatibilityTypeScriptIgnoresUnknownContractJSONFields(t *testing.T) {
	contractJSON, err := os.ReadFile(filepath.Join("..", "testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	want, err := GenerateFromJSON(contractJSON, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("GenerateFromJSON returned error for fixture: %v", err)
	}
	got, err := GenerateFromJSON(v1ContractJSONWithUnknownFields(t, contractJSON), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("GenerateFromJSON returned error for fixture with unknown fields: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unknown contract JSON fields affected TypeScript output\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func v1ContractJSONWithUnknownFields(t *testing.T, data []byte) []byte {
	t.Helper()
	replacements := []struct {
		old string
		new string
	}{
		{
			old: "{\n  \"contract_version\": 1,",
			new: "{\n  \"future_top_level\": {\n    \"ignored\": true\n  },\n  \"contract_version\": 1,",
		},
		{
			old: "  \"module\": {\n    \"name\": \"v1_guardrails\",",
			new: "  \"module\": {\n    \"future_module_field\": \"ignored\",\n    \"name\": \"v1_guardrails\",",
		},
		{
			old: "  \"schema\": {\n    \"version\": 3,",
			new: "  \"schema\": {\n    \"future_schema_field\": [\n      \"ignored\"\n    ],\n    \"version\": 3,",
		},
		{
			old: "    {\n      \"name\": \"recent_messages\",\n      \"sql\": \"SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25\"\n    }",
			new: "    {\n      \"future_query_field\": \"ignored\",\n      \"name\": \"recent_messages\",\n      \"sql\": \"SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25\"\n    }",
		},
	}

	out := append([]byte(nil), data...)
	for _, replacement := range replacements {
		next := bytes.Replace(out, []byte(replacement.old), []byte(replacement.new), 1)
		if bytes.Equal(next, out) {
			t.Fatalf("v1 contract fixture missing replacement target %q", replacement.old)
		}
		out = next
	}
	return out
}

func assertCodegenGoldenBytes(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("SHUNTER_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update golden file %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden file %s mismatch\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
