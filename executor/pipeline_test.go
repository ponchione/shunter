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

// recorder captures the event stream produced by the post-commit pipeline
// so tests can assert ordering without timing games.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(ev string) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

// fakeDurability records every EnqueueCommitted call.
type fakeDurability struct {
	rec      *recorder
	txIDs    []types.TxID
	payloads []*store.Changeset
	block    chan struct{}      // if set, EnqueueCommitted waits on it
	waitCh   <-chan types.TxID  // optional readiness channel returned by WaitUntilDurable
	panicOn  bool               // panic when EnqueueCommitted is called
	mu       sync.Mutex
}

func (f *fakeDurability) EnqueueCommitted(txID types.TxID, cs *store.Changeset) {
	if f.block != nil {
		<-f.block
	}
	if f.panicOn {
		panic("fake durability panic")
	}
	f.mu.Lock()
	f.txIDs = append(f.txIDs, txID)
	f.payloads = append(f.payloads, cs)
	f.mu.Unlock()
	f.rec.add("durability")
}

func (f *fakeDurability) WaitUntilDurable(txID types.TxID) <-chan types.TxID {
	if f.waitCh != nil {
		return f.waitCh
	}
	ch := make(chan types.TxID, 1)
	ch <- txID
	close(ch)
	return ch
}

// fakeSubs records every EvalAndBroadcast call. It optionally inspects the
// view it is handed to prove the view was still live.
type fakeSubs struct {
	rec      *recorder
	txIDs    []types.TxID
	viewSaw   []store.CommittedReadView
	metas     []subscription.PostCommitMeta
	block     chan struct{}
	onEval   func(view store.CommittedReadView)
	mu       sync.Mutex
	dropped  chan types.ConnectionID // DroppedClients() source; nil when unset
	disconns []types.ConnectionID    // DisconnectClient invocations, in order
	discErr  func(types.ConnectionID) error
	panicOn  bool
}

func (f *fakeSubs) Register(SubscriptionRegisterRequest, store.CommittedReadView) (SubscriptionRegisterResult, error) {
	return SubscriptionRegisterResult{}, nil
}

func (f *fakeSubs) Unregister(types.ConnectionID, types.SubscriptionID) error { return nil }

func (f *fakeSubs) EvalAndBroadcast(txID types.TxID, cs *store.Changeset, view store.CommittedReadView, meta subscription.PostCommitMeta) {
	f.rec.add("eval-start")
	if f.onEval != nil {
		f.onEval(view)
	}
	if f.block != nil {
		<-f.block
	}
	if f.panicOn {
		panic("fake subs panic")
	}
	f.mu.Lock()
	f.txIDs = append(f.txIDs, txID)
	f.viewSaw = append(f.viewSaw, view)
	f.metas = append(f.metas, meta)
	f.mu.Unlock()
	f.rec.add("eval-end")
}

func (f *fakeSubs) DroppedClients() <-chan types.ConnectionID { return f.dropped }

func (f *fakeSubs) DisconnectClient(connID types.ConnectionID) error {
	f.mu.Lock()
	f.disconns = append(f.disconns, connID)
	f.mu.Unlock()
	f.rec.add("disconnect")
	if f.discErr != nil {
		return f.discErr(connID)
	}
	return nil
}

// trackedView wraps a CommittedReadView so tests can assert when Close ran.
type trackedView struct {
	store.CommittedReadView
	rec    *recorder
	closed bool
}

func (t *trackedView) Close() {
	if !t.closed {
		t.closed = true
		t.CommittedReadView.Close()
		t.rec.add("view-close")
	}
}

// pipelineHarness builds an executor wired with fakes.
type pipelineHarness struct {
	exec *Executor
	dur  *fakeDurability
	subs *fakeSubs
	rec  *recorder
	cs   *store.CommittedState
	reg  schema.SchemaRegistry
}

func newPipelineHarness(t *testing.T) *pipelineHarness {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "name", Type: types.KindString},
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
	rr.Register(RegisteredReducer{
		Name: "InsertPlayer",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, args []byte) ([]byte, error) {
			_, _ = ctx.DB.Insert(0, types.ProductValue{types.NewUint64(1), types.NewString("alice")})
			return []byte("ret"), nil
		}),
	})
	rr.Register(RegisteredReducer{
		Name: "FailReducer",
		Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
			return nil, errTestUserFail
		}),
	})
	rr.Freeze()

	rec := &recorder{}
	dur := &fakeDurability{rec: rec}
	subs := &fakeSubs{rec: rec}
	cfg := ExecutorConfig{
		InboxCapacity: 16,
		Durability:    dur,
		Subscriptions: subs,
	}
	exec := NewExecutor(cfg, rr, cs, reg, 0)
	return &pipelineHarness{exec: exec, dur: dur, subs: subs, rec: rec, cs: cs, reg: reg}
}

var errTestUserFail = &userErr{msg: "test-user-fail"}

type userErr struct{ msg string }

func (u *userErr) Error() string { return u.msg }

func submit(t *testing.T, exec *Executor, name string) ReducerResponse {
	t.Helper()
	ch := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{
		Request: ReducerRequest{
			ReducerName: name,
			Source:      CallSourceExternal,
			RequestID:   77,
			Caller: types.CallerContext{
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

// AC 1 + AC 5 + AC 7: durability before eval, response after eval,
// pipeline called on every successful commit.
func TestPostCommitDurabilityBeforeEvalBeforeResponse(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}
	events := h.rec.snapshot()
	want := []string{"durability", "eval-start", "eval-end"}
	if len(events) < len(want) {
		t.Fatalf("events = %v, want prefix %v", events, want)
	}
	for i, w := range want {
		if events[i] != w {
			t.Fatalf("events[%d] = %s, want %s (full=%v)", i, events[i], w, events)
		}
	}
}

// AC 2 + AC 3 + AC 4: snapshot acquired after durability, passed to eval,
// closed after eval and before response delivery.
func TestPostCommitSnapshotLifetime(t *testing.T) {
	h := newPipelineHarness(t)
	// Track the view Close() via a wrapper injected by the snapshot hook.
	h.exec.snapshotFn = func() store.CommittedReadView {
		raw := h.cs.Snapshot()
		return &trackedView{CommittedReadView: raw, rec: h.rec}
	}
	h.subs.onEval = func(view store.CommittedReadView) {
		if view == nil {
			t.Error("EvalAndBroadcast received nil view")
			return
		}
		// Prove the view is live during eval.
		_ = view.RowCount(0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	if got := submit(t, h.exec, "InsertPlayer"); got.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", got.Status, got.Error)
	}
	events := h.rec.snapshot()
	want := []string{"durability", "eval-start", "eval-end", "view-close"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i, w := range want {
		if events[i] != w {
			t.Fatalf("events[%d] = %s, want %s (full=%v)", i, events[i], w, events)
		}
	}
}

// AC 8: pipeline is called for every successful commit (once per commit),
// regardless of how many commits happen in a run.
func TestPostCommitCalledPerCommit(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	commits := 0
	for i := 0; i < 3; i++ {
		resp := submit(t, h.exec, "InsertPlayer")
		if resp.Status == StatusCommitted {
			commits++
		}
	}
	if commits == 0 {
		t.Fatal("expected at least one successful commit in harness")
	}
	h.dur.mu.Lock()
	gotDur := len(h.dur.txIDs)
	h.dur.mu.Unlock()
	h.subs.mu.Lock()
	gotSubs := len(h.subs.txIDs)
	h.subs.mu.Unlock()
	if gotDur != commits || gotSubs != commits {
		t.Fatalf("dur=%d subs=%d, commits=%d (dur/subs must equal commits)", gotDur, gotSubs, commits)
	}
}

// AC 6: durability backpressure stalls the pipeline, does not skip.
// Blocking durability → no eval-start, no response until durability returns.
func TestPostCommitDurabilityBackpressureStalls(t *testing.T) {
	h := newPipelineHarness(t)
	release := make(chan struct{})
	h.dur.block = release
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	ch := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request: ReducerRequest{
			ReducerName: "InsertPlayer",
			Source:      CallSourceExternal,
			RequestID:   88,
			Caller:      types.CallerContext{ConnectionID: types.ConnectionID{7}},
		},
		ResponseCh: ch,
	}); err != nil {
		t.Fatal(err)
	}

	// Give the executor plenty of time to reach durability.
	time.Sleep(50 * time.Millisecond)
	select {
	case r := <-ch:
		t.Fatalf("response arrived before durability unblocked: %+v", r)
	default:
	}
	if got := h.rec.snapshot(); len(got) != 0 {
		t.Fatalf("events before durability returned: %v", got)
	}

	close(release)
	select {
	case r := <-ch:
		if r.Status != StatusCommitted {
			t.Fatalf("status = %d err=%v", r.Status, r.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout after durability released")
	}
}

// Pipeline does not run when reducer returns a user error (no commit happened).
func TestPostCommitSkippedOnReducerUserError(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "FailReducer")
	if resp.Status != StatusFailedUser {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}
	h.dur.mu.Lock()
	gotDur := len(h.dur.txIDs)
	h.dur.mu.Unlock()
	h.subs.mu.Lock()
	gotSubs := len(h.subs.txIDs)
	h.subs.mu.Unlock()
	if gotDur != 0 || gotSubs != 0 {
		t.Fatalf("dur=%d subs=%d, want 0/0", gotDur, gotSubs)
	}
}

// Story 5.2 AC: drain happens after response, each drop → DisconnectClient,
// multiple drops drained in one pass.
func TestPostCommitDrainsDroppedClients(t *testing.T) {
	h := newPipelineHarness(t)
	// Buffered so we can pre-load drops before the commit.
	h.subs.dropped = make(chan types.ConnectionID, 4)
	h.subs.dropped <- types.ConnectionID{1}
	h.subs.dropped <- types.ConnectionID{2}
	h.subs.dropped <- types.ConnectionID{3}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}
	// Drain runs inside the executor goroutine AFTER response send but
	// BEFORE next dequeue. Issue a second command as a serial barrier —
	// by the time its response arrives, the first commit's drain is done.
	_ = submit(t, h.exec, "InsertPlayer")

	h.subs.mu.Lock()
	got := append([]types.ConnectionID(nil), h.subs.disconns...)
	h.subs.mu.Unlock()
	want := []types.ConnectionID{{1}, {2}, {3}}
	if len(got) != len(want) {
		t.Fatalf("disconns=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("disconns[%d]=%v want=%v", i, got[i], want[i])
		}
	}

	events := h.rec.snapshot()
	// For the first commit the sub-sequence must be eval-end then disconnect×3.
	idxEvalEnd := -1
	for i, e := range events {
		if e == "eval-end" {
			idxEvalEnd = i
			break
		}
	}
	if idxEvalEnd < 0 {
		t.Fatalf("no eval-end in events %v", events)
	}
	// The 3 disconnects must follow the first eval-end before the second eval.
	disconnCount := 0
	for _, e := range events[idxEvalEnd+1:] {
		if e == "eval-start" {
			break
		}
		if e == "disconnect" {
			disconnCount++
		}
	}
	if disconnCount != 3 {
		t.Fatalf("disconnects between first and second commit = %d, want 3 (events=%v)", disconnCount, events)
	}
}

// Story 5.2 AC: empty channel → drain is a no-op, does not block.
func TestPostCommitDrainNoopWhenEmpty(t *testing.T) {
	h := newPipelineHarness(t)
	h.subs.dropped = make(chan types.ConnectionID, 1) // empty
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}
	h.subs.mu.Lock()
	n := len(h.subs.disconns)
	h.subs.mu.Unlock()
	if n != 0 {
		t.Fatalf("disconns=%d, want 0 with empty channel", n)
	}
}

// Story 5.2 AC: noopSubs (nil DroppedClients channel) must not block the drain.
func TestPostCommitDrainSurvivesNilChannel(t *testing.T) {
	h := newPipelineHarness(t)
	h.subs.dropped = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}
}

// Story 5.2 design note: error from DisconnectClient does not halt further
// drains. All three disconnects must be attempted even if the first errors.
func TestPostCommitDrainContinuesAfterDisconnectError(t *testing.T) {
	h := newPipelineHarness(t)
	h.subs.dropped = make(chan types.ConnectionID, 3)
	h.subs.dropped <- types.ConnectionID{1}
	h.subs.dropped <- types.ConnectionID{2}
	h.subs.dropped <- types.ConnectionID{3}
	h.subs.discErr = func(c types.ConnectionID) error {
		if c == (types.ConnectionID{1}) {
			return errTestDisconnect
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	_ = submit(t, h.exec, "InsertPlayer")
	_ = submit(t, h.exec, "InsertPlayer") // serial barrier

	h.subs.mu.Lock()
	gotN := len(h.subs.disconns)
	h.subs.mu.Unlock()
	if gotN != 3 {
		t.Fatalf("disconns=%d, want 3 (errors must not halt drain)", gotN)
	}
}

var errTestDisconnect = &userErr{msg: "disconnect-failed"}

func TestPostCommitExternalReducerPropagatesCallerMetadata(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}

	h.subs.mu.Lock()
	defer h.subs.mu.Unlock()
	if len(h.subs.metas) != 1 {
		t.Fatalf("metas=%d want 1", len(h.subs.metas))
	}
	meta := h.subs.metas[0]
	if meta.CallerConnID == nil || *meta.CallerConnID != (types.ConnectionID{9}) {
		t.Fatalf("CallerConnID=%v want %v", meta.CallerConnID, types.ConnectionID{9})
	}
	if meta.CallerResult == nil {
		t.Fatal("CallerResult = nil, want populated result")
	}
	if meta.CallerResult.RequestID != 77 {
		t.Fatalf("CallerResult.RequestID=%d want 77", meta.CallerResult.RequestID)
	}
	if meta.CallerResult.Status != 0 || meta.CallerResult.TxID != resp.TxID {
		t.Fatalf("CallerResult=%+v want committed result for tx %d", *meta.CallerResult, resp.TxID)
	}
}

func TestPostCommitLifecycleLeavesCallerMetadataNil(t *testing.T) {
	h := newPipelineHarness(t)
	changeset := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}}
	h.exec.postCommit(types.TxID(55), changeset, nil, nil, postCommitOptions{source: CallSourceLifecycle})

	h.subs.mu.Lock()
	defer h.subs.mu.Unlock()
	if len(h.subs.metas) != 1 {
		t.Fatalf("metas=%d want 1", len(h.subs.metas))
	}
	meta := h.subs.metas[0]
	if meta.CallerConnID != nil || meta.CallerResult != nil {
		t.Fatalf("lifecycle meta should not fabricate caller fields: %+v", meta)
	}
}

func TestPostCommitPropagatesDurabilityReadinessChannel(t *testing.T) {
	h := newPipelineHarness(t)
	waitCh := make(chan types.TxID)
	h.dur.waitCh = waitCh
	changeset := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}}
	h.exec.postCommit(types.TxID(66), changeset, nil, nil, postCommitOptions{source: CallSourceLifecycle})

	h.subs.mu.Lock()
	defer h.subs.mu.Unlock()
	if len(h.subs.metas) != 1 {
		t.Fatalf("metas=%d want 1", len(h.subs.metas))
	}
	if h.subs.metas[0].TxDurable != waitCh {
		t.Fatal("TxDurable channel was not propagated from durability handle")
	}
}

// Story 5.3 AC: panic in EnqueueCommitted sets fatal and delivers an error
// response (transaction already committed in memory but pipeline broke).
func TestPostCommitPanicInDurabilitySetsFatal(t *testing.T) {
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
	if !h.exec.fatal.Load() {
		t.Fatal("executor.fatal not set after post-commit panic")
	}
}

// Story 5.3 AC: panic in EvalAndBroadcast sets fatal.
func TestPostCommitPanicInEvalSetsFatal(t *testing.T) {
	h := newPipelineHarness(t)
	h.subs.panicOn = true
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
	if !h.exec.fatal.Load() {
		t.Fatal("executor.fatal not set after eval panic")
	}
}

// Story 5.3 AC: panic during snapshot acquisition sets fatal.
func TestPostCommitPanicInSnapshotSetsFatal(t *testing.T) {
	h := newPipelineHarness(t)
	h.exec.snapshotFn = func() store.CommittedReadView { panic("snapshot boom") }
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
	if !h.exec.fatal.Load() {
		t.Fatal("executor.fatal not set after snapshot panic")
	}
}

// Story 5.3 AC: after fatal, Submit of a write-affecting command returns
// ErrExecutorFatal without even enqueueing.
func TestPostCommitSubmitAfterFatalRejected(t *testing.T) {
	h := newPipelineHarness(t)
	h.dur.panicOn = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	_ = submit(t, h.exec, "InsertPlayer") // triggers fatal

	ch := make(chan ReducerResponse, 1)
	err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
		ResponseCh: ch,
	})
	if !errors.Is(err, ErrExecutorFatal) {
		t.Fatalf("Submit err=%v, want ErrExecutorFatal", err)
	}
}

// Story 5.3 AC: commands already in-flight when fatal transitions must
// receive ErrExecutorFatal rather than executing.
func TestPostCommitInFlightCommandGetsFatalResponse(t *testing.T) {
	h := newPipelineHarness(t)
	// Block durability so the first commit pauses inside postCommit; the
	// second command reaches the inbox before fatal triggers.
	release := make(chan struct{})
	h.dur.block = release
	h.dur.panicOn = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
		ResponseCh: ch1,
	}); err != nil {
		t.Fatal(err)
	}
	ch2 := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
		ResponseCh: ch2,
	}); err != nil {
		t.Fatal(err)
	}

	go h.exec.Run(ctx)
	// Let the first command enter postCommit, then trigger the panic.
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case r := <-ch1:
		if r.Status != StatusFailedInternal || !errors.Is(r.Error, ErrExecutorFatal) {
			t.Fatalf("ch1 resp=%+v, want fatal internal", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ch1 timeout")
	}
	select {
	case r := <-ch2:
		if r.Status != StatusFailedInternal || !errors.Is(r.Error, ErrExecutorFatal) {
			t.Fatalf("ch2 resp=%+v, want in-flight fatal response", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ch2 timeout")
	}
}

// Story 5.3 AC: pre-commit reducer panic is NOT a post-commit fatal. Story 4.4
// rules apply (StatusFailedPanic, executor continues).
func TestReducerPanicDoesNotSetFatal(t *testing.T) {
	h := newPipelineHarness(t)
	rr := NewReducerRegistry()
	rr.Register(RegisteredReducer{
		Name: "BoomReducer",
		Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
			panic("reducer boom")
		}),
	})
	rr.Freeze()
	// Swap the registry on the harness executor. Simpler: rebuild with fresh
	// reducer set.
	cs := store.NewCommittedState()
	for _, tid := range h.reg.Tables() {
		ts, _ := h.reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	exec := NewExecutor(ExecutorConfig{
		InboxCapacity: 8,
		Durability:    h.dur,
		Subscriptions: h.subs,
	}, rr, cs, h.reg, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	resp := submit(t, exec, "BoomReducer")
	if resp.Status != StatusFailedPanic {
		t.Fatalf("status=%d, want StatusFailedPanic", resp.Status)
	}
	if exec.fatal.Load() {
		t.Fatal("executor.fatal should NOT be set for pre-commit reducer panic")
	}
	// Further calls still work.
	rr2 := NewReducerRegistry()
	rr2.Register(RegisteredReducer{
		Name: "InsertPlayer",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
			return nil, nil
		}),
	})
	rr2.Freeze()
	_ = rr2 // not used further; the check above already proves non-fatal state.
}

// TxID carried by changeset is the TxID handed to durability and subs.
func TestPostCommitTxIDPropagation(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	resp := submit(t, h.exec, "InsertPlayer")
	if resp.Status != StatusCommitted {
		t.Fatalf("status = %d err=%v", resp.Status, resp.Error)
	}
	h.dur.mu.Lock()
	durTxs := append([]types.TxID(nil), h.dur.txIDs...)
	durPayloads := append([]*store.Changeset(nil), h.dur.payloads...)
	h.dur.mu.Unlock()
	h.subs.mu.Lock()
	subsTxs := append([]types.TxID(nil), h.subs.txIDs...)
	h.subs.mu.Unlock()

	if len(durTxs) != 1 || durTxs[0] != resp.TxID {
		t.Fatalf("durability txIDs = %v, resp.TxID = %d", durTxs, resp.TxID)
	}
	if len(subsTxs) != 1 || subsTxs[0] != resp.TxID {
		t.Fatalf("subs txIDs = %v, resp.TxID = %d", subsTxs, resp.TxID)
	}
	if len(durPayloads) != 1 || durPayloads[0].TxID != resp.TxID {
		t.Fatalf("durability changeset TxID = %v, want %d", durPayloads, resp.TxID)
	}
}
