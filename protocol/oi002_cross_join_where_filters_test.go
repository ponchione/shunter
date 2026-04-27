package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002CrossJoinWhereMultipleLiteralFiltersMixedProjection(t *testing.T) {
	conn := testConnDirect(nil)
	projectedSchema := &schema.TableSchema{
		Name: "Inventory",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "quantity", Type: schema.KindUint32},
			{Index: 1, Name: "id", Type: schema.KindUint32},
		},
	}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "Inventory",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "quantity", Type: schema.KindUint32},
			{Name: "enabled", Type: schema.KindBool},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	projectedSchema.ID = inventoryReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID: {
			{types.NewUint32(1), types.NewUint32(100)},
			{types.NewUint32(2), types.NewUint32(100)},
			{types.NewUint32(3), types.NewUint32(200)},
			{types.NewUint32(4), types.NewUint32(300)},
		},
		inventoryReg.ID: {
			{types.NewUint32(100), types.NewUint32(9), types.NewBool(false)},
			{types.NewUint32(200), types.NewUint32(3), types.NewBool(true)},
			{types.NewUint32(300), types.NewUint32(7), types.NewBool(true)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID: []byte{0x91, 0x03},
		QueryString: "SELECT product.quantity, o.id FROM Orders o JOIN Inventory product " +
			"WHERE o.product_id = product.id AND product.enabled = TRUE AND o.id > 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "Inventory" {
		t.Fatalf("Tables = %+v, want single first-projected-relation Inventory envelope", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{
		{types.NewUint32(3), types.NewUint32(3)},
		{types.NewUint32(7), types.NewUint32(4)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}
