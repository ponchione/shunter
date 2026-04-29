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

func TestModuleQueryPermissionAndReadModelMetadataIsDescribed(t *testing.T) {
	mod := NewModule("chat").Query(QueryDeclaration{
		Name:        "recent_messages",
		SQL:         "SELECT * FROM messages",
		Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
	})

	desc := mod.Describe()
	if len(desc.Queries) != 1 {
		t.Fatalf("queries = %#v, want one query", desc.Queries)
	}
	if got := desc.Queries[0].Permissions.Required; len(got) != 1 || got[0] != "messages:read" {
		t.Fatalf("query permissions = %#v, want messages:read", desc.Queries[0].Permissions)
	}
	if got := desc.Queries[0].ReadModel.Tables; len(got) != 1 || got[0] != "messages" {
		t.Fatalf("query read model tables = %#v, want messages", desc.Queries[0].ReadModel.Tables)
	}
	if got := desc.Queries[0].ReadModel.Tags; len(got) != 1 || got[0] != "history" {
		t.Fatalf("query read model tags = %#v, want history", desc.Queries[0].ReadModel.Tags)
	}
	if got := desc.Queries[0].SQL; got != "SELECT * FROM messages" {
		t.Fatalf("query SQL = %q, want declaration SQL", got)
	}
}

func TestModuleViewPermissionAndReadModelMetadataIsDescribed(t *testing.T) {
	mod := NewModule("chat").View(ViewDeclaration{
		Name:        "live_messages",
		SQL:         "SELECT * FROM messages",
		Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
	})

	desc := mod.Describe()
	if len(desc.Views) != 1 {
		t.Fatalf("views = %#v, want one view", desc.Views)
	}
	if got := desc.Views[0].Permissions.Required; len(got) != 1 || got[0] != "messages:subscribe" {
		t.Fatalf("view permissions = %#v, want messages:subscribe", desc.Views[0].Permissions)
	}
	if got := desc.Views[0].ReadModel.Tables; len(got) != 1 || got[0] != "messages" {
		t.Fatalf("view read model tables = %#v, want messages", desc.Views[0].ReadModel.Tables)
	}
	if got := desc.Views[0].ReadModel.Tags; len(got) != 1 || got[0] != "realtime" {
		t.Fatalf("view read model tags = %#v, want realtime", desc.Views[0].ReadModel.Tags)
	}
	if got := desc.Views[0].SQL; got != "SELECT * FROM messages" {
		t.Fatalf("view SQL = %q, want declaration SQL", got)
	}
}

func TestModuleQueryAndViewDeclarationsAreCopiedAtRegistration(t *testing.T) {
	queryPermissions := []string{"messages:read"}
	queryTables := []string{"messages"}
	queryTags := []string{"history"}
	queryClassifications := []MigrationClassification{MigrationClassificationAdditive}
	viewPermissions := []string{"messages:subscribe"}
	viewTables := []string{"messages"}
	viewTags := []string{"realtime"}
	viewClassifications := []MigrationClassification{MigrationClassificationManualReviewNeeded}

	mod := NewModule("chat").
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: queryPermissions},
			ReadModel:   ReadModelMetadata{Tables: queryTables, Tags: queryTags},
			Migration: MigrationMetadata{
				Classifications: queryClassifications,
			},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: viewPermissions},
			ReadModel:   ReadModelMetadata{Tables: viewTables, Tags: viewTags},
			Migration: MigrationMetadata{
				Classifications: viewClassifications,
			},
		})

	queryPermissions[0] = "mutated"
	queryTables[0] = "mutated"
	queryTags[0] = "mutated"
	queryClassifications[0] = MigrationClassificationDeprecated
	viewPermissions[0] = "mutated"
	viewTables[0] = "mutated"
	viewTags[0] = "mutated"
	viewClassifications[0] = MigrationClassificationDeprecated

	desc := mod.Describe()
	if got := desc.Queries[0].Permissions.Required; len(got) != 1 || got[0] != "messages:read" {
		t.Fatalf("query permissions = %#v, want registration-time copy", got)
	}
	if got := desc.Queries[0].ReadModel.Tables; len(got) != 1 || got[0] != "messages" {
		t.Fatalf("query tables = %#v, want registration-time copy", got)
	}
	if got := desc.Queries[0].ReadModel.Tags; len(got) != 1 || got[0] != "history" {
		t.Fatalf("query tags = %#v, want registration-time copy", got)
	}
	if got := desc.Queries[0].Migration.Classifications; len(got) != 1 || got[0] != MigrationClassificationAdditive {
		t.Fatalf("query migration classifications = %#v, want registration-time copy", got)
	}
	if got := desc.Queries[0].SQL; got != "SELECT * FROM messages" {
		t.Fatalf("query SQL = %q, want registration-time copy", got)
	}
	if got := desc.Views[0].Permissions.Required; len(got) != 1 || got[0] != "messages:subscribe" {
		t.Fatalf("view permissions = %#v, want registration-time copy", got)
	}
	if got := desc.Views[0].ReadModel.Tables; len(got) != 1 || got[0] != "messages" {
		t.Fatalf("view tables = %#v, want registration-time copy", got)
	}
	if got := desc.Views[0].ReadModel.Tags; len(got) != 1 || got[0] != "realtime" {
		t.Fatalf("view tags = %#v, want registration-time copy", got)
	}
	if got := desc.Views[0].Migration.Classifications; len(got) != 1 || got[0] != MigrationClassificationManualReviewNeeded {
		t.Fatalf("view migration classifications = %#v, want registration-time copy", got)
	}
	if got := desc.Views[0].SQL; got != "SELECT * FROM messages" {
		t.Fatalf("view SQL = %q, want registration-time copy", got)
	}
}

func TestBuildValidatesDeclaredReadSQLAgainstSchema(t *testing.T) {
	tests := []struct {
		name string
		mod  *Module
	}{
		{
			name: "query missing table",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "recent_messages",
				SQL:  "SELECT * FROM missing",
			}),
		},
		{
			name: "view projection unsupported by subscription SQL",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages",
				SQL:  "SELECT id FROM messages",
			}),
		},
		{
			name: "view limit unsupported by subscription SQL",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages",
				SQL:  "SELECT * FROM messages LIMIT 1",
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.mod, Config{DataDir: t.TempDir()})
			if err == nil || !errors.Is(err, ErrInvalidDeclarationSQL) {
				t.Fatalf("expected ErrInvalidDeclarationSQL, got %v", err)
			}
		})
	}
}

func TestBuildInvalidDeclaredReadSQLDoesNotFreezeModule(t *testing.T) {
	mod := validChatModule().Query(QueryDeclaration{
		Name: "missing_messages",
		SQL:  "SELECT * FROM missing",
	})

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("expected ErrInvalidDeclarationSQL, got %v", err)
	}

	missing := messagesTableDef()
	missing.Name = "missing"
	mod.TableDef(missing)
	if _, err := Build(mod, Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build after fixing declaration SQL returned error: %v", err)
	}
}

func TestBuildAcceptsValidDeclaredReadSQL(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT id FROM messages WHERE body = 'hello' LIMIT 1",
		}).
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages WHERE body = 'hello'",
		})

	if _, err := Build(mod, Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build returned error: %v", err)
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
	desc.Queries[0].SQL = "SELECT * FROM mutated_query"
	desc.Views[0].SQL = "SELECT * FROM mutated_view"
	desc.Queries[0].Permissions.Required = append(desc.Queries[0].Permissions.Required, "mutated_permission")
	desc.Views[0].ReadModel.Tables = append(desc.Views[0].ReadModel.Tables, "mutated_table")

	second := mod.Describe()
	assertQueryDescription(t, second.Queries, "recent_messages")
	assertViewDescription(t, second.Views, "live_messages")
	if hasQueryDescription(second.Queries, "mutated_query") {
		t.Fatalf("queries = %#v, want detached query descriptions", second.Queries)
	}
	if hasViewDescription(second.Views, "mutated_view") {
		t.Fatalf("views = %#v, want detached view descriptions", second.Views)
	}
	if len(second.Queries[0].Permissions.Required) != 0 {
		t.Fatalf("queries = %#v, want detached permission metadata", second.Queries)
	}
	if len(second.Views[0].ReadModel.Tables) != 0 {
		t.Fatalf("views = %#v, want detached read model metadata", second.Views)
	}
	if second.Queries[0].SQL != "" || second.Views[0].SQL != "" {
		t.Fatalf("declarations = %#v/%#v, want detached SQL metadata", second.Queries, second.Views)
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
