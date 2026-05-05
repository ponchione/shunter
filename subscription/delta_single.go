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

// MatchRow reports whether pred matches a row from the given table.
// Leaves for other tables are treated as no constraint.
func MatchRow(pred Predicate, table TableID, row types.ProductValue) bool {
	return MatchRowSide(pred, table, 0, row)
}

// MatchRowSide evaluates pred against one side of a possible self-join.
// Leaves for other tables or aliases are treated as no constraint.
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
	case ColEqCol:
		leftMatches := p.LeftTable == table && p.LeftAlias == sideAlias
		rightMatches := p.RightTable == table && p.RightAlias == sideAlias
		if !leftMatches && !rightMatches {
			return true
		}
		if leftMatches && rightMatches {
			if int(p.LeftColumn) >= len(row) || int(p.RightColumn) >= len(row) {
				return false
			}
			return row[p.LeftColumn].Equal(row[p.RightColumn])
		}
		return true
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

// MatchJoinPair evaluates a join filter against both relation rows at once.
// Single-side MatchRowSide intentionally treats leaves from the other table as
// pass-through so an AND filter can be evaluated side-by-side. That shortcut is
// not valid for OR: `(left.x = 1 OR right.y = 2)` must be evaluated as a
// boolean expression over the joined pair, not as two independent row filters.
func MatchJoinPair(pred Predicate, leftTable TableID, leftAlias uint8, leftRow types.ProductValue, rightTable TableID, rightAlias uint8, rightRow types.ProductValue) bool {
	if pred == nil {
		return true
	}
	switch p := pred.(type) {
	case ColEq:
		row, ok := joinPredicateRow(p.Table, p.Alias, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
		if !ok || int(p.Column) >= len(row) {
			return false
		}
		return row[p.Column].Equal(p.Value)
	case ColNe:
		row, ok := joinPredicateRow(p.Table, p.Alias, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
		if !ok || int(p.Column) >= len(row) {
			return false
		}
		return !row[p.Column].Equal(p.Value)
	case ColRange:
		row, ok := joinPredicateRow(p.Table, p.Alias, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
		if !ok || int(p.Column) >= len(row) {
			return false
		}
		return matchBounds(row[p.Column], p.Lower, p.Upper)
	case ColEqCol:
		left, ok := joinPredicateRow(p.LeftTable, p.LeftAlias, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
		if !ok || int(p.LeftColumn) >= len(left) {
			return false
		}
		right, ok := joinPredicateRow(p.RightTable, p.RightAlias, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
		if !ok || int(p.RightColumn) >= len(right) {
			return false
		}
		return left[p.LeftColumn].Equal(right[p.RightColumn])
	case And:
		return MatchJoinPair(p.Left, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow) &&
			MatchJoinPair(p.Right, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
	case Or:
		return MatchJoinPair(p.Left, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow) ||
			MatchJoinPair(p.Right, leftTable, leftAlias, leftRow, rightTable, rightAlias, rightRow)
	case AllRows:
		return true
	case NoRows:
		return false
	case Join:
		return true
	case CrossJoin:
		return true
	}
	return false
}

func joinPredicateRow(table TableID, alias uint8, leftTable TableID, leftAlias uint8, leftRow types.ProductValue, rightTable TableID, rightAlias uint8, rightRow types.ProductValue) (types.ProductValue, bool) {
	if leftTable == rightTable {
		if table != leftTable {
			return nil, false
		}
		switch alias {
		case leftAlias:
			return leftRow, true
		case rightAlias:
			return rightRow, true
		default:
			return nil, false
		}
	}
	switch table {
	case leftTable:
		return leftRow, true
	case rightTable:
		return rightRow, true
	default:
		return nil, false
	}
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
