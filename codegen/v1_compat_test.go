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

func TestV1CompatibilityTypeScriptSnapshotCoversStableCategories(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.ts"))
	if err != nil {
		t.Fatalf("read v1 TypeScript fixture: %v", err)
	}
	ts := string(data)

	for _, want := range []string{
		`export const shunterProtocol = {`,
		`defaultSubprotocol: "v1.bsatn.shunter",`,
		`export interface MessagesRow {`,
		`id: bigint;`,
		`sender: string;`,
		`body: string;`,
		`sentAt: bigint;`,
		`export const tables = {`,
		`messages: "messages",`,
		`export type TableName = (typeof tables)[keyof typeof tables];`,
		`export type TableRows = {`,
		`"messages": MessagesRow;`,
		`export const tableReadPolicies = {`,
		`messages: { access: "permissioned", permissions: ["messages:read"] },`,
		`export const visibilityFilters = {`,
		`ownMessages: { sql: "SELECT * FROM messages WHERE sender = :sender", returnTable: "messages", returnTableId: 0, usesCallerIdentity: true },`,
		`export const reducers = {`,
		`createMessage: "create_message",`,
		`export function callCreateMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`,
		`export const lifecycleReducers = {`,
		`OnConnect: "OnConnect",`,
		`export const queries = {`,
		`recentMessages: "recent_messages",`,
		`export const querySQL = {`,
		`recentMessages: "SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25",`,
		`export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`,
		`export const views = {`,
		`liveMessageProjection: "live_message_projection",`,
		`liveMessageCount: "live_message_count",`,
		`export const viewSQL = {`,
		`liveMessageProjection: "SELECT id, body AS text FROM messages",`,
		`liveMessageCount: "SELECT COUNT(*) AS n FROM messages",`,
		`export function subscribeLiveMessageProjection(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`,
		`export function subscribeLiveMessageCount(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`,
		`export const permissions = {`,
		`createMessage: { required: ["messages:write"] },`,
		`recentMessages: { required: ["messages:read"] },`,
		`liveMessageProjection: { required: ["messages:subscribe"] },`,
		`liveMessageCount: { required: ["messages:subscribe"] },`,
		`export const readModels = {`,
		`recentMessages: { tables: ["messages"], tags: ["history", "v1"] },`,
		`liveMessageProjection: { tables: ["messages"], tags: ["projection", "v1"] },`,
		`liveMessageCount: { tables: ["messages"], tags: ["aggregate", "v1"] },`,
	} {
		assertContains(t, ts, want)
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
