package schema

import (
	"errors"
	"testing"
)

func TestBuildAcceptsNullableColumn(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "nickname", Type: KindString, Nullable: true},
		},
	})

	engine, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build nullable column: %v", err)
	}
	ts, ok := engine.Registry().Table(0)
	if !ok {
		t.Fatal("registered table missing")
	}
	if !ts.Columns[1].Nullable {
		t.Fatalf("nickname nullable = false, want true")
	}
}

func TestBuildRejectsNullableAutoIncrementColumn(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true, Nullable: true, AutoIncrement: true},
			{Name: "nickname", Type: KindString},
		},
	})

	_, err := b.Build(EngineOptions{})
	if !errors.Is(err, ErrNullableAutoIncrement) {
		t.Fatalf("expected ErrNullableAutoIncrement, got %v", err)
	}
}
