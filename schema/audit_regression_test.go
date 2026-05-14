package schema

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ponchione/shunter/types"
)

func TestBuildSystemTablesMatchSpecExactly(t *testing.T) {
	e, err := validBuilder().Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	reg := e.Registry()

	_, sysClients, ok := reg.TableByName("sys_clients")
	if !ok {
		t.Fatal("sys_clients should exist")
	}
	wantClients := []ColumnSchema{
		{Index: 0, Name: "connection_id", Type: KindBytes, Nullable: false},
		{Index: 1, Name: "identity", Type: KindBytes, Nullable: false},
		{Index: 2, Name: "connected_at", Type: KindInt64, Nullable: false},
	}
	if diff := cmp.Diff(wantClients, sysClients.Columns); diff != "" {
		t.Fatalf("sys_clients columns mismatch (-want +got):\n%s", diff)
	}
	pk, ok := sysClients.PrimaryIndex()
	if !ok || pk.Name != "pk" || !pk.Primary || !pk.Unique {
		t.Fatalf("sys_clients pk = %+v", pk)
	}
	if diff := cmp.Diff([]int{0}, pk.Columns); diff != "" {
		t.Fatalf("sys_clients pk columns mismatch (-want +got):\n%s", diff)
	}

	_, sysScheduled, ok := reg.TableByName("sys_scheduled")
	if !ok {
		t.Fatal("sys_scheduled should exist")
	}
	wantScheduled := []ColumnSchema{
		{Index: 0, Name: "schedule_id", Type: KindUint64, Nullable: false, AutoIncrement: true},
		{Index: 1, Name: "reducer_name", Type: KindString, Nullable: false},
		{Index: 2, Name: "args", Type: KindBytes, Nullable: false},
		{Index: 3, Name: "next_run_at_ns", Type: KindInt64, Nullable: false},
		{Index: 4, Name: "repeat_ns", Type: KindInt64, Nullable: false},
	}
	if diff := cmp.Diff(wantScheduled, sysScheduled.Columns); diff != "" {
		t.Fatalf("sys_scheduled columns mismatch (-want +got):\n%s", diff)
	}
	pk, ok = sysScheduled.PrimaryIndex()
	if !ok || pk.Name != "pk" || !pk.Primary || !pk.Unique {
		t.Fatalf("sys_scheduled pk = %+v", pk)
	}
	if diff := cmp.Diff([]int{0}, pk.Columns); diff != "" {
		t.Fatalf("sys_scheduled pk columns mismatch (-want +got):\n%s", diff)
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
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Reducers() mismatch (-want +got):\n%s", diff)
	}

	mutated := reg.Reducers()
	mutated[0] = "Mutated"
	if diff := cmp.Diff(mutated, reg.Reducers()); diff == "" {
		t.Fatal("Reducers() should return a fresh slice")
	}
}

func TestBuildRejectsNilReducerAndNilLifecycleHandlers(t *testing.T) {
	b := validBuilder()
	b.Reducer("NilReducer", nil)
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNilReducerHandler) || !strings.Contains(err.Error(), "NilReducer") {
		t.Fatalf("expected ErrNilReducerHandler mentioning NilReducer, got %v", err)
	}

	b = validBuilder()
	b.OnConnect(nil)
	_, err = b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNilReducerHandler) || !strings.Contains(err.Error(), "OnConnect") {
		t.Fatalf("expected ErrNilReducerHandler mentioning OnConnect, got %v", err)
	}

	b = validBuilder()
	b.OnDisconnect(nil)
	_, err = b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNilReducerHandler) || !strings.Contains(err.Error(), "OnDisconnect") {
		t.Fatalf("expected ErrNilReducerHandler mentioning OnDisconnect, got %v", err)
	}
}

func TestBuildRejectsDuplicateOnDisconnect(t *testing.T) {
	b := validBuilder()
	b.OnDisconnect(func(*types.ReducerContext) error { return nil })
	b.OnDisconnect(func(*types.ReducerContext) error { return nil })
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrDuplicateLifecycleReducer) || !strings.Contains(err.Error(), "OnDisconnect") {
		t.Fatalf("expected ErrDuplicateLifecycleReducer mentioning OnDisconnect, got %v", err)
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

func TestBuildRejectsMultipleAutoIncrementColumns(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "jobs",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "sequence", Type: KindUint64, AutoIncrement: true},
		},
		Indexes: []IndexDefinition{
			{Name: "unique_sequence", Columns: []string{"sequence"}, Unique: true},
		},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrMultipleAutoIncrement) {
		t.Fatalf("expected ErrMultipleAutoIncrement, got %v", err)
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

func TestBuildRejectsExplicitIndexNamedPKWhenPrimaryKeySynthesizesPKIndex(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{{Name: "pk", Columns: []string{"name"}}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), `duplicate index name "pk"`) {
		t.Fatalf("expected duplicate synthetic pk index name validation error, got %v", err)
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
	if err == nil || !errors.Is(err, ErrColumnNotFound) || !strings.Contains(err.Error(), "missing_idx") {
		t.Fatalf("expected ErrColumnNotFound for missing index column, got %v", err)
	}
}

func TestBuildRejectsIndexWithDuplicateColumn(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
		Indexes: []IndexDefinition{{Name: "by_name_twice", Columns: []string{"name", "name"}}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !strings.Contains(err.Error(), `duplicate index column "name"`) {
		t.Fatalf("expected duplicate index column validation error, got %v", err)
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
		CachedAt *map[string]string
	}
	_, err := discoverFields(reflect.TypeFor[Player](), "")
	if err == nil {
		t.Fatal("expected unsupported field type error")
	}
	if !strings.Contains(err.Error(), "Player.CachedAt") {
		t.Fatalf("expected error to include Player.CachedAt, got %v", err)
	}
}
