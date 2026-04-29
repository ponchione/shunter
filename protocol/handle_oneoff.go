package protocol

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/ponchione/shunter/bsatn"
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

// handleOneOffQuery executes a one-off table scan with optional
// comparison predicates against committed state and sends the result
// back to the client (SPEC-005 §7.4).
//
// The wire carries a SQL string (SQL-string) which is parsed and
// coerced against the schema before the existing snapshot-scan path
// runs.
func handleOneOffQuery(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	receipt := time.Now()
	readSL := authorizedSchemaLookupForConn(sl, conn)
	compiled, err := compileSQLQueryString(msg.QueryString, readSL, &conn.Identity, true, true)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return
	}

	tableID, _, ok := readSL.TableByName(compiled.TableName)
	if !ok {
		sendOneOffError(conn, msg.MessageID, fmt.Sprintf("no such table: `%s`. If the table exists, it may be marked private.", compiled.TableName), receipt)
		return
	}

	pred := compiled.Predicate
	if err := subscription.ValidateQueryPredicate(pred, readSL); err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return
	}
	if err := ctx.Err(); err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return
	}

	view := stateAccess.Snapshot()
	viewClosed := false
	closeView := func() {
		if !viewClosed {
			view.Close()
			viewClosed = true
		}
	}
	defer closeView()
	failRead := func(err error) bool {
		if err == nil {
			return false
		}
		closeView()
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return true
	}
	resolver, _ := readSL.(schema.IndexResolver)
	rowLimit := oneOffRowLimit(compiled.Limit)
	var matchedRows []types.ProductValue
	var encodedRows []types.ProductValue
	rowsAlreadyProjected := false
	if compiled.Aggregate != nil {
		// Aggregate shape happens over the full matched input; LIMIT then
		// constrains the one-row aggregate output (reference ProjectList::Limit
		// wraps ProjectList::Agg). LIMIT 0 drops the aggregate row entirely;
		// LIMIT >= 1 keeps the single count row.
		matchedCount, err := countOneOffMatches(ctx, view, tableID, pred, resolver)
		if failRead(err) {
			return
		}
		if compiled.Limit == nil || *compiled.Limit > 0 {
			encodedRows = []types.ProductValue{{types.NewUint64(matchedCount)}}
		}
	} else if rowLimit != 0 {
		if joinPred, ok := pred.(subscription.Join); ok {
			if len(compiled.ProjectionColumns) != 0 {
				matchedRows, err = evaluateOneOffJoinProjection(ctx, view, joinPred, compiled.ProjectionColumns, resolver, rowLimit)
				if failRead(err) {
					return
				}
				rowsAlreadyProjected = true
			} else {
				matchedRows, err = evaluateOneOffJoin(ctx, view, tableID, joinPred, resolver, rowLimit)
				if failRead(err) {
					return
				}
			}
		} else if crossPred, ok := pred.(subscription.CrossJoin); ok {
			if len(compiled.ProjectionColumns) != 0 {
				matchedRows, err = evaluateOneOffCrossJoinProjection(ctx, view, crossPred, compiled.ProjectionColumns, rowLimit)
				if failRead(err) {
					return
				}
				rowsAlreadyProjected = true
			} else {
				matchedRows, err = evaluateOneOffCrossJoin(ctx, view, tableID, crossPred, rowLimit)
				if failRead(err) {
					return
				}
			}
		} else {
			for _, pv := range view.TableScan(tableID) {
				if failRead(ctx.Err()) {
					return
				}
				if subscription.MatchRow(pred, tableID, pv) {
					matchedRows = append(matchedRows, pv)
					if oneOffLimitReached(len(matchedRows), rowLimit) {
						break
					}
				}
			}
		}
		if len(compiled.ProjectionColumns) != 0 && !rowsAlreadyProjected {
			encodedRows = projectOneOffRows(matchedRows, compiled.ProjectionColumns)
		} else {
			encodedRows = matchedRows
		}
	}
	closeView()
	var rows [][]byte
	for _, pv := range encodedRows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, pv); err != nil {
			sendOneOffError(conn, msg.MessageID, "encode error: "+err.Error(), receipt)
			return
		}
		rows = append(rows, buf.Bytes())
	}

	encoded := EncodeRowList(rows)
	sendError(conn, OneOffQueryResponse{
		MessageID: msg.MessageID,
		Tables: []OneOffTable{{
			TableName: compiled.TableName,
			Rows:      encoded,
		}},
		TotalHostExecutionDuration: elapsedMicrosI64(receipt),
	})
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

func oneOffLimitReached(count int, limit int) bool {
	return limit >= 0 && count >= limit
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

func evaluateOneOffJoinProjection(ctx context.Context, view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, resolver schema.IndexResolver, limit int) ([]types.ProductValue, error) {
	var rows []types.ProductValue
	err := visitOneOffJoinPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias, columns))
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows, err
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
	if cross.ProjectRight {
		for _, rightRow := range view.TableScan(cross.Right) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, leftRow := range view.TableScan(cross.Left) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns))
				if oneOffLimitReached(len(rows), limit) {
					return rows, nil
				}
			}
		}
		return rows, nil
	}
	for _, leftRow := range view.TableScan(cross.Left) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, rightRow := range view.TableScan(cross.Right) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns))
			if oneOffLimitReached(len(rows), limit) {
				return rows, nil
			}
		}
	}
	return rows, nil
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

func evaluateOneOffCrossJoin(ctx context.Context, view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoin, limit int) ([]types.ProductValue, error) {
	if projectedTable != cross.ProjectedTable() {
		return nil, nil
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
	otherTable := cross.Left
	if projectedTable == cross.Left {
		otherTable = cross.Right
	}
	return uint64(view.RowCount(projectedTable)) * uint64(view.RowCount(otherTable)), nil
}
