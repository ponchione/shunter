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

func TestRuntimeExportContractJSONDocumentsDefaultSnapshotFilename(t *testing.T) {
	if DefaultContractSnapshotFilename != "shunter.contract.json" {
		t.Fatalf("DefaultContractSnapshotFilename = %q, want shunter.contract.json", DefaultContractSnapshotFilename)
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
