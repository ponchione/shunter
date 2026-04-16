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
