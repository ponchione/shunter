package protocol

import (
	"cmp"
	"container/heap"
	"context"
	"fmt"
	"math"
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
	handleOneOffQueryWithVisibilityAndLimits(ctx, conn, msg, stateAccess, sl, nil, SQLQueryLimits{
		MaxRows:  DefaultSQLQueryMaxRows,
		MaxBytes: DefaultSQLQueryMaxBytes,
	})
}

func handleOneOffQueryWithVisibility(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
) {
	handleOneOffQueryWithVisibilityAndLimits(ctx, conn, msg, stateAccess, sl, visibilityFilters, SQLQueryLimits{
		MaxRows:  DefaultSQLQueryMaxRows,
		MaxBytes: DefaultSQLQueryMaxBytes,
	})
}

func handleOneOffQueryWithVisibilityAndLimits(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
	limits SQLQueryLimits,
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

	result, err := ExecuteCompiledSQLQueryWithLimits(ctx, compiled, stateAccess, readSL, limits)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		recordProtocolMessage(conn.Observer, "one_off_query", "internal_error")
		return
	}

	encoded, err := encodeProductRowsForColumnsWithLimit(result.Rows, result.Columns, limits.MaxBytes)
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
	return executeCompiledSQLQuery(ctx, compiled, stateAccess, sl, SQLQueryLimits{})
}

// ExecuteCompiledSQLQueryWithLimits evaluates a precompiled query while
// enforcing host-controlled row and encoded-byte result limits.
func ExecuteCompiledSQLQueryWithLimits(ctx context.Context, compiled CompiledSQLQuery, stateAccess CommittedStateAccess, sl SchemaLookup, limits SQLQueryLimits) (SQLQueryResult, error) {
	normalized, err := NormalizeSQLQueryLimits(limits)
	if err != nil {
		return SQLQueryResult{}, err
	}
	return executeCompiledSQLQuery(ctx, compiled, stateAccess, sl, normalized)
}

func executeCompiledSQLQuery(ctx context.Context, compiled CompiledSQLQuery, stateAccess CommittedStateAccess, sl SchemaLookup, limits SQLQueryLimits) (SQLQueryResult, error) {
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
		result, err := executeCompiledSQLMultiJoin(ctx, query, stateAccess, resolver, limits)
		if err != nil {
			return SQLQueryResult{}, err
		}
		return result, nil
	}
	if err := subscription.ValidateQueryPredicate(pred, sl); err != nil {
		return SQLQueryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SQLQueryResult{}, err
	}

	view := stateAccess.Snapshot()
	defer view.Close()
	resultColumns := oneOffResultColumns(query, tableSchema)
	resultBudget, err := newEncodedResultBudget(resultColumns, limits.MaxBytes)
	if err != nil {
		return SQLQueryResult{}, err
	}

	rowLimit := oneOffRowLimit(query.Limit)
	rowOffset := oneOffRowOffset(query.Offset)
	executionRowLimit, err := oneOffExecutionRowLimit(rowLimit, rowOffset, limits.MaxRows)
	if err != nil {
		return SQLQueryResult{}, err
	}
	scanLimit := oneOffScanLimit(rowOffset, executionRowLimit)
	var encodedRows []types.ProductValue
	if query.Aggregate != nil {
		// Aggregate shape happens over the full matched input; OFFSET/LIMIT then
		// slice the one-row aggregate output (reference ProjectList::Limit wraps
		// ProjectList::Agg). LIMIT 0 or OFFSET >= 1 drops the aggregate row.
		aggregateValue, err := evaluateOneOffAggregate(ctx, view, tableID, pred, resolver, query.Aggregate)
		if err != nil {
			return SQLQueryResult{}, err
		}
		encodedRows = sliceOneOffRows([]types.ProductValue{{aggregateValue}}, rowOffset, rowLimit)
		for _, row := range encodedRows {
			if _, err := resultBudget.add(row); err != nil {
				return SQLQueryResult{}, err
			}
		}
	} else if rowLimit != 0 {
		if joinPred, ok := pred.(subscription.Join); ok {
			if len(query.ProjectionColumns) != 0 {
				if len(query.OrderBy) != 0 {
					encodedRows, err = evaluateOneOffJoinProjectionOrdered(ctx, view, joinPred, query.ProjectionColumns, query.OrderBy, resolver, rowOffset, rowLimit, scanLimit, resultBudget)
				} else {
					encodedRows, err = evaluateOneOffJoinProjection(ctx, view, joinPred, query.ProjectionColumns, resolver, rowOffset, executionRowLimit, resultBudget)
				}
			} else {
				if len(query.OrderBy) != 0 {
					encodedRows, err = evaluateOneOffJoinOrdered(ctx, view, joinPred, query.OrderBy, resolver, rowOffset, rowLimit, scanLimit, resultBudget)
				} else {
					encodedRows, err = evaluateOneOffJoin(ctx, view, joinPred, resolver, rowOffset, executionRowLimit, resultBudget)
				}
			}
		} else if crossPred, ok := pred.(subscription.CrossJoin); ok {
			if len(query.ProjectionColumns) != 0 {
				if len(query.OrderBy) != 0 {
					encodedRows, err = evaluateOneOffCrossJoinProjectionOrdered(ctx, view, crossPred, query.ProjectionColumns, query.OrderBy, rowOffset, rowLimit, scanLimit, resultBudget)
				} else {
					encodedRows, err = evaluateOneOffCrossJoinProjection(ctx, view, crossPred, query.ProjectionColumns, rowOffset, executionRowLimit, resultBudget)
				}
			} else {
				if len(query.OrderBy) != 0 {
					encodedRows, err = evaluateOneOffCrossJoinOrdered(ctx, view, crossPred, query.OrderBy, rowOffset, rowLimit, scanLimit, resultBudget)
				} else {
					encodedRows, err = evaluateOneOffCrossJoin(ctx, view, tableID, crossPred, rowOffset, executionRowLimit, resultBudget)
				}
			}
		} else {
			projectRow := func(row types.ProductValue) types.ProductValue {
				if len(query.ProjectionColumns) == 0 {
					return row
				}
				return projectOneOffRow(row, query.ProjectionColumns)
			}
			if idx, ok := oneOffSingleTableOrderIndex(query.OrderBy, tableID, tableSchema, resolver); ok {
				encodedRows, err = evaluateOneOffSingleTableOrderedByIndex(ctx, view, tableID, pred, idx, query.OrderBy, rowOffset, rowLimit, executionRowLimit, scanLimit, projectRow, resultBudget)
			} else if len(query.OrderBy) != 0 {
				encodedRows, err = evaluateOneOffSingleTableOrdered(ctx, view, tableID, pred, resolver, query.OrderBy, rowOffset, rowLimit, scanLimit, projectRow, resultBudget)
			} else {
				collector := newOneOffResultCollector(rowOffset, executionRowLimit, resultBudget)
				_, err = visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(pv types.ProductValue) bool {
					return collector.Visit(projectRow(pv))
				})
				if err == nil {
					encodedRows, err = collector.Result()
				}
			}
		}
		if err != nil {
			return SQLQueryResult{}, err
		}
	}
	result := SQLQueryResult{TableName: query.TableName, Columns: resultColumns, Rows: encodedRows}
	if err := validateSQLQueryResultRowLimit(result, limits); err != nil {
		return SQLQueryResult{}, err
	}
	return result, nil
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

func projectOneOffRow(row types.ProductValue, columns []compiledSQLProjectionColumn) types.ProductValue {
	out := make(types.ProductValue, 0, len(columns))
	for _, col := range columns {
		idx := col.Schema.Index
		if idx < 0 || idx >= len(row) {
			continue
		}
		out = append(out, row[idx])
	}
	return out
}

func oneOffRowLimit(limit *uint64) int {
	if limit == nil {
		return -1
	}
	if *limit > uint64(math.MaxInt) {
		return math.MaxInt
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
	indexID schema.IndexID
	descs   []bool
}

func oneOffSingleTableOrderIndex(orderBy []compiledSQLOrderBy, tableID schema.TableID, tableSchema *schema.TableSchema, resolver schema.IndexResolver) (oneOffOrderIndex, bool) {
	if len(orderBy) == 0 {
		return oneOffOrderIndex{}, false
	}
	columns := make([]int, len(orderBy))
	descs := make([]bool, len(orderBy))
	for i, term := range orderBy {
		if term.Column.Table != tableID || term.Column.Alias != 0 || term.Column.Schema.Index < 0 {
			return oneOffOrderIndex{}, false
		}
		columns[i] = term.Column.Schema.Index
		descs[i] = term.Desc
	}
	if len(columns) == 1 && resolver != nil {
		if indexID, ok := resolver.IndexIDForColumn(tableID, types.ColID(columns[0])); ok {
			return oneOffOrderIndex{
				indexID: indexID,
				descs:   descs,
			}, true
		}
	}
	if tableSchema == nil {
		return oneOffOrderIndex{}, false
	}
	for _, idx := range tableSchema.Indexes {
		if slices.Equal(idx.Columns, columns) {
			return oneOffOrderIndex{
				indexID: idx.ID,
				descs:   descs,
			}, true
		}
	}
	return oneOffOrderIndex{}, false
}

func evaluateOneOffSingleTableOrderedByIndex(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, orderIndex oneOffOrderIndex, orderBy []compiledSQLOrderBy, offset, limit, executionLimit, capacity int, projectRow func(types.ProductValue) types.ProductValue, budget *encodedResultBudget) ([]types.ProductValue, error) {
	if !orderIndex.allAsc() {
		collector := newOrderedOneOffCollector(orderBy, capacity, budget)
		for _, row := range view.IndexRange(tableID, orderIndex.indexID, store.UnboundedLow(), store.UnboundedHigh()) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !subscription.MatchRow(pred, tableID, row) {
				continue
			}
			key := make([]types.Value, len(orderBy))
			for i, term := range orderBy {
				idx := term.Column.Schema.Index
				if idx < 0 || idx >= len(row) {
					return nil, fmt.Errorf("ORDER BY column %q is missing from row", term.Column.Schema.Name)
				}
				key[i] = row[idx]
			}
			if err := collector.Add(projectRow(row), key); err != nil {
				return nil, err
			}
		}
		return materializeOrderedOneOffRows(collector.SortedRows(), offset, limit), nil
	}

	collector := newOneOffResultCollector(offset, executionLimit, budget)
	for _, row := range view.IndexRange(tableID, orderIndex.indexID, store.UnboundedLow(), store.UnboundedHigh()) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !subscription.MatchRow(pred, tableID, row) {
			continue
		}
		if !collector.Visit(projectRow(row)) {
			break
		}
	}
	return collector.Result()
}

func (idx oneOffOrderIndex) allAsc() bool {
	for _, desc := range idx.descs {
		if desc {
			return false
		}
	}
	return true
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
	if *offset > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(*offset)
}

func oneOffExecutionRowLimit(limit, offset, maxRows int) (int, error) {
	if limit == 0 || maxRows <= 0 {
		return limit, nil
	}
	detectionLimit := maxRows
	if detectionLimit < math.MaxInt {
		detectionLimit++
	}
	if limit < 0 || limit > detectionLimit {
		return detectionLimit, nil
	}
	return limit, nil
}

func oneOffScanLimit(offset int, limit int) int {
	if limit == 0 {
		return 0
	}
	if limit < 0 {
		return -1
	}
	if offset > math.MaxInt-limit {
		return math.MaxInt
	}
	return offset + limit
}

func oneOffLimitReached(count int, limit int) bool {
	return limit >= 0 && count >= limit
}

func oneOffWindowBounds(length int, offset int, limit int) (int, int) {
	start := offset
	if start > length {
		start = length
	}
	end := length
	if limit >= 0 && limit < end-start {
		end = start + limit
	}
	return start, end
}

type orderedOneOffRow struct {
	row          types.ProductValue
	key          []types.Value
	ordinal      uint64
	encodedBytes int
}

type orderedOneOffCollector struct {
	rows      []orderedOneOffRow
	orderBy   []compiledSQLOrderBy
	capacity  int
	nextOrder uint64
	budget    *encodedResultBudget
}

func newOrderedOneOffCollector(orderBy []compiledSQLOrderBy, capacity int, budget *encodedResultBudget) *orderedOneOffCollector {
	return &orderedOneOffCollector{orderBy: orderBy, capacity: capacity, budget: budget}
}

func (c *orderedOneOffCollector) Len() int { return len(c.rows) }

// Less keeps the worst retained row at heap index zero.
func (c *orderedOneOffCollector) Less(i, j int) bool {
	return compareOrderedOneOffRows(c.rows[i], c.rows[j], c.orderBy) > 0
}

func (c *orderedOneOffCollector) Swap(i, j int) { c.rows[i], c.rows[j] = c.rows[j], c.rows[i] }

func (c *orderedOneOffCollector) Push(value any) {
	c.rows = append(c.rows, value.(orderedOneOffRow))
}

func (c *orderedOneOffCollector) Pop() any {
	last := len(c.rows) - 1
	value := c.rows[last]
	c.rows = c.rows[:last]
	return value
}

func (c *orderedOneOffCollector) Add(row types.ProductValue, key []types.Value) error {
	item := orderedOneOffRow{row: row, key: key, ordinal: c.nextOrder}
	c.nextOrder++
	if c.capacity == 0 {
		return nil
	}
	if c.capacity < 0 {
		encodedBytes, err := c.budget.add(row)
		if err != nil {
			return err
		}
		item.encodedBytes = encodedBytes
		c.rows = append(c.rows, item)
		return nil
	}
	if len(c.rows) < c.capacity {
		encodedBytes, err := c.budget.add(row)
		if err != nil {
			return err
		}
		item.encodedBytes = encodedBytes
		heap.Push(c, item)
		return nil
	}
	if compareOrderedOneOffRows(item, c.rows[0], c.orderBy) < 0 {
		encodedBytes, err := c.budget.replace(c.rows[0].encodedBytes, row)
		if err != nil {
			return err
		}
		item.encodedBytes = encodedBytes
		c.rows[0] = item
		heap.Fix(c, 0)
	}
	return nil
}

func (c *orderedOneOffCollector) SortedRows() []orderedOneOffRow {
	slices.SortFunc(c.rows, func(a, b orderedOneOffRow) int {
		return compareOrderedOneOffRows(a, b, c.orderBy)
	})
	return c.rows
}

func compareOrderedOneOffRows(a, b orderedOneOffRow, orderBy []compiledSQLOrderBy) int {
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
	return cmp.Compare(a.ordinal, b.ordinal)
}

func evaluateOneOffSingleTableOrdered(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, orderBy []compiledSQLOrderBy, offset, limit, capacity int, projectRow func(types.ProductValue) types.ProductValue, budget *encodedResultBudget) ([]types.ProductValue, error) {
	collector := newOrderedOneOffCollector(orderBy, capacity, budget)
	var orderErr error
	_, err := visitOneOffSingleTableRows(ctx, view, tableID, pred, resolver, func(row types.ProductValue) bool {
		key := make([]types.Value, len(orderBy))
		for i, term := range orderBy {
			idx := term.Column.Schema.Index
			if idx < 0 || idx >= len(row) {
				orderErr = fmt.Errorf("ORDER BY column %q is missing from row", term.Column.Schema.Name)
				return false
			}
			key[i] = row[idx]
		}
		if err := collector.Add(projectRow(row), key); err != nil {
			orderErr = err
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if orderErr != nil {
		return nil, orderErr
	}
	return materializeOrderedOneOffRows(collector.SortedRows(), offset, limit), nil
}

func materializeOrderedOneOffRows(rows []orderedOneOffRow, offset int, limit int) []types.ProductValue {
	start, end := oneOffWindowBounds(len(rows), offset, limit)
	outLen := end - start
	out := make([]types.ProductValue, 0, outLen)
	for i := start; i < end; i++ {
		out = append(out, rows[i].row)
	}
	return out
}

func sliceOneOffRows(rows []types.ProductValue, offset int, limit int) []types.ProductValue {
	start, end := oneOffWindowBounds(len(rows), offset, limit)
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

func evaluateOneOffJoin(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver, offset, limit int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	// Trust Join.ProjectRight because a self-join has the same table ID on both
	// sides and only the compile-time signal disambiguates the projected side.
	collector := newOneOffResultCollector(offset, limit, budget)
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		if join.ProjectRight {
			return collector.Visit(rightRow)
		}
		return collector.Visit(leftRow)
	})
	if err != nil {
		return nil, err
	}
	return collector.Result()
}

func evaluateOneOffJoinOrdered(ctx context.Context, view store.CommittedReadView, join subscription.Join, orderBy []compiledSQLOrderBy, resolver schema.IndexResolver, offset, limit, capacity int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	return collectOrderedOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffJoinPairs(ctx, view, join, resolver, visit)
		},
		join.Left, join.LeftAlias, join.Right, join.RightAlias, nil, join.ProjectRight, orderBy, offset, limit, capacity, budget,
	)
}

func countOneOffJoin(ctx context.Context, view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver) (uint64, error) {
	var count uint64
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		count++
		return true
	})
	return count, err
}

func evaluateOneOffJoinProjection(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, resolver schema.IndexResolver, offset, limit int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	return collectOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffJoinPairs(ctx, view, join, resolver, visit)
		},
		join.Left, join.LeftAlias, join.Right, join.RightAlias, columns, offset, limit, budget,
	)
}

func evaluateOneOffJoinProjectionOrdered(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, resolver schema.IndexResolver, offset, limit, capacity int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	return collectOrderedOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffJoinPairs(ctx, view, join, resolver, visit)
		},
		join.Left, join.LeftAlias, join.Right, join.RightAlias, columns, join.ProjectRight, orderBy, offset, limit, capacity, budget,
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
				outerValue, ok := oneOffRowValue(outerRow, outerCol)
				if !ok {
					continue
				}
				key := store.NewIndexKey(outerValue)
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
		if _, ok := oneOffRowValue(outerRow, outerCol); !ok {
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
	leftIndex := int(join.LeftCol)
	if leftIndex < 0 || leftIndex >= len(leftRow) {
		return false
	}
	rightIndex := int(join.RightCol)
	if rightIndex < 0 || rightIndex >= len(rightRow) {
		return false
	}
	if !types.EqualValues(&leftRow[leftIndex], &rightRow[rightIndex]) {
		return false
	}
	if join.Filter != nil && !subscription.MatchJoinPair(join.Filter, join.Left, join.LeftAlias, leftRow, join.Right, join.RightAlias, rightRow) {
		return false
	}
	return true
}

func oneOffRowValue(row types.ProductValue, col types.ColID) (types.Value, bool) {
	idx := int(col)
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func evaluateOneOffCrossJoinProjection(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, columns []compiledSQLProjectionColumn, offset, limit int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	return collectOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffCrossJoinPairs(ctx, view, cross, true, visit)
		},
		cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns, offset, limit, budget,
	)
}

func evaluateOneOffCrossJoinProjectionOrdered(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy, offset, limit, capacity int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	return collectOrderedOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffCrossJoinPairs(ctx, view, cross, true, visit)
		},
		cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns, cross.ProjectRight, orderBy, offset, limit, capacity, budget,
	)
}

func evaluateOneOffCrossJoinOrdered(ctx context.Context, view store.CommittedReadView, cross subscription.CrossJoin, orderBy []compiledSQLOrderBy, offset, limit, capacity int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	return collectOrderedOneOffPairProjections(
		func(visit func(types.ProductValue, types.ProductValue) bool) error {
			return visitOneOffCrossJoinPairs(ctx, view, cross, true, visit)
		},
		cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, nil, cross.ProjectRight, orderBy, offset, limit, capacity, budget,
	)
}

func collectOneOffPairProjections(visitPairs func(func(types.ProductValue, types.ProductValue) bool) error, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, columns []compiledSQLProjectionColumn, offset, limit int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	collector := newOneOffResultCollector(offset, limit, budget)
	err := visitPairs(func(leftRow, rightRow types.ProductValue) bool {
		return collector.Visit(projectOneOffJoinPair(leftRow, rightRow, leftID, leftAlias, rightID, rightAlias, columns))
	})
	if err != nil {
		return nil, err
	}
	return collector.Result()
}

type orderedOneOffPair struct {
	leftRow  types.ProductValue
	rightRow types.ProductValue
}

type oneOffPairSource uint8

const (
	oneOffPairSourceInvalid oneOffPairSource = iota
	oneOffPairSourceLeft
	oneOffPairSourceRight
)

type orderedOneOffPairTerm struct {
	source     oneOffPairSource
	index      int
	columnName string
}

func (term orderedOneOffPairTerm) value(pair orderedOneOffPair) types.Value {
	if term.source == oneOffPairSourceLeft {
		return pair.leftRow[term.index]
	}
	return pair.rightRow[term.index]
}

func collectOrderedOneOffPairProjections(visitPairs func(func(types.ProductValue, types.ProductValue) bool) error, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, columns []compiledSQLProjectionColumn, projectRight bool, orderBy []compiledSQLOrderBy, offset, limit, capacity int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	collector := newOrderedOneOffCollector(orderBy, capacity, budget)
	var orderErr error
	orderTerms := compileOrderedOneOffPairTerms(leftID, leftAlias, rightID, rightAlias, orderBy)
	err := visitPairs(func(leftRow, rightRow types.ProductValue) bool {
		pair := orderedOneOffPair{leftRow: leftRow, rightRow: rightRow}
		if err := validateOrderedOneOffPairTerms(pair, orderTerms); err != nil {
			orderErr = err
			return false
		}
		key := make([]types.Value, len(orderTerms))
		for i, term := range orderTerms {
			key[i] = term.value(pair)
		}
		var row types.ProductValue
		if len(columns) != 0 {
			row = projectOneOffJoinPair(leftRow, rightRow, leftID, leftAlias, rightID, rightAlias, columns)
		} else if projectRight {
			row = rightRow
		} else {
			row = leftRow
		}
		if err := collector.Add(row, key); err != nil {
			orderErr = err
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if orderErr != nil {
		return nil, orderErr
	}
	return materializeOrderedOneOffRows(collector.SortedRows(), offset, limit), nil
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
	switch classifyOneOffPairColumnSource(col, leftID, leftAlias, rightID, rightAlias) {
	case oneOffPairSourceLeft:
		return leftRow, true
	case oneOffPairSourceRight:
		return rightRow, true
	default:
		return nil, false
	}
}

func classifyOneOffPairColumnSource(col compiledSQLProjectionColumn, leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8) oneOffPairSource {
	if leftID == rightID {
		switch {
		case col.Table == leftID && col.Alias == leftAlias:
			return oneOffPairSourceLeft
		case col.Table == rightID && col.Alias == rightAlias:
			return oneOffPairSourceRight
		default:
			return oneOffPairSourceInvalid
		}
	}
	switch col.Table {
	case leftID:
		return oneOffPairSourceLeft
	case rightID:
		return oneOffPairSourceRight
	default:
		return oneOffPairSourceInvalid
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

func compileOrderedOneOffPairTerms(leftID schema.TableID, leftAlias uint8, rightID schema.TableID, rightAlias uint8, orderBy []compiledSQLOrderBy) []orderedOneOffPairTerm {
	terms := make([]orderedOneOffPairTerm, len(orderBy))
	for i, orderTerm := range orderBy {
		column := orderTerm.Column
		terms[i] = orderedOneOffPairTerm{
			source:     classifyOneOffPairColumnSource(column, leftID, leftAlias, rightID, rightAlias),
			index:      column.Schema.Index,
			columnName: column.Schema.Name,
		}
	}
	return terms
}

func validateOrderedOneOffPairTerms(pair orderedOneOffPair, terms []orderedOneOffPairTerm) error {
	for _, term := range terms {
		var row types.ProductValue
		switch term.source {
		case oneOffPairSourceLeft:
			row = pair.leftRow
		case oneOffPairSourceRight:
			row = pair.rightRow
		default:
			return fmt.Errorf("ORDER BY column %q is not from the projected table", term.columnName)
		}
		if term.index < 0 || term.index >= len(row) {
			return fmt.Errorf("ORDER BY column %q is missing from row", term.columnName)
		}
	}
	return nil
}

func evaluateOneOffCrossJoin(ctx context.Context, view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoin, offset, limit int, budget *encodedResultBudget) ([]types.ProductValue, error) {
	if projectedTable != cross.ProjectedTable() {
		return nil, nil
	}
	if cross.Filter != nil {
		collector := newOneOffResultCollector(offset, limit, budget)
		err := visitOneOffCrossJoinPairs(ctx, view, cross, true, func(leftRow, rightRow types.ProductValue) bool {
			if cross.ProjectRight {
				return collector.Visit(rightRow)
			}
			return collector.Visit(leftRow)
		})
		if err != nil {
			return nil, err
		}
		return collector.Result()
	}
	otherTable := cross.Left
	if projectedTable == cross.Left {
		otherTable = cross.Right
	}
	otherCount := view.RowCount(otherTable)
	if otherCount == 0 {
		return nil, nil
	}
	collector := newOneOffResultCollector(offset, limit, budget)
	for _, row := range view.TableScan(projectedTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i := 0; i < otherCount; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !collector.Visit(row) {
				return collector.Result()
			}
		}
	}
	return collector.Result()
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
	return checkedCrossJoinRowCount(view.RowCount(projectedTable), view.RowCount(otherTable))
}

func checkedCrossJoinRowCount(projectedCount, otherCount int) (uint64, error) {
	if projectedCount < 0 || otherCount < 0 {
		return 0, fmt.Errorf("cross join row count is negative: %d * %d", projectedCount, otherCount)
	}
	projected := uint64(projectedCount)
	other := uint64(otherCount)
	if projected != 0 && other > math.MaxUint64/projected {
		return 0, fmt.Errorf("cross join row count overflow: %d * %d", projectedCount, otherCount)
	}
	return projected * other, nil
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
