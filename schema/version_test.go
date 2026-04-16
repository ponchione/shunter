package schema

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func buildCompatibilityEngine(t *testing.T) *Engine {
	t.Helper()
	b := NewBuilder()
	b.SchemaVersion(7)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{{Name: "name_idx", Columns: []string{"name"}}},
	})
	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func snapshotFromRegistry(t *testing.T, reg SchemaRegistry) *SnapshotSchema {
	t.Helper()
	s := &SnapshotSchema{Version: reg.Version()}
	for _, tid := range reg.Tables() {
		ts, ok := reg.Table(tid)
		if !ok {
			t.Fatalf("missing table %d", tid)
		}
		s.Tables = append(s.Tables, *ts)
	}
	return s
}

func TestCheckSchemaCompatibilityMatchingSnapshot(t *testing.T) {
	e := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, e.Registry())
	if err := CheckSchemaCompatibility(e.Registry(), snapshot); err != nil {
		t.Fatalf("matching snapshot should be compatible: %v", err)
	}
}

func TestCheckSchemaCompatibilityVersionMismatch(t *testing.T) {
	e := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, e.Registry())
	snapshot.Version++

	err := CheckSchemaCompatibility(e.Registry(), snapshot)
	if err == nil {
		t.Fatal("version mismatch should fail")
	}
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError, got %T", err)
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("version mismatch detail missing from %q", err)
	}
}

func TestCheckSchemaCompatibilityStructuralMismatch(t *testing.T) {
	e := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, e.Registry())
	snapshot.Tables[0].Columns[1].Name = "display_name"

	err := CheckSchemaCompatibility(e.Registry(), snapshot)
	if err == nil {
		t.Fatal("structural mismatch should fail")
	}
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError, got %T", err)
	}
	if !strings.Contains(err.Error(), "display_name") {
		t.Fatalf("structural diff detail missing from %q", err)
	}
}

func TestCheckSchemaCompatibilityNilSnapshotIsCompatible(t *testing.T) {
	e := buildCompatibilityEngine(t)
	if err := CheckSchemaCompatibility(e.Registry(), nil); err != nil {
		t.Fatalf("nil snapshot should be compatible: %v", err)
	}
}

func TestEngineStartRunsCompatibilityCheck(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(3)
	b.TableDef(TableDefinition{
		Name:    "players",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64, PrimaryKey: true}},
	})
	e, err := b.Build(EngineOptions{
		StartupSnapshotSchema: &SnapshotSchema{
			Version: 99,
			Tables: []TableSchema{{
				ID:      0,
				Name:    "players",
				Columns: []ColumnSchema{{Index: 0, Name: "id", Type: KindUint64}},
				Indexes: []IndexSchema{{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = e.Start(context.Background())
	if err == nil {
		t.Fatal("Start should fail on incompatible snapshot schema")
	}
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError from Start, got %T", err)
	}
}
