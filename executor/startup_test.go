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
	var sawReady atomic.Bool
	var gateRejectsObserved atomic.Int32

	h := newStartupHarness(t, startupSeed{
		clients: []sysClientsSeed{{conn: types.ConnectionID{1}}},
	})
	h.subs.onEval = func(store.CommittedReadView) {
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

	// Let Startup get into postCommit.
	time.Sleep(20 * time.Millisecond)
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

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

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
