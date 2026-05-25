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

func TestCheckSchemaCompatibilityVersionMismatchWithoutStructuralChangeIsAdditive(t *testing.T) {
	e := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, e.Registry())
	snapshot.Version++

	if err := CheckSchemaCompatibility(e.Registry(), snapshot); err != nil {
		t.Fatalf("version-only mismatch should be additive-compatible: %v", err)
	}
	report := AnalyzeSchemaCompatibility(e.Registry(), snapshot)
	if !report.Compatible || report.Status != SchemaCompatibilityAdditive {
		t.Fatalf("compatibility report = %#v, want additive compatible", report)
	}
	if len(report.Changes) != 1 || report.Changes[0].Kind != SchemaCompatibilityChangeSchemaVersion {
		t.Fatalf("compatibility changes = %#v, want schema version change", report.Changes)
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

func TestCheckSchemaCompatibilityEventKindMismatch(t *testing.T) {
	e := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, e.Registry())
	snapshot.Tables[0].IsEvent = !snapshot.Tables[0].IsEvent

	err := CheckSchemaCompatibility(e.Registry(), snapshot)
	if err == nil {
		t.Fatal("event kind mismatch should fail")
	}
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError, got %T", err)
	}
	if !strings.Contains(err.Error(), "kind mismatch") {
		t.Fatalf("event kind mismatch detail missing from %q", err)
	}
}

func TestCheckSchemaCompatibilityNilSnapshotIsCompatible(t *testing.T) {
	e := buildCompatibilityEngine(t)
	if err := CheckSchemaCompatibility(e.Registry(), nil); err != nil {
		t.Fatalf("nil snapshot should be compatible: %v", err)
	}
}

func TestCheckSchemaCompatibilityAddedNonUniqueIndexIsAdditive(t *testing.T) {
	base := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, base.Registry())

	b := NewBuilder()
	b.SchemaVersion(8)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{
			{Name: "name_idx", Columns: []string{"name"}},
			{Name: "name_scan_idx", Columns: []string{"name"}},
		},
	})
	current, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckSchemaCompatibility(current.Registry(), snapshot); err != nil {
		t.Fatalf("safe additive schema should be compatible: %v", err)
	}
	report := AnalyzeSchemaCompatibility(current.Registry(), snapshot)
	if !report.Compatible || report.Status != SchemaCompatibilityAdditive {
		t.Fatalf("compatibility report = %#v, want additive compatible", report)
	}
	if len(report.Changes) != 2 {
		t.Fatalf("compatibility changes = %#v, want version and index", report.Changes)
	}
}

func TestCheckSchemaCompatibilityAddedTableAndNonUniqueIndexAreAdditive(t *testing.T) {
	base := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, base.Registry())

	b := NewBuilder()
	b.SchemaVersion(8)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{
			{Name: "name_idx", Columns: []string{"name"}},
			{Name: "name_scan_idx", Columns: []string{"name"}},
		},
	})
	b.TableDef(TableDefinition{
		Name: "audit_events",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "message", Type: KindString},
		},
	})
	current, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckSchemaCompatibility(current.Registry(), snapshot); err != nil {
		t.Fatalf("safe added table and index should be compatible: %v", err)
	}
	report := AnalyzeSchemaCompatibility(current.Registry(), snapshot)
	if !report.Compatible || report.Status != SchemaCompatibilityAdditive {
		t.Fatalf("compatibility report = %#v, want additive", report)
	}
	if len(report.Changes) != 3 {
		t.Fatalf("compatibility changes = %#v, want version, index, and table", report.Changes)
	}
	reconciled, _ := ReconcileRegistryForSnapshot(current.Registry(), snapshot)
	if id, _, ok := reconciled.TableByName("audit_events"); !ok || id <= 2 {
		t.Fatalf("reconciled audit_events id = %d ok=%t, want fresh id above snapshot tables", id, ok)
	}
	if id, _, ok := reconciled.TableByName("sys_clients"); !ok || id != 1 {
		t.Fatalf("reconciled sys_clients id = %d ok=%t, want snapshot id 1", id, ok)
	}
}

func TestCheckSchemaCompatibilityAddedColumnAndUniqueIndexAreBlocked(t *testing.T) {
	base := buildCompatibilityEngine(t)
	snapshot := snapshotFromRegistry(t, base.Registry())

	b := NewBuilder()
	b.SchemaVersion(8)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
			{Name: "nickname", Type: KindString, Nullable: true},
		},
		Indexes: []IndexDefinition{
			{Name: "name_idx", Columns: []string{"name"}},
			{Name: "nickname_unique_idx", Columns: []string{"nickname"}, Unique: true},
		},
	})
	current, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	err = CheckSchemaCompatibility(current.Registry(), snapshot)
	if err == nil {
		t.Fatal("row-shape and unique-index changes should be blocked")
	}
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError, got %T", err)
	}
	report := AnalyzeSchemaCompatibility(current.Registry(), snapshot)
	if report.Compatible || report.Status != SchemaCompatibilityBlocked {
		t.Fatalf("compatibility report = %#v, want blocked", report)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("compatibility issues = %#v, want column and unique-index issues", report.Issues)
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
				Columns: []ColumnSchema{{Index: 0, Name: "other_id", Type: KindUint64}},
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
