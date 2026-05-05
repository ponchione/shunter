package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const authReadNoSuchMessages = "no such table: `messages`. If the table exists, it may be marked private."

func strictReadAdmissionConn() *Conn {
	conn := testConnDirect(nil)
	conn.AllowAllPermissions = false
	conn.Permissions = nil
	return conn
}

func authReadOneTableLookup(policy schema.ReadPolicy) *mockSchemaLookup {
	sl := newMockSchema("messages", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
		schema.ColumnSchema{Index: 1, Name: "body", Type: schema.KindString},
	)
	entry := sl.tables["messages"]
	entry.schema.ReadPolicy = policy
	sl.tables["messages"] = entry
	return sl
}

func authReadJoinLookup(visiblePolicy, secretPolicy schema.ReadPolicy) *mockSchemaLookup {
	return &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"visible": {
			id: 1,
			schema: &schema.TableSchema{
				ID:         1,
				Name:       "visible",
				ReadPolicy: visiblePolicy,
				Columns: []schema.ColumnSchema{
					{Index: 0, Name: "id", Type: schema.KindUint64},
					{Index: 1, Name: "label", Type: schema.KindString},
				},
			},
		},
		"secret": {
			id: 2,
			schema: &schema.TableSchema{
				ID:         2,
				Name:       "secret",
				ReadPolicy: secretPolicy,
				Columns: []schema.ColumnSchema{
					{Index: 0, Name: "id", Type: schema.KindUint64},
					{Index: 1, Name: "visible_id", Type: schema.KindUint64},
					{Index: 2, Name: "flag", Type: schema.KindBool},
				},
			},
		},
	}}
}

func authReadState(rows map[schema.TableID][]types.ProductValue) *mockStateAccess {
	return &mockStateAccess{snap: &mockSnapshot{rows: rows}}
}

func requireOneOffAuthError(t *testing.T, conn *Conn, want string) {
	t.Helper()
	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected one-off error, got success")
	}
	if *result.Error != want {
		t.Fatalf("one-off error = %q, want %q", *result.Error, want)
	}
	if len(result.Tables) != 0 {
		t.Fatalf("one-off error returned %d tables, want 0", len(result.Tables))
	}
}

func requireNoSubscribeFrame(t *testing.T, conn *Conn) {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected subscribe frame: %x", frame)
	default:
	}
}

func TestAuthReadAdmissionOneOffDefaultPrivateRejected(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadOneTableLookup(schema.ReadPolicy{})

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("private-one-off"),
		QueryString: "SELECT * FROM messages",
	}, authReadState(nil), sl)

	requireOneOffAuthError(t, conn, authReadNoSuchMessages)
}

func TestAuthReadAdmissionAggregateOrderByPrivateRejected(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadOneTableLookup(schema.ReadPolicy{})

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("private-aggregate-order"),
		QueryString: "SELECT COUNT(*) AS n FROM messages ORDER BY n",
	}, authReadState(nil), sl)

	requireOneOffAuthError(t, conn, authReadNoSuchMessages)
}

func TestAuthReadAdmissionSubscribeDefaultPrivateRejected(t *testing.T) {
	conn := strictReadAdmissionConn()
	executor := &mockSubExecutor{}
	sl := authReadOneTableLookup(schema.ReadPolicy{})

	handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
		RequestID:   10,
		QueryID:     20,
		QueryString: "SELECT * FROM messages",
	}, executor, sl)

	requireSubscriptionError(t, conn, 10, 20, authReadNoSuchMessages+", executing: `SELECT * FROM messages`")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor registered an unauthorized default-private subscription")
	}
}

func TestAuthReadAdmissionOneOffPublicSucceeds(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadOneTableLookup(schema.ReadPolicy{Access: schema.TableAccessPublic})

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("public-one-off"),
		QueryString: "SELECT * FROM messages",
	}, authReadState(map[schema.TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("hello")}},
	}), sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("one-off public query error = %q, want nil", *result.Error)
	}
	if len(result.Tables) != 1 {
		t.Fatalf("one-off public query returned %d tables, want 1", len(result.Tables))
	}
}

func TestAuthReadAdmissionSubscribePublicSucceeds(t *testing.T) {
	conn := strictReadAdmissionConn()
	executor := &mockSubExecutor{}
	sl := authReadOneTableLookup(schema.ReadPolicy{Access: schema.TableAccessPublic})

	handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
		RequestID:   11,
		QueryID:     21,
		QueryString: "SELECT * FROM messages",
	}, executor, sl)

	requireNoSubscribeFrame(t, conn)
	if req := executor.getRegisterSetReq(); req == nil {
		t.Fatal("executor did not register authorized public subscription")
	}
}

func TestAuthReadAdmissionPermissionedTableChecksCallerTags(t *testing.T) {
	sl := authReadOneTableLookup(schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read"},
	})

	missingConn := strictReadAdmissionConn()
	handleOneOffQuery(context.Background(), missingConn, &OneOffQueryMsg{
		MessageID:   []byte("missing-permission"),
		QueryString: "SELECT * FROM messages",
	}, authReadState(nil), sl)
	requireOneOffAuthError(t, missingConn, authReadNoSuchMessages)

	allowedConn := strictReadAdmissionConn()
	allowedConn.Permissions = []string{"messages:read"}
	handleSubscribeSingle(context.Background(), allowedConn, &SubscribeSingleMsg{
		RequestID:   12,
		QueryID:     22,
		QueryString: "SELECT * FROM messages",
	}, &mockSubExecutor{}, sl)
	requireNoSubscribeFrame(t, allowedConn)
}

func TestAuthReadAdmissionAllowAllBypassesPrivateAndPermissionedTables(t *testing.T) {
	privateConn := strictReadAdmissionConn()
	privateConn.AllowAllPermissions = true
	privateLookup := authReadOneTableLookup(schema.ReadPolicy{Access: schema.TableAccessPrivate})
	handleOneOffQuery(context.Background(), privateConn, &OneOffQueryMsg{
		MessageID:   []byte("allow-all-private"),
		QueryString: "SELECT * FROM messages",
	}, authReadState(map[schema.TableID][]types.ProductValue{}), privateLookup)
	if result := drainOneOff(t, privateConn); result.Error != nil {
		t.Fatalf("allow-all private one-off error = %q, want nil", *result.Error)
	}

	permissionedConn := strictReadAdmissionConn()
	permissionedConn.AllowAllPermissions = true
	executor := &mockSubExecutor{}
	permissionedLookup := authReadOneTableLookup(schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read"},
	})
	handleSubscribeSingle(context.Background(), permissionedConn, &SubscribeSingleMsg{
		RequestID:   13,
		QueryID:     23,
		QueryString: "SELECT * FROM messages",
	}, executor, permissionedLookup)
	requireNoSubscribeFrame(t, permissionedConn)
	if req := executor.getRegisterSetReq(); req == nil {
		t.Fatal("executor did not register allow-all permissioned subscription")
	}
}

func TestAuthReadAdmissionPrivateJoinTableRejectedWhenProjected(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadJoinLookup(
		schema.ReadPolicy{Access: schema.TableAccessPublic},
		schema.ReadPolicy{Access: schema.TableAccessPrivate},
	)

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("project-private"),
		QueryString: "SELECT secret.* FROM visible JOIN secret ON visible.id = secret.visible_id",
	}, authReadState(nil), sl)

	requireOneOffAuthError(t, conn, "no such table: `secret`. If the table exists, it may be marked private.")
}

func TestAuthReadAdmissionPrivateJoinTableRejectedWhenNotProjected(t *testing.T) {
	conn := strictReadAdmissionConn()
	executor := &mockSubExecutor{}
	sl := authReadJoinLookup(
		schema.ReadPolicy{Access: schema.TableAccessPublic},
		schema.ReadPolicy{Access: schema.TableAccessPrivate},
	)
	const sqlText = "SELECT visible.* FROM visible JOIN secret ON visible.id = secret.visible_id"

	handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
		RequestID:   14,
		QueryID:     24,
		QueryString: sqlText,
	}, executor, sl)

	requireSubscriptionError(t, conn, 14, 24, "no such table: `secret`. If the table exists, it may be marked private., executing: `"+sqlText+"`")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor registered a subscription with an unauthorized non-projected join table")
	}
}

func TestAuthReadAdmissionPrivateJoinPredicateDoesNotLeakShape(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadJoinLookup(
		schema.ReadPolicy{Access: schema.TableAccessPublic},
		schema.ReadPolicy{Access: schema.TableAccessPrivate},
	)

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("private-join-shape"),
		QueryString: "SELECT visible.* FROM visible JOIN secret ON visible.id = secret.missing",
	}, authReadState(nil), sl)

	requireOneOffAuthError(t, conn, "no such table: `secret`. If the table exists, it may be marked private.")
}

func TestAuthReadAdmissionPrivateMultiWayJoinTableRejected(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadJoinLookup(
		schema.ReadPolicy{Access: schema.TableAccessPublic},
		schema.ReadPolicy{Access: schema.TableAccessPrivate},
	)

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("private-multi-join"),
		QueryString: "SELECT visible.* FROM visible JOIN secret ON visible.id = secret.visible_id JOIN visible AS v2 ON secret.visible_id = v2.id",
	}, authReadState(nil), sl)

	requireOneOffAuthError(t, conn, "no such table: `secret`. If the table exists, it may be marked private.")
}

func TestAuthReadAdmissionSubscribeMultiUnauthorizedQueryRegistersNone(t *testing.T) {
	conn := strictReadAdmissionConn()
	executor := &mockSubExecutor{}
	sl := authReadJoinLookup(
		schema.ReadPolicy{Access: schema.TableAccessPublic},
		schema.ReadPolicy{Access: schema.TableAccessPrivate},
	)
	const unauthorizedSQL = "SELECT * FROM secret"

	handleSubscribeMulti(context.Background(), conn, &SubscribeMultiMsg{
		RequestID: 15,
		QueryID:   25,
		QueryStrings: []string{
			"SELECT * FROM visible",
			unauthorizedSQL,
		},
	}, executor, sl)

	requireSubscriptionError(t, conn, 15, 25, "no such table: `secret`. If the table exists, it may be marked private., executing: `"+unauthorizedSQL+"`")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor registered a SubscribeMulti batch after one query was unauthorized")
	}
}

func TestAuthReadAdmissionUnknownTableErrorTextUnchanged(t *testing.T) {
	conn := strictReadAdmissionConn()
	sl := authReadOneTableLookup(schema.ReadPolicy{Access: schema.TableAccessPublic})

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("unknown-one-off"),
		QueryString: "SELECT * FROM missing",
	}, authReadState(nil), sl)

	requireOneOffAuthError(t, conn, "no such table: `missing`. If the table exists, it may be marked private.")
}

func TestAuthReadAdmissionSubscriptionUnknownTableErrorTextWrappedWithSQL(t *testing.T) {
	conn := strictReadAdmissionConn()
	executor := &mockSubExecutor{}
	sl := authReadOneTableLookup(schema.ReadPolicy{Access: schema.TableAccessPublic})
	const sqlText = "SELECT * FROM missing"

	handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
		RequestID:   16,
		QueryID:     26,
		QueryString: sqlText,
	}, executor, sl)

	requireSubscriptionError(t, conn, 16, 26, "no such table: `missing`. If the table exists, it may be marked private., executing: `"+sqlText+"`")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor registered a subscription for an unknown table")
	}
}
