package protocol

import (
	"bytes"
	"context"
	"fmt"

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
	compiled, err := compileSQLQueryString(msg.QueryString, sl)
	if err != nil {
		sendError(conn, OneOffQueryResult{
			MessageID: msg.MessageID,
			Status:    1,
			Error:     err.Error(),
		})
		return
	}

	tableID, _, ok := sl.TableByName(compiled.TableName)
	if !ok {
		sendError(conn, OneOffQueryResult{
			MessageID: msg.MessageID,
			Status:    1,
			Error:     fmt.Sprintf("unknown table %q", compiled.TableName),
		})
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
			sendError(conn, OneOffQueryResult{
				MessageID: msg.MessageID,
				Status:    1,
				Error:     "encode error: " + err.Error(),
			})
			return
		}
		rows = append(rows, buf.Bytes())
	}
	view.Close()

	encoded := EncodeRowList(rows)
	sendError(conn, OneOffQueryResult{
		MessageID: msg.MessageID,
		Status:    0,
		Rows:      encoded,
	})
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
	projectLeft := projectedTable == join.Left
	var projectedJoinCol, otherJoinCol types.ColID
	var otherTable schema.TableID
	if projectLeft {
		projectedJoinCol = join.LeftCol
		otherJoinCol = join.RightCol
		otherTable = join.Right
	} else {
		projectedJoinCol = join.RightCol
		otherJoinCol = join.LeftCol
		otherTable = join.Left
	}
	var rows []types.ProductValue
	for _, projectedRow := range view.TableScan(projectedTable) {
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
