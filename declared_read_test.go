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

func TestDeclaredViewRejectsOrderBy(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages ORDER BY body",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want ORDER BY rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "Unsupported: SELECT * FROM messages ORDER BY body") {
		t.Fatalf("Build error = %v, want ORDER BY unsupported text", err)
	}
}

func TestDeclaredViewRejectsOrderByProjectionAlias(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT body AS text FROM messages ORDER BY text",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want ORDER BY projection alias rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "Unsupported: SELECT body AS text FROM messages ORDER BY text") {
		t.Fatalf("Build error = %v, want ORDER BY unsupported text", err)
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

func TestDeclaredViewRejectsCountColumnAggregate(t *testing.T) {
	_, err := Build(validChatModule().
		View(ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT COUNT(body) AS n FROM messages",
		}), Config{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("Build error = nil, want aggregate rejection for declared view")
	}
	if !errors.Is(err, ErrInvalidDeclarationSQL) {
		t.Fatalf("Build error = %v, want ErrInvalidDeclarationSQL", err)
	}
	if !strings.Contains(err.Error(), "Column projections are not supported in subscriptions") {
		t.Fatalf("Build error = %v, want table-shaped view aggregate rejection", err)
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
			SQL:         "SELECT * FROM messages",
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
			SQL:         "SELECT * FROM messages",
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
