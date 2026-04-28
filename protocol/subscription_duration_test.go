package protocol

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
)

// TestShunterSubscribeSingleDurationNonZeroOnCompileFail pins that the
// receipt-timestamp seam populates `TotalHostExecutionDurationMicros`
// with a measured value on the handler-short-circuit path
// (compile-error before dispatch). Reference field semantics: duration
// reflects the full admission time; a deferred-measurement 0 is no
// longer legal on any admission-origin emit site. Pin: non-zero.
func TestShunterSubscribeSingleDurationNonZeroOnCompileFail(t *testing.T) {
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

// TestShunterSubscribeSingleDurationNonZeroOnSubmitFail pins the same
// contract on the executor-unavailable short-circuit path.
func TestShunterSubscribeSingleDurationNonZeroOnSubmitFail(t *testing.T) {
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

// TestShunterSubscribeMultiDurationNonZeroOnCompileFail pins the
// receipt-timestamp seam on handleSubscribeMulti's compile-short-circuit.
func TestShunterSubscribeMultiDurationNonZeroOnCompileFail(t *testing.T) {
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

// TestShunterSubscribeMultiDurationNonZeroOnSubmitFail pins the same
// contract on the multi executor-unavailable short-circuit path.
func TestShunterSubscribeMultiDurationNonZeroOnSubmitFail(t *testing.T) {
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

func TestHandleSubscribeSingle_SQLTooLongRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	msg := &SubscribeSingleMsg{RequestID: 15, QueryID: 115, QueryString: overlongSQLQuery()}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if !strings.Contains(se.Error, "maximum allowed length") {
		t.Fatalf("Error = %q, want maximum allowed length message", se.Error)
	}
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on overlength SQL path")
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection before executor", req)
	}
}

func TestHandleSubscribeMulti_SQLTooLongRejectedBeforeExecutor(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	msg := &SubscribeMultiMsg{RequestID: 16, QueryID: 116, QueryStrings: []string{"SELECT * FROM users", overlongSQLQuery()}}

	handleSubscribeMulti(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if !strings.Contains(se.Error, "maximum allowed length") {
		t.Fatalf("Error = %q, want maximum allowed length message", se.Error)
	}
	if se.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero on overlength SQL path")
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection before executor", req)
	}
}

// TestShunterUnsubscribeSingleDurationNonZeroOnSubmitFail pins the seam
// on handleUnsubscribeSingle's executor-unavailable short-circuit.
func TestShunterUnsubscribeSingleDurationNonZeroOnSubmitFail(t *testing.T) {
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

// TestShunterUnsubscribeMultiDurationNonZeroOnSubmitFail pins the same
// contract on handleUnsubscribeMulti.
func TestShunterUnsubscribeMultiDurationNonZeroOnSubmitFail(t *testing.T) {
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
