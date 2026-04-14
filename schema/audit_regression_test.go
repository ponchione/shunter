package schema

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestBuildSystemTablesMatchSpecExactly(t *testing.T) {
	e, err := validBuilder().Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	reg := e.Registry()

	sysClients, ok := reg.TableByName("sys_clients")
	if !ok {
		t.Fatal("sys_clients should exist")
	}
	wantClients := []ColumnSchema{
		{Index: 0, Name: "connection_id", Type: KindBytes, Nullable: false},
		{Index: 1, Name: "identity", Type: KindBytes, Nullable: false},
		{Index: 2, Name: "connected_at", Type: KindInt64, Nullable: false},
	}
	if !reflect.DeepEqual(sysClients.Columns, wantClients) {
		t.Fatalf("sys_clients columns = %+v, want %+v", sysClients.Columns, wantClients)
	}
	pk, ok := sysClients.PrimaryIndex()
	if !ok || pk.Name != "pk" || !pk.Primary || !pk.Unique || !reflect.DeepEqual(pk.Columns, []int{0}) {
		t.Fatalf("sys_clients pk = %+v", pk)
	}

	sysScheduled, ok := reg.TableByName("sys_scheduled")
	if !ok {
		t.Fatal("sys_scheduled should exist")
	}
	wantScheduled := []ColumnSchema{
		{Index: 0, Name: "schedule_id", Type: KindUint64, Nullable: false},
		{Index: 1, Name: "reducer_name", Type: KindString, Nullable: false},
		{Index: 2, Name: "args", Type: KindBytes, Nullable: false},
		{Index: 3, Name: "next_run_at_ns", Type: KindInt64, Nullable: false},
		{Index: 4, Name: "repeat_ns", Type: KindInt64, Nullable: false},
	}
	if !reflect.DeepEqual(sysScheduled.Columns, wantScheduled) {
		t.Fatalf("sys_scheduled columns = %+v, want %+v", sysScheduled.Columns, wantScheduled)
	}
	pk, ok = sysScheduled.PrimaryIndex()
	if !ok || pk.Name != "pk" || !pk.Primary || !pk.Unique || !reflect.DeepEqual(pk.Columns, []int{0}) {
		t.Fatalf("sys_scheduled pk = %+v", pk)
	}
}

func TestRegistryReducersPreserveRegistrationOrderAndFreshSlice(t *testing.T) {
	b := validBuilder()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	b.Reducer("CreatePlayer", h).Reducer("DeletePlayer", h).Reducer("UpdateScore", h)
	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	reg := e.Registry()

	got := reg.Reducers()
	want := []string{"CreatePlayer", "DeletePlayer", "UpdateScore"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Reducers() = %v, want %v", got, want)
	}

	mutated := reg.Reducers()
	mutated[0] = "Mutated"
	if reflect.DeepEqual(mutated, reg.Reducers()) {
		t.Fatal("Reducers() should return a fresh slice")
	}
}

func TestBuildRejectsNilReducerAndNilLifecycleHandlers(t *testing.T) {
	b := validBuilder()
	b.Reducer("NilReducer", nil)
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "NilReducer") {
		t.Fatalf("expected nil reducer validation error, got %v", err)
	}

	b = validBuilder()
	b.OnConnect(nil)
	_, err = b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "OnConnect handler") {
		t.Fatalf("expected nil OnConnect validation error, got %v", err)
	}

	b = validBuilder()
	b.OnDisconnect(nil)
	_, err = b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "OnDisconnect handler") {
		t.Fatalf("expected nil OnDisconnect validation error, got %v", err)
	}
}

func TestBuildRejectsDuplicateOnDisconnect(t *testing.T) {
	b := validBuilder()
	b.OnDisconnect(func(*types.ReducerContext) error { return nil })
	b.OnDisconnect(func(*types.ReducerContext) error { return nil })
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "duplicate OnDisconnect") {
		t.Fatalf("expected duplicate OnDisconnect validation error, got %v", err)
	}
}

func TestBuildRejectsAutoIncrementWithoutKey(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "jobs",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, AutoIncrement: true},
		},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrAutoIncrementRequiresKey) {
		t.Fatalf("expected ErrAutoIncrementRequiresKey, got %v", err)
	}
}

func TestBuildRejectsExplicitIndexContainingPrimaryKeyColumn(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{{Name: "bad_idx", Columns: []string{"id"}}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "must not appear in explicit index") {
		t.Fatalf("expected PK-in-index validation error, got %v", err)
	}
}

func TestBuildRejectsIndexWithMissingColumn(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "players",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64, PrimaryKey: true}},
		Indexes: []IndexDefinition{{Name: "missing_idx", Columns: []string{"name"}}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "references nonexistent column") {
		t.Fatalf("expected missing-column validation error, got %v", err)
	}
}

func TestBuildRejectsInvalidValueKind(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "players",
		Columns: []ColumnDefinition{{Name: "id", Type: ValueKind(999), PrimaryKey: true}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), "invalid ValueKind") {
		t.Fatalf("expected invalid ValueKind validation error, got %v", err)
	}
}

func TestDiscoverFieldsErrorsIncludeTopLevelStructName(t *testing.T) {
	type Player struct {
		CachedAt *int64
	}
	_, err := discoverFields(reflect.TypeFor[Player](), "")
	if err == nil {
		t.Fatal("expected unsupported field type error")
	}
	if !strings.Contains(err.Error(), "Player.CachedAt") {
		t.Fatalf("expected error to include Player.CachedAt, got %v", err)
	}
}
