package codegen

import (
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestGeneratorAcceptsCanonicalContractJSON(t *testing.T) {
	data, err := contractFixture().MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	out, err := GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("GenerateFromJSON returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export interface MessagesRow {`)
	assertContains(t, ts, `id: bigint;`)
	assertContains(t, ts, `body: string;`)
	assertContains(t, ts, `sentAt: bigint;`)
	assertContains(t, ts, `payload: Uint8Array;`)
	assertContains(t, ts, `tags: string[];`)
	assertContains(t, ts, `export const reducers = {`)
	assertContains(t, ts, `sendMessage: "send_message",`)
	assertContains(t, ts, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `export const lifecycleReducers = {`)
	assertContains(t, ts, `OnConnect: "OnConnect",`)
	assertContains(t, ts, `export const queries = {`)
	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `export function queryRecentMessages(runQuery: QueryRunner, args?: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `export const views = {`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeView: ViewSubscriber, args?: Uint8Array): Promise<() => void> {`)
}

func TestGeneratorAcceptsModuleContractWithoutRuntime(t *testing.T) {
	out, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	assertContains(t, string(out), `export interface MessagesRow {`)
	assertContains(t, string(out), `export function subscribeMessages(subscribeTable: TableSubscriber<MessagesRow>): Promise<() => void> {`)
}

func TestGeneratorRejectsUnsupportedLanguage(t *testing.T) {
	_, err := Generate(contractFixture(), Options{Language: "go"})
	if err == nil {
		t.Fatal("Generate returned nil error, want unsupported language error")
	}
	if !strings.Contains(err.Error(), `unsupported language "go"`) {
		t.Fatalf("Generate error = %v, want unsupported language", err)
	}
}

func TestGeneratorRejectsUnusableContractJSON(t *testing.T) {
	_, err := GenerateFromJSON([]byte(`{"version":1,"tables":[],"reducers":[]}`), Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !strings.Contains(err.Error(), "invalid module contract") {
		t.Fatalf("GenerateFromJSON error = %v, want invalid module contract", err)
	}
}

func TestTypeScriptGeneratorIsDeterministic(t *testing.T) {
	first, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	second, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("Generate was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func contractFixture() shunter.ModuleContract {
	return shunter.ModuleContract{
		ContractVersion: shunter.ModuleContractVersion,
		Module: shunter.ModuleContractIdentity{
			Name:     "chat",
			Version:  "v1.2.3",
			Metadata: map[string]string{"team": "runtime"},
		},
		Schema: schema.SchemaExport{
			Version: 1,
			Tables: []schema.TableExport{
				{
					Name: "messages",
					Columns: []schema.ColumnExport{
						{Name: "id", Type: "uint64"},
						{Name: "body", Type: "string"},
						{Name: "sent_at", Type: "timestamp"},
						{Name: "payload", Type: "bytes"},
						{Name: "tags", Type: "arrayString"},
					},
					Indexes: []schema.IndexExport{
						{Name: "messages_pk", Columns: []string{"id"}, Unique: true, Primary: true},
					},
				},
			},
			Reducers: []schema.ReducerExport{
				{Name: "send_message", Lifecycle: false},
				{Name: "OnConnect", Lifecycle: true},
			},
		},
		Queries: []shunter.QueryDescription{
			{Name: "recent_messages"},
		},
		Views: []shunter.ViewDescription{
			{Name: "live_messages"},
		},
		Permissions: shunter.PermissionContract{
			Reducers: []shunter.PermissionContractDeclaration{},
			Queries:  []shunter.PermissionContractDeclaration{},
			Views:    []shunter.PermissionContractDeclaration{},
		},
		ReadModel: shunter.ReadModelContract{
			Declarations: []shunter.ReadModelContractDeclaration{},
		},
		Migrations: shunter.MigrationContract{
			Declarations: []shunter.MigrationContractDeclaration{},
		},
		Codegen: shunter.CodegenContractMetadata{
			ContractFormat:          shunter.ModuleContractFormat,
			ContractVersion:         shunter.ModuleContractVersion,
			DefaultSnapshotFilename: shunter.DefaultContractSnapshotFilename,
		},
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("generated TypeScript missing %q:\n%s", needle, haystack)
	}
}
