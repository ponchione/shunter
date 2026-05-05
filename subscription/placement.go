package subscription

import (
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// PruningIndexes composes the three pruning tiers (SPEC-004 §5).
type PruningIndexes struct {
	Value    *ValueIndex
	Range    *RangeIndex
	JoinEdge *JoinEdgeIndex
	Table    *TableIndex
}

// NewPruningIndexes constructs an empty composite.
func NewPruningIndexes() *PruningIndexes {
	return &PruningIndexes{
		Value:    NewValueIndex(),
		Range:    NewRangeIndex(),
		JoinEdge: NewJoinEdgeIndex(),
		Table:    NewTableIndex(),
	}
}

// TestOnlyIsEmpty reports whether every underlying tier is devoid of
// placement entries. Used by unwind tests to assert no stale index rows
// survive a failed RegisterSet. Not part of the production contract.
func (p *PruningIndexes) TestOnlyIsEmpty() bool {
	if p == nil {
		return true
	}
	if len(p.Value.args) != 0 || len(p.Value.cols) != 0 {
		return false
	}
	if len(p.Range.ranges) != 0 || len(p.Range.cols) != 0 {
		return false
	}
	if len(p.JoinEdge.edges) != 0 || len(p.JoinEdge.byTable) != 0 {
		return false
	}
	if len(p.Table.tables) != 0 {
		return false
	}
	return true
}

// PlaceSubscription routes each (query, table) pair to one pruning tier.
// Self-joins use table-level placement because leaves apply to one side only.
func PlaceSubscription(idx *PruningIndexes, pred Predicate, hash QueryHash) {
	mutateSubscriptionPlacement(idx, pred, hash, true, nil)
}

// RemoveSubscription reverses PlaceSubscription.
func RemoveSubscription(idx *PruningIndexes, pred Predicate, hash QueryHash) {
	mutateSubscriptionPlacement(idx, pred, hash, false, nil)
}

func placeSubscriptionForResolver(idx *PruningIndexes, pred Predicate, hash QueryHash, resolver IndexResolver) {
	mutateSubscriptionPlacement(idx, pred, hash, true, resolver)
}

func removeSubscriptionForResolver(idx *PruningIndexes, pred Predicate, hash QueryHash, resolver IndexResolver) {
	mutateSubscriptionPlacement(idx, pred, hash, false, resolver)
}

func mutateSubscriptionPlacement(idx *PruningIndexes, pred Predicate, hash QueryHash, add bool, resolver IndexResolver) {
	if j, ok := pred.(Join); ok && j.Left == j.Right {
		mutateTablePlacement(idx, j.Left, hash, add)
		return
	}
	join := findJoin(pred)
	for _, t := range pred.Tables() {
		colEqs := findColEqs(pred, t)
		if len(colEqs) > 0 {
			for _, ce := range colEqs {
				mutateValuePlacement(idx, t, ce.Column, ce.Value, hash, add)
			}
			continue
		}
		colRanges := findColRanges(pred, t)
		if len(colRanges) > 0 {
			for _, cr := range colRanges {
				mutateRangePlacement(idx, t, cr.Column, cr.Lower, cr.Upper, hash, add)
			}
			continue
		}
		if join != nil {
			if placements := joinEdgesFor(pred, join, t, resolver); len(placements) > 0 {
				for _, placement := range placements {
					mutateJoinEdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				continue
			}
		}
		mutateTablePlacement(idx, t, hash, add)
	}
}

func mutateValuePlacement(idx *PruningIndexes, table TableID, col ColID, value Value, hash QueryHash, add bool) {
	if add {
		idx.Value.Add(table, col, value, hash)
		return
	}
	idx.Value.Remove(table, col, value, hash)
}

func mutateRangePlacement(idx *PruningIndexes, table TableID, col ColID, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.Range.Add(table, col, lower, upper, hash)
		return
	}
	idx.Range.Remove(table, col, lower, upper, hash)
}

func mutateJoinEdgePlacement(idx *PruningIndexes, edge JoinEdge, value Value, hash QueryHash, add bool) {
	if add {
		idx.JoinEdge.Add(edge, value, hash)
		return
	}
	idx.JoinEdge.Remove(edge, value, hash)
}

func mutateTablePlacement(idx *PruningIndexes, table TableID, hash QueryHash, add bool) {
	if add {
		idx.Table.Add(table, hash)
		return
	}
	idx.Table.Remove(table, hash)
}

// CollectCandidatesForTable returns the set of candidate query hashes for a
// single changed table. Consults all three tiers and unions the results.
//
// The resolver is optional — when nil, Tier 2 lookups are skipped (useful in
// tests that only exercise Tier 1 and Tier 3).
func CollectCandidatesForTable(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
) []QueryHash {
	st := acquireCandidateScratch()
	defer releaseCandidateScratch(st)
	return collectCandidatesForTableInto(idx, table, rows, committed, resolver, st.candidates)
}

func collectCandidatesForTableInto(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	set map[QueryHash]struct{},
) []QueryHash {
	for h := range set {
		delete(set, h)
	}

	// Tier 1: equality-indexed subscriptions.
	idx.Value.ForEachTrackedColumn(table, func(col ColID) {
		forEachRowColumnValue(rows, col, func(v Value) {
			idx.Value.ForEachHash(table, col, v, func(h QueryHash) {
				set[h] = struct{}{}
			})
		})
	})

	// Tier 1b: range-indexed subscriptions.
	idx.Range.ForEachTrackedColumn(table, func(col ColID) {
		forEachRowColumnValue(rows, col, func(v Value) {
			idx.Range.ForEachHash(table, col, v, func(h QueryHash) {
				set[h] = struct{}{}
			})
		})
	})

	// Tier 2: join edges where this table is the LHS side.
	if committed != nil && resolver != nil {
		idx.JoinEdge.ForEachEdge(table, func(edge JoinEdge) {
			rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
			if !ok {
				return
			}
			for _, row := range rows {
				if int(edge.LHSJoinCol) >= len(row) {
					continue
				}
				joinVal := row[edge.LHSJoinCol]
				key := store.NewIndexKey(joinVal)
				rowIDs := committed.IndexSeek(edge.RHSTable, rhsIdx, key)
				for _, rid := range rowIDs {
					rhsRow, ok := committed.GetRow(edge.RHSTable, rid)
					if !ok {
						continue
					}
					if int(edge.RHSFilterCol) >= len(rhsRow) {
						continue
					}
					idx.JoinEdge.ForEachHash(edge, rhsRow[edge.RHSFilterCol], func(h QueryHash) {
						set[h] = struct{}{}
					})
				}
			}
		})
	}

	// Tier 3: table fallback.
	idx.Table.ForEachHash(table, func(h QueryHash) {
		set[h] = struct{}{}
	})

	out := make([]QueryHash, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	return out
}

// findColRanges returns ColRange predicates whose bounds cover every matching
// row for table t. Range placement follows the same safety rule as equality
// placement: AND may use any required child constraint; OR must constrain all
// branches or fall back to a broader tier.
func findColRanges(pred Predicate, t TableID) []ColRange {
	out, ok := requiredColRanges(pred, t)
	if !ok {
		return nil
	}
	return out
}

func requiredColRanges(pred Predicate, t TableID) ([]ColRange, bool) {
	switch p := pred.(type) {
	case ColRange:
		if p.Table == t && rangeHasBound(p) {
			return []ColRange{p}, true
		}
		return nil, false
	case And:
		left, leftOK := requiredColRanges(p.Left, t)
		right, rightOK := requiredColRanges(p.Right, t)
		switch {
		case leftOK && rightOK:
			return append(left, right...), true
		case leftOK:
			return left, true
		case rightOK:
			return right, true
		default:
			return nil, false
		}
	case Or:
		left, leftOK := requiredColRanges(p.Left, t)
		right, rightOK := requiredColRanges(p.Right, t)
		if !leftOK || !rightOK {
			return nil, false
		}
		return append(left, right...), true
	case Join:
		if p.Filter != nil {
			return requiredColRanges(p.Filter, t)
		}
	}
	return nil, false
}

func rangeHasBound(p ColRange) bool {
	return !p.Lower.Unbounded || !p.Upper.Unbounded
}

// findColEqs returns ColEq predicates whose values cover every matching row
// for table t. Equality placement is safe through AND when any child
// constrains t, and through OR only when every branch constrains t; otherwise
// callers must fall back to a broader tier.
func findColEqs(pred Predicate, t TableID) []ColEq {
	out, ok := requiredColEqs(pred, t)
	if !ok {
		return nil
	}
	return out
}

func requiredColEqs(pred Predicate, t TableID) ([]ColEq, bool) {
	switch p := pred.(type) {
	case ColEq:
		if p.Table == t {
			return []ColEq{p}, true
		}
		return nil, false
	case And:
		left, leftOK := requiredColEqs(p.Left, t)
		right, rightOK := requiredColEqs(p.Right, t)
		switch {
		case leftOK && rightOK:
			return append(left, right...), true
		case leftOK:
			return left, true
		case rightOK:
			return right, true
		default:
			return nil, false
		}
	case Or:
		left, leftOK := requiredColEqs(p.Left, t)
		right, rightOK := requiredColEqs(p.Right, t)
		if !leftOK || !rightOK {
			return nil, false
		}
		return append(left, right...), true
	case Join:
		if p.Filter != nil {
			return requiredColEqs(p.Filter, t)
		}
	}
	return nil, false
}

// findJoin returns the first Join in the tree, or nil if there is none.
func findJoin(pred Predicate) *Join {
	switch p := pred.(type) {
	case Join:
		return &p
	case And:
		if j := findJoin(p.Left); j != nil {
			return j
		}
		return findJoin(p.Right)
	case Or:
		if j := findJoin(p.Left); j != nil {
			return j
		}
		return findJoin(p.Right)
	}
	return nil
}

type joinEdgePlacement struct {
	edge  JoinEdge
	value Value
}

// joinEdgesFor computes the JoinEdge/filter-value placements for Tier 2 for the
// given table. Returns nil when no filterable edge exists for this table
// (callers then fall through to Tier 3).
func joinEdgesFor(pred Predicate, join *Join, t TableID, resolver IndexResolver) []joinEdgePlacement {
	var other TableID
	var myJoinCol, otherJoinCol ColID
	switch t {
	case join.Left:
		other = join.Right
		myJoinCol = join.LeftCol
		otherJoinCol = join.RightCol
	case join.Right:
		other = join.Left
		myJoinCol = join.RightCol
		otherJoinCol = join.LeftCol
	default:
		return nil
	}
	otherColEqs := findColEqs(pred, other)
	if len(otherColEqs) == 0 {
		return nil
	}
	if resolver != nil {
		if _, ok := resolver.IndexIDForColumn(other, otherJoinCol); !ok {
			return nil
		}
	}
	placements := make([]joinEdgePlacement, 0, len(otherColEqs))
	for _, ce := range otherColEqs {
		placements = append(placements, joinEdgePlacement{
			edge: JoinEdge{
				LHSTable:     t,
				RHSTable:     other,
				LHSJoinCol:   myJoinCol,
				RHSJoinCol:   otherJoinCol,
				RHSFilterCol: ce.Column,
			},
			value: ce.Value,
		})
	}
	return placements
}
