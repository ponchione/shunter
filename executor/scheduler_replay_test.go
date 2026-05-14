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

func TestSchedulerReplayEmpty(t *testing.T) {
	s, _, _, inbox := schedulerWorkerFixture(t)

	maxID := s.ReplayFromCommitted()

	if maxID != 0 {
		t.Errorf("max schedule_id on empty table = %d, want 0", maxID)
	}
	if !s.nextWakeup.IsZero() {
		t.Errorf("nextWakeup on empty table = %v, want zero", s.nextWakeup)
	}
	select {
	case cmd := <-inbox:
		t.Fatalf("empty replay should not enqueue: %+v", cmd)
	default:
	}
}

func TestSchedulerReplayEnqueuesPastDue(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	// now = Unix(100, 0); row at T=50 is due.
	fireAt := time.Unix(50, 0).UnixNano()
	seedSchedule(t, cs, tid, 3, "past", nil, fireAt, 0)

	s.ReplayFromCommitted()

	select {
	case cmd := <-inbox:
		call := cmd.(CallReducerCmd)
		if call.Request.ReducerName != "past" {
			t.Errorf("got %q, want past", call.Request.ReducerName)
		}
		if call.Request.Source != CallSourceScheduled {
			t.Errorf("Source = %v, want CallSourceScheduled", call.Request.Source)
		}
		if call.Request.ScheduleID != 3 {
			t.Errorf("ScheduleID = %d, want 3", call.Request.ScheduleID)
		}
		if call.Request.IntendedFireAt != fireAt {
			t.Errorf("IntendedFireAt = %d, want %d", call.Request.IntendedFireAt, fireAt)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("past-due row should have been enqueued by replay")
	}
}

func TestSchedulerReplayArmsTimerForFuture(t *testing.T) {
	s, cs, tid, _ := schedulerWorkerFixture(t)
	seedSchedule(t, cs, tid, 1, "f", nil, time.Unix(900, 0).UnixNano(), 0)

	s.ReplayFromCommitted()

	if s.nextWakeup != time.Unix(900, 0) {
		t.Errorf("nextWakeup = %v, want %v", s.nextWakeup, time.Unix(900, 0))
	}
}

func TestSchedulerReplayFutureWakeupWaitsForInjectedClockAfterRestart(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	var nowNs atomic.Int64
	nowNs.Store(time.Unix(100, 0).UnixNano())
	s.now = func() time.Time { return time.Unix(0, nowNs.Load()) }
	fireAt := time.Unix(500, 0).UnixNano()
	seedSchedule(t, cs, tid, 2, "recovered-future", nil, fireAt, 0)

	s.ReplayFromCommitted()
	if s.nextWakeup != time.Unix(500, 0) {
		t.Fatalf("nextWakeup = %v, want %v", s.nextWakeup, time.Unix(500, 0))
	}
	select {
	case cmd := <-inbox:
		t.Fatalf("future replay should not enqueue before injected scheduler clock reaches wakeup: %+v", cmd)
	default:
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
		t.Fatalf("future recovered schedule enqueued before injected scheduler clock reached wakeup: %+v", cmd)
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
		if call.Request.ScheduleID != 2 {
			t.Fatalf("ScheduleID = %d, want 2", call.Request.ScheduleID)
		}
		if call.Request.IntendedFireAt != fireAt {
			t.Fatalf("IntendedFireAt = %d, want %d", call.Request.IntendedFireAt, fireAt)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("future recovered schedule was not enqueued after injected scheduler clock advanced")
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSchedulerReplayReturnsMaxID(t *testing.T) {
	s, cs, tid, _ := schedulerWorkerFixture(t)
	seedSchedule(t, cs, tid, 3, "a", nil, time.Unix(900, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 17, "b", nil, time.Unix(900, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 5, "c", nil, time.Unix(900, 0).UnixNano(), 0)

	maxID := s.ReplayFromCommitted()

	if maxID != 17 {
		t.Errorf("maxID = %d, want 17", maxID)
	}
}

func TestSchedulerReplaySuppressesDuplicateFirstPostReplayScan(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	fireAt := time.Unix(50, 0).UnixNano()
	seedSchedule(t, cs, tid, 7, "past", nil, fireAt, 0)

	s.ReplayFromCommitted()
	s.scan()

	var got []ScheduleID
	for {
		select {
		case cmd := <-inbox:
			got = append(got, cmd.(CallReducerCmd).Request.ScheduleID)
		default:
			if len(got) != 1 || got[0] != 7 {
				t.Fatalf("queued schedule IDs after replay+first scan = %v, want [7]", got)
			}
			return
		}
	}
}

func TestSchedulerReplayRepeatedCallDoesNotDuplicateInFlightDueRow(t *testing.T) {
	s, cs, tid, inbox := schedulerWorkerFixture(t)
	fireAt := time.Unix(50, 0).UnixNano()
	seedSchedule(t, cs, tid, 23, "past", nil, fireAt, 0)

	s.ReplayFromCommitted()
	s.ReplayFromCommitted()

	var got []ScheduleID
	for {
		select {
		case cmd := <-inbox:
			got = append(got, cmd.(CallReducerCmd).Request.ScheduleID)
		default:
			if len(got) != 1 || got[0] != 23 {
				t.Fatalf("queued schedule IDs after repeated replay = %v, want [23]", got)
			}
			wasInFlight, wasReplayQueued := s.completeInFlight(23, fireAt)
			if !wasInFlight || !wasReplayQueued {
				t.Fatalf("completeInFlight = (%v, %v), want replay-queued in-flight row", wasInFlight, wasReplayQueued)
			}
			return
		}
	}
}

func TestSchedulerReplayOverflowLeavesSkippedDueRowsRetryable(t *testing.T) {
	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, tid)
	s.now = func() time.Time { return time.Unix(100, 0) }
	seedSchedule(t, cs, tid, 8, "first-due", nil, time.Unix(50, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 9, "skipped-due", nil, time.Unix(60, 0).UnixNano(), 0)

	s.ReplayFromCommitted()
	var replayedID ScheduleID
	select {
	case cmd := <-inbox:
		replayedID = cmd.(CallReducerCmd).Request.ScheduleID
	case <-time.After(100 * time.Millisecond):
		t.Fatal("replay should enqueue one due schedule before inbox saturation")
	}

	s.scan()
	select {
	case cmd := <-inbox:
		scannedID := cmd.(CallReducerCmd).Request.ScheduleID
		if scannedID == replayedID {
			t.Fatalf("scan duplicated replayed schedule %d instead of picking skipped due row", scannedID)
		}
		if replayedID+scannedID != 17 {
			t.Fatalf("replay+scan schedule IDs = %d and %d, want 8 and 9 in either order", replayedID, scannedID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scan did not pick up due schedule skipped by saturated replay")
	}
}

func TestSchedulerRunPicksUpDueRowsSkippedBySaturatedReplay(t *testing.T) {
	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, tid)
	s.now = func() time.Time { return time.Unix(100, 0) }
	seedSchedule(t, cs, tid, 18, "replayed-due", nil, time.Unix(50, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 19, "overflowed-due", nil, time.Unix(60, 0).UnixNano(), 0)

	s.ReplayFromCommitted()
	var replayedID ScheduleID
	select {
	case cmd := <-inbox:
		replayedID = cmd.(CallReducerCmd).Request.ScheduleID
	case <-time.After(100 * time.Millisecond):
		t.Fatal("replay should enqueue one due schedule before inbox saturation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	select {
	case cmd := <-inbox:
		scannedID := cmd.(CallReducerCmd).Request.ScheduleID
		if scannedID == replayedID {
			t.Fatalf("Run duplicated replayed schedule %d instead of picking skipped due row", scannedID)
		}
		if replayedID+scannedID != 37 {
			t.Fatalf("replay+Run schedule IDs = %d and %d, want 18 and 19 in either order", replayedID, scannedID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not pick up due schedule skipped by saturated replay")
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSchedulerRunDrainsMultipleRowsSkippedBySaturatedReplayWithoutDuplicates(t *testing.T) {
	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, tid)
	s.now = func() time.Time { return time.Unix(100, 0) }
	seedSchedule(t, cs, tid, 20, "replayed-or-overflowed-a", nil, time.Unix(50, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 21, "replayed-or-overflowed-b", nil, time.Unix(60, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 22, "replayed-or-overflowed-c", nil, time.Unix(70, 0).UnixNano(), 0)

	s.ReplayFromCommitted()
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	got := map[ScheduleID]int{}
	for len(got) < 3 {
		select {
		case cmd := <-inbox:
			call, ok := cmd.(CallReducerCmd)
			if !ok {
				t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
			}
			got[call.Request.ScheduleID]++
			if got[call.Request.ScheduleID] > 1 {
				t.Fatalf("schedule %d was enqueued more than once; got=%v", call.Request.ScheduleID, got)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("Scheduler.Run did not drain all replay-overflowed due rows; got=%v", got)
		}
	}
	if got[20] != 1 || got[21] != 1 || got[22] != 1 {
		t.Fatalf("queued schedule IDs = %v, want exactly one enqueue for 20, 21, and 22", got)
	}

	s.Notify()
	assertNoReceive(t, inbox, 50*time.Millisecond, "duplicate replay-overflowed schedule after rescan")

	cancel()
	select {
	case <-runDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestSchedulerReplayOverflowStillArmsEarliestFutureWakeup(t *testing.T) {
	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, tid)
	s.now = func() time.Time { return time.Unix(100, 0) }
	seedSchedule(t, cs, tid, 10, "due-a", nil, time.Unix(50, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 11, "due-b", nil, time.Unix(60, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 12, "future-late", nil, time.Unix(900, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 13, "future-early", nil, time.Unix(400, 0).UnixNano(), 0)

	s.ReplayFromCommitted()

	if s.nextWakeup != time.Unix(400, 0) {
		t.Fatalf("nextWakeup after saturated replay = %v, want %v", s.nextWakeup, time.Unix(400, 0))
	}
	select {
	case <-inbox:
	default:
		t.Fatal("replay should have enqueued one due row before saturation")
	}
}

func TestSchedulerReplaySaturatedInboxStillReturnsMaxID(t *testing.T) {
	_, cs, tid, _ := schedulerWorkerFixture(t)
	inbox := make(chan ExecutorCommand, 1)
	s := NewScheduler(inbox, cs, tid)
	s.now = func() time.Time { return time.Unix(100, 0) }
	seedSchedule(t, cs, tid, 30, "queued-due", nil, time.Unix(50, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 99, "skipped-due-high-id", nil, time.Unix(60, 0).UnixNano(), 0)
	seedSchedule(t, cs, tid, 45, "future-after-saturation", nil, time.Unix(900, 0).UnixNano(), 0)

	maxID := s.ReplayFromCommitted()

	if maxID != 99 {
		t.Fatalf("ReplayFromCommitted maxID with saturated inbox = %d, want 99", maxID)
	}
	select {
	case cmd := <-inbox:
		call, ok := cmd.(CallReducerCmd)
		if !ok {
			t.Fatalf("enqueued cmd type=%T, want CallReducerCmd", cmd)
		}
		if call.Request.ScheduleID != 30 && call.Request.ScheduleID != 99 {
			t.Fatalf("queued schedule_id = %d, want one recovered due row 30 or 99", call.Request.ScheduleID)
		}
	default:
		t.Fatal("replay should enqueue one due row before saturation")
	}
	if s.nextWakeup != time.Unix(900, 0) {
		t.Fatalf("nextWakeup after saturated replay = %v, want %v", s.nextWakeup, time.Unix(900, 0))
	}
}

// TestSchedulerReplayPreservesScanOrderWithoutSorting pins the
// intentional divergence from reference scheduler.rs:118-130 that the
// scheduler does not sort past-due rows by next_run_at_ns during replay;
// it preserves the committed-scan order it is given. The committed-state
// TableScan surface is explicitly unordered, so this Shunter pin targets the
// order-preservation seam directly rather than assuming a specific map
// iteration order in the fixture.
func TestSchedulerReplayPreservesScanOrderWithoutSorting(t *testing.T) {
	s := &Scheduler{}
	rows := []types.ProductValue{
		{
			types.NewUint64(1),
			types.NewString("b"),
			types.NewBytes(nil),
			types.NewInt64(time.Unix(20, 0).UnixNano()),
			types.NewInt64(0),
		},
		{
			types.NewUint64(2),
			types.NewString("a"),
			types.NewBytes(nil),
			types.NewInt64(time.Unix(10, 0).UnixNano()),
			types.NewInt64(0),
		},
		{
			types.NewUint64(3),
			types.NewString("c"),
			types.NewBytes(nil),
			types.NewInt64(time.Unix(30, 0).UnixNano()),
			types.NewInt64(0),
		},
	}

	var got []string
	maxID, nextWakeup, ok := s.scanRows(rows, time.Unix(100, 0).UnixNano(), func(row types.ProductValue) bool {
		got = append(got, row[SysScheduledColReducerName].AsString())
		return true
	})
	if !ok {
		t.Fatal("scanRows unexpectedly reported cancellation")
	}
	if maxID != 3 {
		t.Fatalf("maxID = %d, want 3", maxID)
	}
	if !nextWakeup.IsZero() {
		t.Fatalf("nextWakeup = %v, want zero for all-past-due rows", nextWakeup)
	}
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("enqueue count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("enqueue order = %v, want %v (preserve scan order; do not sort by next_run_at_ns)", got, want)
		}
	}
}

// Cross-process restart simulation: NewExecutor should reset schedSeq
// from the max existing schedule_id so post-restart Schedule() doesn't
// clash with replayed rows.
func TestNewExecutorResetsSchedSeqFromExistingRows(t *testing.T) {
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

	// Simulate a prior process that had allocated schedule_id up to 5.
	seedSchedule(t, cs, schedTS.ID, 3, "x", nil, time.Unix(100, 0).UnixNano(), 0)
	seedSchedule(t, cs, schedTS.ID, 5, "y", nil, time.Unix(100, 0).UnixNano(), 0)
	seedSchedule(t, cs, schedTS.ID, 1, "z", nil, time.Unix(100, 0).UnixNano(), 0)

	rr := NewReducerRegistry()
	if err := rr.Register(RegisteredReducer{Name: "new", Handler: noopReducerHandler}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()
	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)

	// Next allocated ID must be 6.
	tx := store.NewTransaction(cs, reg)
	h := exec.newSchedulerHandle(tx)
	got, err := h.Schedule("new", nil, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != 6 {
		t.Errorf("first post-restart Schedule() returned %d, want 6", got)
	}
}

func TestNewExecutorExhaustedRecoveredSchedSeqFailsWithoutWrapping(t *testing.T) {
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

	seedSchedule(t, cs, schedTS.ID, ^uint64(0), "last", nil, time.Unix(100, 0).UnixNano(), 0)

	rr := NewReducerRegistry()
	if err := rr.Register(RegisteredReducer{Name: "wrapped", Handler: noopReducerHandler}); err != nil {
		t.Fatal(err)
	}
	rr.Freeze()
	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)

	tx := store.NewTransaction(cs, reg)
	h := exec.newSchedulerHandle(tx)
	got, err := h.Schedule("wrapped", nil, time.Unix(200, 0))
	if !errors.Is(err, ErrScheduleIDExhausted) {
		t.Fatalf("Schedule after recovered max schedule_id = (%d, %v), want ErrScheduleIDExhausted", got, err)
	}
	if got != 0 {
		t.Fatalf("Schedule after recovered max schedule_id returned id %d, want 0", got)
	}
	if inserts := tx.TxState().Inserts(h.tableID); len(inserts) != 0 {
		t.Fatalf("exhausted schedule sequence inserted rows=%v, want none", inserts)
	}
}
