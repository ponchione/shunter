package subscription

import (
	"slices"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// PruningIndexes composes the pruning tiers (SPEC-004 §5).
type PruningIndexes struct {
	Value              *ValueIndex
	Range              *RangeIndex
	JoinEdge           *JoinEdgeIndex
	JoinRangeEdge      *JoinRangeEdgeIndex
	JoinPathEdge       *JoinPathEdgeIndex
	JoinRangePathEdge  *JoinRangePathEdgeIndex
	JoinPath3Edge      *JoinPath3EdgeIndex
	JoinRangePath3Edge *JoinRangePath3EdgeIndex
	JoinPath4Edge      *JoinPath4EdgeIndex
	JoinRangePath4Edge *JoinRangePath4EdgeIndex
	JoinPath5Edge      *JoinPath5EdgeIndex
	JoinRangePath5Edge *JoinRangePath5EdgeIndex
	JoinPath6Edge      *JoinPath6EdgeIndex
	JoinRangePath6Edge *JoinRangePath6EdgeIndex
	Table              *TableIndex
}

// NewPruningIndexes constructs an empty composite.
func NewPruningIndexes() *PruningIndexes {
	return &PruningIndexes{
		Value:              NewValueIndex(),
		Range:              NewRangeIndex(),
		JoinEdge:           NewJoinEdgeIndex(),
		JoinRangeEdge:      NewJoinRangeEdgeIndex(),
		JoinPathEdge:       NewJoinPathEdgeIndex(),
		JoinRangePathEdge:  NewJoinRangePathEdgeIndex(),
		JoinPath3Edge:      NewJoinPath3EdgeIndex(),
		JoinRangePath3Edge: NewJoinRangePath3EdgeIndex(),
		JoinPath4Edge:      NewJoinPath4EdgeIndex(),
		JoinRangePath4Edge: NewJoinRangePath4EdgeIndex(),
		JoinPath5Edge:      NewJoinPath5EdgeIndex(),
		JoinRangePath5Edge: NewJoinRangePath5EdgeIndex(),
		JoinPath6Edge:      NewJoinPath6EdgeIndex(),
		JoinRangePath6Edge: NewJoinRangePath6EdgeIndex(),
		Table:              NewTableIndex(),
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
				for _, placement := range placements.pathEdges {
					mutateJoinPathEdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range placements.rangePathEdges {
					mutateJoinRangePathEdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				for _, placement := range placements.path3Edges {
					mutateJoinPath3EdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range placements.rangePath3Edges {
					mutateJoinRangePath3EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				for _, placement := range placements.path4Edges {
					mutateJoinPath4EdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range placements.rangePath4Edges {
					mutateJoinRangePath4EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				for _, placement := range placements.path5Edges {
					mutateJoinPath5EdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range placements.rangePath5Edges {
					mutateJoinRangePath5EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				for _, placement := range placements.path6Edges {
					mutateJoinPath6EdgePlacement(idx, placement.edge, placement.value, hash, add)
				}
				for _, placement := range placements.rangePath6Edges {
					mutateJoinRangePath6EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
				}
				for _, placement := range placements.existenceEdges {
					mutateJoinExistencePlacement(idx, placement.edge, hash, add)
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
		if mutateMultiJoinSplitOrFilterPlacement(idx, pred.Relations, conditions.required, pred.Filter, t, hash, add, resolver) {
			continue
		}
		if mutateMultiJoinFilterPlacement(idx, pred.Relations, conditions, t, hash, add, resolver) {
			continue
		}
		if mutateMultiJoinRequiredFilterEdgePlacement(idx, pred.Relations, conditions, t, hash, add, resolver) {
			continue
		}
		if tableCounts[t] > 1 && mutateAliasCompoundPlacement(idx, pred, conditions, t, hash, add, resolver) {
			continue
		}
		if placements := multiJoinExistenceEdgesFor(pred.Relations, conditions.required, t, resolver); len(placements) > 0 {
			for _, placement := range placements {
				mutateJoinExistencePlacement(idx, placement.edge, hash, add)
			}
			continue
		}
		mutateTablePlacement(idx, t, hash, add)
	}
}

func mutateAliasCompoundPlacement(idx *PruningIndexes, pred MultiJoin, conditions multiJoinPlacementConditionSet, t TableID, hash QueryHash, add bool, resolver IndexResolver) bool {
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
		relationEdges := multiJoinExistenceEdgesForRelation(conditions.required, relation, resolver)
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

func mutateMultiJoinFilterPlacement(idx *PruningIndexes, relations []MultiJoinRelation, conditions multiJoinPlacementConditionSet, t TableID, hash QueryHash, add bool, resolver IndexResolver) bool {
	relationIndexes := multiJoinRelationIndexesForTable(relations, t)
	if len(relationIndexes) == 0 {
		return false
	}
	var filters colFilterPlacements
	var edges []joinExistenceEdgePlacement
	for _, relation := range relationIndexes {
		placement, ok := conditions.filter[relation]
		if !ok || !placement.hasAny() {
			return false
		}
		filters = mergeColFilterPlacements(filters, placement.filters)
		if len(placement.conditions) == 0 {
			continue
		}
		relationEdges := multiJoinExistenceEdgesForRelation(placement.conditions, relation, resolver)
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

func mutateMultiJoinSplitOrFilterPlacement(
	idx *PruningIndexes,
	relations []MultiJoinRelation,
	conditions []MultiJoinCondition,
	filter Predicate,
	t TableID,
	hash QueryHash,
	add bool,
	resolver IndexResolver,
) bool {
	relationIndexes := multiJoinRelationIndexesForTable(relations, t)
	if len(relationIndexes) == 0 {
		return false
	}
	var placements splitJoinOrPlacements
	for _, relation := range relationIndexes {
		relationPlacements, ok := splitMultiJoinOrFilterPlacementsForRelation(filter, relations, conditions, relation, resolver)
		if !ok || !relationPlacements.hasAny() {
			return false
		}
		placements.append(relationPlacements)
	}
	if !placements.hasAny() {
		return false
	}
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
	for _, placement := range placements.pathEdges {
		mutateJoinPathEdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePathEdges {
		mutateJoinRangePathEdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path3Edges {
		mutateJoinPath3EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath3Edges {
		mutateJoinRangePath3EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path4Edges {
		mutateJoinPath4EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath4Edges {
		mutateJoinRangePath4EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path5Edges {
		mutateJoinPath5EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath5Edges {
		mutateJoinRangePath5EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path6Edges {
		mutateJoinPath6EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath6Edges {
		mutateJoinRangePath6EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.existenceEdges {
		mutateJoinExistencePlacement(idx, placement.edge, hash, add)
	}
	return true
}

func mutateMultiJoinRequiredFilterEdgePlacement(
	idx *PruningIndexes,
	relations []MultiJoinRelation,
	conditions multiJoinPlacementConditionSet,
	t TableID,
	hash QueryHash,
	add bool,
	resolver IndexResolver,
) bool {
	if resolver == nil || len(conditions.filter) == 0 {
		return false
	}
	relationIndexes := multiJoinRelationIndexesForTable(relations, t)
	if len(relationIndexes) == 0 {
		return false
	}
	filterPlacements := multiJoinRequiredLocalFilterPlacements(conditions.filter)
	if len(filterPlacements) == 0 {
		return false
	}

	var placements splitJoinOrPlacements
	for _, relation := range relationIndexes {
		if filters := filterPlacements[relation]; filters.hasAny() {
			placements.eqs = append(placements.eqs, filters.eqs...)
			placements.ranges = append(placements.ranges, filters.ranges...)
			continue
		}

		var relationPlacements splitJoinOrPlacements
		for targetRelation, filters := range filterPlacements {
			if targetRelation == relation || !filters.hasAny() {
				continue
			}
			relationPlacements.append(multiJoinFilterEdgesBetweenRelations(conditions.required, relation, targetRelation, filters, resolver))
		}
		if !relationPlacements.hasAny() {
			return false
		}
		placements.append(relationPlacements)
	}
	if !placements.hasAny() {
		return false
	}

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
	for _, placement := range placements.pathEdges {
		mutateJoinPathEdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePathEdges {
		mutateJoinRangePathEdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path3Edges {
		mutateJoinPath3EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath3Edges {
		mutateJoinRangePath3EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path4Edges {
		mutateJoinPath4EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath4Edges {
		mutateJoinRangePath4EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path5Edges {
		mutateJoinPath5EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath5Edges {
		mutateJoinRangePath5EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	for _, placement := range placements.path6Edges {
		mutateJoinPath6EdgePlacement(idx, placement.edge, placement.value, hash, add)
	}
	for _, placement := range placements.rangePath6Edges {
		mutateJoinRangePath6EdgePlacement(idx, placement.edge, placement.lower, placement.upper, hash, add)
	}
	return true
}

func multiJoinRequiredLocalFilterPlacements(placements map[int]multiJoinRelationFilterPlacement) map[int]colFilterPlacements {
	out := make(map[int]colFilterPlacements, len(placements))
	for relation, placement := range placements {
		if !placement.filters.hasAny() {
			continue
		}
		out[relation] = mergeColFilterPlacements(out[relation], placement.filters)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func multiJoinTableCounts(pred MultiJoin) map[TableID]int {
	counts := make(map[TableID]int, len(pred.Relations))
	for _, rel := range pred.Relations {
		counts[rel.Table]++
	}
	return counts
}

type multiJoinPlacementConditionSet struct {
	required []MultiJoinCondition
	filter   map[int]multiJoinRelationFilterPlacement
}

type multiJoinRelationFilterPlacement struct {
	filters    colFilterPlacements
	conditions []MultiJoinCondition
}

func (p multiJoinRelationFilterPlacement) hasAny() bool {
	return p.filters.hasAny() || len(p.conditions) > 0
}

func multiJoinPlacementConditions(pred MultiJoin) multiJoinPlacementConditionSet {
	return multiJoinPlacementConditionSet{
		required: pred.Conditions,
		filter:   multiJoinFilterConditionsByRelation(pred.Filter, pred.Relations),
	}
}

// Filter predicates become relation-local placements only when every matching
// tuple must satisfy at least one indexed constraint for that relation. For OR
// this keeps only relation coverage common to every branch.
func multiJoinFilterConditionsByRelation(pred Predicate, relations []MultiJoinRelation) map[int]multiJoinRelationFilterPlacement {
	switch p := pred.(type) {
	case ColEq:
		relation, ok := multiJoinFilterRelationIndex(relations, p.Table, p.Alias)
		if !ok {
			return nil
		}
		return map[int]multiJoinRelationFilterPlacement{
			relation: {filters: colFilterPlacements{eqs: []ColEq{p}}},
		}
	case ColRange:
		if !rangeHasBound(p) {
			return nil
		}
		relation, ok := multiJoinFilterRelationIndex(relations, p.Table, p.Alias)
		if !ok {
			return nil
		}
		return map[int]multiJoinRelationFilterPlacement{
			relation: {filters: colFilterPlacements{ranges: []ColRange{p}}},
		}
	case ColNe:
		relation, ok := multiJoinFilterRelationIndex(relations, p.Table, p.Alias)
		if !ok {
			return nil
		}
		return map[int]multiJoinRelationFilterPlacement{
			relation: {filters: colFilterPlacements{ranges: colNeRanges(p)}},
		}
	case ColEqCol:
		left, ok := multiJoinFilterColumnRef(relations, p.LeftTable, p.LeftAlias, p.LeftColumn)
		if !ok {
			return nil
		}
		right, ok := multiJoinFilterColumnRef(relations, p.RightTable, p.RightAlias, p.RightColumn)
		if !ok || left.Relation == right.Relation {
			return nil
		}
		condition := MultiJoinCondition{Left: left, Right: right}
		return map[int]multiJoinRelationFilterPlacement{
			left.Relation:  {conditions: []MultiJoinCondition{condition}},
			right.Relation: {conditions: []MultiJoinCondition{condition}},
		}
	case And:
		return mergeMultiJoinFilterConditionMaps(
			multiJoinFilterConditionsByRelation(p.Left, relations),
			multiJoinFilterConditionsByRelation(p.Right, relations),
		)
	case Or:
		return intersectMultiJoinFilterConditionMaps(
			multiJoinFilterConditionsByRelation(p.Left, relations),
			multiJoinFilterConditionsByRelation(p.Right, relations),
		)
	default:
		return nil
	}
}

func mergeMultiJoinFilterConditionMaps(left, right map[int]multiJoinRelationFilterPlacement) map[int]multiJoinRelationFilterPlacement {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	out := make(map[int]multiJoinRelationFilterPlacement, len(left)+len(right))
	for relation, placement := range left {
		out[relation] = mergeMultiJoinRelationFilterPlacement(out[relation], placement)
	}
	for relation, placement := range right {
		out[relation] = mergeMultiJoinRelationFilterPlacement(out[relation], placement)
	}
	return out
}

func intersectMultiJoinFilterConditionMaps(left, right map[int]multiJoinRelationFilterPlacement) map[int]multiJoinRelationFilterPlacement {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	out := make(map[int]multiJoinRelationFilterPlacement)
	for relation, leftPlacement := range left {
		rightPlacement, ok := right[relation]
		if !ok {
			continue
		}
		placement := intersectMultiJoinRelationFilterPlacement(leftPlacement, rightPlacement)
		if placement.hasAny() {
			out[relation] = placement
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func intersectMultiJoinRelationFilterPlacement(left, right multiJoinRelationFilterPlacement) multiJoinRelationFilterPlacement {
	return multiJoinRelationFilterPlacement{
		filters:    mergeColFilterPlacements(left.filters, right.filters),
		conditions: intersectMultiJoinConditions(left.conditions, right.conditions),
	}
}

func intersectMultiJoinConditions(left, right []MultiJoinCondition) []MultiJoinCondition {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	var out []MultiJoinCondition
	for _, leftCondition := range left {
		for _, rightCondition := range right {
			if !multiJoinConditionsEqual(leftCondition, rightCondition) {
				continue
			}
			if !multiJoinConditionSliceContains(out, leftCondition) {
				out = append(out, leftCondition)
			}
			break
		}
	}
	return out
}

func multiJoinConditionSliceContains(conditions []MultiJoinCondition, target MultiJoinCondition) bool {
	for _, condition := range conditions {
		if multiJoinConditionsEqual(condition, target) {
			return true
		}
	}
	return false
}

func multiJoinConditionsEqual(left, right MultiJoinCondition) bool {
	return left.Left == right.Left && left.Right == right.Right ||
		left.Left == right.Right && left.Right == right.Left
}

func mergeMultiJoinRelationFilterPlacement(left, right multiJoinRelationFilterPlacement) multiJoinRelationFilterPlacement {
	left.filters = mergeColFilterPlacements(left.filters, right.filters)
	left.conditions = append(left.conditions, right.conditions...)
	return left
}

func splitMultiJoinOrFilterPlacementsForRelation(
	filter Predicate,
	relations []MultiJoinRelation,
	conditions []MultiJoinCondition,
	relation int,
	resolver IndexResolver,
) (splitJoinOrPlacements, bool) {
	switch p := filter.(type) {
	case Or:
		branches := multiJoinOrBranches(p)
		if len(branches) < 2 {
			return splitJoinOrPlacements{}, false
		}
		var out splitJoinOrPlacements
		for _, branch := range branches {
			placements, ok := splitMultiJoinOrBranchPlacementsForRelation(branch, relations, conditions, relation, resolver)
			if !ok || !placements.hasAny() {
				return splitJoinOrPlacements{}, false
			}
			out.append(placements)
		}
		return out, true
	case And:
		if placements, ok := splitMultiJoinOrFilterPlacementsForRelation(p.Left, relations, conditions, relation, resolver); ok && placements.hasAny() {
			return placements, true
		}
		if placements, ok := splitMultiJoinOrFilterPlacementsForRelation(p.Right, relations, conditions, relation, resolver); ok && placements.hasAny() {
			return placements, true
		}
	}
	return splitJoinOrPlacements{}, false
}

func multiJoinOrBranches(pred Predicate) []Predicate {
	if p, ok := pred.(Or); ok {
		left := multiJoinOrBranches(p.Left)
		right := multiJoinOrBranches(p.Right)
		return append(left, right...)
	}
	return []Predicate{pred}
}

func splitMultiJoinOrBranchPlacementsForRelation(
	branch Predicate,
	relations []MultiJoinRelation,
	conditions []MultiJoinCondition,
	relation int,
	resolver IndexResolver,
) (splitJoinOrPlacements, bool) {
	branchFilters := multiJoinBranchLocalFilterPlacements(branch, relations)
	if len(branchFilters) == 0 {
		if placements := multiJoinBranchColumnEqualityPlacements(branch, relations, relation, resolver); placements.hasAny() {
			return placements, true
		}
		return splitJoinOrPlacements{}, false
	}
	if filters := branchFilters[relation]; filters.hasAny() {
		return splitJoinOrPlacements{
			eqs:    filters.eqs,
			ranges: filters.ranges,
		}, true
	}

	var out splitJoinOrPlacements
	branchConditions := multiJoinBranchColumnEqualityConditions(branch, relations)
	filterEdgeConditions := append(slices.Clone(conditions), branchConditions...)
	for targetRelation, filters := range branchFilters {
		if targetRelation == relation || !filters.hasAny() {
			continue
		}
		out.append(multiJoinFilterEdgesBetweenRelations(filterEdgeConditions, relation, targetRelation, filters, resolver))
	}
	if !out.hasAny() {
		out.append(multiJoinColumnEqualityPlacementsForConditions(branchConditions, relation, resolver))
	}
	return out, out.hasAny()
}

func multiJoinBranchColumnEqualityPlacements(
	branch Predicate,
	relations []MultiJoinRelation,
	relation int,
	resolver IndexResolver,
) splitJoinOrPlacements {
	conditions := multiJoinBranchColumnEqualityConditions(branch, relations)
	if len(conditions) == 0 {
		return splitJoinOrPlacements{}
	}
	return multiJoinColumnEqualityPlacementsForConditions(conditions, relation, resolver)
}

func multiJoinColumnEqualityPlacementsForConditions(
	conditions []MultiJoinCondition,
	relation int,
	resolver IndexResolver,
) splitJoinOrPlacements {
	edges := multiJoinExistenceEdgesForRelation(conditions, relation, resolver)
	if len(edges) == 0 {
		return splitJoinOrPlacements{}
	}
	return splitJoinOrPlacements{existenceEdges: edges}
}

func multiJoinBranchColumnEqualityConditions(pred Predicate, relations []MultiJoinRelation) []MultiJoinCondition {
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
		left := multiJoinBranchColumnEqualityConditions(p.Left, relations)
		right := multiJoinBranchColumnEqualityConditions(p.Right, relations)
		return append(left, right...)
	default:
		return nil
	}
}

func multiJoinBranchLocalFilterPlacements(pred Predicate, relations []MultiJoinRelation) map[int]colFilterPlacements {
	switch p := pred.(type) {
	case ColEq:
		relation, ok := multiJoinFilterRelationIndex(relations, p.Table, p.Alias)
		if !ok {
			return nil
		}
		return map[int]colFilterPlacements{
			relation: {eqs: []ColEq{p}},
		}
	case ColRange:
		if !rangeHasBound(p) {
			return nil
		}
		relation, ok := multiJoinFilterRelationIndex(relations, p.Table, p.Alias)
		if !ok {
			return nil
		}
		return map[int]colFilterPlacements{
			relation: {ranges: []ColRange{p}},
		}
	case ColNe:
		relation, ok := multiJoinFilterRelationIndex(relations, p.Table, p.Alias)
		if !ok {
			return nil
		}
		return map[int]colFilterPlacements{
			relation: {ranges: colNeRanges(p)},
		}
	case And:
		return mergeMultiJoinBranchLocalFilterMaps(
			multiJoinBranchLocalFilterPlacements(p.Left, relations),
			multiJoinBranchLocalFilterPlacements(p.Right, relations),
		)
	default:
		return nil
	}
}

func mergeMultiJoinBranchLocalFilterMaps(left, right map[int]colFilterPlacements) map[int]colFilterPlacements {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	out := make(map[int]colFilterPlacements, len(left)+len(right))
	for relation, filters := range left {
		out[relation] = mergeColFilterPlacements(out[relation], filters)
	}
	for relation, filters := range right {
		out[relation] = mergeColFilterPlacements(out[relation], filters)
	}
	return out
}

func multiJoinFilterEdgesBetweenRelations(
	conditions []MultiJoinCondition,
	lhsRelation int,
	rhsRelation int,
	filters colFilterPlacements,
	resolver IndexResolver,
) splitJoinOrPlacements {
	if resolver == nil {
		return splitJoinOrPlacements{}
	}
	var out splitJoinOrPlacements
	for _, path := range multiJoinFilterEdgeConditionPaths(conditions, lhsRelation, rhsRelation) {
		lhs := path.lhs
		rhs := path.rhs
		if _, ok := resolver.IndexIDForColumn(rhs.Table, rhs.Column); !ok {
			continue
		}
		for _, ce := range filters.eqs {
			out.edges = append(out.edges, joinEdgePlacement{
				edge: JoinEdge{
					LHSTable:     lhs.Table,
					RHSTable:     rhs.Table,
					LHSJoinCol:   lhs.Column,
					RHSJoinCol:   rhs.Column,
					RHSFilterCol: ce.Column,
				},
				value: ce.Value,
			})
		}
		for _, cr := range filters.ranges {
			out.rangeEdges = append(out.rangeEdges, joinRangeEdgePlacement{
				edge: JoinEdge{
					LHSTable:     lhs.Table,
					RHSTable:     rhs.Table,
					LHSJoinCol:   lhs.Column,
					RHSJoinCol:   rhs.Column,
					RHSFilterCol: cr.Column,
				},
				lower: cr.Lower,
				upper: cr.Upper,
			})
		}
	}
	for _, path := range multiJoinFilterEdgeTwoHopConditionPaths(conditions, lhsRelation, rhsRelation) {
		if path.midFirst.Column == path.midSecond.Column {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.midFirst.Table, path.midFirst.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.rhs.Table, path.rhs.Column); !ok {
			continue
		}
		edge := JoinPathEdge{
			LHSTable:     path.lhs.Table,
			MidTable:     path.midFirst.Table,
			RHSTable:     path.rhs.Table,
			LHSJoinCol:   path.lhs.Column,
			MidFirstCol:  path.midFirst.Column,
			MidSecondCol: path.midSecond.Column,
			RHSJoinCol:   path.rhs.Column,
			RHSFilterCol: 0,
		}
		for _, ce := range filters.eqs {
			edge.RHSFilterCol = ce.Column
			out.pathEdges = append(out.pathEdges, joinPathEdgePlacement{
				edge:  edge,
				value: ce.Value,
			})
		}
		for _, cr := range filters.ranges {
			edge.RHSFilterCol = cr.Column
			out.rangePathEdges = append(out.rangePathEdges, joinRangePathEdgePlacement{
				edge:  edge,
				lower: cr.Lower,
				upper: cr.Upper,
			})
		}
	}
	for _, path := range multiJoinFilterEdgeThreeHopConditionPaths(conditions, lhsRelation, rhsRelation) {
		if path.mid1First.Column == path.mid1Second.Column && path.mid2First.Column == path.mid2Second.Column {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid1First.Table, path.mid1First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid2First.Table, path.mid2First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.rhs.Table, path.rhs.Column); !ok {
			continue
		}
		edge := JoinPath3Edge{
			LHSTable:      path.lhs.Table,
			Mid1Table:     path.mid1First.Table,
			Mid2Table:     path.mid2First.Table,
			RHSTable:      path.rhs.Table,
			LHSJoinCol:    path.lhs.Column,
			Mid1FirstCol:  path.mid1First.Column,
			Mid1SecondCol: path.mid1Second.Column,
			Mid2FirstCol:  path.mid2First.Column,
			Mid2SecondCol: path.mid2Second.Column,
			RHSJoinCol:    path.rhs.Column,
			RHSFilterCol:  0,
		}
		for _, ce := range filters.eqs {
			edge.RHSFilterCol = ce.Column
			out.path3Edges = append(out.path3Edges, joinPath3EdgePlacement{
				edge:  edge,
				value: ce.Value,
			})
		}
		for _, cr := range filters.ranges {
			edge.RHSFilterCol = cr.Column
			out.rangePath3Edges = append(out.rangePath3Edges, joinRangePath3EdgePlacement{
				edge:  edge,
				lower: cr.Lower,
				upper: cr.Upper,
			})
		}
	}
	for _, path := range multiJoinFilterEdgeFourHopConditionPaths(conditions, lhsRelation, rhsRelation) {
		if path.mid1First.Column == path.mid1Second.Column &&
			path.mid2First.Column == path.mid2Second.Column &&
			path.mid3First.Column == path.mid3Second.Column {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid1First.Table, path.mid1First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid2First.Table, path.mid2First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid3First.Table, path.mid3First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.rhs.Table, path.rhs.Column); !ok {
			continue
		}
		edge := JoinPath4Edge{
			LHSTable:      path.lhs.Table,
			Mid1Table:     path.mid1First.Table,
			Mid2Table:     path.mid2First.Table,
			Mid3Table:     path.mid3First.Table,
			RHSTable:      path.rhs.Table,
			LHSJoinCol:    path.lhs.Column,
			Mid1FirstCol:  path.mid1First.Column,
			Mid1SecondCol: path.mid1Second.Column,
			Mid2FirstCol:  path.mid2First.Column,
			Mid2SecondCol: path.mid2Second.Column,
			Mid3FirstCol:  path.mid3First.Column,
			Mid3SecondCol: path.mid3Second.Column,
			RHSJoinCol:    path.rhs.Column,
			RHSFilterCol:  0,
		}
		for _, ce := range filters.eqs {
			edge.RHSFilterCol = ce.Column
			out.path4Edges = append(out.path4Edges, joinPath4EdgePlacement{
				edge:  edge,
				value: ce.Value,
			})
		}
		for _, cr := range filters.ranges {
			edge.RHSFilterCol = cr.Column
			out.rangePath4Edges = append(out.rangePath4Edges, joinRangePath4EdgePlacement{
				edge:  edge,
				lower: cr.Lower,
				upper: cr.Upper,
			})
		}
	}
	for _, path := range multiJoinFilterEdgeFiveHopConditionPaths(conditions, lhsRelation, rhsRelation) {
		if path.mid1First.Column == path.mid1Second.Column &&
			path.mid2First.Column == path.mid2Second.Column &&
			path.mid3First.Column == path.mid3Second.Column &&
			path.mid4First.Column == path.mid4Second.Column {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid1First.Table, path.mid1First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid2First.Table, path.mid2First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid3First.Table, path.mid3First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid4First.Table, path.mid4First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.rhs.Table, path.rhs.Column); !ok {
			continue
		}
		edge := JoinPath5Edge{
			LHSTable:      path.lhs.Table,
			Mid1Table:     path.mid1First.Table,
			Mid2Table:     path.mid2First.Table,
			Mid3Table:     path.mid3First.Table,
			Mid4Table:     path.mid4First.Table,
			RHSTable:      path.rhs.Table,
			LHSJoinCol:    path.lhs.Column,
			Mid1FirstCol:  path.mid1First.Column,
			Mid1SecondCol: path.mid1Second.Column,
			Mid2FirstCol:  path.mid2First.Column,
			Mid2SecondCol: path.mid2Second.Column,
			Mid3FirstCol:  path.mid3First.Column,
			Mid3SecondCol: path.mid3Second.Column,
			Mid4FirstCol:  path.mid4First.Column,
			Mid4SecondCol: path.mid4Second.Column,
			RHSJoinCol:    path.rhs.Column,
			RHSFilterCol:  0,
		}
		for _, ce := range filters.eqs {
			edge.RHSFilterCol = ce.Column
			out.path5Edges = append(out.path5Edges, joinPath5EdgePlacement{
				edge:  edge,
				value: ce.Value,
			})
		}
		for _, cr := range filters.ranges {
			edge.RHSFilterCol = cr.Column
			out.rangePath5Edges = append(out.rangePath5Edges, joinRangePath5EdgePlacement{
				edge:  edge,
				lower: cr.Lower,
				upper: cr.Upper,
			})
		}
	}
	for _, path := range multiJoinFilterEdgeSixHopConditionPaths(conditions, lhsRelation, rhsRelation) {
		if path.mid1First.Column == path.mid1Second.Column &&
			path.mid2First.Column == path.mid2Second.Column &&
			path.mid3First.Column == path.mid3Second.Column &&
			path.mid4First.Column == path.mid4Second.Column &&
			path.mid5First.Column == path.mid5Second.Column {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid1First.Table, path.mid1First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid2First.Table, path.mid2First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid3First.Table, path.mid3First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid4First.Table, path.mid4First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.mid5First.Table, path.mid5First.Column); !ok {
			continue
		}
		if _, ok := resolver.IndexIDForColumn(path.rhs.Table, path.rhs.Column); !ok {
			continue
		}
		edge := JoinPath6Edge{
			LHSTable:      path.lhs.Table,
			Mid1Table:     path.mid1First.Table,
			Mid2Table:     path.mid2First.Table,
			Mid3Table:     path.mid3First.Table,
			Mid4Table:     path.mid4First.Table,
			Mid5Table:     path.mid5First.Table,
			RHSTable:      path.rhs.Table,
			LHSJoinCol:    path.lhs.Column,
			Mid1FirstCol:  path.mid1First.Column,
			Mid1SecondCol: path.mid1Second.Column,
			Mid2FirstCol:  path.mid2First.Column,
			Mid2SecondCol: path.mid2Second.Column,
			Mid3FirstCol:  path.mid3First.Column,
			Mid3SecondCol: path.mid3Second.Column,
			Mid4FirstCol:  path.mid4First.Column,
			Mid4SecondCol: path.mid4Second.Column,
			Mid5FirstCol:  path.mid5First.Column,
			Mid5SecondCol: path.mid5Second.Column,
			RHSJoinCol:    path.rhs.Column,
			RHSFilterCol:  0,
		}
		for _, ce := range filters.eqs {
			edge.RHSFilterCol = ce.Column
			out.path6Edges = append(out.path6Edges, joinPath6EdgePlacement{
				edge:  edge,
				value: ce.Value,
			})
		}
		for _, cr := range filters.ranges {
			edge.RHSFilterCol = cr.Column
			out.rangePath6Edges = append(out.rangePath6Edges, joinRangePath6EdgePlacement{
				edge:  edge,
				lower: cr.Lower,
				upper: cr.Upper,
			})
		}
	}
	return out
}

type multiJoinFilterEdgeConditionPath struct {
	lhs MultiJoinColumnRef
	rhs MultiJoinColumnRef
}

type multiJoinFilterEdgeTwoHopConditionPath struct {
	lhs       MultiJoinColumnRef
	midFirst  MultiJoinColumnRef
	midSecond MultiJoinColumnRef
	rhs       MultiJoinColumnRef
}

type multiJoinFilterEdgeThreeHopConditionPath struct {
	lhs        MultiJoinColumnRef
	mid1First  MultiJoinColumnRef
	mid1Second MultiJoinColumnRef
	mid2First  MultiJoinColumnRef
	mid2Second MultiJoinColumnRef
	rhs        MultiJoinColumnRef
}

type multiJoinFilterEdgeFourHopConditionPath struct {
	lhs        MultiJoinColumnRef
	mid1First  MultiJoinColumnRef
	mid1Second MultiJoinColumnRef
	mid2First  MultiJoinColumnRef
	mid2Second MultiJoinColumnRef
	mid3First  MultiJoinColumnRef
	mid3Second MultiJoinColumnRef
	rhs        MultiJoinColumnRef
}

type multiJoinFilterEdgeFiveHopConditionPath struct {
	lhs        MultiJoinColumnRef
	mid1First  MultiJoinColumnRef
	mid1Second MultiJoinColumnRef
	mid2First  MultiJoinColumnRef
	mid2Second MultiJoinColumnRef
	mid3First  MultiJoinColumnRef
	mid3Second MultiJoinColumnRef
	mid4First  MultiJoinColumnRef
	mid4Second MultiJoinColumnRef
	rhs        MultiJoinColumnRef
}

type multiJoinFilterEdgeSixHopConditionPath struct {
	lhs        MultiJoinColumnRef
	mid1First  MultiJoinColumnRef
	mid1Second MultiJoinColumnRef
	mid2First  MultiJoinColumnRef
	mid2Second MultiJoinColumnRef
	mid3First  MultiJoinColumnRef
	mid3Second MultiJoinColumnRef
	mid4First  MultiJoinColumnRef
	mid4Second MultiJoinColumnRef
	mid5First  MultiJoinColumnRef
	mid5Second MultiJoinColumnRef
	rhs        MultiJoinColumnRef
}

type multiJoinFilterEdgeConditionPathState struct {
	start   MultiJoinColumnRef
	current MultiJoinColumnRef
}

type multiJoinFilterEdgeConditionPathKey struct {
	relation    int
	column      ColID
	startColumn ColID
}

func multiJoinFilterEdgeConditionPaths(conditions []MultiJoinCondition, lhsRelation int, rhsRelation int) []multiJoinFilterEdgeConditionPath {
	if lhsRelation == rhsRelation {
		return nil
	}
	var paths []multiJoinFilterEdgeConditionPath
	var queue []multiJoinFilterEdgeConditionPathState
	seen := make(map[multiJoinFilterEdgeConditionPathKey]struct{})
	addPathOrState := func(start, current MultiJoinColumnRef) {
		if current.Relation == rhsRelation {
			paths = append(paths, multiJoinFilterEdgeConditionPath{lhs: start, rhs: current})
			return
		}
		key := multiJoinFilterEdgeConditionPathKey{
			relation:    current.Relation,
			column:      current.Column,
			startColumn: start.Column,
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		queue = append(queue, multiJoinFilterEdgeConditionPathState{start: start, current: current})
	}

	for _, condition := range conditions {
		start, current, ok := multiJoinConditionRefsFromRelation(condition, lhsRelation)
		if !ok || start.Relation == current.Relation {
			continue
		}
		addPathOrState(start, current)
	}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		for _, condition := range conditions {
			current, next, ok := multiJoinConditionRefsFromRelation(condition, state.current.Relation)
			if !ok || current.Relation == next.Relation || current.Column != state.current.Column {
				continue
			}
			addPathOrState(state.start, next)
		}
	}
	return paths
}

func multiJoinFilterEdgeTwoHopConditionPaths(conditions []MultiJoinCondition, lhsRelation int, rhsRelation int) []multiJoinFilterEdgeTwoHopConditionPath {
	if lhsRelation == rhsRelation {
		return nil
	}
	var paths []multiJoinFilterEdgeTwoHopConditionPath
	for _, first := range conditions {
		lhs, midFirst, ok := multiJoinConditionRefsFromRelation(first, lhsRelation)
		if !ok || lhs.Relation == midFirst.Relation {
			continue
		}
		for _, second := range conditions {
			midSecond, rhs, ok := multiJoinConditionRefsFromRelation(second, midFirst.Relation)
			if !ok || midSecond.Relation == rhs.Relation || rhs.Relation != rhsRelation {
				continue
			}
			paths = append(paths, multiJoinFilterEdgeTwoHopConditionPath{
				lhs:       lhs,
				midFirst:  midFirst,
				midSecond: midSecond,
				rhs:       rhs,
			})
		}
	}
	return paths
}

func multiJoinFilterEdgeThreeHopConditionPaths(conditions []MultiJoinCondition, lhsRelation int, rhsRelation int) []multiJoinFilterEdgeThreeHopConditionPath {
	if lhsRelation == rhsRelation {
		return nil
	}
	var paths []multiJoinFilterEdgeThreeHopConditionPath
	for _, first := range conditions {
		lhs, mid1First, ok := multiJoinConditionRefsFromRelation(first, lhsRelation)
		if !ok || lhs.Relation == mid1First.Relation {
			continue
		}
		for _, second := range conditions {
			mid1Second, mid2First, ok := multiJoinConditionRefsFromRelation(second, mid1First.Relation)
			if !ok || mid1Second.Relation == mid2First.Relation || mid2First.Relation == lhsRelation {
				continue
			}
			for _, third := range conditions {
				mid2Second, rhs, ok := multiJoinConditionRefsFromRelation(third, mid2First.Relation)
				if !ok || mid2Second.Relation == rhs.Relation || rhs.Relation != rhsRelation {
					continue
				}
				paths = append(paths, multiJoinFilterEdgeThreeHopConditionPath{
					lhs:        lhs,
					mid1First:  mid1First,
					mid1Second: mid1Second,
					mid2First:  mid2First,
					mid2Second: mid2Second,
					rhs:        rhs,
				})
			}
		}
	}
	return paths
}

func multiJoinFilterEdgeFourHopConditionPaths(conditions []MultiJoinCondition, lhsRelation int, rhsRelation int) []multiJoinFilterEdgeFourHopConditionPath {
	if lhsRelation == rhsRelation {
		return nil
	}
	var paths []multiJoinFilterEdgeFourHopConditionPath
	for _, first := range conditions {
		lhs, mid1First, ok := multiJoinConditionRefsFromRelation(first, lhsRelation)
		if !ok || lhs.Relation == mid1First.Relation {
			continue
		}
		for _, second := range conditions {
			mid1Second, mid2First, ok := multiJoinConditionRefsFromRelation(second, mid1First.Relation)
			if !ok || mid1Second.Relation == mid2First.Relation || mid2First.Relation == lhsRelation {
				continue
			}
			for _, third := range conditions {
				mid2Second, mid3First, ok := multiJoinConditionRefsFromRelation(third, mid2First.Relation)
				if !ok || mid2Second.Relation == mid3First.Relation || mid3First.Relation == lhsRelation || mid3First.Relation == mid1First.Relation {
					continue
				}
				for _, fourth := range conditions {
					mid3Second, rhs, ok := multiJoinConditionRefsFromRelation(fourth, mid3First.Relation)
					if !ok || mid3Second.Relation == rhs.Relation || rhs.Relation != rhsRelation {
						continue
					}
					paths = append(paths, multiJoinFilterEdgeFourHopConditionPath{
						lhs:        lhs,
						mid1First:  mid1First,
						mid1Second: mid1Second,
						mid2First:  mid2First,
						mid2Second: mid2Second,
						mid3First:  mid3First,
						mid3Second: mid3Second,
						rhs:        rhs,
					})
				}
			}
		}
	}
	return paths
}

func multiJoinFilterEdgeFiveHopConditionPaths(conditions []MultiJoinCondition, lhsRelation int, rhsRelation int) []multiJoinFilterEdgeFiveHopConditionPath {
	if lhsRelation == rhsRelation {
		return nil
	}
	var paths []multiJoinFilterEdgeFiveHopConditionPath
	for _, first := range conditions {
		lhs, mid1First, ok := multiJoinConditionRefsFromRelation(first, lhsRelation)
		if !ok || lhs.Relation == mid1First.Relation {
			continue
		}
		for _, second := range conditions {
			mid1Second, mid2First, ok := multiJoinConditionRefsFromRelation(second, mid1First.Relation)
			if !ok || mid1Second.Relation == mid2First.Relation || mid2First.Relation == lhsRelation {
				continue
			}
			for _, third := range conditions {
				mid2Second, mid3First, ok := multiJoinConditionRefsFromRelation(third, mid2First.Relation)
				if !ok || mid2Second.Relation == mid3First.Relation || mid3First.Relation == lhsRelation || mid3First.Relation == mid1First.Relation {
					continue
				}
				for _, fourth := range conditions {
					mid3Second, mid4First, ok := multiJoinConditionRefsFromRelation(fourth, mid3First.Relation)
					if !ok || mid3Second.Relation == mid4First.Relation || mid4First.Relation == lhsRelation || mid4First.Relation == mid1First.Relation || mid4First.Relation == mid2First.Relation {
						continue
					}
					for _, fifth := range conditions {
						mid4Second, rhs, ok := multiJoinConditionRefsFromRelation(fifth, mid4First.Relation)
						if !ok || mid4Second.Relation == rhs.Relation || rhs.Relation != rhsRelation {
							continue
						}
						paths = append(paths, multiJoinFilterEdgeFiveHopConditionPath{
							lhs:        lhs,
							mid1First:  mid1First,
							mid1Second: mid1Second,
							mid2First:  mid2First,
							mid2Second: mid2Second,
							mid3First:  mid3First,
							mid3Second: mid3Second,
							mid4First:  mid4First,
							mid4Second: mid4Second,
							rhs:        rhs,
						})
					}
				}
			}
		}
	}
	return paths
}

func multiJoinFilterEdgeSixHopConditionPaths(conditions []MultiJoinCondition, lhsRelation int, rhsRelation int) []multiJoinFilterEdgeSixHopConditionPath {
	if lhsRelation == rhsRelation {
		return nil
	}
	var paths []multiJoinFilterEdgeSixHopConditionPath
	for _, first := range conditions {
		lhs, mid1First, ok := multiJoinConditionRefsFromRelation(first, lhsRelation)
		if !ok || lhs.Relation == mid1First.Relation {
			continue
		}
		for _, second := range conditions {
			mid1Second, mid2First, ok := multiJoinConditionRefsFromRelation(second, mid1First.Relation)
			if !ok || mid1Second.Relation == mid2First.Relation || mid2First.Relation == lhsRelation {
				continue
			}
			for _, third := range conditions {
				mid2Second, mid3First, ok := multiJoinConditionRefsFromRelation(third, mid2First.Relation)
				if !ok || mid2Second.Relation == mid3First.Relation || mid3First.Relation == lhsRelation || mid3First.Relation == mid1First.Relation {
					continue
				}
				for _, fourth := range conditions {
					mid3Second, mid4First, ok := multiJoinConditionRefsFromRelation(fourth, mid3First.Relation)
					if !ok || mid3Second.Relation == mid4First.Relation || mid4First.Relation == lhsRelation || mid4First.Relation == mid1First.Relation || mid4First.Relation == mid2First.Relation {
						continue
					}
					for _, fifth := range conditions {
						mid4Second, mid5First, ok := multiJoinConditionRefsFromRelation(fifth, mid4First.Relation)
						if !ok || mid4Second.Relation == mid5First.Relation || mid5First.Relation == lhsRelation || mid5First.Relation == mid1First.Relation || mid5First.Relation == mid2First.Relation || mid5First.Relation == mid3First.Relation {
							continue
						}
						for _, sixth := range conditions {
							mid5Second, rhs, ok := multiJoinConditionRefsFromRelation(sixth, mid5First.Relation)
							if !ok || mid5Second.Relation == rhs.Relation || rhs.Relation != rhsRelation {
								continue
							}
							paths = append(paths, multiJoinFilterEdgeSixHopConditionPath{
								lhs:        lhs,
								mid1First:  mid1First,
								mid1Second: mid1Second,
								mid2First:  mid2First,
								mid2Second: mid2Second,
								mid3First:  mid3First,
								mid3Second: mid3Second,
								mid4First:  mid4First,
								mid4Second: mid4Second,
								mid5First:  mid5First,
								mid5Second: mid5Second,
								rhs:        rhs,
							})
						}
					}
				}
			}
		}
	}
	return paths
}

func multiJoinConditionRefsFromRelation(condition MultiJoinCondition, relation int) (MultiJoinColumnRef, MultiJoinColumnRef, bool) {
	switch {
	case condition.Left.Relation == relation:
		return condition.Left, condition.Right, true
	case condition.Right.Relation == relation:
		return condition.Right, condition.Left, true
	default:
		return MultiJoinColumnRef{}, MultiJoinColumnRef{}, false
	}
}

func multiJoinFilterRelationIndex(relations []MultiJoinRelation, table TableID, alias uint8) (int, bool) {
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
		return 0, false
	}
	if tableCount > 1 && relations[match].Alias != alias {
		return 0, false
	}
	return match, true
}

func multiJoinFilterColumnRef(relations []MultiJoinRelation, table TableID, alias uint8, column ColID) (MultiJoinColumnRef, bool) {
	match, ok := multiJoinFilterRelationIndex(relations, table, alias)
	if !ok {
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

func mutateJoinPathEdgePlacement(idx *PruningIndexes, edge JoinPathEdge, value Value, hash QueryHash, add bool) {
	if add {
		idx.JoinPathEdge.Add(edge, value, hash)
		return
	}
	idx.JoinPathEdge.Remove(edge, value, hash)
}

func mutateJoinRangePathEdgePlacement(idx *PruningIndexes, edge JoinPathEdge, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.JoinRangePathEdge.Add(edge, lower, upper, hash)
		return
	}
	idx.JoinRangePathEdge.Remove(edge, lower, upper, hash)
}

func mutateJoinPath3EdgePlacement(idx *PruningIndexes, edge JoinPath3Edge, value Value, hash QueryHash, add bool) {
	if add {
		idx.JoinPath3Edge.Add(edge, value, hash)
		return
	}
	idx.JoinPath3Edge.Remove(edge, value, hash)
}

func mutateJoinRangePath3EdgePlacement(idx *PruningIndexes, edge JoinPath3Edge, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.JoinRangePath3Edge.Add(edge, lower, upper, hash)
		return
	}
	idx.JoinRangePath3Edge.Remove(edge, lower, upper, hash)
}

func mutateJoinPath4EdgePlacement(idx *PruningIndexes, edge JoinPath4Edge, value Value, hash QueryHash, add bool) {
	if add {
		idx.JoinPath4Edge.Add(edge, value, hash)
		return
	}
	idx.JoinPath4Edge.Remove(edge, value, hash)
}

func mutateJoinRangePath4EdgePlacement(idx *PruningIndexes, edge JoinPath4Edge, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.JoinRangePath4Edge.Add(edge, lower, upper, hash)
		return
	}
	idx.JoinRangePath4Edge.Remove(edge, lower, upper, hash)
}

func mutateJoinPath5EdgePlacement(idx *PruningIndexes, edge JoinPath5Edge, value Value, hash QueryHash, add bool) {
	if add {
		idx.JoinPath5Edge.Add(edge, value, hash)
		return
	}
	idx.JoinPath5Edge.Remove(edge, value, hash)
}

func mutateJoinRangePath5EdgePlacement(idx *PruningIndexes, edge JoinPath5Edge, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.JoinRangePath5Edge.Add(edge, lower, upper, hash)
		return
	}
	idx.JoinRangePath5Edge.Remove(edge, lower, upper, hash)
}

func mutateJoinPath6EdgePlacement(idx *PruningIndexes, edge JoinPath6Edge, value Value, hash QueryHash, add bool) {
	if add {
		idx.JoinPath6Edge.Add(edge, value, hash)
		return
	}
	idx.JoinPath6Edge.Remove(edge, value, hash)
}

func mutateJoinRangePath6EdgePlacement(idx *PruningIndexes, edge JoinPath6Edge, lower, upper Bound, hash QueryHash, add bool) {
	if add {
		idx.JoinRangePath6Edge.Add(edge, lower, upper, hash)
		return
	}
	idx.JoinRangePath6Edge.Remove(edge, lower, upper, hash)
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
	collectJoinPathEdgeCandidates(idx, table, rows, committed, resolver, func(h QueryHash) {
		set[h] = struct{}{}
	})
	collectJoinPath3EdgeCandidates(idx, table, rows, committed, resolver, func(h QueryHash) {
		set[h] = struct{}{}
	})
	collectJoinPath4EdgeCandidates(idx, table, rows, committed, resolver, func(h QueryHash) {
		set[h] = struct{}{}
	})
	collectJoinPath5EdgeCandidates(idx, table, rows, committed, resolver, func(h QueryHash) {
		set[h] = struct{}{}
	})
	collectJoinPath6EdgeCandidates(idx, table, rows, committed, resolver, func(h QueryHash) {
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

type joinPathEdgePlacement struct {
	edge  JoinPathEdge
	value Value
}

type joinRangePathEdgePlacement struct {
	edge  JoinPathEdge
	lower Bound
	upper Bound
}

type joinPath3EdgePlacement struct {
	edge  JoinPath3Edge
	value Value
}

type joinRangePath3EdgePlacement struct {
	edge  JoinPath3Edge
	lower Bound
	upper Bound
}

type joinPath4EdgePlacement struct {
	edge  JoinPath4Edge
	value Value
}

type joinRangePath4EdgePlacement struct {
	edge  JoinPath4Edge
	lower Bound
	upper Bound
}

type joinPath5EdgePlacement struct {
	edge  JoinPath5Edge
	value Value
}

type joinRangePath5EdgePlacement struct {
	edge  JoinPath5Edge
	lower Bound
	upper Bound
}

type joinPath6EdgePlacement struct {
	edge  JoinPath6Edge
	value Value
}

type joinRangePath6EdgePlacement struct {
	edge  JoinPath6Edge
	lower Bound
	upper Bound
}

type joinExistenceEdgePlacement struct {
	edge JoinEdge
}

type splitJoinOrPlacements struct {
	eqs             []ColEq
	ranges          []ColRange
	edges           []joinEdgePlacement
	rangeEdges      []joinRangeEdgePlacement
	pathEdges       []joinPathEdgePlacement
	rangePathEdges  []joinRangePathEdgePlacement
	path3Edges      []joinPath3EdgePlacement
	rangePath3Edges []joinRangePath3EdgePlacement
	path4Edges      []joinPath4EdgePlacement
	rangePath4Edges []joinRangePath4EdgePlacement
	path5Edges      []joinPath5EdgePlacement
	rangePath5Edges []joinRangePath5EdgePlacement
	path6Edges      []joinPath6EdgePlacement
	rangePath6Edges []joinRangePath6EdgePlacement
	existenceEdges  []joinExistenceEdgePlacement
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
	return len(p.eqs) > 0 || len(p.ranges) > 0 || len(p.edges) > 0 || len(p.rangeEdges) > 0 || len(p.pathEdges) > 0 || len(p.rangePathEdges) > 0 || len(p.path3Edges) > 0 || len(p.rangePath3Edges) > 0 || len(p.path4Edges) > 0 || len(p.rangePath4Edges) > 0 || len(p.path5Edges) > 0 || len(p.rangePath5Edges) > 0 || len(p.path6Edges) > 0 || len(p.rangePath6Edges) > 0 || len(p.existenceEdges) > 0
}

func (p *splitJoinOrPlacements) append(other splitJoinOrPlacements) {
	p.eqs = append(p.eqs, other.eqs...)
	p.ranges = append(p.ranges, other.ranges...)
	p.edges = append(p.edges, other.edges...)
	p.rangeEdges = append(p.rangeEdges, other.rangeEdges...)
	p.pathEdges = append(p.pathEdges, other.pathEdges...)
	p.rangePathEdges = append(p.rangePathEdges, other.rangePathEdges...)
	p.path3Edges = append(p.path3Edges, other.path3Edges...)
	p.rangePath3Edges = append(p.rangePath3Edges, other.rangePath3Edges...)
	p.path4Edges = append(p.path4Edges, other.path4Edges...)
	p.rangePath4Edges = append(p.rangePath4Edges, other.rangePath4Edges...)
	p.path5Edges = append(p.path5Edges, other.path5Edges...)
	p.rangePath5Edges = append(p.rangePath5Edges, other.rangePath5Edges...)
	p.path6Edges = append(p.path6Edges, other.path6Edges...)
	p.rangePath6Edges = append(p.rangePath6Edges, other.rangePath6Edges...)
	p.existenceEdges = append(p.existenceEdges, other.existenceEdges...)
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
	if !ok || !placements.hasAny() || !splitJoinOrHasRemotePlacement(placements) {
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
	case And:
		if left, ok := splitJoinOrPredicatePlacements(p.Left, side, resolver); ok && splitJoinOrHasRemotePlacement(left) {
			return left, true
		}
		if right, ok := splitJoinOrPredicatePlacements(p.Right, side, resolver); ok && splitJoinOrHasRemotePlacement(right) {
			return right, true
		}
		return splitJoinOrBranchPlacements(pred, side, resolver)
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
	case ColEqCol:
		placement, ok := splitJoinOrColumnEqualityExistencePlacement(p, side, resolver)
		if !ok {
			return splitJoinOrPlacements{}, false
		}
		return splitJoinOrPlacements{existenceEdges: []joinExistenceEdgePlacement{placement}}, true
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

func splitJoinOrHasRemotePlacement(p splitJoinOrPlacements) bool {
	return len(p.edges) > 0 || len(p.rangeEdges) > 0 || len(p.pathEdges) > 0 || len(p.rangePathEdges) > 0 || len(p.path3Edges) > 0 || len(p.rangePath3Edges) > 0 || len(p.path4Edges) > 0 || len(p.rangePath4Edges) > 0 || len(p.existenceEdges) > 0
}

func splitJoinOrColumnEqualityExistencePlacement(p ColEqCol, side joinPlacementSide, resolver IndexResolver) (joinExistenceEdgePlacement, bool) {
	var lhsCol, rhsCol ColID
	switch {
	case p.LeftTable == side.table && p.RightTable == side.other:
		lhsCol = p.LeftColumn
		rhsCol = p.RightColumn
	case p.RightTable == side.table && p.LeftTable == side.other:
		lhsCol = p.RightColumn
		rhsCol = p.LeftColumn
	default:
		return joinExistenceEdgePlacement{}, false
	}
	if resolver == nil {
		return joinExistenceEdgePlacement{}, false
	}
	if _, ok := resolver.IndexIDForColumn(side.other, rhsCol); !ok {
		return joinExistenceEdgePlacement{}, false
	}
	return joinExistenceEdgePlacement{edge: JoinEdge{
		LHSTable:     side.table,
		RHSTable:     side.other,
		LHSJoinCol:   lhsCol,
		RHSJoinCol:   rhsCol,
		RHSFilterCol: rhsCol,
	}}, true
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

func collectJoinPathEdgeCandidates(
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
	idx.JoinPathEdge.ForEachEdge(table, func(edge JoinPathEdge) {
		forEachJoinedPathRHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinPathEdge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePathEdge.ForEachEdge(table, func(edge JoinPathEdge) {
		forEachJoinedPathRHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinRangePathEdge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedPathRHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPathEdge,
	fn func(Value),
) {
	midIdx, ok := resolver.IndexIDForColumn(edge.MidTable, edge.MidFirstCol)
	if !ok {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, row := range rows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		midKey := store.NewIndexKey(row[edge.LHSJoinCol])
		midRowIDs := committed.IndexSeek(edge.MidTable, midIdx, midKey)
		for _, midRID := range midRowIDs {
			midRow, ok := committed.GetRow(edge.MidTable, midRID)
			if !ok || int(edge.MidSecondCol) >= len(midRow) {
				continue
			}
			rhsKey := store.NewIndexKey(midRow[edge.MidSecondCol])
			rhsRowIDs := committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey)
			for _, rhsRID := range rhsRowIDs {
				rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
				if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
					continue
				}
				fn(rhsRow[edge.RHSFilterCol])
			}
		}
	}
}

func collectJoinPath3EdgeCandidates(
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
	idx.JoinPath3Edge.ForEachEdge(table, func(edge JoinPath3Edge) {
		forEachJoinedPath3RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinPath3Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath3Edge.ForEachEdge(table, func(edge JoinPath3Edge) {
		forEachJoinedPath3RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath3Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedPath3RHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath3Edge,
	fn func(Value),
) {
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, row := range rows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			mid2Key := store.NewIndexKey(mid1Row[edge.Mid1SecondCol])
			for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
				mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
				if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
					continue
				}
				rhsKey := store.NewIndexKey(mid2Row[edge.Mid2SecondCol])
				for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
					rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
					if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
						continue
					}
					fn(rhsRow[edge.RHSFilterCol])
				}
			}
		}
	}
}

func collectJoinPath4EdgeCandidates(
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
	idx.JoinPath4Edge.ForEachEdge(table, func(edge JoinPath4Edge) {
		forEachJoinedPath4RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinPath4Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath4Edge.ForEachEdge(table, func(edge JoinPath4Edge) {
		forEachJoinedPath4RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath4Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedPath4RHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath4Edge,
	fn func(Value),
) {
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	mid3Idx, ok := resolver.IndexIDForColumn(edge.Mid3Table, edge.Mid3FirstCol)
	if !ok {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, row := range rows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			mid2Key := store.NewIndexKey(mid1Row[edge.Mid1SecondCol])
			for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
				mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
				if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
					continue
				}
				mid3Key := store.NewIndexKey(mid2Row[edge.Mid2SecondCol])
				for _, mid3RID := range committed.IndexSeek(edge.Mid3Table, mid3Idx, mid3Key) {
					mid3Row, ok := committed.GetRow(edge.Mid3Table, mid3RID)
					if !ok || int(edge.Mid3SecondCol) >= len(mid3Row) {
						continue
					}
					rhsKey := store.NewIndexKey(mid3Row[edge.Mid3SecondCol])
					for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
						rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
						if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
							continue
						}
						fn(rhsRow[edge.RHSFilterCol])
					}
				}
			}
		}
	}
}

func collectJoinPath5EdgeCandidates(
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
	idx.JoinPath5Edge.ForEachEdge(table, func(edge JoinPath5Edge) {
		forEachJoinedPath5RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinPath5Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath5Edge.ForEachEdge(table, func(edge JoinPath5Edge) {
		forEachJoinedPath5RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath5Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedPath5RHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	fn func(Value),
) {
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	mid3Idx, ok := resolver.IndexIDForColumn(edge.Mid3Table, edge.Mid3FirstCol)
	if !ok {
		return
	}
	mid4Idx, ok := resolver.IndexIDForColumn(edge.Mid4Table, edge.Mid4FirstCol)
	if !ok {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, row := range rows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			mid2Key := store.NewIndexKey(mid1Row[edge.Mid1SecondCol])
			for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
				mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
				if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
					continue
				}
				mid3Key := store.NewIndexKey(mid2Row[edge.Mid2SecondCol])
				for _, mid3RID := range committed.IndexSeek(edge.Mid3Table, mid3Idx, mid3Key) {
					mid3Row, ok := committed.GetRow(edge.Mid3Table, mid3RID)
					if !ok || int(edge.Mid3SecondCol) >= len(mid3Row) {
						continue
					}
					mid4Key := store.NewIndexKey(mid3Row[edge.Mid3SecondCol])
					for _, mid4RID := range committed.IndexSeek(edge.Mid4Table, mid4Idx, mid4Key) {
						mid4Row, ok := committed.GetRow(edge.Mid4Table, mid4RID)
						if !ok || int(edge.Mid4SecondCol) >= len(mid4Row) {
							continue
						}
						rhsKey := store.NewIndexKey(mid4Row[edge.Mid4SecondCol])
						for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
							rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
							if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
								continue
							}
							fn(rhsRow[edge.RHSFilterCol])
						}
					}
				}
			}
		}
	}
}

func collectJoinPath6EdgeCandidates(
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
	idx.JoinPath6Edge.ForEachEdge(table, func(edge JoinPath6Edge) {
		forEachJoinedPath6RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinPath6Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath6Edge.ForEachEdge(table, func(edge JoinPath6Edge) {
		forEachJoinedPath6RHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath6Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedPath6RHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	fn func(Value),
) {
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	mid3Idx, ok := resolver.IndexIDForColumn(edge.Mid3Table, edge.Mid3FirstCol)
	if !ok {
		return
	}
	mid4Idx, ok := resolver.IndexIDForColumn(edge.Mid4Table, edge.Mid4FirstCol)
	if !ok {
		return
	}
	mid5Idx, ok := resolver.IndexIDForColumn(edge.Mid5Table, edge.Mid5FirstCol)
	if !ok {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, row := range rows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			mid2Key := store.NewIndexKey(mid1Row[edge.Mid1SecondCol])
			for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
				mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
				if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
					continue
				}
				mid3Key := store.NewIndexKey(mid2Row[edge.Mid2SecondCol])
				for _, mid3RID := range committed.IndexSeek(edge.Mid3Table, mid3Idx, mid3Key) {
					mid3Row, ok := committed.GetRow(edge.Mid3Table, mid3RID)
					if !ok || int(edge.Mid3SecondCol) >= len(mid3Row) {
						continue
					}
					mid4Key := store.NewIndexKey(mid3Row[edge.Mid3SecondCol])
					for _, mid4RID := range committed.IndexSeek(edge.Mid4Table, mid4Idx, mid4Key) {
						mid4Row, ok := committed.GetRow(edge.Mid4Table, mid4RID)
						if !ok || int(edge.Mid4SecondCol) >= len(mid4Row) {
							continue
						}
						mid5Key := store.NewIndexKey(mid4Row[edge.Mid4SecondCol])
						for _, mid5RID := range committed.IndexSeek(edge.Mid5Table, mid5Idx, mid5Key) {
							mid5Row, ok := committed.GetRow(edge.Mid5Table, mid5RID)
							if !ok || int(edge.Mid5SecondCol) >= len(mid5Row) {
								continue
							}
							rhsKey := store.NewIndexKey(mid5Row[edge.Mid5SecondCol])
							for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
								rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
								if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
									continue
								}
								fn(rhsRow[edge.RHSFilterCol])
							}
						}
					}
				}
			}
		}
	}
}

func collectJoinFilterDeltaCandidates(
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
		forEachJoinedChangedRHSFilterValue(rows, changeset, edge, func(v Value) {
			idx.JoinEdge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangeEdge.ForEachEdge(table, func(edge JoinEdge) {
		forEachJoinedChangedRHSFilterValue(rows, changeset, edge, func(v Value) {
			idx.JoinRangeEdge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedRHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	edge JoinEdge,
	fn func(Value),
) {
	tc := changeset.Tables[edge.RHSTable]
	if tc == nil {
		return
	}
	lhsKeys := changedJoinKeySet(lhsRows, edge.LHSJoinCol)
	if len(lhsKeys) == 0 {
		return
	}
	forEachChangedRHSFilterValue(lhsKeys, edge, tc.Inserts, fn)
	forEachChangedRHSFilterValue(lhsKeys, edge, tc.Deletes, fn)
}

func changedJoinKeySet(rows []types.ProductValue, col ColID) map[valueKey]struct{} {
	keys := make(map[valueKey]struct{}, len(rows))
	for _, row := range rows {
		if int(col) >= len(row) {
			continue
		}
		keys[encodeValueKey(row[col])] = struct{}{}
	}
	return keys
}

func forEachChangedRHSFilterValue(
	lhsKeys map[valueKey]struct{},
	edge JoinEdge,
	rhsRows []types.ProductValue,
	fn func(Value),
) {
	for _, row := range rhsRows {
		if int(edge.RHSJoinCol) >= len(row) || int(edge.RHSFilterCol) >= len(row) {
			continue
		}
		if _, ok := lhsKeys[encodeValueKey(row[edge.RHSJoinCol])]; ok {
			fn(row[edge.RHSFilterCol])
		}
	}
}

func collectJoinPathFilterDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.JoinPathEdge.ForEachEdge(table, func(edge JoinPathEdge) {
		forEachJoinedChangedPathRHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinPathEdge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePathEdge.ForEachEdge(table, func(edge JoinPathEdge) {
		forEachJoinedChangedPathRHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinRangePathEdge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedPathRHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPathEdge,
	fn func(Value),
) {
	lhsKeys := changedJoinKeySet(lhsRows, edge.LHSJoinCol)
	if len(lhsKeys) == 0 {
		return
	}
	midValues := make(map[valueKey]Value)
	if midChanges := changeset.Tables[edge.MidTable]; midChanges != nil {
		collectChangedPathMidValues(lhsKeys, edge, midChanges.Inserts, midValues)
		collectChangedPathMidValues(lhsKeys, edge, midChanges.Deletes, midValues)
	}
	collectCommittedPathMidValues(lhsRows, committed, resolver, edge, midValues)
	if len(midValues) == 0 {
		return
	}
	if rhsChanges := changeset.Tables[edge.RHSTable]; rhsChanges != nil {
		forEachChangedPathRHSFilterValue(midValues, edge, rhsChanges.Inserts, fn)
		forEachChangedPathRHSFilterValue(midValues, edge, rhsChanges.Deletes, fn)
	}
	forEachCommittedPathRHSFilterValue(midValues, committed, resolver, edge, fn)
}

func collectChangedPathMidValues(
	lhsKeys map[valueKey]struct{},
	edge JoinPathEdge,
	midRows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range midRows {
		if int(edge.MidFirstCol) >= len(row) || int(edge.MidSecondCol) >= len(row) {
			continue
		}
		if _, ok := lhsKeys[encodeValueKey(row[edge.MidFirstCol])]; ok {
			out[encodeValueKey(row[edge.MidSecondCol])] = row[edge.MidSecondCol]
		}
	}
}

func collectCommittedPathMidValues(
	lhsRows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPathEdge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	midIdx, ok := resolver.IndexIDForColumn(edge.MidTable, edge.MidFirstCol)
	if !ok {
		return
	}
	for _, row := range lhsRows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		midKey := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, midRID := range committed.IndexSeek(edge.MidTable, midIdx, midKey) {
			midRow, ok := committed.GetRow(edge.MidTable, midRID)
			if !ok || int(edge.MidSecondCol) >= len(midRow) {
				continue
			}
			out[encodeValueKey(midRow[edge.MidSecondCol])] = midRow[edge.MidSecondCol]
		}
	}
}

func forEachChangedPathRHSFilterValue(
	midValues map[valueKey]Value,
	edge JoinPathEdge,
	rhsRows []types.ProductValue,
	fn func(Value),
) {
	for _, row := range rhsRows {
		if int(edge.RHSJoinCol) >= len(row) || int(edge.RHSFilterCol) >= len(row) {
			continue
		}
		if _, ok := midValues[encodeValueKey(row[edge.RHSJoinCol])]; ok {
			fn(row[edge.RHSFilterCol])
		}
	}
}

func forEachCommittedPathRHSFilterValue(
	midValues map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPathEdge,
	fn func(Value),
) {
	if committed == nil || resolver == nil {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, value := range midValues {
		rhsKey := store.NewIndexKey(value)
		for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
			rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
			if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
				continue
			}
			fn(rhsRow[edge.RHSFilterCol])
		}
	}
}

func collectJoinPath3FilterDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.JoinPath3Edge.ForEachEdge(table, func(edge JoinPath3Edge) {
		forEachJoinedChangedPath3RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinPath3Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath3Edge.ForEachEdge(table, func(edge JoinPath3Edge) {
		forEachJoinedChangedPath3RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath3Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedPath3RHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath3Edge,
	fn func(Value),
) {
	lhsKeys := changedJoinKeySet(lhsRows, edge.LHSJoinCol)
	if len(lhsKeys) == 0 {
		return
	}
	mid1Values := make(map[valueKey]Value)
	if mid1Changes := changeset.Tables[edge.Mid1Table]; mid1Changes != nil {
		collectChangedPath3Mid1Values(lhsKeys, edge, mid1Changes.Inserts, mid1Values)
		collectChangedPath3Mid1Values(lhsKeys, edge, mid1Changes.Deletes, mid1Values)
	}
	collectCommittedPath3Mid1Values(lhsRows, committed, resolver, edge, mid1Values)
	if len(mid1Values) == 0 {
		return
	}

	mid2Values := make(map[valueKey]Value)
	if mid2Changes := changeset.Tables[edge.Mid2Table]; mid2Changes != nil {
		collectChangedPath3Mid2Values(mid1Values, edge, mid2Changes.Inserts, mid2Values)
		collectChangedPath3Mid2Values(mid1Values, edge, mid2Changes.Deletes, mid2Values)
	}
	collectCommittedPath3Mid2Values(mid1Values, committed, resolver, edge, mid2Values)
	if len(mid2Values) == 0 {
		return
	}

	if rhsChanges := changeset.Tables[edge.RHSTable]; rhsChanges != nil {
		forEachChangedPath3RHSFilterValue(mid2Values, edge, rhsChanges.Inserts, fn)
		forEachChangedPath3RHSFilterValue(mid2Values, edge, rhsChanges.Deletes, fn)
	}
	forEachCommittedPath3RHSFilterValue(mid2Values, committed, resolver, edge, fn)
}

func collectChangedPath3Mid1Values(
	lhsKeys map[valueKey]struct{},
	edge JoinPath3Edge,
	mid1Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid1Rows {
		if int(edge.Mid1FirstCol) >= len(row) || int(edge.Mid1SecondCol) >= len(row) {
			continue
		}
		if _, ok := lhsKeys[encodeValueKey(row[edge.Mid1FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid1SecondCol])] = row[edge.Mid1SecondCol]
		}
	}
}

func collectCommittedPath3Mid1Values(
	lhsRows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath3Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	for _, row := range lhsRows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			out[encodeValueKey(mid1Row[edge.Mid1SecondCol])] = mid1Row[edge.Mid1SecondCol]
		}
	}
}

func collectChangedPath3Mid2Values(
	mid1Values map[valueKey]Value,
	edge JoinPath3Edge,
	mid2Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid2Rows {
		if int(edge.Mid2FirstCol) >= len(row) || int(edge.Mid2SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid1Values[encodeValueKey(row[edge.Mid2FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid2SecondCol])] = row[edge.Mid2SecondCol]
		}
	}
}

func collectCommittedPath3Mid2Values(
	mid1Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath3Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	for _, value := range mid1Values {
		mid2Key := store.NewIndexKey(value)
		for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
			mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
			if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
				continue
			}
			out[encodeValueKey(mid2Row[edge.Mid2SecondCol])] = mid2Row[edge.Mid2SecondCol]
		}
	}
}

func forEachChangedPath3RHSFilterValue(
	mid2Values map[valueKey]Value,
	edge JoinPath3Edge,
	rhsRows []types.ProductValue,
	fn func(Value),
) {
	for _, row := range rhsRows {
		if int(edge.RHSJoinCol) >= len(row) || int(edge.RHSFilterCol) >= len(row) {
			continue
		}
		if _, ok := mid2Values[encodeValueKey(row[edge.RHSJoinCol])]; ok {
			fn(row[edge.RHSFilterCol])
		}
	}
}

func forEachCommittedPath3RHSFilterValue(
	mid2Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath3Edge,
	fn func(Value),
) {
	if committed == nil || resolver == nil {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, value := range mid2Values {
		rhsKey := store.NewIndexKey(value)
		for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
			rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
			if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
				continue
			}
			fn(rhsRow[edge.RHSFilterCol])
		}
	}
}

func collectJoinPath4FilterDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.JoinPath4Edge.ForEachEdge(table, func(edge JoinPath4Edge) {
		forEachJoinedChangedPath4RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinPath4Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath4Edge.ForEachEdge(table, func(edge JoinPath4Edge) {
		forEachJoinedChangedPath4RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath4Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedPath4RHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath4Edge,
	fn func(Value),
) {
	lhsKeys := changedJoinKeySet(lhsRows, edge.LHSJoinCol)
	if len(lhsKeys) == 0 {
		return
	}
	mid1Values := make(map[valueKey]Value)
	if mid1Changes := changeset.Tables[edge.Mid1Table]; mid1Changes != nil {
		collectChangedPath4Mid1Values(lhsKeys, edge, mid1Changes.Inserts, mid1Values)
		collectChangedPath4Mid1Values(lhsKeys, edge, mid1Changes.Deletes, mid1Values)
	}
	collectCommittedPath4Mid1Values(lhsRows, committed, resolver, edge, mid1Values)
	if len(mid1Values) == 0 {
		return
	}

	mid2Values := make(map[valueKey]Value)
	if mid2Changes := changeset.Tables[edge.Mid2Table]; mid2Changes != nil {
		collectChangedPath4Mid2Values(mid1Values, edge, mid2Changes.Inserts, mid2Values)
		collectChangedPath4Mid2Values(mid1Values, edge, mid2Changes.Deletes, mid2Values)
	}
	collectCommittedPath4Mid2Values(mid1Values, committed, resolver, edge, mid2Values)
	if len(mid2Values) == 0 {
		return
	}

	mid3Values := make(map[valueKey]Value)
	if mid3Changes := changeset.Tables[edge.Mid3Table]; mid3Changes != nil {
		collectChangedPath4Mid3Values(mid2Values, edge, mid3Changes.Inserts, mid3Values)
		collectChangedPath4Mid3Values(mid2Values, edge, mid3Changes.Deletes, mid3Values)
	}
	collectCommittedPath4Mid3Values(mid2Values, committed, resolver, edge, mid3Values)
	if len(mid3Values) == 0 {
		return
	}

	if rhsChanges := changeset.Tables[edge.RHSTable]; rhsChanges != nil {
		forEachChangedPath4RHSFilterValue(mid3Values, edge, rhsChanges.Inserts, fn)
		forEachChangedPath4RHSFilterValue(mid3Values, edge, rhsChanges.Deletes, fn)
	}
	forEachCommittedPath4RHSFilterValue(mid3Values, committed, resolver, edge, fn)
}

func collectChangedPath4Mid1Values(
	lhsKeys map[valueKey]struct{},
	edge JoinPath4Edge,
	mid1Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid1Rows {
		if int(edge.Mid1FirstCol) >= len(row) || int(edge.Mid1SecondCol) >= len(row) {
			continue
		}
		if _, ok := lhsKeys[encodeValueKey(row[edge.Mid1FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid1SecondCol])] = row[edge.Mid1SecondCol]
		}
	}
}

func collectCommittedPath4Mid1Values(
	lhsRows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath4Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	for _, row := range lhsRows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			out[encodeValueKey(mid1Row[edge.Mid1SecondCol])] = mid1Row[edge.Mid1SecondCol]
		}
	}
}

func collectChangedPath4Mid2Values(
	mid1Values map[valueKey]Value,
	edge JoinPath4Edge,
	mid2Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid2Rows {
		if int(edge.Mid2FirstCol) >= len(row) || int(edge.Mid2SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid1Values[encodeValueKey(row[edge.Mid2FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid2SecondCol])] = row[edge.Mid2SecondCol]
		}
	}
}

func collectCommittedPath4Mid2Values(
	mid1Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath4Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	for _, value := range mid1Values {
		mid2Key := store.NewIndexKey(value)
		for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
			mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
			if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
				continue
			}
			out[encodeValueKey(mid2Row[edge.Mid2SecondCol])] = mid2Row[edge.Mid2SecondCol]
		}
	}
}

func collectChangedPath4Mid3Values(
	mid2Values map[valueKey]Value,
	edge JoinPath4Edge,
	mid3Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid3Rows {
		if int(edge.Mid3FirstCol) >= len(row) || int(edge.Mid3SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid2Values[encodeValueKey(row[edge.Mid3FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid3SecondCol])] = row[edge.Mid3SecondCol]
		}
	}
}

func collectCommittedPath4Mid3Values(
	mid2Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath4Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid3Idx, ok := resolver.IndexIDForColumn(edge.Mid3Table, edge.Mid3FirstCol)
	if !ok {
		return
	}
	for _, value := range mid2Values {
		mid3Key := store.NewIndexKey(value)
		for _, mid3RID := range committed.IndexSeek(edge.Mid3Table, mid3Idx, mid3Key) {
			mid3Row, ok := committed.GetRow(edge.Mid3Table, mid3RID)
			if !ok || int(edge.Mid3SecondCol) >= len(mid3Row) {
				continue
			}
			out[encodeValueKey(mid3Row[edge.Mid3SecondCol])] = mid3Row[edge.Mid3SecondCol]
		}
	}
}

func forEachChangedPath4RHSFilterValue(
	mid3Values map[valueKey]Value,
	edge JoinPath4Edge,
	rhsRows []types.ProductValue,
	fn func(Value),
) {
	for _, row := range rhsRows {
		if int(edge.RHSJoinCol) >= len(row) || int(edge.RHSFilterCol) >= len(row) {
			continue
		}
		if _, ok := mid3Values[encodeValueKey(row[edge.RHSJoinCol])]; ok {
			fn(row[edge.RHSFilterCol])
		}
	}
}

func forEachCommittedPath4RHSFilterValue(
	mid3Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath4Edge,
	fn func(Value),
) {
	if committed == nil || resolver == nil {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, value := range mid3Values {
		rhsKey := store.NewIndexKey(value)
		for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
			rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
			if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
				continue
			}
			fn(rhsRow[edge.RHSFilterCol])
		}
	}
}

func collectJoinPath5FilterDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.JoinPath5Edge.ForEachEdge(table, func(edge JoinPath5Edge) {
		forEachJoinedChangedPath5RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinPath5Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath5Edge.ForEachEdge(table, func(edge JoinPath5Edge) {
		forEachJoinedChangedPath5RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath5Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedPath5RHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	fn func(Value),
) {
	lhsKeys := changedJoinKeySet(lhsRows, edge.LHSJoinCol)
	if len(lhsKeys) == 0 {
		return
	}
	mid1Values := make(map[valueKey]Value)
	if mid1Changes := changeset.Tables[edge.Mid1Table]; mid1Changes != nil {
		collectChangedPath5Mid1Values(lhsKeys, edge, mid1Changes.Inserts, mid1Values)
		collectChangedPath5Mid1Values(lhsKeys, edge, mid1Changes.Deletes, mid1Values)
	}
	collectCommittedPath5Mid1Values(lhsRows, committed, resolver, edge, mid1Values)
	if len(mid1Values) == 0 {
		return
	}

	mid2Values := make(map[valueKey]Value)
	if mid2Changes := changeset.Tables[edge.Mid2Table]; mid2Changes != nil {
		collectChangedPath5Mid2Values(mid1Values, edge, mid2Changes.Inserts, mid2Values)
		collectChangedPath5Mid2Values(mid1Values, edge, mid2Changes.Deletes, mid2Values)
	}
	collectCommittedPath5Mid2Values(mid1Values, committed, resolver, edge, mid2Values)
	if len(mid2Values) == 0 {
		return
	}

	mid3Values := make(map[valueKey]Value)
	if mid3Changes := changeset.Tables[edge.Mid3Table]; mid3Changes != nil {
		collectChangedPath5Mid3Values(mid2Values, edge, mid3Changes.Inserts, mid3Values)
		collectChangedPath5Mid3Values(mid2Values, edge, mid3Changes.Deletes, mid3Values)
	}
	collectCommittedPath5Mid3Values(mid2Values, committed, resolver, edge, mid3Values)
	if len(mid3Values) == 0 {
		return
	}

	mid4Values := make(map[valueKey]Value)
	if mid4Changes := changeset.Tables[edge.Mid4Table]; mid4Changes != nil {
		collectChangedPath5Mid4Values(mid3Values, edge, mid4Changes.Inserts, mid4Values)
		collectChangedPath5Mid4Values(mid3Values, edge, mid4Changes.Deletes, mid4Values)
	}
	collectCommittedPath5Mid4Values(mid3Values, committed, resolver, edge, mid4Values)
	if len(mid4Values) == 0 {
		return
	}

	if rhsChanges := changeset.Tables[edge.RHSTable]; rhsChanges != nil {
		forEachChangedPath5RHSFilterValue(mid4Values, edge, rhsChanges.Inserts, fn)
		forEachChangedPath5RHSFilterValue(mid4Values, edge, rhsChanges.Deletes, fn)
	}
	forEachCommittedPath5RHSFilterValue(mid4Values, committed, resolver, edge, fn)
}

func collectChangedPath5Mid1Values(
	lhsKeys map[valueKey]struct{},
	edge JoinPath5Edge,
	mid1Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid1Rows {
		if int(edge.Mid1FirstCol) >= len(row) || int(edge.Mid1SecondCol) >= len(row) {
			continue
		}
		if _, ok := lhsKeys[encodeValueKey(row[edge.Mid1FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid1SecondCol])] = row[edge.Mid1SecondCol]
		}
	}
}

func collectCommittedPath5Mid1Values(
	lhsRows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	for _, row := range lhsRows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			out[encodeValueKey(mid1Row[edge.Mid1SecondCol])] = mid1Row[edge.Mid1SecondCol]
		}
	}
}

func collectChangedPath5Mid2Values(
	mid1Values map[valueKey]Value,
	edge JoinPath5Edge,
	mid2Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid2Rows {
		if int(edge.Mid2FirstCol) >= len(row) || int(edge.Mid2SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid1Values[encodeValueKey(row[edge.Mid2FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid2SecondCol])] = row[edge.Mid2SecondCol]
		}
	}
}

func collectCommittedPath5Mid2Values(
	mid1Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	for _, value := range mid1Values {
		mid2Key := store.NewIndexKey(value)
		for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
			mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
			if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
				continue
			}
			out[encodeValueKey(mid2Row[edge.Mid2SecondCol])] = mid2Row[edge.Mid2SecondCol]
		}
	}
}

func collectChangedPath5Mid3Values(
	mid2Values map[valueKey]Value,
	edge JoinPath5Edge,
	mid3Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid3Rows {
		if int(edge.Mid3FirstCol) >= len(row) || int(edge.Mid3SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid2Values[encodeValueKey(row[edge.Mid3FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid3SecondCol])] = row[edge.Mid3SecondCol]
		}
	}
}

func collectCommittedPath5Mid3Values(
	mid2Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid3Idx, ok := resolver.IndexIDForColumn(edge.Mid3Table, edge.Mid3FirstCol)
	if !ok {
		return
	}
	for _, value := range mid2Values {
		mid3Key := store.NewIndexKey(value)
		for _, mid3RID := range committed.IndexSeek(edge.Mid3Table, mid3Idx, mid3Key) {
			mid3Row, ok := committed.GetRow(edge.Mid3Table, mid3RID)
			if !ok || int(edge.Mid3SecondCol) >= len(mid3Row) {
				continue
			}
			out[encodeValueKey(mid3Row[edge.Mid3SecondCol])] = mid3Row[edge.Mid3SecondCol]
		}
	}
}

func collectChangedPath5Mid4Values(
	mid3Values map[valueKey]Value,
	edge JoinPath5Edge,
	mid4Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid4Rows {
		if int(edge.Mid4FirstCol) >= len(row) || int(edge.Mid4SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid3Values[encodeValueKey(row[edge.Mid4FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid4SecondCol])] = row[edge.Mid4SecondCol]
		}
	}
}

func collectCommittedPath5Mid4Values(
	mid3Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid4Idx, ok := resolver.IndexIDForColumn(edge.Mid4Table, edge.Mid4FirstCol)
	if !ok {
		return
	}
	for _, value := range mid3Values {
		mid4Key := store.NewIndexKey(value)
		for _, mid4RID := range committed.IndexSeek(edge.Mid4Table, mid4Idx, mid4Key) {
			mid4Row, ok := committed.GetRow(edge.Mid4Table, mid4RID)
			if !ok || int(edge.Mid4SecondCol) >= len(mid4Row) {
				continue
			}
			out[encodeValueKey(mid4Row[edge.Mid4SecondCol])] = mid4Row[edge.Mid4SecondCol]
		}
	}
}

func forEachChangedPath5RHSFilterValue(
	mid4Values map[valueKey]Value,
	edge JoinPath5Edge,
	rhsRows []types.ProductValue,
	fn func(Value),
) {
	for _, row := range rhsRows {
		if int(edge.RHSJoinCol) >= len(row) || int(edge.RHSFilterCol) >= len(row) {
			continue
		}
		if _, ok := mid4Values[encodeValueKey(row[edge.RHSJoinCol])]; ok {
			fn(row[edge.RHSFilterCol])
		}
	}
}

func forEachCommittedPath5RHSFilterValue(
	mid4Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath5Edge,
	fn func(Value),
) {
	if committed == nil || resolver == nil {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, value := range mid4Values {
		rhsKey := store.NewIndexKey(value)
		for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
			rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
			if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
				continue
			}
			fn(rhsRow[edge.RHSFilterCol])
		}
	}
}

func collectJoinPath6FilterDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.JoinPath6Edge.ForEachEdge(table, func(edge JoinPath6Edge) {
		forEachJoinedChangedPath6RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinPath6Edge.ForEachHash(edge, v, add)
		})
	})
	idx.JoinRangePath6Edge.ForEachEdge(table, func(edge JoinPath6Edge) {
		forEachJoinedChangedPath6RHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.JoinRangePath6Edge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedPath6RHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	fn func(Value),
) {
	lhsKeys := changedJoinKeySet(lhsRows, edge.LHSJoinCol)
	if len(lhsKeys) == 0 {
		return
	}
	mid1Values := make(map[valueKey]Value)
	if mid1Changes := changeset.Tables[edge.Mid1Table]; mid1Changes != nil {
		collectChangedPath6Mid1Values(lhsKeys, edge, mid1Changes.Inserts, mid1Values)
		collectChangedPath6Mid1Values(lhsKeys, edge, mid1Changes.Deletes, mid1Values)
	}
	collectCommittedPath6Mid1Values(lhsRows, committed, resolver, edge, mid1Values)
	if len(mid1Values) == 0 {
		return
	}

	mid2Values := make(map[valueKey]Value)
	if mid2Changes := changeset.Tables[edge.Mid2Table]; mid2Changes != nil {
		collectChangedPath6Mid2Values(mid1Values, edge, mid2Changes.Inserts, mid2Values)
		collectChangedPath6Mid2Values(mid1Values, edge, mid2Changes.Deletes, mid2Values)
	}
	collectCommittedPath6Mid2Values(mid1Values, committed, resolver, edge, mid2Values)
	if len(mid2Values) == 0 {
		return
	}

	mid3Values := make(map[valueKey]Value)
	if mid3Changes := changeset.Tables[edge.Mid3Table]; mid3Changes != nil {
		collectChangedPath6Mid3Values(mid2Values, edge, mid3Changes.Inserts, mid3Values)
		collectChangedPath6Mid3Values(mid2Values, edge, mid3Changes.Deletes, mid3Values)
	}
	collectCommittedPath6Mid3Values(mid2Values, committed, resolver, edge, mid3Values)
	if len(mid3Values) == 0 {
		return
	}

	mid4Values := make(map[valueKey]Value)
	if mid4Changes := changeset.Tables[edge.Mid4Table]; mid4Changes != nil {
		collectChangedPath6Mid4Values(mid3Values, edge, mid4Changes.Inserts, mid4Values)
		collectChangedPath6Mid4Values(mid3Values, edge, mid4Changes.Deletes, mid4Values)
	}
	collectCommittedPath6Mid4Values(mid3Values, committed, resolver, edge, mid4Values)
	if len(mid4Values) == 0 {
		return
	}

	mid5Values := make(map[valueKey]Value)
	if mid5Changes := changeset.Tables[edge.Mid5Table]; mid5Changes != nil {
		collectChangedPath6Mid5Values(mid4Values, edge, mid5Changes.Inserts, mid5Values)
		collectChangedPath6Mid5Values(mid4Values, edge, mid5Changes.Deletes, mid5Values)
	}
	collectCommittedPath6Mid5Values(mid4Values, committed, resolver, edge, mid5Values)
	if len(mid5Values) == 0 {
		return
	}

	if rhsChanges := changeset.Tables[edge.RHSTable]; rhsChanges != nil {
		forEachChangedPath6RHSFilterValue(mid5Values, edge, rhsChanges.Inserts, fn)
		forEachChangedPath6RHSFilterValue(mid5Values, edge, rhsChanges.Deletes, fn)
	}
	forEachCommittedPath6RHSFilterValue(mid5Values, committed, resolver, edge, fn)
}

func collectChangedPath6Mid1Values(
	lhsKeys map[valueKey]struct{},
	edge JoinPath6Edge,
	mid1Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid1Rows {
		if int(edge.Mid1FirstCol) >= len(row) || int(edge.Mid1SecondCol) >= len(row) {
			continue
		}
		if _, ok := lhsKeys[encodeValueKey(row[edge.Mid1FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid1SecondCol])] = row[edge.Mid1SecondCol]
		}
	}
}

func collectCommittedPath6Mid1Values(
	lhsRows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid1Idx, ok := resolver.IndexIDForColumn(edge.Mid1Table, edge.Mid1FirstCol)
	if !ok {
		return
	}
	for _, row := range lhsRows {
		if int(edge.LHSJoinCol) >= len(row) {
			continue
		}
		mid1Key := store.NewIndexKey(row[edge.LHSJoinCol])
		for _, mid1RID := range committed.IndexSeek(edge.Mid1Table, mid1Idx, mid1Key) {
			mid1Row, ok := committed.GetRow(edge.Mid1Table, mid1RID)
			if !ok || int(edge.Mid1SecondCol) >= len(mid1Row) {
				continue
			}
			out[encodeValueKey(mid1Row[edge.Mid1SecondCol])] = mid1Row[edge.Mid1SecondCol]
		}
	}
}

func collectChangedPath6Mid2Values(
	mid1Values map[valueKey]Value,
	edge JoinPath6Edge,
	mid2Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid2Rows {
		if int(edge.Mid2FirstCol) >= len(row) || int(edge.Mid2SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid1Values[encodeValueKey(row[edge.Mid2FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid2SecondCol])] = row[edge.Mid2SecondCol]
		}
	}
}

func collectCommittedPath6Mid2Values(
	mid1Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid2Idx, ok := resolver.IndexIDForColumn(edge.Mid2Table, edge.Mid2FirstCol)
	if !ok {
		return
	}
	for _, value := range mid1Values {
		mid2Key := store.NewIndexKey(value)
		for _, mid2RID := range committed.IndexSeek(edge.Mid2Table, mid2Idx, mid2Key) {
			mid2Row, ok := committed.GetRow(edge.Mid2Table, mid2RID)
			if !ok || int(edge.Mid2SecondCol) >= len(mid2Row) {
				continue
			}
			out[encodeValueKey(mid2Row[edge.Mid2SecondCol])] = mid2Row[edge.Mid2SecondCol]
		}
	}
}

func collectChangedPath6Mid3Values(
	mid2Values map[valueKey]Value,
	edge JoinPath6Edge,
	mid3Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid3Rows {
		if int(edge.Mid3FirstCol) >= len(row) || int(edge.Mid3SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid2Values[encodeValueKey(row[edge.Mid3FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid3SecondCol])] = row[edge.Mid3SecondCol]
		}
	}
}

func collectCommittedPath6Mid3Values(
	mid2Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid3Idx, ok := resolver.IndexIDForColumn(edge.Mid3Table, edge.Mid3FirstCol)
	if !ok {
		return
	}
	for _, value := range mid2Values {
		mid3Key := store.NewIndexKey(value)
		for _, mid3RID := range committed.IndexSeek(edge.Mid3Table, mid3Idx, mid3Key) {
			mid3Row, ok := committed.GetRow(edge.Mid3Table, mid3RID)
			if !ok || int(edge.Mid3SecondCol) >= len(mid3Row) {
				continue
			}
			out[encodeValueKey(mid3Row[edge.Mid3SecondCol])] = mid3Row[edge.Mid3SecondCol]
		}
	}
}

func collectChangedPath6Mid4Values(
	mid3Values map[valueKey]Value,
	edge JoinPath6Edge,
	mid4Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid4Rows {
		if int(edge.Mid4FirstCol) >= len(row) || int(edge.Mid4SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid3Values[encodeValueKey(row[edge.Mid4FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid4SecondCol])] = row[edge.Mid4SecondCol]
		}
	}
}

func collectCommittedPath6Mid4Values(
	mid3Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid4Idx, ok := resolver.IndexIDForColumn(edge.Mid4Table, edge.Mid4FirstCol)
	if !ok {
		return
	}
	for _, value := range mid3Values {
		mid4Key := store.NewIndexKey(value)
		for _, mid4RID := range committed.IndexSeek(edge.Mid4Table, mid4Idx, mid4Key) {
			mid4Row, ok := committed.GetRow(edge.Mid4Table, mid4RID)
			if !ok || int(edge.Mid4SecondCol) >= len(mid4Row) {
				continue
			}
			out[encodeValueKey(mid4Row[edge.Mid4SecondCol])] = mid4Row[edge.Mid4SecondCol]
		}
	}
}

func collectChangedPath6Mid5Values(
	mid4Values map[valueKey]Value,
	edge JoinPath6Edge,
	mid5Rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range mid5Rows {
		if int(edge.Mid5FirstCol) >= len(row) || int(edge.Mid5SecondCol) >= len(row) {
			continue
		}
		if _, ok := mid4Values[encodeValueKey(row[edge.Mid5FirstCol])]; ok {
			out[encodeValueKey(row[edge.Mid5SecondCol])] = row[edge.Mid5SecondCol]
		}
	}
}

func collectCommittedPath6Mid5Values(
	mid4Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	mid5Idx, ok := resolver.IndexIDForColumn(edge.Mid5Table, edge.Mid5FirstCol)
	if !ok {
		return
	}
	for _, value := range mid4Values {
		mid5Key := store.NewIndexKey(value)
		for _, mid5RID := range committed.IndexSeek(edge.Mid5Table, mid5Idx, mid5Key) {
			mid5Row, ok := committed.GetRow(edge.Mid5Table, mid5RID)
			if !ok || int(edge.Mid5SecondCol) >= len(mid5Row) {
				continue
			}
			out[encodeValueKey(mid5Row[edge.Mid5SecondCol])] = mid5Row[edge.Mid5SecondCol]
		}
	}
}

func forEachChangedPath6RHSFilterValue(
	mid5Values map[valueKey]Value,
	edge JoinPath6Edge,
	rhsRows []types.ProductValue,
	fn func(Value),
) {
	for _, row := range rhsRows {
		if int(edge.RHSJoinCol) >= len(row) || int(edge.RHSFilterCol) >= len(row) {
			continue
		}
		if _, ok := mid5Values[encodeValueKey(row[edge.RHSJoinCol])]; ok {
			fn(row[edge.RHSFilterCol])
		}
	}
}

func forEachCommittedPath6RHSFilterValue(
	mid5Values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge JoinPath6Edge,
	fn func(Value),
) {
	if committed == nil || resolver == nil {
		return
	}
	rhsIdx, ok := resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
	if !ok {
		return
	}
	for _, value := range mid5Values {
		rhsKey := store.NewIndexKey(value)
		for _, rhsRID := range committed.IndexSeek(edge.RHSTable, rhsIdx, rhsKey) {
			rhsRow, ok := committed.GetRow(edge.RHSTable, rhsRID)
			if !ok || int(edge.RHSFilterCol) >= len(rhsRow) {
				continue
			}
			fn(rhsRow[edge.RHSFilterCol])
		}
	}
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
