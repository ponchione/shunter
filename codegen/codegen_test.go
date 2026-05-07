package codegen

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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
	assertContains(t, ts, `export type TableRow<Name extends TableName> = TableRows[Name];`)
	assertContains(t, ts, `export type TableSubscriber<Row = never> = ShunterTableSubscriber<TableName, TableRows, Row>;`)
	assertContains(t, ts, `export type TableRows = {`)
	assertContains(t, ts, `"messages": MessagesRow;`)
	assertContains(t, ts, `export const reducers = {`)
	assertContains(t, ts, `sendMessage: "send_message",`)
	assertContains(t, ts, `export type ReducerCaller = ShunterReducerCaller<ReducerName, Uint8Array, Uint8Array>;`)
	assertContains(t, ts, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `export const lifecycleReducers = {`)
	assertContains(t, ts, `OnConnect: "OnConnect",`)
	assertContains(t, ts, `export type QueryRunner = ShunterQueryRunner<Uint8Array>;`)
	assertContains(t, ts, `export type ViewSubscriber = ShunterViewSubscriber;`)
	assertContains(t, ts, `export type DeclaredQueryRunner = ShunterDeclaredQueryRunner<ExecutableQueryName, Uint8Array>;`)
	assertContains(t, ts, `export type DeclaredViewSubscriber = ShunterDeclaredViewSubscriber<ExecutableViewName>;`)
	assertContains(t, ts, `export const tableReadPolicies = {`)
	assertContains(t, ts, `messages: { access: "private", permissions: [] },`)
	assertContains(t, ts, `export const queries = {`)
	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `export const querySQL = {`)
	assertContains(t, ts, `recentMessages: "SELECT * FROM messages",`)
	assertContains(t, ts, `export type ExecutableQueryName = (typeof queries)[keyof typeof querySQL];`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export const views = {`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `export const viewSQL = {`)
	assertContains(t, ts, `liveMessages: "SELECT * FROM messages",`)
	assertContains(t, ts, `export type ExecutableViewName = (typeof views)[keyof typeof viewSQL];`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_messages");`)
	assertNotContains(t, ts, `return runQuery("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return subscribeView("SELECT * FROM messages");`)
	assertContains(t, ts, `export const permissions = {`)
	assertContains(t, ts, `reducers: {`)
	assertContains(t, ts, `sendMessage: { required: ["messages:send"] },`)
	assertContains(t, ts, `queries: {`)
	assertContains(t, ts, `recentMessages: { required: ["messages:read"] },`)
	assertContains(t, ts, `views: {`)
	assertContains(t, ts, `liveMessages: { required: ["messages:subscribe"] },`)
	assertContains(t, ts, `export const readModels = {`)
	assertContains(t, ts, `recentMessages: { tables: ["messages"], tags: ["history"] },`)
	assertContains(t, ts, `liveMessages: { tables: ["messages"], tags: ["realtime"] },`)
}

func TestGeneratorMapsNullableColumnsToUnionNull(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns[1].Nullable = true
	out, err := GenerateTypeScript(contract)
	if err != nil {
		t.Fatalf("GenerateTypeScript returned error: %v", err)
	}
	assertContains(t, string(out), `body: string | null;`)
}

func TestGeneratorAcceptsModuleContractWithoutRuntime(t *testing.T) {
	out, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	assertContains(t, string(out), `export interface MessagesRow {`)
	assertContains(t, string(out), `export function subscribeMessages(subscribeTable: TableSubscriber<MessagesRow>): Promise<() => void> {`)
}

func TestTypeScriptGeneratorMapsUUIDColumns(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns = append(contract.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "external_id",
		Type: "uuid",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export type UUID = string;`)
	assertContains(t, ts, `externalId: UUID;`)
}

func TestTypeScriptGeneratorMapsDurationColumns(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns = append(contract.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "ttl",
		Type: "duration",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `ttl: bigint;`)
}

func TestTypeScriptGeneratorMapsJSONColumns(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns = append(contract.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "metadata",
		Type: "json",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	assertContains(t, string(out), `metadata: unknown;`)
}

func TestTypeScriptGeneratorExportsCountDistinctDeclaredQuerySQL(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{
		Name: "distinct_message_bodies",
		SQL:  "SELECT COUNT(DISTINCT body) AS n FROM messages",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `distinctMessageBodies: "distinct_message_bodies",`)
	assertContains(t, ts, `distinctMessageBodies: "SELECT COUNT(DISTINCT body) AS n FROM messages",`)
	assertContains(t, ts, `export function queryDistinctMessageBodies(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("distinct_message_bodies");`)
}

func TestTypeScriptGeneratorExportsAggregateOrderByDeclaredQuerySQL(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{
		Name: "message_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages ORDER BY n",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `messageCount: "message_count",`)
	assertContains(t, ts, `messageCount: "SELECT COUNT(*) AS n FROM messages ORDER BY n",`)
	assertContains(t, ts, `export function queryMessageCount(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("message_count");`)
}

func TestTypeScriptGeneratorExportsJoinWhereColumnComparisonDeclaredQuerySQL(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables, schema.TableExport{
		Name: "s",
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
			{Name: "u32", Type: "uint64"},
		},
	})
	contract.Queries = append(contract.Queries, shunter.QueryDescription{
		Name: "matching_messages",
		SQL:  "SELECT messages.id FROM messages JOIN s ON messages.id = s.u32 WHERE messages.id = s.id",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `matchingMessages: "matching_messages",`)
	assertContains(t, ts, `matchingMessages: "SELECT messages.id FROM messages JOIN s ON messages.id = s.u32 WHERE messages.id = s.id",`)
	assertContains(t, ts, `export function queryMatchingMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("matching_messages");`)
}

func TestTypeScriptGeneratorExportsJoinWhereColumnComparisonDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables, schema.TableExport{
		Name: "s",
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
			{Name: "u32", Type: "uint64"},
		},
		Indexes: []schema.IndexExport{{Name: "idx_s_u32", Columns: []string{"u32"}}},
	})
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_matching_messages",
		SQL:  "SELECT messages.* FROM messages JOIN s ON messages.id = s.u32 WHERE messages.id = s.id",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveMatchingMessages: "live_matching_messages",`)
	assertContains(t, ts, `liveMatchingMessages: "SELECT messages.* FROM messages JOIN s ON messages.id = s.u32 WHERE messages.id = s.id",`)
	assertContains(t, ts, `export function subscribeLiveMatchingMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_matching_messages");`)
}

func TestTypeScriptGeneratorExportsProjectedDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_message_bodies",
		SQL:  "SELECT body AS text FROM messages",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveMessageBodies: "live_message_bodies",`)
	assertContains(t, ts, `liveMessageBodies: "SELECT body AS text FROM messages",`)
	assertContains(t, ts, `export function subscribeLiveMessageBodies(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_message_bodies");`)
}

func TestTypeScriptGeneratorExportsOrderedDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_ordered_messages",
		SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveOrderedMessages: "live_ordered_messages",`)
	assertContains(t, ts, `liveOrderedMessages: "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC",`)
	assertContains(t, ts, `export function subscribeLiveOrderedMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_ordered_messages");`)
}

func TestTypeScriptGeneratorExportsLimitedDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_limited_messages",
		SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveLimitedMessages: "live_limited_messages",`)
	assertContains(t, ts, `liveLimitedMessages: "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2",`)
	assertContains(t, ts, `export function subscribeLiveLimitedMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_limited_messages");`)
}

func TestTypeScriptGeneratorExportsOffsetDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_offset_messages",
		SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2 OFFSET 1",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveOffsetMessages: "live_offset_messages",`)
	assertContains(t, ts, `liveOffsetMessages: "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2 OFFSET 1",`)
	assertContains(t, ts, `export function subscribeLiveOffsetMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_offset_messages");`)
}

func TestTypeScriptGeneratorExportsAggregateDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_message_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveMessageCount: "live_message_count",`)
	assertContains(t, ts, `liveMessageCount: "SELECT COUNT(*) AS n FROM messages",`)
	assertContains(t, ts, `export function subscribeLiveMessageCount(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_message_count");`)
}

func TestTypeScriptGeneratorExportsJoinAggregateDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_self_join_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages AS a JOIN messages AS b ON a.id = b.id",
	}, shunter.ViewDescription{
		Name: "live_self_join_distinct_bodies",
		SQL:  "SELECT COUNT(DISTINCT a.body) AS n FROM messages AS a JOIN messages AS b ON a.id = b.id",
	}, shunter.ViewDescription{
		Name: "live_self_join_total",
		SQL:  "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b ON a.id = b.id",
	}, shunter.ViewDescription{
		Name: "live_self_cross_join_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages AS a JOIN messages AS b",
	}, shunter.ViewDescription{
		Name: "live_self_cross_join_total",
		SQL:  "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b",
	}, shunter.ViewDescription{
		Name: "live_self_multi_join_total",
		SQL:  "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b ON a.id = b.id JOIN messages AS c ON b.id = c.id",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveSelfJoinCount: "live_self_join_count",`)
	assertContains(t, ts, `liveSelfJoinCount: "SELECT COUNT(*) AS n FROM messages AS a JOIN messages AS b ON a.id = b.id",`)
	assertContains(t, ts, `export function subscribeLiveSelfJoinCount(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_self_join_count");`)
	assertContains(t, ts, `liveSelfJoinDistinctBodies: "live_self_join_distinct_bodies",`)
	assertContains(t, ts, `liveSelfJoinDistinctBodies: "SELECT COUNT(DISTINCT a.body) AS n FROM messages AS a JOIN messages AS b ON a.id = b.id",`)
	assertContains(t, ts, `export function subscribeLiveSelfJoinDistinctBodies(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_self_join_distinct_bodies");`)
	assertContains(t, ts, `liveSelfJoinTotal: "live_self_join_total",`)
	assertContains(t, ts, `liveSelfJoinTotal: "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b ON a.id = b.id",`)
	assertContains(t, ts, `export function subscribeLiveSelfJoinTotal(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_self_join_total");`)
	assertContains(t, ts, `liveSelfCrossJoinCount: "live_self_cross_join_count",`)
	assertContains(t, ts, `liveSelfCrossJoinCount: "SELECT COUNT(*) AS n FROM messages AS a JOIN messages AS b",`)
	assertContains(t, ts, `export function subscribeLiveSelfCrossJoinCount(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_self_cross_join_count");`)
	assertContains(t, ts, `liveSelfCrossJoinTotal: "live_self_cross_join_total",`)
	assertContains(t, ts, `liveSelfCrossJoinTotal: "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b",`)
	assertContains(t, ts, `export function subscribeLiveSelfCrossJoinTotal(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_self_cross_join_total");`)
	assertContains(t, ts, `liveSelfMultiJoinTotal: "live_self_multi_join_total",`)
	assertContains(t, ts, `liveSelfMultiJoinTotal: "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b ON a.id = b.id JOIN messages AS c ON b.id = c.id",`)
	assertContains(t, ts, `export function subscribeLiveSelfMultiJoinTotal(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_self_multi_join_total");`)
}

func TestTypeScriptGeneratorExportsCountDistinctAggregateDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_distinct_message_bodies",
		SQL:  "SELECT COUNT(DISTINCT body) AS n FROM messages",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveDistinctMessageBodies: "live_distinct_message_bodies",`)
	assertContains(t, ts, `liveDistinctMessageBodies: "SELECT COUNT(DISTINCT body) AS n FROM messages",`)
	assertContains(t, ts, `export function subscribeLiveDistinctMessageBodies(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_distinct_message_bodies");`)
}

func TestTypeScriptGeneratorExportsSumAggregateDeclaredViewSQL(t *testing.T) {
	contract := contractFixture()
	contract.Views = append(contract.Views, shunter.ViewDescription{
		Name: "live_message_total",
		SQL:  "SELECT SUM(id) AS total FROM messages",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `liveMessageTotal: "live_message_total",`)
	assertContains(t, ts, `liveMessageTotal: "SELECT SUM(id) AS total FROM messages",`)
	assertContains(t, ts, `export function subscribeLiveMessageTotal(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_message_total");`)
}

func TestTypeScriptGeneratorExportsMultiJoinDeclaredQuerySQL(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables,
		schema.TableExport{
			Name: "s",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
				{Name: "u32", Type: "uint64"},
			},
		},
		schema.TableExport{
			Name: "r",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
				{Name: "u32", Type: "uint64"},
			},
		},
	)
	contract.Queries = append(contract.Queries, shunter.QueryDescription{
		Name: "multi_join_messages",
		SQL:  "SELECT r.id FROM messages JOIN s ON messages.id = s.u32 JOIN r ON s.u32 = r.u32",
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `multiJoinMessages: "multi_join_messages",`)
	assertContains(t, ts, `multiJoinMessages: "SELECT r.id FROM messages JOIN s ON messages.id = s.u32 JOIN r ON s.u32 = r.u32",`)
	assertContains(t, ts, `export function queryMultiJoinMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("multi_join_messages");`)
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

func TestGeneratorRejectsUnsupportedLanguageBeforeDecodingContractJSON(t *testing.T) {
	_, err := GenerateFromJSON([]byte(`{`), Options{Language: "go"})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want unsupported language error")
	}
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrUnsupportedLanguage", err)
	}
	if errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON decoded contract before rejecting language: %v", err)
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

func TestGeneratorRejectsCodegenMetadataMismatchWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Codegen.ContractFormat = "unexpected.format"
	contract.Codegen.ContractVersion = shunter.ModuleContractVersion + 1
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), `codegen.contract_format = "unexpected.format"`) {
		t.Fatalf("GenerateFromJSON error = %v, want codegen format context", err)
	}
	if !strings.Contains(err.Error(), "codegen.contract_version") {
		t.Fatalf("GenerateFromJSON error = %v, want codegen version context", err)
	}
}

func TestGeneratorRejectsContractWithInvalidDeclarationSQL(t *testing.T) {
	contract := contractFixture()
	contract.Queries[0].SQL = "SELECT * FROM missing"
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !strings.Contains(err.Error(), "queries.recent_messages.sql") {
		t.Fatalf("GenerateFromJSON error = %v, want declaration SQL context", err)
	}
}

func TestGeneratorRejectsUnknownPermissionTargetWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "missing_reducer",
		Required: []string{"messages:send"},
	}}
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), "permissions.reducer.missing_reducer references unknown reducer") {
		t.Fatalf("GenerateFromJSON error = %v, want permission reducer target context", err)
	}
}

func TestGeneratorRejectsInvalidTableReadPolicyWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPublic,
		Permissions: []string{"messages:read"},
	}
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), "schema.tables.messages.read_policy invalid") {
		t.Fatalf("GenerateFromJSON error = %v, want table read policy context", err)
	}
	if !strings.Contains(err.Error(), "public read policy must not include permissions") {
		t.Fatalf("GenerateFromJSON error = %v, want public read policy detail", err)
	}
}

func TestGeneratorRejectsInvalidSchemaColumnTypeWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns[1].Type = "notAType"
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), `schema.tables.messages.columns.body type "notAType" is invalid`) {
		t.Fatalf("GenerateFromJSON error = %v, want invalid schema column type context", err)
	}
}

func TestGeneratorRejectsUnknownReadModelTargetWithContext(t *testing.T) {
	contract := contractFixture()
	contract.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceQuery,
		Name:    "missing_query",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), "read_model.query.missing_query references unknown query") {
		t.Fatalf("GenerateFromJSON error = %v, want read model query target context", err)
	}
}

func TestGeneratorRejectsInvalidReadModelSurfaceWithContext(t *testing.T) {
	contract := contractFixture()
	contract.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: "subscription",
		Name:    "recent_messages",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), `read_model surface "subscription" is invalid`) {
		t.Fatalf("GenerateFromJSON error = %v, want invalid read model surface context", err)
	}
}

func TestGeneratorRejectsInvalidMigrationSurfaceWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: "subscription",
		Name:    "recent_messages",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
		},
	}}
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), `migrations surface "subscription" is invalid`) {
		t.Fatalf("GenerateFromJSON error = %v, want invalid migration surface context", err)
	}
}

func TestGeneratorRejectsUnknownMigrationTargetWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceTable,
		Name:    "missing_table",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
		},
	}}
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), "migrations.table.missing_table references unknown table") {
		t.Fatalf("GenerateFromJSON error = %v, want unknown migration target context", err)
	}
}

func TestTypeScriptGeneratorEscapesModuleMetadataInHeaderComment(t *testing.T) {
	contract := contractFixture()
	contract.Module.Name = "chat\nexport const injected = true;"
	contract.Module.Version = "v1\r\nmore"

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `// Module: chat\nexport const injected = true; v1\r\nmore`)
	assertNotContains(t, ts, "\nexport const injected = true;")
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

func TestTypeScriptGeneratorConcurrentDeterminismShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc0de6e17)
		workers    = 6
		iterations = 64
	)
	fixtures := []struct {
		name     string
		contract shunter.ModuleContract
	}{
		{name: "canonical", contract: contractFixture()},
		{name: "collision", contract: codegenIdentifierCollisionFixture()},
	}
	for i := range fixtures {
		fixtures[i].contract.Module.Metadata["soak"] = fixtures[i].name
	}

	expected := make([][]byte, len(fixtures))
	canonicalJSON := make([][]byte, len(fixtures))
	for i, fixture := range fixtures {
		out, err := Generate(fixture.contract, Options{Language: LanguageTypeScript})
		if err != nil {
			t.Fatalf("seed=%#x fixture=%s operation=GenerateExpected observed_error=%v expected=nil", seed, fixture.name, err)
		}
		expected[i] = out
		data, err := fixture.contract.MarshalCanonicalJSON()
		if err != nil {
			t.Fatalf("seed=%#x fixture=%s operation=MarshalCanonicalJSON observed_error=%v expected=nil", seed, fixture.name, err)
		}
		canonicalJSON[i] = data
	}

	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	failures := make(chan string, workers*iterations)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			for op := range iterations {
				fixtureIndex := (int(seed) + worker*11 + op*7) % len(fixtures)
				fixture := fixtures[fixtureIndex]
				var (
					out []byte
					err error
				)
				if (int(seed)+worker+op)%2 == 0 {
					out, err = Generate(fixture.contract, Options{Language: LanguageTypeScript})
				} else {
					out, err = GenerateFromJSON(canonicalJSON[fixtureIndex], Options{Language: LanguageTypeScript})
				}
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d fixture=%s operation=Generate observed_error=%v expected=nil",
						seed, worker, op, workers, iterations, fixture.name, err)
					continue
				}
				if !bytes.Equal(out, expected[fixtureIndex]) {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d fixture=%s operation=GenerateDeterminism observed_len=%d expected_len=%d",
						seed, worker, op, workers, iterations, fixture.name, len(out), len(expected[fixtureIndex]))
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}

	waitForCodegenSoakWorkers(t, ready, workers, "seed=0xc0de6e17 codegen workers started")
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestTypeScriptGeneratorAvoidsTableViewSubscribeHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables, schema.TableExport{
		Name: "live_messages",
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
		},
		Indexes: []schema.IndexExport{},
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export function subscribeLiveMessages(subscribeTable: TableSubscriber<LiveMessagesRow>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribeLiveMessages2(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
}

func TestTypeScriptGeneratorDisambiguatesReducerHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers, schema.ReducerExport{Name: "send-message"})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `sendMessage: "send_message",`)
	assertContains(t, ts, `sendMessage2: "send-message",`)
	assertContains(t, ts, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `return callReducer("send_message", args);`)
	assertContains(t, ts, `export function callSendMessage2(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `return callReducer("send-message", args);`)
}

func TestTypeScriptGeneratorDisambiguatesTopLevelHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers, schema.ReducerExport{Name: "SendMessage"})
	contract.Queries = append(contract.Queries,
		shunter.QueryDescription{Name: "RecentMessages", SQL: "SELECT * FROM messages"},
		shunter.QueryDescription{Name: "sQL", SQL: "SELECT * FROM messages"},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `return callReducer("send_message", args);`)
	assertContains(t, ts, `export function callSendMessage2(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `return callReducer("SendMessage", args);`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export function queryRecentMessages2(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("RecentMessages");`)
	assertContains(t, ts, `sQL: "sQL",`)
	assertContains(t, ts, `export const querySQL = {`)
	assertContains(t, ts, `export function querySQL2(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("sQL");`)
	assertNotContains(t, ts, `export function querySQL(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
}

func TestTypeScriptGeneratorDisambiguatesLifecycleReducerIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers,
		schema.ReducerExport{Name: "on-connect", Lifecycle: true},
		schema.ReducerExport{Name: "default", Lifecycle: true},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const lifecycleReducers = {`)
	assertContains(t, ts, `OnConnect: "OnConnect",`)
	assertContains(t, ts, `OnConnect2: "on-connect",`)
	assertContains(t, ts, `Default: "default",`)
}

func TestTypeScriptGeneratorDisambiguatesFallbackAndReservedTableIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables,
		schema.TableExport{
			Name:    "!!!",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
		schema.TableExport{
			Name:    "_",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
		schema.TableExport{
			Name:    "class",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
		schema.TableExport{
			Name:    "class!",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export interface _Row {`)
	assertContains(t, ts, `export interface _2Row {`)
	assertContains(t, ts, `_: "!!!",`)
	assertContains(t, ts, `_2: "_",`)
	assertContains(t, ts, `class_: "class",`)
	assertContains(t, ts, `class_2: "class!",`)
	assertContains(t, ts, `"!!!": _Row;`)
	assertContains(t, ts, `"_": _2Row;`)
	assertContains(t, ts, `"class": ClassRow;`)
	assertContains(t, ts, `"class!": Class2Row;`)
	assertContains(t, ts, `export function subscribe_(subscribeTable: TableSubscriber<_Row>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribe_2(subscribeTable: TableSubscriber<_2Row>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribeClass(subscribeTable: TableSubscriber<ClassRow>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribeClass2(subscribeTable: TableSubscriber<Class2Row>): Promise<() => void> {`)
}

func TestTypeScriptGeneratorDisambiguatesRowFieldIdentifierCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns = []schema.ColumnExport{
		{Name: "body_text", Type: "string"},
		{Name: "body-text", Type: "string"},
		{Name: "class", Type: "uint64"},
		{Name: "class!", Type: "uint64"},
	}
	contract.Schema.Tables[0].Indexes = nil

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export interface MessagesRow {`)
	assertContains(t, ts, `bodyText: string;`)
	assertContains(t, ts, `bodyText2: string;`)
	assertContains(t, ts, `class_: bigint;`)
	assertContains(t, ts, `class_2: bigint;`)
}

func TestTypeScriptGeneratorDeclaredHelpersUseNamedCallbacksNotSQL(t *testing.T) {
	out, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export type QueryRunner = ShunterQueryRunner<Uint8Array>;`)
	assertContains(t, ts, `export type ViewSubscriber = ShunterViewSubscriber;`)
	assertContains(t, ts, `export type DeclaredQueryRunner = ShunterDeclaredQueryRunner<ExecutableQueryName, Uint8Array>;`)
	assertContains(t, ts, `export type DeclaredViewSubscriber = ShunterDeclaredViewSubscriber<ExecutableViewName>;`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_messages");`)
	assertNotContains(t, ts, `return runDeclaredQuery("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return subscribeDeclaredView("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return runQuery("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return subscribeView("SELECT * FROM messages");`)
}

func TestTypeScriptGeneratorDisambiguatesDeclaredReadHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{Name: "recent-messages", SQL: "SELECT * FROM messages"})
	contract.Views = append(contract.Views, shunter.ViewDescription{Name: "live messages", SQL: "SELECT * FROM messages"})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `recentMessages2: "recent-messages",`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export function queryRecentMessages2(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent-messages");`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `liveMessages2: "live messages",`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_messages");`)
	assertContains(t, ts, `export function subscribeLiveMessages2(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live messages");`)
}

func TestTypeScriptGeneratorDisambiguatesDeclaredReadFallbackAndReservedIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries,
		shunter.QueryDescription{Name: "!!!", SQL: "SELECT * FROM messages"},
		shunter.QueryDescription{Name: "_", SQL: "SELECT * FROM messages"},
		shunter.QueryDescription{Name: "class", SQL: "SELECT * FROM messages"},
	)
	contract.Views = append(contract.Views,
		shunter.ViewDescription{Name: "???", SQL: "SELECT * FROM messages"},
		shunter.ViewDescription{Name: "default", SQL: "SELECT * FROM messages"},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `_: "!!!",`)
	assertContains(t, ts, `_2: "_",`)
	assertContains(t, ts, `class_: "class",`)
	assertContains(t, ts, `export function query_(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("!!!");`)
	assertContains(t, ts, `export function query_2(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("_");`)
	assertContains(t, ts, `export function queryClass_(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("class");`)
	assertContains(t, ts, `export function subscribe_(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("???");`)
	assertContains(t, ts, `export function subscribeDefault_(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("default");`)
}

func TestTypeScriptGeneratorDoesNotEmitExecutableHelpersForMetadataOnlyDeclarations(t *testing.T) {
	contract := contractFixture()
	contract.Queries[0].SQL = ""
	contract.Views[0].SQL = ""

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const queries = {`)
	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `export const querySQL = {`)
	assertNotContains(t, ts, `export function queryRecentMessages(`)
	assertContains(t, ts, `export const views = {`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `export const viewSQL = {`)
	assertNotContains(t, ts, `export function subscribeLiveMessages(`)
}

func TestTypeScriptGeneratorEmitsTableReadPolicyMetadata(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read"},
	}

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const tableReadPolicies = {`)
	assertContains(t, ts, `messages: { access: "permissioned", permissions: ["messages:read"] },`)
}

func TestTypeScriptGeneratorDisambiguatesTableReadPolicyMetadataIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables,
		schema.TableExport{
			Name: "audit_log",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
			Indexes: []schema.IndexExport{{Name: "audit_log_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
			ReadPolicy: schema.ReadPolicy{
				Access:      schema.TableAccessPermissioned,
				Permissions: []string{`audit:read"quoted`},
			},
		},
		schema.TableExport{
			Name: "audit-log",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
			Indexes: []schema.IndexExport{{Name: "audit_log_2_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
			ReadPolicy: schema.ReadPolicy{
				Access:      schema.TableAccessPermissioned,
				Permissions: []string{`audit\archive`},
			},
		},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const tableReadPolicies = {`)
	assertContains(t, ts, `auditLog: { access: "permissioned", permissions: ["audit:read\"quoted"] },`)
	assertContains(t, ts, `auditLog2: { access: "permissioned", permissions: ["audit\\archive"] },`)
}

func TestTypeScriptGeneratorEmitsVisibilityFilterMetadata(t *testing.T) {
	contract := contractFixture()
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:               "own_messages",
		SQL:                "SELECT * FROM messages WHERE body = :sender",
		ReturnTable:        "messages",
		ReturnTableID:      0,
		UsesCallerIdentity: true,
	}}

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const visibilityFilters = {`)
	assertContains(t, ts, `ownMessages: { sql: "SELECT * FROM messages WHERE body = :sender", returnTable: "messages", returnTableId: 0, usesCallerIdentity: true },`)
}

func TestTypeScriptGeneratorDisambiguatesVisibilityFilterMetadataIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{
		{
			Name:          "own_messages",
			SQL:           `SELECT * FROM messages WHERE body = 'said "hi"'`,
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
		{
			Name:          "own-messages",
			SQL:           `SELECT * FROM messages WHERE body = 'archived'`,
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
		{
			Name:          "class",
			SQL:           `SELECT * FROM messages WHERE body = 'reserved'`,
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
	}

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const visibilityFilters = {`)
	assertContains(t, ts, `ownMessages: { sql: "SELECT * FROM messages WHERE body = 'said \"hi\"'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`)
	assertContains(t, ts, `ownMessages2: { sql: "SELECT * FROM messages WHERE body = 'archived'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`)
	assertContains(t, ts, `class_: { sql: "SELECT * FROM messages WHERE body = 'reserved'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`)
}

func TestTypeScriptGeneratorDisambiguatesMetadataMapIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{Name: "recent-messages", SQL: "SELECT * FROM messages"})
	contract.Views = append(contract.Views, shunter.ViewDescription{Name: "live messages", SQL: "SELECT * FROM messages"})
	contract.Permissions.Queries = append(contract.Permissions.Queries, shunter.PermissionContractDeclaration{
		Name:     "recent-messages",
		Required: []string{`messages:read"quoted`},
	})
	contract.Permissions.Views = append(contract.Permissions.Views, shunter.PermissionContractDeclaration{
		Name:     "live messages",
		Required: []string{`messages:subscribe\slash`},
	})
	contract.ReadModel.Declarations = append(contract.ReadModel.Declarations,
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "recent-messages",
			Tables:  []string{"messages"},
			Tags:    []string{`audit"trail`},
		},
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceView,
			Name:    "live messages",
			Tables:  []string{"messages"},
			Tags:    []string{`live\feed`},
		},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `recentMessages2: "recent-messages",`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `liveMessages2: "live messages",`)
	assertContains(t, ts, `recentMessages2: { required: ["messages:read\"quoted"] },`)
	assertContains(t, ts, `liveMessages2: { required: ["messages:subscribe\\slash"] },`)
	assertContains(t, ts, `recentMessages2: { tables: ["messages"], tags: ["audit\"trail"] },`)
	assertContains(t, ts, `liveMessages2: { tables: ["messages"], tags: ["live\\feed"] },`)
}

func TestTypeScriptGeneratorDisambiguatesReducerPermissionMetadataIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers, schema.ReducerExport{Name: "send-message"})
	contract.Permissions.Reducers = append(contract.Permissions.Reducers, shunter.PermissionContractDeclaration{
		Name:     "send-message",
		Required: []string{`messages:send-alt`},
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `reducers: {`)
	assertContains(t, ts, `sendMessage: { required: ["messages:send"] },`)
	assertContains(t, ts, `sendMessage2: { required: ["messages:send-alt"] },`)
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
			{Name: "recent_messages", SQL: "SELECT * FROM messages"},
		},
		Views: []shunter.ViewDescription{
			{Name: "live_messages", SQL: "SELECT * FROM messages"},
		},
		Permissions: shunter.PermissionContract{
			Reducers: []shunter.PermissionContractDeclaration{
				{Name: "send_message", Required: []string{"messages:send"}},
			},
			Queries: []shunter.PermissionContractDeclaration{
				{Name: "recent_messages", Required: []string{"messages:read"}},
			},
			Views: []shunter.PermissionContractDeclaration{
				{Name: "live_messages", Required: []string{"messages:subscribe"}},
			},
		},
		ReadModel: shunter.ReadModelContract{
			Declarations: []shunter.ReadModelContractDeclaration{
				{
					Surface: shunter.ReadModelSurfaceQuery,
					Name:    "recent_messages",
					Tables:  []string{"messages"},
					Tags:    []string{"history"},
				},
				{
					Surface: shunter.ReadModelSurfaceView,
					Name:    "live_messages",
					Tables:  []string{"messages"},
					Tags:    []string{"realtime"},
				},
			},
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

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("generated TypeScript unexpectedly contains %q:\n%s", needle, haystack)
	}
}

func waitForCodegenSoakWorkers(t *testing.T, ch <-chan struct{}, want int, label string) {
	t.Helper()
	for i := 0; i < want; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: observed %d/%d workers", label, i, want)
		}
	}
}
