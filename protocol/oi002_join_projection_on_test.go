package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002JoinProjectionOn_JoinOnResolutionPrecedesProjectionQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		}}},
		"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		}}},
	}}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT x.* FROM t JOIN s ON t.missing = s.id"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF3},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "`missing` is not in scope"
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002JoinProjectionOn_SubscribeJoinOnResolutionPrecedesProjectionQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		}}},
		"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		}}},
	}}

	const sqlText = "SELECT x.* FROM t JOIN s ON t.missing = s.id"
	msg := &SubscribeSingleMsg{
		RequestID:   736,
		QueryID:     737,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}

func TestOI002JoinProjectionOn_RightColumnListMatchesWildcardOrder(t *testing.T) {
	fixture := newOI002OrdersInventoryFixture(t, oi002OrdersInventoryOptions{OrdersProductIDIndex: true})
	projectedTS := &schema.TableSchema{ID: fixture.inventory.ID, Name: "Inventory", Columns: fixture.inventory.Columns}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		fixture.orders.ID: {
			{types.NewUint32(1), types.NewUint32(102)},
			{types.NewUint32(2), types.NewUint32(100)},
			{types.NewUint32(3), types.NewUint32(100)},
		},
		fixture.inventory.ID: {
			{types.NewUint32(100), types.NewUint32(9)},
			{types.NewUint32(102), types.NewUint32(3)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	run := func(query string, messageID byte) []types.ProductValue {
		t.Helper()
		conn := testConnDirect(nil)
		handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
			MessageID:   []byte{messageID},
			QueryString: query,
		}, stateAccess, fixture.lookup)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("%s error = %q, want nil", query, *result.Error)
		}
		if len(result.Tables) != 1 || result.Tables[0].TableName != "Inventory" {
			t.Fatalf("%s tables = %+v, want single Inventory envelope", query, result.Tables)
		}
		return decodeRows(t, firstTableRows(result), projectedTS)
	}

	wildcardRows := run("SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id", 0xF5)
	columnRows := run("SELECT product.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id", 0xF6)
	assertProductRowsEqual(t, columnRows, wildcardRows)
}
