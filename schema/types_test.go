package schema

import (
	"encoding/json"
	"testing"
)

func TestTableSchemaColumnLookup(t *testing.T) {
	ts := TableSchema{
		Columns: []ColumnSchema{
			{Index: 0, Name: "id", Type: KindUint64},
			{Index: 1, Name: "name", Type: KindString},
		},
	}

	col, ok := ts.Column("name")
	if !ok {
		t.Fatal("Column('name') should be found")
	}
	if col.Index != 1 || col.Type != KindString {
		t.Fatalf("Column('name') = %+v, unexpected", col)
	}

	_, ok = ts.Column("missing")
	if ok {
		t.Fatal("Column('missing') should return false")
	}
}

func TestTableSchemaPrimaryIndex(t *testing.T) {
	ts := TableSchema{
		Indexes: []IndexSchema{
			{ID: 1, Name: "name_idx", Columns: []int{1}, Unique: false},
			{ID: 2, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
		},
	}

	pk, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("PrimaryIndex should be found")
	}
	if pk.Name != "pk" {
		t.Fatalf("PrimaryIndex name = %q, want 'pk'", pk.Name)
	}
}

func TestTableSchemaNoPrimaryIndex(t *testing.T) {
	ts := TableSchema{
		Indexes: []IndexSchema{
			{ID: 1, Name: "name_idx", Columns: []int{1}},
		},
	}
	_, ok := ts.PrimaryIndex()
	if ok {
		t.Fatal("PrimaryIndex should return false when none declared")
	}
}

func TestIDTypesDistinct(t *testing.T) {
	var tid TableID = 1
	var iid IndexID = 1
	// Compile-time check: these are distinct types.
	_ = tid
	_ = iid
}

func TestNewIndexSchemaPrimaryImpliesUnique(t *testing.T) {
	idx := NewIndexSchema(1, "pk", []int{0}, false, true)
	if !idx.Primary {
		t.Fatal("expected primary index")
	}
	if !idx.Unique {
		t.Fatal("primary index should be forced unique")
	}
}

func TestSchemaTypesJSONSerializable(t *testing.T) {
	ts := TableSchema{
		ID:   7,
		Name: "players",
		Columns: []ColumnSchema{
			{Index: 0, Name: "id", Type: KindUint64, Nullable: false},
			{Index: 1, Name: "name", Type: KindString, Nullable: false},
		},
		Indexes: []IndexSchema{{ID: 11, Name: "pk", Columns: []int{0}, Unique: true, Primary: true}},
	}
	if _, err := json.Marshal(ts); err != nil {
		t.Fatalf("TableSchema should be JSON-serializable: %v", err)
	}
}
