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

// mockSubExecutor records RegisterSubscription / RegisterSubscriptionSet /
// UnregisterSubscriptionSet calls and implements the full ExecutorInbox
// interface with stubs for the remaining methods.
type mockSubExecutor struct {
	mu sync.Mutex
	// Legacy single-path bookkeeping (kept until Task 9 removes the
	// old RegisterSubscription/UnregisterSubscription seams).
	registerReq *RegisterSubscriptionRequest
	registerErr error
	// Set-path bookkeeping (Task 8 split).
	registerSetReq     *RegisterSubscriptionSetRequest
	registerSetErr     error
	unregisterSetReq   *UnregisterSubscriptionSetRequest
	unregisterSetErr   error
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

func (m *mockSubExecutor) RegisterSubscription(_ context.Context, req RegisterSubscriptionRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerReq = &req
	return m.registerErr
}

func (m *mockSubExecutor) UnregisterSubscription(_ context.Context, _ UnregisterSubscriptionRequest) error {
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

func (m *mockSubExecutor) getRegisterReq() *RegisterSubscriptionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerReq
}

func (m *mockSubExecutor) getRegisterSetReq() *RegisterSubscriptionSetRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerSetReq
}

func (m *mockSubExecutor) getUnregisterSetReq() *UnregisterSubscriptionSetRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unregisterSetReq
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
		RequestID: 10,
		QueryID:   7,
		Query: Query{
			TableName: "users",
			Predicates: []Predicate{
				{Column: "name", Value: types.NewString("alice")},
			},
		},
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
	if req.ResponseCh == nil {
		t.Error("ResponseCh = nil, want non-nil subscribe response channel")
	}

	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if colEq.Column != 1 {
		t.Errorf("Predicates[0].Column = %d, want 1", colEq.Column)
	}
}

func TestHandleSubscribeSingle_DeliversAsyncSubscribeApplied(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	// Reserve so SendSubscribeSingleApplied's IsPending guard passes.
	if err := conn.Subscriptions.Reserve(7); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	msg := &SubscribeSingleMsg{
		RequestID: 10,
		QueryID:   7,
		Query:     Query{TableName: "users"},
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil || req.ResponseCh == nil {
		t.Fatal("executor did not receive subscribe response channel")
	}
	req.ResponseCh <- SubscriptionSetCommandResponse{
		SingleApplied: &SubscribeSingleApplied{RequestID: 10, QueryID: 7, TableName: "users", Rows: []byte{}},
	}

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d (TagSubscribeSingleApplied)", tag, TagSubscribeSingleApplied)
	}
	applied := decoded.(SubscribeSingleApplied)
	if applied.RequestID != 10 || applied.QueryID != 7 {
		t.Fatalf("SubscribeSingleApplied = %+v", applied)
	}
}

func TestHandleSubscribeSingle_UnknownTable(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1) // only "users" exists

	msg := &SubscribeSingleMsg{
		RequestID: 5,
		QueryID:   99,
		Query:     Query{TableName: "nonexistent"},
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
		RequestID: 3,
		QueryID:   50,
		Query:     Query{TableName: "users"},
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
		Queries: []Query{
			{TableName: "users"},
			{TableName: "orders"},
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
	if req.ResponseCh == nil {
		t.Error("ResponseCh = nil, want non-nil subscribe response channel")
	}
}

func TestHandleSubscribeMulti_DeliversAsyncMultiApplied(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	msg := &SubscribeMultiMsg{
		RequestID: 12,
		QueryID:   88,
		Queries: []Query{
			{TableName: "users"},
		},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	req := exec.getRegisterSetReq()
	if req == nil || req.ResponseCh == nil {
		t.Fatal("executor did not receive subscribe response channel")
	}
	req.ResponseCh <- SubscriptionSetCommandResponse{
		MultiApplied: &SubscribeMultiApplied{RequestID: 12, QueryID: 88},
	}

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
		Queries: []Query{
			{TableName: "users"},
			{TableName: "missing"},
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
		RequestID: 14,
		QueryID:   100,
		Queries: []Query{
			{TableName: "users"},
		},
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
