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

// compileQuery resolves a wire Query against the schema and returns the
// compiled subscription predicate. Errors carry context suitable for
// SubscriptionError.Error. Shared between handleSubscribeSingle and
// handleSubscribeMulti.
func compileQuery(q Query, sl SchemaLookup) (subscription.Predicate, error) {
	tableID, ts, ok := sl.TableByName(q.TableName)
	if !ok {
		return nil, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", q.TableName)
	}
	return NormalizePredicates(tableID, ts, q.Predicates)
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
	if strings.EqualFold(alias, stmt.Join.LeftAlias) {
		return false
	}
	if strings.EqualFold(alias, stmt.Join.RightAlias) {
		return true
	}
	if selfJoin {
		return false
	}
	return strings.EqualFold(alias, stmt.Join.RightTable)
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

func callerHashIdentity(conn *Conn, compiled compiledSQLQuery) *types.Identity {
	if !compiled.UsesCallerIdentity {
		return nil
	}
	id := conn.Identity
	return &id
}

// wrapSubscribeCompileErrorSQL mirrors reference `DBError::WithSql`
// (reference/SpacetimeDB/crates/core/src/error.rs:140,
// `"{error}, executing: `{sql}`"`). SubscribeSingle/SubscribeMulti
// admission surfaces emit this suffixed shape so clients can correlate
// the rejection with the exact offending query string. Reference emit
// sites: module_subscription_actor.rs:643 (SubscribeSingle
// `compile_query_with_hashes`), :1068 (SubscribeMulti per-SQL
// `compile_query_with_hashes`) — both go through the
// `return_on_err_with_sql_bool!` macro that wraps the inner error via
// `DBError::WithSql`.
func wrapSubscribeCompileErrorSQL(err error, sqlText string) string {
	// Reference subscription parser routes the `SELECT ALL ...` /
	// `SELECT DISTINCT ...` rejection through
	// `SubscriptionUnsupported::Select`, which renders with the
	// `Unsupported SELECT:` prefix instead of OneOff's `Unsupported:`.
	// Detect the typed error and switch the inner rendering before
	// applying the `DBError::WithSql` `, executing: ...` wrap.
	var unsupSelectErr sql.UnsupportedSelectError
	if errors.As(err, &unsupSelectErr) {
		return fmt.Sprintf("%s, executing: `%s`", unsupSelectErr.SubscribeError(), sqlText)
	}
	return fmt.Sprintf("%s, executing: `%s`", err.Error(), sqlText)
}

func compileSQLQueryString(qs string, sl SchemaLookup, caller *types.Identity, allowLimit bool, allowProjection bool) (compiledSQLQuery, error) {
	stmt, err := sql.Parse(qs)
	if err != nil {
		// Reference compile-stage typed errors (DuplicateName for join
		// alias collisions, UnresolvedVar for qualifier-not-in-scope)
		// carry the literal text verbatim. The generic `parse:` prefix
		// would obscure the reference shape on both OneOff (raw) and
		// SubscribeSingle/Multi (WithSql-wrapped) surfaces, so let
		// typed errors flow through unwrapped — same pattern as the
		// `normalizeSQLFilterForRelations` bypass for
		// `InvalidLiteralError` / `UnexpectedTypeError`.
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
		leftID, leftTS, ok := sl.TableByName(stmt.Join.LeftTable)
		if !ok {
			return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.Join.LeftTable)
		}
		rightID, rightTS, ok := sl.TableByName(stmt.Join.RightTable)
		if !ok {
			return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.Join.RightTable)
		}
		// Reference `type_from` (`expr/src/check.rs:79-89`) resolves the
		// left relvar through `type_relvar` BEFORE entering the join
		// loop's HashSet duplicate-alias check. Parser stage detects
		// `LeftAlias == RightAlias` and defers the rejection here so a
		// missing left/right table emits the no-such-table text first.
		if stmt.Join.AliasCollision {
			return compiledSQLQuery{}, sql.DuplicateNameError{Name: stmt.Join.LeftAlias}
		}
		// Reference `type_from` (check.rs:99-104) types the JOIN ON
		// expression through `type_expr` (lib.rs:101-102) BEFORE the
		// resulting `RelExpr` is handed to `type_proj` for projection
		// typing or to `type_select` for WHERE folding. Resolve ON
		// columns + their type/array checks here so:
		//   - `SELECT * FROM t JOIN s ON t.missing = s.id` raises
		//     `Unresolved::Var{missing}` BEFORE the bare-wildcard
		//     `InvalidWildcard::Join` rejection below.
		//   - `... ON t.missing = s.id WHERE FALSE` raises
		//     `Unresolved::Var{missing}` BEFORE the FalsePredicate
		//     short-circuit later in this branch returns NoRows.
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
			// Reference `type_expr` (expr/src/lib.rs:134-140) types the ON
			// binop's LEFT side first with no expectation, then types the
			// RIGHT side with the LEFT type as the expected. The mismatch
			// arm at lib.rs:111-112 calls `UnexpectedType::new(col_type,
			// ty)` — `col_type` is the RIGHT side's column type (renders in
			// the `(expected)` slot per errors.rs:104) and `ty` is the LEFT
			// side's column type that was passed as expected (renders in
			// the `(inferred)` slot). Mirror the slot ordering so both
			// surfaces (OneOff raw, SubscribeSingle WithSql-wrapped) carry
			// the reference text instead of the late `subscription:
			// invalid predicate: join column kinds differ` from
			// `subscription/validate.go::validateJoin`.
			if leftCol.Type != rightCol.Type {
				return compiledSQLQuery{}, sql.UnexpectedTypeError{
					Expected: sql.AlgebraicName(rightCol.Type),
					Inferred: sql.AlgebraicName(leftCol.Type),
				}
			}
			// Reference `type_expr` (expr/src/lib.rs:138) routes equality
			// against an Array/Product column through `op_supports_type`
			// (lib.rs:155), which rejects non-primitive types and emits
			// `InvalidOp{op, ty}`. The ON binop reaches lib.rs:134-140
			// (neither side a Lit) so the type comes from the LEFT field
			// after the `leftCol.Type != rightCol.Type` mismatch arm above
			// has already returned.
			if isArrayKind(leftCol.Type) {
				return compiledSQLQuery{}, sql.InvalidOpError{
					Op:   "=",
					Type: sql.AlgebraicName(leftCol.Type),
				}
			}
		}
		relations := map[string]relationSchema{
			stmt.Join.LeftTable:  {id: leftID, ts: leftTS},
			stmt.Join.RightTable: {id: rightID, ts: rightTS},
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
		// Reference `InvalidWildcard::Join` at
		// reference/SpacetimeDB/crates/expr/src/errors.rs:41 emits
		// "SELECT * is not supported for joins" via `type_proj` at
		// reference/SpacetimeDB/crates/expr/src/lib.rs:56 when
		// `ast::Project::Star(None)` meets `input.nfields() > 1`. Match
		// the raw text so SubscribeSingle/Multi WithSql-wrap and OneOff's
		// unwrapped emit both carry the reference literal. ON-column and
		// WHERE resolution above run first so type errors are not masked
		// by this guard.
		if stmt.ProjectedAlias == "" && len(stmt.ProjectionColumns) == 0 && stmt.Aggregate == nil {
			return compiledSQLQuery{}, fmt.Errorf("SELECT * is not supported for joins")
		}
		projectedID := leftID
		if joinProjectsRight(stmt, leftID == rightID) {
			projectedID = rightID
		}
		aliasTag := aliasTagForJoin(stmt, leftID == rightID)
		projectionColumns, err := compileJoinProjectionColumns(stmt.ProjectionColumns,
			relationSchema{id: leftID, ts: leftTS}, stmt.Join.LeftTable,
			relationSchema{id: rightID, ts: rightTS}, stmt.Join.RightTable,
			aliasTag)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		if !allowProjection && len(stmt.ProjectionColumns) != 0 {
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
			return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
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
				return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: join, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, Limit: limit}, nil
			}
			cross := subscription.CrossJoin{Left: leftID, Right: rightID}
			if leftID == rightID {
				cross.LeftAlias = 0
				cross.RightAlias = 1
			}
			cross.ProjectRight = joinProjectsRight(stmt, leftID == rightID)
			return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: cross, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, Limit: limit}, nil
		}
		if _, ok := normalizedPredicate.(sql.FalsePredicate); ok {
			return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: subscription.NoRows{Table: projectedID}, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, Limit: limit}, nil
		}
		var filter subscription.Predicate
		if stmt.Join.HasOn && normalizedPredicate != nil {
			var err error
			filter, err = compileSQLPredicateForRelations(normalizedPredicate, relations, aliasTag, caller)
			if err != nil {
				return compiledSQLQuery{}, err
			}
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
			Limit:              limit,
		}, nil
	}
	projectedID, ts, ok := sl.TableByName(stmt.ProjectedTable)
	if !ok {
		return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.ProjectedTable)
	}
	// Reference type-checker order: `type_select` (WHERE) precedes
	// `type_proj` (projection columns). Reference path:
	// `SubChecker::type_set` (check.rs:139-146) wraps the projection in
	// `type_proj(type_select(input, expr, vars)?, project, vars)`, so a
	// missing WHERE column raises `Unresolved::Var` before the
	// projection list is walked. The WHERE pass also captures the
	// resolved predicate for the final compiledSQLQuery.
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
		return compiledSQLQuery{}, fmt.Errorf("Column projections are not supported in subscriptions; Subscriptions must return a table type")
	}
	limit, err := compileStatementLimit(stmt, qs)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: pred, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, Limit: limit}, nil
}

func aliasTagForJoin(stmt sql.Statement, selfJoin bool) func(string) uint8 {
	if !selfJoin {
		return func(string) uint8 { return 0 }
	}
	rightAliasUpper := strings.ToUpper(stmt.Join.RightAlias)
	return func(a string) uint8 {
		if strings.EqualFold(a, "") {
			return 0
		}
		if strings.ToUpper(a) == rightAliasUpper {
			return 1
		}
		return 0
	}
}

func resolveProjectedJoinRelation(stmt sql.Statement, leftID, rightID schema.TableID) (schema.TableID, bool) {
	switch {
	case strings.EqualFold(stmt.ProjectedAlias, stmt.Join.LeftAlias):
		return leftID, true
	case strings.EqualFold(stmt.ProjectedAlias, stmt.Join.RightAlias):
		return rightID, true
	case leftID != rightID && strings.EqualFold(stmt.ProjectedAlias, stmt.Join.LeftTable):
		return leftID, true
	case leftID != rightID && strings.EqualFold(stmt.ProjectedAlias, stmt.Join.RightTable):
		return rightID, true
	default:
		return 0, false
	}
}

func resolveJoinOnColumn(ref sql.ColumnRef, stmt sql.Statement, leftTS, rightTS *schema.TableSchema) (string, *schema.ColumnSchema, error) {
	switch {
	case strings.EqualFold(ref.Alias, stmt.Join.LeftAlias):
		col, ok := leftTS.Column(ref.Column)
		if !ok {
			return "", nil, sql.UnresolvedVarError{Name: ref.Column}
		}
		return "left", col, nil
	case strings.EqualFold(ref.Alias, stmt.Join.RightAlias):
		col, ok := rightTS.Column(ref.Column)
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
	if leftID == rightID {
		return subscription.Join{}, fmt.Errorf("self-join cross join WHERE column equality not supported")
	}
	cmp, filterPred, err := splitCrossJoinWherePredicate(stmt.Predicate)
	if err != nil {
		return subscription.Join{}, err
	}
	if cmp.Op != "=" {
		return subscription.Join{}, fmt.Errorf("cross join WHERE column comparisons only support '='")
	}
	leftRef, rightRef := cmp.Left, cmp.Right
	if strings.EqualFold(leftRef.Table, stmt.Join.RightTable) && strings.EqualFold(rightRef.Table, stmt.Join.LeftTable) {
		leftRef, rightRef = rightRef, leftRef
	}
	if !strings.EqualFold(leftRef.Table, stmt.Join.LeftTable) || !strings.EqualFold(rightRef.Table, stmt.Join.RightTable) {
		return subscription.Join{}, fmt.Errorf("cross join WHERE column equality must compare left and right relations")
	}
	leftCol, ok := leftTS.Column(leftRef.Column)
	if !ok {
		return subscription.Join{}, sql.UnresolvedVarError{Name: leftRef.Column}
	}
	rightCol, ok := rightTS.Column(rightRef.Column)
	if !ok {
		return subscription.Join{}, sql.UnresolvedVarError{Name: rightRef.Column}
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
			stmt.Join.LeftTable:  {id: leftID, ts: leftTS},
			stmt.Join.RightTable: {id: rightID, ts: rightTS},
		}, caller)
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

func splitCrossJoinWherePredicate(pred sql.Predicate) (sql.ColumnComparisonPredicate, sql.Predicate, error) {
	switch p := pred.(type) {
	case sql.ColumnComparisonPredicate:
		return p, nil, nil
	case sql.AndPredicate:
		if cmp, ok := p.Left.(sql.ColumnComparisonPredicate); ok {
			if _, rightIsColumnComparison := p.Right.(sql.ColumnComparisonPredicate); rightIsColumnComparison {
				return sql.ColumnComparisonPredicate{}, nil, fmt.Errorf("cross join WHERE supports exactly one qualified column equality")
			}
			return cmp, p.Right, nil
		}
		if cmp, ok := p.Right.(sql.ColumnComparisonPredicate); ok {
			return cmp, p.Left, nil
		}
		return sql.ColumnComparisonPredicate{}, nil, fmt.Errorf("cross join WHERE only supports qualified column equality")
	default:
		return sql.ColumnComparisonPredicate{}, nil, fmt.Errorf("cross join WHERE only supports qualified column equality")
	}
}

func compileCrossJoinWhereLiteralFilter(pred sql.Predicate, relations map[string]relationSchema, caller *types.Identity) (subscription.Predicate, error) {
	cmp, ok := pred.(sql.ComparisonPredicate)
	if !ok {
		return nil, fmt.Errorf("cross join WHERE filter supports exactly one column-literal predicate")
	}
	return normalizeSQLFilterForRelations(cmp.Filter, relations, func(string) uint8 { return 0 }, caller)
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
		if p.Filter.Alias != "" && !strings.EqualFold(p.Filter.Alias, tableAlias) {
			return nil, sql.UnresolvedVarError{Name: p.Filter.Alias}
		}
		f := p.Filter
		f.Table = tableAlias
		return normalizeSQLFilterForRelations(f, map[string]relationSchema{tableAlias: rel}, func(string) uint8 { return 0 }, caller)
	case sql.AndPredicate:
		left, err := compileSQLPredicateForSingleRelation(p.Left, rel, tableAlias, caller)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLPredicateForSingleRelation(p.Right, rel, tableAlias, caller)
		if err != nil {
			return nil, err
		}
		return subscription.And{Left: left, Right: right}, nil
	case sql.OrPredicate:
		left, err := compileSQLPredicateForSingleRelation(p.Left, rel, tableAlias, caller)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLPredicateForSingleRelation(p.Right, rel, tableAlias, caller)
		if err != nil {
			return nil, err
		}
		return subscription.Or{Left: left, Right: right}, nil
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
		if col.SourceQualifier != "" && !strings.EqualFold(col.SourceQualifier, tableAlias) {
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

func compileJoinProjectionColumns(columns []sql.ProjectionColumn, left relationSchema, leftTable string, right relationSchema, rightTable string, aliasTag func(string) uint8) ([]compiledSQLProjectionColumn, error) {
	if len(columns) == 0 {
		return nil, nil
	}
	resolved := make([]compiledSQLProjectionColumn, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		if err := checkDuplicateProjectionName(col, seen); err != nil {
			return nil, err
		}
		var rel relationSchema
		switch {
		case strings.EqualFold(col.Table, leftTable):
			rel = left
		case strings.EqualFold(col.Table, rightTable):
			rel = right
		default:
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

// checkDuplicateProjectionName mirrors the duplicate-alias guard inside
// reference `type_proj::Exprs` (expr/src/check.rs:67-72): each projection
// element's effective output name (its `AS` alias, or the column name when
// no alias was written) must be unique across the SELECT list. Reference
// emits `DuplicateName(alias)` from `lib.rs:67` which renders as
// `Duplicate name `{alias}“. The check is interleaved with the column
// resolution loop so iteration order matches reference: a duplicate alias
// is reported on the SECOND occurrence, after the FIRST has already been
// type-checked.
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
	tsCol, ok := ts.Column(col.Column)
	if !ok {
		return compiledSQLProjectionColumn{}, sql.UnresolvedVarError{Name: col.Column}
	}
	compiledCol := *tsCol
	if col.OutputAlias != "" {
		compiledCol.Name = col.OutputAlias
	}
	return compiledSQLProjectionColumn{Schema: compiledCol, Table: tableID, Alias: alias}, nil
}

// parseQueryString turns a client-supplied SQL string into the internal
// Query form used by compileQuery. It resolves the table against the
// schema and coerces each literal against the matching column kind.
// Errors carry context suitable for SubscriptionError.Error /
// OneOffQueryResponse.Error.
func parseQueryString(qs string, sl SchemaLookup, caller *types.Identity) (Query, error) {
	stmt, err := sql.Parse(qs)
	if err != nil {
		return Query{}, fmt.Errorf("parse: %v", err)
	}
	_, ts, ok := sl.TableByName(stmt.Table)
	if !ok {
		return Query{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.Table)
	}
	q := Query{TableName: stmt.Table}
	for _, f := range stmt.Filters {
		col, ok := ts.Column(f.Column)
		if !ok {
			return Query{}, sql.UnresolvedVarError{Name: f.Column}
		}
		v, err := coerceLiteral(f.Literal, col.Type, caller)
		if err != nil {
			return Query{}, fmt.Errorf("coerce column %q: %v", f.Column, err)
		}
		q.Predicates = append(q.Predicates, Predicate{Column: f.Column, Op: f.Op, Value: v})
	}
	return q, nil
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
	case sql.AndPredicate:
		left, err := compileSQLPredicateForRelations(p.Left, relations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLPredicateForRelations(p.Right, relations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		return subscription.And{Left: left, Right: right}, nil
	case sql.OrPredicate:
		left, err := compileSQLPredicateForRelations(p.Left, relations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLPredicateForRelations(p.Right, relations, aliasTag, caller)
		if err != nil {
			return nil, err
		}
		return subscription.Or{Left: left, Right: right}, nil
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

func normalizeSQLFilterForRelations(f sql.Filter, relations map[string]relationSchema, aliasTag func(string) uint8, caller *types.Identity) (subscription.Predicate, error) {
	rel, ok := relations[f.Table]
	if !ok {
		name := f.Alias
		if name == "" {
			name = f.Table
		}
		return nil, sql.UnresolvedVarError{Name: name}
	}
	col, ok := rel.ts.Column(f.Column)
	if !ok {
		return nil, sql.UnresolvedVarError{Name: f.Column}
	}
	v, err := coerceLiteral(f.Literal, col.Type, caller)
	if err != nil {
		// Parity error types carry the reference literal verbatim; do
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

// NormalizePredicates converts a slice of wire-level Predicate (column
// comparison + value) into a single subscription.Predicate tree suitable
// for the evaluator. Empty predicates produce AllRows; a single
// predicate produces ColEq/ColRange; multiple predicates are folded left
// into nested And nodes.
func NormalizePredicates(
	tableID schema.TableID,
	ts *schema.TableSchema,
	preds []Predicate,
) (subscription.Predicate, error) {
	if len(preds) == 0 {
		return subscription.AllRows{Table: tableID}, nil
	}

	eqs := make([]subscription.Predicate, 0, len(preds))
	for _, p := range preds {
		col, ok := ts.Column(p.Column)
		if !ok {
			return nil, sql.UnresolvedVarError{Name: p.Column}
		}
		normalized, err := normalizePredicate(tableID, col.Index, 0, p.Op, p.Value)
		if err != nil {
			return nil, err
		}
		eqs = append(eqs, normalized)
	}

	if len(eqs) == 1 {
		return eqs[0], nil
	}

	result := subscription.And{Left: eqs[0], Right: eqs[1]}
	for i := 2; i < len(eqs); i++ {
		result = subscription.And{Left: result, Right: eqs[i]}
	}
	return result, nil
}
