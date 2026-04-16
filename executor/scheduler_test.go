package executor

import (
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// setupScheduler builds a CommittedState + SchemaRegistry that includes
// sys_scheduled, then returns a fresh Transaction + handle over it.
func setupScheduler(t *testing.T) (*store.Transaction, *schedulerHandle, schema.SchemaRegistry) {
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

	ts, ok := SysScheduledTable(reg)
	if !ok {
		t.Fatal("sys_scheduled must resolve")
	}

	tx := store.NewTransaction(cs, reg)
	h := &schedulerHandle{
		tx:      tx,
		tableID: ts.ID,
		seq:     store.NewSequence(),
	}
	return tx, h, reg
}

func TestSchedulerHandleScheduleInsertsRow(t *testing.T) {
	tx, h, _ := setupScheduler(t)

	fireAt := time.Date(2030, 1, 2, 3, 4, 5, 6, time.UTC)
	id, err := h.Schedule("greet", []byte{0xaa, 0xbb}, fireAt)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("ScheduleID must not be zero")
	}

	var found types.ProductValue
	for _, row := range tx.TxState().Inserts(h.tableID) {
		if row[SysScheduledColScheduleID].AsUint64() == uint64(id) {
			found = row
			break
		}
	}
	if found == nil {
		t.Fatal("inserted row not visible in tx state")
	}
	if got := found[SysScheduledColReducerName].AsString(); got != "greet" {
		t.Errorf("reducer_name = %q, want greet", got)
	}
	if got := string(found[SysScheduledColArgs].AsBytes()); got != string([]byte{0xaa, 0xbb}) {
		t.Errorf("args payload mismatch: got %x", found[SysScheduledColArgs].AsBytes())
	}
	if got := found[SysScheduledColNextRunAtNs].AsInt64(); got != fireAt.UnixNano() {
		t.Errorf("next_run_at_ns = %d, want %d", got, fireAt.UnixNano())
	}
	if got := found[SysScheduledColRepeatNs].AsInt64(); got != 0 {
		t.Errorf("repeat_ns = %d, want 0 (one-shot)", got)
	}
}

func TestSchedulerHandleScheduleRepeatSetsRepeatNs(t *testing.T) {
	tx, h, _ := setupScheduler(t)

	interval := 250 * time.Millisecond
	id, err := h.ScheduleRepeat("tick", nil, interval)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("ScheduleID must not be zero")
	}

	var found types.ProductValue
	for _, row := range tx.TxState().Inserts(h.tableID) {
		if row[SysScheduledColScheduleID].AsUint64() == uint64(id) {
			found = row
			break
		}
	}
	if found == nil {
		t.Fatal("repeat row missing")
	}
	if got := found[SysScheduledColRepeatNs].AsInt64(); got != interval.Nanoseconds() {
		t.Errorf("repeat_ns = %d, want %d", got, interval.Nanoseconds())
	}
	if got := found[SysScheduledColNextRunAtNs].AsInt64(); got <= time.Now().Add(-1*time.Second).UnixNano() {
		t.Errorf("next_run_at_ns should be near now, got %d", got)
	}
}

func TestSchedulerHandleScheduleDistinctIDs(t *testing.T) {
	_, h, _ := setupScheduler(t)

	a, err := h.Schedule("r1", nil, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Schedule("r2", nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("IDs must be distinct: a=%d b=%d", a, b)
	}
	if b <= a {
		t.Errorf("sequence must grow: a=%d b=%d", a, b)
	}
}

func TestSchedulerHandleCancelDeletesTxLocal(t *testing.T) {
	tx, h, _ := setupScheduler(t)

	id, err := h.Schedule("r", nil, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := h.Cancel(id)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("Cancel should return true for existing schedule")
	}

	// Row should no longer appear as a tx-local insert.
	for _, row := range tx.TxState().Inserts(h.tableID) {
		if row[SysScheduledColScheduleID].AsUint64() == uint64(id) {
			t.Fatal("cancelled row still visible in tx inserts")
		}
	}
}

func TestSchedulerHandleCancelMissing(t *testing.T) {
	_, h, _ := setupScheduler(t)
	deleted, err := h.Cancel(ScheduleID(9999))
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("Cancel of missing id should return false")
	}
}

// Integration-ish: run a reducer that schedules, verify commit persists the row.
func TestSchedulerHandleCommitPersistsRow(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	eng, _ := b.Build(schema.EngineOptions{})
	reg := eng.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}

	rr := NewReducerRegistry()
	rr.Register(RegisteredReducer{
		Name: "sched",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		h := ctx.Scheduler
			_, err := h.Schedule("tick", nil, time.Unix(42, 0))
			return nil, err
		}),
	})
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)
	ctx := t.Context()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "sched", Source: CallSourceExternal},
		ResponseCh: respCh,
	})
	resp := <-respCh
	if resp.Status != StatusCommitted {
		t.Fatalf("status=%d err=%v", resp.Status, resp.Error)
	}

	schedTID, _ := SysScheduledTable(reg)
	tbl, _ := cs.Table(schedTID.ID)
	if tbl.RowCount() != 1 {
		t.Fatalf("committed sys_scheduled row count = %d, want 1", tbl.RowCount())
	}
}

func TestSchedulerHandleRollbackDiscardsSchedule(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "noop",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
		},
	})
	eng, _ := b.Build(schema.EngineOptions{})
	reg := eng.Registry()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}

	rr := NewReducerRegistry()
	sentinel := &struct{ err error }{err: errSchedRollback}
	rr.Register(RegisteredReducer{
		Name: "schedFail",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
			h := ctx.Scheduler
			_, _ = h.Schedule("tick", nil, time.Unix(42, 0))
			return nil, sentinel.err
		}),
	})
	rr.Freeze()

	exec := NewExecutor(ExecutorConfig{InboxCapacity: 4}, rr, cs, reg, 0)
	ctx := t.Context()
	go exec.Run(ctx)

	respCh := make(chan ReducerResponse, 1)
	exec.Submit(CallReducerCmd{
		Request:    ReducerRequest{ReducerName: "schedFail", Source: CallSourceExternal},
		ResponseCh: respCh,
	})
	resp := <-respCh
	if resp.Status != StatusFailedUser {
		t.Fatalf("status=%d, want StatusFailedUser, err=%v", resp.Status, resp.Error)
	}

	schedTID, _ := SysScheduledTable(reg)
	tbl, _ := cs.Table(schedTID.ID)
	if tbl.RowCount() != 0 {
		t.Fatalf("rollback should discard schedule; committed row count = %d", tbl.RowCount())
	}
}

var errSchedRollback = stubError("reducer rolled back")

type stubError string

func (s stubError) Error() string { return string(s) }
