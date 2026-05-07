package contractdiff

import (
	"errors"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestMigrationPlanReportsReviewActionsAndWarnings(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Schema.Tables = append(current.Schema.Tables, schema.TableExport{
		Name:    "members",
		Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		Indexes: []schema.IndexExport{{Name: "members_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
	})
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	current.Views = append(current.Views, shunter.ViewDescription{Name: "live_messages"})
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility: shunter.MigrationCompatibilityBreaking,
				Classifications: []shunter.MigrationClassification{
					shunter.MigrationClassificationDataRewriteNeeded,
				},
				Notes: "backfill sent_at",
			},
		},
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "members",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
				Notes:           "new table",
			},
		},
		{
			Surface: shunter.MigrationSurfaceQuery,
			Name:    "recent_messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility: shunter.MigrationCompatibilityUnknown,
				Classifications: []shunter.MigrationClassification{
					shunter.MigrationClassificationManualReviewNeeded,
				},
				Notes: "query review",
			},
		},
	}

	plan := Plan(old, current, PlanOptions{
		Policy: PolicyOptions{RequirePreviousVersion: true},
	})

	tableEntry := requirePlanEntry(t, plan, ChangeKindAdditive, SurfaceTable, "members")
	if tableEntry.Action != PlanActionReviewRequired || tableEntry.Severity != PlanSeverityReview {
		t.Fatalf("members entry action/severity = %s/%s, want review required", tableEntry.Action, tableEntry.Severity)
	}
	if tableEntry.MigrationMetadata == nil || tableEntry.MigrationMetadata.Notes != "new table" {
		t.Fatalf("members migration metadata = %#v, want attached metadata", tableEntry.MigrationMetadata)
	}

	columnEntry := requirePlanEntry(t, plan, ChangeKindAdditive, SurfaceColumn, "messages.sent_at")
	if columnEntry.Action != PlanActionExecutionUnsupported || columnEntry.Severity != PlanSeverityBlocking {
		t.Fatalf("sent_at entry action/severity = %s/%s, want unsupported blocking", columnEntry.Action, columnEntry.Severity)
	}
	assertPlanClassification(t, columnEntry, shunter.MigrationClassificationDataRewriteNeeded)

	queryEntry := requirePlanEntry(t, plan, ChangeKindAdditive, SurfaceQuery, "recent_messages")
	if queryEntry.Action != PlanActionManualReviewNeeded || queryEntry.Severity != PlanSeverityWarning {
		t.Fatalf("query entry action/severity = %s/%s, want manual review warning", queryEntry.Action, queryEntry.Severity)
	}
	assertPlanClassification(t, queryEntry, shunter.MigrationClassificationManualReviewNeeded)

	assertPlanWarning(t, plan.Warnings, WarningMissingMigrationMetadata, SurfaceView, "live_messages")
	assertPlanWarning(t, plan.Warnings, WarningMissingPreviousVersion, SurfaceModule, "chat")
	if plan.Summary.Additive != 4 {
		t.Fatalf("summary additive = %d, want 4", plan.Summary.Additive)
	}
	if plan.Summary.DataRewriteNeeded != 1 || plan.Summary.ExecutionUnsupported != 1 {
		t.Fatalf("summary rewrite/unsupported = %d/%d, want 1/1", plan.Summary.DataRewriteNeeded, plan.Summary.ExecutionUnsupported)
	}
	if plan.Summary.ManualReviewNeeded != 1 {
		t.Fatalf("summary manual review = %d, want 1", plan.Summary.ManualReviewNeeded)
	}
}

func TestMigrationPlanReportsBreakingIndexChangesAndPolicyDisagreement(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns[0].Type = "string"
	current.Schema.Tables[0].Indexes[0].Unique = false
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility: shunter.MigrationCompatibilityCompatible,
			},
		},
	}

	plan := Plan(old, current, PlanOptions{})

	columnEntry := requirePlanEntry(t, plan, ChangeKindBreaking, SurfaceColumn, "messages.id")
	if columnEntry.Action != PlanActionManualReviewNeeded || columnEntry.Severity != PlanSeverityBlocking {
		t.Fatalf("column entry action/severity = %s/%s, want blocking manual review", columnEntry.Action, columnEntry.Severity)
	}
	indexEntry := requirePlanEntry(t, plan, ChangeKindBreaking, SurfaceIndex, "messages.messages_pk")
	if indexEntry.Action != PlanActionManualReviewNeeded || indexEntry.Severity != PlanSeverityBlocking {
		t.Fatalf("index entry action/severity = %s/%s, want blocking manual review", indexEntry.Action, indexEntry.Severity)
	}
	assertPlanWarning(t, plan.Warnings, WarningRiskyChangeDeclaredCompatible, SurfaceColumn, "messages.id")
	assertPlanWarning(t, plan.Warnings, WarningRiskyChangeDeclaredCompatible, SurfaceIndex, "messages.messages_pk")
	if plan.Summary.Breaking != 2 || plan.Summary.Blocking != 2 {
		t.Fatalf("summary breaking/blocking = %d/%d, want 2/2", plan.Summary.Breaking, plan.Summary.Blocking)
	}
	if !plan.Summary.BackupRecommended {
		t.Fatal("summary backup recommended = false, want true for blocking changes")
	}
}

func TestMigrationPlanAddsBackupRestoreGuidanceForBlockingChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns[0].Type = "string"

	plan := Plan(old, current, PlanOptions{})

	if !plan.Summary.BackupRecommended {
		t.Fatal("BackupRecommended = false, want true")
	}
	if len(plan.Guidance) != 1 {
		t.Fatalf("guidance count = %d, want 1: %#v", len(plan.Guidance), plan.Guidance)
	}
	if plan.Guidance[0].Code != PlanGuidanceBackupRestore {
		t.Fatalf("guidance code = %s, want %s", plan.Guidance[0].Code, PlanGuidanceBackupRestore)
	}
	for _, want := range []string{"durable DataDir", "shunter.BackupDataDir", "shunter.RestoreDataDir"} {
		if !strings.Contains(plan.Guidance[0].Detail, want) {
			t.Fatalf("guidance detail = %q, want substring %q", plan.Guidance[0].Detail, want)
		}
	}
	if !strings.Contains(plan.Text(), "guidance backup-restore:") {
		t.Fatalf("plan text missing backup guidance:\n%s", plan.Text())
	}
	jsonOut, err := plan.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	if !strings.Contains(string(jsonOut), `"backup_recommended": true`) {
		t.Fatalf("plan JSON missing backup recommendation:\n%s", jsonOut)
	}
	if !strings.Contains(string(jsonOut), `"guidance": [`) {
		t.Fatalf("plan JSON missing guidance array:\n%s", jsonOut)
	}
}

func TestMigrationPlanOmitsBackupRestoreGuidanceForReviewOnlyChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})

	plan := Plan(old, current, PlanOptions{})

	if plan.Summary.BackupRecommended {
		t.Fatal("BackupRecommended = true, want false for review-only additive changes")
	}
	if len(plan.Guidance) != 0 {
		t.Fatalf("guidance = %#v, want empty", plan.Guidance)
	}
}

func TestMigrationPlanReportsMetadataOnlyChangesSeparately(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Module.Version = "v1.1.0"
	current.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceQuery,
		Name:    "history",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}
	current.Migrations.Module = shunter.MigrationMetadata{
		ModuleVersion:   "v1.1.0",
		SchemaVersion:   1,
		ContractVersion: shunter.ModuleContractVersion,
		PreviousVersion: "v1.0.0",
		Compatibility:   shunter.MigrationCompatibilityCompatible,
		Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationDeprecated},
		Notes:           "metadata-only release",
	}

	plan := Plan(old, current, PlanOptions{})

	requirePlanEntry(t, plan, ChangeKindMetadata, SurfaceModule, "chat")
	requirePlanEntry(t, plan, ChangeKindMetadata, SurfaceReadModel, "query.history")
	migrationEntry := requirePlanEntry(t, plan, ChangeKindMetadata, SurfaceMigrationMetadata, "module")
	if migrationEntry.MigrationMetadata == nil || migrationEntry.MigrationMetadata.Notes != "metadata-only release" {
		t.Fatalf("migration metadata entry = %#v, want current module metadata", migrationEntry.MigrationMetadata)
	}
	if plan.Summary.MetadataOnly != 3 {
		t.Fatalf("summary metadata only = %d, want 3", plan.Summary.MetadataOnly)
	}
}

func TestMigrationPlanClassifiesStricterReadPolicyAsBreaking(t *testing.T) {
	old := contractFixture()
	old.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read"},
	}

	plan := Plan(old, current, PlanOptions{})

	entry := requirePlanEntry(t, plan, ChangeKindBreaking, SurfaceTableReadPolicy, "messages")
	if entry.Action != PlanActionManualReviewNeeded || entry.Severity != PlanSeverityBlocking {
		t.Fatalf("read policy entry action/severity = %s/%s, want blocking manual review", entry.Action, entry.Severity)
	}
}

func TestMigrationPlanClassifiesLooserReadPolicyAsManualReview(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}

	plan := Plan(old, current, PlanOptions{})

	entry := requirePlanEntry(t, plan, ChangeKindAdditive, SurfaceTableReadPolicy, "messages")
	if entry.Action != PlanActionManualReviewNeeded || entry.Severity != PlanSeverityWarning {
		t.Fatalf("read policy entry action/severity = %s/%s, want warning manual review", entry.Action, entry.Severity)
	}
	assertPlanClassification(t, entry, shunter.MigrationClassificationManualReviewNeeded)
}

func TestMigrationPlanClassifiesReducerPermissionChangesByAccessImpact(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "send_message",
		Required: []string{"messages:send"},
	}}

	plan := Plan(old, current, PlanOptions{})

	entry := requirePlanEntry(t, plan, ChangeKindBreaking, SurfacePermission, "reducer.send_message")
	if entry.Action != PlanActionManualReviewNeeded || entry.Severity != PlanSeverityBlocking {
		t.Fatalf("stricter reducer permission action/severity = %s/%s, want blocking manual review", entry.Action, entry.Severity)
	}

	old = current
	current = contractFixture()

	plan = Plan(old, current, PlanOptions{})

	entry = requirePlanEntry(t, plan, ChangeKindAdditive, SurfacePermission, "reducer.send_message")
	if entry.Action != PlanActionManualReviewNeeded || entry.Severity != PlanSeverityWarning {
		t.Fatalf("looser reducer permission action/severity = %s/%s, want warning manual review", entry.Action, entry.Severity)
	}
	assertPlanClassification(t, entry, shunter.MigrationClassificationManualReviewNeeded)
}

func TestMigrationPlanIgnoresPermissionOrderOnlyChanges(t *testing.T) {
	old := contractFixture()
	old.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read", "messages:audit"},
	}
	old.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "send_message",
		Required: []string{"messages:send", "messages:audit"},
	}}
	old.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:read", "messages:audit"},
	}}
	old.Permissions.Views = []shunter.PermissionContractDeclaration{{
		Name:     "live",
		Required: []string{"messages:subscribe", "messages:audit"},
	}}
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:audit", "messages:read"},
	}
	current.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "send_message",
		Required: []string{"messages:audit", "messages:send"},
	}}
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:audit", "messages:read"},
	}}
	current.Permissions.Views = []shunter.PermissionContractDeclaration{{
		Name:     "live",
		Required: []string{"messages:audit", "messages:subscribe"},
	}}

	plan := Plan(old, current, PlanOptions{})

	if len(plan.Entries) != 0 || len(plan.Warnings) != 0 {
		t.Fatalf("plan = %#v, want no entries or warnings for order-only permission changes", plan)
	}
}

func TestMigrationPlanJSONIsDeterministicAndNewlineTerminated(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})

	plan, err := PlanJSON(mustContractJSON(t, old), mustContractJSON(t, current), PlanOptions{})
	if err != nil {
		t.Fatalf("PlanJSON returned error: %v", err)
	}
	first, err := plan.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	second, err := plan.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("second MarshalCanonicalJSON returned error: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("plan JSON was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.HasSuffix(string(first), "\n") {
		t.Fatalf("plan JSON is not newline terminated: %q", first)
	}
	if !strings.Contains(string(first), `"action": "review-required"`) {
		t.Fatalf("plan JSON missing review action:\n%s", first)
	}
}

func TestMigrationPlanJSONFailsClearlyForSemanticInvalidContract(t *testing.T) {
	_, err := PlanJSON([]byte(`{}`), mustContractJSON(t, contractFixture()), PlanOptions{})
	if err == nil {
		t.Fatal("PlanJSON returned nil error, want invalid contract")
	}
	if !errors.Is(err, ErrInvalidContractJSON) {
		t.Fatalf("PlanJSON error = %v, want ErrInvalidContractJSON", err)
	}
	if !strings.Contains(err.Error(), "previous contract") {
		t.Fatalf("PlanJSON error = %v, want previous contract context", err)
	}
}

func TestMigrationPlanValidationWarningsAreReadOnlyContractChecks(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Module.Version = "v1.1.0"
	current.Migrations.Module = shunter.MigrationMetadata{
		ModuleVersion:   "v2.0.0",
		SchemaVersion:   99,
		ContractVersion: 99,
		PreviousVersion: "v0.9.0",
	}

	plan := Plan(old, current, PlanOptions{ValidateContracts: true})

	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataModuleVersionMismatch, SurfaceModule, "chat")
	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataSchemaVersionMismatch, SurfaceSchema, "schema")
	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataContractVersionMismatch, SurfaceContract, "contract")
	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataPreviousVersionMismatch, SurfaceModule, "chat")
	if plan.Summary.PolicyFailed {
		t.Fatal("validation warnings should not make policy fail without strict mode")
	}
}

func TestMigrationPlanValidationWarningsCoverDeclarationMetadata(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Module.Version = "v1.1.0"
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceQuery,
			Name:    "history",
			Metadata: shunter.MigrationMetadata{
				ModuleVersion:   "v2.0.0",
				SchemaVersion:   99,
				ContractVersion: 99,
				PreviousVersion: "v0.9.0",
			},
		},
	}

	plan := Plan(old, current, PlanOptions{ValidateContracts: true})

	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataModuleVersionMismatch, SurfaceQuery, "history")
	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataSchemaVersionMismatch, SurfaceQuery, "history")
	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataContractVersionMismatch, SurfaceQuery, "history")
	assertPlanWarning(t, plan.Warnings, WarningMigrationMetadataPreviousVersionMismatch, SurfaceQuery, "history")
	if !strings.Contains(plan.Text(), "query history migration metadata version") {
		t.Fatalf("plan text missing declaration validation context:\n%s", plan.Text())
	}
}

func TestMigrationPlanValidationWarningsCoverRegressionsAndCodegenMetadata(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Version = old.Schema.Version - 1
	current.ContractVersion = old.ContractVersion - 1
	current.Codegen.ContractFormat = "unexpected.format"
	current.Codegen.ContractVersion = current.ContractVersion + 1

	plan := Plan(old, current, PlanOptions{ValidateContracts: true})

	assertPlanWarning(t, plan.Warnings, WarningSchemaVersionRegressed, SurfaceSchema, "schema")
	assertPlanWarning(t, plan.Warnings, WarningContractVersionRegressed, SurfaceContract, "contract")
	assertPlanWarning(t, plan.Warnings, WarningContractFormatMismatch, SurfaceContract, "contract")
	assertPlanWarning(t, plan.Warnings, WarningCodegenContractVersionMismatch, SurfaceContract, "contract")
	if plan.Summary.Warnings != len(plan.Warnings) {
		t.Fatalf("summary warnings = %d, want %d", plan.Summary.Warnings, len(plan.Warnings))
	}
}

func requirePlanEntry(t *testing.T, plan MigrationPlan, kind ChangeKind, surface Surface, name string) PlanEntry {
	t.Helper()
	for _, entry := range plan.Entries {
		if entry.Kind == kind && entry.Surface == surface && entry.Name == name {
			return entry
		}
	}
	t.Fatalf("plan entries = %#v, want %s %s %s", plan.Entries, kind, surface, name)
	return PlanEntry{}
}

func assertPlanClassification(t *testing.T, entry PlanEntry, classification shunter.MigrationClassification) {
	t.Helper()
	for _, got := range entry.Classifications {
		if got == classification {
			return
		}
	}
	t.Fatalf("entry classifications = %#v, want %s", entry.Classifications, classification)
}

func assertPlanWarning(t *testing.T, warnings []PlanWarning, code WarningCode, surface Surface, name string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code && warning.Surface == surface && warning.Name == name {
			return
		}
	}
	t.Fatalf("warnings = %#v, want %s %s %s", warnings, code, surface, name)
}
