package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002JoinProjectionCrossJoinMixedColumnsReturnsBothSides(t *testing.T) {
	conn := testConnDirect(nil)
	fixture := newOI002OrdersInventoryFixture(t, oi002OrdersInventoryOptions{})
	projectedSchema := fixture.inventoryProjectionSchema(
		schema.ColumnSchema{Index: 0, Name: "quantity", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		fixture.orders.ID: {
			{types.NewUint32(1), types.NewUint32(100)},
			{types.NewUint32(2), types.NewUint32(200)},
		},
		fixture.inventory.ID: {
			{types.NewUint32(10), types.NewUint32(9)},
			{types.NewUint32(11), types.NewUint32(3)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x91, 0x02},
		QueryString: "SELECT product.quantity, o.id FROM Orders o JOIN Inventory product",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, fixture.lookup)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "Inventory" {
		t.Fatalf("Tables = %+v, want single first-projected-relation Inventory envelope", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{
		{types.NewUint32(9), types.NewUint32(1)},
		{types.NewUint32(9), types.NewUint32(2)},
		{types.NewUint32(3), types.NewUint32(1)},
		{types.NewUint32(3), types.NewUint32(2)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}
