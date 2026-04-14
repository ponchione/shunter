package executor

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestSysScheduledTableResolves(t *testing.T) {
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

	ts, ok := SysScheduledTable(reg)
	if !ok {
		t.Fatal("SysScheduledTable should resolve on a freshly built registry")
	}
	if ts.Name != SysScheduledTableName {
		t.Fatalf("name = %q, want %q", ts.Name, SysScheduledTableName)
	}
	if len(ts.Columns) != 5 {
		t.Fatalf("columns = %d, want 5", len(ts.Columns))
	}
}

func TestSysScheduledColumnLayout(t *testing.T) {
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
	ts, ok := SysScheduledTable(reg)
	if !ok {
		t.Fatal("expected sys_scheduled")
	}

	want := []struct {
		pos  int
		name string
		kind types.ValueKind
	}{
		{SysScheduledColScheduleID, "schedule_id", types.KindUint64},
		{SysScheduledColReducerName, "reducer_name", types.KindString},
		{SysScheduledColArgs, "args", types.KindBytes},
		{SysScheduledColNextRunAtNs, "next_run_at_ns", types.KindInt64},
		{SysScheduledColRepeatNs, "repeat_ns", types.KindInt64},
	}
	for _, w := range want {
		if w.pos >= len(ts.Columns) {
			t.Fatalf("column %d (%s) missing", w.pos, w.name)
		}
		col := ts.Columns[w.pos]
		if col.Name != w.name {
			t.Errorf("col[%d].Name = %q, want %q", w.pos, col.Name, w.name)
		}
		if col.Type != w.kind {
			t.Errorf("col[%d].Type = %v, want %v", w.pos, col.Type, w.kind)
		}
	}

	// Primary key on schedule_id, plus autoincrement on the same column.
	var pkFound bool
	for _, idx := range ts.Indexes {
		if !idx.Primary {
			continue
		}
		pkFound = true
		if len(idx.Columns) != 1 || idx.Columns[0] != SysScheduledColScheduleID {
			t.Errorf("pk index columns = %v, want [%d]", idx.Columns, SysScheduledColScheduleID)
		}
	}
	if !pkFound {
		t.Error("sys_scheduled should have a primary-key index on schedule_id")
	}
	// AutoIncrement lives on the ColumnDefinition input (schema/system_tables.go)
	// and is consumed by schema.Build validation + store.Sequence wiring. It is
	// not re-exposed on the built ColumnSchema, so we can't assert it from here.
	// Coverage for the flag comes from schema/system_tables.go being the single
	// source of truth plus the Build-time validator in schema/validate_structure.go.
}
