package protocol

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/store"
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
// equality predicates against committed state and sends the result
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
	q, err := parseQueryString(msg.QueryString, sl)
	if err != nil {
		sendError(conn, OneOffQueryResult{
			RequestID: msg.RequestID,
			Status:    1,
			Error:     err.Error(),
		})
		return
	}

	tableID, ts, ok := sl.TableByName(q.TableName)
	if !ok {
		sendError(conn, OneOffQueryResult{
			RequestID: msg.RequestID,
			Status:    1,
			Error:     fmt.Sprintf("unknown table %q", q.TableName),
		})
		return
	}

	matchers := make([]colMatcher, 0, len(q.Predicates))
	for _, p := range q.Predicates {
		col, _ := ts.Column(p.Column)
		matchers = append(matchers, colMatcher{colIdx: col.Index, value: p.Value})
	}

	view := stateAccess.Snapshot()
	var rows [][]byte
	for _, pv := range view.TableScan(tableID) {
		if matchesAll(pv, matchers) {
			var buf bytes.Buffer
			if err := bsatn.EncodeProductValue(&buf, pv); err != nil {
				view.Close()
				sendError(conn, OneOffQueryResult{
					RequestID: msg.RequestID,
					Status:    1,
					Error:     "encode error: " + err.Error(),
				})
				return
			}
			rows = append(rows, buf.Bytes())
		}
	}
	view.Close()

	encoded := EncodeRowList(rows)
	sendError(conn, OneOffQueryResult{
		RequestID: msg.RequestID,
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
