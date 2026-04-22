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

// colMatcher pairs a column index with the value to match against.
type colMatcher struct {
	colIdx int
	value  types.Value
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
	compiled, err := compileSQLQueryString(msg.QueryString, sl, &conn.Identity)
	if err != nil {
		sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
		return
	}

	tableID, _, ok := sl.TableByName(compiled.TableName)
	if !ok {
		sendOneOffError(conn, msg.MessageID, fmt.Sprintf("unknown table %q", compiled.TableName), receipt)
		return
	}

	pred := compiled.Predicate

	view := stateAccess.Snapshot()
	var matchedRows []types.ProductValue
	if joinPred, ok := pred.(subscription.Join); ok {
		matchedRows = evaluateOneOffJoin(view, tableID, joinPred)
	} else if crossPred, ok := pred.(subscription.CrossJoinProjected); ok {
		matchedRows = evaluateOneOffCrossJoin(view, tableID, crossPred)
	} else {
		for _, pv := range view.TableScan(tableID) {
			if subscription.MatchRow(pred, tableID, pv) {
				matchedRows = append(matchedRows, pv)
			}
		}
	}
	var rows [][]byte
	for _, pv := range matchedRows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, pv); err != nil {
			view.Close()
			sendOneOffError(conn, msg.MessageID, "encode error: "+err.Error(), receipt)
			return
		}
		rows = append(rows, buf.Bytes())
	}
	view.Close()

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

// matchesAll returns true when pv satisfies every predicate.
func matchesAll(pv types.ProductValue, matchers []colMatcher) bool {
	for _, m := range matchers {
		if m.colIdx >= len(pv) {
			return false
		}
		if !pv[m.colIdx].Equal(m.value) {
			return false
		}
	}
	return true
}

func evaluateOneOffJoin(view store.CommittedReadView, projectedTable schema.TableID, join subscription.Join) []types.ProductValue {
	// Trust Join.ProjectRight (compile-time signal) over projectedTable
	// equality, because a self-join has Left == Right == projectedTable on
	// both sides and only the boolean disambiguates.
	projectLeft := !join.ProjectRight
	var projectedJoinCol, otherJoinCol types.ColID
	var otherTable schema.TableID
	var scanTable schema.TableID
	if projectLeft {
		projectedJoinCol = join.LeftCol
		otherJoinCol = join.RightCol
		otherTable = join.Right
		scanTable = join.Left
	} else {
		projectedJoinCol = join.RightCol
		otherJoinCol = join.LeftCol
		otherTable = join.Left
		scanTable = join.Right
	}
	_ = projectedTable
	var rows []types.ProductValue
	for _, projectedRow := range view.TableScan(scanTable) {
		if int(projectedJoinCol) >= len(projectedRow) {
			continue
		}
		matched := false
		for _, otherRow := range view.TableScan(otherTable) {
			if int(otherJoinCol) >= len(otherRow) {
				continue
			}
			if !projectedRow[projectedJoinCol].Equal(otherRow[otherJoinCol]) {
				continue
			}
			if join.Filter != nil {
				var leftRow, rightRow types.ProductValue
				if projectLeft {
					leftRow, rightRow = projectedRow, otherRow
				} else {
					leftRow, rightRow = otherRow, projectedRow
				}
				if !subscription.MatchRowSide(join.Filter, join.Left, join.LeftAlias, leftRow) ||
					!subscription.MatchRowSide(join.Filter, join.Right, join.RightAlias, rightRow) {
					continue
				}
			}
			matched = true
			break
		}
		if matched {
			rows = append(rows, projectedRow)
		}
	}
	return rows
}

func evaluateOneOffCrossJoin(view store.CommittedReadView, projectedTable schema.TableID, cross subscription.CrossJoinProjected) []types.ProductValue {
	if projectedTable != cross.Projected {
		return nil
	}
	if view.RowCount(cross.Other) == 0 {
		return nil
	}
	var rows []types.ProductValue
	for _, row := range view.TableScan(projectedTable) {
		rows = append(rows, row)
	}
	return rows
}
