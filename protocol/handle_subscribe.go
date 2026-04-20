package protocol

import (
	"fmt"

	"github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

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
		switch p.Op {
		case "", "=":
			eqs = append(eqs, subscription.ColEq{
				Table:  tableID,
				Column: types.ColID(col.Index),
				Value:  p.Value,
			})
		case "!=", "<>":
			eqs = append(eqs, subscription.ColNe{
				Table:  tableID,
				Column: types.ColID(col.Index),
				Value:  p.Value,
			})
		case ">":
			eqs = append(eqs, subscription.ColRange{
				Table:  tableID,
				Column: types.ColID(col.Index),
				Lower:  subscription.Bound{Value: p.Value, Inclusive: false},
				Upper:  subscription.Bound{Unbounded: true},
			})
		case ">=":
			eqs = append(eqs, subscription.ColRange{
				Table:  tableID,
				Column: types.ColID(col.Index),
				Lower:  subscription.Bound{Value: p.Value, Inclusive: true},
				Upper:  subscription.Bound{Unbounded: true},
			})
		case "<":
			eqs = append(eqs, subscription.ColRange{
				Table:  tableID,
				Column: types.ColID(col.Index),
				Lower:  subscription.Bound{Unbounded: true},
				Upper:  subscription.Bound{Value: p.Value, Inclusive: false},
			})
		case "<=":
			eqs = append(eqs, subscription.ColRange{
				Table:  tableID,
				Column: types.ColID(col.Index),
				Lower:  subscription.Bound{Unbounded: true},
				Upper:  subscription.Bound{Value: p.Value, Inclusive: true},
			})
		default:
			return nil, fmt.Errorf("unsupported comparison operator %q", p.Op)
		}
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
