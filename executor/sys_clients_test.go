package executor

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestSysClientsTableResolves(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := eng.Registry()

	ts, ok := SysClientsTable(reg)
	if !ok {
		t.Fatal("SysClientsTable should resolve on a freshly built registry")
	}
	if ts.Name != SysClientsTableName {
		t.Fatalf("name = %q, want %q", ts.Name, SysClientsTableName)
	}
	if len(ts.Columns) != 3 {
		t.Fatalf("columns = %d, want 3", len(ts.Columns))
	}
}

func TestSysClientsColumnLayout(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := eng.Registry()
	ts, ok := SysClientsTable(reg)
	if !ok {
		t.Fatal("expected sys_clients")
	}

	want := []struct {
		name string
		kind types.ValueKind
	}{
		{"connection_id", types.KindBytes},
		{"identity", types.KindBytes},
		{"connected_at", types.KindInt64},
	}
	for i, w := range want {
		if int(i) >= len(ts.Columns) {
			t.Fatalf("column %d missing", i)
		}
		col := ts.Columns[i]
		if col.Name != w.name {
			t.Errorf("col[%d].Name = %q, want %q", i, col.Name, w.name)
		}
		if col.Type != w.kind {
			t.Errorf("col[%d].Type = %v, want %v", i, col.Type, w.kind)
		}
	}
	// Primary-key info lives on the synthesized IndexSchema, not the column.
	var pkFound bool
	for _, idx := range ts.Indexes {
		if !idx.Primary {
			continue
		}
		pkFound = true
		if len(idx.Columns) != 1 || idx.Columns[0] != SysClientsColConnectionID {
			t.Errorf("pk index columns = %v, want [%d]", idx.Columns, SysClientsColConnectionID)
		}
	}
	if !pkFound {
		t.Error("sys_clients should have a primary-key index on connection_id")
	}
}
