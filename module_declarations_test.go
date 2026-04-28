package shunter

import (
	"errors"
	"testing"
)

func TestModuleQueryDeclarationMetadataIsDescribed(t *testing.T) {
	mod := NewModule("chat").Query(QueryDeclaration{Name: "recent_messages"})

	desc := mod.Describe()
	assertQueryDescription(t, desc.Queries, "recent_messages")
	if len(desc.Views) != 0 {
		t.Fatalf("views = %#v, want none", desc.Views)
	}
}

func TestModuleViewDeclarationMetadataIsDescribed(t *testing.T) {
	mod := NewModule("chat").View(ViewDeclaration{Name: "live_messages"})

	desc := mod.Describe()
	assertViewDescription(t, desc.Views, "live_messages")
	if len(desc.Queries) != 0 {
		t.Fatalf("queries = %#v, want none", desc.Queries)
	}
}

func TestModuleDeclarationNamesMustBeNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		mod  *Module
	}{
		{
			name: "query",
			mod:  validChatModule().Query(QueryDeclaration{Name: "   "}),
		},
		{
			name: "view",
			mod:  validChatModule().View(ViewDeclaration{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.mod, Config{DataDir: t.TempDir()})
			if err == nil || !errors.Is(err, ErrEmptyDeclarationName) {
				t.Fatalf("expected ErrEmptyDeclarationName, got %v", err)
			}
		})
	}
}

func TestModuleDuplicateQueryDeclarationNamesFailBuild(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{Name: "recent_messages"}).
		Query(QueryDeclaration{Name: "recent_messages"})

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, ErrDuplicateDeclarationName) {
		t.Fatalf("expected ErrDuplicateDeclarationName, got %v", err)
	}
}

func TestModuleDuplicateViewDeclarationNamesFailBuild(t *testing.T) {
	mod := validChatModule().
		View(ViewDeclaration{Name: "live_messages"}).
		View(ViewDeclaration{Name: "live_messages"})

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, ErrDuplicateDeclarationName) {
		t.Fatalf("expected ErrDuplicateDeclarationName, got %v", err)
	}
}

func TestModuleDeclarationNamesShareQueryAndViewNamespace(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{Name: "messages"}).
		View(ViewDeclaration{Name: "messages"})

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, ErrDuplicateDeclarationName) {
		t.Fatalf("expected ErrDuplicateDeclarationName, got %v", err)
	}
}

func TestModuleDeclarationDescriptionsAreDetached(t *testing.T) {
	mod := NewModule("chat").
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"})

	desc := mod.Describe()
	if len(desc.Queries) != 1 {
		t.Fatalf("queries = %#v, want one query", desc.Queries)
	}
	if len(desc.Views) != 1 {
		t.Fatalf("views = %#v, want one view", desc.Views)
	}
	desc.Queries[0].Name = "mutated_query"
	desc.Views[0].Name = "mutated_view"

	second := mod.Describe()
	assertQueryDescription(t, second.Queries, "recent_messages")
	assertViewDescription(t, second.Views, "live_messages")
	if hasQueryDescription(second.Queries, "mutated_query") {
		t.Fatalf("queries = %#v, want detached query descriptions", second.Queries)
	}
	if hasViewDescription(second.Views, "mutated_view") {
		t.Fatalf("views = %#v, want detached view descriptions", second.Views)
	}
}

func TestRuntimeDescribeIncludesBuiltModuleDeclarations(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"})

	before := mod.Describe()
	assertQueryDescription(t, before.Queries, "recent_messages")
	assertViewDescription(t, before.Views, "live_messages")

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	afterModule := mod.Describe()
	assertQueryDescription(t, afterModule.Queries, "recent_messages")
	assertViewDescription(t, afterModule.Views, "live_messages")

	runtimeDesc := rt.Describe()
	assertQueryDescription(t, runtimeDesc.Module.Queries, "recent_messages")
	assertViewDescription(t, runtimeDesc.Module.Views, "live_messages")
}

func TestModuleDeclarationsDoNotAffectTableOrReducerRegistration(t *testing.T) {
	mod := validChatModule().
		Reducer("send_message", noopReducer).
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	export := rt.ExportSchema()
	if !hasTableExport(export.Tables, "messages") {
		t.Fatalf("tables = %#v, want messages table", export.Tables)
	}
	if !hasReducerExport(export.Reducers, "send_message", false) {
		t.Fatalf("reducers = %#v, want send_message reducer", export.Reducers)
	}
}

func assertQueryDescription(t *testing.T, queries []QueryDescription, name string) {
	t.Helper()
	if !hasQueryDescription(queries, name) {
		t.Fatalf("queries = %#v, want query %q", queries, name)
	}
}

func assertViewDescription(t *testing.T, views []ViewDescription, name string) {
	t.Helper()
	if !hasViewDescription(views, name) {
		t.Fatalf("views = %#v, want view %q", views, name)
	}
}

func hasQueryDescription(queries []QueryDescription, name string) bool {
	for _, query := range queries {
		if query.Name == name {
			return true
		}
	}
	return false
}

func hasViewDescription(views []ViewDescription, name string) bool {
	for _, view := range views {
		if view.Name == name {
			return true
		}
	}
	return false
}
