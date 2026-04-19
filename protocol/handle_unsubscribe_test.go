package protocol

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --- handleUnsubscribeSingle tests ---

func TestHandleUnsubscribeSingleSuccess(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &UnsubscribeSingleMsg{RequestID: 1, QueryID: 42}
	handleUnsubscribeSingle(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.unregisterSetReq == nil {
		t.Fatal("UnregisterSubscriptionSet request was not recorded")
	}
	if exec.unregisterSetReq.ConnID != conn.ID {
		t.Errorf("ConnID = %x, want %x", exec.unregisterSetReq.ConnID, conn.ID)
	}
	if exec.unregisterSetReq.QueryID != 42 {
		t.Errorf("QueryID = %d, want 42", exec.unregisterSetReq.QueryID)
	}
	if exec.unregisterSetReq.RequestID != 1 {
		t.Errorf("RequestID = %d, want 1", exec.unregisterSetReq.RequestID)
	}
	if exec.unregisterSetReq.Reply == nil {
		t.Error("Reply = nil, want non-nil unsubscribe reply closure")
	}

	// No error message should have been sent.
	select {
	case <-conn.OutboundCh:
		t.Error("unexpected message on OutboundCh for successful unsubscribe")
	default:
	}
}

func TestHandleUnsubscribeSingle_DeliversAsyncUnsubscribeApplied(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &UnsubscribeSingleMsg{RequestID: 1, QueryID: 42}
	handleUnsubscribeSingle(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	reply := exec.unregisterSetReq.Reply
	exec.mu.Unlock()
	if reply == nil {
		t.Fatal("missing unsubscribe reply closure")
	}
	reply(UnsubscribeSetCommandResponse{
		SingleApplied: &UnsubscribeSingleApplied{RequestID: 1, QueryID: 42},
	})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagUnsubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d (TagUnsubscribeSingleApplied)", tag, TagUnsubscribeSingleApplied)
	}
	applied := decoded.(UnsubscribeSingleApplied)
	if applied.RequestID != 1 || applied.QueryID != 42 {
		t.Fatalf("UnsubscribeSingleApplied = %+v", applied)
	}
}

func TestHandleUnsubscribeSingle_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{unregisterSetErr: errors.New("db down")}

	msg := &UnsubscribeSingleMsg{RequestID: 4, QueryID: 7}
	handleUnsubscribeSingle(context.Background(), conn, msg, exec)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.RequestID != 4 {
		t.Errorf("RequestID = %d, want 4", se.RequestID)
	}
	if !strings.Contains(se.Error, "executor unavailable") {
		t.Errorf("Error = %q, want to contain 'executor unavailable'", se.Error)
	}
	if !strings.Contains(se.Error, "db down") {
		t.Errorf("Error = %q, want to contain underlying cause 'db down'", se.Error)
	}
}

// --- handleUnsubscribeMulti tests ---

func TestHandleUnsubscribeMultiSuccess(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &UnsubscribeMultiMsg{RequestID: 21, QueryID: 77}
	handleUnsubscribeMulti(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.unregisterSetReq == nil {
		t.Fatal("UnregisterSubscriptionSet request was not recorded")
	}
	if exec.unregisterSetReq.QueryID != 77 {
		t.Errorf("QueryID = %d, want 77", exec.unregisterSetReq.QueryID)
	}
	if exec.unregisterSetReq.RequestID != 21 {
		t.Errorf("RequestID = %d, want 21", exec.unregisterSetReq.RequestID)
	}
	if exec.unregisterSetReq.Reply == nil {
		t.Error("Reply = nil, want non-nil unsubscribe reply closure")
	}

	// No error message should have been sent.
	select {
	case <-conn.OutboundCh:
		t.Error("unexpected message on OutboundCh for successful unsubscribe")
	default:
	}
}

func TestHandleUnsubscribeMulti_DeliversAsyncMultiApplied(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{}

	msg := &UnsubscribeMultiMsg{RequestID: 22, QueryID: 88}
	handleUnsubscribeMulti(context.Background(), conn, msg, exec)

	exec.mu.Lock()
	reply := exec.unregisterSetReq.Reply
	exec.mu.Unlock()
	if reply == nil {
		t.Fatal("missing unsubscribe reply closure")
	}
	reply(UnsubscribeSetCommandResponse{
		MultiApplied: &UnsubscribeMultiApplied{RequestID: 22, QueryID: 88},
	})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagUnsubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d (TagUnsubscribeMultiApplied)", tag, TagUnsubscribeMultiApplied)
	}
	applied := decoded.(UnsubscribeMultiApplied)
	if applied.RequestID != 22 || applied.QueryID != 88 {
		t.Fatalf("UnsubscribeMultiApplied = %+v", applied)
	}
}

func TestHandleUnsubscribeMulti_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockDispatchExecutor{unregisterSetErr: errors.New("db down")}

	msg := &UnsubscribeMultiMsg{RequestID: 24, QueryID: 99}
	handleUnsubscribeMulti(context.Background(), conn, msg, exec)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.RequestID != 24 {
		t.Errorf("RequestID = %d, want 24", se.RequestID)
	}
	if se.QueryID != 99 {
		t.Errorf("QueryID = %d, want 99", se.QueryID)
	}
	if !strings.Contains(se.Error, "executor unavailable") {
		t.Errorf("Error = %q, want to contain 'executor unavailable'", se.Error)
	}
}
