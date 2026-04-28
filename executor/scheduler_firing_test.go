package executor

import (
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// firingFixture builds a committed state + registered reducer and returns
// the pieces needed to exercise CallSourceScheduled firing semantics.
// The "fire" reducer records how many times it was called and carries
// an optional sentinel error for failure-path tests.
type firingFixture struct {
	exec       *Executor
	cs         *store.CommittedState
	reg        schema.SchemaRegistry
	schedTable schema.TableID
	calls      *int
}

func newFiringFixture(t *testing.T, reducerErr error) firingFixture {
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

	callCount := 0
	rr := NewReducerRegistry()
	rr.Register(RegisteredReducer{
		Name: "fire",
		Handler: types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) {
			callCount++
			return nil, reducerErr
		}),
	})
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)
	ctx := t.Context()
	go exec.Run(ctx)

	return firingFixture{
		exec:       exec,
		cs:         cs,
		reg:        reg,
		schedTable: schedTS.ID,
		calls:      &callCount,
	}
}

func fireCallCmd(ff firingFixture, id ScheduleID, intendedNs int64) ReducerResponse {
	respCh := make(chan ReducerResponse, 1)
	ff.exec.Submit(CallReducerCmd{
		Request: ReducerRequest{
			ReducerName:    "fire",
			Source:         CallSourceScheduled,
			ScheduleID:     id,
			IntendedFireAt: intendedNs,
		},
		ResponseCh: respCh,
	})
	return <-respCh
}

func TestFiringOneShotDeletesRow(t *testing.T) {
	ff := newFiringFixture(t, nil)
	intendedNs := time.Unix(100, 0).UnixNano()
	seedDone := make(chan struct{})
	go func() {
		seedSchedule(t, ff.cs, ff.schedTable, 7, "fire", nil, intendedNs, 0)
		close(seedDone)
	}()
	select {
	case <-seedDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("seedSchedule deadlocked while executor loop was active")
	}

	resp := fireCallCmd(ff, 7, intendedNs)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}
	if *ff.calls != 1 {
		t.Fatalf("reducer call count = %d, want 1", *ff.calls)
	}
	tbl, _ := ff.cs.Table(ff.schedTable)
	if tbl.RowCount() != 0 {
		t.Fatalf("one-shot commit should delete row; remaining = %d", tbl.RowCount())
	}
}

func TestFiringRepeatAdvancesNextRun(t *testing.T) {
	ff := newFiringFixture(t, nil)
	intendedNs := time.Unix(100, 0).UnixNano()
	repeatNs := int64(5 * time.Second)
	seedSchedule(t, ff.cs, ff.schedTable, 11, "fire", nil, intendedNs, repeatNs)

	resp := fireCallCmd(ff, 11, intendedNs)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	tbl, _ := ff.cs.Table(ff.schedTable)
	if tbl.RowCount() != 1 {
		t.Fatalf("repeating commit should keep row; count=%d", tbl.RowCount())
	}
	for _, row := range tbl.Scan() {
		got := row[SysScheduledColNextRunAtNs].AsInt64()
		want := intendedNs + repeatNs
		if got != want {
			t.Errorf("next_run_at_ns = %d, want %d (intended+repeat)", got, want)
		}
	}
}

func TestFiringFailureLeavesRowUnchanged(t *testing.T) {
	ff := newFiringFixture(t, errFireFailed)
	intendedNs := time.Unix(100, 0).UnixNano()
	seedSchedule(t, ff.cs, ff.schedTable, 99, "fire", nil, intendedNs, 0)

	resp := fireCallCmd(ff, 99, intendedNs)
	if resp.Status != StatusFailedUser {
		t.Fatalf("status=%d, want StatusFailedUser", resp.Status)
	}

	tbl, _ := ff.cs.Table(ff.schedTable)
	if tbl.RowCount() != 1 {
		t.Fatalf("failure should keep row for retry; count=%d", tbl.RowCount())
	}
}

// Fixed-rate: scheduler intended T=100s, execution runs effectively "later"
// (we don't simulate clock drift here since firing is synchronous within the
// test, but the contract is that next = intended+repeat, NOT now+repeat).
func TestFiringFixedRateUsesIntendedFireTime(t *testing.T) {
	ff := newFiringFixture(t, nil)
	intendedNs := time.Unix(100, 0).UnixNano()
	repeatNs := int64(10 * time.Second)
	seedSchedule(t, ff.cs, ff.schedTable, 42, "fire", nil, intendedNs, repeatNs)

	resp := fireCallCmd(ff, 42, intendedNs)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	tbl, _ := ff.cs.Table(ff.schedTable)
	for _, row := range tbl.Scan() {
		got := row[SysScheduledColNextRunAtNs].AsInt64()
		want := intendedNs + repeatNs
		if got != want {
			t.Fatalf("fixed-rate violated: got %d, want %d (now-based would be much larger)", got, want)
		}
	}
}

// Cancel-race: row deleted between enqueue and firing. Reducer still runs
// (at-least-once), commit succeeds, no schedule-advance happens.
func TestFiringMissingRowSucceeds(t *testing.T) {
	ff := newFiringFixture(t, nil)
	// Do NOT seed a row. Scheduler pretends it saw one at time T=50.
	intendedNs := time.Unix(50, 0).UnixNano()

	resp := fireCallCmd(ff, 123, intendedNs)
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}
	if *ff.calls != 1 {
		t.Fatalf("reducer should still run even if row was cancelled; calls=%d", *ff.calls)
	}
}

var errFireFailed = stubError("scheduled reducer failed on purpose")

// TestSchedulerPanicRetainsScheduledRow pins the intentional divergence from
// reference scheduler.rs:445-455 that Shunter
// preserves sys_scheduled rows on reducer panic (consistent with
// reducer-error), while the reference deletes one-shot rows in a
// fresh tx regardless of panic outcome. See
// docs/shunter-design-decisions.md#scheduler-startup-and-firing.
func TestSchedulerPanicRetainsScheduledRow(t *testing.T) {
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

	rr := NewReducerRegistry()
	rr.Register(RegisteredReducer{
		Name: "firePanic",
		Handler: types.ReducerHandler(func(_ *types.ReducerContext, _ []byte) ([]byte, error) {
			panic("scheduled reducer panic on purpose")
		}),
	})
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)
	go exec.Run(t.Context())

	intendedNs := time.Unix(100, 0).UnixNano()
	seedSchedule(t, cs, schedTS.ID, 77, "firePanic", nil, intendedNs, 0)

	respCh := make(chan ReducerResponse, 1)
	exec.Submit(CallReducerCmd{
		Request: ReducerRequest{
			ReducerName:    "firePanic",
			Source:         CallSourceScheduled,
			ScheduleID:     77,
			IntendedFireAt: intendedNs,
		},
		ResponseCh: respCh,
	})
	resp := <-respCh
	if resp.Status != StatusFailedPanic {
		t.Fatalf("status=%d err=%v, want StatusFailedPanic", resp.Status, resp.Error)
	}

	// Reference scheduler.rs:445-455 would delete the one-shot row in
	// a fresh tx after catch_unwind. Shunter rolls back the reducer's
	// tx and leaves the row for retry. Pin the Shunter outcome.
	tbl, _ := cs.Table(schedTS.ID)
	if tbl.RowCount() != 1 {
		t.Fatalf("Shunter preserves one-shot row on panic (intentional divergence); remaining = %d, want 1", tbl.RowCount())
	}
}
