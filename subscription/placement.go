package subscription

import (
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// PruningIndexes composes the pruning tiers (SPEC-004 §5).
type PruningIndexes struct {
	Value         *ValueIndex
	Range         *RangeIndex
	JoinEdge      *JoinEdgeIndex
	JoinRangeEdge *JoinRangeEdgeIndex
	Table         *TableIndex
}

// NewPruningIndexes constructs an empty composite.
func NewPruningIndexes() *PruningIndexes {
	return &PruningIndexes{
		Value:         NewValueIndex(),
		Range:         NewRangeIndex(),
		JoinEdge:      NewJoinEdgeIndex(),
		JoinRangeEdge: NewJoinRangeEdgeIndex(),
		Table:         NewTableIndex(),
	}
}

// PlaceSubscription routes each (query, table) pair to one pruning tier.
// Predicates that can never match are omitted because they can never emit
// deltas. Self-joins use table-level placement because leaves apply to one side
// only.
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
	if predicateNeverMatches(pred) {
		return
	}
	if p, ok := pred.(MultiJoin); ok {
		mutateMultiJoinPlacement(idx, p, hash, add, resolver)
		return
	}
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
		mixedColEqs, mixedColRanges := findMixedColEqRanges(pred, t)
		if len(mixedColEqs) > 0 || len(mixedColRanges) > 0 {
			for _, ce := range mixedColEqs {
				mutateValuePlacement(idx, t, ce.Column, ce.Value, hash, add)
			}
			for _, cr := range mixedColRanges {
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
			if placements := joinRangeEdgesFor(pred, join, t, resolver); len(placements) > 0 {
				for _, placement := range placements {
					mutateJoinRangeEdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				continue
			}
			if valuePlacements, rangePlacements := mixedJoinEdgesFor(pred, join, t, resolver); len(valuePlacements) > 0 || len(rangePlacements) > 0 {
				for _, placement := range valuePlacements {
					mutateJoinEdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range rangePlacements {
					mutateJoinRangeEdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				continue
			}
			if placements, ok := splitJoinOrPlacementsFor(pred, join, t, resolver); ok {
				for _, ce := range placements.eqs {
					mutateValuePlacement(idx, t, ce.Column, ce.Value, hash, add)
				}
				for _, cr := range placements.ranges {
					mutateRangePlacement(idx, t, cr.Column, cr.Lower, cr.Upper, hash, add)
				}
				for _, placement := range placements.edges {
					mutateJoinEdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range placements.rangeEdges {
					mutateJoinRangeEdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				continue
			}
			if placements := joinExistenceEdgesFor(join, t, resolver); len(placements) > 0 {
				for _, placement := range placements {
					mutateJoinExistencePlacement(idx, placement.edge, hash, add)
				}
				continue
			}
		}
		mutateTablePlacement(idx, t, hash, add)
	}
}

func mutateMultiJoinPlacement(idx *PruningIndexes, pred MultiJoin, hash QueryHash, add bool, resolver IndexResolver) {
	tableCounts := multiJoinTableCounts(pred)
	conditions := multiJoinPlacementConditions(pred)
	for _, t := range pred.Tables() {
		if tableCounts[t] == 1 && mutateLocalFilterPlacement(idx, pred.Filter, t, hash, add) {
			continue
		}
		if tableCounts[t] > 1 && mutateAliasCompoundPlacement(idx, pred, conditions, t, hash, add, resolver) {
			continue
		}
		if placements := multiJoinExistenceEdgesFor(pred.Relations, conditions, t, resolver); len(placements) > 0 {
			for _, placement := range placements {
				mutateJoinExistencePlacement(idx, placement.edge, hash, add)
			}
			continue
		}
		mutateTablePlacement(idx, t, hash, add)
	}
}

func mutateAliasCompoundPlacement(idx *PruningIndexes, pred MultiJoin, conditions []MultiJoinCondition, t TableID, hash QueryHash, add bool, resolver IndexResolver) bool {
	relationIndexes := multiJoinRelationIndexesForTable(pred.Relations, t)
	if len(relationIndexes) == 0 {
		return false
	}
	var filters colFilterPlacements
	var edges []joinExistenceEdgePlacement
	for _, relation := range relationIndexes {
		rel := pred.Relations[relation]
		if aliasPlacements, ok := requiredAliasLocalFilterPlacements(pred.Filter, rel.Table, rel.Alias); ok && aliasPlacements.hasAny() {
			filters = mergeColFilterPlacements(filters, aliasPlacements)
			continue
		}
		relationEdges := multiJoinExistenceEdgesForRelation(conditions, relation, resolver)
		if len(relationEdges) == 0 {
			return false
		}
		edges = append(edges, relationEdges...)
	}
	if !filters.hasAny() && len(edges) == 0 {
		return false
	}
	for _, ce := range filters.eqs {
		mutateValuePlacement(idx, t, ce.Column, ce.Value, hash, add)
	}
	for _, cr := range filters.ranges {
		mutateRangePlacement(idx, t, cr.Column, cr.Lower, cr.Upper, hash, add)
	}
	for _, placement := range edges {
		mutateJoinExistencePlacement(idx, placement.edge, hash, add)
	}
	return true
}

func multiJoinTableCounts(pred MultiJoin) map[TableID]int {
	counts := make(map[TableID]int, len(pred.Relations))
	for _, rel := range pred.Relations {
		counts[rel.Table]++
	}
	return counts
}

func multiJoinPlacementConditions(pred MultiJoin) []MultiJoinCondition {
	filterConditions := requiredMultiJoinFilterConditions(pred.Filter, pred.Relations)
	if len(filterConditions) == 0 {
		return pred.Conditions
	}
	out := make([]MultiJoinCondition, 0, len(pred.Conditions)+len(filterConditions))
	out = append(out, pred.Conditions...)
	out = append(out, filterConditions...)
	return out
}

// Filter column equalities are placement conditions only when every matching
// tuple must satisfy them directly or through AND. OR stays on broader fallback
// until there is a disjunctive edge planner.
func requiredMultiJoinFilterConditions(pred Predicate, relations []MultiJoinRelation) []MultiJoinCondition {
	switch p := pred.(type) {
	case ColEqCol:
		left, ok := multiJoinFilterColumnRef(relations, p.LeftTable, p.LeftAlias, p.LeftColumn)
		if !ok {
			return nil
		}
		right, ok := multiJoinFilterColumnRef(relations, p.RightTable, p.RightAlias, p.RightColumn)
		if !ok || left.Relation == right.Relation {
			return nil
		}
		return []MultiJoinCondition{{Left: left, Right: right}}
	case And:
		left := requiredMultiJoinFilterConditions(p.Left, relations)
		right := requiredMultiJoinFilterConditions(p.Right, relations)
		if len(left) == 0 {
			return right
		}
		if len(right) == 0 {
			return left
		}
		return append(left, right...)
	default:
		return nil
	}
}

func multiJoinFilterColumnRef(relations []MultiJoinRelation, table TableID, alias uint8, column ColID) (MultiJoinColumnRef, bool) {
	match := -1
	tableCount := 0
	for i, rel := range relations {
		if rel.Table != table {
			continue
		}
		tableCount++
		if rel.Alias == alias {
			match = i
		}
		if tableCount == 1 {
			match = i
		}
	}
	if tableCount == 0 || match < 0 {
		return MultiJoinColumnRef{}, false
	}
	if tableCount > 1 && relations[match].Alias != alias {
		return MultiJoinColumnRef{}, false
	}
	rel := relations[match]
	return MultiJoinColumnRef{Relation: match, Table: rel.Table, Column: column, Alias: rel.Alias}, true
}

func mutateLocalFilterPlacement(idx *PruningIndexes, pred Predicate, t TableID, hash QueryHash, add bool) bool {
	colEqs := findColEqs(pred, t)
	if len(colEqs) > 0 {
		for _, ce := range colEqs {
			mutateValuePlacement(idx, t, ce.Column, ce.Value, hash, add)
		}
		return true
	}
	colRanges := findColRanges(pred, t)
	if len(colRanges) > 0 {
		for _, cr := range colRanges {
			mutateRangePlacement(idx, t, cr.Column, cr.Lower, cr.Upper, hash, add)
		}
		return true
	}
	mixedColEqs, mixedColRanges := findMixedColEqRanges(pred, t)
	if len(mixedColEqs) == 0 && len(mixedColRanges) == 0 {
		return false
	}
	for _, ce := range mixedColEqs {
		mutateValuePlacement(idx, t, ce.Column, ce.Value, hash, add)
	}
	for _, cr := range mixedColRanges {
		mutateRangePlacement(idx, t, cr.Column, cr.Lower, cr.Upper, hash, add)
	}
	return true
}

func requiredAliasLocalFilterPlacements(pred Predicate, table TableID, alias uint8) (colFilterPlacements, bool) {
	switch p := pred.(type) {
	case ColEq:
		if p.Table == table && p.Alias == alias {
			return colFilterPlacements{eqs: []ColEq{p}}, true
		}
		return colFilterPlacements{}, false
	case ColRange:
		if p.Table == table && p.Alias == alias && rangeHasBound(p) {
			return colFilterPlacements{ranges: []ColRange{p}}, true
		}
		return colFilterPlacements{}, false
	case ColNe:
		if p.Table == table && p.Alias == alias {
			return colFilterPlacements{ranges: colNeRanges(p)}, true
		}
		return colFilterPlacements{}, false
	case And:
		left, leftOK := requiredAliasLocalFilterPlacements(p.Left, table, alias)
		right, rightOK := requiredAliasLocalFilterPlacements(p.Right, table, alias)
		switch {
		case leftOK && rightOK:
			return mergeColFilterPlacements(left, right), true
		case leftOK:
			return left, true
		case rightOK:
			return right, true
		default:
			return colFilterPlacements{}, false
		}
	case Or:
		left, leftOK := requiredAliasLocalFilterPlacements(p.Left, table, alias)
		right, rightOK := requiredAliasLocalFilterPlacements(p.Right, table, alias)
		if !leftOK || !rightOK {
			return colFilterPlacements{}, false
		}
		return mergeColFilterPlacements(left, right), true
	}
	return colFilterPlacements{}, false
}

func multiJoinExistenceEdgesFor(relations []MultiJoinRelation, conditions []MultiJoinCondition, t TableID, resolver IndexResolver) []joinExistenceEdgePlacement {
	if resolver == nil {
		return nil
	}
	relationIndexes := multiJoinRelationIndexesForTable(relations, t)
	if len(relationIndexes) == 0 {
		return nil
	}
	var placements []joinExistenceEdgePlacement
	for _, relation := range relationIndexes {
		relationPlacements := multiJoinExistenceEdgesForRelation(conditions, relation, resolver)
		if len(relationPlacements) == 0 {
			return nil
		}
		placements = append(placements, relationPlacements...)
	}
	return placements
}

func multiJoinExistenceEdgesForRelation(conditions []MultiJoinCondition, relation int, resolver IndexResolver) []joinExistenceEdgePlacement {
	if resolver == nil {
		return nil
	}
	var placements []joinExistenceEdgePlacement
	for _, condition := range conditions {
		switch {
		case condition.Left.Relation == relation:
			if placement, ok := multiJoinExistenceEdgeForRefs(condition.Left, condition.Right, resolver); ok {
				placements = append(placements, placement)
			}
		case condition.Right.Relation == relation:
			if placement, ok := multiJoinExistenceEdgeForRefs(condition.Right, condition.Left, resolver); ok {
				placements = append(placements, placement)
			}
		}
	}
	return placements
}

func multiJoinRelationIndexesForTable(relations []MultiJoinRelation, table TableID) []int {
	var out []int
	for i, rel := range relations {
		if rel.Table != table {
			continue
		}
		out = append(out, i)
	}
	return out
}

func multiJoinExistenceEdgeForRefs(lhs, rhs MultiJoinColumnRef, resolver IndexResolver) (joinExistenceEdgePlacement, bool) {
	if _, ok := resolver.IndexIDForColumn(rhs.Table, rhs.Column); !ok {
		return joinExistenceEdgePlacement{}, false
	}
	return joinExistenceEdgePlacement{edge: JoinEdge{
		LHSTable:     lhs.Table,
		RHSTable:     rhs.Table,
		LHSJoinCol:   lhs.Column,
		RHSJoinCol:   rhs.Column,
		RHSFilterCol: rhs.Column,
	}}, true
}

func predicateNeverMatches(pred Predicate) bool {
	switch p := pred.(type) {
	case nil:
		return false
	case NoRows:
		return true
	case And:
		return predicateNeverMatches(p.Left) || predicateNeverMatches(p.Right)
	case Or:
		return predicateNeverMatches(p.Left) && predicateNeverMatches(p.Right)
	case Join:
		return predicateNeverMatches(p.Filter)
	case CrossJoin:
		return predicateNeverMatches(p.Filter)
	case MultiJoin:
		return predicateNeverMatches(p.Filter)
	default:
		return false
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

func mutateJoinRangeEdgePlacement(idx *PruningIndexes, edge JoinEdge, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.JoinRangeEdge.Add(edge, lower, upper, hash)
		return
	}
	idx.JoinRangeEdge.Remove(edge, lower, upper, hash)
}

func mutateJoinExistencePlacement(idx *PruningIndexes, edge JoinEdge, hash QueryHash, add bool) {
	if add {
		idx.JoinEdge.AddExistence(edge, hash)
		return
	}
	idx.JoinEdge.RemoveExistence(edge, hash)
}

func mutateTablePlacement(idx *PruningIndexes, table TableID, hash QueryHash, add bool) {
	if add {
		idx.Table.Add(table, hash)
		return
	}
	idx.Table.Remove(table, hash)
}

// CollectCandidatesForTable returns the set of candidate query hashes for a
// single changed table. Consults all pruning tiers and unions the results.
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
	clear(set)

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

	collectJoinEdgeCandidates(idx, table, rows, committed, resolver, func(h QueryHash) {
		set[h] = struct{}{}
	})

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

// findColRanges returns range constraints whose bounds cover every matching row
// for table t. ColNe is represented as two exclusive ranges around the rejected
// value. Range placement follows the same safety rule as equality placement:
// AND may use any required child constraint; OR must constrain all branches or
// fall back to a broader tier.
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
	case ColNe:
		if p.Table == t {
			return colNeRanges(p), true
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
	case CrossJoin:
		if p.Left != p.Right && p.Filter != nil {
			return requiredColRanges(p.Filter, t)
		}
	}
	return nil, false
}

func colNeRanges(p ColNe) []ColRange {
	return []ColRange{
		{
			Table:  p.Table,
			Column: p.Column,
			Alias:  p.Alias,
			Lower:  Bound{Unbounded: true},
			Upper:  Bound{Value: p.Value, Inclusive: false},
		},
		{
			Table:  p.Table,
			Column: p.Column,
			Alias:  p.Alias,
			Lower:  Bound{Value: p.Value, Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
}

func rangeHasBound(p ColRange) bool {
	return !p.Lower.Unbounded || !p.Upper.Unbounded
}

type colFilterPlacements struct {
	eqs    []ColEq
	ranges []ColRange
}

func (p colFilterPlacements) hasAny() bool {
	return len(p.eqs) > 0 || len(p.ranges) > 0
}

// findMixedColEqRanges returns equality/range predicates whose union covers
// every matching row for table t when the pure equality and pure range paths do
// not apply. This lets mixed OR shapes such as `a = 1 OR b > 5` avoid table
// fallback while preserving the same safety rule: every OR branch must carry an
// indexable constraint for t.
func findMixedColEqRanges(pred Predicate, t TableID) ([]ColEq, []ColRange) {
	out, ok := requiredMixedColEqRanges(pred, t)
	if !ok || len(out.eqs) == 0 || len(out.ranges) == 0 {
		return nil, nil
	}
	return out.eqs, out.ranges
}

func requiredMixedColEqRanges(pred Predicate, t TableID) (colFilterPlacements, bool) {
	switch p := pred.(type) {
	case ColEq:
		if p.Table == t {
			return colFilterPlacements{eqs: []ColEq{p}}, true
		}
		return colFilterPlacements{}, false
	case ColRange:
		if p.Table == t && rangeHasBound(p) {
			return colFilterPlacements{ranges: []ColRange{p}}, true
		}
		return colFilterPlacements{}, false
	case ColNe:
		if p.Table == t {
			return colFilterPlacements{ranges: colNeRanges(p)}, true
		}
		return colFilterPlacements{}, false
	case And:
		left, leftOK := requiredMixedColEqRanges(p.Left, t)
		right, rightOK := requiredMixedColEqRanges(p.Right, t)
		switch {
		case leftOK && rightOK:
			return mergeColFilterPlacements(left, right), true
		case leftOK:
			return left, true
		case rightOK:
			return right, true
		default:
			return colFilterPlacements{}, false
		}
	case Or:
		left, leftOK := requiredMixedColEqRanges(p.Left, t)
		right, rightOK := requiredMixedColEqRanges(p.Right, t)
		if !leftOK || !rightOK {
			return colFilterPlacements{}, false
		}
		return mergeColFilterPlacements(left, right), true
	case Join:
		if p.Filter != nil {
			return requiredMixedColEqRanges(p.Filter, t)
		}
	case CrossJoin:
		if p.Left != p.Right && p.Filter != nil {
			return requiredMixedColEqRanges(p.Filter, t)
		}
	}
	return colFilterPlacements{}, false
}

func mergeColFilterPlacements(left, right colFilterPlacements) colFilterPlacements {
	if len(right.eqs) > 0 {
		left.eqs = append(left.eqs, right.eqs...)
	}
	if len(right.ranges) > 0 {
		left.ranges = append(left.ranges, right.ranges...)
	}
	return left
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
	case CrossJoin:
		if p.Left != p.Right && p.Filter != nil {
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

type joinRangeEdgePlacement struct {
	edge  JoinEdge
	lower Bound
	upper Bound
}

type joinExistenceEdgePlacement struct {
	edge JoinEdge
}

type splitJoinOrPlacements struct {
	eqs        []ColEq
	ranges     []ColRange
	edges      []joinEdgePlacement
	rangeEdges []joinRangeEdgePlacement
}

type joinPlacementSide struct {
	table        TableID
	other        TableID
	joinCol      ColID
	otherJoinCol ColID
}

func joinPlacementSideFor(join *Join, table TableID) (joinPlacementSide, bool) {
	switch table {
	case join.Left:
		return joinPlacementSide{
			table:        join.Left,
			other:        join.Right,
			joinCol:      join.LeftCol,
			otherJoinCol: join.RightCol,
		}, true
	case join.Right:
		return joinPlacementSide{
			table:        join.Right,
			other:        join.Left,
			joinCol:      join.RightCol,
			otherJoinCol: join.LeftCol,
		}, true
	default:
		return joinPlacementSide{}, false
	}
}

func (s joinPlacementSide) edge(filterCol ColID) JoinEdge {
	return JoinEdge{
		LHSTable:     s.table,
		RHSTable:     s.other,
		LHSJoinCol:   s.joinCol,
		RHSJoinCol:   s.otherJoinCol,
		RHSFilterCol: filterCol,
	}
}

func (s joinPlacementSide) otherJoinColumnIndexed(resolver IndexResolver) bool {
	if resolver == nil {
		return true
	}
	_, ok := resolver.IndexIDForColumn(s.other, s.otherJoinCol)
	return ok
}

func (p splitJoinOrPlacements) hasAny() bool {
	return len(p.eqs) > 0 || len(p.ranges) > 0 || len(p.edges) > 0 || len(p.rangeEdges) > 0
}

func (p *splitJoinOrPlacements) append(other splitJoinOrPlacements) {
	p.eqs = append(p.eqs, other.eqs...)
	p.ranges = append(p.ranges, other.ranges...)
	p.edges = append(p.edges, other.edges...)
	p.rangeEdges = append(p.rangeEdges, other.rangeEdges...)
}

// joinEdgesFor computes the JoinEdge/filter-value placements for Tier 2 for the
// given table. Returns nil when no filterable edge exists for this table
// (callers then fall through to Tier 3).
func joinEdgesFor(pred Predicate, join *Join, t TableID, resolver IndexResolver) []joinEdgePlacement {
	side, ok := joinPlacementSideFor(join, t)
	if !ok {
		return nil
	}
	otherColEqs := findColEqs(pred, side.other)
	if len(otherColEqs) == 0 {
		return nil
	}
	if !side.otherJoinColumnIndexed(resolver) {
		return nil
	}
	placements := make([]joinEdgePlacement, 0, len(otherColEqs))
	for _, ce := range otherColEqs {
		placements = append(placements, joinEdgePlacement{
			edge:  side.edge(ce.Column),
			value: ce.Value,
		})
	}
	return placements
}

// joinRangeEdgesFor computes the JoinEdge/range-filter placements for Tier 2
// for the given table. Returns nil when no range-filterable edge exists.
func joinRangeEdgesFor(pred Predicate, join *Join, t TableID, resolver IndexResolver) []joinRangeEdgePlacement {
	side, ok := joinPlacementSideFor(join, t)
	if !ok {
		return nil
	}
	otherColRanges := findColRanges(pred, side.other)
	if len(otherColRanges) == 0 {
		return nil
	}
	if !side.otherJoinColumnIndexed(resolver) {
		return nil
	}
	placements := make([]joinRangeEdgePlacement, 0, len(otherColRanges))
	for _, cr := range otherColRanges {
		placements = append(placements, joinRangeEdgePlacement{
			edge:  side.edge(cr.Column),
			lower: cr.Lower,
			upper: cr.Upper,
		})
	}
	return placements
}

// mixedJoinEdgesFor computes the mixed equality/range-filter companion to
// joinEdgesFor and joinRangeEdgesFor. It is used only after the pure paths
// decline placement, so it covers indexable mixed OR shapes without changing
// the established one-tier behavior for pure equality or pure range filters.
func mixedJoinEdgesFor(
	pred Predicate,
	join *Join,
	t TableID,
	resolver IndexResolver,
) ([]joinEdgePlacement, []joinRangeEdgePlacement) {
	side, ok := joinPlacementSideFor(join, t)
	if !ok {
		return nil, nil
	}
	otherColEqs, otherColRanges := findMixedColEqRanges(pred, side.other)
	if len(otherColEqs) == 0 && len(otherColRanges) == 0 {
		return nil, nil
	}
	if !side.otherJoinColumnIndexed(resolver) {
		return nil, nil
	}
	valuePlacements := make([]joinEdgePlacement, 0, len(otherColEqs))
	for _, ce := range otherColEqs {
		valuePlacements = append(valuePlacements, joinEdgePlacement{
			edge:  side.edge(ce.Column),
			value: ce.Value,
		})
	}
	rangePlacements := make([]joinRangeEdgePlacement, 0, len(otherColRanges))
	for _, cr := range otherColRanges {
		rangePlacements = append(rangePlacements, joinRangeEdgePlacement{
			edge:  side.edge(cr.Column),
			lower: cr.Lower,
			upper: cr.Upper,
		})
	}
	return valuePlacements, rangePlacements
}

// splitJoinOrPlacementsFor covers OR filters whose branches constrain
// different join sides. Existing pure paths handle same-side ORs; this path
// avoids falling back to broad join-existence candidates for shapes like
// `left.flag = true OR right.score > 50`.
func splitJoinOrPlacementsFor(pred Predicate, join *Join, t TableID, resolver IndexResolver) (splitJoinOrPlacements, bool) {
	side, ok := joinPlacementSideFor(join, t)
	if !ok {
		return splitJoinOrPlacements{}, false
	}
	if pred == nil {
		return splitJoinOrPlacements{}, false
	}
	if j, ok := pred.(Join); ok {
		return splitJoinOrPlacementsFor(j.Filter, join, t, resolver)
	}
	placements, ok := splitJoinOrPredicatePlacements(pred, side, resolver)
	if !ok || !placements.hasAny() || !splitJoinOrNeedsBothSides(placements) {
		return splitJoinOrPlacements{}, false
	}
	return placements, true
}

func splitJoinOrPredicatePlacements(
	pred Predicate,
	side joinPlacementSide,
	resolver IndexResolver,
) (splitJoinOrPlacements, bool) {
	switch p := pred.(type) {
	case Or:
		left, leftOK := splitJoinOrPredicatePlacements(p.Left, side, resolver)
		right, rightOK := splitJoinOrPredicatePlacements(p.Right, side, resolver)
		if !leftOK || !rightOK {
			return splitJoinOrPlacements{}, false
		}
		left.append(right)
		return left, true
	default:
		return splitJoinOrBranchPlacements(pred, side, resolver)
	}
}

func splitJoinOrBranchPlacements(
	pred Predicate,
	side joinPlacementSide,
	resolver IndexResolver,
) (splitJoinOrPlacements, bool) {
	switch p := pred.(type) {
	case ColEq:
		switch p.Table {
		case side.table:
			return splitJoinOrPlacements{eqs: []ColEq{p}}, true
		case side.other:
			if !side.otherJoinColumnIndexed(resolver) {
				return splitJoinOrPlacements{}, false
			}
			return splitJoinOrPlacements{edges: []joinEdgePlacement{{
				edge:  side.edge(p.Column),
				value: p.Value,
			}}}, true
		default:
			return splitJoinOrPlacements{}, false
		}
	case ColRange:
		if !rangeHasBound(p) {
			return splitJoinOrPlacements{}, false
		}
		switch p.Table {
		case side.table:
			return splitJoinOrPlacements{ranges: []ColRange{p}}, true
		case side.other:
			if !side.otherJoinColumnIndexed(resolver) {
				return splitJoinOrPlacements{}, false
			}
			return splitJoinOrPlacements{rangeEdges: []joinRangeEdgePlacement{{
				edge:  side.edge(p.Column),
				lower: p.Lower,
				upper: p.Upper,
			}}}, true
		default:
			return splitJoinOrPlacements{}, false
		}
	case ColNe:
		ranges := colNeRanges(p)
		switch p.Table {
		case side.table:
			return splitJoinOrPlacements{ranges: ranges}, true
		case side.other:
			if !side.otherJoinColumnIndexed(resolver) {
				return splitJoinOrPlacements{}, false
			}
			out := splitJoinOrPlacements{rangeEdges: make([]joinRangeEdgePlacement, 0, len(ranges))}
			for _, cr := range ranges {
				out.rangeEdges = append(out.rangeEdges, joinRangeEdgePlacement{
					edge:  side.edge(cr.Column),
					lower: cr.Lower,
					upper: cr.Upper,
				})
			}
			return out, true
		default:
			return splitJoinOrPlacements{}, false
		}
	case And:
		var out splitJoinOrPlacements
		if left, ok := splitJoinOrBranchPlacements(p.Left, side, resolver); ok {
			out.append(left)
		}
		if right, ok := splitJoinOrBranchPlacements(p.Right, side, resolver); ok {
			out.append(right)
		}
		return out, out.hasAny()
	default:
		return splitJoinOrPlacements{}, false
	}
}

func splitJoinOrNeedsBothSides(p splitJoinOrPlacements) bool {
	hasCurrentSide := len(p.eqs) > 0 || len(p.ranges) > 0
	hasOtherSide := len(p.edges) > 0 || len(p.rangeEdges) > 0
	return hasCurrentSide && hasOtherSide
}

// joinExistenceEdgesFor computes join-existence placements for Tier 2 for the
// given table. Existence placement is safe when the opposite join column has a
// committed index: candidate collection only needs to prove that at least one
// opposite-side row can share the changed row's join key.
func joinExistenceEdgesFor(join *Join, t TableID, resolver IndexResolver) []joinExistenceEdgePlacement {
	if resolver == nil {
		return nil
	}
	side, ok := joinPlacementSideFor(join, t)
	if !ok {
		return nil
	}
	if !side.otherJoinColumnIndexed(resolver) {
		return nil
	}
	return []joinExistenceEdgePlacement{{
		edge: side.edge(side.otherJoinCol),
	}}
}

func collectJoinEdgeCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if committed == nil || resolver == nil {
		return
	}
	idx.JoinEdge.ForEachEdge(table, func(edge JoinEdge) {
		if forEachJoinedRHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinEdge.ForEachHash(edge, v, add)
		}) {
			idx.JoinEdge.ForEachExistenceHash(edge, add)
		}
	})
	idx.JoinRangeEdge.ForEachEdge(table, func(edge JoinEdge) {
		forEachJoinedRHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinRangeEdge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedRHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinEdge,
	fn func(Value),
) bool {
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return false
	}
	matched := false
	for _, row := range rows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		key := store.NewIndexKey(row[edge.LHSJoinCol])
		rowIDs := committed.IndexSeek(edge.RHSTable, rhsIdx, key)
		for _, rid := range rowIDs {
			rhsRow, ok := committed.GetRow(edge.RHSTable, rid)
			if !ok {
				continue
			}
			matched = true
			if int(edge.RHSFilterCol) >= len(rhsRow) {
				continue
			}
			fn(rhsRow[edge.RHSFilterCol])
		}
	}
	return matched
}

func collectJoinExistenceDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.JoinEdge.ForEachEdge(table, func(edge JoinEdge) {
		tc := changeset.Tables[edge.RHSTable]
		if tc == nil {
			return
		}
		if !joinKeyOverlapsChangedRows(rows, edge.LHSJoinCol, tc.Inserts, edge.RHSJoinCol) &&
			!joinKeyOverlapsChangedRows(rows, edge.LHSJoinCol, tc.Deletes, edge.RHSJoinCol) {
			return
		}
		idx.JoinEdge.ForEachExistenceHash(edge, add)
	})
}

func joinKeyOverlapsChangedRows(lhsRows []types.ProductValue, lhsCol ColID, rhsRows []types.ProductValue, rhsCol ColID) bool {
	if len(lhsRows) == 0 || len(rhsRows) == 0 {
		return false
	}
	rhsKeys := make(map[valueKey]struct{}, len(rhsRows))
	for _, row := range rhsRows {
		if int(rhsCol) >= len(row) {
			continue
		}
		rhsKeys[encodeValueKey(row[rhsCol])] = struct{}{}
	}
	if len(rhsKeys) == 0 {
		return false
	}
	for _, row := range lhsRows {
		if int(lhsCol) >= len(row) {
			continue
		}
		if _, ok := rhsKeys[encodeValueKey(row[lhsCol])]; ok {
			return true
		}
	}
	return false
}
