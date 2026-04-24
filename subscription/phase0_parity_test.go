package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func waitForPhase0Delivery(t *testing.T, mock *mockFanOutSender, wantHeavy, wantLight int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mock.mu.Lock()
		gotH := len(mock.heavyCalls)
		gotL := len(mock.lightCalls)
		mock.mu.Unlock()
		if gotH == wantHeavy && gotL == wantLight {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	t.Fatalf("timed out waiting for delivery counts: got heavy=%d light=%d, want heavy=%d light=%d",
		len(mock.heavyCalls), len(mock.lightCalls), wantHeavy, wantLight)
}

// TestPhase0ParityCanonicalReducerDeliveryFlow is the `P0-DELIVERY-001`
// scenario lock — connect → subscribe → reducer → caller heavy →
// non-caller light, with confirmed-read gating on TxDurable. Phase 1.5
// flipped the caller-side envelope from `ReducerCallResult` to the
// reference heavy `TransactionUpdate` (see
// `docs/parity-decisions.md#outcome-model`).
func TestPhase0ParityCanonicalReducerDeliveryFlow(t *testing.T) {
	mock := &mockFanOutSender{}
	inbox := make(chan FanOutMessage, 1)
	dropped := make(chan types.ConnectionID, 64)
	w := NewFanOutWorker(inbox, mock, dropped)

	caller, other := cid(1), cid(2)
	w.SetConfirmedReads(caller, true)
	w.SetConfirmedReads(other, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	durableCh := make(chan types.TxID, 1)
	inbox <- FanOutMessage{
		TxID:      types.TxID(44),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "widgets", Inserts: []types.ProductValue{{types.NewUint32(10)}}}},
			other:  {{SubscriptionID: 2, TableName: "widgets", Inserts: []types.ProductValue{{types.NewUint32(20)}}}},
		},
		CallerConnID:  &caller,
		CallerOutcome: committedOutcome(77),
	}

	time.Sleep(30 * time.Millisecond)
	mock.mu.Lock()
	if len(mock.heavyCalls) != 0 || len(mock.lightCalls) != 0 {
		mock.mu.Unlock()
		t.Fatalf("delivery should wait for confirmed-read durability; got heavy=%d light=%d before durable",
			len(mock.heavyCalls), len(mock.lightCalls))
	}
	mock.mu.Unlock()

	durableCh <- types.TxID(44)
	waitForPhase0Delivery(t, mock, 1, 1)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.heavyCalls[0].ConnID != caller {
		t.Fatalf("caller heavy connID = %x, want caller %x", mock.heavyCalls[0].ConnID, caller)
	}
	if mock.heavyCalls[0].Outcome.RequestID != 77 {
		t.Fatalf("caller heavy RequestID = %d, want 77", mock.heavyCalls[0].Outcome.RequestID)
	}
	if mock.heavyCalls[0].Outcome.Kind != CallerOutcomeCommitted {
		t.Fatalf("caller heavy Kind = %d, want CallerOutcomeCommitted", mock.heavyCalls[0].Outcome.Kind)
	}
	if len(mock.heavyCalls[0].CallerUpdates) != 1 {
		t.Fatalf("caller embedded updates = %d, want 1", len(mock.heavyCalls[0].CallerUpdates))
	}
	if mock.heavyCalls[0].CallerUpdates[0].SubscriptionID != 1 {
		t.Fatalf("caller embedded sub_id = %d, want 1", mock.heavyCalls[0].CallerUpdates[0].SubscriptionID)
	}

	if mock.lightCalls[0].ConnID != other {
		t.Fatalf("non-caller light connID = %x, want other %x", mock.lightCalls[0].ConnID, other)
	}
	if mock.lightCalls[0].RequestID != 77 {
		t.Fatalf("non-caller light RequestID = %d, want 77 (propagated from caller outcome)", mock.lightCalls[0].RequestID)
	}
	if len(mock.lightCalls[0].Updates) != 1 {
		t.Fatalf("non-caller updates len = %d, want 1", len(mock.lightCalls[0].Updates))
	}
	if mock.lightCalls[0].Updates[0].SubscriptionID != 2 {
		t.Fatalf("non-caller sub_id = %d, want 2", mock.lightCalls[0].Updates[0].SubscriptionID)
	}
}
