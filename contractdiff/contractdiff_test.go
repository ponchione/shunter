package contractdiff

import (
	"encoding/json"
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

func TestContractDiffDetectsNullableColumnChange(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns[0].Nullable = true

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceColumn, "messages.id")
	if text := report.Text(); !strings.Contains(text, "column nullable changed from false to true") {
		t.Fatalf("diff text = %q, want nullable change", text)
	}
}

func TestContractDiffReportsUUIDColumnType(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "external_id",
		Type: "uuid",
	})

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceColumn, "messages.external_id")
	if got := report.Text(); !strings.Contains(got, "additive column messages.external_id: column added with type uuid") {
		t.Fatalf("Text() = %q, want additive uuid column detail", got)
	}

	old = contractFixture()
	old.Schema.Tables[0].Columns[0].Type = "uuid"
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceColumn, "messages.id")
	if got := report.Text(); !strings.Contains(got, "breaking column messages.id: column type changed from uuid to uint64") {
		t.Fatalf("Text() = %q, want breaking uuid column detail", got)
	}
}

func TestContractDiffReportsDurationColumnType(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "ttl",
		Type: "duration",
	})

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceColumn, "messages.ttl")
	if got := report.Text(); !strings.Contains(got, "additive column messages.ttl: column added with type duration") {
		t.Fatalf("Text() = %q, want additive duration column detail", got)
	}

	old = contractFixture()
	old.Schema.Tables[0].Columns[0].Type = "duration"
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceColumn, "messages.id")
	if got := report.Text(); !strings.Contains(got, "breaking column messages.id: column type changed from duration to uint64") {
		t.Fatalf("Text() = %q, want breaking duration column detail", got)
	}
}

func TestContractDiffReportsJSONColumnType(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{
		Name: "metadata",
		Type: "json",
	})

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceColumn, "messages.metadata")
	if got := report.Text(); !strings.Contains(got, "additive column messages.metadata: column added with type json") {
		t.Fatalf("Text() = %q, want additive json column detail", got)
	}

	old = contractFixture()
	old.Schema.Tables[0].Columns[0].Type = "json"
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceColumn, "messages.id")
	if got := report.Text(); !strings.Contains(got, "breaking column messages.id: column type changed from json to uint64") {
		t.Fatalf("Text() = %q, want breaking json column detail", got)
	}
}

func TestContractDiffReportsMetadataOnlyChangesSeparately(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Module.Version = "v1.1.0"
	current.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceQuery,
		Name:    "history",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceModule, "chat")
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceReadModel, "query.history")
}

func TestContractDiffDetectsTableReadPolicyChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read"},
	}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceTableReadPolicy, "messages")

	old = current
	current = contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceTableReadPolicy, "messages")

	old = current
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceTableReadPolicy, "messages")
}

func TestContractDiffIgnoresTableReadPolicyPermissionOrder(t *testing.T) {
	old := contractFixture()
	old.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read", "messages:audit"},
	}
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:audit", "messages:read"},
	}

	report := Compare(old, current)

	assertNoChange(t, report.Changes, SurfaceTableReadPolicy, "messages")
}

func TestContractDiffClassifiesDeclaredReadPermissionChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:read"},
	}}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfacePermission, "query.history")

	old = current
	current = contractFixture()
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:read", "messages:audit"},
	}}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfacePermission, "query.history")

	old = current
	current = contractFixture()
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:read"},
	}}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfacePermission, "query.history")

	old = current
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfacePermission, "query.history")

	old = contractFixture()
	current = contractFixture()
	current.Permissions.Views = []shunter.PermissionContractDeclaration{{
		Name:     "live",
		Required: []string{"messages:subscribe"},
	}}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfacePermission, "view.live")
}

func TestContractDiffClassifiesReducerPermissionChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "send_message",
		Required: []string{"messages:send"},
	}}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfacePermission, "reducer.send_message")

	old = current
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfacePermission, "reducer.send_message")
}

func TestContractDiffDetectsReducerProductSchemaChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Schema.Reducers[0].Args = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "body", Type: "string"}}}
	current.Schema.Reducers[0].Result = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "message_id", Type: "uint64"}}}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceReducer, "send_message")
	if text := report.Text(); !strings.Contains(text, "reducer args schema added") || !strings.Contains(text, "reducer result schema added") {
		t.Fatalf("Text() = %q, want reducer product schema additions", text)
	}

	old = current
	current = contractFixture()
	current.Schema.Reducers[0].Args = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "body", Type: "uint64"}}}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceReducer, "send_message")
	if text := report.Text(); !strings.Contains(text, "reducer args schema changed") ||
		!strings.Contains(text, "reducer result schema removed") {
		t.Fatalf("Text() = %q, want reducer product schema breaking changes", text)
	}
}

func TestContractDiffDetectsDeclaredReadResultMetadataChanges(t *testing.T) {
	old := contractFixture()
	old.Queries[0].SQL = "SELECT id FROM messages"
	old.Views[0].SQL = "SELECT id FROM messages"
	current := contractFixture()
	current.Queries[0].SQL = "SELECT id FROM messages"
	current.Views[0].SQL = "SELECT id FROM messages"
	current.Queries[0].RowSchema = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "id", Type: "uint64"}}}
	current.Queries[0].ResultShape = &shunter.ReadResultShape{Kind: shunter.ReadResultShapeProjection, Table: "messages"}
	current.Views[0].RowSchema = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "id", Type: "uint64"}}}
	current.Views[0].ResultShape = &shunter.ReadResultShape{Kind: shunter.ReadResultShapeProjection, Table: "messages"}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceView, "live")
	if text := report.Text(); !strings.Contains(text, "query row schema added") ||
		!strings.Contains(text, "view result shape added") {
		t.Fatalf("Text() = %q, want declared-read metadata additions", text)
	}

	old = current
	current = contractFixture()
	current.Queries[0].SQL = "SELECT id FROM messages"
	current.Views[0].SQL = "SELECT id FROM messages"
	current.Queries[0].RowSchema = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "id", Type: "string"}}}
	current.Queries[0].ResultShape = &shunter.ReadResultShape{Kind: shunter.ReadResultShapeProjection, Table: "messages"}
	current.Views[0].RowSchema = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "id", Type: "uint64"}}}
	current.Views[0].ResultShape = &shunter.ReadResultShape{Kind: shunter.ReadResultShapeTable, Table: "messages"}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
	if text := report.Text(); !strings.Contains(text, "query row schema changed") ||
		!strings.Contains(text, "view result shape changed") {
		t.Fatalf("Text() = %q, want declared-read metadata breaking changes", text)
	}
}

func TestContractDiffClassifiesDeclaredReadParameterChanges(t *testing.T) {
	t.Run("adding parameters to executable reads is breaking", func(t *testing.T) {
		old := contractFixture()
		old.Queries[0].SQL = "SELECT * FROM messages"
		old.Views[0].SQL = "SELECT * FROM messages"
		current := contractFixture()
		current.Queries[0].SQL = old.Queries[0].SQL
		current.Views[0].SQL = old.Views[0].SQL
		current.Queries[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}
		current.Views[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}

		report := Compare(old, current)

		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
		if text := report.Text(); !strings.Contains(text, "query parameters added") ||
			!strings.Contains(text, "view parameters added") {
			t.Fatalf("Text() = %q, want declared-read parameter additions", text)
		}
	})

	t.Run("adding parameters to metadata-only reads is additive", func(t *testing.T) {
		old := contractFixture()
		current := contractFixture()
		current.Queries[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}
		current.Views[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}

		report := Compare(old, current)

		assertChange(t, report.Changes, ChangeKindAdditive, SurfaceQuery, "history")
		assertChange(t, report.Changes, ChangeKindAdditive, SurfaceView, "live")
		if text := report.Text(); !strings.Contains(text, "query parameters added") ||
			!strings.Contains(text, "view parameters added") {
			t.Fatalf("Text() = %q, want metadata-only parameter additions", text)
		}
	})

	t.Run("removing parameters is breaking", func(t *testing.T) {
		old := contractFixture()
		old.Queries[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}
		old.Views[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}
		current := contractFixture()

		report := Compare(old, current)

		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
		if text := report.Text(); !strings.Contains(text, "query parameters removed") ||
			!strings.Contains(text, "view parameters removed") {
			t.Fatalf("Text() = %q, want declared-read parameter removals", text)
		}
	})

	t.Run("changing parameters is breaking", func(t *testing.T) {
		old := contractFixture()
		old.Queries[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}
		old.Views[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "string"}}}
		current := contractFixture()
		current.Queries[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "topic", Type: "uint64"}}}
		current.Views[0].Parameters = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "renamed_topic", Type: "string"}}}

		report := Compare(old, current)

		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
		if text := report.Text(); !strings.Contains(text, "query parameters changed") ||
			!strings.Contains(text, "view parameters changed") {
			t.Fatalf("Text() = %q, want declared-read parameter changes", text)
		}
	})

	t.Run("new reads primarily report the read as added", func(t *testing.T) {
		old := contractFixture()
		current := contractFixture()
		current.Queries = append(current.Queries, shunter.QueryDescription{
			Name: "messages_by_topic",
			Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
				{Name: "topic", Type: "string"},
			}},
		})
		current.Views = append(current.Views, shunter.ViewDescription{
			Name: "live_messages_by_topic",
			Parameters: &shunter.ProductSchema{Columns: []shunter.ProductColumn{
				{Name: "topic", Type: "string"},
			}},
		})

		report := Compare(old, current)

		assertChange(t, report.Changes, ChangeKindAdditive, SurfaceQuery, "messages_by_topic")
		assertChange(t, report.Changes, ChangeKindAdditive, SurfaceView, "live_messages_by_topic")
		assertNoChange(t, report.Changes, SurfaceQuery, "history")
		if text := report.Text(); strings.Contains(text, "messages_by_topic: query parameters added") ||
			strings.Contains(text, "live_messages_by_topic: view parameters added") {
			t.Fatalf("Text() = %q, want newly added reads reported as added reads", text)
		}
	})
}

func TestContractDiffIgnoresDeclaredReadPermissionOrder(t *testing.T) {
	old := contractFixture()
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

	report := Compare(old, current)

	assertNoChange(t, report.Changes, SurfacePermission, "reducer.send_message")
	assertNoChange(t, report.Changes, SurfacePermission, "query.history")
	assertNoChange(t, report.Changes, SurfacePermission, "view.live")
}

func TestContractDiffDetectsDelimiterCollisionStringSliceChanges(t *testing.T) {
	t.Run("table_read_policy_permissions", func(t *testing.T) {
		old := contractFixture()
		old.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
			Access:      schema.TableAccessPermissioned,
			Permissions: []string{"alpha", "beta"},
		}
		current := contractFixture()
		current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
			Access:      schema.TableAccessPermissioned,
			Permissions: []string{"alpha\x00beta"},
		}

		report, err := CompareJSON(mustContractJSON(t, old), mustContractJSON(t, current))
		if err != nil {
			t.Fatalf("CompareJSON returned error: %v", err)
		}

		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceTableReadPolicy, "messages")
	})

	t.Run("declared_read_permissions", func(t *testing.T) {
		old := contractFixture()
		old.Permissions.Queries = []shunter.PermissionContractDeclaration{{
			Name:     "history",
			Required: []string{"alpha", "beta"},
		}}
		current := contractFixture()
		current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
			Name:     "history",
			Required: []string{"alpha\x00beta"},
		}}

		report, err := CompareJSON(mustContractJSON(t, old), mustContractJSON(t, current))
		if err != nil {
			t.Fatalf("CompareJSON returned error: %v", err)
		}

		assertChange(t, report.Changes, ChangeKindBreaking, SurfacePermission, "query.history")
	})

	t.Run("read_model_tags", func(t *testing.T) {
		old := contractFixture()
		old.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "history",
			Tables:  []string{"messages"},
			Tags:    []string{"alpha", "beta"},
		}}
		current := contractFixture()
		current.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "history",
			Tables:  []string{"messages"},
			Tags:    []string{"alpha\x00beta"},
		}}

		report, err := CompareJSON(mustContractJSON(t, old), mustContractJSON(t, current))
		if err != nil {
			t.Fatalf("CompareJSON returned error: %v", err)
		}

		assertChange(t, report.Changes, ChangeKindMetadata, SurfaceReadModel, "query.history")
	})

	t.Run("index_columns", func(t *testing.T) {
		old := contractFixture()
		old.Schema.Tables[0].Columns = []schema.ColumnExport{
			{Name: "a,b", Type: "uint64"},
			{Name: "a", Type: "uint64"},
			{Name: "b", Type: "uint64"},
		}
		old.Schema.Tables[0].Indexes = []schema.IndexExport{{
			Name:    "collision_ix",
			Columns: []string{"a,b"},
			Unique:  true,
		}}
		current := contractFixture()
		current.Schema.Tables[0].Columns = old.Schema.Tables[0].Columns
		current.Schema.Tables[0].Indexes = []schema.IndexExport{{
			Name:    "collision_ix",
			Columns: []string{"a", "b"},
			Unique:  true,
		}}

		report, err := CompareJSON(mustContractJSON(t, old), mustContractJSON(t, current))
		if err != nil {
			t.Fatalf("CompareJSON returned error: %v", err)
		}

		assertChange(t, report.Changes, ChangeKindBreaking, SurfaceIndex, "messages.collision_ix")
	})
}

func TestContractDiffReportsModuleMetadataChanges(t *testing.T) {
	old := contractFixture()
	old.Module.Metadata = map[string]string{
		"removed": "old",
		"stable":  "same",
		"team":    "runtime",
	}
	current := contractFixture()
	current.Module.Metadata = map[string]string{
		"added":  "new",
		"stable": "same",
		"team":   "platform",
	}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceModule, "chat.metadata.added")
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceModule, "chat.metadata.removed")
	assertChange(t, report.Changes, ChangeKindMetadata, SurfaceModule, "chat.metadata.team")
}

func TestContractDiffDetectsDeclaredReadSQLChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Queries[0].SQL = "SELECT * FROM messages"
	current.Views[0].SQL = "SELECT * FROM messages"

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindAdditive, SurfaceView, "live")

	old = contractFixture()
	old.Queries[0].SQL = "SELECT * FROM messages"
	old.Views[0].SQL = "SELECT * FROM messages"
	current = contractFixture()
	current.Queries[0].SQL = "SELECT id FROM messages"
	current.Views[0].SQL = ""

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceQuery, "history")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceView, "live")
}

func TestContractDiffDetectsVisibilityFilterChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:               "own_messages",
		SQL:                "SELECT * FROM messages WHERE body = :sender",
		ReturnTable:        "messages",
		ReturnTableID:      0,
		UsesCallerIdentity: true,
	}}

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceVisibilityFilter, "own_messages")

	old = current
	current = contractFixture()
	current.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:          "own_messages",
		SQL:           "SELECT * FROM messages WHERE body = 'hello'",
		ReturnTable:   "messages",
		ReturnTableID: 0,
	}}

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceVisibilityFilter, "own_messages")

	old = current
	current = contractFixture()

	report = Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceVisibilityFilter, "own_messages")
}

func TestContractDiffDetectsStableCodegenMetadataChanges(t *testing.T) {
	old := contractFixture()
	current := contractFixture()
	current.Codegen.ContractFormat = "future.module_contract"
	current.Codegen.ContractVersion = shunter.ModuleContractVersion + 1
	current.Codegen.DefaultSnapshotFilename = "future.contract.json"

	report := Compare(old, current)
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceCodegen, "contract_format")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceCodegen, "contract_version")
	assertChange(t, report.Changes, ChangeKindBreaking, SurfaceCodegen, "default_snapshot_filename")
}

func TestContractDiffJSONIgnoresUnknownV1Fields(t *testing.T) {
	oldData := mustContractJSONWithUnknownFields(t, contractFixtureWithStableMetadata())
	currentData := mustContractJSONWithUnknownFields(t, contractFixtureWithStableMetadata())

	report, err := CompareJSON(oldData, currentData)
	if err != nil {
		t.Fatalf("CompareJSON returned error for unknown fields: %v", err)
	}
	if len(report.Changes) != 0 {
		t.Fatalf("changes = %#v, want none for unknown fields", report.Changes)
	}

	plan, err := PlanJSON(oldData, currentData, PlanOptions{ValidateContracts: true})
	if err != nil {
		t.Fatalf("PlanJSON returned error for unknown fields: %v", err)
	}
	if len(plan.Entries) != 0 || len(plan.Warnings) != 0 {
		t.Fatalf("plan = %#v, want no entries or warnings for unknown fields", plan)
	}
}

func TestContractDiffJSONRejectsKnownFieldTypeChanges(t *testing.T) {
	oldData := mustContractJSON(t, contractFixture())
	currentData := []byte(strings.Replace(string(mustContractJSON(t, contractFixture())), `"contract_version": 1`, `"contract_version": "1"`, 1))

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"contract_version",
	)

	current := contractFixture()
	current.Schema.Reducers[0].Args = &shunter.ProductSchema{Columns: []shunter.ProductColumn{{Name: "body", Type: "string"}}}
	var raw map[string]any
	if err := json.Unmarshal(mustContractJSON(t, current), &raw); err != nil {
		t.Fatalf("Unmarshal current contract: %v", err)
	}
	reducer := mustObjectAt(t, mustArray(t, mustObject(t, raw, "schema"), "reducers"), 0)
	reducer["args"] = "not an object"
	currentData, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal invalid reducer args contract: %v", err)
	}
	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"schema.reducers",
		"args",
	)
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

func TestContractDiffJSONFailsClearlyForSemanticInvalidContract(t *testing.T) {
	_, err := CompareJSON([]byte(`{}`), mustContractJSON(t, contractFixture()))
	if err == nil {
		t.Fatal("CompareJSON returned nil error, want invalid contract")
	}
	if !errors.Is(err, ErrInvalidContractJSON) {
		t.Fatalf("CompareJSON error = %v, want ErrInvalidContractJSON", err)
	}
	if !strings.Contains(err.Error(), "previous contract") {
		t.Fatalf("CompareJSON error = %v, want previous contract context", err)
	}
}

func TestContractDiffJSONRejectsSemanticInvalidCurrentContractWithContext(t *testing.T) {
	current := contractFixture()
	current.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:               "own_messages",
		SQL:                "SELECT * FROM messages WHERE body = :sender",
		ReturnTable:        "messages",
		ReturnTableID:      99,
		UsesCallerIdentity: true,
	}}
	oldData := mustContractJSON(t, contractFixture())
	currentData := mustRawContractJSON(t, current)

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"visibility_filters.own_messages return_table_id",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidCurrentMigrationMetadataWithContext(t *testing.T) {
	current := contractFixture()
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceQuery,
		Name:    "history",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibility("maybe"),
			Classifications: []shunter.MigrationClassification{
				shunter.MigrationClassification("rewrite"),
			},
		},
	}}
	oldData := mustContractJSON(t, contractFixture())
	currentData := mustRawContractJSON(t, current)

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		`migrations.query.history.compatibility = "maybe" is invalid`,
		`migrations.query.history.classifications contains invalid value "rewrite"`,
	)
}

func TestContractDiffJSONRejectsCurrentMigrationUnknownTargetWithContext(t *testing.T) {
	current := contractFixture()
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceTable,
		Name:    "missing_table",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
		},
	}}
	oldData := mustContractJSON(t, contractFixture())
	currentData := mustRawContractJSON(t, current)

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"migrations.table.missing_table references unknown table",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidMigrationSurfaceWithContext(t *testing.T) {
	invalidCurrent := contractFixture()
	invalidCurrent.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: "subscription",
		Name:    "recent_messages",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
		},
	}}
	invalidPrevious := contractFixture()
	invalidPrevious.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: "subscription",
		Name:    "recent_messages",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibilityCompatible,
		},
	}}

	for _, tc := range []struct {
		name        string
		previous    []byte
		current     []byte
		wantContext string
	}{
		{
			name:        "current",
			previous:    mustContractJSON(t, contractFixture()),
			current:     mustRawContractJSON(t, invalidCurrent),
			wantContext: "current contract",
		},
		{
			name:        "previous",
			previous:    mustRawContractJSON(t, invalidPrevious),
			current:     mustContractJSON(t, contractFixture()),
			wantContext: "previous contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONEntryPointsRejectInvalidContract(t, tc.previous, tc.current,
				tc.wantContext,
				`migrations surface "subscription" is invalid`,
			)
		})
	}
}

func TestContractDiffJSONRejectsSemanticInvalidCurrentPermissionTargetWithContext(t *testing.T) {
	current := contractFixture()
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "missing_query",
		Required: []string{"messages:read"},
	}}
	oldData := mustContractJSON(t, contractFixture())
	currentData := mustRawContractJSON(t, current)

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"permissions.query.missing_query references unknown query",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidPreviousPermissionTargetWithContext(t *testing.T) {
	previous := contractFixture()
	previous.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "missing_reducer",
		Required: []string{"messages:send"},
	}}
	previousData := mustRawContractJSON(t, previous)
	currentData := mustContractJSON(t, contractFixture())

	assertJSONEntryPointsRejectInvalidContract(t, previousData, currentData,
		"previous contract",
		"permissions.reducer.missing_reducer references unknown reducer",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidCurrentReadModelTargetWithContext(t *testing.T) {
	current := contractFixture()
	current.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceView,
		Name:    "missing_view",
		Tables:  []string{"messages"},
		Tags:    []string{"live"},
	}}
	oldData := mustContractJSON(t, contractFixture())
	currentData := mustRawContractJSON(t, current)

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"read_model.view.missing_view references unknown view",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidReadModelSurfaceWithContext(t *testing.T) {
	invalidCurrent := contractFixture()
	invalidCurrent.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: "subscription",
		Name:    "recent_messages",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}
	invalidPrevious := contractFixture()
	invalidPrevious.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: "subscription",
		Name:    "recent_messages",
		Tables:  []string{"messages"},
		Tags:    []string{"history"},
	}}

	for _, tc := range []struct {
		name        string
		previous    []byte
		current     []byte
		wantContext string
	}{
		{
			name:        "current",
			previous:    mustContractJSON(t, contractFixture()),
			current:     mustRawContractJSON(t, invalidCurrent),
			wantContext: "current contract",
		},
		{
			name:        "previous",
			previous:    mustRawContractJSON(t, invalidPrevious),
			current:     mustContractJSON(t, contractFixture()),
			wantContext: "previous contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONEntryPointsRejectInvalidContract(t, tc.previous, tc.current,
				tc.wantContext,
				`read_model surface "subscription" is invalid`,
			)
		})
	}
}

func TestContractDiffJSONRejectsSemanticInvalidCurrentTableReadPolicyWithContext(t *testing.T) {
	current := contractFixture()
	current.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPublic,
		Permissions: []string{"messages:read"},
	}
	oldData := mustContractJSON(t, contractFixture())
	currentData := mustRawContractJSON(t, current)

	assertJSONEntryPointsRejectInvalidContract(t, oldData, currentData,
		"current contract",
		"schema.tables.messages.read_policy invalid",
		"public read policy must not include permissions",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidPreviousTableReadPolicyWithContext(t *testing.T) {
	previous := contractFixture()
	previous.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPublic,
		Permissions: []string{"messages:read"},
	}
	previousData := mustRawContractJSON(t, previous)
	currentData := mustContractJSON(t, contractFixture())

	assertJSONEntryPointsRejectInvalidContract(t, previousData, currentData,
		"previous contract",
		"schema.tables.messages.read_policy invalid",
		"public read policy must not include permissions",
	)
}

func TestContractDiffJSONRejectsSemanticInvalidPreviousReadModelWithContext(t *testing.T) {
	previous := contractFixture()
	previous.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{{
		Surface: shunter.ReadModelSurfaceQuery,
		Name:    "history",
		Tables:  []string{"missing_table"},
		Tags:    []string{"history"},
	}}
	previousData := mustRawContractJSON(t, previous)
	currentData := mustContractJSON(t, contractFixture())

	assertJSONEntryPointsRejectInvalidContract(t, previousData, currentData,
		"previous contract",
		`read_model.query.history references unknown table "missing_table"`,
	)
}

func TestContractDiffJSONRejectsSemanticInvalidPreviousMigrationMetadataWithContext(t *testing.T) {
	previous := contractFixture()
	previous.Migrations.Declarations = []shunter.MigrationContractDeclaration{{
		Surface: shunter.MigrationSurfaceTable,
		Name:    "messages",
		Metadata: shunter.MigrationMetadata{
			Compatibility: shunter.MigrationCompatibility("maybe"),
		},
	}}
	previousData := mustRawContractJSON(t, previous)
	currentData := mustContractJSON(t, contractFixture())

	assertJSONEntryPointsRejectInvalidContract(t, previousData, currentData,
		"previous contract",
		`migrations.table.messages.compatibility = "maybe" is invalid`,
	)
}

func assertJSONEntryPointsRejectInvalidContract(t *testing.T, previous, current []byte, wantSubstrings ...string) {
	t.Helper()
	for _, tt := range []struct {
		name string
		run  func() error
	}{
		{
			name: "compare",
			run: func() error {
				_, err := CompareJSON(previous, current)
				return err
			},
		},
		{
			name: "plan",
			run: func() error {
				_, err := PlanJSON(previous, current, PlanOptions{ValidateContracts: true})
				return err
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("JSON entry point returned nil error, want invalid contract")
			}
			if !errors.Is(err, ErrInvalidContractJSON) {
				t.Fatalf("JSON entry point error = %v, want ErrInvalidContractJSON", err)
			}
			for _, want := range wantSubstrings {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("JSON entry point error = %v, want substring %q", err, want)
				}
			}
		})
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

func contractFixtureWithStableMetadata() shunter.ModuleContract {
	contract := contractFixture()
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:               "own_messages",
		SQL:                "SELECT * FROM messages WHERE body = :sender",
		ReturnTable:        "messages",
		ReturnTableID:      0,
		UsesCallerIdentity: true,
	}}
	contract.Permissions.Reducers = []shunter.PermissionContractDeclaration{{
		Name:     "send_message",
		Required: []string{"messages:send"},
	}}
	contract.Permissions.Queries = []shunter.PermissionContractDeclaration{{
		Name:     "history",
		Required: []string{"messages:read"},
	}}
	contract.Permissions.Views = []shunter.PermissionContractDeclaration{{
		Name:     "live",
		Required: []string{"messages:subscribe"},
	}}
	contract.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{
		{Surface: shunter.ReadModelSurfaceQuery, Name: "history", Tables: []string{"messages"}, Tags: []string{"history"}},
		{Surface: shunter.ReadModelSurfaceView, Name: "live", Tables: []string{"messages"}, Tags: []string{"live"}},
	}
	contract.Migrations.Module = shunter.MigrationMetadata{
		ModuleVersion:   "v1.0.0",
		SchemaVersion:   1,
		ContractVersion: shunter.ModuleContractVersion,
		PreviousVersion: "v1.0.0",
		Compatibility:   shunter.MigrationCompatibilityCompatible,
		Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
		Notes:           "stable metadata fixture",
	}
	contract.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
				Notes:           "table metadata",
			},
		},
		{
			Surface: shunter.MigrationSurfaceQuery,
			Name:    "history",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationManualReviewNeeded},
				Notes:           "query metadata",
			},
		},
		{
			Surface: shunter.MigrationSurfaceView,
			Name:    "live",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationManualReviewNeeded},
				Notes:           "view metadata",
			},
		},
	}
	return contract
}

func mustContractJSON(t *testing.T, contract shunter.ModuleContract) []byte {
	t.Helper()
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	return data
}

func mustContractJSONWithUnknownFields(t *testing.T, contract shunter.ModuleContract) []byte {
	t.Helper()
	data := mustContractJSON(t, contract)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal contract fixture: %v", err)
	}
	raw["future_top_level"] = map[string]any{"ignored": true}
	mustObject(t, raw, "module")["future_module_field"] = "ignored"
	schemaRaw := mustObject(t, raw, "schema")
	schemaRaw["future_schema_field"] = []any{"ignored"}
	table := mustObjectAt(t, mustArray(t, schemaRaw, "tables"), 0)
	table["future_table_field"] = "ignored"
	mustObjectAt(t, mustArray(t, table, "columns"), 0)["future_column_field"] = "ignored"
	mustObjectAt(t, mustArray(t, table, "indexes"), 0)["future_index_field"] = "ignored"
	mustObject(t, table, "read_policy")["future_read_policy_field"] = "ignored"
	mustObjectAt(t, mustArray(t, schemaRaw, "reducers"), 0)["future_reducer_field"] = "ignored"
	mustObjectAt(t, mustArray(t, raw, "queries"), 0)["future_query_field"] = "ignored"
	mustObjectAt(t, mustArray(t, raw, "views"), 0)["future_view_field"] = "ignored"
	mustObjectAt(t, mustArray(t, raw, "visibility_filters"), 0)["future_visibility_filter_field"] = "ignored"
	permissions := mustObject(t, raw, "permissions")
	permissions["future_permissions_field"] = "ignored"
	mustObjectAt(t, mustArray(t, permissions, "reducers"), 0)["future_permission_declaration_field"] = "ignored"
	mustObjectAt(t, mustArray(t, permissions, "queries"), 0)["future_permission_declaration_field"] = "ignored"
	mustObjectAt(t, mustArray(t, permissions, "views"), 0)["future_permission_declaration_field"] = "ignored"
	readModel := mustObject(t, raw, "read_model")
	readModel["future_read_model_field"] = "ignored"
	mustObjectAt(t, mustArray(t, readModel, "declarations"), 0)["future_read_model_declaration_field"] = "ignored"
	migrations := mustObject(t, raw, "migrations")
	migrations["future_migrations_field"] = "ignored"
	mustObject(t, migrations, "module")["future_module_migration_field"] = "ignored"
	migrationDeclaration := mustObjectAt(t, mustArray(t, migrations, "declarations"), 0)
	migrationDeclaration["future_migration_declaration_field"] = "ignored"
	mustObject(t, migrationDeclaration, "metadata")["future_migration_metadata_field"] = "ignored"
	mustObject(t, raw, "codegen")["future_codegen_field"] = "ignored"
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal contract fixture with unknown fields: %v", err)
	}
	return out
}

func mustObject(t *testing.T, raw map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := raw[key].(map[string]any)
	if !ok {
		t.Fatalf("contract fixture field %q = %#v, want object", key, raw[key])
	}
	return value
}

func mustArray(t *testing.T, raw map[string]any, key string) []any {
	t.Helper()
	value, ok := raw[key].([]any)
	if !ok {
		t.Fatalf("contract fixture field %q = %#v, want array", key, raw[key])
	}
	return value
}

func mustObjectAt(t *testing.T, values []any, index int) map[string]any {
	t.Helper()
	if index >= len(values) {
		t.Fatalf("contract fixture array length = %d, want index %d", len(values), index)
	}
	value, ok := values[index].(map[string]any)
	if !ok {
		t.Fatalf("contract fixture array[%d] = %#v, want object", index, values[index])
	}
	return value
}

func mustRawContractJSON(t *testing.T, contract shunter.ModuleContract) []byte {
	t.Helper()
	data, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
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

func assertNoChange(t *testing.T, changes []Change, surface Surface, name string) {
	t.Helper()
	for _, change := range changes {
		if change.Surface == surface && change.Name == name {
			t.Fatalf("changes = %#v, want no %s %s change", changes, surface, name)
		}
	}
}
