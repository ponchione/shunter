package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// schedulerWorkerFixture builds a committed state with sys_scheduled
// registered and returns a Scheduler hooked to an inbox channel we can
// observe. `now` is fixed; tests advance the simulated clock by
// reassigning s.now.
func schedulerWorkerFixture(t *testing.T) (*Scheduler, *store.CommittedState, schema.TableID, chan ExecutorCommand) {
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
	schedTS, _ := SysScheduledTable(reg)
	inbox := make(chan ExecutorCommand, 16)

	fixedNow := time.Unix(100, 0)
	s := NewScheduler(inbox, cs, schedTS.ID)
	s.now = func() time.Time { return fixedNow }

	return s, cs, schedTS.ID, inbox
}

// seedSchedule inserts a sys_scheduled row directly into committed state
// for test setup. Bypasses Transaction so the worker can observe the row
// without running a reducer. Resolve the table before taking cs.Lock:
// calling CommittedState.Table while already holding the write lock would
// self-deadlock on the RWMutex.
func seedSchedule(t testing.TB, cs *store.CommittedState, tableID schema.TableID, id uint64, reducerName string, args []byte, nextRunAtNs, repeatNs int64) types.RowID {
	t.Helper()
	tbl, ok := cs.Table(tableID)
	if !ok {
		t.Fatal("sys_scheduled missing from committed state")
	}
	cs.Lock()
	defer cs.Unlock()
	rid := tbl.AllocRowID()
	row := types.ProductValue{
		types.NewUint64(id),
		types.NewString(reducerName),
		types.NewBytes(args),
		types.NewInt64(nextRunAtNs),
		types.NewInt64(repeatNs),
	}
	if err := tbl.InsertRow(rid, row); err != nil {
		t.Fatal(err)
	}
	return rid
}

func TestSchedulerScanEnqueuesDueRow(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	// Row fires at t=50s, now is t=100s → due.
	seedSchedule(t, cs, tid, 1, "tick", []byte{0x01}, time.Unix(50, 0).UnixNano(), 0)

	s.scan()

	select {
	case cmd := <-inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("expected CallReducerCmd, got %T", cmd)
		}
		if call.Request.ReducerName != "tick" {
			t.Errorf("ReducerName = %q, want tick", call.Request.ReducerName)
		}
		if call.Request.Source != CallSourceScheduled {
			t.Errorf("Source = %v, want CallSourceScheduled", call.Request.Source)
		}
		if string(call.Request.Args) != string([]byte{0x01}) {
			t.Errorf("Args mismatch: %x", call.Request.Args)
		}
	default:
		t.Fatal("scan should have enqueued exactly one command")
	}
}

func TestSchedulerScanSkipsFuture(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	// Row fires at t=500s, now=100s → future.
	seedSchedule(t, cs, tid, 1, "later", nil, time.Unix(500, 0).UnixNano(), 0)

	s.scan()

	select {
	case cmd := <-inbox:
		t.Fatalf("future row should not have enqueued: %+v", cmd)
	default:
		// good
	}
	if s.nextWakeup.IsZero() || s.nextWakeup != time.Unix(500, 0) {
		t.Errorf("nextWakeup = %v, want %v", s.nextWakeup, time.Unix(500, 0))
	}
}

func TestSchedulerScanEnqueuesAllDue(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	seedSchedule(t, cs, tid, 1, "a", nil, time.Unix(10, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 2, "b", nil, time.Unix(20, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 3, "c", nil, time.Unix(30, 0).UnixNano(), 0)

	s.scan()

	got := map[string]bool{}
	for i := 0; i < 3; i++ {
		select {
		case cmd := <-inbox:
			call := cmd.(CallReducerCmd)
			got[call.Request.ReducerName] = true
		case <-time.After(50 * time.Millisecond):
			t.Fatalf("missing enqueue %d", i)
		}
	}
	if !got["a"] || !got["b"] || !got["c"] {
		t.Errorf("missing due rows: %v", got)
	}
}

func TestSchedulerScanComputesNextWakeupAmongFuture(t *testing.T) {
	s, cs, tid, _ := schedulerWorkerFixture(t)
	// Mix: one due (t=50), three future (300, 200, 400). Minimum future = 200.
	seedSchedule(t, cs, tid, 1, "due", nil, time.Unix(50, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 2, "f1", nil, time.Unix(300, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 3, "f2", nil, time.Unix(200, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 4, "f3", nil, time.Unix(400, 0).UnixNano(), 0)

	s.scan()

	if s.nextWakeup != time.Unix(200, 0) {
		t.Errorf("nextWakeup = %v, want %v", s.nextWakeup, time.Unix(200, 0))
	}
}

func TestSchedulerRunStopsOnCtxCancel(t *testing.T) {
	s, _, _, _ := schedulerWorkerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())

	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	cancel()
	select {
	case <-runDone:
		// good
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestSchedulerNotifyTriggersRescan(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	// Let the worker park on its initial (empty) wait.
	time.Sleep(10 * time.Millisecond)

	// Seed a due row, then notify.
	seedSchedule(t, cs, tid, 1, "tick", nil, time.Unix(50, 0).UnixNano(), 0)
	s.Notify()

	select {
	case cmd := <-inbox:
		call := cmd.(CallReducerCmd)
		if call.Request.ReducerName != "tick" {
			t.Errorf("got reducer %q, want tick", call.Request.ReducerName)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Notify should have triggered rescan within 500ms")
	}
}

func TestSchedulerRunCancelsWhileEnqueueBlocked(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
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
	schedTS, _ := SysScheduledTable(reg)
	seedSchedule(t, cs, schedTS.ID, 1, "blocked", nil, time.Unix(50, 0).UnixNano(), 0)

	inbox := make(chan ExecutorCommand)
	s := NewScheduler(inbox, cs, schedTS.ID)
	s.now = func() time.Time { return time.Unix(100, 0) }

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-runDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Run did not return after cancellation while enqueue was blocked")
	}
}

func TestSchedulerDrainResponsesKeepsDrainingAfterCancel(t *testing.T) {
	s := &Scheduler{respCh: make(chan ReducerResponse, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.drainResponses(ctx)
		close(done)
	}()

	cancel()
	time.Sleep(20 * time.Millisecond)

	s.respCh <- ReducerResponse{Status: StatusFailedUser, Error: stubError("first")}
	sentSecond := make(chan struct{})
	go func() {
		s.respCh <- ReducerResponse{Status: StatusFailedInternal, Error: stubError("second")}
		close(sentSecond)
	}()

	select {
	case <-sentSecond:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("drainResponses stopped reading too early after ctx cancellation")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("drainResponses should exit after draining in-flight responses")
	}
}

// BenchmarkSchedulerScanEnqueue measures one scan→enqueue cycle against
// the SPEC-003 §12 target of <10 ms from wakeup to executor enqueue. The
// benchmark drives scan() directly — no Run goroutine — to keep the
// measurement isolated from scheduler loop overhead and from Go
// testing's multi-N setup behavior.
func BenchmarkSchedulerScanEnqueue(b *testing.B) {
	bld := schema.NewBuilder()
	bld.SchemaVersion(1)
	bld.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	eng, err := bld.Build(schema.EngineOptions{})
	if err != nil {
		b.Fatal(err)
	}
	reg := eng.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	schedTS, _ := SysScheduledTable(reg)

	// One past-due row. Inbox cap 1 so we can drain + re-fill per op.
	seedSchedule(b, cs, schedTS.ID, 1, "bench", nil, time.Now().Add(-time.Second).UnixNano(), 0)

	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, schedTS.ID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.scan()
		<-inbox
	}
}

func TestSchedulerNotifyNonBlocking(t *testing.T) {
	s, _, _, _ := schedulerWorkerFixture(t)
	// Fill the wakeup channel once.
	s.Notify()
	// Second Notify must not block even though the channel is full
	// (buffered cap 1) because Notify uses non-blocking send.
	done := make(chan struct{})
	go func() {
		s.Notify()
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Notify blocked when channel was full — must be non-blocking")
	}
}
