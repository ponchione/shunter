package schema

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestSchemaRegistrySatisfiesLookupAndResolver pins that SchemaRegistry
// (the interface returned by Engine.Registry) embeds both SchemaLookup and
// IndexResolver per SPEC-006 §7. Purely compile-time shape check.
func TestSchemaRegistrySatisfiesLookupAndResolver(t *testing.T) {
	e, err := validBuilder().Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	var _ SchemaLookup = e.Registry()
	var _ IndexResolver = e.Registry()
}

// TestBuildReservedReducerNameReturnsSentinel pins that a normal reducer
// registration using a lifecycle-reserved name is rejected with
// ErrReservedReducerName (SPEC-006 §13) instead of surfacing a string error.
func TestBuildReservedReducerNameReturnsSentinel(t *testing.T) {
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	for _, name := range []string{"OnConnect", "OnDisconnect"} {
		b := validBuilder()
		b.Reducer(name, h)
		_, err := b.Build(EngineOptions{})
		if err == nil {
			t.Fatalf("Build should have failed for reserved reducer %q", name)
		}
		if !errors.Is(err, ErrReservedReducerName) {
			t.Fatalf("reducer %q: expected ErrReservedReducerName, got %v", name, err)
		}
	}
}

// TestBuildNilReducerHandlerReturnsSentinel pins that a nil reducer handler
// returns ErrNilReducerHandler (SPEC-006 §13).
func TestBuildNilReducerHandlerReturnsSentinel(t *testing.T) {
	b := validBuilder()
	b.Reducer("NullHandler", nil)
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNilReducerHandler) {
		t.Fatalf("expected ErrNilReducerHandler, got %v", err)
	}
}

// TestBuildNilLifecycleHandlersReturnSentinel pins that registering a nil
// OnConnect/OnDisconnect handler returns ErrNilReducerHandler.
func TestBuildNilLifecycleHandlersReturnSentinel(t *testing.T) {
	b := validBuilder()
	b.OnConnect(nil)
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNilReducerHandler) {
		t.Fatalf("OnConnect nil: expected ErrNilReducerHandler, got %v", err)
	}

	b = validBuilder()
	b.OnDisconnect(nil)
	_, err = b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrNilReducerHandler) {
		t.Fatalf("OnDisconnect nil: expected ErrNilReducerHandler, got %v", err)
	}
}

// TestBuildDuplicateLifecycleRegistrationReturnsSentinel pins that a second
// OnConnect or OnDisconnect registration produces ErrDuplicateLifecycleReducer
// (SPEC-006 §13).
func TestBuildDuplicateLifecycleRegistrationReturnsSentinel(t *testing.T) {
	b := validBuilder()
	b.OnConnect(func(*types.ReducerContext) error { return nil })
	b.OnConnect(func(*types.ReducerContext) error { return nil })
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrDuplicateLifecycleReducer) {
		t.Fatalf("duplicate OnConnect: expected ErrDuplicateLifecycleReducer, got %v", err)
	}

	b = validBuilder()
	b.OnDisconnect(func(*types.ReducerContext) error { return nil })
	b.OnDisconnect(func(*types.ReducerContext) error { return nil })
	_, err = b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrDuplicateLifecycleReducer) {
		t.Fatalf("duplicate OnDisconnect: expected ErrDuplicateLifecycleReducer, got %v", err)
	}
}

// TestBuildInvalidTableNameReturnsSentinel pins that a table name failing the
// [A-Za-z][A-Za-z0-9_]* pattern returns ErrInvalidTableName (SPEC-006 §13).
// Previously this path surfaced ErrEmptyTableName, conflating two distinct
// contract errors.
func TestBuildInvalidTableNameReturnsSentinel(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "123bad",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64, PrimaryKey: true}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrInvalidTableName) {
		t.Fatalf("expected ErrInvalidTableName, got %v", err)
	}
	if errors.Is(err, ErrEmptyTableName) {
		t.Fatal("invalid-pattern name must not masquerade as ErrEmptyTableName")
	}
}

// TestBuildEmptyColumnNameReturnsSentinel pins that an empty column name
// returns ErrEmptyColumnName (SPEC-006 §13).
func TestBuildEmptyColumnNameReturnsSentinel(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "", Type: KindString},
		},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrEmptyColumnName) {
		t.Fatalf("expected ErrEmptyColumnName, got %v", err)
	}
}

// TestBuildIndexMissingColumnReturnsColumnNotFound pins that an index
// referencing a nonexistent column returns ErrColumnNotFound (SPEC-006 §13),
// which is the canonical schema-layer sentinel consumed by SPEC-001 store
// integrity checks and SPEC-004 predicate validation via re-export.
func TestBuildIndexMissingColumnReturnsColumnNotFound(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name:    "players",
		Columns: []ColumnDefinition{{Name: "id", Type: KindUint64, PrimaryKey: true}},
		Indexes: []IndexDefinition{{Name: "missing_idx", Columns: []string{"name"}}},
	})
	_, err := b.Build(EngineOptions{})
	if err == nil || !errors.Is(err, ErrColumnNotFound) {
		t.Fatalf("expected ErrColumnNotFound, got %v", err)
	}
}
