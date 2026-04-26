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
	mu         sync.Mutex
	lightCalls []lightCall
	heavyCalls []heavyCall
	errCalls   []errCall
	sendErr    error
}

type lightCall struct {
	ConnID    types.ConnectionID
	RequestID uint32
	Updates   []SubscriptionUpdate
}
type heavyCall struct {
	ConnID        types.ConnectionID
	Outcome       CallerOutcome
	CallerUpdates []SubscriptionUpdate
}
type errCall struct {
	ConnID types.ConnectionID
	Err    SubscriptionError
}

func (m *mockFanOutSender) SendTransactionUpdateHeavy(connID types.ConnectionID, outcome CallerOutcome, callerUpdates []SubscriptionUpdate, memo *EncodingMemo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heavyCalls = append(m.heavyCalls, heavyCall{ConnID: connID, Outcome: outcome, CallerUpdates: callerUpdates})
	return m.sendErr
}
func (m *mockFanOutSender) SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []SubscriptionUpdate, memo *EncodingMemo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lightCalls = append(m.lightCalls, lightCall{ConnID: connID, RequestID: requestID, Updates: updates})
	return m.sendErr
}
func (m *mockFanOutSender) SendSubscriptionError(connID types.ConnectionID, subErr SubscriptionError) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errCalls = append(m.errCalls, errCall{ConnID: connID, Err: subErr})
	return m.sendErr
}

func cid(b byte) types.ConnectionID {
	var id types.ConnectionID
	id[0] = b
	return id
}

func committedOutcome(requestID uint32) *CallerOutcome {
	return &CallerOutcome{Kind: CallerOutcomeCommitted, RequestID: requestID}
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
	if len(mock.lightCalls) != 2 {
		t.Fatalf("lightCalls = %d, want 2", len(mock.lightCalls))
	}
	if len(mock.heavyCalls) != 0 {
		t.Fatalf("heavyCalls = %d, want 0 (no caller)", len(mock.heavyCalls))
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

// TestFanOutWorker_CallerDiversion verifies the Phase 1.5 split: caller
// receives the heavy envelope with the caller's row delta embedded in
// CallerUpdates; non-callers receive the light envelope.
func TestFanOutWorker_CallerDiversion(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller, other := cid(1), cid(2)
	inbox <- FanOutMessage{
		TxID: types.TxID(20),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(10)}}}},
			other:  {{SubscriptionID: 2, TableName: "t1", Inserts: []types.ProductValue{{types.NewUint32(20)}}}},
		},
		CallerConnID:  &caller,
		CallerOutcome: committedOutcome(7),
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d, want 1", len(mock.heavyCalls))
	}
	if mock.heavyCalls[0].ConnID != caller {
		t.Fatalf("caller connID mismatch")
	}
	if mock.heavyCalls[0].Outcome.RequestID != 7 {
		t.Fatalf("RequestID = %d, want 7", mock.heavyCalls[0].Outcome.RequestID)
	}
	if len(mock.heavyCalls[0].CallerUpdates) != 1 {
		t.Fatalf("caller updates = %d, want 1", len(mock.heavyCalls[0].CallerUpdates))
	}

	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d, want 1", len(mock.lightCalls))
	}
	if mock.lightCalls[0].ConnID != other {
		t.Fatalf("non-caller connID mismatch")
	}
	if mock.lightCalls[0].RequestID != 7 {
		t.Fatalf("light RequestID = %d, want 7 (propagated from caller outcome)", mock.lightCalls[0].RequestID)
	}
}

// TestFanOutWorker_CallerDiversion_FailedStatus verifies that a failed
// reducer still delivers the heavy envelope to the caller; non-caller
// fanout is still delivered because the row-touches may have been
// preserved for other reducers. Phase 1.5 collapses all failure modes
// (user, panic, not_found) onto `CallerOutcomeFailed`.
func TestFanOutWorker_CallerDiversion_FailedStatus(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller := cid(1)
	inbox <- FanOutMessage{
		TxID: types.TxID(30),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1"}},
		},
		CallerConnID: &caller,
		CallerOutcome: &CallerOutcome{
			Kind:      CallerOutcomeFailed,
			RequestID: 3,
			Error:     "panic in reducer",
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d, want 1", len(mock.heavyCalls))
	}
	if mock.heavyCalls[0].Outcome.Kind != CallerOutcomeFailed {
		t.Fatalf("outcome Kind = %d, want CallerOutcomeFailed", mock.heavyCalls[0].Outcome.Kind)
	}
	if len(mock.lightCalls) != 0 {
		t.Fatalf("lightCalls = %d, want 0 (only caller, no non-caller recipients)", len(mock.lightCalls))
	}
}

// TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout is the
// Phase 1.5 `P0-DELIVERY-002` pin: even when no subscriptions are active
// and the fanout is empty, a reducer-originated commit must still
// deliver the caller's heavy envelope so the caller observes its outcome.
func TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller := cid(1)
	inbox <- FanOutMessage{
		TxID:          types.TxID(42),
		Fanout:        CommitFanout{}, // no subscriptions touched
		CallerConnID:  &caller,
		CallerOutcome: committedOutcome(99),
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d, want 1 (caller must observe outcome even with empty fanout)", len(mock.heavyCalls))
	}
	if mock.heavyCalls[0].Outcome.RequestID != 99 {
		t.Fatalf("heavy RequestID = %d, want 99", mock.heavyCalls[0].Outcome.RequestID)
	}
	if len(mock.heavyCalls[0].CallerUpdates) != 0 {
		t.Fatalf("caller updates = %d, want 0", len(mock.heavyCalls[0].CallerUpdates))
	}
	if len(mock.lightCalls) != 0 {
		t.Fatalf("lightCalls = %d, want 0", len(mock.lightCalls))
	}
}

// TestFanOutWorker_NoSuccessNotify_SuppressesCallerHeavy_OnCommitted
// pins the Phase 1.5 sub-slice: when the caller set
// CallReducerFlags::NoSuccessNotify and the outcome is committed, the
// caller's heavy envelope is skipped entirely. Caller observes nothing;
// non-caller recipients still receive their light envelopes.
func TestFanOutWorker_NoSuccessNotify_SuppressesCallerHeavy_OnCommitted(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller, other := cid(1), cid(2)
	inbox <- FanOutMessage{
		TxID: types.TxID(40),
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "t1"}},
			other:  {{SubscriptionID: 2, TableName: "t1"}},
		},
		CallerConnID: &caller,
		CallerOutcome: &CallerOutcome{
			Kind:      CallerOutcomeCommitted,
			RequestID: 4,
			Flags:     CallerOutcomeFlagNoSuccessNotify,
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 0 {
		t.Fatalf("heavyCalls = %d, want 0 (NoSuccessNotify suppresses caller-echo on commit)", len(mock.heavyCalls))
	}
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d, want 1 (non-caller still gets light)", len(mock.lightCalls))
	}
	if mock.lightCalls[0].ConnID != other {
		t.Fatalf("light recipient = %x, want %x", mock.lightCalls[0].ConnID[:], other[:])
	}
}

// TestFanOutWorker_NoSuccessNotify_EmptyFanout_NoDelivery pins that a
// caller-only batch with NoSuccessNotify + committed performs no delivery
// at all — no heavy to caller, no light to anyone.
func TestFanOutWorker_NoSuccessNotify_EmptyFanout_NoDelivery(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller := cid(1)
	inbox <- FanOutMessage{
		TxID:         types.TxID(41),
		Fanout:       CommitFanout{},
		CallerConnID: &caller,
		CallerOutcome: &CallerOutcome{
			Kind:      CallerOutcomeCommitted,
			RequestID: 5,
			Flags:     CallerOutcomeFlagNoSuccessNotify,
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 0 {
		t.Fatalf("heavyCalls = %d, want 0", len(mock.heavyCalls))
	}
	if len(mock.lightCalls) != 0 {
		t.Fatalf("lightCalls = %d, want 0", len(mock.lightCalls))
	}
}

// TestFanOutWorker_NoSuccessNotify_DoesNotSuppressOnFailed pins that
// the flag only suppresses the success echo: a failed reducer still
// delivers the heavy envelope so the caller observes the failure.
func TestFanOutWorker_NoSuccessNotify_DoesNotSuppressOnFailed(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	caller := cid(1)
	inbox <- FanOutMessage{
		TxID:         types.TxID(42),
		Fanout:       CommitFanout{},
		CallerConnID: &caller,
		CallerOutcome: &CallerOutcome{
			Kind:      CallerOutcomeFailed,
			RequestID: 6,
			Error:     "boom",
			Flags:     CallerOutcomeFlagNoSuccessNotify,
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d, want 1 (failure always delivered even with NoSuccessNotify)", len(mock.heavyCalls))
	}
	if mock.heavyCalls[0].Outcome.Kind != CallerOutcomeFailed {
		t.Fatalf("outcome Kind = %d, want CallerOutcomeFailed", mock.heavyCalls[0].Outcome.Kind)
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

func (s *selectiveFailSender) SendTransactionUpdateHeavy(connID types.ConnectionID, outcome CallerOutcome, callerUpdates []SubscriptionUpdate, memo *EncodingMemo) error {
	return nil
}
func (s *selectiveFailSender) SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []SubscriptionUpdate, memo *EncodingMemo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if connID == s.fail {
		return ErrSendBufferFull
	}
	s.okCount++
	return nil
}
func (s *selectiveFailSender) SendSubscriptionError(connID types.ConnectionID, subErr SubscriptionError) error {
	return nil
}

func TestFanOutWorker_PublicProtocolDefault_WaitsForDurability(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preCount := len(mock.lightCalls)
	mock.mu.Unlock()
	if preCount != 0 {
		t.Fatalf("lightCalls = %d before TxDurable, want 0", preCount)
	}

	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d after TxDurable, want 1", len(mock.lightCalls))
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

	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preCount := len(mock.lightCalls)
	mock.mu.Unlock()
	if preCount != 0 {
		t.Fatalf("lightCalls = %d before TxDurable, want 0", preCount)
	}

	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d after TxDurable, want 1", len(mock.lightCalls))
	}
}

func TestFanOutWorker_SubscriptionError_PublicProtocolDefault_WaitsForDurability(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	conn1 := cid(1)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Errors: map[types.ConnectionID][]SubscriptionError{
			conn1: {{SubscriptionID: 5, Message: "eval failed"}},
		},
	}

	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preCount := len(mock.errCalls)
	mock.mu.Unlock()
	if preCount != 0 {
		t.Fatalf("errCalls = %d before TxDurable, want 0", preCount)
	}

	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.errCalls) != 1 {
		t.Fatalf("errCalls = %d after TxDurable, want 1", len(mock.errCalls))
	}
}

// TestFanOutWorker_ConfirmedReadCallerOnly_Waits verifies Phase 1.5
// confirmed-read gating for caller-only batches: a heavy delivery with
// no non-caller fanout still waits for TxDurable before delivery.
func TestFanOutWorker_ConfirmedReadCallerOnly_Waits(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	caller := cid(1)
	w.SetConfirmedReads(caller, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	inbox <- FanOutMessage{
		TxID:          types.TxID(1),
		TxDurable:     durableCh,
		Fanout:        CommitFanout{},
		CallerConnID:  &caller,
		CallerOutcome: committedOutcome(9),
	}

	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preCount := len(mock.heavyCalls)
	mock.mu.Unlock()
	if preCount != 0 {
		t.Fatalf("heavyCalls = %d before TxDurable, want 0", preCount)
	}

	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d after TxDurable, want 1", len(mock.heavyCalls))
	}
}

func TestFanOutWorker_FastRecipientDoesNotWaitForConfirmedCaller(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	caller := cid(1)
	fast := cid(2)
	w.SetConfirmedReads(caller, true)
	w.SetConfirmedReads(fast, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			fast: {{SubscriptionID: 2, TableName: "t1"}},
		},
		CallerConnID:  &caller,
		CallerOutcome: committedOutcome(11),
	}

	time.Sleep(50 * time.Millisecond)
	mock.mu.Lock()
	preLight := len(mock.lightCalls)
	preHeavy := len(mock.heavyCalls)
	mock.mu.Unlock()
	if preLight != 1 {
		t.Fatalf("lightCalls = %d before TxDurable, want 1 for fast recipient", preLight)
	}
	if preHeavy != 0 {
		t.Fatalf("heavyCalls = %d before TxDurable, want 0 for confirmed-read caller", preHeavy)
	}

	durableCh <- types.TxID(1)

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d after TxDurable, want 1", len(mock.lightCalls))
	}
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d after TxDurable, want 1", len(mock.heavyCalls))
	}
}

func TestFanOutWorker_FastCallerOnly_DoesNotWaitForTxDurable(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	caller := cid(1)
	w.SetConfirmedReads(caller, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	inbox <- FanOutMessage{
		TxID:          types.TxID(1),
		TxDurable:     make(chan types.TxID),
		Fanout:        CommitFanout{},
		CallerConnID:  &caller,
		CallerOutcome: committedOutcome(12),
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls = %d, want 1 for fast-read caller", len(mock.heavyCalls))
	}
	if len(mock.lightCalls) != 0 {
		t.Fatalf("lightCalls = %d, want 0", len(mock.lightCalls))
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
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d, want 1 (nil TxDurable = already durable)", len(mock.lightCalls))
	}
}

func TestFanOutWorker_SetConfirmedReads_Toggle(t *testing.T) {
	w := &FanOutWorker{confirmedReads: make(map[types.ConnectionID]bool), fastReads: make(map[types.ConnectionID]bool)}
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
	w := &FanOutWorker{confirmedReads: make(map[types.ConnectionID]bool), fastReads: make(map[types.ConnectionID]bool)}
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
		CallerOutcome: committedOutcome(1),
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
	if mock.errCalls[0].Err.SubscriptionID != 5 || mock.errCalls[1].Err.SubscriptionID != 6 {
		t.Fatalf("errCalls SubIDs = %d, %d; want 5, 6", mock.errCalls[0].Err.SubscriptionID, mock.errCalls[1].Err.SubscriptionID)
	}
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d, want 1", len(mock.lightCalls))
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
		onLight: func(connID types.ConnectionID) {
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
	onLight func(types.ConnectionID)
	onErr   func(types.ConnectionID)
}

func (s *orderTrackingSender) SendTransactionUpdateHeavy(connID types.ConnectionID, outcome CallerOutcome, callerUpdates []SubscriptionUpdate, memo *EncodingMemo) error {
	return nil
}
func (s *orderTrackingSender) SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []SubscriptionUpdate, memo *EncodingMemo) error {
	if s.onLight != nil {
		s.onLight(connID)
	}
	return nil
}
func (s *orderTrackingSender) SendSubscriptionError(connID types.ConnectionID, subErr SubscriptionError) error {
	if s.onErr != nil {
		s.onErr(connID)
	}
	return nil
}

func TestFanOutWorker_Acceptance_FullFlow(t *testing.T) {
	// Full pipeline: Manager.EvalAndBroadcast → inbox → FanOutWorker → mock sender.
	mock := &mockFanOutSender{}
	fanoutCh := make(chan FanOutMessage, 64)

	s := testSchema()
	mgr := NewManager(s, s, WithFanOutInbox(fanoutCh))

	worker := NewFanOutWorker(fanoutCh, mock, mgr.DroppedChanSend())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Run(ctx)

	conn1 := cid(1)
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     conn1,
		QueryID:    10,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantSubID := mgr.querySets[conn1][10][0]

	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(42), types.NewString("alice")}},
		nil,
	)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	time.Sleep(100 * time.Millisecond)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls = %d, want 1", len(mock.lightCalls))
	}
	if mock.lightCalls[0].ConnID != conn1 {
		t.Fatalf("connID mismatch")
	}
	if len(mock.lightCalls[0].Updates) == 0 {
		t.Fatal("no updates delivered")
	}
	if mock.lightCalls[0].Updates[0].SubscriptionID != wantSubID {
		t.Fatalf("SubscriptionID = %d, want %d", mock.lightCalls[0].Updates[0].SubscriptionID, wantSubID)
	}
}
