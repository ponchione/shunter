package shunter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
	"github.com/ponchione/websocket"
)

type declaredViewAppliedGateSender struct {
	delegate protocol.ClientSender
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (s *declaredViewAppliedGateSender) Send(connID types.ConnectionID, msg any) error {
	switch msg.(type) {
	case protocol.SubscribeSingleApplied, *protocol.SubscribeSingleApplied:
		s.once.Do(func() { close(s.started) })
		<-s.release
	}
	return s.delegate.Send(connID, msg)
}

func (s *declaredViewAppliedGateSender) SendTransactionUpdate(connID types.ConnectionID, update *protocol.TransactionUpdate) error {
	return s.delegate.SendTransactionUpdate(connID, update)
}

func (s *declaredViewAppliedGateSender) SendTransactionUpdateLight(connID types.ConnectionID, update *protocol.TransactionUpdateLight) error {
	return s.delegate.SendTransactionUpdateLight(connID, update)
}

type asyncReducerResult struct {
	result ReducerResult
	err    error
}

func TestProtocolDeclaredViewAppliedEnqueueBlocksLaterCommit(t *testing.T) {
	t.Run("parameterless", func(t *testing.T) {
		rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
			Reducer("insert_message", insertMessageReducer).
			View(ViewDeclaration{
				Name:        "live_messages",
				SQL:         "SELECT * FROM messages",
				Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			}), declaredReadProtocolConfig(t))
		defer rt.Close()
		insertMessage(t, rt, "initial")

		client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "ordering-subscriber", "messages:subscribe"))
		gateDeclaredViewApplied(t, rt, func() {
			writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
				RequestID: 71,
				QueryID:   81,
				Name:      "live_messages",
			})
		}, func() <-chan asyncReducerResult {
			result := make(chan asyncReducerResult, 1)
			go func() {
				got, err := rt.CallReducer(context.Background(), "insert_message", []byte("later"))
				result <- asyncReducerResult{result: got, err: err}
			}()
			return result
		})

		requireDeclaredReadAppliedRows(t, client, 71, 81, "messages", 1)
		requireDeclaredReadDeltaRows(t, client, 81, "messages", 1, 0)
	})

	t.Run("parameterized", func(t *testing.T) {
		rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
			Reducer("insert_message_with_body", insertMessageWithBodyReducer).
			View(ViewDeclaration{
				Name:        "live_messages_by_body",
				SQL:         "SELECT * FROM messages WHERE body = :body",
				Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			}, WithViewParameters(ProductSchema{Columns: []ProductColumn{
				{Name: "body", Type: "string"},
			}})), declaredReadProtocolConfig(t))
		defer rt.Close()
		insertMessageWithBody(t, rt, 1, "alpha")

		client := dialDeclaredReadProtocolV2(t, rt, mintDeclaredReadProtocolToken(t, "parameter-ordering-subscriber", "messages:subscribe"))
		params := encodeDeclaredReadProtocolParams(t, []schema.ColumnSchema{{Name: "body", Type: types.KindString}}, types.ProductValue{types.NewString("alpha")})
		gateDeclaredViewApplied(t, rt, func() {
			writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewWithParametersMsg{
				RequestID: 72,
				QueryID:   82,
				Name:      "live_messages_by_body",
				Params:    params,
			})
		}, func() <-chan asyncReducerResult {
			result := make(chan asyncReducerResult, 1)
			go func() {
				got, err := rt.CallReducer(context.Background(), "insert_message_with_body", append([]byte{2}, []byte("alpha")...))
				result <- asyncReducerResult{result: got, err: err}
			}()
			return result
		})

		columns := []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "body", Type: types.KindString},
		}
		initial := requireDeclaredReadAppliedValues(t, client, 72, 82, "messages", columns)
		assertDeclaredReadVisibleMessageRows(t, initial, []uint64{1}, "alpha", "ordered parameterized initial")
		inserts, deletes := requireDeclaredReadDeltaValues(t, client, 82, "messages", columns)
		assertDeclaredReadVisibleMessageRows(t, inserts, []uint64{2}, "alpha", "ordered parameterized delta")
		if len(deletes) != 0 {
			t.Fatalf("parameterized ordered delta deletes = %#v, want none", deletes)
		}
	})
}

func gateDeclaredViewApplied(t *testing.T, rt *Runtime, subscribe func(), startReducer func() <-chan asyncReducerResult) {
	t.Helper()
	gate := &declaredViewAppliedGateSender{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	rt.mu.Lock()
	gate.delegate = rt.protocolSender
	rt.protocolSender = gate
	rt.mu.Unlock()
	defer func() {
		rt.mu.Lock()
		rt.protocolSender = gate.delegate
		rt.mu.Unlock()
	}()
	released := false
	defer func() {
		if !released {
			close(gate.release)
		}
	}()

	subscribe()
	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		t.Fatal("declared Applied enqueue did not reach the gate")
	}
	reducerResult := startReducer()
	select {
	case got := <-reducerResult:
		t.Fatalf("later reducer completed before Applied enqueue: result=%+v err=%v", got.result, got.err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gate.release)
	released = true
	select {
	case got := <-reducerResult:
		if got.err != nil {
			t.Fatalf("later CallReducer admission: %v", got.err)
		}
		if got.result.Status != StatusCommitted {
			t.Fatalf("later reducer result = %+v, want committed", got.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("later reducer did not complete after Applied enqueue release")
	}
}

type declaredViewSendResultSender struct {
	delegate protocol.ClientSender
	result   chan error
}

func (s *declaredViewSendResultSender) Send(connID types.ConnectionID, msg any) error {
	err := s.delegate.Send(connID, msg)
	select {
	case s.result <- err:
	default:
	}
	return err
}

func (s *declaredViewSendResultSender) SendTransactionUpdate(connID types.ConnectionID, update *protocol.TransactionUpdate) error {
	return s.delegate.SendTransactionUpdate(connID, update)
}

func (s *declaredViewSendResultSender) SendTransactionUpdateLight(connID types.ConnectionID, update *protocol.TransactionUpdateLight) error {
	return s.delegate.SendTransactionUpdateLight(connID, update)
}

func TestProtocolDeclaredViewSnapshotPreparationFailureDoesNotPublish(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "snapshot exceeds the tiny response envelope")

	opts := protocol.DefaultProtocolOptions()
	opts.MaxOutboundMessageSize = 1
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	conn.Permissions = []string{"messages:subscribe"}
	sender := &declaredReadCapturingSender{}
	rt.mu.Lock()
	rt.protocolSender = sender
	rt.mu.Unlock()

	rt.HandleSubscribeDeclaredView(context.Background(), conn, &protocol.SubscribeDeclaredViewMsg{
		RequestID: 73,
		QueryID:   83,
		Name:      "live_messages",
	})
	waitForDeclaredViewSend(t, sender)
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("ActiveSubscriptionSets = %d, want 0 after snapshot-size failure", active)
	}
	_, _, gotMsg, _, _ := sender.snapshot()
	response, ok := gotMsg.(protocol.SubscriptionError)
	if !ok {
		t.Fatalf("response = %T, want SubscriptionError", gotMsg)
	}
	if !strings.Contains(response.Error, protocol.ErrOutboundMessageLimit.Error()) {
		t.Fatalf("SubscriptionError = %q, want outbound message limit", response.Error)
	}
}

func TestPrepareProtocolDeclaredViewAppliedRejectsRowEncodingFailure(t *testing.T) {
	rt := &Runtime{registry: nil}
	plan := declaredViewAdmissionPlan{
		tableName: "messages",
		projection: []subscription.ProjectionColumn{{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		}},
	}
	_, err := rt.prepareProtocolDeclaredViewApplied(
		protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, nil),
		plan,
		74,
		84,
		time.Now(),
		[]subscription.SubscriptionUpdate{{
			TableName: "messages",
			Inserts:   []types.ProductValue{{types.NewString("wrong wire kind")}},
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "encode error") {
		t.Fatalf("prepare error = %v, want row encoding failure", err)
	}
}

func TestProtocolDeclaredViewAppliedBackpressureRemovesPublishedSet(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	opts := protocol.DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	conn.Permissions = []string{"messages:subscribe"}
	if err := rt.protocolConns.Add(conn); err != nil {
		t.Fatalf("ConnManager.Add: %v", err)
	}
	defer func() {
		rt.protocolConns.Remove(conn.ID)
		conn.Disconnect(context.Background(), websocket.StatusNormalClosure, "test cleanup", nil, nil)
	}()
	conn.OutboundCh <- []byte{0}
	sender := &declaredViewSendResultSender{result: make(chan error, 1)}
	rt.mu.Lock()
	sender.delegate = rt.protocolSender
	rt.protocolSender = sender
	rt.mu.Unlock()

	rt.HandleSubscribeDeclaredView(context.Background(), conn, &protocol.SubscribeDeclaredViewMsg{
		RequestID: 75,
		QueryID:   85,
		Name:      "live_messages",
	})
	select {
	case err := <-sender.result:
		if !errors.Is(err, protocol.ErrClientBufferFull) {
			t.Fatalf("Applied send error = %v, want ErrClientBufferFull", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Applied send did not reach outbound backpressure")
	}
	deadline := time.Now().Add(2 * time.Second)
	for rt.subscriptions.ActiveSubscriptionSets() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("ActiveSubscriptionSets = %d, want 0 after Applied backpressure", active)
	}
}

func waitForDeclaredViewSend(t *testing.T, sender *declaredReadCapturingSender) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		calls, _, _, _, _ := sender.snapshot()
		if calls != 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("declared view response was not sent")
}
