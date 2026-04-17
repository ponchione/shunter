package protocol

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/ponchione/shunter/bsatn"
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
	mu             sync.Mutex
	calls          []senderCall
	sendErr        error
	txUpdates      []*TransactionUpdate
	reducerResults []*ReducerCallResult
	genericMsgs    []any
}

type senderCall struct {
	method string
	connID types.ConnectionID
}

func (m *mockClientSender) Send(connID types.ConnectionID, msg any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "Send", connID: connID})
	m.genericMsgs = append(m.genericMsgs, msg)
	return m.sendErr
}
func (m *mockClientSender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendTransactionUpdate", connID: connID})
	m.txUpdates = append(m.txUpdates, update)
	return m.sendErr
}
func (m *mockClientSender) SendReducerResult(connID types.ConnectionID, result *ReducerCallResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendReducerResult", connID: connID})
	m.reducerResults = append(m.reducerResults, result)
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
		nil,
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
		nil,
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
		nil,
	)
	if !errors.Is(err, subscription.ErrSendConnGone) {
		t.Fatalf("err = %v, want ErrSendConnGone", err)
	}
}

func TestFanOutSenderAdapter_RowPayloadRoundTrip(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	updates := []subscription.SubscriptionUpdate{{
		SubscriptionID: 5,
		TableName:      "players",
		Inserts: []types.ProductValue{
			{types.NewUint32(42), types.NewString("alice")},
		},
		Deletes: []types.ProductValue{
			{types.NewUint32(7), types.NewString("gone")},
		},
	}}
	if err := adapter.SendTransactionUpdate(connID(1), types.TxID(22), updates, nil); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.txUpdates) != 1 {
		t.Fatalf("txUpdates=%d want 1", len(mock.txUpdates))
	}
	got := mock.txUpdates[0]
	if len(got.Updates) != 1 {
		t.Fatalf("encoded updates=%d want 1", len(got.Updates))
	}
	checkRows := func(name string, rows []types.ProductValue, encoded []byte) {
		decoded, err := DecodeRowList(encoded)
		if err != nil {
			t.Fatalf("DecodeRowList(%s): %v", name, err)
		}
		if len(decoded) != len(rows) {
			t.Fatalf("%s rows=%d want %d", name, len(decoded), len(rows))
		}
		for i, row := range rows {
			var want bytes.Buffer
			if err := bsatn.EncodeProductValue(&want, row); err != nil {
				t.Fatalf("EncodeProductValue(%s[%d]): %v", name, i, err)
			}
			if !bytes.Equal(decoded[i], want.Bytes()) {
				t.Fatalf("%s[%d] bytes mismatch\n got=%v\nwant=%v", name, i, decoded[i], want.Bytes())
			}
		}
	}
	checkRows("inserts", updates[0].Inserts, got.Updates[0].Inserts)
	checkRows("deletes", updates[0].Deletes, got.Updates[0].Deletes)
}

func TestFanOutSenderAdapter_SendReducerResultSuccessPath(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	result := &subscription.ReducerCallResult{
		RequestID: 9,
		Status:    0,
		TxID:      types.TxID(44),
		Energy:    999,
		TransactionUpdate: []subscription.SubscriptionUpdate{{
			SubscriptionID: 1,
			TableName:      "players",
			Inserts:        []types.ProductValue{{types.NewUint32(1)}},
		}},
	}
	if err := adapter.SendReducerResult(connID(1), result, nil); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.reducerResults) != 1 {
		t.Fatalf("reducerResults=%d want 1", len(mock.reducerResults))
	}
	got := mock.reducerResults[0]
	if got.RequestID != 9 || got.TxID != 44 || got.Energy != 0 {
		t.Fatalf("encoded reducer result = %+v", got)
	}
	if len(got.TransactionUpdate) != 1 {
		t.Fatalf("TransactionUpdate len=%d want 1", len(got.TransactionUpdate))
	}
}

func TestFanOutSenderAdapter_SendSubscriptionErrorSuccessPath(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	if err := adapter.SendSubscriptionError(connID(3), 77, "boom"); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.genericMsgs) != 1 {
		t.Fatalf("genericMsgs=%d want 1", len(mock.genericMsgs))
	}
	msg, ok := mock.genericMsgs[0].(SubscriptionError)
	if !ok {
		t.Fatalf("message type = %T, want SubscriptionError", mock.genericMsgs[0])
	}
	if msg.SubscriptionID != 77 || msg.Error != "boom" {
		t.Fatalf("subscription error = %+v", msg)
	}
}

func TestFanOutSenderAdapter_MemoizesRowEncodingAcrossTransactionUpdateCalls(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	memo := subscription.NewEncodingMemo()

	sharedRows := []types.ProductValue{{types.NewUint32(42), types.NewString("shared")}}
	calls := 0
	oldEncodeRows := encodeRowsUnmemoized
	encodeRowsUnmemoized = func(rows []types.ProductValue) ([]byte, error) {
		calls++
		return oldEncodeRows(rows)
	}
	defer func() { encodeRowsUnmemoized = oldEncodeRows }()

	updates1 := []subscription.SubscriptionUpdate{{SubscriptionID: 1, TableName: "players", Inserts: sharedRows}}
	updates2 := []subscription.SubscriptionUpdate{{SubscriptionID: 2, TableName: "players", Inserts: sharedRows}}
	if err := adapter.SendTransactionUpdate(connID(1), types.TxID(55), updates1, memo); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SendTransactionUpdate(connID(2), types.TxID(55), updates2, memo); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("encodeRows calls = %d, want 1 shared binary encode", calls)
	}
}

func TestFanOutSenderAdapter_MemoCacheDoesNotLeakAcrossTransactions(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	sharedRows := []types.ProductValue{{types.NewUint32(7), types.NewString("fresh")}}

	calls := 0
	oldEncodeRows := encodeRowsUnmemoized
	encodeRowsUnmemoized = func(rows []types.ProductValue) ([]byte, error) {
		calls++
		return oldEncodeRows(rows)
	}
	defer func() { encodeRowsUnmemoized = oldEncodeRows }()

	updates := []subscription.SubscriptionUpdate{{SubscriptionID: 1, TableName: "players", Inserts: sharedRows}}
	if err := adapter.SendTransactionUpdate(connID(1), types.TxID(60), updates, subscription.NewEncodingMemo()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SendTransactionUpdate(connID(1), types.TxID(61), updates, subscription.NewEncodingMemo()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("encodeRows calls = %d, want 2 across separate delivery batches", calls)
	}
}
