package contractdiff

import (
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestPolicyWarnsForMissingMigrationMetadataWithoutFailingByDefault(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{})
	assertWarning(t, result.Warnings, WarningMissingMigrationMetadata, SurfaceQuery, "recent_messages")
	if result.Failed {
		t.Fatal("default policy result failed, want report-only warnings")
	}
}

func TestPolicyStrictModeFailsOnWarnings(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Views = append(current.Views, shunter.ViewDescription{Name: "live_messages"})

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{Strict: true})
	assertWarning(t, result.Warnings, WarningMissingMigrationMetadata, SurfaceView, "live_messages")
	if !result.Failed {
		t.Fatal("strict policy result did not fail on warnings")
	}
}

func TestPolicyWarnsWhenBreakingChangeDeclaredCompatible(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns[0].Type = "string"
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility: shunter.MigrationCompatibilityCompatible,
			},
		},
	}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{})
	assertWarning(t, result.Warnings, WarningRiskyChangeDeclaredCompatible, SurfaceColumn, "messages.id")
}

func TestPolicyWarnsWhenAdditiveChangeDeclaredBreaking(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility: shunter.MigrationCompatibilityBreaking,
			},
		},
	}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{})
	assertWarning(t, result.Warnings, WarningBreakingDeclaredForAdditiveChange, SurfaceColumn, "messages.sent_at")
}

func TestPolicyRequiresMigrationMetadataForDeclaredReadPermissionChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:read"},
	}}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{})
	assertWarning(t, result.Warnings, WarningMissingMigrationMetadata, SurfacePermission, "query.history")

	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceQuery,
		Name:    "history",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityBreaking,
			Notes:         "tighten query permission",
		},
	}}
	result = CheckPolicy(Compare(old, current), current, PolicyOptions{Strict: true})
	if result.Failed {
		t.Fatalf("strict policy failed despite query migration metadata: %#v", result.Warnings)
	}
}

func TestPolicyRequiresMigrationMetadataForAdditiveTableReadPolicyChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{})
	assertWarning(t, result.Warnings, WarningMissingMigrationMetadata, SurfaceTableReadPolicy, "messages")

	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceTable,
		Name:    "messages",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
			Notes:         "loosen read policy",
		},
	}}
	result = CheckPolicy(Compare(old, current), current, PolicyOptions{Strict: true})
	if result.Failed {
		t.Fatalf("strict policy failed despite table read-policy migration metadata: %#v", result.Warnings)
	}
}

func TestPolicyAllowsModuleMigrationMetadataForVisibilityFilterChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:          "published_messages",
		SQL:           "SELECT * FROM messages WHERE body = 'published'",
		ReturnTable:   "messages",
		ReturnTableID: 0,
	}}
	current.Migrations.Module = shunter.MigrationMetadata{
		Compatibility: shunter.MigrationCompatibilityBreaking,
		Notes:         "visibility policy changed",
	}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{Strict: true})
	if hasWarning(result.Warnings, WarningMissingMigrationMetadata, SurfaceVisibilityFilter, "published_messages") {
		t.Fatalf("visibility filter change should be covered by module migration metadata: %#v", result.Warnings)
	}
	if result.Failed {
		t.Fatalf("strict policy failed despite module migration metadata: %#v", result.Warnings)
	}
}

func TestPolicyAllowsModuleMigrationMetadataForRemovedReadSurfaces(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Queries = nil
	current.Views = nil
	current.Migrations.Module = shunter.MigrationMetadata{
		Compatibility: shunter.MigrationCompatibilityBreaking,
		Notes:         "remove legacy read surfaces",
	}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{Strict: true})
	if hasWarning(result.Warnings, WarningMissingMigrationMetadata, SurfaceQuery, "history") {
		t.Fatalf("query removal should be covered by module migration metadata: %#v", result.Warnings)
	}
	if hasWarning(result.Warnings, WarningMissingMigrationMetadata, SurfaceView, "live") {
		t.Fatalf("view removal should be covered by module migration metadata: %#v", result.Warnings)
	}
	if result.Failed {
		t.Fatalf("strict policy failed despite module migration metadata: %#v", result.Warnings)
	}
}

func TestPolicyAllowsModuleMigrationMetadataForRemovedTables(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables = nil
	current.Migrations.Module = shunter.MigrationMetadata{
		Compatibility: shunter.MigrationCompatibilityBreaking,
		Notes:         "remove legacy table",
	}

	result := CheckPolicy(Compare(old, current), current, PolicyOptions{Strict: true})
	if hasWarning(result.Warnings, WarningMissingMigrationMetadata, SurfaceTable, "messages") {
		t.Fatalf("table removal should be covered by module migration metadata: %#v", result.Warnings)
	}
	if result.Failed {
		t.Fatalf("strict policy failed despite module migration metadata: %#v", result.Warnings)
	}
}

func TestPolicyCanRequirePreviousVersionReference(t *testing.T) {
	current := contractFixture()

	result := CheckPolicy(Report{}, current, PolicyOptions{RequirePreviousVersion: true})
	assertWarning(t, result.Warnings, WarningMissingPreviousVersion, SurfaceModule, "chat")
}

func assertWarning(t *testing.T, warnings []PolicyWarning, code WarningCode, surface Surface, name string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code && warning.Surface == surface && warning.Name == name {
			return
		}
	}
	t.Fatalf("warnings = %#v, want %s %s %s", warnings, code, surface, name)
}

func hasWarning(warnings []PolicyWarning, code WarningCode, surface Surface, name string) bool {
	for _, warning := range warnings {
		if warning.Code == code && warning.Surface == surface && warning.Name == name {
			return true
		}
	}
	return false
}
