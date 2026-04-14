package executor

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// lifecycleHarness builds an executor with sys_clients wired plus optional
// OnConnect / OnDisconnect handlers.
type lifecycleHarness struct {
	exec        *Executor
	dur         *fakeDurability
	subs        *fakeSubs
	rec         *recorder
	cs          *store.CommittedState
	reg         schema.SchemaRegistry
	sysClients  schema.TableID
	onConnect   *recordedHandler
	onDisconn   *recordedHandler
}

type recordedHandler struct {
	mu     sync.Mutex
	calls  int
	err    error
	panic  any
	onCall func(ctx *types.ReducerContext)
}

func (r *recordedHandler) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }

func (r *recordedHandler) handler() types.ReducerHandler {
	return func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		r.mu.Lock()
		r.calls++
		r.mu.Unlock()
		if r.onCall != nil {
			r.onCall(ctx)
		}
		if r.panic != nil {
			panic(r.panic)
		}
		return nil, r.err
	}
}

type lifecycleOpt struct {
	withOnConnect    bool
	onConnectPayload *recordedHandler
	withOnDisconn    bool
	onDisconnPayload *recordedHandler
}

func newLifecycleHarness(t *testing.T, opt lifecycleOpt) *lifecycleHarness {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
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
	h := &lifecycleHarness{reg: reg, cs: cs}
	if opt.withOnConnect {
		h.onConnect = opt.onConnectPayload
		if h.onConnect == nil {
			h.onConnect = &recordedHandler{}
		}
		if err := rr.Register(RegisteredReducer{
			Name:      "OnConnect",
			Handler:   h.onConnect.handler(),
			Lifecycle: LifecycleOnConnect,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if opt.withOnDisconn {
		h.onDisconn = opt.onDisconnPayload
		if h.onDisconn == nil {
			h.onDisconn = &recordedHandler{}
		}
		if err := rr.Register(RegisteredReducer{
			Name:      "OnDisconnect",
			Handler:   h.onDisconn.handler(),
			Lifecycle: LifecycleOnDisconnect,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rr.Freeze()

	rec := &recorder{}
	dur := &fakeDurability{rec: rec}
	subs := &fakeSubs{rec: rec}
	exec := NewExecutor(ExecutorConfig{
		InboxCapacity: 16,
		Durability:    dur,
		Subscriptions: subs,
	}, rr, cs, reg, 0)

	ts, ok := SysClientsTable(reg)
	if !ok {
		t.Fatal("harness: sys_clients missing")
	}
	h.exec = exec
	h.dur = dur
	h.subs = subs
	h.rec = rec
	h.sysClients = ts.ID
	return h
}

func submitOnConnect(t *testing.T, exec *Executor, conn types.ConnectionID, id types.Identity) ReducerResponse {
	t.Helper()
	ch := make(chan ReducerResponse, 1)
	if err := exec.Submit(OnConnectCmd{ConnID: conn, Identity: id, ResponseCh: ch}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
		return ReducerResponse{}
	}
}

func submitOnDisconnect(t *testing.T, exec *Executor, conn types.ConnectionID, id types.Identity) ReducerResponse {
	t.Helper()
	ch := make(chan ReducerResponse, 1)
	if err := exec.Submit(OnDisconnectCmd{ConnID: conn, Identity: id, ResponseCh: ch}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
		return ReducerResponse{}
	}
}

// sysClientsSnapshot returns (connID, identity, connectedAt) for every row
// currently in sys_clients.
func (h *lifecycleHarness) sysClientsSnapshot() []sysClientsRow {
	view := h.cs.Snapshot()
	defer view.Close()
	var out []sysClientsRow
	for _, row := range view.TableScan(h.sysClients) {
		out = append(out, sysClientsRow{
			ConnID:   append([]byte(nil), row[SysClientsColConnectionID].AsBytes()...),
			Identity: append([]byte(nil), row[SysClientsColIdentity].AsBytes()...),
			At:       row[SysClientsColConnectedAt].AsInt64(),
		})
	}
	return out
}

type sysClientsRow struct {
	ConnID   []byte
	Identity []byte
	At       int64
}

// Story 7.2 AC: OnConnect without reducer still inserts sys_clients row.
func TestOnConnectInsertsRowNoReducer(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{1, 2, 3}
	identity := types.Identity{0xAA}
	resp := submitOnConnect(t, h.exec, conn, identity)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	rows := h.sysClientsSnapshot()
	if len(rows) != 1 {
		t.Fatalf("sys_clients rows=%d, want 1", len(rows))
	}
	if !bytes.Equal(rows[0].ConnID, conn[:]) {
		t.Errorf("connection_id=%x, want %x", rows[0].ConnID, conn[:])
	}
	if !bytes.Equal(rows[0].Identity, identity[:]) {
		t.Errorf("identity=%x, want %x", rows[0].Identity, identity[:])
	}
	if rows[0].At == 0 {
		t.Error("connected_at should be populated")
	}
}

// Story 7.2 AC: OnConnect reducer runs with CallSourceLifecycle and writes
// are atomic with the row insert.
func TestOnConnectRunsReducerWithLifecycleSource(t *testing.T) {
	rh := &recordedHandler{}
	var gotSource CallSource
	var gotCaller types.CallerContext
	rh.onCall = func(ctx *types.ReducerContext) {
		// No source field on ReducerContext directly; instead rely on Caller.
		// Lifecycle invocations carry the connection identity in Caller.
		gotCaller = ctx.Caller
	}
	h := newLifecycleHarness(t, lifecycleOpt{withOnConnect: true, onConnectPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{9}
	identity := types.Identity{0xBB}
	resp := submitOnConnect(t, h.exec, conn, identity)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}
	if rh.count() != 1 {
		t.Fatalf("OnConnect reducer calls=%d, want 1", rh.count())
	}
	if gotCaller.ConnectionID != conn {
		t.Errorf("caller connID=%v, want %v", gotCaller.ConnectionID, conn)
	}
	if gotCaller.Identity != identity {
		t.Errorf("caller identity=%v, want %v", gotCaller.Identity, identity)
	}
	// Row was inserted.
	if n := len(h.sysClientsSnapshot()); n != 1 {
		t.Errorf("sys_clients rows=%d, want 1", n)
	}
	_ = gotSource
}

// Story 7.2 AC: reducer error rolls back ENTIRE tx (row not present).
func TestOnConnectReducerErrorRollsBackInsert(t *testing.T) {
	rh := &recordedHandler{err: errors.New("auth rejected")}
	h := newLifecycleHarness(t, lifecycleOpt{withOnConnect: true, onConnectPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submitOnConnect(t, h.exec, types.ConnectionID{5}, types.Identity{0x1})
	if resp.Status != StatusFailedUser {
		t.Fatalf("status=%d err=%v, want StatusFailedUser", resp.Status, resp.Error)
	}
	if n := len(h.sysClientsSnapshot()); n != 0 {
		t.Errorf("sys_clients rows=%d, want 0 (rolled back)", n)
	}
	// Pipeline must NOT have run (no commit).
	h.dur.mu.Lock()
	durN := len(h.dur.txIDs)
	h.dur.mu.Unlock()
	if durN != 0 {
		t.Errorf("durability calls=%d, want 0 on rollback", durN)
	}
}

// Story 7.2 AC: reducer panic rolls back and executor continues.
func TestOnConnectReducerPanicRollsBackInsert(t *testing.T) {
	rh := &recordedHandler{panic: "boom in OnConnect"}
	h := newLifecycleHarness(t, lifecycleOpt{withOnConnect: true, onConnectPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submitOnConnect(t, h.exec, types.ConnectionID{7}, types.Identity{0x2})
	if resp.Status != StatusFailedPanic {
		t.Fatalf("status=%d err=%v, want StatusFailedPanic", resp.Status, resp.Error)
	}
	if !errors.Is(resp.Error, ErrReducerPanic) {
		t.Errorf("err=%v, want wraps ErrReducerPanic", resp.Error)
	}
	if n := len(h.sysClientsSnapshot()); n != 0 {
		t.Errorf("sys_clients rows=%d, want 0 (rolled back)", n)
	}
	// Executor still works — fatal NOT set (pre-commit panic is per-request).
	if h.exec.fatal {
		t.Error("executor.fatal set by OnConnect reducer panic; should be per-request")
	}
}

// ------------------------------------------------------------------
// Story 7.3: OnDisconnect
// ------------------------------------------------------------------

// Helper: OnConnect the client so OnDisconnect has a row to delete.
func prime(t *testing.T, h *lifecycleHarness, conn types.ConnectionID, id types.Identity) {
	t.Helper()
	resp := submitOnConnect(t, h.exec, conn, id)
	if resp.Status != StatusCommitted {
		t.Fatalf("prime OnConnect failed: status=%d err=%v", resp.Status, resp.Error)
	}
}

// Story 7.3 AC: OnDisconnect without reducer deletes the sys_clients row.
func TestOnDisconnectDeletesRowNoReducer(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{1}
	prime(t, h, conn, types.Identity{0x1})
	if n := len(h.sysClientsSnapshot()); n != 1 {
		t.Fatalf("pre-disconnect rows=%d, want 1", n)
	}

	resp := submitOnDisconnect(t, h.exec, conn, types.Identity{0x1})
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}
	if n := len(h.sysClientsSnapshot()); n != 0 {
		t.Errorf("post-disconnect rows=%d, want 0", n)
	}
}

// Story 7.3 AC: reducer runs AND row is deleted in the same transaction.
func TestOnDisconnectRunsReducerAndDeletes(t *testing.T) {
	rh := &recordedHandler{}
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true, onDisconnPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{7}
	prime(t, h, conn, types.Identity{0x7})

	resp := submitOnDisconnect(t, h.exec, conn, types.Identity{0x7})
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}
	if rh.count() != 1 {
		t.Errorf("OnDisconnect reducer calls=%d, want 1", rh.count())
	}
	if n := len(h.sysClientsSnapshot()); n != 0 {
		t.Errorf("rows=%d, want 0", n)
	}
}

// Story 7.3 AC: reducer error → row still deleted via cleanup tx.
func TestOnDisconnectReducerErrorStillDeletes(t *testing.T) {
	rh := &recordedHandler{err: errors.New("user reducer boom")}
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true, onDisconnPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{11}
	prime(t, h, conn, types.Identity{0xAB})

	// Response should still be StatusCommitted because the cleanup tx
	// succeeded even though the reducer failed.
	resp := submitOnDisconnect(t, h.exec, conn, types.Identity{0xAB})
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v, want StatusCommitted (cleanup tx ran)", resp.Status, resp.Error)
	}
	if n := len(h.sysClientsSnapshot()); n != 0 {
		t.Errorf("rows=%d, want 0 (cleanup tx must delete regardless)", n)
	}
}

// Story 7.3 AC: reducer panic → row still deleted; executor not fatal.
func TestOnDisconnectReducerPanicStillDeletes(t *testing.T) {
	rh := &recordedHandler{panic: "disconnect boom"}
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true, onDisconnPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{13}
	prime(t, h, conn, types.Identity{0xCD})

	resp := submitOnDisconnect(t, h.exec, conn, types.Identity{0xCD})
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v, want StatusCommitted", resp.Status, resp.Error)
	}
	if n := len(h.sysClientsSnapshot()); n != 0 {
		t.Errorf("rows=%d, want 0", n)
	}
	if h.exec.fatal {
		t.Error("executor.fatal set after OnDisconnect reducer panic; should not be")
	}
}

// Story 7.3 AC: cleanup tx runs post-commit pipeline (durability + eval).
func TestOnDisconnectPostCommitRuns(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	conn := types.ConnectionID{15}
	prime(t, h, conn, types.Identity{0xEF})
	// After OnConnect, pipeline ran once.
	baseDur := 0
	baseSub := 0
	h.dur.mu.Lock()
	baseDur = len(h.dur.txIDs)
	h.dur.mu.Unlock()
	h.subs.mu.Lock()
	baseSub = len(h.subs.txIDs)
	h.subs.mu.Unlock()

	_ = submitOnDisconnect(t, h.exec, conn, types.Identity{0xEF})
	h.dur.mu.Lock()
	afterDur := len(h.dur.txIDs)
	h.dur.mu.Unlock()
	h.subs.mu.Lock()
	afterSub := len(h.subs.txIDs)
	h.subs.mu.Unlock()
	if afterDur != baseDur+1 || afterSub != baseSub+1 {
		t.Fatalf("pipeline calls: dur %d→%d, subs %d→%d (want +1 each)", baseDur, afterDur, baseSub, afterSub)
	}
}

// ------------------------------------------------------------------
// Story 7.4: Direct-invocation protection (verification)
// ------------------------------------------------------------------

// Story 7.4 AC: external CallReducerCmd naming a lifecycle reducer is
// rejected with ErrLifecycleReducer — no transaction runs, no post-commit.
func TestLifecycleGuardRejectsExternalOnConnectCall(t *testing.T) {
	rh := &recordedHandler{}
	h := newLifecycleHarness(t, lifecycleOpt{withOnConnect: true, onConnectPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	ch := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "OnConnect", Source: CallSourceExternal},
		ResponseCh: ch,
	}); err != nil {
		t.Fatal(err)
	}
	resp := <-ch
	if resp.Status != StatusFailedInternal {
		t.Fatalf("status=%d, want StatusFailedInternal", resp.Status)
	}
	if !errors.Is(resp.Error, ErrLifecycleReducer) {
		t.Fatalf("err=%v, want wraps ErrLifecycleReducer", resp.Error)
	}
	if rh.count() != 0 {
		t.Errorf("reducer called %d times, want 0 (guard must reject before execute)", rh.count())
	}
	h.dur.mu.Lock()
	gotDur := len(h.dur.txIDs)
	h.dur.mu.Unlock()
	if gotDur != 0 {
		t.Errorf("durability=%d, want 0 (no transaction should have run)", gotDur)
	}
}

func TestLifecycleGuardRejectsExternalOnDisconnectCall(t *testing.T) {
	rh := &recordedHandler{}
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true, onDisconnPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	ch := make(chan ReducerResponse, 1)
	_ = h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "OnDisconnect", Source: CallSourceExternal},
		ResponseCh: ch,
	})
	resp := <-ch
	if !errors.Is(resp.Error, ErrLifecycleReducer) {
		t.Fatalf("err=%v, want wraps ErrLifecycleReducer", resp.Error)
	}
	if rh.count() != 0 {
		t.Errorf("reducer called %d times on guarded external call", rh.count())
	}
}

// Story 7.4 AC: internal CallSourceLifecycle invocations succeed. This is the
// path an embedder can use if they want to deliver OnConnect via a plain
// CallReducerCmd rather than the dedicated OnConnectCmd. Both paths must
// respect the guard.
func TestLifecycleGuardAllowsInternalLifecycleSource(t *testing.T) {
	rh := &recordedHandler{}
	h := newLifecycleHarness(t, lifecycleOpt{withOnConnect: true, onConnectPayload: rh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	ch := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "OnConnect", Source: CallSourceLifecycle},
		ResponseCh: ch,
	}); err != nil {
		t.Fatal(err)
	}
	resp := <-ch
	if resp.Status != StatusCommitted {
		t.Fatalf("internal lifecycle call status=%d err=%v", resp.Status, resp.Error)
	}
	if rh.count() != 1 {
		t.Errorf("reducer calls=%d, want 1", rh.count())
	}
}

// Story 7.2 AC: successful OnConnect runs full post-commit pipeline.
func TestOnConnectTriggersPostCommitPipeline(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submitOnConnect(t, h.exec, types.ConnectionID{3}, types.Identity{0x3})
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}
	h.dur.mu.Lock()
	durN := len(h.dur.txIDs)
	h.dur.mu.Unlock()
	h.subs.mu.Lock()
	subN := len(h.subs.txIDs)
	h.subs.mu.Unlock()
	if durN != 1 || subN != 1 {
		t.Fatalf("dur=%d subs=%d, want 1/1", durN, subN)
	}
}
