package protocol

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type registrySchemaLookup struct{ reg schema.SchemaRegistry }

func (r registrySchemaLookup) Table(id schema.TableID) (*schema.TableSchema, bool) {
	return r.reg.Table(id)
}

func (r registrySchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	return r.reg.TableByName(name)
}

func (r registrySchemaLookup) TableExists(id schema.TableID) bool {
	return r.reg.TableExists(id)
}

func (r registrySchemaLookup) TableName(id schema.TableID) string {
	return r.reg.TableName(id)
}

func (r registrySchemaLookup) ColumnExists(table schema.TableID, col types.ColID) bool {
	return r.reg.ColumnExists(table, col)
}

func (r registrySchemaLookup) ColumnType(table schema.TableID, col types.ColID) schema.ValueKind {
	return r.reg.ColumnType(table, col)
}

func (r registrySchemaLookup) HasIndex(table schema.TableID, col types.ColID) bool {
	return r.reg.HasIndex(table, col)
}

func (r registrySchemaLookup) ColumnCount(table schema.TableID) int {
	return r.reg.ColumnCount(table)
}

func requireOptionalUint32(t *testing.T, got *uint32, want uint32, field string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", field, got, want)
	}
}

// --- Test mocks ---

type mockSchemaLookup struct {
	tables map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}
}

func (m *mockSchemaLookup) Table(id schema.TableID) (*schema.TableSchema, bool) {
	for _, entry := range m.tables {
		if entry.id == id {
			return entry.schema, true
		}
	}
	return nil, false
}

func (m *mockSchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	entry, ok := m.tables[name]
	if !ok {
		return 0, nil, false
	}
	return entry.id, entry.schema, true
}

func (m *mockSchemaLookup) TableExists(id schema.TableID) bool {
	_, ok := m.Table(id)
	return ok
}

func (m *mockSchemaLookup) TableName(id schema.TableID) string {
	for name, entry := range m.tables {
		if entry.id == id {
			return name
		}
	}
	return ""
}

func (m *mockSchemaLookup) ColumnExists(table schema.TableID, col types.ColID) bool {
	ts, ok := m.Table(table)
	if !ok {
		return false
	}
	return int(col) >= 0 && int(col) < len(ts.Columns)
}

func (m *mockSchemaLookup) ColumnType(table schema.TableID, col types.ColID) schema.ValueKind {
	ts, ok := m.Table(table)
	if !ok || int(col) < 0 || int(col) >= len(ts.Columns) {
		return 0
	}
	return ts.Columns[col].Type
}

func (m *mockSchemaLookup) HasIndex(table schema.TableID, col types.ColID) bool {
	ts, ok := m.Table(table)
	if !ok {
		return false
	}
	for _, idx := range ts.Indexes {
		if len(idx.Columns) == 1 && idx.Columns[0] == int(col) {
			return true
		}
	}
	return false
}

func (m *mockSchemaLookup) ColumnCount(table schema.TableID) int {
	ts, ok := m.Table(table)
	if !ok {
		return 0
	}
	return len(ts.Columns)
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

type validatingSubExecutor struct {
	mockSubExecutor
	schema subscription.SchemaLookup
}

func (v *validatingSubExecutor) RegisterSubscriptionSet(ctx context.Context, req RegisterSubscriptionSetRequest) error {
	v.mockSubExecutor.RegisterSubscriptionSet(ctx, req)
	for _, pred := range req.Predicates {
		p, ok := pred.(subscription.Predicate)
		if !ok {
			req.Reply(SubscriptionSetCommandResponse{Error: &SubscriptionError{
				RequestID: optionalUint32(req.RequestID),
				QueryID:   optionalUint32(req.QueryID),
				Error:     "invalid predicate request",
			}})
			return nil
		}
		if err := subscription.ValidatePredicate(p, v.schema); err != nil {
			req.Reply(SubscriptionSetCommandResponse{Error: &SubscriptionError{
				RequestID: optionalUint32(req.RequestID),
				QueryID:   optionalUint32(req.QueryID),
				Error:     err.Error(),
			}})
			return nil
		}
	}
	return nil
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

func TestHandleSubscribeSingle_MixedCaseTableRejectedByExactSQLPolicy(t *testing.T) {
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

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `USERS`. If the table exists, it may be marked private., executing: `SELECT * FROM USERS WHERE ID = 1 AND users.DISPLAY_NAME = 'alice'`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor should not receive RegisterSubscriptionSet for case-mismatched table")
	}
}

func TestHandleSubscribeSingle_AmbiguousCaseFoldedTableNameRejectedBeforeRegistration(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "Users",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "users",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
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
		QueryString: "SELECT * FROM USERS WHERE id = 1",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `USERS`. If the table exists, it may be marked private., executing: `SELECT * FROM USERS WHERE id = 1`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called for ambiguous case-folded table names")
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

func TestHandleSubscribeSingle_OrComparisonWithAliasAndHexBytes(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   17,
		QueryID:     14,
		QueryString: "SELECT * FROM s AS r WHERE r.bytes = 0xABCD OR bytes = X'ABCD'",
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
	want := types.NewBytes([]byte{0xAB, 0xCD})
	if left.Column != 1 || right.Column != 1 {
		t.Fatalf("unexpected OR column ids: left=%d right=%d", left.Column, right.Column)
	}
	if !left.Value.Equal(want) || !right.Value.Equal(want) {
		t.Fatalf("unexpected OR values: left=%v right=%v want=%v", left.Value, right.Value, want)
	}
}

func TestHandleSubscribeSingle_LowercaseXEscapedStringOnBytesRejectedWithSQL(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)
	msg := &SubscribeSingleMsg{
		RequestID:   18,
		QueryID:     15,
		QueryString: "SELECT * FROM s WHERE bytes = 'x''AB'",
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "The literal expression `x'AB` cannot be parsed as type `Array<U8>`, executing: `SELECT * FROM s WHERE bytes = 'x''AB'`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when lowercase x string content rejects as Array<U8>")
	}
}

func TestHandleSubscribeSingle_OrComparisonWithAlias(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   17,
		QueryID:     14,
		QueryString: "SELECT item.* FROM users AS item WHERE item.id = 1 OR name = 'alice'",
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
	if left.Column != 0 || right.Column != 1 {
		t.Fatalf("unexpected OR column ids: left=%d right=%d", left.Column, right.Column)
	}
	if !left.Value.Equal(types.NewUint32(1)) {
		t.Fatalf("left value = %v, want 1", left.Value)
	}
	if !right.Value.Equal(types.NewString("alice")) {
		t.Fatalf("right value = %v, want alice", right.Value)
	}
}

func TestHandleSubscribeSingle_WhereTrueLiteralCompilesToAllRows(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "flag", Type: schema.KindBool},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   18,
		QueryID:     15,
		QueryString: "SELECT * FROM t WHERE TRUE",
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
	allRows, ok := req.Predicates[0].(subscription.AllRows)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want AllRows", req.Predicates[0])
	}
	if allRows.Table != 1 {
		t.Fatalf("AllRows.Table = %d, want 1", allRows.Table)
	}
}

func TestHandleSubscribeSingle_TrueAndComparisonNormalizesToComparison(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "flag", Type: schema.KindBool},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   118,
		QueryID:     115,
		QueryString: "SELECT * FROM t WHERE TRUE AND id = 7",
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
	if colEq.Table != 1 || colEq.Column != 0 {
		t.Fatalf("predicate target = table %d col %d, want table 1 col 0", colEq.Table, colEq.Column)
	}
	if !colEq.Value.Equal(types.NewUint32(7)) {
		t.Fatalf("predicate value = %v, want 7", colEq.Value)
	}
}

func TestHandleSubscribeSingle_TrueOrComparisonNormalizesToAllRows(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "flag", Type: schema.KindBool},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   119,
		QueryID:     116,
		QueryString: "SELECT * FROM t WHERE TRUE OR id = 7",
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
	allRows, ok := req.Predicates[0].(subscription.AllRows)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want AllRows", req.Predicates[0])
	}
	if allRows.Table != 1 {
		t.Fatalf("AllRows.Table = %d, want 1", allRows.Table)
	}
}

func TestHandleSubscribeSingle_SQLWhereFalseCompilesToNoRows(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "flag", Type: schema.KindBool},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   120,
		QueryID:     117,
		QueryString: "SELECT * FROM t WHERE FALSE",
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
	noRows, ok := req.Predicates[0].(subscription.NoRows)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want NoRows", req.Predicates[0])
	}
	if noRows.Table != 1 {
		t.Fatalf("NoRows.Table = %d, want 1", noRows.Table)
	}
}

func TestHandleSubscribeSingle_SQLWhereFalseOrComparisonNormalizesToComparison(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "flag", Type: schema.KindBool},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   121,
		QueryID:     118,
		QueryString: "SELECT * FROM t WHERE FALSE OR id = 7",
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
	if colEq.Table != 1 || colEq.Column != 0 {
		t.Fatalf("predicate target = table %d col %d, want table 1 col 0", colEq.Table, colEq.Column)
	}
	if !colEq.Value.Equal(types.NewUint32(7)) {
		t.Fatalf("predicate value = %v, want 7", colEq.Value)
	}
}

func TestHandleSubscribeSingle_SQLWhereFalseAndComparisonCompilesToNoRows(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "flag", Type: schema.KindBool},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   122,
		QueryID:     119,
		QueryString: "SELECT * FROM t WHERE FALSE AND id = 7",
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
	noRows, ok := req.Predicates[0].(subscription.NoRows)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want NoRows", req.Predicates[0])
	}
	if noRows.Table != 1 {
		t.Fatalf("NoRows.Table = %d, want 1", noRows.Table)
	}
}

func TestHandleSubscribeSingle_CrossJoinWhereFalseStillRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "Orders",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "Inventory",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}

	const sqlText = "SELECT Orders.* FROM Orders JOIN Inventory WHERE FALSE"
	msg := &SubscribeSingleMsg{
		RequestID:   123,
		QueryID:     120,
		QueryString: sqlText,
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, registrySchemaLookup{reg: eng.Registry()})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "cross join WHERE not supported, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection", req)
	}
}

func TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityStillRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "u32", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "u32", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}

	const sqlText = "SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32"
	msg := &SubscribeSingleMsg{
		RequestID:   124,
		QueryID:     121,
		QueryString: sqlText,
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, registrySchemaLookup{reg: eng.Registry()})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "cross join WHERE not supported, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection", req)
	}
}

func TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityAndLiteralFilterStillRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "u32", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "u32", Type: schema.KindUint32},
			{Name: "enabled", Type: schema.KindBool},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}

	const sqlText = "SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE"
	msg := &SubscribeSingleMsg{
		RequestID:   125,
		QueryID:     122,
		QueryString: sqlText,
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, registrySchemaLookup{reg: eng.Registry()})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "cross join WHERE not supported, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection", req)
	}
}

// TestHandleSubscribeSingle_JoinCountAggregateOnCrossJoinWhereStillRejected
// pins that even after one-off admits the bounded combination of cross-join
// WHERE column-equality (+ optional one-column-literal filter) with
// COUNT(*) [AS] alias aggregate, subscriptions still deliberately reject
// both the cross-join WHERE shape and the aggregate projection before
// executor registration. Reference subscription SQL grammar allows only
// STAR / ident.STAR projection and has no cross-join WHERE form; the
// subscribe-side rejection is the outer guard. The rejection message is
// "cross join WHERE not supported" because the cross-join guard fires
// before the aggregate-projection guard on subscribe.
func TestHandleSubscribeSingle_JoinCountAggregateOnCrossJoinWhereStillRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "t_id", Type: schema.KindUint32},
			{Name: "active", Type: schema.KindBool},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}

	const sqlText = "SELECT COUNT(*) AS n FROM t JOIN s WHERE t.id = s.t_id AND s.active = TRUE"
	msg := &SubscribeSingleMsg{
		RequestID:   126,
		QueryID:     123,
		QueryString: sqlText,
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, registrySchemaLookup{reg: eng.Registry()})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Column projections are not supported in subscriptions; Subscriptions must return a table type, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q (aggregate guard fires before cross-join WHERE guard)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection", req)
	}
}

func TestHandleSubscribeSingle_QuotedSpecialCharacterIdentifiers(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("Balance$", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "status", Type: schema.KindString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   21,
		QueryID:     18,
		QueryString: `SELECT * FROM "Balance$" WHERE "id" = 7`,
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
	if colEq.Table != 1 || colEq.Column != 0 {
		t.Fatalf("predicate target = table %d col %d, want table 1 col 0", colEq.Table, colEq.Column)
	}
	if !colEq.Value.Equal(types.NewUint32(7)) {
		t.Fatalf("predicate value = %v, want 7", colEq.Value)
	}
}

func TestHandleSubscribeSingle_QuotedReservedIdentifiers(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("Order", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "status", Type: schema.KindString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   19,
		QueryID:     16,
		QueryString: `SELECT * FROM "Order" WHERE "id" = 7`,
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
	if colEq.Table != 1 || colEq.Column != 0 {
		t.Fatalf("predicate target = table %d col %d, want table 1 col 0", colEq.Table, colEq.Column)
	}
	if !colEq.Value.Equal(types.NewUint32(7)) {
		t.Fatalf("predicate value = %v, want 7", colEq.Value)
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

func TestHandleSubscribeSingle_JoinFilterOnLeftFloatColumn(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
			{Name: "f32", Type: schema.KindFloat32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, ok := eng.Registry().TableByName("t")
	if !ok {
		t.Fatal("registry missing table t")
	}

	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   20,
		QueryID:     17,
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.f32 = 0.1",
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
	colEq, ok := joinPred.Filter.(subscription.ColEq)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColEq", joinPred.Filter)
	}
	want, err := types.NewFloat32(0.1)
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	if colEq.Table != tReg.ID || colEq.Column != 2 {
		t.Fatalf("filter target = table %d col %d, want table %d col 2", colEq.Table, colEq.Column, tReg.ID)
	}
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want %v", colEq.Value, want)
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
	_, orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventory, ok := eng.Registry().TableByName("Inventory")
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

func TestHandleSubscribeSingle_JoinFilterTrueAndComparisonNormalizesFilter(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_orders_product_id", Columns: []string{"product_id"}}},
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
		RequestID:   120,
		QueryID:     117,
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE TRUE AND product.quantity < 10",
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
	rng, ok := joinPred.Filter.(subscription.ColRange)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColRange", joinPred.Filter)
	}
	if !rng.Upper.Value.Equal(types.NewUint32(10)) || rng.Upper.Inclusive || rng.Upper.Unbounded {
		t.Fatalf("upper bound = %+v, want exclusive bounded 10", rng.Upper)
	}
}

func TestHandleSubscribeSingle_QuotedIdentifiersJoinFilterOnRightTable(t *testing.T) {
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
		RequestID:   117,
		QueryID:     114,
		QueryString: `SELECT "Orders".* FROM "Orders" JOIN "Inventory" ON "Orders"."product_id" = "Inventory"."id" WHERE "Inventory"."quantity" < 10`,
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
	_, orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventory, ok := eng.Registry().TableByName("Inventory")
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

func TestHandleSubscribeSingle_QuotedIdentifiersJoinFilterWithParenthesizedConjunction(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "users",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "other",
		Columns: []schema.ColumnDefinition{
			{Name: "uid", Type: schema.KindUint32, PrimaryKey: true},
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
		RequestID:   118,
		QueryID:     115,
		QueryString: `SELECT "users".* FROM "users" JOIN "other" ON "users"."id" = "other"."uid" WHERE (("users"."id" = 1) AND ("users"."id" > 0))`,
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
	_, users, ok := eng.Registry().TableByName("users")
	if !ok {
		t.Fatal("users table missing from registry")
	}
	_, other, ok := eng.Registry().TableByName("other")
	if !ok {
		t.Fatal("other table missing from registry")
	}
	if joinPred.Left != users.ID || joinPred.Right != other.ID {
		t.Fatalf("join tables = %d/%d, want %d/%d", joinPred.Left, joinPred.Right, users.ID, other.ID)
	}
	andPred, ok := joinPred.Filter.(subscription.And)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want And", joinPred.Filter)
	}
	left, ok := andPred.Left.(subscription.ColEq)
	if !ok {
		t.Fatalf("Join.Filter.Left type = %T, want ColEq", andPred.Left)
	}
	right, ok := andPred.Right.(subscription.ColRange)
	if !ok {
		t.Fatalf("Join.Filter.Right type = %T, want ColRange", andPred.Right)
	}
	if left.Table != users.ID || left.Column != 0 || !left.Value.Equal(types.NewUint32(1)) {
		t.Fatalf("left predicate = %+v, want users.id = 1", left)
	}
	if right.Table != users.ID || right.Column != 0 {
		t.Fatalf("right predicate table/column = %d/%d, want %d/0", right.Table, right.Column, users.ID)
	}
	if right.Lower.Unbounded || !right.Lower.Value.Equal(types.NewUint32(0)) || right.Lower.Inclusive {
		t.Fatalf("lower bound = %+v, want exclusive bounded 0", right.Lower)
	}
	if !right.Upper.Unbounded {
		t.Fatalf("upper bound = %+v, want unbounded", right.Upper)
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
	_, orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventory, ok := eng.Registry().TableByName("Inventory")
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
	// TD-142 Slice 14: SELECT on the RHS alias must thread ProjectRight=true
	// so the runtime emits RHS-shape rows.
	if !joinPred.ProjectRight {
		t.Fatal("Join.ProjectRight = false, want true for SELECT product.*")
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
	_, orders, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventory, ok := eng.Registry().TableByName("Inventory")
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
		Name:    "Orders",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "Inventory",
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
	pred, ok := req.Predicates[0].(subscription.CrossJoin)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want CrossJoin", req.Predicates[0])
	}
	_, orders, _ := eng.Registry().TableByName("Orders")
	_, inventory, _ := eng.Registry().TableByName("Inventory")
	if pred.Left != orders.ID || pred.Right != inventory.ID || pred.ProjectRight {
		t.Fatalf("cross join predicate = %+v, want Left=Orders Right=Inventory ProjectRight=false", pred)
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
	requireOptionalUint32(t, se.QueryID, 17, "SubscriptionError.QueryID")
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
	pred, ok := req.Predicates[0].(subscription.CrossJoin)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want CrossJoin", req.Predicates[0])
	}
	_, tTable, _ := eng.Registry().TableByName("t")
	if pred.Left != tTable.ID || pred.Right != tTable.ID {
		t.Fatalf("self cross join predicate = %+v, want Left/Right both t", pred)
	}
	if pred.LeftAlias == pred.RightAlias {
		t.Fatalf("self cross join aliases must differ, got Left=%d Right=%d", pred.LeftAlias, pred.RightAlias)
	}
	if pred.ProjectRight {
		t.Fatal("SELECT a.* must compile to ProjectRight=false on self-cross-join")
	}
}

func TestHandleSubscribeSingle_AliasedSelfCrossJoinProjectsRight(t *testing.T) {
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
	msg := &SubscribeSingleMsg{RequestID: 25, QueryID: 26, QueryString: "SELECT b.* FROM t AS a JOIN t AS b"}
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
	pred, ok := req.Predicates[0].(subscription.CrossJoin)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want CrossJoin", req.Predicates[0])
	}
	if !pred.ProjectRight {
		t.Fatal("SELECT b.* must compile to ProjectRight=true on self-cross-join")
	}
}

func TestHandleSubscribeSingle_AliasedSelfEquiJoin(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
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
	_, tTable, _ := eng.Registry().TableByName("t")
	if pred.Left != tTable.ID || pred.Right != tTable.ID {
		t.Fatalf("self equi-join predicate = %+v, want Left/Right both t", pred)
	}
	if pred.LeftAlias == pred.RightAlias {
		t.Fatalf("self-join aliases must differ, got Left=%d Right=%d", pred.LeftAlias, pred.RightAlias)
	}
	if pred.Filter != nil {
		t.Fatalf("Filter = %+v, want nil", pred.Filter)
	}
	if pred.ProjectRight {
		t.Fatal("SELECT a.* must compile to ProjectRight=false (LHS side)")
	}
}

func TestHandleSubscribeSingle_CaseDistinctRelationAliasesRouteJoinSides(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_s_u32", Columns: []string{"u32"}}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{
		RequestID:   34,
		QueryID:     35,
		QueryString: `SELECT "R".* FROM t AS "R" JOIN s AS r ON "R".u32 = r.u32`,
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
	pred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	tID, _, _ := eng.Registry().TableByName("t")
	sID, _, _ := eng.Registry().TableByName("s")
	if pred.Left != tID || pred.Right != sID {
		t.Fatalf("join sides = %d/%d, want %d/%d", pred.Left, pred.Right, tID, sID)
	}
	if pred.ProjectRight {
		t.Fatal(`SELECT "R".* must compile to ProjectRight=false`)
	}
}

// TD-142 Slice 14: self-join `SELECT b.*` threads ProjectRight=true so the
// runtime emits rows shaped like the b-side instance. The parser-side
// ProjectedAlias="b" drives this decision; the physical table is the same on
// both sides so the alias is the only signal.
func TestHandleSubscribeSingle_AliasedSelfEquiJoinProjectsRight(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}
	msg := &SubscribeSingleMsg{RequestID: 60, QueryID: 61, QueryString: "SELECT b.* FROM t AS a JOIN t AS b ON a.u32 = b.u32"}
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
	pred, ok := req.Predicates[0].(subscription.Join)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want Join", req.Predicates[0])
	}
	if !pred.ProjectRight {
		t.Fatal("SELECT b.* must compile to ProjectRight=true on self-join")
	}
}

func TestHandleSubscribeSingle_AliasedSelfEquiJoinWithWhere(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
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
	_, tTable, _ := eng.Registry().TableByName("t")
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
	msg := &SubscribeSingleMsg{RequestID: 22, QueryID: 19, QueryString: "SELECT t.* FROM t JOIN t"}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 19, "SubscriptionError.QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatal("executor should not be called for unaliased self cross join")
	}
}

// TestHandleSubscribeSingle_MultiWayJoinRejected pins the reference-matched
// rejection of three-way join shapes at the subscribe admission boundary.
// Externally the client receives a SubscriptionError; internally the parser
// short-circuits before admission. Reference subscription runtime bails with
// "Invalid number of tables in subscription: {N}" at
// reference/SpacetimeDB/crates/subscription/src/lib.rs:251.
func TestHandleSubscribeSingle_MultiWayJoinRejected(t *testing.T) {
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
			executor := &mockSubExecutor{}
			sl := registrySchemaLookup{reg: eng.Registry()}
			msg := &SubscribeSingleMsg{RequestID: 70, QueryID: 71, QueryString: c.queryString}
			handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
			tag, decoded := drainServerMsgEventually(t, conn)
			if tag != TagSubscriptionError {
				t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
			}
			se := decoded.(SubscriptionError)
			requireOptionalUint32(t, se.QueryID, 71, "SubscriptionError.QueryID")
			if req := executor.getRegisterSetReq(); req != nil {
				t.Fatal("executor should not be called for multi-way join")
			}
		})
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
	requireOptionalUint32(t, se.QueryID, 99, "SubscriptionError.QueryID")

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
	requireOptionalUint32(t, se.QueryID, 50, "SubscriptionError.QueryID")
	requireOptionalUint32(t, se.RequestID, 3, "SubscriptionError.RequestID")
}

func TestHandleSubscribeSingle_UnindexedJoinRejectedAtCompileStage(t *testing.T) {
	conn := testConnDirect(nil)
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
			{Name: "id", Type: schema.KindUint32},
			{Name: "quantity", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}
	executor := &mockSubExecutor{}

	const sqlText = "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id"
	msg := &SubscribeSingleMsg{
		RequestID:   4,
		QueryID:     51,
		QueryString: sqlText,
	}

	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 51, "SubscriptionError.QueryID")
	requireOptionalUint32(t, se.RequestID, 4, "SubscriptionError.RequestID")
	want := "Subscriptions require indexes on join columns, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile-stage rejection", req)
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
	requireOptionalUint32(t, se.QueryID, 99, "QueryID")
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
	requireOptionalUint32(t, se.QueryID, 100, "QueryID")
	requireOptionalUint32(t, se.RequestID, 14, "RequestID")
}

// Reference expr type-check coverage accepts `:sender` as the caller-identity
// parameter on both identity-typed columns and byte-array columns
// (`crates/expr/src/check.rs` lines 434-440). Pin the subscribe-single path
// end-to-end: the compiled predicate must carry the caller's 32-byte identity
// payload materialized as KindBytes so the evaluator can match it against the
// row column without any wire-level parameter substitution.
func TestHandleSubscribeSingle_SenderParameterOnIdentityColumn(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{1, 2, 3, 4}
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindBytes},
		schema.ColumnSchema{Index: 1, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   25,
		QueryID:     40,
		QueryString: "SELECT * FROM s WHERE id = :sender",
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
	if colEq.Table != 1 || colEq.Column != 0 {
		t.Fatalf("predicate target = table %d col %d, want table 1 col 0", colEq.Table, colEq.Column)
	}
	want := types.NewBytes(conn.Identity[:])
	if !colEq.Value.Equal(want) {
		t.Fatalf("predicate value = %v, want caller identity bytes", colEq.Value)
	}
}

func TestHandleSubscribeSingle_SenderParameterOnBytesColumn(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{9, 9, 9, 9}
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   26,
		QueryID:     41,
		QueryString: "SELECT * FROM s WHERE bytes = :sender",
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
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if colEq.Column != 1 {
		t.Fatalf("predicate column = %d, want 1", colEq.Column)
	}
	want := types.NewBytes(conn.Identity[:])
	if !colEq.Value.Equal(want) {
		t.Fatalf("predicate value = %v, want caller identity bytes", colEq.Value)
	}
}

func TestHandleSubscribeSingle_SenderParameterCarriesHashIdentity(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{9, 8, 7, 6}
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   126,
		QueryID:     141,
		QueryString: "SELECT * FROM s WHERE bytes = :sender",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	if len(req.PredicateHashIdentities) != 1 {
		t.Fatalf("len(PredicateHashIdentities) = %d, want 1", len(req.PredicateHashIdentities))
	}
	if req.PredicateHashIdentities[0] == nil {
		t.Fatal("PredicateHashIdentities[0] = nil, want conn.Identity")
	}
	if *req.PredicateHashIdentities[0] != conn.Identity {
		t.Fatalf("PredicateHashIdentities[0] = %x, want %x", *req.PredicateHashIdentities[0], conn.Identity)
	}
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	want := types.NewBytes(conn.Identity[:])
	if !colEq.Value.Equal(want) {
		t.Fatalf("predicate value = %v, want caller identity bytes", colEq.Value)
	}
}

func TestHandleSubscribeSingle_LiteralBytesDoesNotCarryHashIdentity(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{5, 4, 3, 2}
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   127,
		QueryID:     142,
		QueryString: "SELECT * FROM s WHERE bytes = 0x0102",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.PredicateHashIdentities) != 1 {
		t.Fatalf("len(PredicateHashIdentities) = %d, want 1", len(req.PredicateHashIdentities))
	}
	if req.PredicateHashIdentities[0] != nil {
		t.Fatalf("PredicateHashIdentities[0] = %x, want nil for literal bytes", *req.PredicateHashIdentities[0])
	}
}

func TestHandleSubscribeMulti_MixedLiteralAndSenderParameterCarriesPerPredicateHashIdentity(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{1, 3, 5, 7}
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeMultiMsg{
		RequestID: 211,
		QueryID:   212,
		QueryStrings: []string{
			"SELECT * FROM s WHERE u32 = 7",
			"SELECT * FROM s WHERE bytes = :sender",
		},
	}
	handleSubscribeMulti(context.Background(), conn, msg, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet call")
	}
	if len(req.Predicates) != 2 {
		t.Fatalf("len(Predicates) = %d, want 2", len(req.Predicates))
	}
	if len(req.PredicateHashIdentities) != 2 {
		t.Fatalf("len(PredicateHashIdentities) = %d, want 2", len(req.PredicateHashIdentities))
	}
	if req.PredicateHashIdentities[0] != nil {
		t.Fatalf("PredicateHashIdentities[0] = %x, want nil for literal predicate", *req.PredicateHashIdentities[0])
	}
	if req.PredicateHashIdentities[1] == nil {
		t.Fatal("PredicateHashIdentities[1] = nil, want conn.Identity")
	}
	if *req.PredicateHashIdentities[1] != conn.Identity {
		t.Fatalf("PredicateHashIdentities[1] = %x, want %x", *req.PredicateHashIdentities[1], conn.Identity)
	}
}

// TestHandleSubscribeSingle_ParitySenderResolvesToHexOnStringColumn pins
// reference resolve_sender → lib.rs:353 onto the SubscribeSingle admission
// surface. The compiled predicate must carry the caller hex string as the
// equality target on a `KindString` column so the executor receives a
// well-formed ColEq predicate (no protocol-level rejection). The earlier
// rejection assertion was based on a misread of check.rs:487-488; that
// reject case is `arr = :sender` (Array<String>), not String.
func TestHandleSubscribeSingle_ParitySenderResolvesToHexOnStringColumn(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{0xab, 0xcd}
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "name", Type: schema.KindString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   27,
		QueryID:     42,
		QueryString: "SELECT * FROM t WHERE name = :sender",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x (resolve_sender on KindString must succeed)", frame)
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
	want := types.NewString(conn.Identity.Hex())
	if !colEq.Value.Equal(want) {
		t.Fatalf("predicate value = %v, want String(caller hex)", colEq.Value)
	}
}

// TestHandleSubscribeSingle_SenderParameterOnAliasedSingleTable extends the
// reference `select * from s where id = :sender` positive shape
// (reference/SpacetimeDB/crates/expr/src/check.rs lines 435-440) to the
// aliased single-table form `select * from s as r where r.bytes = :sender`.
// The compile path resolves the alias back to the base table for the
// relations map key, so the caller-identity threading already established
// for the unaliased shape should carry through unchanged.
func TestHandleSubscribeSingle_SenderParameterOnAliasedSingleTable(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{5, 6, 7, 8}
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   70,
		QueryID:     71,
		QueryString: "SELECT * FROM s AS r WHERE r.bytes = :sender",
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
	if colEq.Table != 1 || colEq.Column != 1 {
		t.Fatalf("predicate target = table %d col %d, want table 1 col 1", colEq.Table, colEq.Column)
	}
	want := types.NewBytes(conn.Identity[:])
	if !colEq.Value.Equal(want) {
		t.Fatalf("predicate value = %v, want caller identity bytes", colEq.Value)
	}
}

// TestHandleSubscribeSingle_SenderParameterInJoinFilter pins the :sender
// parameter as a join WHERE leaf. Reference positive shape combines the
// inner-join projection form at check.rs lines 462-464 with the :sender
// parameter at lines 435-440. Caller identity is threaded through
// compileSQLPredicateForRelations on the join branch the same way as the
// standalone single-table branch.
func TestHandleSubscribeSingle_SenderParameterInJoinFilter(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
			{Name: "bytes", Type: schema.KindBytes},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, sReg, ok := eng.Registry().TableByName("s")
	if !ok {
		t.Fatal("registry missing table s")
	}

	conn := testConnDirect(nil)
	conn.Identity = types.Identity{0xAA, 0xBB}
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   72,
		QueryID:     73,
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE s.bytes = :sender",
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
	colEq, ok := joinPred.Filter.(subscription.ColEq)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColEq", joinPred.Filter)
	}
	if colEq.Table != sReg.ID || colEq.Column != 2 {
		t.Fatalf("filter target = table %d col %d, want table %d col 2", colEq.Table, colEq.Column, sReg.ID)
	}
	want := types.NewBytes(conn.Identity[:])
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want caller identity bytes", colEq.Value)
	}
}

// TestHandleSubscribeSingle_ParitySenderInJoinFilterResolvesOnStringColumn
// pins resolve_sender → lib.rs:353 on the join-WHERE surface. With
// `WHERE s.label = :sender` against a `KindString` column on the joined
// relation, the compiled join predicate must carry a String(caller hex)
// equality leaf (no protocol-level rejection). Earlier versions asserted a
// rejection on the assumption that `:sender` was bytes-only on join sides;
// reference admits the same widening on join WHERE leaves as on standalone
// single-table predicates.
func TestHandleSubscribeSingle_ParitySenderInJoinFilterResolvesOnStringColumn(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
			{Name: "label", Type: schema.KindString},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, sReg, ok := eng.Registry().TableByName("s")
	if !ok {
		t.Fatal("registry missing table s")
	}

	conn := testConnDirect(nil)
	conn.Identity = types.Identity{0xab, 0xcd}
	executor := &mockSubExecutor{}
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   74,
		QueryID:     75,
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE s.label = :sender",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x (resolve_sender on KindString join leaf must succeed)", frame)
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
	colEq, ok := joinPred.Filter.(subscription.ColEq)
	if !ok {
		t.Fatalf("Join.Filter type = %T, want ColEq", joinPred.Filter)
	}
	if colEq.Table != sReg.ID || colEq.Column != 2 {
		t.Fatalf("filter target = table %d col %d, want table %d col 2", colEq.Table, colEq.Column, sReg.ID)
	}
	want := types.NewString(conn.Identity.Hex())
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want String(caller hex)", colEq.Value)
	}
}

// TestHandleSubscribeSingle_StringLiteralOnIntegerColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 498-501 (`select * from t where u32 = 'str'` /
// "Field u32 is not a string") onto the SubscribeSingle admission surface.
// Shunter enforces the rejection at the coerce boundary inside
// compileSQLQueryString; this pin keeps the externally visible behavior
// tied to the reference shape rather than leaving it incidental.
func TestHandleSubscribeSingle_StringLiteralOnIntegerColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   80,
		QueryID:     81,
		QueryString: "SELECT * FROM t WHERE u32 = 'str'",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 81, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a string literal targets an integer column")
	}
}

// TestHandleSubscribeSingle_FloatLiteralOnIntegerColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 502-504 (`select * from t where t.u32 = 1.3` /
// "Field u32 is not a float") onto the SubscribeSingle admission surface.
// Float literals now parse end-to-end (LitFloat) after the 2026-04-21
// follow-through, so the rejection must fire at the coerce boundary rather
// than at the parser.
func TestHandleSubscribeSingle_FloatLiteralOnIntegerColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   82,
		QueryID:     83,
		QueryString: "SELECT * FROM t WHERE t.u32 = 1.3",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 83, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a float literal targets an integer column")
	}
}

// TestHandleSubscribeSingle_ParityStringDigitsOnIntegerColumnWidens pins
// the reference widening at expr/src/lib.rs:255-352 onto the
// SubscribeSingle admission surface. `WHERE u32 = '42'` must compile
// (no SubscriptionError) and bind `u32` against `types.NewUint32(42)`
// via the new LitString-on-numeric routing through `parseNumericLiteral`.
func TestHandleSubscribeSingle_ParityStringDigitsOnIntegerColumnWidens(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	msg := &SubscribeSingleMsg{
		RequestID:   86,
		QueryID:     87,
		QueryString: "SELECT * FROM t WHERE u32 = '42'",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x", frame)
	default:
	}
	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet — widening rejected")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if !colEq.Value.Equal(types.NewUint32(42)) {
		t.Fatalf("Predicates[0].Value = %v, want Uint32(42)", colEq.Value)
	}
}

// TestHandleSubscribeSingle_ParityNonNumericStringOnIntegerEmitsInvalidLiteral
// pins the reference reject text on the SubscribeSingle admission
// surface. `WHERE u32 = 'foo'` rejects with “ The literal expression
// `foo` cannot be parsed as type `U32` “, WithSql-wrapped via the
// existing `wrapSubscribeCompileErrorSQL` seam (the suffix added per
// `error.rs:140` `DBError::WithSql`).
func TestHandleSubscribeSingle_ParityNonNumericStringOnIntegerEmitsInvalidLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	msg := &SubscribeSingleMsg{
		RequestID:   88,
		QueryID:     89,
		QueryString: "SELECT * FROM t WHERE u32 = 'foo'",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "The literal expression `foo` cannot be parsed as type `U32`, executing: `SELECT * FROM t WHERE u32 = 'foo'`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when LitString rejects via InvalidLiteral")
	}
}

// TestHandleSubscribeSingle_ParityNumericLiteralOnStringColumnWidens pins the
// reference widening at expr/src/lib.rs:353 (`AlgebraicType::String =>
// Ok(AlgebraicValue::String(value.into()))`) onto the SubscribeSingle
// admission surface. `WHERE name = 42` and `WHERE name = 1.3` must compile
// (no SubscriptionError) and bind `name` against the widened String value
// derived from the source literal — Shunter renders LitInt via
// `strconv.FormatInt` and LitFloat via `strconv.FormatFloat('g', -1, 64)`
// at the coerce boundary, so the executor sees a `ColEq` carrying
// `types.NewString("42")` / `types.NewString("1.3")` respectively.
func TestHandleSubscribeSingle_ParityNumericLiteralOnStringColumnWidens(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		wantValue types.Value
	}{
		{"LitInt", "SELECT * FROM t WHERE t.name = 42", types.NewString("42")},
		{"LitFloat", "SELECT * FROM t WHERE t.name = 1.3", types.NewString("1.3")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "name", Type: schema.KindString},
			)

			msg := &SubscribeSingleMsg{
				RequestID:   84,
				QueryID:     85,
				QueryString: tc.sql,
			}
			handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

			select {
			case frame := <-conn.OutboundCh:
				t.Fatalf("unexpected message on OutboundCh: %x", frame)
			default:
			}

			req := executor.getRegisterSetReq()
			if req == nil {
				t.Fatal("executor did not receive RegisterSubscriptionSet — widening rejected")
			}
			if len(req.Predicates) != 1 {
				t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
			}
			colEq, ok := req.Predicates[0].(subscription.ColEq)
			if !ok {
				t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
			}
			if colEq.Column != 0 {
				t.Fatalf("Predicates[0].Column = %d, want 0", colEq.Column)
			}
			if !colEq.Value.Equal(tc.wantValue) {
				t.Fatalf("Predicates[0].Value = %v, want %v", colEq.Value, tc.wantValue)
			}
		})
	}
}

// TestHandleSubscribeSingle_ParityScientificLiteralOverflowPreservesSourceText
// pins the source-text seam through the SubscribeSingle (WithSql wrapper)
// admission surface. `WHERE u8 = 1e3` collapses at the parser to LitInt(1000)
// but keeps `Literal.Text = "1e3"`. Reference parse_int folds to_u8 None
// into `InvalidLiteral::new("1e3", U8)`; Shunter renders the same text
// via `renderLiteralSourceText`, then `wrapSubscribeCompileErrorSQL`
// suffixes the SQL per `error.rs:140` `DBError::WithSql`.
func TestHandleSubscribeSingle_ParityScientificLiteralOverflowPreservesSourceText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	msg := &SubscribeSingleMsg{
		RequestID:   90,
		QueryID:     91,
		QueryString: "SELECT * FROM t WHERE u8 = 1e3",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "The literal expression `1e3` cannot be parsed as type `U8`, executing: `SELECT * FROM t WHERE u8 = 1e3`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when LitInt overflow source-text rejects via InvalidLiteral")
	}
}

// TestHandleSubscribeSingle_ParityHexLiteralWidensOntoStringColumn pins the
// reference `parse(value, String)` arm at lib.rs:353 onto the SubscribeSingle
// admission surface for a Hex source-text literal. `WHERE name =
// 0xDEADBEEF` keeps the original token through `Literal.Text` (parser sets
// it on tokHex), so the compiled predicate carries `String("0xDEADBEEF")`
// as the equality target — no SubscriptionError, executor receives a
// well-formed ColEq.
func TestHandleSubscribeSingle_ParityHexLiteralWidensOntoStringColumn(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "name", Type: schema.KindString},
	)
	msg := &SubscribeSingleMsg{
		RequestID:   92,
		QueryID:     93,
		QueryString: "SELECT * FROM t WHERE name = 0xDEADBEEF",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected message on OutboundCh: %x (hex widening must succeed)", frame)
	default:
	}

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("executor did not receive RegisterSubscriptionSet")
	}
	if len(req.Predicates) != 1 {
		t.Fatalf("len(Predicates) = %d, want 1", len(req.Predicates))
	}
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if !colEq.Value.Equal(types.NewString("0xDEADBEEF")) {
		t.Fatalf("Predicates[0].Value = %v, want String(\"0xDEADBEEF\")", colEq.Value)
	}
}

// TestHandleSubscribeSingle_ParityUnknownTableRejected pins the reference
// type-check rejection at reference/SpacetimeDB/crates/expr/src/check.rs
// lines 483-485 (`select * from r` / "Table r does not exist") onto the
// SubscribeSingle admission surface. Shunter enforces this incidentally via
// SchemaLookup.TableByName returning !ok inside compileSQLQueryString
// (protocol/handle_subscribe.go:152-154); this pin promotes the rejection
// from incidental to named parity contract.
func TestHandleSubscribeSingle_ParityUnknownTableRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   84,
		QueryID:     85,
		QueryString: "SELECT * FROM r",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 85, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when the FROM table is unknown")
	}
}

// TestHandleSubscribeSingle_ParityUnknownColumnRejected pins the reference
// type-check rejection at reference/SpacetimeDB/crates/expr/src/check.rs
// lines 491-493 (`select * from t where t.a = 1` / "Field a does not exist
// on table t") onto the SubscribeSingle admission surface. Shunter enforces
// this incidentally via rel.ts.Column returning !ok inside
// normalizeSQLFilterForRelations (protocol/handle_subscribe.go:250-253); the
// pin promotes the rejection from incidental to named parity contract.
func TestHandleSubscribeSingle_ParityUnknownColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   86,
		QueryID:     87,
		QueryString: "SELECT * FROM t WHERE t.a = 1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 87, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a qualified WHERE column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 495-497 (`select * from t as r where r.a = 1` / "Field a
// does not exist on table t") onto the SubscribeSingle admission surface.
// The aliased single-table shape resolves `r` to base table `t` in the
// parser's relationBindings, then normalizeSQLFilterForRelations fails the
// rel.ts.Column lookup. The pin keeps the rejection named on the alias-
// qualified surface rather than leaving it collapsed under the unaliased
// case.
func TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   88,
		QueryID:     89,
		QueryString: "SELECT * FROM t AS r WHERE r.a = 1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 89, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when an alias-qualified WHERE column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityBaseTableQualifierAfterAliasRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 506-509 (`select * from t as r where t.u32 = 5` / "t is not
// in scope after alias") onto the SubscribeSingle admission surface. Once an
// AS alias is introduced in the FROM, the base table name is out of scope;
// Shunter's parser enforces this incidentally at parseComparison via
// resolveQualifier returning !ok against relationBindings.byQualifier
// (query/sql/parser.go:750-753). The pin promotes the rejection from
// incidental to named parity contract.
func TestHandleSubscribeSingle_ParityBaseTableQualifierAfterAliasRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   90,
		QueryID:     91,
		QueryString: "SELECT * FROM t AS r WHERE t.u32 = 5",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 91, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when the base-table qualifier is out of scope after an AS alias")
	}
}

// TestHandleSubscribeSingle_ParityBareColumnProjectionRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 510-513 (`select u32 from t` / "Subscriptions must be typed
// to a single table") onto the SubscribeSingle admission surface. Shunter's
// parser rejects any projection other than `*` or `table.*` at parseProjection
// (query/sql/parser.go:517-528). The pin promotes the rejection from
// incidental to named parity contract on the protocol boundary.
func TestHandleSubscribeSingle_ParityBareColumnProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   92,
		QueryID:     93,
		QueryString: "SELECT u32 FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 93, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a bare column projection")
	}
}

func TestHandleSubscribeSingle_UnquotedNullWhereRejectedBeforeRegistration(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "null", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   92,
		QueryID:     94,
		QueryString: "SELECT * FROM t WHERE NULL = 1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 94, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when unquoted NULL appears in column position")
	}
}

// TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected pins
// the reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 515-517 (`select * from t join s` / "Subscriptions must be
// typed to a single table") onto the SubscribeSingle admission surface.
// Shunter's parser requires joined queries to name the projected side via a
// qualified projection at parseStatement (query/sql/parser.go:468-469). The
// pin promotes the rejection from incidental to named parity contract.
func TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   94,
		QueryID:     95,
		QueryString: "SELECT * FROM t JOIN s",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 95, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a join query lacks a qualified projection")
	}
}

// TestHandleSubscribeSingle_ParityJoinStarProjectionRejectText pins the
// reference type-check rejection text at
// reference/SpacetimeDB/crates/expr/src/errors.rs:41 (`InvalidWildcard::Join`
// = "SELECT * is not supported for joins"), emitted at
// reference/SpacetimeDB/crates/expr/src/lib.rs:56 when `type_proj` sees
// `ast::Project::Star(None)` against an input with `nfields() > 1`. The
// SubscribeSingle compile-origin error path wraps the inner text with
// `DBError::WithSql` (reference error.rs:140) → `"{error}, executing:
// `{sql}`"`. This pin is the exact-text companion to the shape-only
// rejection pin at TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected.
func TestHandleSubscribeSingle_ParityJoinStarProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}

	const sqlText = "SELECT * FROM t JOIN s"
	msg := &SubscribeSingleMsg{
		RequestID:   220,
		QueryID:     221,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, registrySchemaLookup{reg: eng.Registry()})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "SELECT * is not supported for joins, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on SELECT * JOIN rejection")
	}
}

// TestHandleSubscribeSingle_ParitySelfJoinWithoutAliasesRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 519-521 (`select t.* from t join t` / "Self join requires
// aliases") onto the SubscribeSingle admission surface. Shunter's parser
// rejects the same-alias self-join shape in parseJoinClause
// (query/sql/parser.go:577-579). The pin promotes the rejection from
// incidental to named parity contract.
func TestHandleSubscribeSingle_ParitySelfJoinWithoutAliasesRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   96,
		QueryID:     97,
		QueryString: "SELECT t.* FROM t JOIN t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 97, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called for a self-join without aliases")
	}
}

// TestHandleSubscribeSingle_ParityForwardAliasReferenceRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 526-528 (`select t.* from t join s on t.u32 = r.u32 join s
// as r` / "Alias r is not in scope when it is referenced") onto the
// SubscribeSingle admission surface. Shunter's parser rejects the forward
// alias reference incidentally in parseQualifiedColumnRef via resolveQualifier
// returning !ok against the first join's lookup table (query/sql/parser.go:629
// -631); the multi-way-join rejection at parseStatement (query/sql/parser.go:
// 482-489) would otherwise also fire, but the forward reference fails first.
// The pin names the shape as a parity rejection contract.
func TestHandleSubscribeSingle_ParityForwardAliasReferenceRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   98,
		QueryID:     99,
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 99, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a join references an alias declared later")
	}
}

// TestHandleSubscribeSingle_ParityLimitClauseRejected pins the reference type-
// check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// TestHandleSubscribeSingle_ParityLimitClauseRejected pins reference
// `SubParser::parse_query` (sql-parser/src/parser/sub.rs:94-107)
// rejection of subscription queries carrying `limit: Some(...)` through
// `SubscriptionUnsupported::feature(query)`, rendered as
// `Unsupported: {query}` (parser/errors.rs:18-19) and wrapped with
// `DBError::WithSql` for SubscribeSingle.
func TestHandleSubscribeSingle_ParityLimitClauseRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM t LIMIT 5"
	msg := &SubscribeSingleMsg{
		RequestID:   100,
		QueryID:     101,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 101, "QueryID")
	want := "Unsupported: " + sqlText + ", executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q (LIMIT-in-subscription must emit SubscriptionUnsupported::Feature)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a LIMIT clause trails the query")
	}
}

// TestHandleSubscribeSingle_ParityLimitPrecedesSetQuantifierRejectText pins
// reference `SubParser::parse_query` ordering: subscription LIMIT rejection
// fires before `parse_select` can route SELECT ALL / DISTINCT to the
// `Unsupported SELECT:` arm.
func TestHandleSubscribeSingle_ParityLimitPrecedesSetQuantifierRejectText(t *testing.T) {
	cases := []struct {
		name    string
		sqlText string
		queryID uint32
	}{
		{name: "distinct", sqlText: "SELECT DISTINCT * FROM t LIMIT 5", queryID: 106},
		{name: "all", sqlText: "SELECT ALL * FROM t LIMIT 5", queryID: 107},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
			)

			msg := &SubscribeSingleMsg{
				RequestID:   tc.queryID - 1,
				QueryID:     tc.queryID,
				QueryString: tc.sqlText,
			}
			handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

			tag, decoded := drainServerMsgEventually(t, conn)
			if tag != TagSubscriptionError {
				t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
			}
			se := decoded.(SubscriptionError)
			requireOptionalUint32(t, se.QueryID, tc.queryID, "QueryID")
			want := "Unsupported: " + tc.sqlText + ", executing: `" + tc.sqlText + "`"
			if se.Error != want {
				t.Fatalf("Error = %q, want %q (subscription LIMIT rejection must precede set quantifier)", se.Error, want)
			}
			if req := executor.getRegisterSetReq(); req != nil {
				t.Error("executor should not be called when LIMIT and a set quantifier are rejected")
			}
		})
	}
}

// TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral pins the reference
// valid-literal shape at reference/SpacetimeDB/crates/expr/src/check.rs:297-
// 300 (`select * from t where u32 = +1` / "Leading `+`"): a leading `+` on
// an integer literal is admitted end-to-end (parser accepts, coerce produces
// the unsigned value, subscribe admission registers the set). Mirrors the
// already-landed leading `-` support (`TestParseWhereNegativeInt`).
func TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   104,
		QueryID:     105,
		QueryString: "SELECT * FROM t WHERE u32 = +7",
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
	want := types.NewUint32(7)
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want %v", colEq.Value, want)
	}
}

// TestHandleSubscribeSingle_ParityUnqualifiedWhereInJoinRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 534-537 (`select t.* from t join s on t.u32 = s.u32 where
// bytes = 0xABCD` / "Columns must be qualified in join expressions") onto the
// SubscribeSingle admission surface. Shunter's parser enforces the qualify
// requirement under a join binding at parseComparison
// (query/sql/parser.go:761-763). The pin promotes the rejection from
// incidental to named parity contract.
func TestHandleSubscribeSingle_ParityUnqualifiedWhereInJoinRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   102,
		QueryID:     103,
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE bytes = 0xABCD",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 103, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a WHERE column is unqualified inside a join")
	}
}

// TestHandleSubscribeSingle_ParityScientificNotationUnsignedInteger pins the
// reference valid-literal shape at reference/SpacetimeDB/crates/expr/src/
// check.rs:302-304 (`select * from t where u32 = 1e3` / "Scientific
// notation"): an integer-valued exponent-form numeric binds to an unsigned
// integer column end-to-end.
func TestHandleSubscribeSingle_ParityScientificNotationUnsignedInteger(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   110,
		QueryID:     111,
		QueryString: "SELECT * FROM t WHERE u32 = 1e3",
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
	want := types.NewUint32(1000)
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want %v", colEq.Value, want)
	}
}

// TestHandleSubscribeSingle_ParityScientificNotationFloatNegativeExponent
// pins reference/SpacetimeDB/crates/expr/src/check.rs:314-316 (`select * from
// t where f32 = 1e-3` / "Negative exponent"): a non-integral exponent-form
// numeric binds to a float column end-to-end.
func TestHandleSubscribeSingle_ParityScientificNotationFloatNegativeExponent(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   112,
		QueryID:     113,
		QueryString: "SELECT * FROM t WHERE f32 = 1e-3",
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
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	want, err := types.NewFloat32(float32(1e-3))
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want %v", colEq.Value, want)
	}
}

// TestHandleSubscribeSingle_ParityLeadingDotFloatLiteral pins reference/
// SpacetimeDB/crates/expr/src/check.rs:322-324 (`select * from t where
// f32 = .1` / "Leading `.`"): a leading-dot numeric with no integer part
// binds to a float column end-to-end.
func TestHandleSubscribeSingle_ParityLeadingDotFloatLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   114,
		QueryID:     115,
		QueryString: "SELECT * FROM t WHERE f32 = .1",
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
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	want, err := types.NewFloat32(float32(0.1))
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want %v", colEq.Value, want)
	}
}

// TestHandleSubscribeSingle_ParityScientificNotationOverflowInfinity pins
// reference/SpacetimeDB/crates/expr/src/check.rs:326-328 (`select * from t
// where f32 = 1e40` / "Infinity"): a magnitude beyond float32 range binds to
// the f32 column as +Inf rather than being rejected.
func TestHandleSubscribeSingle_ParityScientificNotationOverflowInfinity(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   116,
		QueryID:     117,
		QueryString: "SELECT * FROM t WHERE f32 = 1e40",
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
	colEq, ok := req.Predicates[0].(subscription.ColEq)
	if !ok {
		t.Fatalf("Predicates[0] type = %T, want ColEq", req.Predicates[0])
	}
	if colEq.Value.Kind() != types.KindFloat32 {
		t.Fatalf("Kind = %v, want KindFloat32", colEq.Value.Kind())
	}
	if !math.IsInf(float64(colEq.Value.AsFloat32()), 1) {
		t.Fatalf("value = %v, want +Inf", colEq.Value.AsFloat32())
	}
}

// TestHandleSubscribeSingle_ParityInvalidLiteralNegativeIntOnUnsignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:382-385 (`select * from
// t where u8 = -1` / "Negative integer for unsigned column") onto the
// SubscribeSingle admission surface. `-1` parses to LitInt(-1) and
// coerceUnsigned (query/sql/coerce.go:119) rejects negative ints before they
// reach an unsigned column; the pin names the rejection as a parity contract.
func TestHandleSubscribeSingle_ParityInvalidLiteralNegativeIntOnUnsignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   118,
		QueryID:     119,
		QueryString: "SELECT * FROM t WHERE u8 = -1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 119, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a negative literal targets an unsigned column")
	}
}

// TestHandleSubscribeSingle_ParityInvalidLiteralScientificOverflowRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:386-389 (`select * from
// t where u8 = 1e3` / "Out of bounds") onto the SubscribeSingle admission
// surface. `1e3` parses via parseNumericLiteral as an integer-valued literal
// that collapses to LitInt(1000); coerceUnsigned (query/sql/coerce.go:123)
// rejects it as out of range for u8 (max 255).
func TestHandleSubscribeSingle_ParityInvalidLiteralScientificOverflowRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   120,
		QueryID:     121,
		QueryString: "SELECT * FROM t WHERE u8 = 1e3",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 121, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a scientific-notation literal overflows the unsigned column")
	}
}

// TestHandleSubscribeSingle_ParityInvalidLiteralFloatOnUnsignedRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:390-393 (`select * from t
// where u8 = 0.1` / "Float as integer") onto the SubscribeSingle admission
// surface. A non-integral decimal stays LitFloat and coerceUnsigned
// (query/sql/coerce.go:116) rejects non-LitInt against an integer column.
// Complements the existing u32 = 1.3 pin by naming the u8 column variant.
func TestHandleSubscribeSingle_ParityInvalidLiteralFloatOnUnsignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   122,
		QueryID:     123,
		QueryString: "SELECT * FROM t WHERE u8 = 0.1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 123, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a float literal targets an unsigned column")
	}
}

// TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnUnsignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:394-397 (`select * from
// t where u32 = 1e-3` / "Float as integer") onto the SubscribeSingle
// admission surface. `1e-3` parses to 0.001, fails the integer-valued collapse
// in parseNumericLiteral (non-integral), stays LitFloat, and coerceUnsigned
// rejects LitFloat against a KindUint32 column.
func TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnUnsignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   124,
		QueryID:     125,
		QueryString: "SELECT * FROM t WHERE u32 = 1e-3",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 125, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a non-integral scientific literal targets an unsigned column")
	}
}

// TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnSignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:398-401 (`select * from
// t where i32 = 1e-3` / "Float as integer") onto the SubscribeSingle
// admission surface. Mirrors the unsigned case on a signed column:
// parseNumericLiteral leaves 0.001 as LitFloat, and coerceSigned
// (query/sql/coerce.go:106) rejects non-LitInt against a KindInt32 column.
func TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnSignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "i32", Type: schema.KindInt32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   126,
		QueryID:     127,
		QueryString: "SELECT * FROM t WHERE i32 = 1e-3",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 127, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a non-integral scientific literal targets a signed column")
	}
}

// TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth pins
// reference/SpacetimeDB/crates/expr/src/check.rs:360-370
// (`valid_literals_for_type`) at the SubscribeSingle admission surface.
// The reference test iterates every numeric column kind and asserts that
// `{ty} = 127` parses and type-checks; Shunter realizes the full
// i8/u8/i16/u16/i32/u32/i64/u64/f32/f64/i128/u128/i256/u256 set
// (128-bit added 2026-04-21 slice 1, 256-bit added 2026-04-21 slice 2).
// Each subtest builds a single-column table, sends
// `SELECT * FROM t WHERE {colname} = 127`, and asserts the executor
// receives a ColEq predicate with the width-native value. The reference
// `u256 = 1e40` row stays deferred until BigDecimal literal widening.
func TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth(t *testing.T) {
	f32Want, err := types.NewFloat32(127)
	if err != nil {
		t.Fatalf("NewFloat32(127): %v", err)
	}
	f64Want, err := types.NewFloat64(127)
	if err != nil {
		t.Fatalf("NewFloat64(127): %v", err)
	}

	cases := []struct {
		colName string
		kind    schema.ValueKind
		want    types.Value
	}{
		{"i8", schema.KindInt8, types.NewInt8(127)},
		{"u8", schema.KindUint8, types.NewUint8(127)},
		{"i16", schema.KindInt16, types.NewInt16(127)},
		{"u16", schema.KindUint16, types.NewUint16(127)},
		{"i32", schema.KindInt32, types.NewInt32(127)},
		{"u32", schema.KindUint32, types.NewUint32(127)},
		{"i64", schema.KindInt64, types.NewInt64(127)},
		{"u64", schema.KindUint64, types.NewUint64(127)},
		{"f32", schema.KindFloat32, f32Want},
		{"f64", schema.KindFloat64, f64Want},
		{"i128", schema.KindInt128, types.NewInt128(0, 127)},
		{"u128", schema.KindUint128, types.NewUint128(0, 127)},
		{"i256", schema.KindInt256, types.NewInt256(0, 0, 0, 127)},
		{"u256", schema.KindUint256, types.NewUint256(0, 0, 0, 127)},
	}

	for i, tc := range cases {
		t.Run(tc.colName, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: tc.colName, Type: tc.kind},
			)

			requestID := uint32(200 + i*2)
			queryID := uint32(201 + i*2)
			msg := &SubscribeSingleMsg{
				RequestID:   requestID,
				QueryID:     queryID,
				QueryString: "SELECT * FROM t WHERE " + tc.colName + " = 127",
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
			if !colEq.Value.Equal(tc.want) {
				t.Fatalf("filter value = %v, want %v", colEq.Value, tc.want)
			}
		})
	}
}

// TestHandleSubscribeSingle_ParityValidLiteralU256Scientific pins the
// remaining reference `valid_literals` row at
// reference/SpacetimeDB/crates/expr/src/check.rs:330-332
// (`select * from t where u256 = 1e40` / "u256"). The reference BigDecimal
// is_integer path treats `1e40` as the exact integer 10^40, which fits u256
// (max ~1.16e77). Shunter's parser now promotes `1e40` to LitBigInt and
// coerce decomposes it into four uint64 words matching the 256-bit layout.
// Admission must succeed and the executor must receive a ColEq predicate
// carrying the 10^40 Uint256 value.
func TestHandleSubscribeSingle_ParityValidLiteralU256Scientific(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u256", Type: schema.KindUint256},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   250,
		QueryID:     251,
		QueryString: "SELECT * FROM t WHERE u256 = 1e40",
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
	wantBig, _ := new(big.Int).SetString("10000000000000000000000000000000000000000", 10)
	want, err := sql.Coerce(sql.Literal{Kind: sql.LitBigInt, Big: wantBig}, schema.KindUint256)
	if err != nil {
		t.Fatalf("build expected: %v", err)
	}
	if !colEq.Value.Equal(want) {
		t.Fatalf("filter value = %v, want Uint256(10^40)", colEq.Value)
	}
}

// TestHandleSubscribeSingle_ParityUint256NegativeRejected extends the
// reference invalid_literals bundle at check.rs:382-385 to the Uint256
// column kind. `-1` parses to LitInt(-1) and coerce's KindUint256 branch
// rejects negative ints just like the u8 / u128 rows do.
func TestHandleSubscribeSingle_ParityUint256NegativeRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u256", Type: schema.KindUint256},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   242,
		QueryID:     243,
		QueryString: "SELECT * FROM t WHERE u256 = -1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 243, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a negative literal targets a Uint256 column")
	}
}

// TestHandleSubscribeSingle_ParityTimestampLiteralAccepted pins the reference
// valid_literals rows at check.rs:334-352 onto the SubscribeSingle admission
// surface: RFC3339-shaped string literals bind to a KindTimestamp column. The
// coerce path (query/sql/coerce.go) parses `T`/space separator, optional
// fractional seconds up to nanoseconds (truncated to micros), and both `Z`
// and numeric offset forms. Each subtest runs
// `SELECT * FROM t WHERE ts = '<shape>'` and confirms the executor receives a
// ColEq predicate carrying a Timestamp value with the expected micros.
func TestHandleSubscribeSingle_ParityTimestampLiteralAccepted(t *testing.T) {
	cases := []struct {
		name  string
		lit   string
		micro int64
	}{
		{"rfc3339_utc_no_fraction", "2025-02-10T15:45:30Z", 1_739_202_330_000_000},
		{"rfc3339_utc_millis", "2025-02-10T15:45:30.123Z", 1_739_202_330_123_000},
		{"rfc3339_utc_nanos_truncated", "2025-02-10T15:45:30.123456789Z", 1_739_202_330_123_456},
		{"space_separator_offset", "2025-02-10 15:45:30+02:00", 1_739_195_130_000_000},
		{"space_separator_millis_offset", "2025-02-10 15:45:30.123+02:00", 1_739_195_130_123_000},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "ts", Type: schema.KindTimestamp},
			)

			requestID := uint32(260 + i*2)
			queryID := uint32(261 + i*2)
			msg := &SubscribeSingleMsg{
				RequestID:   requestID,
				QueryID:     queryID,
				QueryString: "SELECT * FROM t WHERE ts = '" + tc.lit + "'",
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
			if colEq.Value.Kind() != schema.KindTimestamp {
				t.Fatalf("filter kind = %v, want Timestamp", colEq.Value.Kind())
			}
			if got := colEq.Value.AsTimestamp(); got != tc.micro {
				t.Fatalf("filter micros = %d, want %d", got, tc.micro)
			}
		})
	}
}

// TestHandleSubscribeSingle_ParityTimestampMalformedRejected pins reference
// `InvalidLiteral` text for a non-RFC3339 string on a Timestamp column on
// the SubscribeSingle admission surface. Reference path: `parse(value,
// Timestamp)` (expr/src/lib.rs:359) falls through the catch-all `bail!`,
// folded by lib.rs:99 into `InvalidLiteral::new(v.into_string(), ty)`.
// SubscribeSingle wraps compile errors with `DBError::WithSql`
// (module_subscription_actor.rs:643), so the pin carries the
// `, executing: ` suffix.
func TestHandleSubscribeSingle_ParityTimestampMalformedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "ts", Type: schema.KindTimestamp},
	)

	const sqlText = "SELECT * FROM t WHERE ts = 'not-a-timestamp'"
	msg := &SubscribeSingleMsg{
		RequestID:   270,
		QueryID:     271,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 271, "QueryID")
	want := "The literal expression `not-a-timestamp` cannot be parsed as type `(__timestamp_micros_since_unix_epoch__: I64)`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a malformed timestamp literal")
	}
}

// TestHandleSubscribeSingle_ParityBoolLiteralOnTimestampRejectText pins
// reference `UnexpectedType` text for a bool literal on a Timestamp column.
// Reference lib.rs:94 routes the bool arm directly to UnexpectedType
// (errors.rs:100) ahead of the lib.rs:99 InvalidLiteral fallback. Timestamp
// inferred name is the SATS Product fmt. SubscribeSingle wraps with
// DBError::WithSql.
func TestHandleSubscribeSingle_ParityBoolLiteralOnTimestampRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "ts", Type: schema.KindTimestamp},
	)

	const sqlText = "SELECT * FROM t WHERE ts = TRUE"
	msg := &SubscribeSingleMsg{
		RequestID:   272,
		QueryID:     273,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Unexpected type: (expected) Bool != (__timestamp_micros_since_unix_epoch__: I64) (inferred), executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a bool literal targets a Timestamp column")
	}
}

// TestHandleSubscribeSingle_ParityStringLiteralOnArrayStringRejectText pins
// reference `InvalidLiteral` text for a scalar string literal targeting an
// Array<String> column. Reference `parse(value, Array<String>)` at
// lib.rs:359 hits the array-kind catch-all `bail!`, folded by lib.rs:99
// into `InvalidLiteral::new(v.into_string(), ty)`. Array<String> renders
// through the parameterized array fmt. SubscribeSingle wraps with
// DBError::WithSql.
func TestHandleSubscribeSingle_ParityStringLiteralOnArrayStringRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "arr", Type: schema.KindArrayString},
	)

	const sqlText = "SELECT * FROM t WHERE arr = 'x'"
	msg := &SubscribeSingleMsg{
		RequestID:   274,
		QueryID:     275,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "The literal expression `x` cannot be parsed as type `Array<String>`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a scalar literal targets an Array<String> column")
	}
}

// TestHandleSubscribeSingle_ParityBoolLiteralOnArrayStringRejectText pins
// reference `UnexpectedType` text for a bool literal on an Array<String>
// column. Reference lib.rs:94 routes the bool arm to UnexpectedType ahead
// of the lib.rs:99 InvalidLiteral fallback. SubscribeSingle wraps with
// DBError::WithSql.
func TestHandleSubscribeSingle_ParityBoolLiteralOnArrayStringRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "arr", Type: schema.KindArrayString},
	)

	const sqlText = "SELECT * FROM t WHERE arr = TRUE"
	msg := &SubscribeSingleMsg{
		RequestID:   276,
		QueryID:     277,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Unexpected type: (expected) Bool != Array<String> (inferred), executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a bool literal targets an Array<String> column")
	}
}

// TestHandleSubscribeSingle_ParityUint128NegativeRejected extends the
// reference invalid_literals bundle at check.rs:382-385 to the Uint128
// column kind (landed 2026-04-21 alongside the 128-bit column-kind
// widening). `-1` parses to LitInt(-1) and coerce's KindUint128 branch
// rejects negative ints just like the u8 row does.
func TestHandleSubscribeSingle_ParityUint128NegativeRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u128", Type: schema.KindUint128},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   240,
		QueryID:     241,
		QueryString: "SELECT * FROM t WHERE u128 = -1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 241, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a negative literal targets a Uint128 column")
	}
}

// TestHandleSubscribeSingle_ParityDMLStatementRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`delete from t` / "DML not allowed in subscriptions") onto the
// SubscribeSingle admission surface. Shunter's SELECT-only parser rejects any
// leading token other than SELECT at parseStatement's expectKeyword("SELECT")
// call (query/sql/parser.go:475-477). The pin promotes the rejection from
// incidental to named parity contract.
func TestHandleSubscribeSingle_ParityDMLStatementRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   130,
		QueryID:     131,
		QueryString: "DELETE FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 131, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a DML statement")
	}
}

// TestHandleSubscribeSingle_ParityEmptyStatementRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (empty string / "Empty") onto the SubscribeSingle admission surface.
// Shunter's parser rejects via expectKeyword("SELECT") returning "expected
// SELECT, got end of input" on a token stream that tokenizes to only EOF.
func TestHandleSubscribeSingle_ParityEmptyStatementRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   132,
		QueryID:     133,
		QueryString: "",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 133, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on an empty query string")
	}
}

// TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected pins the
// reference subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (single space / "Empty after whitespace skip") onto the SubscribeSingle
// admission surface. Shunter's tokenizer drops whitespace so the parser sees
// only EOF and fails at expectKeyword("SELECT").
func TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   134,
		QueryID:     135,
		QueryString: "   ",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 135, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a whitespace-only query string")
	}
}

// TestHandleSubscribeSingle_ParityDistinctProjectionRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`select distinct a from t` / "DISTINCT not supported") onto the
// SubscribeSingle admission surface. Shunter's parseProjection requires `*`
// or `table.*` (query/sql/parser.go:553-572); the DISTINCT identifier is
// consumed as a qualifier candidate, the next token is `a` not `.`, and the
// parser rejects with "projection must be '*' or 'table.*'".
func TestHandleSubscribeSingle_ParityDistinctProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT DISTINCT u32 FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   136,
		QueryID:     137,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 137, "QueryID")
	want := "Unsupported SELECT: " + sqlText + ", executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a DISTINCT projection")
	}
}

// TestHandleSubscribeSingle_ParityAllModifierRejected pins the reference
// subscription-parser rejection at sub.rs:120-149 (and the inner SQL
// parser at sql.rs:362-394). The set quantifier `ALL` produces a non-None
// `distinct` field which the subscribe `parse_select` arm rejects through
// `SubscriptionUnsupported::Select(select)` rendered as
// `Unsupported SELECT: {select}`, then wrapped via `DBError::WithSql`.
// The test schema deliberately includes a column named `ALL` to confirm
// the parser detects the modifier rather than reinterpreting the keyword
// as a column reference with output alias `u32`.
func TestHandleSubscribeSingle_ParityAllModifierRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "ALL", Type: schema.KindUint32},
	)

	const sqlText = "SELECT ALL u32 FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   422,
		QueryID:     423,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Unsupported SELECT: " + sqlText + ", executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a SELECT ALL projection")
	}
}

// TestHandleSubscribeSingle_ParitySubqueryInFromRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`select * from (select * from t) join (select * from s) on a = b` /
// "Subqueries in FROM not supported") onto the SubscribeSingle admission
// surface. Shunter's parseStatement requires an identifier token after FROM
// (query/sql/parser.go:485-488); the `(` token is tokLParen, not an identifier,
// so the parser rejects with "expected table name".
func TestHandleSubscribeSingle_ParitySubqueryInFromRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   138,
		QueryID:     139,
		QueryString: "SELECT * FROM (SELECT * FROM t) JOIN (SELECT * FROM s) ON a = b",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 139, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a subquery in FROM")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedSelectLiteralWithoutFromRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select 1` / "FROM is required") onto the SubscribeSingle admission surface.
// Shunter's parseProjection only accepts `*` or `table.*`
// (query/sql/parser.go:553-572); the integer literal `1` matches neither and
// the parser rejects with "projection must be '*' or 'table.*'".
func TestHandleSubscribeSingle_ParitySqlUnsupportedSelectLiteralWithoutFromRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   140,
		QueryID:     141,
		QueryString: "SELECT 1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 141, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on SELECT without FROM")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedMultiPartTableNameRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a from s.t` / "Multi-part table names") onto the SubscribeSingle
// admission surface. Shunter's parseProjection rejects the bare identifier `a`
// (non-`*` / non-`table.*`) before FROM parsing begins, so the rejection fires
// at the projection surface with "projection must be '*' or 'table.*'".
func TestHandleSubscribeSingle_ParitySqlUnsupportedMultiPartTableNameRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   142,
		QueryID:     143,
		QueryString: "SELECT a FROM s.t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 143, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on multi-part table name")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedBitStringLiteralRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select * from t where a = B'1010'` / "Bit-string literals") onto the
// SubscribeSingle admission surface. Shunter's lexer tokenizes `B` as an
// identifier and `'1010'` as a separate string literal; parseLiteral then
// rejects the identifier RHS with "expected literal, got identifier "B"".
func TestHandleSubscribeSingle_ParitySqlUnsupportedBitStringLiteralRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   144,
		QueryID:     145,
		QueryString: "SELECT * FROM t WHERE u32 = B'1010'",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 145, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a bit-string literal")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedWildcardWithBareColumnsRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a.*, b, c from t` / "Wildcard with non-wildcard projections") onto
// the SubscribeSingle admission surface. Shunter's parseProjection accepts one
// projection item; after consuming `a.*` the parser expects FROM but finds `,`
// and rejects with "expected FROM, got \",\"".
func TestHandleSubscribeSingle_ParitySqlUnsupportedWildcardWithBareColumnsRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   146,
		QueryID:     147,
		QueryString: "SELECT t.*, b, c FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 147, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on wildcard with bare columns")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedOrderByWithLimitExpressionRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select * from t order by a limit b` / "Limit expression") onto the
// SubscribeSingle admission surface. The standalone ORDER BY clause already
// trips Shunter's EOF guard at parseStatement (query/sql/parser.go:547-549)
// with "unexpected token \"ORDER\"" before reaching the LIMIT identifier.
func TestHandleSubscribeSingle_ParitySqlUnsupportedOrderByWithLimitExpressionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   148,
		QueryID:     149,
		QueryString: "SELECT * FROM t ORDER BY u32 LIMIT u32",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 149, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on ORDER BY with LIMIT expression")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedAggregateWithGroupByRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a, count(*) from t group by a` / "GROUP BY") onto the SubscribeSingle
// admission surface. parseProjection rejects the leading bare column `a` with
// "projection must be '*' or 'table.*'" before the aggregate or GROUP BY
// keyword is ever seen.
func TestHandleSubscribeSingle_ParitySqlUnsupportedAggregateWithGroupByRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   150,
		QueryID:     151,
		QueryString: "SELECT u32, COUNT(*) FROM t GROUP BY u32",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 151, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on aggregate with GROUP BY")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedImplicitCommaJoinRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a.* from t as a, s as b where a.id = b.id and b.c = 1` /
// "Implicit joins") onto the SubscribeSingle admission surface. After
// consuming `t AS a`, parseStatement's EOF/keyword guard hits `,` and rejects
// with "unexpected token \",\"".
func TestHandleSubscribeSingle_ParitySqlUnsupportedImplicitCommaJoinRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   152,
		QueryID:     153,
		QueryString: "SELECT a.* FROM t AS a, s AS b WHERE a.u32 = b.u32",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 153, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on implicit comma join")
	}
}

// TestHandleSubscribeSingle_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select t.* from t join s on int = u32` / "Joins require qualified vars")
// onto the SubscribeSingle admission surface. parseJoinClause calls
// parseQualifiedColumnRef for the left side of ON (query/sql/parser.go:629),
// which requires `ident.ident`; the bare identifier `int` fails there.
func TestHandleSubscribeSingle_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   154,
		QueryID:     155,
		QueryString: "SELECT t.* FROM t JOIN s ON int = u32",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 155, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on unqualified JOIN ON vars")
	}
}

// TestHandleSubscribeSingle_ParitySqlInvalidEmptySelectRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select from t` / "Empty SELECT") onto the SubscribeSingle admission
// surface. parseProjection rejects because the next token after SELECT is the
// identifier `from`, which is then followed by `t` (not a dot), so the
// projection fails with "projection must be '*' or 'table.*'".
func TestHandleSubscribeSingle_ParitySqlInvalidEmptySelectRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   156,
		QueryID:     157,
		QueryString: "SELECT FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 157, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on empty SELECT")
	}
}

// TestHandleSubscribeSingle_ParitySqlInvalidEmptyFromRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a from where b = 1` / "Empty FROM") onto the SubscribeSingle
// admission surface. parseProjection rejects the bare column `a` with
// "projection must be '*' or 'table.*'" before the empty FROM is examined.
func TestHandleSubscribeSingle_ParitySqlInvalidEmptyFromRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   158,
		QueryID:     159,
		QueryString: "SELECT a FROM WHERE b = 1",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 159, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on empty FROM")
	}
}

// TestHandleSubscribeSingle_ParitySqlInvalidEmptyWhereRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a from t where` / "Empty WHERE") onto the SubscribeSingle admission
// surface. parseProjection rejects the bare column `a` with "projection must
// be '*' or 'table.*'" before the empty WHERE is examined.
func TestHandleSubscribeSingle_ParitySqlInvalidEmptyWhereRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   160,
		QueryID:     161,
		QueryString: "SELECT a FROM t WHERE",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 161, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on empty WHERE")
	}
}

// TestHandleSubscribeSingle_ParitySqlInvalidEmptyGroupByRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a, count(*) from t group by` / "Empty GROUP BY") onto the
// SubscribeSingle admission surface. parseProjection rejects the leading bare
// column `a` with "projection must be '*' or 'table.*'" before the aggregate
// or empty GROUP BY is examined.
func TestHandleSubscribeSingle_ParitySqlInvalidEmptyGroupByRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   162,
		QueryID:     163,
		QueryString: "SELECT a, COUNT(*) FROM t GROUP BY",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 163, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on empty GROUP BY")
	}
}

// TestHandleSubscribeSingle_ParityCountAliasRejected pins the deliberate
// subscribe-side policy rejection for parsed aggregate projections. Query SQL
// may widen to accept `COUNT(*) [AS] alias`, but subscriptions must still return
// SubscriptionError and skip executor registration.
func TestHandleSubscribeSingle_ParityCountAliasRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   164,
		QueryID:     165,
		QueryString: "SELECT COUNT(*) AS n FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 165, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on aggregate projection")
	}
}

func TestHandleSubscribeSingle_ParityCountBareAliasRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   174,
		QueryID:     175,
		QueryString: "SELECT COUNT(*) n FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 175, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on bare-alias aggregate projection")
	}
}

// TestHandleSubscribeSingle_ParityCountAliasWithLimitRejected pins the
// deliberate subscribe-side rejection for aggregate projections composed
// with LIMIT. One-off/ad hoc SQL accepts the combination, but
// subscriptions must still return SubscriptionError and skip executor
// registration. The compileSQLQueryString guard order means
// allowLimit=false catches LIMIT before the aggregate guard fires, so
// the visible error is the reference `SubscriptionUnsupported::Feature`
// shape `Unsupported: {sql}` wrapped with `DBError::WithSql`.
func TestHandleSubscribeSingle_ParityCountAliasWithLimitRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT COUNT(*) AS n FROM t LIMIT 1"
	msg := &SubscribeSingleMsg{
		RequestID:   184,
		QueryID:     185,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 185, "QueryID")
	want := "Unsupported: " + sqlText + ", executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q (LIMIT-in-subscription must emit SubscriptionUnsupported::Feature)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on aggregate+LIMIT projection")
	}
}

func TestHandleSubscribeSingle_JoinCountAggregateStillRejected(t *testing.T) {
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
			{Index: 0, Name: "t_id", Type: schema.KindUint32},
			{Index: 1, Name: "active", Type: schema.KindBool},
		}}},
	}}

	msg := &SubscribeSingleMsg{
		RequestID:   176,
		QueryID:     177,
		QueryString: "SELECT COUNT(*) n FROM t JOIN s ON t.id = s.t_id WHERE s.active = TRUE",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 177, "QueryID")
	if !strings.Contains(se.Error, "Column projections are not supported in subscriptions; Subscriptions must return a table type") {
		t.Fatalf("Error = %q, want deliberate aggregate subscription rejection", se.Error)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on join-backed aggregate projection")
	}
}

func TestHandleSubscribeSingle_ParityAliasedBareColumnProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   166,
		QueryID:     167,
		QueryString: "SELECT u32 AS n FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 167, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on aliased explicit projection")
	}
}

func TestHandleSubscribeSingle_ParityJoinColumnProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "Orders",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "product_id", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_orders_product_id", Columns: []string{"product_id"}}},
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
	sl := registrySchemaLookup{reg: eng.Registry()}

	msg := &SubscribeSingleMsg{
		RequestID:   168,
		QueryID:     169,
		QueryString: "SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 169, "QueryID")
	if !strings.Contains(se.Error, "Column projections are not supported in subscriptions; Subscriptions must return a table type") {
		t.Fatalf("Error = %q, want deliberate subscription projection rejection", se.Error)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called for join-backed column-list projection")
	}
}

// TestHandleSubscribeSingle_ParitySqlInvalidAggregateWithoutAliasRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select count(*) from t` / "Aggregate without alias") onto the
// SubscribeSingle admission surface. parseProjection reads `count` as an
// identifier qualifier, then finds `(` where it expects a dot, rejecting with
// "projection must be '*' or 'table.*'".
func TestHandleSubscribeSingle_ParitySqlInvalidAggregateWithoutAliasRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   164,
		QueryID:     165,
		QueryString: "SELECT COUNT(*) FROM t",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 165, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on aggregate without alias")
	}
}

// TestHandleSubscribeSingle_ParityArraySenderRejected pins reference
// check.rs:487-489 (`select * from t where arr = :sender` / "The :sender
// param is an identity"). With KindArrayString realized, the coerce layer
// rejects :sender against the array column because :sender only resolves
// to the 32-byte identity (KindBytes) representation. The rejection is
// now a positive parity contract instead of falling through the default
// "column kind not supported" branch.
func TestHandleSubscribeSingle_ParityArraySenderRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "arr", Type: schema.KindArrayString},
	)

	msg := &SubscribeSingleMsg{
		RequestID:   400,
		QueryID:     401,
		QueryString: "SELECT * FROM t WHERE arr = :sender",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 401, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when :sender targets an array column")
	}
}

// TestHandleSubscribeSingle_ParityArrayJoinOnRejected pins reference
// check.rs:523-525 (`select t.* from t join s on t.arr = s.arr` / "Product
// values are not comparable"). The join compile path refuses to build a
// subscription.Join when either side of the ON clause names an array
// column.
func TestHandleSubscribeSingle_ParityArrayJoinOnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := &mockSchemaLookup{
		tables: map[string]struct {
			id     schema.TableID
			schema *schema.TableSchema
		}{
			"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
				{Index: 0, Name: "arr", Type: schema.KindArrayString},
			}}},
			"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
				{Index: 0, Name: "arr", Type: schema.KindArrayString},
			}}},
		},
	}

	msg := &SubscribeSingleMsg{
		RequestID:   402,
		QueryID:     403,
		QueryString: "SELECT t.* FROM t JOIN s ON t.arr = s.arr",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 403, "QueryID")
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on array-on-array join ON")
	}
}

// TestHandleSubscribeSingle_ParityJoinOnStrictEqualityRejectText pins the
// reference subscription parser's `JoinType` rejection for any JOIN ON shape
// other than a pure qualified-column equality. SubscribeSingle wraps the raw
// parser text with DBError::WithSql.
func TestHandleSubscribeSingle_ParityJoinOnStrictEqualityRejectText(t *testing.T) {
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
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	requireOptionalUint32(t, se.QueryID, 15, "SubscriptionError.QueryID")
	requireOptionalUint32(t, se.RequestID, 18, "SubscriptionError.RequestID")
	want := "Non-inner joins are not supported, executing: `" + msg.QueryString + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when JOIN ON is not a pure equality")
	}
}

// TestHandleSubscribeSingle_ParityCompileErrorIncludesExecutingSqlSuffix pins
// the reference `DBError::WithSql` shape at
// reference/SpacetimeDB/crates/core/src/error.rs:140
// (`"{error}, executing: `{sql}`"`): subscribe-admission compile failures
// carry the offending SQL text in the SubscriptionError wire message so
// clients can correlate errors with the exact query they sent. Reference
// emit site: module_subscription_actor.rs:643 (SubscribeSingle
// `compile_query_with_hashes`) via the `return_on_err_with_sql_bool!`
// macro.
func TestHandleSubscribeSingle_ParityCompileErrorIncludesExecutingSqlSuffix(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1)

	const badSQL = "SELECT * FROM missing"
	msg := &SubscribeSingleMsg{
		RequestID:   210,
		QueryID:     211,
		QueryString: badSQL,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	wantSuffix := ", executing: `" + badSQL + "`"
	if !strings.HasSuffix(se.Error, wantSuffix) {
		t.Fatalf("Error = %q, want suffix %q (reference DBError::WithSql)", se.Error, wantSuffix)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a compile-error admission")
	}
}

// TestHandleSubscribeMulti_ParityCompileErrorIncludesExecutingSqlSuffix pins
// the reference `DBError::WithSql` shape at
// reference/SpacetimeDB/crates/core/src/error.rs:140
// (`"{error}, executing: `{sql}`"`) on the SubscribeMulti admission
// surface. Reference emit site: module_subscription_actor.rs:1068
// (SubscribeMulti `compile_query_with_hashes`), where each SQL string is
// wrapped per-item; the first failing SQL's text is what appears in the
// SubscriptionError message.
func TestHandleSubscribeMulti_ParityCompileErrorIncludesExecutingSqlSuffix(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("users", 1)

	const badSQL = "SELECT * FROM missing"
	msg := &SubscribeMultiMsg{
		RequestID: 212,
		QueryID:   213,
		QueryStrings: []string{
			"SELECT * FROM users",
			badSQL,
		},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	wantSuffix := ", executing: `" + badSQL + "`"
	if !strings.HasSuffix(se.Error, wantSuffix) {
		t.Fatalf("Error = %q, want suffix %q (reference DBError::WithSql names the offending SQL)", se.Error, wantSuffix)
	}
	if req := exec.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a compile-error admission")
	}
}

// TestHandleSubscribeMulti_ParityJoinStarProjectionRejectText pins
// reference/SpacetimeDB/crates/expr/src/errors.rs:41
// (`InvalidWildcard::Join` = "SELECT * is not supported for joins",
// emit reference/SpacetimeDB/crates/expr/src/lib.rs:56) on the
// SubscribeMulti admission surface. SubscribeMulti routes each SQL
// through `compile_query_with_hashes` at
// module_subscription_actor.rs:1068 via `return_on_err_with_sql_bool!`,
// so the per-item compile failure wraps the inner text with the
// `DBError::WithSql` suffix (error.rs:140).
func TestHandleSubscribeMulti_ParityJoinStarProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}

	const badSQL = "SELECT * FROM t JOIN s"
	msg := &SubscribeMultiMsg{
		RequestID:    222,
		QueryID:      223,
		QueryStrings: []string{"SELECT * FROM t", badSQL},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, registrySchemaLookup{reg: eng.Registry()})

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "SELECT * is not supported for joins, executing: `" + badSQL + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := exec.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on SELECT * JOIN rejection")
	}
}

// TestHandleSubscribeSingle_ParityUnknownTableRejectText pins the reference
// type-check rejection literal at
// reference/SpacetimeDB/crates/expr/src/errors.rs:14
// (`Unresolved::Table` = "no such table: `{0}`. If the table exists, it may
// be marked private."). SubscribeSingle compile-origin wraps the inner text
// with `DBError::WithSql` (reference error.rs:140) → `"{error}, executing:
// `{sql}`"`. Exact-text companion to TestHandleSubscribeSingle_ParityUnknownTableRejected.
func TestHandleSubscribeSingle_ParityUnknownTableRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM r"
	msg := &SubscribeSingleMsg{
		RequestID:   230,
		QueryID:     231,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `r`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when the FROM table is unknown")
	}
}

// TestHandleSubscribeMulti_ParityUnknownTableRejectText pins the same
// `Unresolved::Table` literal on the SubscribeMulti admission surface.
// Reference SubscribeMulti wraps each per-item compile error with
// `DBError::WithSql` (module_subscription_actor.rs:1068 via
// `return_on_err_with_sql_bool!`).
func TestHandleSubscribeMulti_ParityUnknownTableRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const badSQL = "SELECT * FROM r"
	msg := &SubscribeMultiMsg{
		RequestID:    232,
		QueryID:      233,
		QueryStrings: []string{"SELECT * FROM t", badSQL},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `r`. If the table exists, it may be marked private., executing: `" + badSQL + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := exec.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when the FROM table is unknown")
	}
}

// TestHandleSubscribeSingle_ParityUnknownFieldRejectText pins the reference
// type-check rejection literal at
// reference/SpacetimeDB/crates/expr/src/errors.rs:11-13
// (`Unresolved::Var` = "`{0}` is not in scope"). Reference emit site
// `_type_expr` lib.rs:107: a missing column inside an existing relvar
// surfaces as `Unresolved::var(&field)`. SubscribeSingle compile-origin
// wraps with `DBError::WithSql` (error.rs:140).
func TestHandleSubscribeSingle_ParityUnknownFieldRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM t WHERE t.missing_col = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   240,
		QueryID:     241,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing_col` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a WHERE column is unknown")
	}
}

// TestHandleSubscribeMulti_ParityUnknownFieldRejectText pins the same
// `Unresolved::Var` literal on the SubscribeMulti admission surface.
func TestHandleSubscribeMulti_ParityUnknownFieldRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const badSQL = "SELECT * FROM t WHERE t.missing_col = 1"
	msg := &SubscribeMultiMsg{
		RequestID:    242,
		QueryID:      243,
		QueryStrings: []string{"SELECT * FROM t", badSQL},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing_col` is not in scope, executing: `" + badSQL + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := exec.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a WHERE column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityAggregateReturnTypeRejectText pins the
// reference `Unsupported::ReturnType` literal at
// reference/SpacetimeDB/crates/expr/src/errors.rs:47 ("Column projections
// are not supported in subscriptions; Subscriptions must return a table
// type"). Reference emit site expr/src/check.rs:174 via
// `expect_table_type` on the `parse_and_type_sub` path: aggregate
// (ProjectList::Agg) and column-list (ProjectList::List) projections both
// fall through to the unified literal on the v1 subscribe surface.
// SubscribeSingle wraps the inner text with `DBError::WithSql`.
func TestHandleSubscribeSingle_ParityAggregateReturnTypeRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT COUNT(*) AS n FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   250,
		QueryID:     251,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Column projections are not supported in subscriptions; Subscriptions must return a table type, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on aggregate projection in subscription")
	}
}

// TestHandleSubscribeSingle_ParityColumnListReturnTypeRejectText pins the
// same reference `Unsupported::ReturnType` literal onto the column-list
// projection path: `ProjectList::List` in reference expr/src/check.rs:174
// likewise fails `expect_table_type` and emits the unified subscription
// literal.
func TestHandleSubscribeSingle_ParityColumnListReturnTypeRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT u32 AS n FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   252,
		QueryID:     253,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Column projections are not supported in subscriptions; Subscriptions must return a table type, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on column-list projection in subscription")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarProjectionColumnRejectText pins
// reference `Unresolved::Var` (errors.rs:11-13, "`{name}` is not in scope")
// for a SubscribeSingle column-list projection where the named column does
// not exist on the FROM-clause table. Reference path: `type_proj::Exprs`
// (check.rs:67-80) walks each projection element through `type_expr` BEFORE
// `expect_table_type` runs the `Unsupported::ReturnType` check at
// check.rs:174 — so a missing-column projection emits `Unresolved::Var`,
// not the column-projection-not-supported literal. SubscribeSingle wraps
// compile errors with `DBError::WithSql`.
func TestHandleSubscribeSingle_ParityUnresolvedVarProjectionColumnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT missing FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   256,
		QueryID:     257,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a projection column is unknown")
	}
}

// TestHandleSubscribeMulti_ParityUnresolvedVarProjectionColumnRejectText
// pins the same `Unresolved::Var` literal on the SubscribeMulti admission
// surface for a column-list projection naming a missing column.
func TestHandleSubscribeMulti_ParityUnresolvedVarProjectionColumnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	exec := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const badSQL = "SELECT missing FROM t"
	msg := &SubscribeMultiMsg{
		RequestID:    258,
		QueryID:      259,
		QueryStrings: []string{"SELECT * FROM t", badSQL},
	}
	handleSubscribeMulti(context.Background(), conn, msg, exec, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + badSQL + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := exec.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a projection column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityBoolLiteralOnIntegerColumnRejectText pins
// the reference `UnexpectedType` literal from
// reference/SpacetimeDB/crates/expr/src/errors.rs:100 (via the emit site at
// lib.rs:94 for a bool literal in a non-bool column) onto the
// SubscribeSingle admission surface. SubscribeSingle wraps compile errors
// with `DBError::WithSql` (module_subscription_actor.rs:643 via
// `return_on_err_with_sql_bool!`), so the client sees the
// `, executing: `{sql}“ suffix.
func TestHandleSubscribeSingle_ParityBoolLiteralOnIntegerColumnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM t WHERE u32 = TRUE"
	msg := &SubscribeSingleMsg{
		RequestID:   254,
		QueryID:     255,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Unexpected type: (expected) Bool != U32 (inferred), executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a bool literal targets an integer column")
	}
}

// TestHandleSubscribeSingle_ParityIntOverflowOnUint8RejectText pins the
// reference `InvalidLiteral` literal from
// reference/SpacetimeDB/crates/expr/src/errors.rs:108 (emitted at lib.rs:99
// when `parse(v, ty)` fails) onto the SubscribeSingle admission surface.
// SubscribeSingle wraps compile errors with `DBError::WithSql`
// (module_subscription_actor.rs:643 via `return_on_err_with_sql_bool!`).
// Scope: plain integer literal — scientific-notation parity is deferred.
func TestHandleSubscribeSingle_ParityIntOverflowOnUint8RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)

	const sqlText = "SELECT * FROM t WHERE u8 = 1000"
	msg := &SubscribeSingleMsg{
		RequestID:   256,
		QueryID:     257,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "The literal expression `1000` cannot be parsed as type `U8`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when an integer literal overflows an unsigned column")
	}
}

// TestHandleSubscribeSingle_ParityFloatLiteralOnUint32RejectText pins the
// reference `InvalidLiteral` literal for a fractional LitFloat against an
// integer column on the SubscribeSingle admission surface. SubscribeSingle
// wraps compile errors with `DBError::WithSql`
// (module_subscription_actor.rs:643 via `return_on_err_with_sql_bool!`), so
// the pinned text carries the `, executing: ` suffix on top of the reference
// `"The literal expression `{literal}` cannot be parsed as type `{ty}`"`
// from errors.rs:84 / lib.rs:99. Scope mirrors the OneOff pin: plain `1.3`
// whose canonical FormatFloat rendering matches the original token — source-
// text preservation for round-trip-lossy forms is a separate slice.
func TestHandleSubscribeSingle_ParityFloatLiteralOnUint32RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM t WHERE u32 = 1.3"
	msg := &SubscribeSingleMsg{
		RequestID:   258,
		QueryID:     259,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "The literal expression `1.3` cannot be parsed as type `U32`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a float literal targets an integer column")
	}
}

// TestHandleSubscribeSingle_ParityNonBoolLiteralOnBoolRejectText pins the
// reference `InvalidLiteral` literal for non-Bool primitive literals
// targeted at a Bool column on the SubscribeSingle admission surface.
// Reference catch-all `bail!` on parse(v, Bool) folds into
// `InvalidLiteral::new(v.into_string(), Bool)` at lib.rs:99 (errors.rs:84);
// SubscribeSingle wraps compile errors with `DBError::WithSql` so the pin
// carries the `, executing: ` suffix. LitBytes deferred (no preserved
// source text).
func TestHandleSubscribeSingle_ParityNonBoolLiteralOnBoolRejectText(t *testing.T) {
	cases := []struct {
		name        string
		queryString string
		wantLit     string
	}{
		{"LitInt", "SELECT * FROM t WHERE b = 1", "1"},
		{"LitFloat", "SELECT * FROM t WHERE b = 1.3", "1.3"},
		{"LitString", "SELECT * FROM t WHERE b = 'foo'", "foo"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "b", Type: schema.KindBool},
			)

			msg := &SubscribeSingleMsg{
				RequestID:   uint32(260 + i*2),
				QueryID:     uint32(261 + i*2),
				QueryString: tc.queryString,
			}
			handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

			tag, decoded := drainServerMsgEventually(t, conn)
			if tag != TagSubscriptionError {
				t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
			}
			se := decoded.(SubscriptionError)
			want := "The literal expression `" + tc.wantLit + "` cannot be parsed as type `Bool`, executing: `" + tc.queryString + "`"
			if se.Error != want {
				t.Fatalf("Error = %q, want %q", se.Error, want)
			}
			if req := executor.getRegisterSetReq(); req != nil {
				t.Error("executor should not be called when a non-Bool literal targets a Bool column")
			}
		})
	}
}

// TestHandleSubscribeSingle_ParityDuplicateJoinAliasRejectText pins the
// reference `DuplicateName` literal for an explicitly-aliased join where
// both sides share the same alias. Reference path: `type_from`
// (lib.rs:88-89) emits `DuplicateName(alias)` after seeing the alias
// inserted twice into `Relvars`. SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityDuplicateJoinAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT dup.* FROM t AS dup JOIN s AS dup ON dup.u32 = dup.u32"
	msg := &SubscribeSingleMsg{
		RequestID:   400,
		QueryID:     401,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Duplicate name `dup`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a join uses duplicate aliases")
	}
}

// TestHandleSubscribeSingle_ParityDuplicateSelfJoinRejectText pins the
// `DuplicateName` literal for an unaliased self-join — the right side's
// derived alias collides with the left side's base table name `t`.
func TestHandleSubscribeSingle_ParityDuplicateSelfJoinRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN t"
	msg := &SubscribeSingleMsg{
		RequestID:   402,
		QueryID:     403,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Duplicate name `t`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on an unaliased self-join")
	}
}

// TestHandleSubscribeSingle_ParityJoinColumnKindMismatchRejectText pins
// the reference `UnexpectedType` literal for an ON binop whose two field
// references resolve to different algebraic kinds. SubscribeSingle wraps
// with DBError::WithSql.
func TestHandleSubscribeSingle_ParityJoinColumnKindMismatchRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "name", Type: schema.KindString}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.u32 = s.name"
	msg := &SubscribeSingleMsg{
		RequestID:   404,
		QueryID:     405,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Unexpected type: (expected) String != U32 (inferred), executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called on a join column kind mismatch")
	}
}

// TestHandleSubscribeSingle_ParityJoinArrayColumnInvalidOpRejectText pins
// the reference `InvalidOp` literal for an ON binop comparing two
// Array<…> columns. SubscribeSingle wraps with DBError::WithSql. Schema
// uses a hand-built `mockSchemaLookup` because `schema.NewBuilder`
// rejects `KindArrayString` as a column-storage kind.
func TestHandleSubscribeSingle_ParityJoinArrayColumnInvalidOpRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "arr", Type: schema.KindArrayString}}}
	sTS := &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{{Index: 0, Name: "arr", Type: schema.KindArrayString}}}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: tTS},
		"s": {id: 2, schema: sTS},
	}}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.arr = s.arr"
	msg := &SubscribeSingleMsg{
		RequestID:   406,
		QueryID:     407,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Invalid binary operator `=` for type `Array<String>`, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when ON compares Array columns")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarUnqualifiedWhereRejectText
// pins the reference `Unresolved::Var` literal for an unqualified
// single-table WHERE column whose name does not exist on the relvar.
// SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnresolvedVarUnqualifiedWhereRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM t WHERE missing = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   408,
		QueryID:     409,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a WHERE column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarJoinOnMissingRejectText
// pins the reference `Unresolved::Var` literal for an unknown JOIN ON
// equality operand. SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnresolvedVarJoinOnMissingRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.missing = s.id"
	msg := &SubscribeSingleMsg{
		RequestID:   410,
		QueryID:     411,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a JOIN ON column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarJoinWhereQualifiedMissingRejectText
// pins the reference `Unresolved::Var` literal for a qualified WHERE
// column on the right side of a join whose field does not exist.
// SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnresolvedVarJoinWhereQualifiedMissingRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "t_id", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.id = s.t_id WHERE s.missing = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   412,
		QueryID:     413,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a join WHERE column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarBaseTableAfterAliasRejectText
// pins the reference `Unresolved::Var` literal for a WHERE column
// qualified by the base table name AFTER an `AS` alias has been declared
// on the FROM relvar. Reference `_type_expr` (lib.rs:103) emits
// `Unresolved::var(&table)` when the qualifier name is absent from
// `Relvars`. SubscribeSingle wraps compile errors with `DBError::WithSql`.
func TestHandleSubscribeSingle_ParityUnresolvedVarBaseTableAfterAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM t AS r WHERE t.u32 = 5"
	msg := &SubscribeSingleMsg{
		RequestID:   414,
		QueryID:     415,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`t` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a WHERE qualifier is out of scope")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarBareJoinWildcardOnMissingRejectText
// pins reference `type_from` ordering: the JOIN ON expression types
// before `type_proj` runs the bare-wildcard rejection. SubscribeSingle
// wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnresolvedVarBareJoinWildcardOnMissingRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT * FROM t JOIN s ON t.missing = s.id"
	msg := &SubscribeSingleMsg{
		RequestID:   418,
		QueryID:     419,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when JOIN ON column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarJoinOnMissingNotHiddenByWhereFalseRejectText
// pins the reference order in which `type_from` types the ON expression
// before the WHERE predicate is folded. SubscribeSingle wraps with
// DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnresolvedVarJoinOnMissingNotHiddenByWhereFalseRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.missing = s.id WHERE FALSE"
	msg := &SubscribeSingleMsg{
		RequestID:   420,
		QueryID:     421,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError) — FALSE-WHERE pruning must not bypass ON resolution", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when JOIN ON column is unknown under WHERE FALSE")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarWherePrecedesProjectionRejectText
// pins the reference type-checker order: `type_select` (WHERE) runs
// before `type_proj` (projection columns). Reference path:
// `SubChecker::type_set` (check.rs:139-146) computes
// `type_proj(type_select(input, expr, vars)?, project, vars)`.
// SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnresolvedVarWherePrecedesProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT missing FROM t WHERE other_missing = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   416,
		QueryID:     417,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`other_missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a WHERE column is unknown")
	}
}

// TestHandleSubscribeSingle_ParityBooleanConstantWhereDoesNotMaskBranchErrors
// pins reference `_type_expr` order for logical WHERE expressions on the
// SubscribeSingle WithSql-wrapped surface: both operands are typed before
// Bool operators are lowered.
func TestHandleSubscribeSingle_ParityBooleanConstantWhereDoesNotMaskBranchErrors(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"FalseAndMissing", "SELECT * FROM t WHERE FALSE AND missing = 1", "`missing` is not in scope"},
		{"TrueOrMissing", "SELECT * FROM t WHERE TRUE OR missing = 1", "`missing` is not in scope"},
		{"FalseAndInvalidLiteral", "SELECT * FROM t WHERE FALSE AND u32 = 1.5", "The literal expression `1.5` cannot be parsed as type `U32`"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
			)

			msg := &SubscribeSingleMsg{
				RequestID:   uint32(500 + i*2),
				QueryID:     uint32(501 + i*2),
				QueryString: tc.sql,
			}
			handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

			tag, decoded := drainServerMsgEventually(t, conn)
			if tag != TagSubscriptionError {
				t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
			}
			se := decoded.(SubscriptionError)
			want := tc.want + ", executing: `" + tc.sql + "`"
			if se.Error != want {
				t.Fatalf("Error = %q, want %q", se.Error, want)
			}
			if req := executor.getRegisterSetReq(); req != nil {
				t.Error("executor should not be called when a logical branch fails type-checking")
			}
		})
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarQualifiedProjectionQualifierRejectText
// pins reference `type_proj::Exprs` `Unresolved::var(&table)` emit on
// the SubscribeSingle WithSql-wrapped surface.
func TestHandleSubscribeSingle_ParityUnresolvedVarQualifiedProjectionQualifierRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT x.u32 FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   422,
		QueryID:     423,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`x` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a projection qualifier is out of scope")
	}
}

// TestHandleSubscribeSingle_ParityUnresolvedVarQualifiedWildcardQualifierRejectText
// pins reference `type_proj` `Project::Star(Some(var))`
// `Unresolved::var(&var)` emit on the SubscribeSingle WithSql-wrapped
// surface.
func TestHandleSubscribeSingle_ParityUnresolvedVarQualifiedWildcardQualifierRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT x.* FROM t"
	msg := &SubscribeSingleMsg{
		RequestID:   424,
		QueryID:     425,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "`x` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when a wildcard projection qualifier is out of scope")
	}
}

// TestHandleSubscribeSingle_ParityMissingLeftTablePrecedesDuplicateJoinAliasRejectText
// pins reference `type_from` ordering: left-relvar resolution precedes
// duplicate-alias detection. SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityMissingLeftTablePrecedesDuplicateJoinAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT dup.* FROM missing AS dup JOIN s AS dup ON dup.id = dup.id"
	msg := &SubscribeSingleMsg{
		RequestID:   426,
		QueryID:     427,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when the left join table is missing")
	}
}

// TestHandleSubscribeSingle_ParityUnqualifiedNamesProjectionRejectText
// pins the reference `SqlUnsupported::UnqualifiedNames` literal for an
// unqualified projection column inside a JOIN scope. SubscribeSingle
// wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnqualifiedNamesProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT id FROM t JOIN s ON t.id = s.id"
	msg := &SubscribeSingleMsg{
		RequestID:   428,
		QueryID:     429,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Names must be qualified when using joins, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when projection column is unqualified in join scope")
	}
}

// TestHandleSubscribeSingle_ParityUnqualifiedNamesWhereRejectText pins
// the reference `SqlUnsupported::UnqualifiedNames` literal for an
// unqualified WHERE column inside a JOIN scope. SubscribeSingle wraps
// with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnqualifiedNamesWhereRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.id = s.id WHERE id = 7"
	msg := &SubscribeSingleMsg{
		RequestID:   430,
		QueryID:     431,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Names must be qualified when using joins, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when WHERE column is unqualified in join scope")
	}
}

// TestHandleSubscribeSingle_ParityUnqualifiedNamesJoinOnRejectText pins
// the reference `SqlUnsupported::UnqualifiedNames` literal for an
// unqualified JOIN ON operand. SubscribeSingle wraps with DBError::WithSql.
func TestHandleSubscribeSingle_ParityUnqualifiedNamesJoinOnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	const sqlText = "SELECT t.* FROM t JOIN s ON id = s.id"
	msg := &SubscribeSingleMsg{
		RequestID:   432,
		QueryID:     433,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Names must be qualified when using joins, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when JOIN ON operand is unqualified")
	}
}

// TestHandleSubscribeSingle_ParitySenderParameterCaseSensitiveRejectText
// pins reference `parse_expr` (sql-parser/src/parser/mod.rs:223)
// byte-equal `":sender"` admission. Any other casing (e.g. `:SENDER`)
// falls through to `SqlUnsupported::Expr` rendered as
// `Unsupported expression: {expr}`. SubscribeSingle wraps with
// DBError::WithSql.
func TestHandleSubscribeSingle_ParitySenderParameterCaseSensitiveRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM s WHERE id = :SENDER"
	msg := &SubscribeSingleMsg{
		RequestID:   434,
		QueryID:     435,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	want := "Unsupported expression: :SENDER, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when :sender placeholder is byte-mismatched")
	}
}

// TestHandleSubscribeSingle_ParityProjectionGuardYieldsToTableNotFound pins
// reference `SubChecker::type_set` (check.rs:137-156) ordering: `type_from`
// runs BEFORE `expect_table_type` (check.rs:168-176), so a missing FROM
// table emits the no-such-table text instead of the
// `Unsupported::ReturnType` projection-return guard.
func TestHandleSubscribeSingle_ParityProjectionGuardYieldsToTableNotFound(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT u32 FROM missing_table"
	msg := &SubscribeSingleMsg{
		RequestID:   436,
		QueryID:     437,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing_table`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q (table-not-found must precede table-type return guard)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when FROM table is missing")
	}
}

// TestHandleSubscribeSingle_ParityProjectionGuardYieldsToWhereResolution
// pins reference `SubChecker::type_set` (check.rs:137-156) ordering:
// `type_select` runs BEFORE `expect_table_type` (check.rs:168-176), so a
// missing WHERE column emits `Unresolved::Var` instead of the
// `Unsupported::ReturnType` projection-return guard.
func TestHandleSubscribeSingle_ParityProjectionGuardYieldsToWhereResolution(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT u32 FROM t WHERE missing = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   438,
		QueryID:     439,
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
		t.Fatalf("Error = %q, want %q (WHERE resolution must precede table-type return guard)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when WHERE column is unresolved")
	}
}

// TestHandleSubscribeSingle_ParityAggregateGuardYieldsToTableNotFound pins
// the same `SubChecker::type_set` ordering on the aggregate path:
// `type_from` precedes the `Unsupported::ReturnType` guard for
// `ProjectList::Agg`. Locks the prior early-aggregate guard reorder so
// `SELECT COUNT(*) FROM missing_table` emits the no-such-table text.
func TestHandleSubscribeSingle_ParityAggregateGuardYieldsToTableNotFound(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT COUNT(*) AS n FROM missing_table"
	msg := &SubscribeSingleMsg{
		RequestID:   440,
		QueryID:     441,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing_table`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q (aggregate path: table-not-found must precede table-type return guard)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when aggregate FROM table is missing")
	}
}

// TestHandleSubscribeSingle_ParityAggregateGuardYieldsToWhereResolution
// pins the aggregate-path WHERE-precedes-return-guard ordering.
func TestHandleSubscribeSingle_ParityAggregateGuardYieldsToWhereResolution(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)

	const sqlText = "SELECT COUNT(*) AS n FROM t WHERE missing = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   442,
		QueryID:     443,
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
		t.Fatalf("Error = %q, want %q (aggregate path: WHERE resolution must precede table-type return guard)", se.Error, want)
	}
	if req := executor.getRegisterSetReq(); req != nil {
		t.Error("executor should not be called when aggregate WHERE column is unresolved")
	}
}
