package protocol

import (
	"bytes"
	"context"
	"iter"
	"testing"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// --- Mocks ---

// mockSnapshot implements store.CommittedReadView with in-memory rows.
type mockSnapshot struct {
	rows map[schema.TableID][]types.ProductValue
}

func (s *mockSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for i, pv := range s.rows[id] {
			if !yield(types.RowID(i), pv) {
				return
			}
		}
	}
}

func (s *mockSnapshot) IndexScan(_ schema.TableID, _ schema.IndexID, _ types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	return func(func(types.RowID, types.ProductValue) bool) {}
}

func (s *mockSnapshot) IndexSeek(_ schema.TableID, _ schema.IndexID, _ store.IndexKey) []types.RowID {
	return nil
}

func (s *mockSnapshot) IndexRange(_ schema.TableID, _ schema.IndexID, _, _ store.Bound) iter.Seq2[types.RowID, types.ProductValue] {
	return func(func(types.RowID, types.ProductValue) bool) {}
}

func (s *mockSnapshot) GetRow(_ schema.TableID, _ types.RowID) (types.ProductValue, bool) {
	return nil, false
}

func (s *mockSnapshot) RowCount(id schema.TableID) int { return len(s.rows[id]) }

func (s *mockSnapshot) Close() {}

// mockStateAccess implements CommittedStateAccess.
type mockStateAccess struct {
	snap *mockSnapshot
}

func (m *mockStateAccess) Snapshot() store.CommittedReadView { return m.snap }

// --- Helpers ---

// drainOneOff reads one frame from OutboundCh and decodes it as a
// OneOffQueryResult. Fatals if nothing is queued or decode fails.
func drainOneOff(t *testing.T, conn *Conn) OneOffQueryResult {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		_, msg, err := DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("DecodeServerMessage: %v", err)
		}
		result, ok := msg.(OneOffQueryResult)
		if !ok {
			t.Fatalf("expected OneOffQueryResult, got %T", msg)
		}
		return result
	default:
		t.Fatal("expected a frame on OutboundCh, got none")
		return OneOffQueryResult{} // unreachable
	}
}

// decodeRows decodes a RowList payload back into ProductValues using
// the given schema for BSATN decoding.
func decodeRows(t *testing.T, encoded []byte, ts *schema.TableSchema) []types.ProductValue {
	t.Helper()
	rawRows, err := DecodeRowList(encoded)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	pvs := make([]types.ProductValue, 0, len(rawRows))
	for _, raw := range rawRows {
		pv, err := bsatn.DecodeProductValueFromBytes(raw, ts)
		if err != nil {
			t.Fatalf("DecodeProductValueFromBytes: %v", err)
		}
		pvs = append(pvs, pv)
	}
	return pvs
}

// --- Tests ---

func TestHandleOneOffQuery_Valid(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewString("alice")},
				{types.NewUint32(2), types.NewString("bob")},
				{types.NewUint32(3), types.NewString("carol")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x10},
		QueryString: "SELECT * FROM users WHERE id = 2",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if !bytes.Equal(result.MessageID, msg.MessageID) {
		t.Errorf("MessageID = %v, want %v", result.MessageID, msg.MessageID)
	}
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}

	pvs := decodeRows(t, result.Rows, ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(2)) {
		t.Errorf("row[0].id = %v, want Uint32(2)", pvs[0][0])
	}
	if !pvs[0][1].Equal(types.NewString("bob")) {
		t.Errorf("row[0].name = %v, want String(bob)", pvs[0][1])
	}
}

func TestHandleOneOffQuery_QualifiedStarAlias(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewString("alice")},
				{types.NewUint32(2), types.NewString("bob")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x15},
		QueryString: "SELECT item.* FROM users AS item WHERE item.name = 'alice'",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if !bytes.Equal(result.MessageID, msg.MessageID) {
		t.Errorf("MessageID = %v, want %v", result.MessageID, msg.MessageID)
	}
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}

	pvs := decodeRows(t, result.Rows, ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) {
		t.Errorf("row[0].id = %v, want Uint32(1)", pvs[0][0])
	}
	if !pvs[0][1].Equal(types.NewString("alice")) {
		t.Errorf("row[0].name = %v, want String(alice)", pvs[0][1])
	}
}

func TestHandleOneOffQuery_LessThanOrEqualComparison(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewUint32(9)},
				{types.NewUint32(2), types.NewUint32(10)},
				{types.NewUint32(3), types.NewUint32(11)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x16},
		QueryString: "SELECT * FROM metrics WHERE score <= 10",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(2)) {
		t.Fatalf("unexpected ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_NotEqualComparison(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewUint32(9)},
				{types.NewUint32(2), types.NewUint32(10)},
				{types.NewUint32(3), types.NewUint32(11)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x17},
		QueryString: "SELECT * FROM metrics WHERE score <> 10",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_OrComparison(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewUint32(9)},
				{types.NewUint32(2), types.NewUint32(10)},
				{types.NewUint32(3), types.NewUint32(11)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x18},
		QueryString: "SELECT * FROM metrics WHERE score = 9 OR score = 11",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_JoinProjectionOnLeftTable(t *testing.T) {
	conn := testConnDirect(nil)
	ordersTS := &schema.TableSchema{
		ID:   1,
		Name: "Orders",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "product_id", Type: schema.KindUint32},
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
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	ordersTS.ID = ordersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			ordersReg.ID: {
				{types.NewUint32(1), types.NewUint32(100)},
				{types.NewUint32(2), types.NewUint32(101)},
				{types.NewUint32(3), types.NewUint32(102)},
			},
			inventoryReg.ID: {
				{types.NewUint32(100), types.NewUint32(9)},
				{types.NewUint32(101), types.NewUint32(10)},
				{types.NewUint32(102), types.NewUint32(3)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x19},
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, ordersTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected order ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
	if !pvs[0][1].Equal(types.NewUint32(100)) || !pvs[1][1].Equal(types.NewUint32(102)) {
		t.Fatalf("unexpected product ids returned: %v, %v", pvs[0][1], pvs[1][1])
	}
}

func TestHandleOneOffQuery_JoinProjectionOnRightTable(t *testing.T) {
	conn := testConnDirect(nil)
	inventoryTS := &schema.TableSchema{
		ID:   2,
		Name: "Inventory",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "quantity", Type: schema.KindUint32},
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
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	inventoryTS.ID = inventoryReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			ordersReg.ID: {
				{types.NewUint32(1), types.NewUint32(100)},
				{types.NewUint32(2), types.NewUint32(100)},
				{types.NewUint32(3), types.NewUint32(102)},
			},
			inventoryReg.ID: {
				{types.NewUint32(100), types.NewUint32(9)},
				{types.NewUint32(101), types.NewUint32(10)},
				{types.NewUint32(102), types.NewUint32(3)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1a},
		QueryString: "SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, inventoryTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(100)) || !pvs[1][0].Equal(types.NewUint32(102)) {
		t.Fatalf("unexpected inventory ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
	if !pvs[0][1].Equal(types.NewUint32(9)) || !pvs[1][1].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected quantities returned: %v, %v", pvs[0][1], pvs[1][1])
	}
}

func TestHandleOneOffQuery_JoinProjectionOnRightTableWithLeftFilter(t *testing.T) {
	conn := testConnDirect(nil)
	inventoryTS := &schema.TableSchema{
		ID:   2,
		Name: "Inventory",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "quantity", Type: schema.KindUint32},
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
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	inventoryTS.ID = inventoryReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			ordersReg.ID: {
				{types.NewUint32(1), types.NewUint32(100)},
				{types.NewUint32(2), types.NewUint32(100)},
				{types.NewUint32(3), types.NewUint32(102)},
			},
			inventoryReg.ID: {
				{types.NewUint32(100), types.NewUint32(9)},
				{types.NewUint32(101), types.NewUint32(10)},
				{types.NewUint32(102), types.NewUint32(3)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1b},
		QueryString: "SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE o.id = 1",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, inventoryTS)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(100)) {
		t.Fatalf("unexpected inventory id returned: %v", pvs[0][0])
	}
	if !pvs[0][1].Equal(types.NewUint32(9)) {
		t.Fatalf("unexpected quantity returned: %v", pvs[0][1])
	}
}

func TestHandleOneOffQuery_AliasedSelfEquiJoin(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1f}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, tTS)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3 (every row matches itself by u32)", len(pvs))
	}
}

func TestHandleOneOffQuery_AliasedSelfEquiJoinWithWhereAside(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	// Three rows all share u32=5; only id=1 satisfies `a.id = 1`.
	// The filter must only constrain the a-side, so every b is still a
	// valid partner. Expected projected rows: one (id=1, u32=5).
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x21}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, tTS)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1 (only a-side row with id=1)", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) {
		t.Fatalf("got id=%v, want id=1", pvs[0][0])
	}
}

func TestHandleOneOffQuery_AliasedSelfEquiJoinWithWhereBside(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	// Filter on the b-side: every a-side row with matching u32 is emitted
	// because b's id=1 row covers all of them. Projection is a.*, so all 3
	// rows are expected.
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x22}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE b.id = 1"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, tTS)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3 (every a-side row has a b partner with id=1 via u32=5)", len(pvs))
	}
}

// TD-142 Slice 14: one-off self-join RHS projection (`SELECT b.*`) must
// return only b-side rows. For a self-join both sides share the same
// physical table, so Join.ProjectRight is the only signal distinguishing
// b.* from a.* inside the one-off evaluator.
func TestHandleOneOffQuery_AliasedSelfEquiJoinProjectsRight(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	// Only id=2 matches a-side filter, but projection is b.* so every b with
	// matching u32 comes through. Three rows share u32=5 so b may be any of
	// them whenever a matches; the projected rows are distinct b-rows.
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x2a}, QueryString: "SELECT b.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 2"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, tTS)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3 (every b partners with the single a.id=2)", len(pvs))
	}
}

func TestHandleOneOffQuery_AliasedSelfCrossJoinProjection(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {{types.NewUint32(1)}, {types.NewUint32(2)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1d}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, tTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
}

func TestHandleOneOffQuery_AliasedSelfCrossJoinEmptyTable(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{tReg.ID: nil}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1e}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, tTS)
	if len(pvs) != 0 {
		t.Fatalf("got %d rows, want 0 (empty table self-join should project nothing)", len(pvs))
	}
}

// TestHandleOneOffQuery_MultiWayJoinRejected pins the reference-matched
// rejection of three-way join shapes at the one-off admission boundary.
// Client receives OneOffQueryResult with Status=1 and a non-empty error.
// Reference subscription runtime bails with
// "Invalid number of tables in subscription: {N}" at
// reference/SpacetimeDB/crates/subscription/src/lib.rs:251.
func TestHandleOneOffQuery_MultiWayJoinRejected(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	cases := []struct {
		name        string
		queryString string
	}{
		{"cross_chain", "SELECT t.* FROM t JOIN s JOIN s AS r"},
		{"on_chain", "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 JOIN s AS r ON s.u32 = r.u32"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := registrySchemaLookup{reg: eng.Registry()}
			snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
			stateAccess := &mockStateAccess{snap: snap}
			msg := &OneOffQueryMsg{MessageID: []byte{0x60}, QueryString: c.queryString}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
			result := drainOneOff(t, conn)
			if result.Status != 1 {
				t.Fatalf("Status = %d, want 1 (error)", result.Status)
			}
			if result.Error == "" {
				t.Fatal("expected non-empty error message")
			}
		})
	}
}

func TestHandleOneOffQuery_CrossJoinProjection(t *testing.T) {
	conn := testConnDirect(nil)
	ordersTS := &schema.TableSchema{ID: 1, Name: "Orders", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "Orders", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}}})
	b.TableDef(schema.TableDefinition{Name: "Inventory", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	ordersReg, _ := eng.Registry().TableByName("Orders")
	inventoryReg, _ := eng.Registry().TableByName("Inventory")
	ordersTS.ID = ordersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID:    {{types.NewUint32(1)}, {types.NewUint32(2)}},
		inventoryReg.ID: {{types.NewUint32(10)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1c}, QueryString: "SELECT o.* FROM Orders o JOIN Inventory product"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}
	pvs := decodeRows(t, result.Rows, ordersTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
}

func TestHandleOneOffQuery_NoMatches(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewString("alice")},
				{types.NewUint32(2), types.NewString("bob")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x20},
		QueryString: "SELECT * FROM users WHERE id = 999",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if !bytes.Equal(result.MessageID, msg.MessageID) {
		t.Errorf("MessageID = %v, want %v", result.MessageID, msg.MessageID)
	}
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}

	rawRows, err := DecodeRowList(result.Rows)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rawRows) != 0 {
		t.Errorf("got %d rows, want 0", len(rawRows))
	}
}

func TestHandleOneOffQuery_EmptyPredicates(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   2,
		Name: "items",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "label", Type: schema.KindString},
		},
	}
	sl := newMockSchema("items", 2, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			2: {
				{types.NewUint32(10), types.NewString("alpha")},
				{types.NewUint32(20), types.NewString("beta")},
				{types.NewUint32(30), types.NewString("gamma")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x30},
		QueryString: "SELECT * FROM items",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Status != 0 {
		t.Fatalf("Status = %d, want 0; Error = %q", result.Status, result.Error)
	}

	pvs := decodeRows(t, result.Rows, ts)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3", len(pvs))
	}

	// Verify all three rows came back in order.
	wantIDs := []uint32{10, 20, 30}
	for i, wantID := range wantIDs {
		if !pvs[i][0].Equal(types.NewUint32(wantID)) {
			t.Errorf("row[%d].id = %v, want Uint32(%d)", i, pvs[i][0], wantID)
		}
	}
}

func TestHandleOneOffQuery_UnknownTable(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x40},
		QueryString: "SELECT * FROM nonexistent",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if !bytes.Equal(result.MessageID, msg.MessageID) {
		t.Errorf("MessageID = %v, want %v", result.MessageID, msg.MessageID)
	}
	if result.Status != 1 {
		t.Fatalf("Status = %d, want 1 (error)", result.Status)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleOneOffQuery_UnknownColumn(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewString("alice")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x50},
		QueryString: "SELECT * FROM users WHERE bogus_col = 1",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if !bytes.Equal(result.MessageID, msg.MessageID) {
		t.Errorf("MessageID = %v, want %v", result.MessageID, msg.MessageID)
	}
	if result.Status != 1 {
		t.Fatalf("Status = %d, want 1 (error)", result.Status)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}
