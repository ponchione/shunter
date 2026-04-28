package subscription

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestCanonicalReducerDeliveryFlow pins the canonical reducer-delivery flow:
// connect, subscribe, commit a reducer transaction, deliver the caller's heavy
// outcome, and deliver non-caller light updates after confirmed-read durability.
func TestCanonicalReducerDeliveryFlow(t *testing.T) {
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

	assertMockCounts(t, mock, "before confirmed-read durability", 0, 0, 0)

	durableCh <- types.TxID(44)
	waitForMockCounts(t, mock, "confirmed-read durability", 1, 1, 0)

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
