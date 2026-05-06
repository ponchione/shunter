package shunter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestV1CompatibilityModuleContractGolden(t *testing.T) {
	rt := buildV1CompatibilityRuntime(t)

	got, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	assertGoldenBytes(t, filepath.Join("testdata", "v1_module_contract.json"), got)

	var decoded ModuleContract
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Unmarshal golden contract JSON: %v", err)
	}
	if err := ValidateModuleContract(decoded); err != nil {
		t.Fatalf("golden contract did not validate: %v", err)
	}
	recoded, err := decoded.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON after decode returned error: %v", err)
	}
	if !bytes.Equal(got, recoded) {
		t.Fatalf("golden contract did not canonicalize idempotently\nfirst:\n%s\nsecond:\n%s", got, recoded)
	}
}

func TestV1CompatibilityModuleContractExportEntryPointsMatch(t *testing.T) {
	rt := buildV1CompatibilityRuntime(t)

	want, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	contract := rt.ExportContract()
	got, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("ExportContract().MarshalCanonicalJSON returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ExportContract canonical JSON differs from ExportContractJSON\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	exportedSchema := rt.ExportSchema()
	if exportedSchema == nil {
		t.Fatal("ExportSchema returned nil")
	}
	if !reflect.DeepEqual(*exportedSchema, contract.Schema) {
		t.Fatalf("ExportSchema differs from ExportContract().Schema\n--- got ---\n%#v\n--- want ---\n%#v", *exportedSchema, contract.Schema)
	}
}

func TestV1CompatibilityModuleContractJSONIgnoresUnknownFields(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	withUnknown := v1ContractJSONWithUnknownFields(t, want)

	var decoded ModuleContract
	if err := json.Unmarshal(withUnknown, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON with unknown fields: %v", err)
	}
	if err := ValidateModuleContract(decoded); err != nil {
		t.Fatalf("contract with unknown fields did not validate: %v", err)
	}
	got, err := decoded.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unknown fields affected canonical contract JSON\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestV1CompatibilityModuleContractRejectsVersionDrift(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	var base ModuleContract
	if err := json.Unmarshal(data, &base); err != nil {
		t.Fatalf("Unmarshal v1 contract fixture: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ModuleContract)
		want   string
	}{
		{
			name: "contract_version",
			mutate: func(contract *ModuleContract) {
				contract.ContractVersion = ModuleContractVersion + 1
			},
			want: "contract_version = 2, want 1",
		},
		{
			name: "codegen_contract_version",
			mutate: func(contract *ModuleContract) {
				contract.Codegen.ContractVersion = ModuleContractVersion + 1
			},
			want: "codegen.contract_version = 2, want 1",
		},
		{
			name: "codegen_contract_format",
			mutate: func(contract *ModuleContract) {
				contract.Codegen.ContractFormat = "future.module_contract"
			},
			want: `codegen.contract_format = "future.module_contract"`,
		},
		{
			name: "codegen_default_snapshot_filename",
			mutate: func(contract *ModuleContract) {
				contract.Codegen.DefaultSnapshotFilename = "future.contract.json"
			},
			want: `codegen.default_snapshot_filename = "future.contract.json"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := base
			tc.mutate(&contract)
			err := ValidateModuleContract(contract)
			if err == nil {
				t.Fatal("ValidateModuleContract returned nil error, want v1 known-field rejection")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateModuleContract error = %v, want context %q", err, tc.want)
			}
		})
	}
}

func TestV1CompatibilityModuleContractFixtureCoversStableArtifacts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("Unmarshal top-level contract JSON: %v", err)
	}
	for _, key := range []string{
		"contract_version",
		"module",
		"schema",
		"queries",
		"views",
		"visibility_filters",
		"permissions",
		"read_model",
		"migrations",
		"codegen",
	} {
		if _, ok := topLevel[key]; !ok {
			t.Fatalf("v1 contract fixture missing top-level key %q", key)
		}
	}

	var contract ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("Unmarshal v1 contract fixture: %v", err)
	}
	if contract.ContractVersion != ModuleContractVersion {
		t.Fatalf("contract_version = %d, want %d", contract.ContractVersion, ModuleContractVersion)
	}
	assertV1FixtureTable(t, contract)
	assertV1FixtureDeclaredReads(t, contract)
	assertV1FixtureVisibilityFilter(t, contract)
	assertV1FixtureMetadata(t, contract)
}

func buildV1CompatibilityRuntime(t *testing.T) *Runtime {
	t.Helper()

	mod := NewModule("v1_guardrails").
		Version("v1.0.0").
		Metadata(map[string]string{"owner": "v1-contract", "purpose": "compatibility-fixture"}).
		SchemaVersion(3).
		TableDef(v1CompatibilityMessagesTableDef(), schema.WithReadPermissions("messages:read")).
		Reducer("create_message", noopReducer, WithReducerPermissions(PermissionMetadata{
			Required: []string{"messages:write"},
		})).
		OnConnect(noopLifecycle).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE sender = :sender",
		}).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history", "v1"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "declared query fixture",
			},
		}).
		View(ViewDeclaration{
			Name:        "live_message_projection",
			SQL:         "SELECT id, body AS text FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"projection", "v1"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "declared live view projection fixture",
			},
		}).
		View(ViewDeclaration{
			Name:        "live_message_count",
			SQL:         "SELECT COUNT(*) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"aggregate", "v1"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "declared live view count fixture",
			},
		}).
		Migration(MigrationMetadata{
			ModuleVersion:   "v1.0.0",
			SchemaVersion:   3,
			ContractVersion: ModuleContractVersion,
			PreviousVersion: "v0.9.0",
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
			Notes:           "representative v1 contract fixture",
		}).
		TableMigration("messages", MigrationMetadata{
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
			Notes:           "messages table fixture",
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt
}

func v1CompatibilityMessagesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "sender", Type: types.KindString},
			{Name: "body", Type: types.KindString},
			{Name: "sent_at", Type: types.KindTimestamp},
		},
		Indexes: []schema.IndexDefinition{
			{Name: "messages_sender_idx", Columns: []string{"sender"}},
			{Name: "messages_sent_at_idx", Columns: []string{"sent_at"}},
		},
	}
}

func assertV1FixtureTable(t *testing.T, contract ModuleContract) {
	t.Helper()
	for _, table := range contract.Schema.Tables {
		if table.Name != "messages" {
			continue
		}
		if got, want := table.ReadPolicy.Access, schema.TableAccessPermissioned; got != want {
			t.Fatalf("messages read policy access = %s, want %s", got, want)
		}
		if len(table.ReadPolicy.Permissions) != 1 || table.ReadPolicy.Permissions[0] != "messages:read" {
			t.Fatalf("messages read permissions = %#v, want [messages:read]", table.ReadPolicy.Permissions)
		}
		for _, column := range []string{"id", "sender", "body", "sent_at"} {
			if !v1FixtureHasColumn(table.Columns, column) {
				t.Fatalf("messages table missing column %q: %#v", column, table.Columns)
			}
		}
		return
	}
	t.Fatalf("v1 contract fixture missing messages table: %#v", contract.Schema.Tables)
}

func assertV1FixtureDeclaredReads(t *testing.T, contract ModuleContract) {
	t.Helper()
	assertV1FixtureQuerySQL(t, contract.Queries, "recent_messages", "SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25")
	assertV1FixtureViewSQL(t, contract.Views, "live_message_projection", "SELECT id, body AS text FROM messages")
	assertV1FixtureViewSQL(t, contract.Views, "live_message_count", "SELECT COUNT(*) AS n FROM messages")
	assertV1FixturePermission(t, contract.Permissions.Reducers, "create_message", "messages:write")
	assertV1FixturePermission(t, contract.Permissions.Queries, "recent_messages", "messages:read")
	assertV1FixturePermission(t, contract.Permissions.Views, "live_message_projection", "messages:subscribe")
	assertV1FixturePermission(t, contract.Permissions.Views, "live_message_count", "messages:subscribe")
	assertV1FixtureReadModel(t, contract.ReadModel.Declarations, ReadModelSurfaceQuery, "recent_messages", "history")
	assertV1FixtureReadModel(t, contract.ReadModel.Declarations, ReadModelSurfaceView, "live_message_projection", "projection")
	assertV1FixtureReadModel(t, contract.ReadModel.Declarations, ReadModelSurfaceView, "live_message_count", "aggregate")
}

func assertV1FixtureVisibilityFilter(t *testing.T, contract ModuleContract) {
	t.Helper()
	if len(contract.VisibilityFilters) != 1 {
		t.Fatalf("visibility_filters = %#v, want one own_messages filter", contract.VisibilityFilters)
	}
	filter := contract.VisibilityFilters[0]
	if filter.Name != "own_messages" ||
		filter.SQL != "SELECT * FROM messages WHERE sender = :sender" ||
		filter.ReturnTable != "messages" ||
		filter.ReturnTableID != 0 ||
		!filter.UsesCallerIdentity {
		t.Fatalf("visibility filter = %#v, want own_messages caller filter on messages", filter)
	}
}

func assertV1FixtureMetadata(t *testing.T, contract ModuleContract) {
	t.Helper()
	if contract.Module.Name != "v1_guardrails" || contract.Module.Version != "v1.0.0" {
		t.Fatalf("module identity = %#v, want v1_guardrails v1.0.0", contract.Module)
	}
	if contract.Migrations.Module.ContractVersion != ModuleContractVersion ||
		contract.Migrations.Module.Compatibility != MigrationCompatibilityCompatible {
		t.Fatalf("module migration metadata = %#v, want v1 compatible metadata", contract.Migrations.Module)
	}
	for _, want := range []struct {
		surface string
		name    string
	}{
		{MigrationSurfaceTable, "messages"},
		{MigrationSurfaceQuery, "recent_messages"},
		{MigrationSurfaceView, "live_message_projection"},
		{MigrationSurfaceView, "live_message_count"},
	} {
		if !v1FixtureHasMigration(contract.Migrations.Declarations, want.surface, want.name) {
			t.Fatalf("migrations missing %s %q: %#v", want.surface, want.name, contract.Migrations.Declarations)
		}
	}
	if contract.Codegen.ContractFormat != ModuleContractFormat ||
		contract.Codegen.ContractVersion != ModuleContractVersion ||
		contract.Codegen.DefaultSnapshotFilename != DefaultContractSnapshotFilename {
		t.Fatalf("codegen metadata = %#v, want v1 defaults", contract.Codegen)
	}
}

func v1FixtureHasColumn(columns []schema.ColumnExport, name string) bool {
	for _, column := range columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func assertV1FixtureQuerySQL(t *testing.T, queries []QueryDescription, name, sql string) {
	t.Helper()
	for _, query := range queries {
		if query.Name == name {
			if query.SQL != sql {
				t.Fatalf("query %q SQL = %q, want %q", name, query.SQL, sql)
			}
			return
		}
	}
	t.Fatalf("queries missing %q: %#v", name, queries)
}

func assertV1FixtureViewSQL(t *testing.T, views []ViewDescription, name, sql string) {
	t.Helper()
	for _, view := range views {
		if view.Name == name {
			if view.SQL != sql {
				t.Fatalf("view %q SQL = %q, want %q", name, view.SQL, sql)
			}
			return
		}
	}
	t.Fatalf("views missing %q: %#v", name, views)
}

func assertV1FixturePermission(t *testing.T, declarations []PermissionContractDeclaration, name, required string) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Name == name {
			if len(declaration.Required) != 1 || declaration.Required[0] != required {
				t.Fatalf("permission %q = %#v, want required %q", name, declaration.Required, required)
			}
			return
		}
	}
	t.Fatalf("permissions missing %q: %#v", name, declarations)
}

func assertV1FixtureReadModel(t *testing.T, declarations []ReadModelContractDeclaration, surface, name, tag string) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Surface == surface && declaration.Name == name {
			if len(declaration.Tables) != 1 || declaration.Tables[0] != "messages" {
				t.Fatalf("read model %s %q tables = %#v, want [messages]", surface, name, declaration.Tables)
			}
			if !v1FixtureHasString(declaration.Tags, tag) || !v1FixtureHasString(declaration.Tags, "v1") {
				t.Fatalf("read model %s %q tags = %#v, want %q and v1", surface, name, declaration.Tags, tag)
			}
			return
		}
	}
	t.Fatalf("read models missing %s %q: %#v", surface, name, declarations)
}

func v1FixtureHasMigration(declarations []MigrationContractDeclaration, surface, name string) bool {
	for _, declaration := range declarations {
		if declaration.Surface == surface && declaration.Name == name {
			return true
		}
	}
	return false
}

func v1FixtureHasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func assertGoldenBytes(t *testing.T, path string, got []byte) {
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
