package executor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

var errReducerBoom = errors.New("reducer boom")

func TestReducerRegistryRules(t *testing.T) {
	h := types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) { return nil, nil })
	rr := NewReducerRegistry()
	if _, ok := rr.Lookup("missing"); ok {
		t.Fatal("missing reducer should not resolve")
	}
	if err := rr.Register(RegisteredReducer{Name: "A", Handler: h}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "A", Handler: h}); err == nil {
		t.Fatal("duplicate reducer should fail")
	}
	if err := rr.Register(RegisteredReducer{Name: "OnConnect", Handler: h}); err == nil {
		t.Fatal("reserved lifecycle name should fail for normal reducer")
	}
	if err := rr.Register(RegisteredReducer{Name: "Wrong", Handler: h, Lifecycle: LifecycleOnConnect}); err == nil {
		t.Fatal("wrong lifecycle name should fail")
	}
	if err := rr.Register(RegisteredReducer{Name: "OnConnect", Handler: h, Lifecycle: LifecycleOnConnect}); err != nil {
		t.Fatal(err)
	}
	if err := rr.Register(RegisteredReducer{Name: "OnDisconnect", Handler: h, Lifecycle: LifecycleOnDisconnect}); err != nil {
		t.Fatal(err)
	}
	if _, ok := rr.LookupLifecycle(LifecycleOnConnect); !ok {
		t.Fatal("LookupLifecycle should find registered lifecycle reducer")
	}
	if len(rr.All()) != 3 {
		t.Fatalf("All len = %d, want 3", len(rr.All()))
	}
	rr.Freeze()
	rr.Freeze()
	if err := rr.Register(RegisteredReducer{Name: "late", Handler: h}); err == nil {
		t.Fatal("register after freeze should fail")
	}
}

func TestRunLoopSequentialAndCancels(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "players", Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}, {Name: "name", Type: types.KindString}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := eng.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	rr := NewReducerRegistry()
	var active atomic.Int32
	var peak atomic.Int32
	var mu sync.Mutex
	order := []string{}
	started := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	if err := rr.Register(RegisteredReducer{Name: "slow", Handler: func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		if got := active.Add(1); got > peak.Load() {
			peak.Store(got)
		}
		mu.Lock()
		order = append(order, "start")
		mu.Unlock()
		started <- struct{}{}
		<-release
		mu.Lock()
		order = append(order, "end")
		mu.Unlock()
		active.Add(-1)
		return nil, nil
	}}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()
	exec := NewExecutor(ExecutorConfig{InboxCapacity: 8}, rr, cs, reg, 0)
	ctx, cancel := context.WithCancel(context.Background())
	go exec.Run(ctx)
	resp1 := make(chan ReducerResponse, 1)
	resp2 := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "slow"}, ResponseCh: resp1}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "slow"}, ResponseCh: resp2}); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	<-resp1
	<-started
	release <- struct{}{}
	<-resp2
	cancel()
	<-exec.done
	if peak.Load() != 1 {
		t.Fatalf("peak concurrent reducers = %d, want 1", peak.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 4 || order[0] != "start" || order[1] != "end" || order[2] != "start" || order[3] != "end" {
		t.Fatalf("sequential order = %v", order)
	}
}

func TestDispatchPanicRecoverySurvivesNextCommand(t *testing.T) {
	exec := &Executor{inbox: make(chan ExecutorCommand, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)
	resp := make(chan ReducerResponse, 1)
	exec.inbox <- CallReducerCmd{Request: ReducerRequest{ReducerName: "boom"}, ResponseCh: resp}
	good := func() *Executor {
		b := schema.NewBuilder()
		b.SchemaVersion(1)
		b.TableDef(schema.TableDefinition{Name: "players", Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}, {Name: "name", Type: types.KindString}}})
		eng, _ := b.Build(schema.EngineOptions{})
		reg := eng.Registry()
		cs := store.NewCommittedState()
		for _, tid := range reg.Tables() {
			ts, _ := reg.Table(tid)
			cs.RegisterTable(tid, store.NewTable(ts))
		}
		rr := NewReducerRegistry()
		_ = rr.Register(RegisteredReducer{Name: "ok", Handler: func(*types.ReducerContext, []byte) ([]byte, error) { return []byte("ok"), nil }})
		rr.Freeze()
		return NewExecutor(ExecutorConfig{InboxCapacity: 2}, rr, cs, reg, 0)
	}()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go good.Run(ctx2)
	goodResp := make(chan ReducerResponse, 1)
	if err := good.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "ok"}, ResponseCh: goodResp}); err != nil {
		t.Fatal(err)
	}
	if got := <-goodResp; got.Status != StatusCommitted {
		t.Fatalf("good executor should still work, got %+v", got)
	}
}

func TestHandleCallReducerBeginExecuteCommitRollback(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "players", Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}, {Name: "name", Type: types.KindString}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := eng.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	players, _ := cs.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("taken")}); err != nil {
		t.Fatal(err)
	}
	rr := NewReducerRegistry()
	var captured types.CallerContext
	var scheduler types.ReducerScheduler
	_ = rr.Register(RegisteredReducer{Name: "ok", Handler: func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		captured = ctx.Caller
		scheduler = ctx.Scheduler
		_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(2), types.NewString("alice")})
		if err != nil {
			return nil, err
		}
		return []byte("ret"), nil
	}})
	_ = rr.Register(RegisteredReducer{Name: "user", Handler: func(*types.ReducerContext, []byte) ([]byte, error) { return nil, errors.New("user fail") }})
	_ = rr.Register(RegisteredReducer{Name: "panic", Handler: func(*types.ReducerContext, []byte) ([]byte, error) { panic(errReducerBoom) }})
	_ = rr.Register(RegisteredReducer{Name: "commit-user", Handler: func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		tx := ctx.DB.Underlying().(*store.Transaction)
		tx.TxState().AddInsert(0, 999, types.ProductValue{types.NewUint64(1), types.NewString("dup")})
		return nil, nil
	}})
	_ = rr.Register(RegisteredReducer{Name: "commit-internal", Handler: func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		tx := ctx.DB.Underlying().(*store.Transaction)
		tx.TxState().AddInsert(999, 1, types.ProductValue{types.NewUint64(9), types.NewString("ghost")})
		return nil, nil
	}})
	_ = rr.Register(RegisteredReducer{Name: "OnConnect", Lifecycle: LifecycleOnConnect, Handler: func(*types.ReducerContext, []byte) ([]byte, error) { return nil, nil }})
	rr.Freeze()
	exec := NewExecutor(ExecutorConfig{InboxCapacity: 16}, rr, cs, reg, 41)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	call := func(name string, source CallSource) ReducerResponse {
		ch := make(chan ReducerResponse, 1)
		caller := types.CallerContext{
			Timestamp: time.Unix(1, 0),
			Principal: types.AuthPrincipal{
				Issuer:      "issuer",
				Subject:     "alice",
				Audience:    []string{"shunter-api"},
				Permissions: []string{"principal:permission"},
			},
		}
		if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: name, Source: source, Caller: caller}, ResponseCh: ch}); err != nil {
			t.Fatal(err)
		}
		return <-ch
	}

	resp := call("ok", CallSourceExternal)
	if resp.Status != StatusCommitted || string(resp.ReturnBSATN) != "ret" || resp.TxID != 42 {
		t.Fatalf("commit response = %+v", resp)
	}
	if captured.Timestamp.Equal(time.Unix(1, 0)) || captured.Timestamp.Location() != time.UTC {
		t.Fatalf("caller timestamp was not replaced at dequeue: %+v", captured)
	}
	if captured.Principal.Issuer != "issuer" || captured.Principal.Subject != "alice" ||
		len(captured.Principal.Audience) != 1 || captured.Principal.Audience[0] != "shunter-api" ||
		len(captured.Principal.Permissions) != 1 || captured.Principal.Permissions[0] != "principal:permission" {
		t.Fatalf("caller principal = %+v, want copied issuer/subject/audience/permissions", captured.Principal)
	}
	if scheduler == nil {
		t.Fatal("scheduler should be populated on reducer context")
	}
	if players.RowCount() != 2 {
		t.Fatalf("expected committed insert, row count = %d", players.RowCount())
	}

	resp = call("user", CallSourceExternal)
	if resp.Status != StatusFailedUser || resp.TxID != 0 {
		t.Fatalf("user failure = %+v", resp)
	}
	resp = call("panic", CallSourceExternal)
	if resp.Status != StatusFailedPanic || !errors.Is(resp.Error, ErrReducerPanic) {
		t.Fatalf("panic failure = %+v", resp)
	}
	if !errors.Is(resp.Error, errReducerBoom) {
		t.Fatalf("panic failure should preserve original panic error chain, got %+v", resp)
	}
	resp = call("commit-user", CallSourceExternal)
	if resp.Status != StatusFailedUser || resp.Error == nil || !strings.Contains(resp.Error.Error(), "commit:") {
		t.Fatalf("commit user failure = %+v", resp)
	}
	resp = call("commit-internal", CallSourceExternal)
	if resp.Status != StatusFailedInternal || resp.Error == nil || !strings.Contains(resp.Error.Error(), "commit:") {
		t.Fatalf("commit internal failure = %+v", resp)
	}
	resp = call("OnConnect", CallSourceExternal)
	if !errors.Is(resp.Error, ErrLifecycleReducer) {
		t.Fatalf("external lifecycle call should be rejected, got %+v", resp)
	}
	resp = call("OnConnect", CallSourceScheduled)
	if !errors.Is(resp.Error, ErrLifecycleReducer) {
		t.Fatalf("scheduled lifecycle call should be rejected, got %+v", resp)
	}
	resp = call("missing", CallSourceExternal)
	if !errors.Is(resp.Error, ErrReducerNotFound) {
		t.Fatalf("missing reducer should surface ErrReducerNotFound, got %+v", resp)
	}
}

func TestHandleCallReducerNilResponseChannelDoesNotPanic(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "players", Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}, {Name: "name", Type: types.KindString}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := eng.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	rr := NewReducerRegistry()
	if err := rr.Register(RegisteredReducer{Name: "ok", Handler: func(*types.ReducerContext, []byte) ([]byte, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()
	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleCallReducer should tolerate nil ResponseCh, panicked with %v", r)
		}
	}()

	exec.handleCallReducer(CallReducerCmd{
		Request: ReducerRequest{ReducerName: "ok", Source: CallSourceExternal},
	})
}
