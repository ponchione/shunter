package protocolclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/protocol"
)

func TestDialSendsBearerTokenNegotiatesSubprotocolAndReadsIdentity(t *testing.T) {
	const token = "operator-token"
	wantIdentity := protocol.IdentityToken{
		Identity:     [32]byte{1},
		Token:        "server-token",
		ConnectionID: [16]byte{2},
	}
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, identity, err := Dial(ctx, Options{URL: srv.wsURL(), Token: token})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
	if client.IdentityToken() != wantIdentity {
		t.Fatalf("client identity = %+v, want %+v", client.IdentityToken(), wantIdentity)
	}
	if client.Subprotocol() != protocol.SubprotocolV2 {
		t.Fatalf("subprotocol = %q, want %q", client.Subprotocol(), protocol.SubprotocolV2)
	}
	if got := client.NextRequestID(); got != 1 {
		t.Fatalf("first request ID = %d, want 1", got)
	}
	if got := client.NextRequestID(); got != 2 {
		t.Fatalf("second request ID = %d, want 2", got)
	}
}

func TestDialRequiresExplicitTokenBeforeNetwork(t *testing.T) {
	called := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(http.ResponseWriter, *http.Request) {
		called <- struct{}{}
	})

	_, _, err := Dial(context.Background(), Options{URL: srv.wsURL(), Token: " \t"})
	if !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("Dial error = %v, want ErrTokenRequired", err)
	}
	select {
	case <-called:
		t.Fatal("server was called despite missing token")
	default:
	}
}

func TestDialClassifiesIdentityReadTimeout(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Dial timeout error = %v, want ErrTimeout", err)
	}
}

func TestClientSendAndReadUseProtocolCodecs(t *testing.T) {
	const token = "operator-token"
	serverDone := make(chan protocol.CallReducerMsg, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read client message: %v", err)
			return
		}
		_, msg, err := protocol.DecodeClientMessage(frame)
		if err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		call, ok := msg.(protocol.CallReducerMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.CallReducerMsg", msg)
			return
		}
		serverDone <- call
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusFailed{Error: "boom"},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: token})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	requestID := client.NextRequestID()
	if err := client.Send(ctx, protocol.CallReducerMsg{
		ReducerName: "send_message",
		Args:        []byte{1, 2, 3},
		RequestID:   requestID,
		Flags:       protocol.CallReducerFlagsNoSuccessNotify,
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	select {
	case call := <-serverDone:
		if call.ReducerName != "send_message" || call.RequestID != requestID || call.Flags != protocol.CallReducerFlagsNoSuccessNotify {
			t.Fatalf("server call = %+v", call)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive call: %v", ctx.Err())
	}

	tag, msg, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if tag != protocol.TagTransactionUpdate {
		t.Fatalf("server tag = %d, want TransactionUpdate", tag)
	}
	update, ok := msg.(protocol.TransactionUpdate)
	if !ok {
		t.Fatalf("server message = %T, want TransactionUpdate", msg)
	}
	if update.ReducerCall.RequestID != requestID {
		t.Fatalf("update request ID = %d, want %d", update.ReducerCall.RequestID, requestID)
	}
}

func TestDialRejectsUnexpectedFirstMessage(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: []byte{1}})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if !errors.Is(err, ErrUnexpectedMessage) {
		t.Fatalf("Dial unexpected message error = %v, want ErrUnexpectedMessage", err)
	}
}

type protocolClientHTTPServer struct {
	*httptest.Server
}

func protocolClientTestServer(t *testing.T, handler http.HandlerFunc) *protocolClientHTTPServer {
	t.Helper()
	srv := &protocolClientHTTPServer{Server: httptest.NewServer(handler)}
	t.Cleanup(srv.Close)
	return srv
}

func (s *protocolClientHTTPServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

func acceptProtocolClientTestConn(t *testing.T, w http.ResponseWriter, r *http.Request) *websocket.Conn {
	t.Helper()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: protocol.SupportedSubprotocols(),
	})
	if err != nil {
		t.Fatalf("websocket.Accept: %v", err)
	}
	return ws
}

func writeProtocolClientServerMessage(t *testing.T, ws *websocket.Conn, msg any) {
	t.Helper()
	frame, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("EncodeServerMessage(%T): %v", msg, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("server write %T: %v", msg, err)
	}
}
