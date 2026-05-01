package protocol

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/types"
)

type protocolMetricObserver struct {
	mu            sync.Mutex
	active        []int
	connResults   []string
	messageEvents []protocolMessageMetric
	backpressure  []string
	authFailures  []string
}

type protocolMessageMetric struct {
	kind   string
	result string
}

func (o *protocolMetricObserver) RecordProtocolConnections(active int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active = append(o.active, active)
}

func (o *protocolMetricObserver) RecordProtocolMessage(kind, result string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.messageEvents = append(o.messageEvents, protocolMessageMetric{kind: kind, result: result})
}

func (o *protocolMetricObserver) LogProtocolConnectionRejected(result string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.connResults = append(o.connResults, result)
}

func (o *protocolMetricObserver) LogProtocolConnectionOpened(types.ConnectionID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.connResults = append(o.connResults, "accepted")
}

func (o *protocolMetricObserver) LogProtocolConnectionClosed(types.ConnectionID, string) {}
func (o *protocolMetricObserver) LogProtocolProtocolError(string, string, error)         {}

func (o *protocolMetricObserver) LogProtocolAuthFailed(reason string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.authFailures = append(o.authFailures, reason)
}

func (o *protocolMetricObserver) LogProtocolBackpressure(direction, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.backpressure = append(o.backpressure, direction)
}

func (o *protocolMetricObserver) requireActive(t *testing.T, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		o.mu.Lock()
		for _, got := range o.active {
			if got == want {
				o.mu.Unlock()
				return
			}
		}
		snapshot := append([]int(nil), o.active...)
		o.mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("missing active protocol gauge %d in %v", want, snapshot)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (o *protocolMetricObserver) requireConnectionResults(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		o.mu.Lock()
		got := append([]string(nil), o.connResults...)
		o.mu.Unlock()
		if sameStringMultiset(got, want) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("connection results = %v, want %v", got, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (o *protocolMetricObserver) requireMessage(t *testing.T, kind, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, event := range o.messageEvents {
		if event.kind == kind && event.result == result {
			return
		}
	}
	t.Fatalf("missing protocol message kind=%q result=%q in %+v", kind, result, o.messageEvents)
}

func (o *protocolMetricObserver) requireBackpressure(t *testing.T, direction string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.backpressure {
		if got == direction {
			return
		}
	}
	t.Fatalf("missing backpressure direction %q in %v", direction, o.backpressure)
}

func TestProtocolMetricsConnectionGaugeAndAcceptedCounter(t *testing.T) {
	observer := &protocolMetricObserver{}
	inbox := &fakeInbox{}
	s, mgr := lifecycleServer(t, inbox)
	s.Observer = observer
	srv := newTestServer(t, s)

	c, _, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	data, err := readOneBinary(t, c, 2*time.Second)
	if err != nil {
		t.Fatalf("read identity: %v", err)
	}
	_, msg, err := DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("decode identity: %v", err)
	}
	identity := msg.(IdentityToken)

	observer.requireConnectionResults(t, "accepted")
	observer.requireActive(t, 1)

	conn := mgr.Get(identity.ConnectionID)
	if conn == nil {
		t.Fatal("admitted connection missing from manager")
	}
	conn.Disconnect(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr)
	observer.requireActive(t, 0)
}

func TestProtocolMetricsConnectionRejectionMapsExactlyOneResult(t *testing.T) {
	observer := &protocolMetricObserver{}
	inbox := &fakeInbox{onConnectErr: errors.New("nope")}
	s, _ := lifecycleServer(t, inbox)
	s.Observer = observer
	srv := newTestServer(t, s)

	c, _, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	_, _ = readOneBinary(t, c, 2*time.Second)

	observer.requireConnectionResults(t, "rejected_executor")
}

func TestProtocolMetricsAuthFailureRecordsRejectedAuth(t *testing.T) {
	observer := &protocolMetricObserver{}
	s := &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Options:  DefaultProtocolOptions(),
		Conns:    NewConnManager(),
		Observer: observer,
	}
	srv := newTestServer(t, s)
	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{Subprotocols: []string{SubprotocolV1}})
	if err == nil {
		t.Fatal("dial without auth unexpectedly succeeded")
	}

	observer.requireConnectionResults(t, "rejected_auth")
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.authFailures) != 1 || observer.authFailures[0] != "missing_token" {
		t.Fatalf("auth failures = %v, want [missing_token]", observer.authFailures)
	}
}

func TestProtocolMetricsMalformedMessageRecordsDecodedKind(t *testing.T) {
	observer := &protocolMetricObserver{}
	opts := DefaultProtocolOptions()
	conn, clientWS := testConnPair(t, &opts)
	conn.Observer = observer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, &MessageHandlers{})

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	if err := clientWS.Write(writeCtx, websocket.MessageBinary, []byte{TagCallReducer}); err != nil {
		writeCancel()
		t.Fatalf("write malformed frame: %v", err)
	}
	writeCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on malformed message")
	}
	observer.requireMessage(t, "call_reducer", "malformed")
}

func TestProtocolMetricsBackpressureRecordsInboundAndOutboundDirections(t *testing.T) {
	t.Run("inbound", func(t *testing.T) {
		observer := &protocolMetricObserver{}
		opts := DefaultProtocolOptions()
		opts.IncomingQueueMessages = 1
		conn, clientWS := testConnPair(t, &opts)
		conn.Observer = observer
		block := make(chan struct{})
		defer close(block)
		handlers := &MessageHandlers{
			OnSubscribeSingle: func(context.Context, *Conn, *SubscribeSingleMsg) {
				<-block
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := runDispatchAsync(conn, ctx, handlers)
		for i := uint32(0); i < 2; i++ {
			frame, _ := EncodeClientMessage(SubscribeSingleMsg{RequestID: i, QueryID: i + 1, QueryString: "SELECT * FROM t"})
			writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
			_ = clientWS.Write(writeCtx, websocket.MessageBinary, frame)
			writeCancel()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("dispatch loop did not exit on inbound backpressure")
		}
		observer.requireBackpressure(t, "inbound")
	})

	t.Run("outbound", func(t *testing.T) {
		observer := &protocolMetricObserver{}
		opts := DefaultProtocolOptions()
		opts.OutgoingBufferMessages = 1
		conn := testConnDirect(&opts)
		conn.Observer = observer
		mgr := NewConnManager()
		mgr.Add(conn)
		sender := NewClientSender(mgr, &fakeInbox{})
		msg := SubscribeSingleApplied{RequestID: 1, QueryID: 1, TableName: "t"}
		if err := sender.Send(conn.ID, msg); err != nil {
			t.Fatalf("first send: %v", err)
		}
		if err := sender.Send(conn.ID, msg); !errors.Is(err, ErrClientBufferFull) {
			t.Fatalf("second send = %v, want ErrClientBufferFull", err)
		}
		observer.requireBackpressure(t, "outbound")
	})
}

func sameStringMultiset(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	used := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, g := range got {
			if used[i] || g != w {
				continue
			}
			used[i] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}
