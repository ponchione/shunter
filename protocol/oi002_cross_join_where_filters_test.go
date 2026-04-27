package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002CrossJoinWhereMultipleLiteralFiltersMixedProjection(t *testing.T) {
	conn := testConnDirect(nil)
	fixture := newOI002OrdersInventoryFixture(t, oi002OrdersInventoryOptions{InventoryEnabledColumn: true})
	projectedSchema := fixture.inventoryProjectionSchema(
		schema.ColumnSchema{Index: 0, Name: "quantity", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		fixture.orders.ID: {
			{types.NewUint32(1), types.NewUint32(100)},
			{types.NewUint32(2), types.NewUint32(100)},
			{types.NewUint32(3), types.NewUint32(200)},
			{types.NewUint32(4), types.NewUint32(300)},
		},
		fixture.inventory.ID: {
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
		{types.NewUint32(3), types.NewUint32(3)},
		{types.NewUint32(7), types.NewUint32(4)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestOI002CrossJoinWhereLiteralFilterValidationPrecedesConstantFolding(t *testing.T) {
	conn := testConnDirect(nil)
	sl := exactIdentifierJoinSchema()
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

	const sqlText = "SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND (TRUE OR s.missing = 1)"
	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x91, 0x04},
		QueryString: sqlText,
	}, stateAccess, sl)

	requireOneOffError(t, conn, "`missing` is not in scope")
}
