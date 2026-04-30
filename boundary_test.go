package shunter

import "testing"

func TestRuntimeBuildSnapshotsAuthoredModuleBoundary(t *testing.T) {
	metadata := map[string]string{"team": "runtime"}
	moduleClasses := []MigrationClassification{MigrationClassificationManualReviewNeeded}
	tableClasses := []MigrationClassification{MigrationClassificationDataRewriteNeeded}
	reducerPermissions := []string{"messages:send"}
	queryPermissions := []string{"messages:read"}
	queryTables := []string{"messages"}
	queryTags := []string{"history"}
	queryClasses := []MigrationClassification{MigrationClassificationAdditive}
	viewPermissions := []string{"messages:subscribe"}
	viewTables := []string{"messages"}
	viewTags := []string{"realtime"}
	viewClasses := []MigrationClassification{MigrationClassificationManualReviewNeeded}

	mod := validChatModule().
		Version("v1.2.3").
		Metadata(metadata).
		Migration(MigrationMetadata{
			ModuleVersion:   "v1.2.3",
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: moduleClasses,
		}).
		TableMigration("messages", MigrationMetadata{
			Compatibility:   MigrationCompatibilityBreaking,
			Classifications: tableClasses,
		}).
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: reducerPermissions})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: queryPermissions},
			ReadModel:   ReadModelMetadata{Tables: queryTables, Tags: queryTags},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: queryClasses,
			},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			Permissions: PermissionMetadata{Required: viewPermissions},
			ReadModel:   ReadModelMetadata{Tables: viewTables, Tags: viewTags},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityUnknown,
				Classifications: viewClasses,
			},
		})

	metadata["team"] = "mutated-before-build"
	moduleClasses[0] = MigrationClassificationDeprecated
	tableClasses[0] = MigrationClassificationDeprecated
	reducerPermissions[0] = "mutated-before-build"
	queryPermissions[0] = "mutated-before-build"
	queryTables[0] = "mutated-before-build"
	queryTags[0] = "mutated-before-build"
	queryClasses[0] = MigrationClassificationDeprecated
	viewPermissions[0] = "mutated-before-build"
	viewTables[0] = "mutated-before-build"
	viewTags[0] = "mutated-before-build"
	viewClasses[0] = MigrationClassificationDeprecated

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	mod.Version("v9.9.9").
		Metadata(map[string]string{"team": "mutated-after-build"}).
		Migration(MigrationMetadata{ModuleVersion: "v9.9.9"}).
		TableMigration("messages", MigrationMetadata{Classifications: []MigrationClassification{MigrationClassificationDeprecated}}).
		Reducer("post_build_reducer", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"post:build"}})).
		Query(QueryDeclaration{Name: "post_build_query"}).
		View(ViewDeclaration{Name: "post_build_view"})

	desc := rt.Describe()
	if desc.Module.Version != "v1.2.3" {
		t.Fatalf("runtime module version = %q, want build-time version", desc.Module.Version)
	}
	if got := desc.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("runtime module metadata team = %q, want runtime", got)
	}
	assertMigrationMetadata(t, desc.Module.Migration, MigrationCompatibilityCompatible, MigrationClassificationManualReviewNeeded)
	assertMigrationMetadata(t, desc.Module.TableMigrations["messages"], MigrationCompatibilityBreaking, MigrationClassificationDataRewriteNeeded)
	assertQueryDescriptionMetadata(t, desc.Module.Queries, "recent_messages", "messages:read", "messages", "history", MigrationCompatibilityCompatible, MigrationClassificationAdditive)
	assertViewDescriptionMetadata(t, desc.Module.Views, "live_messages", "messages:subscribe", "messages", "realtime", MigrationCompatibilityUnknown, MigrationClassificationManualReviewNeeded)
	if hasQueryDescription(desc.Module.Queries, "post_build_query") {
		t.Fatalf("runtime queries = %#v, want build-time snapshot only", desc.Module.Queries)
	}
	if hasViewDescription(desc.Module.Views, "post_build_view") {
		t.Fatalf("runtime views = %#v, want build-time snapshot only", desc.Module.Views)
	}

	contract := rt.ExportContract()
	if contract.Module.Version != "v1.2.3" {
		t.Fatalf("contract module version = %q, want build-time version", contract.Module.Version)
	}
	if got := contract.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("contract module metadata team = %q, want runtime", got)
	}
	assertPermissionContractDeclaration(t, contract.Permissions.Reducers, "send_message", "messages:send")
	assertPermissionContractDeclaration(t, contract.Permissions.Queries, "recent_messages", "messages:read")
	assertPermissionContractDeclaration(t, contract.Permissions.Views, "live_messages", "messages:subscribe")
	assertReadModelContractDeclaration(t, contract.ReadModel.Declarations, ReadModelSurfaceQuery, "recent_messages", "messages", "history")
	assertReadModelContractDeclaration(t, contract.ReadModel.Declarations, ReadModelSurfaceView, "live_messages", "messages", "realtime")
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceTable, "messages", MigrationCompatibilityBreaking, MigrationClassificationDataRewriteNeeded)
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceQuery, "recent_messages", MigrationCompatibilityCompatible, MigrationClassificationAdditive)
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceView, "live_messages", MigrationCompatibilityUnknown, MigrationClassificationManualReviewNeeded)
	if hasReducerExport(contract.Schema.Reducers, "post_build_reducer", false) {
		t.Fatalf("contract reducers = %#v, want build-time schema snapshot only", contract.Schema.Reducers)
	}
	if hasPermissionContractDeclaration(contract.Permissions.Reducers, "post_build_reducer") {
		t.Fatalf("contract reducer permissions = %#v, want build-time metadata snapshot only", contract.Permissions.Reducers)
	}
}

func TestRuntimeStoresAuthoredModuleStateInInternalSnapshot(t *testing.T) {
	rt, err := Build(validChatModule().
		Version("v1.2.3").
		Metadata(map[string]string{"team": "runtime"}).
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	snapshot := rt.module.describe()
	if snapshot.Name != "chat" {
		t.Fatalf("snapshot name = %q, want chat", snapshot.Name)
	}
	assertQueryDescription(t, snapshot.Queries, "recent_messages")
	assertViewDescription(t, snapshot.Views, "live_messages")

	snapshot.Metadata["team"] = "mutated"
	snapshot.Queries[0].Name = "mutated_query"
	snapshot.Views[0].Name = "mutated_view"

	again := rt.module.describe()
	if got := again.Metadata["team"]; got != "runtime" {
		t.Fatalf("second snapshot metadata team = %q, want runtime", got)
	}
	assertQueryDescription(t, again.Queries, "recent_messages")
	assertViewDescription(t, again.Views, "live_messages")
}

func TestRuntimeDescribeBoundaryMetadataIsDeeplyDetached(t *testing.T) {
	rt, err := Build(validChatModule().
		Metadata(map[string]string{"team": "runtime"}).
		Migration(MigrationMetadata{Classifications: []MigrationClassification{MigrationClassificationManualReviewNeeded}}).
		TableMigration("messages", MigrationMetadata{Classifications: []MigrationClassification{MigrationClassificationDataRewriteNeeded}}).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
			Migration:   MigrationMetadata{Classifications: []MigrationClassification{MigrationClassificationAdditive}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
			Migration:   MigrationMetadata{Classifications: []MigrationClassification{MigrationClassificationManualReviewNeeded}},
		}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	desc := rt.Describe()
	desc.Module.Metadata["team"] = "mutated"
	desc.Module.Migration.Classifications[0] = MigrationClassificationDeprecated
	tableMigration := desc.Module.TableMigrations["messages"]
	tableMigration.Classifications[0] = MigrationClassificationDeprecated
	desc.Module.TableMigrations["messages"] = tableMigration
	desc.Module.Queries[0].Name = "mutated_query"
	desc.Module.Queries[0].Permissions.Required[0] = "mutated"
	desc.Module.Queries[0].ReadModel.Tables[0] = "mutated"
	desc.Module.Queries[0].ReadModel.Tags[0] = "mutated"
	desc.Module.Queries[0].Migration.Classifications[0] = MigrationClassificationDeprecated
	desc.Module.Views[0].Name = "mutated_view"
	desc.Module.Views[0].Permissions.Required[0] = "mutated"
	desc.Module.Views[0].ReadModel.Tables[0] = "mutated"
	desc.Module.Views[0].ReadModel.Tags[0] = "mutated"
	desc.Module.Views[0].Migration.Classifications[0] = MigrationClassificationDeprecated

	again := rt.Describe()
	if got := again.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("second Describe metadata team = %q, want runtime", got)
	}
	assertMigrationMetadata(t, again.Module.Migration, "", MigrationClassificationManualReviewNeeded)
	assertMigrationMetadata(t, again.Module.TableMigrations["messages"], "", MigrationClassificationDataRewriteNeeded)
	assertQueryDescriptionMetadata(t, again.Module.Queries, "recent_messages", "messages:read", "messages", "history", "", MigrationClassificationAdditive)
	assertViewDescriptionMetadata(t, again.Module.Views, "live_messages", "messages:subscribe", "messages", "realtime", "", MigrationClassificationManualReviewNeeded)
}

func assertMigrationMetadata(t *testing.T, metadata MigrationMetadata, compatibility MigrationCompatibility, classification MigrationClassification) {
	t.Helper()
	if metadata.Compatibility != compatibility {
		t.Fatalf("migration compatibility = %q, want %q", metadata.Compatibility, compatibility)
	}
	for _, got := range metadata.Classifications {
		if got == classification {
			return
		}
	}
	t.Fatalf("migration classifications = %#v, want %q", metadata.Classifications, classification)
}

func assertQueryDescriptionMetadata(t *testing.T, queries []QueryDescription, name, permission, table, tag string, compatibility MigrationCompatibility, classification MigrationClassification) {
	t.Helper()
	for _, query := range queries {
		if query.Name != name {
			continue
		}
		assertPermissionMetadata(t, query.Permissions, permission)
		assertReadModelMetadata(t, query.ReadModel, table, tag)
		assertMigrationMetadata(t, query.Migration, compatibility, classification)
		return
	}
	t.Fatalf("queries = %#v, want query %q", queries, name)
}

func assertViewDescriptionMetadata(t *testing.T, views []ViewDescription, name, permission, table, tag string, compatibility MigrationCompatibility, classification MigrationClassification) {
	t.Helper()
	for _, view := range views {
		if view.Name != name {
			continue
		}
		assertPermissionMetadata(t, view.Permissions, permission)
		assertReadModelMetadata(t, view.ReadModel, table, tag)
		assertMigrationMetadata(t, view.Migration, compatibility, classification)
		return
	}
	t.Fatalf("views = %#v, want view %q", views, name)
}

func assertPermissionMetadata(t *testing.T, metadata PermissionMetadata, permission string) {
	t.Helper()
	if got := metadata.Required; len(got) != 1 || got[0] != permission {
		t.Fatalf("permission metadata = %#v, want %q", metadata, permission)
	}
}

func assertReadModelMetadata(t *testing.T, metadata ReadModelMetadata, table, tag string) {
	t.Helper()
	if got := metadata.Tables; len(got) != 1 || got[0] != table {
		t.Fatalf("read model tables = %#v, want %q", metadata.Tables, table)
	}
	if got := metadata.Tags; len(got) != 1 || got[0] != tag {
		t.Fatalf("read model tags = %#v, want %q", metadata.Tags, tag)
	}
}

func hasPermissionContractDeclaration(declarations []PermissionContractDeclaration, name string) bool {
	for _, declaration := range declarations {
		if declaration.Name == name {
			return true
		}
	}
	return false
}
