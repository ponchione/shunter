package shunter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestBuildCreatesDeclaredReadCatalogEntries(t *testing.T) {
	mod := validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages WHERE body = :sender",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
			},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages WHERE body = :sender",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityUnknown,
				Classifications: []MigrationClassification{MigrationClassificationManualReviewNeeded},
			},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	query, ok := rt.readCatalog.lookup("recent_messages")
	if !ok {
		t.Fatal("query catalog entry missing")
	}
	if query.Name != "recent_messages" || query.Kind != declaredReadKindQuery || query.SQL != "SELECT * FROM messages WHERE body = :sender" {
		t.Fatalf("query catalog entry = %+v", query)
	}
	if query.compiled == nil {
		t.Fatal("query compiled metadata = nil, want executable metadata")
	}
	assertStringSlice(t, query.Permissions.Required, []string{"messages:read"}, "query permissions")
	assertStringSlice(t, query.ReadModel.Tables, []string{"messages"}, "query read model tables")
	assertStringSlice(t, query.ReadModel.Tags, []string{"history"}, "query read model tags")
	assertMigrationMetadata(t, query.Migration, MigrationCompatibilityCompatible, MigrationClassificationAdditive)
	assertTableIDs(t, query.ReferencedTables, []schema.TableID{0}, "query referenced tables")
	if !query.UsesCallerIdentity {
		t.Fatal("query UsesCallerIdentity = false, want true")
	}

	view, ok := rt.readCatalog.lookup("live_messages")
	if !ok {
		t.Fatal("view catalog entry missing")
	}
	if view.Name != "live_messages" || view.Kind != declaredReadKindView || view.SQL != "SELECT * FROM messages WHERE body = :sender" {
		t.Fatalf("view catalog entry = %+v", view)
	}
	if view.compiled == nil {
		t.Fatal("view compiled metadata = nil, want executable metadata")
	}
	assertStringSlice(t, view.Permissions.Required, []string{"messages:subscribe"}, "view permissions")
	assertStringSlice(t, view.ReadModel.Tables, []string{"messages"}, "view read model tables")
	assertStringSlice(t, view.ReadModel.Tags, []string{"realtime"}, "view read model tags")
	assertMigrationMetadata(t, view.Migration, MigrationCompatibilityUnknown, MigrationClassificationManualReviewNeeded)
	assertTableIDs(t, view.ReferencedTables, []schema.TableID{0}, "view referenced tables")
	if !view.UsesCallerIdentity {
		t.Fatal("view UsesCallerIdentity = false, want true")
	}
}

func TestDeclaredReadCatalogEntriesAreDetachedFromDescriptionsAndContracts(t *testing.T) {
	rt, err := Build(validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
			},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityUnknown,
				Classifications: []MigrationClassification{MigrationClassificationManualReviewNeeded},
			},
		}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	desc := rt.Describe()
	desc.Module.Queries[0].Permissions.Required[0] = "mutated"
	desc.Module.Queries[0].ReadModel.Tables[0] = "mutated"
	desc.Module.Queries[0].Migration.Classifications[0] = MigrationClassificationDeprecated
	desc.Module.Views[0].Permissions.Required[0] = "mutated"
	desc.Module.Views[0].ReadModel.Tags[0] = "mutated"
	desc.Module.Views[0].Migration.Classifications[0] = MigrationClassificationDeprecated

	contract := rt.ExportContract()
	contract.Queries[0].Permissions.Required[0] = "contract-mutated"
	contract.Views[0].ReadModel.Tables[0] = "contract-mutated"

	query, ok := rt.readCatalog.lookup("recent_messages")
	if !ok {
		t.Fatal("query catalog entry missing")
	}
	query.Permissions.Required[0] = "entry-mutated"
	query.ReadModel.Tables[0] = "entry-mutated"
	query.Migration.Classifications[0] = MigrationClassificationDeprecated

	again, ok := rt.readCatalog.lookup("recent_messages")
	if !ok {
		t.Fatal("query catalog entry missing on second lookup")
	}
	assertStringSlice(t, again.Permissions.Required, []string{"messages:read"}, "query permissions after mutation")
	assertStringSlice(t, again.ReadModel.Tables, []string{"messages"}, "query read model tables after mutation")
	assertMigrationMetadata(t, again.Migration, MigrationCompatibilityCompatible, MigrationClassificationAdditive)

	view, ok := rt.readCatalog.lookup("live_messages")
	if !ok {
		t.Fatal("view catalog entry missing")
	}
	assertStringSlice(t, view.Permissions.Required, []string{"messages:subscribe"}, "view permissions after mutation")
	assertStringSlice(t, view.ReadModel.Tags, []string{"realtime"}, "view read model tags after mutation")
	assertMigrationMetadata(t, view.Migration, MigrationCompatibilityUnknown, MigrationClassificationManualReviewNeeded)
}

func TestCallQueryOverPrivateBaseTableUsesDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	result, err := rt.CallQuery(context.Background(), "recent_messages", WithDeclaredReadPermissions("messages:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "recent_messages" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want recent_messages/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 1 || result.Rows[0][1].AsString() != "hello" {
		t.Fatalf("rows = %#v, want inserted private-table row", result.Rows)
	}
}

func TestDeclaredQueryOrderByDescOffsetLimit(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages ORDER BY body DESC LIMIT 1 OFFSET 1",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "bravo")
	insertMessage(t, rt, "alpha")
	insertMessage(t, rt, "charlie")

	result, err := rt.CallQuery(context.Background(), "recent_messages", WithDeclaredReadPermissions("messages:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "recent_messages" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want recent_messages/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %#v, want one offset ordered row", result.Rows)
	}
	if result.Rows[0][1].AsString() != "bravo" {
		t.Fatalf("rows = %#v, want body order offset row bravo", result.Rows)
	}
}

func TestDeclaredQueryOrderByProjectionAlias(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "ranked_messages",
			SQL:         "SELECT body AS text FROM messages ORDER BY text DESC LIMIT 2",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "bravo")
	insertMessage(t, rt, "alpha")
	insertMessage(t, rt, "charlie")

	result, err := rt.CallQuery(context.Background(), "ranked_messages", WithDeclaredReadPermissions("messages:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "ranked_messages" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want ranked_messages/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %#v, want two ordered projected rows", result.Rows)
	}
	if result.Rows[0][0].AsString() != "charlie" || result.Rows[1][0].AsString() != "bravo" {
		t.Fatalf("rows = %#v, want text alias order charlie, bravo", result.Rows)
	}
}

func TestDeclaredQueryMultiColumnOrderByProjectionAlias(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Query(QueryDeclaration{
			Name:        "ranked_messages",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2 OFFSET 1",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	result, err := rt.CallQuery(context.Background(), "ranked_messages", WithDeclaredReadPermissions("messages:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "ranked_messages" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want ranked_messages/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %#v, want two ordered projected rows", result.Rows)
	}
	if result.Rows[0][0].AsUint64() != 2 || result.Rows[0][1].AsString() != "charlie" ||
		result.Rows[1][0].AsUint64() != 3 || result.Rows[1][1].AsString() != "bravo" {
		t.Fatalf("rows = %#v, want offset rows (2, charlie), (3, bravo)", result.Rows)
	}
}

func TestDeclaredQueryJoinWhereColumnComparisonExecutes(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, NewModule("join_reads").
		SchemaVersion(1).
		TableDef(joinReadTableDef("t")).
		TableDef(joinReadTableDef("s")).
		Reducer("seed_join_rows", seedJoinRowsReducer).
		Query(QueryDeclaration{
			Name:        "matching_t_rows",
			SQL:         "SELECT t.id FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id ORDER BY t.id",
			Permissions: PermissionMetadata{Required: []string{"joins:read"}},
		}))
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "seed_join_rows", nil)
	if err != nil {
		t.Fatalf("seed reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("seed reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}

	result, err := rt.CallQuery(context.Background(), "matching_t_rows", WithDeclaredReadPermissions("joins:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "matching_t_rows" || result.TableName != "t" {
		t.Fatalf("result identity = (%q, %q), want matching_t_rows/t", result.Name, result.TableName)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %#v, want two matching projected rows", result.Rows)
	}
	if result.Rows[0][0].AsUint64() != 1 || result.Rows[1][0].AsUint64() != 3 {
		t.Fatalf("rows = %#v, want projected ids 1, 3", result.Rows)
	}
}

func TestDeclaredViewJoinWhereColumnComparisonSubscribes(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, NewModule("live_join_reads").
		SchemaVersion(1).
		TableDef(joinReadIndexedTableDef("t")).
		TableDef(joinReadIndexedTableDef("s")).
		Reducer("seed_join_rows", seedJoinRowsReducer).
		View(ViewDeclaration{
			Name:        "live_matching_t_rows",
			SQL:         "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id",
			Permissions: PermissionMetadata{Required: []string{"joins:subscribe"}},
		}))
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "seed_join_rows", nil)
	if err != nil {
		t.Fatalf("seed reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("seed reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}

	sub, err := rt.SubscribeView(context.Background(), "live_matching_t_rows", 14, WithDeclaredReadPermissions("joins:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if sub.Name != "live_matching_t_rows" || sub.TableName != "t" {
		t.Fatalf("subscription identity = (%q, %q), want live_matching_t_rows/t", sub.Name, sub.TableName)
	}
	if len(sub.InitialRows) != 2 {
		t.Fatalf("initial rows = %#v, want two matching rows", sub.InitialRows)
	}
	if !rowsHaveUint64IDs(sub.InitialRows, 1, 3) {
		t.Fatalf("initial rows = %#v, want t ids 1 and 3", sub.InitialRows)
	}
}

func TestDeclaredQueryMultiWayJoinAggregateExecutes(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, NewModule("multi_join_reads").
		SchemaVersion(1).
		TableDef(joinReadTableDef("t")).
		TableDef(joinReadTableDef("s")).
		Reducer("seed_join_rows", seedJoinRowsReducer).
		Query(QueryDeclaration{
			Name:        "matching_tuple_count",
			SQL:         "SELECT COUNT(*) AS n FROM t JOIN s ON t.u32 = s.u32 JOIN s AS r ON s.u32 = r.u32 WHERE r.id <> 99",
			Permissions: PermissionMetadata{Required: []string{"joins:read"}},
		}))
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "seed_join_rows", nil)
	if err != nil {
		t.Fatalf("seed reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("seed reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}

	result, err := rt.CallQuery(context.Background(), "matching_tuple_count", WithDeclaredReadPermissions("joins:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "matching_tuple_count" || result.TableName != "t" {
		t.Fatalf("result identity = (%q, %q), want matching_tuple_count/t", result.Name, result.TableName)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].AsUint64() != 5 {
		t.Fatalf("rows = %#v, want count 5", result.Rows)
	}
}

func TestDeclaredViewMultiWayJoinSubscribes(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, NewModule("multi_join_view_reads").
		SchemaVersion(1).
		TableDef(joinReadIndexedTableDef("t")).
		TableDef(joinReadIndexedTableDef("s")).
		Reducer("seed_join_rows", seedJoinRowsReducer).
		View(ViewDeclaration{
			Name:        "live_matching_tuple_rows",
			SQL:         "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 JOIN s AS r ON s.u32 = r.u32 WHERE r.id <> 99",
			Permissions: PermissionMetadata{Required: []string{"joins:subscribe"}},
		}))
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "seed_join_rows", nil)
	if err != nil {
		t.Fatalf("seed reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("seed reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}

	sub, err := rt.SubscribeView(context.Background(), "live_matching_tuple_rows", 16, WithDeclaredReadPermissions("joins:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if sub.Name != "live_matching_tuple_rows" || sub.TableName != "t" {
		t.Fatalf("subscription identity = (%q, %q), want live_matching_tuple_rows/t", sub.Name, sub.TableName)
	}
	if !rowsHaveUint64IDs(sub.InitialRows, 1, 2, 2, 3, 3) {
		t.Fatalf("initial rows = %#v, want projected t ids 1,2,2,3,3", sub.InitialRows)
	}
}

func TestDeclaredViewMultiWayJoinAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x31)
	bob := visibilityRuntimeIdentity(0x32)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_visible_message_chain",
			SQL:         "SELECT a.* FROM messages AS a JOIN messages AS b ON a.id = b.id JOIN messages AS c ON b.id = c.id",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_visible_message_chain", 17,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if len(sub.InitialRows) != 1 || sub.InitialRows[0][0].AsUint64() != 1 || sub.InitialRows[0][1].AsString() != alice.Hex() {
		t.Fatalf("visible multi-way view rows = %#v, want only caller row", sub.InitialRows)
	}
}

func TestDeclaredViewAllowsOrderBy(t *testing.T) {
	if _, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages ORDER BY body",
		}), Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build rejected ordered declared view: %v", err)
	}
}

func TestDeclaredViewAllowsOrderByProjectionAlias(t *testing.T) {
	if _, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT body AS text FROM messages ORDER BY text",
		}), Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build rejected projected ordered declared view: %v", err)
	}
}

func TestDeclaredViewAllowsMultiColumnOrderBy(t *testing.T) {
	if _, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages ORDER BY body DESC, id ASC",
		}), Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build rejected multi-column ordered declared view: %v", err)
	}
}

func TestDeclaredViewAllowsLimit(t *testing.T) {
	if _, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages LIMIT 2",
		}), Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build rejected limited declared view: %v", err)
	}
}

func TestDeclaredViewAllowsOrderByLimitProjectionAlias(t *testing.T) {
	if _, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2",
		}), Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build rejected projected ordered limited declared view: %v", err)
	}
}

func TestDeclaredViewRejectsJoinOrderBy(t *testing.T) {
	_, err := Build(NewModule("live_join_ordered").
		SchemaVersion(1).
		TableDef(joinReadIndexedTableDef("t")).
		TableDef(joinReadIndexedTableDef("s")).
		View(ViewDeclaration{
			Name: "live_matching_t_rows",
			SQL:  "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 ORDER BY t.id",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want join ORDER BY rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live ORDER BY views require a single table") {
		t.Fatalf("Build error = %v, want live single-table ORDER BY rejection", err)
	}
}

func TestDeclaredViewRejectsJoinLimit(t *testing.T) {
	_, err := Build(NewModule("live_join_limited").
		SchemaVersion(1).
		TableDef(joinReadIndexedTableDef("t")).
		TableDef(joinReadIndexedTableDef("s")).
		View(ViewDeclaration{
			Name: "live_matching_t_rows",
			SQL:  "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 LIMIT 1",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want join LIMIT rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live LIMIT views require a single table") {
		t.Fatalf("Build error = %v, want live single-table LIMIT rejection", err)
	}
}

func TestDeclaredViewRejectsOffset(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages OFFSET 1",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want OFFSET rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "Unsupported: SELECT * FROM messages OFFSET 1") {
		t.Fatalf("Build error = %v, want OFFSET unsupported text", err)
	}
}

func TestDeclaredViewRejectsSumColumnAggregate(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT SUM(id) AS total FROM messages",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want aggregate rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live aggregate views support COUNT only") {
		t.Fatalf("Build error = %v, want live COUNT-only aggregate rejection", err)
	}
}

func TestDeclaredViewRejectsCountDistinctAggregate(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT COUNT(DISTINCT body) AS n FROM messages",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want COUNT DISTINCT rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live aggregate views do not support COUNT(DISTINCT ...)") {
		t.Fatalf("Build error = %v, want live COUNT DISTINCT rejection", err)
	}
}

func TestDeclaredViewRejectsJoinAggregate(t *testing.T) {
	_, err := Build(NewModule("live_join_count").
		SchemaVersion(1).
		TableDef(joinReadIndexedTableDef("t")).
		TableDef(joinReadIndexedTableDef("s")).
		View(ViewDeclaration{
			Name: "live_join_count",
			SQL:  "SELECT COUNT(*) AS n FROM t JOIN s ON t.u32 = s.u32",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want join aggregate rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live aggregate views require a single table") {
		t.Fatalf("Build error = %v, want live single-table aggregate rejection", err)
	}
}

func TestDeclaredViewRejectsAggregateLimit(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_message_count",
			SQL:  "SELECT COUNT(*) AS n FROM messages LIMIT 1",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want aggregate LIMIT rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live LIMIT views do not support aggregate views") {
		t.Fatalf("Build error = %v, want aggregate LIMIT rejection", err)
	}
}

func TestDeclaredViewRejectsAggregateOrderBy(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_message_count",
			SQL:  "SELECT COUNT(*) AS n FROM messages ORDER BY n",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want aggregate ORDER BY rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "live ORDER BY views do not support aggregate views") {
		t.Fatalf("Build error = %v, want aggregate ORDER BY rejection", err)
	}
}

func TestSubscribeViewOverPrivateBaseTableUsesDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	sub, err := rt.SubscribeView(context.Background(), "live_messages", 7, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if sub.Name != "live_messages" || sub.QueryID != 7 || sub.TableName != "messages" {
		t.Fatalf("subscription identity = (%q, %d, %q), want live_messages/7/messages", sub.Name, sub.QueryID, sub.TableName)
	}
	if len(sub.InitialRows) != 1 || sub.InitialRows[0][1].AsString() != "hello" {
		t.Fatalf("initial rows = %#v, want inserted private-table row", sub.InitialRows)
	}
}

func TestSubscribeViewOrderByReturnsOrderedInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_ordered_messages",
			SQL:         "SELECT * FROM messages ORDER BY body DESC, id ASC",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	sub, err := rt.SubscribeView(context.Background(), "live_ordered_messages", 22, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView ordered table-shaped view: %v", err)
	}
	if got, want := rowUint64IDs(sub.InitialRows), []uint64{1, 2, 3, 4}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ordered initial ids = %v, want %v; rows=%#v", got, want, sub.InitialRows)
	}
}

func TestSubscribeViewOrderByReturnsOrderedProjectedInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_ordered_message_ranks",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	sub, err := rt.SubscribeView(context.Background(), "live_ordered_message_ranks", 23, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView ordered projected view: %v", err)
	}
	if got, want := rowUint64IDs(sub.InitialRows), []uint64{1, 2, 3, 4}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ordered projected initial ids = %v, want %v; rows=%#v", got, want, sub.InitialRows)
	}
	if len(sub.Columns) != 2 || sub.Columns[0].Name != "id" || sub.Columns[1].Name != "text" {
		t.Fatalf("projected columns = %#v, want id/text", sub.Columns)
	}
}

func TestSubscribeViewLimitReturnsLimitedInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_limited_messages",
			SQL:         "SELECT * FROM messages ORDER BY id ASC LIMIT 2",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "alpha")

	sub, err := rt.SubscribeView(context.Background(), "live_limited_messages", 25, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView limited table-shaped view: %v", err)
	}
	if got, want := rowUint64IDs(sub.InitialRows), []uint64{1, 2}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("limited initial ids = %v, want %v; rows=%#v", got, want, sub.InitialRows)
	}
}

func TestSubscribeViewLimitReturnsLimitedProjectedInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_limited_message_ranks",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	sub, err := rt.SubscribeView(context.Background(), "live_limited_message_ranks", 26, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView limited projected view: %v", err)
	}
	if got, want := rowUint64IDs(sub.InitialRows), []uint64{1, 2}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("limited projected initial ids = %v, want %v; rows=%#v", got, want, sub.InitialRows)
	}
	if len(sub.Columns) != 2 || sub.Columns[0].Name != "id" || sub.Columns[1].Name != "text" {
		t.Fatalf("projected columns = %#v, want id/text", sub.Columns)
	}
}

func TestSubscribeViewColumnProjectionReturnsProjectedInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_message_bodies",
			SQL:         "SELECT body AS text FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	sub, err := rt.SubscribeView(context.Background(), "live_message_bodies", 8, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if sub.Name != "live_message_bodies" || sub.QueryID != 8 || sub.TableName != "messages" {
		t.Fatalf("subscription identity = (%q, %d, %q), want live_message_bodies/8/messages", sub.Name, sub.QueryID, sub.TableName)
	}
	if len(sub.Columns) != 1 || sub.Columns[0].Name != "text" || sub.Columns[0].Type != types.KindString {
		t.Fatalf("columns = %#v, want projected text string column", sub.Columns)
	}
	if len(sub.InitialRows) != 1 || len(sub.InitialRows[0]) != 1 || sub.InitialRows[0][0].AsString() != "hello" {
		t.Fatalf("initial rows = %#v, want one projected body row", sub.InitialRows)
	}
}

func TestSubscribeViewAggregateCountInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, NewModule("live_score_counts").
		SchemaVersion(1).
		TableDef(nullableScoresTableDef()).
		Reducer("seed_scores", seedNullableScoresReducer).
		View(ViewDeclaration{
			Name:        "live_score_rows_count",
			SQL:         "SELECT COUNT(*) AS n FROM scores",
			Permissions: PermissionMetadata{Required: []string{"scores:subscribe"}},
		}).
		View(ViewDeclaration{
			Name:        "live_non_null_score_count",
			SQL:         "SELECT COUNT(score) AS n FROM scores",
			Permissions: PermissionMetadata{Required: []string{"scores:subscribe"}},
		}))
	defer rt.Close()
	res, err := rt.CallReducer(context.Background(), "seed_scores", nil)
	if err != nil {
		t.Fatalf("seed reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("seed reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}

	allRows, err := rt.SubscribeView(context.Background(), "live_score_rows_count", 18, WithDeclaredReadPermissions("scores:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView COUNT(*): %v", err)
	}
	if allRows.TableName != "scores" || len(allRows.Columns) != 1 || allRows.Columns[0].Name != "n" || allRows.Columns[0].Type != types.KindUint64 {
		t.Fatalf("COUNT(*) subscription shape = table %q columns %#v, want scores/n Uint64", allRows.TableName, allRows.Columns)
	}
	if len(allRows.InitialRows) != 1 || allRows.InitialRows[0][0].AsUint64() != 5 {
		t.Fatalf("COUNT(*) initial rows = %#v, want count 5", allRows.InitialRows)
	}

	nonNullRows, err := rt.SubscribeView(context.Background(), "live_non_null_score_count", 19, WithDeclaredReadPermissions("scores:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView COUNT(score): %v", err)
	}
	if nonNullRows.TableName != "scores" || len(nonNullRows.Columns) != 1 || nonNullRows.Columns[0].Name != "n" || nonNullRows.Columns[0].Type != types.KindUint64 {
		t.Fatalf("COUNT(score) subscription shape = table %q columns %#v, want scores/n Uint64", nonNullRows.TableName, nonNullRows.Columns)
	}
	if len(nonNullRows.InitialRows) != 1 || nonNullRows.InitialRows[0][0].AsUint64() != 3 {
		t.Fatalf("COUNT(score) initial rows = %#v, want non-null count 3", nonNullRows.InitialRows)
	}
}

func TestDeclaredQueryAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x21)
	bob := visibilityRuntimeIdentity(0x22)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	result, err := rt.CallQuery(context.Background(), "recent_messages",
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:read"),
	)
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][1].AsString() != alice.Hex() {
		t.Fatalf("visible query rows = %#v, want only caller row", result.Rows)
	}
}

func TestDeclaredQueryCountColumnAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x25)
	bob := visibilityRuntimeIdentity(0x26)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		Query(QueryDeclaration{
			Name:        "visible_count",
			SQL:         "SELECT COUNT(body) AS n FROM messages LIMIT 1",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())
	insertMessageWithBody(t, rt, 3, alice.Hex())

	result, err := rt.CallQuery(context.Background(), "visible_count",
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:read"),
	)
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "visible_count" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want visible_count/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].AsUint64() != 2 {
		t.Fatalf("visible count rows = %#v, want one count row with 2", result.Rows)
	}
}

func TestDeclaredQueryCountDistinctColumnAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x27)
	bob := visibilityRuntimeIdentity(0x28)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		Query(QueryDeclaration{
			Name:        "visible_distinct_bodies",
			SQL:         "SELECT COUNT(DISTINCT body) AS n FROM messages LIMIT 1",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())
	insertMessageWithBody(t, rt, 3, alice.Hex())

	result, err := rt.CallQuery(context.Background(), "visible_distinct_bodies",
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:read"),
	)
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "visible_distinct_bodies" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want visible_distinct_bodies/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].AsUint64() != 1 {
		t.Fatalf("visible count distinct rows = %#v, want one count row with 1", result.Rows)
	}
}

func TestDeclaredQuerySumColumnExecutesThroughRuntimePath(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Query(QueryDeclaration{
			Name:        "message_id_total",
			SQL:         "SELECT SUM(id) AS total FROM messages ORDER BY total DESC LIMIT 1",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "alpha")
	insertMessageWithBody(t, rt, 2, "bravo")
	insertMessageWithBody(t, rt, 3, "charlie")

	result, err := rt.CallQuery(context.Background(), "message_id_total", WithDeclaredReadPermissions("messages:read"))
	if err != nil {
		t.Fatalf("CallQuery: %v", err)
	}
	if result.Name != "message_id_total" || result.TableName != "messages" {
		t.Fatalf("result identity = (%q, %q), want message_id_total/messages", result.Name, result.TableName)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].AsUint64() != 6 {
		t.Fatalf("sum rows = %#v, want one sum row with 6", result.Rows)
	}
}

func TestDeclaredQueryNullableAggregateSemantics(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, NewModule("nullable_score_reads").
		SchemaVersion(1).
		TableDef(nullableScoresTableDef()).
		Reducer("seed_scores", seedNullableScoresReducer).
		Query(QueryDeclaration{
			Name:        "non_null_score_count",
			SQL:         "SELECT COUNT(score) AS n FROM scores",
			Permissions: PermissionMetadata{Required: []string{"scores:read"}},
		}).
		Query(QueryDeclaration{
			Name:        "distinct_score_count",
			SQL:         "SELECT COUNT(DISTINCT score) AS n FROM scores",
			Permissions: PermissionMetadata{Required: []string{"scores:read"}},
		}).
		Query(QueryDeclaration{
			Name:        "score_total",
			SQL:         "SELECT SUM(score) AS total FROM scores",
			Permissions: PermissionMetadata{Required: []string{"scores:read"}},
		}).
		Query(QueryDeclaration{
			Name:        "all_null_score_total",
			SQL:         "SELECT SUM(score) AS total FROM scores WHERE score IS NULL",
			Permissions: PermissionMetadata{Required: []string{"scores:read"}},
		}))
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "seed_scores", nil)
	if err != nil {
		t.Fatalf("seed reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("seed reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}

	cases := []struct {
		name string
		want types.Value
	}{
		{name: "non_null_score_count", want: types.NewUint64(3)},
		{name: "distinct_score_count", want: types.NewUint64(2)},
		{name: "score_total", want: types.NewUint64(23)},
		{name: "all_null_score_total", want: types.NewNull(types.KindUint64)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := rt.CallQuery(context.Background(), tt.name, WithDeclaredReadPermissions("scores:read"))
			if err != nil {
				t.Fatalf("CallQuery: %v", err)
			}
			if result.Name != tt.name || result.TableName != "scores" {
				t.Fatalf("result identity = (%q, %q), want %s/scores", result.Name, result.TableName, tt.name)
			}
			if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || !result.Rows[0][0].Equal(tt.want) {
				t.Fatalf("rows = %#v, want one row with %v", result.Rows, tt.want)
			}
		})
	}
}

func TestDeclaredViewAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x23)
	bob := visibilityRuntimeIdentity(0x24)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages ORDER BY body",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_messages", 8,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if len(sub.InitialRows) != 1 || sub.InitialRows[0][1].AsString() != alice.Hex() {
		t.Fatalf("visible view rows = %#v, want only caller row", sub.InitialRows)
	}
}

func TestDeclaredViewOrderByAppliesAfterVisibility(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x35)
	bob := visibilityRuntimeIdentity(0x36)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_visible_ordered_messages",
			SQL:         "SELECT * FROM messages ORDER BY id DESC",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 3, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_visible_ordered_messages", 24,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView ordered visibility: %v", err)
	}
	if got, want := rowUint64IDs(sub.InitialRows), []uint64{3, 1}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("visible ordered initial ids = %v, want %v; rows=%#v", got, want, sub.InitialRows)
	}
}

func TestDeclaredViewLimitAppliesAfterVisibility(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x37)
	bob := visibilityRuntimeIdentity(0x38)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_visible_limited_messages",
			SQL:         "SELECT * FROM messages ORDER BY id DESC LIMIT 1",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 3, alice.Hex())
	insertMessageWithBody(t, rt, 4, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_visible_limited_messages", 27,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView limited visibility: %v", err)
	}
	if got, want := rowUint64IDs(sub.InitialRows), []uint64{4}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("visible limited initial ids = %v, want %v; rows=%#v", got, want, sub.InitialRows)
	}
}

func TestDeclaredViewColumnProjectionAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x31)
	bob := visibilityRuntimeIdentity(0x32)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_message_bodies",
			SQL:         "SELECT body AS text FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_message_bodies", 9,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if len(sub.InitialRows) != 1 || len(sub.InitialRows[0]) != 1 || sub.InitialRows[0][0].AsString() != alice.Hex() {
		t.Fatalf("visible projected view rows = %#v, want only caller body", sub.InitialRows)
	}
}

func TestDeclaredViewAggregateCountAppliesVisibilityAfterPermissionSucceeds(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x33)
	bob := visibilityRuntimeIdentity(0x34)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_visible_message_count",
			SQL:         "SELECT COUNT(body) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())
	insertMessageWithBody(t, rt, 3, alice.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_visible_message_count", 20,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView aggregate visibility: %v", err)
	}
	if len(sub.InitialRows) != 1 || len(sub.InitialRows[0]) != 1 || sub.InitialRows[0][0].AsUint64() != 2 {
		t.Fatalf("visible aggregate view rows = %#v, want count 2", sub.InitialRows)
	}
}

func TestDeclaredViewJoinWhereColumnComparisonAppliesVisibility(t *testing.T) {
	alice := visibilityRuntimeIdentity(0x25)
	bob := visibilityRuntimeIdentity(0x26)
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		View(ViewDeclaration{
			Name:        "live_matching_messages",
			SQL:         "SELECT a.* FROM messages AS a JOIN messages AS b ON a.id = b.id WHERE a.body = b.body",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, alice.Hex())
	insertMessageWithBody(t, rt, 2, bob.Hex())

	sub, err := rt.SubscribeView(context.Background(), "live_matching_messages", 15,
		WithDeclaredReadIdentity(alice),
		WithDeclaredReadPermissions("messages:subscribe"),
	)
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if len(sub.InitialRows) != 1 || sub.InitialRows[0][0].AsUint64() != 1 || sub.InitialRows[0][1].AsString() != alice.Hex() {
		t.Fatalf("visible join view rows = %#v, want only caller row", sub.InitialRows)
	}
}

func TestDeclaredReadMissingPermissionRejectsBeforeExecutionOrRegistration(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages ORDER BY body LIMIT 1",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	_, err := rt.CallQuery(context.Background(), "recent_messages", WithDeclaredReadPermissions("messages:write"))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("CallQuery missing permission error = %v, want ErrPermissionDenied", err)
	}

	_, err = rt.SubscribeView(context.Background(), "live_messages", 9, WithDeclaredReadPermissions("messages:read"))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("SubscribeView missing permission error = %v, want ErrPermissionDenied", err)
	}
	_, err = rt.SubscribeView(context.Background(), "live_messages", 9, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView after rejected attempt with same query id: %v", err)
	}
}

func TestDeclaredAggregateViewMissingPermissionRejectsBeforeRegistration(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_message_count",
			SQL:         "SELECT COUNT(*) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	_, err := rt.SubscribeView(context.Background(), "live_message_count", 21, WithDeclaredReadPermissions("messages:read"))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("SubscribeView aggregate missing permission error = %v, want ErrPermissionDenied", err)
	}
	sub, err := rt.SubscribeView(context.Background(), "live_message_count", 21, WithDeclaredReadPermissions("messages:subscribe"))
	if err != nil {
		t.Fatalf("SubscribeView aggregate after rejected attempt with same query id: %v", err)
	}
	if len(sub.InitialRows) != 1 || sub.InitialRows[0][0].AsUint64() != 1 {
		t.Fatalf("aggregate initial rows after permission success = %#v, want count 1", sub.InitialRows)
	}
}

func TestDeclaredReadAllowAllPermissionsBypassesDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte("strict-declared-read-secret"),
	})
	defer rt.Close()
	insertMessage(t, rt, "hello")

	result, err := rt.CallQuery(context.Background(), "recent_messages", WithDeclaredReadAllowAllPermissions())
	if err != nil {
		t.Fatalf("CallQuery allow-all: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("allow-all query rows = %d, want 1", len(result.Rows))
	}
	sub, err := rt.SubscribeView(context.Background(), "live_messages", 11, WithDeclaredReadAllowAllPermissions())
	if err != nil {
		t.Fatalf("SubscribeView allow-all: %v", err)
	}
	if len(sub.InitialRows) != 1 {
		t.Fatalf("allow-all view rows = %d, want 1", len(sub.InitialRows))
	}
}

func TestUnknownDeclaredReadNameDoesNotFallBackToRawSQL(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}))
	defer rt.Close()

	_, err := rt.CallQuery(context.Background(), "SELECT * FROM messages", WithDeclaredReadPermissions("messages:read"))
	if !errors.Is(err, ErrUnknownDeclaredRead) {
		t.Fatalf("CallQuery unknown SQL-shaped name error = %v, want ErrUnknownDeclaredRead", err)
	}
	_, err = rt.SubscribeView(context.Background(), "SELECT * FROM messages", 12, WithDeclaredReadPermissions("messages:subscribe"))
	if !errors.Is(err, ErrUnknownDeclaredRead) {
		t.Fatalf("SubscribeView unknown SQL-shaped name error = %v, want ErrUnknownDeclaredRead", err)
	}
}

func TestMetadataOnlyDeclaredReadCannotExecute(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Query(QueryDeclaration{Name: "recent_messages"}).
		View(ViewDeclaration{Name: "live_messages"}))
	defer rt.Close()

	_, err := rt.CallQuery(context.Background(), "recent_messages")
	if !errors.Is(err, ErrDeclaredReadNotExecutable) {
		t.Fatalf("CallQuery metadata-only error = %v, want ErrDeclaredReadNotExecutable", err)
	}
	_, err = rt.SubscribeView(context.Background(), "live_messages", 13)
	if !errors.Is(err, ErrDeclaredReadNotExecutable) {
		t.Fatalf("SubscribeView metadata-only error = %v, want ErrDeclaredReadNotExecutable", err)
	}
}

func TestRawSQLEquivalentDoesNotInheritDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}))
	defer rt.Close()

	if _, err := rt.CallQuery(context.Background(), "recent_messages", WithDeclaredReadPermissions("messages:read")); err != nil {
		t.Fatalf("CallQuery with declaration permission: %v", err)
	}

	rawLookup := protocol.NewAuthorizedSchemaLookup(rt.registry, types.CallerContext{Permissions: []string{"messages:read"}})
	err := protocol.ValidateSQLQueryString("SELECT * FROM messages", rawLookup, protocol.SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
		AllowOrderBy:    true,
		AllowOffset:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "no such table: `messages`. If the table exists, it may be marked private.") {
		t.Fatalf("raw SQL validation error = %v, want private table rejection", err)
	}
}

func buildStartedDeclaredReadRuntime(t *testing.T, mod *Module) *Runtime {
	t.Helper()
	return buildStartedDeclaredReadRuntimeWithConfig(t, mod, Config{DataDir: t.TempDir()})
}

func buildStartedDeclaredReadRuntimeWithConfig(t *testing.T, mod *Module, cfg Config) *Runtime {
	t.Helper()
	rt, err := Build(mod, cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return rt
}

func insertMessageReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	id := uint64(1)
	if len(args) > 0 {
		id = uint64(args[0])
	}
	_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(id), types.NewString(string(args))})
	return nil, err
}

func insertMessageWithBodyReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing id")
	}
	_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(uint64(args[0])), types.NewString(string(args[1:]))})
	return nil, err
}

func deleteMessageByIDReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing id")
	}
	id := uint64(args[0])
	for rowID, row := range ctx.DB.ScanTable(0) {
		if len(row) > 0 && row[0].AsUint64() == id {
			return nil, ctx.DB.Delete(0, rowID)
		}
	}
	return nil, fmt.Errorf("message %d not found", id)
}

func joinReadTableDef(name string) schema.TableDefinition {
	return schema.TableDefinition{
		Name: name,
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "u32", Type: types.KindUint64},
		},
	}
}

func joinReadIndexedTableDef(name string) schema.TableDefinition {
	def := joinReadTableDef(name)
	def.Indexes = []schema.IndexDefinition{{Name: "idx_" + name + "_u32", Columns: []string{"u32"}}}
	return def
}

func rowsHaveUint64IDs(rows []types.ProductValue, ids ...uint64) bool {
	if len(rows) != len(ids) {
		return false
	}
	want := make(map[uint64]int, len(ids))
	for _, id := range ids {
		want[id]++
	}
	for _, row := range rows {
		if len(row) == 0 {
			return false
		}
		id := row[0].AsUint64()
		if want[id] == 0 {
			return false
		}
		want[id]--
	}
	return true
}

func rowUint64IDs(rows []types.ProductValue) []uint64 {
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		ids = append(ids, row[0].AsUint64())
	}
	return ids
}

func nullableScoresTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "scores",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "score", Type: types.KindUint32, Nullable: true},
		},
	}
}

func seedNullableScoresReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	for _, row := range []types.ProductValue{
		{types.NewUint64(1), types.NewNull(types.KindUint32)},
		{types.NewUint64(2), types.NewUint32(7)},
		{types.NewUint64(3), types.NewUint32(7)},
		{types.NewUint64(4), types.NewUint32(9)},
		{types.NewUint64(5), types.NewNull(types.KindUint32)},
	} {
		if _, err := ctx.DB.Insert(0, row); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func seedJoinRowsReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	for _, row := range []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(10)},
		{types.NewUint64(2), types.NewUint64(20)},
		{types.NewUint64(3), types.NewUint64(20)},
	} {
		if _, err := ctx.DB.Insert(0, row); err != nil {
			return nil, err
		}
	}
	for _, row := range []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(10)},
		{types.NewUint64(99), types.NewUint64(20)},
		{types.NewUint64(3), types.NewUint64(20)},
	} {
		if _, err := ctx.DB.Insert(1, row); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func insertMessage(t *testing.T, rt *Runtime, body string) {
	t.Helper()
	res, err := rt.CallReducer(context.Background(), "insert_message", []byte(body))
	if err != nil {
		t.Fatalf("insert reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("insert reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
}

func insertMessageWithBody(t *testing.T, rt *Runtime, id byte, body string) {
	t.Helper()
	args := append([]byte{id}, []byte(body)...)
	res, err := rt.CallReducer(context.Background(), "insert_message_with_body", args)
	if err != nil {
		t.Fatalf("insert visibility reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("insert visibility reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
}

func deleteMessageByID(t *testing.T, rt *Runtime, id byte) {
	t.Helper()
	res, err := rt.CallReducer(context.Background(), "delete_message_by_id", []byte{id})
	if err != nil {
		t.Fatalf("delete message reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("delete message reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
}

func visibilityRuntimeIdentity(seed byte) types.Identity {
	var id types.Identity
	for i := range id {
		id[i] = seed
	}
	return id
}

func assertStringSlice(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %#v, want %#v", label, got, want)
		}
	}
}

func assertTableIDs(t *testing.T, got, want []schema.TableID, label string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
}
