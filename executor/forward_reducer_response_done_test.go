package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
)

// TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs
// pins the OI-004 Tier-B hardening fix for the `forwardReducerResponse`
// goroutine leak. This test is the regression record for that contract.
//
// Sharp edge: the production dispatch ctx is rooted at
// context.Background() (protocol/upgrade.go:201 through
// Conn.runDispatchLoop). If the executor accepts a CallReducer but then
// never sends on its internal ProtocolCallReducerResponse channel — e.g.
// crash mid-commit, hung reducer on a shutting-down engine, executor
// never reaching the reply seam — the forwarder goroutine would select
// only on respCh and ctx.Done and leak forever, holding the owning
// *Conn and its transitive state alive past disconnect.
//
// Contract: forwardReducerResponse must exit promptly when
// req.Done closes (wired from Conn.closed at the handleCallReducer
// site, fired as step 4 of the SPEC-005 §5.3 teardown) even if
// respCh never fires. Direct analog to the watchReducerResponse
// hardening (2026-04-20) on the protocol-side watcher.
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
