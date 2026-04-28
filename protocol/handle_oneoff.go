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
// The wire carries a SQL string (Phase 2 Slice 1) which is parsed and
// coerced against the schema before the existing snapshot-scan path
// runs.
func handleOneOffQuery(
	ctx context.Context,
	conn *Conn,
	msg *OneOffQueryMsg,
	stateAccess CommittedStateAccess,
	sl SchemaLookup,
) {
	receipt := time.Now()
	compiled, err := compileSQLQueryString(msg.QueryString, sl, &conn.Identity, true, true)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return
	}

	tableID, _, ok := sl.TableByName(compiled.TableName)
	if !ok {
		sendOneOffError(conn, msg.MessageID, fmt.Sprintf("no such table: `%s`. If the table exists, it may be marked private.", compiled.TableName), receipt)
		return
	}

	pred := compiled.Predicate
	if err := subscription.ValidateQueryPredicate(pred, sl); err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return
	}

	view := stateAccess.Snapshot()
	resolver, _ := sl.(schema.IndexResolver)
	rowLimit := oneOffRowLimit(compiled.Limit)
	var matchedRows []types.ProductValue
	var encodedRows []types.ProductValue
	rowsAlreadyProjected := false
	if compiled.Aggregate != nil {
		// Aggregate shape happens over the full matched input; LIMIT then
		// constrains the one-row aggregate output (reference ProjectList::Limit
		// wraps ProjectList::Agg). LIMIT 0 drops the aggregate row entirely;
		// LIMIT >= 1 keeps the single count row.
		matchedCount := countOneOffMatches(view, tableID, pred, resolver)
		if compiled.Limit == nil || *compiled.Limit > 0 {
			encodedRows = []types.ProductValue{{types.NewUint64(matchedCount)}}
		}
	} else if rowLimit != 0 {
		if joinPred, ok := pred.(subscription.Join); ok {
			if len(compiled.ProjectionColumns) != 0 {
				matchedRows = evaluateOneOffJoinProjection(view, joinPred, compiled.ProjectionColumns, resolver, rowLimit)
				rowsAlreadyProjected = true
			} else {
				matchedRows = evaluateOneOffJoin(view, tableID, joinPred, resolver, rowLimit)
			}
		} else if crossPred, ok := pred.(subscription.CrossJoin); ok {
			if len(compiled.ProjectionColumns) != 0 {
				matchedRows = evaluateOneOffCrossJoinProjection(view, crossPred, compiled.ProjectionColumns, rowLimit)
				rowsAlreadyProjected = true
			} else {
				matchedRows = evaluateOneOffCrossJoin(view, tableID, crossPred, rowLimit)
			}
		} else {
			for _, pv := range view.TableScan(tableID) {
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
	view.Close()
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

func countOneOffMatches(view store.CommittedReadView, tableID schema.TableID, pred subscription.Predicate, resolver schema.IndexResolver) uint64 {
	if joinPred, ok := pred.(subscription.Join); ok {
		return countOneOffJoin(view, tableID, joinPred, resolver)
	}
	if crossPred, ok := pred.(subscription.CrossJoin); ok {
		return countOneOffCrossJoin(view, tableID, crossPred)
	}
	var count uint64
	for _, pv := range view.TableScan(tableID) {
		if subscription.MatchRow(pred, tableID, pv) {
			count++
		}
	}
	return count
}

func evaluateOneOffJoin(view store.CommittedReadView, projectedTable schema.TableID, join subscription.Join, resolver schema.IndexResolver, limit int) []types.ProductValue {
	// Trust Join.ProjectRight (compile-time signal) over projectedTable
	// equality, because a self-join has Left == Right == projectedTable on
	// both sides and only the boolean disambiguates.
	_ = projectedTable
	var rows []types.ProductValue
	visitOneOffJoinPairs(view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		if join.ProjectRight {
			rows = append(rows, rightRow)
		} else {
			rows = append(rows, leftRow)
		}
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows
}

func countOneOffJoin(view store.CommittedReadView, projectedTable schema.TableID, join subscription.Join, resolver schema.IndexResolver) uint64 {
	_ = projectedTable
	var count uint64
	visitOneOffJoinPairs(view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		count++
		return true
	})
	return count
}

func evaluateOneOffJoinProjection(view store.CommittedReadView, join subscription.Join, columns []compiledSQLProjectionColumn, resolver schema.IndexResolver, limit int) []types.ProductValue {
	var rows []types.ProductValue
	visitOneOffJoinPairs(view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias, columns))
		return !oneOffLimitReached(len(rows), limit)
	})
	return rows
}

func visitOneOffJoinPairs(view store.CommittedReadView, join subscription.Join, resolver schema.IndexResolver, visit func(leftRow, rightRow types.ProductValue) bool) {
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
					if int(join.RightCol) >= len(rightRow) {
						continue
					}
					key := store.NewIndexKey(rightRow[join.RightCol])
					for _, rid := range view.IndexSeek(join.Left, leftIdx, key) {
						leftRow, ok := view.GetRow(join.Left, rid)
						if !ok {
							continue
						}
						if !visitIfMatch(leftRow, rightRow) {
							return
						}
					}
				}
				return
			}
		}
		for _, rightRow := range view.TableScan(join.Right) {
			if int(join.RightCol) >= len(rightRow) {
				continue
			}
			for _, leftRow := range view.TableScan(join.Left) {
				if !visitIfMatch(leftRow, rightRow) {
					return
				}
			}
		}
		return
	}

	if resolver != nil {
		if rightIdx, ok := resolver.IndexIDForColumn(join.Right, join.RightCol); ok {
			for _, leftRow := range view.TableScan(join.Left) {
				if int(join.LeftCol) >= len(leftRow) {
					continue
				}
				key := store.NewIndexKey(leftRow[join.LeftCol])
				for _, rid := range view.IndexSeek(join.Right, rightIdx, key) {
					rightRow, ok := view.GetRow(join.Right, rid)
					if !ok {
						continue
					}
					if !visitIfMatch(leftRow, rightRow) {
						return
					}
				}
			}
			return
		}
	}
	for _, leftRow := range view.TableScan(join.Left) {
		if int(join.LeftCol) >= len(leftRow) {
			continue
		}
		for _, rightRow := range view.TableScan(join.Right) {
			if !visitIfMatch(leftRow, rightRow) {
				return
			}
		}
	}
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

func evaluateOneOffCrossJoinProjection(view store.CommittedReadView, cross subscription.CrossJoin, columns []compiledSQLProjectionColumn, limit int) []types.ProductValue {
	var rows []types.ProductValue
	if cross.ProjectRight {
		for _, rightRow := range view.TableScan(cross.Right) {
			for _, leftRow := range view.TableScan(cross.Left) {
				rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns))
				if oneOffLimitReached(len(rows), limit) {
					return rows
				}
			}
		}
		return rows
	}
	for _, leftRow := range view.TableScan(cross.Left) {
		for _, rightRow := range view.TableScan(cross.Right) {
			rows = append(rows, projectOneOffJoinPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, columns))
			if oneOffLimitReached(len(rows), limit) {
				return rows
			}
		}
	}
	return rows
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

func evaluateOneOffCrossJoin(view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoin, limit int) []types.ProductValue {
	if projectedTable != cross.ProjectedTable() {
		return nil
	}
	otherTable := cross.Left
	if projectedTable == cross.Left {
		otherTable = cross.Right
	}
	otherCount := view.RowCount(otherTable)
	if otherCount == 0 {
		return nil
	}
	var rows []types.ProductValue
	for _, row := range view.TableScan(projectedTable) {
		for i := 0; i < otherCount; i++ {
			rows = append(rows, row)
			if oneOffLimitReached(len(rows), limit) {
				return rows
			}
		}
	}
	return rows
}

func countOneOffCrossJoin(view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoin) uint64 {
	if projectedTable != cross.ProjectedTable() {
		return 0
	}
	otherTable := cross.Left
	if projectedTable == cross.Left {
		otherTable = cross.Right
	}
	return uint64(view.RowCount(projectedTable)) * uint64(view.RowCount(otherTable))
}
