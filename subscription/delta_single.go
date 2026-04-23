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
//
// Callers outside a self-join context use this form: side alias defaults to
// zero and filter leaves default to zero, so the alias comparison in
// MatchRowSide is trivially true.
func MatchRow(pred Predicate, table TableID, row types.ProductValue) bool {
	return MatchRowSide(pred, table, 0, row)
}

// MatchRowSide evaluates pred against a row coming from (table, sideAlias).
// The sideAlias distinguishes two relation instances that share the same
// TableID in a self-join: tryJoinFilter passes Join.LeftAlias for the
// left-side row and Join.RightAlias for the right-side row. Filter leaves
// whose Alias field does not match sideAlias are treated as "no constraint
// on this row", mirroring the existing cross-table pass-through for the
// Table field.
func MatchRowSide(pred Predicate, table TableID, sideAlias uint8, row types.ProductValue) bool {
	if pred == nil {
		return true
	}
	switch p := pred.(type) {
	case ColEq:
		if p.Table != table {
			return true
		}
		if p.Alias != sideAlias {
			return true
		}
		if int(p.Column) >= len(row) {
			return false
		}
		return row[p.Column].Equal(p.Value)
	case ColNe:
		if p.Table != table {
			return true
		}
		if p.Alias != sideAlias {
			return true
		}
		if int(p.Column) >= len(row) {
			return false
		}
		return !row[p.Column].Equal(p.Value)
	case ColRange:
		if p.Table != table {
			return true
		}
		if p.Alias != sideAlias {
			return true
		}
		if int(p.Column) >= len(row) {
			return false
		}
		return matchBounds(row[p.Column], p.Lower, p.Upper)
	case And:
		return MatchRowSide(p.Left, table, sideAlias, row) && MatchRowSide(p.Right, table, sideAlias, row)
	case Or:
		return MatchRowSide(p.Left, table, sideAlias, row) || MatchRowSide(p.Right, table, sideAlias, row)
	case AllRows:
		return true
	case NoRows:
		return false
	case Join:
		// A Join is a structural predicate, not a row-level filter.
		// Treat as pass; the join-delta evaluator handles it directly.
		return true
	case CrossJoin:
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
