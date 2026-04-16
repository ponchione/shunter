package subscription

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

// mockFanOutSender records delivery calls for test assertions.
type mockFanOutSender struct {
	mu       sync.Mutex
	txCalls  []txCall
	resCalls []resCall
	errCalls []errCall
	sendErr  error
}

type txCall struct {
	ConnID  types.ConnectionID
	TxID    types.TxID
	Updates []SubscriptionUpdate
}
type resCall struct {
	ConnID types.ConnectionID
	Result *ReducerCallResult
}
type errCall struct {
	ConnID types.ConnectionID
	SubID  types.SubscriptionID
	Msg    string
}

func (m *mockFanOutSender) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txCalls = append(m.txCalls, txCall{ConnID: connID, TxID: txID, Updates: updates})
	return m.sendErr
}
func (m *mockFanOutSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resCalls = append(m.resCalls, resCall{ConnID: connID, Result: result})
	return m.sendErr
}
func (m *mockFanOutSender) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errCalls = append(m.errCalls, errCall{ConnID: connID, SubID: subID, Msg: message})
	return m.sendErr
}

func cid(b byte) types.ConnectionID {
	var id types.ConnectionID
	id[0] = b
	return id
}

func TestFanOutWorker_NonCallerDelivery(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1, conn2 := cid(1), cid(2)
	inbox <- FanOutMessage{
		TxID: types.TxID(10),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(1)}}}},
			conn2: {{SubscriptionID: 2, TableName: "t2", Deletes: []types.ProductValue{{types.NewUint32(2)}}}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 2 {
		t.Fatalf("txCalls = %d, want 2", len(mock.txCalls))
	}
	for _, c := range mock.txCalls {
		if c.TxID != 10 {
			t.Fatalf("TxID = %d, want 10", c.TxID)
		}
	}
	if len(mock.resCalls) != 0 {
		t.Fatalf("resCalls = %d, want 0 (no caller)", len(mock.resCalls))
	}
}

func TestFanOutWorker_ContextCancel(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit on context cancel")
	}
}

func TestFanOutWorker_ClosedInbox(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	done := make(chan struct{})
	go func() {
		w.Run(context.Background())
		close(done)
	}()

	close(inbox)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit on closed inbox")
	}
}

func TestFanOutWorker_CallerDiversion(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller, other := cid(1), cid(2)
	callerResult := &ReducerCallResult{
		RequestID: 7,
		Status:    0,
		TxID:      types.TxID(20),
	}
	inbox <- FanOutMessage{
		TxID: types.TxID(20),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(10)}}}},
			other:  {{SubscriptionID: 2, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(20)}}}},
		},
		CallerConnID: &caller,
		CallerResult: callerResult,
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	// Caller gets ReducerCallResult, not TransactionUpdate.
	if len(mock.resCalls) != 1 {
		t.Fatalf("resCalls = %d, want 1", len(mock.resCalls))
	}
	if mock.resCalls[0].ConnID != caller {
		t.Fatalf("caller connID mismatch")
	}
	if mock.resCalls[0].Result.RequestID != 7 {
		t.Fatalf("RequestID = %d, want 7", mock.resCalls[0].Result.RequestID)
	}
	// Caller's updates embedded in the result
	if len(mock.resCalls[0].Result.TransactionUpdate) != 1 {
		t.Fatalf("caller TransactionUpdate len = %d, want 1", len(mock.resCalls[0].Result.TransactionUpdate))
	}

	// Other connection gets standalone TransactionUpdate.
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1", len(mock.txCalls))
	}
	if mock.txCalls[0].ConnID != other {
		t.Fatalf("non-caller connID mismatch")
	}
}

func TestFanOutWorker_CallerDiversion_FailedStatus(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller := cid(1)
	callerResult := &ReducerCallResult{
		RequestID: 3,
		Status:    1, // failed
		TxID:      types.TxID(30),
		Error:     "panic in reducer",
	}
	inbox <- FanOutMessage{
		TxID: types.TxID(30),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1"}},
		},
		CallerConnID: &caller,
		CallerResult: callerResult,
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.resCalls) != 1 {
		t.Fatalf("resCalls = %d, want 1", len(mock.resCalls))
	}
	// Failed status: result delivered with error, no TransactionUpdate embedded.
	if mock.resCalls[0].Result.Status != 1 {
		t.Fatalf("Status = %d, want 1", mock.resCalls[0].Result.Status)
	}
	if mock.resCalls[0].Result.TransactionUpdate != nil {
		t.Fatalf("TransactionUpdate should be nil for failed status")
	}
	// Caller NOT in txCalls.
	if len(mock.txCalls) != 0 {
		t.Fatalf("txCalls = %d, want 0 (failed reducer, no standalone delivery)", len(mock.txCalls))
	}
}
