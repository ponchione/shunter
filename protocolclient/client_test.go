package protocolclient

import (
	"bytes"
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

func TestDialRequiresExplicitURLBeforeNetwork(t *testing.T) {
	_, _, err := Dial(context.Background(), Options{URL: " \t", Token: "operator-token"})
	if !errors.Is(err, ErrURLRequired) {
		t.Fatalf("Dial error = %v, want ErrURLRequired", err)
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

func TestDialRejectsMissingSubprotocol(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConnWithSubprotocols(t, w, r, nil)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("Dial missing subprotocol error = %v, want ErrProtocolVersion", err)
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

func TestClientCallReducerWaitsForMatchingTransactionUpdate(t *testing.T) {
	received := make(chan protocol.CallReducerMsg, 1)
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
		received <- call
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	update, err := client.CallReducer(ctx, "send_message", []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("CallReducer returned error: %v", err)
	}
	if update.ReducerCall.ReducerName != "send_message" || update.ReducerCall.RequestID != 1 {
		t.Fatalf("CallReducer update = %+v", update)
	}

	select {
	case call := <-received:
		if call.ReducerName != "send_message" || call.RequestID != 1 || call.Flags != protocol.CallReducerFlagsFullUpdate {
			t.Fatalf("server call = %+v", call)
		}
		if !bytes.Equal(call.Args, []byte{1, 2, 3}) {
			t.Fatalf("server call args = %x", call.Args)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive call: %v", ctx.Err())
	}
}

func TestClientCallReducerSurfacesFailedStatus(t *testing.T) {
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
		call := msg.(protocol.CallReducerMsg)
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusFailed{Error: "boom"},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.CallReducer(ctx, "send_message", nil)
	if !errors.Is(err, ErrReducerFailed) {
		t.Fatalf("CallReducer error = %v, want ErrReducerFailed", err)
	}
}

func TestClientCallReducerRejectsMismatchedResponse(t *testing.T) {
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
		call := msg.(protocol.CallReducerMsg)
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: "other_reducer", RequestID: call.RequestID},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.CallReducer(ctx, "send_message", nil)
	if !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("CallReducer mismatch error = %v, want ErrResponseMismatch", err)
	}
}

func TestDialAndCallReducerUsesExplicitTokenAndCloses(t *testing.T) {
	const token = "operator-token"
	wantIdentity := protocol.IdentityToken{Identity: [32]byte{1}, Token: "server-token", ConnectionID: [16]byte{2}}
	received := make(chan protocol.CallReducerMsg, 1)
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)

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
		received <- call
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err = ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, update, err := DialAndCallReducer(ctx, Options{URL: srv.wsURL(), Token: token}, ReducerCallRequest{
		Name:      "send_message",
		Arguments: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("DialAndCallReducer returned error: %v", err)
	}
	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
	if update.ReducerCall.ReducerName != "send_message" || update.ReducerCall.RequestID != 1 {
		t.Fatalf("DialAndCallReducer update = %+v", update)
	}

	select {
	case call := <-received:
		if call.ReducerName != "send_message" || call.RequestID != 1 || call.Flags != protocol.CallReducerFlagsFullUpdate {
			t.Fatalf("server call = %+v", call)
		}
		if !bytes.Equal(call.Args, []byte{1, 2, 3}) {
			t.Fatalf("server call args = %x", call.Args)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive call: %v", ctx.Err())
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close: %v", ctx.Err())
	}
}

func TestDialAndCallReducerRequiresExplicitTokenBeforeNetwork(t *testing.T) {
	called := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(http.ResponseWriter, *http.Request) {
		called <- struct{}{}
	})

	_, _, err := DialAndCallReducer(context.Background(), Options{
		URL:   srv.wsURL(),
		Token: " \t",
	}, ReducerCallRequest{
		Name: "send_message",
	})
	if !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("DialAndCallReducer error = %v, want ErrTokenRequired", err)
	}
	select {
	case <-called:
		t.Fatal("server was called despite missing token")
	default:
	}
}

func TestDialAndCallReducerClosesAfterReducerError(t *testing.T) {
	wantIdentity := protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}}
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)

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
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusFailed{Error: "boom"},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err = ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, update, err := DialAndCallReducer(ctx, Options{URL: srv.wsURL(), Token: "operator-token"}, ReducerCallRequest{
		Name: "send_message",
	})
	if !errors.Is(err, ErrReducerFailed) {
		t.Fatalf("DialAndCallReducer error = %v, want ErrReducerFailed", err)
	}
	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
	if failed, ok := update.Status.(protocol.StatusFailed); !ok || failed.Error != "boom" {
		t.Fatalf("update status = %+v, want failed boom", update.Status)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close after reducer error: %v", ctx.Err())
	}
}

func TestClientDeclaredQueryWaitsForMatchingResponse(t *testing.T) {
	received := make(chan protocol.DeclaredQueryMsg, 1)
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
		query, ok := msg.(protocol.DeclaredQueryMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryMsg", msg)
			return
		}
		received <- query
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: query.MessageID})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	response, err := client.DeclaredQuery(ctx, "recent_messages")
	if err != nil {
		t.Fatalf("DeclaredQuery returned error: %v", err)
	}
	if !bytes.Equal(response.MessageID, []byte{1, 0, 0, 0}) {
		t.Fatalf("response message ID = %x", response.MessageID)
	}

	select {
	case query := <-received:
		if query.Name != "recent_messages" || !bytes.Equal(query.MessageID, []byte{1, 0, 0, 0}) {
			t.Fatalf("server query = %+v", query)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive query: %v", ctx.Err())
	}
}

func TestClientDeclaredQueryRejectsMismatchedResponse(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read client message: %v", err)
			return
		}
		if _, _, err := protocol.DecodeClientMessage(frame); err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: []byte{99}})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.DeclaredQuery(ctx, "recent_messages")
	if !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("DeclaredQuery mismatch error = %v, want ErrResponseMismatch", err)
	}
}

func TestClientDeclaredQueryWithParametersWaitsForMatchingResponse(t *testing.T) {
	received := make(chan protocol.DeclaredQueryWithParametersMsg, 1)
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
		query, ok := msg.(protocol.DeclaredQueryWithParametersMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryWithParametersMsg", msg)
			return
		}
		received <- query
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: query.MessageID})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	response, err := client.DeclaredQueryWithParameters(ctx, "recent_messages", []byte{9, 8, 7})
	if err != nil {
		t.Fatalf("DeclaredQueryWithParameters returned error: %v", err)
	}
	if !bytes.Equal(response.MessageID, []byte{1, 0, 0, 0}) {
		t.Fatalf("response message ID = %x", response.MessageID)
	}

	select {
	case query := <-received:
		if query.Name != "recent_messages" || !bytes.Equal(query.MessageID, []byte{1, 0, 0, 0}) || !bytes.Equal(query.Params, []byte{9, 8, 7}) {
			t.Fatalf("server query = %+v", query)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive query: %v", ctx.Err())
	}
}

func TestClientDeclaredQueryWithParametersSurfacesQueryError(t *testing.T) {
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
		query := msg.(protocol.DeclaredQueryWithParametersMsg)
		queryErr := "bad query"
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: query.MessageID, Error: &queryErr})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.DeclaredQueryWithParameters(ctx, "recent_messages", nil)
	if !errors.Is(err, ErrDeclaredQueryFailed) {
		t.Fatalf("DeclaredQueryWithParameters error = %v, want ErrDeclaredQueryFailed", err)
	}
}

func TestClientDeclaredQueryWithParametersRequiresV2Subprotocol(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConnWithSubprotocols(t, w, r, []string{protocol.SubprotocolV1})
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.DeclaredQueryWithParameters(ctx, "recent_messages", nil)
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("DeclaredQueryWithParameters v1 error = %v, want ErrProtocolVersion", err)
	}
}

func TestClientExecuteDeclaredQueryChoosesNoParameterRequest(t *testing.T) {
	received := make(chan protocol.DeclaredQueryMsg, 1)
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
		query, ok := msg.(protocol.DeclaredQueryMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryMsg", msg)
			return
		}
		received <- query
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: query.MessageID})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	response, err := client.ExecuteDeclaredQuery(ctx, DeclaredQueryRequest{Name: "recent_messages"})
	if err != nil {
		t.Fatalf("ExecuteDeclaredQuery returned error: %v", err)
	}
	if !bytes.Equal(response.MessageID, []byte{1, 0, 0, 0}) {
		t.Fatalf("response message ID = %x", response.MessageID)
	}

	select {
	case query := <-received:
		if query.Name != "recent_messages" || !bytes.Equal(query.MessageID, []byte{1, 0, 0, 0}) {
			t.Fatalf("server query = %+v", query)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive query: %v", ctx.Err())
	}
}

func TestClientExecuteDeclaredQueryChoosesParameterizedRequest(t *testing.T) {
	received := make(chan protocol.DeclaredQueryWithParametersMsg, 1)
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
		query, ok := msg.(protocol.DeclaredQueryWithParametersMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryWithParametersMsg", msg)
			return
		}
		received <- query
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: query.MessageID})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	response, err := client.ExecuteDeclaredQuery(ctx, DeclaredQueryRequest{
		Name:          "recent_messages",
		HasParameters: true,
	})
	if err != nil {
		t.Fatalf("ExecuteDeclaredQuery returned error: %v", err)
	}
	if !bytes.Equal(response.MessageID, []byte{1, 0, 0, 0}) {
		t.Fatalf("response message ID = %x", response.MessageID)
	}

	select {
	case query := <-received:
		if query.Name != "recent_messages" || !bytes.Equal(query.MessageID, []byte{1, 0, 0, 0}) || len(query.Params) != 0 {
			t.Fatalf("server query = %+v", query)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive query: %v", ctx.Err())
	}
}

func TestClientExecuteDeclaredQueryWithParametersRequiresV2Subprotocol(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConnWithSubprotocols(t, w, r, []string{protocol.SubprotocolV1})
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		ctx, cancel := context.WithTimeout(r.Context(), 50*time.Millisecond)
		defer cancel()
		if _, _, err := ws.Read(ctx); err == nil {
			received <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.ExecuteDeclaredQuery(ctx, DeclaredQueryRequest{
		Name:          "recent_messages",
		HasParameters: true,
	})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("ExecuteDeclaredQuery v1 error = %v, want ErrProtocolVersion", err)
	}
	select {
	case <-received:
		t.Fatal("server received parameterized query despite v1 subprotocol")
	default:
	}
}

func TestClientExecuteDeclaredQueryReusesResponseValidation(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read client message: %v", err)
			return
		}
		if _, _, err := protocol.DecodeClientMessage(frame); err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: []byte{99}})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer client.Close(context.Background())

	_, err = client.ExecuteDeclaredQuery(ctx, DeclaredQueryRequest{
		Name:          "recent_messages",
		Parameters:    []byte{1, 2, 3},
		HasParameters: true,
	})
	if !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("ExecuteDeclaredQuery mismatch error = %v, want ErrResponseMismatch", err)
	}
}

func TestDialAndExecuteDeclaredQueryUsesExplicitTokenAndCloses(t *testing.T) {
	const token = "operator-token"
	wantIdentity := protocol.IdentityToken{Identity: [32]byte{1}, Token: "server-token", ConnectionID: [16]byte{2}}
	received := make(chan protocol.DeclaredQueryWithParametersMsg, 1)
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)

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
		query, ok := msg.(protocol.DeclaredQueryWithParametersMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryWithParametersMsg", msg)
			return
		}
		received <- query
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: query.MessageID})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err = ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, response, err := DialAndExecuteDeclaredQuery(ctx, Options{URL: srv.wsURL(), Token: token}, DeclaredQueryRequest{
		Name:          "recent_messages",
		Parameters:    []byte{9, 8, 7},
		HasParameters: true,
	})
	if err != nil {
		t.Fatalf("DialAndExecuteDeclaredQuery returned error: %v", err)
	}
	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
	if !bytes.Equal(response.MessageID, []byte{1, 0, 0, 0}) {
		t.Fatalf("response message ID = %x", response.MessageID)
	}

	select {
	case query := <-received:
		if query.Name != "recent_messages" || !bytes.Equal(query.Params, []byte{9, 8, 7}) {
			t.Fatalf("server query = %+v", query)
		}
	case <-ctx.Done():
		t.Fatalf("server did not receive query: %v", ctx.Err())
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close: %v", ctx.Err())
	}
}

func TestDialAndExecuteDeclaredQueryRequiresExplicitTokenBeforeNetwork(t *testing.T) {
	called := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(http.ResponseWriter, *http.Request) {
		called <- struct{}{}
	})

	_, _, err := DialAndExecuteDeclaredQuery(context.Background(), Options{
		URL:   srv.wsURL(),
		Token: " \t",
	}, DeclaredQueryRequest{
		Name:          "recent_messages",
		HasParameters: true,
	})
	if !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("DialAndExecuteDeclaredQuery error = %v, want ErrTokenRequired", err)
	}
	select {
	case <-called:
		t.Fatal("server was called despite missing token")
	default:
	}
}

func TestDialAndExecuteDeclaredQueryWithParametersRequiresV2BeforeRequest(t *testing.T) {
	readDone := make(chan struct{}, 1)
	received := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConnWithSubprotocols(t, w, r, []string{protocol.SubprotocolV1})
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if _, _, err := ws.Read(readCtx); err == nil {
			received <- struct{}{}
		}
		readDone <- struct{}{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := DialAndExecuteDeclaredQuery(ctx, Options{
		URL:   srv.wsURL(),
		Token: "operator-token",
	}, DeclaredQueryRequest{
		Name:          "recent_messages",
		Parameters:    []byte{1, 2, 3},
		HasParameters: true,
	})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("DialAndExecuteDeclaredQuery v1 error = %v, want ErrProtocolVersion", err)
	}

	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatalf("server did not finish request read: %v", ctx.Err())
	}
	select {
	case <-received:
		t.Fatal("server received parameterized query despite v1 subprotocol")
	default:
	}
}

func TestDialAndExecuteDeclaredQueryRejectsMissingSubprotocolBeforeRequest(t *testing.T) {
	readDone := make(chan struct{}, 1)
	received := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConnWithSubprotocols(t, w, r, nil)
		defer ws.CloseNow()

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if _, _, err := ws.Read(readCtx); err == nil {
			received <- struct{}{}
		}
		readDone <- struct{}{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := DialAndExecuteDeclaredQuery(ctx, Options{
		URL:   srv.wsURL(),
		Token: "operator-token",
	}, DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("DialAndExecuteDeclaredQuery missing subprotocol error = %v, want ErrProtocolVersion", err)
	}

	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatalf("server did not finish request read: %v", ctx.Err())
	}
	select {
	case <-received:
		t.Fatal("server received declared query despite missing subprotocol")
	default:
	}
}

func TestDialAndExecuteDeclaredQueryClosesAfterQueryError(t *testing.T) {
	wantIdentity := protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}}
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)

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
		query, ok := msg.(protocol.DeclaredQueryMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryMsg", msg)
			return
		}
		queryErr := "bad query"
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{
			MessageID: query.MessageID,
			Error:     &queryErr,
		})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err = ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, response, err := DialAndExecuteDeclaredQuery(ctx, Options{URL: srv.wsURL(), Token: "operator-token"}, DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if !errors.Is(err, ErrDeclaredQueryFailed) {
		t.Fatalf("DialAndExecuteDeclaredQuery error = %v, want ErrDeclaredQueryFailed", err)
	}
	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
	if response.Error == nil || *response.Error != "bad query" {
		t.Fatalf("response error = %v, want bad query", response.Error)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close after query error: %v", ctx.Err())
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
	return acceptProtocolClientTestConnWithSubprotocols(t, w, r, protocol.SupportedSubprotocols())
}

func acceptProtocolClientTestConnWithSubprotocols(t *testing.T, w http.ResponseWriter, r *http.Request, subprotocols []string) *websocket.Conn {
	t.Helper()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: subprotocols,
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
