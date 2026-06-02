package shunter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/websocket"
)

func BenchmarkDeclaredReadHostedSubscriptionReducerDelta(b *testing.B) {
	benchmarkDeclaredReadHostedSubscriptionReducerDelta(b, 1)
}

func BenchmarkDeclaredReadHostedSubscriptionReducerFanout2(b *testing.B) {
	benchmarkDeclaredReadHostedSubscriptionReducerDelta(b, 2)
}

type declaredReadHostedSubscriptionBenchmarkSubscriber struct {
	client  *websocket.Conn
	queryID uint32
}

func benchmarkDeclaredReadHostedSubscriptionReducerDelta(b *testing.B, subscriberCount int) {
	b.Helper()
	if subscriberCount < 1 {
		b.Fatalf("subscriber count = %d, want at least one", subscriberCount)
	}
	rt := buildDeclaredReadHostedSubscriptionBenchmarkRuntime(b)
	b.Cleanup(func() {
		if err := rt.Close(); err != nil {
			b.Fatalf("Close runtime: %v", err)
		}
	})

	srv := httptest.NewServer(rt.HTTPHandler())
	b.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/subscribe"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	caller := dialDeclaredReadHostedSubscriptionBenchmarkProtocol(b, ctx, wsURL, "hosted-subscription-caller", "messages:write")
	b.Cleanup(func() {
		_ = caller.CloseNow()
	})

	subscribers := make([]declaredReadHostedSubscriptionBenchmarkSubscriber, 0, subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		client := dialDeclaredReadHostedSubscriptionBenchmarkProtocol(
			b,
			ctx,
			wsURL,
			fmt.Sprintf("hosted-subscription-subscriber-%d", i+1),
			"messages:subscribe",
		)
		b.Cleanup(func() {
			_ = client.CloseNow()
		})
		subscribers = append(subscribers, declaredReadHostedSubscriptionBenchmarkSubscriber{
			client:  client,
			queryID: uint32(9101 + i),
		})
	}
	for i, subscriber := range subscribers {
		benchmarkSubscribeDeclaredReadHostedView(b, ctx, subscriber.client, uint32(9001+i), subscriber.queryID)
	}

	insertArgs := append([]byte{250}, []byte("hosted-delta")...)
	deleteArgs := []byte{250}

	benchmarkCallDeclaredReadHostedReducer(b, ctx, caller, 8001, "insert_message_with_body", insertArgs)
	for _, subscriber := range subscribers {
		benchmarkRequireDeclaredReadHostedDelta(b, ctx, subscriber.client, subscriber.queryID, 1, 0)
	}
	benchmarkCallDeclaredReadHostedReducer(b, ctx, caller, 8002, "delete_message_by_id", deleteArgs)
	for _, subscriber := range subscribers {
		benchmarkRequireDeclaredReadHostedDelta(b, ctx, subscriber.client, subscriber.queryID, 0, 1)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		insertRequestID := uint32(10000 + i*2)
		deleteRequestID := insertRequestID + 1
		benchmarkCallDeclaredReadHostedReducer(b, ctx, caller, insertRequestID, "insert_message_with_body", insertArgs)
		for _, subscriber := range subscribers {
			benchmarkRequireDeclaredReadHostedDelta(b, ctx, subscriber.client, subscriber.queryID, 1, 0)
		}
		benchmarkCallDeclaredReadHostedReducer(b, ctx, caller, deleteRequestID, "delete_message_by_id", deleteArgs)
		for _, subscriber := range subscribers {
			benchmarkRequireDeclaredReadHostedDelta(b, ctx, subscriber.client, subscriber.queryID, 0, 1)
		}
	}
}

func buildDeclaredReadHostedSubscriptionBenchmarkRuntime(b *testing.B) *Runtime {
	b.Helper()
	rt, err := Build(validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer,
			WithReducerPermissions(PermissionMetadata{Required: []string{"messages:write"}})).
		Reducer("delete_message_by_id", deleteMessageByIDReducer,
			WithReducerPermissions(PermissionMetadata{Required: []string{"messages:write"}})).
		View(ViewDeclaration{
			Name:        "live_hosted_delta_messages",
			SQL:         "SELECT * FROM messages WHERE body = 'hosted-delta'",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), Config{
		DataDir:        b.TempDir(),
		EnableProtocol: true,
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte(declaredReadProtocolSigningKey),
	})
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		b.Fatalf("Start: %v", err)
	}
	return rt
}

func dialDeclaredReadHostedSubscriptionBenchmarkProtocol(
	b *testing.B,
	ctx context.Context,
	wsURL string,
	subject string,
	permissions ...string,
) *websocket.Conn {
	b.Helper()
	token := mintDeclaredReadHostedSubscriptionBenchmarkToken(b, subject, permissions...)
	client, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + token}},
		Subprotocols: []string{protocol.SubprotocolV1},
	})
	if err != nil {
		b.Fatalf("dial runtime protocol: %v", err)
	}
	tag, msg := benchmarkReadDeclaredReadHostedProtocolMessage(b, ctx, client)
	if tag != protocol.TagIdentityToken {
		b.Fatalf("first protocol tag = %d, msg = %T, want IdentityToken", tag, msg)
	}
	return client
}

func mintDeclaredReadHostedSubscriptionBenchmarkToken(b *testing.B, subject string, permissions ...string) string {
	b.Helper()
	claims := jwt.MapClaims{
		"iss":         "declared-read-benchmark",
		"sub":         subject,
		"permissions": permissions,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(declaredReadProtocolSigningKey))
	if err != nil {
		b.Fatalf("sign token: %v", err)
	}
	return signed
}

func benchmarkSubscribeDeclaredReadHostedView(
	b *testing.B,
	ctx context.Context,
	client *websocket.Conn,
	requestID uint32,
	queryID uint32,
) {
	b.Helper()
	benchmarkWriteDeclaredReadHostedProtocolMessage(b, ctx, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: requestID,
		QueryID:   queryID,
		Name:      "live_hosted_delta_messages",
	})
	tag, msg := benchmarkReadDeclaredReadHostedProtocolMessage(b, ctx, client)
	if tag != protocol.TagSubscribeSingleApplied {
		b.Fatalf("subscribe tag = %d, want SubscribeSingleApplied", tag)
	}
	applied, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok || applied.RequestID != requestID || applied.QueryID != queryID || applied.TableName != "messages" {
		b.Fatalf("declared view applied = %+v, want request=%d query=%d table=messages", msg, requestID, queryID)
	}
	rows, err := protocol.DecodeRowList(applied.Rows)
	if err != nil {
		b.Fatalf("DecodeRowList declared view initial rows: %v", err)
	}
	if len(rows) != 0 {
		b.Fatalf("declared view initial row count = %d, want 0", len(rows))
	}
}

func benchmarkCallDeclaredReadHostedReducer(
	b *testing.B,
	ctx context.Context,
	client *websocket.Conn,
	requestID uint32,
	reducerName string,
	args []byte,
) {
	b.Helper()
	benchmarkWriteDeclaredReadHostedProtocolMessage(b, ctx, client, protocol.CallReducerMsg{
		RequestID:   requestID,
		ReducerName: reducerName,
		Args:        args,
		Flags:       protocol.CallReducerFlagsFullUpdate,
	})
	tag, msg := benchmarkReadDeclaredReadHostedProtocolMessage(b, ctx, client)
	if tag != protocol.TagTransactionUpdate {
		b.Fatalf("reducer %s tag = %d, want TransactionUpdate", reducerName, tag)
	}
	update, ok := msg.(protocol.TransactionUpdate)
	if !ok {
		b.Fatalf("reducer %s response = %T, want TransactionUpdate", reducerName, msg)
	}
	if update.ReducerCall.RequestID != requestID || update.ReducerCall.ReducerName != reducerName {
		b.Fatalf("reducer call info = %+v, want request=%d reducer=%s", update.ReducerCall, requestID, reducerName)
	}
	if _, ok := update.Status.(protocol.StatusCommitted); !ok {
		b.Fatalf("reducer %s status = %#v, want committed", reducerName, update.Status)
	}
}

func benchmarkRequireDeclaredReadHostedDelta(
	b *testing.B,
	ctx context.Context,
	client *websocket.Conn,
	queryID uint32,
	wantInserts int,
	wantDeletes int,
) {
	b.Helper()
	tag, msg := benchmarkReadDeclaredReadHostedProtocolMessage(b, ctx, client)
	if tag != protocol.TagTransactionUpdateLight {
		b.Fatalf("delta tag = %d, want TransactionUpdateLight", tag)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		b.Fatalf("delta response = %T, want TransactionUpdateLight", msg)
	}
	if len(update.Update) != 1 {
		b.Fatalf("declared view update entries = %+v, want one entry", update.Update)
	}
	entry := update.Update[0]
	if entry.QueryID != queryID || entry.TableName != "messages" {
		b.Fatalf("declared view update entry = %+v, want query=%d table=messages", entry, queryID)
	}
	inserts, err := protocol.DecodeRowList(entry.Inserts)
	if err != nil {
		b.Fatalf("DecodeRowList delta inserts: %v", err)
	}
	deletes, err := protocol.DecodeRowList(entry.Deletes)
	if err != nil {
		b.Fatalf("DecodeRowList delta deletes: %v", err)
	}
	if len(inserts) != wantInserts || len(deletes) != wantDeletes {
		b.Fatalf("delta inserts/deletes = %d/%d, want %d/%d", len(inserts), len(deletes), wantInserts, wantDeletes)
	}
}

func benchmarkWriteDeclaredReadHostedProtocolMessage(b *testing.B, ctx context.Context, client *websocket.Conn, msg any) {
	b.Helper()
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		b.Fatalf("EncodeClientMessage(%T): %v", msg, err)
	}
	if err := client.Write(ctx, websocket.MessageBinary, frame); err != nil {
		b.Fatalf("write protocol message %T: %v", msg, err)
	}
}

func benchmarkReadDeclaredReadHostedProtocolMessage(b *testing.B, ctx context.Context, client *websocket.Conn) (uint8, any) {
	b.Helper()
	mt, frame, err := client.Read(ctx)
	if err != nil {
		b.Fatalf("read protocol message: %v", err)
	}
	if mt != websocket.MessageBinary {
		b.Fatalf("protocol message type = %v, want binary", mt)
	}
	tag, msg, err := protocol.DecodeServerMessage(frame)
	if err != nil {
		b.Fatalf("DecodeServerMessage: %v", err)
	}
	return tag, msg
}
