package subscription

import "github.com/ponchione/shunter/types"

// EvalSingleTableDelta produces the per-subscription delta for a single-table
// predicate (SPEC-004 §6.1). Inserts and deletes from the changeset are
// filtered independently; no deduplication is required because a single-table
// scan cannot emit duplicates.
func EvalSingleTableDelta(dv *DeltaView, pred Predicate, table TableID) (inserts, deletes []types.ProductValue) {
	for _, row := range dv.InsertedRows(table) {
		if MatchRow(pred, table, row) {
			inserts = append(inserts, row)
		}
	}
	for _, row := range dv.DeletedRows(table) {
		if MatchRow(pred, table, row) {
			deletes = append(deletes, row)
		}
	}
	return inserts, deletes
}

// MatchRow reports whether pred matches the given row from the given table.
//
// Predicate leaves that reference a different table are treated as "no
// constraint on this row" (return true). This lets callers reuse MatchRow
// to evaluate a join's Filter against each side of a joined pair.
func MatchRow(pred Predicate, table TableID, row types.ProductValue) bool {
	if pred == nil {
		return true
	}
	switch p := pred.(type) {
	case ColEq:
		if p.Table != table {
			return true
		}
		if int(p.Column) >= len(row) {
			return false
		}
		return row[p.Column].Equal(p.Value)
	case ColRange:
		if p.Table != table {
			return true
		}
		if int(p.Column) >= len(row) {
			return false
		}
		return matchBounds(row[p.Column], p.Lower, p.Upper)
	case And:
		return MatchRow(p.Left, table, row) && MatchRow(p.Right, table, row)
	case AllRows:
		return true
	case Join:
		// A Join is a structural predicate, not a row-level filter.
		// Treat as pass; the join-delta evaluator handles it directly.
		return true
	}
	return false
}

// matchBounds reports whether v falls within [lower, upper].
func matchBounds(v Value, lower, upper Bound) bool {
	if !lower.Unbounded {
		c := v.Compare(lower.Value)
		if lower.Inclusive {
			if c < 0 {
				return false
			}
		} else {
			if c <= 0 {
				return false
			}
		}
	}
	if !upper.Unbounded {
		c := v.Compare(upper.Value)
		if upper.Inclusive {
			if c > 0 {
				return false
			}
		} else {
			if c >= 0 {
				return false
			}
		}
	}
	return true
}
