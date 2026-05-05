package protocol

import (
	"context"
	"fmt"
	"slices"
	"time"

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

	encoded, err := EncodeProductRows(result.Rows)
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
	tableID, _, ok := sl.TableByName(query.TableName)
	if !ok {
		//lint:ignore ST1005 protocol tests pin this sentence-form error text.
		return SQLQueryResult{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", query.TableName)
	}

	pred := query.Predicate
	if err := subscription.ValidateQueryPredicate(pred, sl); err != nil {
		return SQLQueryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SQLQueryResult{}, err
	}

	view := stateAccess.Snapshot()
	defer view.Close()

	resolver, _ := sl.(schema.IndexResolver)
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
				rows, err := evaluateOneOffJoin(ctx, view, tableID, joinPred, resolver, scanLimit)
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
			if idx, ok := oneOffSingleTableOrderIndex(query.OrderBy, tableID, resolver); ok {
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
	return SQLQueryResult{TableName: query.TableName, Rows: encodedRows}, nil
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
	desc        bool
	columnIndex int
	columnName  string
}

func oneOffSingleTableOrderIndex(orderBy []compiledSQLOrderBy, tableID schema.TableID, resolver schema.IndexResolver) (oneOffOrderIndex, bool) {
	if resolver == nil || len(orderBy) != 1 {
		return oneOffOrderIndex{}, false
	}
	term := orderBy[0]
	if term.Column.Table != tableID || term.Column.Alias != 0 || term.Column.Schema.Index < 0 {
		return oneOffOrderIndex{}, false
	}
	indexID, ok := resolver.IndexIDForColumn(tableID, types.ColID(term.Column.Schema.Index))
	if !ok {
		return oneOffOrderIndex{}, false
	}
	return oneOffOrderIndex{
		indexID:     indexID,
		desc:        term.Desc,
		columnIndex: term.Column.Schema.Index,
		columnName:  term.Column.Schema.Name,
	}, true
}

func evaluateOneOffSingleTableOrderedByIndex(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, orderIndex oneOffOrderIndex, offset int, limit int) ([]types.ProductValue, error) {
	var rows []types.ProductValue
	seen := 0
	for _, row := range view.IndexRange(tableID, orderIndex.indexID, store.UnboundedLow(), store.UnboundedHigh()) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !subscription.MatchRow(pred, tableID, row) {
			continue
		}
		if orderIndex.desc {
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
	if orderIndex.desc {
		return materializeDescendingIndexedOneOffRows(rows, orderIndex.columnIndex, orderIndex.columnName, offset, limit)
	}
	return rows, nil
}

func materializeDescendingIndexedOneOffRows(rows []types.ProductValue, columnIndex int, columnName string, offset int, limit int) ([]types.ProductValue, error) {
	if limit == 0 || len(rows) == 0 {
		return nil, nil
	}
	var out []types.ProductValue
	seen := 0
	for end := len(rows); end > 0; {
		start := end - 1
		if columnIndex < 0 || columnIndex >= len(rows[start]) {
			return nil, fmt.Errorf("ORDER BY column %q is missing from row", columnName)
		}
		key := rows[start][columnIndex]
		for start > 0 {
			prev := rows[start-1]
			if columnIndex < 0 || columnIndex >= len(prev) {
				return nil, fmt.Errorf("ORDER BY column %q is missing from row", columnName)
			}
			if prev[columnIndex].Compare(key) != 0 {
				break
			}
			start--
		}
		for i := start; i < end; i++ {
			if seen < offset {
				seen++
				continue
			}
			out = append(out, rows[i])
			seen++
			if oneOffLimitReached(len(out), limit) {
				return out, nil
			}
		}
		end = start
	}
	return out, nil
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
		return countOneOffJoin(ctx, view, tableID, joinPred, resolver)
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
	if joinPred, ok := pred.(subscription.Join); ok {
		return countOneOffJoinColumn(ctx, view, joinPred, resolver, argument)
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		return countOneOffCrossJoinColumn(ctx, view, crossPred, argument)
	}
	var count uint64
	_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(pv types.ProductValue) bool {
		if oneOffAggregateRowColumnPresent(pv, tableID, argument) {
			count++
		}
		return true
	})
	return count, err
}

func countDistinctOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, column compiledSQLProjectionColumn) (uint64, error) {
	seen := newOneOffDistinctValueSet()
	if joinPred, ok := pred.(subscription.Join); ok {
		err := visitOneOffJoinPairs(ctx, view, joinPred, resolver, func(leftRow, rightRow types.ProductValue) bool {
			value, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, joinPred.Left, joinPred.LeftAlias, joinPred.Right, joinPred.RightAlias, column)
			if ok {
				seen.add(value)
			}
			return true
		})
		return seen.count(), err
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		err := visitOneOffCrossJoinPairs(ctx, view, crossPred, false, func(leftRow, rightRow types.ProductValue) bool {
			value, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, crossPred.Left, crossPred.LeftAlias, crossPred.Right, crossPred.RightAlias, column)
			if ok {
				seen.add(value)
			}
			return true
		})
		return seen.count(), err
	}
	_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(pv types.ProductValue) bool {
		value, ok := oneOffAggregateRowColumnValue(pv, tableID, column)
		if ok {
			seen.add(value)
		}
		return true
	})
	return seen.count(), err
}

func sumOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (types.Value, error) {
	if aggregate == nil || aggregate.Argument == nil {
		return types.Value{}, fmt.Errorf("SUM aggregate requires a column argument")
	}
	argument := *aggregate.Argument
	acc := newOneOffSumAccumulator(aggregate.ResultColumn.Type)
	if joinPred, ok := pred.(subscription.Join); ok {
		err := visitOneOffJoinPairs(ctx, view, joinPred, resolver, func(leftRow, rightRow types.ProductValue) bool {
			value, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, joinPred.Left, joinPred.LeftAlias, joinPred.Right, joinPred.RightAlias, argument)
			if !ok {
				return true
			}
			if err := acc.add(value); err != nil {
				acc.err = err
				return false
			}
			return true
		})
		if err != nil {
			return types.Value{}, err
		}
		return acc.value()
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		err := visitOneOffCrossJoinPairs(ctx, view, crossPred, false, func(leftRow, rightRow types.ProductValue) bool {
			value, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, crossPred.Left, crossPred.LeftAlias, crossPred.Right, crossPred.RightAlias, argument)
			if !ok {
				return true
			}
			if err := acc.add(value); err != nil {
				acc.err = err
				return false
			}
			return true
		})
		if err != nil {
			return types.Value{}, err
		}
		return acc.value()
	}
	var addErr error
	_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(pv types.ProductValue) bool {
		value, ok := oneOffAggregateRowColumnValue(pv, tableID, argument)
		if !ok {
			return true
		}
		if err := acc.add(value); err != nil {
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
	return acc.value()
}

func oneOffAggregateRowColumnPresent(row types.ProductValue, tableID schema.TableID, column compiledSQLProjectionColumn) bool {
	_, ok := oneOffAggregateRowColumnValue(row, tableID, column)
	return ok
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

type oneOffDistinctValueSet struct {
	buckets map[uint64][]types.Value
	n       uint64
}

func newOneOffDistinctValueSet() *oneOffDistinctValueSet {
	return &oneOffDistinctValueSet{buckets: make(map[uint64][]types.Value)}
}

func (s *oneOffDistinctValueSet) add(value types.Value) {
	hash := value.Hash64()
	for _, existing := range s.buckets[hash] {
		if value.Equal(existing) {
			return
		}
	}
	s.buckets[hash] = append(s.buckets[hash], value)
	s.n++
}

func (s *oneOffDistinctValueSet) count() uint64 {
	return s.n
}

type oneOffSumAccumulator struct {
	kind types.ValueKind
	i64  int64
	u64  uint64
	f64  float64
	err  error
}

func newOneOffSumAccumulator(kind types.ValueKind) *oneOffSumAccumulator {
	return &oneOffSumAccumulator{kind: kind}
}

func (a *oneOffSumAccumulator) add(value types.Value) error {
	if a.err != nil {
		return a.err
	}
	switch a.kind {
	case types.KindInt64:
		n, ok := oneOffValueAsInt64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-signed value kind %s", value.Kind())
			return a.err
		}
		sum := a.i64 + n
		if (n > 0 && sum < a.i64) || (n < 0 && sum > a.i64) {
			a.err = fmt.Errorf("SUM aggregate overflowed Int64")
			return a.err
		}
		a.i64 = sum
	case types.KindUint64:
		n, ok := oneOffValueAsUint64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-unsigned value kind %s", value.Kind())
			return a.err
		}
		if ^uint64(0)-a.u64 < n {
			a.err = fmt.Errorf("SUM aggregate overflowed Uint64")
			return a.err
		}
		a.u64 += n
	case types.KindFloat64:
		n, ok := oneOffValueAsFloat64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-float value kind %s", value.Kind())
			return a.err
		}
		a.f64 += n
	default:
		a.err = fmt.Errorf("SUM aggregate result kind %s not supported", a.kind)
	}
	return a.err
}

func (a *oneOffSumAccumulator) value() (types.Value, error) {
	if a.err != nil {
		return types.Value{}, a.err
	}
	switch a.kind {
	case types.KindInt64:
		return types.NewInt64(a.i64), nil
	case types.KindUint64:
		return types.NewUint64(a.u64), nil
	case types.KindFloat64:
		return types.NewFloat64(a.f64)
	default:
		return types.Value{}, fmt.Errorf("SUM aggregate result kind %s not supported", a.kind)
	}
}

func oneOffValueAsInt64(value types.Value) (int64, bool) {
	switch value.Kind() {
	case types.KindInt8:
		return int64(value.AsInt8()), true
	case types.KindInt16:
		return int64(value.AsInt16()), true
	case types.KindInt32:
		return int64(value.AsInt32()), true
	case types.KindInt64:
		return value.AsInt64(), true
	default:
		return 0, false
	}
}

func oneOffValueAsUint64(value types.Value) (uint64, bool) {
	switch value.Kind() {
	case types.KindUint8:
		return uint64(value.AsUint8()), true
	case types.KindUint16:
		return uint64(value.AsUint16()), true
	case types.KindUint32:
		return uint64(value.AsUint32()), true
	case types.KindUint64:
		return value.AsUint64(), true
	default:
		return 0, false
	}
}

func oneOffValueAsFloat64(value types.Value) (float64, bool) {
	switch value.Kind() {
	case types.KindFloat32:
		return float64(value.AsFloat32()), true
	case types.KindFloat64:
		return value.AsFloat64(), true
	default:
		return 0, false
	}
}

func evaluateOneOffJoin(ctx context.Context, view store.CommittedReadView, projectedTable schema.TableID, join subscription.Join, resolver schema.IndexResolver, limit int) ([]types.ProductValue, error) {
	// Trust Join.ProjectRight (compile-time signal) over projectedTable
	// equality, because a self-join has Left == Right == projectedTable on
	// both sides and only the boolean disambiguates.
	_ = projectedTable
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

func countOneOffJoin(ctx context.Context, view store.CommittedReadView, projectedTable schema.TableID, join subscription.Join, resolver schema.IndexResolver) (uint64, error) {
	_ = projectedTable
	var count uint64
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		count++
		return true
	})
	return count, err
}

func countOneOffJoinColumn(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver, column compiledSQLProjectionColumn) (uint64, error) {
	var count uint64
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		if oneOffAggregateJoinColumnPresent(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias, column) {
			count++
		}
		return true
	})
	return count, err
}

func evaluateOneOffJoinProjection(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, resolver schema.IndexResolver, limit int) ([]types.ProductValue, error) {
	var rows []types.ProductValue
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias, columns))
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows, err
}

func evaluateOneOffJoinProjectionOrdered(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, resolver schema.IndexResolver, offset int, limit int) ([]types.ProductValue, error) {
	var rows []orderedOneOffRow
	var orderErr error
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		key, err := orderKeysFromJoinPair(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias, orderBy)
		if err != nil {
			orderErr = err
			return false
		}
		rows = append(rows, orderedOneOffRow{
			row: projectOneOffJoinPair(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias, columns),
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

func visitOneOffJoinPairs(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver, visit func(leftRow, rightRow types.ProductValue) bool) error {
	visitIfMatch := func(leftRow, rightRow types.ProductValue) bool {
		if !oneOffJoinPairMatches(join, leftRow, rightRow) {
			return true
		}
		return visit(leftRow, rightRow)
	}

	if join.ProjectRight {
		if resolver != nil {
			if leftIdx, ok := resolver.IndexIDForColumn(join.Left, join.LeftCol); ok {
				for _, rightRow := range view.TableScan(join.Right) {
					if err := ctx.Err(); err != nil {
						return err
					}
					if int(join.RightCol) >= len(rightRow) {
						continue
					}
					key := store.NewIndexKey(rightRow[join.RightCol])
					for _, rid := range view.IndexSeek(join.Left, leftIdx, key) {
						if err := ctx.Err(); err != nil {
							return err
						}
						leftRow, ok := view.GetRow(join.Left, rid)
						if !ok {
							continue
						}
						if !visitIfMatch(leftRow, rightRow) {
							return nil
						}
					}
				}
				return nil
			}
		}
		for _, rightRow := range view.TableScan(join.Right) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if int(join.RightCol) >= len(rightRow) {
				continue
			}
			for _, leftRow := range view.TableScan(join.Left) {
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

	if resolver != nil {
		if rightIdx, ok := resolver.IndexIDForColumn(join.Right, join.RightCol); ok {
			for _, leftRow := range view.TableScan(join.Left) {
				if err := ctx.Err(); err != nil {
					return err
				}
				if int(join.LeftCol) >= len(leftRow) {
					continue
				}
				key := store.NewIndexKey(leftRow[join.LeftCol])
				for _, rid := range view.IndexSeek(join.Right, rightIdx, key) {
					if err := ctx.Err(); err != nil {
						return err
					}
					rightRow, ok := view.GetRow(join.Right, rid)
					if !ok {
						continue
					}
					if !visitIfMatch(leftRow, rightRow) {
						return nil
					}
				}
			}
			return nil
		}
	}
	for _, leftRow := range view.TableScan(join.Left) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if int(join.LeftCol) >= len(leftRow) {
			continue
		}
		for _, rightRow := range view.TableScan(join.Right) {
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
	var rows []types.ProductValue
	err := visitOneOffCrossJoinPairs(ctx, view, cross, true, func(leftRow, rightRow types.ProductValue) bool {
		rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns))
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows, err
}

func evaluateOneOffCrossJoinProjectionOrdered(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, offset int, limit int) ([]types.ProductValue, error) {
	var rows []orderedOneOffRow
	var orderErr error
	err := visitOneOffCrossJoinPairs(ctx, view, cross, true, func(leftRow, rightRow types.ProductValue) bool {
		key, err := orderKeysFromJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, orderBy)
		if err != nil {
			orderErr = err
			return false
		}
		rows = append(rows, orderedOneOffRow{
			row: projectOneOffJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns),
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

func oneOffAggregateJoinColumnPresent(leftRow, rightRow types.ProductValue, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, column compiledSQLProjectionColumn) bool {
	_, ok := oneOffAggregateJoinColumnValue(leftRow, rightRow, leftID, leftAlias, rightID, rightAlias, column)
	return ok
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

func countOneOffCrossJoinColumn(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, column compiledSQLProjectionColumn) (uint64, error) {
	var count uint64
	err := visitOneOffCrossJoinPairs(ctx, view, cross, false, func(leftRow, rightRow types.ProductValue) bool {
		if oneOffAggregateJoinColumnPresent(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, column) {
			count++
		}
		return true
	})
	return count, err
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
