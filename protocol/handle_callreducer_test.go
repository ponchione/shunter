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
// unsubscribe and call-reducer interactions. It backs both the unsubscribe
// handler tests (which exercise the set-based seam) and the call-reducer
// tests in this file.
type mockDispatchExecutor struct {
	mu               sync.Mutex
	registerSetReq   *RegisterSubscriptionSetRequest
	registerSetErr   error
	unregisterSetReq *UnregisterSubscriptionSetRequest
	unregisterSetErr error
	callReducerReq   *CallReducerRequest
	callReducerErr   error
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

func (m *mockDispatchExecutor) RegisterSubscriptionSet(_ context.Context, req RegisterSubscriptionSetRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerSetReq = &req
	return m.registerSetErr
}

func (m *mockDispatchExecutor) UnregisterSubscriptionSet(_ context.Context, req UnregisterSubscriptionSetRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterSetReq = &req
	return m.unregisterSetErr
}

func (m *mockDispatchExecutor) CallReducer(_ context.Context, req CallReducerRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callReducerReq = &req
	return m.callReducerErr
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

func TestHandleCallReducer_DeliversAsyncHeavyTransactionUpdate(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &CallReducerMsg{
		RequestID:   10,
		ReducerName: "AddUser",
		Args:        []byte{0xCA, 0xFE},
	}
	handleCallReducer(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	respCh := exec.callReducerReq.ResponseCh
	exec.mu.Unlock()
	if respCh == nil {
		t.Fatal("missing reducer response channel")
	}
	respCh <- TransactionUpdate{
		Status: StatusCommitted{},
		ReducerCall: ReducerCallInfo{
			ReducerName: "AddUser",
			RequestID:   10,
		},
	}

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagTransactionUpdate {
		t.Fatalf("tag = %d, want %d (TagTransactionUpdate)", tag, TagTransactionUpdate)
	}
	tu := decoded.(TransactionUpdate)
	if tu.ReducerCall.RequestID != 10 {
		t.Fatalf("TransactionUpdate.ReducerCall.RequestID = %d, want 10", tu.ReducerCall.RequestID)
	}
	if _, ok := tu.Status.(StatusCommitted); !ok {
		t.Fatalf("Status = %T, want StatusCommitted", tu.Status)
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

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagTransactionUpdate {
		t.Fatalf("tag = %d, want %d (TagTransactionUpdate)", tag, TagTransactionUpdate)
	}
	tu := decoded.(TransactionUpdate)
	if tu.ReducerCall.RequestID != 20 {
		t.Errorf("ReducerCall.RequestID = %d, want 20", tu.ReducerCall.RequestID)
	}
	failed, ok := tu.Status.(StatusFailed)
	if !ok {
		t.Fatalf("Status = %T, want StatusFailed", tu.Status)
	}
	if !strings.Contains(failed.Error, "lifecycle reducer") {
		t.Errorf("StatusFailed.Error = %q, want to contain 'lifecycle reducer'", failed.Error)
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

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagTransactionUpdate {
		t.Fatalf("tag = %d, want %d (TagTransactionUpdate)", tag, TagTransactionUpdate)
	}
	tu := decoded.(TransactionUpdate)
	if tu.ReducerCall.RequestID != 30 {
		t.Errorf("ReducerCall.RequestID = %d, want 30", tu.ReducerCall.RequestID)
	}
	if _, ok := tu.Status.(StatusFailed); !ok {
		t.Fatalf("Status = %T, want StatusFailed", tu.Status)
	}
}

// TestHandleCallReducer_ForwardsFlags_NoSuccessNotify pins that the
// NoSuccessNotify wire flag is forwarded onto CallReducerRequest.Flags
// so downstream seams (executor, fanout worker) can honor the caller's
// opt-out. Phase 1.5 sub-slice.
func TestHandleCallReducer_ForwardsFlags_NoSuccessNotify(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &CallReducerMsg{
		RequestID:   50,
		ReducerName: "DoThing",
		Args:        []byte{0x02},
		Flags:       CallReducerFlagsNoSuccessNotify,
	}
	handleCallReducer(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.callReducerReq == nil {
		t.Fatal("executor.CallReducer was not called")
	}
	if exec.callReducerReq.Flags != CallReducerFlagsNoSuccessNotify {
		t.Errorf("CallReducerRequest.Flags = %d, want %d (NoSuccessNotify)",
			exec.callReducerReq.Flags, CallReducerFlagsNoSuccessNotify)
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

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagTransactionUpdate {
		t.Fatalf("tag = %d, want %d (TagTransactionUpdate)", tag, TagTransactionUpdate)
	}
	tu := decoded.(TransactionUpdate)
	if tu.ReducerCall.RequestID != 40 {
		t.Errorf("ReducerCall.RequestID = %d, want 40", tu.ReducerCall.RequestID)
	}
	failed, ok := tu.Status.(StatusFailed)
	if !ok {
		t.Fatalf("Status = %T, want StatusFailed", tu.Status)
	}
	if !strings.Contains(failed.Error, "executor unavailable") {
		t.Errorf("StatusFailed.Error = %q, want to contain 'executor unavailable'", failed.Error)
	}
	if !strings.Contains(failed.Error, "reducer crashed") {
		t.Errorf("StatusFailed.Error = %q, want to contain underlying cause 'reducer crashed'", failed.Error)
	}
}
