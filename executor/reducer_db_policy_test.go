package executor

import (
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestReducerDBRejectsSystemTableMutationsAndAllowsReads(t *testing.T) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "items",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "body", Type: types.KindString},
		},
	})
	engine, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := engine.Registry()
	cs := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		table, _ := reg.Table(tableID)
		cs.RegisterTable(tableID, store.NewTable(table))
	}

	itemsID, _, ok := reg.TableByName("items")
	if !ok {
		t.Fatal("items table missing")
	}
	clients, ok := SysClientsTable(reg)
	if !ok {
		t.Fatal("sys_clients table missing")
	}
	scheduled, ok := SysScheduledTable(reg)
	if !ok {
		t.Fatal("sys_scheduled table missing")
	}

	tx := store.NewTransaction(cs, reg)
	conn := types.ConnectionID{1}
	identity := types.Identity{2}
	clientRow := types.ProductValue{
		types.NewBytes(conn[:]),
		types.NewBytes(identity[:]),
		types.NewInt64(3),
	}
	clientRowID, err := tx.Insert(clients.ID, clientRow)
	if err != nil {
		t.Fatal(err)
	}
	scheduleRow := types.ProductValue{
		types.NewUint64(7),
		types.NewString("tick"),
		types.NewBytes(nil),
		types.NewInt64(8),
		types.NewInt64(0),
	}
	scheduleRowID, err := tx.Insert(scheduled.ID, scheduleRow)
	if err != nil {
		t.Fatal(err)
	}

	db := (&Executor{schemaReg: reg, schedTableID: scheduled.ID}).newReducerDBAdapter(tx)
	for _, tc := range []struct {
		name    string
		tableID schema.TableID
		rowID   types.RowID
		row     types.ProductValue
	}{
		{name: SysClientsTableName, tableID: clients.ID, rowID: clientRowID, row: clientRow},
		{name: SysScheduledTableName, tableID: scheduled.ID, rowID: scheduleRowID, row: scheduleRow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Insert(uint32(tc.tableID), tc.row); !errors.Is(err, ErrReducerSystemTableMutation) || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("Insert error = %v, want system-table mutation error naming %s", err, tc.name)
			}
			if err := db.Delete(uint32(tc.tableID), tc.rowID); !errors.Is(err, ErrReducerSystemTableMutation) || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("Delete error = %v, want system-table mutation error naming %s", err, tc.name)
			}
			if _, err := db.Update(uint32(tc.tableID), tc.rowID, tc.row); !errors.Is(err, ErrReducerSystemTableMutation) || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("Update error = %v, want system-table mutation error naming %s", err, tc.name)
			}

			if row, found := db.GetRow(uint32(tc.tableID), tc.rowID); !found || !row.Equal(tc.row) {
				t.Fatalf("GetRow = (%v, %v), want system row", row, found)
			}
			if got := countReducerRows(db.ScanTable(uint32(tc.tableID))); got != 1 {
				t.Fatalf("ScanTable row count = %d, want 1", got)
			}
			if got := countReducerRows(db.SeekIndex(uint32(tc.tableID), 0, tc.row[0])); got != 1 {
				t.Fatalf("SeekIndex row count = %d, want 1", got)
			}
		})
	}

	itemID, err := db.Insert(uint32(itemsID), types.ProductValue{types.NewUint64(1), types.NewString("first")})
	if err != nil {
		t.Fatalf("Insert user row: %v", err)
	}
	updatedItemID, err := db.Update(uint32(itemsID), itemID, types.ProductValue{types.NewUint64(1), types.NewString("updated")})
	if err != nil {
		t.Fatalf("Update user row: %v", err)
	}
	if err := db.Delete(uint32(itemsID), updatedItemID); err != nil {
		t.Fatalf("Delete user row: %v", err)
	}
	if got := db.Underlying(); got != nil {
		t.Fatalf("Underlying = %T, want nil", got)
	}
}

func countReducerRows(rows iter.Seq2[types.RowID, types.ProductValue]) int {
	count := 0
	for range rows {
		count++
	}
	return count
}
