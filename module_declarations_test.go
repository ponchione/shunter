package shunter

import (
	"errors"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
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

func TestNoParameterDeclaredReadsKeepUnkeyedLiteralShape(t *testing.T) {
	query := QueryDeclaration{
		"recent_messages",
		"SELECT * FROM messages",
		PermissionMetadata{},
		ReadModelMetadata{},
		MigrationMetadata{},
	}
	view := ViewDeclaration{
		"live_messages",
		"SELECT * FROM messages",
		PermissionMetadata{},
		ReadModelMetadata{},
		MigrationMetadata{},
	}

	mod := NewModule("chat").Query(query).View(view)
	desc := mod.Describe()
	if len(desc.Queries) != 1 || desc.Queries[0].Name != "recent_messages" {
		t.Fatalf("query description = %#v, want unkeyed declaration registered", desc.Queries)
	}
	if len(desc.Views) != 1 || desc.Views[0].Name != "live_messages" {
		t.Fatalf("view description = %#v, want unkeyed declaration registered", desc.Views)
	}
}

func TestModuleVisibilityFilterDeclarationMetadataIsDescribed(t *testing.T) {
	mod := NewModule("chat").VisibilityFilter(VisibilityFilterDeclaration{
		Name: "own_messages",
		SQL:  "SELECT * FROM messages WHERE body = :sender",
	})

	desc := mod.Describe()
	if len(desc.VisibilityFilters) != 1 {
		t.Fatalf("visibility filters = %#v, want one filter", desc.VisibilityFilters)
	}
	if got := desc.VisibilityFilters[0].Name; got != "own_messages" {
		t.Fatalf("filter name = %q, want own_messages", got)
	}
	if got := desc.VisibilityFilters[0].SQL; got != "SELECT * FROM messages WHERE body = :sender" {
		t.Fatalf("filter SQL = %q, want authored SQL", got)
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
	queryParameterColumns := []ProductColumn{{Name: "topic", Type: "string"}}
	viewPermissions := []string{"messages:subscribe"}
	viewTables := []string{"messages"}
	viewTags := []string{"realtime"}
	viewClassifications := []MigrationClassification{MigrationClassificationManualReviewNeeded}
	viewParameterColumns := []ProductColumn{{Name: "topic", Type: "string"}}

	mod := NewModule("chat").
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: queryPermissions},
			ReadModel:   ReadModelMetadata{Tables: queryTables, Tags: queryTags},
			Migration: MigrationMetadata{
				Classifications: queryClassifications,
			},
		}, WithQueryParameters(ProductSchema{Columns: queryParameterColumns})).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: viewPermissions},
			ReadModel:   ReadModelMetadata{Tables: viewTables, Tags: viewTags},
			Migration: MigrationMetadata{
				Classifications: viewClassifications,
			},
		}, WithViewParameters(ProductSchema{Columns: viewParameterColumns}))

	queryPermissions[0] = "mutated"
	queryTables[0] = "mutated"
	queryTags[0] = "mutated"
	queryClassifications[0] = MigrationClassificationDeprecated
	queryParameterColumns[0].Name = "mutated"
	viewPermissions[0] = "mutated"
	viewTables[0] = "mutated"
	viewTags[0] = "mutated"
	viewClassifications[0] = MigrationClassificationDeprecated
	viewParameterColumns[0].Type = "uint64"

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
	assertProductSchemaColumns(t, desc.Queries[0].Parameters, []ProductColumn{{Name: "topic", Type: "string"}})
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
	assertProductSchemaColumns(t, desc.Views[0].Parameters, []ProductColumn{{Name: "topic", Type: "string"}})
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
			name: "view string sum aggregate unsupported by live SQL",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages",
				SQL:  "SELECT SUM(body) AS total FROM messages",
			}),
		},
		{
			name: "view aggregate offset unsupported by live SQL",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages",
				SQL:  "SELECT COUNT(*) AS n FROM messages OFFSET 1",
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

func TestBuildAcceptsDeclaredReadSQLParameters(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name: "messages_by_body",
			SQL:  "SELECT * FROM messages WHERE body = :body",
		}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
			{Name: "body", Type: "string"},
		}})).
		View(ViewDeclaration{
			Name: "live_messages_by_id",
			SQL:  "SELECT * FROM messages WHERE id = :message_id",
		}, WithViewParameters(ProductSchema{Columns: []ProductColumn{
			{Name: "message_id", Type: "uint64"},
		}}))

	if _, err := Build(mod, Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
}

func TestBuildRejectsInvalidDeclaredReadSQLParameters(t *testing.T) {
	tests := []struct {
		name string
		mod  *Module
		want string
	}{
		{
			name: "unknown query placeholder",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "messages_by_body",
				SQL:  "SELECT * FROM messages WHERE body = :missing",
			}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
			}})),
			want: `query "messages_by_body": coerce column "body": unsupported SQL: SQL parameter :missing is not declared`,
		},
		{
			name: "unused query parameter",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "messages_by_body",
				SQL:  "SELECT * FROM messages WHERE body = 'hello'",
			}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
			}})),
			want: `query "messages_by_body": unsupported SQL: SQL parameter :body is declared but not used`,
		},
		{
			name: "compatible repeated query parameter",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "messages_by_body",
				SQL:  "SELECT * FROM messages WHERE body = :body OR body != :body",
			}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
			}})),
		},
		{
			name: "incompatible repeated query parameter",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "messages_by_body",
				SQL:  "SELECT * FROM messages WHERE body = :body OR id = :body",
			}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
			}})),
			want: `SQL parameter :body type String is incompatible with column "id" type U64`,
		},
		{
			name: "unsupported limit parameter position",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "messages_by_limit",
				SQL:  "SELECT * FROM messages WHERE body = :body LIMIT :limit",
			}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
				{Name: "limit", Type: "uint64"},
			}})),
			want: `SQL parameter :limit is not supported in LIMIT`,
		},
		{
			name: "unknown view placeholder",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages_by_body",
				SQL:  "SELECT * FROM messages WHERE body = :missing",
			}, WithViewParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
			}})),
			want: `view "live_messages_by_body": coerce column "body": unsupported SQL: SQL parameter :missing is not declared`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.mod, Config{DataDir: t.TempDir()})
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Build returned error: %v", err)
				}
				return
			}
			if err == nil || !errors.Is(err, ErrInvalidDeclarationSQL) {
				t.Fatalf("expected ErrInvalidDeclarationSQL, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Build error = %v, want context %q", err, tt.want)
			}
		})
	}
}

func TestBuildValidatesVisibilityFilterDeclarations(t *testing.T) {
	tests := []struct {
		name string
		mod  *Module
		want error
	}{
		{
			name: "blank name",
			mod: validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
				Name: " ",
				SQL:  "SELECT * FROM messages",
			}),
			want: ErrEmptyDeclarationName,
		},
		{
			name: "duplicate name",
			mod: validChatModule().
				VisibilityFilter(VisibilityFilterDeclaration{Name: "own_messages", SQL: "SELECT * FROM messages"}).
				VisibilityFilter(VisibilityFilterDeclaration{Name: "own_messages", SQL: "SELECT * FROM messages WHERE body = 'hello'"}),
			want: ErrDuplicateDeclarationName,
		},
		{
			name: "blank SQL",
			mod: validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
				Name: "own_messages",
				SQL:  " ",
			}),
			want: ErrInvalidDeclarationSQL,
		},
		{
			name: "invalid SQL",
			mod: validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
				Name: "own_messages",
				SQL:  "SELECT FROM messages",
			}),
			want: ErrInvalidDeclarationSQL,
		},
		{
			name: "unknown return table",
			mod: validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
				Name: "own_messages",
				SQL:  "SELECT * FROM missing",
			}),
			want: ErrInvalidDeclarationSQL,
		},
		{
			name: "unsupported join",
			mod: validChatModule().
				TableDef(schema.TableDefinition{
					Name: "owners",
					Columns: []schema.ColumnDefinition{
						{Name: "id", Type: types.KindUint64, PrimaryKey: true},
						{Name: "body", Type: types.KindString},
					},
				}).
				VisibilityFilter(VisibilityFilterDeclaration{
					Name: "joined_messages",
					SQL:  "SELECT messages.* FROM messages JOIN owners ON messages.id = owners.id",
				}),
			want: ErrInvalidDeclarationSQL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.mod, Config{DataDir: t.TempDir()})
			if err == nil || !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
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
		Query(QueryDeclaration{
			Name: "recent_messages",
		}, WithQueryParameters(ProductSchema{Columns: []ProductColumn{
			{Name: "topic", Type: "string"},
		}})).
		View(ViewDeclaration{
			Name: "live_messages",
		}, WithViewParameters(ProductSchema{Columns: []ProductColumn{
			{Name: "topic", Type: "string"},
		}}))

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
	desc.Queries[0].Parameters.Columns[0].Name = "mutated_query_param"
	desc.Views[0].Parameters.Columns[0].Type = "uint64"

	second := mod.Describe()
	assertQueryDescription(t, second.Queries, "recent_messages")
	assertViewDescription(t, second.Views, "live_messages")
	assertProductSchemaColumns(t, second.Queries[0].Parameters, []ProductColumn{{Name: "topic", Type: "string"}})
	assertProductSchemaColumns(t, second.Views[0].Parameters, []ProductColumn{{Name: "topic", Type: "string"}})
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

func TestRuntimeDescribeIncludesValidatedVisibilityFilterMetadata(t *testing.T) {
	mod := validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
		Name: "own_messages",
		SQL:  "SELECT * FROM messages WHERE body = :sender",
	})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	desc := rt.Describe()
	if len(desc.Module.VisibilityFilters) != 1 {
		t.Fatalf("visibility filters = %#v, want one filter", desc.Module.VisibilityFilters)
	}
	filter := desc.Module.VisibilityFilters[0]
	if filter.Name != "own_messages" {
		t.Fatalf("filter name = %q, want own_messages", filter.Name)
	}
	if filter.SQL != "SELECT * FROM messages WHERE body = :sender" {
		t.Fatalf("filter SQL = %q, want authored SQL", filter.SQL)
	}
	if filter.ReturnTable != "messages" || filter.ReturnTableID != 0 {
		t.Fatalf("filter return table = %q/%d, want messages/0", filter.ReturnTable, filter.ReturnTableID)
	}
	if !filter.UsesCallerIdentity {
		t.Fatal("filter UsesCallerIdentity = false, want true")
	}
}

func TestRuntimeDescribePreservesMultipleVisibilityFiltersForOneTableInOrder(t *testing.T) {
	mod := validChatModule().
		VisibilityFilter(VisibilityFilterDeclaration{Name: "hello_messages", SQL: "SELECT * FROM messages WHERE body = 'hello'"}).
		VisibilityFilter(VisibilityFilterDeclaration{Name: "own_messages", SQL: "SELECT * FROM messages WHERE body = :sender"})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	filters := rt.Describe().Module.VisibilityFilters
	if len(filters) != 2 {
		t.Fatalf("visibility filters = %#v, want two filters", filters)
	}
	if filters[0].Name != "hello_messages" || filters[1].Name != "own_messages" {
		t.Fatalf("filter order = %#v, want declaration order", filters)
	}
	if filters[0].ReturnTable != "messages" || filters[1].ReturnTable != "messages" {
		t.Fatalf("filter return tables = %#v, want messages for both", filters)
	}
}

func TestRuntimeVisibilityFilterDescriptionsAreDetached(t *testing.T) {
	mod := validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
		Name: "own_messages",
		SQL:  "SELECT * FROM messages WHERE body = :sender",
	})
	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	desc := rt.Describe()
	desc.Module.VisibilityFilters[0].Name = "mutated"
	desc.Module.VisibilityFilters[0].SQL = "SELECT * FROM mutated"
	desc.Module.VisibilityFilters[0].ReturnTable = "mutated"
	desc.Module.VisibilityFilters[0].UsesCallerIdentity = false

	again := rt.Describe()
	filter := again.Module.VisibilityFilters[0]
	if filter.Name != "own_messages" ||
		filter.SQL != "SELECT * FROM messages WHERE body = :sender" ||
		filter.ReturnTable != "messages" ||
		!filter.UsesCallerIdentity {
		t.Fatalf("second visibility filter = %#v, want detached metadata", filter)
	}
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
