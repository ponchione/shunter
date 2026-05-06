package shunter

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestRuntimeExportContractIncludesModuleSchemaDeclarationsAndReservedSections(t *testing.T) {
	rt := buildContractRuntime(t)

	contract := rt.ExportContract()
	if contract.ContractVersion != ModuleContractVersion {
		t.Fatalf("ContractVersion = %d, want %d", contract.ContractVersion, ModuleContractVersion)
	}
	if contract.Module.Name != "chat" {
		t.Fatalf("module name = %q, want chat", contract.Module.Name)
	}
	if contract.Module.Version != "v1.2.3" {
		t.Fatalf("module version = %q, want v1.2.3", contract.Module.Version)
	}
	if got := contract.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("module metadata team = %q, want runtime", got)
	}
	if contract.Schema.Version != 1 {
		t.Fatalf("schema version = %d, want 1", contract.Schema.Version)
	}
	if !hasTableExport(contract.Schema.Tables, "messages") {
		t.Fatalf("tables = %#v, want messages table", contract.Schema.Tables)
	}
	if !hasReducerExport(contract.Schema.Reducers, "send_message", false) {
		t.Fatalf("reducers = %#v, want send_message reducer", contract.Schema.Reducers)
	}
	if !hasReducerExport(contract.Schema.Reducers, "OnConnect", true) {
		t.Fatalf("reducers = %#v, want OnConnect lifecycle reducer", contract.Schema.Reducers)
	}
	assertQueryDescription(t, contract.Queries, "recent_messages")
	assertViewDescription(t, contract.Views, "live_messages")
	if contract.Permissions.Reducers == nil || len(contract.Permissions.Reducers) != 0 {
		t.Fatalf("permission reducers = %#v, want reserved empty slice", contract.Permissions.Reducers)
	}
	if contract.Permissions.Queries == nil || len(contract.Permissions.Queries) != 0 {
		t.Fatalf("permission queries = %#v, want reserved empty slice", contract.Permissions.Queries)
	}
	if contract.Permissions.Views == nil || len(contract.Permissions.Views) != 0 {
		t.Fatalf("permission views = %#v, want reserved empty slice", contract.Permissions.Views)
	}
	if contract.ReadModel.Declarations == nil || len(contract.ReadModel.Declarations) != 0 {
		t.Fatalf("read model declarations = %#v, want reserved empty slice", contract.ReadModel.Declarations)
	}
	if contract.VisibilityFilters == nil || len(contract.VisibilityFilters) != 0 {
		t.Fatalf("visibility filters = %#v, want reserved empty slice", contract.VisibilityFilters)
	}
	if contract.Migrations.Declarations == nil || len(contract.Migrations.Declarations) != 0 {
		t.Fatalf("migration declarations = %#v, want reserved empty slice", contract.Migrations.Declarations)
	}
	if contract.Codegen.ContractFormat != ModuleContractFormat {
		t.Fatalf("codegen contract format = %q, want %q", contract.Codegen.ContractFormat, ModuleContractFormat)
	}
	if contract.Codegen.ContractVersion != ModuleContractVersion {
		t.Fatalf("codegen contract version = %d, want %d", contract.Codegen.ContractVersion, ModuleContractVersion)
	}
	if contract.Codegen.DefaultSnapshotFilename != DefaultContractSnapshotFilename {
		t.Fatalf("codegen default snapshot = %q, want %q", contract.Codegen.DefaultSnapshotFilename, DefaultContractSnapshotFilename)
	}
}

func TestRuntimeExportContractIncludesPermissionAndReadModelMetadata(t *testing.T) {
	mod := validChatModule().
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	assertPermissionContractDeclaration(t, contract.Permissions.Reducers, "send_message", "messages:send")
	assertPermissionContractDeclaration(t, contract.Permissions.Queries, "recent_messages", "messages:read")
	assertPermissionContractDeclaration(t, contract.Permissions.Views, "live_messages", "messages:subscribe")
	assertReadModelContractDeclaration(t, contract.ReadModel.Declarations, ReadModelSurfaceQuery, "recent_messages", "messages", "history")
	assertReadModelContractDeclaration(t, contract.ReadModel.Declarations, ReadModelSurfaceView, "live_messages", "messages", "realtime")
}

func TestRuntimeExportContractIncludesTableReadPolicyMetadata(t *testing.T) {
	mod := NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef(), schema.WithReadPermissions("messages:read"))

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	if len(contract.Schema.Tables) == 0 {
		t.Fatal("contract schema has no tables")
	}
	policy := contract.Schema.Tables[0].ReadPolicy
	if policy.Access != schema.TableAccessPermissioned {
		t.Fatalf("contract read access = %s, want permissioned", policy.Access)
	}
	if len(policy.Permissions) != 1 || policy.Permissions[0] != "messages:read" {
		t.Fatalf("contract read permissions = %#v, want [messages:read]", policy.Permissions)
	}

	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	if !strings.Contains(string(data), `"read_policy": {`) ||
		!strings.Contains(string(data), `"access": "permissioned"`) ||
		!strings.Contains(string(data), `"permissions": [`) ||
		!strings.Contains(string(data), `"messages:read"`) {
		t.Fatalf("contract JSON missing table read policy metadata:\n%s", data)
	}
}

func TestRuntimeExportContractWithAuthoredMetadataValidates(t *testing.T) {
	mod := validChatModule().
		Version("v1.2.3").
		Metadata(map[string]string{"team": "runtime"}).
		Migration(MigrationMetadata{
			ModuleVersion:   "v1.2.3",
			SchemaVersion:   1,
			ContractVersion: ModuleContractVersion,
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
		}).
		TableMigration("messages", MigrationMetadata{
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
		}).
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{
			Required: []string{"messages:send"},
		})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
			},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
			},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := ValidateModuleContract(rt.ExportContract()); err != nil {
		t.Fatalf("ExportContract did not validate: %v", err)
	}
	data, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	var decoded ModuleContract
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	if err := ValidateModuleContract(decoded); err != nil {
		t.Fatalf("decoded ExportContractJSON did not validate: %v", err)
	}
}

func TestModuleContractValidationAllowsMigrationMetadataNamesAcrossSurfaces(t *testing.T) {
	mod := validChatModule().
		TableMigration("messages", MigrationMetadata{
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
		}).
		Query(QueryDeclaration{
			Name: "messages",
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationManualReviewNeeded},
			},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceTable, "messages", MigrationCompatibilityCompatible, MigrationClassificationAdditive)
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceQuery, "messages", MigrationCompatibilityCompatible, MigrationClassificationManualReviewNeeded)
	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected surface-scoped migration metadata names: %v", err)
	}
}

func TestModuleContractValidationAcceptsUUIDColumnType(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Schema.Tables[0].Columns = append(contract.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "external_id",
		Type: "uuid",
	})

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected uuid column type: %v", err)
	}
}

func TestModuleContractValidationAcceptsDurationColumnType(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Schema.Tables[0].Columns = append(contract.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "ttl",
		Type: "duration",
	})

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected duration column type: %v", err)
	}
}

func TestModuleContractValidationAcceptsJSONColumnType(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Schema.Tables[0].Columns = append(contract.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "metadata",
		Type: "json",
	})

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected json column type: %v", err)
	}
}

func TestModuleContractValidationRejectsUnknownColumnType(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Schema.Tables[0].Columns[1].Type = "notAType"

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted unknown column type")
	}
	if !strings.Contains(err.Error(), `schema.tables.messages.columns.body type "notAType" is invalid`) {
		t.Fatalf("ValidateModuleContract error = %v, want invalid schema column type context", err)
	}
}

func TestModuleContractValidationRejectsDuplicateCompositeIndexColumns(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Schema.Tables[0].Indexes = append(contract.Schema.Tables[0].Indexes, schema.IndexExport{
		Name:    "body_body_idx",
		Columns: []string{"body", "body"},
		Unique:  false,
	})

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted duplicate composite index columns")
	}
	if !strings.Contains(err.Error(), `schema.tables.messages.indexes.body_body_idx duplicate index column "body"`) {
		t.Fatalf("ValidateModuleContract error = %v, want duplicate index column context", err)
	}
}

func TestModuleContractValidationRejectsInvalidDeclarationSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Queries = []QueryDescription{{Name: "recent_messages", SQL: "SELECT * FROM missing"}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted invalid query SQL")
	}
	if !strings.Contains(err.Error(), "queries.recent_messages.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want query SQL context", err)
	}

	contract = buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{Name: "live_messages", SQL: "SELECT * FROM missing"}}

	err = ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted invalid view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_messages.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
}

func TestModuleContractValidationAllowsProjectedViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_message_bodies",
		SQL:  "SELECT body AS text FROM messages",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected projected view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsMultiColumnOrderByQuerySQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Queries = []QueryDescription{{
		Name: "ranked_messages",
		SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected multi-column ORDER BY query SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsMultiColumnOrderByViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_messages",
		SQL:  "SELECT * FROM messages ORDER BY body DESC, id ASC",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected multi-column ORDER BY view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsLimitViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_messages",
		SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected LIMIT view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsOffsetViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_messages",
		SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2 OFFSET 1",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected OFFSET view SQL: %v", err)
	}
}

func TestModuleContractValidationRejectsJoinOrderByViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_matching_t_rows",
		SQL:  "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 ORDER BY t.id",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted join ORDER BY view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_matching_t_rows.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live ORDER BY views require a single table") {
		t.Fatalf("ValidateModuleContract error = %v, want single-table ORDER BY unsupported text", err)
	}
}

func TestModuleContractValidationRejectsJoinLimitViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_matching_t_rows",
		SQL:  "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 LIMIT 1",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted join LIMIT view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_matching_t_rows.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live LIMIT views require a single table") {
		t.Fatalf("ValidateModuleContract error = %v, want single-table LIMIT unsupported text", err)
	}
}

func TestModuleContractValidationRejectsJoinOffsetViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_matching_t_rows",
		SQL:  "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 OFFSET 1",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted join OFFSET view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_matching_t_rows.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live OFFSET views require a single table") {
		t.Fatalf("ValidateModuleContract error = %v, want single-table OFFSET unsupported text", err)
	}
}

func TestModuleContractValidationAllowsJoinWhereColumnComparisonQuerySQL(t *testing.T) {
	contract := buildJoinReadContract(t)
	contract.Queries = []QueryDescription{{
		Name: "matching_t_rows",
		SQL:  "SELECT t.id FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected join WHERE column comparison query SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsJoinWhereColumnComparisonViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_matching_t_rows",
		SQL:  "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected join WHERE column comparison view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsCrossJoinWhereColumnEqualityViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_matching_t_rows",
		SQL:  "SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected cross-join WHERE equality view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsSumAggregateQuerySQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Queries = []QueryDescription{{
		Name: "message_id_total",
		SQL:  "SELECT SUM(id) AS total FROM messages",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected SUM aggregate query SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsAggregateOrderByAliasQuerySQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Queries = []QueryDescription{{
		Name: "message_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages ORDER BY n",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected aggregate ORDER BY query SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsCountDistinctAggregateQuerySQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Queries = []QueryDescription{{
		Name: "distinct_message_bodies",
		SQL:  "SELECT COUNT(DISTINCT body) AS n FROM messages",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected COUNT DISTINCT aggregate query SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsCountAggregateViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_message_count",
		SQL:  "SELECT COUNT(body) AS n FROM messages",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected COUNT aggregate view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsSumAggregateViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_message_total",
		SQL:  "SELECT SUM(id) AS total FROM messages",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected SUM aggregate view SQL: %v", err)
	}
}

func TestModuleContractValidationAllowsCountDistinctAggregateViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_messages",
		SQL:  "SELECT COUNT(DISTINCT body) AS n FROM messages",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected COUNT DISTINCT aggregate view SQL: %v", err)
	}
}

func TestModuleContractValidationRejectsSumStringAggregateViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_messages",
		SQL:  "SELECT SUM(body) AS total FROM messages",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted SUM string aggregate view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_messages.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "SUM aggregate only supports 64-bit integer and float columns") {
		t.Fatalf("ValidateModuleContract error = %v, want SUM numeric-column unsupported text", err)
	}
}

func TestModuleContractValidationRejectsSumDistinctAggregateViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_messages",
		SQL:  "SELECT SUM(DISTINCT id) AS total FROM messages",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted SUM DISTINCT aggregate view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_messages.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "only COUNT(DISTINCT column) aggregate projections supported") {
		t.Fatalf("ValidateModuleContract error = %v, want SUM DISTINCT aggregate unsupported text", err)
	}
}

func TestModuleContractValidationAllowsJoinCountStarAggregateViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_join_count",
		SQL:  "SELECT COUNT(*) AS n FROM t JOIN s ON t.u32 = s.u32",
	}}

	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected join COUNT(*) aggregate view SQL: %v", err)
	}
}

func TestModuleContractValidationRejectsJoinSumAggregateViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_join_total",
		SQL:  "SELECT SUM(t.id) AS total FROM t JOIN s ON t.u32 = s.u32",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted join SUM aggregate view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_join_total.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live join aggregate views support COUNT(*) only") {
		t.Fatalf("ValidateModuleContract error = %v, want join COUNT(*)-only unsupported text", err)
	}
}

func TestModuleContractValidationRejectsJoinCountDistinctAggregateViewSQL(t *testing.T) {
	contract := buildJoinReadIndexedContract(t)
	contract.Views = []ViewDescription{{
		Name: "live_join_distinct_count",
		SQL:  "SELECT COUNT(DISTINCT t.id) AS n FROM t JOIN s ON t.u32 = s.u32",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted join COUNT DISTINCT aggregate view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_join_distinct_count.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live join aggregate views support COUNT(*) only") {
		t.Fatalf("ValidateModuleContract error = %v, want join COUNT(*)-only unsupported text", err)
	}
}

func TestModuleContractValidationRejectsAggregateOrderByViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_message_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages ORDER BY n",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted aggregate ORDER BY view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_message_count.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live ORDER BY views do not support aggregate views") {
		t.Fatalf("ValidateModuleContract error = %v, want aggregate ORDER BY unsupported text", err)
	}
}

func TestModuleContractValidationRejectsAggregateLimitViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_message_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages LIMIT 1",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted aggregate LIMIT view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_message_count.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live LIMIT views do not support aggregate views") {
		t.Fatalf("ValidateModuleContract error = %v, want aggregate LIMIT unsupported text", err)
	}
}

func TestModuleContractValidationRejectsAggregateOffsetViewSQL(t *testing.T) {
	contract := buildContractRuntime(t).ExportContract()
	contract.Views = []ViewDescription{{
		Name: "live_message_count",
		SQL:  "SELECT COUNT(*) AS n FROM messages OFFSET 1",
	}}

	err := ValidateModuleContract(contract)
	if err == nil {
		t.Fatal("ValidateModuleContract accepted aggregate OFFSET view SQL")
	}
	if !strings.Contains(err.Error(), "views.live_message_count.sql") {
		t.Fatalf("ValidateModuleContract error = %v, want view SQL context", err)
	}
	if !strings.Contains(err.Error(), "live OFFSET views do not support aggregate views") {
		t.Fatalf("ValidateModuleContract error = %v, want aggregate OFFSET unsupported text", err)
	}
}

func TestModuleContractValidationRejectsInvalidTableReadPolicyMetadata(t *testing.T) {
	tests := []struct {
		name   string
		policy schema.ReadPolicy
	}{
		{
			name:   "invalid access",
			policy: schema.ReadPolicy{Access: schema.TableAccess(99)},
		},
		{
			name:   "permissioned blank tag",
			policy: schema.ReadPolicy{Access: schema.TableAccessPermissioned, Permissions: []string{"messages:read", " "}},
		},
		{
			name:   "permissioned duplicate tag",
			policy: schema.ReadPolicy{Access: schema.TableAccessPermissioned, Permissions: []string{"messages:read", "messages:read"}},
		},
		{
			name:   "public with permissions",
			policy: schema.ReadPolicy{Access: schema.TableAccessPublic, Permissions: []string{"messages:read"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := buildContractRuntime(t).ExportContract()
			contract.Schema.Tables[0].ReadPolicy = tt.policy

			err := ValidateModuleContract(contract)
			if err == nil {
				t.Fatal("ValidateModuleContract accepted invalid table read policy metadata")
			}
			if !strings.Contains(err.Error(), "read_policy") {
				t.Fatalf("ValidateModuleContract error = %v, want read_policy context", err)
			}
		})
	}
}

func TestModuleContractValidationRejectsInvalidDeclaredReadPermissionMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ModuleContract)
	}{
		{
			name: "duplicate query permission tag",
			mutate: func(c *ModuleContract) {
				c.Permissions.Queries = []PermissionContractDeclaration{{
					Name:     "recent_messages",
					Required: []string{"messages:read", "messages:read"},
				}}
			},
		},
		{
			name: "empty query permission requirements",
			mutate: func(c *ModuleContract) {
				c.Permissions.Queries = []PermissionContractDeclaration{{
					Name:     "recent_messages",
					Required: nil,
				}}
			},
		},
		{
			name: "duplicate view permission tag",
			mutate: func(c *ModuleContract) {
				c.Permissions.Views = []PermissionContractDeclaration{{
					Name:     "live_messages",
					Required: []string{"messages:subscribe", "messages:subscribe"},
				}}
			},
		},
		{
			name: "unknown query permission target",
			mutate: func(c *ModuleContract) {
				c.Permissions.Queries = []PermissionContractDeclaration{{
					Name:     "missing",
					Required: []string{"messages:read"},
				}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := buildContractRuntime(t).ExportContract()
			tt.mutate(&contract)

			err := ValidateModuleContract(contract)
			if err == nil {
				t.Fatal("ValidateModuleContract accepted invalid declared read permission metadata")
			}
			if !strings.Contains(err.Error(), "permissions.") {
				t.Fatalf("ValidateModuleContract error = %v, want permissions context", err)
			}
		})
	}
}

func TestBuildRejectsAuthoredMetadataThatWouldInvalidateContract(t *testing.T) {
	tests := []struct {
		name string
		mod  *Module
	}{
		{
			name: "blank module metadata key",
			mod:  validChatModule().Metadata(map[string]string{" ": "ops"}),
		},
		{
			name: "empty reducer permission",
			mod: validChatModule().Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{
				Required: []string{"messages:send", " "},
			})),
		},
		{
			name: "empty query permission",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "recent_messages",
				Permissions: PermissionMetadata{
					Required: []string{"messages:read", ""},
				},
			}),
		},
		{
			name: "duplicate query permission",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "recent_messages",
				Permissions: PermissionMetadata{
					Required: []string{"messages:read", "messages:read"},
				},
			}),
		},
		{
			name: "unknown query read model table",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "recent_messages",
				ReadModel: ReadModelMetadata{
					Tables: []string{"missing"},
				},
			}),
		},
		{
			name: "empty view read model tag",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages",
				ReadModel: ReadModelMetadata{
					Tables: []string{"messages"},
					Tags:   []string{" "},
				},
			}),
		},
		{
			name: "invalid module migration compatibility",
			mod: validChatModule().Migration(MigrationMetadata{
				Compatibility: MigrationCompatibility("unsupported"),
			}),
		},
		{
			name: "invalid table migration classification",
			mod: validChatModule().TableMigration("messages", MigrationMetadata{
				Classifications: []MigrationClassification{MigrationClassification("rewrite")},
			}),
		},
		{
			name: "invalid query migration compatibility",
			mod: validChatModule().Query(QueryDeclaration{
				Name: "recent_messages",
				Migration: MigrationMetadata{
					Compatibility: MigrationCompatibility("unsupported"),
				},
			}),
		},
		{
			name: "invalid view migration classification",
			mod: validChatModule().View(ViewDeclaration{
				Name: "live_messages",
				Migration: MigrationMetadata{
					Classifications: []MigrationClassification{MigrationClassification("rewrite")},
				},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.mod, Config{DataDir: t.TempDir()})
			if err == nil || !errors.Is(err, ErrInvalidModuleMetadata) {
				t.Fatalf("expected ErrInvalidModuleMetadata, got %v", err)
			}
		})
	}
}

func TestBuildInvalidReadModelMetadataDoesNotFreezeModule(t *testing.T) {
	mod := validChatModule().Query(QueryDeclaration{
		Name: "missing_messages",
		ReadModel: ReadModelMetadata{
			Tables: []string{"missing"},
		},
	})

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, ErrInvalidModuleMetadata) {
		t.Fatalf("expected ErrInvalidModuleMetadata, got %v", err)
	}

	missing := messagesTableDef()
	missing.Name = "missing"
	mod.TableDef(missing)
	if _, err := Build(mod, Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build after adding missing read-model table returned error: %v", err)
	}
}

func TestRuntimeExportContractIncludesDeclarationSQLMetadata(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT id FROM messages WHERE body = 'hello' LIMIT 1",
		}).
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages WHERE body = 'hello'",
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	assertQuerySQL(t, contract.Queries, "recent_messages", "SELECT id FROM messages WHERE body = 'hello' LIMIT 1")
	assertViewSQL(t, contract.Views, "live_messages", "SELECT * FROM messages WHERE body = 'hello'")

	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	var decoded ModuleContract
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	assertQuerySQL(t, decoded.Queries, "recent_messages", "SELECT id FROM messages WHERE body = 'hello' LIMIT 1")
	assertViewSQL(t, decoded.Views, "live_messages", "SELECT * FROM messages WHERE body = 'hello'")
}

func TestRuntimeExportContractIncludesVisibilityFilterMetadata(t *testing.T) {
	mod := validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
		Name: "own_messages",
		SQL:  "SELECT * FROM messages WHERE body = :sender",
	})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	if len(contract.VisibilityFilters) != 1 {
		t.Fatalf("visibility filters = %#v, want one filter", contract.VisibilityFilters)
	}
	filter := contract.VisibilityFilters[0]
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
	if err := ValidateModuleContract(contract); err != nil {
		t.Fatalf("ValidateModuleContract rejected visibility filter metadata: %v", err)
	}

	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	if !strings.Contains(string(data), `"visibility_filters": [`) ||
		!strings.Contains(string(data), `"return_table": "messages"`) ||
		!strings.Contains(string(data), `"uses_caller_identity": true`) {
		t.Fatalf("contract JSON missing visibility filter metadata:\n%s", data)
	}
	var decoded ModuleContract
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	if len(decoded.VisibilityFilters) != 1 || decoded.VisibilityFilters[0].Name != "own_messages" {
		t.Fatalf("decoded visibility filters = %#v, want own_messages", decoded.VisibilityFilters)
	}
}

func TestModuleContractValidationRejectsInvalidVisibilityFilterMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ModuleContract)
	}{
		{
			name: "blank name",
			mutate: func(c *ModuleContract) {
				c.VisibilityFilters[0].Name = " "
			},
		},
		{
			name: "invalid SQL",
			mutate: func(c *ModuleContract) {
				c.VisibilityFilters[0].SQL = "SELECT * FROM missing"
			},
		},
		{
			name: "wrong return table",
			mutate: func(c *ModuleContract) {
				c.VisibilityFilters[0].ReturnTable = "missing"
			},
		},
		{
			name: "wrong caller identity metadata",
			mutate: func(c *ModuleContract) {
				c.VisibilityFilters[0].UsesCallerIdentity = false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
				Name: "own_messages",
				SQL:  "SELECT * FROM messages WHERE body = :sender",
			})
			rt, err := Build(mod, Config{DataDir: t.TempDir()})
			if err != nil {
				t.Fatalf("Build returned error: %v", err)
			}
			contract := rt.ExportContract()
			tt.mutate(&contract)

			err = ValidateModuleContract(contract)
			if err == nil {
				t.Fatal("ValidateModuleContract accepted invalid visibility filter metadata")
			}
			if !strings.Contains(err.Error(), "visibility_filters") {
				t.Fatalf("ValidateModuleContract error = %v, want visibility_filters context", err)
			}
		})
	}
}

func TestRuntimeExportContractVisibilityFiltersAreDetached(t *testing.T) {
	mod := validChatModule().VisibilityFilter(VisibilityFilterDeclaration{
		Name: "own_messages",
		SQL:  "SELECT * FROM messages WHERE body = :sender",
	})
	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	contract.VisibilityFilters[0].Name = "mutated"
	contract.VisibilityFilters[0].ReturnTable = "mutated"

	again := rt.ExportContract()
	if len(again.VisibilityFilters) != 1 {
		t.Fatalf("visibility filters = %#v, want one filter", again.VisibilityFilters)
	}
	if again.VisibilityFilters[0].Name != "own_messages" || again.VisibilityFilters[0].ReturnTable != "messages" {
		t.Fatalf("second visibility filter = %#v, want detached metadata", again.VisibilityFilters[0])
	}
}

func TestRuntimeExportContractReturnsDetachedSnapshot(t *testing.T) {
	rt := buildContractRuntime(t)

	contract := rt.ExportContract()
	contract.Module.Metadata["team"] = "mutated"
	contract.Schema.Tables[0].Name = "mutated_table"
	contract.Schema.Tables[0].Columns[0].Name = "mutated_column"
	contract.Schema.Tables[0].Indexes[0].Columns[0] = "mutated_index_column"
	contract.Schema.Reducers[0].Name = "mutated_reducer"
	contract.Queries[0].Name = "mutated_query"
	contract.Views[0].Name = "mutated_view"
	contract.Permissions.Reducers = append(contract.Permissions.Reducers, PermissionContractDeclaration{Name: "mutated"})
	contract.ReadModel.Declarations = append(contract.ReadModel.Declarations, ReadModelContractDeclaration{Name: "mutated"})
	contract.VisibilityFilters = append(contract.VisibilityFilters, VisibilityFilterDescription{Name: "mutated"})
	contract.Migrations.Declarations = append(contract.Migrations.Declarations, MigrationContractDeclaration{Name: "mutated"})

	again := rt.ExportContract()
	if got := again.Module.Metadata["team"]; got != "runtime" {
		t.Fatalf("second contract metadata team = %q, want runtime", got)
	}
	if !hasTableExport(again.Schema.Tables, "messages") {
		t.Fatalf("second contract tables = %#v, want messages table", again.Schema.Tables)
	}
	if !hasReducerExport(again.Schema.Reducers, "send_message", false) {
		t.Fatalf("second contract reducers = %#v, want send_message reducer", again.Schema.Reducers)
	}
	assertQueryDescription(t, again.Queries, "recent_messages")
	assertViewDescription(t, again.Views, "live_messages")
	if len(again.Permissions.Reducers) != 0 {
		t.Fatalf("second contract permission reducers = %#v, want empty", again.Permissions.Reducers)
	}
	if len(again.ReadModel.Declarations) != 0 {
		t.Fatalf("second contract read model declarations = %#v, want empty", again.ReadModel.Declarations)
	}
	if len(again.VisibilityFilters) != 0 {
		t.Fatalf("second contract visibility filters = %#v, want empty", again.VisibilityFilters)
	}
	if len(again.Migrations.Declarations) != 0 {
		t.Fatalf("second contract migration declarations = %#v, want empty", again.Migrations.Declarations)
	}
}

func TestRuntimeExportContractMetadataReturnsDetachedSnapshot(t *testing.T) {
	mod := validChatModule().
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	contract.Permissions.Reducers[0].Required[0] = "mutated"
	contract.Permissions.Queries[0].Required = append(contract.Permissions.Queries[0].Required, "mutated")
	contract.ReadModel.Declarations[0].Tables[0] = "mutated"
	contract.ReadModel.Declarations[0].Tags = append(contract.ReadModel.Declarations[0].Tags, "mutated")

	again := rt.ExportContract()
	assertPermissionContractDeclaration(t, again.Permissions.Reducers, "send_message", "messages:send")
	assertPermissionContractDeclaration(t, again.Permissions.Queries, "recent_messages", "messages:read")
	assertReadModelContractDeclaration(t, again.ReadModel.Declarations, ReadModelSurfaceQuery, "recent_messages", "messages", "history")
}

func TestRuntimeExportContractWorksAcrossLifecycle(t *testing.T) {
	rt := buildContractRuntime(t)

	beforeStart := rt.ExportContract()
	assertQueryDescription(t, beforeStart.Queries, "recent_messages")

	if err := rt.Start(t.Context()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	afterClose := rt.ExportContract()
	if afterClose.Module.Name != "chat" {
		t.Fatalf("module name after close = %q, want chat", afterClose.Module.Name)
	}
	if !hasTableExport(afterClose.Schema.Tables, "messages") {
		t.Fatalf("tables after close = %#v, want messages table", afterClose.Schema.Tables)
	}
}

func TestRuntimeExportContractJSONIsDeterministicAndRoundTrips(t *testing.T) {
	rt := buildContractRuntime(t)

	first, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	second, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("second ExportContractJSON returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("ExportContractJSON was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatalf("ExportContractJSON = %q, want trailing newline", first)
	}

	var decoded ModuleContract
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	if decoded.Module.Name != "chat" {
		t.Fatalf("decoded module name = %q, want chat", decoded.Module.Name)
	}
	if !hasTableExport(decoded.Schema.Tables, "messages") {
		t.Fatalf("decoded tables = %#v, want messages table", decoded.Schema.Tables)
	}
	assertQueryDescription(t, decoded.Queries, "recent_messages")
	assertViewDescription(t, decoded.Views, "live_messages")
	if decoded.Codegen.DefaultSnapshotFilename != DefaultContractSnapshotFilename {
		t.Fatalf("decoded default snapshot = %q, want %q", decoded.Codegen.DefaultSnapshotFilename, DefaultContractSnapshotFilename)
	}
}

func TestRuntimeExportContractJSONUsesCanonicalDeclarationKeys(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT * FROM messages",
		}).
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages",
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	data, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	assertCanonicalDeclarationKeys(t, topLevel["queries"], "recent_messages", "SELECT * FROM messages")
	assertCanonicalDeclarationKeys(t, topLevel["views"], "live_messages", "SELECT * FROM messages")
}

func TestModuleContractJSONAcceptsLegacyDeclarationKeys(t *testing.T) {
	data := []byte(`{
  "contract_version": 1,
  "module": {"name": "chat", "version": "v1.0.0", "metadata": {}},
  "schema": {"version": 1, "tables": [], "reducers": []},
  "queries": [{"Name": "recent_messages", "SQL": "SELECT * FROM messages"}],
  "views": [{"Name": "live_messages", "SQL": "SELECT * FROM messages"}],
  "permissions": {"reducers": [], "queries": [], "views": []},
  "read_model": {"declarations": []},
  "migrations": {"module": {"classifications": []}, "declarations": []},
  "codegen": {
    "contract_format": "shunter.module_contract",
    "contract_version": 1,
    "default_snapshot_filename": "shunter.contract.json"
  }
}`)

	var contract ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("Unmarshal legacy contract JSON: %v", err)
	}
	assertQuerySQL(t, contract.Queries, "recent_messages", "SELECT * FROM messages")
	assertViewSQL(t, contract.Views, "live_messages", "SELECT * FROM messages")
}

func TestRuntimeExportContractJSONDocumentsDefaultSnapshotFilename(t *testing.T) {
	if DefaultContractSnapshotFilename != "shunter.contract.json" {
		t.Fatalf("DefaultContractSnapshotFilename = %q, want shunter.contract.json", DefaultContractSnapshotFilename)
	}
}

func TestRuntimeExportContractOmitsProcessBoundaryMetadata(t *testing.T) {
	rt := buildContractRuntime(t)

	data, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	for _, key := range []string{
		"process_boundary",
		"processBoundary",
		"invocation_protocol",
		"out_of_process",
	} {
		if _, ok := topLevel[key]; ok {
			t.Fatalf("contract JSON unexpectedly included %q: %s", key, data)
		}
	}
}

func buildContractRuntime(t *testing.T) *Runtime {
	t.Helper()
	mod := validChatModule().
		Version("v1.2.3").
		Metadata(map[string]string{"team": "runtime"}).
		Reducer("send_message", noopReducer).
		OnConnect(noopLifecycle).
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt
}

func buildJoinReadContract(t *testing.T) ModuleContract {
	t.Helper()
	rt, err := Build(NewModule("join_contract").
		SchemaVersion(1).
		TableDef(joinReadTableDef("t")).
		TableDef(joinReadTableDef("s")), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt.ExportContract()
}

func buildJoinReadIndexedContract(t *testing.T) ModuleContract {
	t.Helper()
	rt, err := Build(NewModule("join_indexed_contract").
		SchemaVersion(1).
		TableDef(joinReadIndexedTableDef("t")).
		TableDef(joinReadIndexedTableDef("s")), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt.ExportContract()
}

func assertPermissionContractDeclaration(t *testing.T, declarations []PermissionContractDeclaration, name, required string) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Name != name {
			continue
		}
		if len(declaration.Required) != 1 || declaration.Required[0] != required {
			t.Fatalf("permission declaration %q = %#v, want required %q", name, declaration, required)
		}
		return
	}
	t.Fatalf("permission declarations = %#v, want %q", declarations, name)
}

func assertReadModelContractDeclaration(t *testing.T, declarations []ReadModelContractDeclaration, surface, name, table, tag string) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Surface != surface || declaration.Name != name {
			continue
		}
		if len(declaration.Tables) != 1 || declaration.Tables[0] != table {
			t.Fatalf("read model declaration %q/%q tables = %#v, want %q", surface, name, declaration.Tables, table)
		}
		if len(declaration.Tags) != 1 || declaration.Tags[0] != tag {
			t.Fatalf("read model declaration %q/%q tags = %#v, want %q", surface, name, declaration.Tags, tag)
		}
		return
	}
	t.Fatalf("read model declarations = %#v, want %s %q", declarations, surface, name)
}

func assertQuerySQL(t *testing.T, queries []QueryDescription, name, sql string) {
	t.Helper()
	for _, query := range queries {
		if query.Name != name {
			continue
		}
		if query.SQL != sql {
			t.Fatalf("query %q SQL = %q, want %q", name, query.SQL, sql)
		}
		return
	}
	t.Fatalf("queries = %#v, want %q", queries, name)
}

func assertViewSQL(t *testing.T, views []ViewDescription, name, sql string) {
	t.Helper()
	for _, view := range views {
		if view.Name != name {
			continue
		}
		if view.SQL != sql {
			t.Fatalf("view %q SQL = %q, want %q", name, view.SQL, sql)
		}
		return
	}
	t.Fatalf("views = %#v, want %q", views, name)
}

func assertCanonicalDeclarationKeys(t *testing.T, raw json.RawMessage, name, sql string) {
	t.Helper()
	var declarations []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declarations); err != nil {
		t.Fatalf("Unmarshal declarations: %v", err)
	}
	if len(declarations) != 1 {
		t.Fatalf("declarations = %#v, want one declaration", declarations)
	}
	if _, ok := declarations[0]["Name"]; ok {
		t.Fatalf("declaration used legacy Name key: %s", raw)
	}
	if _, ok := declarations[0]["SQL"]; ok {
		t.Fatalf("declaration used legacy SQL key: %s", raw)
	}
	var gotName string
	if err := json.Unmarshal(declarations[0]["name"], &gotName); err != nil {
		t.Fatalf("Unmarshal declaration name: %v", err)
	}
	if gotName != name {
		t.Fatalf("declaration name = %q, want %q", gotName, name)
	}
	var gotSQL string
	if err := json.Unmarshal(declarations[0]["sql"], &gotSQL); err != nil {
		t.Fatalf("Unmarshal declaration sql: %v", err)
	}
	if gotSQL != sql {
		t.Fatalf("declaration sql = %q, want %q", gotSQL, sql)
	}
}
