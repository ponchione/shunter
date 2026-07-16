package protocol

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/websocket"
)

func TestLiveInboundMessageLimitRejectsNegativeOptionBeforeUpgrade(t *testing.T) {
	s, recorder := strictServer(t)
	s.Options.MaxMessageSize = -1
	srv := newTestServer(t, s)

	client, resp, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		subprotocols: []string{SubprotocolV1},
	})
	if client != nil {
		client.CloseNow()
		t.Fatal("negative MaxMessageSize unexpectedly reached WebSocket setup")
	}
	if err == nil {
		t.Fatal("negative MaxMessageSize unexpectedly completed WebSocket upgrade")
	}
	if resp == nil || resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("negative MaxMessageSize response = %v, want HTTP %d", resp, http.StatusInternalServerError)
	}
	recorder.mu.Lock()
	upgrades := len(recorder.seen)
	recorder.mu.Unlock()
	if upgrades != 0 {
		t.Fatalf("Upgraded callback calls = %d, want 0 for invalid protocol options", upgrades)
	}
}

func TestLiveInboundMessageLimit(t *testing.T) {
	const libraryDefaultReadLimit = 32 * 1024

	encodeCall := func(t *testing.T, argsBytes int) []byte {
		t.Helper()
		frame, err := EncodeClientMessage(CallReducerMsg{
			ReducerName: "message_limit_probe",
			Args:        make([]byte, argsBytes),
			RequestID:   41,
		})
		if err != nil {
			t.Fatalf("EncodeClientMessage: %v", err)
		}
		return frame
	}

	smallFrame := encodeCall(t, 128)
	largeFrame := encodeCall(t, libraryDefaultReadLimit+1024)
	if len(largeFrame) <= libraryDefaultReadLimit {
		t.Fatalf("large probe frame length = %d, want above library default %d", len(largeFrame), libraryDefaultReadLimit)
	}

	tests := []struct {
		name       string
		frame      []byte
		limit      int64
		wantArgs   int
		wantHandle bool
		wantClose  websocket.StatusCode
	}{
		{
			name:       "frame exactly at configured limit reaches handler",
			frame:      smallFrame,
			limit:      int64(len(smallFrame)),
			wantArgs:   128,
			wantHandle: true,
		},
		{
			name:      "same frame one byte above configured limit is rejected",
			frame:     smallFrame,
			limit:     int64(len(smallFrame) - 1),
			wantClose: websocket.StatusMessageTooBig,
		},
		{
			name:       "configured limit above library default reaches handler",
			frame:      largeFrame,
			limit:      int64(len(largeFrame)),
			wantArgs:   libraryDefaultReadLimit + 1024,
			wantHandle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			handled := make(chan CallReducerMsg, 1)
			dispatchDone := make(chan struct{})

			s, _ := strictServer(t)
			s.Options.MaxMessageSize = tt.limit
			s.Upgraded = func(ctx context.Context, uc *UpgradeContext) {
				defer close(dispatchDone)
				conn := NewConn(
					uc.ConnectionID,
					uc.Identity,
					uc.Token,
					false,
					uc.Conn,
					&s.Options,
				)
				conn.ProtocolVersion = uc.ProtocolVersion
				conn.runDispatchLoop(ctx, &MessageHandlers{
					OnCallReducer: func(_ context.Context, _ *Conn, msg *CallReducerMsg) {
						calls.Add(1)
						handled <- *msg
					},
				})
			}
			srv := newTestServer(t, s)

			client, resp, err := dialWS(t, srv, wsDialOpts{
				authHeader:   "Bearer " + mintValidToken(t),
				subprotocols: []string{SubprotocolV1},
			})
			if err != nil {
				t.Fatalf("dial live connection: %v (resp=%v)", err, resp)
			}
			t.Cleanup(func() { client.CloseNow() })

			writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
			err = client.Write(writeCtx, websocket.MessageBinary, tt.frame)
			cancelWrite()
			if err != nil {
				t.Fatalf("write %d-byte frame with %d-byte limit: %v", len(tt.frame), tt.limit, err)
			}

			if tt.wantHandle {
				select {
				case got := <-handled:
					if got.ReducerName != "message_limit_probe" || got.RequestID != 41 || len(got.Args) != tt.wantArgs {
						t.Fatalf("handler received wrong reducer message: name=%q request=%d args=%d", got.ReducerName, got.RequestID, len(got.Args))
					}
				case <-time.After(2 * time.Second):
					t.Fatalf("valid %d-byte frame did not reach handler at configured limit %d", len(tt.frame), tt.limit)
				}
				if got := calls.Load(); got != 1 {
					t.Fatalf("handler calls = %d, want exactly 1", got)
				}
				client.CloseNow()
			} else {
				readCtx, cancelRead := context.WithTimeout(context.Background(), 2*time.Second)
				_, _, readErr := client.Read(readCtx)
				cancelRead()
				if got := websocket.CloseStatus(readErr); got != tt.wantClose {
					t.Fatalf("oversized %d-byte frame close status = %d, want %d; error=%v", len(tt.frame), got, tt.wantClose, readErr)
				}
			}

			select {
			case <-dispatchDone:
			case <-time.After(2 * time.Second):
				t.Fatalf("dispatch did not stop after %d-byte frame case", len(tt.frame))
			}
			if !tt.wantHandle {
				if got := calls.Load(); got != 0 {
					t.Fatalf("oversized frame reached handler %d time(s), want 0", got)
				}
				select {
				case got := <-handled:
					t.Fatalf("oversized frame delivered unexpected handler message: %+v", got)
				default:
				}
			}
		})
	}
}
