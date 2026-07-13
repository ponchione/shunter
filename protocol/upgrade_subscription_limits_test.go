package protocol

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/websocket"
)

func liveSubscriptionLimitServer(t *testing.T, maxQueries int) (*Server, *mockSubExecutor) {
	t.Helper()
	executor := &mockSubExecutor{}
	schemaLookup := newMockSchema("users", 1, schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32})
	_, table, _ := schemaLookup.TableByName("users")
	table.ReadPolicy.Access = schema.TableAccessPublic
	return &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Options:            DefaultProtocolOptions(),
		Executor:           executor,
		Conns:              NewConnManager(),
		Schema:             schemaLookup,
		SubscriptionLimits: SubscriptionLimits{MaxQueriesPerSet: maxQueries},
	}, executor
}

func TestHandleSubscribeAppliesConfiguredSubscriptionLimitBeforeCompile(t *testing.T) {
	server, executor := liveSubscriptionLimitServer(t, 2)
	srv := newTestServer(t, server)
	conn, resp, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if _, err := readOneBinary(t, conn, 2*time.Second); err != nil {
		t.Fatalf("read IdentityToken: %v", err)
	}

	frame, err := EncodeClientMessage(SubscribeMultiMsg{
		RequestID: 1,
		QueryID:   2,
		QueryStrings: []string{
			"SELECT * FROM users WHERE id = 1",
			"SELECT * FROM users WHERE id = 2",
			"SELECT * FROM missing WHERE id = 3",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write SubscribeMulti: %v", err)
	}
	response, err := readOneBinary(t, conn, 2*time.Second)
	if err != nil {
		t.Fatalf("read SubscriptionError: %v", err)
	}
	tag, decoded, err := DecodeServerMessage(response)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscriptionError {
		t.Fatalf("response tag = %d, want TagSubscriptionError", tag)
	}
	if got := decoded.(SubscriptionError).Error; got != "subscription: query count limit exceeded: queries_per_set=3 cap=2, executing: ``" {
		t.Fatalf("SubscriptionError = %q, want configured query-limit rejection", got)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("oversized request reached executor: %+v", req)
	}
}

func TestHandleSubscribeAcceptsConfiguredSubscriptionExactLimit(t *testing.T) {
	server, executor := liveSubscriptionLimitServer(t, 2)
	srv := newTestServer(t, server)
	conn, resp, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if _, err := readOneBinary(t, conn, 2*time.Second); err != nil {
		t.Fatalf("read IdentityToken: %v", err)
	}

	frame, err := EncodeClientMessage(SubscribeMultiMsg{
		RequestID: 1,
		QueryID:   2,
		QueryStrings: []string{
			"SELECT * FROM users WHERE id = 1",
			"SELECT * FROM users WHERE id = 2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write SubscribeMulti: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if req := executor.getRegisterSetReq(); req != nil {
			if len(req.Predicates) != 2 {
				t.Fatalf("compiled predicates = %d, want 2", len(req.Predicates))
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("exact-limit subscription did not reach executor")
}

func TestHandleSubscribeRejectsInvalidSubscriptionLimitsBeforeUpgrade(t *testing.T) {
	for _, maxQueries := range []int{-1, int(MaxSubscribeMultiQueriesHard) + 1} {
		server, _ := strictServer(t)
		server.SubscriptionLimits.MaxQueriesPerSet = maxQueries
		srv := newTestServer(t, server)
		_, resp, err := dialWS(t, srv, wsDialOpts{
			authHeader:   "Bearer " + mintValidToken(t),
			subprotocols: []string{SubprotocolV1},
		})
		if err == nil {
			t.Fatalf("dial with MaxQueriesPerSet=%d succeeded", maxQueries)
		}
		if resp == nil || resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("MaxQueriesPerSet=%d status = %v, want 500", maxQueries, resp)
		}
	}
}
