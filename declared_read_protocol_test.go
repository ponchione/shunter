package shunter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
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
	requireDeclaredReadOneOffRows(t, client, "messages", 1)
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
	requireDeclaredReadAppliedRows(t, client, 31, 41, "messages", 1)
}

func TestProtocolDeclaredReadsSurviveCleanRestart(t *testing.T) {
	dataDir := t.TempDir()
	cfg := declaredReadProtocolConfig(t)
	cfg.DataDir = dataDir
	module := func() *Module {
		return validChatModule().
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
			})
	}

	rt := buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	insertMessage(t, rt, "hello")
	if err := rt.Close(); err != nil {
		t.Fatalf("Close before declared-read restart: %v", err)
	}

	rt = buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "restart-client", "messages:read", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query-after-restart"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffRows(t, client, "messages", 1)

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 51,
		QueryID:   61,
		Name:      "live_messages",
	})
	requireDeclaredReadAppliedRows(t, client, 51, 61, "messages", 1)

	insertMessage(t, rt, "world")
	requireDeclaredReadDeltaRows(t, client, 61, "messages", 1, 0)
}

func TestProtocolDeclaredReadRejectionsDoNotRecoverAfterCleanRestart(t *testing.T) {
	dataDir := t.TempDir()
	cfg := declaredReadProtocolConfig(t)
	cfg.DataDir = dataDir
	module := func() *Module {
		return validChatModule().
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
			})
	}

	rt := buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	deniedClient := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "denied-before-restart"))
	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.DeclaredQueryMsg{
		MessageID: []byte("denied-declared-query-before-restart"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffError(t, deniedClient, "permission denied")

	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.SubscribeDeclaredViewMsg{
		RequestID: 71,
		QueryID:   81,
		Name:      "live_messages",
	})
	requireDeclaredReadSubscriptionError(t, deniedClient, 71, 81, "permission denied")

	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.DeclaredQueryMsg{
		MessageID: []byte("unknown-declared-query-before-restart"),
		Name:      "missing_declared_query",
	})
	requireDeclaredReadOneOffError(t, deniedClient, "unknown declared read")

	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.SubscribeDeclaredViewMsg{
		RequestID: 72,
		QueryID:   82,
		Name:      "missing_declared_view",
	})
	requireDeclaredReadSubscriptionError(t, deniedClient, 72, 82, "unknown declared read")

	insertMessage(t, rt, "before-restart")
	if err := rt.Close(); err != nil {
		t.Fatalf("Close after rejected declared reads before restart: %v", err)
	}

	rt = buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "authorized-after-restart", "messages:read", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query-after-rejected-restart"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffRows(t, client, "messages", 1)

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 71,
		QueryID:   81,
		Name:      "live_messages",
	})
	requireDeclaredReadAppliedRows(t, client, 71, 81, "messages", 1)

	insertMessage(t, rt, "after-restart")
	requireDeclaredReadDeltaRows(t, client, 81, "messages", 1, 0)
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

func TestProtocolDeclaredQueryUsesRuntimeClientSender(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	sender := &declaredReadCapturingSender{}
	rt.mu.Lock()
	rt.protocolSender = sender
	rt.mu.Unlock()

	conn := newDeclaredReadProtocolTestConn(t, "messages:read")
	rt.HandleDeclaredQuery(context.Background(), conn, &protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query-through-sender"),
		Name:      "recent_messages",
	})

	sendCalls, gotConnID, gotMsg, heavyCalls, lightCalls := sender.snapshot()
	if sendCalls != 1 {
		t.Fatalf("sender Send calls = %d, want 1", sendCalls)
	}
	if heavyCalls != 0 || lightCalls != 0 {
		t.Fatalf("transaction sender calls = heavy:%d light:%d, want 0/0", heavyCalls, lightCalls)
	}
	if gotConnID != conn.ID {
		t.Fatalf("sender conn ID = %x, want %x", gotConnID[:], conn.ID[:])
	}
	if got := len(conn.OutboundCh); got != 0 {
		t.Fatalf("conn outbound queue length = %d, want 0; declared reads must use ClientSender", got)
	}

	resp, ok := gotMsg.(protocol.OneOffQueryResponse)
	if !ok {
		t.Fatalf("sender msg = %T, want OneOffQueryResponse", gotMsg)
	}
	if resp.Error != nil {
		t.Fatalf("declared query error = %q, want nil", *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != "messages" {
		t.Fatalf("declared query tables = %+v, want messages table", resp.Tables)
	}
	rows, err := protocol.DecodeRowList(resp.Tables[0].Rows)
	if err != nil {
		t.Fatalf("DecodeRowList sender response: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sender response rows = %d, want 1", len(rows))
	}
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

type declaredReadCapturingSender struct {
	mu sync.Mutex

	sendCalls int
	sendConn  types.ConnectionID
	sendMsg   any
	sendErr   error

	heavyCalls int
	lightCalls int
}

func (s *declaredReadCapturingSender) Send(connID types.ConnectionID, msg any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCalls++
	s.sendConn = connID
	s.sendMsg = msg
	return s.sendErr
}

func (s *declaredReadCapturingSender) SendTransactionUpdate(types.ConnectionID, *protocol.TransactionUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heavyCalls++
	return nil
}

func (s *declaredReadCapturingSender) SendTransactionUpdateLight(types.ConnectionID, *protocol.TransactionUpdateLight) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lightCalls++
	return nil
}

func (s *declaredReadCapturingSender) snapshot() (sendCalls int, sendConn types.ConnectionID, sendMsg any, heavyCalls, lightCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendCalls, s.sendConn, s.sendMsg, s.heavyCalls, s.lightCalls
}

func newDeclaredReadProtocolTestConn(t *testing.T, permissions ...string) *protocol.Conn {
	t.Helper()
	opts := protocol.DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	conn.Permissions = append([]string(nil), permissions...)
	return conn
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

func requireDeclaredReadOneOffRows(t *testing.T, client *websocket.Conn, wantTable string, wantRows int) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want OneOffQueryResponse", tag)
	}
	resp := msg.(protocol.OneOffQueryResponse)
	if resp.Error != nil {
		t.Fatalf("declared query error = %q, want nil", *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != wantTable {
		t.Fatalf("declared query tables = %+v, want table %q", resp.Tables, wantTable)
	}
	rows, err := protocol.DecodeRowList(resp.Tables[0].Rows)
	if err != nil {
		t.Fatalf("DecodeRowList declared query rows for table %q: %v", wantTable, err)
	}
	if len(rows) != wantRows {
		t.Fatalf("declared query row count = %d, want %d for table %q", len(rows), wantRows, wantTable)
	}
}

func requireDeclaredReadAppliedRows(t *testing.T, client *websocket.Conn, requestID, queryID uint32, wantTable string, wantRows int) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want SubscribeSingleApplied for request=%d query=%d", tag, requestID, queryID)
	}
	applied := msg.(protocol.SubscribeSingleApplied)
	if applied.RequestID != requestID || applied.QueryID != queryID || applied.TableName != wantTable {
		t.Fatalf("declared view applied = %+v, want request=%d query=%d table=%q", applied, requestID, queryID, wantTable)
	}
	rows, err := protocol.DecodeRowList(applied.Rows)
	if err != nil {
		t.Fatalf("DecodeRowList declared view rows for request=%d query=%d table=%q: %v", requestID, queryID, wantTable, err)
	}
	if len(rows) != wantRows {
		t.Fatalf("declared view initial row count = %d, want %d for request=%d query=%d table=%q", len(rows), wantRows, requestID, queryID, wantTable)
	}
}

func requireDeclaredReadDeltaRows(t *testing.T, client *websocket.Conn, queryID uint32, wantTable string, wantInserts, wantDeletes int) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("tag = %d, want TransactionUpdateLight for query=%d", tag, queryID)
	}
	update := msg.(protocol.TransactionUpdateLight)
	if len(update.Update) != 1 {
		t.Fatalf("declared view update entries = %+v, want one entry for query=%d", update.Update, queryID)
	}
	entry := update.Update[0]
	if entry.QueryID != queryID || entry.TableName != wantTable {
		t.Fatalf("declared view update entry = %+v, want query=%d table=%q", entry, queryID, wantTable)
	}
	insertRows, err := protocol.DecodeRowList(entry.Inserts)
	if err != nil {
		t.Fatalf("DecodeRowList declared view delta inserts for query=%d table=%q: %v", queryID, wantTable, err)
	}
	deleteRows, err := protocol.DecodeRowList(entry.Deletes)
	if err != nil {
		t.Fatalf("DecodeRowList declared view delta deletes for query=%d table=%q: %v", queryID, wantTable, err)
	}
	if len(insertRows) != wantInserts || len(deleteRows) != wantDeletes {
		t.Fatalf("declared view delta inserts/deletes = %d/%d, want %d/%d for query=%d table=%q", len(insertRows), len(deleteRows), wantInserts, wantDeletes, queryID, wantTable)
	}
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
