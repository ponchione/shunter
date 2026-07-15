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
	defer closeProtocolClientTestClient(t, client)

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

func TestClientNextRequestIDSkipsZeroAfterWrap(t *testing.T) {
	client := &Client{}
	client.nextID.Store(^uint32(0) - 1)

	if got := client.NextRequestID(); got != ^uint32(0) {
		t.Fatalf("request ID before wrap = %d, want %d", got, uint32(^uint32(0)))
	}
	if got := client.NextRequestID(); got != 1 {
		t.Fatalf("request ID after wrap = %d, want 1", got)
	}
}

func TestClientNextRequestIDSkipsAbandonedResponseIDs(t *testing.T) {
	client := &Client{}
	abandoned := responseIdentity{tag: protocol.TagProcedureResponse, requestID: 1}
	if !client.reserveAbandonedResponseLocked(abandoned) {
		t.Fatal("reserveAbandonedResponseLocked rejected the first reservation")
	}
	client.nextID.Store(^uint32(0))

	if got := client.NextRequestID(); got != 2 {
		t.Fatalf("request ID with 1 reserved = %d, want 2", got)
	}

	client.pendingMu.Lock()
	if cleared := client.clearAbandonedResponseLocked(abandoned); !cleared {
		t.Fatal("abandoned response was not cleared")
	}
	client.pendingMu.Unlock()
	client.nextID.Store(^uint32(0))
	if got := client.NextRequestID(); got != 1 {
		t.Fatalf("request ID after late response = %d, want 1", got)
	}
}

func TestClientBoundsAbandonedResponseRegistry(t *testing.T) {
	client := &Client{maxPendingMessages: 1}
	if !client.reserveAbandonedResponseLocked(responseIdentity{tag: protocol.TagProcedureResponse, requestID: 1}) {
		t.Fatal("first abandoned response reservation was rejected")
	}
	if client.reserveAbandonedResponseLocked(responseIdentity{tag: protocol.TagProcedureResponse, requestID: 2}) {
		t.Fatal("second abandoned response reservation exceeded the configured bound")
	}
	if got := len(client.abandonedResponses); got != 1 {
		t.Fatalf("abandoned response count = %d, want 1", got)
	}
}

func TestClientServerMessageReadLimitBoundaries(t *testing.T) {
	for _, path := range []string{"typed response", "asynchronous read"} {
		for _, tc := range []struct {
			name      string
			limit     int64
			frameSize int
			wantLarge bool
		}{
			{name: "default accepts above 32 KiB", frameSize: 32769},
			{name: "exact configured limit", limit: 4096, frameSize: 4096},
			{name: "one byte over configured limit", limit: 4096, frameSize: 4097, wantLarge: true},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
					ws := acceptProtocolClientTestConn(t, w, r)
					defer ws.CloseNow()
					writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

					var frame []byte
					if path == "typed response" {
						msg, ok := readProtocolClientMessage(t, r, ws)
						if !ok {
							return
						}
						query, ok := msg.(protocol.DeclaredQueryMsg)
						if !ok {
							t.Errorf("client message = %T, want DeclaredQueryMsg", msg)
							return
						}
						frame = sizedOneOffResponseFrame(t, tc.frameSize, query.MessageID)
					} else {
						frame = sizedLightUpdateFrame(t, tc.frameSize)
					}
					writeProtocolClientServerFrame(t, ws, frame)
					<-r.Context().Done()
				})

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				client, _, err := Dial(ctx, Options{
					URL:                   srv.wsURL(),
					Token:                 "operator-token",
					MaxServerMessageBytes: tc.limit,
				})
				if err != nil {
					t.Fatalf("Dial: %v", err)
				}
				defer closeProtocolClientTestClient(t, client)

				if path == "typed response" {
					_, err = client.DeclaredQuery(ctx, "large_result")
				} else {
					_, _, err = client.Read(ctx)
				}
				if tc.wantLarge {
					if !errors.Is(err, websocket.ErrMessageTooBig) {
						t.Fatalf("read error = %v, want websocket.ErrMessageTooBig", err)
					}
				} else if err != nil {
					t.Fatalf("read error = %v, want nil", err)
				}
			})
		}
	}
}

func TestDialAppliesServerMessageLimitBeforeIdentityRead(t *testing.T) {
	const limit = 256
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerFrame(t, ws, sizedIdentityFrame(t, limit+1))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Dial(ctx, Options{
		URL:                   srv.wsURL(),
		Token:                 "operator-token",
		MaxServerMessageBytes: limit,
	})
	if !errors.Is(err, websocket.ErrMessageTooBig) {
		t.Fatalf("Dial error = %v, want websocket.ErrMessageTooBig", err)
	}
}

func TestClientLateTypedResponsesDoNotPoisonNextOperation(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(context.Context, *Client) (any, error)
	}{
		{
			name: "reducer",
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.CallReducer(ctx, "send_message", nil)
			},
		},
		{
			name: "declared query",
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.DeclaredQuery(ctx, "recent_messages")
			},
		},
		{
			name: "parameterized declared query",
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.DeclaredQueryWithParameters(ctx, "recent_messages", []byte{1})
			},
		},
		{
			name: "SQL query",
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.SQLQuery(ctx, "SELECT * FROM messages")
			},
		},
		{
			name: "procedure",
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.CallProcedure(ctx, "refresh", nil)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firstReceived := make(chan struct{})
			srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				ws := acceptProtocolClientTestConn(t, w, r)
				defer ws.CloseNow()
				writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

				first, ok := readProtocolClientMessage(t, r, ws)
				if !ok {
					return
				}
				close(firstReceived)
				second, ok := readProtocolClientMessage(t, r, ws)
				if !ok {
					return
				}
				writeProtocolClientServerMessage(t, ws, responseForTypedRequest(t, first, "first"))
				writeProtocolClientServerMessage(t, ws, responseForTypedRequest(t, second, "second"))
				<-r.Context().Done()
			})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer closeProtocolClientTestClient(t, client)

			firstCtx, cancelFirst := context.WithCancel(ctx)
			firstDone := make(chan error, 1)
			go func() {
				_, err := tc.call(firstCtx, client)
				firstDone <- err
			}()
			select {
			case <-firstReceived:
				cancelFirst()
			case <-ctx.Done():
				t.Fatalf("server did not receive first request: %v", ctx.Err())
			}
			if err := <-firstDone; !errors.Is(err, context.Canceled) {
				t.Fatalf("first call error = %v, want context.Canceled", err)
			}

			result, err := tc.call(ctx, client)
			if err != nil {
				t.Fatalf("second call: %v", err)
			}
			assertSecondTypedResponse(t, result)

			tag, late, err := client.Read(ctx)
			if err != nil {
				t.Fatalf("Read late response: %v", err)
			}
			assertFirstTypedResponse(t, tag, late)
		})
	}
}

func TestClientConcurrentProcedureAndReducerPreserveInterleavedLightUpdate(t *testing.T) {
	procedureReceived := make(chan struct{})
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
			return
		}
		procedure, ok := msg.(protocol.CallProcedureMsg)
		if !ok {
			t.Errorf("first client message = %T, want CallProcedureMsg", msg)
			return
		}
		close(procedureReceived)
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdateLight{
			RequestID: 0,
			Update:    []protocol.SubscriptionUpdate{{QueryID: 77, TableName: "messages"}},
		})
		writeProtocolClientServerMessage(t, ws, protocol.ProcedureResponse{
			MessageID: procedure.MessageID,
			Result:    []byte("procedure-ok"),
		})

		msg, ok = readProtocolClientMessage(t, r, ws)
		if !ok {
			return
		}
		reducer, ok := msg.(protocol.CallReducerMsg)
		if !ok {
			t.Errorf("second client message = %T, want CallReducerMsg", msg)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status: protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{
				ReducerName: reducer.ReducerName,
				RequestID:   reducer.RequestID,
			},
		})
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer closeProtocolClientTestClient(t, client)

	procedureResult := make(chan protocol.ProcedureResponse, 1)
	procedureErr := make(chan error, 1)
	go func() {
		response, err := client.CallProcedure(ctx, "refresh", nil)
		procedureResult <- response
		procedureErr <- err
	}()
	select {
	case <-procedureReceived:
	case <-ctx.Done():
		t.Fatalf("server did not receive procedure: %v", ctx.Err())
	}
	reducerResult := make(chan protocol.TransactionUpdate, 1)
	reducerErr := make(chan error, 1)
	go func() {
		response, err := client.CallReducer(ctx, "send", nil)
		reducerResult <- response
		reducerErr <- err
	}()

	if err := <-procedureErr; err != nil {
		t.Fatalf("CallProcedure: %v", err)
	}
	if got := <-procedureResult; string(got.Result) != "procedure-ok" {
		t.Fatalf("procedure result = %q, want procedure-ok", got.Result)
	}
	if err := <-reducerErr; err != nil {
		t.Fatalf("CallReducer: %v", err)
	}
	if got := <-reducerResult; got.ReducerCall.ReducerName != "send" {
		t.Fatalf("reducer response = %+v, want send", got.ReducerCall)
	}
	tag, msg, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("Read preserved light update: %v", err)
	}
	light, ok := msg.(protocol.TransactionUpdateLight)
	if tag != protocol.TagTransactionUpdateLight || !ok || len(light.Update) != 1 || light.Update[0].QueryID != 77 {
		t.Fatalf("preserved message = tag %d %T %+v, want light query 77", tag, msg, msg)
	}
}

func TestClientConcurrentReadCannotStealTypedReducerResponse(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
			return
		}
		call, ok := msg.(protocol.CallReducerMsg)
		if !ok {
			t.Errorf("client message = %T, want CallReducerMsg", msg)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status: protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{
				ReducerName: call.ReducerName,
				RequestID:   call.RequestID,
			},
		})
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdateLight{
			Update: []protocol.SubscriptionUpdate{{QueryID: 91, TableName: "messages"}},
		})
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer closeProtocolClientTestClient(t, client)

	type readResult struct {
		tag uint8
		msg any
		err error
	}
	readDone := make(chan readResult, 1)
	go func() {
		tag, msg, err := client.Read(ctx)
		readDone <- readResult{tag: tag, msg: msg, err: err}
	}()
	select {
	case result := <-readDone:
		t.Fatalf("Read returned before a server message: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}

	type reducerResult struct {
		update protocol.TransactionUpdate
		err    error
	}
	reducerDone := make(chan reducerResult, 1)
	go func() {
		update, err := client.CallReducer(ctx, "send", nil)
		reducerDone <- reducerResult{update: update, err: err}
	}()

	select {
	case result := <-reducerDone:
		if result.err != nil {
			t.Fatalf("CallReducer: %v", result.err)
		}
		if result.update.ReducerCall.ReducerName != "send" {
			t.Fatalf("CallReducer response = %+v, want send", result.update.ReducerCall)
		}
	case <-ctx.Done():
		t.Fatalf("CallReducer timed out: %v", ctx.Err())
	}

	select {
	case result := <-readDone:
		if result.err != nil {
			t.Fatalf("Read: %v", result.err)
		}
		light, ok := result.msg.(protocol.TransactionUpdateLight)
		if result.tag != protocol.TagTransactionUpdateLight || !ok || len(light.Update) != 1 || light.Update[0].QueryID != 91 {
			t.Fatalf("Read result = tag %d %T %+v, want light update 91", result.tag, result.msg, result.msg)
		}
	case <-ctx.Done():
		t.Fatalf("Read timed out: %v", ctx.Err())
	}
}

func TestClientBoundsPendingAsynchronousMessages(t *testing.T) {
	for _, tc := range []struct {
		name               string
		maxPendingMessages int
		maxPendingBytes    int64
		messageCount       int
	}{
		{name: "message count", maxPendingMessages: 3, messageCount: 4},
		{name: "decoded bytes", maxPendingMessages: 10, maxPendingBytes: 1, messageCount: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				ws := acceptProtocolClientTestConn(t, w, r)
				defer ws.CloseNow()
				writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
				if _, ok := readProtocolClientMessage(t, r, ws); !ok {
					return
				}
				for i := 0; i < tc.messageCount; i++ {
					writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdateLight{
						Update: []protocol.SubscriptionUpdate{{QueryID: uint32(i + 1), TableName: "messages"}},
					})
				}
				<-r.Context().Done()
			})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			client, _, err := Dial(ctx, Options{
				URL:                srv.wsURL(),
				Token:              "operator-token",
				MaxPendingMessages: tc.maxPendingMessages,
				MaxPendingBytes:    tc.maxPendingBytes,
			})
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}

			_, err = client.CallReducer(ctx, "send", nil)
			if !errors.Is(err, ErrPendingMessageLimit) {
				t.Fatalf("CallReducer error = %v, want ErrPendingMessageLimit", err)
			}
		})
	}
}

func TestClientPopPendingClearsConsumedPayloadSlot(t *testing.T) {
	payload := &struct{ value string }{value: "retained"}
	backing := []queuedServerMessage{{tag: protocol.TagTransactionUpdateLight, msg: payload, size: 17}}
	client := &Client{pending: backing, pendingBytes: 17}

	got := client.popPendingLocked()
	if got.msg != payload {
		t.Fatalf("popped payload = %v, want original payload", got.msg)
	}
	if backing[0].msg != nil {
		t.Fatalf("consumed backing slot retained payload: %+v", backing[0])
	}
	if client.pending != nil {
		t.Fatalf("pending = %#v, want nil after final dequeue", client.pending)
	}
	if client.pendingBytes != 0 {
		t.Fatalf("pendingBytes = %d, want 0", client.pendingBytes)
	}
}

func TestClientCloseDeadlineBoundsNonCooperatingPeer(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		<-r.Context().Done()
	})
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDial()
	client, _, err := Dial(dialCtx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelClose()
	started := time.Now()
	err = client.Close(closeCtx)
	elapsed := time.Since(started)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Close error = %v, want ErrTimeout", err)
	}
	if elapsed > 750*time.Millisecond {
		t.Fatalf("Close elapsed = %v, want caller deadline bound", elapsed)
	}
}

func TestDialAndHelpersBoundNonCooperatingCloseByCallerContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(context.Context, Options) error
	}{
		{
			name: "reducer",
			run: func(ctx context.Context, opts Options) error {
				_, _, err := DialAndCallReducer(ctx, opts, ReducerCallRequest{Name: "send_message"})
				return err
			},
		},
		{
			name: "declared query",
			run: func(ctx context.Context, opts Options) error {
				_, _, err := DialAndExecuteDeclaredQuery(ctx, opts, DeclaredQueryRequest{Name: "recent_messages"})
				return err
			},
		},
		{
			name: "sql query",
			run: func(ctx context.Context, opts Options) error {
				_, _, err := DialAndExecuteSQLQuery(ctx, opts, SQLQueryRequest{QueryString: "SELECT * FROM messages"})
				return err
			},
		},
		{
			name: "procedure",
			run: func(ctx context.Context, opts Options) error {
				_, _, err := DialAndCallProcedure(ctx, opts, ProcedureCallRequest{Name: "send_system_message"})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []struct {
				name              string
				timeout           time.Duration
				cancelBeforeClose bool
				wantErr           error
			}{
				{name: "deadline", timeout: 100 * time.Millisecond, wantErr: ErrTimeout},
				{name: "already canceled after operation", timeout: 2 * time.Second, cancelBeforeClose: true, wantErr: context.Canceled},
			} {
				t.Run(mode.name, func(t *testing.T) {
					srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
						ws := acceptProtocolClientTestConn(t, w, r)
						defer ws.CloseNow()
						writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
						msg, ok := readProtocolClientMessage(t, r, ws)
						if !ok {
							return
						}
						switch request := msg.(type) {
						case protocol.CallReducerMsg:
							writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
								Status:      protocol.StatusCommitted{},
								ReducerCall: protocol.ReducerCallInfo{ReducerName: request.ReducerName, RequestID: request.RequestID},
							})
						case protocol.DeclaredQueryMsg:
							writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: request.MessageID})
						case protocol.OneOffQueryMsg:
							writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: request.MessageID})
						case protocol.CallProcedureMsg:
							writeProtocolClientServerMessage(t, ws, protocol.ProcedureResponse{MessageID: request.MessageID})
						default:
							t.Errorf("client message = %T, want one-off request", msg)
							return
						}
						<-r.Context().Done()
					})
					ctx, cancel := context.WithTimeout(context.Background(), mode.timeout)
					defer cancel()
					if mode.cancelBeforeClose {
						previous := dialAndBeforeCloseHook
						dialAndBeforeCloseHook = cancel
						t.Cleanup(func() { dialAndBeforeCloseHook = previous })
					}
					started := time.Now()
					err := tc.run(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
					elapsed := time.Since(started)
					if !errors.Is(err, mode.wantErr) {
						t.Fatalf("DialAnd helper error = %v, want %v", err, mode.wantErr)
					}
					if elapsed > 750*time.Millisecond {
						t.Fatalf("DialAnd helper elapsed = %v, want caller-context close bound", elapsed)
					}
				})
			}
		})
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

func TestDialAllowsExplicitAnonymousConnection(t *testing.T) {
	wantIdentity := protocol.IdentityToken{
		Identity:     [32]byte{1},
		Token:        "minted-token",
		ConnectionID: [16]byte{2},
	}
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, identity, err := Dial(ctx, Options{URL: srv.wsURL(), AllowAnonymous: true})
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer closeProtocolClientTestClient(t, client)
	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
}

func TestDialAndCallReducerAllowsNilContext(t *testing.T) {
	wantIdentity := protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}}
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, wantIdentity)

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
			return
		}
		call, ok := msg.(protocol.CallReducerMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.CallReducerMsg", msg)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})

		readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = ws.Read(readCtx)
	})

	var ctx context.Context
	identity, update, err := DialAndCallReducer(ctx, Options{
		URL:   srv.wsURL(),
		Token: "operator-token",
	}, ReducerCallRequest{Name: "send_message"})
	if err != nil {
		t.Fatalf("DialAndCallReducer returned error: %v", err)
	}
	if identity != wantIdentity {
		t.Fatalf("identity = %+v, want %+v", identity, wantIdentity)
	}
	if update.ReducerCall.ReducerName != "send_message" || update.ReducerCall.RequestID != 1 {
		t.Fatalf("DialAndCallReducer update = %+v", update)
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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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
		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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
		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
		_, _, err := ws.Read(readCtx)
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

func TestDialAndCallReducerRequiresExplicitURLBeforeNetwork(t *testing.T) {
	_, _, err := DialAndCallReducer(context.Background(), Options{
		URL:   " \t",
		Token: "operator-token",
	}, ReducerCallRequest{
		Name: "send_message",
	})
	if !errors.Is(err, ErrURLRequired) {
		t.Fatalf("DialAndCallReducer error = %v, want ErrURLRequired", err)
	}
}

func TestDialAndCallReducerRejectsMissingSubprotocolBeforeRequest(t *testing.T) {
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
	_, _, err := DialAndCallReducer(ctx, Options{
		URL:   srv.wsURL(),
		Token: "operator-token",
	}, ReducerCallRequest{
		Name: "send_message",
	})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("DialAndCallReducer missing subprotocol error = %v, want ErrProtocolVersion", err)
	}

	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatalf("server did not finish request read: %v", ctx.Err())
	}
	select {
	case <-received:
		t.Fatal("server received reducer call despite missing subprotocol")
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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
		_, _, err := ws.Read(readCtx)
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

func TestDialAndCallReducerClosesAfterMismatchedResponse(t *testing.T) {
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
			return
		}
		call, ok := msg.(protocol.CallReducerMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.CallReducerMsg", msg)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: "other_reducer", RequestID: call.RequestID},
		})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err := ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := DialAndCallReducer(ctx, Options{URL: srv.wsURL(), Token: "operator-token"}, ReducerCallRequest{
		Name: "send_message",
	})
	if !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("DialAndCallReducer error = %v, want ErrResponseMismatch", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close after response mismatch: %v", ctx.Err())
	}
}

func TestDialAndCallReducerClosesAfterUnexpectedResponse(t *testing.T) {
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		if _, ok := readProtocolClientMessage(t, r, ws); !ok {
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: []byte{1}})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err := ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := DialAndCallReducer(ctx, Options{URL: srv.wsURL(), Token: "operator-token"}, ReducerCallRequest{
		Name: "send_message",
	})
	if !errors.Is(err, ErrUnexpectedMessage) {
		t.Fatalf("DialAndCallReducer error = %v, want ErrUnexpectedMessage", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close after unexpected response: %v", ctx.Err())
	}
}

func TestClientDeclaredQueryWaitsForMatchingResponse(t *testing.T) {
	received := make(chan protocol.DeclaredQueryMsg, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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
		if _, ok := readProtocolClientMessage(t, r, ws); !ok {
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
	defer closeProtocolClientTestClient(t, client)

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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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
		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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
	defer closeProtocolClientTestClient(t, client)

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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
	defer closeProtocolClientTestClient(t, client)

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
	defer closeProtocolClientTestClient(t, client)

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
		if _, ok := readProtocolClientMessage(t, r, ws); !ok {
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
	defer closeProtocolClientTestClient(t, client)

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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
		_, _, err := ws.Read(readCtx)
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

func TestDialAndExecuteDeclaredQueryRequiresExplicitURLBeforeNetwork(t *testing.T) {
	_, _, err := DialAndExecuteDeclaredQuery(context.Background(), Options{
		URL:   " \t",
		Token: "operator-token",
	}, DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if !errors.Is(err, ErrURLRequired) {
		t.Fatalf("DialAndExecuteDeclaredQuery error = %v, want ErrURLRequired", err)
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

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
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
		_, _, err := ws.Read(readCtx)
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

func TestDialAndExecuteDeclaredQueryClosesAfterMismatchedResponse(t *testing.T) {
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		if _, ok := readProtocolClientMessage(t, r, ws); !ok {
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.OneOffQueryResponse{MessageID: []byte{99}})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err := ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := DialAndExecuteDeclaredQuery(ctx, Options{URL: srv.wsURL(), Token: "operator-token"}, DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("DialAndExecuteDeclaredQuery error = %v, want ErrResponseMismatch", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close after response mismatch: %v", ctx.Err())
	}
}

func TestDialAndExecuteDeclaredQueryClosesAfterUnexpectedResponse(t *testing.T) {
	closed := make(chan struct{}, 1)
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		writeProtocolClientServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})

		msg, ok := readProtocolClientMessage(t, r, ws)
		if !ok {
			return
		}
		if _, ok := msg.(protocol.DeclaredQueryMsg); !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryMsg", msg)
			return
		}
		writeProtocolClientServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: "send_message", RequestID: 99},
		})

		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, _, err := ws.Read(readCtx)
		if err != nil {
			closed <- struct{}{}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := DialAndExecuteDeclaredQuery(ctx, Options{URL: srv.wsURL(), Token: "operator-token"}, DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if !errors.Is(err, ErrUnexpectedMessage) {
		t.Fatalf("DialAndExecuteDeclaredQuery error = %v, want ErrUnexpectedMessage", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatalf("server did not observe client close after unexpected response: %v", ctx.Err())
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

func TestDialClassifiesMalformedIdentityFrameAsUnexpectedMessage(t *testing.T) {
	srv := protocolClientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		ws := acceptProtocolClientTestConn(t, w, r)
		defer ws.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := ws.Write(ctx, websocket.MessageBinary, []byte{protocol.TagIdentityToken}); err != nil {
			t.Fatalf("server write malformed identity: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Dial(ctx, Options{URL: srv.wsURL(), Token: "operator-token"})
	if !errors.Is(err, ErrUnexpectedMessage) {
		t.Fatalf("Dial malformed frame error = %v, want ErrUnexpectedMessage", err)
	}
	if !errors.Is(err, protocol.ErrMalformedMessage) {
		t.Fatalf("Dial malformed frame error = %v, want protocol.ErrMalformedMessage", err)
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

func closeProtocolClientTestClient(t *testing.T, client *Client) {
	t.Helper()
	if client == nil || client.conn == nil {
		return
	}
	if err := client.conn.CloseNow(); err != nil {
		t.Errorf("CloseNow test client: %v", err)
	}
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

func writeProtocolClientServerFrame(t *testing.T, ws *websocket.Conn, frame []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("server write %d-byte frame: %v", len(frame), err)
	}
}

func sizedOneOffResponseFrame(t *testing.T, size int, messageID []byte) []byte {
	t.Helper()
	msg := protocol.OneOffQueryResponse{
		MessageID: append([]byte(nil), messageID...),
		Tables:    []protocol.OneOffTable{{TableName: "large_result"}},
	}
	base, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("encode base one-off response: %v", err)
	}
	if size < len(base) {
		t.Fatalf("one-off frame size %d is smaller than base encoding %d", size, len(base))
	}
	msg.Tables[0].Rows = make([]byte, size-len(base))
	frame, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("encode sized one-off response: %v", err)
	}
	if len(frame) != size {
		t.Fatalf("one-off frame size = %d, want %d", len(frame), size)
	}
	return frame
}

func sizedLightUpdateFrame(t *testing.T, size int) []byte {
	t.Helper()
	msg := protocol.TransactionUpdateLight{
		Update: []protocol.SubscriptionUpdate{{QueryID: 1, TableName: "large_result"}},
	}
	base, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("encode base light update: %v", err)
	}
	if size < len(base) {
		t.Fatalf("light-update frame size %d is smaller than base encoding %d", size, len(base))
	}
	msg.Update[0].Inserts = make([]byte, size-len(base))
	frame, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("encode sized light update: %v", err)
	}
	if len(frame) != size {
		t.Fatalf("light-update frame size = %d, want %d", len(frame), size)
	}
	return frame
}

func sizedIdentityFrame(t *testing.T, size int) []byte {
	t.Helper()
	msg := protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}}
	base, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("encode base identity: %v", err)
	}
	if size < len(base) {
		t.Fatalf("identity frame size %d is smaller than base encoding %d", size, len(base))
	}
	msg.Token = strings.Repeat("x", size-len(base))
	frame, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("encode sized identity: %v", err)
	}
	if len(frame) != size {
		t.Fatalf("identity frame size = %d, want %d", len(frame), size)
	}
	return frame
}

func responseForTypedRequest(t *testing.T, request any, marker string) any {
	t.Helper()
	switch msg := request.(type) {
	case protocol.CallReducerMsg:
		return protocol.TransactionUpdate{
			Status: protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{
				ReducerName: msg.ReducerName,
				RequestID:   msg.RequestID,
			},
		}
	case protocol.DeclaredQueryMsg:
		return markedOneOffResponse(msg.MessageID, marker)
	case protocol.DeclaredQueryWithParametersMsg:
		return markedOneOffResponse(msg.MessageID, marker)
	case protocol.OneOffQueryMsg:
		return markedOneOffResponse(msg.MessageID, marker)
	case protocol.CallProcedureMsg:
		return protocol.ProcedureResponse{MessageID: append([]byte(nil), msg.MessageID...), Result: []byte(marker)}
	default:
		t.Fatalf("typed request = %T, want a supported request", request)
		return nil
	}
}

func markedOneOffResponse(messageID []byte, marker string) protocol.OneOffQueryResponse {
	return protocol.OneOffQueryResponse{
		MessageID: append([]byte(nil), messageID...),
		Tables: []protocol.OneOffTable{{
			TableName: "result",
			Rows:      []byte(marker),
		}},
	}
}

func assertSecondTypedResponse(t *testing.T, result any) {
	t.Helper()
	switch msg := result.(type) {
	case protocol.TransactionUpdate:
		if msg.ReducerCall.RequestID != 2 {
			t.Fatalf("second reducer request ID = %d, want 2", msg.ReducerCall.RequestID)
		}
	case protocol.OneOffQueryResponse:
		if !bytes.Equal(msg.MessageID, []byte{2, 0, 0, 0}) || len(msg.Tables) != 1 || string(msg.Tables[0].Rows) != "second" {
			t.Fatalf("second query response = %+v", msg)
		}
	case protocol.ProcedureResponse:
		if !bytes.Equal(msg.MessageID, []byte{2, 0, 0, 0}) || string(msg.Result) != "second" {
			t.Fatalf("second procedure response = %+v", msg)
		}
	default:
		t.Fatalf("second response = %T, want typed response", result)
	}
}

func assertFirstTypedResponse(t *testing.T, tag uint8, result any) {
	t.Helper()
	switch msg := result.(type) {
	case protocol.TransactionUpdate:
		if tag != protocol.TagTransactionUpdate || msg.ReducerCall.RequestID != 1 {
			t.Fatalf("late reducer response = tag %d %+v", tag, msg)
		}
	case protocol.OneOffQueryResponse:
		if tag != protocol.TagOneOffQueryResponse || !bytes.Equal(msg.MessageID, []byte{1, 0, 0, 0}) || len(msg.Tables) != 1 || string(msg.Tables[0].Rows) != "first" {
			t.Fatalf("late query response = tag %d %+v", tag, msg)
		}
	case protocol.ProcedureResponse:
		if tag != protocol.TagProcedureResponse || !bytes.Equal(msg.MessageID, []byte{1, 0, 0, 0}) || string(msg.Result) != "first" {
			t.Fatalf("late procedure response = tag %d %+v", tag, msg)
		}
	default:
		t.Fatalf("late response = tag %d %T, want typed response", tag, result)
	}
}

func readProtocolClientMessage(t *testing.T, r *http.Request, ws *websocket.Conn) (any, bool) {
	t.Helper()
	_, frame, err := ws.Read(r.Context())
	if err != nil {
		t.Errorf("server read client message: %v", err)
		return nil, false
	}
	_, msg, err := protocol.DecodeClientMessage(frame)
	if err != nil {
		t.Errorf("DecodeClientMessage: %v", err)
		return nil, false
	}
	return msg, true
}
