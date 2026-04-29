package shunter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/protocol"
)

const declaredReadProtocolSigningKey = "declared-read-protocol-secret"

func TestProtocolDeclaredQuerySucceedsWithDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "reader", "messages:read"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query"),
		Name:      "recent_messages",
	})

	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want OneOffQueryResponse", tag)
	}
	resp := msg.(protocol.OneOffQueryResponse)
	if resp.Error != nil {
		t.Fatalf("declared query error = %q, want nil", *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != "messages" {
		t.Fatalf("declared query tables = %+v, want messages table", resp.Tables)
	}
	rows, err := protocol.DecodeRowList(resp.Tables[0].Rows)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("declared query row count = %d, want 1", len(rows))
	}
}

func TestProtocolDeclaredViewSucceedsWithDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 31,
		QueryID:   41,
		Name:      "live_messages",
	})

	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want SubscribeSingleApplied", tag)
	}
	applied := msg.(protocol.SubscribeSingleApplied)
	if applied.RequestID != 31 || applied.QueryID != 41 || applied.TableName != "messages" {
		t.Fatalf("declared view applied = %+v, want request/query/table identity", applied)
	}
	rows, err := protocol.DecodeRowList(applied.Rows)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("declared view initial row count = %d, want 1", len(rows))
	}
}

func TestProtocolDeclaredReadsReportPermissionDeniedAndUnknownName(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "missing"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("missing-permission-query"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffError(t, client, "permission denied")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 32,
		QueryID:   42,
		Name:      "live_messages",
	})
	requireDeclaredReadSubscriptionError(t, client, 32, 42, "permission denied")

	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("unknown-query"),
		Name:      "SELECT * FROM messages",
	})
	requireDeclaredReadOneOffError(t, client, "unknown declared read")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 33,
		QueryID:   43,
		Name:      "SELECT * FROM messages",
	})
	requireDeclaredReadSubscriptionError(t, client, 33, 43, "unknown declared read")
}

func TestProtocolRawSQLEquivalentDoesNotUseDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "raw-sql", "messages:read", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   []byte("raw-query"),
		QueryString: "SELECT * FROM messages",
	})
	requireDeclaredReadOneOffError(t, client, "no such table: `messages`. If the table exists, it may be marked private.")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeSingleMsg{
		RequestID:   34,
		QueryID:     44,
		QueryString: "SELECT * FROM messages",
	})
	requireDeclaredReadSubscriptionError(t, client, 34, 44, "no such table: `messages`. If the table exists, it may be marked private.")
}

func declaredReadProtocolConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte(declaredReadProtocolSigningKey),
	}
}

func mintDeclaredReadProtocolToken(t *testing.T, subject string, permissions ...string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":         "declared-read-test",
		"sub":         subject,
		"permissions": permissions,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(declaredReadProtocolSigningKey))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func dialDeclaredReadProtocol(t *testing.T, rt *Runtime, token string) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(rt.HTTPHandler())
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/subscribe"
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: []string{protocol.SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("dial runtime protocol: %v", err)
	}
	t.Cleanup(func() { client.CloseNow() })

	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagIdentityToken {
		t.Fatalf("first protocol tag = %d, msg = %T, want IdentityToken", tag, msg)
	}
	return client
}

func writeDeclaredReadProtocolMessage(t *testing.T, client *websocket.Conn, msg any) {
	t.Helper()
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		t.Fatalf("EncodeClientMessage(%T): %v", msg, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write protocol message %T: %v", msg, err)
	}
}

func readDeclaredReadProtocolMessage(t *testing.T, client *websocket.Conn) (uint8, any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, frame, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read protocol message: %v", err)
	}
	tag, msg, err := protocol.DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("DecodeServerMessage: %v", err)
	}
	return tag, msg
}

func requireDeclaredReadOneOffError(t *testing.T, client *websocket.Conn, wantSubstring string) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want OneOffQueryResponse", tag)
	}
	resp := msg.(protocol.OneOffQueryResponse)
	if resp.Error == nil {
		t.Fatal("one-off response error = nil, want error")
	}
	if !strings.Contains(*resp.Error, wantSubstring) {
		t.Fatalf("one-off response error = %q, want substring %q", *resp.Error, wantSubstring)
	}
}

func requireDeclaredReadSubscriptionError(t *testing.T, client *websocket.Conn, requestID, queryID uint32, wantSubstring string) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("tag = %d, want SubscriptionError", tag)
	}
	resp := msg.(protocol.SubscriptionError)
	if resp.RequestID == nil || *resp.RequestID != requestID {
		t.Fatalf("subscription error request id = %v, want %d", resp.RequestID, requestID)
	}
	if resp.QueryID == nil || *resp.QueryID != queryID {
		t.Fatalf("subscription error query id = %v, want %d", resp.QueryID, queryID)
	}
	if !strings.Contains(resp.Error, wantSubstring) {
		t.Fatalf("subscription error = %q, want substring %q", resp.Error, wantSubstring)
	}
}
