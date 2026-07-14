package executor

import (
	"testing"
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestSystemTablePrimaryKeyLookupTracksTransactionLocalInsertAndDelete(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{})
	tx := store.NewTransaction(h.cs, h.reg)
	conn := types.ConnectionID{1}
	identity := types.Identity{2}
	row := types.ProductValue{
		types.NewBytes(conn[:]),
		types.NewBytes(identity[:]),
		types.NewInt64(3),
	}
	wantRowID, err := tx.Insert(h.sysClients, row)
	if err != nil {
		t.Fatal(err)
	}
	rowID, got, found := systemTableRowByPrimaryKey(tx, h.sysClients, types.NewBytes(conn[:]))
	if !found || rowID != wantRowID || !got.Equal(row) {
		t.Fatalf("lookup after tx insert = (%d, %v, %v), want (%d, row, true)", rowID, got, found, wantRowID)
	}

	if err := deleteSysClientsRow(tx, h.sysClients, conn); err != nil {
		t.Fatal(err)
	}
	if _, _, found := systemTableRowByPrimaryKey(tx, h.sysClients, types.NewBytes(conn[:])); found {
		t.Fatal("primary-key lookup returned row deleted in the same transaction")
	}
}

func TestAdvanceOrDeleteScheduleFindsTransactionLocalRowByPrimaryKey(t *testing.T) {
	tx, scheduler, _ := setupScheduler(t)
	id, err := scheduler.Schedule("r", nil, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	exec := &Executor{schedTableID: scheduler.tableID}
	if err := exec.advanceOrDeleteSchedule(tx, id, time.Unix(10, 0).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, _, found := systemTableRowByPrimaryKey(tx, scheduler.tableID, types.NewUint64(uint64(id))); found {
		t.Fatal("one-shot schedule fired in the transaction remains visible")
	}
}
