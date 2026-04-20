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

// IndexResolver maps (table, column) → indexID when an index on that single
// column exists. Used by Tier 2 candidate collection to resolve the RHS row
// for a join edge.
type IndexResolver interface {
	IndexIDForColumn(table TableID, col ColID) (IndexID, bool)
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
	if j, ok := pred.(Join); ok && j.Left == j.Right {
		idx.Table.Add(j.Left, hash)
		return
	}
	join := findJoin(pred)
	for _, t := range pred.Tables() {
		colEqs := findColEqs(pred, t)
		if len(colEqs) > 0 {
			for _, ce := range colEqs {
				idx.Value.Add(t, ce.Column, ce.Value, hash)
			}
			continue
		}
		if join != nil {
			if edge, val, ok := joinEdgeFor(pred, join, t); ok {
				idx.JoinEdge.Add(edge, val, hash)
				continue
			}
		}
		idx.Table.Add(t, hash)
	}
}

// RemoveSubscription reverses PlaceSubscription.
func RemoveSubscription(idx *PruningIndexes, pred Predicate, hash QueryHash) {
	if j, ok := pred.(Join); ok && j.Left == j.Right {
		idx.Table.Remove(j.Left, hash)
		return
	}
	join := findJoin(pred)
	for _, t := range pred.Tables() {
		colEqs := findColEqs(pred, t)
		if len(colEqs) > 0 {
			for _, ce := range colEqs {
				idx.Value.Remove(t, ce.Column, ce.Value, hash)
			}
			continue
		}
		if join != nil {
			if edge, val, ok := joinEdgeFor(pred, join, t); ok {
				idx.JoinEdge.Remove(edge, val, hash)
				continue
			}
		}
		idx.Table.Remove(t, hash)
	}
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
	for _, col := range idx.Value.TrackedColumns(table) {
		for _, row := range rows {
			if int(col) >= len(row) {
				continue
			}
			for _, h := range idx.Value.Lookup(table, col, row[col]) {
				set[h] = struct{}{}
			}
		}
	}

	// Tier 2: join edges where this table is the LHS side.
	if committed != nil && resolver != nil {
		for _, edge := range idx.JoinEdge.EdgesForTable(table) {
			rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
			if !ok {
				continue
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
					for _, h := range idx.JoinEdge.Lookup(edge, rhsRow[edge.RHSFilterCol]) {
						set[h] = struct{}{}
					}
				}
			}
		}
	}

	// Tier 3: table fallback.
	for _, h := range idx.Table.Lookup(table) {
		set[h] = struct{}{}
	}

	out := make([]QueryHash, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	return out
}

// findColEqs returns every ColEq predicate in the tree whose Table matches t.
func findColEqs(pred Predicate, t TableID) []ColEq {
	var out []ColEq
	walkColEqs(pred, t, &out)
	return out
}

func walkColEqs(pred Predicate, t TableID, out *[]ColEq) {
	switch p := pred.(type) {
	case ColEq:
		if p.Table == t {
			*out = append(*out, p)
		}
	case And:
		if p.Left != nil {
			walkColEqs(p.Left, t, out)
		}
		if p.Right != nil {
			walkColEqs(p.Right, t, out)
		}
	case Or:
		if p.Left != nil {
			walkColEqs(p.Left, t, out)
		}
		if p.Right != nil {
			walkColEqs(p.Right, t, out)
		}
	case Join:
		if p.Filter != nil {
			walkColEqs(p.Filter, t, out)
		}
	}
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
