package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
)

// Once admitted, CallReducer waits for executor completion even when the
// owning request ends. This keeps connection teardown from overtaking an
// accepted reducer command.
func TestProtocolInboxAdapter_CallReducer_ReqDoneWaitsForAdmittedCommand(t *testing.T) {
	commands := make(chan CallReducerCmd, 1)
	adapter := newProtocolInboxAdapter(stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("command type = %T, want CallReducerCmd", cmd)
		}
		commands <- call
		return nil
	}}, nil)
	reqDone := make(chan struct{})
	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{7},
		Identity:    types.Identity{8},
		RequestID:   123,
		ReducerName: "HangingReducer",
		ResponseCh:  make(chan protocol.TransactionUpdate),
		Done:        reqDone,
	}
	done := make(chan struct{})
	go func() {
		_ = adapter.CallReducer(context.Background(), req)
		close(done)
	}()

	var call CallReducerCmd
	select {
	case call = <-commands:
	case <-time.After(time.Second):
		t.Fatal("CallReducer command was not admitted")
	}

	close(reqDone)

	select {
	case <-done:
		t.Fatal("CallReducer returned before the admitted command completed")
	case <-time.After(25 * time.Millisecond):
	}

	call.ProtocolResponseCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusCommitted}}
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("CallReducer did not return after executor completion")
	}

	select {
	case update := <-req.ResponseCh:
		t.Fatalf("unexpected TransactionUpdate delivered on Done-triggered exit: %+v", update)
	default:
	}
}

func TestProtocolInboxAdapter_CallReducer_ExitsOnReqDoneWhenOutboundBlocked(t *testing.T) {
	adapter := newProtocolInboxAdapter(stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
		call := cmd.(CallReducerCmd)
		call.ProtocolResponseCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusCommitted}}
		return nil
	}}, nil)
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
	go func() {
		_ = adapter.CallReducer(context.Background(), req)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("CallReducer returned before req.Done signalled while outbound channel was blocked")
	case <-time.After(25 * time.Millisecond):
	}

	close(reqDone)

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("CallReducer did not exit after req.Done closed while outbound send was blocked")
	}

	select {
	case update := <-req.ResponseCh:
		t.Fatalf("unexpected TransactionUpdate delivered on Done-triggered outbound exit: %+v", update)
	default:
	}
}

func TestProtocolInboxAdapter_CallReducer_ExitsWhenReqDoneAlreadyClosedAfterCompletion(t *testing.T) {
	reqDone := make(chan struct{})
	close(reqDone)
	adapter := newProtocolInboxAdapter(stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
		call := cmd.(CallReducerCmd)
		call.ProtocolResponseCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusCommitted}}
		return nil
	}}, nil)

	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{9},
		Identity:    types.Identity{10},
		RequestID:   456,
		ReducerName: "AlreadyTorndown",
		ResponseCh:  make(chan protocol.TransactionUpdate, 1),
		Done:        reqDone,
	}
	done := make(chan struct{})
	go func() {
		_ = adapter.CallReducer(context.Background(), req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("CallReducer did not exit when req.Done was pre-closed")
	}
}
