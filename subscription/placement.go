package subscription

import (
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// PruningIndexes composes the three pruning tiers (SPEC-004 §5).
type PruningIndexes struct {
	Value    *ValueIndex
	JoinEdge *JoinEdgeIndex
	Table    *TableIndex
}

// NewPruningIndexes constructs an empty composite.
func NewPruningIndexes() *PruningIndexes {
	return &PruningIndexes{
		Value:    NewValueIndex(),
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
	if len(p.JoinEdge.edges) != 0 || len(p.JoinEdge.byTable) != 0 {
		return false
	}
	if len(p.Table.tables) != 0 {
		return false
	}
	return true
}

// PlaceSubscription routes each (query, table) pair to exactly one tier
// following the §5.4 invariant. A two-table subscription may land in
// different tiers for each table.
//
// Self-joins (Join.Left == Join.Right) always fall through to Tier 3 for
// their shared table: filter leaves are alias-tagged and apply to only one
// side of a joined pair, so Tier 1 / Tier 2 lookups keyed on the leaf value
// would prune out legitimate candidates whose insertion plays the other
// (unconstrained) side.
func PlaceSubscription(idx *PruningIndexes, pred Predicate, hash QueryHash) {
	mutateSubscriptionPlacement(idx, pred, hash, true)
}

// RemoveSubscription reverses PlaceSubscription.
func RemoveSubscription(idx *PruningIndexes, pred Predicate, hash QueryHash) {
	mutateSubscriptionPlacement(idx, pred, hash, false)
}

func mutateSubscriptionPlacement(idx *PruningIndexes, pred Predicate, hash QueryHash, add bool) {
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
		if join != nil {
			if edge, val, ok := joinEdgeFor(pred, join, t); ok {
				mutateJoinEdgePlacement(idx, edge, val, hash, add)
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
		for _, row := range rows {
			if int(col) >= len(row) {
				continue
			}
			idx.Value.ForEachHash(table, col, row[col], func(h QueryHash) {
				set[h] = struct{}{}
			})
		}
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

// joinEdgeFor computes the JoinEdge and filter value for Tier 2 placement
// for the given table. Returns ok=false when no filterable edge exists for
// this table (callers then fall through to Tier 3).
func joinEdgeFor(pred Predicate, join *Join, t TableID) (JoinEdge, Value, bool) {
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
		return JoinEdge{}, Value{}, false
	}
	otherColEqs := findColEqs(pred, other)
	if len(otherColEqs) == 0 {
		return JoinEdge{}, Value{}, false
	}
	ce := otherColEqs[0]
	return JoinEdge{
		LHSTable:     t,
		RHSTable:     other,
		LHSJoinCol:   myJoinCol,
		RHSJoinCol:   otherJoinCol,
		RHSFilterCol: ce.Column,
	}, ce.Value, true
}
