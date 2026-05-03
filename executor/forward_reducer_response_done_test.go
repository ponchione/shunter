package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
)

// TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs
// pins that reducer response forwarding exits when the owning request is done.
func TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs(t *testing.T) {
	respCh := make(chan ProtocolCallReducerResponse) // never sends, never closes

	done := make(chan struct{})
	reqDone := make(chan struct{})
	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{7},
		Identity:    types.Identity{8},
		RequestID:   123,
		ReducerName: "HangingReducer",
		ResponseCh:  make(chan protocol.TransactionUpdate, 1),
		Done:        reqDone,
	}
	adapter := &ProtocolInboxAdapter{}

	go func() {
		adapter.forwardReducerResponse(context.Background(), req, respCh)
		close(done)
	}()

	// Forwarder must not exit spontaneously while both respCh and
	// reqDone are open; pin the blocked state for a small window.
	select {
	case <-done:
		t.Fatal("forwardReducerResponse returned before req.Done signalled")
	case <-time.After(25 * time.Millisecond):
	}

	close(reqDone)

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("forwardReducerResponse did not exit after req.Done closed; goroutine leak")
	}

	select {
	case update := <-req.ResponseCh:
		t.Fatalf("unexpected TransactionUpdate delivered on Done-triggered exit: %+v", update)
	default:
	}
}

// TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenOutboundBlocked
// pins the second half of the same lifecycle contract: after the executor
// has produced a reducer response, the forwarding goroutine must still stop
// if the owning connection is torn down while the protocol response channel
// is blocked.
func TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenOutboundBlocked(t *testing.T) {
	respCh := make(chan ProtocolCallReducerResponse, 1)
	respCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusCommitted}}

	done := make(chan struct{})
	reqDone := make(chan struct{})
	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{11},
		Identity:    types.Identity{12},
		RequestID:   789,
		ReducerName: "BlockedOutboundReducer",
		ResponseCh:  make(chan protocol.TransactionUpdate),
		Done:        reqDone,
	}
	adapter := &ProtocolInboxAdapter{}

	go func() {
		adapter.forwardReducerResponse(context.Background(), req, respCh)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("forwardReducerResponse returned before req.Done signalled while outbound channel was blocked")
	case <-time.After(25 * time.Millisecond):
	}

	close(reqDone)

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("forwardReducerResponse did not exit after req.Done closed while outbound send was blocked")
	}

	select {
	case update := <-req.ResponseCh:
		t.Fatalf("unexpected TransactionUpdate delivered on Done-triggered outbound exit: %+v", update)
	default:
	}
}

// TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneAlreadyClosed
// pins that a pre-closed req.Done does not wedge the forwarder: the
// goroutine returns promptly even if no other select arm fires. Guards
// against a future refactor that stops watching req.Done on the fast
// path.
func TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneAlreadyClosed(t *testing.T) {
	respCh := make(chan ProtocolCallReducerResponse) // never fires

	reqDone := make(chan struct{})
	close(reqDone)

	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{9},
		Identity:    types.Identity{10},
		RequestID:   456,
		ReducerName: "AlreadyTorndown",
		ResponseCh:  make(chan protocol.TransactionUpdate, 1),
		Done:        reqDone,
	}
	adapter := &ProtocolInboxAdapter{}

	done := make(chan struct{})
	go func() {
		adapter.forwardReducerResponse(context.Background(), req, respCh)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("forwardReducerResponse did not exit when req.Done was pre-closed")
	}
}
