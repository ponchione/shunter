package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func waitForPhase0Delivery(t *testing.T, mock *mockFanOutSender, wantRes, wantTx int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mock.mu.Lock()
		gotRes := len(mock.resCalls)
		gotTx := len(mock.txCalls)
		mock.mu.Unlock()
		if gotRes == wantRes && gotTx == wantTx {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	t.Fatalf("timed out waiting for delivery counts: got res=%d tx=%d, want res=%d tx=%d", len(mock.resCalls), len(mock.txCalls), wantRes, wantTx)
}

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
	callerResult := &ReducerCallResult{
		RequestID: 77,
		Status:    0,
		TxID:      types.TxID(44),
	}
	inbox <- FanOutMessage{
		TxID:      types.TxID(44),
		TxDurable: durableCh,
		Fanout: CommitFanout{
			caller: {{SubscriptionID: 1, TableName: "widgets", Inserts: []types.ProductValue{{types.NewUint32(10)}}}},
			other:  {{SubscriptionID: 2, TableName: "widgets", Inserts: []types.ProductValue{{types.NewUint32(20)}}}},
		},
		CallerConnID: &caller,
		CallerResult: callerResult,
	}

	time.Sleep(30 * time.Millisecond)
	mock.mu.Lock()
	if len(mock.resCalls) != 0 || len(mock.txCalls) != 0 {
		mock.mu.Unlock()
		t.Fatalf("delivery should wait for confirmed-read durability; got res=%d tx=%d before durable", len(mock.resCalls), len(mock.txCalls))
	}
	mock.mu.Unlock()

	durableCh <- types.TxID(44)
	waitForPhase0Delivery(t, mock, 1, 1)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.resCalls[0].ConnID != caller {
		t.Fatalf("caller result connID = %x, want caller %x", mock.resCalls[0].ConnID, caller)
	}
	if mock.resCalls[0].Result.RequestID != 77 || mock.resCalls[0].Result.TxID != 44 {
		t.Fatalf("caller result = %+v, want request_id=77 tx_id=44", *mock.resCalls[0].Result)
	}
	if len(mock.resCalls[0].Result.TransactionUpdate) != 1 {
		t.Fatalf("caller embedded updates = %d, want 1", len(mock.resCalls[0].Result.TransactionUpdate))
	}
	if mock.resCalls[0].Result.TransactionUpdate[0].SubscriptionID != 1 {
		t.Fatalf("caller embedded sub_id = %d, want 1", mock.resCalls[0].Result.TransactionUpdate[0].SubscriptionID)
	}

	if mock.txCalls[0].ConnID != other {
		t.Fatalf("non-caller update connID = %x, want other %x", mock.txCalls[0].ConnID, other)
	}
	if mock.txCalls[0].TxID != 44 {
		t.Fatalf("non-caller tx_id = %d, want 44", mock.txCalls[0].TxID)
	}
	if len(mock.txCalls[0].Updates) != 1 {
		t.Fatalf("non-caller updates len = %d, want 1", len(mock.txCalls[0].Updates))
	}
	if mock.txCalls[0].Updates[0].SubscriptionID != 2 {
		t.Fatalf("non-caller sub_id = %d, want 2", mock.txCalls[0].Updates[0].SubscriptionID)
	}
}
