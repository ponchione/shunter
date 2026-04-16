package protocol

import (
	"errors"
	"sync"
	"testing"

	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func TestEncodeSubscriptionUpdate_SingleInsertDelete(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 42,
		TableName:      "users",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
		Deletes:        []types.ProductValue{{types.NewUint32(2)}},
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	if pu.SubscriptionID != 42 {
		t.Fatalf("SubscriptionID = %d, want 42", pu.SubscriptionID)
	}
	if pu.TableName != "users" {
		t.Fatalf("TableName = %q, want %q", pu.TableName, "users")
	}
	insRows, err := DecodeRowList(pu.Inserts)
	if err != nil {
		t.Fatal(err)
	}
	if len(insRows) != 1 {
		t.Fatalf("Inserts row count = %d, want 1", len(insRows))
	}
	delRows, err := DecodeRowList(pu.Deletes)
	if err != nil {
		t.Fatal(err)
	}
	if len(delRows) != 1 {
		t.Fatalf("Deletes row count = %d, want 1", len(delRows))
	}
}

func TestEncodeSubscriptionUpdate_Empty(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 1,
		TableName:      "empty",
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	insRows, err := DecodeRowList(pu.Inserts)
	if err != nil {
		t.Fatal(err)
	}
	if len(insRows) != 0 {
		t.Fatalf("Inserts row count = %d, want 0", len(insRows))
	}
}

func TestEncodeSubscriptionUpdate_MultiRow(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 7,
		TableName:      "data",
		Inserts: []types.ProductValue{
			{types.NewUint32(10), types.NewString("alice")},
			{types.NewUint32(20), types.NewString("bob")},
			{types.NewUint32(30), types.NewString("carol")},
		},
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	insRows, err := DecodeRowList(pu.Inserts)
	if err != nil {
		t.Fatal(err)
	}
	if len(insRows) != 3 {
		t.Fatalf("Inserts row count = %d, want 3", len(insRows))
	}
}

func TestEncodeReducerCallResult_Committed(t *testing.T) {
	sr := &subscription.ReducerCallResult{
		RequestID: 5,
		Status:    0,
		TxID:      types.TxID(100),
		Error:     "",
		Energy:    999,
	}
	callerUpdates := []subscription.SubscriptionUpdate{{
		SubscriptionID: 1,
		TableName:      "t1",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
	}}
	pr, err := encodeReducerCallResult(sr, callerUpdates)
	if err != nil {
		t.Fatal(err)
	}
	if pr.RequestID != 5 {
		t.Fatalf("RequestID = %d, want 5", pr.RequestID)
	}
	if pr.Status != 0 {
		t.Fatalf("Status = %d, want 0", pr.Status)
	}
	if pr.TxID != 100 {
		t.Fatalf("TxID = %d, want 100", pr.TxID)
	}
	if pr.Energy != 0 {
		t.Fatalf("Energy = %d, want 0 (v1)", pr.Energy)
	}
	if len(pr.TransactionUpdate) != 1 {
		t.Fatalf("TransactionUpdate len = %d, want 1", len(pr.TransactionUpdate))
	}
}

func TestEncodeReducerCallResult_Failed(t *testing.T) {
	sr := &subscription.ReducerCallResult{
		RequestID: 3,
		Status:    1,
		TxID:      types.TxID(50),
		Error:     "panic",
	}
	callerUpdates := []subscription.SubscriptionUpdate{{
		SubscriptionID: 1,
		TableName:      "t1",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
	}}
	pr, err := encodeReducerCallResult(sr, callerUpdates)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != 1 {
		t.Fatalf("Status = %d, want 1", pr.Status)
	}
	if pr.Error != "panic" {
		t.Fatalf("Error = %q, want %q", pr.Error, "panic")
	}
	if len(pr.TransactionUpdate) != 0 {
		t.Fatalf("TransactionUpdate len = %d, want 0 for failed status", len(pr.TransactionUpdate))
	}
}

// --- Adapter integration tests ---

type mockClientSender struct {
	mu      sync.Mutex
	calls   []senderCall
	sendErr error
}

type senderCall struct {
	method string
	connID types.ConnectionID
}

func (m *mockClientSender) Send(connID types.ConnectionID, msg any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "Send", connID: connID})
	return m.sendErr
}
func (m *mockClientSender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendTransactionUpdate", connID: connID})
	return m.sendErr
}
func (m *mockClientSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendReducerResult", connID: connID})
	return m.sendErr
}

func connID(b byte) types.ConnectionID {
	var id types.ConnectionID
	id[0] = b
	return id
}

func TestFanOutSenderAdapter_SendTransactionUpdate(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	err := adapter.SendTransactionUpdate(
		connID(1), types.TxID(100),
		[]subscription.SubscriptionUpdate{{
			SubscriptionID: 5,
			TableName:      "t1",
			Inserts:        []types.ProductValue{{types.NewUint32(42)}},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.calls) != 1 || mock.calls[0].method != "SendTransactionUpdate" {
		t.Fatalf("calls = %+v, want 1 SendTransactionUpdate", mock.calls)
	}
}

func TestFanOutSenderAdapter_BufferFull_MapsError(t *testing.T) {
	mock := &mockClientSender{sendErr: ErrClientBufferFull}
	adapter := NewFanOutSenderAdapter(mock)
	err := adapter.SendTransactionUpdate(
		connID(1), types.TxID(1),
		[]subscription.SubscriptionUpdate{{SubscriptionID: 1, TableName: "t"}},
	)
	if !errors.Is(err, subscription.ErrSendBufferFull) {
		t.Fatalf("err = %v, want ErrSendBufferFull", err)
	}
}

func TestFanOutSenderAdapter_ConnNotFound_MapsError(t *testing.T) {
	mock := &mockClientSender{sendErr: ErrConnNotFound}
	adapter := NewFanOutSenderAdapter(mock)
	err := adapter.SendTransactionUpdate(
		connID(1), types.TxID(1),
		[]subscription.SubscriptionUpdate{{SubscriptionID: 1, TableName: "t"}},
	)
	if !errors.Is(err, subscription.ErrSendConnGone) {
		t.Fatalf("err = %v, want ErrSendConnGone", err)
	}
}
