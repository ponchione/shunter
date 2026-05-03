package protocol

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type compiledSQLQuery struct {
	TableName          string
	Predicate          subscription.Predicate
	UsesCallerIdentity bool
	ProjectionColumns  []compiledSQLProjectionColumn
	Aggregate          *compiledSQLAggregate
	OrderBy            *compiledSQLOrderBy
	Limit              *uint64
}

type compiledSQLProjectionColumn struct {
	Schema schema.ColumnSchema
	Table  schema.TableID
	Alias  uint8
}

type compiledSQLAggregate struct {
	Func         string
	ResultColumn schema.ColumnSchema
}

type compiledSQLOrderBy struct {
	Column compiledSQLProjectionColumn
	Desc   bool
}

// VisibilityFilter is validated row-level visibility metadata supplied by the
// hosted runtime. SQL must compile to one table-shaped predicate for
// ReturnTableID.
type VisibilityFilter struct {
	SQL                string
	ReturnTableID      schema.TableID
	UsesCallerIdentity bool
}

// CompiledSQLQuery is prevalidated SQL metadata produced by the protocol SQL
// compiler. It is used by runtime-owned declared reads without routing those
// reads through raw SQL admission.
type CompiledSQLQuery struct {
	query compiledSQLQuery
}

func newCompiledSQLQuery(query compiledSQLQuery) CompiledSQLQuery {
	return CompiledSQLQuery{query: copyCompiledSQLQuery(query)}
}

// Copy returns a detached copy of the compiled SQL metadata.
func (q CompiledSQLQuery) Copy() CompiledSQLQuery {
	return newCompiledSQLQuery(q.query)
}

// TableName returns the projected table name for this compiled query.
func (q CompiledSQLQuery) TableName() string {
	return q.query.TableName
}

// Predicate returns the compiled subscription predicate backing the query.
func (q CompiledSQLQuery) Predicate() subscription.Predicate {
	return q.query.Predicate
}

// UsesCallerIdentity reports whether the compiled SQL references :sender.
func (q CompiledSQLQuery) UsesCallerIdentity() bool {
	return q.query.UsesCallerIdentity
}

// ReferencedTables returns the table IDs referenced by the compiled predicate.
func (q CompiledSQLQuery) ReferencedTables() []schema.TableID {
	if q.query.Predicate == nil {
		return nil
	}
	tables := q.query.Predicate.Tables()
	out := make([]schema.TableID, len(tables))
	copy(out, tables)
	return out
}

// PredicateHashIdentity returns the hash identity needed for :sender-aware
// subscription predicates. Queries without :sender return nil.
func (q CompiledSQLQuery) PredicateHashIdentity(identity types.Identity) *types.Identity {
	if !q.query.UsesCallerIdentity {
		return nil
	}
	id := identity
	return &id
}

func copyCompiledSQLQuery(query compiledSQLQuery) compiledSQLQuery {
	out := query
	if len(query.ProjectionColumns) > 0 {
		out.ProjectionColumns = make([]compiledSQLProjectionColumn, len(query.ProjectionColumns))
		copy(out.ProjectionColumns, query.ProjectionColumns)
	}
	if query.Aggregate != nil {
		aggregate := *query.Aggregate
		out.Aggregate = &aggregate
	}
	if query.OrderBy != nil {
		orderBy := *query.OrderBy
		out.OrderBy = &orderBy
	}
	if query.Limit != nil {
		limit := *query.Limit
		out.Limit = &limit
	}
	return out
}

// SQLQueryValidationOptions controls how ValidateSQLQueryString applies the
// protocol SQL compiler to authored declaration metadata.
type SQLQueryValidationOptions struct {
	AllowLimit      bool
	AllowProjection bool
	AllowOrderBy    bool
}

type relationSchema struct {
	id schema.TableID
	ts *schema.TableSchema
}

// SchemaLookup resolves table names to their schema-level identifiers
// and column metadata. The host wires the concrete implementation;
// the protocol layer uses it during subscribe/query handling to
// validate table + column references before forwarding to the executor
// and to run shared predicate validation for one-off admission.
type SchemaLookup interface {
	schema.SchemaLookup
}

func lookupSQLTableExact(sl SchemaLookup, name string) (schema.TableID, *schema.TableSchema, bool) {
	id, ts, ok := sl.TableByName(name)
	if !ok || ts == nil || ts.Name != name {
		return 0, nil, false
	}
	return id, ts, true
}

func lookupSQLColumnExact(ts *schema.TableSchema, name string) (*schema.ColumnSchema, bool) {
	if ts == nil {
		return nil, false
	}
	for i := range ts.Columns {
		if ts.Columns[i].Name == name {
			return &ts.Columns[i], true
		}
	}
	return nil, false
}

// joinProjectsRight decides whether the SELECT target names the right side of
// the join. For distinct-table joins a match against the right table's alias
// (or its base name when unaliased) is sufficient. For self-joins the table
// names collide, so the alias alone carries the signal.
func joinProjectsRight(stmt sql.Statement, selfJoin bool) bool {
	if stmt.Join == nil {
		return false
	}
	alias := stmt.ProjectedAlias
	if alias == "" {
		return false
	}
	if alias == stmt.Join.LeftAlias {
		return false
	}
	if alias == stmt.Join.RightAlias {
		return true
	}
	if selfJoin {
		return false
	}
	return alias == stmt.Join.RightTable
}

func isSQLTruePredicate(pred sql.Predicate) bool {
	_, ok := pred.(sql.TruePredicate)
	return ok
}

func isSQLFalsePredicate(pred sql.Predicate) bool {
	_, ok := pred.(sql.FalsePredicate)
	return ok
}

func normalizeSQLPredicate(pred sql.Predicate) sql.Predicate {
	switch p := pred.(type) {
	case nil:
		return nil
	case sql.TruePredicate:
		return p
	case sql.FalsePredicate:
		return p
	case sql.ComparisonPredicate:
		return p
	case sql.AndPredicate:
		left := normalizeSQLPredicate(p.Left)
		right := normalizeSQLPredicate(p.Right)
		if isSQLFalsePredicate(left) {
			return left
		}
		if isSQLFalsePredicate(right) {
			return right
		}
		if isSQLTruePredicate(left) {
			return right
		}
		if isSQLTruePredicate(right) {
			return left
		}
		return sql.AndPredicate{Left: left, Right: right}
	case sql.OrPredicate:
		left := normalizeSQLPredicate(p.Left)
		right := normalizeSQLPredicate(p.Right)
		if isSQLTruePredicate(left) || isSQLTruePredicate(right) {
			return sql.TruePredicate{}
		}
		if isSQLFalsePredicate(left) {
			return right
		}
		if isSQLFalsePredicate(right) {
			return left
		}
		return sql.OrPredicate{Left: left, Right: right}
	default:
		return pred
	}
}

func sqlPredicateUsesCallerIdentity(pred sql.Predicate) bool {
	switch p := pred.(type) {
	case nil:
		return false
	case sql.TruePredicate:
		return false
	case sql.FalsePredicate:
		return false
	case sql.ComparisonPredicate:
		return p.Filter.Literal.Kind == sql.LitSender
	case sql.AndPredicate:
		return sqlPredicateUsesCallerIdentity(p.Left) || sqlPredicateUsesCallerIdentity(p.Right)
	case sql.OrPredicate:
		return sqlPredicateUsesCallerIdentity(p.Left) || sqlPredicateUsesCallerIdentity(p.Right)
	default:
		return false
	}
}

// wrapSubscribeCompileErrorSQL appends the offending SQL to a compile error.
func wrapSubscribeCompileErrorSQL(err error, sqlText string) string {
	// Subscribe surfaces use a distinct unsupported-SELECT prefix.
	var unsupSelectErr sql.UnsupportedSelectError
	if errors.As(err, &unsupSelectErr) {
		return fmt.Sprintf("%s, executing: `%s`", unsupSelectErr.SubscribeError(), sqlText)
	}
	return fmt.Sprintf("%s, executing: `%s`", err.Error(), sqlText)
}

// ValidateSQLQueryString validates a SQL read string against the same compiler
// used by OneOffQuery and Subscribe protocol admission.
func ValidateSQLQueryString(qs string, sl SchemaLookup, opts SQLQueryValidationOptions) error {
	if sl == nil {
		return fmt.Errorf("schema lookup must not be nil")
	}
	var caller types.Identity
	_, err := CompileSQLQueryString(qs, sl, &caller, opts)
	return err
}

// CompileSQLQueryString compiles SQL against the supplied schema lookup and
// caller identity. It is a narrow runtime seam for declared reads; raw external
// SQL must still pass an auth-aware SchemaLookup when using this compiler.
func CompileSQLQueryString(qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions) (CompiledSQLQuery, error) {
	if sl == nil {
		return CompiledSQLQuery{}, fmt.Errorf("schema lookup must not be nil")
	}
	compiled, err := compileSQLQueryString(qs, sl, caller, opts.AllowLimit, opts.AllowProjection, opts.AllowOrderBy)
	if err != nil {
		return CompiledSQLQuery{}, err
	}
	return newCompiledSQLQuery(compiled), nil
}

// CompileSQLQueryStringWithVisibility compiles SQL and expands matching
// row-level visibility filters into every table relation before execution.
func CompileSQLQueryStringWithVisibility(qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions, filters []VisibilityFilter, allowAll bool) (CompiledSQLQuery, error) {
	compiled, err := CompileSQLQueryString(qs, sl, caller, opts)
	if err != nil {
		return CompiledSQLQuery{}, err
	}
	return ApplyVisibilityFilters(compiled, sl, caller, filters, allowAll)
}

// ApplyVisibilityFilters returns a copy of compiled with visibility predicates
// attached to each table relation. allowAll bypasses row-level visibility.
func ApplyVisibilityFilters(compiled CompiledSQLQuery, sl SchemaLookup, caller *types.Identity, filters []VisibilityFilter, allowAll bool) (CompiledSQLQuery, error) {
	if sl == nil {
		return CompiledSQLQuery{}, fmt.Errorf("schema lookup must not be nil")
	}
	query := copyCompiledSQLQuery(compiled.query)
	if allowAll || len(filters) == 0 || query.Predicate == nil {
		return newCompiledSQLQuery(query), nil
	}
	expanded, usesCallerIdentity, err := expandPredicateVisibility(query.Predicate, sl, caller, filters)
	if err != nil {
		return CompiledSQLQuery{}, err
	}
	query.Predicate = expanded
	query.UsesCallerIdentity = query.UsesCallerIdentity || usesCallerIdentity
	return newCompiledSQLQuery(query), nil
}

func expandPredicateVisibility(pred subscription.Predicate, sl SchemaLookup, caller *types.Identity, filters []VisibilityFilter) (subscription.Predicate, bool, error) {
	switch p := pred.(type) {
	case subscription.Join:
		leftVis, leftUses, err := visibilityPredicateForRelation(p.Left, p.LeftAlias, sl, caller, filters)
		if err != nil {
			return nil, false, err
		}
		rightVis, rightUses, err := visibilityPredicateForRelation(p.Right, p.RightAlias, sl, caller, filters)
		if err != nil {
			return nil, false, err
		}
		p.Filter = andSubscriptionPredicates(p.Filter, andSubscriptionPredicates(leftVis, rightVis))
		return p, leftUses || rightUses, nil
	case subscription.CrossJoin:
		leftVis, leftUses, err := visibilityPredicateForRelation(p.Left, p.LeftAlias, sl, caller, filters)
		if err != nil {
			return nil, false, err
		}
		rightVis, rightUses, err := visibilityPredicateForRelation(p.Right, p.RightAlias, sl, caller, filters)
		if err != nil {
			return nil, false, err
		}
		p.Filter = andSubscriptionPredicates(p.Filter, andSubscriptionPredicates(leftVis, rightVis))
		return p, leftUses || rightUses, nil
	default:
		tables := pred.Tables()
		if len(tables) == 0 {
			return pred, false, nil
		}
		vis, uses, err := visibilityPredicateForRelation(tables[0], 0, sl, caller, filters)
		if err != nil {
			return nil, false, err
		}
		return andSubscriptionPredicates(pred, vis), uses, nil
	}
}

func visibilityPredicateForRelation(table schema.TableID, alias uint8, sl SchemaLookup, caller *types.Identity, filters []VisibilityFilter) (subscription.Predicate, bool, error) {
	var out subscription.Predicate
	var usesCallerIdentity bool
	for _, filter := range filters {
		if filter.ReturnTableID != table {
			continue
		}
		compiled, err := CompileSQLQueryString(filter.SQL, sl, caller, SQLQueryValidationOptions{
			AllowLimit:      false,
			AllowProjection: false,
		})
		if err != nil {
			return nil, false, fmt.Errorf("visibility filter %q: %w", filter.SQL, err)
		}
		referenced := compiled.ReferencedTables()
		if len(referenced) != 1 || referenced[0] != table {
			return nil, false, fmt.Errorf("visibility filter %q does not return table %d", filter.SQL, table)
		}
		usesCallerIdentity = usesCallerIdentity || filter.UsesCallerIdentity || compiled.UsesCallerIdentity()
		out = orSubscriptionPredicates(out, retagVisibilityPredicate(compiled.Predicate(), table, alias))
	}
	return out, usesCallerIdentity, nil
}

func retagVisibilityPredicate(pred subscription.Predicate, table schema.TableID, alias uint8) subscription.Predicate {
	switch p := pred.(type) {
	case subscription.ColEq:
		if p.Table == table {
			p.Alias = alias
		}
		return p
	case subscription.ColNe:
		if p.Table == table {
			p.Alias = alias
		}
		return p
	case subscription.ColRange:
		if p.Table == table {
			p.Alias = alias
		}
		return p
	case subscription.And:
		return subscription.And{
			Left:  retagVisibilityPredicate(p.Left, table, alias),
			Right: retagVisibilityPredicate(p.Right, table, alias),
		}
	case subscription.Or:
		return subscription.Or{
			Left:  retagVisibilityPredicate(p.Left, table, alias),
			Right: retagVisibilityPredicate(p.Right, table, alias),
		}
	default:
		return pred
	}
}

func andSubscriptionPredicates(left, right subscription.Predicate) subscription.Predicate {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	default:
		return subscription.And{Left: left, Right: right}
	}
}

func orSubscriptionPredicates(left, right subscription.Predicate) subscription.Predicate {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	default:
		return subscription.Or{Left: left, Right: right}
	}
}

func compileSQLQueryString(qs string, sl SchemaLookup, caller *types.Identity, allowLimit bool, allowProjection bool, allowOrderBy bool) (compiledSQLQuery, error) {
	stmt, err := sql.Parse(qs)
	if err != nil {
		// Typed compile errors already carry the final user-facing text.
		var dupErr sql.DuplicateNameError
		if errors.As(err, &dupErr) {
			return compiledSQLQuery{}, err
		}
		var unresolvedErr sql.UnresolvedVarError
		if errors.As(err, &unresolvedErr) {
			return compiledSQLQuery{}, err
		}
		var unsupSelectErr sql.UnsupportedSelectError
		if errors.As(err, &unsupSelectErr) {
			if !allowLimit && unsupSelectErr.HasLimit {
				return compiledSQLQuery{}, sql.UnsupportedFeatureError{SQL: unsupSelectErr.SQL}
			}
			return compiledSQLQuery{}, err
		}
		var unqualErr sql.UnqualifiedNamesError
		if errors.As(err, &unqualErr) {
			return compiledSQLQuery{}, err
		}
		var joinTypeErr sql.UnsupportedJoinTypeError
		if errors.As(err, &joinTypeErr) {
			return compiledSQLQuery{}, err
		}
		var unsupExprErr sql.UnsupportedExprError
		if errors.As(err, &unsupExprErr) {
			return compiledSQLQuery{}, err
		}
		return compiledSQLQuery{}, fmt.Errorf("parse: %v", err)
	}
	if stmt.UnsupportedLimit {
		return compiledSQLQuery{}, sql.UnsupportedFeatureError{SQL: qs}
	}
	if !allowOrderBy && stmt.OrderBy != nil {
		return compiledSQLQuery{}, sql.UnsupportedFeatureError{SQL: qs}
	}
	if !allowLimit && stmt.HasLimit {
		// Reference `SubParser::parse_query`
		// (sql-parser/src/parser/sub.rs:94-107) rejects subscription
		// queries carrying `limit: Some(...)` through
		// `SubscriptionUnsupported::feature(query)`, rendered as
		// `Unsupported: {query}` via parser/errors.rs:18-19. The full
		// original SQL stands in for the formatted Query.
		return compiledSQLQuery{}, sql.UnsupportedFeatureError{SQL: qs}
	}
	// Aggregate / column-list projection-return guards fire AFTER
	// `type_from` (schema lookup) and `type_select` (WHERE resolution) and
	// `type_proj` (projection resolution). Reference path:
	// `SubChecker::type_set` (check.rs:137-156) typechecks `type_from` →
	// `type_select` → `type_proj` BEFORE `expect_table_type` (check.rs:168-176)
	// emits `Unsupported::ReturnType` for `ProjectList::List` / `Agg`. So
	// missing-table / `Unresolved::Var` text takes precedence over the
	// table-type return guard. The guards live below at the tails of the
	// join branch and single-table branch.
	//
	// Keep the original predicate tree for type resolution. Reference
	// `_type_expr` types both operands of logical Bool expressions before
	// lowering AND/OR, so constant folding must not hide errors in the
	// folded-away branch.
	normalizedPredicate := normalizeSQLPredicate(stmt.Predicate)
	usesCallerIdentity := sqlPredicateUsesCallerIdentity(normalizedPredicate)
	if stmt.Join != nil {
		leftID, leftTS, ok := lookupSQLTableExact(sl, stmt.Join.LeftTable)
		if !ok {
			//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
			return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.Join.LeftTable)
		}
		// Duplicate aliases are rejected after left-table lookup and before
		// right-table lookup.
		if stmt.Join.AliasCollision {
			return compiledSQLQuery{}, sql.DuplicateNameError{Name: stmt.Join.LeftAlias}
		}
		rightID, rightTS, ok := lookupSQLTableExact(sl, stmt.Join.RightTable)
		if !ok {
			//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
			return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.Join.RightTable)
		}
		// Resolve JOIN ON before projection and WHERE folding errors.
		var leftCol, rightCol *schema.ColumnSchema
		if stmt.Join.HasOn {
			leftSide, resolvedLeftCol, err := resolveJoinOnColumn(stmt.Join.LeftOn, stmt, leftTS, rightTS)
			if err != nil {
				return compiledSQLQuery{}, err
			}
			rightSide, resolvedRightCol, err := resolveJoinOnColumn(stmt.Join.RightOn, stmt, leftTS, rightTS)
			if err != nil {
				return compiledSQLQuery{}, err
			}
			if leftSide == rightSide {
				return compiledSQLQuery{}, fmt.Errorf("JOIN ON must compare columns from different relations")
			}
			if leftSide == "right" && rightSide == "left" {
				resolvedLeftCol, resolvedRightCol = resolvedRightCol, resolvedLeftCol
				leftSide, rightSide = rightSide, leftSide
			}
			if leftSide != "left" || rightSide != "right" {
				return compiledSQLQuery{}, fmt.Errorf("JOIN ON must compare left relation to right relation")
			}
			leftCol, rightCol = resolvedLeftCol, resolvedRightCol
			// Match the public UnexpectedType slot ordering for ON mismatches.
			if leftCol.Type != rightCol.Type {
				return compiledSQLQuery{}, sql.UnexpectedTypeError{
					Expected: sql.AlgebraicName(rightCol.Type),
					Inferred: sql.AlgebraicName(leftCol.Type),
				}
			}
			// Array equality in JOIN ON is rejected as an invalid operator.
			if isArrayKind(leftCol.Type) {
				return compiledSQLQuery{}, sql.InvalidOpError{
					Op:   "=",
					Type: sql.AlgebraicName(leftCol.Type),
				}
			}
		}
		relations := map[string]relationSchema{
			stmt.Join.LeftAlias:  {id: leftID, ts: leftTS},
			stmt.Join.RightAlias: {id: rightID, ts: rightTS},
		}
		if stmt.Join.HasOn && stmt.Predicate != nil {
			if _, err := compileSQLPredicateForRelations(stmt.Predicate, relations, aliasTagForJoin(stmt, leftID == rightID), caller); err != nil {
				return compiledSQLQuery{}, err
			}
		}
		if stmt.ProjectedAlias != "" && len(stmt.ProjectionColumns) == 0 {
			if _, ok := resolveProjectedJoinRelation(stmt, leftID, rightID); !ok || stmt.ProjectedAliasUnknown {
				return compiledSQLQuery{}, sql.UnresolvedVarError{Name: stmt.ProjectedAlias}
			}
		}
		// Bare SELECT * is not supported for joins after ON/WHERE resolution.
		if stmt.ProjectedAlias == "" && len(stmt.ProjectionColumns) == 0 && stmt.Aggregate == nil {
			return compiledSQLQuery{}, fmt.Errorf("SELECT * is not supported for joins")
		}
		projectedID := leftID
		if joinProjectsRight(stmt, leftID == rightID) {
			projectedID = rightID
		}
		aliasTag := aliasTagForJoin(stmt, leftID == rightID)
		projectionColumns, err := compileJoinProjectionColumns(stmt.ProjectionColumns, relations, aliasTag)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		if !allowProjection && len(stmt.ProjectionColumns) != 0 {
			//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
			return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
		}
		aggregate, err := compileAggregateProjection(stmt.Aggregate)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		// Reference `expect_table_type` (check.rs:168-176) rejects
		// `ProjectList::Agg` with `Unsupported::ReturnType` AFTER
		// `type_proj` resolves the projection. Aggregate guard mirrors
		// the column-list guard above so schema/WHERE/JOIN-ON errors
		// surface first.
		if !allowProjection && stmt.Aggregate != nil {
			//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
			return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
		}
		orderBy, err := compileJoinOrderBy(stmt.OrderBy, stmt, relations, projectedID, aliasTag, leftID == rightID)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		if stmt.OrderBy != nil && stmt.Aggregate != nil {
			return compiledSQLQuery{}, fmt.Errorf("ORDER BY is not supported with aggregate projections")
		}
		limit, err := compileStatementLimit(stmt, qs)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		if !stmt.Join.HasOn {
			if stmt.Predicate != nil {
				if !allowProjection {
					return compiledSQLQuery{}, fmt.Errorf("cross join WHERE not supported")
				}
				join, err := compileCrossJoinWhereColumnEquality(stmt, leftID, leftTS, rightID, rightTS, caller)
				if err != nil {
					return compiledSQLQuery{}, err
				}
				return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: join, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, OrderBy: orderBy, Limit: limit}, nil
			}
			cross := subscription.CrossJoin{Left: leftID, Right: rightID}
			if leftID == rightID {
				cross.LeftAlias = 0
				cross.RightAlias = 1
			}
			cross.ProjectRight = joinProjectsRight(stmt, leftID == rightID)
			return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: cross, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, OrderBy: orderBy, Limit: limit}, nil
		}
		if _, ok := normalizedPredicate.(sql.FalsePredicate); ok {
			return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: subscription.NoRows{Table: projectedID}, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, OrderBy: orderBy, Limit: limit}, nil
		}
		var filter subscription.Predicate
		if stmt.Join.HasOn && normalizedPredicate != nil {
			var err error
			filter, err = compileSQLPredicateForRelations(normalizedPredicate, relations, aliasTag, caller)
			if err != nil {
				return compiledSQLQuery{}, err
			}
		}
		if !allowProjection && !sl.HasIndex(leftID, types.ColID(leftCol.Index)) && !sl.HasIndex(rightID, types.ColID(rightCol.Index)) {
			//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
			return compiledSQLQuery{}, fmt.Errorf("Subscriptions require indexes on join columns")
		}
		join := subscription.Join{
			Left: leftID, Right: rightID,
			LeftCol: types.ColID(leftCol.Index), RightCol: types.ColID(rightCol.Index),
			Filter: filter,
		}
		if leftID == rightID {
			join.LeftAlias = 0
			join.RightAlias = 1
		}
		join.ProjectRight = joinProjectsRight(stmt, leftID == rightID)
		return compiledSQLQuery{
			TableName:          stmt.ProjectedTable,
			Predicate:          join,
			UsesCallerIdentity: usesCallerIdentity,
			ProjectionColumns:  projectionColumns,
			Aggregate:          aggregate,
			OrderBy:            orderBy,
			Limit:              limit,
		}, nil
	}
	projectedID, ts, ok := lookupSQLTableExact(sl, stmt.ProjectedTable)
	if !ok {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.ProjectedTable)
	}
	// Resolve WHERE before projection columns so predicate errors win.
	if _, err := compileSQLPredicateForSingleRelation(stmt.Predicate, relationSchema{id: projectedID, ts: ts}, stmt.TableAlias, caller); err != nil {
		return compiledSQLQuery{}, err
	}
	pred, err := compileSQLPredicateForSingleRelation(normalizedPredicate, relationSchema{id: projectedID, ts: ts}, stmt.TableAlias, caller)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if stmt.ProjectedAliasUnknown && len(stmt.ProjectionColumns) == 0 {
		return compiledSQLQuery{}, sql.UnresolvedVarError{Name: stmt.ProjectedAlias}
	}
	projectionColumns, err := compileProjectionColumns(stmt.ProjectedTable, stmt.TableAlias, stmt.ProjectionColumns, projectedID, ts)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if !allowProjection && len(stmt.ProjectionColumns) != 0 {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
	}
	aggregate, err := compileAggregateProjection(stmt.Aggregate)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	// Reference `expect_table_type` (check.rs:168-176) rejects
	// `ProjectList::Agg` with `Unsupported::ReturnType` AFTER `type_proj`
	// resolves the projection. Aggregate guard mirrors the column-list
	// guard above so schema/WHERE errors surface first.
	if !allowProjection && stmt.Aggregate != nil {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
	}
	orderBy, err := compileSingleRelationOrderBy(stmt.OrderBy, stmt.ProjectedTable, stmt.TableAlias, projectedID, ts)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if stmt.OrderBy != nil && stmt.Aggregate != nil {
		return compiledSQLQuery{}, fmt.Errorf("ORDER BY is not supported with aggregate projections")
	}
	limit, err := compileStatementLimit(stmt, qs)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: pred, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, OrderBy: orderBy, Limit: limit}, nil
}

func aliasTagForJoin(stmt sql.Statement, selfJoin bool) func(string) uint8 {
	if !selfJoin {
		return func(string) uint8 { return 0 }
	}
	return func(a string) uint8 {
		if a == "" {
			return 0
		}
		if a == stmt.Join.RightAlias {
			return 1
		}
		return 0
	}
}

func resolveProjectedJoinRelation(stmt sql.Statement, leftID, rightID schema.TableID) (schema.TableID, bool) {
	switch {
	case stmt.ProjectedAlias == stmt.Join.LeftAlias:
		return leftID, true
	case stmt.ProjectedAlias == stmt.Join.RightAlias:
		return rightID, true
	case leftID != rightID && stmt.ProjectedAlias == stmt.Join.LeftTable:
		return leftID, true
	case leftID != rightID && stmt.ProjectedAlias == stmt.Join.RightTable:
		return rightID, true
	default:
		return 0, false
	}
}

func resolveJoinOnColumn(ref sql.ColumnRef, stmt sql.Statement, leftTS, rightTS *schema.TableSchema) (string, *schema.ColumnSchema, error) {
	switch {
	case ref.Alias == stmt.Join.LeftAlias:
		col, ok := lookupSQLColumnExact(leftTS, ref.Column)
		if !ok {
			return "", nil, sql.UnresolvedVarError{Name: ref.Column}
		}
		return "left", col, nil
	case ref.Alias == stmt.Join.RightAlias:
		col, ok := lookupSQLColumnExact(rightTS, ref.Column)
		if !ok {
			return "", nil, sql.UnresolvedVarError{Name: ref.Column}
		}
		return "right", col, nil
	default:
		return "", nil, sql.UnresolvedVarError{Name: ref.Alias}
	}
}

func compileStatementLimit(stmt sql.Statement, sqlText string) (*uint64, error) {
	if stmt.UnsupportedLimit {
		return nil, sql.UnsupportedFeatureError{SQL: sqlText}
	}
	if stmt.InvalidLimit != nil {
		return nil, sql.InvalidLiteralError{Literal: limitLiteralText(*stmt.InvalidLimit), Type: "U64"}
	}
	return stmt.Limit, nil
}

func limitLiteralText(lit sql.Literal) string {
	if lit.Text != "" {
		return lit.Text
	}
	if lit.Big != nil {
		return lit.Big.String()
	}
	switch lit.Kind {
	case sql.LitInt:
		return fmt.Sprintf("%d", lit.Int)
	case sql.LitFloat:
		return fmt.Sprintf("%g", lit.Float)
	default:
		return ""
	}
}

func compileAggregateProjection(agg *sql.AggregateProjection) (*compiledSQLAggregate, error) {
	if agg == nil {
		return nil, nil
	}
	if !strings.EqualFold(agg.Func, "COUNT") {
		return nil, fmt.Errorf("aggregate projections not supported")
	}
	return &compiledSQLAggregate{
		Func:         "COUNT",
		ResultColumn: schema.ColumnSchema{Index: 0, Name: agg.Alias, Type: schema.KindUint64},
	}, nil
}

func compileCrossJoinWhereColumnEquality(stmt sql.Statement, leftID schema.TableID, leftTS *schema.TableSchema, rightID schema.TableID, rightTS *schema.TableSchema, caller *types.Identity) (subscription.Join, error) {
	cmp, filterPred, err := splitCrossJoinWherePredicate(stmt.Predicate)
	if err != nil {
		return subscription.Join{}, err
	}
	if cmp.Op != "=" {
		return subscription.Join{}, fmt.Errorf("cross join WHERE column comparisons only support '='")
	}
	leftRef, rightRef := cmp.Left, cmp.Right
	leftSide, leftRel, err := resolveJoinPredicateRelation(leftRef, stmt, relationSchema{id: leftID, ts: leftTS}, relationSchema{id: rightID, ts: rightTS})
	if err != nil {
		return subscription.Join{}, err
	}
	rightSide, rightRel, err := resolveJoinPredicateRelation(rightRef, stmt, relationSchema{id: leftID, ts: leftTS}, relationSchema{id: rightID, ts: rightTS})
	if err != nil {
		return subscription.Join{}, err
	}
	if leftSide == rightSide {
		return subscription.Join{}, fmt.Errorf("cross join WHERE column equality must compare left and right relations")
	}
	if leftSide == "right" && rightSide == "left" {
		leftRef, rightRef = rightRef, leftRef
		leftRel, rightRel = rightRel, leftRel
		leftSide, rightSide = rightSide, leftSide
	}
	if leftSide != "left" || rightSide != "right" {
		return subscription.Join{}, fmt.Errorf("cross join WHERE column equality must compare left and right relations")
	}
	leftCol, ok := lookupSQLColumnExact(leftRel.ts, leftRef.Column)
	if !ok {
		return subscription.Join{}, sql.UnresolvedVarError{Name: leftRef.Column}
	}
	rightCol, ok := lookupSQLColumnExact(rightRel.ts, rightRef.Column)
	if !ok {
		return subscription.Join{}, sql.UnresolvedVarError{Name: rightRef.Column}
	}
	if leftID == rightID {
		return subscription.Join{}, fmt.Errorf("self-join cross join WHERE column equality not supported")
	}
	if leftCol.Type != rightCol.Type {
		return subscription.Join{}, sql.UnexpectedTypeError{
			Expected: sql.AlgebraicName(rightCol.Type),
			Inferred: sql.AlgebraicName(leftCol.Type),
		}
	}
	if isArrayKind(leftCol.Type) {
		return subscription.Join{}, sql.InvalidOpError{
			Op:   "=",
			Type: sql.AlgebraicName(leftCol.Type),
		}
	}
	var filter subscription.Predicate
	if filterPred != nil {
		filter, err = compileCrossJoinWhereLiteralFilter(filterPred, map[string]relationSchema{
			stmt.Join.LeftAlias:  {id: leftID, ts: leftTS},
			stmt.Join.RightAlias: {id: rightID, ts: rightTS},
		}, caller, leftID)
		if err != nil {
			return subscription.Join{}, err
		}
	}
	return subscription.Join{
		Left:         leftID,
		Right:        rightID,
		LeftCol:      types.ColID(leftCol.Index),
		RightCol:     types.ColID(rightCol.Index),
		Filter:       filter,
		ProjectRight: joinProjectsRight(stmt, false),
	}, nil
}

func resolveJoinPredicateRelation(ref sql.ColumnRef, stmt sql.Statement, left relationSchema, right relationSchema) (string, relationSchema, error) {
	switch ref.Alias {
	case stmt.Join.LeftAlias:
		return "left", left, nil
	case stmt.Join.RightAlias:
		return "right", right, nil
	default:
		name := ref.Alias
		if name == "" {
			name = ref.Table
		}
		return "", relationSchema{}, sql.UnresolvedVarError{Name: name}
	}
}

type crossJoinWhereParts struct {
	cmp    sql.ColumnComparisonPredicate
	hasCmp bool
	filter sql.Predicate
}

func splitCrossJoinWherePredicate(pred sql.Predicate) (sql.ColumnComparisonPredicate, sql.Predicate, error) {
	parts, err := splitCrossJoinWherePredicateTree(pred)
	if err != nil {
		return sql.ColumnComparisonPredicate{}, nil, err
	}
	if !parts.hasCmp {
		return sql.ColumnComparisonPredicate{}, nil, fmt.Errorf("cross join WHERE only supports qualified column equality")
	}
	return parts.cmp, parts.filter, nil
}

func splitCrossJoinWherePredicateTree(pred sql.Predicate) (crossJoinWhereParts, error) {
	switch p := pred.(type) {
	case sql.ColumnComparisonPredicate:
		return crossJoinWhereParts{cmp: p, hasCmp: true}, nil
	case sql.AndPredicate:
		left, err := splitCrossJoinWherePredicateTree(p.Left)
		if err != nil {
			return crossJoinWhereParts{}, err
		}
		right, err := splitCrossJoinWherePredicateTree(p.Right)
		if err != nil {
			return crossJoinWhereParts{}, err
		}
		if left.hasCmp && right.hasCmp {
			return crossJoinWhereParts{}, fmt.Errorf("cross join WHERE supports exactly one qualified column equality")
		}
		parts := crossJoinWhereParts{filter: joinSQLPredicatesWithAnd(left.filter, right.filter)}
		switch {
		case left.hasCmp:
			parts.cmp = left.cmp
			parts.hasCmp = true
		case right.hasCmp:
			parts.cmp = right.cmp
			parts.hasCmp = true
		}
		return parts, nil
	default:
		return crossJoinWhereParts{filter: pred}, nil
	}
}

func joinSQLPredicatesWithAnd(left, right sql.Predicate) sql.Predicate {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	default:
		return sql.AndPredicate{Left: left, Right: right}
	}
}

func compileCrossJoinWhereLiteralFilter(pred sql.Predicate, relations map[string]relationSchema, caller *types.Identity, noRowsTable schema.TableID) (subscription.Predicate, error) {
	if _, err := compileSQLPredicateForRelations(pred, relations, func(string) uint8 { return 0 }, caller); err != nil {
		return nil, err
	}
	normalized := normalizeSQLPredicate(pred)
	switch p := normalized.(type) {
	case nil:
		return nil, nil
	case sql.TruePredicate:
		return nil, nil
	case sql.FalsePredicate:
		return subscription.NoRows{Table: noRowsTable}, nil
	default:
		return compileSQLPredicateForRelations(p, relations, func(string) uint8 { return 0 }, caller)
	}
}

func compileSQLPredicateForSingleRelation(pred sql.Predicate, rel relationSchema, tableAlias string, caller *types.Identity) (subscription.Predicate, error) {
	switch p := pred.(type) {
	case nil:
		return subscription.AllRows{Table: rel.id}, nil
	case sql.TruePredicate:
		return subscription.AllRows{Table: rel.id}, nil
	case sql.FalsePredicate:
		return subscription.NoRows{Table: rel.id}, nil
	case sql.ComparisonPredicate:
		if p.Filter.Alias != "" && p.Filter.Alias != tableAlias {
			return nil, sql.UnresolvedVarError{Name: p.Filter.Alias}
		}
		f := p.Filter
		f.Table = tableAlias
		return normalizeSQLFilterForRelations(f, map[string]relationSchema{tableAlias: rel}, func(string) uint8 { return 0 }, caller)
	case sql.AndPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForSingleRelation(child, rel, tableAlias, caller)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.And{Left: left, Right: right}
		})
	case sql.OrPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForSingleRelation(child, rel, tableAlias, caller)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.Or{Left: left, Right: right}
		})
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

func compileProjectionColumns(projectedTable string, tableAlias string, columns []sql.ProjectionColumn, tableID schema.TableID, ts *schema.TableSchema) ([]compiledSQLProjectionColumn, error) {
	if len(columns) == 0 {
		return nil, nil
	}
	resolved := make([]compiledSQLProjectionColumn, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		if err := checkDuplicateProjectionName(col, seen); err != nil {
			return nil, err
		}
		if col.SourceQualifier != "" && col.SourceQualifier != tableAlias {
			return nil, sql.UnresolvedVarError{Name: projectionQualifierName(col)}
		}
		compiledCol, err := compileProjectionColumn(col, tableID, ts, 0)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, compiledCol)
	}
	return resolved, nil
}

func compileJoinProjectionColumns(columns []sql.ProjectionColumn, relations map[string]relationSchema, aliasTag func(string) uint8) ([]compiledSQLProjectionColumn, error) {
	if len(columns) == 0 {
		return nil, nil
	}
	resolved := make([]compiledSQLProjectionColumn, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		if err := checkDuplicateProjectionName(col, seen); err != nil {
			return nil, err
		}
		qualifier := col.SourceQualifier
		if qualifier == "" {
			qualifier = col.Table
		}
		rel, ok := relations[qualifier]
		if !ok {
			return nil, sql.UnresolvedVarError{Name: projectionQualifierName(col)}
		}
		compiledCol, err := compileProjectionColumn(col, rel.id, rel.ts, aliasTag(col.SourceQualifier))
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, compiledCol)
	}
	return resolved, nil
}

// checkDuplicateProjectionName rejects duplicate output names in SELECT lists.
func checkDuplicateProjectionName(col sql.ProjectionColumn, seen map[string]struct{}) error {
	name := col.OutputAlias
	if name == "" {
		name = col.Column
	}
	if _, dup := seen[name]; dup {
		return sql.DuplicateNameError{Name: name}
	}
	seen[name] = struct{}{}
	return nil
}

func projectionQualifierName(col sql.ProjectionColumn) string {
	if col.SourceQualifier != "" {
		return col.SourceQualifier
	}
	return col.Table
}

func compileProjectionColumn(col sql.ProjectionColumn, tableID schema.TableID, ts *schema.TableSchema, alias uint8) (compiledSQLProjectionColumn, error) {
	tsCol, ok := lookupSQLColumnExact(ts, col.Column)
	if !ok {
		return compiledSQLProjectionColumn{}, sql.UnresolvedVarError{Name: col.Column}
	}
	compiledCol := *tsCol
	if col.OutputAlias != "" {
		compiledCol.Name = col.OutputAlias
	}
	return compiledSQLProjectionColumn{Schema: compiledCol, Table: tableID, Alias: alias}, nil
}

func compileSingleRelationOrderBy(orderBy *sql.OrderByColumn, projectedTable string, tableAlias string, tableID schema.TableID, ts *schema.TableSchema) (*compiledSQLOrderBy, error) {
	if orderBy == nil {
		return nil, nil
	}
	if orderBy.SourceQualifier != "" && orderBy.SourceQualifier != tableAlias {
		return nil, sql.UnresolvedVarError{Name: orderBy.SourceQualifier}
	}
	if orderBy.SourceQualifier == "" && orderBy.Table != projectedTable {
		return nil, sql.UnresolvedVarError{Name: orderBy.Table}
	}
	col, ok := lookupSQLColumnExact(ts, orderBy.Column)
	if !ok {
		return nil, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	return &compiledSQLOrderBy{
		Column: compiledSQLProjectionColumn{Schema: *col, Table: tableID, Alias: 0},
		Desc:   orderBy.Desc,
	}, nil
}

func compileJoinOrderBy(orderBy *sql.OrderByColumn, stmt sql.Statement, relations map[string]relationSchema, projectedID schema.TableID, aliasTag func(string) uint8, selfJoin bool) (*compiledSQLOrderBy, error) {
	if orderBy == nil {
		return nil, nil
	}
	qualifier := orderBy.SourceQualifier
	if qualifier == "" {
		qualifier = orderBy.Table
	}
	rel, ok := relations[qualifier]
	if !ok {
		return nil, sql.UnresolvedVarError{Name: qualifier}
	}
	if rel.id != projectedID || (selfJoin && aliasTag(qualifier) != aliasTag(stmt.ProjectedAlias)) {
		return nil, fmt.Errorf("ORDER BY only supports columns from the projected table")
	}
	col, ok := lookupSQLColumnExact(rel.ts, orderBy.Column)
	if !ok {
		return nil, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	return &compiledSQLOrderBy{
		Column: compiledSQLProjectionColumn{Schema: *col, Table: rel.id, Alias: aliasTag(qualifier)},
		Desc:   orderBy.Desc,
	}, nil
}

// isArrayKind reports whether a column kind is an array/product kind that
// cannot participate in equality or range comparisons. Today the only
// realized array kind is KindArrayString; the helper keeps the join-ON and
// future filter guards in one place so additional array element widenings
// stay narrow.
func isArrayKind(k types.ValueKind) bool {
	return k == types.KindArrayString
}

func coerceLiteral(lit sql.Literal, kind types.ValueKind, caller *types.Identity) (types.Value, error) {
	if caller != nil {
		raw := (*[32]byte)(caller)
		return sql.CoerceWithCaller(lit, kind, raw)
	}
	return sql.Coerce(lit, kind)
}

func compileSQLPredicateForRelations(pred sql.Predicate, relations map[string]relationSchema, aliasTag func(string) uint8, caller *types.Identity) (subscription.Predicate, error) {
	switch p := pred.(type) {
	case nil:
		if len(relations) != 1 {
			return nil, nil
		}
		for _, rel := range relations {
			return subscription.AllRows{Table: rel.id}, nil
		}
		return nil, nil
	case sql.TruePredicate:
		if len(relations) != 1 {
			return nil, nil
		}
		for _, rel := range relations {
			return subscription.AllRows{Table: rel.id}, nil
		}
		return nil, nil
	case sql.FalsePredicate:
		if len(relations) != 1 {
			return nil, nil
		}
		for _, rel := range relations {
			return subscription.NoRows{Table: rel.id}, nil
		}
		return nil, nil
	case sql.ComparisonPredicate:
		return normalizeSQLFilterForRelations(p.Filter, relations, aliasTag, caller)
	case sql.ColumnComparisonPredicate:
		return nil, fmt.Errorf("join WHERE column comparisons not supported")
	case sql.AndPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForRelations(child, relations, aliasTag, caller)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.And{Left: left, Right: right}
		})
	case sql.OrPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForRelations(child, relations, aliasTag, caller)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.Or{Left: left, Right: right}
		})
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

func compileSQLBinaryPredicate(
	leftSQL, rightSQL sql.Predicate,
	compileChild func(sql.Predicate) (subscription.Predicate, error),
	combine func(subscription.Predicate, subscription.Predicate) subscription.Predicate,
) (subscription.Predicate, error) {
	left, err := compileChild(leftSQL)
	if err != nil {
		return nil, err
	}
	right, err := compileChild(rightSQL)
	if err != nil {
		return nil, err
	}
	return combine(left, right), nil
}

func normalizeSQLFilterForRelations(f sql.Filter, relations map[string]relationSchema, aliasTag func(string) uint8, caller *types.Identity) (subscription.Predicate, error) {
	relationKey := f.Table
	if f.Alias != "" {
		relationKey = f.Alias
	}
	rel, ok := relations[relationKey]
	if !ok {
		name := f.Alias
		if name == "" {
			name = f.Table
		}
		return nil, sql.UnresolvedVarError{Name: name}
	}
	col, ok := lookupSQLColumnExact(rel.ts, f.Column)
	if !ok {
		return nil, sql.UnresolvedVarError{Name: f.Column}
	}
	v, err := coerceLiteral(f.Literal, col.Type, caller)
	if err != nil {
		// Reference-informed error types carry the source literal verbatim; do
		// not prefix with "coerce column" so the text matches
		// reference expr/src/errors.rs (UnexpectedType:100,
		// InvalidLiteral:84).
		var utErr sql.UnexpectedTypeError
		if errors.As(err, &utErr) {
			return nil, err
		}
		var ilErr sql.InvalidLiteralError
		if errors.As(err, &ilErr) {
			return nil, err
		}
		return nil, fmt.Errorf("coerce column %q: %v", f.Column, err)
	}
	return normalizePredicate(rel.id, col.Index, aliasTag(f.Alias), f.Op, v)
}

func normalizePredicate(tableID schema.TableID, colIndex int, alias uint8, op string, value types.Value) (subscription.Predicate, error) {
	column := types.ColID(colIndex)
	switch op {
	case "", "=":
		return subscription.ColEq{Table: tableID, Column: column, Alias: alias, Value: value}, nil
	case "!=", "<>":
		return subscription.ColNe{Table: tableID, Column: column, Alias: alias, Value: value}, nil
	case ">":
		return subscription.ColRange{Table: tableID, Column: column, Alias: alias, Lower: subscription.Bound{Value: value, Inclusive: false}, Upper: subscription.Bound{Unbounded: true}}, nil
	case ">=":
		return subscription.ColRange{Table: tableID, Column: column, Alias: alias, Lower: subscription.Bound{Value: value, Inclusive: true}, Upper: subscription.Bound{Unbounded: true}}, nil
	case "<":
		return subscription.ColRange{Table: tableID, Column: column, Alias: alias, Lower: subscription.Bound{Unbounded: true}, Upper: subscription.Bound{Value: value, Inclusive: false}}, nil
	case "<=":
		return subscription.ColRange{Table: tableID, Column: column, Alias: alias, Lower: subscription.Bound{Unbounded: true}, Upper: subscription.Bound{Value: value, Inclusive: true}}, nil
	default:
		return nil, fmt.Errorf("unsupported comparison operator %q", op)
	}
}
