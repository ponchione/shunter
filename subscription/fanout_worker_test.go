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
	changed    chan struct{}
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
	m.signalLocked()
	return m.sendErr
}
func (m *mockFanOutSender) SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []SubscriptionUpdate, memo *EncodingMemo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lightCalls = append(m.lightCalls, lightCall{ConnID: connID, RequestID: requestID, Updates: updates})
	m.signalLocked()
	return m.sendErr
}
func (m *mockFanOutSender) SendSubscriptionError(connID types.ConnectionID, subErr SubscriptionError) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errCalls = append(m.errCalls, errCall{ConnID: connID, Err: subErr})
	m.signalLocked()
	return m.sendErr
}

func (m *mockFanOutSender) signalLocked() {
	if m.changed != nil {
		close(m.changed)
		m.changed = make(chan struct{})
	}
}

func (m *mockFanOutSender) waitChLocked() <-chan struct{} {
	if m.changed == nil {
		m.changed = make(chan struct{})
	}
	return m.changed
}

func waitForMockCounts(t *testing.T, mock *mockFanOutSender, label string, wantLight, wantHeavy, wantErr int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		mock.mu.Lock()
		gotLight := len(mock.lightCalls)
		gotHeavy := len(mock.heavyCalls)
		gotErr := len(mock.errCalls)
		if gotLight == wantLight && gotHeavy == wantHeavy && gotErr == wantErr {
			mock.mu.Unlock()
			return
		}
		ch := mock.waitChLocked()
		mock.mu.Unlock()

		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("%s delivery counts: light=%d heavy=%d err=%d, want light=%d heavy=%d err=%d",
				label, gotLight, gotHeavy, gotErr, wantLight, wantHeavy, wantErr)
		}
	}
}

func assertMockCounts(t *testing.T, mock *mockFanOutSender, label string, wantLight, wantHeavy, wantErr int) {
	t.Helper()
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.lightCalls) != wantLight || len(mock.heavyCalls) != wantHeavy || len(mock.errCalls) != wantErr {
		t.Fatalf("%s delivery counts: light=%d heavy=%d err=%d, want light=%d heavy=%d err=%d",
			label, len(mock.lightCalls), len(mock.heavyCalls), len(mock.errCalls), wantLight, wantHeavy, wantErr)
	}
}

func waitForFanOutWorkerExit(t *testing.T, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s: worker did not exit", label)
	}
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

	waitForMockCounts(t, mock, "non-caller delivery", 2, 0, 0)
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
	inbox := make(chan FanOutMessage)
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

	sent := make(chan struct{})
	go func() {
		inbox <- FanOutMessage{
			TxID:      types.TxID(1),
			TxDurable: make(chan types.TxID),
			Fanout: CommitFanout{
				conn1: {{SubscriptionID: 1, TableName: "t1"}},
			},
		}
		close(sent)
	}()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("worker did not receive TxDurable-gated message")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit while waiting on TxDurable")
	}
}

func TestFanOutWorker_ClosedTxDurableSkipsBlockedMessageAndContinues(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 2)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)
	conn1 := cid(1)
	w.SetConfirmedReads(conn1, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	failedDurable := make(chan types.TxID)
	close(failedDurable)
	inbox <- FanOutMessage{
		TxID:      types.TxID(1),
		TxDurable: failedDurable,
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "blocked"}},
		},
	}
	inbox <- FanOutMessage{
		TxID: types.TxID(2),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 2, TableName: "after"}},
		},
	}

	waitForMockCounts(t, mock, "post-failed-durable delivery", 1, 0, 0)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.lightCalls[0].Updates[0].TableName != "after" {
		t.Fatalf("delivered table = %q, want after", mock.lightCalls[0].Updates[0].TableName)
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

// TestFanOutWorker_CallerDiversion verifies the outcome-model split: caller
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

	waitForMockCounts(t, mock, "caller diversion", 1, 1, 0)
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
// preserved for other reducers. outcome-model collapses all failure modes
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

	waitForMockCounts(t, mock, "failed caller diversion", 0, 1, 0)
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

// TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout pins that even when
// no subscriptions are active and the fanout is empty, a reducer-originated
// commit must still deliver the caller's heavy envelope so the caller observes
// its outcome.
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

	waitForMockCounts(t, mock, "empty fanout caller heavy", 0, 1, 0)
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
// pins the outcome-model contract: when the caller set
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

	waitForMockCounts(t, mock, "NoSuccessNotify non-caller delivery", 1, 0, 0)
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
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

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

	close(inbox)
	waitForFanOutWorkerExit(t, done, "NoSuccessNotify empty fanout")

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

	waitForMockCounts(t, mock, "NoSuccessNotify failure delivery", 0, 1, 0)
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

func TestFanOutWorker_BufferFullClearsFastReadState(t *testing.T) {
	conn1 := cid(1)
	mock := &mockFanOutSender{sendErr: ErrSendBufferFull}
	dropped := make(chan types.ConnectionID, 1)
	w := NewFanOutWorker(nil, mock, dropped)
	w.SetConfirmedReads(conn1, false)
	if w.requiresConfirmedRead(conn1) {
		t.Fatal("test setup failed: expected fast-read policy before drop")
	}

	w.deliver(context.Background(), FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			conn1: {{SubscriptionID: 1, TableName: "t1"}},
		},
	})

	if _, ok := w.fastReads[conn1]; ok {
		t.Fatal("buffer-full drop left stale fast-read state")
	}
	if _, ok := w.confirmedReads[conn1]; ok {
		t.Fatal("buffer-full drop left stale confirmed-read state")
	}
	if !w.requiresConfirmedRead(conn1) {
		t.Fatal("dropped client should fall back to default confirmed-read policy if reused")
	}
}

func TestFanOutWorker_ConnGone_Silent(t *testing.T) {
	mock := &mockFanOutSender{sendErr: ErrSendConnGone}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	inbox <- FanOutMessage{
		TxID: types.TxID(1),
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	}

	close(inbox)
	waitForFanOutWorkerExit(t, done, "conn-gone delivery")

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

	waitForSelectiveOK(t, sender, 2)
}

// selectiveFailSender fails with ErrSendBufferFull for a specific connID.
type selectiveFailSender struct {
	mu      sync.Mutex
	changed chan struct{}
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
	s.signalLocked()
	return nil
}
func (s *selectiveFailSender) SendSubscriptionError(connID types.ConnectionID, subErr SubscriptionError) error {
	return nil
}

func (s *selectiveFailSender) signalLocked() {
	if s.changed != nil {
		close(s.changed)
		s.changed = make(chan struct{})
	}
}

func (s *selectiveFailSender) waitChLocked() <-chan struct{} {
	if s.changed == nil {
		s.changed = make(chan struct{})
	}
	return s.changed
}

func waitForSelectiveOK(t *testing.T, sender *selectiveFailSender, want int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		sender.mu.Lock()
		got := sender.okCount
		if got >= want {
			sender.mu.Unlock()
			return
		}
		ch := sender.waitChLocked()
		sender.mu.Unlock()

		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("okCount = %d, want >= %d", got, want)
		}
	}
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

	assertMockCounts(t, mock, "before TxDurable", 0, 0, 0)

	durableCh <- types.TxID(1)

	waitForMockCounts(t, mock, "after TxDurable", 1, 0, 0)
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

	assertMockCounts(t, mock, "before TxDurable", 0, 0, 0)

	durableCh <- types.TxID(1)

	waitForMockCounts(t, mock, "after TxDurable", 1, 0, 0)
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

	assertMockCounts(t, mock, "before TxDurable", 0, 0, 0)

	durableCh <- types.TxID(1)

	waitForMockCounts(t, mock, "after TxDurable", 0, 0, 1)
	cancel()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.errCalls) != 1 {
		t.Fatalf("errCalls = %d after TxDurable, want 1", len(mock.errCalls))
	}
}

// TestFanOutWorker_ConfirmedReadCallerOnly_Waits verifies outcome-model
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

	assertMockCounts(t, mock, "before TxDurable", 0, 0, 0)

	durableCh <- types.TxID(1)

	waitForMockCounts(t, mock, "after TxDurable", 0, 1, 0)
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

	waitForMockCounts(t, mock, "fast recipient before TxDurable", 1, 0, 0)

	durableCh <- types.TxID(1)

	waitForMockCounts(t, mock, "confirmed caller after TxDurable", 1, 1, 0)
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

	waitForMockCounts(t, mock, "fast caller-only delivery", 0, 1, 0)
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

	waitForMockCounts(t, mock, "nil TxDurable delivery", 1, 0, 0)
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
	ready := make(chan types.TxID, 1)
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
	ready <- 2
	close(ready)
}

func TestFanOutWorker_ConcurrentPolicyChurnShortSoak(t *testing.T) {
	const (
		seed       = int64(20260506)
		workers    = 3
		iterations = 64
	)
	mock := &mockFanOutSender{}
	w := NewFanOutWorker(nil, mock, make(chan types.ConnectionID, 16))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connIDs := []types.ConnectionID{cid(1), cid(2), cid(3), cid(4)}
	caller := cid(9)
	ready := make(chan types.TxID, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.deliver(ctx, FanOutMessage{
			TxID:          7,
			TxDurable:     ready,
			CallerConnID:  &caller,
			CallerOutcome: committedOutcome(700),
			Fanout: CommitFanout{
				connIDs[0]: {{SubscriptionID: 10, TableName: "players"}},
				connIDs[1]: {{SubscriptionID: 11, TableName: "players"}},
				connIDs[2]: {{SubscriptionID: 12, TableName: "players"}},
				connIDs[3]: {{SubscriptionID: 13, TableName: "players"}},
				caller:     {{SubscriptionID: 99, TableName: "players"}},
			},
		})
	}()

	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			for op := 0; op < iterations; op++ {
				conn := connIDs[(int(seed)+worker+op)%len(connIDs)]
				w.SetConfirmedReads(conn, (op+worker)%3 != 0)
				if op%5 == 0 {
					w.RemoveClient(connIDs[(worker+op/5)%len(connIDs)])
				}
			}
		}()
	}
	wg.Wait()
	ready <- 7
	close(ready)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("seed %d policy churn delivery did not finish after %d workers x %d ops", seed, workers, iterations)
	}
	assertMockCounts(t, mock, "seed 20260506 policy churn", len(connIDs), 1, 0)
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

	waitForMockCounts(t, mock, "subscription error delivery", 1, 0, 2)
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
	changed := make(chan struct{})
	signal := func() {
		close(changed)
		changed = make(chan struct{})
	}

	sender := &orderTrackingSender{
		onLight: func(connID types.ConnectionID) {
			mu.Lock()
			order = append(order, call{kind: "tx", conn: connID})
			signal()
			mu.Unlock()
		},
		onErr: func(connID types.ConnectionID) {
			mu.Lock()
			order = append(order, call{kind: "err", conn: connID})
			signal()
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

	deadline := time.After(time.Second)
	for {
		mu.Lock()
		if len(order) >= 2 {
			mu.Unlock()
			break
		}
		ch := changed
		mu.Unlock()

		select {
		case <-ch:
		case <-deadline:
			mu.Lock()
			got := len(order)
			mu.Unlock()
			t.Fatalf("order len = %d, want >= 2", got)
		}
	}
	cancel()

	mu.Lock()
	defer mu.Unlock()
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

func TestFanOutWorker_FullFlow(t *testing.T) {
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

	waitForMockCounts(t, mock, "manager fanout delivery", 1, 0, 0)
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
