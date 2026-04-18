package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// Phase 1.5 caller-metadata wiring sub-slice
// (docs/parity-phase0-ledger.md P0-DELIVERY-001 follow-ups). Pins that
// an external CallReducer populates the caller-bound metadata on
// subscription.CallerOutcome so the heavy TransactionUpdate envelope
// carries real values instead of the Phase 1.5 zero stubs. Reference
// fields being matched:
//
//	TransactionUpdate.CallerIdentity            (Identity)
//	TransactionUpdate.ReducerCall.ReducerName   (Box<str>)
//	TransactionUpdate.ReducerCall.ReducerID     (ReducerId u32)
//	TransactionUpdate.ReducerCall.Args          (Bytes)
//	TransactionUpdate.Timestamp                 (Timestamp i64 ns)
//	TransactionUpdate.TotalHostExecutionDuration (TimeDuration i64 ns)

func submitWithMetadata(
	t *testing.T,
	exec *Executor,
	name string,
	identity types.Identity,
	args []byte,
) ReducerResponse {
	t.Helper()
	ch := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{
		Request: ReducerRequest{
			ReducerName: name,
			Args:        args,
			Source:      CallSourceExternal,
			RequestID:   42,
			Caller: types.CallerContext{
				Identity:     identity,
				ConnectionID: types.ConnectionID{9},
			},
		},
		ResponseCh: ch,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-ch:
		return resp
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
		return ReducerResponse{}
	}
}

func latestCallerOutcome(t *testing.T, h *pipelineHarness) subscription.CallerOutcome {
	t.Helper()
	h.subs.mu.Lock()
	defer h.subs.mu.Unlock()
	if len(h.subs.metas) == 0 {
		t.Fatal("no PostCommitMeta captured")
	}
	meta := h.subs.metas[len(h.subs.metas)-1]
	if meta.CallerOutcome == nil {
		t.Fatal("CallerOutcome is nil on captured meta")
	}
	return *meta.CallerOutcome
}

func TestCallerOutcomeCarriesCallerIdentity(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	wantID := types.Identity{0xab, 0xcd, 0xef}
	resp := submitWithMetadata(t, h.exec, "InsertPlayer", wantID, []byte("args"))
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	got := latestCallerOutcome(t, h)
	if got.CallerIdentity != wantID {
		t.Errorf("CallerOutcome.CallerIdentity = %x, want %x", got.CallerIdentity, wantID)
	}
}

func TestCallerOutcomeCarriesReducerNameAndArgs(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	args := []byte{1, 2, 3, 4}
	resp := submitWithMetadata(t, h.exec, "InsertPlayer", types.Identity{}, args)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	got := latestCallerOutcome(t, h)
	if got.ReducerName != "InsertPlayer" {
		t.Errorf("CallerOutcome.ReducerName = %q, want %q", got.ReducerName, "InsertPlayer")
	}
	if string(got.Args) != string(args) {
		t.Errorf("CallerOutcome.Args = %v, want %v", got.Args, args)
	}
}

func TestCallerOutcomeCarriesTimestampAndDuration(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	before := time.Now().UnixNano()
	resp := submitWithMetadata(t, h.exec, "InsertPlayer", types.Identity{}, nil)
	after := time.Now().UnixNano()
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	got := latestCallerOutcome(t, h)
	if got.Timestamp < before || got.Timestamp > after {
		t.Errorf("CallerOutcome.Timestamp = %d, want in [%d,%d]", got.Timestamp, before, after)
	}
	if got.TotalHostExecutionDuration <= 0 {
		t.Errorf("CallerOutcome.TotalHostExecutionDuration = %d, want > 0", got.TotalHostExecutionDuration)
	}
	bound := after - before + int64(time.Second)
	if got.TotalHostExecutionDuration > bound {
		t.Errorf("CallerOutcome.TotalHostExecutionDuration = %d, exceeds upper bound %d",
			got.TotalHostExecutionDuration, bound)
	}
}

func TestCallerOutcomeCarriesReducerID(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submitWithMetadata(t, h.exec, "InsertPlayer", types.Identity{}, nil)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	rr, ok := h.exec.registry.Lookup("InsertPlayer")
	if !ok {
		t.Fatal("InsertPlayer not in registry")
	}
	if rr.ID == 0 && rr.Name == "" {
		t.Fatal("registry lookup returned zero value")
	}

	got := latestCallerOutcome(t, h)
	if got.ReducerID != rr.ID {
		t.Errorf("CallerOutcome.ReducerID = %d, want registry ID %d", got.ReducerID, rr.ID)
	}
}

func TestRegistryAssignsMonotonicReducerIDs(t *testing.T) {
	rr := NewReducerRegistry()
	if err := rr.Register(RegisteredReducer{Name: "A", Handler: noopHandler}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "B", Handler: noopHandler}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "C", Handler: noopHandler}); err != nil {
		t.Fatal(err)
	}

	a, _ := rr.Lookup("A")
	b, _ := rr.Lookup("B")
	c, _ := rr.Lookup("C")
	if !(a.ID < b.ID && b.ID < c.ID) {
		t.Errorf("reducer IDs not monotonic: A=%d B=%d C=%d", a.ID, b.ID, c.ID)
	}
}

func noopHandler(*types.ReducerContext, []byte) ([]byte, error) { return nil, nil }
