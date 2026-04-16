package executor

import (
	"context"
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
			tx := ctx.DB.(*store.Transaction)
			tx.Insert(0, types.ProductValue{types.NewUint64(1), types.NewString("alice")})
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
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
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

func TestSubmitWithContextRejectOnFullReturnsBusy(t *testing.T) {
	exec, _ := setupExecutor()
	exec = NewExecutor(ExecutorConfig{InboxCapacity: 1, RejectOnFull: true}, exec.registry, exec.committed, exec.schemaReg, 0)
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
