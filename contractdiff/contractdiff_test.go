package contractdiff

import (
	"errors"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestContractDiffIdenticalContractsProduceNoChanges(t *testing.T) {
	report := Compare(contractFixture(), contractFixture())

	if len(report.Changes) != 0 {
		t.Fatalf("changes = %#v, want none", report.Changes)
	}
	if text := report.Text(); text != "No contract changes.\n" {
		t.Fatalf("Text() = %q, want no changes line", text)
	}
}

func TestContractDiffDetectsAdditiveSurfaceChangesDeterministically(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables = append(current.Schema.Tables, schema.TableExport{
		Name:    "members",
		Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		Indexes: []schema.IndexExport{{Name: "members_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
	})
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	current.Views = append(current.Views, shunter.ViewDescription{Name: "live_messages"})

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceColumn, "messages.sent_at")
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceTable, "members")
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceQuery, "recent_messages")
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceView, "live_messages")

	want := strings.Join([]string{
		"additive column messages.sent_at: column added with type timestamp",
		"additive query recent_messages: query added",
		"additive table members: table added",
		"additive view live_messages: view added",
		"",
	}, "\n")
	if got := report.Text(); got != want {
		t.Fatalf("Text() =\n%s\nwant:\n%s", got, want)
	}
}

func TestContractDiffDetectsBreakingSurfaceChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns[0].Type = "string"
	current.Schema.Reducers = nil
	current.Queries = nil
	current.Views = nil

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceColumn, "messages.id")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceReducer, "send_message")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
}

func TestContractDiffReportsMetadataOnlyChangesSeparately(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Module.Version = "v1.1.0"
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{Name: "history", Required: []string{"messages:read"}}}
	current.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceQuery,
		Name:    "history",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceModule, "chat")
	assertChange(t, report.Changes, ChangeKindMetadata, SurfacePermission, "query.history")
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceReadModel, "query.history")
}

func TestContractDiffDetectsDeclaredReadSQLChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Queries[0].SQL = "SELECT * FROM messages"
	current.Views[0].SQL = "SELECT * FROM messages"

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceView, "live")

	old = current
	current.Queries[0].SQL = "SELECT id FROM messages"
	current.Views[0].SQL = ""

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
}

func TestContractDiffJSONFailsClearlyForMalformedInput(t *testing.T) {
	_, err := CompareJSON([]byte(`{`), mustContractJSON(t, contractFixture()))
	if err == nil {
		t.Fatal("CompareJSON returned nil error, want invalid contract")
	}
	if !errors.Is(err, ErrInvalidContractJSON) {
		t.Fatalf("CompareJSON error = %v, want ErrInvalidContractJSON", err)
	}
}

func contractFixture() shunter.ModuleContract {
	return shunter.ModuleContract{
		ContractVersion: shunter.ModuleContractVersion,
		Module: shunter.ModuleContractIdentity{
			Name:     "chat",
			Version:  "v1.0.0",
			Metadata: map[string]string{},
		},
		Schema: schema.SchemaExport{
			Version: 1,
			Tables: []schema.TableExport{
				{
					Name: "messages",
					Columns: []schema.ColumnExport{
						{Name: "id", Type: "uint64"},
						{Name: "body", Type: "string"},
					},
					Indexes: []schema.IndexExport{{Name: "messages_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
				},
			},
			Reducers: []schema.ReducerExport{{Name: "send_message"}},
		},
		Queries: []shunter.QueryDescription{{Name: "history"}},
		Views:   []shunter.ViewDescription{{Name: "live"}},
		Permissions: shunter.PermissionContract{
			Reducers: []shunter.PermissionContractDeclaration{},
			Queries:  []shunter.PermissionContractDeclaration{},
			Views:    []shunter.PermissionContractDeclaration{},
		},
		ReadModel: shunter.ReadModelContract{Declarations: []shunter.ReadModelContractDeclaration{}},
		Migrations: shunter.MigrationContract{
			Module:       shunter.MigrationMetadata{Classifications: []shunter.MigrationClassification{}},
			Declarations: []shunter.MigrationContractDeclaration{},
		},
		Codegen: shunter.CodegenContractMetadata{
			ContractFormat:          shunter.ModuleContractFormat,
			ContractVersion:         shunter.ModuleContractVersion,
			DefaultSnapshotFilename: shunter.DefaultContractSnapshotFilename,
		},
	}
}

func mustContractJSON(t *testing.T, contract shunter.ModuleContract) []byte {
	t.Helper()
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	return data
}

func assertChange(t *testing.T, changes []Change, kind ChangeKind, surface Surface, name string) {
	t.Helper()
	for _, change := range changes {
		if change.Kind == kind && change.Surface == surface && change.Name == name {
			return
		}
	}
	t.Fatalf("changes = %#v, want %s %s %s", changes, kind, surface, name)
}
