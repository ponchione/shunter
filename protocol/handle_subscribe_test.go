package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type registrySchemaLookup struct{ reg schema.SchemaRegistry }

func (r registrySchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	ts, ok := r.reg.TableByName(name)
	if !ok {
		return 0, nil, false
	}
	return ts.ID, ts, true
}

// --- Test mocks ---

type mockSchemaLookup struct {
	tables map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}
}

func (m *mockSchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	entry, ok := m.tables[name]
	if !ok {
		return 0, nil, false
	}
	return entry.id, entry.schema, true
}

func newMockSchema(name string, id schema.TableID, cols ...schema.ColumnSchema) *mockSchemaLookup {
	ts := &schema.TableSchema{ID: id, Name: name, Columns: cols}
	return &mockSchemaLookup{
		tables: map[string]struct {
			id     schema.TableID
			schema *schema.TableSchema
		}{
			name: {id: id, schema: ts},
		},
	}
}

// mockSubExecutor records RegisterSubscriptionSet / UnregisterSubscriptionSet
// calls and implements the full ExecutorInbox interface with stubs for the
// remaining methods.
type mockSubExecutor struct {
	mu               sync.Mutex
	registerSetReq   *RegisterSubscriptionSetRequest
	registerSetErr   error
	unregisterSetReq *UnregisterSubscriptionSetRequest
	unregisterSetErr error
}

func (m *mockSubExecutor) OnConnect(_ context.Context, _ types.ConnectionID, _ types.Identity) error {
	return nil
}

func (m *mockSubExecutor) OnDisconnect(_ context.Context, _ types.ConnectionID, _ types.Identity) error {
	return nil
}

func (m *mockSubExecutor) DisconnectClientSubscriptions(_ context.Context, _ types.ConnectionID) error {
	return nil
}

func (m *mockSubExecutor) RegisterSubscriptionSet(_ context.Context, req RegisterSubscriptionSetRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerSetReq = &req
	return m.registerSetErr
}

func (m *mockSubExecutor) UnregisterSubscriptionSet(_ context.Context, req UnregisterSubscriptionSetRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterSetReq = &req
	return m.unregisterSetErr
}

func (m *mockSubExecutor) CallReducer(_ context.Context, _ CallReducerRequest) error {
	return nil
}

func (m *mockSubExecutor) getRegisterSetReq() *RegisterSubscriptionSetRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerSetReq
}

// --- NormalizePredicates tests ---

func TestNormalizePredicates_Empty(t *testing.T) {
	tableID := schema.TableID(1)
	ts := &schema.TableSchema{ID: tableID, Name: "users"}

	pred, err := NormalizePredicates(tableID, ts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allRows, ok := pred.(subscription.AllRows)
	if !ok {
		t.Fatalf("expected AllRows, got %T", pred)
	}
	if allRows.Table != tableID {
		t.Errorf("Table = %d, want %d", allRows.Table, tableID)
	}
}

func TestNormalizePredicates_Single(t *testing.T) {
	tableID := schema.TableID(5)
	ts := &schema.TableSchema{
		ID:   tableID,
		Name: "messages",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "channel_id", Type: schema.KindUint32},
		},
	}

	preds := []Predicate{
		{Column: "channel_id", Value: types.NewUint32(42)},
	}

	pred, err := NormalizePredicates(tableID, ts, preds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	colEq, ok := pred.(subscription.ColEq)
	if !ok {
		t.Fatalf("expected ColEq, got %T", pred)
	}
	if colEq.Table != tableID {
		t.Errorf("Table = %d, want %d", colEq.Table, tableID)
	}
	if colEq.Column != 1 {
		t.Errorf("Column = %d, want 1", colEq.Column)
	}
	if !colEq.Value.Equal(types.NewUint32(42)) {
		t.Errorf("Value mismatch: got %v", colEq.Value)
	}
}

func TestNormalizePredicates_ThreePredicates(t *testing.T) {
	tableID := schema.TableID(3)
	ts := &schema.TableSchema{
		ID:   tableID,
		Name: "events",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "a", Type: schema.KindUint32},
			{Index: 1, Name: "b", Type: schema.KindString},
			{Index: 2, Name: "c", Type: schema.KindUint32},
		},
	}

	preds := []Predicate{
		{Column: "a", Value: types.NewUint32(1)},
		{Column: "b", Value: types.NewString("hello")},
		{Column: "c", Value: types.NewUint32(99)},
	}

	pred, err := NormalizePredicates(tableID, ts, preds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected: And{And{ColEq(a), ColEq(b)}, ColEq(c)}
	outerAnd, ok := pred.(subscription.And)
	if !ok {
		t.Fatalf("expected And at top level, got %T", pred)
	}

	innerAnd, ok := outerAnd.Left.(subscription.And)
	if !ok {
		t.Fatalf("expected And on left, got %T", outerAnd.Left)
	}

	// innerAnd.Left = ColEq(a)
	colA, ok := innerAnd.Left.(subscription.ColEq)
	if !ok {
		t.Fatalf("expected ColEq for 'a', got %T", innerAnd.Left)
	}
	if colA.Column != 0 {
		t.Errorf("a: Column = %d, want 0", colA.Column)
	}

	// innerAnd.Right = ColEq(b)
	colB, ok := innerAnd.Right.(subscription.ColEq)
	if !ok {
		t.Fatalf("expected ColEq for 'b', got %T", innerAnd.Right)
	}
	if colB.Column != 1 {
		t.Errorf("b: Column = %d, want 1", colB.Column)
	}

	// outerAnd.Right = ColEq(c)
	colC, ok := outerAnd.Right.(subscription.ColEq)
	if !ok {
		t.Fatalf("expected ColEq for 'c', got %T", outerAnd.Right)
	}
	if colC.Column != 2 {
		t.Errorf("c: Column = %d, want 2", colC.Column)
	}
}

func TestNormalizePredicates_UnknownColumn(t *testing.T) {
	tableID := schema.TableID(1)
	ts := &schema.TableSchema{
		ID:   tableID,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		},
	}

	preds := []Predicate{
		{Column: "nonexistent", Value: types.NewUint32(1)},
	}

	_, err := NormalizePredicates(tableID, ts, preds)
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
	if got := err.Error(); got != `unknown column "nonexistent" on table "users"` {
		t.Errorf("error = %q, want mention of unknown column", got)
	}
}

// --- handleSubscribeSingle tests ---

func TestHandleSubscribeSingleSuccess(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   10,
		QueryID:     7,
		QueryString: "SELECT * FROM users WHERE name = 'alice'",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	// No error sent to client.
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	// Executor received the set-based request.
	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if req.ConnID != conn.ID {
		t.Errorf("ConnID mismatch")
	}
	if req.QueryID != 7 {
		t.Errorf("QueryID = %d, want 7", req.QueryID)
	}
	if req.RequestID != 10 {
		t.Errorf("RequestID = %d, want 10", req.RequestID)
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	if req.Reply == nil {
		t.Error("Reply = nil, want non-nil subscribe reply closure")
	}

	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if colEq.Column != 1 {
		t.Errorf("Predicates[0].Column = %d, want 1", colEq.Column)
	}
}

func TestHandleSubscribeSingle_QualifiedColumnsSameTable(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   11,
		QueryID:     8,
		QueryString: "SELECT * FROM users WHERE users.name = 'alice'",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if colEq.Column != 1 {
		t.Fatalf("Predicates[0].Column = %d, want 1", colEq.Column)
	}
}

func TestHandleSubscribeSingle_MixedCaseTableAndColumns(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "users",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "display_name", Type: schema.KindString},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   12,
		QueryID:     9,
		QueryString: "SELECT * FROM USERS WHERE ID = 1 AND users.DISPLAY_NAME = 'alice'",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1 query predicate", len(req.Predicates))
	}
	andPred, ok := req.Predicates[0].(subscription.And)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want And", req.Predicates[0])
	}
	first, ok := andPred.Left.(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0].Left type = %T, want ColEq", andPred.Left)
	}
	second, ok := andPred.Right.(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0].Right type = %T, want ColEq", andPred.Right)
	}
	if first.Column != 0 {
		t.Fatalf("Predicates[0].Left.Column = %d, want 0", first.Column)
	}
	if second.Column != 1 {
		t.Fatalf("Predicates[0].Right.Column = %d, want 1", second.Column)
	}
}

func TestHandleSubscribeSingle_GreaterThanComparison(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("metrics", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "score", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   14,
		QueryID:     11,
		QueryString: "SELECT * FROM metrics WHERE score > 10",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	rng, ok := req.Predicates[0].(subscription.ColRange)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColRange", req.Predicates[0])
	}
	if rng.Column != 1 {
		t.Fatalf("Predicates[0].Column = %d, want 1", rng.Column)
	}
	if rng.Lower.Unbounded || rng.Lower.Inclusive {
		t.Fatalf("lower bound = %+v, want exclusive bounded lower", rng.Lower)
	}
	if !rng.Lower.Value.Equal(types.NewUint32(10)) {
		t.Fatalf("lower bound value = %v, want 10", rng.Lower.Value)
	}
	if !rng.Upper.Unbounded {
		t.Fatalf("upper bound = %+v, want unbounded upper", rng.Upper)
	}
}

func TestHandleSubscribeSingle_NotEqualComparison(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("metrics", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "score", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   15,
		QueryID:     12,
		QueryString: "SELECT * FROM metrics WHERE score != 10",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	neq, ok := req.Predicates[0].(subscription.ColNe)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColNe", req.Predicates[0])
	}
	if neq.Column != 1 {
		t.Fatalf("Predicates[0].Column = %d, want 1", neq.Column)
	}
	if !neq.Value.Equal(types.NewUint32(10)) {
		t.Fatalf("Predicates[0].Value = %v, want 10", neq.Value)
	}
}

func TestHandleSubscribeSingle_OrComparison(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("metrics", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "score", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   16,
		QueryID:     13,
		QueryString: "SELECT * FROM metrics WHERE score = 9 OR score = 11",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	orPred, ok := req.Predicates[0].(subscription.Or)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Or", req.Predicates[0])
	}
	left, ok := orPred.Left.(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0].Left type = %T, want ColEq", orPred.Left)
	}
	right, ok := orPred.Right.(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0].Right type = %T, want ColEq", orPred.Right)
	}
	if !left.Value.Equal(types.NewUint32(9)) || !right.Value.Equal(types.NewUint32(11)) {
		t.Fatalf("unexpected OR values: left=%v right=%v", left.Value, right.Value)
	}
	if left.Column != 1 || right.Column != 1 {
		t.Fatalf("unexpected OR column ids: left=%d right=%d", left.Column, right.Column)
	}
}

func TestHandleSubscribeSingle_QualifiedStarAlias(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "users",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "name", Type: schema.KindString},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   13,
		QueryID:     10,
		QueryString: "SELECT item.* FROM users AS item WHERE item.name = 'alice'",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if colEq.Column != 1 {
		t.Fatalf("Predicates[0].Column = %d, want 1", colEq.Column)
	}
}

func TestHandleSubscribeSingle_JoinFilterOnRightTable(t *testing.T) {
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

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   17,
		QueryID:     14,
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	joinPred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	inventory, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	if joinPred.Left != orders.ID || joinPred.Right != inventory.ID {
		t.Fatalf("join tables = %d/%d, want %d/%d", joinPred.Left, joinPred.Right, orders.ID, inventory.ID)
	}
	if joinPred.LeftCol != 1 || joinPred.RightCol != 0 {
		t.Fatalf("join cols = %d/%d, want 1/0", joinPred.LeftCol, joinPred.RightCol)
	}
	rng, ok := joinPred.Filter.(subscription.ColRange)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColRange", joinPred.Filter)
	}
	if rng.Table != inventory.ID || rng.Column != 1 {
		t.Fatalf("range table/column = %d/%d, want %d/1", rng.Table, rng.Column, inventory.ID)
	}
	if !rng.Upper.Value.Equal(types.NewUint32(10)) || rng.Upper.Inclusive || rng.Upper.Unbounded {
		t.Fatalf("upper bound = %+v, want exclusive bounded 10", rng.Upper)
	}
}

func TestHandleSubscribeSingle_JoinProjectionOnRightTable(t *testing.T) {
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

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   18,
		QueryID:     15,
		QueryString: "SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	joinPred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	inventory, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	if joinPred.Left != orders.ID || joinPred.Right != inventory.ID {
		t.Fatalf("join tables = %d/%d, want %d/%d", joinPred.Left, joinPred.Right, orders.ID, inventory.ID)
	}
	if joinPred.LeftCol != 1 || joinPred.RightCol != 0 {
		t.Fatalf("join cols = %d/%d, want 1/0", joinPred.LeftCol, joinPred.RightCol)
	}
	if joinPred.Filter != nil {
		t.Fatalf("Join.Filter = %T, want nil", joinPred.Filter)
	}
}

func TestHandleSubscribeSingle_JoinProjectionOnRightTableWithLeftFilter(t *testing.T) {
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

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   19,
		QueryID:     16,
		QueryString: "SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE o.id = 1",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	joinPred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	inventory, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	if joinPred.Left != orders.ID || joinPred.Right != inventory.ID {
		t.Fatalf("join tables = %d/%d, want %d/%d", joinPred.Left, joinPred.Right, orders.ID, inventory.ID)
	}
	colEq, ok := joinPred.Filter.(subscription.ColEq)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColEq", joinPred.Filter)
	}
	if colEq.Table != orders.ID || colEq.Column != 0 {
		t.Fatalf("filter table/column = %d/%d, want %d/0", colEq.Table, colEq.Column, orders.ID)
	}
	if !colEq.Value.Equal(types.NewUint32(1)) {
		t.Fatalf("filter value = %v, want 1", colEq.Value)
	}
}

func TestHandleSubscribeSingle_CrossJoinProjection(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "Inventory",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{RequestID: 21, QueryID: 18, QueryString: "SELECT o.* FROM Orders o JOIN Inventory product"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}
	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	pred, ok := req.Predicates[0].(subscription.CrossJoinProjected)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want CrossJoinProjected", req.Predicates[0])
	}
	orders, _ := eng.Registry().TableByName("Orders")
	inventory, _ := eng.Registry().TableByName("Inventory")
	if pred.Projected != orders.ID || pred.Other != inventory.ID {
		t.Fatalf("cross join predicate = %+v, want projected Orders other Inventory", pred)
	}
}

func TestHandleSubscribeSingle_DeliversSubscribeAppliedViaReplyClosure(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   10,
		QueryID:     7,
		QueryString: "SELECT * FROM users",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil || req.Reply == nil {
		t.Fatal("executor did not receive subscribe reply closure")
	}
	req.Reply(SubscriptionSetCommandResponse{
		SingleApplied: &SubscribeSingleApplied{RequestID: 10, QueryID: 7, TableName: "users", Rows: []byte{}},
	})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d (TagSubscribeSingleApplied)", tag, TagSubscribeSingleApplied)
	}
	applied := decoded.(SubscribeSingleApplied)
	if applied.RequestID != 10 || applied.QueryID != 7 {
		t.Fatalf("SubscribeSingleApplied = %+v", applied)
	}
}

func TestHandleSubscribeSingle_AliasedBaseTableQualifiedWhereRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   20,
		QueryID:     17,
		QueryString: "SELECT item.* FROM users AS item WHERE users.id = 1",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.QueryID != 17 {
		t.Fatalf("SubscriptionError.QueryID = %d, want 17", se.QueryID)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor should not be called for aliased base-table qualified WHERE")
	}
}

func TestHandleSubscribeSingle_AliasedSelfCrossJoin(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{RequestID: 23, QueryID: 24, QueryString: "SELECT a.* FROM t AS a JOIN t AS b"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}
	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	pred, ok := req.Predicates[0].(subscription.CrossJoinProjected)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want CrossJoinProjected", req.Predicates[0])
	}
	tTable, _ := eng.Registry().TableByName("t")
	if pred.Projected != tTable.ID || pred.Other != tTable.ID {
		t.Fatalf("self cross join predicate = %+v, want projected/other both t", pred)
	}
}

func TestHandleSubscribeSingle_AliasedSelfEquiJoin(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{RequestID: 30, QueryID: 31, QueryString: "SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}
	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	pred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	tTable, _ := eng.Registry().TableByName("t")
	if pred.Left != tTable.ID || pred.Right != tTable.ID {
		t.Fatalf("self equi-join predicate = %+v, want Left/Right both t", pred)
	}
	if pred.LeftAlias == pred.RightAlias {
		t.Fatalf("self-join aliases must differ, got Left=%d Right=%d", pred.LeftAlias, pred.RightAlias)
	}
	if pred.Filter != nil {
		t.Fatalf("Filter = %+v, want nil", pred.Filter)
	}
}

func TestHandleSubscribeSingle_AliasedSelfEquiJoinWithWhere(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{RequestID: 32, QueryID: 33, QueryString: "SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}
	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	pred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	tTable, _ := eng.Registry().TableByName("t")
	if pred.Left != tTable.ID || pred.Right != tTable.ID {
		t.Fatalf("self equi-join predicate = %+v, want Left/Right both t", pred)
	}
	if pred.LeftAlias == pred.RightAlias {
		t.Fatalf("self-join aliases must differ, got Left=%d Right=%d", pred.LeftAlias, pred.RightAlias)
	}
	filter, ok := pred.Filter.(subscription.ColEq)
	if !ok {
		t.Fatalf("Filter type = %T, want ColEq", pred.Filter)
	}
	if filter.Table != tTable.ID || filter.Column != 0 {
		t.Fatalf("Filter target = %+v, want table=t column=id", filter)
	}
	if filter.Alias != pred.LeftAlias {
		t.Fatalf("Filter.Alias = %d, want %d (a-side = LeftAlias)", filter.Alias, pred.LeftAlias)
	}
	if !filter.Value.Equal(types.NewUint32(1)) {
		t.Fatalf("Filter.Value = %v, want uint32(1)", filter.Value)
	}
}

func TestHandleSubscribeSingle_UnaliasedSelfCrossJoinRejected(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{RequestID: 22, QueryID: 19, QueryString: "SELECT t.* FROM t JOIN t"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.QueryID != 19 {
		t.Fatalf("SubscriptionError.QueryID = %d, want 19", se.QueryID)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor should not be called for unaliased self cross join")
	}
}

func TestHandleSubscribeSingle_UnknownTable(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1) // only "users" exists

	msg := &SubscribeSingleMsg{
		RequestID:   5,
		QueryID:     99,
		QueryString: "SELECT * FROM nonexistent",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.QueryID != 99 {
		t.Errorf("SubscriptionError.QueryID = %d, want 99", se.QueryID)
	}

	// Executor must not have been called.
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called for unknown table")
	}
}

func TestHandleSubscribeSingle_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{
		registerSetErr: errors.New("queue full"),
	}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   3,
		QueryID:     50,
		QueryString: "SELECT * FROM users",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.QueryID != 50 {
		t.Errorf("SubscriptionError.QueryID = %d, want 50", se.QueryID)
	}
	if se.RequestID != 3 {
		t.Errorf("SubscriptionError.RequestID = %d, want 3", se.RequestID)
	}
}

// --- handleSubscribeMulti tests ---

func TestHandleSubscribeMultiSuccess(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := &mockSchemaLookup{
		tables: map[string]struct {
			id     schema.TableID
			schema *schema.TableSchema
		}{
			"users":  {id: 1, schema: &schema.TableSchema{ID: 1, Name: "users"}},
			"orders": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "orders"}},
		},
	}

	msg := &SubscribeMultiMsg{
		RequestID: 11,
		QueryID:   77,
		QueryStrings: []string{
			"SELECT * FROM users",
			"SELECT * FROM orders",
		},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	req := exec.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if req.QueryID != 77 || len(req.Predicates) != 2 {
		t.Fatalf("req = %+v, want QueryID=77 len(Predicates)=2", req)
	}
	if req.RequestID != 11 {
		t.Errorf("RequestID = %d, want 11", req.RequestID)
	}
	if req.Reply == nil {
		t.Error("Reply = nil, want non-nil subscribe reply closure")
	}
}

func TestHandleSubscribeMulti_DeliversMultiAppliedViaReplyClosure(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	msg := &SubscribeMultiMsg{
		RequestID:    12,
		QueryID:      88,
		QueryStrings: []string{"SELECT * FROM users"},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	req := exec.getRegisterSetReq()
	if req == nil || req.Reply == nil {
		t.Fatal("executor did not receive subscribe reply closure")
	}
	req.Reply(SubscriptionSetCommandResponse{
		MultiApplied: &SubscribeMultiApplied{RequestID: 12, QueryID: 88},
	})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscribeMultiApplied {
		t.Fatalf("tag = %d, want %d (TagSubscribeMultiApplied)", tag, TagSubscribeMultiApplied)
	}
	applied := decoded.(SubscribeMultiApplied)
	if applied.RequestID != 12 || applied.QueryID != 88 {
		t.Fatalf("SubscribeMultiApplied = %+v", applied)
	}
}

func TestHandleSubscribeMulti_UnknownTable(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("users", 1)

	msg := &SubscribeMultiMsg{
		RequestID: 13,
		QueryID:   99,
		QueryStrings: []string{
			"SELECT * FROM users",
			"SELECT * FROM missing",
		},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.QueryID != 99 {
		t.Errorf("QueryID = %d, want 99", se.QueryID)
	}
	if req := exec.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when any query is invalid")
	}
}

func TestHandleSubscribeMulti_ExecutorReject(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{registerSetErr: errors.New("queue full")}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	msg := &SubscribeMultiMsg{
		RequestID:    14,
		QueryID:      100,
		QueryStrings: []string{"SELECT * FROM users"},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.QueryID != 100 {
		t.Errorf("QueryID = %d, want 100", se.QueryID)
	}
	if se.RequestID != 14 {
		t.Errorf("RequestID = %d, want 14", se.RequestID)
	}
}
