package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func setupExecutor() (*Executor, schema.SchemaRegistry) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "name", Type: types.KindString},
		},
	})
	e, _ := b.Build(schema.EngineOptions{})
	reg := e.Registry()

	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}

	rr := NewReducerRegistry()
	rr.Register(RegisteredReducer{
		Name: "InsertPlayer",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, args []byte) ([]byte, error) {
			_, _ = ctx.DB.Insert(0, types.ProductValue{types.NewUint64(1), types.NewString("alice")})
			return nil, nil
		}),
	})
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 16}, rr, cs, reg, 0)
	return exec, reg
}

func TestExecutorRunAndSubmit(t *testing.T) {
	exec, _ := setupExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	err := exec.Submit(CallReducerCmd{
		Request: ReducerRequest{
			ReducerName: "InsertPlayer",
			Source:      CallSourceExternal,
		},
		ResponseCh: respCh,
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case resp := <-respCh:
		if resp.Status != StatusCommitted {
			t.Fatalf("expected StatusCommitted, got %d, err=%v", resp.Status, resp.Error)
		}
		if resp.TxID == 0 {
			t.Fatal("TxID should be > 0")
		}
		if got := exec.committed.CommittedTxID(); got != resp.TxID {
			t.Fatalf("committed horizon = %d, want response txID %d", got, resp.TxID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestReducerContextHandlesClosedAfterReducerReturn(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "items",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	engine, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := engine.Registry()
	tableID, _, ok := reg.TableByName("items")
	if !ok {
		t.Fatal("items table missing")
	}
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}

	var leakedDB types.ReducerDB
	var leakedScheduler types.ReducerScheduler
	var leakedTx *store.Transaction
	rr := NewReducerRegistry()
	rr.Register(RegisteredReducer{
		Name: "leak",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, args []byte) ([]byte, error) {
			leakedDB = ctx.DB
			leakedScheduler = ctx.Scheduler
			leakedTx, _ = ctx.DB.Underlying().(*store.Transaction)
			return nil, nil
		}),
	})
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "leak", Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-respCh:
		if resp.Status != StatusCommitted {
			t.Fatalf("status=%d err=%v, want committed", resp.Status, resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reducer response")
	}

	if _, err := leakedDB.Insert(uint32(tableID), types.ProductValue{types.NewUint64(1)}); !errors.Is(err, store.ErrTransactionClosed) {
		t.Fatalf("leaked DB Insert err=%v, want ErrTransactionClosed", err)
	}
	if got := leakedDB.Underlying(); got != nil {
		t.Fatalf("leaked DB Underlying = %T, want nil", got)
	}
	if _, err := leakedScheduler.Schedule("leak", nil, time.Now()); !errors.Is(err, store.ErrTransactionClosed) {
		t.Fatalf("leaked scheduler Schedule err=%v, want ErrTransactionClosed", err)
	}
	if _, err := leakedScheduler.ScheduleRepeat("leak", nil, 0); !errors.Is(err, store.ErrTransactionClosed) {
		t.Fatalf("leaked scheduler ScheduleRepeat err=%v, want ErrTransactionClosed", err)
	}
	if leakedTx == nil {
		t.Fatal("reducer did not expose underlying transaction during call")
	}
	if txState := leakedTx.TxState(); txState != nil {
		t.Fatal("leaked transaction TxState should be nil after reducer return")
	}
	if _, err := leakedTx.Insert(tableID, types.ProductValue{types.NewUint64(2)}); !errors.Is(err, store.ErrTransactionClosed) {
		t.Fatalf("leaked transaction Insert err=%v, want ErrTransactionClosed", err)
	}
}

func TestExecutorUnknownReducer(t *testing.T) {
	exec, _ := setupExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "DoesNotExist", Source: CallSourceExternal},
		ResponseCh: respCh,
	})

	select {
	case resp := <-respCh:
		if resp.Status != StatusFailedInternal {
			t.Fatalf("expected StatusFailedInternal, got %d", resp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecutorPermissionDeniedBeforeReducerExecution(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	engine, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := engine.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}

	called := false
	rr := NewReducerRegistry()
	if err := rr.Register(RegisteredReducer{
		Name: "InsertMessage",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
			called = true
			_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(1)})
			return nil, err
		}),
		RequiredPermissions: []string{"messages:send"},
	}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "InsertMessage", Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-respCh:
		if resp.Status != StatusFailedPermission {
			t.Fatalf("status = %d, want StatusFailedPermission; err=%v", resp.Status, resp.Error)
		}
		if !errors.Is(resp.Error, ErrPermissionDenied) {
			t.Fatalf("err = %v, want ErrPermissionDenied", resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if called {
		t.Fatal("reducer handler ran despite missing permission")
	}
}

func TestExecutorShutdown(t *testing.T) {
	exec, _ := setupExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	exec.Shutdown()
	err := exec.Submit(CallReducerCmd{})
	if err != ErrExecutorShutdown {
		t.Fatalf("post-shutdown Submit should return ErrExecutorShutdown, got %v", err)
	}
}

func TestExecutorShutdownWithoutRunUnblocksBlockedSubmit(t *testing.T) {
	exec, _ := setupExecutorWithObserver(nil, ExecutorConfig{InboxCapacity: 1})
	if err := exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
		ResponseCh: make(chan ReducerResponse, 1),
	}); err != nil {
		t.Fatalf("first submit: %v", err)
	}

	blockedSubmit := make(chan error, 1)
	go func() {
		blockedSubmit <- exec.Submit(CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
			ResponseCh: make(chan ReducerResponse, 1),
		})
	}()

	select {
	case err := <-blockedSubmit:
		t.Fatalf("second submit returned before shutdown: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	shutdownDone := make(chan struct{})
	go func() {
		exec.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not complete without Run")
	}
	select {
	case err := <-blockedSubmit:
		if !errors.Is(err, ErrExecutorShutdown) {
			t.Fatalf("blocked Submit error = %v, want ErrExecutorShutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Submit did not unblock after Shutdown")
	}
}

func TestExecutorShutdownWithoutRunUnblocksBlockedSubmitWithContext(t *testing.T) {
	exec, _ := setupExecutorWithObserver(nil, ExecutorConfig{InboxCapacity: 1})
	if err := exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	if err := exec.SubmitWithContext(context.Background(), CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
		ResponseCh: make(chan ReducerResponse, 1),
	}); err != nil {
		t.Fatalf("first SubmitWithContext: %v", err)
	}

	blockedSubmit := make(chan error, 1)
	go func() {
		blockedSubmit <- exec.SubmitWithContext(context.Background(), CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
			ResponseCh: make(chan ReducerResponse, 1),
		})
	}()

	select {
	case err := <-blockedSubmit:
		t.Fatalf("second SubmitWithContext returned before shutdown: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	shutdownDone := make(chan struct{})
	go func() {
		exec.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not complete without Run")
	}
	select {
	case err := <-blockedSubmit:
		if !errors.Is(err, ErrExecutorShutdown) {
			t.Fatalf("blocked SubmitWithContext error = %v, want ErrExecutorShutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked SubmitWithContext did not unblock after Shutdown")
	}
}

func waitForExecutorSignals(t *testing.T, ch <-chan struct{}, want int, label string) {
	t.Helper()
	for i := 0; i < want; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: observed %d/%d signals", label, i, want)
		}
	}
}

func TestExecutorRunDrainsInFlightSubmitOnShutdown(t *testing.T) {
	exec, _ := setupExecutorWithObserver(nil, ExecutorConfig{InboxCapacity: 1})
	exec.inbox = make(chan ExecutorCommand)
	respCh := make(chan ReducerResponse, 1)
	exec.inflightSubmits.Add(1)
	exec.shutdown.Store(true)
	close(exec.shutdownCh)

	done := make(chan struct{})
	go func() {
		exec.Run(context.Background())
		close(done)
	}()
	go func() {
		exec.inbox <- CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "ok", Source: CallSourceExternal},
			ResponseCh: respCh,
		}
		exec.inflightSubmits.Add(-1)
	}()

	select {
	case resp := <-respCh:
		if !errors.Is(resp.Error, ErrExecutorShutdown) {
			t.Fatalf("shutdown-drained response error = %v, want ErrExecutorShutdown", resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for shutdown-drained response")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not exit after draining in-flight submit")
	}
}

func TestExecutorRunContextCancelStartsShutdownAndDrainsQueuedWork(t *testing.T) {
	exec, _ := setupExecutorWithObserver(nil, ExecutorConfig{InboxCapacity: 1})
	respCh := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "ok", Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatalf("pre-run submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		exec.Run(ctx)
		close(done)
	}()

	select {
	case resp := <-respCh:
		if !errors.Is(resp.Error, ErrExecutorShutdown) {
			t.Fatalf("context-cancel-drained response error = %v, want ErrExecutorShutdown", resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for context-cancel-drained response")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not exit after context cancellation")
	}
	if !exec.ShutdownStarted() {
		t.Fatal("context cancellation should mark executor shutdown started")
	}
	if err := exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "ok", Source: CallSourceExternal},
		ResponseCh: make(chan ReducerResponse, 1),
	}); !errors.Is(err, ErrExecutorShutdown) {
		t.Fatalf("post-context-cancel Submit error = %v, want ErrExecutorShutdown", err)
	}
}

func TestExecutorShutdownConcurrentSubmittersDoNotPanic(t *testing.T) {
	exec, _ := setupExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	panicCh := make(chan any, 1)
	stop := make(chan struct{})
	started := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			signaled := false
			defer func() {
				if r := recover(); r != nil {
					select {
					case panicCh <- r:
					default:
					}
				}
			}()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if !signaled {
					started <- struct{}{}
					signaled = true
				}
				respCh := make(chan ReducerResponse, 1)
				err := exec.Submit(CallReducerCmd{
					Request: ReducerRequest{
						ReducerName: "InsertPlayer",
						Source:      CallSourceExternal,
					},
					ResponseCh: respCh,
				})
				if err != nil && !errors.Is(err, ErrExecutorShutdown) {
					select {
					case panicCh <- err:
					default:
					}
					return
				}
			}
		}()
	}

	waitForExecutorSignals(t, started, 8, "submitters started")
	shutdownDone := make(chan struct{})
	go func() {
		exec.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not complete")
	}
	close(stop)
	wg.Wait()

	select {
	case p := <-panicCh:
		t.Fatalf("concurrent submit/shutdown should not panic, got %v", p)
	default:
	}
}

func TestSubmitAfterPostCommitFatalReturnsExecutorFatal(t *testing.T) {
	h := newPipelineHarness(t)
	h.dur.panicOn = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusFailedInternal {
		t.Fatalf("status=%d, want StatusFailedInternal", resp.Status)
	}
	if !errors.Is(resp.Error, ErrExecutorFatal) {
		t.Fatalf("err=%v, want wraps ErrExecutorFatal", resp.Error)
	}

	err := h.exec.Submit(CallReducerCmd{})
	if !errors.Is(err, ErrExecutorFatal) {
		t.Fatalf("Submit after post-commit fatal = %v, want %v", err, ErrExecutorFatal)
	}
}

func TestSubmitWithContextRejectOnFullReturnsBusy(t *testing.T) {
	exec, _ := setupExecutor()
	exec = NewExecutor(ExecutorConfig{InboxCapacity: 1, RejectOnFull: true}, exec.registry, exec.committed, exec.schemaReg, 0)
	if err := exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	exec.inbox <- CallReducerCmd{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := exec.SubmitWithContext(ctx, CallReducerCmd{})
	if err != ErrExecutorBusy {
		t.Fatalf("SubmitWithContext on full reject-mode inbox = %v, want %v", err, ErrExecutorBusy)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Fatalf("SubmitWithContext should reject immediately, took %v", elapsed)
	}
}

func TestSendReducerResponse_UnbufferedChannelBlocksUntilReceiverReady(t *testing.T) {
	ch := make(chan ReducerResponse)
	done := make(chan struct{})

	go func() {
		sendReducerResponse(ch, ReducerResponse{Status: StatusCommitted})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("sendReducerResponse returned before a receiver was ready")
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case resp := <-ch:
		if resp.Status != StatusCommitted {
			t.Fatalf("status = %d, want %d", resp.Status, StatusCommitted)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting to receive reducer response")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendReducerResponse did not finish after receiver consumed response")
	}
}

func TestSendProtocolCallReducerResponse_UnbufferedChannelBlocksUntilReceiverReady(t *testing.T) {
	ch := make(chan ProtocolCallReducerResponse)
	done := make(chan struct{})

	go func() {
		sendProtocolCallReducerResponse(ch, ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusCommitted}})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("sendProtocolCallReducerResponse returned before a receiver was ready")
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case resp := <-ch:
		if resp.Reducer.Status != StatusCommitted {
			t.Fatalf("status = %d, want %d", resp.Reducer.Status, StatusCommitted)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting to receive protocol reducer response")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendProtocolCallReducerResponse did not finish after receiver consumed response")
	}
}

func TestSubmitRejectsUnbufferedResponseChannels(t *testing.T) {
	exec, _ := setupExecutor()
	ctx := context.Background()
	if err := exec.Startup(ctx, nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	tests := []struct {
		name   string
		submit func() error
	}{
		{
			name: "call reducer response channel",
			submit: func() error {
				return exec.Submit(CallReducerCmd{
					Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
					ResponseCh: make(chan ReducerResponse),
				})
			},
		},
		{
			name: "call reducer protocol response channel",
			submit: func() error {
				return exec.Submit(CallReducerCmd{
					Request:            ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
					ProtocolResponseCh: make(chan ProtocolCallReducerResponse),
				})
			},
		},
		{
			name: "disconnect subscriptions response channel",
			submit: func() error {
				return exec.Submit(DisconnectClientSubscriptionsCmd{
					ConnID:     types.ConnectionID{1},
					ResponseCh: make(chan error),
				})
			},
		},
		{
			name: "submit with context onconnect response channel",
			submit: func() error {
				return exec.SubmitWithContext(ctx, OnConnectCmd{
					ConnID:     types.ConnectionID{1},
					Identity:   types.Identity{2},
					ResponseCh: make(chan ReducerResponse),
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.submit(); !errors.Is(err, ErrExecutorUnbufferedResponseChannel) {
				t.Fatalf("submit error = %v, want %v", err, ErrExecutorUnbufferedResponseChannel)
			}
		})
	}
}

func TestReducerRegistryBasics(t *testing.T) {
	rr := NewReducerRegistry()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })

	if err := rr.Register(RegisteredReducer{Name: "A", Handler: h}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "A", Handler: h}); err == nil {
		t.Fatal("duplicate should error")
	}

	r, ok := rr.Lookup("A")
	if !ok || r.Name != "A" {
		t.Fatal("Lookup failed")
	}

	rr.Freeze()
	if err := rr.Register(RegisteredReducer{Name: "B", Handler: h}); err == nil {
		t.Fatal("frozen registry should reject")
	}
}

func TestReducerRegistryLifecycle(t *testing.T) {
	rr := NewReducerRegistry()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })

	// Normal reducer with reserved name.
	err := rr.Register(RegisteredReducer{Name: "OnConnect", Handler: h, Lifecycle: LifecycleNone})
	if err == nil {
		t.Fatal("normal reducer with reserved name should error")
	}

	// Lifecycle with correct name.
	err = rr.Register(RegisteredReducer{Name: "OnConnect", Handler: h, Lifecycle: LifecycleOnConnect})
	if err != nil {
		t.Fatal(err)
	}

	r, ok := rr.LookupLifecycle(LifecycleOnConnect)
	if !ok || r.Name != "OnConnect" {
		t.Fatal("LookupLifecycle failed")
	}
}

func TestReducerRegistryFrozenLookupsReturnDetachedCopies(t *testing.T) {
	rr := NewReducerRegistry()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })
	if err := rr.Register(RegisteredReducer{Name: "A", Handler: h}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "OnConnect", Handler: h, Lifecycle: LifecycleOnConnect}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "OnDisconnect", Handler: h, Lifecycle: LifecycleOnDisconnect}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()

	lookup, ok := rr.Lookup("A")
	if !ok {
		t.Fatal("Lookup should find registered reducer")
	}
	lookup.Name = "MUTATED"
	if again, ok := rr.Lookup("A"); !ok || again.Name != "A" {
		t.Fatalf("Lookup should return detached copy, got ok=%v reducer=%+v", ok, again)
	}

	all := rr.All()
	if len(all) != 3 {
		t.Fatalf("All len = %d, want 3", len(all))
	}
	for _, reducer := range all {
		if reducer.Lifecycle == LifecycleOnConnect {
			reducer.Lifecycle = LifecycleNone
			reducer.Name = "Broken"
		}
	}
	lifecycle, ok := rr.LookupLifecycle(LifecycleOnConnect)
	if !ok || lifecycle.Name != "OnConnect" {
		t.Fatalf("LookupLifecycle should be unchanged after All mutation, got ok=%v reducer=%+v", ok, lifecycle)
	}
}

func TestReducerRegistryConcurrentRegisterLookupAndFreeze(t *testing.T) {
	rr := NewReducerRegistry()
	h := types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) { return nil, nil })

	start := make(chan struct{})
	var wg sync.WaitGroup
	ready := make(chan struct{}, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			ready <- struct{}{}
			name := fmt.Sprintf("r%d", id)
			for j := 0; j < 200; j++ {
				_ = rr.Register(RegisteredReducer{Name: name, Handler: h})
				_, _ = rr.Lookup(name)
				_, _ = rr.LookupLifecycle(LifecycleOnConnect)
				_ = rr.All()
				_ = rr.IsFrozen()
			}
		}(i)
	}

	close(start)
	waitForExecutorSignals(t, ready, 4, "registry workers ready")
	rr.Freeze()
	wg.Wait()

	if !rr.IsFrozen() {
		t.Fatal("Freeze should latch frozen state")
	}
	if err := rr.Register(RegisteredReducer{Name: "late", Handler: h}); err == nil {
		t.Fatal("register after freeze should fail")
	}
}
