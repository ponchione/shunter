package protocol

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func copyCompiledSQLMultiJoin(in *compiledSQLMultiJoin) *compiledSQLMultiJoin {
	if in == nil {
		return nil
	}
	out := *in
	out.Relations = slices.Clone(in.Relations)
	out.Conditions = slices.Clone(in.Conditions)
	if in.Filter != nil {
		out.Filter = copyCompiledSQLMultiPredicate(in.Filter)
	}
	return &out
}

func copyCompiledSQLMultiPredicate(in *compiledSQLMultiPredicate) *compiledSQLMultiPredicate {
	if in == nil {
		return nil
	}
	out := *in
	out.Left = copyCompiledSQLMultiPredicate(in.Left)
	out.Right = copyCompiledSQLMultiPredicate(in.Right)
	return &out
}

func (m *compiledSQLMultiJoin) referencedTables() []schema.TableID {
	if m == nil {
		return nil
	}
	out := make([]schema.TableID, 0, len(m.Relations))
	seen := make(map[schema.TableID]struct{}, len(m.Relations))
	for _, rel := range m.Relations {
		if _, ok := seen[rel.Table]; ok {
			continue
		}
		seen[rel.Table] = struct{}{}
		out = append(out, rel.Table)
	}
	return out
}

func subscriptionMultiJoinPredicate(multi *compiledSQLMultiJoin) (subscription.Predicate, error) {
	if multi == nil {
		return nil, fmt.Errorf("multi-join metadata must not be nil")
	}
	relations := make([]subscription.MultiJoinRelation, len(multi.Relations))
	for i, rel := range multi.Relations {
		relations[i] = subscription.MultiJoinRelation{Table: rel.Table, Alias: rel.Alias}
	}
	conditions := make([]subscription.MultiJoinCondition, len(multi.Conditions))
	for i, condition := range multi.Conditions {
		conditions[i] = subscription.MultiJoinCondition{
			Left:  subscriptionMultiJoinColumnRef(condition.Left),
			Right: subscriptionMultiJoinColumnRef(condition.Right),
		}
	}
	projectedTable := schema.TableID(0)
	if multi.ProjectedRelation >= 0 && multi.ProjectedRelation < len(multi.Relations) {
		projectedTable = multi.Relations[multi.ProjectedRelation].Table
	}
	filter, err := compiledMultiPredicateToSubscription(multi.Filter, projectedTable)
	if err != nil {
		return nil, err
	}
	for _, rel := range multi.Relations {
		filter = andSubscriptionPredicates(filter, rel.Visibility)
	}
	return subscription.MultiJoin{
		Relations:         relations,
		Conditions:        conditions,
		ProjectedRelation: multi.ProjectedRelation,
		Filter:            filter,
	}, nil
}

func subscriptionMultiJoinColumnRef(ref compiledSQLMultiColumnRef) subscription.MultiJoinColumnRef {
	return subscription.MultiJoinColumnRef{
		Relation: ref.Relation,
		Table:    ref.Column.table,
		Column:   ref.Column.column,
		Alias:    ref.Column.alias,
	}
}

func compiledMultiPredicateToSubscription(pred *compiledSQLMultiPredicate, noRowsTable schema.TableID) (subscription.Predicate, error) {
	if pred == nil {
		return nil, nil
	}
	switch pred.Kind {
	case compiledSQLMultiPredicateTrue:
		return nil, nil
	case compiledSQLMultiPredicateFalse:
		return subscription.NoRows{Table: noRowsTable}, nil
	case compiledSQLMultiPredicateComparison:
		col := pred.Column.Column
		return normalizePredicate(col.table, int(col.column), col.alias, pred.Op, pred.Value)
	case compiledSQLMultiPredicateColumnComparison:
		left := pred.LeftColumn.Column
		right := pred.RightColumn.Column
		return subscription.ColEqCol{
			LeftTable:   left.table,
			LeftColumn:  left.column,
			LeftAlias:   left.alias,
			RightTable:  right.table,
			RightColumn: right.column,
			RightAlias:  right.alias,
		}, nil
	case compiledSQLMultiPredicateAnd:
		left, err := compiledMultiPredicateToSubscription(pred.Left, noRowsTable)
		if err != nil {
			return nil, err
		}
		right, err := compiledMultiPredicateToSubscription(pred.Right, noRowsTable)
		if err != nil {
			return nil, err
		}
		return andSubscriptionPredicates(left, right), nil
	case compiledSQLMultiPredicateOr:
		left, err := compiledMultiPredicateToSubscription(pred.Left, noRowsTable)
		if err != nil {
			return nil, err
		}
		right, err := compiledMultiPredicateToSubscription(pred.Right, noRowsTable)
		if err != nil {
			return nil, err
		}
		if left == nil || right == nil {
			return nil, nil
		}
		return orSubscriptionPredicates(left, right), nil
	default:
		return nil, fmt.Errorf("unsupported multi-join predicate kind %d", pred.Kind)
	}
}

func compileMultiJoinSQLQuery(stmt sql.Statement, orderBy []sql.OrderByColumn, normalizedPredicate sql.Predicate, usesCallerIdentity bool, sqlText string, sl SchemaLookup, caller *types.Identity, allowProjection bool) (compiledSQLQuery, error) {
	relations, relationMap, aliasTags, err := compileMultiJoinRelations(stmt, sl)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	aliasTag := func(qualifier string) uint8 { return aliasTags[qualifier] }
	conditions, err := compileMultiJoinConditions(stmt.Joins, relationMap, relations, aliasTag)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if _, err := compileSQLMultiPredicateForRelations(stmt.Predicate, relationMap, relations, aliasTag, caller); err != nil {
		return compiledSQLQuery{}, err
	}
	filter, err := compileSQLMultiPredicateForRelations(normalizedPredicate, relationMap, relations, aliasTag, caller)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	projectedRelation, _, err := resolveMultiJoinProjectedRelation(stmt, relations, relationMap, aliasTags)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if stmt.ProjectedAlias == "" && len(stmt.ProjectionColumns) == 0 && stmt.Aggregate == nil {
		return compiledSQLQuery{}, fmt.Errorf("SELECT * is not supported for joins")
	}
	projectionColumns, err := compileMultiJoinProjectionColumns(stmt.ProjectionColumns, relationMap, aliasTag)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if !allowProjection && len(stmt.ProjectionColumns) != 0 {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
	}
	aggregate, err := compileMultiJoinAggregateProjection(stmt.Aggregate, relationMap, aliasTag)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if !allowProjection && stmt.Aggregate != nil {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
	}
	var compiledOrder []compiledSQLOrderBy
	if stmt.Aggregate != nil {
		compiledOrder, err = compileAggregateOrderBy(orderBy, aggregate, stmt.ProjectedTable)
	} else {
		compiledOrder, err = compileMultiJoinOrderBy(orderBy, stmt, relations, relationMap, projectionColumns, projectedRelation)
	}
	if err != nil {
		return compiledSQLQuery{}, err
	}
	limit, err := compileStatementLimit(stmt, sqlText)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	offset, err := compileStatementOffset(stmt, sqlText)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if !allowProjection {
		for _, condition := range conditions {
			left := condition.Left.Column
			right := condition.Right.Column
			if !sl.HasIndex(left.table, left.column) && !sl.HasIndex(right.table, right.column) {
				//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
				return compiledSQLQuery{}, fmt.Errorf("Subscriptions require indexes on join columns")
			}
		}
	}
	multi := &compiledSQLMultiJoin{
		Relations:         relations,
		Conditions:        conditions,
		Filter:            filter,
		ProjectedRelation: projectedRelation,
	}
	pred, err := subscriptionMultiJoinPredicate(multi)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	return compiledSQLQuery{
		TableName:          stmt.ProjectedTable,
		Predicate:          pred,
		UsesCallerIdentity: usesCallerIdentity,
		MultiJoin:          multi,
		ProjectionColumns:  projectionColumns,
		Aggregate:          aggregate,
		OrderBy:            compiledOrder,
		OrderByPresent:     len(orderBy) != 0,
		Limit:              limit,
		Offset:             offset,
	}, nil
}

func compileMultiJoinRelations(stmt sql.Statement, sl SchemaLookup) ([]compiledSQLMultiJoinRelation, map[string]relationSchema, map[string]uint8, error) {
	if len(stmt.Joins) > 255 {
		return nil, nil, nil, fmt.Errorf("multi-way join supports at most 256 relations")
	}
	leftID, leftTS, ok := lookupSQLTableExact(sl, stmt.Table)
	if !ok {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return nil, nil, nil, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.Table)
	}
	relations := []compiledSQLMultiJoinRelation{{
		Table:     leftID,
		Alias:     0,
		Qualifier: stmt.TableAlias,
		Schema:    leftTS,
	}}
	relationMap := map[string]relationSchema{stmt.TableAlias: {id: leftID, ts: leftTS}}
	aliasTags := map[string]uint8{stmt.TableAlias: 0}
	for _, join := range stmt.Joins {
		if join.AliasCollision {
			return nil, nil, nil, sql.DuplicateNameError{Name: join.RightAlias}
		}
		rightID, rightTS, ok := lookupSQLTableExact(sl, join.RightTable)
		if !ok {
			//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
			return nil, nil, nil, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", join.RightTable)
		}
		alias := uint8(len(relations))
		relations = append(relations, compiledSQLMultiJoinRelation{
			Table:     rightID,
			Alias:     alias,
			Qualifier: join.RightAlias,
			Schema:    rightTS,
		})
		relationMap[join.RightAlias] = relationSchema{id: rightID, ts: rightTS}
		aliasTags[join.RightAlias] = alias
	}
	return relations, relationMap, aliasTags, nil
}

func compileMultiJoinConditions(joins []sql.JoinClause, relationMap map[string]relationSchema, relations []compiledSQLMultiJoinRelation, aliasTag func(string) uint8) ([]compiledSQLMultiJoinCondition, error) {
	conditions := make([]compiledSQLMultiJoinCondition, 0, len(joins))
	for _, join := range joins {
		if !join.HasOn {
			continue
		}
		left, err := compileSQLMultiColumnRefForRelations(join.LeftOn, relationMap, relations, aliasTag)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLMultiColumnRefForRelations(join.RightOn, relationMap, relations, aliasTag)
		if err != nil {
			return nil, err
		}
		if left.Relation == right.Relation {
			return nil, fmt.Errorf("JOIN ON must compare columns from different relations")
		}
		if left.Column.schema.Type != right.Column.schema.Type {
			return nil, sql.UnexpectedTypeError{
				Expected: sql.AlgebraicName(right.Column.schema.Type),
				Inferred: sql.AlgebraicName(left.Column.schema.Type),
			}
		}
		if isArrayKind(left.Column.schema.Type) {
			return nil, sql.InvalidOpError{
				Op:   "=",
				Type: sql.AlgebraicName(left.Column.schema.Type),
			}
		}
		conditions = append(conditions, compiledSQLMultiJoinCondition{Left: left, Right: right})
	}
	return conditions, nil
}

func resolveMultiJoinProjectedRelation(stmt sql.Statement, relations []compiledSQLMultiJoinRelation, relationMap map[string]relationSchema, aliasTags map[string]uint8) (int, schema.TableID, error) {
	projectedAlias := stmt.ProjectedAlias
	if projectedAlias == "" && len(stmt.ProjectionColumns) != 0 {
		projectedAlias = stmt.ProjectionColumns[0].SourceQualifier
	}
	if projectedAlias == "" {
		return 0, relations[0].Table, nil
	}
	rel, ok := relationMap[projectedAlias]
	if !ok || stmt.ProjectedAliasUnknown {
		return 0, 0, sql.UnresolvedVarError{Name: projectedAlias}
	}
	alias := aliasTags[projectedAlias]
	for i, candidate := range relations {
		if candidate.Table == rel.id && candidate.Alias == alias {
			return i, rel.id, nil
		}
	}
	return 0, 0, sql.UnresolvedVarError{Name: projectedAlias}
}

func compileMultiJoinProjectionColumns(columns []sql.ProjectionColumn, relations map[string]relationSchema, aliasTag func(string) uint8) ([]compiledSQLProjectionColumn, error) {
	return compileJoinProjectionColumns(columns, relations, aliasTag)
}

func compileMultiJoinAggregateProjection(agg *sql.AggregateProjection, relations map[string]relationSchema, aliasTag func(string) uint8) (*compiledSQLAggregate, error) {
	if agg == nil || agg.Column == nil {
		return compileAggregateProjection(agg, nil)
	}
	ref := *agg.Column
	qualifier := ref.Alias
	if qualifier == "" {
		qualifier = ref.Table
	}
	rel, ok := relations[qualifier]
	if !ok {
		return nil, sql.UnresolvedVarError{Name: qualifier}
	}
	col, ok := lookupSQLColumnExact(rel.ts, ref.Column)
	if !ok {
		return nil, sql.UnresolvedVarError{Name: ref.Column}
	}
	argument := compiledSQLProjectionColumn{Schema: *col, Table: rel.id, Alias: aliasTag(ref.Alias)}
	return compileAggregateProjection(agg, &argument)
}

func compileMultiJoinOrderBy(orderBy []sql.OrderByColumn, stmt sql.Statement, relations []compiledSQLMultiJoinRelation, relationMap map[string]relationSchema, projectionColumns []compiledSQLProjectionColumn, projectedRelation int) ([]compiledSQLOrderBy, error) {
	return compileOrderByList(orderBy, func(term sql.OrderByColumn) (compiledSQLOrderBy, error) {
		return compileMultiJoinOrderByTerm(term, stmt, relations, relationMap, projectionColumns, projectedRelation)
	})
}

func compileMultiJoinOrderByTerm(orderBy sql.OrderByColumn, stmt sql.Statement, relations []compiledSQLMultiJoinRelation, relationMap map[string]relationSchema, projectionColumns []compiledSQLProjectionColumn, projectedRelation int) (compiledSQLOrderBy, error) {
	if orderBy.SourceQualifier == "" && orderBy.Table == "" {
		projectionCol, ok, err := resolveOrderByProjectionOutputName(orderBy.Column, projectionColumns)
		if err != nil {
			return compiledSQLOrderBy{}, err
		}
		if ok {
			relationIndex, ok := multiJoinRelationIndex(relations, projectionCol.Table, projectionCol.Alias)
			if !ok {
				return compiledSQLOrderBy{}, fmt.Errorf("ORDER BY column %q is not from the projected table", projectionCol.Schema.Name)
			}
			return compiledSQLOrderBy{Column: projectionCol, Desc: orderBy.Desc, Relation: relationIndex}, nil
		}
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	qualifier := orderBy.SourceQualifier
	if qualifier == "" {
		qualifier = orderBy.Table
	}
	rel, ok := relationMap[qualifier]
	if !ok {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: qualifier}
	}
	termRelation, ok := multiJoinRelationIndexByQualifier(relations, qualifier)
	if !ok {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: qualifier}
	}
	if termRelation != projectedRelation {
		return compiledSQLOrderBy{}, fmt.Errorf("ORDER BY only supports columns from the projected table")
	}
	col, ok := lookupSQLColumnExact(rel.ts, orderBy.Column)
	if !ok {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	return compiledSQLOrderBy{
		Column:   compiledSQLProjectionColumn{Schema: *col, Table: rel.id, Alias: relations[termRelation].Alias},
		Desc:     orderBy.Desc,
		Relation: termRelation,
	}, nil
}

func compileSQLMultiPredicateForRelations(pred sql.Predicate, relations map[string]relationSchema, multiRelations []compiledSQLMultiJoinRelation, aliasTag func(string) uint8, caller *types.Identity) (*compiledSQLMultiPredicate, error) {
	switch p := pred.(type) {
	case nil:
		return nil, nil
	case sql.TruePredicate:
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateTrue}, nil
	case sql.FalsePredicate:
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateFalse}, nil
	case sql.ComparisonPredicate:
		columnRef := sql.ColumnRef{Table: p.Filter.Table, Column: p.Filter.Column, Alias: p.Filter.Alias}
		column, err := compileSQLMultiColumnRefForRelations(columnRef, relations, multiRelations, aliasTag)
		if err != nil {
			return nil, err
		}
		v, err := coerceLiteral(p.Filter.Literal, column.Column.schema.Type, caller)
		if err != nil {
			var utErr sql.UnexpectedTypeError
			if errors.As(err, &utErr) {
				return nil, err
			}
			var ilErr sql.InvalidLiteralError
			if errors.As(err, &ilErr) {
				return nil, err
			}
			return nil, fmt.Errorf("coerce column %q: %v", p.Filter.Column, err)
		}
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateComparison, Column: column, Op: p.Filter.Op, Value: v}, nil
	case sql.NullPredicate:
		column, err := compileSQLMultiColumnRefForRelations(p.Column, relations, multiRelations, aliasTag)
		if err != nil {
			return nil, err
		}
		op := "="
		if p.Not {
			op = "!="
		}
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateComparison, Column: column, Op: op, Value: types.NewNull(column.Column.schema.Type)}, nil
	case sql.ColumnComparisonPredicate:
		if p.Op != "=" {
			return nil, fmt.Errorf("join WHERE column comparisons only support '='")
		}
		left, err := compileSQLMultiColumnRefForRelations(p.Left, relations, multiRelations, aliasTag)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLMultiColumnRefForRelations(p.Right, relations, multiRelations, aliasTag)
		if err != nil {
			return nil, err
		}
		if left.Column.schema.Type != right.Column.schema.Type {
			return nil, sql.UnexpectedTypeError{
				Expected: sql.AlgebraicName(right.Column.schema.Type),
				Inferred: sql.AlgebraicName(left.Column.schema.Type),
			}
		}
		if isArrayKind(left.Column.schema.Type) {
			return nil, sql.InvalidOpError{
				Op:   "=",
				Type: sql.AlgebraicName(left.Column.schema.Type),
			}
		}
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateColumnComparison, LeftColumn: left, RightColumn: right}, nil
	case sql.AndPredicate:
		left, err := compileSQLMultiPredicateForRelations(p.Left, relations, multiRelations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLMultiPredicateForRelations(p.Right, relations, multiRelations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateAnd, Left: left, Right: right}, nil
	case sql.OrPredicate:
		left, err := compileSQLMultiPredicateForRelations(p.Left, relations, multiRelations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLMultiPredicateForRelations(p.Right, relations, multiRelations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		return &compiledSQLMultiPredicate{Kind: compiledSQLMultiPredicateOr, Left: left, Right: right}, nil
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

func compileSQLMultiColumnRefForRelations(ref sql.ColumnRef, relations map[string]relationSchema, multiRelations []compiledSQLMultiJoinRelation, aliasTag func(string) uint8) (compiledSQLMultiColumnRef, error) {
	column, err := compileSQLColumnRefForRelations(ref, relations, aliasTag)
	if err != nil {
		return compiledSQLMultiColumnRef{}, err
	}
	relation, ok := multiJoinRelationIndex(multiRelations, column.table, column.alias)
	if !ok {
		qualifier := ref.Alias
		if qualifier == "" {
			qualifier = ref.Table
		}
		return compiledSQLMultiColumnRef{}, sql.UnresolvedVarError{Name: qualifier}
	}
	return compiledSQLMultiColumnRef{Column: column, Relation: relation}, nil
}

func multiJoinRelationIndex(relations []compiledSQLMultiJoinRelation, table schema.TableID, alias uint8) (int, bool) {
	for i, rel := range relations {
		if rel.Table == table && rel.Alias == alias {
			return i, true
		}
	}
	return 0, false
}

func multiJoinRelationIndexByQualifier(relations []compiledSQLMultiJoinRelation, qualifier string) (int, bool) {
	for i, rel := range relations {
		if rel.Qualifier == qualifier {
			return i, true
		}
	}
	return 0, false
}

func applyMultiJoinVisibility(multi *compiledSQLMultiJoin, sl SchemaLookup, caller *types.Identity, filters []VisibilityFilter) (bool, error) {
	if multi == nil || len(filters) == 0 {
		return false, nil
	}
	var usesCallerIdentity bool
	for i := range multi.Relations {
		rel := &multi.Relations[i]
		vis, uses, err := visibilityPredicateForRelation(rel.Table, rel.Alias, sl, caller, filters)
		if err != nil {
			return false, err
		}
		rel.Visibility = andSubscriptionPredicates(rel.Visibility, vis)
		usesCallerIdentity = usesCallerIdentity || uses
	}
	return usesCallerIdentity, nil
}
