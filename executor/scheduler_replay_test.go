package executor

import (
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
	seedSchedule(t, cs, tid, 3, "past", nil, time.Unix(50, 0).UnixNano(), 0)

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

// TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting pins the
// intentional divergence from reference scheduler.rs:118-130 that the
// scheduler does not sort past-due rows by next_run_at_ns during replay;
// it preserves the committed-scan order it is given. The committed-state
// TableScan surface is explicitly unordered, so this parity pin targets the
// order-preservation seam directly rather than assuming a specific map
// iteration order in the fixture.
func TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting(t *testing.T) {
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
