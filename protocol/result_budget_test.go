package protocol

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"math"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestEncodedResultBudgetMatchesColumnAwareRowListSize(t *testing.T) {
	complexColumns := []schema.ColumnSchema{
		{Index: 0, Name: "optional", Type: schema.KindUint64, Nullable: true},
		{Index: 1, Name: "name", Type: schema.KindString},
		{Index: 2, Name: "payload", Type: schema.KindBytes},
		{Index: 3, Name: "labels", Type: schema.KindArrayString},
		{Index: 4, Name: "wide", Type: schema.KindUint256},
	}
	tests := []struct {
		name    string
		columns []schema.ColumnSchema
		rows    []types.ProductValue
	}{
		{name: "empty"},
		{
			name: "one row",
			columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint32},
				{Index: 1, Name: "name", Type: schema.KindString},
			},
			rows: []types.ProductValue{{types.NewUint32(7), types.NewString("seven")}},
		},
		{
			name: "multiple rows",
			columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint64},
			},
			rows: []types.ProductValue{{types.NewUint64(1)}, {types.NewUint64(^uint64(0))}},
		},
		{
			name:    "nullable variable and wide values",
			columns: complexColumns,
			rows: []types.ProductValue{{
				types.NewNull(types.KindUint64),
				types.NewString("shunter"),
				types.NewBytes([]byte{0, 1, 2, 3}),
				types.NewArrayString([]string{"north", "", "south"}),
				types.NewUint256(1, 2, 3, 4),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeProductRowsForColumns(tt.rows, tt.columns)
			if err != nil {
				t.Fatalf("EncodeProductRowsForColumns: %v", err)
			}
			budget, err := newEncodedResultBudget(tt.columns, len(encoded))
			if err != nil {
				t.Fatalf("newEncodedResultBudget(exact): %v", err)
			}
			for _, row := range tt.rows {
				if _, err := budget.add(row); err != nil {
					t.Fatalf("add(exact): %v", err)
				}
			}
			if budget.bytes != len(encoded) {
				t.Fatalf("accounted bytes = %d, encoded bytes = %d", budget.bytes, len(encoded))
			}
			if _, err := encodeProductRowsForColumnsWithLimit(tt.rows, tt.columns, len(encoded)); err != nil {
				t.Fatalf("limited encoder at exact size: %v", err)
			}

			tooSmall := len(encoded) - 1
			budget, err = newEncodedResultBudget(tt.columns, tooSmall)
			if len(tt.rows) == 0 {
				if !errors.Is(err, ErrSQLQueryResultLimit) {
					t.Fatalf("empty result below four bytes: error = %v, want ErrSQLQueryResultLimit", err)
				}
			} else {
				if err != nil {
					t.Fatalf("newEncodedResultBudget(one byte under): %v", err)
				}
				for i, row := range tt.rows {
					_, err = budget.add(row)
					if err != nil && i != len(tt.rows)-1 {
						t.Fatalf("add failed before final row: %v", err)
					}
				}
				if !errors.Is(err, ErrSQLQueryResultLimit) {
					t.Fatalf("one byte under: error = %v, want ErrSQLQueryResultLimit", err)
				}
			}
			if _, err := encodeProductRowsForColumnsWithLimit(tt.rows, tt.columns, tooSmall); !errors.Is(err, ErrSQLQueryResultLimit) {
				t.Fatalf("limited encoder one byte under: error = %v, want ErrSQLQueryResultLimit", err)
			}
		})
	}
}

func TestOrderedOneOffCollectorUpdatesRetainedByteBudget(t *testing.T) {
	columns := []schema.ColumnSchema{{Index: 0, Name: "payload", Type: schema.KindString}}
	large := types.ProductValue{types.NewString("large retained candidate")}
	small := types.ProductValue{types.NewString("x")}
	largeEncoded, err := EncodeProductRowsForColumns([]types.ProductValue{large}, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns: %v", err)
	}
	budget, err := newEncodedResultBudget(columns, len(largeEncoded))
	if err != nil {
		t.Fatalf("newEncodedResultBudget: %v", err)
	}
	collector := newOrderedOneOffCollector([]compiledSQLOrderBy{{}}, 1, budget)
	if err := collector.Add(large, []types.Value{types.NewUint32(10)}); err != nil {
		t.Fatalf("add large candidate: %v", err)
	}
	if err := collector.Add(small, []types.Value{types.NewUint32(1)}); err != nil {
		t.Fatalf("replace with smaller candidate: %v", err)
	}
	smallEncoded, err := EncodeProductRowsForColumns([]types.ProductValue{small}, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns(small): %v", err)
	}
	if budget.bytes != len(smallEncoded) {
		t.Fatalf("bytes after replacement = %d, want %d", budget.bytes, len(smallEncoded))
	}
	before := budget.bytes
	if err := collector.Add(types.ProductValue{types.NewString("discarded candidate much larger than the cap")}, []types.Value{types.NewUint32(20)}); err != nil {
		t.Fatalf("discarded candidate consumed budget: %v", err)
	}
	if budget.bytes != before {
		t.Fatalf("bytes after discarded candidate = %d, want %d", budget.bytes, before)
	}
}

type yieldingCountSnapshot struct {
	*mockSnapshot
	yielded int
}

func (s *yieldingCountSnapshot) TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue] {
	base := s.mockSnapshot.TableScan(id)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, row := range base {
			s.yielded++
			if !yield(rid, row) {
				return
			}
		}
	}
}

func TestUnorderedOneOffStopsAtFirstOverBudgetRow(t *testing.T) {
	columns := []schema.ColumnSchema{{Index: 0, Name: "payload", Type: schema.KindString}}
	rows := []types.ProductValue{
		{types.NewString("first")},
		{types.NewString("second exceeds the retained result budget")},
		{types.NewString("must not be visited")},
	}
	firstEncoded, err := EncodeProductRowsForColumns(rows[:1], columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns: %v", err)
	}
	snapshot := &yieldingCountSnapshot{mockSnapshot: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: rows}}}
	sl := newMockSchema("items", 1, columns...)
	compiled, err := CompileSQLQueryString("SELECT * FROM items", sl, nil, SQLQueryValidationOptions{})
	if err != nil {
		t.Fatalf("CompileSQLQueryString: %v", err)
	}
	_, err = ExecuteCompiledSQLQueryWithLimits(context.Background(), compiled, &mockStateAccess{snap: snapshot}, sl, SQLQueryLimits{
		MaxRows:  10,
		MaxBytes: len(firstEncoded),
	})
	if !errors.Is(err, ErrSQLQueryResultLimit) {
		t.Fatalf("ExecuteCompiledSQLQueryWithLimits error = %v, want ErrSQLQueryResultLimit", err)
	}
	if snapshot.yielded != 2 {
		t.Fatalf("yielded rows = %d, want 2", snapshot.yielded)
	}
}

func TestRawOneOffReportsHostedResultByteLimit(t *testing.T) {
	columns := []schema.ColumnSchema{{Index: 0, Name: "payload", Type: schema.KindString}}
	sl := newMockSchema("items", 1, columns...)
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {{types.NewString("oversized")}},
	}}}
	conn := testConnDirect(nil)
	handleOneOffQueryWithVisibilityAndLimits(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0x62},
		QueryString: "SELECT * FROM items",
	}, state, sl, nil, SQLQueryLimits{MaxRows: 10, MaxBytes: 4})
	result := drainOneOff(t, conn)
	if result.Error == nil || !strings.Contains(*result.Error, ErrSQLQueryResultLimit.Error()) {
		t.Fatalf("raw query error = %v, want %q classification", result.Error, ErrSQLQueryResultLimit)
	}
}

func TestRawOneOffReservesCompleteResponseEnvelope(t *testing.T) {
	columns := []schema.ColumnSchema{{Index: 0, Name: "payload", Type: schema.KindString}}
	rows := []types.ProductValue{{types.NewString(strings.Repeat("x", 1024))}}
	encoded, err := EncodeProductRowsForColumns(rows, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns: %v", err)
	}
	messageID := []byte("response-budget")
	success := OneOffQueryResponse{
		MessageID: messageID,
		Tables: []OneOffTable{{
			TableName: "items",
			Rows:      encoded,
		}},
		TotalHostExecutionDuration: 1,
	}
	responseSize, err := ValidateServerMessageSize(success, 0)
	if err != nil {
		t.Fatalf("ValidateServerMessageSize: %v", err)
	}
	opts := DefaultProtocolOptions()
	opts.MaxOutboundMessageSize = responseSize - 1
	conn := testConnDirect(&opts)
	observer := &protocolMetricObserver{}
	conn.Observer = observer
	sl := newMockSchema("items", 1, columns...)
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: rows}}}

	handleOneOffQueryWithVisibilityAndLimits(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   messageID,
		QueryString: "SELECT * FROM items",
	}, state, sl, nil, SQLQueryLimits{MaxRows: 10, MaxBytes: len(encoded)})

	result := drainOneOff(t, conn)
	if !bytes.Equal(result.MessageID, messageID) {
		t.Fatalf("response message ID = %x, want %x", result.MessageID, messageID)
	}
	if result.Error == nil || !strings.Contains(*result.Error, ErrSQLQueryResultLimit.Error()) {
		t.Fatalf("response error = %v, want envelope-aware result limit", result.Error)
	}
	if len(result.Tables) != 0 {
		t.Fatalf("response tables = %+v, want correlated error without payload", result.Tables)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	for _, event := range observer.messageEvents {
		if event.kind == "one_off_query" && event.result == "ok" {
			t.Fatalf("oversized response recorded false success metric: %+v", observer.messageEvents)
		}
	}
}

func TestJoinShapesEnforceOneOffByteBudget(t *testing.T) {
	aSchema := &schema.TableSchema{ID: 1, Name: "a", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint32},
		{Index: 1, Name: "join_key", Type: schema.KindUint32},
	}}
	bSchema := &schema.TableSchema{ID: 2, Name: "b", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint32},
		{Index: 1, Name: "join_key", Type: schema.KindUint32},
		{Index: 2, Name: "payload", Type: schema.KindString},
	}}
	cSchema := &schema.TableSchema{ID: 3, Name: "c", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint32},
		{Index: 1, Name: "join_key", Type: schema.KindUint32},
		{Index: 2, Name: "payload", Type: schema.KindString},
	}}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"a": {id: aSchema.ID, schema: aSchema},
		"b": {id: bSchema.ID, schema: bSchema},
		"c": {id: cSchema.ID, schema: cSchema},
	}}
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {{types.NewUint32(1), types.NewUint32(7)}},
		2: {{types.NewUint32(2), types.NewUint32(7), types.NewString("two-way")}},
		3: {{types.NewUint32(3), types.NewUint32(7), types.NewString("multi-way")}},
	}}}
	opts := SQLQueryValidationOptions{AllowProjection: true}
	queries := []string{
		"SELECT b.payload FROM a JOIN b ON a.join_key = b.join_key",
		"SELECT b.payload FROM a JOIN b",
		"SELECT c.payload FROM a JOIN b ON a.join_key = b.join_key JOIN c ON b.join_key = c.join_key",
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			compiled, err := CompileSQLQueryString(query, sl, nil, opts)
			if err != nil {
				t.Fatalf("CompileSQLQueryString: %v", err)
			}
			_, err = ExecuteCompiledSQLQueryWithLimits(context.Background(), compiled, state, sl, SQLQueryLimits{MaxRows: 10, MaxBytes: 4})
			if !errors.Is(err, ErrSQLQueryResultLimit) {
				t.Fatalf("ExecuteCompiledSQLQueryWithLimits error = %v, want ErrSQLQueryResultLimit", err)
			}
		})
	}
}

func TestOffsetBeyondRowCapAcrossOneOffQueryShapes(t *testing.T) {
	aSchema := &schema.TableSchema{ID: 1, Name: "a", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint32},
		{Index: 1, Name: "join_key", Type: schema.KindUint32},
	}}
	bSchema := &schema.TableSchema{ID: 2, Name: "b", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint32},
		{Index: 1, Name: "join_key", Type: schema.KindUint32},
	}}
	cSchema := &schema.TableSchema{ID: 3, Name: "c", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint32},
		{Index: 1, Name: "join_key", Type: schema.KindUint32},
	}}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"a": {id: aSchema.ID, schema: aSchema},
		"b": {id: bSchema.ID, schema: bSchema},
		"c": {id: cSchema.ID, schema: cSchema},
	}}
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(3), types.NewUint32(7)},
			{types.NewUint32(1), types.NewUint32(7)},
			{types.NewUint32(2), types.NewUint32(7)},
		},
		2: {{types.NewUint32(20), types.NewUint32(7)}},
		3: {{types.NewUint32(30), types.NewUint32(7)}},
	}}}
	opts := SQLQueryValidationOptions{AllowLimit: true, AllowProjection: true, AllowOrderBy: true, AllowOffset: true}
	queries := []string{
		"SELECT * FROM a LIMIT 1 OFFSET 2",
		"SELECT * FROM a ORDER BY id LIMIT 1 OFFSET 2",
		"SELECT b.* FROM a JOIN b ON a.join_key = b.join_key LIMIT 1 OFFSET 2",
		"SELECT b.* FROM a JOIN b LIMIT 1 OFFSET 2",
		"SELECT c.* FROM a JOIN b ON a.join_key = b.join_key JOIN c ON b.join_key = c.join_key LIMIT 1 OFFSET 2",
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			compiled, err := CompileSQLQueryString(query, sl, nil, opts)
			if err != nil {
				t.Fatalf("CompileSQLQueryString: %v", err)
			}
			result, err := ExecuteCompiledSQLQueryWithLimits(context.Background(), compiled, state, sl, SQLQueryLimits{MaxRows: 1, MaxBytes: 1 << 20})
			if err != nil {
				t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
			}
			if len(result.Rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(result.Rows))
			}
		})
	}
}

func TestOffsetBeyondRowCapUsesOrderedIndexPath(t *testing.T) {
	columns := []schema.ColumnSchema{{Index: 0, Name: "id", Type: schema.KindUint32}}
	baseLookup := newMockSchema("items", 1, columns...)
	sl := &indexedOneOffSchemaLookup{mockSchemaLookup: baseLookup, table: 1, column: 0, index: 9}
	snapshot := &indexedCountingSnapshot{
		mockSnapshot: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
			1: {{types.NewUint32(3)}, {types.NewUint32(1)}, {types.NewUint32(2)}},
		}},
		table: 1, column: 0, index: 9,
	}
	compiled, err := CompileSQLQueryString("SELECT * FROM items ORDER BY id LIMIT 1 OFFSET 2", sl, nil, SQLQueryValidationOptions{
		AllowLimit: true, AllowOrderBy: true, AllowOffset: true,
	})
	if err != nil {
		t.Fatalf("CompileSQLQueryString: %v", err)
	}
	result, err := ExecuteCompiledSQLQueryWithLimits(context.Background(), compiled, &mockStateAccess{snap: snapshot}, sl, SQLQueryLimits{MaxRows: 1, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
	}
	assertProductRowsEqual(t, result.Rows, []types.ProductValue{{types.NewUint32(3)}})
	if snapshot.indexRanges != 1 || snapshot.tableScans != 0 {
		t.Fatalf("index ranges=%d table scans=%d, want 1/0", snapshot.indexRanges, snapshot.tableScans)
	}
}

func TestOneOffScanLimitSaturates(t *testing.T) {
	if got := oneOffScanLimit(math.MaxInt, 1); got != math.MaxInt {
		t.Fatalf("oneOffScanLimit(MaxInt, 1) = %d, want MaxInt", got)
	}
	maxOffset := uint64(math.MaxUint64)
	if got := oneOffRowOffset(&maxOffset); got != math.MaxInt {
		t.Fatalf("oneOffRowOffset(MaxUint64) = %d, want MaxInt", got)
	}
}
