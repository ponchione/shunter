package protocol

import (
	"context"
	"errors"
	"testing"

	"github.com/ponchione/shunter/schema"
)

// TestParitySubscribeSingleDurationNonZeroOnCompileFail pins that the
// receipt-timestamp seam populates `TotalHostExecutionDurationMicros`
// with a measured value on the handler-short-circuit path
// (compile-error before dispatch). Reference field semantics: duration
// reflects the full admission time; a deferred-measurement 0 is no
// longer legal on any admission-origin emit site. Pin: non-zero.
func TestParitySubscribeSingleDurationNonZeroOnCompileFail(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1) // only "users" exists

	msg := &SubscribeSingleMsg{
		RequestID:   5,
		QueryID:     99,
		QueryString: "SELECT * FROM nonexistent",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero (receipt seam wired)")
	}
}

// TestParitySubscribeSingleDurationNonZeroOnSubmitFail pins the same
// contract on the executor-unavailable short-circuit path.
func TestParitySubscribeSingleDurationNonZeroOnSubmitFail(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{registerSetErr: errors.New("queue full")}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	msg := &SubscribeSingleMsg{RequestID: 3, QueryID: 50, QueryString: "SELECT * FROM users"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on submit-fail path")
	}
}

// TestParitySubscribeMultiDurationNonZeroOnCompileFail pins the
// receipt-timestamp seam on handleSubscribeMulti's compile-short-circuit.
func TestParitySubscribeMultiDurationNonZeroOnCompileFail(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1)
	msg := &SubscribeMultiMsg{
		RequestID:    9,
		QueryID:      88,
		QueryStrings: []string{"SELECT * FROM nonexistent"},
	}
	handleSubscribeMulti(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on multi compile-fail path")
	}
}

// TestParitySubscribeMultiDurationNonZeroOnSubmitFail pins the same
// contract on the multi executor-unavailable short-circuit path.
func TestParitySubscribeMultiDurationNonZeroOnSubmitFail(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{registerSetErr: errors.New("queue full")}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	msg := &SubscribeMultiMsg{RequestID: 4, QueryID: 60, QueryStrings: []string{"SELECT * FROM users"}}
	handleSubscribeMulti(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on multi submit-fail path")
	}
}

// TestParityUnsubscribeSingleDurationNonZeroOnSubmitFail pins the seam
// on handleUnsubscribeSingle's executor-unavailable short-circuit.
func TestParityUnsubscribeSingleDurationNonZeroOnSubmitFail(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{unregisterSetErr: errors.New("db down")}
	msg := &UnsubscribeSingleMsg{RequestID: 4, QueryID: 7}
	handleUnsubscribeSingle(context.Background(), conn, msg, exec)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on unsubscribe-single submit-fail path")
	}
}

// TestParityUnsubscribeMultiDurationNonZeroOnSubmitFail pins the same
// contract on handleUnsubscribeMulti.
func TestParityUnsubscribeMultiDurationNonZeroOnSubmitFail(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{unregisterSetErr: errors.New("db down")}
	msg := &UnsubscribeMultiMsg{RequestID: 24, QueryID: 99}
	handleUnsubscribeMulti(context.Background(), conn, msg, exec)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on unsubscribe-multi submit-fail path")
	}
}
