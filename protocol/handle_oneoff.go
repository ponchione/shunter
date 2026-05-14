package protocol

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ponchione/shunter/internal/valueagg"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// CommittedStateAccess provides a point-in-time snapshot of committed
// state for read-only queries.
type CommittedStateAccess interface {
	Snapshot() store.CommittedReadView
}

// SQLQueryResult is the unencoded row result for a compiled one-off SQL query.
type SQLQueryResult struct {
	TableName string
	Columns   []schema.ColumnSchema
	Rows      []types.ProductValue
}

// handleOneOffQuery parses and executes one SQL read against committed state.
func handleOneOffQuery(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
) {
	handleOneOffQueryWithVisibility(ctx, conn, msg, stateAccess, sl, nil)
}

func handleOneOffQueryWithVisibility(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	receipt := time.Now()
	readSL := authorizedSchemaLookupForConn(sl, conn)
	caller := readCallerContext(conn)
	compiled, err := CompileSQLQueryStringWithVisibility(msg.QueryString, readSL, &caller.Identity, SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
		AllowOrderBy:    true,
		AllowOffset:     true,
	}, visibilityFilters, caller.AllowAllPermissions)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		recordProtocolMessage(conn.Observer, "one_off_query", "validation_error")
		return
	}

	result, err := ExecuteCompiledSQLQuery(ctx, compiled, stateAccess, readSL)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		recordProtocolMessage(conn.Observer, "one_off_query", "internal_error")
		return
	}

	encoded, err := EncodeProductRowsForColumns(result.Rows, result.Columns)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, "encode error: "+err.Error(), receipt)
		recordProtocolMessage(conn.Observer, "one_off_query", "internal_error")
		return
	}
	sendError(conn, OneOffQueryResponse{
		MessageID: msg.MessageID,
		Tables: []OneOffTable{{
			TableName: result.TableName,
			Rows:      encoded,
		}},
		TotalHostExecutionDuration: elapsedMicrosI64(receipt),
	})
	recordProtocolMessage(conn.Observer, "one_off_query", "ok")
}

// ExecuteCompiledSQLQuery evaluates a precompiled one-off SQL query against a
// committed snapshot and returns detached row values.
func ExecuteCompiledSQLQuery(ctx context.Context, compiled CompiledSQLQuery, stateAccess CommittedStateAccess, sl SchemaLookup) (SQLQueryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if stateAccess == nil {
		return SQLQueryResult{}, fmt.Errorf("committed state access must not be nil")
	}
	if sl == nil {
		return SQLQueryResult{}, fmt.Errorf("schema lookup must not be nil")
	}
	query := compiled.query
	tableID, tableSchema, ok := sl.TableByName(query.TableName)
	if !ok {
		//lint:ignore ST1005 protocol tests pin this sentence-form error text.
		return SQLQueryResult{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", query.TableName)
	}

	resolver, _ := sl.(schema.IndexResolver)
	pred := query.Predicate
	if query.MultiJoin != nil {
		return executeCompiledSQLMultiJoin(ctx, query, stateAccess, resolver)
	}
	if err := subscription.ValidateQueryPredicate(pred, sl); err != nil {
		return SQLQueryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SQLQueryResult{}, err
	}

	view := stateAccess.Snapshot()
	defer view.Close()

	rowLimit := oneOffRowLimit(query.Limit)
	rowOffset := oneOffRowOffset(query.Offset)
	scanLimit := rowLimit
	if len(query.OrderBy) != 0 {
		scanLimit = -1
	} else {
		scanLimit = oneOffScanLimit(rowOffset, rowLimit)
	}
	var matchedRows []types.ProductValue
	var encodedRows []types.ProductValue
	rowsAlreadyProjected := false
	rowsAlreadyOrderedAndLimited := false
	if query.Aggregate != nil {
		// Aggregate shape happens over the full matched input; OFFSET/LIMIT then
		// slice the one-row aggregate output (reference ProjectList::Limit wraps
		// ProjectList::Agg). LIMIT 0 or OFFSET >= 1 drops the aggregate row.
		aggregateValue, err := evaluateOneOffAggregate(ctx, view, tableID, pred, resolver, query.Aggregate)
		if err != nil {
			return SQLQueryResult{}, err
		}
		encodedRows = sliceOneOffRows([]types.ProductValue{{aggregateValue}}, rowOffset, rowLimit)
	} else if rowLimit != 0 {
		if joinPred, ok := pred.(subscription.Join); ok {
			if len(query.ProjectionColumns) != 0 {
				var rows []types.ProductValue
				var err error
				if len(query.OrderBy) != 0 {
					rows, err = evaluateOneOffJoinProjectionOrdered(ctx, view, joinPred, query.ProjectionColumns, query.OrderBy, resolver, rowOffset, rowLimit)
				} else {
					rows, err = evaluateOneOffJoinProjection(ctx, view, joinPred, query.ProjectionColumns, resolver, scanLimit)
				}
				if err != nil {
					return SQLQueryResult{}, err
				}
				matchedRows = rows
				rowsAlreadyProjected = true
			} else {
				rows, err := evaluateOneOffJoin(ctx, view, joinPred, resolver, scanLimit)
				if err != nil {
					return SQLQueryResult{}, err
				}
				matchedRows = rows
			}
		} else if crossPred, ok := pred.(subscription.CrossJoin); ok {
			if len(query.ProjectionColumns) != 0 {
				var rows []types.ProductValue
				var err error
				if len(query.OrderBy) != 0 {
					rows, err = evaluateOneOffCrossJoinProjectionOrdered(ctx, view, crossPred, query.ProjectionColumns, query.OrderBy, rowOffset, rowLimit)
				} else {
					rows, err = evaluateOneOffCrossJoinProjection(ctx, view, crossPred, query.ProjectionColumns, scanLimit)
				}
				if err != nil {
					return SQLQueryResult{}, err
				}
				matchedRows = rows
				rowsAlreadyProjected = true
			} else {
				rows, err := evaluateOneOffCrossJoin(ctx, view, tableID, crossPred, scanLimit)
				if err != nil {
					return SQLQueryResult{}, err
				}
				matchedRows = rows
			}
		} else {
			if idx, ok := oneOffSingleTableOrderIndex(query.OrderBy, tableID, tableSchema, resolver); ok {
				rows, err := evaluateOneOffSingleTableOrderedByIndex(ctx, view, tableID, pred, idx, rowOffset, rowLimit)
				if err != nil {
					return SQLQueryResult{}, err
				}
				matchedRows = rows
				rowsAlreadyOrderedAndLimited = true
			} else {
				_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(pv types.ProductValue) bool {
					matchedRows = append(matchedRows, pv)
					return !oneOffLimitReached(len(matchedRows), scanLimit)
				})
				if err != nil {
					return SQLQueryResult{}, err
				}
			}
		}
		if len(query.OrderBy) != 0 && !rowsAlreadyProjected && !rowsAlreadyOrderedAndLimited {
			var err error
			matchedRows, err = orderAndLimitOneOffRows(matchedRows, query.OrderBy, rowOffset, rowLimit)
			if err != nil {
				return SQLQueryResult{}, err
			}
		} else if len(query.OrderBy) == 0 {
			matchedRows = sliceOneOffRows(matchedRows, rowOffset, rowLimit)
		}
		if len(query.ProjectionColumns) != 0 && !rowsAlreadyProjected {
			encodedRows = projectOneOffRows(matchedRows, query.ProjectionColumns)
		} else {
			encodedRows = matchedRows
		}
	}
	return SQLQueryResult{TableName: query.TableName, Columns: oneOffResultColumns(query, tableSchema), Rows: encodedRows}, nil
}

func oneOffResultColumns(query compiledSQLQuery, fallback *schema.TableSchema) []schema.ColumnSchema {
	if fallback == nil {
		return query.resultColumns(nil)
	}
	return query.resultColumns(fallback.Columns)
}

// sendOneOffError emits a failure OneOffQueryResponse matching reference
// module_host.rs:2300 (`error: Some(msg), results: vec![]`).
func sendOneOffError(conn *Conn, messageID []byte, errMsg string, receipt time.Time) {
	sendError(conn, OneOffQueryResponse{
		MessageID:                  messageID,
		Error:                      &errMsg,
		TotalHostExecutionDuration: elapsedMicrosI64(receipt),
	})
}

// elapsedMicrosI64 reports the non-zero microsecond delta since receipt
// as an i64 (reference `TimeDuration` is i64 micros — v1.rs / sats
// time_duration.rs). Zero is bumped to 1 so the wire value clearly
// distinguishes measured from the deferred-measurement sentinel.
func elapsedMicrosI64(receipt time.Time) int64 {
	us := time.Since(receipt).Microseconds()
	if us <= 0 {
		return 1
	}
	return us
}

func projectOneOffRows(rows []types.ProductValue, columns []compiledSQLProjectionColumn) []types.ProductValue {
	projected := make([]types.ProductValue, 0, len(rows))
	for _, row := range rows {
		out := make(types.ProductValue, 0, len(columns))
		for _, col := range columns {
			idx := col.Schema.Index
			if idx < 0 || idx >= len(row) {
				continue
			}
			out = append(out, row[idx])
		}
		projected = append(projected, out)
	}
	return projected
}

func oneOffRowLimit(limit *uint64) int {
	if limit == nil {
		return -1
	}
	maxInt := int(^uint(0) >> 1)
	if *limit > uint64(maxInt) {
		return maxInt
	}
	return int(*limit)
}

func oneOffIndexableEquality(pred subscription.Predicate, tableID schema.TableID) (subscription.ColEq, bool) {
	switch p := pred.(type) {
	case subscription.ColEq:
		if p.Table == tableID && p.Alias == 0 {
			return p, true
		}
	case subscription.And:
		if eq, ok := oneOffIndexableEquality(p.Left, tableID); ok {
			return eq, true
		}
		if eq, ok := oneOffIndexableEquality(p.Right, tableID); ok {
			return eq, true
		}
	}
	return subscription.ColEq{}, false
}

func oneOffIndexableRange(pred subscription.Predicate, tableID schema.TableID) (subscription.ColRange, bool) {
	switch p := pred.(type) {
	case subscription.ColRange:
		if p.Table == tableID && p.Alias == 0 {
			return p, true
		}
	case subscription.And:
		if r, ok := oneOffIndexableRange(p.Left, tableID); ok {
			return r, true
		}
		if r, ok := oneOffIndexableRange(p.Right, tableID); ok {
			return r, true
		}
	}
	return subscription.ColRange{}, false
}

type oneOffOrderIndex struct {
	indexID     schema.IndexID
	columnIdxs  []int
	columnNames []string
	descs       []bool
}

func oneOffSingleTableOrderIndex(orderBy []compiledSQLOrderBy, tableID schema.TableID, tableSchema *schema.TableSchema, resolver schema.IndexResolver) (oneOffOrderIndex, bool) {
	if len(orderBy) == 0 {
		return oneOffOrderIndex{}, false
	}
	columns := make([]int, len(orderBy))
	columnNames := make([]string, len(orderBy))
	descs := make([]bool, len(orderBy))
	for i, term := range orderBy {
		if term.Column.Table != tableID || term.Column.Alias != 0 || term.Column.Schema.Index < 0 {
			return oneOffOrderIndex{}, false
		}
		columns[i] = term.Column.Schema.Index
		columnNames[i] = term.Column.Schema.Name
		descs[i] = term.Desc
	}
	if len(columns) == 1 && resolver != nil {
		if indexID, ok := resolver.IndexIDForColumn(tableID, types.ColID(columns[0])); ok {
			return oneOffOrderIndex{
				indexID:     indexID,
				columnIdxs:  columns,
				columnNames: columnNames,
				descs:       descs,
			}, true
		}
	}
	if tableSchema == nil {
		return oneOffOrderIndex{}, false
	}
	for _, idx := range tableSchema.Indexes {
		if slices.Equal(idx.Columns, columns) {
			return oneOffOrderIndex{
				indexID:     idx.ID,
				columnIdxs:  columns,
				columnNames: columnNames,
				descs:       descs,
			}, true
		}
	}
	return oneOffOrderIndex{}, false
}

func evaluateOneOffSingleTableOrderedByIndex(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, orderIndex oneOffOrderIndex, offset int, limit int) ([]types.ProductValue, error) {
	var rows []types.ProductValue
	seen := 0
	allAsc := orderIndex.allAsc()
	for _, row := range view.IndexRange(tableID, orderIndex.indexID, store.UnboundedLow(), store.UnboundedHigh()) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !subscription.MatchRow(pred, tableID, row) {
			continue
		}
		if !allAsc {
			rows = append(rows, row)
			continue
		}
		if seen < offset {
			seen++
			continue
		}
		rows = append(rows, row)
		seen++
		if oneOffLimitReached(len(rows), limit) {
			return rows, nil
		}
	}
	if !allAsc {
		return materializeIndexedOneOffRows(rows, orderIndex.columnIdxs, orderIndex.columnNames, orderIndex.descs, offset, limit)
	}
	return rows, nil
}

func (idx oneOffOrderIndex) allAsc() bool {
	for _, desc := range idx.descs {
		if desc {
			return false
		}
	}
	return true
}

func materializeIndexedOneOffRows(rows []types.ProductValue, columnIdxs []int, columnNames []string, descs []bool, offset int, limit int) ([]types.ProductValue, error) {
	if limit == 0 || len(rows) == 0 {
		return nil, nil
	}
	var out []types.ProductValue
	seen := 0
	var emitSpan func(start, end, depth int) (bool, error)
	emitSpan = func(start, end, depth int) (bool, error) {
		if depth >= len(columnIdxs) {
			for i := start; i < end; i++ {
				if seen < offset {
					seen++
					continue
				}
				out = append(out, rows[i])
				seen++
				if oneOffLimitReached(len(out), limit) {
					return false, nil
				}
			}
			return true, nil
		}
		spans, err := oneOffOrderKeySpans(rows, start, end, columnIdxs[depth], oneOffOrderColumnName(columnNames, depth))
		if err != nil {
			return false, err
		}
		if descs[depth] {
			for i := len(spans) - 1; i >= 0; i-- {
				keepGoing, err := emitSpan(spans[i].start, spans[i].end, depth+1)
				if err != nil || !keepGoing {
					return keepGoing, err
				}
			}
			return true, nil
		}
		for _, span := range spans {
			keepGoing, err := emitSpan(span.start, span.end, depth+1)
			if err != nil {
				return false, err
			}
			if !keepGoing {
				return false, nil
			}
		}
		return true, nil
	}
	if _, err := emitSpan(0, len(rows), 0); err != nil {
		return nil, err
	}
	return out, nil
}

type oneOffRowSpan struct {
	start int
	end   int
}

func oneOffOrderKeySpans(rows []types.ProductValue, start, end int, columnIndex int, columnName string) ([]oneOffRowSpan, error) {
	var spans []oneOffRowSpan
	for spanStart := start; spanStart < end; {
		spanEnd := spanStart + 1
		for spanEnd < end {
			equal, err := oneOffOrderColumnValuesEqual(rows[spanEnd-1], rows[spanEnd], columnIndex, columnName)
			if err != nil {
				return nil, err
			}
			if !equal {
				break
			}
			spanEnd++
		}
		spans = append(spans, oneOffRowSpan{start: spanStart, end: spanEnd})
		spanStart = spanEnd
	}
	return spans, nil
}

func oneOffOrderColumnValuesEqual(left, right types.ProductValue, columnIndex int, columnName string) (bool, error) {
	if columnIndex < 0 || columnIndex >= len(left) || columnIndex >= len(right) {
		return false, fmt.Errorf("ORDER BY column %q is missing from row", columnName)
	}
	return left[columnIndex].Equal(right[columnIndex]), nil
}

func oneOffOrderColumnName(columnNames []string, depth int) string {
	if depth < len(columnNames) {
		return columnNames[depth]
	}
	return ""
}

func visitOneOffSingleTableRows(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, visit func(types.ProductValue) bool) (bool, error) {
	if resolver != nil {
		if eq, ok := oneOffIndexableEquality(pred, tableID); ok {
			if idx, ok := resolver.IndexIDForColumn(eq.Table, eq.Column); ok {
				key := store.NewIndexKey(eq.Value)
				for _, rid := range view.IndexSeek(eq.Table, idx, key) {
					if err := ctx.Err(); err != nil {
						return true, err
					}
					row, ok := view.GetRow(eq.Table, rid)
					if !ok {
						continue
					}
					if subscription.MatchRow(pred, tableID, row) && !visit(row) {
						return true, nil
					}
				}
				return true, nil
			}
		}
		if r, ok := oneOffIndexableRange(pred, tableID); ok {
			if idx, ok := resolver.IndexIDForColumn(r.Table, r.Column); ok {
				lower := storeBoundFromSubscriptionBound(r.Lower)
				upper := storeBoundFromSubscriptionBound(r.Upper)
				for _, row := range view.IndexRange(r.Table, idx, lower, upper) {
					if err := ctx.Err(); err != nil {
						return true, err
					}
					if subscription.MatchRow(pred, tableID, row) && !visit(row) {
						return true, nil
					}
				}
				return true, nil
			}
		}
	}
	for _, pv := range view.TableScan(tableID) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if subscription.MatchRow(pred, tableID, pv) && !visit(pv) {
			return false, nil
		}
	}
	return false, nil
}

func storeBoundFromSubscriptionBound(b subscription.Bound) store.Bound {
	return store.Bound{Value: b.Value, Inclusive: b.Inclusive, Unbounded: b.Unbounded}
}

func oneOffRowOffset(offset *uint64) int {
	if offset == nil {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if *offset > uint64(maxInt) {
		return maxInt
	}
	return int(*offset)
}

func oneOffScanLimit(offset int, limit int) int {
	if limit == 0 {
		return 0
	}
	if limit < 0 {
		return -1
	}
	maxInt := int(^uint(0) >> 1)
	if offset > maxInt-limit {
		return maxInt
	}
	return offset + limit
}

func oneOffLimitReached(count int, limit int) bool {
	return limit >= 0 && count >= limit
}

type orderedOneOffRow struct {
	row types.ProductValue
	key []types.Value
}

func orderAndLimitOneOffRows(rows []types.ProductValue, orderBy []compiledSQLOrderBy, offset int, limit int) ([]types.ProductValue, error) {
	if len(orderBy) == 0 {
		return sliceOneOffRows(rows, offset, limit), nil
	}
	ordered := make([]orderedOneOffRow, 0, len(rows))
	for _, row := range rows {
		keys := make([]types.Value, len(orderBy))
		for i, term := range orderBy {
			idx := term.Column.Schema.Index
			if idx < 0 || idx >= len(row) {
				return nil, fmt.Errorf("ORDER BY column %q is missing from row", term.Column.Schema.Name)
			}
			keys[i] = row[idx]
		}
		ordered = append(ordered, orderedOneOffRow{row: row, key: keys})
	}
	return materializeOrderedOneOffRows(ordered, orderBy, offset, limit), nil
}

func materializeOrderedOneOffRows(rows []orderedOneOffRow, orderBy []compiledSQLOrderBy, offset int, limit int) []types.ProductValue {
	slices.SortStableFunc(rows, func(a, b orderedOneOffRow) int {
		for i, term := range orderBy {
			cmp := a.key[i].Compare(b.key[i])
			if cmp == 0 {
				continue
			}
			if term.Desc {
				return -cmp
			}
			return cmp
		}
		return 0
	})
	start := offset
	if start > len(rows) {
		start = len(rows)
	}
	end := len(rows)
	if limit >= 0 && limit < end-start {
		end = start + limit
	}
	outLen := end - start
	out := make([]types.ProductValue, 0, outLen)
	for i := start; i < end; i++ {
		out = append(out, rows[i].row)
	}
	return out
}

func sliceOneOffRows(rows []types.ProductValue, offset int, limit int) []types.ProductValue {
	start := offset
	if start > len(rows) {
		start = len(rows)
	}
	end := len(rows)
	if limit >= 0 && limit < end-start {
		end = start + limit
	}
	return rows[start:end]
}

func countOneOffMatches(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver) (uint64, error) {
	if joinPred, ok := pred.(subscription.Join); ok {
		return countOneOffJoin(ctx, view, joinPred, resolver)
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		return countOneOffCrossJoin(ctx, view, tableID, crossPred)
	}
	var count uint64
	_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(types.ProductValue) bool {
		count++
		return true
	})
	return count, err
}

func evaluateOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (types.Value, error) {
	if aggregate == nil {
		return types.Value{}, fmt.Errorf("aggregate metadata must not be nil")
	}
	switch aggregate.Func {
	case "COUNT":
		matchedCount, err := countOneOffAggregate(ctx, view, tableID, pred, resolver, aggregate)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewUint64(matchedCount), nil
	case "SUM":
		return sumOneOffAggregate(ctx, view, tableID, pred, resolver, aggregate)
	default:
		return types.Value{}, fmt.Errorf("aggregate %q not supported", aggregate.Func)
	}
}

func countOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (uint64, error) {
	if aggregate == nil || aggregate.Argument == nil {
		return countOneOffMatches(ctx, view, tableID, pred, resolver)
	}
	argument := *aggregate.Argument
	if aggregate.Distinct {
		return countDistinctOneOffAggregate(ctx, view, tableID, pred, resolver, argument)
	}
	var count uint64
	err := visitOneOffAggregateColumnValues(ctx, view, tableID, pred, resolver, argument, func(value types.Value) bool {
		if !value.IsNull() {
			count++
		}
		return true
	})
	return count, err
}

func countDistinctOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, column compiledSQLProjectionColumn) (uint64, error) {
	seen := valueagg.NewDistinctSet()
	err := visitOneOffAggregateColumnValues(ctx, view, tableID, pred, resolver, column, func(value types.Value) bool {
		if !value.IsNull() {
			seen.Add(value)
		}
		return true
	})
	return seen.Count(), err
}

func sumOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (types.Value, error) {
	if aggregate == nil || aggregate.Argument == nil {
		return types.Value{}, fmt.Errorf("SUM aggregate requires a column argument")
	}
	if aggregate.Distinct {
		return types.Value{}, fmt.Errorf("SUM(DISTINCT ...) aggregate not supported")
	}
	argument := *aggregate.Argument
	acc := valueagg.NewSum(aggregate.ResultColumn.Type, aggregate.ResultColumn.Nullable)
	var addErr error
	err := visitOneOffAggregateColumnValues(ctx, view, tableID, pred, resolver, argument, func(value types.Value) bool {
		if err := acc.Add(value); err != nil {
			addErr = err
			return false
		}
		return true
	})
	if err != nil {
		return types.Value{}, err
	}
	if addErr != nil {
		return types.Value{}, addErr
	}
	return acc.Value()
}

func visitOneOffAggregateColumnValues(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, column compiledSQLProjectionColumn, visit func(types.Value) bool) error {
	if joinPred, ok := pred.(subscription.Join); ok {
		return visitOneOffJoinPairs(ctx, view, joinPred, resolver, func(leftRow, rightRow types.ProductValue) bool {
			value, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, joinPred.Left, joinPred.LeftAlias, joinPred.Right, joinPred.RightAlias, column)
			return !ok || visit(value)
		})
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		return visitOneOffCrossJoinPairs(ctx, view, crossPred, false, func(leftRow, rightRow types.ProductValue) bool {
			value, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, crossPred.Left, crossPred.LeftAlias, crossPred.Right, crossPred.RightAlias, column)
			return !ok || visit(value)
		})
	}
	_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(row types.ProductValue) bool {
		value, ok := oneOffAggregateRowColumnValue(row, tableID, column)
		return !ok || visit(value)
	})
	return err
}

func oneOffAggregateRowColumnValue(row types.ProductValue, tableID schema.TableID, column compiledSQLProjectionColumn) (types.Value, bool) {
	if column.Table != tableID {
		return types.Value{}, false
	}
	idx := column.Schema.Index
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func evaluateOneOffJoin(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver, limit int) ([]types.ProductValue, error) {
	// Trust Join.ProjectRight because a self-join has the same table ID on both
	// sides and only the compile-time signal disambiguates the projected side.
	var rows []types.ProductValue
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		if join.ProjectRight {
			rows = append(rows, rightRow)
		} else {
			rows = append(rows, leftRow)
		}
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows, err
}

func countOneOffJoin(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver) (uint64, error) {
	var count uint64
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		count++
		return true
	})
	return count, err
}

func evaluateOneOffJoinProjection(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, resolver schema.IndexResolver, limit int) ([]types.ProductValue, error) {
	return collectOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffJoinPairs(ctx, view, join, resolver, visit)
		},
		join.Left, join.LeftAlias, join.Right, join.RightAlias, columns, limit,
	)
}

func evaluateOneOffJoinProjectionOrdered(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, resolver schema.IndexResolver, offset int, limit int) ([]types.ProductValue, error) {
	return collectOrderedOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffJoinPairs(ctx, view, join, resolver, visit)
		},
		join.Left, join.LeftAlias, join.Right, join.RightAlias, columns, orderBy, offset, limit,
	)
}

func visitOneOffJoinPairs(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver, visit func(leftRow, rightRow types.ProductValue) bool) error {
	visitIfMatch := func(leftRow, rightRow types.ProductValue) bool {
		if !oneOffJoinPairMatches(join, leftRow, rightRow) {
			return true
		}
		return visit(leftRow, rightRow)
	}

	outerTable, outerCol := join.Left, join.LeftCol
	innerTable, innerCol := join.Right, join.RightCol
	emit := func(outerRow, innerRow types.ProductValue) bool {
		if join.ProjectRight {
			return visitIfMatch(innerRow, outerRow)
		}
		return visitIfMatch(outerRow, innerRow)
	}
	if join.ProjectRight {
		outerTable, outerCol = join.Right, join.RightCol
		innerTable, innerCol = join.Left, join.LeftCol
	}

	if resolver != nil {
		if innerIdx, ok := resolver.IndexIDForColumn(innerTable, innerCol); ok {
			for _, outerRow := range view.TableScan(outerTable) {
				if err := ctx.Err(); err != nil {
					return err
				}
				if int(outerCol) >= len(outerRow) {
					continue
				}
				key := store.NewIndexKey(outerRow[outerCol])
				for _, rid := range view.IndexSeek(innerTable, innerIdx, key) {
					if err := ctx.Err(); err != nil {
						return err
					}
					innerRow, ok := view.GetRow(innerTable, rid)
					if !ok {
						continue
					}
					if !emit(outerRow, innerRow) {
						return nil
					}
				}
			}
			return nil
		}
	}

	for _, outerRow := range view.TableScan(outerTable) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if int(outerCol) >= len(outerRow) {
			continue
		}
		for _, innerRow := range view.TableScan(innerTable) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !emit(outerRow, innerRow) {
				return nil
			}
		}
	}
	return nil
}

func oneOffJoinPairMatches(join subscription.Join, leftRow, rightRow types.ProductValue) bool {
	if int(join.LeftCol) >= len(leftRow) || int(join.RightCol) >= len(rightRow) {
		return false
	}
	if !leftRow[join.LeftCol].Equal(rightRow[join.RightCol]) {
		return false
	}
	if join.Filter != nil && !subscription.MatchJoinPair(join.Filter, join.Left, join.LeftAlias, leftRow, join.Right, join.RightAlias, rightRow) {
		return false
	}
	return true
}

func evaluateOneOffCrossJoinProjection(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, columns []compiledSQLProjectionColumn, limit int) ([]types.ProductValue, error) {
	return collectOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffCrossJoinPairs(ctx, view, cross, true, visit)
		},
		cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns, limit,
	)
}

func evaluateOneOffCrossJoinProjectionOrdered(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, offset int, limit int) ([]types.ProductValue, error) {
	return collectOrderedOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffCrossJoinPairs(ctx, view, cross, true, visit)
		},
		cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns, orderBy, offset, limit,
	)
}

func collectOneOffPairProjections(visitPairs func(func(types.ProductValue, types.ProductValue) bool) error, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, columns []compiledSQLProjectionColumn, limit int) ([]types.ProductValue, error) {
	var rows []types.ProductValue
	err := visitPairs(func(leftRow, rightRow types.ProductValue) bool {
		rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, leftID, leftAlias, rightID, rightAlias, columns))
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows, err
}

func collectOrderedOneOffPairProjections(visitPairs func(func(types.ProductValue, types.ProductValue) bool) error, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, offset int, limit int) ([]types.ProductValue, error) {
	var rows []orderedOneOffRow
	var orderErr error
	err := visitPairs(func(leftRow, rightRow types.ProductValue) bool {
		key, err := orderKeysFromJoinPair(leftRow, rightRow, leftID, leftAlias, rightID, rightAlias, orderBy)
		if err != nil {
			orderErr = err
			return false
		}
		rows = append(rows, orderedOneOffRow{
			row: projectOneOffJoinPair(leftRow, rightRow, leftID, leftAlias, rightID, rightAlias, columns),
			key: key,
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	if orderErr != nil {
		return nil, orderErr
	}
	return materializeOrderedOneOffRows(rows, orderBy, offset, limit), nil
}

func projectOneOffJoinPair(leftRow, rightRow types.ProductValue, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, columns []compiledSQLProjectionColumn) types.ProductValue {
	out := make(types.ProductValue, 0, len(columns))
	for _, col := range columns {
		source, ok := projectedJoinColumnSource(col, leftID, leftAlias, leftRow, rightID, rightAlias, rightRow)
		if !ok {
			continue
		}
		idx := col.Schema.Index
		if idx < 0 || idx >= len(source) {
			continue
		}
		out = append(out, source[idx])
	}
	return out
}

func projectedJoinColumnSource(col compiledSQLProjectionColumn, leftID schema.TableID, leftAlias uint8, leftRow types.ProductValue, rightID schema.TableID, rightAlias uint8, rightRow types.ProductValue) (types.ProductValue, bool) {
	if leftID == rightID {
		switch {
		case col.Table == leftID && col.Alias == leftAlias:
			return leftRow, true
		case col.Table == rightID && col.Alias == rightAlias:
			return rightRow, true
		default:
			return nil, false
		}
	}
	switch col.Table {
	case leftID:
		return leftRow, true
	case rightID:
		return rightRow, true
	default:
		return nil, false
	}
}

func oneOffAggregateJoinColumnValue(leftRow, rightRow types.ProductValue, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, column compiledSQLProjectionColumn) (types.Value, bool) {
	source, ok := projectedJoinColumnSource(column, leftID, leftAlias, leftRow, rightID, rightAlias, rightRow)
	if !ok {
		return types.Value{}, false
	}
	idx := column.Schema.Index
	if idx < 0 || idx >= len(source) {
		return types.Value{}, false
	}
	return source[idx], true
}

func orderKeysFromJoinPair(leftRow, rightRow types.ProductValue, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, orderBy []compiledSQLOrderBy) ([]types.Value, error) {
	keys := make([]types.Value, len(orderBy))
	for i, term := range orderBy {
		source, ok := projectedJoinColumnSource(term.Column, leftID, leftAlias, leftRow, rightID, rightAlias, rightRow)
		if !ok {
			return nil, fmt.Errorf("ORDER BY column %q is not from the projected table", term.Column.Schema.Name)
		}
		idx := term.Column.Schema.Index
		if idx < 0 || idx >= len(source) {
			return nil, fmt.Errorf("ORDER BY column %q is missing from row", term.Column.Schema.Name)
		}
		keys[i] = source[idx]
	}
	return keys, nil
}

func evaluateOneOffCrossJoin(ctx context.Context, view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoin, limit int) ([]types.ProductValue, error) {
	if projectedTable != cross.ProjectedTable() {
		return nil, nil
	}
	if cross.Filter != nil {
		var rows []types.ProductValue
		err := visitOneOffCrossJoinPairs(ctx, view, cross, true, func(leftRow, rightRow types.ProductValue) bool {
			if cross.ProjectRight {
				rows = append(rows, rightRow)
			} else {
				rows = append(rows, leftRow)
			}
			return !oneOffLimitReached(len(rows), limit)
		})
		return rows, err
	}
	otherTable := cross.Left
	if projectedTable == cross.Left {
		otherTable = cross.Right
	}
	otherCount := view.RowCount(otherTable)
	if otherCount == 0 {
		return nil, nil
	}
	var rows []types.ProductValue
	for _, row := range view.TableScan(projectedTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i := 0; i < otherCount; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			rows = append(rows, row)
			if oneOffLimitReached(len(rows), limit) {
				return rows, nil
			}
		}
	}
	return rows, nil
}

func countOneOffCrossJoin(ctx context.Context, view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoin) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if projectedTable != cross.ProjectedTable() {
		return 0, nil
	}
	if cross.Filter != nil {
		var count uint64
		err := visitOneOffCrossJoinPairs(ctx, view, cross, false, func(leftRow, rightRow types.ProductValue) bool {
			count++
			return true
		})
		return count, err
	}
	otherTable := cross.Left
	if projectedTable == cross.Left {
		otherTable = cross.Right
	}
	return uint64(view.RowCount(projectedTable)) * uint64(view.RowCount(otherTable)), nil
}

func visitOneOffCrossJoinPairs(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, projectedSideOuter bool, visit func(leftRow, rightRow types.ProductValue) bool) error {
	visitIfMatch := func(leftRow, rightRow types.ProductValue) bool {
		if cross.Filter != nil && !subscription.MatchJoinPair(cross.Filter, cross.Left, cross.LeftAlias, leftRow, cross.Right, cross.RightAlias, rightRow) {
			return true
		}
		return visit(leftRow, rightRow)
	}
	if projectedSideOuter && cross.ProjectRight {
		for _, rightRow := range view.TableScan(cross.Right) {
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, leftRow := range view.TableScan(cross.Left) {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !visitIfMatch(leftRow, rightRow) {
					return nil
				}
			}
		}
		return nil
	}
	for _, leftRow := range view.TableScan(cross.Left) {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, rightRow := range view.TableScan(cross.Right) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !visitIfMatch(leftRow, rightRow) {
				return nil
			}
		}
	}
	return nil
}
