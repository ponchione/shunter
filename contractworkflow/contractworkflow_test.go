package contractworkflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/codegen"
	"github.com/ponchione/shunter/contractdiff"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
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

func TestCheckPolicyFilesUsesV1ReducerPermissionPolicy(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeContractFixture(t, dir, "previous.json", workflowContractFixture())
	current := workflowContractFixture()
	current.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "send_message",
		Required: []string{"messages:send"},
	}}
	currentPath := writeContractFixture(t, dir, "current.json", current)

	result, err := CheckPolicyFiles(previousPath, currentPath, contractdiff.PolicyOptions{Strict: true})
	if err != nil {
		t.Fatalf("CheckPolicyFiles returned error: %v", err)
	}
	if !result.Failed {
		t.Fatal("strict reducer permission policy result did not fail on missing migration metadata")
	}
	got, err := FormatPolicy(result, FormatText)
	if err != nil {
		t.Fatalf("FormatPolicy returned error: %v", err)
	}
	assertContains(t, string(got), "missing-migration-metadata permission reducer.send_message: breaking change has no migration metadata")

	current.Migrations.Module = shunter.MigrationMetadata{
		Compatibility: shunter.MigrationCompatibilityBreaking,
		Notes:         "tighten reducer permission",
	}
	currentPath = writeContractFixture(t, dir, "current-with-metadata.json", current)

	result, err = CheckPolicyFiles(previousPath, currentPath, contractdiff.PolicyOptions{Strict: true})
	if err != nil {
		t.Fatalf("CheckPolicyFiles with metadata returned error: %v", err)
	}
	if result.Failed {
		t.Fatalf("strict reducer permission policy failed despite module migration metadata: %#v", result.Warnings)
	}
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

func TestPlanFilesIncludesBackupGuidanceForBlockingPlan(t *testing.T) {
	dir := t.TempDir()
	previousPath := writeContractFixture(t, dir, "previous.json", workflowContractFixture())
	current := workflowContractFixture()
	current.Schema.Tables[0].Columns[0].Type = "string"
	currentPath := writeContractFixture(t, dir, "current.json", current)

	plan, err := PlanFiles(previousPath, currentPath, contractdiff.PlanOptions{})
	if err != nil {
		t.Fatalf("PlanFiles returned error: %v", err)
	}

	got, err := FormatPlan(plan, FormatText)
	if err != nil {
		t.Fatalf("FormatPlan returned error: %v", err)
	}
	assertContains(t, string(got), "guidance backup-restore:")
	assertContains(t, string(got), "shunter.BackupDataDir")

	jsonOut, err := FormatPlan(plan, FormatJSON)
	if err != nil {
		t.Fatalf("FormatPlan JSON returned error: %v", err)
	}
	assertContains(t, string(jsonOut), `"backup_recommended": true`)
	assertContains(t, string(jsonOut), `"guidance": [`)
	assertContains(t, string(jsonOut), `"code": "backup-restore"`)
}

func TestLoadContractFileReadsValidatedLocalContract(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())

	contract, err := LoadContractFile(contractPath, "operator contract")
	if err != nil {
		t.Fatalf("LoadContractFile returned error: %v", err)
	}
	if contract.Module.Name != "chat" || len(contract.Schema.Reducers) != 1 {
		t.Fatalf("LoadContractFile contract = %+v", contract)
	}
}

func TestLoadContractFileRejectsMissingMalformedAndInvalidContracts(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	malformedPath := filepath.Join(dir, "malformed.json")
	if err := writeFile(malformedPath, []byte(`{`)); err != nil {
		t.Fatalf("write malformed contract: %v", err)
	}
	invalidPath := filepath.Join(dir, "invalid.json")
	if err := writeFile(invalidPath, []byte(`{"contract_version":0}`)); err != nil {
		t.Fatalf("write invalid contract: %v", err)
	}

	for _, tc := range []struct {
		name        string
		path        string
		wantContext string
	}{
		{
			name:        "missing",
			path:        missingPath,
			wantContext: "read contract",
		},
		{
			name:        "malformed",
			path:        malformedPath,
			wantContext: "invalid module contract JSON: release gate",
		},
		{
			name:        "invalid",
			path:        invalidPath,
			wantContext: "invalid module contract JSON: release gate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadContractFile(tc.path, "release gate")
			if err == nil {
				t.Fatal("LoadContractFile returned nil error")
			}
			if !strings.Contains(err.Error(), tc.wantContext) {
				t.Fatalf("LoadContractFile error = %v, want context %q", err, tc.wantContext)
			}
		})
	}
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

func TestFindReducerUsesLocalContractDeclarations(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers, schema.ReducerExport{
		Name:      "archive_message",
		Lifecycle: true,
	})

	reducer, ok := FindReducer(contract, " archive_message ")
	if !ok {
		t.Fatal("FindReducer did not find reducer with trimmed name")
	}
	if reducer.Name != "archive_message" || !reducer.Lifecycle {
		t.Fatalf("FindReducer reducer = %+v", reducer)
	}

	if _, ok := FindReducer(contract, "missing_reducer"); ok {
		t.Fatal("FindReducer found missing reducer")
	}
	if _, ok := FindReducer(contract, " \t"); ok {
		t.Fatal("FindReducer found empty reducer name")
	}
}

func TestFindQueryUsesLocalContractDeclarations(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{
		Name: "recent_messages",
		SQL:  "SELECT * FROM messages",
	})

	query, ok := FindQuery(contract, " recent_messages ")
	if !ok {
		t.Fatal("FindQuery did not find query with trimmed name")
	}
	if query.Name != "recent_messages" || query.SQL != "SELECT * FROM messages" {
		t.Fatalf("FindQuery query = %+v", query)
	}

	if _, ok := FindQuery(contract, "missing_query"); ok {
		t.Fatal("FindQuery found missing query")
	}
	if _, ok := FindQuery(contract, " \t"); ok {
		t.Fatal("FindQuery found empty query name")
	}
}

func TestReducerArgumentSchemaSelectsContractSchema(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{
		{
			Name: "send_message",
			Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "body", Type: "string"},
			}},
		},
		{
			Name: "ping",
			Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{}},
		},
	}

	args, err := ReducerArgumentSchema(contract, " send_message ")
	if err != nil {
		t.Fatalf("ReducerArgumentSchema returned error: %v", err)
	}
	if len(args.Columns) != 1 || args.Columns[0].Name != "body" || args.Columns[0].Type != "string" {
		t.Fatalf("ReducerArgumentSchema args = %+v", args)
	}

	emptyArgs, err := ReducerArgumentSchema(contract, "ping")
	if err != nil {
		t.Fatalf("ReducerArgumentSchema empty schema returned error: %v", err)
	}
	if emptyArgs.Columns == nil || len(emptyArgs.Columns) != 0 {
		t.Fatalf("ReducerArgumentSchema empty args = %+v, want present empty columns", emptyArgs)
	}
}

func TestReducerArgumentSchemaRejectsUnknownAndSchemaLessReducers(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{Name: "send_message"}}

	_, err := ReducerArgumentSchema(contract, "missing")
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("ReducerArgumentSchema missing error = %v, want ErrSurfaceNotFound", err)
	}

	_, err = ReducerArgumentSchema(contract, "send_message")
	if !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("ReducerArgumentSchema schema-less error = %v, want ErrArgumentSchemaMissing", err)
	}
}

func TestQueryArgumentSchemaSelectsContractParameters(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{
		{
			Name: "recent_messages",
			Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
				{Name: "topic", Type: "string"},
			}},
		},
		{
			Name:       "all_messages",
			Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{}},
		},
	}

	args, err := QueryArgumentSchema(contract, " recent_messages ")
	if err != nil {
		t.Fatalf("QueryArgumentSchema returned error: %v", err)
	}
	if len(args.Columns) != 1 || args.Columns[0].Name != "topic" || args.Columns[0].Type != "string" {
		t.Fatalf("QueryArgumentSchema args = %+v", args)
	}

	emptyArgs, err := QueryArgumentSchema(contract, "all_messages")
	if err != nil {
		t.Fatalf("QueryArgumentSchema empty schema returned error: %v", err)
	}
	if emptyArgs.Columns == nil || len(emptyArgs.Columns) != 0 {
		t.Fatalf("QueryArgumentSchema empty args = %+v, want present empty columns", emptyArgs)
	}
}

func TestQueryArgumentSchemaRejectsUnknownAndSchemaLessQueries(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{Name: "history"}}

	_, err := QueryArgumentSchema(contract, "missing")
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("QueryArgumentSchema missing error = %v, want ErrSurfaceNotFound", err)
	}

	_, err = QueryArgumentSchema(contract, "history")
	if !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("QueryArgumentSchema schema-less error = %v, want ErrArgumentSchemaMissing", err)
	}
}

func TestQueryRowSchemaSelectsContractResultRows(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		}},
	}}

	rows, err := QueryRowSchema(contract, " recent_messages ")
	if err != nil {
		t.Fatalf("QueryRowSchema returned error: %v", err)
	}
	if len(rows.Columns) != 2 || rows.Columns[0].Name != "id" || rows.Columns[1].Name != "body" {
		t.Fatalf("QueryRowSchema rows = %+v", rows)
	}
}

func TestQueryRowSchemaRejectsUnknownAndSchemaLessQueries(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{Name: "history"}}

	_, err := QueryRowSchema(contract, "missing")
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("QueryRowSchema missing error = %v, want ErrSurfaceNotFound", err)
	}

	_, err = QueryRowSchema(contract, "history")
	if !errors.Is(err, ErrResultSchemaMissing) {
		t.Fatalf("QueryRowSchema schema-less error = %v, want ErrResultSchemaMissing", err)
	}
}

func TestProductValueFromJSONDecodesSchemaOrderedValues(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "id", Type: "uint64"},
		{Name: "body", Type: "string"},
		{Name: "published", Type: "bool"},
		{Name: "score", Type: "float64"},
		{Name: "tags", Type: "arrayString"},
		{Name: "payload", Type: "json"},
		{Name: "blob", Type: "bytes"},
		{Name: "maybe", Type: "string", Nullable: true},
		{Name: "created_at", Type: "timestamp"},
		{Name: "request_id", Type: "uuid"},
	}}

	row, err := ProductValueFromJSON(product, []byte(`{
		"request_id": "123e4567-e89b-12d3-a456-426614174000",
		"created_at": 42,
		"maybe": null,
		"blob": "aGk=",
		"payload": {"z": 1, "a": true},
		"tags": ["go", "ts"],
		"score": 9.5,
		"published": true,
		"body": "hello",
		"id": 7
	}`))
	if err != nil {
		t.Fatalf("ProductValueFromJSON returned error: %v", err)
	}
	if len(row) != len(product.Columns) {
		t.Fatalf("ProductValueFromJSON row length = %d, want %d", len(row), len(product.Columns))
	}
	if row[0].Kind() != types.KindUint64 || row[0].AsUint64() != 7 {
		t.Fatalf("id value = %+v", row[0])
	}
	if row[1].Kind() != types.KindString || row[1].AsString() != "hello" {
		t.Fatalf("body value = %+v", row[1])
	}
	if row[2].Kind() != types.KindBool || !row[2].AsBool() {
		t.Fatalf("published value = %+v", row[2])
	}
	if row[3].Kind() != types.KindFloat64 || row[3].AsFloat64() != 9.5 {
		t.Fatalf("score value = %+v", row[3])
	}
	if got := row[4].AsArrayString(); len(got) != 2 || got[0] != "go" || got[1] != "ts" {
		t.Fatalf("tags value = %+v", got)
	}
	if got := string(row[5].AsJSON()); got != `{"a":true,"z":1}` {
		t.Fatalf("payload JSON = %s", got)
	}
	if got := string(row[6].AsBytes()); got != "hi" {
		t.Fatalf("blob value = %q", got)
	}
	if !row[7].IsNull() || row[7].Kind() != types.KindString {
		t.Fatalf("nullable value = %+v", row[7])
	}
	if row[8].Kind() != types.KindTimestamp || row[8].AsTimestamp() != 42 {
		t.Fatalf("created_at value = %+v", row[8])
	}
	if row[9].Kind() != types.KindUUID || row[9].UUIDString() != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("request_id value = %+v", row[9])
	}
}

func TestProductValueFromJSONRejectsInvalidArgumentObjects(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "id", Type: "uint8"},
		{Name: "body", Type: "string"},
	}}

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "non-object", raw: `[]`},
		{name: "unknown field", raw: `{"id": 1, "body": "hello", "extra": true}`},
		{name: "missing required", raw: `{"id": 1}`},
		{name: "duplicate field", raw: `{"id": 1, "id": 2, "body": "hello"}`},
		{name: "type mismatch", raw: `{"id": 1, "body": 2}`},
		{name: "integer range", raw: `{"id": 300, "body": "hello"}`},
		{name: "null non-nullable", raw: `{"id": null, "body": "hello"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ProductValueFromJSON(product, []byte(tc.raw))
			if !errors.Is(err, ErrInvalidArgumentJSON) {
				t.Fatalf("ProductValueFromJSON error = %v, want ErrInvalidArgumentJSON", err)
			}
		})
	}
}

func TestProductValueFromJSONRejectsUnsupportedContractTypes(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "value", Type: "not-a-kind"},
	}}

	_, err := ProductValueFromJSON(product, []byte(`{"value": 1}`))
	if !errors.Is(err, ErrUnsupportedArgumentType) {
		t.Fatalf("ProductValueFromJSON unsupported type error = %v, want ErrUnsupportedArgumentType", err)
	}
}

func TestEncodeProductValueArgumentsMatchesBSATNColumnEncoding(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "id", Type: "uint64"},
		{Name: "body", Type: "string"},
		{Name: "maybe", Type: "string", Nullable: true},
	}}
	raw := []byte(`{"body": "hello", "maybe": null, "id": 7}`)

	row, err := ProductValueFromJSON(product, raw)
	if err != nil {
		t.Fatalf("ProductValueFromJSON returned error: %v", err)
	}
	columns, err := productColumnsForBSATN(product)
	if err != nil {
		t.Fatalf("productColumnsForBSATN returned error: %v", err)
	}
	want, err := bsatn.AppendProductValueForColumns(nil, row, columns)
	if err != nil {
		t.Fatalf("AppendProductValueForColumns returned error: %v", err)
	}

	got, err := EncodeProductValueArguments(product, raw)
	if err != nil {
		t.Fatalf("EncodeProductValueArguments returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeProductValueArguments bytes = %x, want %x", got, want)
	}

	decoded, err := bsatn.DecodeProductValueFromBytes(got, productTableSchema(t, "args", product))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 3 || decoded[0].AsUint64() != 7 || decoded[1].AsString() != "hello" || !decoded[2].IsNull() {
		t.Fatalf("decoded arguments = %+v", decoded)
	}
}

func TestEncodeProductValueArgumentsFromReducerArgumentSchema(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{
		Name: "send_message",
		Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
			{Name: "body", Type: "string"},
			{Name: "urgent", Type: "bool"},
		}},
	}}

	args, err := ReducerArgumentSchema(contract, "send_message")
	if err != nil {
		t.Fatalf("ReducerArgumentSchema returned error: %v", err)
	}
	encoded, err := EncodeProductValueArguments(args, []byte(`{"urgent": true, "body": "hello"}`))
	if err != nil {
		t.Fatalf("EncodeProductValueArguments returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "send_message_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsString() != "hello" || !decoded[1].AsBool() {
		t.Fatalf("decoded reducer arguments = %+v", decoded)
	}
}

func TestEncodeProductValueArgumentsFromQueryArgumentSchema(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "topic", Type: "string"},
			{Name: "limit", Type: "uint32"},
		}},
	}}

	args, err := QueryArgumentSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryArgumentSchema returned error: %v", err)
	}
	encoded, err := EncodeProductValueArguments(args, []byte(`{"limit": 25, "topic": "general"}`))
	if err != nil {
		t.Fatalf("EncodeProductValueArguments returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "recent_messages_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsString() != "general" || decoded[1].AsUint32() != 25 {
		t.Fatalf("decoded query arguments = %+v", decoded)
	}
}

func TestEncodeReducerArgumentsUsesNamedContractSurface(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{
		Name: "send_message",
		Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
			{Name: "body", Type: "string"},
			{Name: "urgent", Type: "bool"},
		}},
	}}

	encoded, err := EncodeReducerArguments(contract, " send_message ", []byte(`{"urgent": true, "body": "hello"}`))
	if err != nil {
		t.Fatalf("EncodeReducerArguments returned error: %v", err)
	}
	args, err := ReducerArgumentSchema(contract, "send_message")
	if err != nil {
		t.Fatalf("ReducerArgumentSchema returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "send_message_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsString() != "hello" || !decoded[1].AsBool() {
		t.Fatalf("decoded reducer arguments = %+v", decoded)
	}
}

func TestEncodeQueryArgumentsUsesNamedContractSurface(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "topic", Type: "string"},
			{Name: "limit", Type: "uint32"},
		}},
	}}

	encoded, err := EncodeQueryArguments(contract, " recent_messages ", []byte(`{"limit": 25, "topic": "general"}`))
	if err != nil {
		t.Fatalf("EncodeQueryArguments returned error: %v", err)
	}
	args, err := QueryArgumentSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryArgumentSchema returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "recent_messages_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsString() != "general" || decoded[1].AsUint32() != 25 {
		t.Fatalf("decoded query arguments = %+v", decoded)
	}
}

func TestEncodeOptionalQueryArgumentsPreservesNoParameterQuery(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{Name: "history"}}

	encoded, hasArguments, err := EncodeOptionalQueryArguments(contract, " history ", nil, false)
	if err != nil {
		t.Fatalf("EncodeOptionalQueryArguments returned error: %v", err)
	}
	if hasArguments {
		t.Fatal("EncodeOptionalQueryArguments hasArguments = true, want false")
	}
	if encoded != nil {
		t.Fatalf("EncodeOptionalQueryArguments encoded = %x, want nil", encoded)
	}
}

func TestEncodeOptionalQueryArgumentsEncodesParameterizedQuery(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "topic", Type: "string"},
			{Name: "limit", Type: "uint32"},
		}},
	}}

	encoded, hasArguments, err := EncodeOptionalQueryArguments(contract, " recent_messages ", []byte(`{"limit": 25, "topic": "general"}`), true)
	if err != nil {
		t.Fatalf("EncodeOptionalQueryArguments returned error: %v", err)
	}
	if !hasArguments {
		t.Fatal("EncodeOptionalQueryArguments hasArguments = false, want true")
	}
	args, err := QueryArgumentSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryArgumentSchema returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "recent_messages_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsString() != "general" || decoded[1].AsUint32() != 25 {
		t.Fatalf("decoded optional query arguments = %+v", decoded)
	}
}

func TestEncodeOptionalQueryArgumentsTreatsEmptySchemaAsNoParameterQuery(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name:       "all_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{}},
	}}

	encoded, hasArguments, err := EncodeOptionalQueryArguments(contract, "all_messages", []byte(`{}`), true)
	if err != nil {
		t.Fatalf("EncodeOptionalQueryArguments returned error: %v", err)
	}
	if hasArguments {
		t.Fatal("EncodeOptionalQueryArguments hasArguments = true, want false")
	}
	if encoded != nil {
		t.Fatalf("EncodeOptionalQueryArguments encoded = %x, want nil", encoded)
	}
}

func TestEncodeOptionalQueryArgumentsRejectsMissingSchemaAndInvalidInput(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{
		{Name: "history"},
		{
			Name: "recent_messages",
			Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
				{Name: "limit", Type: "uint8"},
			}},
		},
	}

	_, _, err := EncodeOptionalQueryArguments(contract, "missing", nil, false)
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("missing query error = %v, want ErrSurfaceNotFound", err)
	}
	_, _, err = EncodeOptionalQueryArguments(contract, "history", []byte(`{}`), true)
	if err != nil {
		t.Fatalf("schema-less empty query arguments error = %v, want nil", err)
	}
	_, _, err = EncodeOptionalQueryArguments(contract, "history", []byte(`{"limit": 1}`), true)
	if !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("schema-less non-empty query arguments error = %v, want ErrInvalidArgumentJSON", err)
	}
	_, _, err = EncodeOptionalQueryArguments(contract, "recent_messages", nil, false)
	if !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("missing query arguments error = %v, want ErrArgumentSchemaMissing", err)
	}
	_, _, err = EncodeOptionalQueryArguments(contract, "recent_messages", []byte(`{"limit": 300}`), true)
	if !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("invalid query arguments error = %v, want ErrInvalidArgumentJSON", err)
	}
}

func TestEncodeNamedArgumentsPreservesStructuredErrors(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{Name: "send_message"}}
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "limit", Type: "uint8"},
		}},
	}}

	_, err := EncodeReducerArguments(contract, "missing", []byte(`{}`))
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("EncodeReducerArguments missing error = %v, want ErrSurfaceNotFound", err)
	}
	_, err = EncodeReducerArguments(contract, "send_message", []byte(`{}`))
	if !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("EncodeReducerArguments schema-less error = %v, want ErrArgumentSchemaMissing", err)
	}
	_, err = EncodeQueryArguments(contract, "recent_messages", []byte(`{"limit": 300}`))
	if !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("EncodeQueryArguments invalid JSON error = %v, want ErrInvalidArgumentJSON", err)
	}
}

func TestEncodeSurfaceArgumentsSupportsReducerEmptySchema(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{
		Name: "ping",
		Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{}},
	}}

	encoded, err := EncodeSurfaceArguments(contract, ArgumentSurfaceReducer, " ping ", []byte(`{}`))
	if err != nil {
		t.Fatalf("EncodeSurfaceArguments returned error: %v", err)
	}
	if len(encoded) != 0 {
		t.Fatalf("encoded empty reducer arguments = %x, want empty BSATN row", encoded)
	}
	args, err := ReducerArgumentSchema(contract, "ping")
	if err != nil {
		t.Fatalf("ReducerArgumentSchema returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "ping_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded empty reducer arguments = %+v, want empty row", decoded)
	}
}

func TestEncodeSurfaceArgumentsSupportsDeclaredQueryEmptySchema(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name:       "all_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{}},
	}}

	encoded, err := EncodeSurfaceArguments(contract, ArgumentSurfaceDeclaredQuery, " all_messages ", []byte(`{}`))
	if err != nil {
		t.Fatalf("EncodeSurfaceArguments returned error: %v", err)
	}
	if len(encoded) != 0 {
		t.Fatalf("encoded empty query arguments = %x, want empty BSATN row", encoded)
	}
	args, err := QueryArgumentSchema(contract, "all_messages")
	if err != nil {
		t.Fatalf("QueryArgumentSchema returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "all_messages_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded empty query arguments = %+v, want empty row", decoded)
	}
}

func TestEncodeSurfaceArgumentsRejectsSchemaLessSurfaces(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{Name: "send_message"}}
	contract.Queries = []shunter.QueryDescription{{Name: "history"}}

	_, err := EncodeSurfaceArguments(contract, ArgumentSurfaceReducer, "send_message", []byte(`{}`))
	if !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("schema-less reducer error = %v, want ErrArgumentSchemaMissing", err)
	}
	_, err = EncodeSurfaceArguments(contract, ArgumentSurfaceDeclaredQuery, "history", []byte(`{}`))
	if !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("schema-less query error = %v, want ErrArgumentSchemaMissing", err)
	}
}

func TestEncodeSurfaceArgumentsRejectsUnknownNames(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{
		Name: "send_message",
		Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{}},
	}}
	contract.Queries = []shunter.QueryDescription{{
		Name:       "history",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{}},
	}}

	_, err := EncodeSurfaceArguments(contract, ArgumentSurfaceReducer, "missing", []byte(`{}`))
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("missing reducer error = %v, want ErrSurfaceNotFound", err)
	}
	_, err = EncodeSurfaceArguments(contract, ArgumentSurfaceDeclaredQuery, "missing", []byte(`{}`))
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("missing query error = %v, want ErrSurfaceNotFound", err)
	}
}

func TestEncodeSurfaceArgumentsRejectsMalformedJSON(t *testing.T) {
	contract := workflowContractFixture()
	contract.Schema.Reducers = []schema.ReducerExport{{
		Name: "send_message",
		Args: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
			{Name: "body", Type: "string"},
		}},
	}}
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "limit", Type: "uint32"},
		}},
	}}

	_, err := EncodeSurfaceArguments(contract, ArgumentSurfaceReducer, "send_message", []byte(`{"body":`))
	if !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("malformed reducer JSON error = %v, want ErrInvalidArgumentJSON", err)
	}
	_, err = EncodeSurfaceArguments(contract, ArgumentSurfaceDeclaredQuery, "recent_messages", []byte(`{"limit":`))
	if !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("malformed query JSON error = %v, want ErrInvalidArgumentJSON", err)
	}
}

func TestEncodeSurfaceArgumentsDeclaredQueryDecodeEquivalence(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "topic", Type: "string"},
			{Name: "limit", Type: "uint32"},
		}},
	}}

	encoded, err := EncodeSurfaceArguments(contract, ArgumentSurfaceDeclaredQuery, "recent_messages", []byte(`{"limit": 25, "topic": "general"}`))
	if err != nil {
		t.Fatalf("EncodeSurfaceArguments returned error: %v", err)
	}
	args, err := QueryArgumentSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryArgumentSchema returned error: %v", err)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(encoded, productTableSchema(t, "recent_messages_args", args))
	if err != nil {
		t.Fatalf("DecodeProductValueFromBytes returned error: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsString() != "general" || decoded[1].AsUint32() != 25 {
		t.Fatalf("decoded query arguments = %+v", decoded)
	}
}

func TestEncodeSurfaceArgumentsRejectsUnsupportedKind(t *testing.T) {
	_, err := EncodeSurfaceArguments(workflowContractFixture(), ArgumentSurfaceKind("view"), "history", []byte(`{}`))
	if !errors.Is(err, ErrUnsupportedSurfaceKind) {
		t.Fatalf("unsupported kind error = %v, want ErrUnsupportedSurfaceKind", err)
	}
}

func TestDecodeQueryRowsUsesContractRowSchema(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"},
	}}
	rowSchema, err := QueryRowSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryRowSchema returned error: %v", err)
	}
	columns, err := productColumnsForBSATN(rowSchema)
	if err != nil {
		t.Fatalf("productColumnsForBSATN returned error: %v", err)
	}
	rowList, err := protocol.EncodeProductRowsForColumns([]types.ProductValue{
		{types.NewUint64(7), types.NewString("hello")},
		{types.NewUint64(8), types.NewString("bye")},
	}, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns returned error: %v", err)
	}

	decoded, err := DecodeQueryRows(contract, " recent_messages ", "messages", rowList)
	if err != nil {
		t.Fatalf("DecodeQueryRows returned error: %v", err)
	}
	if decoded.Name != "recent_messages" || decoded.TableName != "messages" {
		t.Fatalf("decoded metadata = %+v", decoded)
	}
	if len(decoded.Columns) != 2 || decoded.Columns[0].Name != "id" || decoded.Columns[1].Name != "body" {
		t.Fatalf("decoded columns = %+v", decoded.Columns)
	}
	if len(decoded.Rows) != 2 ||
		decoded.Rows[0][0].AsUint64() != 7 ||
		decoded.Rows[0][1].AsString() != "hello" ||
		decoded.Rows[1][0].AsUint64() != 8 ||
		decoded.Rows[1][1].AsString() != "bye" {
		t.Fatalf("decoded rows = %+v", decoded.Rows)
	}
}

func TestDecodeQueryRowsSupportsEmptyRowList(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"},
	}}

	decoded, err := DecodeQueryRows(contract, "recent_messages", "messages", protocol.EncodeRowList(nil))
	if err != nil {
		t.Fatalf("DecodeQueryRows returned error: %v", err)
	}
	if len(decoded.Rows) != 0 {
		t.Fatalf("decoded empty RowList rows = %+v, want none", decoded.Rows)
	}
}

func TestDecodeQueryRowsPreservesStructuredErrors(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"},
	}}

	_, err := DecodeQueryRows(contract, "missing", "messages", protocol.EncodeRowList(nil))
	if !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("DecodeQueryRows missing error = %v, want ErrSurfaceNotFound", err)
	}

	schemaLess := workflowContractFixture()
	schemaLess.Queries = []shunter.QueryDescription{{Name: "history"}}
	_, err = DecodeQueryRows(schemaLess, "history", "messages", protocol.EncodeRowList(nil))
	if !errors.Is(err, ErrResultSchemaMissing) {
		t.Fatalf("DecodeQueryRows schema-less error = %v, want ErrResultSchemaMissing", err)
	}

	_, err = DecodeQueryRows(contract, "recent_messages", "other", protocol.EncodeRowList(nil))
	if !errors.Is(err, ErrResultTableMismatch) {
		t.Fatalf("DecodeQueryRows table mismatch error = %v, want ErrResultTableMismatch", err)
	}

	_, err = DecodeQueryRows(contract, "recent_messages", "messages", []byte{1, 0, 0})
	if !errors.Is(err, protocol.ErrMalformedMessage) {
		t.Fatalf("DecodeQueryRows malformed RowList error = %v, want protocol.ErrMalformedMessage", err)
	}

	rowList := protocol.EncodeRowList([][]byte{{0xff}})
	_, err = DecodeQueryRows(contract, "recent_messages", "messages", rowList)
	var typeTagErr *bsatn.TypeTagMismatchError
	if !errors.As(err, &typeTagErr) {
		t.Fatalf("DecodeQueryRows malformed row error = %v, want bsatn.TypeTagMismatchError", err)
	}
}

func TestDecodeQueryResponseUsesSingleResponseTable(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "body", Type: "string"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeProjection, Table: "messages"},
	}}
	rowSchema, err := QueryRowSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryRowSchema returned error: %v", err)
	}
	columns, err := productColumnsForBSATN(rowSchema)
	if err != nil {
		t.Fatalf("productColumnsForBSATN returned error: %v", err)
	}
	rowList, err := protocol.EncodeProductRowsForColumns([]types.ProductValue{
		{types.NewString("hello")},
	}, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns returned error: %v", err)
	}

	decoded, err := DecodeQueryResponse(contract, "recent_messages", protocol.OneOffQueryResponse{
		Tables: []protocol.OneOffTable{{
			TableName: "messages",
			Rows:      rowList,
		}},
	})
	if err != nil {
		t.Fatalf("DecodeQueryResponse returned error: %v", err)
	}
	if len(decoded.Rows) != 1 || decoded.Rows[0][0].AsString() != "hello" {
		t.Fatalf("decoded response rows = %+v", decoded.Rows)
	}
}

func TestDecodeQueryResponseRejectsUnexpectedTableCounts(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"},
	}}

	_, err := DecodeQueryResponse(contract, "recent_messages", protocol.OneOffQueryResponse{})
	if !errors.Is(err, ErrResultTableCount) {
		t.Fatalf("DecodeQueryResponse empty tables error = %v, want ErrResultTableCount", err)
	}

	_, err = DecodeQueryResponse(contract, "recent_messages", protocol.OneOffQueryResponse{
		Tables: []protocol.OneOffTable{
			{TableName: "messages", Rows: protocol.EncodeRowList(nil)},
			{TableName: "messages", Rows: protocol.EncodeRowList(nil)},
		},
	})
	if !errors.Is(err, ErrResultTableCount) {
		t.Fatalf("DecodeQueryResponse multiple tables error = %v, want ErrResultTableCount", err)
	}
}

func TestDecodeQueryResponseJSONRowsComposesResponseDecodeAndJSONRendering(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"},
	}}
	rowSchema, err := QueryRowSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryRowSchema returned error: %v", err)
	}
	columns, err := productColumnsForBSATN(rowSchema)
	if err != nil {
		t.Fatalf("productColumnsForBSATN returned error: %v", err)
	}
	rowList, err := protocol.EncodeProductRowsForColumns([]types.ProductValue{
		{types.NewUint64(7), types.NewString("hello")},
	}, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns returned error: %v", err)
	}

	rows, err := DecodeQueryResponseJSONRows(contract, "recent_messages", protocol.OneOffQueryResponse{
		Tables: []protocol.OneOffTable{{TableName: "messages", Rows: rowList}},
	})
	if err != nil {
		t.Fatalf("DecodeQueryResponseJSONRows returned error: %v", err)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("Marshal JSON rows returned error: %v", err)
	}
	want := `[{"body":"hello","id":"7"}]`
	if string(out) != want {
		t.Fatalf("JSON rows = %s, want %s", out, want)
	}
}

func TestDecodeQueryResponseJSONResultPreservesQueryMetadata(t *testing.T) {
	contract := workflowContractFixture()
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		RowSchema: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
			{Name: "id", Type: "uint64"},
			{Name: "body", Type: "string"},
		}},
		ResultShape: &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"},
	}}
	rowSchema, err := QueryRowSchema(contract, "recent_messages")
	if err != nil {
		t.Fatalf("QueryRowSchema returned error: %v", err)
	}
	columns, err := productColumnsForBSATN(rowSchema)
	if err != nil {
		t.Fatalf("productColumnsForBSATN returned error: %v", err)
	}
	rowList, err := protocol.EncodeProductRowsForColumns([]types.ProductValue{
		{types.NewUint64(7), types.NewString("hello")},
	}, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns returned error: %v", err)
	}

	result, err := DecodeQueryResponseJSONResult(contract, "recent_messages", protocol.OneOffQueryResponse{
		Tables: []protocol.OneOffTable{{TableName: "messages", Rows: rowList}},
	})
	if err != nil {
		t.Fatalf("DecodeQueryResponseJSONResult returned error: %v", err)
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal JSON result returned error: %v", err)
	}
	want := `{"name":"recent_messages","table_name":"messages","rows":[{"body":"hello","id":"7"}]}`
	if string(out) != want {
		t.Fatalf("JSON result = %s, want %s", out, want)
	}
}

func TestProductValueToJSONRowConvertsContractValues(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "ok", Type: "bool"},
		{Name: "count", Type: "uint64"},
		{Name: "label", Type: "string"},
		{Name: "blob", Type: "bytes"},
		{Name: "wide_signed", Type: "int128"},
		{Name: "wide_unsigned", Type: "uint128"},
		{Name: "at", Type: "timestamp"},
		{Name: "tags", Type: "arrayString"},
		{Name: "request_id", Type: "uuid"},
		{Name: "duration", Type: "duration"},
		{Name: "payload", Type: "json"},
		{Name: "maybe", Type: "string", Nullable: true},
	}}
	uuidValue, err := types.ParseUUID("123e4567-e89b-12d3-a456-426614174000")
	if err != nil {
		t.Fatalf("ParseUUID returned error: %v", err)
	}
	jsonValue, err := types.NewJSON([]byte(`{"z":1,"a":true}`))
	if err != nil {
		t.Fatalf("NewJSON returned error: %v", err)
	}
	row := types.ProductValue{
		types.NewBool(true),
		types.NewUint64(^uint64(0)),
		types.NewString("hello"),
		types.NewBytes([]byte("hi")),
		types.NewInt128(-1, ^uint64(0)),
		types.NewUint128(1, 0),
		types.NewTimestamp(42),
		types.NewArrayString([]string{"go", "ts"}),
		uuidValue,
		types.NewDuration(123),
		jsonValue,
		types.NewNull(types.KindString),
	}

	jsonRow, err := ProductValueToJSONRow(product, row)
	if err != nil {
		t.Fatalf("ProductValueToJSONRow returned error: %v", err)
	}
	out, err := json.Marshal(jsonRow)
	if err != nil {
		t.Fatalf("Marshal JSON row returned error: %v", err)
	}
	want := `{"at":"42","blob":"aGk=","count":"18446744073709551615","duration":"123","label":"hello","maybe":null,"ok":true,"payload":{"a":true,"z":1},"request_id":"123e4567-e89b-12d3-a456-426614174000","tags":["go","ts"],"wide_signed":"-1","wide_unsigned":"18446744073709551616"}`
	if string(out) != want {
		t.Fatalf("JSON row = %s, want %s", out, want)
	}
}

func TestDecodedQueryRowsToJSONRowsUsesDecodedColumns(t *testing.T) {
	decoded := DecodedQueryRows{
		Name: "recent_messages",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "body", Type: types.KindString},
			{Index: 1, Name: "urgent", Type: types.KindBool},
		},
		Rows: []types.ProductValue{
			{types.NewString("hello"), types.NewBool(true)},
		},
	}

	rows, err := DecodedQueryRowsToJSONRows(decoded)
	if err != nil {
		t.Fatalf("DecodedQueryRowsToJSONRows returned error: %v", err)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("Marshal JSON rows returned error: %v", err)
	}
	want := `[{"body":"hello","urgent":true}]`
	if string(out) != want {
		t.Fatalf("JSON rows = %s, want %s", out, want)
	}
}

func TestDecodedQueryRowsToJSONResultUsesDecodedMetadata(t *testing.T) {
	decoded := DecodedQueryRows{
		Name:      "recent_messages",
		TableName: "messages",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "body", Type: types.KindString},
		},
		Rows: []types.ProductValue{
			{types.NewString("hello")},
		},
	}

	result, err := DecodedQueryRowsToJSONResult(decoded)
	if err != nil {
		t.Fatalf("DecodedQueryRowsToJSONResult returned error: %v", err)
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal JSON result returned error: %v", err)
	}
	want := `{"name":"recent_messages","table_name":"messages","rows":[{"body":"hello"}]}`
	if string(out) != want {
		t.Fatalf("JSON result = %s, want %s", out, want)
	}
}

func TestProductValueToJSONRowPreservesStructuredShapeErrors(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "id", Type: "uint64"},
		{Name: "body", Type: "string", Nullable: true},
	}}

	_, err := ProductValueToJSONRow(product, types.ProductValue{types.NewUint64(1)})
	if !errors.Is(err, ErrProductValueShape) {
		t.Fatalf("ProductValueToJSONRow short row error = %v, want ErrProductValueShape", err)
	}

	_, err = ProductValueToJSONRow(product, types.ProductValue{types.NewString("wrong"), types.NewString("hello")})
	if !errors.Is(err, ErrProductValueShape) {
		t.Fatalf("ProductValueToJSONRow kind mismatch error = %v, want ErrProductValueShape", err)
	}

	_, err = ProductValueToJSONRow(product, types.ProductValue{types.NewUint64(1), types.NewNull(types.KindUint64)})
	if !errors.Is(err, ErrProductValueShape) {
		t.Fatalf("ProductValueToJSONRow null kind mismatch error = %v, want ErrProductValueShape", err)
	}
}

func TestEncodeProductValueArgumentsPreservesStructuredErrors(t *testing.T) {
	product := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "id", Type: "uint8"},
	}}

	_, err := EncodeProductValueArguments(product, []byte(`{"id": 300}`))
	if !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("EncodeProductValueArguments invalid JSON error = %v, want ErrInvalidArgumentJSON", err)
	}

	unsupported := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "id", Type: "not-a-kind"},
	}}
	_, err = EncodeProductValueArguments(unsupported, []byte(`{"id": 1}`))
	if !errors.Is(err, ErrUnsupportedArgumentType) {
		t.Fatalf("EncodeProductValueArguments unsupported type error = %v, want ErrUnsupportedArgumentType", err)
	}
}

func TestExportRuntimeFileWritesCanonicalContractJSON(t *testing.T) {
	dir := t.TempDir()
	rt := buildWorkflowRuntime(t)
	outputPath := filepath.Join(dir, shunter.DefaultContractSnapshotFilename)

	if err := ExportRuntimeFile(rt, outputPath); err != nil {
		t.Fatalf("ExportRuntimeFile returned error: %v", err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read exported contract: %v", err)
	}
	want, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("exported contract mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	assertContains(t, string(got), `"contract_format": "shunter.module_contract"`)
	assertNoWorkflowTempFiles(t, dir, shunter.DefaultContractSnapshotFilename)
}

func TestExportRuntimeFileRejectsEmptyOutputPathBeforeRuntimeUse(t *testing.T) {
	err := ExportRuntimeFile(nil, " \t")
	if err == nil {
		t.Fatal("ExportRuntimeFile returned nil error for empty output path")
	}
	if !strings.Contains(err.Error(), "contract output path is required") {
		t.Fatalf("ExportRuntimeFile error = %v, want output path error", err)
	}
}

func TestExportRuntimeFileRejectsNilRuntimeWithoutMutatingOutput(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, shunter.DefaultContractSnapshotFilename)
	original := []byte("existing contract output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	err := ExportRuntimeFile(nil, outputPath)
	if !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("ExportRuntimeFile nil runtime error = %v, want ErrRuntimeRequired", err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing output: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("nil runtime mutated output:\nobserved=%q\nexpected=%q", got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateRuntimeFileWritesDeterministicTypeScriptFromRuntime(t *testing.T) {
	dir := t.TempDir()
	rt := buildWorkflowRuntime(t)
	outputPath := filepath.Join(dir, "client.ts")

	direct, err := GenerateRuntime(rt, codegen.Options{Language: codegen.LanguageTypeScript})
	if err != nil {
		t.Fatalf("GenerateRuntime returned error: %v", err)
	}
	assertContains(t, string(direct), `export interface MessagesRow {`)
	assertContains(t, string(direct), `history: "history",`)

	if err := GenerateRuntimeFile(rt, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("GenerateRuntimeFile returned error: %v", err)
	}
	first := readTextFile(t, outputPath)
	if first != string(direct) {
		t.Fatalf("runtime file output differs from direct output:\nfile:\n%s\ndirect:\n%s", first, direct)
	}

	if err := GenerateRuntimeFile(rt, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("second GenerateRuntimeFile returned error: %v", err)
	}
	second := readTextFile(t, outputPath)
	if first != second {
		t.Fatalf("generated runtime TypeScript was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateRuntimeRejectsNilRuntimeWithoutMutatingOutput(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	_, err := GenerateRuntime(nil, codegen.Options{Language: codegen.LanguageTypeScript})
	if !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("GenerateRuntime nil runtime error = %v, want ErrRuntimeRequired", err)
	}

	err = GenerateRuntimeFile(nil, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("GenerateRuntimeFile nil runtime error = %v, want ErrRuntimeRequired", err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing output: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("nil runtime mutated output:\nobserved=%q\nexpected=%q", got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateRuntimeRejectsUnsupportedLanguageBeforeRuntimeUse(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	_, err := GenerateRuntime(nil, codegen.Options{Language: "go"})
	if err == nil {
		t.Fatal("GenerateRuntime returned nil error for unsupported language")
	}
	if !errors.Is(err, codegen.ErrUnsupportedLanguage) {
		t.Fatalf("GenerateRuntime error = %v, want ErrUnsupportedLanguage", err)
	}

	err = GenerateRuntimeFile(nil, outputPath, codegen.Options{Language: "go"})
	if err == nil {
		t.Fatal("GenerateRuntimeFile returned nil error for unsupported language")
	}
	if !errors.Is(err, codegen.ErrUnsupportedLanguage) {
		t.Fatalf("GenerateRuntimeFile error = %v, want ErrUnsupportedLanguage", err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing output: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("unsupported runtime language mutated output:\nobserved=%q\nexpected=%q", got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
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

func TestGenerateFileInvalidContractLeavesOutputUntouched(t *testing.T) {
	cases := []struct {
		name            string
		mutate          func(*shunter.ModuleContract)
		wantErrContains string
	}{
		{
			name: "semantic_invalid_migration_compatibility",
			mutate: func(contract *shunter.ModuleContract) {
				contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
					Surface: shunter.MigrationSurfaceQuery,
					Name:    "history",
					Metadata: shunter.MigrationMetadata{
						Compatibility: shunter.MigrationCompatibility("maybe"),
					},
				}}
			},
			wantErrContains: "migrations.query.history.compatibility",
		},
		{
			name: "invalid_migration_surface",
			mutate: func(contract *shunter.ModuleContract) {
				contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
					Surface: "subscription",
					Name:    "recent_messages",
					Metadata: shunter.MigrationMetadata{
						Compatibility: shunter.MigrationCompatibilityCompatible,
					},
				}}
			},
			wantErrContains: `migrations surface "subscription" is invalid`,
		},
		{
			name: "unknown_migration_target",
			mutate: func(contract *shunter.ModuleContract) {
				contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
					Surface: shunter.MigrationSurfaceTable,
					Name:    "missing_table",
					Metadata: shunter.MigrationMetadata{
						Compatibility: shunter.MigrationCompatibilityCompatible,
					},
				}}
			},
			wantErrContains: "migrations.table.missing_table references unknown table",
		},
		{
			name: "unknown_permission_target",
			mutate: func(contract *shunter.ModuleContract) {
				contract.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
					Name:     "missing_reducer",
					Required: []string{"messages:send"},
				}}
			},
			wantErrContains: "permissions.reducer.missing_reducer references unknown reducer",
		},
		{
			name: "invalid_table_read_policy",
			mutate: func(contract *shunter.ModuleContract) {
				contract.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
					Access:      schema.TableAccessPublic,
					Permissions: []string{"messages:read"},
				}
			},
			wantErrContains: "schema.tables.messages.read_policy invalid",
		},
		{
			name: "invalid_schema_column_type",
			mutate: func(contract *shunter.ModuleContract) {
				contract.Schema.Tables[0].Columns[1].Type = "notAType"
			},
			wantErrContains: `schema.tables.messages.columns.body type "notAType" is invalid`,
		},
		{
			name: "unknown_read_model_target",
			mutate: func(contract *shunter.ModuleContract) {
				contract.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
					Surface: shunter.ReadModelSurfaceQuery,
					Name:    "missing_query",
					Tables:  []string{"messages"},
					Tags:    []string{"history"},
				}}
			},
			wantErrContains: "read_model.query.missing_query references unknown query",
		},
		{
			name: "invalid_read_model_surface",
			mutate: func(contract *shunter.ModuleContract) {
				contract.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
					Surface: "subscription",
					Name:    "recent_messages",
					Tables:  []string{"messages"},
					Tags:    []string{"history"},
				}}
			},
			wantErrContains: `read_model surface "subscription" is invalid`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertInvalidContractGenerateFileLeavesOutputUntouched(t, tc.mutate, tc.wantErrContains)
		})
	}
}

func assertInvalidContractGenerateFileLeavesOutputUntouched(
	t *testing.T,
	mutate func(*shunter.ModuleContract),
	wantErrContains string,
) {
	t.Helper()
	dir := t.TempDir()
	invalidContract := workflowContractFixture()
	mutate(&invalidContract)
	contractPath := writeContractFixture(t, dir, "contract.json", invalidContract)
	outputPath := filepath.Join(dir, "client.ts")
	original := []byte("existing generated output\n")
	if err := os.WriteFile(outputPath, original, 0o666); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFile returned nil error for invalid contract")
	}
	if !errors.Is(err, codegen.ErrInvalidContract) {
		t.Fatalf("GenerateFile error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), wantErrContains) {
		t.Fatalf("GenerateFile error = %v, want context %q", err, wantErrContains)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing output: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("invalid contract mutated output:\nobserved=%q\nexpected=%q", got, original)
	}
	assertNoWorkflowTempFiles(t, dir, filepath.Base(outputPath))
}

func TestGenerateFileCreatesNewOutputWithOwnerWritableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are POSIX-specific")
	}

	const trace = "trace=workflow-codegen-new-output-mode"
	dir := t.TempDir()
	contractPath := writeContractFixture(t, dir, "contract.json", workflowContractFixture())
	outputPath := filepath.Join(dir, "client.ts")

	if err := GenerateFile(contractPath, outputPath, codegen.Options{Language: codegen.LanguageTypeScript}); err != nil {
		t.Fatalf("%s GenerateFile returned error: %v", trace, err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("%s stat output: %v", trace, err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("%s output mode = %#o, want 0644", trace, got)
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

func buildWorkflowRuntime(t *testing.T) *shunter.Runtime {
	t.Helper()
	mod := shunter.NewModule("workflow").
		Version("v1.0.0").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "messages",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: schema.KindUint64, PrimaryKey: true, AutoIncrement: true},
				{Name: "body", Type: schema.KindString},
			},
		}).
		Query(shunter.QueryDeclaration{
			Name: "history",
			SQL:  "SELECT * FROM messages",
		})
	rt, err := shunter.Build(mod, shunter.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build workflow runtime returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func writeContractFixture(t *testing.T, dir, name string, contract shunter.ModuleContract) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := writeFile(path, mustContractJSON(t, contract)); err != nil {
		t.Fatalf("write contract fixture %s: %v", name, err)
	}
	return path
}

func productTableSchema(t *testing.T, name string, product schema.ProductSchemaExport) *schema.TableSchema {
	t.Helper()
	columns, err := productColumnsForBSATN(product)
	if err != nil {
		t.Fatalf("productColumnsForBSATN returned error: %v", err)
	}
	return &schema.TableSchema{Name: name, Columns: columns}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
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
