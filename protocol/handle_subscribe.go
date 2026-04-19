package protocol

import (
	"fmt"

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

// NormalizePredicates converts a slice of wire-level Predicate (column
// name + value) into a single subscription.Predicate tree suitable for
// the evaluator. Empty predicates produce AllRows; a single predicate
// produces ColEq; multiple predicates are folded left into nested And
// nodes.
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
		eqs = append(eqs, subscription.ColEq{
			Table:  tableID,
			Column: types.ColID(col.Index),
			Value:  p.Value,
		})
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
