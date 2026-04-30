package shunter

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestModuleDescribeReturnsDetachedAuthoredMetadata(t *testing.T) {
	metadata := map[string]string{"owner": "ops"}
	mod := NewModule("chat").Version("v1.2.3").Metadata(metadata)
	metadata["owner"] = "mutated"

	desc := mod.Describe()
	if desc.Name != "chat" {
		t.Fatalf("Name = %q, want chat", desc.Name)
	}
	if desc.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want v1.2.3", desc.Version)
	}
	if got := desc.Metadata["owner"]; got != "ops" {
		t.Fatalf("Metadata owner = %q, want ops", got)
	}

	desc.Metadata["owner"] = "changed"
	if got := mod.Describe().Metadata["owner"]; got != "ops" {
		t.Fatalf("second Describe metadata owner = %q, want ops", got)
	}
}

func TestRuntimeExportSchemaWorksBeforeStartAndIsDetached(t *testing.T) {
	mod := validChatModule().
		Version("v1.2.3").
		Reducer("send_message", noopReducer).
		OnConnect(noopLifecycle)
	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	export := rt.ExportSchema()
	if export.Version != 1 {
		t.Fatalf("schema version = %d, want 1", export.Version)
	}
	if !hasTableExport(export.Tables, "messages") {
		t.Fatalf("tables = %#v, want messages table", export.Tables)
	}
	if len(export.Reducers) != 2 {
		t.Fatalf("reducers = %#v, want normal reducer plus lifecycle", export.Reducers)
	}
	if !hasReducerExport(export.Reducers, "send_message", false) {
		t.Fatalf("reducers = %#v, want send_message non-lifecycle reducer", export.Reducers)
	}
	if !hasReducerExport(export.Reducers, "OnConnect", true) {
		t.Fatalf("reducers = %#v, want OnConnect lifecycle reducer", export.Reducers)
	}

	export.Tables[0].Name = "mutated"
	if got := rt.ExportSchema().Tables[0].Name; got != "messages" {
		t.Fatalf("second ExportSchema table name = %q, want detached messages", got)
	}
}

func TestRuntimeDescribeReportsModuleAndHealthWithoutStart(t *testing.T) {
	mod := validChatModule().Version("v1.2.3").Metadata(map[string]string{"team": "runtime"})
	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	desc := rt.Describe()
	if desc.Module.Name != "chat" {
		t.Fatalf("module name = %q, want chat", desc.Module.Name)
	}
	if desc.Module.Version != "v1.2.3" {
		t.Fatalf("module version = %q, want v1.2.3", desc.Module.Version)
	}
	if got := desc.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("module metadata team = %q, want runtime", got)
	}
	if desc.Health.State != RuntimeStateBuilt || desc.Health.Ready {
		t.Fatalf("health = %#v, want built and not ready", desc.Health)
	}

	desc.Module.Metadata["team"] = "mutated"
	if got := rt.Describe().Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("second Describe module metadata team = %q, want runtime", got)
	}
}

func TestRuntimeDescribeReflectsLifecycleState(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	ready := rt.Describe()
	if ready.Health.State != RuntimeStateReady || !ready.Health.Ready {
		t.Fatalf("ready health = %#v, want ready state", ready.Health)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	closed := rt.Describe()
	if closed.Health.State != RuntimeStateClosed || closed.Health.Ready {
		t.Fatalf("closed health = %#v, want closed and not ready", closed.Health)
	}
}

func hasTableExport(tables []schema.TableExport, name string) bool {
	for _, table := range tables {
		if table.Name == name {
			return true
		}
	}
	return false
}

func hasReducerExport(reducers []schema.ReducerExport, name string, lifecycle bool) bool {
	for _, reducer := range reducers {
		if reducer.Name == name && reducer.Lifecycle == lifecycle {
			return true
		}
	}
	return false
}

func noopReducer(*schema.ReducerContext, []byte) ([]byte, error) { return nil, nil }

func noopLifecycle(*schema.ReducerContext) error { return nil }
