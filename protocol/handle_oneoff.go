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
	if query.Aggregate != nil {
		// Aggregate shape happens over the full matched input; OFFSET/LIMIT then
		// slice the one-row aggregate output (reference ProjectList::Limit wraps
		// ProjectList::Agg). LIMIT 0 or OFFSET >= 1 drops the aggregate row.
		matchedCount, err := countOneOffAggregate(ctx, view, tableID, pred, resolver, query.Aggregate)
		if err != nil {
			return SQLQueryResult{}, err
		}
		encodedRows = sliceOneOffRows([]types.ProductValue{{types.NewUint64(matchedCount)}}, rowOffset, rowLimit)
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
			for _, pv := range view.TableScan(tableID) {
				if err := ctx.Err(); err != nil {
					return SQLQueryResult{}, err
				}
				if subscription.MatchRow(pred, tableID, pv) {
					matchedRows = append(matchedRows, pv)
					if oneOffLimitReached(len(matchedRows), scanLimit) {
						break
					}
				}
			}
		}
		if len(query.OrderBy) != 0 && !rowsAlreadyProjected {
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
	for _, pv := range view.TableScan(tableID) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if subscription.MatchRow(pred, tableID, pv) {
			count++
		}
	}
	return count, nil
}

func countOneOffAggregate(ctx context.Context, view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (uint64, error) {
	if aggregate == nil || aggregate.Argument == nil {
		return countOneOffMatches(ctx, view, tableID, pred, resolver)
	}
	argument := *aggregate.Argument
	if joinPred, ok := pred.(subscription.Join); ok {
		return countOneOffJoinColumn(ctx, view, joinPred, resolver, argument)
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		return countOneOffCrossJoinColumn(ctx, view, crossPred, argument)
	}
	var count uint64
	for _, pv := range view.TableScan(tableID) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if subscription.MatchRow(pred, tableID, pv) && oneOffAggregateRowColumnPresent(pv, tableID, argument) {
			count++
		}
	}
	return count, nil
}

func oneOffAggregateRowColumnPresent(row types.ProductValue, tableID schema.TableID, column compiledSQLProjectionColumn) bool {
	if column.Table != tableID {
		return false
	}
	idx := column.Schema.Index
	return idx >= 0 && idx < len(row)
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
	source, ok := projectedJoinColumnSource(column, leftID, leftAlias, leftRow, rightID, rightAlias, rightRow)
	if !ok {
		return false
	}
	idx := column.Schema.Index
	return idx >= 0 && idx < len(source)
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
		err := visitOneOffCrossJoinPairs(ctx, view, cross, false, func(leftRow, rightRow types.ProductValue) bool {
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
