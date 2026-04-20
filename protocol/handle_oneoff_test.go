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

func (s *mockSnapshot) RowCount(_ schema.TableID) int { return 0 }

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
