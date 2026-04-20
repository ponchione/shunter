package schema

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func validBuilder() *Builder {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
	})
	return b
}

func TestBuildValid(t *testing.T) {
	e, err := validBuilder().Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil Engine")
	}
}

func TestBuildReturnsValidationErrors(t *testing.T) {
	b := NewBuilder() // no version, no tables
	_, err := b.Build(EngineOptions{})
	if err == nil {
		t.Fatal("Build should fail without version and tables")
	}
}

func TestBuildSecondCallFails(t *testing.T) {
	b := validBuilder()
	_, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Build(EngineOptions{})
	if !errors.Is(err, ErrAlreadyBuilt) {
		t.Fatalf("second Build should return ErrAlreadyBuilt, got %v", err)
	}
}

func TestBuildAssignsTableIDs(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "alpha",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64, PrimaryKey: true}},
	})
	b.TableDef(TableDefinition{
		Name:    "beta",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64, PrimaryKey: true}},
	})

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := e.Registry()

	ids := reg.Tables()
	// 2 user + 2 system = 4
	if len(ids) != 4 {
		t.Fatalf("expected 4 tables, got %d", len(ids))
	}

	// User tables first.
	alpha, ok := reg.Table(TableID(0))
	if !ok || alpha.Name != "alpha" {
		t.Fatal("TableID 0 should be alpha")
	}
	beta, ok := reg.Table(TableID(1))
	if !ok || beta.Name != "beta" {
		t.Fatal("TableID 1 should be beta")
	}
}

func TestBuildSystemTablesExist(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	sc, ok := reg.TableByName("sys_clients")
	if !ok {
		t.Fatal("sys_clients should exist")
	}
	if len(sc.Columns) != 3 {
		t.Fatalf("sys_clients should have 3 columns, got %d", len(sc.Columns))
	}

	ss, ok := reg.TableByName("sys_scheduled")
	if !ok {
		t.Fatal("sys_scheduled should exist")
	}
	if len(ss.Columns) != 5 {
		t.Fatalf("sys_scheduled should have 5 columns, got %d", len(ss.Columns))
	}
}

func TestBuildSynthesizesPKIndex(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()
	ts, _ := reg.TableByName("players")
	pk, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("players should have a primary index")
	}
	if !pk.Primary || !pk.Unique {
		t.Fatal("PK index should be primary and unique")
	}
	if len(pk.Columns) != 1 || pk.Columns[0] != 0 {
		t.Fatal("PK index should reference column 0")
	}
}

func TestBuildNoPKNoPrimaryIndex(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "logs",
		Columns: []ColumnDefinition{{Name: "msg", Type: KindString}},
	})
	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := e.Registry().TableByName("logs")
	_, ok := ts.PrimaryIndex()
	if ok {
		t.Fatal("table without PK column should have no primary index")
	}
}

func TestBuildReflectionPathIntegration(t *testing.T) {
	type Player struct {
		ID    uint64 `shunter:"primarykey,autoincrement"`
		Name  string `shunter:"index"`
		Email string `shunter:"unique"`
	}
	b := NewBuilder()
	b.SchemaVersion(1)
	if err := RegisterTable[Player](b); err != nil {
		t.Fatal(err)
	}
	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	ts, ok := e.Registry().TableByName("player")
	if !ok {
		t.Fatal("player table should exist")
	}
	if len(ts.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ts.Columns))
	}
	pk, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("should have PK index")
	}
	if pk.ID != 0 {
		t.Fatal("PK index should be IndexID 0")
	}
}

// Validation tests

func TestBuildMissingVersion(t *testing.T) {
	b := NewBuilder()
	b.TableDef(TableDefinition{
		Name:    "t",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrSchemaVersionNotSet) {
		t.Fatalf("expected ErrSchemaVersionNotSet, got %v", err)
	}
}

func TestBuildNoTables(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNoTables) {
		t.Fatalf("expected ErrNoTables, got %v", err)
	}
}

func TestBuildReservedTableName(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "sys_clients",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrReservedTableName) {
		t.Fatalf("expected ErrReservedTableName, got %v", err)
	}
}

func TestBuildDuplicateTableName(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "t",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64}},
	})
	b.TableDef(TableDefinition{
		Name:    "t",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrDuplicateTableName) {
		t.Fatalf("expected ErrDuplicateTableName, got %v", err)
	}
}

func TestBuildDuplicatePK(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "t",
		Columns: []ColumnDefinition{
			{Name: "a", Type: KindUint64, PrimaryKey: true},
			{Name: "b", Type: KindUint64, PrimaryKey: true},
		},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrDuplicatePrimaryKey) {
		t.Fatalf("expected ErrDuplicatePrimaryKey, got %v", err)
	}
}

func TestBuildAutoIncrementOnString(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "t",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindString, PrimaryKey: true, AutoIncrement: true},
		},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrAutoIncrementType) {
		t.Fatalf("expected ErrAutoIncrementType, got %v", err)
	}
}

func TestBuildDuplicateReducerName(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "t",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64}},
	})
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	b.Reducer("Foo", h).Reducer("Foo", h)
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrDuplicateReducerName) {
		t.Fatalf("expected ErrDuplicateReducerName, got %v", err)
	}
}

func TestBuildReducerReservedName(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "t",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64}},
	})
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	b.Reducer("OnConnect", h)
	_, err := b.Build(EngineOptions{})
	if err == nil {
		t.Fatal("expected error for reserved reducer name")
	}
}

// Registry tests

func TestRegistryTableLookup(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	ts, ok := reg.Table(TableID(0))
	if !ok || ts.Name != "players" {
		t.Fatal("Table(0) should return players")
	}

	_, ok = reg.Table(TableID(99))
	if ok {
		t.Fatal("Table(99) should return false")
	}
}

func TestRegistryTableByName(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	ts, ok := reg.TableByName("players")
	if !ok || ts.ID != 0 {
		t.Fatal("TableByName('players') should return TableID 0")
	}

	_, ok = reg.TableByName("nonexistent")
	if ok {
		t.Fatal("TableByName('nonexistent') should return false")
	}
}

func TestRegistryTableByNameCaseInsensitive(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	ts, ok := reg.TableByName("PLAYERS")
	if !ok || ts.ID != 0 {
		t.Fatal("TableByName('PLAYERS') should return TableID 0")
	}
}

func TestRegistryTableLookupReturnsDetachedCopy(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	ts, ok := reg.Table(TableID(0))
	if !ok {
		t.Fatal("Table(0) should return players")
	}
	originalIndexName := ts.Indexes[0].Name
	ts.Name = "mutated"
	ts.Columns[0].Name = "mutated_col"
	ts.Indexes[0].Name = "mutated_idx"

	again, ok := reg.Table(TableID(0))
	if !ok {
		t.Fatal("Table(0) should still exist")
	}
	if again.Name != "players" {
		t.Fatalf("Table(0).Name = %q, want players", again.Name)
	}
	if again.Columns[0].Name != "id" {
		t.Fatalf("Table(0).Columns[0].Name = %q, want id", again.Columns[0].Name)
	}
	if again.Indexes[0].Name != originalIndexName {
		t.Fatalf("Table(0).Indexes[0].Name = %q, want %q", again.Indexes[0].Name, originalIndexName)
	}
}

func TestRegistryTableByNameReturnsDetachedCopy(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	ts, ok := reg.TableByName("players")
	if !ok {
		t.Fatal("TableByName(players) should exist")
	}
	ts.Name = "mutated"
	ts.Columns[1].Name = "mutated_name"
	ts.Indexes[0].Columns[0] = 99

	again, ok := reg.TableByName("players")
	if !ok {
		t.Fatal("TableByName(players) should still exist")
	}
	if again.Name != "players" {
		t.Fatalf("TableByName(players).Name = %q, want players", again.Name)
	}
	if again.Columns[1].Name != "name" {
		t.Fatalf("TableByName(players).Columns[1].Name = %q, want name", again.Columns[1].Name)
	}
	if again.Indexes[0].Columns[0] != 0 {
		t.Fatalf("TableByName(players).Indexes[0].Columns[0] = %d, want 0", again.Indexes[0].Columns[0])
	}
}

func TestRegistryTablesReturnsFreshSlice(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	reg := e.Registry()

	a := reg.Tables()
	b2 := reg.Tables()
	a[0] = TableID(999)
	if b2[0] == TableID(999) {
		t.Fatal("Tables() should return a fresh slice")
	}
}

func TestRegistryVersion(t *testing.T) {
	e, _ := validBuilder().Build(EngineOptions{})
	if e.Registry().Version() != 1 {
		t.Fatalf("expected version 1, got %d", e.Registry().Version())
	}
}

func TestRegistryReducer(t *testing.T) {
	b := validBuilder()
	called := false
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) {
		called = true
		return nil, nil
	})
	b.Reducer("Test", h)
	e, _ := b.Build(EngineOptions{})
	reg := e.Registry()

	fn, ok := reg.Reducer("Test")
	if !ok || fn == nil {
		t.Fatal("Reducer('Test') should be found")
	}
	fn(nil, nil) //nolint:errcheck
	if !called {
		t.Fatal("handler should be callable")
	}

	_, ok = reg.Reducer("Missing")
	if ok {
		t.Fatal("Reducer('Missing') should return false")
	}
}

func TestRegistryLifecycle(t *testing.T) {
	b := validBuilder()
	b.OnConnect(func(_ *types.ReducerContext) error { return nil })
	e, _ := b.Build(EngineOptions{})
	reg := e.Registry()

	if reg.OnConnect() == nil {
		t.Fatal("OnConnect should not be nil")
	}
	if reg.OnDisconnect() != nil {
		t.Fatal("OnDisconnect should be nil when not registered")
	}
}
