package shunter

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestBuildExplicitVersionedModuleSucceeds(t *testing.T) {
	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef())

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if rt == nil {
		t.Fatal("Build returned nil runtime")
	}
	if got := rt.ModuleName(); got != "chat" {
		t.Fatalf("ModuleName() = %q, want %q", got, "chat")
	}
	if got := rt.engine.Registry().Version(); got != 1 {
		t.Fatalf("registry version = %d, want 1", got)
	}
	if _, ts, ok := rt.engine.Registry().TableByName("messages"); !ok || ts == nil {
		t.Fatal("messages table missing from built registry")
	}
}

func TestBuildSchemaVersionWithoutTablesStillFailsAtSchemaLayer(t *testing.T) {
	mod := NewModule("chat").SchemaVersion(1)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrNoTables) {
		t.Fatalf("expected ErrNoTables, got %v", err)
	}
}

func TestBuildDuplicateTableDefPreservesSchemaError(t *testing.T) {
	mod := NewModule("chat").SchemaVersion(1)
	def := messagesTableDef()
	mod.TableDef(def).TableDef(def)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrDuplicateTableName) {
		t.Fatalf("expected ErrDuplicateTableName, got %v", err)
	}
}

func TestBuildReducerWrapperRegistersReducer(t *testing.T) {
	handler := func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		return nil, nil
	}

	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		Reducer("send_message", handler)

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if _, ok := rt.engine.Registry().Reducer("send_message"); !ok {
		t.Fatal("send_message reducer missing from registry")
	}
}

func TestBuildDuplicateReducerWrapperPreservesSchemaError(t *testing.T) {
	handler := func(_ *schema.ReducerContext, _ []byte) ([]byte, error) { return nil, nil }

	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		Reducer("send_message", handler).
		Reducer("send_message", handler)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrDuplicateReducerName) {
		t.Fatalf("expected ErrDuplicateReducerName, got %v", err)
	}
}

func TestBuildReservedReducerWrapperPreservesSchemaError(t *testing.T) {
	handler := func(_ *schema.ReducerContext, _ []byte) ([]byte, error) { return nil, nil }

	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		Reducer("OnConnect", handler)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrReservedReducerName) {
		t.Fatalf("expected ErrReservedReducerName, got %v", err)
	}
}

func TestBuildNilReducerWrapperPreservesSchemaError(t *testing.T) {
	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		Reducer("send_message", nil)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrNilReducerHandler) {
		t.Fatalf("expected ErrNilReducerHandler, got %v", err)
	}
}

func TestBuildLifecycleWrappersRegisterHandlers(t *testing.T) {
	onConnect := func(_ *schema.ReducerContext) error { return nil }
	onDisconnect := func(_ *schema.ReducerContext) error { return nil }

	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		OnConnect(onConnect).
		OnDisconnect(onDisconnect)

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if rt.engine.Registry().OnConnect() == nil {
		t.Fatal("OnConnect handler missing")
	}
	if rt.engine.Registry().OnDisconnect() == nil {
		t.Fatal("OnDisconnect handler missing")
	}
}

func TestBuildDuplicateOnConnectWrapperPreservesSchemaError(t *testing.T) {
	handler := func(_ *schema.ReducerContext) error { return nil }

	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		OnConnect(handler).
		OnConnect(handler)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrDuplicateLifecycleReducer) {
		t.Fatalf("expected ErrDuplicateLifecycleReducer, got %v", err)
	}
}

func TestBuildNilOnDisconnectWrapperPreservesSchemaError(t *testing.T) {
	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef()).
		OnDisconnect(nil)

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrNilReducerHandler) {
		t.Fatalf("expected ErrNilReducerHandler, got %v", err)
	}
}

func TestBuildSecondCallPreservesAlreadyBuiltError(t *testing.T) {
	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef())

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil || rt == nil {
		t.Fatalf("first Build failed: rt=%v err=%v", rt, err)
	}

	_, err = Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, schema.ErrAlreadyBuilt) {
		t.Fatalf("expected ErrAlreadyBuilt on second Build, got %v", err)
	}
}

func messagesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "body", Type: types.KindString},
		},
	}
}
