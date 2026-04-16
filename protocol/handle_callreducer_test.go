package protocol

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ponchione/shunter/types"
)

// mockDispatchExecutor is a test double for ExecutorInbox that records
// unsubscribe and call-reducer interactions.
type mockDispatchExecutor struct {
	mu             sync.Mutex
	unregisterReq  *UnregisterSubscriptionRequest
	unregisterErr  error
	callReducerReq *CallReducerRequest
	callReducerErr error
}

func (m *mockDispatchExecutor) OnConnect(_ context.Context, _ types.ConnectionID, _ types.Identity) error {
	return nil
}

func (m *mockDispatchExecutor) OnDisconnect(_ context.Context, _ types.ConnectionID, _ types.Identity) error {
	return nil
}

func (m *mockDispatchExecutor) DisconnectClientSubscriptions(_ context.Context, _ types.ConnectionID) error {
	return nil
}

func (m *mockDispatchExecutor) RegisterSubscription(_ context.Context, _ RegisterSubscriptionRequest) error {
	return nil
}

func (m *mockDispatchExecutor) UnregisterSubscription(_ context.Context, req UnregisterSubscriptionRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterReq = &req
	return m.unregisterErr
}

func (m *mockDispatchExecutor) CallReducer(_ context.Context, req CallReducerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callReducerReq = &req
	return m.callReducerErr
}

// --- Unsubscribe tests ---

func TestHandleUnsubscribe_Active(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	// Reserve and activate a subscription so IsActive returns true.
	if err := conn.Subscriptions.Reserve(42); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	conn.Subscriptions.Activate(42)

	msg := &UnsubscribeMsg{RequestID: 1, SubscriptionID: 42}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	// Subscription must be removed from tracker.
	if conn.Subscriptions.IsActiveOrPending(42) {
		t.Error("subscription 42 still tracked after unsubscribe")
	}

	// Executor must have received the unregister call.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.unregisterReq == nil {
		t.Fatal("UnregisterSubscription request was not recorded")
	}
	if exec.unregisterReq.ConnID != conn.ID {
		t.Errorf("UnregisterSubscription connID = %x, want %x", exec.unregisterReq.ConnID, conn.ID)
	}
	if exec.unregisterReq.SubscriptionID != 42 {
		t.Errorf("UnregisterSubscription subID = %d, want 42", exec.unregisterReq.SubscriptionID)
	}
	if exec.unregisterReq.RequestID != 1 {
		t.Errorf("UnregisterSubscription requestID = %d, want 1", exec.unregisterReq.RequestID)
	}
	if exec.unregisterReq.SendDropped {
		t.Error("SendDropped = true, want false")
	}
	if exec.unregisterReq.ResponseCh == nil {
		t.Error("ResponseCh = nil, want non-nil unsubscribe response channel")
	}

	// No error message should have been sent.
	select {
	case <-conn.OutboundCh:
		t.Error("unexpected message on OutboundCh for successful unsubscribe")
	default:
	}
}

func TestHandleUnsubscribe_Pending(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	// Reserve but do NOT activate — subscription is pending.
	if err := conn.Subscriptions.Reserve(10); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	msg := &UnsubscribeMsg{RequestID: 2, SubscriptionID: 10}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	// Pending subscription must NOT be removed.
	if !conn.Subscriptions.IsActiveOrPending(10) {
		t.Error("pending subscription 10 was removed; should still be tracked")
	}

	// Error should have been sent.
	tag, decoded := drainServerMsg(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.RequestID != 2 {
		t.Errorf("RequestID = %d, want 2", se.RequestID)
	}
	if se.SubscriptionID != 10 {
		t.Errorf("SubscriptionID = %d, want 10", se.SubscriptionID)
	}
	if !strings.Contains(se.Error, "subscription_id not found") {
		t.Errorf("Error = %q, want to contain 'subscription_id not found'", se.Error)
	}
}

func TestHandleUnsubscribe_NotFound(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &UnsubscribeMsg{RequestID: 3, SubscriptionID: 999}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	tag, decoded := drainServerMsg(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.RequestID != 3 {
		t.Errorf("RequestID = %d, want 3", se.RequestID)
	}
	if se.SubscriptionID != 999 {
		t.Errorf("SubscriptionID = %d, want 999", se.SubscriptionID)
	}
	if !strings.Contains(se.Error, "subscription_id not found") {
		t.Errorf("Error = %q, want to contain 'subscription_id not found'", se.Error)
	}
}

func TestHandleUnsubscribe_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{unregisterErr: errors.New("db down")}

	// Reserve and activate so the handler reaches the executor call.
	if err := conn.Subscriptions.Reserve(7); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	conn.Subscriptions.Activate(7)

	msg := &UnsubscribeMsg{RequestID: 4, SubscriptionID: 7}
	handleUnsubscribe(context.Background(), conn, msg, exec)

	// Subscription must still be tracked (executor rejected the removal).
	if !conn.Subscriptions.IsActiveOrPending(7) {
		t.Error("subscription 7 removed despite executor rejection")
	}

	tag, decoded := drainServerMsg(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.RequestID != 4 {
		t.Errorf("RequestID = %d, want 4", se.RequestID)
	}
	if !strings.Contains(se.Error, "executor unavailable") {
		t.Errorf("Error = %q, want to contain 'executor unavailable'", se.Error)
	}
	if !strings.Contains(se.Error, "db down") {
		t.Errorf("Error = %q, want to contain underlying cause 'db down'", se.Error)
	}
}

// --- CallReducer tests ---

func TestHandleCallReducer_Valid(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &CallReducerMsg{
		RequestID:   10,
		ReducerName: "AddUser",
		Args:        []byte{0xCA, 0xFE},
	}
	handleCallReducer(context.Background(), conn, msg, exec)

	// Executor must have received the request.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.callReducerReq == nil {
		t.Fatal("executor.CallReducer was not called")
	}
	req := exec.callReducerReq
	if req.ConnID != conn.ID {
		t.Errorf("ConnID = %x, want %x", req.ConnID, conn.ID)
	}
	if req.Identity != conn.Identity {
		t.Errorf("Identity = %x, want %x", req.Identity, conn.Identity)
	}
	if req.RequestID != 10 {
		t.Errorf("RequestID = %d, want 10", req.RequestID)
	}
	if req.ReducerName != "AddUser" {
		t.Errorf("ReducerName = %q, want %q", req.ReducerName, "AddUser")
	}
	if len(req.Args) != 2 || req.Args[0] != 0xCA || req.Args[1] != 0xFE {
		t.Errorf("Args = %x, want cafe", req.Args)
	}
	if req.ResponseCh == nil {
		t.Error("ResponseCh = nil, want non-nil reducer response channel")
	}

	// No error message should have been sent.
	select {
	case <-conn.OutboundCh:
		t.Error("unexpected message on OutboundCh for successful call")
	default:
	}
}

func TestHandleCallReducer_OnConnect(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &CallReducerMsg{
		RequestID:   20,
		ReducerName: "OnConnect",
		Args:        nil,
	}
	handleCallReducer(context.Background(), conn, msg, exec)

	// Executor must NOT have been called.
	exec.mu.Lock()
	if exec.callReducerReq != nil {
		t.Error("executor.CallReducer was called for lifecycle reducer OnConnect")
	}
	exec.mu.Unlock()

	tag, decoded := drainServerMsg(t, conn)
	if tag != TagReducerCallResult {
		t.Fatalf("tag = %d, want %d (TagReducerCallResult)", tag, TagReducerCallResult)
	}
	rcr := decoded.(ReducerCallResult)
	if rcr.RequestID != 20 {
		t.Errorf("RequestID = %d, want 20", rcr.RequestID)
	}
	if rcr.Status != 3 {
		t.Errorf("Status = %d, want 3 (not_found)", rcr.Status)
	}
	if !strings.Contains(rcr.Error, "lifecycle reducer") {
		t.Errorf("Error = %q, want to contain 'lifecycle reducer'", rcr.Error)
	}
}

func TestHandleCallReducer_OnDisconnect(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &CallReducerMsg{
		RequestID:   30,
		ReducerName: "OnDisconnect",
		Args:        nil,
	}
	handleCallReducer(context.Background(), conn, msg, exec)

	// Executor must NOT have been called.
	exec.mu.Lock()
	if exec.callReducerReq != nil {
		t.Error("executor.CallReducer was called for lifecycle reducer OnDisconnect")
	}
	exec.mu.Unlock()

	tag, decoded := drainServerMsg(t, conn)
	if tag != TagReducerCallResult {
		t.Fatalf("tag = %d, want %d (TagReducerCallResult)", tag, TagReducerCallResult)
	}
	rcr := decoded.(ReducerCallResult)
	if rcr.RequestID != 30 {
		t.Errorf("RequestID = %d, want 30", rcr.RequestID)
	}
	if rcr.Status != 3 {
		t.Errorf("Status = %d, want 3 (not_found)", rcr.Status)
	}
}

func TestHandleCallReducer_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{callReducerErr: errors.New("reducer crashed")}

	msg := &CallReducerMsg{
		RequestID:   40,
		ReducerName: "DoThing",
		Args:        []byte{0x01},
	}
	handleCallReducer(context.Background(), conn, msg, exec)

	tag, decoded := drainServerMsg(t, conn)
	if tag != TagReducerCallResult {
		t.Fatalf("tag = %d, want %d (TagReducerCallResult)", tag, TagReducerCallResult)
	}
	rcr := decoded.(ReducerCallResult)
	if rcr.RequestID != 40 {
		t.Errorf("RequestID = %d, want 40", rcr.RequestID)
	}
	if rcr.Status != 3 {
		t.Errorf("Status = %d, want 3 (not_found)", rcr.Status)
	}
	if !strings.Contains(rcr.Error, "executor unavailable") {
		t.Errorf("Error = %q, want to contain 'executor unavailable'", rcr.Error)
	}
	if !strings.Contains(rcr.Error, "reducer crashed") {
		t.Errorf("Error = %q, want to contain underlying cause 'reducer crashed'", rcr.Error)
	}
}
