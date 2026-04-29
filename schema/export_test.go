package schema

import (
	"encoding/json"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestExportSchemaIncludesTablesReducersAndLifecycle(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(5)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{{Name: "name_idx", Columns: []string{"name"}, Unique: true}},
	})
	b.Reducer("CreatePlayer", func(*ReducerContext, []byte) ([]byte, error) { return nil, nil })
	b.OnConnect(func(*ReducerContext) error { return nil })

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	export := e.ExportSchema()
	if export.Version != 5 {
		t.Fatalf("ExportSchema version = %d, want 5", export.Version)
	}
	if len(export.Tables) != 3 {
		t.Fatalf("ExportSchema tables = %d, want 3 (user + system tables)", len(export.Tables))
	}
	if export.Tables[0].Name != "players" {
		t.Fatalf("first exported table = %q, want players", export.Tables[0].Name)
	}
	if export.Tables[0].Columns[0].Type != "uint64" || export.Tables[0].Columns[1].Type != "string" {
		t.Fatalf("column export types = %+v", export.Tables[0].Columns)
	}
	if export.Tables[0].Indexes[0].Columns[0] != "id" {
		t.Fatalf("primary index column export = %v, want [id]", export.Tables[0].Indexes[0].Columns)
	}
	if !export.Tables[0].Indexes[0].Primary || !export.Tables[0].Indexes[0].Unique {
		t.Fatalf("primary index export flags = %+v", export.Tables[0].Indexes[0])
	}
	if export.Reducers[0] != (ReducerExport{Name: "CreatePlayer", Lifecycle: false}) {
		t.Fatalf("first reducer export = %+v", export.Reducers[0])
	}
	if export.Reducers[1] != (ReducerExport{Name: "OnConnect", Lifecycle: true}) {
		t.Fatalf("lifecycle reducer export = %+v", export.Reducers[1])
	}
}

func TestExportSchemaIncludesExtendedColumnKinds(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "extended",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "i128", Type: KindInt128},
			{Name: "u128", Type: KindUint128},
			{Name: "i256", Type: KindInt256},
			{Name: "u256", Type: KindUint256},
			{Name: "created_at", Type: KindTimestamp},
			{Name: "tags", Type: KindArrayString},
		},
	})

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	export := e.ExportSchema()
	got := map[string]string{}
	for _, column := range export.Tables[0].Columns {
		got[column.Name] = column.Type
	}
	want := map[string]string{
		"id":         "uint64",
		"i128":       "int128",
		"u128":       "uint128",
		"i256":       "int256",
		"u256":       "uint256",
		"created_at": "timestamp",
		"tags":       "arrayString",
	}
	for name, wantType := range want {
		if got[name] != wantType {
			t.Fatalf("exported column %q type = %q, want %q; all columns = %#v", name, got[name], wantType, export.Tables[0].Columns)
		}
	}
}

func TestExportSchemaIncludesTableReadPolicy(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "messages",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
		},
	}, WithReadPermissions("messages:read"))

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	export := e.ExportSchema()
	if len(export.Tables) == 0 {
		t.Fatal("ExportSchema returned no tables")
	}
	policy := export.Tables[0].ReadPolicy
	if policy.Access != TableAccessPermissioned {
		t.Fatalf("export read access = %s, want permissioned", policy.Access)
	}
	if len(policy.Permissions) != 1 || policy.Permissions[0] != "messages:read" {
		t.Fatalf("export read permissions = %#v, want [messages:read]", policy.Permissions)
	}

	export.Tables[0].ReadPolicy.Permissions[0] = "mutated"
	again := e.ExportSchema()
	if got := again.Tables[0].ReadPolicy.Permissions[0]; got != "messages:read" {
		t.Fatalf("second export read permission = %q, want detached copy", got)
	}
}

func TestSchemaExportJSONRoundTripIncludesTableReadPolicy(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "messages",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
		},
	}, WithPublicRead())

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	data, err := json.Marshal(e.ExportSchema())
	if err != nil {
		t.Fatalf("Marshal export: %v", err)
	}
	var decoded SchemaExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal export: %v", err)
	}
	if decoded.Tables[0].ReadPolicy.Access != TableAccessPublic {
		t.Fatalf("decoded read access = %s, want public; json=%s", decoded.Tables[0].ReadPolicy.Access, data)
	}
}

func TestExportSchemaReturnsDetachedSnapshot(t *testing.T) {
	e, err := validBuilder().Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	export := e.ExportSchema()
	export.Tables[0].Name = "mutated"
	export.Tables[0].Columns[0].Name = "mutated_col"
	export.Tables[0].Indexes[0].Columns[0] = "mutated_idx_col"
	export.Reducers = append(export.Reducers, ReducerExport{Name: "Mutated", Lifecycle: false})

	again := e.ExportSchema()
	if again.Tables[0].Name != "players" {
		t.Fatalf("detached table name = %q, want players", again.Tables[0].Name)
	}
	if again.Tables[0].Columns[0].Name != "id" {
		t.Fatalf("detached column name = %q, want id", again.Tables[0].Columns[0].Name)
	}
	if again.Tables[0].Indexes[0].Columns[0] != "id" {
		t.Fatalf("detached index columns = %v, want [id]", again.Tables[0].Indexes[0].Columns)
	}
}

func TestSchemaExportJSONRoundTrip(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "files",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "payload", Type: KindBytes},
		},
	})
	b.Reducer("Upload", func(*ReducerContext, []byte) ([]byte, error) { return []byte("ok"), nil })
	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(e.ExportSchema())
	if err != nil {
		t.Fatalf("Marshal export: %v", err)
	}
	var decoded SchemaExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal export: %v", err)
	}
	if decoded.Version != 1 {
		t.Fatalf("decoded version = %d, want 1", decoded.Version)
	}
	if decoded.Tables[0].Columns[1].Type != ValueKindExportString(types.KindBytes) {
		t.Fatalf("decoded bytes column type = %q, want bytes", decoded.Tables[0].Columns[1].Type)
	}
}
