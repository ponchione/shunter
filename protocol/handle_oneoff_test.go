package protocol

import (
	"bytes"
	"context"
	"iter"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// buildUint256From1e40 materializes the 10^40 Uint256 value via the same
// coerce path the parser + admission use, so test expectations and the
// stored row stay in lockstep if the word layout ever changes.
func buildUint256From1e40() (types.Value, error) {
	big1e40, _ := new(big.Int).SetString("10000000000000000000000000000000000000000", 10)
	return sql.Coerce(sql.Literal{Kind: sql.LitBigInt, Big: big1e40}, schema.KindUint256)
}

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
// OneOffQueryResponse. Fatals if nothing is queued or decode fails.
func drainOneOff(t *testing.T, conn *Conn) OneOffQueryResponse {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		_, msg, err := DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("DecodeServerMessage: %v", err)
		}
		result, ok := msg.(OneOffQueryResponse)
		if !ok {
			t.Fatalf("expected OneOffQueryResponse, got %T", msg)
		}
		return result
	default:
		t.Fatal("expected a frame on OutboundCh, got none")
		return OneOffQueryResponse{} // unreachable
	}
}

// firstTableRows returns the Rows payload of the first OneOffTable, or
// nil if Tables is empty. Most Phase 2 Slice 1c handler tests populate
// exactly one table matching `compiled.TableName`.
func firstTableRows(r OneOffQueryResponse) []byte {
	if len(r.Tables) == 0 {
		return nil
	}
	return r.Tables[0].Rows
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

func assertProductRowsEqual(t *testing.T, got, want []types.ProductValue) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if !got[i].Equal(want[i]) {
			t.Fatalf("row[%d] = %v, want %v", i, got[i], want[i])
		}
	}
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}

	pvs := decodeRows(t, firstTableRows(result), ts)
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}

	pvs := decodeRows(t, firstTableRows(result), ts)
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_SameTableAndChildOrderReturnsSameRows(t *testing.T) {
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
				{types.NewUint32(3), types.NewString("alice")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE id = 1 AND name = 'alice'",
		"SELECT * FROM users WHERE name = 'alice' AND id = 1",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x30 + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableOrChildOrderReturnsSameRows(t *testing.T) {
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
	queries := []string{
		"SELECT * FROM users WHERE id = 1 OR id = 2",
		"SELECT * FROM users WHERE id = 2 OR id = 1",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x32 + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableGroupedAndReturnsSameRows(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
			{Index: 2, Name: "age", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewString("alice"), types.NewUint32(30)},
				{types.NewUint32(1), types.NewString("alice"), types.NewUint32(31)},
				{types.NewUint32(2), types.NewString("alice"), types.NewUint32(30)},
				{types.NewUint32(1), types.NewString("bob"), types.NewUint32(30)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE (id = 1 AND name = 'alice') AND age = 30",
		"SELECT * FROM users WHERE id = 1 AND (name = 'alice' AND age = 30)",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x34 + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableGroupedOrReturnsSameRows(t *testing.T) {
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
				{types.NewUint32(4), types.NewString("dave")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE (id = 1 OR id = 2) OR id = 3",
		"SELECT * FROM users WHERE id = 1 OR (id = 2 OR id = 3)",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x36 + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableDuplicateAndReturnsSameRows(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewString("alice")},
			{types.NewUint32(2), types.NewString("bob")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE id = 1",
		"SELECT * FROM users WHERE id = 1 AND id = 1",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x38 + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableDuplicateOrReturnsSameRows(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewString("alice")},
			{types.NewUint32(2), types.NewString("bob")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE id = 1",
		"SELECT * FROM users WHERE id = 1 OR id = 1",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x3a + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableOrAbsorptionReturnsSameRows(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewString("alice")},
			{types.NewUint32(1), types.NewString("bob")},
			{types.NewUint32(2), types.NewString("alice")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE id = 1",
		"SELECT * FROM users WHERE id = 1 OR (id = 1 AND name = 'alice')",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x3c + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_SameTableAndAbsorptionReturnsSameRows(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewString("alice")},
			{types.NewUint32(1), types.NewString("bob")},
			{types.NewUint32(2), types.NewString("alice")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		"SELECT * FROM users WHERE id = 1",
		"SELECT * FROM users WHERE id = 1 AND (id = 1 OR name = 'alice')",
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x3e + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ts))
	}

	assertProductRowsEqual(t, rows[0], rows[1])
}

func TestHandleOneOffQuery_OrComparisonWithAliasAndHexBytes(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "s",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "bytes", Type: schema.KindBytes},
		},
	}
	sl := newMockSchema("s", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewBytes([]byte{0xAB, 0xCD})},
				{types.NewUint32(2), types.NewBytes([]byte{0x00, 0x01})},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x19},
		QueryString: "SELECT * FROM s AS r WHERE r.bytes = 0xABCD OR bytes = X'ABCD'",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

func TestHandleOneOffQuery_OrComparisonWithAlias(t *testing.T) {
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
				{types.NewUint32(1), types.NewString("bob")},
				{types.NewUint32(2), types.NewString("alice")},
				{types.NewUint32(3), types.NewString("carol")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x19},
		QueryString: "SELECT item.* FROM users AS item WHERE item.id = 1 OR name = 'alice'",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[0][1].Equal(types.NewString("bob")) {
		t.Fatalf("first row = %v, want id=1 name=bob", pvs[0])
	}
	if !pvs[1][0].Equal(types.NewUint32(2)) || !pvs[1][1].Equal(types.NewString("alice")) {
		t.Fatalf("second row = %v, want id=2 name=alice", pvs[1])
	}
}

func TestHandleOneOffQuery_WhereTrueLiteralReturnsAllRows(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "flag", Type: schema.KindBool},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewBool(true)},
				{types.NewUint32(2), types.NewBool(false)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1a},
		QueryString: "SELECT * FROM t WHERE TRUE",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(2)) {
		t.Fatalf("unexpected ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_TrueAndComparisonMatchesComparison(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "flag", Type: schema.KindBool},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewBool(true)},
				{types.NewUint32(8), types.NewBool(false)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x2a},
		QueryString: "SELECT * FROM t WHERE TRUE AND id = 7",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(7)) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

func TestHandleOneOffQuery_TrueOrComparisonReturnsAllRows(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "flag", Type: schema.KindBool},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewBool(true)},
				{types.NewUint32(8), types.NewBool(false)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x2b},
		QueryString: "SELECT * FROM t WHERE TRUE OR id = 7",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
}

func TestHandleOneOffQuery_SQLWhereFalseReturnsNoRows(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "flag", Type: schema.KindBool},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewBool(true)},
				{types.NewUint32(8), types.NewBool(false)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x2c},
		QueryString: "SELECT * FROM t WHERE FALSE",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 0 {
		t.Fatalf("got %d rows, want 0", len(pvs))
	}
}

func TestHandleOneOffQuery_SQLWhereFalseOrComparisonReturnsComparisonRows(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "flag", Type: schema.KindBool},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewBool(true)},
				{types.NewUint32(8), types.NewBool(false)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x2d},
		QueryString: "SELECT * FROM t WHERE FALSE OR id = 7",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(7)) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

func TestHandleOneOffQuery_SQLWhereFalseAndComparisonReturnsNoRows(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "flag", Type: schema.KindBool},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewBool(true)},
				{types.NewUint32(8), types.NewBool(false)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x2e},
		QueryString: "SELECT * FROM t WHERE FALSE AND id = 7",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 0 {
		t.Fatalf("got %d rows, want 0", len(pvs))
	}
}

func TestHandleOneOffQuery_QuotedSpecialCharacterIdentifiers(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "Balance$",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "status", Type: schema.KindString},
		},
	}
	sl := newMockSchema("Balance$", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewString("open")},
				{types.NewUint32(8), types.NewString("closed")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1d},
		QueryString: `SELECT * FROM "Balance$" WHERE "id" = 7`,
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(7)) || !pvs[0][1].Equal(types.NewString("open")) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

func TestHandleOneOffQuery_QuotedReservedIdentifiers(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "Order",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "status", Type: schema.KindString},
		},
	}
	sl := newMockSchema("Order", 1, ts.Columns...)

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(7), types.NewString("open")},
				{types.NewUint32(8), types.NewString("closed")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1b},
		QueryString: `SELECT * FROM "Order" WHERE "id" = 7`,
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(7)) || !pvs[0][1].Equal(types.NewString("open")) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

func TestHandleOneOffQuery_JoinFilterOnLeftFloatColumn(t *testing.T) {
	conn := testConnDirect(nil)
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
	_, tReg, _ := eng.Registry().TableByName("t")
	_, sReg, _ := eng.Registry().TableByName("s")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: tReg.Columns}
	sl := registrySchemaLookup{reg: eng.Registry()}

	goodFloat, err := types.NewFloat32(0.1)
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	otherFloat, err := types.NewFloat32(0.2)
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			tReg.ID: {
				{types.NewUint32(1), types.NewUint32(10), goodFloat},
				{types.NewUint32(2), types.NewUint32(10), otherFloat},
			},
			sReg.ID: {
				{types.NewUint32(7), types.NewUint32(10)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1c},
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.f32 = 0.1",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[0][2].Equal(goodFloat) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

func TestHandleOneOffQuery_UnindexedJoinReturnsRows(t *testing.T) {
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
	_, ordersTS, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryTS, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			ordersTS.ID: {
				{types.NewUint32(1), types.NewUint32(100)},
				{types.NewUint32(2), types.NewUint32(101)},
			},
			inventoryTS.ID: {
				{types.NewUint32(100), types.NewUint32(9)},
				{types.NewUint32(101), types.NewUint32(10)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x19},
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (one-off joins do not require subscription indexes)", *result.Error)
	}
	if len(result.Tables) != 1 {
		t.Fatalf("Tables len = %d, want 1", len(result.Tables))
	}
	if result.Tables[0].TableName != "Orders" {
		t.Fatalf("TableName = %q, want Orders", result.Tables[0].TableName)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[0][1].Equal(types.NewUint32(100)) {
		t.Fatalf("row 0 = %v, want order 1/product 100", pvs[0])
	}
	if !pvs[1][0].Equal(types.NewUint32(2)) || !pvs[1][1].Equal(types.NewUint32(101)) {
		t.Fatalf("row 1 = %v, want order 2/product 101", pvs[1])
	}
}

func TestHandleOneOffQuery_CrossJoinWhereColumnEqualityReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
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
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, ok := eng.Registry().TableByName("t")
	if !ok {
		t.Fatal("t table missing from registry")
	}
	_, sReg, ok := eng.Registry().TableByName("s")
	if !ok {
		t.Fatal("s table missing from registry")
	}
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: tReg.Columns}
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			tReg.ID: {
				{types.NewUint32(1), types.NewUint32(10)},
				{types.NewUint32(2), types.NewUint32(20)},
				{types.NewUint32(3), types.NewUint32(20)},
			},
			sReg.ID: {
				{types.NewUint32(9), types.NewUint32(20)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1d},
		QueryString: "SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (one-off cross-join WHERE column equality is query-only accepted)", *result.Error)
	}
	if len(result.Tables) != 1 {
		t.Fatalf("Tables len = %d, want 1", len(result.Tables))
	}
	if result.Tables[0].TableName != "t" {
		t.Fatalf("TableName = %q, want t", result.Tables[0].TableName)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(2)) || !pvs[0][1].Equal(types.NewUint32(20)) {
		t.Fatalf("row 0 = %v, want t id=2/u32=20", pvs[0])
	}
	if !pvs[1][0].Equal(types.NewUint32(3)) || !pvs[1][1].Equal(types.NewUint32(20)) {
		t.Fatalf("row 1 = %v, want t id=3/u32=20", pvs[1])
	}
}

func TestHandleOneOffQuery_CrossJoinWhereColumnEqualityAndLiteralFilterReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
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
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, ok := eng.Registry().TableByName("t")
	if !ok {
		t.Fatal("t table missing from registry")
	}
	_, sReg, ok := eng.Registry().TableByName("s")
	if !ok {
		t.Fatal("s table missing from registry")
	}
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: tReg.Columns}
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			tReg.ID: {
				{types.NewUint32(1), types.NewUint32(10)},
				{types.NewUint32(2), types.NewUint32(20)},
				{types.NewUint32(3), types.NewUint32(30)},
			},
			sReg.ID: {
				{types.NewUint32(9), types.NewUint32(20), types.NewBool(false)},
				{types.NewUint32(10), types.NewUint32(30), types.NewBool(true)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x7d},
		QueryString: "SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (one-off cross-join WHERE equality plus filter is query-only accepted)", *result.Error)
	}
	if len(result.Tables) != 1 {
		t.Fatalf("Tables len = %d, want 1", len(result.Tables))
	}
	if result.Tables[0].TableName != "t" {
		t.Fatalf("TableName = %q, want t", result.Tables[0].TableName)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(3)) || !pvs[0][1].Equal(types.NewUint32(30)) {
		t.Fatalf("row 0 = %v, want t id=3/u32=30", pvs[0])
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
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

func TestHandleOneOffQuery_JoinFilterChildOrderReturnsSameRows(t *testing.T) {
	ordersTS := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "group_id", Type: schema.KindUint32},
		},
	}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "users",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "group_id", Type: schema.KindUint32},
		},
		Indexes: []schema.IndexDefinition{{Name: "idx_users_group_id", Columns: []string{"group_id"}}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "other",
		Columns: []schema.ColumnDefinition{
			{Name: "uid", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "flag", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, usersReg, ok := eng.Registry().TableByName("users")
	if !ok {
		t.Fatal("users table missing from registry")
	}
	_, otherReg, ok := eng.Registry().TableByName("other")
	if !ok {
		t.Fatal("other table missing from registry")
	}
	ordersTS.ID = usersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			usersReg.ID: {
				{types.NewUint32(1), types.NewUint32(100)},
				{types.NewUint32(2), types.NewUint32(101)},
			},
			otherReg.ID: {
				{types.NewUint32(100), types.NewUint32(1)},
				{types.NewUint32(101), types.NewUint32(1)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	queries := []string{
		`SELECT "users".* FROM "users" JOIN "other" ON "users"."group_id" = "other"."uid" WHERE (("users"."id" = 1) AND ("users"."id" > 0))`,
		`SELECT "users".* FROM "users" JOIN "other" ON "users"."group_id" = "other"."uid" WHERE (("users"."id" > 0) AND ("users"."id" = 1))`,
	}

	var rows [][]types.ProductValue
	for i, query := range queries {
		conn := testConnDirect(nil)
		msg := &OneOffQueryMsg{MessageID: []byte{0x28 + byte(i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q, want nil", query, *result.Error)
		}
		rows = append(rows, decodeRows(t, firstTableRows(result), ordersTS))
	}

	want := []types.ProductValue{{types.NewUint32(1), types.NewUint32(100)}}
	assertProductRowsEqual(t, rows[0], want)
	assertProductRowsEqual(t, rows[1], want)
}

func TestHandleOneOffQuery_JoinFilterTrueAndComparisonSucceeds(t *testing.T) {
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
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
		MessageID:   []byte{0x29},
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE TRUE AND product.quantity < 10",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected order ids returned: %v, %v", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_QuotedIdentifiersJoinProjectionOnLeftTable(t *testing.T) {
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
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
		MessageID:   []byte{0x31},
		QueryString: `SELECT "Orders".* FROM "Orders" JOIN "Inventory" ON "Orders"."product_id" = "Inventory"."id" WHERE "Inventory"."quantity" < 10`,
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
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

func TestHandleOneOffQuery_QuotedIdentifiersJoinProjectionWithParenthesizedConjunction(t *testing.T) {
	conn := testConnDirect(nil)
	usersTS := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		},
	}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "users",
		Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "other",
		Columns: []schema.ColumnDefinition{{Name: "uid", Type: schema.KindUint32, PrimaryKey: true}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, usersReg, ok := eng.Registry().TableByName("users")
	if !ok {
		t.Fatal("users table missing from registry")
	}
	_, otherReg, ok := eng.Registry().TableByName("other")
	if !ok {
		t.Fatal("other table missing from registry")
	}
	usersTS.ID = usersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			usersReg.ID: {
				{types.NewUint32(1)},
				{types.NewUint32(2)},
			},
			otherReg.ID: {
				{types.NewUint32(1)},
				{types.NewUint32(2)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x32},
		QueryString: `SELECT "users".* FROM "users" JOIN "other" ON "users"."id" = "other"."uid" WHERE (("users"."id" = 1) AND ("users"."id" > 0))`,
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), usersTS)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) {
		t.Fatalf("unexpected user id returned: %v", pvs[0][0])
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), inventoryTS)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3 multiplicity-preserving projected inventory rows", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(100)) || !pvs[1][0].Equal(types.NewUint32(100)) || !pvs[2][0].Equal(types.NewUint32(102)) {
		t.Fatalf("unexpected inventory ids returned: %v, %v, %v", pvs[0][0], pvs[1][0], pvs[2][0])
	}
	if !pvs[0][1].Equal(types.NewUint32(9)) || !pvs[1][1].Equal(types.NewUint32(9)) || !pvs[2][1].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected quantities returned: %v, %v, %v", pvs[0][1], pvs[1][1], pvs[2][1])
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), inventoryTS)
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
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 5 {
		t.Fatalf("got %d rows, want 5 (bag semantics across 2x2 + 1x1 self-join pairs)", len(pvs))
	}
}

func TestHandleOneOffQuery_AliasedSelfEquiJoinWithWhereAside(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3 (a.id=1 paired with every b row sharing u32=5)", len(pvs))
	}
	for i, pv := range pvs {
		if !pv[0].Equal(types.NewUint32(1)) {
			t.Fatalf("row %d id=%v, want id=1 on every multiplicity-expanded row", i, pv[0])
		}
	}
}

func TestHandleOneOffQuery_AliasedSelfEquiJoinWithWhereBside(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 3 {
		t.Fatalf("got %d rows, want 3 (every a-side row has a b partner with id=1 via u32=5)", len(pvs))
	}
}

func TestHandleOneOffQuery_AliasedSelfJoinFilterChildOrderVisibleRowsMatch(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	queries := []string{
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND a.id > 0",
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id > 0 AND a.id = 1",
	}
	decoded := make([][]types.ProductValue, 0, len(queries))
	for i, query := range queries {
		conn := testConnDirect(nil)
		stateAccess := &mockStateAccess{snap: snap}
		msg := &OneOffQueryMsg{MessageID: []byte{byte(0x23 + i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %d error = %q, want nil", i, *result.Error)
		}
		rows := decodeRows(t, firstTableRows(result), tTS)
		if len(rows) != 3 {
			t.Fatalf("query %d got %d rows, want 3 multiplicity-expanded rows", i, len(rows))
		}
		for j, row := range rows {
			if !row[0].Equal(types.NewUint32(1)) {
				t.Fatalf("query %d row %d id=%v, want id=1 on every emitted row", i, j, row[0])
			}
		}
		decoded = append(decoded, rows)
	}
	if len(decoded[0]) != len(decoded[1]) {
		t.Fatalf("row counts differ: %d vs %d", len(decoded[0]), len(decoded[1]))
	}
	for i := range decoded[0] {
		if len(decoded[0][i]) != len(decoded[1][i]) {
			t.Fatalf("row %d column counts differ: %d vs %d", i, len(decoded[0][i]), len(decoded[1][i]))
		}
		for j := range decoded[0][i] {
			if !decoded[0][i][j].Equal(decoded[1][i][j]) {
				t.Fatalf("row %d col %d differs: %v vs %v", i, j, decoded[0][i][j], decoded[1][i][j])
			}
		}
	}
}

func TestHandleOneOffQuery_AliasedSelfJoinFilterDuplicateLeafVisibleRowsMatch(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	queries := []string{
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1",
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND a.id = 1",
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 OR a.id = 1",
	}
	decoded := make([][]types.ProductValue, 0, len(queries))
	for i, query := range queries {
		conn := testConnDirect(nil)
		stateAccess := &mockStateAccess{snap: snap}
		msg := &OneOffQueryMsg{MessageID: []byte{byte(0x25 + i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %d error = %q, want nil", i, *result.Error)
		}
		rows := decodeRows(t, firstTableRows(result), tTS)
		if len(rows) != 3 {
			t.Fatalf("query %d got %d rows, want 3 multiplicity-expanded rows", i, len(rows))
		}
		for j, row := range rows {
			if !row[0].Equal(types.NewUint32(1)) {
				t.Fatalf("query %d row %d id=%v, want id=1 on every emitted row", i, j, row[0])
			}
		}
		decoded = append(decoded, rows)
	}
	if len(decoded[0]) != len(decoded[1]) || len(decoded[0]) != len(decoded[2]) {
		t.Fatalf("row counts differ: %d vs %d vs %d", len(decoded[0]), len(decoded[1]), len(decoded[2]))
	}
	for i := range decoded[0] {
		if len(decoded[0][i]) != len(decoded[1][i]) || len(decoded[0][i]) != len(decoded[2][i]) {
			t.Fatalf("row %d column counts differ: %d vs %d vs %d", i, len(decoded[0][i]), len(decoded[1][i]), len(decoded[2][i]))
		}
		for j := range decoded[0][i] {
			if !decoded[0][i][j].Equal(decoded[1][i][j]) {
				t.Fatalf("row %d col %d differs between base and duplicate-and: %v vs %v", i, j, decoded[0][i][j], decoded[1][i][j])
			}
			if !decoded[0][i][j].Equal(decoded[2][i][j]) {
				t.Fatalf("row %d col %d differs between base and duplicate-or: %v vs %v", i, j, decoded[0][i][j], decoded[2][i][j])
			}
		}
	}
}

func TestHandleOneOffQuery_AliasedSelfJoinFilterAbsorptionVisibleRowsMatch(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	queries := []string{
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1",
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 OR (a.id = 1 AND a.id > 0)",
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND (a.id = 1 OR a.id > 0)",
	}
	decoded := make([][]types.ProductValue, 0, len(queries))
	for i, query := range queries {
		conn := testConnDirect(nil)
		stateAccess := &mockStateAccess{snap: snap}
		msg := &OneOffQueryMsg{MessageID: []byte{byte(0x28 + i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %d error = %q, want nil", i, *result.Error)
		}
		rows := decodeRows(t, firstTableRows(result), tTS)
		if len(rows) != 3 {
			t.Fatalf("query %d got %d rows, want 3 multiplicity-expanded rows", i, len(rows))
		}
		for j, row := range rows {
			if !row[0].Equal(types.NewUint32(1)) {
				t.Fatalf("query %d row %d id=%v, want id=1 on every emitted row", i, j, row[0])
			}
		}
		decoded = append(decoded, rows)
	}
	if len(decoded[0]) != len(decoded[1]) || len(decoded[0]) != len(decoded[2]) {
		t.Fatalf("row counts differ: %d vs %d vs %d", len(decoded[0]), len(decoded[1]), len(decoded[2]))
	}
	for i := range decoded[0] {
		if len(decoded[0][i]) != len(decoded[1][i]) || len(decoded[0][i]) != len(decoded[2][i]) {
			t.Fatalf("row %d column counts differ: %d vs %d vs %d", i, len(decoded[0][i]), len(decoded[1][i]), len(decoded[2][i]))
		}
		for j := range decoded[0][i] {
			if !decoded[0][i][j].Equal(decoded[1][i][j]) {
				t.Fatalf("row %d col %d differs between base and absorbed-or: %v vs %v", i, j, decoded[0][i][j], decoded[1][i][j])
			}
			if !decoded[0][i][j].Equal(decoded[2][i][j]) {
				t.Fatalf("row %d col %d differs between base and absorbed-and: %v vs %v", i, j, decoded[0][i][j], decoded[2][i][j])
			}
		}
	}
}

func TestHandleOneOffQuery_AliasedSelfJoinFilterAssociativeGroupingVisibleRowsMatch(t *testing.T) {
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(5)},
		},
	}}
	queries := []string{
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE (a.id = 1 AND a.id > 0) AND a.id < 2",
		"SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND (a.id > 0 AND a.id < 2)",
	}
	decoded := make([][]types.ProductValue, 0, len(queries))
	for i, query := range queries {
		conn := testConnDirect(nil)
		stateAccess := &mockStateAccess{snap: snap}
		msg := &OneOffQueryMsg{MessageID: []byte{byte(0x25 + i)}, QueryString: query}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %d error = %q, want nil", i, *result.Error)
		}
		rows := decodeRows(t, firstTableRows(result), tTS)
		if len(rows) != 3 {
			t.Fatalf("query %d got %d rows, want 3 multiplicity-expanded rows", i, len(rows))
		}
		for j, row := range rows {
			if !row[0].Equal(types.NewUint32(1)) {
				t.Fatalf("query %d row %d id=%v, want id=1 on every emitted row", i, j, row[0])
			}
		}
		decoded = append(decoded, rows)
	}
	if len(decoded[0]) != len(decoded[1]) {
		t.Fatalf("row counts differ: %d vs %d", len(decoded[0]), len(decoded[1]))
	}
	for i := range decoded[0] {
		if len(decoded[0][i]) != len(decoded[1][i]) {
			t.Fatalf("row %d column counts differ: %d vs %d", i, len(decoded[0][i]), len(decoded[1][i]))
		}
		for j := range decoded[0][i] {
			if !decoded[0][i][j].Equal(decoded[1][i][j]) {
				t.Fatalf("row %d col %d differs: %v vs %v", i, j, decoded[0][i][j], decoded[1][i][j])
			}
		}
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
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, _ := eng.Registry().TableByName("t")
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
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
	_, tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {{types.NewUint32(1)}, {types.NewUint32(2)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1d}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 4 {
		t.Fatalf("got %d rows, want 4 cartesian pairs projected onto a.*", len(pvs))
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
	_, tReg, _ := eng.Registry().TableByName("t")
	tTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{tReg.ID: nil}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1e}, QueryString: "SELECT a.* FROM t AS a JOIN t AS b"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
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
			if result.Error == nil || *result.Error == "" {
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
	_, ordersReg, _ := eng.Registry().TableByName("Orders")
	_, inventoryReg, _ := eng.Registry().TableByName("Inventory")
	ordersTS.ID = ordersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID:    {{types.NewUint32(1)}, {types.NewUint32(2)}},
		inventoryReg.ID: {{types.NewUint32(10)}, {types.NewUint32(11)}, {types.NewUint32(12)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x1c}, QueryString: "SELECT o.* FROM Orders o JOIN Inventory product"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
	if len(pvs) != 6 {
		t.Fatalf("got %d rows, want 6 cartesian pairs projected onto Orders", len(pvs))
	}
}

func TestHandleOneOffQuery_CrossJoinProjectsRight(t *testing.T) {
	conn := testConnDirect(nil)
	inventoryTS := &schema.TableSchema{ID: 2, Name: "Inventory", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "Orders", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}}})
	b.TableDef(schema.TableDefinition{Name: "Inventory", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, ordersReg, _ := eng.Registry().TableByName("Orders")
	_, inventoryReg, _ := eng.Registry().TableByName("Inventory")
	inventoryTS.ID = inventoryReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID:    {{types.NewUint32(1)}, {types.NewUint32(2)}, {types.NewUint32(3)}},
		inventoryReg.ID: {{types.NewUint32(10)}, {types.NewUint32(11)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x62}, QueryString: "SELECT product.* FROM Orders o JOIN Inventory product"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), inventoryTS)
	if len(pvs) != 6 {
		t.Fatalf("got %d rows, want 6 cartesian pairs projected onto Inventory", len(pvs))
	}
	for i := 0; i < 3; i++ {
		if !pvs[i][0].Equal(types.NewUint32(10)) {
			t.Fatalf("row %d = %v, want inventory row 10 repeated first", i, pvs[i])
		}
	}
	for i := 3; i < 6; i++ {
		if !pvs[i][0].Equal(types.NewUint32(11)) {
			t.Fatalf("row %d = %v, want inventory row 11 repeated second", i, pvs[i])
		}
	}
}

func TestHandleOneOffQuery_JoinProjectionOnRightTablePreservesMultiplicity(t *testing.T) {
	conn := testConnDirect(nil)
	inventoryTS := &schema.TableSchema{ID: 2, Name: "Inventory", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "quantity", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "Orders", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "product_id", Type: schema.KindUint32}}})
	b.TableDef(schema.TableDefinition{Name: "Inventory", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "quantity", Type: schema.KindUint32}}})
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
	inventoryTS.ID = inventoryReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID:    {{types.NewUint32(1), types.NewUint32(100)}, {types.NewUint32(2), types.NewUint32(100)}},
		inventoryReg.ID: {{types.NewUint32(100), types.NewUint32(9)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{MessageID: []byte{0x61}, QueryString: "SELECT product.* FROM Orders o JOIN Inventory product ON o.product_id = product.id"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), inventoryTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2 duplicate projected inventory rows", len(pvs))
	}
	for i, pv := range pvs {
		if !pv[0].Equal(types.NewUint32(100)) || !pv[1].Equal(types.NewUint32(9)) {
			t.Fatalf("row %d = %v, want duplicated inventory row [100,9]", i, pv)
		}
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}

	rawRows, err := DecodeRowList(firstTableRows(result))
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
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}

	pvs := decodeRows(t, firstTableRows(result), ts)
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
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// Reference expr type-check coverage accepts `:sender` on both identity
// and byte-array columns (`crates/expr/src/check.rs` lines 434-440). Pin the
// one-off path end-to-end: the scan must select only the row whose bytes
// column equals the caller's 32-byte identity payload. No wire parameter
// substitution: :sender is resolved at compile time against conn.Identity.
func TestHandleOneOffQuery_SenderParameterOnBytesColumn(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{7, 8, 9}
	ts := &schema.TableSchema{
		ID:   1,
		Name: "s",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "bytes", Type: schema.KindBytes},
		},
	}
	sl := newMockSchema("s", 1, ts.Columns...)

	callerBytes := make([]byte, 32)
	copy(callerBytes, conn.Identity[:])
	otherBytes := make([]byte, 32)
	otherBytes[0] = 0xFF
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewBytes(callerBytes)},
				{types.NewUint32(2), types.NewBytes(otherBytes)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x60},
		QueryString: "SELECT * FROM s WHERE bytes = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) {
		t.Errorf("row[0].id = %v, want Uint32(1)", pvs[0][0])
	}
}

func TestHandleOneOffQuery_SenderParameterOnIdentityColumn(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{3, 1, 4, 1, 5, 9}
	ts := &schema.TableSchema{
		ID:   1,
		Name: "s",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindBytes},
			{Index: 1, Name: "label", Type: schema.KindString},
		},
	}
	sl := newMockSchema("s", 1, ts.Columns...)

	callerBytes := make([]byte, 32)
	copy(callerBytes, conn.Identity[:])
	otherBytes := make([]byte, 32)
	otherBytes[31] = 0xAA
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewBytes(callerBytes), types.NewString("me")},
				{types.NewBytes(otherBytes), types.NewString("other")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x61},
		QueryString: "SELECT * FROM s WHERE id = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][1].Equal(types.NewString("me")) {
		t.Errorf("row[0].label = %v, want String(me)", pvs[0][1])
	}
}

// Reference expr rejects :sender on columns whose algebraic type is neither
// identity nor bytes (`crates/expr/src/check.rs` lines 487-488). Shunter
// emits a one-off error reply with Status=1 when :sender targets a
// non-bytes column.
func TestHandleOneOffQuery_SenderParameterOnStringColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{1}
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewString("x")}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x62},
		QueryString: "SELECT * FROM t WHERE name = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_SenderParameterOnAliasedSingleTable extends the
// narrow single-table :sender shape (reference check.rs 435-440) to the
// aliased form `select * from s as r where r.bytes = :sender` on the
// one-off query path. Caller identity materializes as the 32-byte bytes
// payload on the target column, filtering the snapshot to the matching row.
func TestHandleOneOffQuery_SenderParameterOnAliasedSingleTable(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{0x11, 0x22}
	ts := &schema.TableSchema{
		ID:   1,
		Name: "s",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "u32", Type: schema.KindUint32},
			{Index: 1, Name: "bytes", Type: schema.KindBytes},
		},
	}
	sl := newMockSchema("s", 1, ts.Columns...)

	callerBytes := make([]byte, 32)
	copy(callerBytes, conn.Identity[:])
	otherBytes := make([]byte, 32)
	otherBytes[0] = 0xFF
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1), types.NewBytes(callerBytes)},
				{types.NewUint32(2), types.NewBytes(otherBytes)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x70},
		QueryString: "SELECT * FROM s AS r WHERE r.bytes = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) {
		t.Errorf("row[0].u32 = %v, want Uint32(1)", pvs[0][0])
	}
}

// TestHandleOneOffQuery_SenderParameterInJoinFilter extends the narrow
// join-backed shape (reference check.rs 462-464) with the :sender parameter
// as a WHERE leaf on the joined relation. Caller identity is threaded
// through the join compile path the same way as the standalone
// single-table path.
func TestHandleOneOffQuery_SenderParameterInJoinFilter(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{0x33, 0x44}
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
	_, tReg, _ := eng.Registry().TableByName("t")
	_, sReg, _ := eng.Registry().TableByName("s")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: tReg.Columns}
	sl := registrySchemaLookup{reg: eng.Registry()}

	callerBytes := make([]byte, 32)
	copy(callerBytes, conn.Identity[:])
	otherBytes := make([]byte, 32)
	otherBytes[31] = 0xAA
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			tReg.ID: {
				{types.NewUint32(1), types.NewUint32(10)},
				{types.NewUint32(2), types.NewUint32(20)},
			},
			sReg.ID: {
				{types.NewUint32(100), types.NewUint32(10), types.NewBytes(callerBytes)},
				{types.NewUint32(101), types.NewUint32(20), types.NewBytes(otherBytes)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x71},
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE s.bytes = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[0][1].Equal(types.NewUint32(10)) {
		t.Fatalf("unexpected row returned: %v", pvs[0])
	}
}

// TestHandleOneOffQuery_StringLiteralOnIntegerColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 498-501 (`select * from t where u32 = 'str'` /
// "Field u32 is not a string") onto the OneOffQuery admission surface. The
// rejection fires at the coerce boundary inside parseQueryString, so the
// one-off reply must arrive with Status=1 and a non-empty Error.
func TestHandleOneOffQuery_StringLiteralOnIntegerColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x80},
		QueryString: "SELECT * FROM t WHERE u32 = 'str'",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_FloatLiteralOnIntegerColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 502-504 (`select * from t where t.u32 = 1.3` /
// "Field u32 is not a float") onto the OneOffQuery admission surface. Float
// literals parse to LitFloat end-to-end (2026-04-21 follow-through), so the
// rejection must surface at the coerce boundary rather than at the parser.
func TestHandleOneOffQuery_FloatLiteralOnIntegerColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x81},
		QueryString: "SELECT * FROM t WHERE t.u32 = 1.3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
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
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityUnknownTableRejected pins the reference type-
// check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// 483-485 (`select * from r` / "Table r does not exist") onto the OneOff
// admission surface. Enforced incidentally via SchemaLookup.TableByName
// returning !ok inside compileSQLQueryString; the pin names the contract.
func TestHandleOneOffQuery_ParityUnknownTableRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x82},
		QueryString: "SELECT * FROM r",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityUnknownColumnRejected pins the reference type-
// check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// 491-493 (`select * from t where t.a = 1` / "Field a does not exist on
// table t") onto the OneOff admission surface. Enforced incidentally via
// rel.ts.Column returning !ok inside normalizeSQLFilterForRelations.
func TestHandleOneOffQuery_ParityUnknownColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x83},
		QueryString: "SELECT * FROM t WHERE t.a = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 495-497 (`select * from t as r where r.a = 1` / "Field a
// does not exist on table t") onto the OneOff admission surface. The
// aliased single-table shape resolves `r` to base table `t` in the parser's
// relationBindings; normalizeSQLFilterForRelations then fails the
// rel.ts.Column lookup. Keeps the rejection named on the alias-qualified
// surface.
func TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x84},
		QueryString: "SELECT * FROM t AS r WHERE r.a = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityBaseTableQualifierAfterAliasRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 506-509 (`select * from t as r where t.u32 = 5` / "t is not
// in scope after alias") onto the OneOff admission surface. Enforced
// incidentally at parser level via resolveQualifier in parseComparison.
func TestHandleOneOffQuery_ParityBaseTableQualifierAfterAliasRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x85},
		QueryString: "SELECT * FROM t AS r WHERE t.u32 = 5",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityBareColumnProjectionReturnsProjectedRows pins the
// query-only single-table column-projection slice on the OneOff path: the
// parser/compile seam may now accept `SELECT u32 FROM t`, and one-off must
// return only the selected column values while keeping the outer table envelope
// unchanged.
func TestHandleOneOffQuery_ParityBareColumnProjectionReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewString("alpha")},
		{types.NewUint32(2), types.NewString("bravo")},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	projectedSchema := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "u32", Type: schema.KindUint32},
		},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x86},
		QueryString: "SELECT u32 FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{
		{types.NewUint32(1)},
		{types.NewUint32(2)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityMultiColumnProjectionReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
		schema.ColumnSchema{Index: 2, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewString("alpha"), types.NewBool(false)},
		{types.NewUint32(2), types.NewString("bravo"), types.NewBool(true)},
		{types.NewUint32(3), types.NewString("charlie"), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	projectedSchema := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "u32", Type: schema.KindUint32},
			{Index: 1, Name: "active", Type: schema.KindBool},
		},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x87},
		QueryString: "SELECT u32, active FROM t WHERE u32 = 2",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{{types.NewUint32(2), types.NewBool(true)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityAliasedBareColumnProjectionReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewString("alpha")},
		{types.NewUint32(2), types.NewString("bravo")},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	projectedSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint32}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x88},
		QueryString: "SELECT u32 AS n FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single base-table envelope for t", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{{types.NewUint32(1)}, {types.NewUint32(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityAliasedBareColumnProjectionWithWhereReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(false)},
		{types.NewUint32(2), types.NewBool(true)},
		{types.NewUint32(3), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	projectedSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint32}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x89},
		QueryString: "SELECT u32 n FROM t WHERE active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{{types.NewUint32(2)}, {types.NewUint32(3)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityAliasedMultiColumnProjectionReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
		schema.ColumnSchema{Index: 2, Name: "name", Type: schema.KindString},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(false), types.NewString("alpha")},
		{types.NewUint32(2), types.NewBool(true), types.NewString("bravo")},
		{types.NewUint32(3), types.NewBool(true), types.NewString("charlie")},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	projectedSchema := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "n", Type: schema.KindUint32},
			{Index: 1, Name: "enabled", Type: schema.KindBool},
		},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8A},
		QueryString: "SELECT u32 AS n, active AS enabled FROM t WHERE active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single base-table envelope for t", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{{types.NewUint32(2), types.NewBool(true)}, {types.NewUint32(3), types.NewBool(true)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityJoinColumnProjectionReturnsProjectedRows(t *testing.T) {
	conn := testConnDirect(nil)
	projectedTS := &schema.TableSchema{
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
			{Name: "note", Type: schema.KindString},
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	projectedTS.ID = ordersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID: {
			{types.NewUint32(1), types.NewUint32(100), types.NewString("alpha")},
			{types.NewUint32(2), types.NewUint32(100), types.NewString("bravo")},
			{types.NewUint32(3), types.NewUint32(102), types.NewString("charlie")},
		},
		inventoryReg.ID: {
			{types.NewUint32(100), types.NewUint32(9)},
			{types.NewUint32(101), types.NewUint32(10)},
			{types.NewUint32(102), types.NewUint32(3)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8B},
		QueryString: "SELECT o.id, o.product_id FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "Orders" {
		t.Fatalf("Tables = %+v, want single Orders table envelope", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedTS)
	wantRows := []types.ProductValue{
		{types.NewUint32(1), types.NewUint32(100)},
		{types.NewUint32(2), types.NewUint32(100)},
		{types.NewUint32(3), types.NewUint32(102)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityJoinColumnProjectionProjectsRight(t *testing.T) {
	conn := testConnDirect(nil)
	projectedTS := &schema.TableSchema{
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
		Indexes: []schema.IndexDefinition{{Name: "idx_orders_product_id", Columns: []string{"product_id"}}},
	})
	b.TableDef(schema.TableDefinition{
		Name: "Inventory",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "quantity", Type: schema.KindUint32},
			{Name: "sku", Type: schema.KindString},
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
	projectedTS.ID = inventoryReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ordersReg.ID: {
			{types.NewUint32(1), types.NewUint32(100)},
			{types.NewUint32(2), types.NewUint32(100)},
			{types.NewUint32(3), types.NewUint32(102)},
		},
		inventoryReg.ID: {
			{types.NewUint32(100), types.NewUint32(9), types.NewString("sku-100")},
			{types.NewUint32(101), types.NewUint32(10), types.NewString("sku-101")},
			{types.NewUint32(102), types.NewUint32(3), types.NewString("sku-102")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8C},
		QueryString: "SELECT product.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "Inventory" {
		t.Fatalf("Tables = %+v, want single Inventory table envelope", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedTS)
	wantRows := []types.ProductValue{
		{types.NewUint32(100), types.NewUint32(9)},
		{types.NewUint32(100), types.NewUint32(9)},
		{types.NewUint32(102), types.NewUint32(3)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityJoinColumnProjectionAllowsMixedRelations(t *testing.T) {
	conn := testConnDirect(nil)
	projectedSchema := &schema.TableSchema{
		ID:   1,
		Name: "Orders",
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
	if !ok {
		t.Fatal("Inventory table missing from registry")
	}
	projectedSchema.ID = ordersReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
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
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8F},
		QueryString: "SELECT o.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "Orders" {
		t.Fatalf("Tables = %+v, want single first-projected-relation Orders envelope", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedSchema)
	wantRows := []types.ProductValue{
		{types.NewUint32(1), types.NewUint32(9)},
		{types.NewUint32(2), types.NewUint32(9)},
		{types.NewUint32(3), types.NewUint32(3)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParitySelfJoinColumnProjectionProjectsLeft(t *testing.T) {
	conn := testConnDirect(nil)
	projectedTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, ok := eng.Registry().TableByName("t")
	if !ok {
		t.Fatal("t table missing from registry")
	}
	projectedTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{MessageID: []byte{0x8D}, QueryString: "SELECT a.id FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE b.id = 2"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedTS)
	wantRows := []types.ProductValue{{types.NewUint32(1)}, {types.NewUint32(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParitySelfJoinColumnProjectionProjectsRight(t *testing.T) {
	conn := testConnDirect(nil)
	projectedTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{Name: "t", Columns: []schema.ColumnDefinition{{Name: "id", Type: schema.KindUint32, PrimaryKey: true}, {Name: "u32", Type: schema.KindUint32}}, Indexes: []schema.IndexDefinition{{Name: "idx_t_u32", Columns: []string{"u32"}}}})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	_, tReg, ok := eng.Registry().TableByName("t")
	if !ok {
		t.Fatal("t table missing from registry")
	}
	projectedTS.ID = tReg.ID
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(1), types.NewUint32(5)},
			{types.NewUint32(2), types.NewUint32(5)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{MessageID: []byte{0x8E}, QueryString: "SELECT b.id FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 2"}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), projectedTS)
	wantRows := []types.ProductValue{{types.NewUint32(1)}, {types.NewUint32(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ParityJoinWithoutQualifiedProjectionRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 515-517 (`select * from t join s` / "Subscriptions must be
// typed to a single table") onto the OneOff admission surface. Enforced
// incidentally at parseStatement requiring a qualified projection for joins.
func TestHandleOneOffQuery_ParityJoinWithoutQualifiedProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x87},
		QueryString: "SELECT * FROM t JOIN s",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySelfJoinWithoutAliasesRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 519-521 (`select t.* from t join t` / "Self join requires
// aliases") onto the OneOff admission surface. Enforced incidentally at
// parseJoinClause when both sides share the same table and alias.
func TestHandleOneOffQuery_ParitySelfJoinWithoutAliasesRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x88},
		QueryString: "SELECT t.* FROM t JOIN t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityForwardAliasReferenceRejected pins the reference
// type-check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// 526-528 (`select t.* from t join s on t.u32 = r.u32 join s as r` / "Alias
// r is not in scope when it is referenced") onto the OneOff admission surface.
// Enforced incidentally in parseQualifiedColumnRef when the forward alias
// reference fails resolveQualifier against the first join's lookup.
func TestHandleOneOffQuery_ParityForwardAliasReferenceRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x89},
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityLimitClauseAppliesToVisibleRows pins the
// query-only LIMIT slice from the reference SQL grammar onto Shunter's one-off
// handler: after the existing row-shaped evaluation path produces matches, the
// handler caps the visible result rows without changing the scan order contract
// beyond this deterministic mock harness.
func TestHandleOneOffQuery_ParityLimitClauseAppliesToVisibleRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)
	ts, ok := sl.Table(1)
	if !ok {
		t.Fatal("mock schema missing table 1")
	}
	wantRows := []types.ProductValue{
		{types.NewUint32(1), types.NewString("alpha")},
		{types.NewUint32(2), types.NewString("bravo")},
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		wantRows[0],
		wantRows[1],
		{types.NewUint32(3), types.NewString("charlie")},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8A},
		QueryString: "SELECT * FROM t LIMIT 2",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), ts)
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ParityLeadingPlusIntLiteral pins the reference
// valid-literal shape at reference/SpacetimeDB/crates/expr/src/check.rs:297-
// 300 (`select * from t where u32 = +1` / "Leading `+`"): a leading `+` on
// an integer literal is admitted end-to-end through the OneOff path.
func TestHandleOneOffQuery_ParityLeadingPlusIntLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(7)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8C},
		QueryString: "SELECT * FROM t WHERE u32 = +7",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
}

// TestHandleOneOffQuery_ParityUnqualifiedWhereInJoinRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 534-537 (`select t.* from t join s on t.u32 = s.u32 where
// bytes = 0xABCD` / "Columns must be qualified in join expressions") onto the
// OneOff admission surface. Enforced incidentally at parseComparison when the
// relation binding has requireQualify set by the join.
func TestHandleOneOffQuery_ParityUnqualifiedWhereInJoinRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "bytes", Type: schema.KindBytes},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8B},
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE bytes = 0xABCD",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityScientificNotationUnsignedInteger pins the
// reference valid-literal shape at reference/SpacetimeDB/crates/expr/src/
// check.rs:302-304 (`select * from t where u32 = 1e3` / "Scientific
// notation") on the OneOff admission path.
func TestHandleOneOffQuery_ParityScientificNotationUnsignedInteger(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1000)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8D},
		QueryString: "SELECT * FROM t WHERE u32 = 1e3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
}

// TestHandleOneOffQuery_ParityScientificNotationFloatNegativeExponent pins
// reference/SpacetimeDB/crates/expr/src/check.rs:314-316 (`select * from t
// where f32 = 1e-3` / "Negative exponent") on the OneOff admission path.
func TestHandleOneOffQuery_ParityScientificNotationFloatNegativeExponent(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)
	v, err := types.NewFloat32(float32(1e-3))
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{v}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8E},
		QueryString: "SELECT * FROM t WHERE f32 = 1e-3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
}

// TestHandleOneOffQuery_ParityLeadingDotFloatLiteral pins reference/
// SpacetimeDB/crates/expr/src/check.rs:322-324 (`select * from t where
// f32 = .1` / "Leading `.`") on the OneOff admission path.
func TestHandleOneOffQuery_ParityLeadingDotFloatLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)
	v, err := types.NewFloat32(float32(0.1))
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{v}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8F},
		QueryString: "SELECT * FROM t WHERE f32 = .1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
}

// TestHandleOneOffQuery_ParityScientificNotationOverflowInfinity pins
// reference/SpacetimeDB/crates/expr/src/check.rs:326-328 (`select * from t
// where f32 = 1e40` / "Infinity") on the OneOff admission path. The stored
// row is +Inf on the f32 column; the query literal `1e40` must coerce to
// the same +Inf value and match.
func TestHandleOneOffQuery_ParityScientificNotationOverflowInfinity(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)
	v, err := types.NewFloat32(float32(math.Inf(1)))
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{v}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x90},
		QueryString: "SELECT * FROM t WHERE f32 = 1e40",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
}

// TestHandleOneOffQuery_ParityInvalidLiteralNegativeIntOnUnsignedRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:382-385 (`select * from t
// where u8 = -1` / "Negative integer for unsigned column") onto the
// OneOffQuery admission surface. `-1` is LitInt(-1); coerceUnsigned
// (query/sql/coerce.go:119) rejects negative literals against unsigned
// columns inside parseQueryString, producing Status=1 with a non-empty
// Error message.
func TestHandleOneOffQuery_ParityInvalidLiteralNegativeIntOnUnsignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x91},
		QueryString: "SELECT * FROM t WHERE u8 = -1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityInvalidLiteralScientificOverflowRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:386-389 (`select * from t
// where u8 = 1e3` / "Out of bounds") onto the OneOffQuery admission surface.
// `1e3` collapses to LitInt(1000) via parseNumericLiteral; coerceUnsigned
// (query/sql/coerce.go:123) rejects the value as out of range for u8.
func TestHandleOneOffQuery_ParityInvalidLiteralScientificOverflowRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x92},
		QueryString: "SELECT * FROM t WHERE u8 = 1e3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityInvalidLiteralFloatOnUnsignedRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:390-393 (`select * from t
// where u8 = 0.1` / "Float as integer") onto the OneOffQuery admission
// surface. Complements the existing u32 = 1.3 pin by naming the u8 column
// variant; coerceUnsigned rejects LitFloat against an integer column.
func TestHandleOneOffQuery_ParityInvalidLiteralFloatOnUnsignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x93},
		QueryString: "SELECT * FROM t WHERE u8 = 0.1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnUnsignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:394-397 (`select * from
// t where u32 = 1e-3` / "Float as integer") onto the OneOffQuery admission
// surface. `1e-3` stays LitFloat (non-integral) and coerceUnsigned rejects
// it against an unsigned column.
func TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnUnsignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x94},
		QueryString: "SELECT * FROM t WHERE u32 = 1e-3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnSignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:398-401 (`select * from
// t where i32 = 1e-3` / "Float as integer") onto the OneOffQuery admission
// surface. Mirrors the unsigned case on a signed column: coerceSigned rejects
// the LitFloat against KindInt32.
func TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnSignedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "i32", Type: schema.KindInt32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewInt32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x95},
		QueryString: "SELECT * FROM t WHERE i32 = 1e-3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth pins
// reference/SpacetimeDB/crates/expr/src/check.rs:360-370
// (`valid_literals_for_type`) at the OneOffQuery admission surface. Each
// subtest builds a single-column table, stores a matching row, and
// confirms `SELECT * FROM t WHERE {colname} = 127` accepts end-to-end on
// every numeric column kind realized by `schema.ValueKind`
// (i8/u8/i16/u16/i32/u32/i64/u64/f32/f64 plus i128/u128 added 2026-04-21
// slice 1 and i256/u256 added 2026-04-21 slice 2). The reference
// `u256 = 1e40` row stays deferred until BigDecimal literal widening.
func TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth(t *testing.T) {
	f32Row, err := types.NewFloat32(127)
	if err != nil {
		t.Fatalf("NewFloat32(127): %v", err)
	}
	f64Row, err := types.NewFloat64(127)
	if err != nil {
		t.Fatalf("NewFloat64(127): %v", err)
	}

	cases := []struct {
		colName string
		kind    schema.ValueKind
		row     types.Value
	}{
		{"i8", schema.KindInt8, types.NewInt8(127)},
		{"u8", schema.KindUint8, types.NewUint8(127)},
		{"i16", schema.KindInt16, types.NewInt16(127)},
		{"u16", schema.KindUint16, types.NewUint16(127)},
		{"i32", schema.KindInt32, types.NewInt32(127)},
		{"u32", schema.KindUint32, types.NewUint32(127)},
		{"i64", schema.KindInt64, types.NewInt64(127)},
		{"u64", schema.KindUint64, types.NewUint64(127)},
		{"f32", schema.KindFloat32, f32Row},
		{"f64", schema.KindFloat64, f64Row},
		{"i128", schema.KindInt128, types.NewInt128(0, 127)},
		{"u128", schema.KindUint128, types.NewUint128(0, 127)},
		{"i256", schema.KindInt256, types.NewInt256(0, 0, 0, 127)},
		{"u256", schema.KindUint256, types.NewUint256(0, 0, 0, 127)},
	}

	for i, tc := range cases {
		t.Run(tc.colName, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: tc.colName, Type: tc.kind},
			)
			snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{tc.row}}}}
			stateAccess := &mockStateAccess{snap: snap}

			msg := &OneOffQueryMsg{
				MessageID:   []byte{byte(0xA0 + i)},
				QueryString: "SELECT * FROM t WHERE " + tc.colName + " = 127",
			}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

			result := drainOneOff(t, conn)
			if result.Error != nil {
				t.Fatalf("Error = %q, want nil (success)", *result.Error)
			}
		})
	}
}

// TestHandleOneOffQuery_ParityValidLiteralU256Scientific pins the remaining
// reference `valid_literals` row at
// reference/SpacetimeDB/crates/expr/src/check.rs:330-332
// (`select * from t where u256 = 1e40` / "u256") at the OneOffQuery
// admission surface. Shunter's parser promotes `1e40` to LitBigInt and
// coerce decomposes 10^40 into four uint64 words for the 256-bit Uint256
// layout. The snapshot holds one matching row so the query should return
// Status == 0.
func TestHandleOneOffQuery_ParityValidLiteralU256Scientific(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u256", Type: schema.KindUint256},
	)
	// 10^40 decomposed via the same coerce path the query admission uses,
	// so the stored row is guaranteed to equal the admission-time value.
	row, err := buildUint256From1e40()
	if err != nil {
		t.Fatalf("build row: %v", err)
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{row}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC5},
		QueryString: "SELECT * FROM t WHERE u256 = 1e40",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
}

// TestHandleOneOffQuery_ParityUint256NegativeRejected extends the
// reference invalid_literals bundle at check.rs:382-385 to the Uint256
// column kind. Mirrors the subscribe-side pin.
func TestHandleOneOffQuery_ParityUint256NegativeRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u256", Type: schema.KindUint256},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint256(0, 0, 0, 1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC2},
		QueryString: "SELECT * FROM t WHERE u256 = -1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityTimestampLiteralAccepted pins the reference
// valid_literals rows at check.rs:334-352 onto the OneOff admission surface.
// Each subtest builds a Timestamp-column table, stores a matching row, and
// confirms `SELECT * FROM t WHERE ts = '<shape>'` accepts end-to-end across
// all five reference RFC3339 shapes.
func TestHandleOneOffQuery_ParityTimestampLiteralAccepted(t *testing.T) {
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
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "ts", Type: schema.KindTimestamp},
			)
			snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewTimestamp(tc.micro)}}}}
			stateAccess := &mockStateAccess{snap: snap}

			msg := &OneOffQueryMsg{
				MessageID:   []byte{byte(0xD0 + i)},
				QueryString: "SELECT * FROM t WHERE ts = '" + tc.lit + "'",
			}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

			result := drainOneOff(t, conn)
			if result.Error != nil {
				t.Fatalf("Error = %q, want nil (success)", *result.Error)
			}
		})
	}
}

// TestHandleOneOffQuery_ParityTimestampMalformedRejected mirrors the
// subscribe-side pin: a non-RFC3339 string on a Timestamp column must reject.
func TestHandleOneOffQuery_ParityTimestampMalformedRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "ts", Type: schema.KindTimestamp},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewTimestamp(0)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD5},
		QueryString: "SELECT * FROM t WHERE ts = 'not-a-timestamp'",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityUint128NegativeRejected extends the
// reference invalid_literals bundle at check.rs:382-385 to the Uint128
// column kind. Mirrors the subscribe-side pin.
func TestHandleOneOffQuery_ParityUint128NegativeRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u128", Type: schema.KindUint128},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint128(0, 1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC1},
		QueryString: "SELECT * FROM t WHERE u128 = -1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityDMLStatementRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`delete from t` / "DML not allowed in subscriptions") onto the OneOff
// admission surface. Enforced incidentally at parseStatement's
// expectKeyword("SELECT").
//
// One-off shares the subscription-shape admission path in Shunter; the
// intentional divergence from reference's wider parse_and_type_sql path is
// recorded in docs/parity-phase0-ledger.md.
func TestHandleOneOffQuery_ParityDMLStatementRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB0},
		QueryString: "DELETE FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityEmptyStatementRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (empty string / "Empty") onto the OneOff admission surface. Enforced
// incidentally at expectKeyword("SELECT") on an EOF-only token stream.
func TestHandleOneOffQuery_ParityEmptyStatementRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB1},
		QueryString: "",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected pins the
// reference subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (single space / "Empty after whitespace skip") onto the OneOff admission
// surface. Enforced incidentally once the tokenizer drops whitespace.
func TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB2},
		QueryString: "   ",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityDistinctProjectionRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`select distinct a from t` / "DISTINCT not supported") onto the OneOff
// admission surface. Enforced incidentally at parseProjection which only
// accepts `*` or `table.*`.
func TestHandleOneOffQuery_ParityDistinctProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB3},
		QueryString: "SELECT DISTINCT u32 FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySubqueryInFromRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`select * from (select * from t) join (select * from s) on a = b` /
// "Subqueries in FROM not supported") onto the OneOff admission surface.
// Enforced incidentally at parseStatement which requires an identifier token
// after FROM.
func TestHandleOneOffQuery_ParitySubqueryInFromRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB4},
		QueryString: "SELECT * FROM (SELECT * FROM t) JOIN (SELECT * FROM s) ON a = b",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedSelectLiteralWithoutFromRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select 1` / "FROM is required") onto the OneOff admission surface.
// parseProjection rejects the integer literal `1` with "projection must be
// '*' or 'table.*'".
func TestHandleOneOffQuery_ParitySqlUnsupportedSelectLiteralWithoutFromRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB5},
		QueryString: "SELECT 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedMultiPartTableNameRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a from s.t` / "Multi-part table names") onto the OneOff admission
// surface. parseProjection rejects the bare identifier `a` before FROM is
// parsed, so rejection fires with "projection must be '*' or 'table.*'".
func TestHandleOneOffQuery_ParitySqlUnsupportedMultiPartTableNameRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB6},
		QueryString: "SELECT a FROM s.t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedBitStringLiteralRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select * from t where a = B'1010'` / "Bit-string literals") onto the
// OneOff admission surface. The lexer tokenizes `B` as an identifier and
// `'1010'` as a separate string literal; parseLiteral rejects the identifier
// RHS of `=` with "expected literal, got identifier \"B\"".
func TestHandleOneOffQuery_ParitySqlUnsupportedBitStringLiteralRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB7},
		QueryString: "SELECT * FROM t WHERE u32 = B'1010'",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedWildcardWithBareColumnsRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a.*, b, c from t` / "Wildcard with non-wildcard projections") onto
// the OneOff admission surface. After parseProjection consumes `t.*`,
// parseStatement expects FROM but finds `,` and rejects with
// "expected FROM, got \",\"".
func TestHandleOneOffQuery_ParitySqlUnsupportedWildcardWithBareColumnsRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB8},
		QueryString: "SELECT t.*, b, c FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedOrderByWithLimitExpressionRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select * from t order by a limit b` / "Limit expression") onto the OneOff
// admission surface. ORDER BY trips parseStatement's EOF guard
// (query/sql/parser.go:547-549) with "unexpected token \"ORDER\"" before the
// LIMIT identifier is examined.
func TestHandleOneOffQuery_ParitySqlUnsupportedOrderByWithLimitExpressionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB9},
		QueryString: "SELECT * FROM t ORDER BY u32 LIMIT u32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedAggregateWithGroupByRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a, count(*) from t group by a` / "GROUP BY") onto the OneOff
// admission surface. parseProjection rejects the leading bare column with
// "projection must be '*' or 'table.*'" before the aggregate or GROUP BY
// keyword is ever seen.
func TestHandleOneOffQuery_ParitySqlUnsupportedAggregateWithGroupByRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xBA},
		QueryString: "SELECT u32, COUNT(*) FROM t GROUP BY u32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedImplicitCommaJoinRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a.* from t as a, s as b where a.id = b.id and b.c = 1` /
// "Implicit joins") onto the OneOff admission surface. After consuming
// `t AS a`, parseStatement's EOF/keyword guard hits `,` and rejects with
// "unexpected token \",\"".
func TestHandleOneOffQuery_ParitySqlUnsupportedImplicitCommaJoinRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xBB},
		QueryString: "SELECT a.* FROM t AS a, s AS b WHERE a.u32 = b.u32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select t.* from t join s on int = u32` / "Joins require qualified vars")
// onto the OneOff admission surface. parseJoinClause calls
// parseQualifiedColumnRef for the left side of ON
// (query/sql/parser.go:629); the bare identifier `int` fails there with
// "expected qualified column reference".
func TestHandleOneOffQuery_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xBC},
		QueryString: "SELECT t.* FROM t JOIN s ON int = u32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlInvalidEmptySelectRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select from t` / "Empty SELECT") onto the OneOff admission surface.
// parseProjection rejects because the next token after SELECT is the
// identifier `from`, which is then followed by `t` (not a dot), so the
// projection fails with "projection must be '*' or 'table.*'".
func TestHandleOneOffQuery_ParitySqlInvalidEmptySelectRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xBD},
		QueryString: "SELECT FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlInvalidEmptyFromRejected pins the reference
// parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a from where b = 1` / "Empty FROM") onto the OneOff admission
// surface. parseProjection rejects the bare column `a` with "projection must
// be '*' or 'table.*'" before the empty FROM is examined.
func TestHandleOneOffQuery_ParitySqlInvalidEmptyFromRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xBE},
		QueryString: "SELECT a FROM WHERE b = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlInvalidEmptyWhereRejected pins the reference
// parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a from t where` / "Empty WHERE") onto the OneOff admission
// surface. parseProjection rejects the bare column `a` with "projection must
// be '*' or 'table.*'" before the empty WHERE is examined.
func TestHandleOneOffQuery_ParitySqlInvalidEmptyWhereRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xBF},
		QueryString: "SELECT a FROM t WHERE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParitySqlInvalidEmptyGroupByRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a, count(*) from t group by` / "Empty GROUP BY") onto the OneOff
// admission surface. parseProjection rejects the leading bare column `a` with
// "projection must be '*' or 'table.*'" before the aggregate or empty GROUP
// BY is examined.
func TestHandleOneOffQuery_ParitySqlInvalidEmptyGroupByRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC0},
		QueryString: "SELECT a, COUNT(*) FROM t GROUP BY",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleOneOffQuery_ParityCountAliasReturnsSingleAggregateRow(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(true)},
		{types.NewUint32(2), types.NewBool(false)},
		{types.NewUint32(3), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC1},
		QueryString: "SELECT COUNT(*) AS n FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(3)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityCountBareAliasReturnsSingleAggregateRow(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(true)},
		{types.NewUint32(2), types.NewBool(false)},
		{types.NewUint32(3), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC4},
		QueryString: "SELECT COUNT(*) n FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(3)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityCountAliasWithWhereReturnsSingleAggregateRow(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(true)},
		{types.NewUint32(2), types.NewBool(false)},
		{types.NewUint32(3), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC2},
		QueryString: "SELECT COUNT(*) AS n FROM t WHERE active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityCountAliasZeroRowsReturnsSingleZeroRow(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(false)},
		{types.NewUint32(2), types.NewBool(false)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC3},
		QueryString: "SELECT COUNT(*) AS n FROM t WHERE active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(0)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityJoinCountAliasReturnsSingleAggregateRow(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1)},
			{types.NewUint32(2)},
		},
		2: {
			{types.NewUint32(1), types.NewBool(true)},
			{types.NewUint32(1), types.NewBool(false)},
			{types.NewUint32(3), types.NewBool(true)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC5},
		QueryString: "SELECT COUNT(*) AS n FROM t JOIN s ON t.id = s.t_id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single aggregate envelope for t", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityJoinCountBareAliasWithWhereReturnsSingleAggregateRow(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1)},
			{types.NewUint32(2)},
		},
		2: {
			{types.NewUint32(1), types.NewBool(true)},
			{types.NewUint32(1), types.NewBool(false)},
			{types.NewUint32(2), types.NewBool(true)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC6},
		QueryString: "SELECT COUNT(*) n FROM t JOIN s ON t.id = s.t_id WHERE s.active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ParityJoinCountWithLimitRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}}},
		"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{{Index: 0, Name: "t_id", Type: schema.KindUint32}}}},
	}}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC7},
		QueryString: "SELECT COUNT(*) AS n FROM t JOIN s ON t.id = s.t_id LIMIT 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected aggregate + LIMIT rejection, got nil error")
	}
	if !strings.Contains(*result.Error, "aggregate projections with LIMIT not supported") {
		t.Fatalf("Error = %q, want aggregate LIMIT rejection", *result.Error)
	}
}

// TestHandleOneOffQuery_ParitySqlInvalidAggregateWithoutAliasRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select count(*) from t` / "Aggregate without alias") onto the OneOff
// admission surface. parseProjection reads `count` as an identifier
// qualifier, then finds `(` where it expects a dot, rejecting with
// "projection must be '*' or 'table.*'".
func TestHandleOneOffQuery_ParitySqlInvalidAggregateWithoutAliasRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC1},
		QueryString: "SELECT COUNT(*) FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestHandleOneOffQuery_ParityArraySenderRejected pins reference
// check.rs:487-489 (`select * from t where arr = :sender` / "The :sender
// param is an identity") onto the OneOffQuery admission surface. With
// KindArrayString realized, the coerce layer rejects :sender against the
// array column instead of hitting the default "column kind not supported"
// branch — the rejection is a positive parity contract.
func TestHandleOneOffQuery_ParityArraySenderRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "arr", Type: schema.KindArrayString},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD0},
		QueryString: "SELECT * FROM t WHERE arr = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message for :sender on array column")
	}
}

// TestHandleOneOffQuery_ParityArrayJoinOnRejected pins reference
// check.rs:523-525 (`select t.* from t join s on t.arr = s.arr` / "Product
// values are not comparable") onto the OneOffQuery admission surface. The
// join compile path rejects when either ON side names an array column.
func TestHandleOneOffQuery_ParityArrayJoinOnRejected(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD1},
		QueryString: "SELECT t.* FROM t JOIN s ON t.arr = s.arr",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	if result.Error == nil || *result.Error == "" {
		t.Error("expected non-empty error message for array-on-array join ON")
	}
}

func TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows(t *testing.T) {
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
	_, ordersReg, ok := eng.Registry().TableByName("Orders")
	if !ok {
		t.Fatal("Orders table missing from registry")
	}
	_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
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
		MessageID:   []byte{0x1e},
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (ON-filter is query-only accepted)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ordersTS)
	if len(pvs) != 2 {
		t.Fatalf("got %d rows, want 2 (orders 1 and 3 match the quantity<10 filter)", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewUint32(1)) || !pvs[1][0].Equal(types.NewUint32(3)) {
		t.Fatalf("unexpected order ids returned: %v, %v (want 1 and 3)", pvs[0][0], pvs[1][0])
	}
}

func TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm(t *testing.T) {
	buildSnap := func() (*mockSnapshot, *schema.TableSchema, schema.SchemaRegistry) {
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
		_, ordersReg, ok := eng.Registry().TableByName("Orders")
		if !ok {
			t.Fatal("Orders table missing from registry")
		}
		_, inventoryReg, ok := eng.Registry().TableByName("Inventory")
		if !ok {
			t.Fatal("Inventory table missing from registry")
		}
		ordersTS := &schema.TableSchema{ID: ordersReg.ID, Name: "Orders", Columns: ordersReg.Columns}
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
		return snap, ordersTS, eng.Registry()
	}

	runQuery := func(q string, id byte) []types.ProductValue {
		conn := testConnDirect(nil)
		snap, ordersTS, reg := buildSnap()
		sl := registrySchemaLookup{reg: reg}
		stateAccess := &mockStateAccess{snap: snap}
		msg := &OneOffQueryMsg{MessageID: []byte{id}, QueryString: q}
		handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
		result := drainOneOff(t, conn)
		if result.Error != nil {
			t.Fatalf("query %q error = %q", q, *result.Error)
		}
		return decodeRows(t, firstTableRows(result), ordersTS)
	}

	onRows := runQuery("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10", 0x20)
	whereRows := runQuery("SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id WHERE product.quantity < 10", 0x21)

	if len(onRows) != len(whereRows) {
		t.Fatalf("row count diverges: ON=%d, WHERE=%d", len(onRows), len(whereRows))
	}
	for i := range onRows {
		if len(onRows[i]) != len(whereRows[i]) {
			t.Fatalf("row %d column count diverges: ON=%d, WHERE=%d", i, len(onRows[i]), len(whereRows[i]))
		}
		for j := range onRows[i] {
			if !onRows[i][j].Equal(whereRows[i][j]) {
				t.Fatalf("row %d col %d diverges: ON=%v, WHERE=%v", i, j, onRows[i][j], whereRows[i][j])
			}
		}
	}
}
