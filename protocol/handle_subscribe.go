package protocol

import (
	"fmt"
	"strings"

	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type compiledSQLQuery struct {
	TableName string
	Predicate subscription.Predicate
}

type relationSchema struct {
	id schema.TableID
	ts *schema.TableSchema
}

// SchemaLookup resolves table names to their schema-level identifiers
// and column metadata. The host wires the concrete implementation;
// the protocol layer uses it during subscribe/query handling to
// validate table + column references before forwarding to the executor.
type SchemaLookup interface {
	TableByName(name string) (schema.TableID, *schema.TableSchema, bool)
}

// compileQuery resolves a wire Query against the schema and returns the
// compiled subscription predicate. Errors carry context suitable for
// SubscriptionError.Error. Shared between handleSubscribeSingle and
// handleSubscribeMulti.
func compileQuery(q Query, sl SchemaLookup) (subscription.Predicate, error) {
	tableID, ts, ok := sl.TableByName(q.TableName)
	if !ok {
		return nil, fmt.Errorf("unknown table %q", q.TableName)
	}
	return NormalizePredicates(tableID, ts, q.Predicates)
}

func compileSQLQueryString(qs string, sl SchemaLookup) (compiledSQLQuery, error) {
	stmt, err := sql.Parse(qs)
	if err != nil {
		return compiledSQLQuery{}, fmt.Errorf("parse: %v", err)
	}
	if stmt.Join != nil {
		leftID, leftTS, ok := sl.TableByName(stmt.Join.LeftTable)
		if !ok {
			return compiledSQLQuery{}, fmt.Errorf("unknown table %q", stmt.Join.LeftTable)
		}
		rightID, rightTS, ok := sl.TableByName(stmt.Join.RightTable)
		if !ok {
			return compiledSQLQuery{}, fmt.Errorf("unknown table %q", stmt.Join.RightTable)
		}
		if !stmt.Join.HasOn {
			if stmt.Predicate != nil {
				return compiledSQLQuery{}, fmt.Errorf("cross join WHERE not supported")
			}
			projectedID, _, ok := sl.TableByName(stmt.ProjectedTable)
			if !ok {
				return compiledSQLQuery{}, fmt.Errorf("unknown table %q", stmt.ProjectedTable)
			}
			otherID := leftID
			if projectedID == leftID {
				otherID = rightID
			}
			return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: subscription.CrossJoinProjected{Projected: projectedID, Other: otherID}}, nil
		}
		leftCol, ok := leftTS.Column(stmt.Join.LeftOn.Column)
		if !ok {
			return compiledSQLQuery{}, fmt.Errorf("unknown column %q on table %q", stmt.Join.LeftOn.Column, leftTS.Name)
		}
		rightCol, ok := rightTS.Column(stmt.Join.RightOn.Column)
		if !ok {
			return compiledSQLQuery{}, fmt.Errorf("unknown column %q on table %q", stmt.Join.RightOn.Column, rightTS.Name)
		}
		relations := map[string]relationSchema{
			stmt.Join.LeftTable:  {id: leftID, ts: leftTS},
			stmt.Join.RightTable: {id: rightID, ts: rightTS},
		}
		// Self-join filter leaves need their Alias field stamped so MatchRowSide
		// can restrict each leaf to the side the user named. Distinct-table
		// joins leave the tag at zero: the Table check alone is sufficient.
		aliasTag := func(string) uint8 { return 0 }
		if leftID == rightID {
			rightAliasUpper := strings.ToUpper(stmt.Join.RightAlias)
			aliasTag = func(a string) uint8 {
				if strings.EqualFold(a, "") {
					return 0
				}
				if strings.ToUpper(a) == rightAliasUpper {
					return 1
				}
				return 0
			}
		}
		var filter subscription.Predicate
		if stmt.Predicate != nil {
			var err error
			filter, err = compileSQLPredicateForRelations(stmt.Predicate, relations, aliasTag)
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
			// Self-join: tag the two relation instances so validation and
			// canonical hashing distinguish them. Parser already enforces
			// distinct aliases at this point.
			join.LeftAlias = 0
			join.RightAlias = 1
		}
		return compiledSQLQuery{
			TableName: stmt.ProjectedTable,
			Predicate: join,
		}, nil
	}
	projectedID, ts, ok := sl.TableByName(stmt.ProjectedTable)
	if !ok {
		return compiledSQLQuery{}, fmt.Errorf("unknown table %q", stmt.ProjectedTable)
	}
	pred, err := compileSQLPredicateForRelations(stmt.Predicate, map[string]relationSchema{stmt.ProjectedTable: {id: projectedID, ts: ts}}, func(string) uint8 { return 0 })
	if err != nil {
		return compiledSQLQuery{}, err
	}
	return compiledSQLQuery{TableName: stmt.ProjectedTable, Predicate: pred}, nil
}

// parseQueryString turns a client-supplied SQL string into the internal
// Query form used by compileQuery. It resolves the table against the
// schema and coerces each literal against the matching column kind.
// Errors carry context suitable for SubscriptionError.Error /
// OneOffQueryResult.Error.
func parseQueryString(qs string, sl SchemaLookup) (Query, error) {
	stmt, err := sql.Parse(qs)
	if err != nil {
		return Query{}, fmt.Errorf("parse: %v", err)
	}
	_, ts, ok := sl.TableByName(stmt.Table)
	if !ok {
		return Query{}, fmt.Errorf("unknown table %q", stmt.Table)
	}
	q := Query{TableName: stmt.Table}
	for _, f := range stmt.Filters {
		col, ok := ts.Column(f.Column)
		if !ok {
			return Query{}, fmt.Errorf("unknown column %q on table %q", f.Column, ts.Name)
		}
		v, err := sql.Coerce(f.Literal, col.Type)
		if err != nil {
			return Query{}, fmt.Errorf("coerce column %q: %v", f.Column, err)
		}
		q.Predicates = append(q.Predicates, Predicate{Column: f.Column, Op: f.Op, Value: v})
	}
	return q, nil
}

func compileSQLPredicateForRelations(pred sql.Predicate, relations map[string]relationSchema, aliasTag func(string) uint8) (subscription.Predicate, error) {
	switch p := pred.(type) {
	case nil:
		if len(relations) != 1 {
			return nil, nil
		}
		for _, rel := range relations {
			return subscription.AllRows{Table: rel.id}, nil
		}
		return nil, nil
	case sql.ComparisonPredicate:
		return normalizeSQLFilterForRelations(p.Filter, relations, aliasTag)
	case sql.AndPredicate:
		left, err := compileSQLPredicateForRelations(p.Left, relations, aliasTag)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLPredicateForRelations(p.Right, relations, aliasTag)
		if err != nil {
			return nil, err
		}
		return subscription.And{Left: left, Right: right}, nil
	case sql.OrPredicate:
		left, err := compileSQLPredicateForRelations(p.Left, relations, aliasTag)
		if err != nil {
			return nil, err
		}
		right, err := compileSQLPredicateForRelations(p.Right, relations, aliasTag)
		if err != nil {
			return nil, err
		}
		return subscription.Or{Left: left, Right: right}, nil
	default:
		return nil, fmt.Errorf("unsupported SQL predicate %T", pred)
	}
}

func normalizeSQLFilterForRelations(f sql.Filter, relations map[string]relationSchema, aliasTag func(string) uint8) (subscription.Predicate, error) {
	rel, ok := relations[f.Table]
	if !ok {
		return nil, fmt.Errorf("unknown table %q in SQL filter", f.Table)
	}
	col, ok := rel.ts.Column(f.Column)
	if !ok {
		return nil, fmt.Errorf("unknown column %q on table %q", f.Column, rel.ts.Name)
	}
	v, err := sql.Coerce(f.Literal, col.Type)
	if err != nil {
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
			return nil, fmt.Errorf("unknown column %q on table %q", p.Column, ts.Name)
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
