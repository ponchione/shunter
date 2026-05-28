package protocol

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	queryplan "github.com/ponchione/shunter/internal/queryplan"
	"github.com/ponchione/shunter/internal/valueagg"
	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type compiledSQLQuery struct {
	TableName          string
	Predicate          subscription.Predicate
	UsesCallerIdentity bool
	MultiJoin          *compiledSQLMultiJoin
	ProjectionColumns  []compiledSQLProjectionColumn
	Aggregate          *compiledSQLAggregate
	OrderBy            []compiledSQLOrderBy
	OrderByPresent     bool
	Limit              *uint64
	Offset             *uint64
}

type compiledSQLProjectionColumn struct {
	Schema schema.ColumnSchema
	Table  schema.TableID
	Alias  uint8
}

type compiledSQLAggregate struct {
	Func         string
	Argument     *compiledSQLProjectionColumn
	Distinct     bool
	ResultColumn schema.ColumnSchema
}

type compiledSQLOrderBy struct {
	Column   compiledSQLProjectionColumn
	Desc     bool
	Relation int
}

type compiledSQLMultiJoin struct {
	Relations         []compiledSQLMultiJoinRelation
	Conditions        []compiledSQLMultiJoinCondition
	Filter            *compiledSQLMultiPredicate
	ProjectedRelation int
}

type compiledSQLMultiJoinRelation struct {
	Table      schema.TableID
	Alias      uint8
	Qualifier  string
	Schema     *schema.TableSchema
	Visibility subscription.Predicate
}

type compiledSQLMultiJoinCondition struct {
	Left  compiledSQLMultiColumnRef
	Right compiledSQLMultiColumnRef
}

type compiledSQLMultiColumnRef struct {
	Column   compiledSQLColumnRef
	Relation int
}

type compiledSQLMultiPredicateKind uint8

const (
	compiledSQLMultiPredicateTrue compiledSQLMultiPredicateKind = iota
	compiledSQLMultiPredicateFalse
	compiledSQLMultiPredicateComparison
	compiledSQLMultiPredicateColumnComparison
	compiledSQLMultiPredicateAnd
	compiledSQLMultiPredicateOr
)

type compiledSQLMultiPredicate struct {
	Kind        compiledSQLMultiPredicateKind
	Left        *compiledSQLMultiPredicate
	Right       *compiledSQLMultiPredicate
	Column      compiledSQLMultiColumnRef
	Op          string
	Value       types.Value
	LeftColumn  compiledSQLMultiColumnRef
	RightColumn compiledSQLMultiColumnRef
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
	compiledSQLQueryAccessor
}

type compiledSQLQueryAccessor struct {
	query compiledSQLQuery
}

func newCompiledSQLQuery(query compiledSQLQuery) CompiledSQLQuery {
	return CompiledSQLQuery{compiledSQLQueryAccessor{query: copyCompiledSQLQuery(query)}}
}

// Copy returns a detached copy of the compiled SQL metadata.
func (q CompiledSQLQuery) Copy() CompiledSQLQuery {
	return newCompiledSQLQuery(q.query)
}

// TableName returns the projected table name for this compiled SQL metadata.
func (q compiledSQLQueryAccessor) TableName() string {
	return q.query.TableName
}

// Predicate returns the compiled subscription predicate backing this metadata.
func (q compiledSQLQueryAccessor) Predicate() subscription.Predicate {
	return q.query.Predicate
}

// SubscriptionProjection returns the optional live-row projection metadata.
func (q compiledSQLQueryAccessor) SubscriptionProjection() []subscription.ProjectionColumn {
	if len(q.query.ProjectionColumns) == 0 {
		return nil
	}
	out := make([]subscription.ProjectionColumn, len(q.query.ProjectionColumns))
	for i, col := range q.query.ProjectionColumns {
		out[i] = subscription.ProjectionColumn{
			Schema: col.Schema,
			Table:  col.Table,
			Column: types.ColID(col.Schema.Index),
			Alias:  col.Alias,
		}
	}
	return out
}

// SubscriptionAggregate returns the optional live aggregate row shape.
func (q compiledSQLQueryAccessor) SubscriptionAggregate() *subscription.Aggregate {
	if q.query.Aggregate == nil {
		return nil
	}
	agg := q.query.Aggregate
	out := &subscription.Aggregate{
		Func:         subscription.AggregateFunc(agg.Func),
		Distinct:     agg.Distinct,
		ResultColumn: agg.ResultColumn,
	}
	if agg.Argument != nil {
		out.Argument = &subscription.AggregateColumn{
			Schema: agg.Argument.Schema,
			Table:  agg.Argument.Table,
			Column: types.ColID(agg.Argument.Schema.Index),
			Alias:  agg.Argument.Alias,
		}
	}
	return out
}

// ResultColumns returns the encoded output columns for this metadata.
func (q compiledSQLQueryAccessor) ResultColumns(sl SchemaLookup) []schema.ColumnSchema {
	var fallback []schema.ColumnSchema
	if sl != nil {
		_, table, ok := sl.TableByName(q.query.TableName)
		if ok && table != nil {
			fallback = table.Columns
		}
	}
	return q.query.resultColumns(fallback)
}

func (query compiledSQLQuery) resultColumns(fallback []schema.ColumnSchema) []schema.ColumnSchema {
	if query.Aggregate != nil {
		return []schema.ColumnSchema{query.Aggregate.ResultColumn}
	}
	if len(query.ProjectionColumns) != 0 {
		columns := make([]schema.ColumnSchema, len(query.ProjectionColumns))
		for i, col := range query.ProjectionColumns {
			columns[i] = col.Schema
		}
		return columns
	}
	return slices.Clone(fallback)
}

// SubscriptionOrderBy returns optional ordering metadata for declared
// single-table live windows. It does not imply positional delta semantics.
func (q compiledSQLQueryAccessor) SubscriptionOrderBy() []subscription.OrderByColumn {
	if len(q.query.OrderBy) == 0 {
		return nil
	}
	out := make([]subscription.OrderByColumn, len(q.query.OrderBy))
	for i, term := range q.query.OrderBy {
		out[i] = subscription.OrderByColumn{
			Schema: term.Column.Schema,
			Table:  term.Column.Table,
			Column: types.ColID(term.Column.Schema.Index),
			Alias:  term.Column.Alias,
			Desc:   term.Desc,
		}
	}
	return out
}

// SubscriptionLimit returns optional LIMIT metadata for declared single-table
// live windows.
func (q compiledSQLQueryAccessor) SubscriptionLimit() *uint64 {
	if q.query.Limit == nil {
		return nil
	}
	limit := *q.query.Limit
	return &limit
}

// SubscriptionOffset returns optional OFFSET metadata for declared single-table
// live windows.
func (q compiledSQLQueryAccessor) SubscriptionOffset() *uint64 {
	if q.query.Offset == nil {
		return nil
	}
	offset := *q.query.Offset
	return &offset
}

// HasOrderBy reports whether the source SQL included an ORDER BY clause.
func (q compiledSQLQueryAccessor) HasOrderBy() bool {
	return q.query.OrderByPresent || len(q.query.OrderBy) != 0
}

// HasLimit reports whether the source SQL included a LIMIT clause.
func (q compiledSQLQueryAccessor) HasLimit() bool {
	return q.query.Limit != nil
}

// HasOffset reports whether the source SQL included an OFFSET clause.
func (q compiledSQLQueryAccessor) HasOffset() bool {
	return q.query.Offset != nil
}

// HasAggregate reports whether this metadata returns an aggregate row shape.
func (q compiledSQLQueryAccessor) HasAggregate() bool {
	return q.query.Aggregate != nil
}

// UsesCallerIdentity reports whether the compiled SQL metadata references :sender.
func (q compiledSQLQueryAccessor) UsesCallerIdentity() bool {
	return q.query.UsesCallerIdentity
}

// ReferencedTables returns the table IDs referenced by the compiled predicate.
func (q compiledSQLQueryAccessor) ReferencedTables() []schema.TableID {
	if q.query.MultiJoin != nil {
		return q.query.MultiJoin.referencedTables()
	}
	if q.query.Predicate == nil {
		return nil
	}
	tables := q.query.Predicate.Tables()
	return slices.Clone(tables)
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
		out.ProjectionColumns = slices.Clone(query.ProjectionColumns)
	}
	if query.Aggregate != nil {
		aggregate := *query.Aggregate
		if query.Aggregate.Argument != nil {
			argument := *query.Aggregate.Argument
			aggregate.Argument = &argument
		}
		out.Aggregate = &aggregate
	}
	if query.MultiJoin != nil {
		out.MultiJoin = copyCompiledSQLMultiJoin(query.MultiJoin)
		pred, err := subscriptionMultiJoinPredicate(out.MultiJoin)
		if err != nil {
			panic("protocol: invalid compiled multi-join predicate: " + err.Error())
		}
		out.Predicate = pred
	}
	if len(query.OrderBy) > 0 {
		out.OrderBy = slices.Clone(query.OrderBy)
	}
	if query.Limit != nil {
		limit := *query.Limit
		out.Limit = &limit
	}
	if query.Offset != nil {
		offset := *query.Offset
		out.Offset = &offset
	}
	return out
}

// SQLQueryValidationOptions controls how ValidateSQLQueryString applies the
// protocol SQL compiler to authored declaration metadata.
type SQLQueryValidationOptions struct {
	AllowLimit      bool
	AllowProjection bool
	AllowOrderBy    bool
	AllowOffset     bool
}

// SQLQueryParameter declares one app placeholder accepted by declared-read SQL
// template validation. :sender is caller identity and is not a parameter.
type SQLQueryParameter struct {
	Name string
	Type types.ValueKind
}

// SQLQueryParameterValue binds one declared-read app placeholder to a concrete
// runtime value. :sender remains caller identity and is not a parameter.
type SQLQueryParameterValue struct {
	Name  string
	Value types.Value
}

// CompiledSQLQueryTemplate is SQL metadata validated against declared
// parameters. Runtime execution must bind concrete parameter values through
// CompileSQLQueryStringWithParameters.
type CompiledSQLQueryTemplate struct {
	compiledSQLQueryAccessor
}

func newCompiledSQLQueryTemplate(query compiledSQLQuery) CompiledSQLQueryTemplate {
	return CompiledSQLQueryTemplate{compiledSQLQueryAccessor{query: copyCompiledSQLQuery(query)}}
}

// Copy returns a detached copy of the compiled SQL template metadata.
func (q CompiledSQLQueryTemplate) Copy() CompiledSQLQueryTemplate {
	return newCompiledSQLQueryTemplate(q.query)
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

// ValidateSQLQueryTemplateString validates declared-read SQL against its
// parameter schema without accepting those parameters on raw SQL surfaces.
func ValidateSQLQueryTemplateString(qs string, sl SchemaLookup, opts SQLQueryValidationOptions, parameters []SQLQueryParameter) error {
	if sl == nil {
		return fmt.Errorf("schema lookup must not be nil")
	}
	var caller types.Identity
	_, err := CompileSQLQueryTemplateString(qs, sl, &caller, opts, parameters)
	return err
}

// CompileSQLQueryString compiles SQL against the supplied schema lookup and
// caller identity. It is a narrow runtime seam for declared reads; raw external
// SQL must still pass an auth-aware SchemaLookup when using this compiler.
func CompileSQLQueryString(qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions) (CompiledSQLQuery, error) {
	compiled, err := compileSQLQueryWithCompiler(qs, sl, runtimeSQLLiteralCompiler{caller: caller}, opts, nil)
	if err != nil {
		return CompiledSQLQuery{}, err
	}
	return newCompiledSQLQuery(compiled), nil
}

// CompileSQLQueryStringWithParameters compiles declared-read SQL while binding
// app placeholders to concrete values. Raw SQL protocol surfaces must not use
// this helper.
func CompileSQLQueryStringWithParameters(qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions, parameters []SQLQueryParameterValue) (CompiledSQLQuery, error) {
	compiled, err := compileSQLQueryWithParameterCompiler(qs, sl, caller, opts, parameters, newBoundSQLLiteralCompiler)
	if err != nil {
		return CompiledSQLQuery{}, err
	}
	return newCompiledSQLQuery(compiled), nil
}

// CompileSQLQueryTemplateString compiles declared-read SQL metadata against the
// supplied parameter schema.
func CompileSQLQueryTemplateString(qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions, parameters []SQLQueryParameter) (CompiledSQLQueryTemplate, error) {
	compiled, err := compileSQLQueryWithParameterCompiler(qs, sl, caller, opts, parameters, newTemplateSQLLiteralCompiler)
	if err != nil {
		return CompiledSQLQueryTemplate{}, err
	}
	return newCompiledSQLQueryTemplate(compiled), nil
}

func compileSQLQueryWithParameterCompiler[P any](qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions, parameters []P, build func(*types.Identity, []P) (*parameterSQLLiteralCompiler, error)) (compiledSQLQuery, error) {
	compiler := sqlLiteralCompiler(runtimeSQLLiteralCompiler{caller: caller})
	var validateUsed func() error
	if len(parameters) != 0 {
		parameterCompiler, err := build(caller, parameters)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		compiler = parameterCompiler
		validateUsed = parameterCompiler.validateUsed
	}
	return compileSQLQueryWithCompiler(qs, sl, compiler, opts, validateUsed)
}

func compileSQLQueryWithCompiler(qs string, sl SchemaLookup, literalCompiler sqlLiteralCompiler, opts SQLQueryValidationOptions, validateUsed func() error) (compiledSQLQuery, error) {
	if sl == nil {
		return compiledSQLQuery{}, fmt.Errorf("schema lookup must not be nil")
	}
	compiled, err := compileSQLQueryString(qs, sl, literalCompiler, opts.AllowLimit, opts.AllowProjection, opts.AllowOrderBy, opts.AllowOffset)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	if validateUsed == nil {
		return compiled, nil
	}
	return compiled, validateUsed()
}

// CompileSQLQueryStringWithVisibility compiles SQL and expands matching
// row-level visibility filters into every table relation before execution.
func CompileSQLQueryStringWithVisibility(qs string, sl SchemaLookup, caller *types.Identity, opts SQLQueryValidationOptions, filters []VisibilityFilter, allowAll bool) (CompiledSQLQuery, error) {
	compiled, err := CompileSQLQueryString(qs, sl, caller, opts)
	if err != nil {
		return CompiledSQLQuery{}, err
	}
	if allowAll || len(filters) == 0 {
		return compiled, nil
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
	if query.MultiJoin != nil {
		if !allowAll && len(filters) != 0 {
			usesCallerIdentity, err := applyMultiJoinVisibility(query.MultiJoin, sl, caller, filters)
			if err != nil {
				return CompiledSQLQuery{}, err
			}
			query.UsesCallerIdentity = query.UsesCallerIdentity || usesCallerIdentity
			pred, err := subscriptionMultiJoinPredicate(query.MultiJoin)
			if err != nil {
				return CompiledSQLQuery{}, err
			}
			query.Predicate = pred
		}
		return newCompiledSQLQuery(query), nil
	}
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
	case subscription.ColEqCol:
		if p.LeftTable == table {
			p.LeftAlias = alias
		}
		if p.RightTable == table {
			p.RightAlias = alias
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

func compileSQLQueryString(qs string, sl SchemaLookup, literalCompiler sqlLiteralCompiler, allowLimit bool, allowProjection bool, allowOrderBy bool, allowOffset bool) (compiledSQLQuery, error) {
	plan, err := queryplan.Build(qs, queryplan.Options{
		AllowLimit:   allowLimit,
		AllowOrderBy: allowOrderBy,
		AllowOffset:  allowOffset,
	})
	if err != nil {
		return compiledSQLQuery{}, err
	}
	stmt := plan.Statement
	stmtOrderBy := plan.OrderBy
	// Resolve tables, predicates, and projections before return-shape guards.
	// Keep the original predicate tree so folding does not hide type errors.
	normalizedPredicate := plan.NormalizedPredicate
	usesCallerIdentity := plan.UsesCallerIdentity
	if len(stmt.Joins) > 1 {
		return compileMultiJoinSQLQuery(stmt, stmtOrderBy, normalizedPredicate, usesCallerIdentity, qs, sl, literalCompiler, allowProjection)
	}
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
			if _, err := compileSQLPredicateForRelations(stmt.Predicate, relations, aliasTagForJoin(stmt, leftID == rightID), literalCompiler, true); err != nil {
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
			return compiledSQLQuery{}, errSubscriptionRequiresTableShape
		}
		aggregate, err := compileJoinAggregateProjection(stmt.Aggregate, relations, aliasTag)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		// Reference `expect_table_type` (check.rs:168-176) rejects
		// `ProjectList::Agg` with `Unsupported::ReturnType` AFTER
		// `type_proj` resolves the projection. Aggregate guard mirrors
		// the column-list guard above so schema/WHERE/JOIN-ON errors
		// surface first.
		if !allowProjection && stmt.Aggregate != nil {
			return compiledSQLQuery{}, errSubscriptionRequiresTableShape
		}
		var orderBy []compiledSQLOrderBy
		if stmt.Aggregate != nil {
			orderBy, err = compileAggregateOrderBy(stmtOrderBy, aggregate, stmt.ProjectedTable)
		} else {
			orderBy, err = compileJoinOrderBy(stmtOrderBy, stmt, relations, projectionColumns, projectedID, aliasTag, leftID == rightID)
		}
		if err != nil {
			return compiledSQLQuery{}, err
		}
		limit, err := compileStatementLimit(stmt, qs)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		offset, err := compileStatementOffset(stmt, qs)
		if err != nil {
			return compiledSQLQuery{}, err
		}
		result := func(pred subscription.Predicate) compiledSQLQuery {
			return compiledSQLQuery{
				TableName:          stmt.ProjectedTable,
				Predicate:          pred,
				UsesCallerIdentity: usesCallerIdentity,
				ProjectionColumns:  projectionColumns,
				Aggregate:          aggregate,
				OrderBy:            orderBy,
				OrderByPresent:     len(stmtOrderBy) != 0,
				Limit:              limit,
				Offset:             offset,
			}
		}
		if !stmt.Join.HasOn {
			if stmt.Predicate != nil {
				join, err := compileCrossJoinWhereColumnEquality(stmt, leftID, leftTS, rightID, rightTS, literalCompiler)
				if err != nil {
					return compiledSQLQuery{}, err
				}
				if !allowProjection && !sl.HasIndex(join.Left, join.LeftCol) && !sl.HasIndex(join.Right, join.RightCol) {
					//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
					return compiledSQLQuery{}, fmt.Errorf("Subscriptions require indexes on join columns")
				}
				return result(join), nil
			}
			cross := subscription.CrossJoin{Left: leftID, Right: rightID}
			if leftID == rightID {
				cross.LeftAlias = 0
				cross.RightAlias = 1
			}
			cross.ProjectRight = joinProjectsRight(stmt, leftID == rightID)
			return result(cross), nil
		}
		if _, ok := normalizedPredicate.(sql.FalsePredicate); ok {
			return result(subscription.NoRows{Table: projectedID}), nil
		}
		var filter subscription.Predicate
		if stmt.Join.HasOn && normalizedPredicate != nil {
			var err error
			filter, err = compileSQLPredicateForRelations(normalizedPredicate, relations, aliasTag, literalCompiler, true)
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
		return result(join), nil
	}
	projectedID, ts, ok := lookupSQLTableExact(sl, stmt.ProjectedTable)
	if !ok {
		//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
		return compiledSQLQuery{}, fmt.Errorf("no such table: `%s`. If the table exists, it may be marked private.", stmt.ProjectedTable)
	}
	// Resolve WHERE before projection columns so predicate errors win.
	if _, err := compileSQLPredicateForSingleRelation(stmt.Predicate, relationSchema{id: projectedID, ts: ts}, stmt.TableAlias, literalCompiler); err != nil {
		return compiledSQLQuery{}, err
	}
	pred, err := compileSQLPredicateForSingleRelation(normalizedPredicate, relationSchema{id: projectedID, ts: ts}, stmt.TableAlias, literalCompiler)
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
		return compiledSQLQuery{}, errSubscriptionRequiresTableShape
	}
	aggregate, err := compileSingleRelationAggregateProjection(stmt.Aggregate, stmt.ProjectedTable, stmt.TableAlias, projectedID, ts)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	// Reference `expect_table_type` (check.rs:168-176) rejects
	// `ProjectList::Agg` with `Unsupported::ReturnType` AFTER `type_proj`
	// resolves the projection. Aggregate guard mirrors the column-list
	// guard above so schema/WHERE errors surface first.
	if !allowProjection && stmt.Aggregate != nil {
		return compiledSQLQuery{}, errSubscriptionRequiresTableShape
	}
	var orderBy []compiledSQLOrderBy
	if stmt.Aggregate != nil {
		orderBy, err = compileAggregateOrderBy(stmtOrderBy, aggregate, stmt.ProjectedTable)
	} else {
		orderBy, err = compileSingleRelationOrderBy(stmtOrderBy, stmt.ProjectedTable, stmt.TableAlias, projectedID, ts, projectionColumns)
	}
	if err != nil {
		return compiledSQLQuery{}, err
	}
	limit, err := compileStatementLimit(stmt, qs)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	offset, err := compileStatementOffset(stmt, qs)
	if err != nil {
		return compiledSQLQuery{}, err
	}
	return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: pred, UsesCallerIdentity: usesCallerIdentity, ProjectionColumns: projectionColumns, Aggregate: aggregate, OrderBy: orderBy, OrderByPresent: len(stmtOrderBy) != 0, Limit: limit, Offset: offset}, nil
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
	return compileStatementUint64Clause("LIMIT", stmt.UnsupportedLimit, stmt.InvalidLimit, stmt.Limit, sqlText)
}

func compileStatementOffset(stmt sql.Statement, sqlText string) (*uint64, error) {
	return compileStatementUint64Clause("OFFSET", stmt.UnsupportedOffset, stmt.InvalidOffset, stmt.Offset, sqlText)
}

func compileStatementUint64Clause(name string, unsupported bool, invalid *sql.Literal, value *uint64, sqlText string) (*uint64, error) {
	if unsupported {
		return nil, sql.UnsupportedFeatureError{SQL: sqlText}
	}
	if invalid == nil {
		return value, nil
	}
	if invalid.Kind == sql.LitParameter {
		return nil, fmt.Errorf("%w: SQL parameter %s is not supported in %s", sql.ErrUnsupportedSQL, limitLiteralText(*invalid), name)
	}
	return nil, sql.InvalidLiteralError{Literal: limitLiteralText(*invalid), Type: "U64"}
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

func compileAggregateProjection(agg *sql.AggregateProjection, argument *compiledSQLProjectionColumn) (*compiledSQLAggregate, error) {
	if agg == nil {
		return nil, nil
	}
	switch {
	case strings.EqualFold(agg.Func, "COUNT"):
		if agg.Distinct && argument == nil {
			return nil, fmt.Errorf("COUNT(DISTINCT ...) aggregate requires a column argument")
		}
		return &compiledSQLAggregate{
			Func:         "COUNT",
			Argument:     argument,
			Distinct:     agg.Distinct,
			ResultColumn: schema.ColumnSchema{Index: 0, Name: agg.Alias, Type: schema.KindUint64},
		}, nil
	case strings.EqualFold(agg.Func, "SUM"):
		if argument == nil {
			return nil, fmt.Errorf("SUM aggregate requires a column argument")
		}
		resultKind, ok := valueagg.SumResultKind(argument.Schema.Type)
		if !ok {
			return nil, fmt.Errorf("SUM aggregate only supports 64-bit integer and float columns")
		}
		return &compiledSQLAggregate{
			Func:         "SUM",
			Argument:     argument,
			Distinct:     agg.Distinct,
			ResultColumn: schema.ColumnSchema{Index: 0, Name: agg.Alias, Type: resultKind, Nullable: argument.Schema.Nullable},
		}, nil
	default:
		return nil, fmt.Errorf("aggregate projections not supported")
	}
}

func compileSingleRelationAggregateProjection(agg *sql.AggregateProjection, projectedTable string, tableAlias string, tableID schema.TableID, ts *schema.TableSchema) (*compiledSQLAggregate, error) {
	if agg == nil || agg.Column == nil {
		return compileAggregateProjection(agg, nil)
	}
	ref := *agg.Column
	if ref.Alias != "" && ref.Alias != tableAlias {
		return nil, sql.UnresolvedVarError{Name: ref.Alias}
	}
	if ref.Alias == "" && ref.Table != projectedTable {
		return nil, sql.UnresolvedVarError{Name: ref.Table}
	}
	col, ok := lookupSQLColumnExact(ts, ref.Column)
	if !ok {
		return nil, sql.UnresolvedVarError{Name: ref.Column}
	}
	argument := compiledSQLProjectionColumn{Schema: *col, Table: tableID, Alias: 0}
	return compileAggregateProjection(agg, &argument)
}

func compileJoinAggregateProjection(agg *sql.AggregateProjection, relations map[string]relationSchema, aliasTag func(string) uint8) (*compiledSQLAggregate, error) {
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

func compileCrossJoinWhereColumnEquality(stmt sql.Statement, leftID schema.TableID, leftTS *schema.TableSchema, rightID schema.TableID, rightTS *schema.TableSchema, literalCompiler sqlLiteralCompiler) (subscription.Join, error) {
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
		}, literalCompiler, leftID)
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

func compileCrossJoinWhereLiteralFilter(pred sql.Predicate, relations map[string]relationSchema, literalCompiler sqlLiteralCompiler, noRowsTable schema.TableID) (subscription.Predicate, error) {
	if _, err := compileSQLPredicateForRelations(pred, relations, func(string) uint8 { return 0 }, literalCompiler, false); err != nil {
		return nil, err
	}
	normalized := queryplan.NormalizePredicate(pred)
	switch p := normalized.(type) {
	case nil:
		return nil, nil
	case sql.TruePredicate:
		return nil, nil
	case sql.FalsePredicate:
		return subscription.NoRows{Table: noRowsTable}, nil
	default:
		return compileSQLPredicateForRelations(p, relations, func(string) uint8 { return 0 }, literalCompiler, false)
	}
}

func compileSQLPredicateForSingleRelation(pred sql.Predicate, rel relationSchema, tableAlias string, literalCompiler sqlLiteralCompiler) (subscription.Predicate, error) {
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
		return normalizeSQLFilterForRelations(f, map[string]relationSchema{tableAlias: rel}, func(string) uint8 { return 0 }, literalCompiler)
	case sql.NullPredicate:
		if p.Column.Alias != "" && p.Column.Alias != tableAlias {
			return nil, sql.UnresolvedVarError{Name: p.Column.Alias}
		}
		p.Column.Table = tableAlias
		return normalizeSQLNullPredicateForRelations(p, map[string]relationSchema{tableAlias: rel}, func(string) uint8 { return 0 })
	case sql.AndPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForSingleRelation(child, rel, tableAlias, literalCompiler)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.And{Left: left, Right: right}
		})
	case sql.OrPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForSingleRelation(child, rel, tableAlias, literalCompiler)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.Or{Left: left, Right: right}
		})
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

func compileProjectionColumns(projectedTable string, tableAlias string, columns []sql.ProjectionColumn, tableID schema.TableID, ts *schema.TableSchema) ([]compiledSQLProjectionColumn, error) {
	return compileProjectionColumnList(columns, func(col sql.ProjectionColumn) (compiledSQLProjectionColumn, error) {
		if col.SourceQualifier != "" && col.SourceQualifier != tableAlias {
			return compiledSQLProjectionColumn{}, sql.UnresolvedVarError{Name: projectionQualifierName(col)}
		}
		return compileProjectionColumn(col, tableID, ts, 0)
	})
}

func compileJoinProjectionColumns(columns []sql.ProjectionColumn, relations map[string]relationSchema, aliasTag func(string) uint8) ([]compiledSQLProjectionColumn, error) {
	return compileProjectionColumnList(columns, func(col sql.ProjectionColumn) (compiledSQLProjectionColumn, error) {
		qualifier := col.SourceQualifier
		if qualifier == "" {
			qualifier = col.Table
		}
		rel, ok := relations[qualifier]
		if !ok {
			return compiledSQLProjectionColumn{}, sql.UnresolvedVarError{Name: projectionQualifierName(col)}
		}
		return compileProjectionColumn(col, rel.id, rel.ts, aliasTag(col.SourceQualifier))
	})
}

func compileProjectionColumnList(columns []sql.ProjectionColumn, compile func(sql.ProjectionColumn) (compiledSQLProjectionColumn, error)) ([]compiledSQLProjectionColumn, error) {
	if len(columns) == 0 {
		return nil, nil
	}
	resolved := make([]compiledSQLProjectionColumn, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		if err := checkDuplicateProjectionName(col, seen); err != nil {
			return nil, err
		}
		compiledCol, err := compile(col)
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

func compileAggregateOrderBy(orderBy []sql.OrderByColumn, aggregate *compiledSQLAggregate, projectedTable string) ([]compiledSQLOrderBy, error) {
	if len(orderBy) == 0 {
		return nil, nil
	}
	if aggregate == nil {
		return nil, fmt.Errorf("aggregate metadata must not be nil")
	}
	for _, term := range orderBy {
		if term.SourceQualifier != "" || term.Column != aggregate.ResultColumn.Name || (term.Table != "" && term.Table != projectedTable) {
			return nil, fmt.Errorf("ORDER BY with aggregate projections only supports the aggregate output alias")
		}
	}
	return nil, nil
}

func compileSingleRelationOrderBy(orderBy []sql.OrderByColumn, projectedTable string, tableAlias string, tableID schema.TableID, ts *schema.TableSchema, projectionColumns []compiledSQLProjectionColumn) ([]compiledSQLOrderBy, error) {
	return compileOrderByList(orderBy, func(term sql.OrderByColumn) (compiledSQLOrderBy, error) {
		return compileSingleRelationOrderByTerm(term, projectedTable, tableAlias, tableID, ts, projectionColumns)
	})
}

func compileSingleRelationOrderByTerm(orderBy sql.OrderByColumn, projectedTable string, tableAlias string, tableID schema.TableID, ts *schema.TableSchema, projectionColumns []compiledSQLProjectionColumn) (compiledSQLOrderBy, error) {
	if orderBy.SourceQualifier != "" && orderBy.SourceQualifier != tableAlias {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.SourceQualifier}
	}
	if orderBy.SourceQualifier == "" {
		if orderBy.Table != projectedTable {
			return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Table}
		}
		projectionCol, projectionOK, err := resolveOrderByProjectionOutputName(orderBy.Column, projectionColumns)
		if err != nil {
			return compiledSQLOrderBy{}, err
		}
		tableCol, tableOK := lookupSQLColumnExact(ts, orderBy.Column)
		if projectionOK {
			if tableOK && tableCol.Index != projectionCol.Schema.Index {
				return compiledSQLOrderBy{}, fmt.Errorf("ORDER BY name %q is ambiguous", orderBy.Column)
			}
			return compiledSQLOrderBy{
				Column: projectionCol,
				Desc:   orderBy.Desc,
			}, nil
		}
		if tableOK {
			return compiledSQLOrderBy{
				Column: compiledSQLProjectionColumn{Schema: *tableCol, Table: tableID, Alias: 0},
				Desc:   orderBy.Desc,
			}, nil
		}
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	col, ok := lookupSQLColumnExact(ts, orderBy.Column)
	if !ok {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	return compiledSQLOrderBy{
		Column: compiledSQLProjectionColumn{Schema: *col, Table: tableID, Alias: 0},
		Desc:   orderBy.Desc,
	}, nil
}

func compileJoinOrderBy(orderBy []sql.OrderByColumn, stmt sql.Statement, relations map[string]relationSchema, projectionColumns []compiledSQLProjectionColumn, projectedID schema.TableID, aliasTag func(string) uint8, selfJoin bool) ([]compiledSQLOrderBy, error) {
	return compileOrderByList(orderBy, func(term sql.OrderByColumn) (compiledSQLOrderBy, error) {
		return compileJoinOrderByTerm(term, stmt, relations, projectionColumns, projectedID, aliasTag, selfJoin)
	})
}

func compileJoinOrderByTerm(orderBy sql.OrderByColumn, stmt sql.Statement, relations map[string]relationSchema, projectionColumns []compiledSQLProjectionColumn, projectedID schema.TableID, aliasTag func(string) uint8, selfJoin bool) (compiledSQLOrderBy, error) {
	if orderBy.SourceQualifier == "" && orderBy.Table == "" {
		projectionCol, ok, err := resolveOrderByProjectionOutputName(orderBy.Column, projectionColumns)
		if err != nil {
			return compiledSQLOrderBy{}, err
		}
		if ok {
			return compiledSQLOrderBy{
				Column: projectionCol,
				Desc:   orderBy.Desc,
			}, nil
		}
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	qualifier := orderBy.SourceQualifier
	if qualifier == "" {
		qualifier = orderBy.Table
	}
	rel, ok := relations[qualifier]
	if !ok {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: qualifier}
	}
	if rel.id != projectedID || (selfJoin && aliasTag(qualifier) != aliasTag(stmt.ProjectedAlias)) {
		return compiledSQLOrderBy{}, fmt.Errorf("ORDER BY only supports columns from the projected table")
	}
	col, ok := lookupSQLColumnExact(rel.ts, orderBy.Column)
	if !ok {
		return compiledSQLOrderBy{}, sql.UnresolvedVarError{Name: orderBy.Column}
	}
	return compiledSQLOrderBy{
		Column: compiledSQLProjectionColumn{Schema: *col, Table: rel.id, Alias: aliasTag(qualifier)},
		Desc:   orderBy.Desc,
	}, nil
}

func compileOrderByList(orderBy []sql.OrderByColumn, compile func(sql.OrderByColumn) (compiledSQLOrderBy, error)) ([]compiledSQLOrderBy, error) {
	if len(orderBy) == 0 {
		return nil, nil
	}
	compiled := make([]compiledSQLOrderBy, 0, len(orderBy))
	for _, term := range orderBy {
		resolved, err := compile(term)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, resolved)
	}
	return compiled, nil
}

func resolveOrderByProjectionOutputName(name string, columns []compiledSQLProjectionColumn) (compiledSQLProjectionColumn, bool, error) {
	var match compiledSQLProjectionColumn
	found := false
	for _, col := range columns {
		if col.Schema.Name != name {
			continue
		}
		if found {
			return compiledSQLProjectionColumn{}, false, fmt.Errorf("ORDER BY name %q is ambiguous", name)
		}
		match = col
		found = true
	}
	return match, found, nil
}

// isArrayKind reports whether a column kind is an array/product kind that
// cannot participate in equality or range comparisons. Today the only
// realized array kind is KindArrayString; the helper keeps the join-ON and
// future filter guards in one place so additional array element widenings
// stay narrow.
func isArrayKind(k types.ValueKind) bool {
	return k == types.KindArrayString
}

type sqlLiteralCompiler interface {
	compileSQLLiteral(lit sql.Literal, kind types.ValueKind, columnName string) (types.Value, error)
}

type runtimeSQLLiteralCompiler struct {
	caller *types.Identity
}

func (c runtimeSQLLiteralCompiler) compileSQLLiteral(lit sql.Literal, kind types.ValueKind, _ string) (types.Value, error) {
	if c.caller != nil {
		raw := (*[32]byte)(c.caller)
		return sql.CoerceWithCaller(lit, kind, raw)
	}
	return sql.Coerce(lit, kind)
}

type sqlParameterUsage struct {
	order []string
	used  map[string]struct{}
}

func newSQLParameterUsage(size int) sqlParameterUsage {
	return sqlParameterUsage{
		order: make([]string, 0, size),
		used:  make(map[string]struct{}, size),
	}
}

func validateSQLParameterDefinition(name, duplicateVerb string, exists bool) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: SQL parameter name must not be empty", sql.ErrUnsupportedSQL)
	}
	if name == "sender" {
		return fmt.Errorf("%w: SQL parameter :sender is reserved for caller identity", sql.ErrUnsupportedSQL)
	}
	if exists {
		return fmt.Errorf("%w: SQL parameter :%s is %s more than once", sql.ErrUnsupportedSQL, name, duplicateVerb)
	}
	return nil
}

func (u *sqlParameterUsage) add(name string) {
	u.order = append(u.order, name)
}

func (u *sqlParameterUsage) markUsed(name string) {
	u.used[name] = struct{}{}
}

func (u sqlParameterUsage) validateUsed(verb string) error {
	for _, name := range u.order {
		if _, ok := u.used[name]; !ok {
			return fmt.Errorf("%w: SQL parameter :%s is %s but not used", sql.ErrUnsupportedSQL, name, verb)
		}
	}
	return nil
}

func sqlLiteralParameterName(lit sql.Literal) string {
	if lit.Param != "" {
		return lit.Param
	}
	return strings.TrimPrefix(lit.Text, ":")
}

type parameterSQLLiteral struct {
	kind  types.ValueKind
	value types.Value
}

type parameterSQLLiteralCompiler struct {
	runtimeSQLLiteralCompiler
	byName   map[string]parameterSQLLiteral
	usage    sqlParameterUsage
	usedVerb string
	missing  string
}

func newParameterSQLLiteralCompiler[P any](caller *types.Identity, parameters []P, usedVerb, missing string, entry func(P) (string, parameterSQLLiteral)) (*parameterSQLLiteralCompiler, error) {
	byName := make(map[string]parameterSQLLiteral, len(parameters))
	usage := newSQLParameterUsage(len(parameters))
	for _, parameter := range parameters {
		name, value := entry(parameter)
		_, exists := byName[name]
		if err := validateSQLParameterDefinition(name, usedVerb, exists); err != nil {
			return nil, err
		}
		byName[name] = value
		usage.add(name)
	}
	return &parameterSQLLiteralCompiler{
		runtimeSQLLiteralCompiler: runtimeSQLLiteralCompiler{caller: caller},
		byName:                    byName,
		usage:                     usage,
		usedVerb:                  usedVerb,
		missing:                   missing,
	}, nil
}

func newTemplateSQLLiteralCompiler(caller *types.Identity, parameters []SQLQueryParameter) (*parameterSQLLiteralCompiler, error) {
	return newParameterSQLLiteralCompiler(caller, parameters, "declared", "is not declared", func(parameter SQLQueryParameter) (string, parameterSQLLiteral) {
		return parameter.Name, parameterSQLLiteral{kind: parameter.Type, value: types.NewNull(parameter.Type)}
	})
}

func newBoundSQLLiteralCompiler(caller *types.Identity, parameters []SQLQueryParameterValue) (*parameterSQLLiteralCompiler, error) {
	return newParameterSQLLiteralCompiler(caller, parameters, "bound", "has no bound value", func(parameter SQLQueryParameterValue) (string, parameterSQLLiteral) {
		value := parameter.Value.Copy()
		return parameter.Name, parameterSQLLiteral{kind: value.Kind(), value: value}
	})
}

func (c *parameterSQLLiteralCompiler) compileSQLLiteral(lit sql.Literal, kind types.ValueKind, columnName string) (types.Value, error) {
	if lit.Kind != sql.LitParameter {
		return c.runtimeSQLLiteralCompiler.compileSQLLiteral(lit, kind, columnName)
	}
	name := sqlLiteralParameterName(lit)
	parameter, ok := c.byName[name]
	if !ok {
		return types.Value{}, fmt.Errorf("%w: SQL parameter :%s %s", sql.ErrUnsupportedSQL, name, c.missing)
	}
	if parameter.kind != kind {
		return types.Value{}, fmt.Errorf("%w: SQL parameter :%s type %s is incompatible with column %q type %s", sql.ErrUnsupportedSQL, name, sql.AlgebraicName(parameter.kind), columnName, sql.AlgebraicName(kind))
	}
	c.usage.markUsed(name)
	return parameter.value, nil
}

func (c *parameterSQLLiteralCompiler) validateUsed() error {
	return c.usage.validateUsed(c.usedVerb)
}

func singleSQLRelationTable(relations map[string]relationSchema) (schema.TableID, bool) {
	if len(relations) != 1 {
		return 0, false
	}
	for _, rel := range relations {
		return rel.id, true
	}
	return 0, false
}

func compileSQLPredicateForRelations(pred sql.Predicate, relations map[string]relationSchema, aliasTag func(string) uint8, literalCompiler sqlLiteralCompiler, allowColumnComparisons bool) (subscription.Predicate, error) {
	switch p := pred.(type) {
	case nil, sql.TruePredicate:
		table, ok := singleSQLRelationTable(relations)
		if !ok {
			return nil, nil
		}
		return subscription.AllRows{Table: table}, nil
	case sql.FalsePredicate:
		table, ok := singleSQLRelationTable(relations)
		if !ok {
			return nil, nil
		}
		return subscription.NoRows{Table: table}, nil
	case sql.ComparisonPredicate:
		return normalizeSQLFilterForRelations(p.Filter, relations, aliasTag, literalCompiler)
	case sql.NullPredicate:
		return normalizeSQLNullPredicateForRelations(p, relations, aliasTag)
	case sql.ColumnComparisonPredicate:
		if !allowColumnComparisons {
			return nil, fmt.Errorf("join WHERE column comparisons not supported")
		}
		return compileSQLColumnComparisonForRelations(p, relations, aliasTag)
	case sql.AndPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForRelations(child, relations, aliasTag, literalCompiler, allowColumnComparisons)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.And{Left: left, Right: right}
		})
	case sql.OrPredicate:
		return compileSQLBinaryPredicate(p.Left, p.Right, func(child sql.Predicate) (subscription.Predicate, error) {
			return compileSQLPredicateForRelations(child, relations, aliasTag, literalCompiler, allowColumnComparisons)
		}, func(left, right subscription.Predicate) subscription.Predicate {
			return subscription.Or{Left: left, Right: right}
		})
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

type compiledSQLColumnRef struct {
	table  schema.TableID
	column types.ColID
	schema *schema.ColumnSchema
	alias  uint8
}

func compileSQLColumnComparisonForRelations(pred sql.ColumnComparisonPredicate, relations map[string]relationSchema, aliasTag func(string) uint8) (subscription.Predicate, error) {
	if pred.Op != "=" {
		return nil, fmt.Errorf("join WHERE column comparisons only support '='")
	}
	left, err := compileSQLColumnRefForRelations(pred.Left, relations, aliasTag)
	if err != nil {
		return nil, err
	}
	right, err := compileSQLColumnRefForRelations(pred.Right, relations, aliasTag)
	if err != nil {
		return nil, err
	}
	if left.schema.Type != right.schema.Type {
		return nil, sql.UnexpectedTypeError{
			Expected: sql.AlgebraicName(right.schema.Type),
			Inferred: sql.AlgebraicName(left.schema.Type),
		}
	}
	if isArrayKind(left.schema.Type) {
		return nil, sql.InvalidOpError{
			Op:   "=",
			Type: sql.AlgebraicName(left.schema.Type),
		}
	}
	return subscription.ColEqCol{
		LeftTable:   left.table,
		LeftColumn:  left.column,
		LeftAlias:   left.alias,
		RightTable:  right.table,
		RightColumn: right.column,
		RightAlias:  right.alias,
	}, nil
}

func compileSQLColumnRefForRelations(ref sql.ColumnRef, relations map[string]relationSchema, aliasTag func(string) uint8) (compiledSQLColumnRef, error) {
	qualifier := ref.Alias
	if qualifier == "" {
		qualifier = ref.Table
	}
	rel, ok := relations[qualifier]
	if !ok {
		return compiledSQLColumnRef{}, sql.UnresolvedVarError{Name: qualifier}
	}
	col, ok := lookupSQLColumnExact(rel.ts, ref.Column)
	if !ok {
		return compiledSQLColumnRef{}, sql.UnresolvedVarError{Name: ref.Column}
	}
	return compiledSQLColumnRef{
		table:  rel.id,
		column: types.ColID(col.Index),
		schema: col,
		alias:  aliasTag(ref.Alias),
	}, nil
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

func normalizeSQLFilterForRelations(f sql.Filter, relations map[string]relationSchema, aliasTag func(string) uint8, literalCompiler sqlLiteralCompiler) (subscription.Predicate, error) {
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
	v, err := literalCompiler.compileSQLLiteral(f.Literal, col.Type, f.Column)
	if err != nil {
		return nil, coerceSQLLiteralError(f.Column, err)
	}
	return normalizePredicate(rel.id, col.Index, aliasTag(f.Alias), f.Op, v)
}

func coerceSQLLiteralError(column string, err error) error {
	// Reference-informed error types carry the source literal verbatim; do not
	// prefix with "coerce column" so diagnostics stay stable.
	var utErr sql.UnexpectedTypeError
	if errors.As(err, &utErr) {
		return err
	}
	var ilErr sql.InvalidLiteralError
	if errors.As(err, &ilErr) {
		return err
	}
	var exprErr sql.UnsupportedExprError
	if errors.As(err, &exprErr) {
		return err
	}
	return fmt.Errorf("coerce column %q: %v", column, err)
}

func normalizeSQLNullPredicateForRelations(p sql.NullPredicate, relations map[string]relationSchema, aliasTag func(string) uint8) (subscription.Predicate, error) {
	ref, err := compileSQLColumnRefForRelations(p.Column, relations, aliasTag)
	if err != nil {
		return nil, err
	}
	value := types.NewNull(ref.schema.Type)
	if p.Not {
		return subscription.ColNe{Table: ref.table, Column: ref.column, Alias: ref.alias, Value: value}, nil
	}
	return subscription.ColEq{Table: ref.table, Column: ref.column, Alias: ref.alias, Value: value}, nil
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
