package schema

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestNewBuilder(t *testing.T) {
	b := NewBuilder()
	if b == nil {
		t.Fatal("NewBuilder returned nil")
	}
}

func TestBuilderTableDefAccumulates(t *testing.T) {
	b := NewBuilder()
	b.TableDef(TableDefinition{Name: "a"}).TableDef(TableDefinition{Name: "b"})
	if len(b.tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(b.tables))
	}
}

func TestBuilderWithTableName(t *testing.T) {
	b := NewBuilder()
	b.TableDef(TableDefinition{Name: "original"}, WithTableName("override"))
	if b.tables[0].Name != "override" {
		t.Fatalf("expected 'override', got %q", b.tables[0].Name)
	}
}

func TestBuilderTableDefUsesDefName(t *testing.T) {
	b := NewBuilder()
	b.TableDef(TableDefinition{Name: "keep_me"})
	if b.tables[0].Name != "keep_me" {
		t.Fatalf("expected 'keep_me', got %q", b.tables[0].Name)
	}
}

func TestBuilderSchemaVersion(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(3)
	if b.version != 3 || !b.versionSet {
		t.Fatal("SchemaVersion not stored")
	}
}

func TestBuilderChaining(t *testing.T) {
	b := NewBuilder()
	result := b.TableDef(TableDefinition{Name: "x"}).SchemaVersion(1)
	if result != b {
		t.Fatal("builder methods should return *Builder for chaining")
	}
}

// Story 3.2: Reducer registration

func TestBuilderReducer(t *testing.T) {
	b := NewBuilder()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	b.Reducer("CreatePlayer", h).Reducer("DeletePlayer", h)
	if len(b.reducers) != 2 {
		t.Fatalf("expected 2 reducers, got %d", len(b.reducers))
	}
}

func TestSchemaReducerAliasesExist(t *testing.T) {
	var h ReducerHandler = func(_ *ReducerContext, _ []byte) ([]byte, error) { return nil, nil }
	_ = h

	b := NewBuilder()
	b.Reducer("CreatePlayer", h)
	b.OnConnect(func(_ *ReducerContext) error { return nil })
	b.OnDisconnect(func(_ *ReducerContext) error { return nil })
}

func TestBuilderReducerDuplicatePreserved(t *testing.T) {
	b := NewBuilder()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	b.Reducer("Foo", h).Reducer("Foo", h)
	if b.reducers["Foo"].count != 2 {
		t.Fatal("duplicate registration count should be preserved")
	}
}

func TestBuilderLifecycle(t *testing.T) {
	b := NewBuilder()
	b.OnConnect(func(_ *types.ReducerContext) error { return nil })
	b.OnDisconnect(func(_ *types.ReducerContext) error { return nil })
	if b.onConnect == nil || b.onDisconnect == nil {
		t.Fatal("lifecycle handlers should be stored")
	}
}

func TestBuilderLifecycleDuplicateCount(t *testing.T) {
	b := NewBuilder()
	b.OnConnect(func(_ *types.ReducerContext) error { return nil })
	b.OnConnect(func(_ *types.ReducerContext) error { return nil })
	if b.onConnectRegistrations != 2 {
		t.Fatal("duplicate OnConnect count should be 2")
	}
}
