package schema

import (
	"reflect"
	"testing"
)

func TestBuildTableDefinitionDefaultName(t *testing.T) {
	type PlayerSession struct {
		ID   uint64 `shunter:"primarykey"`
		Name string
	}
	fields, _ := discoverFields(reflect.TypeFor[PlayerSession](), "")
	def, err := buildTableDefinition("PlayerSession", fields)
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "player_session" {
		t.Fatalf("expected 'player_session', got %q", def.Name)
	}
}

func TestBuildTableDefinitionWithTableName(t *testing.T) {
	type X struct {
		ID uint64 `shunter:"primarykey"`
	}
	fields, _ := discoverFields(reflect.TypeFor[X](), "")
	def, err := buildTableDefinition("X", fields, WithTableName("sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "sessions" {
		t.Fatalf("expected 'sessions', got %q", def.Name)
	}
}

func TestBuildTableDefinitionPKFlags(t *testing.T) {
	type T struct {
		ID   uint64 `shunter:"primarykey,autoincrement"`
		Name string
	}
	fields, _ := discoverFields(reflect.TypeFor[T](), "")
	def, err := buildTableDefinition("T", fields)
	if err != nil {
		t.Fatal(err)
	}
	if !def.Columns[0].PrimaryKey || !def.Columns[0].AutoIncrement {
		t.Fatal("PK and AutoIncrement should be set")
	}
}

func TestBuildTableDefinitionPlainIndex(t *testing.T) {
	type T struct {
		ID   uint64 `shunter:"primarykey"`
		Name string `shunter:"index"`
	}
	fields, _ := discoverFields(reflect.TypeFor[T](), "")
	def, err := buildTableDefinition("T", fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Indexes) != 1 || def.Indexes[0].Name != "name_idx" {
		t.Fatalf("expected 'name_idx' index, got %+v", def.Indexes)
	}
}

func TestBuildTableDefinitionUniqueIndex(t *testing.T) {
	type T struct {
		ID    uint64 `shunter:"primarykey"`
		Email string `shunter:"unique"`
	}
	fields, _ := discoverFields(reflect.TypeFor[T](), "")
	def, err := buildTableDefinition("T", fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Indexes) != 1 || def.Indexes[0].Name != "email_uniq" || !def.Indexes[0].Unique {
		t.Fatalf("expected 'email_uniq' unique index, got %+v", def.Indexes)
	}
}

func TestBuildTableDefinitionCompositeNamedIndex(t *testing.T) {
	type T struct {
		ID      uint64 `shunter:"primarykey"`
		GuildID uint64 `shunter:"index:guild_score"`
		Score   int64  `shunter:"index:guild_score"`
	}
	fields, _ := discoverFields(reflect.TypeFor[T](), "")
	def, err := buildTableDefinition("T", fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Indexes) != 1 {
		t.Fatalf("expected 1 composite index, got %d", len(def.Indexes))
	}
	idx := def.Indexes[0]
	if idx.Name != "guild_score" || len(idx.Columns) != 2 {
		t.Fatalf("expected guild_score with 2 columns, got %+v", idx)
	}
	if idx.Columns[0] != "guild_id" || idx.Columns[1] != "score" {
		t.Fatalf("columns should be in field order: %v", idx.Columns)
	}
}

func TestBuildTableDefinitionMixedUniqueErrors(t *testing.T) {
	type T struct {
		ID uint64 `shunter:"primarykey"`
		A  uint64 `shunter:"unique,index:mixed"`
		B  uint64 `shunter:"index:mixed"`
	}
	fields, _ := discoverFields(reflect.TypeFor[T](), "")
	_, err := buildTableDefinition("T", fields)
	if err == nil {
		t.Fatal("mixed unique flags on same named composite index should error")
	}
}

func TestBuildTableDefinitionNoPKIndex(t *testing.T) {
	type T struct {
		ID   uint64 `shunter:"primarykey"`
		Name string
	}
	fields, _ := discoverFields(reflect.TypeFor[T](), "")
	def, err := buildTableDefinition("T", fields)
	if err != nil {
		t.Fatal(err)
	}
	// No explicit IndexDefinition for PK column.
	if len(def.Indexes) != 0 {
		t.Fatalf("expected 0 explicit indexes (PK synthesized at Build), got %d", len(def.Indexes))
	}
}

func TestRegisterTableExcludedEmbeddedFieldsOmitted(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	if err := RegisterTable[ExcludedEmbedded](b); err != nil {
		t.Fatalf("RegisterTable excluded embedded struct: %v", err)
	}
	eng, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, ts, ok := eng.Registry().TableByName("excluded_embedded")
	if !ok {
		t.Fatal("excluded_embedded table missing")
	}
	if len(ts.Columns) != 1 || ts.Columns[0].Name != "name" {
		t.Fatalf("excluded_embedded columns = %+v, want only name", ts.Columns)
	}
}

func TestRegisterTableAppliesOptionsOnce(t *testing.T) {
	type OptionOnce struct {
		ID uint64 `shunter:"primarykey"`
	}

	b := NewBuilder()
	b.SchemaVersion(1)

	calls := 0
	err := RegisterTable[OptionOnce](b, func(o *tableOptions) {
		calls++
		o.name = "option_once"
	})
	if err != nil {
		t.Fatalf("RegisterTable failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("RegisterTable applied option %d times, want 1", calls)
	}

	eng, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if _, _, ok := eng.Registry().TableByName("option_once"); !ok {
		t.Fatal("option_once table missing")
	}
}
