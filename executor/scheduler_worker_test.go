package executor

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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
	fireAt := time.Unix(50, 0).UnixNano()
	seedSchedule(t, cs, tid, 1, "tick", []byte{0x01}, fireAt, 0)

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
		if call.Request.ScheduleID != 1 {
			t.Errorf("ScheduleID = %d, want 1", call.Request.ScheduleID)
		}
		if call.Request.IntendedFireAt != fireAt {
			t.Errorf("IntendedFireAt = %d, want %d", call.Request.IntendedFireAt, fireAt)
		}
		if call.ResponseCh != nil || call.ProtocolResponseCh != nil {
			t.Fatal("scheduled call should not own a response channel")
		}
		if string(call.Request.Args) != string([]byte{0x01}) {
			t.Errorf("Args mismatch: %x", call.Request.Args)
		}
	default:
		t.Fatal("scan should have enqueued exactly one command")
	}
}

func TestSchedulerMarksInFlightBeforeDueCommandCanComplete(t *testing.T) {
	oldProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(oldProcs) })

	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand)
	s := NewScheduler(inbox, cs, tid)
	s.now = func() time.Time { return time.Unix(100, 0) }
	fireAt := time.Unix(50, 0).UnixNano()
	seedSchedule(t, cs, tid, 101, "fast-complete", nil, fireAt, 0)

	done := make(chan struct{})
	go func() {
		s.scan()
		close(done)
	}()

	select {
	case cmd := <-inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
		}
		wasInFlight, _ := s.completeInFlight(call.Request.ScheduleID, call.Request.IntendedFireAt)
		if !wasInFlight {
			t.Fatal("scheduled attempt completed before scheduler marked it in-flight")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("scan did not enqueue due schedule")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("scan did not return after due command was received")
	}

	rows := s.snapshotScheduleRows()
	if len(rows) != 1 {
		t.Fatalf("schedule rows=%d, want 1", len(rows))
	}
	if s.isInFlight(rows[0]) {
		t.Fatal("completed scheduled attempt left a stale in-flight marker")
	}
}

func TestSchedulerCompletionWithWrongFireAtDoesNotClearInFlight(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	fireAt := time.Unix(50, 0).UnixNano()
	seedSchedule(t, cs, tid, 102, "stale-completion", nil, fireAt, 0)

	s.scan()
	select {
	case cmd := <-inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
		}
		if call.Request.ScheduleID != 102 || call.Request.IntendedFireAt != fireAt {
			t.Fatalf("enqueued request = %+v, want schedule 102 at %d", call.Request, fireAt)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scan did not enqueue due schedule")
	}

	wasInFlight, wasReplayQueued := s.completeInFlight(102, fireAt+1)
	if wasInFlight || wasReplayQueued {
		t.Fatalf("wrong-fire-at completeInFlight = (%v, %v), want false/false", wasInFlight, wasReplayQueued)
	}
	rows := s.snapshotScheduleRows()
	if len(rows) != 1 {
		t.Fatalf("schedule rows=%d, want 1", len(rows))
	}
	if !s.isInFlight(rows[0]) {
		t.Fatal("wrong-fire-at completion cleared the active in-flight marker")
	}

	s.scan()
	select {
	case cmd := <-inbox:
		t.Fatalf("scan duplicated in-flight schedule after wrong-fire-at completion: %+v", cmd)
	default:
	}

	wasInFlight, wasReplayQueued = s.completeInFlight(102, fireAt)
	if !wasInFlight || wasReplayQueued {
		t.Fatalf("correct completeInFlight = (%v, %v), want true/false", wasInFlight, wasReplayQueued)
	}
	if s.isInFlight(rows[0]) {
		t.Fatal("correct completion left active in-flight marker")
	}

	s.scan()
	select {
	case cmd := <-inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("requeued cmd type=%T, want CallReducerCmd", cmd)
		}
		if call.Request.ScheduleID != 102 || call.Request.IntendedFireAt != fireAt {
			t.Fatalf("requeued request = %+v, want schedule 102 at %d", call.Request, fireAt)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scan did not retry due row after correct completion")
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

func TestSchedulerRunUsesInjectedClockForFutureWakeup(t *testing.T) {
	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, tid)
	seedSchedule(t, cs, tid, 33, "future", nil, time.Unix(500, 0).UnixNano(), 0)

	var nowCalls atomic.Int32
	s.now = func() time.Time {
		if nowCalls.Add(1) <= 2 {
			return time.Unix(100, 0)
		}
		return time.Unix(600, 0)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	select {
	case cmd := <-inbox:
		t.Fatalf("future schedule enqueued before injected scheduler clock reached wakeup: %+v", cmd)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case <-runDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSchedulerNotifyRespectsInjectedClockForFutureWakeup(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	var nowNs atomic.Int64
	nowNs.Store(time.Unix(100, 0).UnixNano())
	s.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	seedSchedule(t, cs, tid, 34, "future-notify", nil, time.Unix(500, 0).UnixNano(), 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	s.Notify()
	select {
	case cmd := <-inbox:
		t.Fatalf("future schedule enqueued by notify before injected scheduler clock reached wakeup: %+v", cmd)
	case <-time.After(50 * time.Millisecond):
	}

	nowNs.Store(time.Unix(600, 0).UnixNano())
	s.Notify()
	select {
	case cmd := <-inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
		}
		if call.Request.ScheduleID != 34 {
			t.Fatalf("ScheduleID = %d, want 34", call.Request.ScheduleID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("due future schedule was not enqueued after injected scheduler clock advanced")
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSchedulerRunCancelsWhileEnqueueBlocked(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "noop",
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
	enqueueAttempted := make(chan struct{})
	var enqueueOnce sync.Once
	s.enqueueAttempted = func() {
		enqueueOnce.Do(func() { close(enqueueAttempted) })
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	select {
	case <-enqueueAttempted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("scheduler did not attempt enqueue")
	}
	cancel()
	select {
	case <-runDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Run did not return after cancellation while enqueue was blocked")
	}

	rows := s.snapshotScheduleRows()
	if len(rows) != 1 {
		t.Fatalf("schedule rows after cancelled blocked enqueue=%d, want 1", len(rows))
	}
	if s.isInFlight(rows[0]) {
		t.Fatal("cancelled blocked enqueue left a stale in-flight marker")
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
	fireAt := time.Now().Add(-time.Second).UnixNano()
	seedSchedule(b, cs, schedTS.ID, 1, "bench", nil, fireAt, 0)

	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, schedTS.ID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.scan()
		<-inbox
		s.completeInFlight(1, fireAt)
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

func TestSchedulerNotifyCompletionChurnDoesNotDuplicateInFlightDueRows(t *testing.T) {
	const (
		seed          = uint64(0x5eed2026)
		scheduleCount = 4
		workers       = 4
		iterations    = 64
	)
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	fireAt := time.Unix(50, 0).UnixNano()
	for i := range scheduleCount {
		seedSchedule(t, cs, tid, uint64(i+1), fmt.Sprintf("churn-%d", i+1), nil, fireAt, 0)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()
	defer func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("seed=0x5eed2026 op=cleanup runtime_config=schedules=4/workers=4/iterations=64 observed=scheduler-still-running expected=stopped")
		}
	}()

	initial := readSchedulerCallBatch(t, inbox, scheduleCount, seed, "initial")
	assertSchedulerCallSet(t, initial, seed, "initial", scheduleCount, fireAt)
	assertNoReceive(t, inbox, 50*time.Millisecond, "seed=0x5eed2026 op=initial-duplicate-check runtime_config=schedules=4/workers=4/iterations=64 observed=extra-command expected=no-duplicate-while-in-flight")

	start := make(chan struct{})
	failures := make(chan string, scheduleCount)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				s.Notify()
				if (int(seed)+worker+op)%3 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}
	for op, call := range initial {
		wg.Add(1)
		go func(op int, call CallReducerCmd) {
			defer wg.Done()
			<-start
			if (int(seed)+op)%2 == 0 {
				runtime.Gosched()
			}
			wasInFlight, wasReplayQueued := s.completeInFlight(call.Request.ScheduleID, call.Request.IntendedFireAt)
			if !wasInFlight || wasReplayQueued {
				failures <- fmt.Sprintf("seed=%#x op=complete-%d runtime_config=schedules=%d/workers=%d/iterations=%d operation=completeInFlight(%d,%d) observed=(%v,%v) expected=(true,false)",
					seed, op, scheduleCount, workers, iterations, call.Request.ScheduleID, call.Request.IntendedFireAt, wasInFlight, wasReplayQueued)
			}
			s.Notify()
		}(op, call)
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}

	s.Notify()
	requeued := readSchedulerCallBatch(t, inbox, scheduleCount, seed, "requeued")
	assertSchedulerCallSet(t, requeued, seed, "requeued", scheduleCount, fireAt)
	assertNoReceive(t, inbox, 50*time.Millisecond, "seed=0x5eed2026 op=requeue-duplicate-check runtime_config=schedules=4/workers=4/iterations=64 observed=extra-command expected=one-requeue-per-completed-row")
}

func readSchedulerCallBatch(t *testing.T, inbox <-chan ExecutorCommand, want int, seed uint64, phase string) []CallReducerCmd {
	t.Helper()
	calls := make([]CallReducerCmd, 0, want)
	for op := range want {
		select {
		case cmd := <-inbox:
			call, ok := cmd.(CallReducerCmd)
			if !ok {
				t.Fatalf("seed=%#x phase=%s op=%d operation=read-scheduler-command observed_type=%T expected_type=CallReducerCmd", seed, phase, op, cmd)
			}
			calls = append(calls, call)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("seed=%#x phase=%s op=%d operation=read-scheduler-command observed=timeout expected=%d commands", seed, phase, op, want)
		}
	}
	return calls
}

func assertSchedulerCallSet(t *testing.T, calls []CallReducerCmd, seed uint64, phase string, scheduleCount int, fireAt int64) {
	t.Helper()
	seen := make(map[ScheduleID]struct{}, len(calls))
	for op, call := range calls {
		id := call.Request.ScheduleID
		if id < 1 || id > ScheduleID(scheduleCount) {
			t.Fatalf("seed=%#x phase=%s op=%d operation=validate-schedule-id observed=%d expected=1..%d", seed, phase, op, id, scheduleCount)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("seed=%#x phase=%s op=%d operation=validate-schedule-id observed=duplicate-%d expected=unique-due-row", seed, phase, op, id)
		}
		seen[id] = struct{}{}
		if call.Request.Source != CallSourceScheduled {
			t.Fatalf("seed=%#x phase=%s op=%d operation=validate-source observed=%d expected=%d", seed, phase, op, call.Request.Source, CallSourceScheduled)
		}
		if call.Request.IntendedFireAt != fireAt {
			t.Fatalf("seed=%#x phase=%s op=%d operation=validate-fire-at observed=%d expected=%d", seed, phase, op, call.Request.IntendedFireAt, fireAt)
		}
	}
	if len(seen) != scheduleCount {
		t.Fatalf("seed=%#x phase=%s operation=validate-cardinality observed=%d expected=%d", seed, phase, len(seen), scheduleCount)
	}
}
