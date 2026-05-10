package executor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type executorMetricObserver struct {
	mu               sync.Mutex
	commands         []executorCommandMetric
	commandDurations []executorCommandMetric
	inboxDepths      []int
	fatalGauge       bool
	reducerCalls     []reducerMetric
	reducerDurations []reducerMetric
	commitDurations  []string
}

type executorCommandMetric struct {
	kind   string
	result string
}

type reducerMetric struct {
	reducer string
	result  string
}

func (o *executorMetricObserver) PanicStackEnabled() bool { return false }
func (o *executorMetricObserver) LogExecutorReducerPanic(string, error, types.TxID, string) {
}
func (o *executorMetricObserver) LogExecutorLifecycleReducerFailed(string, string, error) {}
func (o *executorMetricObserver) LogSubscriptionFanoutError(string, *types.ConnectionID, error) {
}

func (o *executorMetricObserver) LogExecutorFatal(error, string, types.TxID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fatalGauge = true
}

func (o *executorMetricObserver) RecordExecutorCommand(kind, result string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.commands = append(o.commands, executorCommandMetric{kind: kind, result: result})
}

func (o *executorMetricObserver) RecordExecutorCommandDuration(kind, result string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.commandDurations = append(o.commandDurations, executorCommandMetric{kind: kind, result: result})
}

func (o *executorMetricObserver) RecordExecutorInboxDepth(depth int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inboxDepths = append(o.inboxDepths, depth)
}

func (o *executorMetricObserver) RecordReducerCall(reducer, result string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reducerCalls = append(o.reducerCalls, reducerMetric{reducer: reducer, result: result})
}

func (o *executorMetricObserver) RecordReducerDuration(reducer, result string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reducerDurations = append(o.reducerDurations, reducerMetric{reducer: reducer, result: result})
}

func (o *executorMetricObserver) RecordStoreCommitDuration(result string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.commitDurations = append(o.commitDurations, result)
}

func (o *executorMetricObserver) requireCommand(t *testing.T, kind, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.commands {
		if got.kind == kind && got.result == result {
			return
		}
	}
	t.Fatalf("missing command metric kind=%q result=%q in %+v", kind, result, o.commands)
}

func (o *executorMetricObserver) requireCommandDuration(t *testing.T, kind, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.commandDurations {
		if got.kind == kind && got.result == result {
			return
		}
	}
	t.Fatalf("missing command duration kind=%q result=%q in %+v", kind, result, o.commandDurations)
}

func (o *executorMetricObserver) waitCommandDuration(t *testing.T, kind, result string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		o.mu.Lock()
		for _, got := range o.commandDurations {
			if got.kind == kind && got.result == result {
				o.mu.Unlock()
				return
			}
		}
		o.mu.Unlock()
		select {
		case <-tick.C:
		case <-deadline:
			o.requireCommandDuration(t, kind, result)
			return
		}
	}
}

func (o *executorMetricObserver) requireNoCommandDuration(t *testing.T, kind, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.commandDurations {
		if got.kind == kind && got.result == result {
			t.Fatalf("unexpected command duration kind=%q result=%q in %+v", kind, result, o.commandDurations)
		}
	}
}

func (o *executorMetricObserver) requireInboxDepth(t *testing.T, depth int) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.inboxDepths {
		if got == depth {
			return
		}
	}
	t.Fatalf("missing inbox depth %d in %v", depth, o.inboxDepths)
}

func (o *executorMetricObserver) requireReducer(t *testing.T, reducer, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.reducerCalls {
		if got.reducer == reducer && got.result == result {
			return
		}
	}
	t.Fatalf("missing reducer metric reducer=%q result=%q in %+v", reducer, result, o.reducerCalls)
}

func (o *executorMetricObserver) requireReducerDuration(t *testing.T, reducer, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.reducerDurations {
		if got.reducer == reducer && got.result == result {
			return
		}
	}
	t.Fatalf("missing reducer duration reducer=%q result=%q in %+v", reducer, result, o.reducerDurations)
}

func (o *executorMetricObserver) requireStoreCommitDuration(t *testing.T, result string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.commitDurations {
		if got == result {
			return
		}
	}
	t.Fatalf("missing store commit duration result=%q in %+v", result, o.commitDurations)
}

func TestExecutorMetricsSubmitRejectionAndInboxDepth(t *testing.T) {
	observer := &executorMetricObserver{}
	exec, _ := setupExecutorWithObserver(observer, ExecutorConfig{InboxCapacity: 1, RejectOnFull: true})

	if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "ok", Source: CallSourceExternal}, ResponseCh: make(chan ReducerResponse, 1)}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "ok", Source: CallSourceExternal}, ResponseCh: make(chan ReducerResponse, 1)}); !errors.Is(err, ErrExecutorBusy) {
		t.Fatalf("second submit = %v, want ErrExecutorBusy", err)
	}
	observer.requireCommand(t, "call_reducer", "rejected")
	observer.requireNoCommandDuration(t, "call_reducer", "rejected")
	observer.requireInboxDepth(t, 1)
}

func TestExecutorSubmitObservabilityDoesNotBlockShutdownLock(t *testing.T) {
	observer := newBlockingCommandObserver("call_reducer", "rejected")
	exec, _ := setupExecutorWithObserver(observer, ExecutorConfig{InboxCapacity: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	submitDone := make(chan error, 1)
	go func() {
		submitDone <- exec.SubmitWithContext(context.Background(), CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "ok", Source: CallSourceExternal},
			ResponseCh: make(chan ReducerResponse, 1),
		})
	}()

	select {
	case <-observer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for blocking observer")
	}

	shutdownDone := make(chan struct{})
	go func() {
		exec.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Shutdown blocked behind submit-time observability")
	}

	close(observer.release)
	if err := <-submitDone; !errors.Is(err, ErrExecutorNotStarted) {
		t.Fatalf("SubmitWithContext error = %v, want ErrExecutorNotStarted", err)
	}
}

func TestExecutorMetricsCommandDurationRecordsOnlyDequeuedCommands(t *testing.T) {
	observer := &executorMetricObserver{}
	exec, _ := setupExecutorWithObserver(observer, ExecutorConfig{InboxCapacity: 4})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "ok", Source: CallSourceExternal}, ResponseCh: respCh}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	resp := receiveReducerResponse(t, respCh)
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d, err=%v, want committed", resp.Status, resp.Error)
	}
	observer.waitCommandDuration(t, "call_reducer", "ok")
	observer.requireCommand(t, "call_reducer", "ok")
	observer.requireCommandDuration(t, "call_reducer", "ok")
	observer.requireStoreCommitDuration(t, "ok")
	observer.requireInboxDepth(t, 0)
}

func TestReducerMetricsResultMappingsAndDeclaredLabels(t *testing.T) {
	observer := &executorMetricObserver{}
	exec, _ := setupExecutorWithObserver(observer, ExecutorConfig{InboxCapacity: 16})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	cases := []struct {
		name       string
		reducer    string
		wantStatus ReducerStatus
		wantResult string
	}{
		{name: "committed", reducer: "ok", wantStatus: StatusCommitted, wantResult: "committed"},
		{name: "user", reducer: "user_error", wantStatus: StatusFailedUser, wantResult: "failed_user"},
		{name: "panic", reducer: "panic", wantStatus: StatusFailedPanic, wantResult: "failed_panic"},
		{name: "permission", reducer: "permissioned", wantStatus: StatusFailedPermission, wantResult: "failed_permission"},
		{name: "internal", reducer: "missing", wantStatus: StatusFailedInternal, wantResult: "failed_internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			respCh := make(chan ReducerResponse, 1)
			if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: tc.reducer, Source: CallSourceExternal}, ResponseCh: respCh}); err != nil {
				t.Fatalf("submit %s: %v", tc.reducer, err)
			}
			resp := receiveReducerResponse(t, respCh)
			if resp.Status != tc.wantStatus {
				t.Fatalf("%s status = %d, err=%v, want %d", tc.reducer, resp.Status, resp.Error, tc.wantStatus)
			}
			wantReducer := tc.reducer
			if tc.reducer == "missing" {
				wantReducer = "unknown"
			}
			observer.requireReducer(t, wantReducer, tc.wantResult)
			if tc.reducer != "permissioned" && tc.reducer != "missing" {
				observer.requireReducerDuration(t, tc.reducer, tc.wantResult)
			}
		})
	}
}

func TestExecutorMetricsFatalGaugeLatches(t *testing.T) {
	observer := &executorMetricObserver{}
	exec, _ := setupExecutorWithObserver(observer, ExecutorConfig{
		InboxCapacity: 4,
		Durability:    panicDurability{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{Request: ReducerRequest{ReducerName: "ok", Source: CallSourceExternal}, ResponseCh: respCh}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	resp := receiveReducerResponse(t, respCh)
	if resp.Status != StatusFailedInternal {
		t.Fatalf("status = %d, err=%v, want internal failure", resp.Status, resp.Error)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if !observer.fatalGauge {
		t.Fatal("executor fatal gauge was not recorded")
	}
}

func setupExecutorWithObserver(observer Observer, cfg ExecutorConfig) (*Executor, schema.SchemaRegistry) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "items",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "body", Type: types.KindString},
		},
	})
	engine, _ := b.Build(schema.EngineOptions{})
	reg := engine.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	rr := NewReducerRegistry()
	_ = rr.Register(RegisteredReducer{Name: "ok", Handler: func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(uint64(time.Now().UnixNano())), types.NewString("ok")})
		return nil, err
	}})
	_ = rr.Register(RegisteredReducer{Name: "user_error", Handler: func(*types.ReducerContext, []byte) ([]byte, error) {
		return nil, errors.New("user failed")
	}})
	_ = rr.Register(RegisteredReducer{Name: "panic", Handler: func(*types.ReducerContext, []byte) ([]byte, error) {
		panic("boom")
	}})
	_ = rr.Register(RegisteredReducer{Name: "permissioned", Handler: func(*types.ReducerContext, []byte) ([]byte, error) {
		return nil, nil
	}, RequiredPermissions: []string{"items:write"}})
	rr.Freeze()
	if cfg.Durability == nil {
		cfg.Durability = noopDurability{}
	}
	if cfg.Subscriptions == nil {
		cfg.Subscriptions = noopSubs{}
	}
	cfg.Observer = observer
	return NewExecutor(cfg, rr, cs, reg, 0), reg
}

func receiveReducerResponse(t *testing.T, ch <-chan ReducerResponse) ReducerResponse {
	t.Helper()
	select {
	case resp := <-ch:
		return resp
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reducer response")
		return ReducerResponse{}
	}
}

type panicDurability struct{}

func (panicDurability) EnqueueCommitted(types.TxID, *store.Changeset) { panic("durability failed") }
func (panicDurability) WaitUntilDurable(types.TxID) <-chan types.TxID { return nil }
func (panicDurability) FatalError() error                             { return nil }

var _ subscription.SubscriptionManager = noopSubs{}

type blockingCommandObserver struct {
	kind    string
	result  string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingCommandObserver(kind, result string) *blockingCommandObserver {
	return &blockingCommandObserver{
		kind:    kind,
		result:  result,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (o *blockingCommandObserver) PanicStackEnabled() bool { return false }
func (o *blockingCommandObserver) LogExecutorReducerPanic(string, error, types.TxID, string) {
}
func (o *blockingCommandObserver) LogExecutorLifecycleReducerFailed(string, string, error) {}
func (o *blockingCommandObserver) LogSubscriptionFanoutError(string, *types.ConnectionID, error) {
}
func (o *blockingCommandObserver) LogExecutorFatal(error, string, types.TxID) {}
func (o *blockingCommandObserver) RecordExecutorCommand(kind, result string) {
	if kind != o.kind || result != o.result {
		return
	}
	o.once.Do(func() { close(o.entered) })
	<-o.release
}
func (o *blockingCommandObserver) RecordExecutorCommandDuration(string, string, time.Duration) {}
func (o *blockingCommandObserver) RecordExecutorInboxDepth(int)                                {}
func (o *blockingCommandObserver) RecordReducerCall(string, string)                            {}
func (o *blockingCommandObserver) RecordReducerDuration(string, string, time.Duration)         {}
func (o *blockingCommandObserver) RecordStoreCommitDuration(string, time.Duration)             {}
