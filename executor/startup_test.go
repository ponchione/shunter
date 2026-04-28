package executor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// startupHarness builds an executor with sys_clients + sys_scheduled
// registered and pre-seeds a caller-supplied set of sys_clients rows and
// sys_scheduled rows before NewExecutor, simulating recovery having
// reconstructed committed state from the previous run. Fake durability /
// subscriptions record the post-commit pipeline so sweep behavior can be
// pinned without timing games.
type startupHarness struct {
	exec       *Executor
	scheduler  *Scheduler
	dur        *fakeDurability
	subs       *fakeSubs
	rec        *recorder
	cs         *store.CommittedState
	reg        schema.SchemaRegistry
	sysClients schema.TableID
	schedTable schema.TableID
	onDisconn  *recordedHandler
	inbox      chan<- ExecutorCommand
}

type startupSeed struct {
	clients       []sysClientsSeed
	schedules     []sysScheduledSeed
	reducers      []RegisteredReducer
	inboxCapacity int
}

type sysClientsSeed struct {
	conn     types.ConnectionID
	identity types.Identity
	at       int64
}

type sysScheduledSeed struct {
	id          uint64
	reducerName string
	args        []byte
	nextRunAtNs int64
	repeatNs    int64
}

// newStartupHarness wires an executor and returns it *unstarted*. Callers
// are responsible for calling exec.Startup when they want the gate to
// flip — several pins assert pre-Startup state, so auto-starting here
// would defeat them. A recorded OnDisconnect reducer is always registered
// so tests can prove the sweep does NOT invoke it.
func newStartupHarness(t *testing.T, seed startupSeed) *startupHarness {
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

	sysTS, ok := SysClientsTable(reg)
	if !ok {
		t.Fatal("harness: sys_clients missing")
	}
	schedTS, ok := SysScheduledTable(reg)
	if !ok {
		t.Fatal("harness: sys_scheduled missing")
	}

	for _, c := range seed.clients {
		seedSysClient(t, cs, sysTS.ID, c)
	}
	for _, s := range seed.schedules {
		seedSchedule(t, cs, schedTS.ID, s.id, s.reducerName, s.args, s.nextRunAtNs, s.repeatNs)
	}

	rr := NewReducerRegistry()
	onDisc := &recordedHandler{}
	if err := rr.Register(RegisteredReducer{
		Name:      "OnDisconnect",
		Handler:   onDisc.handler(),
		Lifecycle: LifecycleOnDisconnect,
	}); err != nil {
		t.Fatal(err)
	}
	for _, reducer := range seed.reducers {
		if err := rr.Register(reducer); err != nil {
			t.Fatal(err)
		}
	}
	rr.Freeze()

	rec := &recorder{}
	dur := &fakeDurability{rec: rec}
	subs := &fakeSubs{rec: rec}
	inboxCapacity := seed.inboxCapacity
	if inboxCapacity == 0 {
		inboxCapacity = 16
	}
	exec := NewExecutor(ExecutorConfig{
		InboxCapacity: inboxCapacity,
		Durability:    dur,
		Subscriptions: subs,
	}, rr, cs, reg, 0)

	sched := NewScheduler(exec.inbox, cs, schedTS.ID)
	sched.now = func() time.Time { return time.Unix(100, 0) }

	return &startupHarness{
		exec:       exec,
		scheduler:  sched,
		dur:        dur,
		subs:       subs,
		rec:        rec,
		cs:         cs,
		reg:        reg,
		sysClients: sysTS.ID,
		schedTable: schedTS.ID,
		onDisconn:  onDisc,
		inbox:      exec.inbox,
	}
}

// seedSysClient inserts a sys_clients row directly into committed state,
// bypassing the executor. Mirrors seedSchedule in scheduler_worker_test.go.
// Used to simulate rows that survived recovery from a prior process.
func seedSysClient(t testing.TB, cs *store.CommittedState, tableID schema.TableID, c sysClientsSeed) {
	t.Helper()
	tbl, ok := cs.Table(tableID)
	if !ok {
		t.Fatal("sys_clients missing from committed state")
	}
	at := c.at
	if at == 0 {
		at = time.Unix(0, 1).UnixNano()
	}
	cs.Lock()
	defer cs.Unlock()
	rid := tbl.AllocRowID()
	row := types.ProductValue{
		types.NewBytes(c.conn[:]),
		types.NewBytes(c.identity[:]),
		types.NewInt64(at),
	}
	if err := tbl.InsertRow(rid, row); err != nil {
		t.Fatal(err)
	}
}

func (h *startupHarness) sysClientsRows() []sysClientsRow {
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

func (h *startupHarness) sysScheduledRows() []types.ProductValue {
	view := h.cs.Snapshot()
	defer view.Close()
	var out []types.ProductValue
	for _, row := range view.TableScan(h.schedTable) {
		out = append(out, row)
	}
	return out
}

// ------------------------------------------------------------------
// Story 3.6: startup orchestration — external-admission gate
// ------------------------------------------------------------------

// Pin 1: SubmitWithContext rejects with ErrExecutorNotStarted before
// Startup runs. Protocol adapter calls SubmitWithContext exclusively
// (executor/protocol_inbox_adapter.go); gating this path is the SPEC-003
// §10.6 / §13.5 first-accept contract.
func TestStartup_SubmitWithContextGatedBeforeStartup(t *testing.T) {
	h := newStartupHarness(t, startupSeed{})
	err := h.exec.SubmitWithContext(context.Background(), CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "noop"},
		ResponseCh: make(chan ReducerResponse, 1),
	})
	if !errors.Is(err, ErrExecutorNotStarted) {
		t.Fatalf("SubmitWithContext pre-Startup = %v, want %v", err, ErrExecutorNotStarted)
	}
}

// Pin 2: after Startup, SubmitWithContext admits commands. This is the
// complement of Pin 1 — the gate flips exactly once, on Startup.
func TestStartup_SubmitWithContextAdmittedAfterStartup(t *testing.T) {
	h := newStartupHarness(t, startupSeed{})
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	// Dispatch loop must be running so an admitted command doesn't
	// block on a full inbox; start it after Startup, per SPEC-003 §13.5
	// ordering.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	err := h.exec.SubmitWithContext(ctx, CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: respCh,
	})
	if err != nil {
		t.Fatalf("SubmitWithContext post-Startup = %v, want admitted", err)
	}
	// Drain the response so the test doesn't race on harness teardown.
	<-respCh
}

// Pin 3: Startup is idempotent. Second call is a no-op and returns nil;
// the sweep does not re-run (state is already clean, but even if more
// rows appeared between calls, first-call-wins by design — the gate is
// a one-time transition).
func TestStartup_Idempotent(t *testing.T) {
	h := newStartupHarness(t, startupSeed{
		clients: []sysClientsSeed{{conn: types.ConnectionID{1}}},
	})
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("first Startup: %v", err)
	}
	firstDur := len(h.dur.txIDs)
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("second Startup: %v", err)
	}
	if n := len(h.dur.txIDs); n != firstDur {
		t.Fatalf("second Startup re-ran sweep: durability calls %d → %d", firstDur, n)
	}
	if !h.exec.externalReady.Load() {
		t.Fatal("externalReady should remain true after idempotent second Startup")
	}
}

// Pin 4: Submit (non-ctx) is deliberately ungated — it is the in-process
// entrypoint used by tests and internal wiring, NOT the external
// protocol admission path. Gating only SubmitWithContext scopes the gate
// to SPEC-003's "external" admission (Story 3.6 / Story 7.5 AC).
func TestStartup_SubmitUngatedBeforeStartup(t *testing.T) {
	h := newStartupHarness(t, startupSeed{})
	respCh := make(chan ReducerResponse, 1)
	err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: respCh,
	})
	if err != nil {
		t.Fatalf("Submit pre-Startup = %v, want admitted", err)
	}
}

// ------------------------------------------------------------------
// Story 7.5: dangling-client sweep
// ------------------------------------------------------------------

// Pin 5: empty sys_clients is a no-op. Startup still flips externalReady
// — this is the usual clean-shutdown startup path, not an error state.
func TestStartup_EmptySysClientsIsNoOp(t *testing.T) {
	h := newStartupHarness(t, startupSeed{})
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	if n := len(h.dur.txIDs); n != 0 {
		t.Errorf("durability calls=%d on empty sweep, want 0", n)
	}
	if n := len(h.subs.txIDs); n != 0 {
		t.Errorf("subs calls=%d on empty sweep, want 0", n)
	}
	if !h.exec.externalReady.Load() {
		t.Fatal("externalReady should be true after empty-table Startup")
	}
}

// Pin 6: every surviving sys_clients row is deleted and the table is
// empty when Startup returns. Multi-row sweep is the realistic post-crash
// shape (SPEC-003 §10.6).
func TestStartup_SweepDeletesAllSurvivingRows(t *testing.T) {
	conns := []types.ConnectionID{{1}, {2}, {3, 7}}
	idents := []types.Identity{{0xAA}, {0xBB}, {0xCC}}
	seed := startupSeed{}
	for i, c := range conns {
		seed.clients = append(seed.clients, sysClientsSeed{conn: c, identity: idents[i]})
	}
	h := newStartupHarness(t, seed)

	if n := len(h.sysClientsRows()); n != len(conns) {
		t.Fatalf("pre-sweep sys_clients rows=%d, want %d", n, len(conns))
	}
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	if n := len(h.sysClientsRows()); n != 0 {
		t.Fatalf("post-sweep sys_clients rows=%d, want 0", n)
	}
}

// Pin 7: the sweep fans out through the post-commit pipeline once per
// deleted row. Acceptance: subscribers observe each sys_clients delete
// (Story 7.5 deliverable: "cleanup commit still runs the post-commit
// pipeline so subscribers see the sys_clients delete").
func TestStartup_SweepFiresPostCommitPipelinePerRow(t *testing.T) {
	seed := startupSeed{
		clients: []sysClientsSeed{
			{conn: types.ConnectionID{1}},
			{conn: types.ConnectionID{2}},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	if got, want := len(h.dur.txIDs), 2; got != want {
		t.Errorf("durability calls=%d, want %d", got, want)
	}
	if got, want := len(h.subs.txIDs), 2; got != want {
		t.Errorf("subs calls=%d, want %d", got, want)
	}
}

// Pin 8: each sweep row is its own fresh cleanup transaction — the
// changeset carries exactly one sys_clients delete and nothing else.
// Validates the "fresh cleanup transaction that only deletes the
// sys_clients row" clause of the Story 7.5 handoff (matches the pattern
// at executor/lifecycle.go:70-92). Pins that the sweep does not
// accidentally batch multiple row-deletes into one tx.
func TestStartup_SweepUsesFreshCleanupTxPerRow(t *testing.T) {
	conns := []types.ConnectionID{{1}, {2}, {3}}
	seed := startupSeed{}
	for _, c := range conns {
		seed.clients = append(seed.clients, sysClientsSeed{conn: c})
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	if got, want := len(h.dur.payloads), len(conns); got != want {
		t.Fatalf("changesets enqueued=%d, want %d", got, want)
	}
	seenConns := map[[16]byte]bool{}
	for i, cs := range h.dur.payloads {
		if cs == nil {
			t.Fatalf("changeset[%d] nil", i)
			continue
		}
		deletes := countChangesetDeletes(cs, h.sysClients)
		if deletes != 1 {
			t.Errorf("changeset[%d] sys_clients deletes=%d, want 1 (fresh cleanup tx per row)", i, deletes)
		}
		if inserts := countChangesetInserts(cs, h.sysClients); inserts != 0 {
			t.Errorf("changeset[%d] sys_clients inserts=%d, want 0 (cleanup-only)", i, inserts)
		}
		conn, ok := firstSysClientsDeleteConn(cs, h.sysClients)
		if !ok {
			t.Errorf("changeset[%d] missing sys_clients delete", i)
			continue
		}
		seenConns[conn] = true
	}
	if got, want := len(seenConns), len(conns); got != want {
		t.Errorf("distinct sweep-deleted connection_ids=%d, want %d", got, want)
	}
	for _, c := range conns {
		var key [16]byte
		copy(key[:], c[:])
		if !seenConns[key] {
			t.Errorf("sweep missed conn=%x", c[:])
		}
	}
}

// Pin 9: OnDisconnect reducer is NEVER invoked by the sweep. Story 7.5
// deliverable step 3 says the sweep uses "the OnDisconnect cleanup path
// (or its equivalent internal helper)"; the handoff narrows that to the
// cleanup-only path (no reducer). Matches the reducer-failure branch of
// handleOnDisconnect, not the reducer-success branch.
func TestStartup_SweepDoesNotInvokeOnDisconnectReducer(t *testing.T) {
	seed := startupSeed{
		clients: []sysClientsSeed{
			{conn: types.ConnectionID{1}},
			{conn: types.ConnectionID{2}},
			{conn: types.ConnectionID{3}},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	if got := h.onDisconn.count(); got != 0 {
		t.Errorf("OnDisconnect reducer calls=%d during sweep, want 0 (cleanup-only path)", got)
	}
}

// Pin 10: while the sweep's post-commit pipeline is mid-flight,
// externalReady is still false. Concurrent SubmitWithContext during the
// sweep must be rejected. This is the hard ordering guarantee from
// Story 7.5 AC: "No external reducer or subscription-registration
// command may interleave ahead of the sweep."
func TestStartup_ExternalRejectedWhileSweepInProgress(t *testing.T) {
	block := make(chan struct{})
	enteredEval := make(chan struct{})
	var sawReady atomic.Bool
	var gateRejectsObserved atomic.Int32

	h := newStartupHarness(t, startupSeed{
		clients: []sysClientsSeed{{conn: types.ConnectionID{1}}},
	})
	h.subs.onEval = func(store.CommittedReadView) {
		close(enteredEval)
		// Inside the sweep's post-commit pipeline the gate must still
		// be closed; concurrent external admission must be rejected.
		if h.exec.externalReady.Load() {
			sawReady.Store(true)
		}
		err := h.exec.SubmitWithContext(context.Background(), CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "racer"},
			ResponseCh: make(chan ReducerResponse, 1),
		})
		if errors.Is(err, ErrExecutorNotStarted) {
			gateRejectsObserved.Add(1)
		}
		<-block
	}

	done := make(chan error, 1)
	go func() { done <- h.exec.Startup(context.Background(), nil) }()

	select {
	case <-enteredEval:
	case <-time.After(2 * time.Second):
		t.Fatal("Startup did not reach postCommit eval")
	}
	if sawReady.Load() {
		t.Error("externalReady flipped true while sweep was still running")
	}
	if got := gateRejectsObserved.Load(); got != 1 {
		t.Errorf("gate rejects observed during sweep=%d, want 1", got)
	}
	close(block)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Startup: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Startup did not return after sweep unblocked")
	}
	if !h.exec.externalReady.Load() {
		t.Fatal("externalReady should be true after Startup returns")
	}
}

// Pin 11: scheduler.ReplayFromCommitted runs before the gate flips.
// Past-due sys_scheduled rows are enqueued into the executor inbox so
// Run consumes them ahead of anything admitted via SubmitWithContext.
// This is the Story 3.6 "scheduler replay gate" step.
func TestStartup_ReplayEnqueuesPastDueBeforeAccept(t *testing.T) {
	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 42, reducerName: "past-due", nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	select {
	case cmd := <-h.exec.inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
		}
		if call.Request.ReducerName != "past-due" {
			t.Errorf("reducer name=%q, want %q", call.Request.ReducerName, "past-due")
		}
		if call.Request.Source != CallSourceScheduled {
			t.Errorf("source=%v, want CallSourceScheduled", call.Request.Source)
		}
	default:
		t.Fatal("past-due scheduled row was not enqueued during Startup replay")
	}
}

func TestStartup_ReplayedScheduleRunsBeforePostStartupExternalSubmit(t *testing.T) {
	order := make(chan string, 2)
	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 43, reducerName: "scheduled-first", nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
		reducers: []RegisteredReducer{
			{
				Name: "scheduled-first",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					order <- "scheduled"
					return nil, nil
				}),
			},
			{
				Name: "external-second",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					order <- "external"
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	respCh := make(chan ReducerResponse, 1)
	if err := h.exec.SubmitWithContext(execCtx, CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "external-second", Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatalf("SubmitWithContext external reducer: %v", err)
	}
	select {
	case resp := <-respCh:
		if resp.Status != StatusCommitted {
			t.Fatalf("external reducer status=%d err=%v, want committed", resp.Status, resp.Error)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for external reducer response")
	}

	got := []string{
		readReducerOrder(t, order, "first reducer after startup"),
		readReducerOrder(t, order, "second reducer after startup"),
	}
	want := []string{"scheduled", "external"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("post-startup reducer order=%v, want %v (replay queue must precede external admission)", got, want)
		}
	}
}

func TestStartup_ReplayOverflowDoesNotBlockDanglingClientSweep(t *testing.T) {
	seed := startupSeed{
		clients:       []sysClientsSeed{{conn: types.ConnectionID{1}}},
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 41, reducerName: "past-due-a", nextRunAtNs: time.Unix(50, 0).UnixNano()},
			{id: 42, reducerName: "past-due-b", nextRunAtNs: time.Unix(60, 0).UnixNano()},
		},
	}
	h := newStartupHarness(t, seed)

	done := make(chan error, 1)
	go func() {
		done <- h.exec.Startup(context.Background(), h.scheduler)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Startup: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Startup blocked on scheduler replay before dangling-client sweep")
	}

	if n := len(h.sysClientsRows()); n != 0 {
		t.Fatalf("post-startup sys_clients rows=%d, want 0", n)
	}
	if !h.exec.externalReady.Load() {
		t.Fatal("externalReady should be true after replay overflow and sweep complete")
	}
	if got, want := len(h.dur.txIDs), 1; got != want {
		t.Fatalf("durability calls=%d, want %d dangling-client cleanup commit", got, want)
	}
	select {
	case cmd := <-h.exec.inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
		}
		if call.Request.Source != CallSourceScheduled {
			t.Fatalf("source=%v, want CallSourceScheduled", call.Request.Source)
		}
	default:
		t.Fatal("at least one past-due scheduled row should be queued during replay")
	}
}

func TestStartup_FailedReplayedScheduleRetriesUnderSchedulerRun(t *testing.T) {
	firstAttempt := make(chan struct{})
	secondAttempt := make(chan struct{})
	var attempts atomic.Int32

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 91, reducerName: "flaky", nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
		reducers: []RegisteredReducer{
			{
				Name: "flaky",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					switch attempts.Add(1) {
					case 1:
						close(firstAttempt)
						return nil, errFireFailed
					case 2:
						close(secondAttempt)
						return nil, nil
					default:
						return nil, nil
					}
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	select {
	case <-firstAttempt:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("replayed scheduled reducer did not run the first attempt")
	}
	if n := len(h.sysScheduledRows()); n != 1 {
		t.Fatalf("failed scheduled reducer should leave row retryable; rows=%d, want 1", n)
	}

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	select {
	case <-secondAttempt:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Scheduler.Run did not retry failed replayed schedule; stale replay suppression may be hiding it")
	}

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier: %v", err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor barrier timed out after retry")
	}

	if n := len(h.sysScheduledRows()); n != 0 {
		t.Fatalf("successful retry should delete one-shot schedule row; rows=%d, want 0", n)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("scheduled reducer attempts=%d, want 2", got)
	}
}

func TestStartup_OverflowedRecoveredScheduleFailureRetriesUnderSchedulerRun(t *testing.T) {
	firstAttempt := make(chan struct{})
	secondAttempt := make(chan struct{})
	releaseSecondAttempt := make(chan struct{})
	var attempts atomic.Int32

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 92, reducerName: "overflow-flaky", nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
		reducers: []RegisteredReducer{
			{
				Name: "overflow-flaky",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					switch attempts.Add(1) {
					case 1:
						close(firstAttempt)
						return nil, errFireFailed
					case 2:
						close(secondAttempt)
						<-releaseSecondAttempt
						return nil, nil
					default:
						return nil, nil
					}
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "startup-barrier"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("pre-startup Submit barrier: %v", err)
	}
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pre-startup barrier did not drain after executor start")
	}

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	select {
	case <-firstAttempt:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Scheduler.Run did not pick up recovered schedule that overflowed Startup replay")
	}
	if n := len(h.sysScheduledRows()); n != 1 {
		t.Fatalf("failed overflowed schedule should leave row retryable; rows=%d, want 1", n)
	}

	select {
	case <-secondAttempt:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Scheduler.Run did not retry failed overflowed schedule after the first attempt completed")
	}
	close(releaseSecondAttempt)

	finalBarrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: finalBarrier,
	}); err != nil {
		t.Fatalf("Submit final barrier: %v", err)
	}
	select {
	case <-finalBarrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor final barrier timed out after overflow retry")
	}

	if n := len(h.sysScheduledRows()); n != 0 {
		t.Fatalf("successful overflow retry should delete one-shot schedule row; rows=%d, want 0", n)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("overflowed scheduled reducer attempts=%d, want 2", got)
	}
}

func TestStartup_PanickingReplayedScheduleRetriesUnderSchedulerRun(t *testing.T) {
	firstAttempt := make(chan struct{})
	secondAttempt := make(chan struct{})
	var attempts atomic.Int32

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 94, reducerName: "panic-then-ok", nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
		reducers: []RegisteredReducer{
			{
				Name: "panic-then-ok",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					switch attempts.Add(1) {
					case 1:
						close(firstAttempt)
						panic("scheduled reducer panic on first recovered attempt")
					case 2:
						close(secondAttempt)
						return nil, nil
					default:
						return nil, nil
					}
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	select {
	case <-firstAttempt:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("replayed panicking scheduled reducer did not run the first attempt")
	}
	if n := len(h.sysScheduledRows()); n != 1 {
		t.Fatalf("panicking scheduled reducer should leave row retryable; rows=%d, want 1", n)
	}

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	select {
	case <-secondAttempt:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Scheduler.Run did not retry panicking replayed schedule")
	}

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier: %v", err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor barrier timed out after panic retry")
	}

	if n := len(h.sysScheduledRows()); n != 0 {
		t.Fatalf("successful panic retry should delete one-shot schedule row; rows=%d, want 0", n)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("panicking scheduled reducer attempts=%d, want 2", got)
	}
}

func TestStartup_FailedRepeatingReplayedScheduleRetryAdvancesFromSameIntendedTime(t *testing.T) {
	firstAt := time.Unix(95, 0).UnixNano()
	repeatNs := int64(10 * time.Second)
	finalAt := firstAt + repeatNs

	observedNextRuns := make(chan int64, 2)
	var attempts atomic.Int32
	var schedTable schema.TableID

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 96, reducerName: "repeat-fail-then-ok", nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: "repeat-fail-then-ok",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					attempt := attempts.Add(1)
					observedNextRuns <- scheduledNextRunFromReducer(ctx, schedTable, 96)
					if attempt == 1 {
						return nil, errFireFailed
					}
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	schedTable = h.schedTable
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	gotFirst := readScheduledAttemptNextRun(t, observedNextRuns, "failed repeating replay attempt")
	if gotFirst != firstAt {
		t.Fatalf("failed repeating replay attempt saw next_run_at_ns=%d, want %d", gotFirst, firstAt)
	}
	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("failed repeating schedule rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != firstAt {
		t.Fatalf("failed repeating attempt advanced next_run_at_ns=%d, want unchanged %d", got, firstAt)
	}

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	gotSecond := readScheduledAttemptNextRun(t, observedNextRuns, "retry repeating replay attempt")
	if gotSecond != firstAt {
		t.Fatalf("retry repeating replay attempt saw next_run_at_ns=%d, want original intended %d", gotSecond, firstAt)
	}

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier: %v", err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor barrier timed out after failed repeating retry")
	}

	rows = h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("repeating schedule rows after retry=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != finalAt {
		t.Fatalf("final repeating next_run_at_ns=%d, want %d (original intended+repeat)", got, finalAt)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("failed repeating schedule attempts=%d, want 2", got)
	}
}

func TestStartup_PanickingRepeatingReplayedScheduleRetryAdvancesFromSameIntendedTime(t *testing.T) {
	firstAt := time.Unix(95, 0).UnixNano()
	repeatNs := int64(10 * time.Second)
	finalAt := firstAt + repeatNs

	observedNextRuns := make(chan int64, 2)
	var attempts atomic.Int32
	var schedTable schema.TableID

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 97, reducerName: "repeat-panic-then-ok", nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: "repeat-panic-then-ok",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					attempt := attempts.Add(1)
					observedNextRuns <- scheduledNextRunFromReducer(ctx, schedTable, 97)
					if attempt == 1 {
						panic("recovered repeating schedule panic")
					}
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	schedTable = h.schedTable
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	gotFirst := readScheduledAttemptNextRun(t, observedNextRuns, "panicking repeating replay attempt")
	if gotFirst != firstAt {
		t.Fatalf("panicking repeating replay attempt saw next_run_at_ns=%d, want %d", gotFirst, firstAt)
	}
	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("panicking repeating schedule rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != firstAt {
		t.Fatalf("panicking repeating attempt advanced next_run_at_ns=%d, want unchanged %d", got, firstAt)
	}

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	gotSecond := readScheduledAttemptNextRun(t, observedNextRuns, "retry panicking repeating replay attempt")
	if gotSecond != firstAt {
		t.Fatalf("retry panicking repeating replay attempt saw next_run_at_ns=%d, want original intended %d", gotSecond, firstAt)
	}

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier: %v", err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor barrier timed out after panicking repeating retry")
	}

	rows = h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("repeating schedule rows after panic retry=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != finalAt {
		t.Fatalf("final panicking repeating next_run_at_ns=%d, want %d (original intended+repeat)", got, finalAt)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("panicking repeating schedule attempts=%d, want 2", got)
	}
}

func TestStartup_FailedRecoveredDueScheduleCanBeCancelledBeforeRetry(t *testing.T) {
	runRecoveredDueScheduleCanBeCancelledBeforeRetry(t, false)
}

func TestStartup_PanickingRecoveredDueScheduleCanBeCancelledBeforeRetry(t *testing.T) {
	runRecoveredDueScheduleCanBeCancelledBeforeRetry(t, true)
}

func TestStartup_RepeatingRecoveredScheduleCatchupStaysFixedRate(t *testing.T) {
	firstAt := time.Unix(95, 0).UnixNano()
	repeatNs := int64(3 * time.Second)
	secondAt := firstAt + repeatNs
	finalAt := secondAt + repeatNs

	observedNextRuns := make(chan int64, 2)
	var attempts atomic.Int32
	var schedTable schema.TableID

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 93, reducerName: "repeat-catchup", nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: "repeat-catchup",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					attempts.Add(1)
					for _, row := range ctx.DB.ScanTable(uint32(schedTable)) {
						if ScheduleID(row[SysScheduledColScheduleID].AsUint64()) == 93 {
							observedNextRuns <- row[SysScheduledColNextRunAtNs].AsInt64()
							return nil, nil
						}
					}
					observedNextRuns <- 0
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	schedTable = h.schedTable
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	gotFirst := readScheduledAttemptNextRun(t, observedNextRuns, "first recovered repeating attempt")
	if gotFirst != firstAt {
		t.Fatalf("first recovered repeating attempt saw next_run_at_ns=%d, want %d", gotFirst, firstAt)
	}
	gotSecond := readScheduledAttemptNextRun(t, observedNextRuns, "second overdue repeating attempt")
	if gotSecond != secondAt {
		t.Fatalf("second overdue repeating attempt saw next_run_at_ns=%d, want %d (fixed-rate intended+repeat)", gotSecond, secondAt)
	}

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier: %v", err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor barrier timed out after repeating catch-up")
	}

	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("repeating schedule rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != finalAt {
		t.Fatalf("final next_run_at_ns=%d, want %d (advanced one fixed-rate interval per fire)", got, finalAt)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("repeating scheduled reducer attempts=%d, want 2", got)
	}
}

func TestStartup_OverflowedRepeatingRecoveredScheduleCatchupStaysFixedRate(t *testing.T) {
	firstAt := time.Unix(95, 0).UnixNano()
	repeatNs := int64(3 * time.Second)
	secondAt := firstAt + repeatNs
	finalAt := secondAt + repeatNs

	observedNextRuns := make(chan int64, 2)
	var attempts atomic.Int32
	var schedTable schema.TableID

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 95, reducerName: "overflow-repeat-catchup", nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: "overflow-repeat-catchup",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					attempts.Add(1)
					for _, row := range ctx.DB.ScanTable(uint32(schedTable)) {
						if ScheduleID(row[SysScheduledColScheduleID].AsUint64()) == 95 {
							observedNextRuns <- row[SysScheduledColNextRunAtNs].AsInt64()
							return nil, nil
						}
					}
					observedNextRuns <- 0
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	schedTable = h.schedTable

	startupBarrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "startup-barrier"},
		ResponseCh: startupBarrier,
	}); err != nil {
		t.Fatalf("pre-startup Submit barrier: %v", err)
	}
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()
	go h.exec.Run(execCtx)

	select {
	case <-startupBarrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pre-startup barrier did not drain after executor start")
	}

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go h.scheduler.Run(schedulerCtx)

	gotFirst := readScheduledAttemptNextRun(t, observedNextRuns, "first overflowed repeating attempt")
	if gotFirst != firstAt {
		t.Fatalf("first overflowed repeating attempt saw next_run_at_ns=%d, want %d", gotFirst, firstAt)
	}
	gotSecond := readScheduledAttemptNextRun(t, observedNextRuns, "second overflowed overdue repeating attempt")
	if gotSecond != secondAt {
		t.Fatalf("second overflowed overdue repeating attempt saw next_run_at_ns=%d, want %d (fixed-rate intended+repeat)", gotSecond, secondAt)
	}

	barrier := make(chan ReducerResponse, 1)
	if err := h.exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier: %v", err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("executor barrier timed out after overflowed repeating catch-up")
	}

	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("overflowed repeating schedule rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != finalAt {
		t.Fatalf("final overflowed next_run_at_ns=%d, want %d (advanced one fixed-rate interval per fire)", got, finalAt)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("overflowed repeating scheduled reducer attempts=%d, want 2", got)
	}
}

func TestStartup_ReplayedRepeatingScheduleFutureAdvanceWaitsForSchedulerClock(t *testing.T) {
	runRepeatingFutureAdvanceWaitsForSchedulerClock(t, false)
}

func TestStartup_OverflowedRepeatingScheduleFutureAdvanceWaitsForSchedulerClock(t *testing.T) {
	runRepeatingFutureAdvanceWaitsForSchedulerClock(t, true)
}

func TestStartup_ReplayedRepeatingFutureAdvanceCanBeCancelledBeforeDue(t *testing.T) {
	firstAt := time.Unix(50, 0).UnixNano()
	repeatNs := int64(450 * time.Second)
	secondAt := firstAt + repeatNs

	observedNextRuns := make(chan int64, 1)
	var attempts atomic.Int32
	var nowNs atomic.Int64
	var schedTable schema.TableID

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 100, reducerName: "repeat-cancel-before-due", nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: "repeat-cancel-before-due",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					attempts.Add(1)
					observedNextRuns <- scheduledNextRunFromReducer(ctx, schedTable, 100)
					return nil, nil
				}),
			},
			{
				Name: "cancel-advanced-repeat",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					deleted, err := ctx.Scheduler.Cancel(100)
					if err != nil {
						return nil, err
					}
					if !deleted {
						return nil, errFireFailed
					}
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	schedTable = h.schedTable
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)

	gotFirst := readScheduledAttemptNextRun(t, observedNextRuns, "first replayed repeating cancelable attempt")
	if gotFirst != firstAt {
		t.Fatalf("first replayed repeating cancelable attempt saw next_run_at_ns=%d, want %d", gotFirst, firstAt)
	}
	waitExecutorBarrier(t, h.exec, "after first replayed repeating cancelable attempt")

	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("cancelable repeating schedule rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != secondAt {
		t.Fatalf("advanced cancelable repeating next_run_at_ns=%d, want %d", got, secondAt)
	}

	startHarnessScheduler(t, h)
	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "cancel-advanced-repeat", "cancel advanced repeat")
	waitExecutorBarrier(t, h.exec, "after cancelling advanced repeat")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("cancelled advanced repeating schedule rows=%d, want 0", len(rows))
	}

	nowNs.Store(time.Unix(600, 0).UnixNano())
	h.scheduler.Notify()
	assertNoReceive(t, observedNextRuns, 50*time.Millisecond, "cancelled recovered repeating schedule")
	if got := attempts.Load(); got != 1 {
		t.Fatalf("cancelled recovered repeating attempts=%d, want 1", got)
	}
}

func TestStartup_InvalidScheduleRepeatIntervalFailsWithoutScheduleRow(t *testing.T) {
	seed := startupSeed{
		reducers: []RegisteredReducer{
			{
				Name: "invalid-repeat",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					_, err := ctx.Scheduler.ScheduleRepeat("never", nil, 0)
					return nil, err
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	respCh := make(chan ReducerResponse, 1)
	if err := h.exec.SubmitWithContext(execCtx, CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "invalid-repeat", Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatalf("SubmitWithContext invalid repeat: %v", err)
	}
	resp := expectAnyResponse(t, respCh, "invalid repeat")
	expectResponseStatus(t, resp, "invalid repeat", StatusFailedUser)
	if !errors.Is(resp.Error, ErrInvalidScheduleInterval) {
		t.Fatalf("invalid repeat error=%v, want %v", resp.Error, ErrInvalidScheduleInterval)
	}
	assertScheduleIDs(t, h)
}

func TestStartup_RepeatingSuccessUpdateNotifiesSchedulerForNextWakeup(t *testing.T) {
	firstAt := time.Unix(50, 0).UnixNano()
	repeatNs := int64(450 * time.Second)
	secondAt := firstAt + repeatNs
	fired := make(chan struct{}, 1)

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 158, reducerName: "repeat-update-notify", nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: "repeat-update-notify",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					fired <- struct{}{}
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	select {
	case <-fired:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("replayed repeating schedule did not fire")
	}
	waitExecutorBarrier(t, h.exec, "after replayed repeating schedule update")
	assertScheduleRowNextRun(t, h, 158, secondAt)

	select {
	case <-h.scheduler.wakeup:
	default:
		t.Fatal("repeating schedule advancement did not notify scheduler to rearm next wakeup")
	}

	h.scheduler.scan()
	if h.scheduler.nextWakeup != time.Unix(0, secondAt) {
		t.Fatalf("nextWakeup after repeating update rescan = %v, want %v", h.scheduler.nextWakeup, time.Unix(0, secondAt))
	}
}

func TestStartup_RecoveredFutureWakeupRearmsForEarlierPostStartupSchedule(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	lateAt := time.Unix(900, 0).UnixNano()
	earlierAt := time.Unix(300, 0)

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 120, reducerName: "recovered-late", nextRunAtNs: lateAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-late", fired),
			{
				Name: "schedule-earlier",
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					_, err := ctx.Scheduler.Schedule("earlier-after-startup", nil, earlierAt)
					return nil, err
				}),
			},
			recordStringReducer("earlier-after-startup", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "schedule-earlier", "schedule earlier")
	assertNoReceive(t, fired, 50*time.Millisecond, "post-startup earlier schedule before injected clock advanced")

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "earlier post-startup schedule")
	if got != "earlier-after-startup" {
		t.Fatalf("first fired reducer after rearm = %q, want earlier-after-startup", got)
	}
	waitExecutorBarrier(t, h.exec, "after earlier post-startup schedule")
	assertNoReceive(t, fired, 50*time.Millisecond, "recovered later schedule before its due time")

	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("remaining recovered future schedules=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColScheduleID].AsUint64(); got != 120 {
		t.Fatalf("remaining schedule_id=%d, want recovered late schedule 120", got)
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != lateAt {
		t.Fatalf("remaining recovered late next_run_at_ns=%d, want %d", got, lateAt)
	}
}

func TestStartup_RecoveredFutureOneShotCanBeCancelledBeforeDue(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 1)
	fireAt := time.Unix(500, 0).UnixNano()

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 130, reducerName: "recovered-cancelled-one-shot", nextRunAtNs: fireAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-cancelled-one-shot", fired),
			cancelScheduleReducer("cancel-recovered-one-shot", 130),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "cancel-recovered-one-shot", "cancel recovered one-shot")
	waitExecutorBarrier(t, h.exec, "after cancelling recovered one-shot")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("cancelled recovered one-shot rows=%d, want 0", len(rows))
	}

	nowNs.Store(time.Unix(600, 0).UnixNano())
	h.scheduler.Notify()
	assertNoReceive(t, fired, 50*time.Millisecond, "cancelled recovered one-shot after due time")
}

func TestStartup_CancellingEarliestRecoveredFutureRearmsLaterWakeup(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	earlyAt := time.Unix(300, 0).UnixNano()
	lateAt := time.Unix(900, 0).UnixNano()

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 131, reducerName: "recovered-early-cancelled", nextRunAtNs: earlyAt},
			{id: 132, reducerName: "recovered-late-after-cancel", nextRunAtNs: lateAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-early-cancelled", fired),
			recordStringReducer("recovered-late-after-cancel", fired),
			cancelScheduleReducer("cancel-recovered-early", 131),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "cancel-recovered-early", "cancel recovered early")
	waitExecutorBarrier(t, h.exec, "after cancelling recovered early")
	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("remaining recovered future rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColScheduleID].AsUint64(); got != 132 {
		t.Fatalf("remaining schedule_id=%d, want late recovered schedule 132", got)
	}

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	assertNoReceive(t, fired, 50*time.Millisecond, "cancelled earliest recovered future after its due time")

	nowNs.Store(time.Unix(950, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "late recovered schedule after earliest cancellation")
	if got != "recovered-late-after-cancel" {
		t.Fatalf("fired reducer after cancelling earliest recovered future = %q, want recovered-late-after-cancel", got)
	}
	waitExecutorBarrier(t, h.exec, "after late recovered schedule")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("remaining recovered future rows after late fire=%d, want 0", len(rows))
	}
}

func TestStartup_PostStartupLaterScheduleDoesNotPostponeRecoveredEarlierWakeup(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	recoveredAt := time.Unix(300, 0).UnixNano()
	laterAt := time.Unix(900, 0)

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 133, reducerName: "recovered-earlier-than-post-startup", nextRunAtNs: recoveredAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-earlier-than-post-startup", fired),
			scheduleAtReducer("schedule-later-after-startup", "later-after-startup", laterAt, nil),
			recordStringReducer("later-after-startup", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "schedule-later-after-startup", "schedule later after startup")
	assertNoReceive(t, fired, 50*time.Millisecond, "post-startup later schedule before recovered earlier due time")

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "recovered earlier schedule")
	if got != "recovered-earlier-than-post-startup" {
		t.Fatalf("first fired reducer after later schedule insert = %q, want recovered-earlier-than-post-startup", got)
	}
	waitExecutorBarrier(t, h.exec, "after recovered earlier schedule")
	assertNoReceive(t, fired, 50*time.Millisecond, "later post-startup schedule before its due time")

	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("remaining future schedules=%d, want later post-startup row", len(rows))
	}
	if got := rows[0][SysScheduledColReducerName].AsString(); got != "later-after-startup" {
		t.Fatalf("remaining reducer_name=%q, want later-after-startup", got)
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != laterAt.UnixNano() {
		t.Fatalf("remaining later next_run_at_ns=%d, want %d", got, laterAt.UnixNano())
	}
}

func TestStartup_RolledBackEarlierScheduleDoesNotReplaceRecoveredFutureWakeup(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	recoveredAt := time.Unix(900, 0).UnixNano()
	rolledBackAt := time.Unix(300, 0)

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 134, reducerName: "recovered-after-rolled-back-schedule", nextRunAtNs: recoveredAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-after-rolled-back-schedule", fired),
			scheduleAtReducer("schedule-earlier-then-fail", "rolled-back-earlier", rolledBackAt, errFireFailed),
			recordStringReducer("rolled-back-earlier", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	submitWithContextAndExpectStatus(t, execCtx, h.exec, "schedule-earlier-then-fail", "rolled-back earlier schedule", StatusFailedUser)
	waitExecutorBarrier(t, h.exec, "after rolled-back earlier schedule")
	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("rows after rolled-back schedule=%d, want only recovered future", len(rows))
	}
	if got := rows[0][SysScheduledColScheduleID].AsUint64(); got != 134 {
		t.Fatalf("remaining schedule_id=%d, want recovered future 134", got)
	}

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	assertNoReceive(t, fired, 50*time.Millisecond, "rolled-back earlier schedule after its due time")

	nowNs.Store(time.Unix(950, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "recovered future after rolled-back earlier schedule")
	if got != "recovered-after-rolled-back-schedule" {
		t.Fatalf("fired reducer after rolled-back earlier schedule = %q, want recovered-after-rolled-back-schedule", got)
	}
	waitExecutorBarrier(t, h.exec, "after recovered future post-rollback")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("rows after recovered future post-rollback=%d, want 0", len(rows))
	}
}

func TestStartup_RolledBackRecoveredCancelKeepsScheduleArmed(t *testing.T) {
	runRolledBackRecoveredCancelKeepsScheduleArmed(t, false)
}

func TestStartup_PanickingRecoveredCancelKeepsScheduleArmed(t *testing.T) {
	runRolledBackRecoveredCancelKeepsScheduleArmed(t, true)
}

func runRolledBackRecoveredCancelKeepsScheduleArmed(t *testing.T, panicAfterCancel bool) {
	t.Helper()
	var nowNs atomic.Int64
	fired := make(chan string, 1)
	fireAt := time.Unix(500, 0).UnixNano()
	cancelReducer := "cancel-recovered-then-fail"
	wantStatus := StatusFailedUser
	if panicAfterCancel {
		cancelReducer = "cancel-recovered-then-panic"
		wantStatus = StatusFailedPanic
	}

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 161, reducerName: "recovered-after-rolled-back-cancel", nextRunAtNs: fireAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-after-rolled-back-cancel", fired),
			{
				Name: cancelReducer,
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					deleted, err := ctx.Scheduler.Cancel(161)
					if err != nil {
						return nil, err
					}
					if !deleted {
						return nil, errFireFailed
					}
					if panicAfterCancel {
						panic("recovered cancel panic")
					}
					return nil, errFireFailed
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	submitWithContextAndExpectStatus(t, execCtx, h.exec, cancelReducer, "rolled-back recovered cancel", wantStatus)
	waitExecutorBarrier(t, h.exec, "after rolled-back recovered cancel")
	assertScheduleRowNextRun(t, h, 161, fireAt)

	startHarnessScheduler(t, h)
	nowNs.Store(time.Unix(600, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "recovered schedule after rolled-back cancel")
	if got != "recovered-after-rolled-back-cancel" {
		t.Fatalf("fired reducer after rolled-back cancel=%q, want recovered-after-rolled-back-cancel", got)
	}
	waitExecutorBarrier(t, h.exec, "after recovered schedule post rolled-back cancel")
	assertScheduleIDs(t, h)
}

func TestStartup_RecoveredFutureSuccessRearmsLaterWakeup(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	earlyAt := time.Unix(300, 0).UnixNano()
	lateAt := time.Unix(900, 0).UnixNano()

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 135, reducerName: "recovered-early-success", nextRunAtNs: earlyAt},
			{id: 136, reducerName: "recovered-late-after-success", nextRunAtNs: lateAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-early-success", fired),
			recordStringReducer("recovered-late-after-success", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "early recovered future success")
	if got != "recovered-early-success" {
		t.Fatalf("first recovered future fired reducer=%q, want recovered-early-success", got)
	}
	waitExecutorBarrier(t, h.exec, "after early recovered future success")
	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("remaining recovered future rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColScheduleID].AsUint64(); got != 136 {
		t.Fatalf("remaining schedule_id=%d, want late recovered schedule 136", got)
	}
	assertNoReceive(t, fired, 50*time.Millisecond, "late recovered future before its due time")

	nowNs.Store(time.Unix(950, 0).UnixNano())
	h.scheduler.Notify()
	got = readReducerOrder(t, fired, "late recovered future after early success")
	if got != "recovered-late-after-success" {
		t.Fatalf("second recovered future fired reducer=%q, want recovered-late-after-success", got)
	}
	waitExecutorBarrier(t, h.exec, "after late recovered future success")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("remaining recovered future rows after late success=%d, want 0", len(rows))
	}
}

func TestStartup_RecoveredRepeatingFutureRearmsEarlierOneShotAfterAdvance(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 3)
	repeatAt := time.Unix(300, 0).UnixNano()
	oneShotAt := time.Unix(600, 0).UnixNano()
	repeatNs := int64(700 * time.Second)
	secondRepeatAt := repeatAt + repeatNs
	thirdRepeatAt := secondRepeatAt + repeatNs

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 137, reducerName: "recovered-repeat-success", nextRunAtNs: repeatAt, repeatNs: repeatNs},
			{id: 138, reducerName: "recovered-one-shot-before-repeat-next", nextRunAtNs: oneShotAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-repeat-success", fired),
			recordStringReducer("recovered-one-shot-before-repeat-next", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "first recovered repeating fire")
	if got != "recovered-repeat-success" {
		t.Fatalf("first recovered repeating fire=%q, want recovered-repeat-success", got)
	}
	waitExecutorBarrier(t, h.exec, "after first recovered repeating fire")
	assertScheduleRowNextRun(t, h, 137, secondRepeatAt)

	nowNs.Store(time.Unix(700, 0).UnixNano())
	h.scheduler.Notify()
	got = readReducerOrder(t, fired, "one-shot before advanced repeat")
	if got != "recovered-one-shot-before-repeat-next" {
		t.Fatalf("second recovered fire=%q, want recovered-one-shot-before-repeat-next", got)
	}
	waitExecutorBarrier(t, h.exec, "after one-shot before advanced repeat")
	assertScheduleRowNextRun(t, h, 137, secondRepeatAt)
	assertNoReceive(t, fired, 50*time.Millisecond, "advanced repeat before its next due time")

	nowNs.Store(time.Unix(1100, 0).UnixNano())
	h.scheduler.Notify()
	got = readReducerOrder(t, fired, "second recovered repeating fire")
	if got != "recovered-repeat-success" {
		t.Fatalf("third recovered fire=%q, want recovered-repeat-success", got)
	}
	waitExecutorBarrier(t, h.exec, "after second recovered repeating fire")
	assertScheduleRowNextRun(t, h, 137, thirdRepeatAt)
}

func TestStartup_FailedRecoveredFutureRetriesBeforeLaterWakeup(t *testing.T) {
	runRecoveredFutureRetryBeforeLaterWakeup(t, false, 0)
}

func TestStartup_PanickingRecoveredFutureRetriesBeforeLaterWakeup(t *testing.T) {
	runRecoveredFutureRetryBeforeLaterWakeup(t, true, 0)
}

func TestStartup_FailedRepeatingRecoveredFutureRetriesBeforeLaterWakeup(t *testing.T) {
	runRecoveredFutureRetryBeforeLaterWakeup(t, false, int64(700*time.Second))
}

func TestStartup_PanickingRepeatingRecoveredFutureRetriesBeforeLaterWakeup(t *testing.T) {
	runRecoveredFutureRetryBeforeLaterWakeup(t, true, int64(700*time.Second))
}

func TestStartup_RecoveredSameTimeFutureWakeupsAllFireOnce(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	fireAt := time.Unix(300, 0).UnixNano()

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 141, reducerName: "recovered-same-time-a", nextRunAtNs: fireAt},
			{id: 142, reducerName: "recovered-same-time-b", nextRunAtNs: fireAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-same-time-a", fired),
			recordStringReducer("recovered-same-time-b", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)
	assertNoReceive(t, fired, 50*time.Millisecond, "same-time recovered futures before due time")

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	assertReducerEvents(t, fired, []string{"recovered-same-time-a", "recovered-same-time-b"}, "same-time recovered futures")
	waitExecutorBarrier(t, h.exec, "after same-time recovered futures")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("same-time recovered future rows after fire=%d, want 0", len(rows))
	}
}

func TestStartup_PostStartupSameTimeScheduleDoesNotHideRecoveredWakeup(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 2)
	fireAt := time.Unix(300, 0)

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 143, reducerName: "recovered-same-time-existing", nextRunAtNs: fireAt.UnixNano()},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("recovered-same-time-existing", fired),
			scheduleAtReducer("schedule-same-time-after-startup", "same-time-after-startup", fireAt, nil),
			recordStringReducer("same-time-after-startup", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "schedule-same-time-after-startup", "schedule same-time after startup")
	assertNoReceive(t, fired, 50*time.Millisecond, "same-time post-startup schedule before due time")

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	assertReducerEvents(t, fired, []string{"recovered-same-time-existing", "same-time-after-startup"}, "same-time recovered and post-startup futures")
	waitExecutorBarrier(t, h.exec, "after same-time recovered and post-startup futures")
	if rows := h.sysScheduledRows(); len(rows) != 0 {
		t.Fatalf("same-time recovered/post-startup rows after fire=%d, want 0", len(rows))
	}
}

func TestStartup_RecoveredSameTimeRepeatingAndOneShotFireOnce(t *testing.T) {
	runSameTimeRepeatingAndOneShotFireOnce(t, false)
}

func TestStartup_PostStartupSameTimeOneShotDoesNotHideRecoveredRepeatingWakeup(t *testing.T) {
	runSameTimeRepeatingAndOneShotFireOnce(t, true)
}

func TestStartup_FailedSameTimeRecoveredFutureRetriesWithoutDuplicatingSibling(t *testing.T) {
	runSameTimeRecoveredFutureRetryWithoutDuplicatingSibling(t, false, 0)
}

func TestStartup_PanickingSameTimeRecoveredFutureRetriesWithoutDuplicatingSibling(t *testing.T) {
	runSameTimeRecoveredFutureRetryWithoutDuplicatingSibling(t, true, 0)
}

func TestStartup_FailedSameTimeRepeatingRecoveredFutureRetriesWithoutDuplicatingSibling(t *testing.T) {
	runSameTimeRecoveredFutureRetryWithoutDuplicatingSibling(t, false, int64(700*time.Second))
}

func TestStartup_PanickingSameTimeRepeatingRecoveredFutureRetriesWithoutDuplicatingSibling(t *testing.T) {
	runSameTimeRecoveredFutureRetryWithoutDuplicatingSibling(t, true, int64(700*time.Second))
}

func TestStartup_MultipleOverflowedDueRowsFireBeforeRecoveredFutureWakeup(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 4)
	futureAt := time.Unix(500, 0)

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 150, reducerName: "overflowed-due-a", nextRunAtNs: time.Unix(50, 0).UnixNano()},
			{id: 151, reducerName: "overflowed-due-b", nextRunAtNs: time.Unix(60, 0).UnixNano()},
			{id: 152, reducerName: "overflowed-due-c", nextRunAtNs: time.Unix(70, 0).UnixNano()},
			{id: 153, reducerName: "recovered-future-after-overflow", nextRunAtNs: futureAt.UnixNano()},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("overflowed-due-a", fired),
			recordStringReducer("overflowed-due-b", fired),
			recordStringReducer("overflowed-due-c", fired),
			recordStringReducer("recovered-future-after-overflow", fired),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	schedulerDone := make(chan struct{})
	go func() {
		h.scheduler.Run(schedulerCtx)
		close(schedulerDone)
	}()

	assertReducerEvents(t, fired, []string{"overflowed-due-a", "overflowed-due-b", "overflowed-due-c"}, "recovered replay-overflowed due rows")
	waitExecutorBarrier(t, h.exec, "after recovered replay-overflowed due rows")

	schedulerCancel()
	select {
	case <-schedulerDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("scheduler did not stop after replay-overflowed due rows drained")
	}
	if h.scheduler.nextWakeup != futureAt {
		t.Fatalf("nextWakeup after replay-overflowed due rows = %v, want %v", h.scheduler.nextWakeup, futureAt)
	}
	assertScheduleIDs(t, h, 153)
	assertNoReceive(t, fired, 50*time.Millisecond, "recovered future before replay-overflow backlog cleared")

	nowNs.Store(time.Unix(600, 0).UnixNano())
	startHarnessScheduler(t, h)
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "recovered future after replay-overflow backlog")
	if got != "recovered-future-after-overflow" {
		t.Fatalf("future reducer after replay-overflow backlog=%q, want recovered-future-after-overflow", got)
	}
	waitExecutorBarrier(t, h.exec, "after recovered future following replay-overflow backlog")
	assertScheduleIDs(t, h)
}

func TestStartup_FailedSameTimeReplayQueuedScheduleRetriesAfterSiblingCommits(t *testing.T) {
	events := make(chan string, 4)
	var attempts atomic.Int32
	fireAt := time.Unix(50, 0).UnixNano()

	seed := startupSeed{
		inboxCapacity: 2,
		schedules: []sysScheduledSeed{
			{id: 154, reducerName: "same-time-replay-flaky", nextRunAtNs: fireAt},
			{id: 155, reducerName: "same-time-replay-sibling", nextRunAtNs: fireAt},
		},
		reducers: []RegisteredReducer{
			{
				Name: "same-time-replay-flaky",
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					switch attempts.Add(1) {
					case 1:
						events <- "same-time-replay-flaky-first"
						return nil, errFireFailed
					case 2:
						events <- "same-time-replay-flaky-retry"
						return nil, nil
					default:
						events <- "same-time-replay-flaky-extra"
						return nil, nil
					}
				}),
			},
			recordStringReducer("same-time-replay-sibling", events),
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	assertReducerEvents(t, events, []string{"same-time-replay-flaky-first", "same-time-replay-sibling"}, "same-time replay queued first attempts")
	got := readReducerOrder(t, events, "same-time replay queued retry")
	if got != "same-time-replay-flaky-retry" {
		t.Fatalf("same-time replay queued retry event=%q, want same-time-replay-flaky-retry", got)
	}
	waitExecutorBarrier(t, h.exec, "after same-time replay queued retry")
	assertScheduleIDs(t, h)
	if got := attempts.Load(); got != 2 {
		t.Fatalf("same-time replay queued flaky attempts=%d, want 2", got)
	}
	assertNoReceive(t, events, 50*time.Millisecond, "duplicate same-time replay queued sibling or retry")
}

func TestStartup_CancelAdvancedRepeatingSharedWakeupLeavesOneShotArmed(t *testing.T) {
	var nowNs atomic.Int64
	fired := make(chan string, 3)
	repeatID := ScheduleID(156)
	firstAt := time.Unix(300, 0).UnixNano()
	repeatNs := int64(700 * time.Second)
	sharedNextAt := firstAt + repeatNs

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: uint64(repeatID), reducerName: "shared-wakeup-repeat", nextRunAtNs: firstAt, repeatNs: repeatNs},
			{id: 157, reducerName: "shared-wakeup-one-shot", nextRunAtNs: sharedNextAt},
		},
		reducers: []RegisteredReducer{
			recordStringReducer("shared-wakeup-repeat", fired),
			recordStringReducer("shared-wakeup-one-shot", fired),
			cancelScheduleReducer("cancel-shared-wakeup-repeat", repeatID),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, fired, "first shared-wakeup repeating fire")
	if got != "shared-wakeup-repeat" {
		t.Fatalf("first shared-wakeup fire=%q, want shared-wakeup-repeat", got)
	}
	waitExecutorBarrier(t, h.exec, "after first shared-wakeup repeating fire")
	assertScheduleRowNextRun(t, h, repeatID, sharedNextAt)
	assertScheduleRowNextRun(t, h, 157, sharedNextAt)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "cancel-shared-wakeup-repeat", "cancel advanced shared-wakeup repeat")
	waitExecutorBarrier(t, h.exec, "after cancelling advanced shared-wakeup repeat")
	assertScheduleIDs(t, h, 157)

	nowNs.Store(time.Unix(1100, 0).UnixNano())
	h.scheduler.Notify()
	got = readReducerOrder(t, fired, "shared-wakeup one-shot after cancelling repeat")
	if got != "shared-wakeup-one-shot" {
		t.Fatalf("shared-wakeup fire after cancelling repeat=%q, want shared-wakeup-one-shot", got)
	}
	waitExecutorBarrier(t, h.exec, "after shared-wakeup one-shot")
	assertScheduleIDs(t, h)
	assertNoReceive(t, fired, 50*time.Millisecond, "cancelled advanced shared-wakeup repeat duplicate")
}

// Pin 12: Startup(ctx, nil) is the test-path shorthand — the scheduler
// replay step is skipped entirely (no inbox enqueue, no scan). Used by
// harnesses that do not need sys_scheduled replay.
func TestStartup_NilSchedulerSkipsReplay(t *testing.T) {
	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 1, reducerName: "past-due", nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	select {
	case cmd := <-h.exec.inbox:
		t.Fatalf("Startup(ctx, nil) enqueued %+v, expected replay skipped", cmd)
	default:
	}
}

// Pin 13: SPEC-003 §6 recovery handoff — NewExecutor initializes its TxID
// counter from recoveredTxID; the first commit after Startup observes
// recoveredTxID+1. Story 3.6 AC: "max_applied_tx_id hand-off from
// SPEC-002 is owned here rather than implied indirectly by constructor
// prose alone" — the chain is NewExecutor → Startup → first accept, and
// this pin anchors the TxID arithmetic end-to-end.
func TestStartup_RecoveredTxIDHandoffChain(t *testing.T) {
	const recoveredTxID = 42
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
	if err := rr.Register(RegisteredReducer{
		Name: "noop",
		Handler: types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) {
			return nil, nil
		}),
	}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()

	rec := &recorder{}
	dur := &fakeDurability{rec: rec}
	subs := &fakeSubs{rec: rec}
	exec := NewExecutor(ExecutorConfig{
		InboxCapacity: 4,
		Durability:    dur,
		Subscriptions: subs,
	}, rr, cs, reg, recoveredTxID)

	if err := exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	if err := exec.SubmitWithContext(ctx, CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "noop", Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatalf("SubmitWithContext: %v", err)
	}
	resp := <-respCh
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v, want StatusCommitted", resp.Status, resp.Error)
	}
	if got, want := resp.TxID, types.TxID(recoveredTxID+1); got != want {
		t.Errorf("first post-Startup TxID=%d, want %d (recoveredTxID+1)", got, want)
	}
}

// Pin 14: ctx cancellation mid-sweep aborts cleanly and leaves
// externalReady false. Startup propagates the context error so the
// embedder knows external admission is still closed. Tests the hard
// guarantee that a cancelled Startup cannot leak a half-open gate.
func TestStartup_CtxCancelMidSweepLeavesGateClosed(t *testing.T) {
	seed := startupSeed{
		clients: []sysClientsSeed{
			{conn: types.ConnectionID{1}},
			{conn: types.ConnectionID{2}},
		},
	}
	h := newStartupHarness(t, seed)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel as soon as the first sweep row enters postCommit, so the
	// second row's ctx.Err() check in sweepDanglingClients trips.
	first := true
	h.subs.onEval = func(store.CommittedReadView) {
		if first {
			first = false
			cancel()
		}
	}

	err := h.exec.Startup(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Startup after cancel = %v, want context.Canceled", err)
	}
	if h.exec.externalReady.Load() {
		t.Fatal("externalReady must stay false when Startup fails")
	}
}

func TestStartup_FailedFirstCallReturnsSameErrorOnLaterCalls(t *testing.T) {
	seed := startupSeed{
		clients: []sysClientsSeed{
			{conn: types.ConnectionID{1}},
			{conn: types.ConnectionID{2}},
		},
	}
	h := newStartupHarness(t, seed)

	ctx, cancel := context.WithCancel(context.Background())
	first := true
	h.subs.onEval = func(store.CommittedReadView) {
		if first {
			first = false
			cancel()
		}
	}

	err := h.exec.Startup(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first Startup after cancel = %v, want context.Canceled", err)
	}
	if h.exec.externalReady.Load() {
		t.Fatal("externalReady must stay false when first Startup fails")
	}
	if rows := h.sysClientsRows(); len(rows) != 1 {
		t.Fatalf("remaining sys_clients rows after failed first Startup = %d, want 1", len(rows))
	}

	err = h.exec.Startup(context.Background(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("second Startup after failed first call = %v, want original context.Canceled", err)
	}
	if h.exec.externalReady.Load() {
		t.Fatal("externalReady must stay false after no-op second Startup")
	}
	if rows := h.sysClientsRows(); len(rows) != 1 {
		t.Fatalf("remaining sys_clients rows after second Startup = %d, want 1", len(rows))
	}
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

func runRepeatingFutureAdvanceWaitsForSchedulerClock(t *testing.T, overflow bool) {
	t.Helper()
	firstAt := time.Unix(50, 0).UnixNano()
	repeatNs := int64(450 * time.Second)
	secondAt := firstAt + repeatNs
	thirdAt := secondAt + repeatNs
	scheduleID := uint64(98)
	reducerName := "repeat-future-replay"
	if overflow {
		scheduleID = 99
		reducerName = "repeat-future-overflow"
	}

	observedNextRuns := make(chan int64, 2)
	var attempts atomic.Int32
	var nowNs atomic.Int64
	var schedTable schema.TableID

	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: scheduleID, reducerName: reducerName, nextRunAtNs: firstAt, repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			{
				Name: reducerName,
				Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
					attempts.Add(1)
					observedNextRuns <- scheduledNextRunFromReducer(ctx, schedTable, ScheduleID(scheduleID))
					return nil, nil
				}),
			},
		},
	}
	h := newStartupHarness(t, seed)
	schedTable = h.schedTable
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }

	var startupBarrier chan ReducerResponse
	if overflow {
		startupBarrier = make(chan ReducerResponse, 1)
		if err := h.exec.Submit(CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "startup-barrier"},
			ResponseCh: startupBarrier,
		}); err != nil {
			t.Fatalf("pre-startup Submit barrier: %v", err)
		}
	}
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	if overflow {
		expectAnyResponse(t, startupBarrier, "pre-startup barrier drain")
		startHarnessScheduler(t, h)
	}

	gotFirst := readScheduledAttemptNextRun(t, observedNextRuns, "first repeating future attempt")
	if gotFirst != firstAt {
		t.Fatalf("first repeating future attempt saw next_run_at_ns=%d, want %d", gotFirst, firstAt)
	}
	waitExecutorBarrier(t, h.exec, "after first repeating future attempt")

	rows := h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("repeating future schedule rows=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != secondAt {
		t.Fatalf("advanced repeating future next_run_at_ns=%d, want %d", got, secondAt)
	}

	if !overflow {
		startHarnessScheduler(t, h)
	}
	assertNoReceive(t, observedNextRuns, 50*time.Millisecond, "advanced repeating schedule before injected clock reached next interval")

	nowNs.Store(time.Unix(600, 0).UnixNano())
	h.scheduler.Notify()
	gotSecond := readScheduledAttemptNextRun(t, observedNextRuns, "second repeating future attempt after clock advance")
	if gotSecond != secondAt {
		t.Fatalf("second repeating future attempt saw next_run_at_ns=%d, want %d", gotSecond, secondAt)
	}
	waitExecutorBarrier(t, h.exec, "after second repeating future attempt")

	rows = h.sysScheduledRows()
	if len(rows) != 1 {
		t.Fatalf("repeating future schedule rows after second fire=%d, want 1", len(rows))
	}
	if got := rows[0][SysScheduledColNextRunAtNs].AsInt64(); got != thirdAt {
		t.Fatalf("final repeating future next_run_at_ns=%d, want %d", got, thirdAt)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("repeating future scheduled reducer attempts=%d, want 2", got)
	}
}

func runRecoveredFutureRetryBeforeLaterWakeup(t *testing.T, panicFirst bool, repeatNs int64) {
	t.Helper()
	var nowNs atomic.Int64
	events := make(chan string, 4)
	earlyAt := time.Unix(300, 0).UnixNano()
	lateAt := time.Unix(900, 0).UnixNano()
	earlyReducer := "failed-recovered-future"
	firstEvent := "failed-first"
	secondEvent := "failed-retry"
	if panicFirst {
		earlyReducer = "panicking-recovered-future"
		firstEvent = "panic-first"
		secondEvent = "panic-retry"
	}

	var attempts atomic.Int32
	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 139, reducerName: earlyReducer, nextRunAtNs: earlyAt, repeatNs: repeatNs},
			{id: 140, reducerName: "later-after-future-retry", nextRunAtNs: lateAt},
		},
		reducers: []RegisteredReducer{
			{
				Name: earlyReducer,
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					switch attempts.Add(1) {
					case 1:
						events <- firstEvent
						if panicFirst {
							panic("recovered future panic")
						}
						return nil, errFireFailed
					case 2:
						events <- secondEvent
						return nil, nil
					default:
						events <- secondEvent
						return nil, nil
					}
				}),
			},
			recordStringReducer("later-after-future-retry", events),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, events, "first recovered future attempt")
	if got != firstEvent {
		t.Fatalf("first recovered future event=%q, want %q", got, firstEvent)
	}
	got = readReducerOrder(t, events, "retry recovered future attempt")
	if got != secondEvent {
		t.Fatalf("retry recovered future event=%q, want %q", got, secondEvent)
	}
	waitExecutorBarrier(t, h.exec, "after recovered future retry")
	if repeatNs == 0 {
		rows := h.sysScheduledRows()
		if len(rows) != 1 {
			t.Fatalf("remaining rows after recovered future retry=%d, want later row", len(rows))
		}
		if got := rows[0][SysScheduledColScheduleID].AsUint64(); got != 140 {
			t.Fatalf("remaining schedule_id=%d, want later schedule 140", got)
		}
	} else {
		assertScheduleRowNextRun(t, h, 139, earlyAt+repeatNs)
		assertScheduleRowNextRun(t, h, 140, lateAt)
	}
	assertNoReceive(t, events, 50*time.Millisecond, "later recovered future before its due time")

	nowNs.Store(time.Unix(950, 0).UnixNano())
	h.scheduler.Notify()
	got = readReducerOrder(t, events, "later recovered future after retry")
	if got != "later-after-future-retry" {
		t.Fatalf("later recovered future event=%q, want later-after-future-retry", got)
	}
	waitExecutorBarrier(t, h.exec, "after later recovered future")
	if repeatNs == 0 {
		if rows := h.sysScheduledRows(); len(rows) != 0 {
			t.Fatalf("remaining rows after later recovered future=%d, want 0", len(rows))
		}
	} else {
		assertScheduleRowNextRun(t, h, 139, earlyAt+repeatNs)
		assertNoReceive(t, events, 50*time.Millisecond, "advanced recovered repeating future before next due time")

		nowNs.Store(time.Unix(1100, 0).UnixNano())
		h.scheduler.Notify()
		got = readReducerOrder(t, events, "advanced repeating recovered future after later row")
		if got != secondEvent {
			t.Fatalf("advanced repeating recovered future event=%q, want %q", got, secondEvent)
		}
		waitExecutorBarrier(t, h.exec, "after advanced repeating recovered future")
		assertScheduleRowNextRun(t, h, 139, earlyAt+(2*repeatNs))
	}
	wantAttempts := int32(2)
	if repeatNs != 0 {
		wantAttempts = 3
	}
	if got := attempts.Load(); got != wantAttempts {
		t.Fatalf("recovered future attempts=%d, want %d", got, wantAttempts)
	}
}

func runSameTimeRecoveredFutureRetryWithoutDuplicatingSibling(t *testing.T, panicFirst bool, repeatNs int64) {
	t.Helper()
	var nowNs atomic.Int64
	events := make(chan string, 5)
	fireAt := time.Unix(300, 0).UnixNano()
	flakyReducer := "failed-same-time-recovered"
	firstEvent := "failed-same-time-first"
	retryEvent := "failed-same-time-retry"
	if panicFirst {
		flakyReducer = "panicking-same-time-recovered"
		firstEvent = "panic-same-time-first"
		retryEvent = "panic-same-time-retry"
	}

	var attempts atomic.Int32
	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: 144, reducerName: flakyReducer, nextRunAtNs: fireAt, repeatNs: repeatNs},
			{id: 145, reducerName: "same-time-recovered-sibling", nextRunAtNs: fireAt},
		},
		reducers: []RegisteredReducer{
			{
				Name: flakyReducer,
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					switch attempts.Add(1) {
					case 1:
						events <- firstEvent
						if panicFirst {
							panic("same-time recovered future panic")
						}
						return nil, errFireFailed
					case 2:
						events <- retryEvent
						return nil, nil
					default:
						events <- retryEvent
						return nil, nil
					}
				}),
			},
			recordStringReducer("same-time-recovered-sibling", events),
		},
	}
	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)
	assertNoReceive(t, events, 50*time.Millisecond, "same-time recovered failure siblings before due time")

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	assertReducerEvents(t, events, []string{firstEvent, "same-time-recovered-sibling"}, "same-time recovered first attempts")
	got := readReducerOrder(t, events, "same-time recovered retry")
	if got != retryEvent {
		t.Fatalf("same-time recovered retry event=%q, want %q", got, retryEvent)
	}
	waitExecutorBarrier(t, h.exec, "after same-time recovered retry")
	if repeatNs == 0 {
		if rows := h.sysScheduledRows(); len(rows) != 0 {
			t.Fatalf("same-time recovered rows after retry=%d, want 0", len(rows))
		}
		if got := attempts.Load(); got != 2 {
			t.Fatalf("same-time recovered attempts=%d, want 2", got)
		}
		assertNoReceive(t, events, 50*time.Millisecond, "same-time sibling duplicate after retry")
		return
	}
	nextAt := fireAt + repeatNs
	assertScheduleRowNextRun(t, h, 144, nextAt)
	assertNoReceive(t, events, 50*time.Millisecond, "same-time sibling duplicate before repeating retry next interval")

	nowNs.Store(time.Unix(1100, 0).UnixNano())
	h.scheduler.Notify()
	got = readReducerOrder(t, events, "same-time repeating retry after next interval")
	if got != retryEvent {
		t.Fatalf("same-time repeating retry event=%q, want %q", got, retryEvent)
	}
	waitExecutorBarrier(t, h.exec, "after same-time repeating retry next interval")
	assertScheduleRowNextRun(t, h, 144, fireAt+(2*repeatNs))
	if got := attempts.Load(); got != 3 {
		t.Fatalf("same-time repeating recovered attempts=%d, want 3", got)
	}
}

func runSameTimeRepeatingAndOneShotFireOnce(t *testing.T, postStartupOneShot bool) {
	t.Helper()
	var nowNs atomic.Int64
	events := make(chan string, 4)
	fireAt := time.Unix(300, 0)
	repeatNs := int64(700 * time.Second)
	repeatID := ScheduleID(146)
	firstRepeatAt := fireAt.UnixNano() + repeatNs
	secondRepeatAt := fireAt.UnixNano() + 2*repeatNs
	repeatingReducer := "same-time-recovered-repeat-success"
	oneShotReducer := "same-time-recovered-one-shot"
	expectedFirstEvents := []string{repeatingReducer, oneShotReducer}

	seed := startupSeed{
		schedules: []sysScheduledSeed{
			{id: uint64(repeatID), reducerName: repeatingReducer, nextRunAtNs: fireAt.UnixNano(), repeatNs: repeatNs},
		},
		reducers: []RegisteredReducer{
			recordStringReducer(repeatingReducer, events),
		},
	}
	if postStartupOneShot {
		oneShotReducer = "same-time-post-startup-one-shot"
		expectedFirstEvents[1] = oneShotReducer
		seed.reducers = append(seed.reducers,
			scheduleAtReducer("schedule-same-time-one-shot-after-startup", oneShotReducer, fireAt, nil),
			recordStringReducer(oneShotReducer, events),
		)
	} else {
		seed.schedules = append(seed.schedules, sysScheduledSeed{
			id:          147,
			reducerName: oneShotReducer,
			nextRunAtNs: fireAt.UnixNano(),
		})
		seed.reducers = append(seed.reducers, recordStringReducer(oneShotReducer, events))
	}

	h := newStartupHarness(t, seed)
	nowNs.Store(time.Unix(100, 0).UnixNano())
	h.scheduler.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	startHarnessScheduler(t, h)
	if postStartupOneShot {
		submitWithContextAndExpectCommitted(t, execCtx, h.exec, "schedule-same-time-one-shot-after-startup", "schedule same-time one-shot after startup")
	}
	assertNoReceive(t, events, 50*time.Millisecond, "same-time repeating/one-shot before due time")

	nowNs.Store(time.Unix(400, 0).UnixNano())
	h.scheduler.Notify()
	assertReducerEvents(t, events, expectedFirstEvents, "same-time repeating and one-shot futures")
	waitExecutorBarrier(t, h.exec, "after same-time repeating and one-shot futures")
	assertScheduleRowNextRun(t, h, repeatID, firstRepeatAt)
	assertNoReceive(t, events, 50*time.Millisecond, "same-time one-shot duplicate before repeating next interval")

	nowNs.Store(time.Unix(1100, 0).UnixNano())
	h.scheduler.Notify()
	got := readReducerOrder(t, events, "same-time repeating second fire")
	if got != repeatingReducer {
		t.Fatalf("same-time repeating second fire=%q, want %q", got, repeatingReducer)
	}
	waitExecutorBarrier(t, h.exec, "after same-time repeating second fire")
	assertScheduleRowNextRun(t, h, repeatID, secondRepeatAt)
}

func runRecoveredDueScheduleCanBeCancelledBeforeRetry(t *testing.T, panicFirst bool) {
	t.Helper()
	events := make(chan string, 2)
	reducerName := "failed-due-cancel-before-retry"
	firstEvent := "failed-due-first"
	if panicFirst {
		reducerName = "panicking-due-cancel-before-retry"
		firstEvent = "panicking-due-first"
	}

	var attempts atomic.Int32
	seed := startupSeed{
		inboxCapacity: 1,
		schedules: []sysScheduledSeed{
			{id: 159, reducerName: reducerName, nextRunAtNs: time.Unix(50, 0).UnixNano()},
		},
		reducers: []RegisteredReducer{
			{
				Name: reducerName,
				Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
					attempts.Add(1)
					events <- firstEvent
					if panicFirst {
						panic("recovered due schedule panic before cancel")
					}
					return nil, errFireFailed
				}),
			},
			cancelScheduleReducer("cancel-failed-due-before-retry", 159),
		},
	}
	h := newStartupHarness(t, seed)
	if err := h.exec.Startup(context.Background(), h.scheduler); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	execCtx := startHarnessExecutor(t, h)
	got := readReducerOrder(t, events, "first recovered due failure before cancel")
	if got != firstEvent {
		t.Fatalf("first recovered due failure event=%q, want %q", got, firstEvent)
	}
	waitExecutorBarrier(t, h.exec, "after first recovered due failure before cancel")
	assertScheduleIDs(t, h, 159)

	submitWithContextAndExpectCommitted(t, execCtx, h.exec, "cancel-failed-due-before-retry", "cancel failed recovered due before retry")
	waitExecutorBarrier(t, h.exec, "after cancelling failed recovered due before retry")
	assertScheduleIDs(t, h)

	startHarnessScheduler(t, h)
	h.scheduler.Notify()
	assertNoReceive(t, events, 50*time.Millisecond, "cancelled failed recovered due retry")
	if got := attempts.Load(); got != 1 {
		t.Fatalf("failed recovered due attempts after cancellation=%d, want 1", got)
	}
}

func recordStringReducer(name string, ch chan<- string) RegisteredReducer {
	return RegisteredReducer{
		Name: name,
		Handler: types.ReducerHandler(func(*types.ReducerContext, []byte) ([]byte, error) {
			ch <- name
			return nil, nil
		}),
	}
}

func cancelScheduleReducer(name string, id ScheduleID) RegisteredReducer {
	return RegisteredReducer{
		Name: name,
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
			deleted, err := ctx.Scheduler.Cancel(id)
			if err != nil {
				return nil, err
			}
			if !deleted {
				return nil, errFireFailed
			}
			return nil, nil
		}),
	}
}

func scheduleAtReducer(name, scheduledReducer string, at time.Time, retErr error) RegisteredReducer {
	return RegisteredReducer{
		Name: name,
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
			if _, err := ctx.Scheduler.Schedule(scheduledReducer, nil, at); err != nil {
				return nil, err
			}
			return nil, retErr
		}),
	}
}

func readScheduledAttemptNextRun(t *testing.T, ch <-chan int64, label string) int64 {
	t.Helper()
	select {
	case nextRun := <-ch:
		return nextRun
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for %s", label)
		return 0
	}
}

func readReducerOrder(t *testing.T, ch <-chan string, label string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for %s", label)
		return ""
	}
}

func assertReducerEvents(t *testing.T, ch <-chan string, want []string, label string) {
	t.Helper()
	remaining := make(map[string]int, len(want))
	for _, event := range want {
		remaining[event]++
	}
	for range want {
		got := readReducerOrder(t, ch, label)
		if remaining[got] == 0 {
			t.Fatalf("%s unexpected event %q, remaining want=%v", label, got, remaining)
		}
		remaining[got]--
		if remaining[got] == 0 {
			delete(remaining, got)
		}
	}
	if len(remaining) != 0 {
		t.Fatalf("%s missing events: %v", label, remaining)
	}
}

func startHarnessExecutor(t *testing.T, h *startupHarness) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.exec.Run(ctx)
	return ctx
}

func startHarnessScheduler(t *testing.T, h *startupHarness) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.scheduler.Run(ctx)
}

func expectAnyResponse(t *testing.T, ch <-chan ReducerResponse, label string) ReducerResponse {
	t.Helper()
	select {
	case resp := <-ch:
		return resp
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for %s", label)
		return ReducerResponse{}
	}
}

func submitWithContextAndExpectCommitted(t *testing.T, ctx context.Context, exec *Executor, reducerName, label string) {
	t.Helper()
	submitWithContextAndExpectStatus(t, ctx, exec, reducerName, label, StatusCommitted)
}

func submitWithContextAndExpectStatus(t *testing.T, ctx context.Context, exec *Executor, reducerName, label string, want ReducerStatus) {
	t.Helper()
	respCh := make(chan ReducerResponse, 1)
	if err := exec.SubmitWithContext(ctx, CallReducerCmd{
		Request:    ReducerRequest{ReducerName: reducerName, Source: CallSourceExternal},
		ResponseCh: respCh,
	}); err != nil {
		t.Fatalf("SubmitWithContext %s: %v", label, err)
	}
	resp := expectAnyResponse(t, respCh, label)
	expectResponseStatus(t, resp, label, want)
}

func expectResponseStatus(t *testing.T, resp ReducerResponse, label string, want ReducerStatus) {
	t.Helper()
	if resp.Status != want {
		t.Fatalf("%s status=%d err=%v, want %d", label, resp.Status, resp.Error, want)
	}
}

func assertNoReceive[T any](t *testing.T, ch <-chan T, d time.Duration, label string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("%s unexpectedly received %v", label, got)
	case <-time.After(d):
	}
}

func assertScheduleRowNextRun(t *testing.T, h *startupHarness, scheduleID ScheduleID, want int64) {
	t.Helper()
	for _, row := range h.sysScheduledRows() {
		if ScheduleID(row[SysScheduledColScheduleID].AsUint64()) == scheduleID {
			if got := row[SysScheduledColNextRunAtNs].AsInt64(); got != want {
				t.Fatalf("schedule %d next_run_at_ns=%d, want %d", scheduleID, got, want)
			}
			return
		}
	}
	t.Fatalf("schedule %d missing from sys_scheduled", scheduleID)
}

func assertScheduleIDs(t *testing.T, h *startupHarness, want ...ScheduleID) {
	t.Helper()
	rows := h.sysScheduledRows()
	got := make(map[ScheduleID]int, len(rows))
	for _, row := range rows {
		got[ScheduleID(row[SysScheduledColScheduleID].AsUint64())]++
	}
	if len(got) != len(want) {
		t.Fatalf("sys_scheduled IDs=%v, want %v", got, want)
	}
	for _, id := range want {
		if got[id] != 1 {
			t.Fatalf("sys_scheduled IDs=%v, want %v", got, want)
		}
	}
}

func waitExecutorBarrier(t *testing.T, exec *Executor, label string) {
	t.Helper()
	barrier := make(chan ReducerResponse, 1)
	if err := exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "not-registered"},
		ResponseCh: barrier,
	}); err != nil {
		t.Fatalf("Submit barrier for %s: %v", label, err)
	}
	select {
	case <-barrier:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("executor barrier timed out for %s", label)
	}
}

func scheduledNextRunFromReducer(ctx *types.ReducerContext, schedTable schema.TableID, scheduleID ScheduleID) int64 {
	for _, row := range ctx.DB.ScanTable(uint32(schedTable)) {
		if ScheduleID(row[SysScheduledColScheduleID].AsUint64()) == scheduleID {
			return row[SysScheduledColNextRunAtNs].AsInt64()
		}
	}
	return 0
}

// countChangesetDeletes returns the number of sys_clients deletes in cs.
func countChangesetDeletes(cs *store.Changeset, sysID schema.TableID) int {
	tbl, ok := cs.Tables[sysID]
	if !ok {
		return 0
	}
	return len(tbl.Deletes)
}

func countChangesetInserts(cs *store.Changeset, sysID schema.TableID) int {
	tbl, ok := cs.Tables[sysID]
	if !ok {
		return 0
	}
	return len(tbl.Inserts)
}

// firstSysClientsDeleteConn returns the connection_id from the first
// sys_clients delete in cs, padded/truncated to 16 bytes for map-key use.
func firstSysClientsDeleteConn(cs *store.Changeset, sysID schema.TableID) ([16]byte, bool) {
	var key [16]byte
	tbl, ok := cs.Tables[sysID]
	if !ok {
		return key, false
	}
	for _, row := range tbl.Deletes {
		if int(SysClientsColConnectionID) >= len(row) {
			continue
		}
		b := row[SysClientsColConnectionID].AsBytes()
		copy(key[:], b)
		return key, true
	}
	return key, false
}
