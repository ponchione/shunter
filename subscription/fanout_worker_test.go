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

func TestFanOutWorker_ContextCancelWhileWaitingOnTxDurable(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	conn1 := cid(1)
	w.SetConfirmedReads(conn1, true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: make(chan types.TxID),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit while waiting on TxDurable")
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

func TestFanOutWorker_BufferFull_DropsClient(t *testing.T) {
	mock := &mockFanOutSender{sendErr: ErrSendBufferFull}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	select {
	case id := <-dropped:
		if id != conn1 {
			t.Fatalf("dropped = %x, want %x", id[:], conn1[:])
		}
	case <-time.After(time.Second):
		t.Fatal("no dropped client signal")
	}
}

func TestFanOutWorker_ConnGone_Silent(t *testing.T) {
	mock := &mockFanOutSender{sendErr: ErrSendConnGone}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case id := <-dropped:
		t.Fatalf("unexpected dropped signal: %x", id[:])
	default:
	}
}

func TestFanOutWorker_MultipleSlowClients(t *testing.T) {
	failConn := cid(2)
	sender := &selectiveFailSender{fail: failConn}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, sender, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			cid(1):   {{SubscriptionID: 1, TableName: "t1"}},
			failConn: {{SubscriptionID: 2, TableName: "t1"}},
			cid(3):   {{SubscriptionID: 3, TableName: "t1"}},
		},
	}

	select {
	case id := <-dropped:
		if id != failConn {
			t.Fatalf("dropped = %x, want %x", id[:], failConn[:])
		}
	case <-time.After(time.Second):
		t.Fatal("no dropped client signal")
	}

	time.Sleep(50 * time.Millisecond)
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.okCount < 2 {
		t.Fatalf("okCount = %d, want >= 2", sender.okCount)
	}
}

// selectiveFailSender fails with ErrSendBufferFull for a specific connID.
type selectiveFailSender struct {
	mu      sync.Mutex
	fail    types.ConnectionID
	okCount int
}

func (s *selectiveFailSender) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if connID == s.fail {
		return ErrSendBufferFull
	}
	s.okCount++
	return nil
}
func (s *selectiveFailSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	return nil
}
func (s *selectiveFailSender) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	return nil
}

func TestFanOutWorker_FastRead_NoWait(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// TxDurable never signals — if worker waits, test will timeout.
	durableCh := make(chan types.TxID)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1 (fast-read should not wait)", len(mock.txCalls))
	}
}

func TestFanOutWorker_ConfirmedRead_Waits(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	conn1 := cid(1)
	w.SetConfirmedReads(conn1, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	// No delivery yet — waiting for TxDurable.
	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preCount := len(mock.txCalls)
	mock.mu.Unlock()
	if preCount != 0 {
		t.Fatalf("txCalls = %d before TxDurable, want 0", preCount)
	}

	// Signal durability.
	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d after TxDurable, want 1", len(mock.txCalls))
	}
}

func TestFanOutWorker_NilTxDurable_Skips(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	conn1 := cid(1)
	w.SetConfirmedReads(conn1, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// TxDurable is nil — treat as already durable.
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1 (nil TxDurable = already durable)", len(mock.txCalls))
	}
}

func TestFanOutWorker_SetConfirmedReads_Toggle(t *testing.T) {
	w := &FanOutWorker{confirmedReads: make(map[types.ConnectionID]bool)}
	conn1 := cid(1)

	w.SetConfirmedReads(conn1, true)
	if !w.confirmedReads[conn1] {
		t.Fatal("expected confirmed reads enabled")
	}

	w.SetConfirmedReads(conn1, false)
	if w.confirmedReads[conn1] {
		t.Fatal("expected confirmed reads disabled")
	}
}

func TestFanOutWorker_RemoveClient(t *testing.T) {
	w := &FanOutWorker{confirmedReads: make(map[types.ConnectionID]bool)}
	conn1 := cid(1)
	w.confirmedReads[conn1] = true

	w.RemoveClient(conn1)
	if _, ok := w.confirmedReads[conn1]; ok {
		t.Fatal("RemoveClient should clear confirmedReads entry")
	}
}

func TestFanOutWorker_DeliverDoesNotMutateFanout(t *testing.T) {
	mock := &mockFanOutSender{}
	w := NewFanOutWorker(nil, mock, make(chan types.ConnectionID, 1))
	caller, other := cid(1), cid(2)
	fanout := CommitFanout{
		caller: {{SubscriptionID: 1, TableName: "t1"}},
		other:  {{SubscriptionID: 2, TableName: "t2"}},
	}
	w.deliver(context.Background(), FanOutMessage{
		TxID:          5,
		Fanout:        fanout,
		CallerConnID:  &caller,
		CallerResult:  &ReducerCallResult{RequestID: 1, Status: 0, TxID: 5},
	})
	if _, ok := fanout[caller]; !ok {
		t.Fatal("deliver mutated original fanout map by removing caller entry")
	}
}

func TestFanOutWorker_DroppedChannelFullDoesNotBlock(t *testing.T) {
	mock := &mockFanOutSender{sendErr: ErrSendBufferFull}
	dropped := make(chan types.ConnectionID, 1)
	dropped <- cid(9)
	w := NewFanOutWorker(nil, mock, dropped)
	done := make(chan struct{})
	go func() {
		w.deliver(context.Background(), FanOutMessage{
			TxID: 1,
			Fanout: CommitFanout{
				cid(1): {{SubscriptionID: 1, TableName: "t1"}},
			},
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deliver blocked on full dropped channel")
	}
}

func TestFanOutWorker_ConfirmedReadPolicyConcurrentToggle(t *testing.T) {
	mock := &mockFanOutSender{}
	w := NewFanOutWorker(nil, mock, make(chan types.ConnectionID, 16))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan types.TxID)
	go func() {
		w.deliver(ctx, FanOutMessage{
			TxID:      2,
			TxDurable: ready,
			Fanout: CommitFanout{
				cid(1): {{SubscriptionID: 1, TableName: "t1"}},
			},
		})
	}()
	for i := 0; i < 100; i++ {
		w.SetConfirmedReads(cid(1), i%2 == 0)
	}
	close(ready)
}

func TestFanOutWorker_SubscriptionErrorDelivery(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
		Errors: map[types.ConnectionID][]SubscriptionError{
			conn1: {
				{SubscriptionID: 5, QueryHash: QueryHash{1}, Message: "eval failed"},
				{SubscriptionID: 6, QueryHash: QueryHash{2}, Message: "type mismatch"},
			},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.errCalls) != 2 {
		t.Fatalf("errCalls = %d, want 2", len(mock.errCalls))
	}
	if mock.errCalls[0].SubID != 5 || mock.errCalls[1].SubID != 6 {
		t.Fatalf("errCalls SubIDs = %d, %d; want 5, 6", mock.errCalls[0].SubID, mock.errCalls[1].SubID)
	}
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1", len(mock.txCalls))
	}
}

func TestFanOutWorker_ErrorsDeliveredBeforeUpdates(t *testing.T) {
	type call struct {
		kind string
		conn types.ConnectionID
	}
	var mu sync.Mutex
	var order []call

	sender := &orderTrackingSender{
		onTx: func(connID types.ConnectionID) {
			mu.Lock()
			order = append(order, call{kind: "tx", conn: connID})
			mu.Unlock()
		},
		onErr: func(connID types.ConnectionID) {
			mu.Lock()
			order = append(order, call{kind: "err", conn: connID})
			mu.Unlock()
		},
	}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, sender, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
		Errors: map[types.ConnectionID][]SubscriptionError{
			conn1: {{SubscriptionID: 5, Message: "boom"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 {
		t.Fatalf("order len = %d, want >= 2", len(order))
	}
	if order[0].kind != "err" {
		t.Fatalf("first call = %q, want 'err' (errors before updates)", order[0].kind)
	}
	if order[1].kind != "tx" {
		t.Fatalf("second call = %q, want 'tx'", order[1].kind)
	}
}

type orderTrackingSender struct {
	onTx  func(types.ConnectionID)
	onErr func(types.ConnectionID)
}

func (s *orderTrackingSender) SendTransactionUpdate(connID types.ConnectionID, txID types.TxID, updates []SubscriptionUpdate) error {
	if s.onTx != nil {
		s.onTx(connID)
	}
	return nil
}
func (s *orderTrackingSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	return nil
}
func (s *orderTrackingSender) SendSubscriptionError(connID types.ConnectionID, subID types.SubscriptionID, message string) error {
	if s.onErr != nil {
		s.onErr(connID)
	}
	return nil
}

func TestFanOutWorker_Acceptance_FullFlow(t *testing.T) {
	// Full pipeline: Manager.EvalAndBroadcast → inbox → FanOutWorker → mock sender.
	// Uses existing test helpers: testSchema() (validate_test.go), simpleChangeset() (delta_view_test.go).
	mock := &mockFanOutSender{}
	fanoutCh := make(chan FanOutMessage, 64)

	s := testSchema() // fakeSchema: table 1 (cols: 0=KindUint64, 1=KindString, idx on 0)
	mgr := NewManager(s, s, WithFanOutInbox(fanoutCh))

	worker := NewFanOutWorker(fanoutCh, mock, mgr.DroppedChanSend())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Run(ctx)

	// Register AllRows subscription on table 1.
	conn1 := cid(1)
	_, err := mgr.Register(SubscriptionRegisterRequest{
		ConnID:         conn1,
		SubscriptionID: 10,
		Predicate:      AllRows{Table: 1},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a commit: one insert to table 1.
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(42), types.NewString("alice")}},
		nil,
	)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	// Wait for fan-out delivery.
	time.Sleep(100 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txCalls) != 1 {
		t.Fatalf("txCalls = %d, want 1", len(mock.txCalls))
	}
	if mock.txCalls[0].ConnID != conn1 {
		t.Fatalf("connID mismatch")
	}
	if mock.txCalls[0].TxID != 1 {
		t.Fatalf("TxID = %d, want 1", mock.txCalls[0].TxID)
	}
	if len(mock.txCalls[0].Updates) == 0 {
		t.Fatal("no updates delivered")
	}
	if mock.txCalls[0].Updates[0].SubscriptionID != 10 {
		t.Fatalf("SubscriptionID = %d, want 10", mock.txCalls[0].Updates[0].SubscriptionID)
	}
}
