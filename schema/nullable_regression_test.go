package schema

import (
	"errors"
	"testing"
)

func TestBuildRejectsNullableColumn(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "nickname", Type: KindString, Nullable: true},
		},
	})

	_, err := b.Build(EngineOptions{})
	if !errors.Is(err, ErrNullableColumn) {
		t.Fatalf("expected ErrNullableColumn, got %v", err)
	}
}
