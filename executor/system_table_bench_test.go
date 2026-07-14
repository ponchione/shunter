package executor

import (
	"context"
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func BenchmarkSweepDanglingClientsByPrimaryKey(b *testing.B) {
	reg, clientsID, _ := benchmarkSystemTableRegistry(b)
	reducers := NewReducerRegistry()
	reducers.Freeze()

	for _, size := range []int{10, 100} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				cs := benchmarkCommittedState(reg)
				insertBenchmarkClients(b, cs, clientsID, size)
				exec := NewExecutor(ExecutorConfig{}, reducers, cs, reg, 0)
				b.StartTimer()
				if err := exec.sweepDanglingClients(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSchedulerCancelByPrimaryKey(b *testing.B) {
	reg, _, scheduledID := benchmarkSystemTableRegistry(b)
	for _, size := range []int{100, 10_000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			cs := benchmarkCommittedState(reg)
			insertBenchmarkSchedules(b, cs, scheduledID, size)
			for b.Loop() {
				tx := store.NewTransaction(cs, reg)
				handle := &schedulerHandle{tx: tx, tableID: scheduledID}
				deleted, err := handle.Cancel(ScheduleID(size))
				if err != nil || !deleted {
					b.Fatalf("Cancel = (%v, %v), want (true, nil)", deleted, err)
				}
			}
		})
	}
}

func BenchmarkScheduleFireByPrimaryKey(b *testing.B) {
	reg, _, scheduledID := benchmarkSystemTableRegistry(b)
	for _, size := range []int{100, 10_000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			cs := benchmarkCommittedState(reg)
			insertBenchmarkSchedules(b, cs, scheduledID, size)
			exec := &Executor{schedTableID: scheduledID}
			for b.Loop() {
				tx := store.NewTransaction(cs, reg)
				if err := exec.advanceOrDeleteSchedule(tx, ScheduleID(size), int64(size)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkSystemTableRegistry(b *testing.B) (schema.SchemaRegistry, schema.TableID, schema.TableID) {
	b.Helper()
	builder := schema.NewBuilder()
	builder.SchemaVersion(1)
	builder.TableDef(schema.TableDefinition{
		Name:    "noop",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: types.KindUint64, PrimaryKey: true}},
	})
	engine, err := builder.Build(schema.EngineOptions{})
	if err != nil {
		b.Fatal(err)
	}
	reg := engine.Registry()
	clients, ok := SysClientsTable(reg)
	if !ok {
		b.Fatal("sys_clients missing")
	}
	scheduled, ok := SysScheduledTable(reg)
	if !ok {
		b.Fatal("sys_scheduled missing")
	}
	return reg, clients.ID, scheduled.ID
}

func benchmarkCommittedState(reg schema.SchemaRegistry) *store.CommittedState {
	cs := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		table, _ := reg.Table(tableID)
		cs.RegisterTable(tableID, store.NewTable(table))
	}
	return cs
}

func insertBenchmarkClients(b *testing.B, cs *store.CommittedState, tableID schema.TableID, count int) {
	b.Helper()
	table, _ := cs.Table(tableID)
	zeroIdentity := types.Identity{}
	for i := 1; i <= count; i++ {
		var conn types.ConnectionID
		binary.LittleEndian.PutUint64(conn[:8], uint64(i))
		if err := table.InsertRow(table.AllocRowID(), types.ProductValue{
			types.NewBytes(conn[:]),
			types.NewBytes(zeroIdentity[:]),
			types.NewInt64(int64(i)),
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func insertBenchmarkSchedules(b *testing.B, cs *store.CommittedState, tableID schema.TableID, count int) {
	b.Helper()
	table, _ := cs.Table(tableID)
	for i := 1; i <= count; i++ {
		if err := table.InsertRow(table.AllocRowID(), types.ProductValue{
			types.NewUint64(uint64(i)),
			types.NewString("noop"),
			types.NewBytes(nil),
			types.NewInt64(int64(i)),
			types.NewInt64(0),
		}); err != nil {
			b.Fatal(err)
		}
	}
}
