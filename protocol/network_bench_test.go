package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/types"
)

func BenchmarkSubscribeSingleWebSocketRoundTrip(b *testing.B) {
	conn := benchmarkSubscribeWebSocketConn(b)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		requestID := uint32(i + 1)
		frame, err := EncodeClientMessage(SubscribeSingleMsg{
			RequestID:   requestID,
			QueryID:     requestID,
			QueryString: "SELECT * FROM orders WHERE user_id = 17",
		})
		if err != nil {
			b.Fatalf("EncodeClientMessage: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
			b.Fatalf("write SubscribeSingle: %v", err)
		}
		tag, decoded := benchmarkReadServerMessage(b, ctx, conn)
		if tag != TagSubscribeSingleApplied {
			b.Fatalf("server tag = %d, want %d", tag, TagSubscribeSingleApplied)
		}
		applied := decoded.(SubscribeSingleApplied)
		if applied.RequestID != requestID || applied.QueryID != requestID {
			b.Fatalf("applied request/query = %d/%d, want %d", applied.RequestID, applied.QueryID, requestID)
		}
	}
}

func benchmarkSubscribeWebSocketConn(b *testing.B) *websocket.Conn {
	b.Helper()
	sl, _ := benchmarkReadSurfaceSchemaAndState()
	s := &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeAnonymous,
		},
		Mint: &auth.MintConfig{
			Issuer:     "https://shunter.local/anonymous",
			Audience:   "shunter-local",
			SigningKey: testSigningKey,
			Expiry:     time.Hour,
		},
		Options:  DefaultProtocolOptions(),
		Executor: &benchmarkSubscribeExecutor{rows: EncodeRowList([][]byte{{0x01, 0x02}})},
		Conns:    NewConnManager(),
		Schema:   sl,
	}
	srv := httptest.NewServer(http.HandlerFunc(s.HandleSubscribe))
	b.Cleanup(srv.Close)

	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(dialCtx, u, &websocket.DialOptions{
		Subprotocols: []string{SubprotocolV1},
	})
	if err != nil {
		b.Fatalf("dial websocket: %v (resp=%v)", err, resp)
	}
	b.Cleanup(func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	})
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b.Fatalf("upgrade status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	tag, _ := benchmarkReadServerMessage(b, dialCtx, conn)
	if tag != TagIdentityToken {
		b.Fatalf("initial server tag = %d, want %d", tag, TagIdentityToken)
	}
	return conn
}

func benchmarkReadServerMessage(b *testing.B, ctx context.Context, conn *websocket.Conn) (uint8, any) {
	b.Helper()
	mt, data, err := conn.Read(ctx)
	if err != nil {
		b.Fatalf("read server message: %v", err)
	}
	if mt != websocket.MessageBinary {
		b.Fatalf("server message type = %v, want binary", mt)
	}
	tag, decoded, err := DecodeServerMessage(data)
	if err != nil {
		b.Fatalf("DecodeServerMessage: %v", err)
	}
	return tag, decoded
}

type benchmarkSubscribeExecutor struct {
	rows []byte
}

func (e *benchmarkSubscribeExecutor) OnConnect(_ context.Context, _ types.ConnectionID, _ types.Identity, _ types.AuthPrincipal) error {
	return nil
}

func (e *benchmarkSubscribeExecutor) OnDisconnect(_ context.Context, _ types.ConnectionID, _ types.Identity, _ types.AuthPrincipal) error {
	return nil
}

func (e *benchmarkSubscribeExecutor) DisconnectClientSubscriptions(_ context.Context, _ types.ConnectionID) error {
	return nil
}

func (e *benchmarkSubscribeExecutor) RegisterSubscriptionSet(_ context.Context, req RegisterSubscriptionSetRequest) error {
	req.Reply(SubscriptionSetCommandResponse{
		SingleApplied: &SubscribeSingleApplied{
			RequestID:                        req.RequestID,
			TotalHostExecutionDurationMicros: elapsedMicros(req.Receipt),
			QueryID:                          req.QueryID,
			TableName:                        "orders",
			Rows:                             e.rows,
		},
	})
	return nil
}

func (e *benchmarkSubscribeExecutor) UnregisterSubscriptionSet(_ context.Context, _ UnregisterSubscriptionSetRequest) error {
	return nil
}

func (e *benchmarkSubscribeExecutor) CallReducer(_ context.Context, _ CallReducerRequest) error {
	return nil
}
