package protocol

import (
	"bytes"
	"context"
	"iter"
	"math"
	"math/big"
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

type countingSnapshot struct {
	*mockSnapshot
	tableScans int
}

func (s *countingSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	s.tableScans++
	return s.mockSnapshot.TableScan(id)
}

type cancelingSnapshot struct {
	*mockSnapshot
	cancel  func()
	yielded int
	closed  bool
}

func (s *cancelingSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	base := s.mockSnapshot.TableScan(id)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, row := range base {
			if !yield(rid, row) {
				return
			}
			s.yielded++
			if s.yielded == 1 && s.cancel != nil {
				s.cancel()
			}
		}
	}
}

func (s *cancelingSnapshot) Close() { s.closed = true }

// mockStateAccess implements CommittedStateAccess.
type mockStateAccess struct {
	snap store.CommittedReadView
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
// nil if Tables is empty. Most one-off message-id handler tests populate
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

func mustUUIDValue(t *testing.T, s string) types.Value {
	t.Helper()
	v, err := types.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return v
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

func TestHandleOneOffQuery_UUIDLiteralFiltersRows(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "entities",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUUID},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("entities", 1, ts.Columns...)
	matchingID := mustUUIDValue(t, "00112233-4455-6677-8899-aabbccddeeff")
	otherID := mustUUIDValue(t, "00112233-4455-6677-8899-aabbccddee00")

	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{matchingID, types.NewString("match")},
				{otherID, types.NewString("other")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x12},
		QueryString: "SELECT * FROM entities WHERE id = '00112233-4455-6677-8899-aabbccddeeff'",
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
	if !pvs[0][0].Equal(matchingID) || !pvs[0][1].Equal(types.NewString("match")) {
		t.Fatalf("row = %v, want matching UUID row", pvs[0])
	}
}

func TestHandleOneOffQueryLimitZeroDoesNotScan(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
	)
	snap := &countingSnapshot{mockSnapshot: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1)},
			{types.NewUint64(2)},
		},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x10},
		QueryString: "SELECT * FROM users LIMIT 0",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	rawRows, err := DecodeRowList(firstTableRows(result))
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rawRows) != 0 {
		t.Fatalf("row count = %d, want 0", len(rawRows))
	}
	if snap.tableScans != 0 {
		t.Fatalf("TableScan calls = %d, want 0", snap.tableScans)
	}
}

func TestHandleOneOffQueryOffsetSkipsMatchedRowsBeforeLimit(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
		schema.ColumnSchema{Index: 1, Name: "name", Type: schema.KindString},
	)
	ts, ok := sl.Table(1)
	if !ok {
		t.Fatal("mock schema missing table 1")
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("hidden-before-where")},
			{types.NewUint64(2), types.NewString("first-match")},
			{types.NewUint64(3), types.NewString("second-match")},
			{types.NewUint64(4), types.NewString("third-match")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x14},
		QueryString: "SELECT * FROM users WHERE id >= 2 LIMIT 1 OFFSET 1",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint64(3), types.NewString("second-match")},
	})
}

func TestHandleOneOffQueryOrderByDescSortsBeforeLimitAndProjection(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
			{Index: 2, Name: "label", Type: schema.KindString},
		},
	}
	projectedTS := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "label", Type: schema.KindString},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(10), types.NewString("low")},
			{types.NewUint32(2), types.NewUint32(30), types.NewString("high")},
			{types.NewUint32(3), types.NewUint32(20), types.NewString("mid")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x13},
		QueryString: "SELECT id, label FROM metrics ORDER BY score DESC LIMIT 2",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), projectedTS)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint32(2), types.NewString("high")},
		{types.NewUint32(3), types.NewString("mid")},
	})
}

func TestHandleOneOffQueryOrderByProjectionAliasSortsBeforeProjection(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
			{Index: 2, Name: "label", Type: schema.KindString},
		},
	}
	projectedTS := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "rank", Type: schema.KindUint32},
			{Index: 1, Name: "label", Type: schema.KindString},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(10), types.NewString("low")},
			{types.NewUint32(2), types.NewUint32(30), types.NewString("high")},
			{types.NewUint32(3), types.NewUint32(20), types.NewString("mid")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x16},
		QueryString: "SELECT score AS rank, label FROM metrics ORDER BY rank DESC LIMIT 2",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), projectedTS)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint32(30), types.NewString("high")},
		{types.NewUint32(20), types.NewString("mid")},
	})
}

func TestHandleOneOffQueryOrderByUnknownProjectionNameRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("metrics", 1,
		schema.ColumnSchema{Index: 0, Name: "score", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(10)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x17},
		QueryString: "SELECT score AS rank FROM metrics ORDER BY missing",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestHandleOneOffQueryOrderByDescSortsBeforeOffsetLimitAndProjection(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
			{Index: 2, Name: "label", Type: schema.KindString},
		},
	}
	projectedTS := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "label", Type: schema.KindString},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(10), types.NewString("low")},
			{types.NewUint32(2), types.NewUint32(30), types.NewString("high")},
			{types.NewUint32(3), types.NewUint32(20), types.NewString("mid")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x15},
		QueryString: "SELECT id, label FROM metrics ORDER BY score DESC LIMIT 1 OFFSET 1",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), projectedTS)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint32(3), types.NewString("mid")},
	})
}

func TestHandleOneOffQueryMultiColumnOrderByTableScan(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "users",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "active", Type: schema.KindBool},
			{Index: 2, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("users", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewBool(true), types.NewString("bravo")},
			{types.NewUint32(2), types.NewBool(true), types.NewString("alpha")},
			{types.NewUint32(3), types.NewBool(false), types.NewString("charlie")},
			{types.NewUint32(4), types.NewBool(false), types.NewString("alpha")},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x18},
		QueryString: "SELECT * FROM users ORDER BY active DESC, name ASC",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint32(2), types.NewBool(true), types.NewString("alpha")},
		{types.NewUint32(1), types.NewBool(true), types.NewString("bravo")},
		{types.NewUint32(4), types.NewBool(false), types.NewString("alpha")},
		{types.NewUint32(3), types.NewBool(false), types.NewString("charlie")},
	})
}

func TestHandleOneOffQueryMultiColumnOrderByProjectionAliasOffsetLimit(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
		},
	}
	projectedTS := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "rank", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(3), types.NewUint32(20)},
			{types.NewUint32(2), types.NewUint32(30)},
			{types.NewUint32(1), types.NewUint32(30)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x19},
		QueryString: "SELECT id, score AS rank FROM metrics ORDER BY rank DESC, id ASC LIMIT 1 OFFSET 1",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), projectedTS)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint32(2), types.NewUint32(30)},
	})
}

func TestHandleOneOffQueryMultiColumnOrderByStableTies(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "metrics",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "score", Type: schema.KindUint32},
			{Index: 2, Name: "bucket", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("metrics", 1, ts.Columns...)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(10), types.NewUint32(1)},
			{types.NewUint32(2), types.NewUint32(10), types.NewUint32(1)},
			{types.NewUint32(3), types.NewUint32(20), types.NewUint32(2)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x1a},
		QueryString: "SELECT * FROM metrics ORDER BY score DESC, bucket ASC",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	assertProductRowsEqual(t, pvs, []types.ProductValue{
		{types.NewUint32(3), types.NewUint32(20), types.NewUint32(2)},
		{types.NewUint32(1), types.NewUint32(10), types.NewUint32(1)},
		{types.NewUint32(2), types.NewUint32(10), types.NewUint32(1)},
	})
}

func TestHandleOneOffQueryCancelsDuringSnapshotScanAndClosesView(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("users", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
	)
	ctx, cancel := context.WithCancel(context.Background())
	snap := &cancelingSnapshot{mockSnapshot: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1)},
			{types.NewUint64(2)},
		},
	}}, cancel: cancel}
	stateAccess := &mockStateAccess{snap: snap}

	handleOneOffQuery(ctx, conn, &OneOffQueryMsg{
		MessageID:   []byte{0x11},
		QueryString: "SELECT * FROM users",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil || *result.Error != context.Canceled.Error() {
		t.Fatalf("Error = %v, want %q", result.Error, context.Canceled.Error())
	}
	if !snap.closed {
		t.Fatal("snapshot should be closed after canceled one-off query")
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

func TestHandleOneOffQuery_LowercaseXEscapedStringOnBytesRejected(t *testing.T) {
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
				{types.NewUint32(1), types.NewBytes([]byte{0xAB})},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1a},
		QueryString: "SELECT * FROM s WHERE bytes = 'x''AB'",
	}

	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	want := "The literal expression `x'AB` cannot be parsed as type `Array<U8>`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
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

func TestHandleOneOffQuery_JoinFilterCrossSideOrDoesNotPassEveryPair(t *testing.T) {
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
				{types.NewUint32(2), types.NewUint32(10)},
				{types.NewUint32(3), types.NewUint32(30)},
			},
			sReg.ID: {
				{types.NewUint32(20), types.NewUint32(10)},
				{types.NewUint32(30), types.NewUint32(30)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x7e},
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = 1 OR s.id = 30",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), tTS)
	wantRows := []types.ProductValue{
		{types.NewUint32(1), types.NewUint32(10)},
		{types.NewUint32(3), types.NewUint32(30)},
	}
	assertProductRowsEqual(t, gotRows, wantRows)
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

func TestHandleOneOffQuery_CaseDistinctRelationAliasesRouteJoinSides(t *testing.T) {
	conn := testConnDirect(nil)
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
	_, tReg, _ := eng.Registry().TableByName("t")
	_, sReg, _ := eng.Registry().TableByName("s")
	tTS := &schema.TableSchema{ID: tReg.ID, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}, {Index: 1, Name: "u32", Type: schema.KindUint32}}}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		tReg.ID: {
			{types.NewUint32(10), types.NewUint32(7)},
			{types.NewUint32(11), types.NewUint32(8)},
		},
		sReg.ID: {
			{types.NewUint32(20), types.NewUint32(7)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x22, 0x01},
		QueryString: `SELECT "R".* FROM t AS "R" JOIN s AS r ON "R".u32 = r.u32`,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)
	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), tTS)
	want := []types.ProductValue{{types.NewUint32(10), types.NewUint32(7)}}
	assertProductRowsEqual(t, pvs, want)
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

// self-join projection contract: one-off self-join RHS projection (`SELECT b.*`) must
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

// TestHandleOneOffQuery_ShunterSenderResolvesToHexOnStringColumn pins :sender
// matching on String columns as caller identity hex text.
func TestHandleOneOffQuery_ShunterSenderResolvesToHexOnStringColumn(t *testing.T) {
	conn := testConnDirect(nil)
	conn.Identity = types.Identity{1, 2, 3}
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)
	callerHex := conn.Identity.Hex()
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewString("alice")},
				{types.NewString(callerHex)},
				{types.NewString("zach")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x62},
		QueryString: "SELECT * FROM t WHERE name = :sender",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (resolve_sender → String widening)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1 matching caller hex", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewString(callerHex)) {
		t.Errorf("row[0].name = %v, want String(%q)", pvs[0][0], callerHex)
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
// rejection fires while coercing the parsed SQL literal against the resolved
// column type, so the one-off reply must arrive with Status=1 and a non-empty
// Error.
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

// TestHandleOneOffQuery_ShunterStringDigitsOnIntegerColumnWidens pins the
// reference widening at expr/src/lib.rs:255-352. `WHERE u32 = '42'` must
// now succeed: parse_int → BigDecimal::from_str("42") → BigDecimal::to_u32
// → Uint32(42). Shunter routes the LitString through `parseNumericLiteral`
// at the coerce boundary and recurses with the resulting LitInt, so the
// admission accepts and the executor scans for u32 == 42.
func TestHandleOneOffQuery_ShunterStringDigitsOnIntegerColumnWidens(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "u32", Type: schema.KindUint32},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewUint32(1)},
				{types.NewUint32(42)},
				{types.NewUint32(99)},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x91},
		QueryString: "SELECT * FROM t WHERE u32 = '42'",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (digit-only widening must succeed)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1 matching u32 == 42", len(pvs))
	}
	if pvs[0][0].AsUint32() != 42 {
		t.Errorf("row[0].u32 = %d, want 42", pvs[0][0].AsUint32())
	}
}

// TestHandleOneOffQuery_ShunterNonNumericStringOnIntegerEmitsInvalidLiteral
// pins the reference reject text at expr/src/errors.rs:84 flowing through
// the new LitString-on-numeric path. `WHERE u32 = 'foo'` must emit “ The
// literal expression `foo` cannot be parsed as type `U32` “ rather than
// the prior generic "string literal cannot be coerced to uint32" text.
// Reference parse_int → BigDecimal::from_str("foo") → None → folds to
// InvalidLiteral via the lib.rs:99 .map_err. The wrapper-bypass in
// `normalizeSQLFilterForRelations` already passes InvalidLiteralError
// through unwrapped.
func TestHandleOneOffQuery_ShunterNonNumericStringOnIntegerEmitsInvalidLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x92},
		QueryString: "SELECT * FROM t WHERE u32 = 'foo'",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	want := "The literal expression `foo` cannot be parsed as type `U32`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterNumericLiteralOnStringColumnWidens pins the
// reference widening at expr/src/lib.rs:353 onto the OneOffQuery admission
// surface. `WHERE name = 42` must succeed and return the row whose `name`
// column equals the string `"42"`; `WHERE name = 1.3` must succeed and
// return the row whose `name` equals `"1.3"`. Reference flows the
// SqlLiteral source text through `parse(value, String)` →
// `AlgebraicValue::String(value.into())`; Shunter renders LitInt via
// `strconv.FormatInt` and LitFloat via `strconv.FormatFloat('g', -1, 64)`
// at the coerce boundary so the bound predicate is `name = "<literal>"`.
func TestHandleOneOffQuery_ShunterNumericLiteralOnStringColumnWidens(t *testing.T) {
	cases := []struct {
		name        string
		sql         string
		matchString string
	}{
		{"LitInt", "SELECT * FROM t WHERE name = 42", "42"},
		{"LitFloat", "SELECT * FROM t WHERE name = 1.3", "1.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			ts := &schema.TableSchema{
				ID:   1,
				Name: "t",
				Columns: []schema.ColumnSchema{
					{Index: 0, Name: "name", Type: schema.KindString},
				},
			}
			sl := newMockSchema("t", 1, ts.Columns...)

			snap := &mockSnapshot{
				rows: map[schema.TableID][]types.ProductValue{
					1: {
						{types.NewString("alice")},
						{types.NewString(tc.matchString)},
						{types.NewString("zach")},
					},
				},
			}
			stateAccess := &mockStateAccess{snap: snap}

			msg := &OneOffQueryMsg{
				MessageID:   []byte{0x90},
				QueryString: tc.sql,
			}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

			result := drainOneOff(t, conn)
			if result.Error != nil {
				t.Fatalf("Error = %q, want nil (widening must succeed)", *result.Error)
			}
			pvs := decodeRows(t, firstTableRows(result), ts)
			if len(pvs) != 1 {
				t.Fatalf("got %d rows, want 1 matching %q", len(pvs), tc.matchString)
			}
			if !pvs[0][0].Equal(types.NewString(tc.matchString)) {
				t.Errorf("row[0].name = %v, want String(%q)", pvs[0][0], tc.matchString)
			}
		})
	}
}

// TestHandleOneOffQuery_ShunterScientificLiteralOverflowPreservesSourceText
// pins the source-text seam through the OneOff (raw) admission surface:
// `WHERE u8 = 1e3` collapses at the parser to LitInt(1000) but keeps the
// `1e3` source token in `Literal.Text`. Reference parse_int folds the
// to_u8 None into `InvalidLiteral::new("1e3", U8)` (lib.rs:99); Shunter
// renders the same text via `renderLiteralSourceText`. The wrapper bypass
// in `normalizeSQLFilterForRelations` already passes InvalidLiteralError
// through unwrapped, so the OneOff error reply carries the verbatim
// reference literal.
func TestHandleOneOffQuery_ShunterScientificLiteralOverflowPreservesSourceText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x93},
		QueryString: "SELECT * FROM t WHERE u8 = 1e3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	want := "The literal expression `1e3` cannot be parsed as type `U8`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterHexLiteralWidensOntoStringColumn pins the
// reference `parse(value, String)` widening at lib.rs:353 onto the OneOff
// admission surface for a Hex source-text literal. `WHERE name =
// 0xDEADBEEF` keeps the original token through `Literal.Text` (parser sets
// it on tokHex), so the widened String value is the original token
// `"0xDEADBEEF"` and the snapshot row matches.
func TestHandleOneOffQuery_ShunterHexLiteralWidensOntoStringColumn(t *testing.T) {
	conn := testConnDirect(nil)
	ts := &schema.TableSchema{
		ID:   1,
		Name: "t",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("t", 1, ts.Columns...)
	snap := &mockSnapshot{
		rows: map[schema.TableID][]types.ProductValue{
			1: {
				{types.NewString("alice")},
				{types.NewString("0xDEADBEEF")},
				{types.NewString("zach")},
			},
		},
	}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x94},
		QueryString: "SELECT * FROM t WHERE name = 0xDEADBEEF",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (hex widening must succeed)", *result.Error)
	}
	pvs := decodeRows(t, firstTableRows(result), ts)
	if len(pvs) != 1 {
		t.Fatalf("got %d rows, want 1 matching String(\"0xDEADBEEF\")", len(pvs))
	}
	if !pvs[0][0].Equal(types.NewString("0xDEADBEEF")) {
		t.Errorf("row[0].name = %v, want String(\"0xDEADBEEF\")", pvs[0][0])
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

// TestHandleOneOffQuery_ShunterUnknownTableRejected pins the reference type-
// check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// 483-485 (`select * from r` / "Table r does not exist") onto the OneOff
// admission surface. Enforced incidentally via SchemaLookup.TableByName
// returning !ok inside compileSQLQueryString; the pin names the contract.
func TestHandleOneOffQuery_ShunterUnknownTableRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterUnknownColumnRejected pins the reference type-
// check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// 491-493 (`select * from t where t.a = 1` / "Field a does not exist on
// table t") onto the OneOff admission surface. Enforced incidentally via
// rel.ts.Column returning !ok inside normalizeSQLFilterForRelations.
func TestHandleOneOffQuery_ShunterUnknownColumnRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterAliasedUnknownColumnRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 495-497 (`select * from t as r where r.a = 1` / "Field a
// does not exist on table t") onto the OneOff admission surface. The
// aliased single-table shape resolves `r` to base table `t` in the parser's
// relationBindings; normalizeSQLFilterForRelations then fails the
// rel.ts.Column lookup. Keeps the rejection named on the alias-qualified
// surface.
func TestHandleOneOffQuery_ShunterAliasedUnknownColumnRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterBaseTableQualifierAfterAliasRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 506-509 (`select * from t as r where t.u32 = 5` / "t is not
// in scope after alias") onto the OneOff admission surface. Enforced
// incidentally at parser level via resolveQualifier in parseComparison.
func TestHandleOneOffQuery_ShunterBaseTableQualifierAfterAliasRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterBareColumnProjectionReturnsProjectedRows pins the
// query-only single-table column-projection slice on the OneOff path: the
// parser/compile seam may now accept `SELECT u32 FROM t`, and one-off must
// return only the selected column values while keeping the outer table envelope
// unchanged.
func TestHandleOneOffQuery_ShunterBareColumnProjectionReturnsProjectedRows(t *testing.T) {
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

func TestHandleOneOffQuery_UnquotedLiteralKeywordProjectionRejectedBeforeColumnLookup(t *testing.T) {
	for _, query := range []string{
		"SELECT TRUE FROM t",
		"SELECT FALSE FROM t",
		"SELECT NULL FROM t",
	} {
		t.Run(query, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "true", Type: schema.KindBool},
				schema.ColumnSchema{Index: 1, Name: "false", Type: schema.KindBool},
				schema.ColumnSchema{Index: 2, Name: "null", Type: schema.KindUint32},
			)
			snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
				{types.NewBool(true), types.NewBool(false), types.NewUint32(1)},
			}}}
			stateAccess := &mockStateAccess{snap: snap}

			msg := &OneOffQueryMsg{
				MessageID:   []byte{0x86, 0x01},
				QueryString: query,
			}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

			result := drainOneOff(t, conn)
			if result.Error == nil {
				t.Fatal("expected error, got nil (success)")
			}
		})
	}
}

func TestHandleOneOffQuery_UnquotedNullWhereRejectedBeforeColumnLookup(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "null", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x86, 0x02},
		QueryString: "SELECT * FROM t WHERE NULL = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
}

func TestHandleOneOffQuery_ShunterMultiColumnProjectionReturnsProjectedRows(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterAliasedBareColumnProjectionReturnsProjectedRows(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterAliasedBareColumnProjectionWithWhereReturnsProjectedRows(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterAliasedMultiColumnProjectionReturnsProjectedRows(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterJoinColumnProjectionReturnsProjectedRows(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterJoinColumnProjectionProjectsRight(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterJoinColumnProjectionAllowsMixedRelations(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterSelfJoinColumnProjectionProjectsLeft(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterSelfJoinColumnProjectionProjectsRight(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterJoinWithoutQualifiedProjectionRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 515-517 (`select * from t join s` / "Subscriptions must be
// typed to a single table") onto the OneOff admission surface. Enforced
// incidentally at parseStatement requiring a qualified projection for joins.
func TestHandleOneOffQuery_ShunterJoinWithoutQualifiedProjectionRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterJoinStarProjectionRejectText pins the
// reference type-check rejection text at
// reference/SpacetimeDB/crates/expr/src/errors.rs:41
// (`InvalidWildcard::Join` = "SELECT * is not supported for joins"),
// emit site reference/SpacetimeDB/crates/expr/src/lib.rs:56 via
// `type_proj` when `ast::Project::Star(None)` meets an input with
// `nfields() > 1`. The OneOff admission surface (module_host.rs:2252
// `compile_subscription`, :2316 `format!("{err}")`) emits the raw error
// text with no `DBError::WithSql` wrap, unlike the subscribe paths.
func TestHandleOneOffQuery_ShunterJoinStarProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x88},
		QueryString: "SELECT * FROM t JOIN s",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "SELECT * is not supported for joins"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterSelfJoinWithoutAliasesRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 519-521 (`select t.* from t join t` / "Self join requires
// aliases") onto the OneOff admission surface. Enforced incidentally at
// parseJoinClause when both sides share the same table and alias.
func TestHandleOneOffQuery_ShunterSelfJoinWithoutAliasesRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterForwardAliasReferenceRejected pins the reference
// type-check rejection at reference/SpacetimeDB/crates/expr/src/check.rs lines
// 526-528 (`select t.* from t join s on t.u32 = r.u32 join s as r` / "Alias
// r is not in scope when it is referenced") onto the OneOff admission surface.
// Enforced incidentally in parseQualifiedColumnRef when the forward alias
// reference fails resolveQualifier against the first join's lookup.
func TestHandleOneOffQuery_ShunterForwardAliasReferenceRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterLimitClauseAppliesToVisibleRows pins the
// query-only LIMIT slice from the reference SQL grammar onto Shunter's one-off
// handler: after the existing row-shaped evaluation path produces matches, the
// handler caps the visible result rows without changing the scan order contract
// beyond this deterministic mock harness.
func TestHandleOneOffQuery_ShunterLimitClauseAppliesToVisibleRows(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterScientificLimitLiteralApplies(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	ts, ok := sl.Table(1)
	if !ok {
		t.Fatal("mock schema missing table 1")
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(7)},
		{types.NewUint32(8)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8B},
		QueryString: "SELECT * FROM t LIMIT 1e3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), ts)
	if len(gotRows) != 2 {
		t.Fatalf("row count = %d, want 2", len(gotRows))
	}
}

func TestHandleOneOffQuery_ShunterFractionalLimitLiteralRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(7)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x8C},
		QueryString: "SELECT * FROM t LIMIT 1.5",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "The literal expression `1.5` cannot be parsed as type `U64`"
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterLeadingPlusIntLiteral pins the reference
// valid-literal shape at reference/SpacetimeDB/crates/expr/src/check.rs:297-
// 300 (`select * from t where u32 = +1` / "Leading `+`"): a leading `+` on
// an integer literal is admitted end-to-end through the OneOff path.
func TestHandleOneOffQuery_ShunterLeadingPlusIntLiteral(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterUnqualifiedWhereInJoinRejected pins the
// reference type-check rejection at reference/SpacetimeDB/crates/expr/src/
// check.rs lines 534-537 (`select t.* from t join s on t.u32 = s.u32 where
// bytes = 0xABCD` / "Columns must be qualified in join expressions") onto the
// OneOff admission surface. Enforced incidentally at parseComparison when the
// relation binding has requireQualify set by the join.
func TestHandleOneOffQuery_ShunterUnqualifiedWhereInJoinRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterScientificNotationUnsignedInteger pins the
// reference valid-literal shape at reference/SpacetimeDB/crates/expr/src/
// check.rs:302-304 (`select * from t where u32 = 1e3` / "Scientific
// notation") on the OneOff admission path.
func TestHandleOneOffQuery_ShunterScientificNotationUnsignedInteger(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterScientificNotationFloatNegativeExponent pins
// reference/SpacetimeDB/crates/expr/src/check.rs:314-316 (`select * from t
// where f32 = 1e-3` / "Negative exponent") on the OneOff admission path.
func TestHandleOneOffQuery_ShunterScientificNotationFloatNegativeExponent(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterLeadingDotFloatLiteral pins reference/
// SpacetimeDB/crates/expr/src/check.rs:322-324 (`select * from t where
// f32 = .1` / "Leading `.`") on the OneOff admission path.
func TestHandleOneOffQuery_ShunterLeadingDotFloatLiteral(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterScientificNotationOverflowInfinity pins
// reference/SpacetimeDB/crates/expr/src/check.rs:326-328 (`select * from t
// where f32 = 1e40` / "Infinity") on the OneOff admission path. The stored
// row is +Inf on the f32 column; the query literal `1e40` must coerce to
// the same +Inf value and match.
func TestHandleOneOffQuery_ShunterScientificNotationOverflowInfinity(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterInvalidLiteralNegativeIntOnUnsignedRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:382-385 (`select * from t
// where u8 = -1` / "Negative integer for unsigned column") onto the
// OneOffQuery admission surface. `-1` is LitInt(-1); coerceUnsigned
// (query/sql/coerce.go:119) rejects negative literals against unsigned
// columns during SQL predicate compilation, producing Status=1 with a
// non-empty Error message.
func TestHandleOneOffQuery_ShunterInvalidLiteralNegativeIntOnUnsignedRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterInvalidLiteralScientificOverflowRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:386-389 (`select * from t
// where u8 = 1e3` / "Out of bounds") onto the OneOffQuery admission surface.
// `1e3` collapses to LitInt(1000) via parseNumericLiteral; coerceUnsigned
// (query/sql/coerce.go:123) rejects the value as out of range for u8.
func TestHandleOneOffQuery_ShunterInvalidLiteralScientificOverflowRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterInvalidLiteralFloatOnUnsignedRejected pins
// reference/SpacetimeDB/crates/expr/src/check.rs:390-393 (`select * from t
// where u8 = 0.1` / "Float as integer") onto the OneOffQuery admission
// surface. Complements the existing u32 = 1.3 pin by naming the u8 column
// variant; coerceUnsigned rejects LitFloat against an integer column.
func TestHandleOneOffQuery_ShunterInvalidLiteralFloatOnUnsignedRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterInvalidLiteralNegativeExponentOnUnsignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:394-397 (`select * from
// t where u32 = 1e-3` / "Float as integer") onto the OneOffQuery admission
// surface. `1e-3` stays LitFloat (non-integral) and coerceUnsigned rejects
// it against an unsigned column.
func TestHandleOneOffQuery_ShunterInvalidLiteralNegativeExponentOnUnsignedRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterInvalidLiteralNegativeExponentOnSignedRejected
// pins reference/SpacetimeDB/crates/expr/src/check.rs:398-401 (`select * from
// t where i32 = 1e-3` / "Float as integer") onto the OneOffQuery admission
// surface. Mirrors the unsigned case on a signed column: coerceSigned rejects
// the LitFloat against KindInt32.
func TestHandleOneOffQuery_ShunterInvalidLiteralNegativeExponentOnSignedRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterValidLiteralOnEachIntegerWidth pins
// reference/SpacetimeDB/crates/expr/src/check.rs:360-370
// (`valid_literals_for_type`) at the OneOffQuery admission surface. Each
// subtest builds a single-column table, stores a matching row, and
// confirms `SELECT * FROM t WHERE {colname} = 127` accepts end-to-end on
// every numeric column kind realized by `schema.ValueKind`
// (i8/u8/i16/u16/i32/u32/i64/u64/f32/f64 plus i128/u128 added 2026-04-21
// slice 1 and i256/u256 added 2026-04-21 slice 2). The reference
// `u256 = 1e40` row stays deferred until BigDecimal literal widening.
func TestHandleOneOffQuery_ShunterValidLiteralOnEachIntegerWidth(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterValidLiteralU256Scientific pins the remaining
// reference `valid_literals` row at
// reference/SpacetimeDB/crates/expr/src/check.rs:330-332
// (`select * from t where u256 = 1e40` / "u256") at the OneOffQuery
// admission surface. Shunter's parser promotes `1e40` to LitBigInt and
// coerce decomposes 10^40 into four uint64 words for the 256-bit Uint256
// layout. The snapshot holds one matching row so the query should return
// Status == 0.
func TestHandleOneOffQuery_ShunterValidLiteralU256Scientific(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterUint256NegativeRejected extends the
// reference invalid_literals bundle at check.rs:382-385 to the Uint256
// column kind. Mirrors the subscribe-side pin.
func TestHandleOneOffQuery_ShunterUint256NegativeRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterTimestampLiteralAccepted pins the reference
// valid_literals rows at check.rs:334-352 onto the OneOff admission surface.
// Each subtest builds a Timestamp-column table, stores a matching row, and
// confirms `SELECT * FROM t WHERE ts = '<shape>'` accepts end-to-end across
// all five reference RFC3339 shapes.
func TestHandleOneOffQuery_ShunterTimestampLiteralAccepted(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterTimestampMalformedRejected pins reference
// `InvalidLiteral` text for a non-RFC3339 string on a Timestamp column.
// Reference path: `parse(value, Timestamp)` (expr/src/lib.rs:359) hits the
// catch-all `bail!`, folded by lib.rs:99 `.map_err` into
// `InvalidLiteral::new(v.into_string(), ty)`. Timestamp renders as the
// Product `(__timestamp_micros_since_unix_epoch__: I64)`. OneOff admission
// has no DBError::WithSql wrap.
func TestHandleOneOffQuery_ShunterTimestampMalformedRejected(t *testing.T) {
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
	want := "The literal expression `not-a-timestamp` cannot be parsed as type `(__timestamp_micros_since_unix_epoch__: I64)`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterBoolLiteralOnTimestampRejectText pins the
// reference `UnexpectedType` literal for a bool literal targeting a
// Timestamp column. Reference path: lib.rs:94 routes
// `(SqlExpr::Lit(SqlLiteral::Bool(_)), Some(ty))` directly to
// `UnexpectedType` (errors.rs:100). Timestamp inferred name comes from the
// SATS Product fmt.
func TestHandleOneOffQuery_ShunterBoolLiteralOnTimestampRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "ts", Type: schema.KindTimestamp},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewTimestamp(0)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD6},
		QueryString: "SELECT * FROM t WHERE ts = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unexpected type: (expected) Bool != (__timestamp_micros_since_unix_epoch__: I64) (inferred)"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterStringLiteralOnArrayStringRejectText pins
// reference `InvalidLiteral` text for a scalar literal on a KindArrayString
// column. Reference `parse(value, Array<String>)` at lib.rs:359 falls
// through the array-kind catch-all, folded by lib.rs:99 into
// `InvalidLiteral::new(v.into_string(), ty)`. Array<String> renders through
// the parameterized array form.
func TestHandleOneOffQuery_ShunterStringLiteralOnArrayStringRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "arr", Type: schema.KindArrayString},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD7},
		QueryString: "SELECT * FROM t WHERE arr = 'x'",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `x` cannot be parsed as type `Array<String>`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterBoolLiteralOnArrayStringRejectText pins the
// reference `UnexpectedType` literal for a bool literal targeting an
// Array<String> column. Reference lib.rs:94 routes the bool arm to
// `UnexpectedType` ahead of the lib.rs:99 InvalidLiteral fallback.
func TestHandleOneOffQuery_ShunterBoolLiteralOnArrayStringRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "arr", Type: schema.KindArrayString},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD8},
		QueryString: "SELECT * FROM t WHERE arr = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unexpected type: (expected) Bool != Array<String> (inferred)"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUint128NegativeRejected extends the
// reference invalid_literals bundle at check.rs:382-385 to the Uint128
// column kind. Mirrors the subscribe-side pin.
func TestHandleOneOffQuery_ShunterUint128NegativeRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterDMLStatementRejected pins DML rejection on
// OneOff admission.
func TestHandleOneOffQuery_ShunterDMLStatementRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterEmptyStatementRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (empty string / "Empty") onto the OneOff admission surface. Enforced
// incidentally at expectKeyword("SELECT") on an EOF-only token stream.
func TestHandleOneOffQuery_ShunterEmptyStatementRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterWhitespaceOnlyStatementRejected pins the
// reference subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (single space / "Empty after whitespace skip") onto the OneOff admission
// surface. Enforced incidentally once the tokenizer drops whitespace.
func TestHandleOneOffQuery_ShunterWhitespaceOnlyStatementRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterDistinctProjectionRejected pins the reference
// SQL parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:362-394 — the
// `parse_select` arm requires `distinct: None`; any non-None set quantifier
// falls into `_ => SqlUnsupported::feature(select)`, which the OneOff
// surface renders as `Unsupported: {select}`.
func TestHandleOneOffQuery_ShunterDistinctProjectionRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT DISTINCT u32 FROM t"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xB3},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unsupported: " + sqlText
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterAllModifierRejected pins the reference SQL
// parser rejection at sql.rs:362-394 for `SELECT ALL ...`. The set
// quantifier `ALL` produces a non-None `distinct` field which the
// `parse_select` arm rejects through `SqlUnsupported::feature(select)`.
// The test schema deliberately includes a column named `ALL` to confirm
// the parser detects the modifier rather than reinterpreting the keyword
// as a column reference with output alias `u32`.
func TestHandleOneOffQuery_ShunterAllModifierRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "ALL", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(7)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT ALL u32 FROM t"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xEE},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unsupported: " + sqlText
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterSubqueryInFromRejected pins the reference
// subscription-parser rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs lines 157-168
// (`select * from (select * from t) join (select * from s) on a = b` /
// "Subqueries in FROM not supported") onto the OneOff admission surface.
// Enforced incidentally at parseStatement which requires an identifier token
// after FROM.
func TestHandleOneOffQuery_ShunterSubqueryInFromRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedSelectLiteralWithoutFromRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select 1` / "FROM is required") onto the OneOff admission surface.
// parseProjection rejects the integer literal `1` with "projection must be
// '*' or 'table.*'".
func TestHandleOneOffQuery_ShunterSqlUnsupportedSelectLiteralWithoutFromRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedMultiPartTableNameRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a from s.t` / "Multi-part table names") onto the OneOff admission
// surface. parseProjection rejects the bare identifier `a` before FROM is
// parsed, so rejection fires with "projection must be '*' or 'table.*'".
func TestHandleOneOffQuery_ShunterSqlUnsupportedMultiPartTableNameRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedBitStringLiteralRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select * from t where a = B'1010'` / "Bit-string literals") onto the
// OneOff admission surface. The lexer tokenizes `B` as an identifier and
// `'1010'` as a separate string literal; parseLiteral rejects the identifier
// RHS of `=` with "expected literal, got identifier \"B\"".
func TestHandleOneOffQuery_ShunterSqlUnsupportedBitStringLiteralRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedWildcardWithBareColumnsRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a.*, b, c from t` / "Wildcard with non-wildcard projections") onto
// the OneOff admission surface. After parseProjection consumes `t.*`,
// parseStatement expects FROM but finds `,` and rejects with
// "expected FROM, got \",\"".
func TestHandleOneOffQuery_ShunterSqlUnsupportedWildcardWithBareColumnsRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedOrderByWithLimitExpressionRejected
// pins the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select * from t order by a limit b` / "Limit expression") onto the OneOff
// admission surface. ORDER BY trips parseStatement's EOF guard
// (query/sql/parser.go:547-549) with "unexpected token \"ORDER\"" before the
// LIMIT identifier is examined.
func TestHandleOneOffQuery_ShunterSqlUnsupportedOrderByWithLimitExpressionRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedAggregateWithGroupByRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a, count(*) from t group by a` / "GROUP BY") onto the OneOff
// admission surface. parseProjection rejects the leading bare column with
// "projection must be '*' or 'table.*'" before the aggregate or GROUP BY
// keyword is ever seen.
func TestHandleOneOffQuery_ShunterSqlUnsupportedAggregateWithGroupByRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedImplicitCommaJoinRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select a.* from t as a, s as b where a.id = b.id and b.c = 1` /
// "Implicit joins") onto the OneOff admission surface. After consuming
// `t AS a`, parseStatement's EOF/keyword guard hits `,` and rejects with
// "unexpected token \",\"".
func TestHandleOneOffQuery_ShunterSqlUnsupportedImplicitCommaJoinRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlUnsupportedUnqualifiedJoinOnVarsRejected pins
// the reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 411-436
// (`select t.* from t join s on int = u32` / "Joins require qualified vars")
// onto the OneOff admission surface. parseJoinClause calls
// parseQualifiedColumnRef for the left side of ON
// (query/sql/parser.go:629); the bare identifier `int` fails there with
// "expected qualified column reference".
func TestHandleOneOffQuery_ShunterSqlUnsupportedUnqualifiedJoinOnVarsRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlInvalidEmptySelectRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select from t` / "Empty SELECT") onto the OneOff admission surface.
// parseProjection rejects because the next token after SELECT is the
// identifier `from`, which is then followed by `t` (not a dot), so the
// projection fails with "projection must be '*' or 'table.*'".
func TestHandleOneOffQuery_ShunterSqlInvalidEmptySelectRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlInvalidEmptyFromRejected pins the reference
// parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a from where b = 1` / "Empty FROM") onto the OneOff admission
// surface. parseProjection rejects the bare column `a` with "projection must
// be '*' or 'table.*'" before the empty FROM is examined.
func TestHandleOneOffQuery_ShunterSqlInvalidEmptyFromRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlInvalidEmptyWhereRejected pins the reference
// parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a from t where` / "Empty WHERE") onto the OneOff admission
// surface. parseProjection rejects the bare column `a` with "projection must
// be '*' or 'table.*'" before the empty WHERE is examined.
func TestHandleOneOffQuery_ShunterSqlInvalidEmptyWhereRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterSqlInvalidEmptyGroupByRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select a, count(*) from t group by` / "Empty GROUP BY") onto the OneOff
// admission surface. parseProjection rejects the leading bare column `a` with
// "projection must be '*' or 'table.*'" before the aggregate or empty GROUP
// BY is examined.
func TestHandleOneOffQuery_ShunterSqlInvalidEmptyGroupByRejected(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterCountAliasReturnsSingleAggregateRow(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterCountBareAliasReturnsSingleAggregateRow(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterCountAliasWithWhereReturnsSingleAggregateRow(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterCountColumnAliasWithWhereLimitReturnsFullAggregate(t *testing.T) {
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
		MessageID:   []byte{0xCD},
		QueryString: "SELECT COUNT(u32) AS n FROM t WHERE active = TRUE LIMIT 1",
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

func TestHandleOneOffQuery_ShunterSumUintColumnAliasWithWhereLimitReturnsFullAggregate(t *testing.T) {
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
		Columns: []schema.ColumnSchema{{Index: 0, Name: "total", Type: schema.KindUint64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD0},
		QueryString: "SELECT SUM(u32) AS total FROM t WHERE active = TRUE LIMIT 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(4)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ShunterSumSignedColumnReturnsInt64Aggregate(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "i32", Type: schema.KindInt32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewInt32(7)},
		{types.NewInt32(-3)},
		{types.NewInt32(2)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "total", Type: schema.KindInt64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD1},
		QueryString: "SELECT SUM(i32) AS total FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewInt64(6)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ShunterSumFloatColumnReturnsFloat64Aggregate(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "f32", Type: schema.KindFloat32},
	)
	v1, err := types.NewFloat32(1.5)
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	v2, err := types.NewFloat32(2.25)
	if err != nil {
		t.Fatalf("NewFloat32: %v", err)
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{v1},
		{v2},
	}}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{
		ID:      1,
		Name:    "t",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "total", Type: schema.KindFloat64}},
	}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD2},
		QueryString: "SELECT SUM(f32) AS total FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantValue, err := types.NewFloat64(3.75)
	if err != nil {
		t.Fatalf("NewFloat64: %v", err)
	}
	wantRows := []types.ProductValue{{wantValue}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ShunterSumNonNumericColumnRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "label", Type: schema.KindString},
	)
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD3},
		QueryString: "SELECT SUM(label) AS total FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected SUM non-numeric rejection, got nil error")
	}
	if got, want := *result.Error, "SUM aggregate only supports 64-bit integer and float columns"; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

func TestHandleOneOffQuery_ShunterCountColumnOrderByRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xCF},
		QueryString: "SELECT COUNT(u32) AS n FROM t ORDER BY u32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected ORDER BY aggregate rejection, got nil error")
	}
	if got, want := *result.Error, "ORDER BY is not supported with aggregate projections"; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

func TestHandleOneOffQuery_ShunterCountAliasZeroRowsReturnsSingleZeroRow(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterJoinCountAliasReturnsSingleAggregateRow(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterJoinCountColumnAliasReturnsSingleAggregateRow(t *testing.T) {
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
		MessageID:   []byte{0xCE},
		QueryString: "SELECT COUNT(s.active) AS n FROM t JOIN s ON t.id = s.t_id WHERE s.active = TRUE",
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

func TestHandleOneOffQuery_ShunterJoinSumColumnAliasReturnsSingleAggregateRow(t *testing.T) {
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
			{Index: 1, Name: "points", Type: schema.KindUint32},
			{Index: 2, Name: "active", Type: schema.KindBool},
		}}},
	}}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1)},
			{types.NewUint32(2)},
		},
		2: {
			{types.NewUint32(1), types.NewUint32(5), types.NewBool(true)},
			{types.NewUint32(1), types.NewUint32(7), types.NewBool(false)},
			{types.NewUint32(2), types.NewUint32(11), types.NewBool(true)},
		},
	}}
	stateAccess := &mockStateAccess{snap: snap}
	aggregateSchema := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "total", Type: schema.KindUint64}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xD4},
		QueryString: "SELECT SUM(s.points) AS total FROM t JOIN s ON t.id = s.t_id WHERE s.active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (success)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(16)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

func TestHandleOneOffQuery_ShunterJoinCountBareAliasWithWhereReturnsSingleAggregateRow(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterJoinCountAliasOnCrossJoinWhereEqualityReturnsAggregate
// pins that one-off/ad hoc SQL counts matched rows for the already-accepted
// cross-join WHERE column-equality shape with the already-accepted join-backed
// COUNT(*) AS alias aggregate.
// The bounded combination yields one uint64 aggregate row equal to the number
// of matched join pairs, with multiplicity preserved on duplicates.
func TestHandleOneOffQuery_ShunterJoinCountAliasOnCrossJoinWhereEqualityReturnsAggregate(t *testing.T) {
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
		MessageID:   []byte{0xC8},
		QueryString: "SELECT COUNT(*) AS n FROM t JOIN s WHERE t.id = s.t_id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (cross-join WHERE + COUNT aggregate is one-off-only accepted)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single aggregate envelope for t", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ShunterJoinCountBareAliasOnCrossJoinWhereEqualityAndFilterReturnsAggregate
// pins the bounded combination of the cross-join WHERE equality-plus-single-
// literal-filter shape with the join-backed COUNT(*) alias aggregate. Only
// matched-and-filtered join pairs are counted, the bare alias form is accepted,
// and the result is a single
// uint64 row under the requested alias.
func TestHandleOneOffQuery_ShunterJoinCountBareAliasOnCrossJoinWhereEqualityAndFilterReturnsAggregate(t *testing.T) {
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
		MessageID:   []byte{0xC9},
		QueryString: "SELECT COUNT(*) n FROM t JOIN s WHERE t.id = s.t_id AND s.active = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (cross-join WHERE equality + filter + COUNT aggregate is one-off-only accepted)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ShunterCountAliasWithLimitOneReturnsFullAggregate pins
// that one-off/ad hoc SQL counts the full matched input before applying LIMIT
// to the one-row aggregate output. A naive implementation that limited
// matchedRows before aggregate shaping would count uint64(1); the correct
// behavior is uint64(2), the full matched-input count.
func TestHandleOneOffQuery_ShunterCountAliasWithLimitOneReturnsFullAggregate(t *testing.T) {
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
		MessageID:   []byte{0xCA},
		QueryString: "SELECT COUNT(*) AS n FROM t WHERE active = TRUE LIMIT 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (aggregate + LIMIT is one-off-only accepted)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ShunterCountAliasWithLimitZeroReturnsNoRows pins that
// LIMIT 0 drops the one-row aggregate output entirely (reference
// ProjectList::Limit on top of ProjectList::Agg(Count)), rather than emitting
// one row containing zero.
func TestHandleOneOffQuery_ShunterCountAliasWithLimitZeroReturnsNoRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(true)},
		{types.NewUint32(2), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xCB},
		QueryString: "SELECT COUNT(*) AS n FROM t LIMIT 0",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (LIMIT 0 is one-off-only accepted)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single aggregate envelope for t", result.Tables)
	}
	rawRows, err := DecodeRowList(firstTableRows(result))
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rawRows) != 0 {
		t.Fatalf("rows = %d, want 0 (LIMIT 0 drops aggregate output row)", len(rawRows))
	}
}

// TestHandleOneOffQuery_ShunterCountAliasWithOffsetReturnsNoRows pins that
// COUNT(*) still counts the full matched input, then OFFSET is applied to the
// one-row aggregate output. OFFSET 1 therefore skips that aggregate row.
func TestHandleOneOffQuery_ShunterCountAliasWithOffsetReturnsNoRows(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "active", Type: schema.KindBool},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {
		{types.NewUint32(1), types.NewBool(true)},
		{types.NewUint32(2), types.NewBool(true)},
	}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xCC},
		QueryString: "SELECT COUNT(*) AS n FROM t WHERE active = TRUE OFFSET 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (aggregate + OFFSET is one-off-only accepted)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single aggregate envelope for t", result.Tables)
	}
	rawRows, err := DecodeRowList(firstTableRows(result))
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rawRows) != 0 {
		t.Fatalf("rows = %d, want 0 (OFFSET 1 skips aggregate output row)", len(rawRows))
	}
}

// TestHandleOneOffQuery_ShunterJoinCountWithLimitReturnsFullAggregate replaces
// the prior aggregate+LIMIT rejection pin. It proves that join multiplicity is
// counted across the full matched input before LIMIT is applied to the one-row
// aggregate output. A naive implementation that limited matchedRows first would
// report uint64(1); the correct behavior is uint64(2), the full matched-pair
// count.
func TestHandleOneOffQuery_ShunterJoinCountWithLimitReturnsFullAggregate(t *testing.T) {
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
		MessageID:   []byte{0xC7},
		QueryString: "SELECT COUNT(*) AS n FROM t JOIN s ON t.id = s.t_id LIMIT 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (join COUNT + LIMIT is one-off-only accepted)", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("Tables = %+v, want single aggregate envelope for t", result.Tables)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ShunterCrossJoinWhereCountWithLimitReturnsFullAggregate
// extends the LIMIT-on-aggregate composition onto the cross-join WHERE
// equality-plus-literal-filter surface already accepted by one-off. Join
// multiplicity and filtering happen first; LIMIT 1 applies only to the one-row
// aggregate output.
func TestHandleOneOffQuery_ShunterCrossJoinWhereCountWithLimitReturnsFullAggregate(t *testing.T) {
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
		MessageID:   []byte{0xCC},
		QueryString: "SELECT COUNT(*) AS n FROM t JOIN s WHERE t.id = s.t_id AND s.active = TRUE LIMIT 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want nil (cross-join WHERE + COUNT + LIMIT is one-off-only accepted)", *result.Error)
	}
	gotRows := decodeRows(t, firstTableRows(result), aggregateSchema)
	wantRows := []types.ProductValue{{types.NewUint64(2)}}
	assertProductRowsEqual(t, gotRows, wantRows)
}

// TestHandleOneOffQuery_ShunterSqlInvalidAggregateWithoutAliasRejected pins the
// reference parse_sql rejection at
// reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs lines 457-476
// (`select count(*) from t` / "Aggregate without alias") onto the OneOff
// admission surface. parseProjection reads `count` as an identifier
// qualifier, then finds `(` where it expects a dot, rejecting with
// "projection must be '*' or 'table.*'".
func TestHandleOneOffQuery_ShunterSqlInvalidAggregateWithoutAliasRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterArraySenderRejected pins reference
// check.rs:487-489 (`select * from t where arr = :sender` / "The :sender
// param is an identity") onto the OneOffQuery admission surface. With
// KindArrayString realized, the coerce layer rejects :sender against the
// array column instead of hitting the default "column kind not supported"
// branch — the rejection is a positive Shunter contract.
func TestHandleOneOffQuery_ShunterArraySenderRejected(t *testing.T) {
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

// TestHandleOneOffQuery_ShunterArrayJoinOnRejected pins reference
// check.rs:523-525 (`select t.* from t join s on t.arr = s.arr` / "Product
// values are not comparable") onto the OneOffQuery admission surface. The
// join compile path rejects when either ON side names an array column.
func TestHandleOneOffQuery_ShunterArrayJoinOnRejected(t *testing.T) {
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

func TestHandleOneOffQuery_ShunterJoinOnStrictEqualityRejectText(t *testing.T) {
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
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "quantity", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1e},
		QueryString: "SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Non-inner joins are not supported"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestHandleOneOffQuery_ShunterCrossJoinKeywordNotAlias(t *testing.T) {
	conn := testConnDirect(nil)
	sl := &mockSchemaLookup{
		tables: map[string]struct {
			id     schema.TableID
			schema *schema.TableSchema
		}{
			"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint32},
			}}},
			"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint32},
			}}},
		},
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x1f},
		QueryString: "SELECT CROSS.* FROM t CROSS JOIN s",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`CROSS` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestHandleOneOffQuery_ShunterLeftJoinKeywordRejected(t *testing.T) {
	conn := testConnDirect(nil)
	sl := &mockSchemaLookup{
		tables: map[string]struct {
			id     schema.TableID
			schema *schema.TableSchema
		}{
			"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint32},
			}}},
			"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint32},
			}}},
		},
	}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x20},
		QueryString: "SELECT LEFT.* FROM t LEFT JOIN s ON LEFT.id = s.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Non-inner joins are not supported"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnknownTableRejectText pins the reference
// type-check rejection literal at
// reference/SpacetimeDB/crates/expr/src/errors.rs:14
// (`Unresolved::Table` = "no such table: `{0}`. If the table exists, it may
// be marked private."). The OneOff admission surface
// (module_host.rs:2252 `compile_subscription`, :2316 `format!("{err}")`)
// emits the raw error text with no `DBError::WithSql` wrap.
func TestHandleOneOffQuery_ShunterUnknownTableRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x90},
		QueryString: "SELECT * FROM r",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "no such table: `r`. If the table exists, it may be marked private."
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

func TestHandleOneOffQuery_AmbiguousCaseFoldedTableNameRejected(t *testing.T) {
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
	sl := registrySchemaLookup{reg: eng.Registry()}
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x91},
		QueryString: "SELECT * FROM USERS WHERE id = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected ambiguous case-folded table name to reject, got nil error")
	}
	want := "no such table: `USERS`. If the table exists, it may be marked private."
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnknownFieldRejectText pins the reference
// type-check rejection literal at
// reference/SpacetimeDB/crates/expr/src/errors.rs:11-13
// (`Unresolved::Var` = "`{0}` is not in scope"). Reference emit site
// `_type_expr` lib.rs:107: a missing column inside an existing relvar
// surfaces as `Unresolved::var(&field)`. OneOff admission emits the raw
// error text with no `DBError::WithSql` wrap.
func TestHandleOneOffQuery_ShunterUnknownFieldRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x91},
		QueryString: "SELECT * FROM t WHERE t.missing_col = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing_col` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterBoolLiteralOnIntegerColumnRejectText pins
// UnexpectedType text for Bool literals on integer columns.
func TestHandleOneOffQuery_ShunterBoolLiteralOnIntegerColumnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x92},
		QueryString: "SELECT * FROM t WHERE u32 = TRUE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unexpected type: (expected) Bool != U32 (inferred)"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterIntOverflowOnUint8RejectText pins
// InvalidLiteral text for integer overflow on narrow unsigned columns.
func TestHandleOneOffQuery_ShunterIntOverflowOnUint8RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x93},
		QueryString: "SELECT * FROM t WHERE u8 = 1000",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `1000` cannot be parsed as type `U8`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterNegativeIntOnUint8RejectText pins the same
// reference `InvalidLiteral` literal for the negative-on-unsigned case.
// Reference `parse_int` calls `BigDecimal::to_u8` which returns None for
// negative inputs; the outer anyhow error is folded into InvalidLiteral by
// the `.map_err` at lib.rs:99. Shunter emits via the negative branch of
// `coerceUnsigned` rather than a dedicated typecheck pass; the text must
// still match the reference literal.
func TestHandleOneOffQuery_ShunterNegativeIntOnUint8RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u8", Type: schema.KindUint8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x94},
		QueryString: "SELECT * FROM t WHERE u8 = -1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `-1` cannot be parsed as type `U8`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterIntOverflowOnInt8RejectText pins the signed
// variant of the same reference `InvalidLiteral` literal. Reference
// `parse_int` → `BigDecimal::to_i8` returns None for 200 (>127); the outer
// anyhow error is folded into InvalidLiteral by `.map_err` at lib.rs:99.
// Shunter emits via the range-check branch of `coerceSigned`; the text
// must match reference.
func TestHandleOneOffQuery_ShunterIntOverflowOnInt8RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "i8", Type: schema.KindInt8},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewInt8(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x95},
		QueryString: "SELECT * FROM t WHERE i8 = 200",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `200` cannot be parsed as type `I8`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterNegativeIntOnUint128RejectText pins the
// reference `InvalidLiteral` literal for the LitInt-negative branch against
// a 128-bit unsigned column (coerce.go:133 in the Uint128 case arm). The
// reference path `parse_int` + `BigDecimal::to_u128` returns None for
// negative inputs; the outer `.map_err` folds the anyhow into
// InvalidLiteral. Scope: LitInt branch — the LitBigInt branch is covered
// by the BigInt cases below.
func TestHandleOneOffQuery_ShunterNegativeIntOnUint128RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u128", Type: schema.KindUint128},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint128(0, 1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x96},
		QueryString: "SELECT * FROM t WHERE u128 = -1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `-1` cannot be parsed as type `U128`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterBigIntOverflowOnUint128RejectText pins the
// reference `InvalidLiteral` literal for the LitBigInt branch against a
// 128-bit unsigned column (coerceBigIntToUint128 in coerce.go). Input
// `2^128` exceeds U128's max and `BigDecimal::to_u128` returns None in
// the reference `parse_int` path.
func TestHandleOneOffQuery_ShunterBigIntOverflowOnUint128RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u128", Type: schema.KindUint128},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint128(0, 1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	// 2^128 = 340282366920938463463374607431768211456 — one past u128::MAX.
	const bigOverflow = "340282366920938463463374607431768211456"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x97},
		QueryString: "SELECT * FROM t WHERE u128 = " + bigOverflow,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `" + bigOverflow + "` cannot be parsed as type `U128`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterBigIntOverflowOnInt128RejectText pins the
// reference `InvalidLiteral` literal for the signed variant.
// `2^127` overflows I128::MAX (2^127 - 1) and reference
// `BigDecimal::to_i128` returns None.
func TestHandleOneOffQuery_ShunterBigIntOverflowOnInt128RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "i128", Type: schema.KindInt128},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewInt128(0, 1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	// 2^127 = 170141183460469231731687303715884105728 — one past i128::MAX.
	const bigOverflow = "170141183460469231731687303715884105728"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x98},
		QueryString: "SELECT * FROM t WHERE i128 = " + bigOverflow,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `" + bigOverflow + "` cannot be parsed as type `I128`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterNegativeIntOnUint256RejectText pins the
// reference `InvalidLiteral` literal for the LitInt-negative branch
// against a 256-bit unsigned column (coerce.go:154 in the Uint256 arm).
func TestHandleOneOffQuery_ShunterNegativeIntOnUint256RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u256", Type: schema.KindUint256},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint256(0, 0, 0, 1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x99},
		QueryString: "SELECT * FROM t WHERE u256 = -1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `-1` cannot be parsed as type `U256`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterFloatLiteralOnUint32RejectText pins
// InvalidLiteral text for float literals on integer columns.
func TestHandleOneOffQuery_ShunterFloatLiteralOnUint32RejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0x9a},
		QueryString: "SELECT * FROM t WHERE u32 = 1.3",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "The literal expression `1.3` cannot be parsed as type `U32`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterNonBoolLiteralOnBoolRejectText pins
// InvalidLiteral text for non-bool literals on Bool columns.
func TestHandleOneOffQuery_ShunterNonBoolLiteralOnBoolRejectText(t *testing.T) {
	cases := []struct {
		name        string
		queryString string
		wantLit     string
	}{
		{"LitInt", "SELECT * FROM t WHERE b = 1", "1"},
		{"LitFloat", "SELECT * FROM t WHERE b = 1.3", "1.3"},
		{"LitString", "SELECT * FROM t WHERE b = 'foo'", "foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "b", Type: schema.KindBool},
			)
			snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewBool(true)}}}}
			stateAccess := &mockStateAccess{snap: snap}

			msg := &OneOffQueryMsg{
				MessageID:   []byte{0x9b},
				QueryString: tc.queryString,
			}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

			result := drainOneOff(t, conn)
			if result.Error == nil {
				t.Fatal("expected error, got nil (success)")
			}
			want := "The literal expression `" + tc.wantLit + "` cannot be parsed as type `Bool`"
			if *result.Error != want {
				t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
			}
		})
	}
}

// TestHandleOneOffQuery_ShunterDuplicateProjectionAliasRejectText pins the
// reference `DuplicateName` literal (errors.rs:120) for a SELECT list whose
// explicit `AS` aliases collide. Reference path: `type_proj::Exprs`
// (check.rs:67-72) tracks each element's alias in a HashSet and emits
// `DuplicateName(alias)` on the second occurrence. OneOff is the only
// surface that reaches this branch — SubscribeSingle rejects the
// column-list projection earlier with `Unsupported::ReturnType` at
// `compileSQLQueryString`'s `allowProjection=false` guard.
func TestHandleOneOffQuery_ShunterDuplicateProjectionAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "i32", Type: schema.KindInt32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1), types.NewInt32(-1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE0},
		QueryString: "SELECT u32 AS dup, i32 AS dup FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Duplicate name `dup`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

func TestHandleOneOffQuery_DuplicateProjectionAliasWithOrderByRejects(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "i32", Type: schema.KindInt32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1), types.NewInt32(-1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE2},
		QueryString: "SELECT u32 AS dup, i32 AS dup FROM t ORDER BY dup",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Duplicate name `dup`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestHandleOneOffQuery_MultiColumnOrderByAmbiguousProjectionNameRejects(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "i32", Type: schema.KindInt32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1), types.NewInt32(-1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE3},
		QueryString: "SELECT u32 AS i32 FROM t ORDER BY u32, i32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "ORDER BY name \"i32\" is ambiguous"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterDuplicateImplicitProjectionRejectText pins
// the same `DuplicateName` literal for a SELECT list with no explicit
// aliases — the effective output name falls back to the column name, so
// `SELECT u32, u32 FROM t` collides on `u32`. Reference reads each
// element's effective name from `ProjectElem(_, SqlIdent(alias))` where
// `alias` is the column name when no `AS` was written.
func TestHandleOneOffQuery_ShunterDuplicateImplicitProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE1},
		QueryString: "SELECT u32, u32 FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Duplicate name `u32`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterDuplicateJoinAliasRejectText pins the
// reference `DuplicateName` literal for a join whose right-side alias
// collides with the left side. Reference path: `type_from`
// (lib.rs:88-89) inserts each alias into a HashSet keyed by `Relvars`
// and emits `DuplicateName(alias.clone())` on collision. Shunter routes
// the same shape through the parser so OneOff (raw) and SubscribeSingle
// (WithSql-wrapped) both carry the reference text.
func TestHandleOneOffQuery_ShunterDuplicateJoinAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE2},
		QueryString: "SELECT dup.* FROM t AS dup JOIN s AS dup ON dup.u32 = dup.u32",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Duplicate name `dup`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterDuplicateSelfJoinRejectText pins the same
// `DuplicateName` literal for an unaliased self-join — `FROM t JOIN t`
// derives both sides' alias from the base table name, so the collision
// surfaces on `t`. Reference treats the unaliased and explicitly-aliased
// shapes identically because `parse_relvar` synthesizes the alias from the
// base table when no `AS` is written.
func TestHandleOneOffQuery_ShunterDuplicateSelfJoinRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE3},
		QueryString: "SELECT t.* FROM t JOIN t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Duplicate name `t`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterJoinColumnKindMismatchRejectText pins
// UnexpectedType text for JOIN ON type mismatches.
func TestHandleOneOffQuery_ShunterJoinColumnKindMismatchRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE4},
		QueryString: "SELECT t.* FROM t JOIN s ON t.u32 = s.name",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unexpected type: (expected) String != U32 (inferred)"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterJoinArrayColumnInvalidOpRejectText pins
// InvalidOp text for array equality in JOIN ON.
func TestHandleOneOffQuery_ShunterJoinArrayColumnInvalidOpRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	tTS := &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{{Index: 0, Name: "arr", Type: schema.KindArrayString}}}
	sTS := &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{{Index: 0, Name: "arr", Type: schema.KindArrayString}}}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: tTS},
		"s": {id: 2, schema: sTS},
	}}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE5},
		QueryString: "SELECT t.* FROM t JOIN s ON t.arr = s.arr",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Invalid binary operator `=` for type `Array<String>`"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarUnqualifiedWhereRejectText pins
// the reference `Unresolved::Var` literal (errors.rs:11-13) for an
// unqualified single-table WHERE column that does not exist on the
// resolved relvar. Reference path: `_type_expr` (lib.rs:107) maps the
// missing-field branch through `Unresolved::var(&field)`. The text
// carries only the field name — the table name does not appear.
func TestHandleOneOffQuery_ShunterUnresolvedVarUnqualifiedWhereRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE6},
		QueryString: "SELECT * FROM t WHERE missing = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarProjectionColumnRejectText pins
// the reference `Unresolved::Var` literal for an unknown projection
// column. Reference path: `type_proj::Exprs` (check.rs:74) routes each
// projection element through `type_expr`, whose missing-field branch at
// lib.rs:107 emits `Unresolved::var(&field)`. OneOff-only: SubscribeSingle
// rejects column-list projections earlier with `Unsupported::ReturnType`.
func TestHandleOneOffQuery_ShunterUnresolvedVarProjectionColumnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE7},
		QueryString: "SELECT missing FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarJoinOnMissingRejectText pins
// the reference `Unresolved::Var` literal for an unknown JOIN ON
// equality operand. Reference `type_from` types the ON binop through
// `type_expr` (lib.rs:101-102), whose field-lookup branch at lib.rs:107
// emits `Unresolved::var(&field)` when the qualified column does not
// exist on its declared relvar.
func TestHandleOneOffQuery_ShunterUnresolvedVarJoinOnMissingRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE8},
		QueryString: "SELECT t.* FROM t JOIN s ON t.missing = s.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarJoinWhereQualifiedMissingRejectText
// pins the reference `Unresolved::Var` literal for a qualified WHERE
// column on the right side of a join whose field does not exist.
// Reference `type_select` routes the WHERE expression through Bool
// `type_expr`, whose field-lookup branch at lib.rs:107 emits
// `Unresolved::var(&field)`.
func TestHandleOneOffQuery_ShunterUnresolvedVarJoinWhereQualifiedMissingRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE9},
		QueryString: "SELECT t.* FROM t JOIN s ON t.id = s.t_id WHERE s.missing = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarBaseTableAfterAliasRejectText pins
// the reference `Unresolved::Var` literal for a WHERE column qualified
// by the base table name AFTER an `AS` alias has been declared on the
// FROM relvar. Reference `_type_expr` (lib.rs:103) emits
// `Unresolved::var(&table)` when `vars.deref().get(&*table)` returns
// None — the base name `t` is no longer in scope once `AS r` rebinds
// the relvar to `r`. The text carries the qualifier name (the table /
// alias identifier), not the column.
func TestHandleOneOffQuery_ShunterUnresolvedVarBaseTableAfterAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xEA},
		QueryString: "SELECT * FROM t AS r WHERE t.u32 = 5",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`t` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (OneOff admission has no DBError::WithSql wrap)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarBareJoinWildcardOnMissingRejectText
// pins reference `type_from` ordering: the JOIN ON expression types
// before `type_proj` runs the bare-wildcard rejection. Reference path:
// `SubChecker::type_from` (check.rs:99-104) types the ON binop through
// `type_expr` (lib.rs:101-102) before the join `RelExpr` is handed to
// `type_proj`. So `SELECT * FROM t JOIN s ON t.missing = s.id` emits
// `Unresolved::Var{missing}` before the `InvalidWildcard::Join` text.
func TestHandleOneOffQuery_ShunterUnresolvedVarBareJoinWildcardOnMissingRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xEC},
		QueryString: "SELECT * FROM t JOIN s ON t.missing = s.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (JOIN ON resolution must precede bare-wildcard rejection)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarJoinOnMissingNotHiddenByWhereFalseRejectText
// pins the reference order in which `type_from` types the ON expression
// before the WHERE predicate is folded. Even when WHERE is `FALSE`,
// reference still types ON first (check.rs:99-104). Shunter's
// FalsePredicate short-circuit must therefore fire AFTER ON-column
// resolution, so a missing ON column raises `Unresolved::Var` before
// the WHERE-FALSE→NoRows rewrite.
func TestHandleOneOffQuery_ShunterUnresolvedVarJoinOnMissingNotHiddenByWhereFalseRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xED},
		QueryString: "SELECT t.* FROM t JOIN s ON t.missing = s.id WHERE FALSE",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success — FALSE pruning should not skip ON resolution)")
	}
	want := "`missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (FALSE-WHERE pruning must not bypass ON-column resolution)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarWherePrecedesProjectionRejectText
// pins the reference type-checker order: `type_select` (WHERE) runs
// before `type_proj` (projection columns). Reference path:
// `SubChecker::type_set` (check.rs:139-146) computes
// `type_proj(type_select(input, expr, vars)?, project, vars)`, so the
// WHERE expression types first and a missing WHERE column raises
// `Unresolved::Var` before the projection list is walked.
func TestHandleOneOffQuery_ShunterUnresolvedVarWherePrecedesProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xEB},
		QueryString: "SELECT missing FROM t WHERE other_missing = 1",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`other_missing` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (WHERE column-resolution must precede projection-column resolution)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterBooleanConstantWhereDoesNotMaskBranchErrors
// pins reference `_type_expr` order for logical WHERE expressions:
// both operands are typed before Bool operators are lowered. Constant
// folding must therefore not hide an unresolved field or invalid literal
// in the other branch.
func TestHandleOneOffQuery_ShunterBooleanConstantWhereDoesNotMaskBranchErrors(t *testing.T) {
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
			sl := newMockSchema("t", 1,
				schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
			)
			snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
			stateAccess := &mockStateAccess{snap: snap}

			msg := &OneOffQueryMsg{
				MessageID:   []byte{0xF0 + byte(i)},
				QueryString: tc.sql,
			}
			handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

			result := drainOneOff(t, conn)
			if result.Error == nil {
				t.Fatal("expected error, got nil (success)")
			}
			if *result.Error != tc.want {
				t.Fatalf("Error = %q, want %q", *result.Error, tc.want)
			}
		})
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarQualifiedProjectionQualifierRejectText
// pins reference `type_proj::Exprs` (`expr/src/lib.rs:65-78`): a
// qualified column whose qualifier is not a declared relvar routes
// through `type_expr` and emits `Unresolved::var(&table)`
// (`expr/src/lib.rs:103`). Shunter's parser previously rejected at
// projection-qualifier resolution with `parse: unsupported SQL:
// projection qualifier "x" does not match relation`; reroute now emits
// the reference `Unresolved::Var` text.
func TestHandleOneOffQuery_ShunterUnresolvedVarQualifiedProjectionQualifierRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xEF},
		QueryString: "SELECT x.u32 FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`x` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (qualified column qualifier mismatch must emit Unresolved::Var)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnresolvedVarQualifiedWildcardQualifierRejectText
// pins reference `type_proj` for `Project::Star(Some(var))`:
// `input.has_field(&var)` miss emits `Unresolved::var(&var)`. Shunter's
// parser previously rejected with `parse: unsupported SQL: projection
// qualifier "x" does not match table "t"`; reroute now emits the
// reference `Unresolved::Var` text.
func TestHandleOneOffQuery_ShunterUnresolvedVarQualifiedWildcardQualifierRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF0},
		QueryString: "SELECT x.* FROM t",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "`x` is not in scope"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (qualified wildcard qualifier mismatch must emit Unresolved::Var)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterMissingLeftTablePrecedesDuplicateJoinAliasRejectText
// pins reference `type_from` (`expr/src/check.rs:79-89`) ordering: the
// left relvar is resolved via `type_relvar` BEFORE the join loop's
// duplicate-alias HashSet check fires. So
// `SELECT dup.* FROM missing AS dup JOIN s AS dup ON dup.id = dup.id`
// emits the missing-table text for the left side, not `Duplicate name`.
func TestHandleOneOffQuery_ShunterMissingLeftTablePrecedesDuplicateJoinAliasRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF1},
		QueryString: "SELECT dup.* FROM missing AS dup JOIN s AS dup ON dup.id = dup.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "no such table: `missing`. If the table exists, it may be marked private."
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (left-table schema lookup must precede duplicate-alias rejection)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnqualifiedNamesProjectionRejectText pins
// the reference `SqlUnsupported::UnqualifiedNames` literal
// (`Names must be qualified when using joins`) for an unqualified
// projection column inside a JOIN scope. Reference
// `SqlSelect::find_unqualified_vars` (sql-parser/src/ast/sql.rs:84-95)
// flags `Project::has_unqualified_vars()` and routes through
// `parser/errors.rs:78-79`.
func TestHandleOneOffQuery_ShunterUnqualifiedNamesProjectionRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF2},
		QueryString: "SELECT id FROM t JOIN s ON t.id = s.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Names must be qualified when using joins"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (unqualified projection column in join must emit UnqualifiedNames)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnqualifiedNamesWhereRejectText pins the
// reference `SqlUnsupported::UnqualifiedNames` literal for an
// unqualified WHERE column inside a JOIN scope. Reference
// `SqlSelect::find_unqualified_vars` flags
// `expr.has_unqualified_vars()` (`SqlExpr::Var(_)` case in
// `ast/mod.rs:140-145`).
func TestHandleOneOffQuery_ShunterUnqualifiedNamesWhereRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF3},
		QueryString: "SELECT t.* FROM t JOIN s ON t.id = s.id WHERE id = 7",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Names must be qualified when using joins"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (unqualified WHERE column in join must emit UnqualifiedNames)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterUnqualifiedNamesJoinOnRejectText pins the
// reference `SqlUnsupported::UnqualifiedNames` literal for an
// unqualified JOIN ON operand. Reference `parse_join`
// (sql-parser/src/parser/mod.rs:50-77) accepts
// `Identifier = CompoundIdentifier` at parse time, then
// `find_unqualified_vars` flags the bare `Identifier` and routes
// through `UnqualifiedNames`.
func TestHandleOneOffQuery_ShunterUnqualifiedNamesJoinOnRejectText(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF4},
		QueryString: "SELECT t.* FROM t JOIN s ON id = s.id",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Names must be qualified when using joins"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (unqualified JOIN ON operand must emit UnqualifiedNames)", *result.Error, want)
	}
}

// TestHandleOneOffQuery_ShunterSenderParameterCaseSensitiveRejectText pins
// reference `parse_expr` (sql-parser/src/parser/mod.rs:223) byte-equal
// `":sender"` admission. Any other casing (e.g. `:SENDER`) falls through
// to `_ => SqlUnsupported::Expr(expr)` (line 270), rendered as
// `Unsupported expression: {expr}` (parser/errors.rs:38-39).
func TestHandleOneOffQuery_ShunterSenderParameterCaseSensitiveRejectText(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("s", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: {{types.NewUint32(1)}}}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF5},
		QueryString: "SELECT * FROM s WHERE id = :SENDER",
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatal("expected error, got nil (success)")
	}
	want := "Unsupported expression: :SENDER"
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q (case-mismatched :sender placeholder must emit SqlUnsupported::Expr)", *result.Error, want)
	}
}
