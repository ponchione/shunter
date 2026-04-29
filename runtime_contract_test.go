package shunter

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRuntimeExportContractIncludesModuleSchemaDeclarationsAndReservedSections(t *testing.T) {
	rt := buildContractRuntime(t)

	contract := rt.ExportContract()
	if contract.ContractVersion != ModuleContractVersion {
		t.Fatalf("ContractVersion = %d, want %d", contract.ContractVersion, ModuleContractVersion)
	}
	if contract.Module.Name != "chat" {
		t.Fatalf("module name = %q, want chat", contract.Module.Name)
	}
	if contract.Module.Version != "v1.2.3" {
		t.Fatalf("module version = %q, want v1.2.3", contract.Module.Version)
	}
	if got := contract.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("module metadata team = %q, want runtime", got)
	}
	if contract.Schema.Version != 1 {
		t.Fatalf("schema version = %d, want 1", contract.Schema.Version)
	}
	if !hasTableExport(contract.Schema.Tables, "messages") {
		t.Fatalf("tables = %#v, want messages table", contract.Schema.Tables)
	}
	if !hasReducerExport(contract.Schema.Reducers, "send_message", false) {
		t.Fatalf("reducers = %#v, want send_message reducer", contract.Schema.Reducers)
	}
	if !hasReducerExport(contract.Schema.Reducers, "OnConnect", true) {
		t.Fatalf("reducers = %#v, want OnConnect lifecycle reducer", contract.Schema.Reducers)
	}
	assertQueryDescription(t, contract.Queries, "recent_messages")
	assertViewDescription(t, contract.Views, "live_messages")
	if contract.Permissions.Reducers == nil || len(contract.Permissions.Reducers) != 0 {
		t.Fatalf("permission reducers = %#v, want reserved empty slice", contract.Permissions.Reducers)
	}
	if contract.Permissions.Queries == nil || len(contract.Permissions.Queries) != 0 {
		t.Fatalf("permission queries = %#v, want reserved empty slice", contract.Permissions.Queries)
	}
	if contract.Permissions.Views == nil || len(contract.Permissions.Views) != 0 {
		t.Fatalf("permission views = %#v, want reserved empty slice", contract.Permissions.Views)
	}
	if contract.ReadModel.Declarations == nil || len(contract.ReadModel.Declarations) != 0 {
		t.Fatalf("read model declarations = %#v, want reserved empty slice", contract.ReadModel.Declarations)
	}
	if contract.Migrations.Declarations == nil || len(contract.Migrations.Declarations) != 0 {
		t.Fatalf("migration declarations = %#v, want reserved empty slice", contract.Migrations.Declarations)
	}
	if contract.Codegen.ContractFormat != ModuleContractFormat {
		t.Fatalf("codegen contract format = %q, want %q", contract.Codegen.ContractFormat, ModuleContractFormat)
	}
	if contract.Codegen.ContractVersion != ModuleContractVersion {
		t.Fatalf("codegen contract version = %d, want %d", contract.Codegen.ContractVersion, ModuleContractVersion)
	}
	if contract.Codegen.DefaultSnapshotFilename != DefaultContractSnapshotFilename {
		t.Fatalf("codegen default snapshot = %q, want %q", contract.Codegen.DefaultSnapshotFilename, DefaultContractSnapshotFilename)
	}
}

func TestRuntimeExportContractIncludesPermissionAndReadModelMetadata(t *testing.T) {
	mod := validChatModule().
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	assertPermissionContractDeclaration(t, contract.Permissions.Reducers, "send_message", "messages:send")
	assertPermissionContractDeclaration(t, contract.Permissions.Queries, "recent_messages", "messages:read")
	assertPermissionContractDeclaration(t, contract.Permissions.Views, "live_messages", "messages:subscribe")
	assertReadModelContractDeclaration(t, contract.ReadModel.Declarations, ReadModelSurfaceQuery, "recent_messages", "messages", "history")
	assertReadModelContractDeclaration(t, contract.ReadModel.Declarations, ReadModelSurfaceView, "live_messages", "messages", "realtime")
}

func TestRuntimeExportContractIncludesDeclarationSQLMetadata(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT id FROM messages WHERE body = 'hello' LIMIT 1",
		}).
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages WHERE body = 'hello'",
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	assertQuerySQL(t, contract.Queries, "recent_messages", "SELECT id FROM messages WHERE body = 'hello' LIMIT 1")
	assertViewSQL(t, contract.Views, "live_messages", "SELECT * FROM messages WHERE body = 'hello'")

	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	var decoded ModuleContract
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	assertQuerySQL(t, decoded.Queries, "recent_messages", "SELECT id FROM messages WHERE body = 'hello' LIMIT 1")
	assertViewSQL(t, decoded.Views, "live_messages", "SELECT * FROM messages WHERE body = 'hello'")
}

func TestRuntimeExportContractReturnsDetachedSnapshot(t *testing.T) {
	rt := buildContractRuntime(t)

	contract := rt.ExportContract()
	contract.Module.Metadata["team"] = "mutated"
	contract.Schema.Tables[0].Name = "mutated_table"
	contract.Schema.Tables[0].Columns[0].Name = "mutated_column"
	contract.Schema.Tables[0].Indexes[0].Columns[0] = "mutated_index_column"
	contract.Schema.Reducers[0].Name = "mutated_reducer"
	contract.Queries[0].Name = "mutated_query"
	contract.Views[0].Name = "mutated_view"
	contract.Permissions.Reducers = append(contract.Permissions.Reducers, PermissionContractDeclaration{Name: "mutated"})
	contract.ReadModel.Declarations = append(contract.ReadModel.Declarations, ReadModelContractDeclaration{Name: "mutated"})
	contract.Migrations.Declarations = append(contract.Migrations.Declarations, MigrationContractDeclaration{Name: "mutated"})

	again := rt.ExportContract()
	if got := again.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("second contract metadata team = %q, want runtime", got)
	}
	if !hasTableExport(again.Schema.Tables, "messages") {
		t.Fatalf("second contract tables = %#v, want messages table", again.Schema.Tables)
	}
	if !hasReducerExport(again.Schema.Reducers, "send_message", false) {
		t.Fatalf("second contract reducers = %#v, want send_message reducer", again.Schema.Reducers)
	}
	assertQueryDescription(t, again.Queries, "recent_messages")
	assertViewDescription(t, again.Views, "live_messages")
	if len(again.Permissions.Reducers) != 0 {
		t.Fatalf("second contract permission reducers = %#v, want empty", again.Permissions.Reducers)
	}
	if len(again.ReadModel.Declarations) != 0 {
		t.Fatalf("second contract read model declarations = %#v, want empty", again.ReadModel.Declarations)
	}
	if len(again.Migrations.Declarations) != 0 {
		t.Fatalf("second contract migration declarations = %#v, want empty", again.Migrations.Declarations)
	}
}

func TestRuntimeExportContractMetadataReturnsDetachedSnapshot(t *testing.T) {
	mod := validChatModule().
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	contract.Permissions.Reducers[0].Required[0] = "mutated"
	contract.Permissions.Queries[0].Required = append(contract.Permissions.Queries[0].Required, "mutated")
	contract.ReadModel.Declarations[0].Tables[0] = "mutated"
	contract.ReadModel.Declarations[0].Tags = append(contract.ReadModel.Declarations[0].Tags, "mutated")

	again := rt.ExportContract()
	assertPermissionContractDeclaration(t, again.Permissions.Reducers, "send_message", "messages:send")
	assertPermissionContractDeclaration(t, again.Permissions.Queries, "recent_messages", "messages:read")
	assertReadModelContractDeclaration(t, again.ReadModel.Declarations, ReadModelSurfaceQuery, "recent_messages", "messages", "history")
}

func TestRuntimeExportContractWorksAcrossLifecycle(t *testing.T) {
	rt := buildContractRuntime(t)

	beforeStart := rt.ExportContract()
	assertQueryDescription(t, beforeStart.Queries, "recent_messages")

	if err := rt.Start(t.Context()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	afterClose := rt.ExportContract()
	if afterClose.Module.Name != "chat" {
		t.Fatalf("module name after close = %q, want chat", afterClose.Module.Name)
	}
	if !hasTableExport(afterClose.Schema.Tables, "messages") {
		t.Fatalf("tables after close = %#v, want messages table", afterClose.Schema.Tables)
	}
}

func TestRuntimeExportContractJSONIsDeterministicAndRoundTrips(t *testing.T) {
	rt := buildContractRuntime(t)

	first, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	second, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("second ExportContractJSON returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("ExportContractJSON was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatalf("ExportContractJSON = %q, want trailing newline", first)
	}

	var decoded ModuleContract
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	if decoded.Module.Name != "chat" {
		t.Fatalf("decoded module name = %q, want chat", decoded.Module.Name)
	}
	if !hasTableExport(decoded.Schema.Tables, "messages") {
		t.Fatalf("decoded tables = %#v, want messages table", decoded.Schema.Tables)
	}
	assertQueryDescription(t, decoded.Queries, "recent_messages")
	assertViewDescription(t, decoded.Views, "live_messages")
	if decoded.Codegen.DefaultSnapshotFilename != DefaultContractSnapshotFilename {
		t.Fatalf("decoded default snapshot = %q, want %q", decoded.Codegen.DefaultSnapshotFilename, DefaultContractSnapshotFilename)
	}
}

func TestRuntimeExportContractJSONUsesCanonicalDeclarationKeys(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT * FROM messages",
		}).
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages",
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	data, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	assertCanonicalDeclarationKeys(t, topLevel["queries"], "recent_messages", "SELECT * FROM messages")
	assertCanonicalDeclarationKeys(t, topLevel["views"], "live_messages", "SELECT * FROM messages")
}

func TestModuleContractJSONAcceptsLegacyDeclarationKeys(t *testing.T) {
	data := []byte(`{
  "contract_version": 1,
  "module": {"name": "chat", "version": "v1.0.0", "metadata": {}},
  "schema": {"version": 1, "tables": [], "reducers": []},
  "queries": [{"Name": "recent_messages", "SQL": "SELECT * FROM messages"}],
  "views": [{"Name": "live_messages", "SQL": "SELECT * FROM messages"}],
  "permissions": {"reducers": [], "queries": [], "views": []},
  "read_model": {"declarations": []},
  "migrations": {"module": {"classifications": []}, "declarations": []},
  "codegen": {
    "contract_format": "shunter.module_contract",
    "contract_version": 1,
    "default_snapshot_filename": "shunter.contract.json"
  }
}`)

	var contract ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("Unmarshal legacy contract JSON: %v", err)
	}
	assertQuerySQL(t, contract.Queries, "recent_messages", "SELECT * FROM messages")
	assertViewSQL(t, contract.Views, "live_messages", "SELECT * FROM messages")
}

func TestRuntimeExportContractJSONDocumentsDefaultSnapshotFilename(t *testing.T) {
	if DefaultContractSnapshotFilename != "shunter.contract.json" {
		t.Fatalf("DefaultContractSnapshotFilename = %q, want shunter.contract.json", DefaultContractSnapshotFilename)
	}
}

func TestRuntimeExportContractOmitsProcessBoundaryMetadata(t *testing.T) {
	rt := buildContractRuntime(t)

	data, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	for _, key := range []string{
		"process_boundary",
		"processBoundary",
		"invocation_protocol",
		"out_of_process",
	} {
		if _, ok := topLevel[key]; ok {
			t.Fatalf("contract JSON unexpectedly included %q: %s", key, data)
		}
	}
}

func buildContractRuntime(t *testing.T) *Runtime {
	t.Helper()
	mod := validChatModule().
		Version("v1.2.3").
		Metadata(map[string]string{"team": "runtime"}).
		Reducer("send_message", noopReducer).
		OnConnect(noopLifecycle).
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt
}

func assertPermissionContractDeclaration(t *testing.T, declarations []PermissionContractDeclaration, name, required string) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Name != name {
			continue
		}
		if len(declaration.Required) != 1 || declaration.Required[0] != required {
			t.Fatalf("permission declaration %q = %#v, want required %q", name, declaration, required)
		}
		return
	}
	t.Fatalf("permission declarations = %#v, want %q", declarations, name)
}

func assertReadModelContractDeclaration(t *testing.T, declarations []ReadModelContractDeclaration, surface, name, table, tag string) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Surface != surface || declaration.Name != name {
			continue
		}
		if len(declaration.Tables) != 1 || declaration.Tables[0] != table {
			t.Fatalf("read model declaration %q/%q tables = %#v, want %q", surface, name, declaration.Tables, table)
		}
		if len(declaration.Tags) != 1 || declaration.Tags[0] != tag {
			t.Fatalf("read model declaration %q/%q tags = %#v, want %q", surface, name, declaration.Tags, tag)
		}
		return
	}
	t.Fatalf("read model declarations = %#v, want %s %q", declarations, surface, name)
}

func assertQuerySQL(t *testing.T, queries []QueryDescription, name, sql string) {
	t.Helper()
	for _, query := range queries {
		if query.Name != name {
			continue
		}
		if query.SQL != sql {
			t.Fatalf("query %q SQL = %q, want %q", name, query.SQL, sql)
		}
		return
	}
	t.Fatalf("queries = %#v, want %q", queries, name)
}

func assertViewSQL(t *testing.T, views []ViewDescription, name, sql string) {
	t.Helper()
	for _, view := range views {
		if view.Name != name {
			continue
		}
		if view.SQL != sql {
			t.Fatalf("view %q SQL = %q, want %q", name, view.SQL, sql)
		}
		return
	}
	t.Fatalf("views = %#v, want %q", views, name)
}

func assertCanonicalDeclarationKeys(t *testing.T, raw json.RawMessage, name, sql string) {
	t.Helper()
	var declarations []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declarations); err != nil {
		t.Fatalf("Unmarshal declarations: %v", err)
	}
	if len(declarations) != 1 {
		t.Fatalf("declarations = %#v, want one declaration", declarations)
	}
	if _, ok := declarations[0]["Name"]; ok {
		t.Fatalf("declaration used legacy Name key: %s", raw)
	}
	if _, ok := declarations[0]["SQL"]; ok {
		t.Fatalf("declaration used legacy SQL key: %s", raw)
	}
	var gotName string
	if err := json.Unmarshal(declarations[0]["name"], &gotName); err != nil {
		t.Fatalf("Unmarshal declaration name: %v", err)
	}
	if gotName != name {
		t.Fatalf("declaration name = %q, want %q", gotName, name)
	}
	var gotSQL string
	if err := json.Unmarshal(declarations[0]["sql"], &gotSQL); err != nil {
		t.Fatalf("Unmarshal declaration sql: %v", err)
	}
	if gotSQL != sql {
		t.Fatalf("declaration sql = %q, want %q", gotSQL, sql)
	}
}
