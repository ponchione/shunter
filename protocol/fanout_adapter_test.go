package protocol

import (
	"bytes"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func TestEncodeSubscriptionUpdate_SingleInsertDelete(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		QueryID:   42,
		TableName: "users",
		Inserts:   []types.ProductValue{{types.NewUint32(1)}},
		Deletes:   []types.ProductValue{{types.NewUint32(2)}},
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	if pu.QueryID != 42 {
		t.Fatalf("QueryID = %d, want 42", pu.QueryID)
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

func TestEncodeSubscriptionUpdate_CarriesClientQueryID(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		SubscriptionID: 77,
		QueryID:        910,
		TableName:      "users",
		Inserts:        []types.ProductValue{{types.NewUint32(1)}},
	}
	pu, err := encodeSubscriptionUpdate(su)
	if err != nil {
		t.Fatal(err)
	}
	if field := reflect.ValueOf(pu).FieldByName("SubscriptionID"); field.IsValid() {
		t.Fatalf("protocol update still exposes internal SubscriptionID field: %+v", pu)
	}
	field := reflect.ValueOf(pu).FieldByName("QueryID")
	if !field.IsValid() {
		t.Fatalf("protocol update missing QueryID field: %+v", pu)
	}
	if got := uint32(field.Uint()); got != su.QueryID {
		t.Fatalf("QueryID = %d, want %d", got, su.QueryID)
	}
}

func TestEncodeSubscriptionUpdate_Empty(t *testing.T) {
	su := subscription.SubscriptionUpdate{
		QueryID:   1,
		TableName: "empty",
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
		QueryID:   7,
		TableName: "data",
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

// --- Adapter integration tests (outcome-model envelope split) ---

type mockClientSender struct {
	mu          sync.Mutex
	calls       []senderCall
	sendErr     error
	heavyCalls  []*TransactionUpdate
	lightCalls  []*TransactionUpdateLight
	genericMsgs []any
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
	m.heavyCalls = append(m.heavyCalls, update)
	return m.sendErr
}
func (m *mockClientSender) SendTransactionUpdateLight(connID types.ConnectionID, update *TransactionUpdateLight) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, senderCall{method: "SendTransactionUpdateLight", connID: connID})
	m.lightCalls = append(m.lightCalls, update)
	return m.sendErr
}

func connID(b byte) types.ConnectionID {
	var id types.ConnectionID
	id[0] = b
	return id
}

func TestFanOutSenderAdapter_SendTransactionUpdateLight(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	err := adapter.SendTransactionUpdateLight(
		connID(1), 11,
		[]subscription.SubscriptionUpdate{{
			QueryID:   5,
			TableName: "t1",
			Inserts:   []types.ProductValue{{types.NewUint32(42)}},
		}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.calls) != 1 || mock.calls[0].method != "SendTransactionUpdateLight" {
		t.Fatalf("calls = %+v, want 1 SendTransactionUpdateLight", mock.calls)
	}
	if mock.lightCalls[0].RequestID != 11 {
		t.Fatalf("RequestID = %d, want 11", mock.lightCalls[0].RequestID)
	}
}

func TestFanOutSenderAdapter_BufferFull_MapsError(t *testing.T) {
	mock := &mockClientSender{sendErr: ErrClientBufferFull}
	adapter := NewFanOutSenderAdapter(mock)
	err := adapter.SendTransactionUpdateLight(
		connID(1), 1,
		[]subscription.SubscriptionUpdate{{QueryID: 1, TableName: "t"}},
		nil,
	)
	if !errors.Is(err, subscription.ErrSendBufferFull) {
		t.Fatalf("err = %v, want ErrSendBufferFull", err)
	}
}

func TestFanOutSenderAdapter_ConnNotFound_MapsError(t *testing.T) {
	mock := &mockClientSender{sendErr: ErrConnNotFound}
	adapter := NewFanOutSenderAdapter(mock)
	err := adapter.SendTransactionUpdateLight(
		connID(1), 1,
		[]subscription.SubscriptionUpdate{{QueryID: 1, TableName: "t"}},
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
		QueryID:   5,
		TableName: "players",
		Inserts: []types.ProductValue{
			{types.NewUint32(42), types.NewString("alice")},
		},
		Deletes: []types.ProductValue{
			{types.NewUint32(7), types.NewString("gone")},
		},
	}}
	if err := adapter.SendTransactionUpdateLight(connID(1), 22, updates, nil); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.lightCalls) != 1 {
		t.Fatalf("lightCalls=%d want 1", len(mock.lightCalls))
	}
	got := mock.lightCalls[0]
	if len(got.Update) != 1 {
		t.Fatalf("encoded updates=%d want 1", len(got.Update))
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
	checkRows("inserts", updates[0].Inserts, got.Update[0].Inserts)
	checkRows("deletes", updates[0].Deletes, got.Update[0].Deletes)
}

func TestFanOutSenderAdapter_SendTransactionUpdateHeavyCommitted(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	outcome := subscription.CallerOutcome{
		Kind:      subscription.CallerOutcomeCommitted,
		RequestID: 9,
	}
	callerUpdates := []subscription.SubscriptionUpdate{{
		QueryID:   1,
		TableName: "players",
		Inserts:   []types.ProductValue{{types.NewUint32(1)}},
	}}
	if err := adapter.SendTransactionUpdateHeavy(connID(1), outcome, callerUpdates, nil); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls=%d want 1", len(mock.heavyCalls))
	}
	got := mock.heavyCalls[0]
	committed, ok := got.Status.(StatusCommitted)
	if !ok {
		t.Fatalf("Status = %T, want StatusCommitted", got.Status)
	}
	if len(committed.Update) != 1 {
		t.Fatalf("committed updates=%d want 1", len(committed.Update))
	}
	if got.ReducerCall.RequestID != 9 {
		t.Fatalf("ReducerCall.RequestID = %d, want 9", got.ReducerCall.RequestID)
	}
}

func TestFanOutSenderAdapter_SendTransactionUpdateHeavyFailed(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	outcome := subscription.CallerOutcome{
		Kind:      subscription.CallerOutcomeFailed,
		RequestID: 3,
		Error:     "panic",
	}
	if err := adapter.SendTransactionUpdateHeavy(connID(1), outcome, nil, nil); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.heavyCalls) != 1 {
		t.Fatalf("heavyCalls=%d want 1", len(mock.heavyCalls))
	}
	failed, ok := mock.heavyCalls[0].Status.(StatusFailed)
	if !ok {
		t.Fatalf("Status = %T, want StatusFailed", mock.heavyCalls[0].Status)
	}
	if failed.Error != "panic" {
		t.Fatalf("StatusFailed.Error = %q, want %q", failed.Error, "panic")
	}
}

// TestFanOutSenderAdapter_SendSubscriptionErrorTransactionOriginClearsIDs
// pins the reference-informed behavior for post-commit evaluation errors.
// The fan-out adapter is only invoked for errors routed through
// FanOutMessage.Errors, which originate from
// `subscription/eval.go::handleEvalError` during a TransactionUpdate
// re-eval. Reference
// `core/src/subscription/module_subscription_manager.rs:1998-2010`
// emits `SubscriptionError { request_id: None, query_id: None, ... }`
// in this exact case, and `core/src/client/messages.rs:622-629`
// propagates the Options straight through. Per-connection diagnostics
// (`subscription.SubscriptionError.RequestID`/`QueryID`) are
// internal logging state; they must not appear on the wire.
func TestFanOutSenderAdapter_SendSubscriptionErrorTransactionOriginClearsIDs(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	if err := adapter.SendSubscriptionError(connID(3), subscription.SubscriptionError{RequestID: 55, SubscriptionID: 77, Message: "boom"}); err != nil {
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
	if msg.RequestID != nil {
		t.Fatalf("RequestID = %v, want nil (TransactionUpdate-origin per reference)", *msg.RequestID)
	}
	if msg.QueryID != nil {
		t.Fatalf("QueryID = %v, want nil (TransactionUpdate-origin per reference)", *msg.QueryID)
	}
	if msg.TableID != nil {
		t.Fatalf("TableID = %v, want nil (TransactionUpdate-origin per reference)", *msg.TableID)
	}
	if msg.Error != "boom" {
		t.Fatalf("Error = %q, want %q", msg.Error, "boom")
	}
}

func TestFanOutSenderAdapter_MemoizesRowEncodingAcrossLightCalls(t *testing.T) {
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

	updates1 := []subscription.SubscriptionUpdate{{QueryID: 1, TableName: "players", Inserts: sharedRows}}
	updates2 := []subscription.SubscriptionUpdate{{QueryID: 2, TableName: "players", Inserts: sharedRows}}
	if err := adapter.SendTransactionUpdateLight(connID(1), 55, updates1, memo); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SendTransactionUpdateLight(connID(2), 55, updates2, memo); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("encodeRows calls = %d, want 1 shared binary encode", calls)
	}
}

func TestFanOutSenderAdapter_MemoizesClonedOuterRowLists(t *testing.T) {
	mock := &mockClientSender{}
	adapter := NewFanOutSenderAdapter(mock)
	memo := subscription.NewEncodingMemo()

	sharedRows := []types.ProductValue{{types.NewUint32(42), types.NewString("shared")}}
	clonedRows := append([]types.ProductValue(nil), sharedRows...)
	calls := 0
	oldEncodeRows := encodeRowsUnmemoized
	encodeRowsUnmemoized = func(rows []types.ProductValue) ([]byte, error) {
		calls++
		return oldEncodeRows(rows)
	}
	defer func() { encodeRowsUnmemoized = oldEncodeRows }()

	updates1 := []subscription.SubscriptionUpdate{{QueryID: 1, TableName: "players", Inserts: sharedRows}}
	updates2 := []subscription.SubscriptionUpdate{{QueryID: 2, TableName: "players", Inserts: clonedRows}}
	if err := adapter.SendTransactionUpdateLight(connID(1), 55, updates1, memo); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SendTransactionUpdateLight(connID(2), 55, updates2, memo); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("encodeRows calls = %d, want 1 shared binary encode for cloned outer rows", calls)
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

	updates := []subscription.SubscriptionUpdate{{QueryID: 1, TableName: "players", Inserts: sharedRows}}
	if err := adapter.SendTransactionUpdateLight(connID(1), 60, updates, subscription.NewEncodingMemo()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SendTransactionUpdateLight(connID(1), 61, updates, subscription.NewEncodingMemo()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("encodeRows calls = %d, want 2 across separate delivery batches", calls)
	}
}
