package protocol

import (
	"testing"

	"github.com/ponchione/shunter/schema"
)

type oi002OrdersInventoryOptions struct {
	InventoryEnabledColumn bool
	OrdersProductIDIndex   bool
}

type oi002OrdersInventoryFixture struct {
	lookup    registrySchemaLookup
	orders    *schema.TableSchema
	inventory *schema.TableSchema
}

func newOI002OrdersInventoryFixture(t *testing.T, opts oi002OrdersInventoryOptions) oi002OrdersInventoryFixture {
	t.Helper()

	orders := schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
	}
	if opts.OrdersProductIDIndex {
		orders.Indexes = []schema.IndexDefinition{{Name: "idx_orders_product_id", Columns: []string{"product_id"}}}
	}

	inventoryColumns := []schema.ColumnDefinition{
		{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
		{Name: "quantity", Type: schema.KindUint32},
	}
	if opts.InventoryEnabledColumn {
		inventoryColumns = append(inventoryColumns, schema.ColumnDefinition{Name: "enabled", Type: schema.KindBool})
	}

	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(orders)
	b.TableDef(schema.TableDefinition{
		Name:    "Inventory",
		Columns: inventoryColumns,
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

	return oi002OrdersInventoryFixture{
		lookup:    registrySchemaLookup{reg: eng.Registry()},
		orders:    ordersReg,
		inventory: inventoryReg,
	}
}

func (f oi002OrdersInventoryFixture) inventoryProjectionSchema(columns ...schema.ColumnSchema) *schema.TableSchema {
	return &schema.TableSchema{
		ID:      f.inventory.ID,
		Name:    "Inventory",
		Columns: columns,
	}
}
