package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func TestFatalAlreadyLatchedStillAdmitsDisconnectCleanup(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true})
	connID := types.ConnectionID{0xA1}
	identity := types.Identity{0xA2}
	primeLifecycleDirect(t, h, connID, identity)
	h.exec.fatal.Store(true)

	disconnectSubscriptions := make(chan error, 1)
	if err := h.exec.Submit(DisconnectClientSubscriptionsCmd{
		ConnID:     connID,
		ResponseCh: disconnectSubscriptions,
	}); err != nil {
		t.Fatalf("Submit DisconnectClientSubscriptionsCmd: %v", err)
	}
	disconnectLifecycle := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(OnDisconnectCmd{
		ConnID:     connID,
		Identity:   identity,
		ResponseCh: disconnectLifecycle,
	}); err != nil {
		t.Fatalf("Submit OnDisconnectCmd: %v", err)
	}
	if err := h.exec.Submit(OnConnectCmd{}); !errors.Is(err, ErrExecutorFatal) {
		t.Fatalf("ordinary Submit error = %v, want ErrExecutorFatal", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go h.exec.Run(runCtx)

	select {
	case err := <-disconnectSubscriptions:
		if err != nil {
			t.Fatalf("subscription cleanup error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription cleanup timed out")
	}
	select {
	case resp := <-disconnectLifecycle:
		if resp.Status != StatusFailedInternal || !errors.Is(resp.Error, ErrExecutorFatal) {
			t.Fatalf("OnDisconnect response = %+v, want reported fatal after cleanup", resp)
		}
		if resp.TxID == 0 {
			t.Fatal("OnDisconnect fatal response omitted committed cleanup TxID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnDisconnect cleanup timed out")
	}

	if got := h.onDisconn.count(); got != 1 {
		t.Fatalf("OnDisconnect reducer calls = %d, want 1", got)
	}
	if rows := h.sysClientsSnapshot(); len(rows) != 0 {
		t.Fatalf("sys_clients rows after fatal cleanup = %d, want 0", len(rows))
	}
	h.subs.mu.Lock()
	disconnectCalls := append([]types.ConnectionID(nil), h.subs.disconns...)
	h.subs.mu.Unlock()
	if len(disconnectCalls) != 1 || disconnectCalls[0] != connID {
		t.Fatalf("subscription disconnect calls = %v, want %x", disconnectCalls, connID[:])
	}
}

func TestFatalLatchedAfterEnqueueStillDispatchesDisconnectCleanup(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true})
	connID := types.ConnectionID{0xB1}
	identity := types.Identity{0xB2}
	primeLifecycleDirect(t, h, connID, identity)

	disconnectSubscriptions := make(chan error, 1)
	if err := h.exec.Submit(DisconnectClientSubscriptionsCmd{ConnID: connID, ResponseCh: disconnectSubscriptions}); err != nil {
		t.Fatal(err)
	}
	disconnectLifecycle := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(OnDisconnectCmd{ConnID: connID, Identity: identity, ResponseCh: disconnectLifecycle}); err != nil {
		t.Fatal(err)
	}
	h.exec.fatal.Store(true)

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go h.exec.Run(runCtx)

	select {
	case err := <-disconnectSubscriptions:
		if err != nil {
			t.Fatalf("subscription cleanup error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription cleanup timed out")
	}
	select {
	case resp := <-disconnectLifecycle:
		if resp.Status != StatusFailedInternal || !errors.Is(resp.Error, ErrExecutorFatal) {
			t.Fatalf("OnDisconnect response = %+v, want reported fatal after cleanup", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnDisconnect cleanup timed out")
	}
	if got := h.onDisconn.count(); got != 1 {
		t.Fatalf("OnDisconnect reducer calls = %d, want 1", got)
	}
	if rows := h.sysClientsSnapshot(); len(rows) != 0 {
		t.Fatalf("sys_clients rows after queued fatal cleanup = %d, want 0", len(rows))
	}
}

func TestDurabilityFatalDuringOnDisconnectStillDeletesClientRow(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true})
	connID := types.ConnectionID{0xC1}
	identity := types.Identity{0xC2}
	primeLifecycleDirect(t, h, connID, identity)
	injected := errors.New("durability unavailable")
	h.dur.fatalErr = injected

	response := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(OnDisconnectCmd{ConnID: connID, Identity: identity, ResponseCh: response}); err != nil {
		t.Fatalf("Submit OnDisconnectCmd: %v", err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go h.exec.Run(runCtx)

	select {
	case resp := <-response:
		if resp.Status != StatusFailedInternal || !errors.Is(resp.Error, injected) {
			t.Fatalf("OnDisconnect response = %+v, want injected durability fatal", resp)
		}
		if resp.TxID == 0 {
			t.Fatal("durability-fatal cleanup omitted committed TxID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnDisconnect cleanup timed out")
	}
	if got := h.onDisconn.count(); got != 1 {
		t.Fatalf("OnDisconnect reducer calls = %d, want 1", got)
	}
	if rows := h.sysClientsSnapshot(); len(rows) != 0 {
		t.Fatalf("sys_clients rows after durability-fatal cleanup = %d, want 0", len(rows))
	}
}

func primeLifecycleDirect(t *testing.T, h *lifecycleHarness, connID types.ConnectionID, identity types.Identity) {
	t.Helper()
	response := make(chan ReducerResponse, 1)
	if result := h.exec.dispatch(OnConnectCmd{ConnID: connID, Identity: identity, ResponseCh: response}); result != "ok" {
		t.Fatalf("direct OnConnect result = %q, want ok", result)
	}
	select {
	case resp := <-response:
		if resp.Status != StatusCommitted {
			t.Fatalf("direct OnConnect response = %+v", resp)
		}
	default:
		t.Fatal("direct OnConnect omitted response")
	}
}
