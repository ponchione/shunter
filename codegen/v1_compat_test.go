package codegen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
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
		`import type {`,
		`DecodedDeclaredQueryResult as ShunterDecodedDeclaredQueryResult,`,
		`DeclaredQueryDecodeOptions as ShunterDeclaredQueryDecodeOptions,`,
		`DeclaredViewSubscriber as ShunterDeclaredViewSubscriber,`,
		`ProtocolMetadata as ShunterProtocolMetadata,`,
		`RawDeclaredQueryResult as ShunterRawDeclaredQueryResult,`,
		`ReducerCallResult as ShunterReducerCallResult,`,
		`ReducerCallResultRequestOptions as ShunterReducerCallResultRequestOptions,`,
		`SubscriptionUnsubscribe as ShunterSubscriptionUnsubscribe,`,
		`TableRowDecoder as ShunterTableRowDecoder,`,
		`TableRowDecoders as ShunterTableRowDecoders,`,
		`callReducerWithResult as shunterCallReducerWithResult,`,
		`decodeDeclaredQueryResult as shunterDecodeDeclaredQueryResult,`,
		`export const shunterProtocol = {`,
		`defaultSubprotocol: "v1.bsatn.shunter",`,
		`} as const satisfies ShunterProtocolMetadata;`,
		`export interface MessagesRow {`,
		`id: bigint;`,
		`sender: string;`,
		`topic: string | null;`,
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
		`export type ReducerCallResult<Name extends ReducerName = ReducerName> = ShunterReducerCallResult<Name, Uint8Array>;`,
		`export type ReducerCallResultOptions = ShunterReducerCallResultRequestOptions<Uint8Array>;`,
		`export function callCreateMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`,
		`export function callCreateMessageResult(callReducer: ReducerCaller, args: Uint8Array, options: ReducerCallResultOptions = {}): Promise<ReducerCallResult<typeof reducers.createMessage>> {`,
		`return shunterCallReducerWithResult(callReducer, "create_message", args, options);`,
		`export const lifecycleReducers = {`,
		`OnConnect: "OnConnect",`,
		`export const queries = {`,
		`recentMessages: "recent_messages",`,
		`export const querySQL = {`,
		`recentMessages: "SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25",`,
		`export type RawDeclaredQueryResult<Name extends ExecutableQueryName = ExecutableQueryName> = ShunterRawDeclaredQueryResult<Name>;`,
		`export type DecodedDeclaredQueryResult<Name extends ExecutableQueryName = ExecutableQueryName, RowsByName extends object = TableRows> = ShunterDecodedDeclaredQueryResult<Name, RowsByName>;`,
		`export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`,
		`export function queryRecentMessagesResult<RowsByName extends object = TableRows>(data: unknown, options: DeclaredQueryDecodeOptions<RowsByName>): DecodedDeclaredQueryResult<typeof queries.recentMessages, RowsByName> {`,
		`export const views = {`,
		`liveMessageProjection: "live_message_projection",`,
		`liveMessageCount: "live_message_count",`,
		`export const viewSQL = {`,
		`liveMessageProjection: "SELECT id, body AS text FROM messages",`,
		`liveMessageCount: "SELECT COUNT(*) AS n FROM messages",`,
		`export function subscribeLiveMessageProjection(subscribeDeclaredView: DeclaredViewSubscriber): Promise<SubscriptionUnsubscribe> {`,
		`export function subscribeLiveMessageCount(subscribeDeclaredView: DeclaredViewSubscriber): Promise<SubscriptionUnsubscribe> {`,
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

func TestV1CompatibilityTypeScriptDeclaredReadResultShapeSurface(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.ts"))
	if err != nil {
		t.Fatalf("read v1 TypeScript fixture: %v", err)
	}
	ts := string(data)

	for _, want := range []string{
		`export type DeclaredQueryRunner = ShunterDeclaredQueryRunner<ExecutableQueryName, Uint8Array>;`,
		`export type RawDeclaredQueryResult<Name extends ExecutableQueryName = ExecutableQueryName> = ShunterRawDeclaredQueryResult<Name>;`,
		`export type DeclaredQueryDecodeOptions<RowsByName extends object = TableRows> = ShunterDeclaredQueryDecodeOptions<RowsByName>;`,
		`export type DecodedDeclaredQueryResult<Name extends ExecutableQueryName = ExecutableQueryName, RowsByName extends object = TableRows> = ShunterDecodedDeclaredQueryResult<Name, RowsByName>;`,
		`recentMessages: "SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25",`,
		`export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`,
		`return runDeclaredQuery("recent_messages");`,
		`export function queryRecentMessagesResult<RowsByName extends object = TableRows>(data: unknown, options: DeclaredQueryDecodeOptions<RowsByName>): DecodedDeclaredQueryResult<typeof queries.recentMessages, RowsByName> {`,
		`return shunterDecodeDeclaredQueryResult("recent_messages", data, options);`,
		`export type DeclaredViewSubscriber = ShunterDeclaredViewSubscriber<ExecutableViewName>;`,
		`export type SubscriptionUnsubscribe = ShunterSubscriptionUnsubscribe;`,
		`liveMessageProjection: "SELECT id, body AS text FROM messages",`,
		`liveMessageCount: "SELECT COUNT(*) AS n FROM messages",`,
		`export function subscribeLiveMessageProjection(subscribeDeclaredView: DeclaredViewSubscriber): Promise<SubscriptionUnsubscribe> {`,
		`return subscribeDeclaredView("live_message_projection");`,
		`export function subscribeLiveMessageCount(subscribeDeclaredView: DeclaredViewSubscriber): Promise<SubscriptionUnsubscribe> {`,
		`return subscribeDeclaredView("live_message_count");`,
		`recentMessages: { tables: ["messages"], tags: ["history", "v1"] },`,
		`liveMessageProjection: { tables: ["messages"], tags: ["projection", "v1"] },`,
		`liveMessageCount: { tables: ["messages"], tags: ["aggregate", "v1"] },`,
	} {
		assertContains(t, ts, want)
	}
	for _, unexpected := range []string{
		`export interface RecentMessagesRow`,
		`export interface LiveMessageProjectionRow`,
		`export interface LiveMessageCountRow`,
	} {
		assertNotContains(t, ts, unexpected)
	}
}

func TestV1CompatibilityTypeScriptIdentifierNormalizationAndCollisions(t *testing.T) {
	out, err := Generate(codegenIdentifierCollisionFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)
	for _, want := range []string{
		`export interface LiveMessagesRow {`,
		`bodyText: string;`,
		`class_: string;`,
		`_1Count: bigint;`,
		`sendMessage2: "send-message",`,
		`Default: "default",`,
		`recentMessages2: "recent-messages",`,
		`liveMessages2: "live messages",`,
	} {
		assertContains(t, ts, want)
	}
}

func TestV1CompatibilityTypeScriptIdentifierNormalizationGolden(t *testing.T) {
	got, err := Generate(v1IdentifierNormalizationContract(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	assertCodegenGoldenBytes(t, filepath.Join("testdata", "v1_identifier_normalization.ts"), got)
}

func TestV1CompatibilityTypeScriptIdentifierNormalizationGoldenCoversRules(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v1_identifier_normalization.ts"))
	if err != nil {
		t.Fatalf("read v1 identifier TypeScript fixture: %v", err)
	}
	ts := string(data)

	for _, want := range []string{
		`export interface _Row {`,
		`export interface _1TableRow {`,
		`export interface ClassRow {`,
		`export interface Class2Row {`,
		`bodyText: string;`,
		`bodyText2: string;`,
		`class_: bigint;`,
		`_1Count: bigint;`,
		`_: "!!!",`,
		`_1Table: "1-table",`,
		`class_: "class",`,
		`class_2: "class!",`,
		`sendMessage: "send_message",`,
		`sendMessage2: "send-message",`,
		`default_: "default",`,
		`OnConnect: "OnConnect",`,
		`OnConnect2: "on-connect",`,
		`Default: "default!",`,
		`recentMessages: "recent_messages",`,
		`recentMessages2: "recent-messages",`,
		`class_: "class",`,
		`sQL: "sQL",`,
		`export function querySQL2(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`,
		`liveMessages: "live_messages",`,
		`liveMessages2: "live messages",`,
		`default_: "default",`,
		`ownMessages: { sql: "SELECT * FROM messages WHERE body = 'own'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`,
		`ownMessages2: { sql: "SELECT * FROM messages WHERE body = 'archived'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`,
		`class_: { sql: "SELECT * FROM messages WHERE body = 'reserved'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`,
		`sendMessage2: { required: ["messages:send-alt"] },`,
		`recentMessages2: { required: ["messages:read-alt"] },`,
		`liveMessages2: { required: ["messages:subscribe-alt"] },`,
		`recentMessages2: { tables: ["messages"], tags: ["history-alt"] },`,
		`liveMessages2: { tables: ["messages"], tags: ["realtime-alt"] },`,
	} {
		assertContains(t, ts, want)
	}
}

func TestV1CompatibilityTypeScriptCoversCurrentValueKindMappings(t *testing.T) {
	columns := []struct {
		name  string
		field string
		kind  schema.ValueKind
		want  string
	}{
		{name: "bool_value", field: "boolValue", kind: schema.KindBool, want: "boolean"},
		{name: "int8_value", field: "int8Value", kind: schema.KindInt8, want: "number"},
		{name: "uint8_value", field: "uint8Value", kind: schema.KindUint8, want: "number"},
		{name: "int16_value", field: "int16Value", kind: schema.KindInt16, want: "number"},
		{name: "uint16_value", field: "uint16Value", kind: schema.KindUint16, want: "number"},
		{name: "int32_value", field: "int32Value", kind: schema.KindInt32, want: "number"},
		{name: "uint32_value", field: "uint32Value", kind: schema.KindUint32, want: "number"},
		{name: "int64_value", field: "int64Value", kind: schema.KindInt64, want: "bigint"},
		{name: "uint64_value", field: "uint64Value", kind: schema.KindUint64, want: "bigint"},
		{name: "float32_value", field: "float32Value", kind: schema.KindFloat32, want: "number"},
		{name: "float64_value", field: "float64Value", kind: schema.KindFloat64, want: "number"},
		{name: "string_value", field: "stringValue", kind: schema.KindString, want: "string"},
		{name: "bytes_value", field: "bytesValue", kind: schema.KindBytes, want: "Uint8Array"},
		{name: "int128_value", field: "int128Value", kind: schema.KindInt128, want: "bigint"},
		{name: "uint128_value", field: "uint128Value", kind: schema.KindUint128, want: "bigint"},
		{name: "int256_value", field: "int256Value", kind: schema.KindInt256, want: "bigint"},
		{name: "uint256_value", field: "uint256Value", kind: schema.KindUint256, want: "bigint"},
		{name: "timestamp_value", field: "timestampValue", kind: schema.KindTimestamp, want: "bigint"},
		{name: "array_string_value", field: "arrayStringValue", kind: schema.KindArrayString, want: "string[]"},
		{name: "uuid_value", field: "uuidValue", kind: schema.KindUUID, want: "UUID"},
		{name: "duration_value", field: "durationValue", kind: schema.KindDuration, want: "bigint"},
		{name: "json_value", field: "jsonValue", kind: schema.KindJSON, want: "unknown"},
	}

	tableColumns := make([]schema.ColumnExport, 0, len(columns))
	for _, column := range columns {
		exportName := schema.ValueKindExportString(column.kind)
		if exportName == "" {
			t.Fatalf("ValueKindExportString(%d) returned an empty export name", column.kind)
		}
		tableColumns = append(tableColumns, schema.ColumnExport{Name: column.name, Type: exportName})
	}

	contract := contractFixture()
	contract.Schema.Tables = []schema.TableExport{
		{
			Name:    "all_values",
			Columns: tableColumns,
			Indexes: []schema.IndexExport{
				{Name: "all_values_pk", Columns: []string{"uint64_value"}, Unique: true, Primary: true},
			},
		},
	}
	contract.Schema.Reducers = []schema.ReducerExport{}
	contract.Queries = []shunter.QueryDescription{}
	contract.Views = []shunter.ViewDescription{}
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{}
	contract.Permissions = shunter.PermissionContract{
		Reducers: []shunter.PermissionContractDeclaration{},
		Queries:  []shunter.PermissionContractDeclaration{},
		Views:    []shunter.PermissionContractDeclaration{},
	}
	contract.ReadModel = shunter.ReadModelContract{
		Declarations: []shunter.ReadModelContractDeclaration{},
	}
	contract.Migrations = shunter.MigrationContract{
		Declarations: []shunter.MigrationContractDeclaration{},
	}

	got, err := GenerateTypeScript(contract)
	if err != nil {
		t.Fatalf("GenerateTypeScript returned error: %v", err)
	}
	ts := string(got)

	assertContains(t, ts, `export interface AllValuesRow {`)
	for _, column := range columns {
		assertContains(t, ts, `  `+column.field+`: `+column.want+`;`)
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

func v1IdentifierNormalizationContract() shunter.ModuleContract {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables,
		schema.TableExport{
			Name: "!!!",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
		},
		schema.TableExport{
			Name: "1-table",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
		},
		schema.TableExport{
			Name: "class",
			Columns: []schema.ColumnExport{
				{Name: "body_text", Type: "string"},
				{Name: "body-text", Type: "string"},
				{Name: "class", Type: "uint64"},
				{Name: "1-count", Type: "uint64"},
			},
		},
		schema.TableExport{
			Name: "class!",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
		},
	)
	contract.Schema.Reducers = append(contract.Schema.Reducers,
		schema.ReducerExport{Name: "send-message"},
		schema.ReducerExport{Name: "default"},
		schema.ReducerExport{Name: "on-connect", Lifecycle: true},
		schema.ReducerExport{Name: "default!", Lifecycle: true},
	)
	contract.Queries = append(contract.Queries,
		shunter.QueryDescription{Name: "recent-messages", SQL: "SELECT * FROM messages"},
		shunter.QueryDescription{Name: "class", SQL: "SELECT * FROM messages"},
		shunter.QueryDescription{Name: "sQL", SQL: "SELECT * FROM messages"},
	)
	contract.Views = append(contract.Views,
		shunter.ViewDescription{Name: "live messages", SQL: "SELECT * FROM messages"},
		shunter.ViewDescription{Name: "default", SQL: "SELECT * FROM messages"},
	)
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{
		{
			Name:          "own_messages",
			SQL:           "SELECT * FROM messages WHERE body = 'own'",
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
		{
			Name:          "own-messages",
			SQL:           "SELECT * FROM messages WHERE body = 'archived'",
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
		{
			Name:          "class",
			SQL:           "SELECT * FROM messages WHERE body = 'reserved'",
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
	}
	contract.Permissions.Reducers = append(contract.Permissions.Reducers,
		shunter.PermissionContractDeclaration{Name: "send-message", Required: []string{"messages:send-alt"}},
		shunter.PermissionContractDeclaration{Name: "default", Required: []string{"messages:send-default"}},
	)
	contract.Permissions.Queries = append(contract.Permissions.Queries,
		shunter.PermissionContractDeclaration{Name: "recent-messages", Required: []string{"messages:read-alt"}},
		shunter.PermissionContractDeclaration{Name: "class", Required: []string{"messages:read-class"}},
		shunter.PermissionContractDeclaration{Name: "sQL", Required: []string{"messages:read-sql"}},
	)
	contract.Permissions.Views = append(contract.Permissions.Views,
		shunter.PermissionContractDeclaration{Name: "live messages", Required: []string{"messages:subscribe-alt"}},
		shunter.PermissionContractDeclaration{Name: "default", Required: []string{"messages:subscribe-default"}},
	)
	contract.ReadModel.Declarations = append(contract.ReadModel.Declarations,
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "recent-messages",
			Tables:  []string{"messages"},
			Tags:    []string{"history-alt"},
		},
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "class",
			Tables:  []string{"messages"},
			Tags:    []string{"history-class"},
		},
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "sQL",
			Tables:  []string{"messages"},
			Tags:    []string{"history-sql"},
		},
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceView,
			Name:    "live messages",
			Tables:  []string{"messages"},
			Tags:    []string{"realtime-alt"},
		},
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceView,
			Name:    "default",
			Tables:  []string{"messages"},
			Tags:    []string{"realtime-default"},
		},
	)
	return contract
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
			old: "      {\n        \"name\": \"messages\",\n        \"columns\": [",
			new: "      {\n        \"future_table_field\": \"ignored\",\n        \"name\": \"messages\",\n        \"columns\": [",
		},
		{
			old: "          {\n            \"name\": \"id\",\n            \"type\": \"uint64\"\n          }",
			new: "          {\n            \"future_column_field\": \"ignored\",\n            \"name\": \"id\",\n            \"type\": \"uint64\"\n          }",
		},
		{
			old: "          {\n            \"name\": \"pk\",\n            \"columns\": [",
			new: "          {\n            \"future_index_field\": \"ignored\",\n            \"name\": \"pk\",\n            \"columns\": [",
		},
		{
			old: "        \"read_policy\": {\n          \"access\": \"permissioned\",",
			new: "        \"read_policy\": {\n          \"future_read_policy_field\": \"ignored\",\n          \"access\": \"permissioned\",",
		},
		{
			old: "      {\n        \"name\": \"create_message\",\n        \"lifecycle\": false\n      }",
			new: "      {\n        \"future_reducer_field\": \"ignored\",\n        \"name\": \"create_message\",\n        \"lifecycle\": false\n      }",
		},
		{
			old: "    {\n      \"name\": \"recent_messages\",\n      \"sql\": \"SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25\"\n    }",
			new: "    {\n      \"future_query_field\": \"ignored\",\n      \"name\": \"recent_messages\",\n      \"sql\": \"SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25\"\n    }",
		},
		{
			old: "    {\n      \"name\": \"live_message_projection\",\n      \"sql\": \"SELECT id, body AS text FROM messages\"\n    }",
			new: "    {\n      \"future_view_field\": \"ignored\",\n      \"name\": \"live_message_projection\",\n      \"sql\": \"SELECT id, body AS text FROM messages\"\n    }",
		},
		{
			old: "    {\n      \"name\": \"own_messages\",\n      \"sql\": \"SELECT * FROM messages WHERE sender = :sender\",",
			new: "    {\n      \"future_visibility_filter_field\": \"ignored\",\n      \"name\": \"own_messages\",\n      \"sql\": \"SELECT * FROM messages WHERE sender = :sender\",",
		},
		{
			old: "  \"permissions\": {\n    \"reducers\": [",
			new: "  \"permissions\": {\n    \"future_permissions_field\": \"ignored\",\n    \"reducers\": [",
		},
		{
			old: "      {\n        \"name\": \"create_message\",\n        \"required\": [",
			new: "      {\n        \"future_permission_declaration_field\": \"ignored\",\n        \"name\": \"create_message\",\n        \"required\": [",
		},
		{
			old: "  \"read_model\": {\n    \"declarations\": [",
			new: "  \"read_model\": {\n    \"future_read_model_field\": \"ignored\",\n    \"declarations\": [",
		},
		{
			old: "      {\n        \"surface\": \"query\",\n        \"name\": \"recent_messages\",",
			new: "      {\n        \"future_read_model_declaration_field\": \"ignored\",\n        \"surface\": \"query\",\n        \"name\": \"recent_messages\",",
		},
		{
			old: "  \"migrations\": {\n    \"module\": {",
			new: "  \"migrations\": {\n    \"future_migrations_field\": \"ignored\",\n    \"module\": {",
		},
		{
			old: "    \"module\": {\n      \"module_version\": \"v1.0.0\",",
			new: "    \"module\": {\n      \"future_module_migration_field\": \"ignored\",\n      \"module_version\": \"v1.0.0\",",
		},
		{
			old: "      {\n        \"surface\": \"table\",\n        \"name\": \"messages\",",
			new: "      {\n        \"future_migration_declaration_field\": \"ignored\",\n        \"surface\": \"table\",\n        \"name\": \"messages\",",
		},
		{
			old: "        \"metadata\": {\n          \"module_version\": \"\",",
			new: "        \"metadata\": {\n          \"future_migration_metadata_field\": \"ignored\",\n          \"module_version\": \"\",",
		},
		{
			old: "  \"codegen\": {\n    \"contract_format\": \"shunter.module_contract\",",
			new: "  \"codegen\": {\n    \"future_codegen_field\": \"ignored\",\n    \"contract_format\": \"shunter.module_contract\",",
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
